package business

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "sync"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
func TestInboundLifecycleRetainsKeyClientsCreditAndExplicitlyStopsLastListener(t *testing.T){
 ctx:=context.Background();s,path:=opened(t);key:=bytes.Repeat([]byte{7},32)
 newRep(t,s,"life-inbound-rep");grant(t,s,"life-inbound-rep","grant-inbound-life-001",1000,100)
 client,err:=s.CreateClient(ctx,NewClient{EventID:"create-inbound-life-001",ClientID:"life-client",RepresentativeID:"life-inbound-rep",Name:"Preserved",DurationDays:30,VolumeBytes:100})
 if err!=nil{t.Fatal(err)}
 in,err:=s.CreateInbound(ctx,key,nativeInput());if err!=nil{t.Fatal(err)}
 if !in.Enabled||in.Archived||in.Revision!=1{t.Fatalf("initial lifecycle state %+v",in)}
 if err=s.BindInboundClients(ctx,10,[]string{client.ID},true);err!=nil{t.Fatal(err)}
 target:=InboundTarget{InboundID:10,NodeID:"new-node",InstallationID:inboundInstall,ListenIP:"127.0.0.1",DirectPort:19620,SwapPort:19621}
 if err=s.PutInboundTarget(ctx,target);err!=nil{t.Fatal(err)}
 first,err:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall);if err!=nil{t.Fatal(err)}
 var encrypted []byte;if err=s.db.QueryRow("SELECT profile_enc FROM native_inbounds WHERE id=10").Scan(&encrypted);err!=nil{t.Fatal(err)}
 oldProfile,err:=openProfile(key,encrypted,10);if err!=nil{t.Fatal(err)}
 edited:=nativeInput();edited.Name="نام جدید";edited.Transport="grpc";edited.GRPCService="pro-service";edited.ServerNames=[]string{"new.example.org"}
 edit:=InboundMutation{EventID:"edit-inbound-life-0001",ExpectedRevision:1,Action:"edit",Reason:"Owner edited profile",Profile:&edited}
 got,err:=s.MutateInbound(ctx,key,10,edit)
 if err!=nil||got.PublicKey!=in.PublicKey||got.Revision!=2||got.BoundClients!=1{t.Fatalf("edit lost key or bindings %+v %v",got,err)}
 if err=s.db.QueryRow("SELECT profile_enc FROM native_inbounds WHERE id=10").Scan(&encrypted);err!=nil{t.Fatal(err)}
 newProfile,err:=openProfile(key,encrypted,10)
 if err!=nil||newProfile.RealityPrivateKey!=oldProfile.RealityPrivateKey{t.Fatal("normal edit rotated private key",err)}
 if replay,e:=s.MutateInbound(ctx,key,10,edit);e!=nil||replay.Revision!=2{t.Fatal("edit retry duplicated mutation",e)}
 changed:=edit;changed.Reason="altered replay";if _,e:=s.MutateInbound(ctx,key,10,changed);!errors.Is(e,ErrConflict){t.Fatal("altered event accepted",e)}
 stale:=InboundMutation{EventID:"stale-inbound-life-001",ExpectedRevision:1,Action:"disable",Reason:"Stale tab"}
 if _,e:=s.MutateInbound(ctx,key,10,stale);!errors.Is(e,ErrConflict){t.Fatal("stale tab overwrote profile",e)}
 disabled:=InboundMutation{EventID:"disable-inbound-life-1",ExpectedRevision:2,Action:"disable",Reason:"Pause inbound"}
 if _,err=s.MutateInbound(ctx,key,10,disabled);err!=nil{t.Fatal(err)}
 stopped,err:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall)
 if err!=nil||stopped.UserCount!=0||!xrayconfig.IsStoppedCandidate(stopped.Candidate)||stopped.Sequence<=first.Sequence{t.Fatalf("last disabled inbound did not produce explicit stop seq=%d users=%d %v",stopped.Sequence,stopped.UserCount,err)}
 enabled:=InboundMutation{EventID:"enable-inbound-life-01",ExpectedRevision:3,Action:"enable",Reason:"Resume inbound"}
 if _,err=s.MutateInbound(ctx,key,10,enabled);err!=nil{t.Fatal(err)}
 resumed,err:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall)
 if err!=nil||resumed.UserCount!=1||resumed.Candidate.NoListeners||!bytes.Contains(resumed.Candidate.Config,[]byte(client.UUID)){t.Fatal("enable lost client",err)}
 archived:=InboundMutation{EventID:"archive-inbound-life01",ExpectedRevision:4,Action:"archive",Reason:"Safe delete"}
 got,err=s.MutateInbound(ctx,key,10,archived)
 if err!=nil||!got.Archived||got.Enabled||got.Revision!=5||got.PublicKey!=in.PublicKey{t.Fatalf("archive changed identity %+v %v",got,err)}
 if _,err=s.MutateInbound(ctx,key,10,archived);err!=nil{t.Fatal("archive retry not idempotent",err)}
 enabled.EventID="resurrect-inbound-001";enabled.ExpectedRevision=5
 if _,err=s.MutateInbound(ctx,key,10,enabled);!errors.Is(err,ErrConflict){t.Fatal("archived profile resurrected",err)}
 if err=s.BindInboundClients(ctx,10,[]string{client.ID},true);!errors.Is(err,ErrConflict){t.Fatal("archived profile allowed new attachment",err)}
 if err=s.PutInboundTarget(ctx,target);!errors.Is(err,ErrConflict){t.Fatal("archived profile allowed deployment",err)}
 keep,err:=s.Client(ctx,client.ID);if err!=nil||keep.UUID!=client.UUID{t.Fatal("archive deleted client",err)}
 balance,err:=s.Representative(ctx,client.RepresentativeID);if err!=nil||balance.VolumeCreditBytes!=900||balance.UnlimitedCreditUnits!=100{t.Fatal("inbound lifecycle mutated credit",err)}
 history,err:=s.ListEvents(ctx,client.RepresentativeID,100,0);if err!=nil||len(history)!=2{t.Fatal("inbound lifecycle deleted billing history",err)}
 operations,err:=s.InboundOperations(ctx,10,100,0);if err!=nil||len(operations)!=4{t.Fatal("operation audit missing or duplicated",err)}
 raw,_:=json.Marshal(operations);if bytes.Contains(raw,[]byte(oldProfile.RealityPrivateKey))||bytes.Contains(raw,[]byte(client.UUID)){t.Fatal("audit exposed secrets")}
 if _,err=s.db.Exec("DELETE FROM native_inbound_operations");err==nil{t.Fatal("inbound audit deleted")}
 if _,err=s.db.Exec("UPDATE native_inbound_operations SET reason='fake'");err==nil{t.Fatal("inbound audit modified")}
 if err=s.Close();err!=nil{t.Fatal(err)};reopened,err:=Open(path);if err!=nil{t.Fatal(err)};defer reopened.Close()
 got,err=reopened.NativeInbound(ctx,key,10);if err!=nil||!got.Archived||got.PublicKey!=in.PublicKey||got.BoundClients!=1{t.Fatal("reopen lost archival history",err)}
 stopped,err=reopened.BuildNodeConfig(ctx,key,"new-node",inboundInstall);if err!=nil||!xrayconfig.IsStoppedCandidate(stopped.Candidate){t.Fatal("reopen revived archived profile",err)}
}
func TestInboundDisableDoesNotRemoveSiblingAndConcurrentEditsConflict(t *testing.T){
 ctx:=context.Background();s,_:=opened(t);key:=bytes.Repeat([]byte{3},32)
 for _,id:=range []uint64{10,11}{p:=nativeInput();p.ID=id;if _,e:=s.CreateInbound(ctx,key,p);e!=nil{t.Fatal(e)}
  if e:=s.PutInboundTarget(ctx,InboundTarget{InboundID:id,NodeID:"new-node",InstallationID:inboundInstall,ListenIP:"127.0.0.1",DirectPort:uint16(19620+id)});e!=nil{t.Fatal(e)}}
 var wg sync.WaitGroup;results:=make(chan error,8)
 for i:=0;i<8;i++{wg.Add(1);go func(i int){defer wg.Done();_,e:=s.MutateInbound(ctx,key,10,InboundMutation{EventID:fmt.Sprintf("concurrent-disable-%04d",i),ExpectedRevision:1,Action:"disable",Reason:"One winning update"});results<-e}(i)}
 wg.Wait();close(results);winners:=0
 for e:=range results{if e==nil{winners++}else if !errors.Is(e,ErrConflict){t.Fatal(e)}}
 if winners!=1{t.Fatalf("concurrent edits succeeded %d times",winners)}
 cfg,e:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall)
 if e!=nil||len(cfg.Candidate.Listeners)!=1||cfg.Candidate.Listeners[0].InboundID!=11{t.Fatal("disable touched sibling",e)}
 invalid:=nativeInput();invalid.ID=11;invalid.RealityTarget="127.0.0.1:22"
 _,e=s.MutateInbound(ctx,key,11,InboundMutation{EventID:"invalid-profile-life01",ExpectedRevision:1,Action:"edit",Reason:"Reject private target",Profile:&invalid})
 if !errors.Is(e,ErrInvalid){t.Fatal("invalid edit accepted",e)}
 in,e:=s.NativeInbound(ctx,key,11);if e!=nil||in.Revision!=1{t.Fatal("invalid edit partially committed",e)}
}
func TestNativeV6ToV7MigrationPreservesProfileAndBusiness(t *testing.T){
 ctx:=context.Background();s,path:=opened(t);key:=bytes.Repeat([]byte{7},32)
 in,e:=s.CreateInbound(ctx,key,nativeInput());if e!=nil{t.Fatal(e)}
 newRep(t,s,"v7-rep");grant(t,s,"v7-rep","migration-native-v7-1",200,50)
 for _,q:=range []string{"DROP TRIGGER native_dirty_profile_update","DROP TRIGGER native_plan_block_enable","DROP TABLE native_inbound_operations","ALTER TABLE native_inbounds DROP COLUMN revision","ALTER TABLE native_inbounds DROP COLUMN archived","ALTER TABLE native_inbounds DROP COLUMN enabled","PRAGMA user_version=6"}{if _,e=s.db.Exec(q);e!=nil{t.Fatal(e)}}
 s.Close();s,e=Open(path);if e!=nil{t.Fatal(e)};defer s.Close()
 kept,e:=s.NativeInbound(ctx,key,10);if e!=nil||kept.PublicKey!=in.PublicKey||!kept.Enabled||kept.Archived||kept.Revision!=1{t.Fatal("migration changed profile",e)}
 rep,e:=s.Representative(ctx,"v7-rep");if e!=nil||rep.VolumeCreditBytes!=200||rep.UnlimitedCreditUnits!=50{t.Fatal("migration altered balances",e)}
 if _,e=s.ListInbounds(ctx,bytes.Repeat([]byte{1},32));!errors.Is(e,ErrUnavailable){t.Fatal("wrong key accepted",e)}
 ops,e:=s.InboundOperations(ctx,10,200,0);if e!=nil||len(ops)!=0{t.Fatal("migration fabricated audit",e)}
 if _,e=s.InboundOperations(ctx,10,201,0);!errors.Is(e,ErrInvalid){t.Fatal("unbounded list",e)}
}

