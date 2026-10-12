package replacement

import (
 "errors"
 "os"
 "path/filepath"
 "reflect"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)

const installID="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const anotherInstall="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func example(t *testing.T)routeplan.Preview{
 t.Helper()
 draft,err:=routeplan.New("old-de","new-de")
 if err!=nil{t.Fatal(err)}
 if err:=draft.SetDirect(2,8569);err!=nil{t.Fatal(err)}
 if err:=draft.SetSwap(2,1185,1185,"10.10.0.2");err!=nil{t.Fatal(err)}
 if err:=draft.SetDirect(1,8080);err!=nil{t.Fatal(err)}
 plan,err:=draft.Preview()
 if err!=nil{t.Fatal(err)}
 return plan
}

func journalForTest(t *testing.T, now *time.Time)(*Journal,string){
 t.Helper()
 path:=filepath.Join(t.TempDir(),"private","replacements.json")
 journal,err:=Open(path,func()time.Time{return *now})
 if err!=nil{t.Fatal(err)}
 return journal,path
}

func TestDurablePreflightDoesNotPublishOrInheritRoutes(t *testing.T){
 now:=time.Date(2026,10,12,12,0,0,0,time.UTC)
 journal,path:=journalForTest(t,&now)
 plan:=example(t)
 created,err:=journal.Create(plan,installID)
 if err!=nil{t.Fatal(err)}
 if created.CanActivate||!created.NeedsXrayValidation||!created.NeedsDataPlaneProbe||
  !created.NeedsSubscriptionCommit||created.Phase!=Pending||created.Preview.HasInheritedOldRoutes{
  t.Fatalf("journal fabricated an active route or inherited a legacy path: %+v",created)
 }
 if op,ok:=journal.Next("new-de",anotherInstall);ok||op.ID!="" {
  t.Fatal("wrong installation can receive replacement plan")
 }
 next,ok:=journal.Next("new-de",installID)
 if !ok||next.ID!=created.ID{t.Fatal("new paired Agent cannot see pending task")}
 wanted,err:=Expect(plan)
 if err!=nil{t.Fatal(err)}
 if len(wanted)!=3||wanted[0].InboundID!=1||wanted[0].Kind!="direct"||
  wanted[1].Kind!="direct"||wanted[2].Kind!="swap"||
  wanted[2].Address!="10.10.0.2:1185"{
  t.Fatalf("inbound route checks not deterministic: %+v",wanted)
 }
 report:=Evidence{OperationID:created.ID,ManifestSHA256:plan.ManifestSHA256,
  NewNodeID:"new-de",InstallationID:installID,Probes:wanted}
 for i:=range report.Probes { report.Probes[i].Reachable=true }
 done,err:=journal.Report(report)
 if err!=nil{t.Fatal(err)}
 if done.Phase!=TCPChecked||done.CanActivate||done.Revision!=2 {
  t.Fatalf("a TCP dial cannot silently authorize Xray/subscription: %+v",done)
 }
 if _,ok:=journal.Next("new-de",installID);ok {t.Fatal("completed TCP check kept issuing work")}
 replay,err:=journal.Report(report)
 if err!=nil||!reflect.DeepEqual(replay,done){t.Fatalf("identical lost ACK not idempotent: %v",err)}
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 if len(raw)==0 {t.Fatal("empty persisted journal")}
 info,err:=os.Stat(path)
 if err!=nil||info.Mode().Perm()!=0600{t.Fatal("private journal permissions")}
 restart,err:=Open(path,func()time.Time{return now.Add(time.Hour)})
 if err!=nil{t.Fatal(err)}
 restored,err:=restart.Get(created.ID)
 if err!=nil||!reflect.DeepEqual(restored,done){
  t.Fatalf("operation/evidence was not durable: %v",err)
 }
 restored.Preview.Deployments[0].Direct.NodePort=1
 reread,_:=restart.Get(created.ID)
 if reread.Preview.Deployments[0].Direct.NodePort==1 {
  t.Fatal("caller mutated stored snapshot")
 }
}

