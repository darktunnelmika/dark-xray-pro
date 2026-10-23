package business

import (
 "context"
 "errors"
 "os"
 "path/filepath"
 "testing"
)

func TestUnlimitedRenewalUsesCumulativeUserMonthDifferenceNotRepeatedRounding(t *testing.T){
 s,path:=opened(t);ctx:=context.Background()
 newRep(t,s,"renew-rep")
 grant(t,s,"renew-rep","grant-renewal-00001",1000,50)
 client,err:=s.CreateClient(ctx,NewClient{
  EventID:"create-renewal-00001",ClientID:"c-renew",RepresentativeID:"renew-rep",
  Name:"Five users",DurationDays:30,IPCount:5,Unlimited:true,
 })
 if err!=nil{t.Fatal(err)}
 initialUUID,initialExpires:=client.UUID,client.ExpiresUnix
 if client.ChargeUnlimitedUnits!=5{t.Fatalf("initial charge %+v",client)}
 first:=ExtendClient{EventID:"extend-renewal-0001",ClientID:client.ID,AdditionalDays:15,Reason:"First partial month"}
 client,err=s.ExtendClient(ctx,first)
 if err!=nil||client.ChargeUnlimitedUnits!=8||client.DurationDays!=45{
  t.Fatalf("first delta must cost 3 units: %+v %v",client,err)
 }
 replay,err:=s.ExtendClient(ctx,first)
 if err!=nil||replay.ExpiresUnix!=client.ExpiresUnix||replay.UUID!=initialUUID{
  t.Fatalf("renewal retry duplicated expiry or UUID: %+v %v",replay,err)
 }
 second:=ExtendClient{EventID:"extend-renewal-0002",ClientID:client.ID,AdditionalDays:15,Reason:"Second partial month"}
 client,err=s.ExtendClient(ctx,second)
 if err!=nil||client.ChargeUnlimitedUnits!=10||client.DurationDays!=60||
  client.ExpiresUnix!=initialExpires+30*86400{
  t.Fatalf("two 15-day renewals must cost 5 additional units total: %+v %v",client,err)
 }
 balance,err:=s.Representative(ctx,"renew-rep")
 if err!=nil||balance.UnlimitedCreditUnits!=40{t.Fatalf("double-charge or lost credit: %+v %v",balance,err)}
 first.Reason="Tampered retry"
 if _,err=s.ExtendClient(ctx,first);!errors.Is(err,ErrConflict){
  t.Fatalf("same operation ID reused with different intent: %v",err)
 }
 ops,err:=s.ListClientOperations(ctx,"renew-rep",30,0)
 if err!=nil||len(ops)!=2||ops[0].UnlimitedCreditDebit+ops[1].UnlimitedCreditDebit!=5{
  t.Fatalf("missing exact charge audit: %+v %v",ops,err)
 }
 if err=s.Close();err!=nil{t.Fatal(err)}
 reloaded,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer reloaded.Close()
 stable,err:=reloaded.Client(ctx,client.ID)
 if err!=nil||stable.ExpiresUnix!=client.ExpiresUnix||stable.ChargeUnlimitedUnits!=10{
  t.Fatalf("restart lost renewal: %+v %v",stable,err)
 }
}
func TestVolumeRenewalAtomicCreditAndReplayUnderInsufficientBalance(t *testing.T){
 s,_:=opened(t);ctx:=context.Background()
 newRep(t,s,"vol-rep")
 grant(t,s,"vol-rep","grant-volume-renewal01",250,0)
 c,err:=s.CreateClient(ctx,NewClient{
  EventID:"new-volume-renewal001",ClientID:"volclient",RepresentativeID:"vol-rep",
  Name:"Unlimited IP volume",VolumeBytes:100,IPCount:0,DurationDays:30,
 })
 if err!=nil{t.Fatal(err)}
 req:=ExtendClient{EventID:"volume-renewal-00001",ClientID:c.ID,AdditionalVolumeBytes:120,AdditionalDays:15,Reason:"Volume and time"}
 c,err=s.ExtendClient(ctx,req)
 if err!=nil||c.VolumeBytes!=220||c.DurationDays!=45||c.ChargeVolumeBytes!=220||
  c.IPCount!=0{t.Fatalf("volume renewal wrong: %+v %v",c,err)}
 if _,err=s.ExtendClient(ctx,req);err!=nil{t.Fatalf("retry failed: %v",err)}
 bad:=ExtendClient{EventID:"volume-renewal-00002",ClientID:c.ID,AdditionalVolumeBytes:100,Reason:"Insufficient"}
 if _,err=s.ExtendClient(ctx,bad);!errors.Is(err,ErrInsufficient){
  t.Fatalf("unfunded volume renewal accepted: %v",err)
 }
 after,err:=s.Client(ctx,c.ID)
 if err!=nil||after.VolumeBytes!=220||after.DurationDays!=45{t.Fatalf("failed txn modified client: %+v %v",after,err)}
 r,err:=s.Representative(ctx,"vol-rep")
 if err!=nil||r.VolumeCreditBytes!=30{t.Fatalf("incorrect volume credit: %+v %v",r,err)}
 ops,err:=s.ListClientOperations(ctx,"vol-rep",20,0)
 if err!=nil||len(ops)!=1||ops[0].VolumeCreditDebit!=120{t.Fatalf("invalid immutable ops: %+v %v",ops,err)}
}
func TestDisableEnableAndArchiveRetainClientAndExistingLedgerAndGroups(t *testing.T){
 s,_:=opened(t);ctx:=context.Background()
 newRep(t,s,"state-rep")
 grant(t,s,"state-rep","grant-state-client-0001",500,10)
 c,err:=s.CreateClient(ctx,NewClient{EventID:"state-client-new-0001",
  ClientID:"retained-client",RepresentativeID:"state-rep",Name:"Retained",
  VolumeBytes:100,IPCount:0,DurationDays:30})
 if err!=nil{t.Fatal(err)}
 g,err:=s.CreateGroup(ctx,NewGroup{ID:"client-group-retained",RepresentativeID:"state-rep",Name:"Group"})
 if err!=nil{t.Fatal(err)}
 if _,err=s.AssignGroupMembers(ctx,g.ID,"state-rep",[]string{c.ID});err!=nil{t.Fatal(err)}
 disabled:=SetClientState{EventID:"disable-client-0001",ClientID:c.ID,Action:"disable",Reason:"Operator requested"}
 c,err=s.SetClientState(ctx,disabled)
 if err!=nil||c.DeploymentState!="disabled"||c.Archived{t.Fatalf("disable wrong: %+v %v",c,err)}
 if _,err=s.SetClientState(ctx,disabled);err!=nil{t.Fatalf("disable retry failed: %v",err)}
 enable:=SetClientState{EventID:"enable-client-00001",ClientID:c.ID,Action:"enable",Reason:"Operator enabled"}
 c,err=s.SetClientState(ctx,enable)
 if err!=nil||c.DeploymentState!="pending_node_apply"{t.Fatalf("enable falsely activated Xray: %+v %v",c,err)}
 archive:=SetClientState{EventID:"archive-client-0001",ClientID:c.ID,Action:"archive",Reason:"Completed service cleanup"}
 c,err=s.SetClientState(ctx,archive)
 if err!=nil||!c.Archived||c.DeploymentState!="disabled"{t.Fatalf("archival wrong: %+v %v",c,err)}
 if _,err=s.SetClientState(ctx,archive);err!=nil{t.Fatalf("idempotent archive retry failed: %v",err)}
 if _,err=s.SetClientState(ctx,SetClientState{EventID:"enable-archived-0001",ClientID:c.ID,Action:"enable",Reason:"Invalid"});!errors.Is(err,ErrConflict){
  t.Fatalf("archived account silently reactivated: %v",err)
 }
 if _,err=s.ExtendClient(ctx,ExtendClient{EventID:"extend-archive-00001",ClientID:c.ID,AdditionalDays:30,Reason:"Invalid"});!errors.Is(err,ErrConflict){
  t.Fatalf("archived user renewed: %v",err)
 }
 got,err:=s.Client(ctx,c.ID)
 if err!=nil||got.UUID!=c.UUID||!got.Archived{t.Fatalf("historical UUID removed: %+v %v",got,err)}
 group,err:=s.Group(ctx,g.ID)
 if err!=nil||group.MemberCount!=1{t.Fatalf("archived client disappeared from historical group: %+v %v",group,err)}
 events,err:=s.ListEvents(ctx,"state-rep",30,0)
 if err!=nil||len(events)!=2{t.Fatalf("archival modified immutable billing history: %+v %v",events,err)}
 rep,err:=s.Representative(ctx,"state-rep")
 if err!=nil||rep.VolumeCreditBytes!=400{t.Fatalf("archive refunded already spent credit: %+v %v",rep,err)}
 operations,err:=s.ListClientOperations(ctx,"state-rep",30,0)
 if err!=nil||len(operations)!=3{t.Fatalf("archive audit missing: %+v %v",operations,err)}
}
func TestClientLifecycleImmutabilitySchemaUpgradeAndEventCollision(t *testing.T){
 ctx:=context.Background()
 path:=filepath.Join(t.TempDir(),"private","business.sqlite")
 s,err:=Open(path);if err!=nil{t.Fatal(err)}
 newRep(t,s,"scope-rep")
 grant(t,s,"scope-rep","fund-scope-rep-0001",1000,20)
 c,err:=s.CreateClient(ctx,NewClient{
  EventID:"source-created-00001",ClientID:"scope-client",RepresentativeID:"scope-rep",
  Name:"Migration record",VolumeBytes:100,DurationDays:30})
 if err!=nil{t.Fatal(err)}
 // Simulate historical v3 database, then exercise automatic v3->v4 migration.
 if _,err=s.db.Exec("DROP TABLE client_operations");err!=nil{t.Fatal(err)}
 if _,err=s.db.Exec("ALTER TABLE clients DROP COLUMN archived");err!=nil{t.Fatal(err)}
 for _,q:=range []string{"DROP TRIGGER native_dirty_profile_update","DROP TRIGGER native_plan_block_enable","DROP TABLE native_inbound_operations","ALTER TABLE native_inbounds DROP COLUMN revision","ALTER TABLE native_inbounds DROP COLUMN archived","ALTER TABLE native_inbounds DROP COLUMN enabled","PRAGMA user_version=3"}{if _,err=s.db.Exec(q);err!=nil{t.Fatal(err)}}
 if err=s.Close();err!=nil{t.Fatal(err)}
 upgraded,err:=Open(path);if err!=nil{t.Fatal(err)}
 defer upgraded.Close()
 got,err:=upgraded.Client(ctx,c.ID)
 if err!=nil||got.UUID!=c.UUID||got.Archived{t.Fatalf("migration changed UUID/archive: %+v %v",got,err)}
 if _,err=upgraded.ExtendClient(ctx,ExtendClient{
  EventID:"fund-scope-rep-0001",ClientID:c.ID,AdditionalDays:30,Reason:"Collision with credit event",
 });!errors.Is(err,ErrConflict){t.Fatalf("finance event ID was reused: %v",err)}
 if _,err=upgraded.SetClientState(ctx,SetClientState{
  EventID:"state-scope-0000001",ClientID:c.ID,Action:"disable",Reason:"Test",
 });err!=nil{t.Fatal(err)}
 if _,err=upgraded.db.Exec("DELETE FROM client_operations");err==nil{t.Fatal("immutable operation audit deleted")}
 if _,err=upgraded.db.Exec("UPDATE client_operations SET kind='archive'");err==nil{t.Fatal("immutable operation audit edited")}
 if _,err=upgraded.db.Exec("INSERT INTO client_operations(event_id,fingerprint,client_id,representative_id,kind,reason,added_days,added_volume_bytes,volume_credit_debit,unlimited_credit_debit,created_unix) VALUES('external','any','invalid','scope-rep','disable','x',0,0,0,0,1)");err==nil{
  t.Fatal("foreign-key reference bypassed")
 }
 if _,err:=os.Stat(path);err!=nil{t.Fatal(err)}
}