func TestArchivedInboundReleasesPortWithoutErasingIdentity(t *testing.T){
 ctx:=context.Background();s,_:=opened(t);key:=bytes.Repeat([]byte{7},32)
 old,err:=s.CreateInbound(ctx,key,nativeInput());if err!=nil{t.Fatal(err)}
 t1:=InboundTarget{InboundID:10,NodeID:"new-node",InstallationID:inboundInstall,ListenIP:"127.0.0.1",DirectPort:19780}
 if err=s.PutInboundTarget(ctx,t1);err!=nil{t.Fatal(err)}
 next:=nativeInput();next.ID=11;if _,err=s.CreateInbound(ctx,key,next);err!=nil{t.Fatal(err)}
 t2:=t1;t2.InboundID=11
 if err=s.PutInboundTarget(ctx,t2);!errors.Is(err,ErrConflict){t.Fatal("live port conflict permitted",err)}
 if _,err=s.MutateInbound(ctx,key,10,InboundMutation{EventID:"archive-port-reuse-01",ExpectedRevision:1,Action:"archive",Reason:"Release retired listener"});err!=nil{t.Fatal(err)}
 if err=s.PutInboundTarget(ctx,t2);err!=nil{t.Fatal("archived port was never released",err)}
 cfg,err:=s.BuildNodeConfig(ctx,key,"new-node",inboundInstall)
 if err!=nil||len(cfg.Candidate.Listeners)!=1||cfg.Candidate.Listeners[0].InboundID!=11{t.Fatal("retired inbound remained in config",err)}
 retained,err:=s.NativeInbound(ctx,key,10);if err!=nil||!retained.Archived||retained.PublicKey!=old.PublicKey{t.Fatal("reusing port erased old identity",err)}
}