func TestFailedProbeRetryIsNotAutomaticallyClaimedReady(t *testing.T){
 now:=time.Now()
 j,_:=journalForTest(t,&now)
 op,err:=j.Create(example(t),installID)
 if err!=nil{t.Fatal(err)}
 checks,_:=Expect(op.Preview)
 for i:=range checks { checks[i].Reachable=true }
 checks[1].Reachable=false
 checks[1].Failure="refused"
 report:=Evidence{
  OperationID:op.ID,ManifestSHA256:op.Preview.ManifestSHA256,
  NewNodeID:op.NewNodeID,InstallationID:installID,Probes:checks,
 }
 result,err:=j.Report(report)
 if err!=nil||result.Phase!=ProbeFailed||result.CanActivate{
  t.Fatalf("failed TCP check incorrectly approved: %v %+v",err,result)
 }
 checks[1].Reachable=true
 checks[1].Failure=""
 report.Probes=checks
 if _,err:=j.Report(report);!errors.Is(err,ErrConflict){
  t.Fatalf("later ambiguous result overwrote an authoritative proof: %v",err)
 }
 if _,ok:=j.Next("new-de",installID);ok{t.Fatal("failed operation automatically retried without owner consent")}
}

func TestRejectForgeryAndCancelWithOptimisticRevision(t *testing.T){
 now:=time.Now()
 j,_:=journalForTest(t,&now)
 op,err:=j.Create(example(t),installID)
 if err!=nil{t.Fatal(err)}
 if _,err:=j.Create(example(t),installID);!errors.Is(err,ErrConflict){
  t.Fatalf("concurrent replacement accepted: %v",err)
 }
 report:=Evidence{
  OperationID:op.ID,ManifestSHA256:op.Preview.ManifestSHA256,
  NewNodeID:op.NewNodeID,InstallationID:installID,
 }
 report.Probes,_=Expect(op.Preview)
 for i:=range report.Probes {report.Probes[i].Reachable=true}
 forged:=report;forged.ManifestSHA256="ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
 if _,err:=j.Report(forged);!errors.Is(err,ErrMismatch){t.Fatal("wrong route hash accepted",err)}
 forged=report;forged.InstallationID=anotherInstall
 if _,err:=j.Report(forged);!errors.Is(err,ErrMismatch){t.Fatal("unpaired Agent replaced a Node",err)}
 forged=report;forged.Probes=append([]Probe(nil),report.Probes...)
 forged.Probes[0].Address="169.254.169.254:80"
 if _,err:=j.Report(forged);!errors.Is(err,ErrMismatch){t.Fatal("extra network probe admitted",err)}
 if _,err:=j.Cancel(op.ID,100);!errors.Is(err,ErrConflict){t.Fatal("stale owner revision cancelled",err)}
 cancelled,err:=j.Cancel(op.ID,op.Revision)
 if err!=nil||cancelled.Phase!=Cancelled||cancelled.Revision!=2{t.Fatal("cancel failed",err)}
 if _,err:=j.Report(report);!errors.Is(err,ErrCancelled){t.Fatal("cancelled operation accepted Agent response",err)}
 if _,ok:=j.Next("new-de",installID);ok{t.Fatal("cancelled operation still published")}
 if _,err:=j.Create(example(t),installID);err!=nil{t.Fatal("cancelled operation held new Node forever",err)}
}

func TestUnsafePermissionsRefuseJournal(t *testing.T){
 now:=time.Now()
 j,path:=journalForTest(t,&now)
 _,err:=j.Create(example(t),installID)
 if err!=nil{t.Fatal(err)}
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=Open(path,nil);!errors.Is(err,ErrUnsafe){t.Fatal("world readable journal accepted",err)}
 if err:=os.Chmod(path,0600);err!=nil{t.Fatal(err)}
 link:=filepath.Join(filepath.Dir(path),"alias.json")
 if err:=os.Symlink(path,link);err!=nil{t.Fatal(err)}
 if _,err:=Open(link,nil);!errors.Is(err,ErrUnsafe){t.Fatal("symlink accepted",err)}
}
