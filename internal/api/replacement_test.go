package api

import (
 "crypto/tls"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
 "github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

const newInstallation="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const newToken="2222222222222222222222222222222222222222222222222222222222222222"

func replacementEnv(t *testing.T)(*Server,*replacement.Journal){
 t.Helper()
 dir:=t.TempDir()
 now:=time.Date(2026,10,12,9,0,0,0,time.UTC)
 reg,err:=noderegistry.Open(filepath.Join(dir,"nodes","registry.json"),func()time.Time{return now})
 if err!=nil{t.Fatal(err)}
 for _,n:=range []struct{id,name,install,token string}{
  {"old-de","Retiring LAB",installID,nodeToken},
  {"new-de","Fresh LAB",newInstallation,newToken},
 }{
  ticket,err:=reg.Issue(n.id,n.name)
  if err!=nil{t.Fatal(err)}
  if _,err:=reg.Claim(ticket.PairCode,n.id,n.install,n.token);err!=nil{t.Fatal(err)}
 }
 j,err:=replacement.Open(filepath.Join(dir,"operations","journal.json"),func()time.Time{return now})
 if err!=nil{t.Fatal(err)}
 api,err:=NewServer(telemetry.NewStore(0,nil),reg,ownerToken)
 if err!=nil{t.Fatal(err)}
 if err:=api.EnableReplacementLab(j);err!=nil{t.Fatal(err)}
 return api,j
}

func testPlan()map[string]any{
 return map[string]any{
  "old_node_id":"old-de", "new_node_id":"new-de",
  "deployments":[]map[string]any{
   {"inbound_id":1,"direct":map[string]any{"node_port":8569},
    "swap":map[string]any{"tunnel_port":1185,"node_port":1185,"tunnel_address":"10.10.0.2"}},
   {"inbound_id":2,"direct":map[string]any{"node_port":8080}},
  },
 }
}

func TestAdminReplacementIsDurableAndDoesNotPublishSubscriptions(t *testing.T){
 service,j:=replacementEnv(t)
 h:=service.Handler()
 if w:=request(t,h,"POST","/api/v1/admin/replacements","",testPlan());w.Code!=http.StatusUnauthorized{
  t.Fatal("anonymous owner created operation",w.Code)
 }
 if w:=request(t,h,"POST","/api/v1/admin/replacements",newToken,testPlan());w.Code!=http.StatusUnauthorized{
  t.Fatal("Agent credential granted owner rights",w.Code)
 }
 create:=request(t,h,"POST","/api/v1/admin/replacements",ownerToken,testPlan())
 if create.Code!=http.StatusCreated{t.Fatal("create",create.Code,create.Body.String())}
 var op replacement.Operation
 if err:=json.Unmarshal(create.Body.Bytes(),&op);err!=nil{t.Fatal(err)}
 if op.Phase!=replacement.Pending||op.CanActivate||op.InstallationID!=newInstallation||
   len(op.Preview.Deployments)!=2||op.NeedsSubscriptionCommit!=true{
  t.Fatalf("invalid replacement draft: %+v",op)
 }
 // A failed/repeated owner request cannot silently replace a concurrent plan.
 if w:=request(t,h,"POST","/api/v1/admin/replacements",ownerToken,testPlan());w.Code!=http.StatusConflict{
  t.Fatal("overlapping replacement accepted",w.Code)
 }
 uri:="/api/v1/agent/replacement-task?node_id=new-de&installation_id="+newInstallation
 if w:=request(t,h,"GET",uri,nodeToken,nil);w.Code!=http.StatusUnauthorized{
  t.Fatal("old Node credential obtained new Node task",w.Code)
 }
 task:=request(t,h,"GET",uri,newToken,nil)
 if task.Code!=http.StatusOK{t.Fatal("paired new Node cannot read task",task.Code)}
 if w:=request(t,h,"GET","/api/v1/admin/replacements/"+op.ID,newToken,nil);w.Code!=http.StatusUnauthorized{
  t.Fatal("Node could inspect owner-only journal",w.Code)
 }
 proofs,err:=replacement.Expect(op.Preview)
 if err!=nil{t.Fatal(err)}
 for i:=range proofs{proofs[i].Reachable=true}
 evidence:=replacement.Evidence{
  OperationID:op.ID,ManifestSHA256:op.Preview.ManifestSHA256,
  NewNodeID:op.NewNodeID,InstallationID:op.InstallationID,Probes:proofs,
 }
 if w:=request(t,h,"POST","/api/v1/agent/replacement-report",nodeToken,evidence);w.Code!=http.StatusUnauthorized{
  t.Fatalf("old Node forged replacement evidence %d",w.Code)
 }
 forged:=evidence
 forged.ManifestSHA256="ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
 if w:=request(t,h,"POST","/api/v1/agent/replacement-report",newToken,forged);w.Code!=http.StatusConflict{
  t.Fatalf("bad manifest accepted %d",w.Code)
 }
 result:=request(t,h,"POST","/api/v1/agent/replacement-report",newToken,evidence)
 if result.Code!=http.StatusOK{t.Fatalf("evidence %d %s",result.Code,result.Body.String())}
 var after replacement.Operation
 if err:=json.Unmarshal(result.Body.Bytes(),&after);err!=nil{t.Fatal(err)}
 if after.Phase!=replacement.TCPChecked||after.CanActivate||after.Revision!=op.Revision+1 ||
  !after.NeedsXrayValidation||!after.NeedsDataPlaneProbe||!after.NeedsSubscriptionCommit{
  t.Fatalf("TCP result fabricated active/subscribed operation: %+v",after)
 }
 if w:=request(t,h,"GET",uri,newToken,nil);w.Code!=http.StatusNoContent{
  t.Fatal("completed TCP evidence still returned as new command",w.Code)
 }
 if w:=request(t,h,"POST","/api/v1/admin/replacements/"+op.ID+"/activate",ownerToken,
    map[string]any{"activate":true});w.Code!=http.StatusNotFound{
  t.Fatalf("unsafe activation accidentally exposed HTTP %d",w.Code)
 }
 restored,err:=j.Get(op.ID)
 if err!=nil||restored.Phase!=replacement.TCPChecked{t.Fatal("journal not updated",err)}
}

func TestReplacementIsStrictOnNewPairedAgentAndOwnerRevision(t *testing.T){
 service,_:=replacementEnv(t)
 h:=service.Handler()
 bad:=testPlan()
 bad["new_node_id"]="unpaired"
 if w:=request(t,h,"POST","/api/v1/admin/replacements",ownerToken,bad);w.Code!=409{
  t.Fatal("unpaired destination accepted",w.Code)
 }
 dupe:=testPlan()
 dupe["deployments"]=[]map[string]any{
  {"inbound_id":1,"direct":map[string]any{"node_port":8569}},
  {"inbound_id":1,"direct":map[string]any{"node_port":8569}},
 }
 if w:=request(t,h,"POST","/api/v1/admin/replacements",ownerToken,dupe);w.Code!=422{
  t.Fatal("duplicate Inbound entries accepted",w.Code)
 }
 create:=request(t,h,"POST","/api/v1/admin/replacements",ownerToken,testPlan())
 var op replacement.Operation
 if err:=json.Unmarshal(create.Body.Bytes(),&op);err!=nil{t.Fatal(err)}
 uri:="/api/v1/admin/replacements/"+op.ID+"/cancel"
 if w:=request(t,h,"POST",uri,ownerToken,map[string]any{"expected_revision":99});w.Code!=409{
  t.Fatal("stale owner revision changed replacement",w.Code)
 }
 if w:=request(t,h,"POST",uri,newToken,map[string]any{"expected_revision":1});w.Code!=401{
  t.Fatal("Agent credential cancelled owner request",w.Code)
 }
 if w:=request(t,h,"POST",uri,ownerToken,map[string]any{"expected_revision":1});w.Code!=200{
  t.Fatal("owner could not safely cancel",w.Code)
 }
 if w:=request(t,h,"POST",uri,ownerToken,map[string]any{"expected_revision":1});w.Code!=200{
  t.Fatal("lost cancellation reply cannot be retried",w.Code)
 }
 if w:=request(t,h,"GET","/api/v1/agent/replacement-task?node_id=new-de&installation_id="+newInstallation,newToken,nil);w.Code!=204{
  t.Fatal("cancelled operation still sent to Agent",w.Code)
 }
}

func TestRemoteTLSListenerDoesNotExposeOwnerReplacementAPIs(t *testing.T){
 service,_:=replacementEnv(t)
 for _,r:=range []struct{method,path string}{
  {"POST","/api/v1/admin/replacements"},
  {"GET","/api/v1/admin/replacements"},
  {"GET","/api/v1/admin/replacements/anything"},
  {"POST","/api/v1/admin/replacements/anything/cancel"},
 }{
  req:=httptest.NewRequest(r.method,r.path,nil)
  req.TLS=&tls.ConnectionState{Version:tls.VersionTLS13}
  req.Header.Set("Authorization","Bearer "+ownerToken)
  w:=httptest.NewRecorder()
  service.AgentHandler().ServeHTTP(w,req)
  if w.Code!=404{t.Errorf("TLS Agent surface exposed owner API %s with HTTP %d",r.path,w.Code)}
 }
}

var _ = routeplan.Deployment{}
