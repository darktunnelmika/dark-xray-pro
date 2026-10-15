//go:build linux

package api

import (
 "bytes"
 "context"
 "crypto/sha256"
 "crypto/tls"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "encoding/pem"
 "net"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "strconv"
 "time"
 "testing"
 "github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
 "github.com/darktunnelmika/dark-xray-pro/internal/business"
 "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)
func nativeFixture(t *testing.T)(string,string){
 t.Helper();script:=`#!/bin/sh
set -eu
if [ "$2" = "-test" ]; then exit 0; fi
exec python3 -u -c 'import json,socket,sys,select
c=json.load(open(sys.argv[1]));servers=[]
for x in c["inbounds"]:
 s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
 s.bind((x["listen"],x["port"]));s.listen(64);servers.append(s)
while True:
 ready,_,_=select.select(servers,[],[],0.2)
 for s in ready:
  conn,_=s.accept();conn.close()
' "$3"
`
 p:=filepath.Join(t.TempDir(),"isolated-core");if e:=os.WriteFile(p,[]byte(script),0700);e!=nil{t.Fatal(e)};sha:=sha256.Sum256([]byte(script));return p,hex.EncodeToString(sha[:])
}
func nativeAPIExercise(t *testing.T,binary,pin string){
 t.Helper();if os.Geteuid()==0{t.Skip("test core must never run as root")};ctx:=context.Background();s:=enabledOwner(t);key:=bytes.Repeat([]byte{6},32)
 if e:=s.EnableNativeInbounds(key);e!=nil{t.Fatal(e)};ticket,e:=s.registry.Issue("native-node","Fresh test installation");if e!=nil{t.Fatal(e)}
 if _,e=s.registry.Claim(ticket.PairCode,"native-node",installID,nodeToken);e!=nil{t.Fatal(e)};cookie,csrf:=browserLogin(t,s,ownerUsername,ownerPassword)
 call:=func(path string,payload any)*httptest.ResponseRecorder{return browserRequest(t,s.Handler(),"POST",path,payload,cookie,csrf,"https://"+browserHost)}
 response:=call("/api/v1/workspace/native-inbounds",business.InboundInput{ID:101,Name:"Actual native",Transport:"tcp",RealityTarget:"www.example.org:443",ServerNames:[]string{"www.example.org"},ShortIDs:[]string{"aa11"}})
 if response.Code!=201{t.Fatalf("inbound create: %d %s",response.Code,response.Body.String())};if strings.Contains(response.Body.String(),"private_key"){t.Fatal("owner response exposed private key")}
 c,e:=s.business.CreateClient(ctx,business.NewClient{EventID:"native-sync-client-0001",ClientID:"native-sync-client",RepresentativeID:"owner",Name:"Native owner",DurationDays:30,VolumeBytes:100,IPCount:0});if e!=nil{t.Fatal(e)}
 if w:=call("/api/v1/workspace/native-inbounds/101/clients",map[string]any{"client_ids":[]string{c.ID},"attach":true});w.Code!=200{t.Fatalf("native client bind: %d %s",w.Code,w.Body.String())}
 listen,e:=net.Listen("tcp4","127.0.0.1:0");if e!=nil{t.Fatal(e)};port:=uint16(listen.Addr().(*net.TCPAddr).Port);listen.Close()
 target:=business.InboundTarget{InboundID:101,NodeID:"native-node",InstallationID:installID,ListenIP:"127.0.0.1",DirectPort:port}
 if w:=call("/api/v1/workspace/native-inbounds/101/target",target);w.Code!=200{t.Fatalf("target: %d %s",w.Code,w.Body.String())}
 preview:=call("/api/v1/workspace/native-nodes/native-node/preview",map[string]any{});if preview.Code!=200||bytes.Contains(preview.Body.Bytes(),[]byte(c.UUID))||bytes.Contains(preview.Body.Bytes(),[]byte("privateKey")){t.Fatal("preview failed or leaked secrets")}
 unsecured:=request(t,s.Handler(),"GET","/api/v1/agent/native-runtime?node_id=native-node&installation_id="+installID,nodeToken,nil);if unsecured.Code!=426{t.Fatal("private candidate exposed on plaintext owner listener")}
 server:=httptest.NewUnstartedServer(s.AgentHandler());server.TLS=&tls.Config{MinVersion:tls.VersionTLS13};server.StartTLS();defer server.Close();cert:=server.Certificate();hash:=sha256.Sum256(cert.Raw)
 client,endpoint,e:=agenttransport.New(agenttransport.Config{Endpoint:server.URL,PinSHA256:hex.EncodeToString(hash[:]),CABundle:pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:cert.Raw})});if e!=nil{t.Fatal(e)}
 o:=managedxray.Options{Directory:filepath.Join(t.TempDir(),"private-runtime"),StateKey:bytes.Repeat([]byte{8},32),LeaseKey:[]byte(nodeToken),NodeID:"native-node",InstallationID:installID,ListenIP:"127.0.0.1",Binary:binary,BinarySHA:pin}
 runtime,e:=managedxray.Open(o);if e!=nil{t.Fatal(e)};defer runtime.Close();state,e:=runtime.Sync(ctx,client,endpoint)
 if e!=nil||!state.ProcessReady||state.ActiveSequence!=1||state.DataPlaneVerified||state.SubscriptionPublished{t.Fatalf("TLS delivery/apply failed: %+v %v",state,e)}
 status,err:=s.business.NativeRuntimeStatus(ctx,"native-node",installID);if err!=nil||status.State!="applied"||!status.ProcessReady||status.DataPlaneVerified{t.Fatalf("durable report missing: %+v %v",status,err)}
 same,e:=runtime.Sync(ctx,client,endpoint);if e!=nil||same.ActiveSHA!=state.ActiveSHA||same.ActiveSequence!=1{t.Fatal("unchanged native config churned",e)}
 wrongReq,_:=http.NewRequest("GET",server.URL+"/api/v1/agent/native-runtime?node_id=native-node&installation_id="+installID,nil);wrongReq.Header.Set("Authorization","Bearer "+otherToken)
 wrong,e:=client.Do(wrongReq);if e!=nil{t.Fatal(e)};wrong.Body.Close();if wrong.StatusCode!=401{t.Fatal("unpaired Agent read native secrets")}
 if _,e=s.business.SetClientState(ctx,business.SetClientState{EventID:"native-revoke-client-001",ClientID:c.ID,Action:"disable",Reason:"Revocation test"});e!=nil{t.Fatal(e)}
 revoked,e:=runtime.Sync(ctx,client,endpoint);if e!=nil||!revoked.ProcessReady||revoked.ActiveSequence!=2||revoked.ActiveSHA==state.ActiveSHA{t.Fatalf("last client revoke did not apply: %+v %v",revoked,e)}
 req,_:=http.NewRequest("GET",server.URL+"/api/v1/agent/native-runtime?node_id=native-node&installation_id="+installID,nil);req.Header.Set("Authorization","Bearer "+nodeToken)
 res,e:=client.Do(req);if e!=nil{t.Fatal(e)};var delivery managedxray.Delivery;e=json.NewDecoder(res.Body).Decode(&delivery);res.Body.Close();if e!=nil{t.Fatal(e)}
 raw,e:=base64.StdEncoding.DecodeString(delivery.ConfigBase64);if e!=nil{t.Fatal(e)};if bytes.Contains(raw,[]byte(c.UUID))||!bytes.Contains(raw,[]byte("\"clients\":[]")){t.Fatal("revoked UUID still active in delivered config")}
 runtime.Close();restarted,e:=managedxray.Open(o);if e!=nil{t.Fatal(e)};defer restarted.Close();if restarted.Snapshot().ProcessReady{t.Fatal("reboot invented active runtime")}
 recovered,e:=restarted.Sync(ctx,client,endpoint);if e!=nil||!recovered.ProcessReady||recovered.ActiveSHA!=revoked.ActiveSHA||recovered.ActiveSequence!=2{t.Fatalf("fresh pinned TLS recovery failed: %+v %v",recovered,e)}
 // Owner lifecycle travels through the same pinned TLS Agent, including the last-listener stop.
 for _,step:=range []struct{action string;expected uint64;stopped bool}{{"disable",1,true},{"enable",2,false},{"archive",3,true}}{
  w:=call("/api/v1/workspace/native-inbounds/101/lifecycle",business.InboundMutation{EventID:"native-inbound-life-"+step.action,ExpectedRevision:step.expected,Action:step.action,Reason:"Real Agent lifecycle acceptance"})
  if w.Code!=200{t.Fatalf("lifecycle %s failed: %d %s",step.action,w.Code,w.Body.String())}
  observed,e:=restarted.Sync(ctx,client,endpoint)
  if e!=nil||!observed.ProcessReady||observed.NoListeners!=step.stopped{t.Fatalf("lifecycle runtime %s: %+v %v",step.action,observed,e)}
  connection,e:=net.DialTimeout("tcp4",net.JoinHostPort("127.0.0.1",strconv.Itoa(int(port))),100*time.Millisecond)
  if e==nil{connection.Close()}
  if (e!=nil)!=step.stopped{t.Fatalf("lifecycle %s listener removal=%v",step.action,e!=nil)}
  report,e:=s.business.NativeRuntimeStatus(ctx,"native-node",installID)
  if e!=nil||report.NoListeners!=step.stopped||(step.stopped&&report.State!="listeners_disabled"){t.Fatalf("lifecycle report %s %+v %v",step.action,report,e)}
 }
 if _,e=s.business.Client(ctx,c.ID);e!=nil{t.Fatal("revocation deleted billing identity",e)};events,e:=s.business.ListEvents(ctx,"owner",100,0);if e!=nil||len(events)!=1{t.Fatal("native Apply altered immutable finance",e)}
}
func TestNativeOwnerToPinnedAgentRuntimeEndToEnd(t *testing.T){b,p:=nativeFixture(t);nativeAPIExercise(t,b,p)}
func TestRealPinnedNativeOwnerToAgentRuntime(t *testing.T){b,p:=os.Getenv("DARK_PRO_TEST_XRAY_BINARY"),os.Getenv("DARK_PRO_TEST_XRAY_SHA256");if b==""&&p==""{t.Skip("pinned Xray not provided")};if b==""||p==""{t.Fatal("both pin and binary required")};nativeAPIExercise(t,b,p)}
func TestNativeInboundOwnerOnlyCSRFAndFreshInstallation(t *testing.T){
 s:=enabledOwner(t);if e:=s.EnableNativeInbounds(bytes.Repeat([]byte{7},32));e!=nil{t.Fatal(e)};c,csrf:=browserLogin(t,s,ownerUsername,ownerPassword)
 input:=business.InboundInput{ID:102,Name:"Scoped",Transport:"tcp",RealityTarget:"example.org:443",ServerNames:[]string{"example.org"},ShortIDs:[]string{"aa"}}
 for _,attempt:=range []struct{cookie *http.Cookie;csrf string;code int}{{nil,csrf,401},{c,"wrong",403},{c,csrf,201}}{w:=browserRequest(t,s.Handler(),"POST","/api/v1/workspace/native-inbounds",input,attempt.cookie,attempt.csrf,"https://"+browserHost);if w.Code!=attempt.code{t.Fatalf("owner/CSRF check: %d",w.Code)}}
 target:=business.InboundTarget{InboundID:102,NodeID:"unpaired",InstallationID:installID,ListenIP:"127.0.0.1",DirectPort:19610}
 w:=browserRequest(t,s.Handler(),"POST","/api/v1/workspace/native-inbounds/102/target",target,c,csrf,"https://"+browserHost);if w.Code!=409{t.Fatal("unpaired installation acquired a native inbound")}
}
