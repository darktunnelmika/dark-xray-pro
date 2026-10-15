package business

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "path/filepath"
 "strings"
 "testing"
 "time"
)
const inboundInstall="11111111111111111111111111111111"
func nativeInput()InboundInput{return InboundInput{ID:10,Name:"Native Reality",Transport:"tcp",RealityTarget:"www.example.org:443",ServerNames:[]string{"www.example.org"},ShortIDs:[]string{"aa11"}}}
func TestNativeClientBindingsRenderAuthoritativeUUIDsAndRevokeWithoutDeletion(t *testing.T){
 ctx:=context.Background();s,path:=opened(t);key:=bytes.Repeat([]byte{5},32);newRep(t,s,"native-rep");grant(t,s,"native-rep","grant-native-credit-001",10000,100)
 c,err:=s.CreateClient(ctx,NewClient{EventID:"client-native-create-01",ClientID:"native-client",RepresentativeID:"native-rep",Name:"Private",DurationDays:30,VolumeBytes:100,IPCount:0});if err!=nil{t.Fatal(err)}
 in,err:=s.CreateInbound(ctx,key,nativeInput());if err!=nil{t.Fatal(err)};if in.PublicKey==""{t.Fatal("public key missing")}
 if err=s.PutInboundTarget(ctx,InboundTarget{InboundID:10,NodeID:"new-node",InstallationID:inboundInstall,ListenIP:"127.0.0.1",DirectPort:19443,SwapPort:19444});err!=nil{t.Fatal(err)}
 if err=s.BindInboundClients(ctx,10,[]string{c.ID},true);err!=nil{t.Fatal(err)};first,err:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall)
 if err!=nil||first.UserCount!=1||first.Sequence!=1||!bytes.Contains(first.Candidate.Config,[]byte(c.UUID)){t.Fatalf("native identity not rendered: seq=%d users=%d %v",first.Sequence,first.UserCount,err)}
 if len(first.Candidate.Listeners)!=2||first.ListenIP!="127.0.0.1"{t.Fatal("direct/swap assignment lost")}
 repeated,err:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall);if err!=nil||repeated.Sequence!=first.Sequence||repeated.ConfigSHA!=first.ConfigSHA{t.Fatal("repeated build caused churn",err)}
 raw,_:=json.Marshal(first);if bytes.Contains(raw,[]byte("privateKey"))||bytes.Contains(raw,[]byte(c.UUID)){t.Fatal("browser summary leaked client/key")}
 rows,err:=s.ListInbounds(ctx,key);if err!=nil||len(rows)!=1||rows[0].BoundClients!=1{t.Fatal("list bindings failed",err)}
 var stored []byte;if err=s.db.QueryRow("SELECT profile_enc FROM native_inbounds WHERE id=10").Scan(&stored);err!=nil{t.Fatal(err)};if bytes.Contains(stored,[]byte("privateKey"))||bytes.Contains(stored,[]byte("reality_private_key")){t.Fatal("key stored in plaintext")}
 if _,err=s.ListInbounds(ctx,bytes.Repeat([]byte{9},32));!errors.Is(err,ErrUnavailable){t.Fatal("wrong encryption key accepted",err)}
 if err=s.BindInboundClients(ctx,10,[]string{c.ID,"not-found"},false);!errors.Is(err,ErrNotFound){t.Fatal("partial binding removal allowed",err)}
 if _,err=s.SetClientState(ctx,SetClientState{EventID:"disable-native-client-01",ClientID:c.ID,Action:"disable",Reason:"security revoke"});err!=nil{t.Fatal(err)}
 empty,err:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall);if err!=nil||empty.UserCount!=0||empty.Sequence!=2||bytes.Contains(empty.Candidate.Config,[]byte(c.UUID))||len(empty.Candidate.Listeners)!=2{t.Fatalf("last-user revocation unsafe: seq=%d users=%d %v",empty.Sequence,empty.UserCount,err)}
 if !bytes.Contains(empty.Candidate.Config,[]byte("\"clients\":[]")){t.Fatal("empty inbound was not kept to revoke last user")};if _,err=s.Client(ctx,c.ID);err!=nil{t.Fatal("revocation deleted client",err)}
 events,err:=s.ListEvents(ctx,"native-rep",100,0);if err!=nil||len(events)!=2{t.Fatal("inbound edit changed finance",err)}
 if _,err=s.BuildNodeConfig(ctx,key,"new-node",strings.Repeat("2",32));!errors.Is(err,ErrNotFound){t.Fatal("different installation inherited settings",err)}
 if err=s.Close();err!=nil{t.Fatal(err)};reopened,err:=Open(path);if err!=nil{t.Fatal(err)};defer reopened.Close();recovered,err:=reopened.BuildNodeConfig(ctx,key,"new-node",inboundInstall)
 if err!=nil||recovered.Sequence!=empty.Sequence||recovered.ConfigSHA!=empty.ConfigSHA{t.Fatal("restart lost authoritative revision",err)}
}
func TestNativeExpiryDisabledRepresentativeAndPortConflicts(t *testing.T){
 ctx:=context.Background();s,_:=opened(t);key:=bytes.Repeat([]byte{7},32);in:=nativeInput();if _,e:=s.CreateInbound(ctx,key,in);e!=nil{t.Fatal(e)};in.ID=11;if _,e:=s.CreateInbound(ctx,key,in);e!=nil{t.Fatal(e)}
 target:=InboundTarget{InboundID:10,NodeID:"new-node",InstallationID:inboundInstall,ListenIP:"127.0.0.1",DirectPort:19600};if e:=s.PutInboundTarget(ctx,target);e!=nil{t.Fatal(e)}
 target.InboundID=11;if e:=s.PutInboundTarget(ctx,target);!errors.Is(e,ErrConflict){t.Fatal("two profiles share port",e)};target.DirectPort=19601;target.ListenIP="0.0.0.0";if e:=s.PutInboundTarget(ctx,target);!errors.Is(e,ErrInvalid){t.Fatal("wildcard allowed",e)}
 newRep(t,s,"expiry-rep");grant(t,s,"expiry-rep","grant-inbound-expiry-001",1000,0);c,e:=s.CreateClient(ctx,NewClient{EventID:"new-inbound-expiry-001",ClientID:"expiry-c",RepresentativeID:"expiry-rep",Name:"Expiring",DurationDays:1,VolumeBytes:10});if e!=nil{t.Fatal(e)}
 if e=s.BindInboundClients(ctx,10,[]string{c.ID},true);e!=nil{t.Fatal(e)};expiry:=time.Now().Add(5*time.Second).Unix();if _,e=s.db.Exec("UPDATE clients SET expires_unix=? WHERE id=?",expiry,c.ID);e!=nil{t.Fatal(e)}
 cfg,e:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall);if e!=nil||cfg.UserCount!=1||cfg.LeaseNotAfter>expiry*int64(time.Second){t.Fatal("lease can exceed client expiry",e)}
 if _,e=s.db.Exec("UPDATE representatives SET enabled=0 WHERE id='expiry-rep'");e!=nil{t.Fatal(e)};revoked,e:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall);if e!=nil||revoked.UserCount!=0{t.Fatal("disabled representative retained access",e)}
 if _,e=s.db.Exec("UPDATE representatives SET enabled=1 WHERE id='expiry-rep'");e!=nil{t.Fatal(e)};if _,e=s.db.Exec("UPDATE clients SET expires_unix=1 WHERE id=?",c.ID);e!=nil{t.Fatal(e)}
 revoked,e=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall);if e!=nil||revoked.UserCount!=0{t.Fatal("expired client retained access",e)}
}
func TestNativeV4SchemaUpgradePreservesPriorData(t *testing.T){
 ctx:=context.Background();path:=filepath.Join(t.TempDir(),"private","test.sqlite");s,e:=Open(path);if e!=nil{t.Fatal(e)};newRep(t,s,"upgrade-native");grant(t,s,"upgrade-native","grant-native-upgrade001",3000,20)
 for _,q:=range []string{"DROP TABLE native_inbound_clients","DROP TABLE native_inbound_targets","DROP TABLE native_inbounds","DROP TABLE native_node_revisions","PRAGMA user_version=4"}{if _,e=s.db.Exec(q);e!=nil{t.Fatal(e)}};if e=s.Close();e!=nil{t.Fatal(e)}
 s,e=Open(path);if e!=nil{t.Fatal(e)};defer s.Close();r,e:=s.Representative(ctx,"upgrade-native");if e!=nil||r.VolumeCreditBytes!=3000||r.UnlimitedCreditUnits!=20{t.Fatal("schema migration lost credit",e)};if _,e=s.CreateInbound(ctx,bytes.Repeat([]byte{7},32),nativeInput());e!=nil{t.Fatal(e)}
}
