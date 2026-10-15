package api

import (
 "bytes"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "encoding/pem"
 "net/http"
 "net/http/httptest"
 "path/filepath"
 "strings"
 "testing"

 "github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
 "github.com/darktunnelmika/dark-xray-pro/internal/configstage"
 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

const expectedXraySHA="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func fakeReality(inbound uint64)xrayconfig.Profile{
 return xrayconfig.Profile{
  InboundID:inbound,Transport:"tcp",RealityTarget:"www.example.org:443",
  ServerNames:[]string{"www.example.org"},
  RealityPrivateKey:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x11},32)),
  ShortIDs:[]string{"0123456789abcdef"},
  Users:[]xrayconfig.User{{UUID:"af469f46-e18a-45f4-bf57-c7f649b10db5",
   Email:"example@lab.invalid"}},
 }
}

func setupStage(t *testing.T)(*Server,*configstage.Store,replacement.Operation,[]xrayconfig.Profile){
 t.Helper()
 s,_:=replacementEnv(t)
 sealed,err:=configstage.Open(filepath.Join(t.TempDir(),"private","stages.json"),
  bytes.Repeat([]byte{0x55},32),nil)
 if err!=nil{t.Fatal(err)}
 if err:=s.EnableConfigStaging(sealed);err!=nil{t.Fatal(err)}
 h:=s.Handler()
 created:=request(t,h,"POST","/api/v1/admin/replacements",ownerToken,testPlan())
 if created.Code!=201{t.Fatal(created.Code,created.Body.String())}
 var op replacement.Operation
 if err:=json.Unmarshal(created.Body.Bytes(),&op);err!=nil{t.Fatal(err)}
 expected,err:=replacement.Expect(op.Preview)
 if err!=nil{t.Fatal(err)}
 for i:=range expected{expected[i].Reachable=true}
 receipt:=replacement.Evidence{OperationID:op.ID,ManifestSHA256:op.Preview.ManifestSHA256,
  NewNodeID:op.NewNodeID,InstallationID:op.InstallationID,Probes:expected}
 validated:=request(t,h,"POST","/api/v1/agent/replacement-report",newToken,receipt)
 if validated.Code!=200{t.Fatal(validated.Code,validated.Body.String())}
 profiles:=[]xrayconfig.Profile{fakeReality(1),fakeReality(2)}
 return s,sealed,op,profiles
}

