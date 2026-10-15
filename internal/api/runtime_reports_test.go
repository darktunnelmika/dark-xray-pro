package api

import("bytes";"context";"crypto/tls";"encoding/json";"net/http/httptest";"strings";"testing";"github.com/darktunnelmika/dark-xray-pro/internal/business";"github.com/darktunnelmika/dark-xray-pro/internal/managedxray")
func TestNativeReportsOwnerAccessAgentScopeAndReceiptBinding(t *testing.T){
 s:=enabledOwner(t);key:=bytes.Repeat([]byte{7},32);ctx:=context.Background()
 if e:=s.EnableNativeInbounds(key);e!=nil{t.Fatal(e)}
 inv,e:=s.registry.Issue("report-node","New report node");if e!=nil{t.Fatal(e)}
 if _,e=s.registry.Claim(inv.PairCode,"report-node",installID,nodeToken);e!=nil{t.Fatal(e)}
 if _,e=s.business.CreateInbound(ctx,key,business.InboundInput{ID:201,Name:"Reports",Transport:"tcp",RealityTarget:"example.org:443",ServerNames:[]string{"example.org"},ShortIDs:[]string{"aa"}});e!=nil{t.Fatal(e)}
 if e=s.business.PutInboundTarget(ctx,business.InboundTarget{InboundID:201,NodeID:"report-node",InstallationID:installID,ListenIP:"127.0.0.1",DirectPort:19443});e!=nil{t.Fatal(e)}
 send:=func(method,path,token string,body any)*httptest.ResponseRecorder{
  b,_:=json.Marshal(body);req:=httptest.NewRequest(method,"https://pro.example"+path,bytes.NewReader(b));req.TLS=&tls.ConnectionState{Version:tls.VersionTLS13}
  req.Header.Set("Authorization","Bearer "+token);req.Header.Set("Content-Type","application/json")
  w:=httptest.NewRecorder();s.AgentHandler().ServeHTTP(w,req);return w
 }
 w:=send("GET","/api/v1/agent/native-runtime?node_id=report-node&installation_id="+installID,nodeToken,nil)
 if w.Code!=200{t.Fatalf("delivery failed %d %s",w.Code,w.Body.String())}
 var d managedxray.Delivery;if e=json.Unmarshal(w.Body.Bytes(),&d);e!=nil{t.Fatal(e)}
 report:=managedxray.RuntimeReport{Lease:d.Lease,Observed:managedxray.Snapshot{Phase:"ready",Highwater:d.Sequence,ActiveSequence:d.Sequence,ActiveSHA:d.Candidate.SHA256,SavedSequence:d.Sequence,SavedSHA:d.Candidate.SHA256,ProcessReady:true,LeaseExpires:d.Lease.Expires}}
 for _,bad:=range []string{"",ownerToken,otherToken}{if w=send("POST","/api/v1/agent/native-runtime-report",bad,report);w.Code!=401{t.Fatalf("credential impersonated reporter %d",w.Code)}}
 altered:=report;altered.Lease.Signature=strings.Repeat("a",64)
 if w=send("POST","/api/v1/agent/native-runtime-report",nodeToken,altered);w.Code!=401{t.Fatal("forged signature accepted",w.Code)}
 if w=send("POST","/api/v1/agent/native-runtime-report",nodeToken,report);w.Code!=204{t.Fatalf("report rejected %d %s",w.Code,w.Body.String())}
 if w=send("POST","/api/v1/agent/native-runtime-report",nodeToken,report);w.Code!=204{t.Fatal("exact retry not idempotent",w.Code)}
 ownerCookie,_:=browserLogin(t,s,ownerUsername,ownerPassword)
 public:=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/native-inbounds-state",nil,nil,"","")
 if public.Code!=401{t.Fatal("public native state leaked",public.Code)}
 owner:=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/native-inbounds-state",nil,ownerCookie,"","")
 if owner.Code!=200||!strings.Contains(owner.Body.String(),`"state":"applied"`)||strings.Contains(owner.Body.String(),"config_base64")||strings.Contains(owner.Body.String(),"privateKey")||strings.Contains(owner.Body.String(),nodeToken){t.Fatalf("owner status wrong or secret leakage: %d",owner.Code)}
 if w=send("GET","/api/v1/workspace/native-inbounds-state",nodeToken,nil);w.Code!=404{t.Fatal("Agent read owner state",w.Code)}
 if _,e=s.business.CreateRepresentative(ctx,"rep-status","Representative");e!=nil{t.Fatal(e)}
 if _,e=s.business.ProvisionRepresentativeLogin(ctx,"rep-status","rep-status","test-status-password-long");e!=nil{t.Fatal(e)}
 repCookie,_:=browserLogin(t,s,"rep-status","test-status-password-long")
 rep:=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/native-inbounds-state",nil,repCookie,"","")
 if rep.Code!=403{t.Fatal("representative accessed owner topology",rep.Code)}
}
