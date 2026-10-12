package main

import (
 "context"
 "net"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/api"
 "github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
 "github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

const owner="owner-key-only-for-isolated-integration-test-123456789"
const token="2222222222222222222222222222222222222222222222222222222222222222"
const oldToken="1111111111111111111111111111111111111111111111111111111111111111"
const oldInstall="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const freshInstall="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func endToEnd(t *testing.T, port int)(*http.Client,string,*replacement.Journal,replacement.Operation){
 t.Helper()
 dir:=t.TempDir()
 reg,err:=noderegistry.Open(filepath.Join(dir,"reg","nodes.json"),nil)
 if err!=nil{t.Fatal(err)}
 for _,n:=range []struct{id,install,secret string}{
  {"old-01",oldInstall,oldToken},
  {"fresh-01",freshInstall,token},
 }{
  invite,err:=reg.Issue(n.id,"LAB "+n.id)
  if err!=nil{t.Fatal(err)}
  if _,err:=reg.Claim(invite.PairCode,n.id,n.install,n.secret);err!=nil{t.Fatal(err)}
 }
 j,err:=replacement.Open(filepath.Join(dir,"journal","operations.json"),nil)
 if err!=nil{t.Fatal(err)}
 draft,err:=routeplan.New("old-01","fresh-01")
 if err!=nil{t.Fatal(err)}
 if err:=draft.SetDirect(12,port);err!=nil{t.Fatal(err)}
 if err:=draft.SetSwap(12,port,port,"127.0.0.1");err!=nil{t.Fatal(err)}
 plan,err:=draft.Preview()
 if err!=nil{t.Fatal(err)}
 op,err:=j.Create(plan,freshInstall)
 if err!=nil{t.Fatal(err)}
 server,err:=api.NewServer(telemetry.NewStore(0,nil),reg,owner)
 if err!=nil{t.Fatal(err)}
 if err:=server.EnableReplacementLab(j);err!=nil{t.Fatal(err)}
 web:=httptest.NewServer(server.Handler())
 t.Cleanup(web.Close)
 return &http.Client{Timeout:5*time.Second},web.URL,j,op
}

func TestLiveHTTPAgentPreflightReportsReachabilityWithoutChangingXray(t *testing.T){
 listener,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 defer listener.Close()
 port:=listener.Addr().(*net.TCPAddr).Port
 client,url,j,op:=endToEnd(t,port)
 ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second)
 defer cancel()
 if err:=pollPreflight(ctx,client,url,"fresh-01",freshInstall,token);err!=nil{
  t.Fatalf("authenticated Agent TCP preflight failed: %v",err)
 }
 result,err:=j.Get(op.ID)
 if err!=nil{t.Fatal(err)}
 if result.Phase!=replacement.TCPChecked||result.CanActivate||
  !result.NeedsXrayValidation||!result.NeedsDataPlaneProbe||
  !result.NeedsSubscriptionCommit||len(result.Evidence.Probes)!=2{
  t.Fatalf("preflight changed network/business or fabricated success: %+v",result)
 }
 for _,probe:=range result.Evidence.Probes{
  if !probe.Reachable {t.Errorf("open lab TCP listener rejected: %+v",probe)}
 }
 if err:=pollPreflight(ctx,client,url,"fresh-01",freshInstall,token);err!=nil{
  t.Fatalf("poll without queued work must be idempotent: %v",err)
 }
 if err:=pollPreflight(ctx,client,url,"fresh-01",freshInstall,oldToken);err==nil{
  t.Fatal("retired Node credential read new Node's preflight")
 }
}

func TestClosedTCPRouteIsNeverTreatedAsActiveXray(t *testing.T){
 listener,err:=net.Listen("tcp","127.0.0.1:0")
 if err!=nil{t.Fatal(err)}
 port:=listener.Addr().(*net.TCPAddr).Port
 listener.Close()
 client,url,j,op:=endToEnd(t,port)
 ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second)
 defer cancel()
 if err:=pollPreflight(ctx,client,url,"fresh-01",freshInstall,token);err!=nil{
  t.Fatalf("negative TCP test did not report a bounded failure: %v",err)
 }
 status,err:=j.Get(op.ID)
 if err!=nil{t.Fatal(err)}
 if status.Phase!=replacement.ProbeFailed||status.CanActivate{
  t.Fatalf("closed TCP port was treated as production route: %+v",status)
 }
 for _,p:=range status.Evidence.Probes{
  if p.Reachable||p.Failure=="" {t.Fatalf("closed port returned healthy evidence: %+v",p)}
 }
}
