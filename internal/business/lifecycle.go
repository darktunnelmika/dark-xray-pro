package business

import (
 "context"
 "database/sql"
 "errors"
 "time"
)

type ExtendClient struct {
 EventID string `json:"event_id"`
 ClientID string `json:"client_id"`
 AdditionalDays int64 `json:"additional_days"`
 AdditionalVolumeBytes int64 `json:"additional_volume_bytes"`
 Reason string `json:"reason"`
}
type SetClientState struct {
 EventID string `json:"event_id"`
 ClientID string `json:"client_id"`
 Action string `json:"action"` // disable | enable | archive
 Reason string `json:"reason"`
}
type ClientOperation struct{
 EventID string `json:"event_id"`
 ClientID string `json:"client_id"`
 RepresentativeID string `json:"representative_id"`
 Kind string `json:"kind"`
 Reason string `json:"reason"`
 AddedDays int64 `json:"added_days"`
 AddedVolumeBytes int64 `json:"added_volume_bytes"`
 VolumeCreditDebit int64 `json:"volume_credit_debit"`
 UnlimitedCreditDebit int64 `json:"unlimited_credit_debit"`
 CreatedUnix int64 `json:"created_unix"`
}
func migrateClientLifecycle(db *sql.DB)error{
 tx,err:=db.Begin();if err!=nil{return err}
 defer tx.Rollback()
 statements:=[]string{
  "ALTER TABLE clients ADD COLUMN archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN(0,1))",
  "CREATE TABLE client_operations (event_id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, client_id TEXT NOT NULL REFERENCES clients(id), representative_id TEXT NOT NULL REFERENCES representatives(id), kind TEXT NOT NULL CHECK(kind IN ('extend','disable','enable','archive')), reason TEXT NOT NULL, added_days INTEGER NOT NULL CHECK(added_days>=0), added_volume_bytes INTEGER NOT NULL CHECK(added_volume_bytes>=0), volume_credit_debit INTEGER NOT NULL CHECK(volume_credit_debit>=0), unlimited_credit_debit INTEGER NOT NULL CHECK(unlimited_credit_debit>=0), created_unix INTEGER NOT NULL)",
  "CREATE INDEX client_operations_rep_created ON client_operations(representative_id,created_unix,event_id)",
  "CREATE INDEX client_operations_client_created ON client_operations(client_id,created_unix,event_id)",
  "CREATE TRIGGER client_operations_immutable_update BEFORE UPDATE ON client_operations BEGIN SELECT RAISE(ABORT,'client operations are append-only'); END",
  "CREATE TRIGGER client_operations_immutable_delete BEFORE DELETE ON client_operations BEGIN SELECT RAISE(ABORT,'client operations are append-only'); END",
  "PRAGMA user_version=4",
 }
 for _,q:=range statements {if _,err=tx.Exec(q);err!=nil{return err}}
 return tx.Commit()
}
func checkClientOp(ctx context.Context,tx *sql.Tx,eventID,fp,kind string)(bool,error){
 var hash,oldKind string
 err:=tx.QueryRowContext(ctx,"SELECT fingerprint,kind FROM client_operations WHERE event_id=?",eventID).Scan(&hash,&oldKind)
 switch{
 case err==nil:if hash==fp&&oldKind==kind{return true,nil};return false,ErrConflict
 case errors.Is(err,sql.ErrNoRows):
  // One shared business event namespace across independent credit and client
  // operation tables; a reused credit event ID is never allowed.
  var count int
  if err:=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM events WHERE event_id=?",eventID).Scan(&count);err!=nil{return false,err}
  if count>0{return false,ErrConflict}
  if err:=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM client_profile_edits WHERE event_id=?",eventID).Scan(&count);err!=nil{return false,err}
  if count>0{return false,ErrConflict}
  return false,nil
 default:return false,err
 }
}
func positiveAdditional(req ExtendClient) bool {
 return eventRE.MatchString(req.EventID)&&idRE.MatchString(req.ClientID)&&
  validName(req.Reason,200)&&
  req.AdditionalDays>=0&&req.AdditionalDays<=MaxDays&&
  req.AdditionalVolumeBytes>=0&&req.AdditionalVolumeBytes<=MaxVolumeCredit&&
  (req.AdditionalDays>0||req.AdditionalVolumeBytes>0)
}
func (s *Store)ExtendClient(ctx context.Context,req ExtendClient)(Client,error){
 if !positiveAdditional(req){return Client{},ErrInvalid}
 hash:=fingerprint("extend",req)
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Client{},err}
 defer tx.Rollback()
 replay,err:=checkClientOp(ctx,tx,req.EventID,hash,"extend")
 if err!=nil{return Client{},err}
 c,err:=readClient(ctx,tx,req.ClientID)
 if errors.Is(err,sql.ErrNoRows){return Client{},ErrNotFound}
 if err!=nil{return Client{},err}
 if replay{return c,nil}
 if c.Archived{return Client{},ErrConflict}
 rep,err:=readRep(ctx,tx,c.RepresentativeID)
 if err!=nil{return Client{},err}
 if !rep.Enabled{return Client{},ErrConflict}
 if c.DurationDays>MaxDays-req.AdditionalDays{return Client{},ErrInvalid}
 days:=c.DurationDays+req.AdditionalDays
 newVolume,ok:=safeAdd(c.VolumeBytes,req.AdditionalVolumeBytes,MaxVolumeCredit)
 if !ok{return Client{},ErrInvalid}
 if c.PlanKind=="unlimited"&&req.AdditionalVolumeBytes!=0{return Client{},ErrInvalid}
 if c.PlanKind!="unlimited"&&c.PlanKind!="volume"{return Client{},ErrConflict}
 volumeCost,unitsCost:=int64(0),int64(0)
 switch c.PlanKind{
 case "unlimited":
  // Charge the differential of cumulatively rounded user-month capacity,
  // not ceil(additional days * users / 30) on each renewal. This prevents
  // overcharging two separate 15-day extensions versus one 30-day extension.
  total:=(days*c.IPCount+29)/30
  if total<c.ChargeUnlimitedUnits{return Client{},ErrConflict}
  unitsCost=total-c.ChargeUnlimitedUnits
 case "volume":
  volumeCost=req.AdditionalVolumeBytes
 }
 if rep.Kind=="representative"{
  if rep.VolumeCreditBytes<volumeCost||rep.UnlimitedCreditUnits<unitsCost{return Client{},ErrInsufficient}
  if _,err=tx.ExecContext(ctx,
   "UPDATE representatives SET volume_credit_bytes=volume_credit_bytes-?,unlimited_credit_units=unlimited_credit_units-? WHERE id=?",
   volumeCost,unitsCost,rep.ID);err!=nil{return Client{},err}
 }else if rep.Kind!="owner"{return Client{},ErrConflict}
 expires:=c.ExpiresUnix
 if now:=time.Now().UTC().Unix();expires<now&&req.AdditionalDays>0{expires=now}
 expires+=req.AdditionalDays*86400
 newChargedVolume,ok:=safeAdd(c.ChargeVolumeBytes,volumeCost,MaxVolumeCredit)
 if !ok{return Client{},ErrInvalid}
 newChargedUnits,ok:=safeAdd(c.ChargeUnlimitedUnits,unitsCost,MaxUnlimitedCredit)
 if !ok{return Client{},ErrInvalid}
 if _,err=tx.ExecContext(ctx,
  "UPDATE clients SET duration_days=?,volume_bytes=?,charged_volume_bytes=?,charged_unlimited_units=?,expires_unix=? WHERE id=?",
  days,newVolume,newChargedVolume,newChargedUnits,expires,c.ID);err!=nil{return Client{},err}
 now:=time.Now().UTC().Unix()
 if _,err=tx.ExecContext(ctx,
  "INSERT INTO client_operations(event_id,fingerprint,client_id,representative_id,kind,reason,added_days,added_volume_bytes,volume_credit_debit,unlimited_credit_debit,created_unix) VALUES(?,?,?,?,'extend',?,?,?,?,?,?)",
  req.EventID,hash,c.ID,c.RepresentativeID,req.Reason,req.AdditionalDays,req.AdditionalVolumeBytes,
  volumeCost,unitsCost,now);err!=nil{return Client{},err}
 if err=tx.Commit();err!=nil{return Client{},err}
 c.DurationDays=days;c.VolumeBytes=newVolume;c.ChargeVolumeBytes=newChargedVolume
 c.ChargeUnlimitedUnits=newChargedUnits;c.ExpiresUnix=expires
 return c,nil
}
func validClientState(req SetClientState)bool{
 return eventRE.MatchString(req.EventID)&&idRE.MatchString(req.ClientID)&&
  validName(req.Reason,200)&&
  (req.Action=="disable"||req.Action=="enable"||req.Action=="archive")
}
func(s *Store)SetClientState(ctx context.Context,req SetClientState)(Client,error){
 if !validClientState(req){return Client{},ErrInvalid}
 hash:=fingerprint(req.Action,req)
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Client{},err}
 defer tx.Rollback()
 replay,err:=checkClientOp(ctx,tx,req.EventID,hash,req.Action)
 if err!=nil{return Client{},err}
 c,err:=readClient(ctx,tx,req.ClientID)
 if errors.Is(err,sql.ErrNoRows){return Client{},ErrNotFound}
 if err!=nil{return Client{},err}
 if replay{return c,nil}
 if c.Archived{return Client{},ErrConflict}
 newStatus,newArchived:=c.DeploymentState,0
 switch req.Action{
 case "disable":
  if c.DeploymentState=="disabled"{return Client{},ErrConflict}
  newStatus="disabled"
 case "enable":
  if c.DeploymentState!="disabled"{return Client{},ErrConflict}
  // Enabling the logical record never activates Xray. It simply marks the
  // client ready for a future, independently acknowledged config apply.
  newStatus="pending_node_apply"
 case "archive":
  newStatus="disabled"
  newArchived=1
 }
 if _,err=tx.ExecContext(ctx,"UPDATE clients SET deployment_state=?,archived=? WHERE id=?",newStatus,newArchived,c.ID);err!=nil{return Client{},err}
 now:=time.Now().UTC().Unix()
 if _,err=tx.ExecContext(ctx,
  "INSERT INTO client_operations(event_id,fingerprint,client_id,representative_id,kind,reason,added_days,added_volume_bytes,volume_credit_debit,unlimited_credit_debit,created_unix) VALUES(?,?,?,?,?,?,0,0,0,0,?)",
  req.EventID,hash,c.ID,c.RepresentativeID,req.Action,req.Reason,now);err!=nil{return Client{},err}
 if err=tx.Commit();err!=nil{return Client{},err}
 c.DeploymentState=newStatus;c.Archived=newArchived==1
 return c,nil
}
func (s *Store)ListClientOperations(ctx context.Context,repID string,limit,offset int)([]ClientOperation,error){
 if (repID!=""&&!idRE.MatchString(repID))||limit<1||limit>200||offset<0||offset>1_000_000{
  return nil,ErrInvalid
 }
 q:="SELECT event_id,client_id,representative_id,kind,reason,added_days,added_volume_bytes,volume_credit_debit,unlimited_credit_debit,created_unix FROM client_operations"
 args:=[]any{}
 if repID!=""{q+=" WHERE representative_id=?";args=append(args,repID)}
 q+=" ORDER BY created_unix DESC,event_id LIMIT ? OFFSET ?"
 args=append(args,limit,offset)
 rows,err:=s.db.QueryContext(ctx,q,args...);if err!=nil{return nil,err}
 defer rows.Close()
 out:=make([]ClientOperation,0)
 for rows.Next(){
  var e ClientOperation
  if err=rows.Scan(&e.EventID,&e.ClientID,&e.RepresentativeID,&e.Kind,&e.Reason,
   &e.AddedDays,&e.AddedVolumeBytes,&e.VolumeCreditDebit,&e.UnlimitedCreditDebit,&e.CreatedUnix);err!=nil{return nil,err}
  out=append(out,e)
 }
 return out,rows.Err()
}
