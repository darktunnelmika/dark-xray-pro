package business

import (
 "bytes"
 "context"
 "errors"
 "os"
 "path/filepath"
 "strings"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
 uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
 "github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)
func usageClient(t *testing.T,s *Store,id string,limit int64)Client{
 t.Helper();c,e:=s.CreateClient(context.Background(),NewClient{EventID:"usage-native-client-"+id,ClientID:id,RepresentativeID:"owner",Name:id,DurationDays:30,VolumeBytes:limit});if e!=nil{t.Fatal(e)};return c
}
func usageSetup(t *testing.T)(*Store,string,[]byte,Client,NodeConfig){
 t.Helper();s,path:=opened(t);ctx:=context.Background();if e:=s.EnableUsageLedger(ctx);e!=nil{t.Fatal(e)}
 key:=bytes.Repeat([]byte{7},32);c:=usageClient(t,s,"metered-native",1000)
 if _,e:=s.CreateInbound(ctx,key,nativeInput());e!=nil{t.Fatal(e)}
 if e:=s.BindInboundClients(ctx,10,[]string{c.ID},true);e!=nil{t.Fatal(e)}
 if e:=s.PutInboundTarget(ctx,InboundTarget{InboundID:10,NodeID:"meter-node",InstallationID:inboundInstall,ListenIP:"127.0.0.1",DirectPort:19589});e!=nil{t.Fatal(e)}
 cfg,e:=s.BuildNodeConfig(ctx,key,"meter-node",inboundInstall);if e!=nil{t.Fatal(e)};return s,path,key,c,cfg
}
func deliveredUsage(t *testing.T,s *Store,cfg NodeConfig,id string)uc.Epoch{
 t.Helper();now:=time.Now();l,e:=managedxray.SignLease(bytes.Repeat([]byte{9},64),managedxray.Lease{NodeID:cfg.NodeID,InstallationID:cfg.InstallationID,Sequence:cfg.Sequence,ConfigSHA:cfg.ConfigSHA,ManifestSHA:cfg.ManifestSHA,Issued:now.UnixNano(),Expires:now.Add(25*time.Second).UnixNano()});if e!=nil{t.Fatal(e)}
 if e=s.RecordNativeDelivery(context.Background(),cfg,l);e!=nil{t.Fatal(e)}
 process,e:=uc.Process(os.Getpid());if e!=nil{t.Fatal(e)}
 epoch,e:=s.RegisterNativeUsageEpoch(context.Background(),cfg,UsageBirth{EpochID:id,KernelBootID:process.BootID,PID:process.PID,StartTicks:process.StartTicks});if e!=nil{t.Fatal(e)};return epoch
}
func TestNativeUsageCreationUnknownHistoryAndStickyRestart(t *testing.T){
 ctx:=context.Background();s,path:=opened(t);old:=usageClient(t,s,"existing-before-meter",1000)
 if s.UsageLedger()!=nil{t.Fatal("implicit metering initialization")};if e:=s.EnableUsageLedger(ctx);e!=nil{t.Fatal(e)}
 unknown,e:=s.ClientUsage(ctx,old.ID);if e!=nil||unknown.Known||unknown.Eligible{t.Fatalf("guessed-zero historical usage: %+v %v",unknown,e)}
 c:=usageClient(t,s,"created-after-meter",1000);a,e:=s.ClientUsage(ctx,c.ID)
 if e!=nil||!a.Known||!a.Eligible||a.Total!=0{t.Fatalf("new atomic baseline: %+v %v",a,e)}
 // Exact retries cannot reinitialize accounts or re-debit representative credit.
 again:=usageClient(t,s,"created-after-meter",1000);if again.UUID!=c.UUID{t.Fatal("replay changed UUID")}
 if e=s.Close();e!=nil{t.Fatal(e)};s,e=Open(path);if e!=nil{t.Fatal(e)};defer s.Close()
 if s.UsageLedger()==nil{t.Fatal("restart without env flag bypassed permanent quota policy")}
 a,e=s.ClientUsage(ctx,old.ID);if e!=nil||a.Known||a.Eligible{t.Fatal("restart guessed old baseline",e)}
 if _,e=s.ClientUsage(ctx,"missing-customer");!errors.Is(e,ErrNotFound){t.Fatal("phantom usage account",e)}
}
func TestNativeQuotaReceiptInvalidatesConfigPreservesHistoryAndRenewal(t *testing.T){
 s,path,key,c,initial:=usageSetup(t);ctx:=context.Background()
 if initial.UserCount!=1||!bytes.Contains(initial.Candidate.Config,[]byte(c.UUID)){t.Fatal("initial eligible UUID missing")}
 epoch:=deliveredUsage(t,s,initial,strings.Repeat("a",32))
 if len(epoch.Bindings)!=1||epoch.Bindings[0].ClientID!=c.ID||!epoch.Bindings[0].Billable{t.Fatal("HUB did not derive terminal mapping")}
 out,e:=uc.Open(uc.Options{Directory:filepath.Join(t.TempDir(),"outbox"),Key:bytes.Repeat([]byte{8},32),Epoch:epoch,Create:true});if e!=nil{t.Fatal(e)};defer out.Close()
 report,e:=out.Observe([]uc.Usage{{ClientID:c.ID,Uplink:800,Downlink:700}},false);if e!=nil{t.Fatal(e)}
 principal:=usageledger.Principal{NodeID:epoch.NodeID,InstallationID:epoch.InstallationID}
 receipt,e:=s.UsageLedger().Accept(ctx,principal,report);if e!=nil{t.Fatal(e)}
 replay,e:=s.UsageLedger().Accept(ctx,principal,report);if e!=nil||receipt!=replay{t.Fatal("receipt retry conflict",e)}
 a,e:=s.ClientUsage(ctx,c.ID);if e!=nil||a.Total!=1500||!a.Exhausted||a.Eligible{t.Fatalf("quota or unclipped total wrong: %+v %v",a,e)}
 next,e:=s.BuildNodeConfig(ctx,key,initial.NodeID,initial.InstallationID)
 if e!=nil||next.UserCount!=0||next.Sequence<=initial.Sequence||next.Generation<=initial.Generation||bytes.Contains(next.Candidate.Config,[]byte(c.UUID)){t.Fatalf("exhausted UUID persisted: %+v %v",next,e)}
 if !bytes.Contains(next.Candidate.Config,[]byte("\"clients\":[]")){t.Fatal("last-user revocation omitted required empty listener")}
 kept,e:=s.Client(ctx,c.ID);if e!=nil||kept.UUID!=c.UUID||kept.DeploymentState!="pending_node_apply"{t.Fatal("quota altered manual client state/identity",e)}
 if _,e=s.ExtendClient(ctx,ExtendClient{EventID:"usage-native-renewal-001",ClientID:c.ID,AdditionalVolumeBytes:1000,Reason:"Reviewed volume topup"});e!=nil{t.Fatal(e)}
 topped,e:=s.BuildNodeConfig(ctx,key,initial.NodeID,initial.InstallationID);if e!=nil||topped.UserCount!=1{t.Fatal("topup did not re-evaluate real remaining quota",e)}
 a,e=s.ClientUsage(ctx,c.ID);if e!=nil||a.Total!=1500||!a.Eligible{t.Fatal("topup zeroed historical consumption",e)}
 if e=s.Close();e!=nil{t.Fatal(e)};s,e=Open(path);if e!=nil{t.Fatal(e)};defer s.Close()
 a,e=s.ClientUsage(ctx,c.ID);if e!=nil||a.Total!=1500{t.Fatal("restart lost receipt/usage",e)}
 replay,e=s.UsageLedger().Accept(ctx,principal,report);if e!=nil||replay!=receipt{t.Fatal("restart recharged report",e)}
 if e=out.Acknowledge(receipt);e!=nil{t.Fatal(e)};pending,e:=out.Pending();if e!=nil||len(pending)!=0{t.Fatal("committed receipt not removed")}
 if _,e=s.SetClientState(ctx,SetClientState{EventID:"archive-metered-native-1",ClientID:c.ID,Action:"archive",Reason:"Preserve final traffic"});e!=nil{t.Fatal(e)}
 a,e=s.ClientUsage(ctx,c.ID);if e!=nil||a.Total!=1500||a.Eligible{t.Fatal("archival changed consumption",e)}
 ev,e:=s.ListEvents(ctx,"owner",20,0);if e!=nil||len(ev)!=1{t.Fatal("usage rewrote purchase/credit history",e)}
}
func TestNativeUsageEpochRequiresCurrentDeliveryAndServerDerivedMappings(t *testing.T){
 s,_,_,_,cfg:=usageSetup(t);ctx:=context.Background();p,e:=uc.Process(os.Getpid());if e!=nil{t.Fatal(e)}
 birth:=UsageBirth{EpochID:strings.Repeat("b",32),KernelBootID:p.BootID,PID:p.PID,StartTicks:p.StartTicks}
 if _,e=s.RegisterNativeUsageEpoch(ctx,cfg,birth);!errors.Is(e,ErrNotFound){t.Fatal("unissued config allowed epoch",e)}
 epoch:=deliveredUsage(t,s,cfg,birth.EpochID);if len(epoch.Bindings)!=1{t.Fatal("invalid authority mapping")}
 same,e:=s.RegisterNativeUsageEpoch(ctx,cfg,birth);if e!=nil||same.ID!=epoch.ID{t.Fatal("registration retry not idempotent",e)}
 birth.EpochID=strings.Repeat("c",32)
 if _,e=s.RegisterNativeUsageEpoch(ctx,cfg,birth);!errors.Is(e,usageledger.ErrConflict){t.Fatal("same process invents second baseline",e)}
 changed:=cfg;changed.Candidate.Config=append([]byte(nil),cfg.Candidate.Config...);changed.Candidate.Config[0]='!'
 if _,e=s.RegisterNativeUsageEpoch(ctx,changed,birth);e==nil{t.Fatal("tampered private delivery accepted")}
 usageClient(t,s,"generation-changed",500)
 if _,e=s.RegisterNativeUsageEpoch(ctx,cfg,birth);!errors.Is(e,ErrConflict){t.Fatal("stale config authorizes new epoch",e)}
}
func TestNativeUsageSeedFailureRollsBackWholeCustomerTransaction(t *testing.T){
 s,_:=opened(t);ctx:=context.Background();if e:=s.EnableUsageLedger(ctx);e!=nil{t.Fatal(e)}
 newRep(t,s,"seed-rep");grant(t,s,"seed-rep","credit-seed-rep-0001",5000,0)
 if _,e:=s.db.Exec("CREATE TRIGGER reject_test_seed BEFORE INSERT ON pro_usage_accounts BEGIN SELECT RAISE(ABORT,'test only fault'); END");e!=nil{t.Fatal(e)}
 _,e:=s.CreateClient(ctx,NewClient{EventID:"client-seed-failure-01",ClientID:"not-committed",RepresentativeID:"seed-rep",Name:"Rejected atomically",DurationDays:30,VolumeBytes:1000})
 if e==nil{t.Fatal("partial account transaction succeeded")}
 if _,e=s.Client(ctx,"not-committed");!errors.Is(e,ErrNotFound){t.Fatal("unmetered client leaked",e)}
 balance,e:=s.Representative(ctx,"seed-rep");if e!=nil||balance.VolumeCreditBytes!=5000{t.Fatal("failed seed debited credit",e)}
}
