package business

import (
 "context"
 "crypto/rand"
 "database/sql"
 "encoding/json"
 "errors"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// InboundMutation is a compare-and-swap operation. It never accepts a private
// key from a browser. Archive is logical deletion: identity, bindings and
// financial history remain recoverable but the inbound has no listener.
type InboundMutation struct {
 EventID string `json:"event_id"`
 ExpectedRevision uint64 `json:"expected_revision"`
 Action string `json:"action"`
 Reason string `json:"reason"`
 Profile *InboundInput `json:"profile,omitempty"`
}
type InboundOperation struct {
 EventID string `json:"event_id"`
 InboundID uint64 `json:"inbound_id"`
 Action string `json:"action"`
 Reason string `json:"reason"`
 FromRevision uint64 `json:"from_revision"`
 ToRevision uint64 `json:"to_revision"`
 CreatedUnix int64 `json:"created_unix"`
}
func migrateInboundLifecycle(db *sql.DB)error{
 tx,err:=db.Begin();if err!=nil{return err};defer tx.Rollback()
 for _,q:=range []string{
  "ALTER TABLE native_inbounds ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN(0,1))",
  "ALTER TABLE native_inbounds ADD COLUMN archived INTEGER NOT NULL DEFAULT 0 CHECK(archived IN(0,1))",
  "ALTER TABLE native_inbounds ADD COLUMN revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1 AND revision<=9007199254740991)",
  "CREATE TABLE native_inbound_operations(event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),action TEXT NOT NULL CHECK(action IN('edit','disable','enable','archive')),reason TEXT NOT NULL,from_revision INTEGER NOT NULL,to_revision INTEGER NOT NULL,created_unix INTEGER NOT NULL)",
  "CREATE INDEX native_inbound_operations_inbound ON native_inbound_operations(inbound_id,to_revision)",
  "CREATE TRIGGER native_inbound_operations_no_update BEFORE UPDATE ON native_inbound_operations BEGIN SELECT RAISE(ABORT,'inbound operations are append-only'); END",
  "CREATE TRIGGER native_inbound_operations_no_delete BEFORE DELETE ON native_inbound_operations BEGIN SELECT RAISE(ABORT,'inbound operations are append-only'); END",
  "PRAGMA user_version=7",
 }{if _,err=tx.Exec(q);err!=nil{return err}}
 return tx.Commit()
}

type inboundReader interface{QueryRowContext(context.Context,string,...any)*sql.Row}
func readNativeInbound(ctx context.Context,q inboundReader,key []byte,id uint64)(NativeInbound,xrayconfig.Profile,error){
 var in NativeInbound;var raw []byte;var enabled,archived int
 err:=q.QueryRowContext(ctx,`SELECT id,name,profile_enc,public_key,enabled,archived,revision,
 (SELECT COUNT(*) FROM native_inbound_clients c WHERE c.inbound_id=native_inbounds.id)
 FROM native_inbounds WHERE id=?`,id).Scan(&in.ID,&in.Name,&raw,&in.PublicKey,&enabled,&archived,&in.Revision,&in.BoundClients)
 if errors.Is(err,sql.ErrNoRows){return in,xrayconfig.Profile{},ErrNotFound}
 if err!=nil{return in,xrayconfig.Profile{},err}
 p,err:=openProfile(key,raw,id);if err!=nil{return in,p,err}
 in.Transport=p.Transport;in.GRPCService=p.GRPCService;in.RealityTarget=p.RealityTarget
 in.ServerNames=p.ServerNames;in.ShortIDs=p.ShortIDs;in.Enabled=enabled==1;in.Archived=archived==1
 return in,p,nil
}
func(s *Store)NativeInbound(ctx context.Context,key []byte,id uint64)(NativeInbound,error){
 in,_,err:=readNativeInbound(ctx,s.db,key,id);return in,err
}
func validInboundMutation(id uint64,m InboundMutation)bool{
 if id==0||id>1<<53-1||!eventRE.MatchString(m.EventID)||m.ExpectedRevision==0||m.ExpectedRevision>=1<<53-1||!validName(m.Reason,200){return false}
 if m.Action=="edit"{return m.Profile!=nil&&m.Profile.ID==id&&validName(m.Profile.Name,128)}
 return m.Profile==nil&&(m.Action=="enable"||m.Action=="disable"||m.Action=="archive")
}
func(s *Store)MutateInbound(ctx context.Context,key []byte,id uint64,m InboundMutation)(NativeInbound,error){
 if !validInboundMutation(id,m){return NativeInbound{},ErrInvalid}
 hash:=fingerprint("native_inbound_operation",struct{ID uint64;Mutation InboundMutation}{id,m})
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return NativeInbound{},err};defer tx.Rollback()
 in,profile,err:=readNativeInbound(ctx,tx,key,id);if err!=nil{return NativeInbound{},err}
 var prior string
 err=tx.QueryRowContext(ctx,"SELECT fingerprint FROM native_inbound_operations WHERE event_id=?",m.EventID).Scan(&prior)
 if err==nil{if prior!=hash{return NativeInbound{},ErrConflict};return in,nil}
 if !errors.Is(err,sql.ErrNoRows){return NativeInbound{},err}
 if in.Archived||in.Revision!=m.ExpectedRevision{return NativeInbound{},ErrConflict}
 enabled,archived:=in.Enabled,in.Archived
 var encrypted []byte
 switch m.Action{
 case "edit":
  p:=m.Profile
  // Preserve the exact private key, public key, short IDs unless explicitly
  // edited, and every client binding. No rotate-on-edit fallback exists.
  profile.Transport=p.Transport;profile.GRPCService=p.GRPCService;profile.RealityTarget=p.RealityTarget
  profile.ServerNames=append([]string(nil),p.ServerNames...);profile.ShortIDs=append([]string(nil),p.ShortIDs...)
  profile.Users=[]xrayconfig.User{}
  if _,err=xrayconfig.RenderAssigned("profile-validation","00000000000000000000000000000000","127.0.0.1",
   []xrayconfig.Assignment{{Profile:profile,DirectPort:10443}});err!=nil{return NativeInbound{},ErrInvalid}
  a,err:=profileAEAD(key);if err!=nil{return NativeInbound{},err}
  raw,err:=json.Marshal(profile);if err!=nil{return NativeInbound{},err}
  nonce:=make([]byte,a.NonceSize());if _,err=rand.Read(nonce);err!=nil{return NativeInbound{},err}
  encrypted=a.Seal(nonce,nonce,raw,profileAAD(id));in.InboundInput=*p
 case "disable":if !enabled{return NativeInbound{},ErrConflict};enabled=false
 case "enable":if enabled{return NativeInbound{},ErrConflict};enabled=true
 case "archive":enabled=false;archived=true
 }
 var result sql.Result
 if m.Action=="edit"{
  result,err=tx.ExecContext(ctx,"UPDATE native_inbounds SET name=?,profile_enc=?,revision=revision+1 WHERE id=? AND revision=? AND archived=0",in.Name,encrypted,id,m.ExpectedRevision)
 }else{
  result,err=tx.ExecContext(ctx,"UPDATE native_inbounds SET enabled=?,archived=?,revision=revision+1 WHERE id=? AND revision=? AND archived=0",enabled,archived,id,m.ExpectedRevision)
 }
 if err!=nil{return NativeInbound{},err};count,err:=result.RowsAffected();if err!=nil{return NativeInbound{},err};if count!=1{return NativeInbound{},ErrConflict}
 _,err=tx.ExecContext(ctx,"INSERT INTO native_inbound_operations(event_id,fingerprint,inbound_id,action,reason,from_revision,to_revision,created_unix) VALUES(?,?,?,?,?,?,?,?)",
  m.EventID,hash,id,m.Action,m.Reason,m.ExpectedRevision,m.ExpectedRevision+1,time.Now().Unix())
 if err!=nil{return NativeInbound{},err}
 if err=tx.Commit();err!=nil{return NativeInbound{},err}
 in.Enabled=enabled;in.Archived=archived;in.Revision=m.ExpectedRevision+1
 return in,nil
}
func(s *Store)InboundOperations(ctx context.Context,id uint64,limit,offset int)([]InboundOperation,error){
 if id==0||id>1<<53-1||limit<1||limit>200||offset<0||offset>1_000_000{return nil,ErrInvalid}
 rows,err:=s.db.QueryContext(ctx,"SELECT event_id,inbound_id,action,reason,from_revision,to_revision,created_unix FROM native_inbound_operations WHERE inbound_id=? ORDER BY to_revision DESC LIMIT ? OFFSET ?",id,limit,offset)
 if err!=nil{return nil,err};defer rows.Close();out:=make([]InboundOperation,0)
 for rows.Next(){var o InboundOperation;if err=rows.Scan(&o.EventID,&o.InboundID,&o.Action,&o.Reason,&o.FromRevision,&o.ToRevision,&o.CreatedUnix);err!=nil{return nil,err};out=append(out,o)}
 return out,rows.Err()
}
