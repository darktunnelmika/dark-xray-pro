// Package business holds durable PRO-native owner/representative credit and
// client records. It never reads a legacy database and never activates Xray.
package business

import (
 "context"
 "crypto/rand"
 "crypto/sha256"
 "database/sql"
 "encoding/hex"
 "encoding/json"
 "errors"
 "fmt"
 "math"
 "net/url"
 "os"
 "path/filepath"
 "regexp"
 "strings"
 "time"
 "unicode"
 "unicode/utf8"
 "github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
 _ "modernc.org/sqlite"
)
const (
 MaxVolumeCredit int64 = 1_000_000_000_000_000_000
 MaxUnlimitedCredit int64 = 1_000_000_000
 MaxDays = 3650
 MaxIPLimit = 1000
 SchemaVersion = 8
)
var (
 ErrInvalid = errors.New("invalid business operation")
 ErrConflict = errors.New("business event ID or identity conflict")
 ErrNotFound = errors.New("business object not found")
 ErrInsufficient = errors.New("representative has insufficient credit")
 ErrUnavailable = errors.New("business database unavailable")
 ErrUnsafePath = errors.New("private business database path is unsafe")
 idRE = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$")
 eventRE = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_.:-]{15,127}$")
)
type Store struct{ db *sql.DB;usage *usageledger.Ledger }
type Representative struct {
 ID string `json:"id"`
 DisplayName string `json:"display_name"`
 Kind string `json:"kind"`
 Enabled bool `json:"enabled"`
 VolumeCreditBytes int64 `json:"volume_credit_bytes"`
 UnlimitedCreditUnits int64 `json:"unlimited_credit_units"`
 CreatedUnix int64 `json:"created_unix"`
}
type CreditChange struct {
 EventID string `json:"event_id"`
 RepresentativeID string `json:"representative_id"`
 VolumeDeltaBytes int64 `json:"volume_delta_bytes"`
 UnlimitedDeltaUnits int64 `json:"unlimited_delta_units"`
 Reason string `json:"reason"`
}
type NewClient struct {
 EventID string `json:"event_id"`
 ClientID string `json:"client_id"`
 RepresentativeID string `json:"representative_id"`
 Name string `json:"name"`
 DurationDays int64 `json:"duration_days"`
 IPCount int64 `json:"ip_count"`
 VolumeBytes int64 `json:"volume_bytes"`
 Unlimited bool `json:"unlimited"`
}
type Client struct {
 ID string `json:"id"`
 RepresentativeID string `json:"representative_id"`
 Name string `json:"name"`
 UUID string `json:"uuid"`
 PlanKind string `json:"plan_kind"`
 IPCount int64 `json:"ip_count"`
 VolumeBytes int64 `json:"volume_bytes"`
 DurationDays int64 `json:"duration_days"`
 ChargeVolumeBytes int64 `json:"charged_volume_bytes"`
 ChargeUnlimitedUnits int64 `json:"charged_unlimited_units"`
 ExpiresUnix int64 `json:"expires_unix"`
 CreatedUnix int64 `json:"created_unix"`
 DeploymentState string `json:"deployment_state"`
 Archived bool `json:"archived"`
}
type Event struct {
 EventID string `json:"event_id"`
 Kind string `json:"kind"`
 RepresentativeID string `json:"representative_id"`
 ObjectID string `json:"object_id"`
 Reason string `json:"reason"`
 VolumeDeltaBytes int64 `json:"volume_delta_bytes"`
 UnlimitedDeltaUnits int64 `json:"unlimited_delta_units"`
 CreatedUnix int64 `json:"created_unix"`
}
func validName(v string, max int) bool {
 if !utf8.ValidString(v) || strings.TrimSpace(v)=="" || utf8.RuneCountInString(v)>max {return false}
 for _,r:=range v {if unicode.IsControl(r) {return false}}
 return true
}
func safeDelta(v,limit int64) bool {return v>=-limit && v<=limit}
func safeAdd(a,b,limit int64)(int64,bool){
 if !safeDelta(a,limit)||!safeDelta(b,limit) {return 0,false}
 if b>0 && a>limit-b || b<0 && a< -b {return 0,false}
 return a+b,true
}
func fingerprint(kind string, value any) string {
 b,_:=json.Marshal(struct{Kind string `json:"kind"`; Value any `json:"value"`}{kind,value})
 h:=sha256.Sum256(b);return hex.EncodeToString(h[:])
}
func randomUUID()(string,error){
 var b [16]byte;if _,err:=rand.Read(b[:]);err!=nil{return "",err}
 b[6]=(b[6]&0x0f)|0x40;b[8]=(b[8]&0x3f)|0x80
 raw:=hex.EncodeToString(b[:]);return raw[:8]+"-"+raw[8:12]+"-"+raw[12:16]+"-"+raw[16:20]+"-"+raw[20:],nil
}
// Refuse symlinked ancestor directories BEFORE creating files. This also
// rejects a renamed new-LAB state directory that points at legacy storage.
func checkPathAncestors(target string)error{
 for dir:=filepath.Dir(target);dir!="/"&&dir!=".";dir=filepath.Dir(dir){
  info,err:=os.Lstat(dir);if errors.Is(err,os.ErrNotExist){continue};if err!=nil{return err}
  if !info.IsDir()||info.Mode()&os.ModeSymlink!=0{return ErrUnsafePath}
 };return nil
}
func Open(path string)(*Store,error){
 if strings.TrimSpace(path)=="" {return nil,ErrUnsafePath}
 abs,err:=filepath.Abs(path);if err!=nil{return nil,err}
 if err=checkPathAncestors(abs);err!=nil{return nil,err}
 parent:=filepath.Dir(abs);if err:=os.MkdirAll(parent,0700);err!=nil{return nil,err}
 if err=checkPathAncestors(abs);err!=nil{return nil,err}
 p,err:=os.Lstat(parent);if err!=nil || !p.IsDir() || p.Mode().Perm()&0077!=0 {return nil,ErrUnsafePath}
 fd,err:=os.OpenFile(abs,os.O_CREATE|os.O_EXCL|os.O_RDWR,0600)
 switch {
 case err==nil:if e:=fd.Close();e!=nil{return nil,e}
 case errors.Is(err,os.ErrExist):
  info,e:=os.Lstat(abs);if e!=nil||!info.Mode().IsRegular()||info.Mode().Perm()&0077!=0 {return nil,ErrUnsafePath}
 default:return nil,err
 }
 // Correct ownership/permissions are enforced before opening SQLite;
 // no legacy database is opened or auto-migrated.
 u:=url.URL{Scheme:"file",Path:abs,RawQuery:"_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"}
 db,err:=sql.Open("sqlite",u.String());if err!=nil{return nil,err}
 db.SetMaxOpenConns(1);db.SetMaxIdleConns(1)
 fail:=func(e error)(*Store,error){_ =db.Close();return nil,e}
 if _,err=db.Exec("PRAGMA busy_timeout = 5000");err!=nil{return fail(err)}
 if _,err=db.Exec("PRAGMA foreign_keys = ON");err!=nil{return fail(err)}
 if _,err=db.Exec("PRAGMA journal_mode = WAL");err!=nil{return fail(err)}
 if _,err=db.Exec("PRAGMA synchronous = FULL");err!=nil{return fail(err)}
 if _,err=db.Exec("PRAGMA trusted_schema = OFF");err!=nil{return fail(err)}
 var version int;if err=db.QueryRow("PRAGMA user_version").Scan(&version);err!=nil{return fail(err)}
 if version!=0 && version!=1 && version!=2 && version!=3 && version!=4 && version!=5 && version!=6 && version!=7 && version!=SchemaVersion && version!=usageledger.ProbeBusinessSchemaVersion && version!=usageledger.HealthBusinessSchemaVersion{return fail(fmt.Errorf("business schema version %d is not supported: %w",version,ErrUnavailable))}
 if version==0 {
  // A pre-existing foreign SQLite file is NOT an empty PRO business DB.
  var foreignTable string
  scanErr:=db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' LIMIT 1").Scan(&foreignTable)
  if scanErr==nil{return fail(ErrUnsafePath)};if !errors.Is(scanErr,sql.ErrNoRows){return fail(scanErr)}
  if err=bootstrap(db);err!=nil{return fail(err)};version=1
 }
 if version==1 {if err=migrateIdentity(db);err!=nil{return fail(err)};version=2}
 if version==2 {if err=migrateGroups(db);err!=nil{return fail(err)};version=3}
 if version==3 {if err=migrateClientLifecycle(db);err!=nil{return fail(err)};version=4}
 if version==4 {if err=migrateNativeInbounds(db);err!=nil{return fail(err)};version=5}
 if version==5 {if err=migrateRuntimeReports(db);err!=nil{return fail(err)};version=6}
 if version==6 {if err=migrateInboundLifecycle(db);err!=nil{return fail(err)}}
 if err=ensureClientProfileEdits(db);err!=nil{return fail(err)}
 if err=ensureRepresentativeControls(db);err!=nil{return fail(err)}
 if err=ensureCatalog(db);err!=nil{return fail(err)}
 if err=ensureCatalogPolicy(db);err!=nil{return fail(err)}
 if err=ensureNativeRouteDrafts(db);err!=nil{return fail(err)}
 if err=ensureRouteTransactions(db);err!=nil{return fail(err)}
 if err=ensureMeteredRouteTransactions(db);err!=nil{return fail(err)}
 if err=ensureMeteredHandovers(db);err!=nil{return fail(err)}
 s:=&Store{db:db};if err=s.restoreUsageLedger(context.Background());err!=nil{return fail(err)}
 return s,nil
}
func bootstrap(db *sql.DB)error{
 tx,err:=db.Begin();if err!=nil{return err};defer tx.Rollback()
 statements:=[]string{
  "CREATE TABLE IF NOT EXISTS representatives (id TEXT PRIMARY KEY, display_name TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('owner','representative')), enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN(0,1)), volume_credit_bytes INTEGER NOT NULL DEFAULT 0 CHECK(volume_credit_bytes>=0 AND volume_credit_bytes<=1000000000000000000), unlimited_credit_units INTEGER NOT NULL DEFAULT 0 CHECK(unlimited_credit_units>=0 AND unlimited_credit_units<=1000000000), created_unix INTEGER NOT NULL)",
  "CREATE TABLE IF NOT EXISTS clients (id TEXT PRIMARY KEY, representative_id TEXT NOT NULL REFERENCES representatives(id), display_name TEXT NOT NULL, uuid TEXT NOT NULL UNIQUE, plan_kind TEXT NOT NULL CHECK(plan_kind IN ('volume','unlimited')), ip_count INTEGER NOT NULL CHECK(ip_count BETWEEN 0 AND 1000), volume_bytes INTEGER NOT NULL CHECK(volume_bytes>=0), duration_days INTEGER NOT NULL CHECK(duration_days BETWEEN 1 AND 3650), charged_volume_bytes INTEGER NOT NULL CHECK(charged_volume_bytes>=0), charged_unlimited_units INTEGER NOT NULL CHECK(charged_unlimited_units>=0), created_unix INTEGER NOT NULL, expires_unix INTEGER NOT NULL, deployment_state TEXT NOT NULL CHECK(deployment_state IN ('pending_node_apply','disabled')))",
  "CREATE INDEX IF NOT EXISTS clients_rep_id ON clients(representative_id,id)",
  "CREATE TABLE IF NOT EXISTS events (event_id TEXT PRIMARY KEY, fingerprint TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('credit_adjustment','client_created')), representative_id TEXT NOT NULL REFERENCES representatives(id), object_id TEXT NOT NULL, reason TEXT NOT NULL, volume_delta_bytes INTEGER NOT NULL, unlimited_delta_units INTEGER NOT NULL, created_unix INTEGER NOT NULL)",
  "CREATE INDEX IF NOT EXISTS events_rep_created ON events(representative_id,created_unix,event_id)",
  "CREATE TRIGGER IF NOT EXISTS events_immutable_update BEFORE UPDATE ON events BEGIN SELECT RAISE(ABORT,'business events are append-only'); END",
  "CREATE TRIGGER IF NOT EXISTS events_immutable_delete BEFORE DELETE ON events BEGIN SELECT RAISE(ABORT,'business events are append-only'); END",
  "INSERT OR IGNORE INTO representatives(id,display_name,kind,enabled,created_unix) VALUES('owner','Primary owner','owner',1,0)",
  "PRAGMA user_version = 1",
 }
 for _,statement:=range statements{if _,err=tx.Exec(statement);err!=nil{return err}};return tx.Commit()
}
func(s *Store)Close()error{return s.db.Close()}
func repScan(row interface{Scan(...any)error})(Representative,error){
 var r Representative;var enabled int
 err:=row.Scan(&r.ID,&r.DisplayName,&r.Kind,&enabled,&r.VolumeCreditBytes,&r.UnlimitedCreditUnits,&r.CreatedUnix)
 if err!=nil{return r,err};r.Enabled=enabled==1;return r,nil
}
const repColumns="id,display_name,kind,enabled,volume_credit_bytes,unlimited_credit_units,created_unix"
func readRep(ctx context.Context,tx *sql.Tx,id string)(Representative,error){return repScan(tx.QueryRowContext(ctx,"SELECT "+repColumns+" FROM representatives WHERE id=?",id))}
func(s *Store)Representative(ctx context.Context,id string)(Representative,error){
 r,err:=repScan(s.db.QueryRowContext(ctx,"SELECT "+repColumns+" FROM representatives WHERE id=?",id));if errors.Is(err,sql.ErrNoRows){return r,ErrNotFound};return r,err
}
func(s *Store)ListRepresentatives(ctx context.Context,limit,offset int)([]Representative,error){
 if limit<1||limit>200||offset<0||offset>1_000_000{return nil,ErrInvalid}
 rows,err:=s.db.QueryContext(ctx,"SELECT "+repColumns+" FROM representatives ORDER BY id LIMIT ? OFFSET ?",limit,offset);if err!=nil{return nil,err};defer rows.Close()
 result:=make([]Representative,0);for rows.Next(){r,e:=repScan(rows);if e!=nil{return nil,e};result=append(result,r)};return result,rows.Err()
}
// Creates only a PRO billing identity; browser credentials are separate.
func(s *Store)CreateRepresentative(ctx context.Context,id,name string)(Representative,error){
 if !idRE.MatchString(id)||id=="owner"||!validName(name,128){return Representative{},ErrInvalid}
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Representative{},err};defer tx.Rollback()
 r,err:=readRep(ctx,tx,id)
 if err==nil{if r.Kind!="representative"||r.DisplayName!=name{return r,ErrConflict};return r,nil}
 if !errors.Is(err,sql.ErrNoRows){return Representative{},err};now:=time.Now().UTC().Unix()
 _,err=tx.ExecContext(ctx,"INSERT INTO representatives(id,display_name,kind,enabled,created_unix) VALUES(?,?,'representative',1,?)",id,name,now);if err!=nil{return Representative{},err}
 r,err=readRep(ctx,tx,id);if err!=nil{return Representative{},err};if err=tx.Commit();err!=nil{return Representative{},err};return r,nil
}
func checkEvent(ctx context.Context,tx *sql.Tx,id,hash,kind string)(bool,error){
 var existingHash,existingKind string;err:=tx.QueryRowContext(ctx,"SELECT fingerprint,kind FROM events WHERE event_id=?",id).Scan(&existingHash,&existingKind)
 switch{case err==nil:if existingHash==hash&&existingKind==kind{return true,nil};return false,ErrConflict
 case errors.Is(err,sql.ErrNoRows):
  var count int
  if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM client_profile_edits WHERE event_id=?",id).Scan(&count);err!=nil{return false,err}
  if count>0{return false,ErrConflict}
  if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM representative_control_events WHERE event_id=?",id).Scan(&count);err!=nil{return false,err}
  if count>0{return false,ErrConflict}
  if err=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM catalog_events WHERE event_id=?",id).Scan(&count);err!=nil{return false,err}
  if count>0{return false,ErrConflict}
  if err:=tx.QueryRowContext(ctx,"SELECT COUNT(*) FROM catalog_policy_events WHERE event_id=?",id).Scan(&count);err!=nil{return false,err}
  if count>0{return false,ErrConflict}
  return false,nil
 default:return false,err}
}
func(s *Store)AdjustCredits(ctx context.Context,c CreditChange)(Representative,error){
 if !eventRE.MatchString(c.EventID)||!idRE.MatchString(c.RepresentativeID)||c.RepresentativeID=="owner"||!validName(c.Reason,200)||!safeDelta(c.VolumeDeltaBytes,MaxVolumeCredit)||!safeDelta(c.UnlimitedDeltaUnits,MaxUnlimitedCredit)||(c.VolumeDeltaBytes==0&&c.UnlimitedDeltaUnits==0){return Representative{},ErrInvalid}
 hash:=fingerprint("credit_adjustment",c);tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Representative{},err};defer tx.Rollback()
 replay,err:=checkEvent(ctx,tx,c.EventID,hash,"credit_adjustment");if err!=nil{return Representative{},err}
 r,err:=readRep(ctx,tx,c.RepresentativeID);if errors.Is(err,sql.ErrNoRows){return Representative{},ErrNotFound};if err!=nil{return Representative{},err};if replay{return r,nil}
 if !r.Enabled||r.Kind!="representative"{return Representative{},ErrConflict}
 volume,ok:=safeAdd(r.VolumeCreditBytes,c.VolumeDeltaBytes,MaxVolumeCredit);if !ok{return Representative{},ErrInsufficient}
 unlimited,ok:=safeAdd(r.UnlimitedCreditUnits,c.UnlimitedDeltaUnits,MaxUnlimitedCredit);if !ok{return Representative{},ErrInsufficient}
 if _,err=tx.ExecContext(ctx,"UPDATE representatives SET volume_credit_bytes=?, unlimited_credit_units=? WHERE id=?",volume,unlimited,c.RepresentativeID);err!=nil{return Representative{},err}
 _,err=tx.ExecContext(ctx,"INSERT INTO events(event_id,fingerprint,kind,representative_id,object_id,reason,volume_delta_bytes,unlimited_delta_units,created_unix) VALUES(?,?,'credit_adjustment',?,'',?,?,?,?)",c.EventID,hash,c.RepresentativeID,c.Reason,c.VolumeDeltaBytes,c.UnlimitedDeltaUnits,time.Now().UTC().Unix());if err!=nil{return Representative{},err}
 if err=tx.Commit();err!=nil{return Representative{},err};r.VolumeCreditBytes=volume;r.UnlimitedCreditUnits=unlimited;return r,nil
}
func clientScan(row interface{Scan(...any)error})(Client,error){
 var c Client;var archived int
 err:=row.Scan(&c.ID,&c.RepresentativeID,&c.Name,&c.UUID,&c.PlanKind,&c.IPCount,&c.VolumeBytes,&c.DurationDays,&c.ChargeVolumeBytes,&c.ChargeUnlimitedUnits,&c.CreatedUnix,&c.ExpiresUnix,&c.DeploymentState,&archived)
 c.Archived=archived==1;return c,err
}
const clientColumns="id,representative_id,display_name,uuid,plan_kind,ip_count,volume_bytes,duration_days,charged_volume_bytes,charged_unlimited_units,created_unix,expires_unix,deployment_state,archived"
func readClient(ctx context.Context,tx *sql.Tx,id string)(Client,error){return clientScan(tx.QueryRowContext(ctx,"SELECT "+clientColumns+" FROM clients WHERE id=?",id))}
func(s *Store)Client(ctx context.Context,id string)(Client,error){c,err:=clientScan(s.db.QueryRowContext(ctx,"SELECT "+clientColumns+" FROM clients WHERE id=?",id));if errors.Is(err,sql.ErrNoRows){return c,ErrNotFound};return c,err}
func(s *Store)ListClients(ctx context.Context,repID string,limit,offset int)([]Client,error){
 if (repID!=""&&!idRE.MatchString(repID))||limit<1||limit>200||offset<0||offset>1_000_000{return nil,ErrInvalid}
 statement:="SELECT "+clientColumns+" FROM clients";args:=[]any{}
 if repID!=""{statement+=" WHERE representative_id=?";args=append(args,repID)}
 statement+=" ORDER BY created_unix DESC,id LIMIT ? OFFSET ?";args=append(args,limit,offset)
 rows,err:=s.db.QueryContext(ctx,statement,args...);if err!=nil{return nil,err};defer rows.Close();result:=make([]Client,0)
 for rows.Next(){c,e:=clientScan(rows);if e!=nil{return nil,e};result=append(result,c)};return result,rows.Err()
}
func validClient(n NewClient)bool{
 if !eventRE.MatchString(n.EventID)||!idRE.MatchString(n.ClientID)||!idRE.MatchString(n.RepresentativeID)||!validName(n.Name,128)||n.DurationDays<1||n.DurationDays>MaxDays||n.IPCount<0||n.IPCount>MaxIPLimit{return false}
 if n.Unlimited{return n.VolumeBytes==0&&n.IPCount>0};return n.VolumeBytes>0&&n.VolumeBytes<=MaxVolumeCredit
}
// Atomic debit + identity + immutable event. Retries never charge twice.
func(s *Store)CreateClient(ctx context.Context,n NewClient)(Client,error){
 if !validClient(n){return Client{},ErrInvalid};chargeVolume,chargeUnits:=int64(0),int64(0);plan:="volume"
 if n.Unlimited{plan="unlimited";if n.DurationDays>math.MaxInt64/n.IPCount{return Client{},ErrInvalid};chargeUnits=(n.DurationDays*n.IPCount+29)/30}else{chargeVolume=n.VolumeBytes}
 hash:=fingerprint("client_created",n);tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return Client{},err};defer tx.Rollback()
 replay,err:=checkEvent(ctx,tx,n.EventID,hash,"client_created");if err!=nil{return Client{},err}
 if replay{c,err:=readClient(ctx,tx,n.ClientID);if errors.Is(err,sql.ErrNoRows){return c,ErrConflict};return c,err}
 r,err:=readRep(ctx,tx,n.RepresentativeID);if errors.Is(err,sql.ErrNoRows){return Client{},ErrNotFound};if err!=nil{return Client{},err};if !r.Enabled{return Client{},ErrConflict}
 // Identity conflicts precede balance checks, so insufficient credit cannot hide one.
 if _,existingErr:=readClient(ctx,tx,n.ClientID);existingErr==nil{return Client{},ErrConflict}else if !errors.Is(existingErr,sql.ErrNoRows){return Client{},existingErr}
 if r.Kind=="representative"{
  if r.VolumeCreditBytes<chargeVolume||r.UnlimitedCreditUnits<chargeUnits{return Client{},ErrInsufficient}
  if _,err=tx.ExecContext(ctx,"UPDATE representatives SET volume_credit_bytes=volume_credit_bytes-?,unlimited_credit_units=unlimited_credit_units-? WHERE id=?",chargeVolume,chargeUnits,r.ID);err!=nil{return Client{},err}
 }else if r.Kind!="owner"{return Client{},ErrConflict}
 uuid,err:=randomUUID();if err!=nil{return Client{},err};now:=time.Now().UTC().Unix();expires:=now+n.DurationDays*86400
 c:=Client{ID:n.ClientID,RepresentativeID:r.ID,Name:n.Name,UUID:uuid,PlanKind:plan,IPCount:n.IPCount,VolumeBytes:n.VolumeBytes,DurationDays:n.DurationDays,ChargeVolumeBytes:chargeVolume,ChargeUnlimitedUnits:chargeUnits,CreatedUnix:now,ExpiresUnix:expires,DeploymentState:"pending_node_apply"}
 _,err=tx.ExecContext(ctx,"INSERT INTO clients("+clientColumns+") VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)",c.ID,c.RepresentativeID,c.Name,c.UUID,c.PlanKind,c.IPCount,c.VolumeBytes,c.DurationDays,c.ChargeVolumeBytes,c.ChargeUnlimitedUnits,c.CreatedUnix,c.ExpiresUnix,c.DeploymentState,0);if err!=nil{return Client{},ErrConflict}
 volumeDelta,unitDelta:=int64(0),int64(0);if r.Kind=="representative"{volumeDelta,unitDelta=-chargeVolume,-chargeUnits}
 _,err=tx.ExecContext(ctx,"INSERT INTO events(event_id,fingerprint,kind,representative_id,object_id,reason,volume_delta_bytes,unlimited_delta_units,created_unix) VALUES(?,?,'client_created',?,?,?,?,?,?)",n.EventID,hash,r.ID,c.ID,"client_created",volumeDelta,unitDelta,now);if err!=nil{return Client{},err}
 if err=s.seedNewUsage(ctx,tx,c);err!=nil{return Client{},err}
 if err=tx.Commit();err!=nil{return Client{},err};return c,nil
}
func(s *Store)ListEvents(ctx context.Context,repID string,limit,offset int)([]Event,error){
 if (repID!=""&&!idRE.MatchString(repID))||limit<1||limit>200||offset<0||offset>1_000_000{return nil,ErrInvalid}
 q:="SELECT event_id,kind,representative_id,object_id,reason,volume_delta_bytes,unlimited_delta_units,created_unix FROM events";args:=[]any{}
 if repID!=""{q+=" WHERE representative_id=?";args=append(args,repID)};q+=" ORDER BY created_unix DESC,event_id LIMIT ? OFFSET ?";args=append(args,limit,offset)
 rows,err:=s.db.QueryContext(ctx,q,args...);if err!=nil{return nil,err};defer rows.Close();out:=make([]Event,0)
 for rows.Next(){var e Event;if err:=rows.Scan(&e.EventID,&e.Kind,&e.RepresentativeID,&e.ObjectID,&e.Reason,&e.VolumeDeltaBytes,&e.UnlimitedDeltaUnits,&e.CreatedUnix);err!=nil{return nil,err};out=append(out,e)};return out,rows.Err()
}
func migrateIdentity(db *sql.DB)error{
 tx,err:=db.Begin();if err!=nil{return err};defer tx.Rollback()
 statements:=[]string{
  "CREATE TABLE IF NOT EXISTS auth_users (id TEXT PRIMARY KEY, username TEXT NOT NULL UNIQUE COLLATE NOCASE, representative_id TEXT NOT NULL REFERENCES representatives(id), role TEXT NOT NULL CHECK(role IN ('owner','representative')), password_salt BLOB NOT NULL, password_hash BLOB NOT NULL, disabled INTEGER NOT NULL DEFAULT 0 CHECK(disabled IN(0,1)), created_unix INTEGER NOT NULL)",
  "CREATE UNIQUE INDEX IF NOT EXISTS unique_business_owner_login ON auth_users(role) WHERE role='owner'",
  "CREATE TABLE IF NOT EXISTS auth_sessions (token_sha256 TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES auth_users(id) ON DELETE CASCADE, created_unix INTEGER NOT NULL, expires_unix INTEGER NOT NULL)",
  "CREATE INDEX IF NOT EXISTS auth_sessions_user ON auth_sessions(user_id,expires_unix)",
  "CREATE INDEX IF NOT EXISTS auth_sessions_expiry ON auth_sessions(expires_unix)",
  "PRAGMA user_version=2",
 };for _,stmt:=range statements{if _,err=tx.Exec(stmt);err!=nil{return err}};return tx.Commit()
}
