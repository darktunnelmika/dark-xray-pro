package routecheck

import (
 "context"
 "net"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)

const installation="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func openTCP(t *testing.T)net.Listener{
 t.Helper()
 l,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 t.Cleanup(func(){l.Close()})
 return l
}

func draftTCP(t *testing.T, port int) routeplan.Preview{
 t.Helper()
 draft,err:=routeplan.New("old","new")
 if err!=nil{t.Fatal(err)}
 if err:=draft.SetDirect(1,port);err!=nil{t.Fatal(err)}
 plan,err:=draft.Preview()
 if err!=nil{t.Fatal(err)}
 return plan
}

func TestAuthenticatedAgentTCPPreflightIsNotXrayProof(t *testing.T){
 l:=openTCP(t)
 port:=l.Addr().(*net.TCPAddr).Port
 plan:=draftTCP(t,port)
 op:=replacement.Operation{
  ID:"11111111111111111111111111111111",
  NewNodeID:"new",InstallationID:installation,Phase:replacement.Pending,
  Preview:plan,CanActivate:false,
 }
 ctx,cancel:=context.WithTimeout(context.Background(),4*time.Second)
 defer cancel()
 report,err:=Run(ctx,op,nil)
 if err!=nil{t.Fatal(err)}
 if !report.AllTCPReachable||len(report.Evidence.Probes)!=1||
  !report.Evidence.Probes[0].Reachable||report.Evidence.Probes[0].Failure!=""{
  t.Fatalf("healthy local TCP was incorrectly rejected: %+v",report)
 }
 if op.CanActivate {t.Fatal("TCP reachability somehow activated old/new Xray")}
}

func TestClosedPortFailsWithoutFabricatingHealthyRoute(t *testing.T){
 l:=openTCP(t)
 port:=l.Addr().(*net.TCPAddr).Port
 _=l.Close()
 op:=replacement.Operation{ID:"22222222222222222222222222222222",
  NewNodeID:"new",InstallationID:installation,Phase:replacement.Pending,
  Preview:draftTCP(t,port)}
 ctx,cancel:=context.WithTimeout(context.Background(),4*time.Second)
 defer cancel()
 result,err:=Run(ctx,op,nil)
 if err!=nil{t.Fatal(err)}
 if result.AllTCPReachable||result.Evidence.Probes[0].Reachable||
  result.Evidence.Probes[0].Failure==""{
  t.Fatalf("closed route reported as live: %+v",result)
 }
}

func TestCannotProbeCancelledOrActivatingOperation(t *testing.T){
 op:=replacement.Operation{ID:"33333333333333333333333333333333",
  NewNodeID:"new",InstallationID:installation,Phase:replacement.Cancelled,
  Preview:draftTCP(t,8569)}
 if _,err:=Run(context.Background(),op,nil);err==nil{
  t.Fatal("cancelled operation caused a network probe")
 }
 op.Phase=replacement.Pending
 op.CanActivate=true
 if _,err:=Run(context.Background(),op,nil);err==nil{
  t.Fatal("unapproved activating plan caused probe")
 }
}