func TestOwnerOnlyEncryptedStageAndTLSAgentReceipt(t *testing.T){
 s,store,op,profiles:=setupStage(t)
 h:=s.Handler()
 path:="/api/v1/admin/replacements/"+op.ID+"/candidate"
 body:=map[string]any{"profiles":profiles,"trusted_xray_sha256":expectedXraySHA}
 if w:=request(t,h,"POST",path,"",body);w.Code!=401 {
  t.Fatalf("anonymous owner candidate accepted: %d",w.Code)
 }
 if w:=request(t,h,"POST",path,newToken,body);w.Code!=401{
  t.Fatalf("paired Agent became owner: %d",w.Code)
 }
 created:=request(t,h,"POST",path,ownerToken,body)
 if created.Code!=201{t.Fatalf("stage %d: %s",created.Code,created.Body.String())}
 var meta configstage.Metadata
 if err:=json.Unmarshal(created.Body.Bytes(),&meta);err!=nil{t.Fatal(err)}
 if meta.Status!=configstage.Pending||meta.CanActivate||
  !meta.NeedsDataPlaneProof||!meta.NeedsSubscriptionCommit||
  meta.ConfigSHA256==""{
  t.Fatalf("owner response incorrectly activated candidate: %+v",meta)
 }
 key:=profiles[0].RealityPrivateKey
 for _,response:=range [][]byte{
  created.Body.Bytes(),
  request(t,h,"GET","/api/v1/admin/config-stages",ownerToken,nil).Body.Bytes(),
  request(t,h,"GET","/api/v1/admin/config-stages/"+op.ID,ownerToken,nil).Body.Bytes(),
 }{
  if bytes.Contains(response,[]byte(key))||bytes.Contains(response,[]byte("privateKey"))||
   bytes.Contains(response,[]byte("ciphertext"))||bytes.Contains(response,[]byte("config_base64")){
   t.Fatal("owner JSON leaked private Reality material")
  }
 }
 if w:=request(t,h,"POST",path,ownerToken,body);w.Code!=201 {
  t.Fatalf("idempotent owner request created conflict: %d",w.Code)
 }
 uri:="/api/v1/agent/config-stage?node_id=new-de&installation_id="+newInstallation
 if w:=request(t,h,"GET",uri,newToken,nil);w.Code!=http.StatusUpgradeRequired {
  t.Fatal("plaintext management listener returned secret config",w.Code)
 }
 tlsSrv:=httptest.NewTLSServer(s.AgentHandler())
 defer tlsSrv.Close()
 sum:=sha256.Sum256(tlsSrv.Certificate().Raw)
 trusted:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:tlsSrv.Certificate().Raw})
 client,url,err:=agenttransport.New(agenttransport.Config{
  Endpoint:tlsSrv.URL,PinSHA256:hex.EncodeToString(sum[:]),CABundle:trusted,
 })
 if err!=nil{t.Fatal(err)}
 defer client.CloseIdleConnections()
 load:=func(token string)(*http.Response,error){
  req,err:=http.NewRequest(http.MethodGet,url+uri,nil)
  if err!=nil{return nil,err}
  req.Header.Set("Authorization","Bearer "+token)
  return client.Do(req)
 }
 denied,err:=load(nodeToken)
 if err!=nil{t.Fatal(err)}
 denied.Body.Close()
 if denied.StatusCode!=401{t.Fatalf("retired Agent saw secret: %d",denied.StatusCode)}
 received,err:=load(newToken)
 if err!=nil{t.Fatal(err)}
 defer received.Body.Close()
 if received.StatusCode!=200{t.Fatalf("paired Agent couldn't load candidate: %d",received.StatusCode)}
 var delivery configstage.Delivery
 if err:=json.NewDecoder(received.Body).Decode(&delivery);err!=nil{t.Fatal(err)}
 raw,err:=base64.StdEncoding.DecodeString(delivery.ConfigBase64)
 if err!=nil{t.Fatal(err)}
 checksum:=sha256.Sum256(raw)
 if hex.EncodeToString(checksum[:])!=meta.ConfigSHA256 ||
  !bytes.Contains(raw,[]byte(key)){
  t.Fatal("validated TLS channel did not return exact original Reality secret")
 }
 invalid:=configstage.Proof{OperationID:op.ID,NodeID:"new-de",
  InstallationID:newInstallation,ManifestSHA256:meta.ManifestSHA256,
  ConfigSHA256:meta.ConfigSHA256,TrustedXraySHA256:strings.Repeat("f",64),
  SyntaxValid:true}
 send:=func(token string,p configstage.Proof)int{
  t.Helper()
  raw,err:=json.Marshal(p)
  if err!=nil{t.Fatal(err)}
  req,err:=http.NewRequest(http.MethodPost,url+"/api/v1/agent/config-stage-report",bytes.NewReader(raw))
  if err!=nil{t.Fatal(err)}
  req.Header.Set("Authorization","Bearer "+token)
  req.Header.Set("Content-Type","application/json")
  res,err:=client.Do(req)
  if err!=nil{t.Fatal(err)}
  defer res.Body.Close()
  return res.StatusCode
 }
 if code:=send(nodeToken,invalid);code!=401{t.Fatalf("old Node could report success: %d",code)}
 if code:=send(newToken,invalid);code!=409{t.Fatalf("wrong binary pin accepted: %d",code)}
 invalid.TrustedXraySHA256=expectedXraySHA
 if code:=send(newToken,invalid);code!=200{t.Fatalf("trusted syntax ACK rejected: %d",code)}
 if code:=send(newToken,invalid);code!=200{t.Fatalf("same ACK was not idempotent: %d",code)}
 result,err:=store.Get(op.ID)
 if err!=nil||result.Status!=configstage.Verified||result.CanActivate{
  t.Fatalf("offline validation was treated as activation: %+v %v",result,err)
 }
 if w:=request(t,h,"POST","/api/v1/admin/replacements/"+op.ID+"/activate",ownerToken,body);w.Code!=404{
  t.Fatal("unexpected Xray activation route exposed",w.Code)
 }
}

func TestInvalidOrUnverifiedStageNeverReturnsLiveConfig(t *testing.T){
 s,_,op,profiles:=setupStage(t)
 h:=s.Handler()
 profiles[0].RealityPrivateKey="invalid-secret"
 request:=map[string]any{"profiles":profiles,"trusted_xray_sha256":expectedXraySHA}
 path:="/api/v1/admin/replacements/"+op.ID+"/candidate"
 if w:=requestFn(t,h,"POST",path,ownerToken,request);w.Code!=422{
  t.Fatalf("invalid Reality key passed review: %d",w.Code)
 }
 if w:=requestFn(t,h,"GET","/api/v1/admin/config-stages",newToken,nil);w.Code!=401{
  t.Fatal("Agent accessed owner stage roster",w.Code)
 }
}

func requestFn(t *testing.T,h http.Handler,method,path,token string,body any)*httptest.ResponseRecorder{
 return request(t,h,method,path,token,body)
}
