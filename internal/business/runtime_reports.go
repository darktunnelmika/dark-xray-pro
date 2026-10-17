package business

import (
 "context"
 "database/sql"
 "encoding/json"
 "errors"
 "fmt"
 "time"
 "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
// Agent observations are not customer-connectivity or billing proof.
type RuntimeStatus struct {
 NoListeners bool `json:"no_listeners"`;
 NodeID string `json:"node_id"`;InstallationID string `json:"installation_id"`
 DesiredSequence uint64 `json:"desired_sequence"`;DesiredSHA string `json:"desired_sha"`
 ReportedSequence uint64 `json:"reported_sequence"`;ReportedSHA string `json:"reported_sha"`
 Phase string `json:"phase"`;State string `json:"state"`;ReportedAt string `json:"reported_at,omitempty"`
 LeaseExpiresAt string `json:"lease_expires_at,omitempty"`;ProcessReady bool `json:"process_ready"`
 DataPlaneVerified bool `json:"data_plane_verified"`;SubscriptionPublished bool `json:"subscription_published"`
}
func migrateRuntimeReports(db *sql.DB)error{
 tx,e:=db.Begin();if e!=nil{return e};defer tx.Rollback()
 statements:=[]string{
 "CREATE TABLE IF NOT EXISTS native_config_generation(id INTEGER PRIMARY KEY CHECK(id=1),value INTEGER NOT NULL CHECK(value>=1 AND value<=9007199254740991))",
 "INSERT OR IGNORE INTO native_config_generation VALUES(1,1)",
 "CREATE TABLE IF NOT EXISTS native_runtime_delivery(node_id TEXT NOT NULL,installation_id TEXT NOT NULL,issued INTEGER NOT NULL,expires INTEGER NOT NULL,sequence INTEGER NOT NULL,sha TEXT NOT NULL,manifest TEXT NOT NULL,generation INTEGER NOT NULL,PRIMARY KEY(node_id,installation_id))",
 "CREATE TABLE IF NOT EXISTS native_runtime_reports(node_id TEXT NOT NULL,installation_id TEXT NOT NULL,issued INTEGER NOT NULL,expires INTEGER NOT NULL,received INTEGER NOT NULL,generation INTEGER NOT NULL,payload TEXT NOT NULL,PRIMARY KEY(node_id,installation_id))",
 }
 // Invalidate old success before the next Agent pull; expiry is lease-bounded.
 for _,t:=range []struct{name,table,event string}{
 {"clients_new","clients","INSERT"},{"clients_config","clients","UPDATE OF uuid,archived,deployment_state,expires_unix"},
 {"representative_enabled","representatives","UPDATE OF enabled"},{"binding_add","native_inbound_clients","INSERT"},
 {"binding_remove","native_inbound_clients","DELETE"},{"target_add","native_inbound_targets","INSERT"},
 {"target_update","native_inbound_targets","UPDATE"},{"target_remove","native_inbound_targets","DELETE"},
 {"profile_update","native_inbounds","UPDATE"},
 }{statements=append(statements,fmt.Sprintf("CREATE TRIGGER IF NOT EXISTS native_dirty_%s AFTER %s ON %s BEGIN UPDATE native_config_generation SET value=value+1 WHERE id=1; END",t.name,t.event,t.table))}
 statements=append(statements,"PRAGMA user_version=6")
 for _,q:=range statements{if _,e=tx.Exec(q);e!=nil{return e}};return tx.Commit()
}
// Record the exact last HUB-issued delivery before sending its private payload.
func(s *Store)RecordNativeDelivery(ctx context.Context,c NodeConfig,l managedxray.Lease)error{
 if c.NodeID!=l.NodeID||c.InstallationID!=l.InstallationID||c.Sequence!=l.Sequence||c.ConfigSHA!=l.ConfigSHA||c.ManifestSHA!=l.ManifestSHA||l.Expires<=l.Issued{return ErrInvalid}
 tx,e:=s.db.BeginTx(ctx,nil);if e!=nil{return e};defer tx.Rollback();var generation uint64
 if e=tx.QueryRowContext(ctx,"SELECT value FROM native_config_generation WHERE id=1").Scan(&generation);e!=nil{return e};if generation!=c.Generation{return ErrConflict}
 result,e:=tx.ExecContext(ctx,`INSERT INTO native_runtime_delivery(node_id,installation_id,issued,expires,sequence,sha,manifest,generation)
 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(node_id,installation_id) DO UPDATE SET issued=excluded.issued,expires=excluded.expires,
 sequence=excluded.sequence,sha=excluded.sha,manifest=excluded.manifest,generation=excluded.generation
 WHERE excluded.issued>native_runtime_delivery.issued`,c.NodeID,c.InstallationID,l.Issued,l.Expires,c.Sequence,c.ConfigSHA,c.ManifestSHA,c.Generation)
 if e!=nil{return e};n,e:=result.RowsAffected();if e!=nil{return e};if n!=1{return ErrConflict};return tx.Commit()
}
func validObservation(o managedxray.Snapshot)bool{
 if o.DataPlaneVerified||o.SubscriptionPublished||o.ActiveSequence>o.Highwater{return false}
 if o.ProcessReady && o.NoListeners!=xrayconfig.IsStoppedHash(o.ActiveSHA){return false}
 phases:=map[string]bool{"awaiting_fresh_lease":true,"ready":true,"ready_observed_admission":true,"applying":true,"recovered_last_good":true,"apply_failed_stopped":true,"rolled_back_after_failed_apply":true,"rolled_back_after_process_exit":true,"process_exited_stopped":true,"lease_expired_stopped":true,"journal_failure_fenced":true,"closed":true}
 if !phases[o.Phase]{return false}
 hash:=func(v string)bool{if v==""{return true};if len(v)!=64{return false};for _,r:=range v{if !(r>='a'&&r<='f'||r>='0'&&r<='9'){return false}};return true}
 if !hash(o.ActiveSHA)||!hash(o.SavedSHA)||!hash(o.PreviousSHA){return false}
 if (o.ActiveSequence==0)!=(o.ActiveSHA=="")||(o.SavedSequence==0)!=(o.SavedSHA==""){return false}
 return !o.ProcessReady||(o.ActiveSequence>0&&o.LeaseExpires>0)
}
func(s *Store)AcceptNativeReport(ctx context.Context,report managedxray.RuntimeReport)error{
 l,o:=report.Lease,report.Observed
 if !validObservation(o)||len(l.NodeID)>64||len(l.InstallationID)!=32{return ErrInvalid}
 now:=time.Now().UnixNano();if l.Expires<=now||l.Issued>now+int64(time.Second)||l.Expires-l.Issued>int64(45*time.Second){return ErrConflict}
 tx,e:=s.db.BeginTx(ctx,nil);if e!=nil{return e};defer tx.Rollback()
 var issued,expires int64;var seq,gen uint64;var sha,manifest string
 e=tx.QueryRowContext(ctx,"SELECT issued,expires,sequence,sha,manifest,generation FROM native_runtime_delivery WHERE node_id=? AND installation_id=?",l.NodeID,l.InstallationID).Scan(&issued,&expires,&seq,&sha,&manifest,&gen)
 if errors.Is(e,sql.ErrNoRows){return ErrNotFound};if e!=nil{return e}
 if issued!=l.Issued||expires!=l.Expires||seq!=l.Sequence||sha!=l.ConfigSHA||manifest!=l.ManifestSHA||o.Highwater>seq||o.ActiveSequence>seq{return ErrConflict}
 if o.ProcessReady&&(o.LeaseExpires>expires||o.ActiveSequence==seq&&o.ActiveSHA!=sha){return ErrConflict}
 raw,e:=json.Marshal(o);if e!=nil{return e};var prior int64;var priorPayload string
 e=tx.QueryRowContext(ctx,"SELECT issued,payload FROM native_runtime_reports WHERE node_id=? AND installation_id=?",l.NodeID,l.InstallationID).Scan(&prior,&priorPayload)
 if e==nil{if prior==issued&&priorPayload==string(raw){return nil};if prior>=issued{return ErrConflict}}else if !errors.Is(e,sql.ErrNoRows){return e}
 _,e=tx.ExecContext(ctx,`INSERT INTO native_runtime_reports(node_id,installation_id,issued,expires,received,generation,payload)
 VALUES(?,?,?,?,?,?,?) ON CONFLICT(node_id,installation_id) DO UPDATE SET issued=excluded.issued,expires=excluded.expires,
 received=excluded.received,generation=excluded.generation,payload=excluded.payload`,l.NodeID,l.InstallationID,issued,expires,now,gen,string(raw))
 if e!=nil{return e};return tx.Commit()
}
func(s *Store)NativeRuntimeStatus(ctx context.Context,node,installation string)(RuntimeStatus,error){
 s0:=RuntimeStatus{NodeID:node,InstallationID:installation,State:"awaiting_report"}
 tx,e:=s.db.BeginTx(ctx,nil);if e!=nil{return s0,e};defer tx.Rollback();var generation uint64
 if e=tx.QueryRowContext(ctx,"SELECT value FROM native_config_generation WHERE id=1").Scan(&generation);e!=nil{return s0,e}
 e=tx.QueryRowContext(ctx,"SELECT sequence,config_sha FROM native_node_revisions WHERE node_id=? AND installation_id=?",node,installation).Scan(&s0.DesiredSequence,&s0.DesiredSHA)
 if errors.Is(e,sql.ErrNoRows){return s0,nil};if e!=nil{return s0,e}
 var received,expiry int64;var gen uint64;var raw string
 e=tx.QueryRowContext(ctx,"SELECT received,expires,generation,payload FROM native_runtime_reports WHERE node_id=? AND installation_id=?",node,installation).Scan(&received,&expiry,&gen,&raw)
 if errors.Is(e,sql.ErrNoRows){return s0,nil};if e!=nil{return s0,e}
 var o managedxray.Snapshot;if json.Unmarshal([]byte(raw),&o)!=nil||!validObservation(o){return s0,ErrUnavailable}
 s0.ReportedSequence=o.ActiveSequence;s0.ReportedSHA=o.ActiveSHA;s0.Phase=o.Phase;s0.NoListeners=o.NoListeners
 s0.ReportedAt=time.Unix(0,received).UTC().Format(time.RFC3339Nano)
 if o.ProcessReady&&o.LeaseExpires<expiry{expiry=o.LeaseExpires};s0.LeaseExpiresAt=time.Unix(0,expiry).UTC().Format(time.RFC3339Nano)
 now:=time.Now().UnixNano()
 switch{
 case now<received||now-received>int64(12*time.Second)||now>=expiry:s0.State="stale"
 case gen!=generation:s0.State="configuration_changed"
 case !o.ProcessReady:s0.State="stopped"
 case o.ActiveSHA!=s0.DesiredSHA||o.ActiveSequence!=s0.DesiredSequence:s0.State="out_of_sync"
 default:s0.State="applied";s0.ProcessReady=true;if o.NoListeners{s0.State="listeners_disabled"}
 };return s0,nil
}
func(s *Store)ListInboundTargets(ctx context.Context)([]InboundTarget,error){
 rows,e:=s.db.QueryContext(ctx,"SELECT inbound_id,node_id,installation_id,listen_ip,direct_port,swap_port FROM native_inbound_targets ORDER BY inbound_id,node_id LIMIT 16385")
 if e!=nil{return nil,e};defer rows.Close();out:=make([]InboundTarget,0)
 for rows.Next(){var t InboundTarget;if e=rows.Scan(&t.InboundID,&t.NodeID,&t.InstallationID,&t.ListenIP,&t.DirectPort,&t.SwapPort);e!=nil{return nil,e};out=append(out,t)}
 if len(out)>16384{return nil,ErrInvalid};return out,rows.Err()
}
func(s *Store)InboundClientIDs(ctx context.Context,id uint64)([]string,error){
 rows,e:=s.db.QueryContext(ctx,"SELECT client_id FROM native_inbound_clients WHERE inbound_id=? ORDER BY client_id LIMIT 20001",id)
 if e!=nil{return nil,e};defer rows.Close();ids:=make([]string,0)
 for rows.Next(){var id string;if e=rows.Scan(&id);e!=nil{return nil,e};ids=append(ids,id)}
 if len(ids)>20000{return nil,ErrInvalid};return ids,rows.Err()
}
