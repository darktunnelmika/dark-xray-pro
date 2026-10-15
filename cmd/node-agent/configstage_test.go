package main

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/base64"
 "encoding/hex"
 "encoding/pem"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "runtime"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
 "github.com/darktunnelmika/dark-xray-pro/internal/api"
 "github.com/darktunnelmika/dark-xray-pro/internal/configstage"
 "github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
 "github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type stageFixture struct {
 client *http.Client
 endpoint string
 store *configstage.Store
 operation replacement.Operation
 cleanup func()
}

func executableFixture(t *testing.T,success bool)(string,string){
 t.Helper()
 if runtime.GOOS!="linux"{t.Skip("Xray validator integration uses Linux only")}
 script:=`#!/bin/sh
set -eu
[ "$#" -eq 4 ] || exit 21
[ "$1" = "run" ] && [ "$2" = "-test" ] && [ "$3" = "-config" ] || exit 22
[ -r "$4" ] && [ "$(stat -c %a "$4")" = "600" ] || exit 23
grep -q '"protocol": "vless"' "$4" || exit 24
`
 if success{script+="exit 0\n"}else{script+="exit 14\n"}
 file:=filepath.Join(t.TempDir(),"synthetic-xray-test")
 if err:=os.WriteFile(file,[]byte(script),0700);err!=nil{t.Fatal(err)}
 h:=sha256.Sum256([]byte(script))
 return file,hex.EncodeToString(h[:])
}

func isolatedStageFixture(t *testing.T, trustedSHA string)stageFixture{
 t.Helper()
 dir:=t.TempDir()
 reg,err:=noderegistry.Open(filepath.Join(dir,"hub-identity","nodes.json"),nil)
 if err!=nil{t.Fatal(err)}
 for _,n:=range []struct{id,install,token string}{
  {"old-de",oldInstall,oldToken},
  {"new-de",freshInstall,token},
 }{
  ticket,err:=reg.Issue(n.id,"LAB "+n.id)
  if err!=nil{t.Fatal(err)}
  if _,err:=reg.Claim(ticket.PairCode,n.id,n.install,n.token);err!=nil{t.Fatal(err)}
 }
 j,err:=replacement.Open(filepath.Join(dir,"ops","journal.json"),nil)
 if err!=nil{t.Fatal(err)}
 route,err:=routeplan.New("old-de","new-de")
 if err!=nil{t.Fatal(err)}
 if err:=route.SetDirect(1,8569);err!=nil{t.Fatal(err)}
 if err:=route.SetSwap(1,6800,1185,"10.10.0.2");err!=nil{t.Fatal(err)}
 preview,err:=route.Preview()
 if err!=nil{t.Fatal(err)}
 operation,err:=j.Create(preview,freshInstall)
 if err!=nil{t.Fatal(err)}
 checks,err:=replacement.Expect(preview)
 if err!=nil{t.Fatal(err)}
 for i:=range checks{checks[i].Reachable=true}
 operation,err=j.Report(replacement.Evidence{
  OperationID:operation.ID,ManifestSHA256:preview.ManifestSHA256,
  NewNodeID:operation.NewNodeID,InstallationID:freshInstall,Probes:checks,
 })
 if err!=nil{t.Fatal(err)}
 profile:=xrayconfig.Profile{
  InboundID:1,Transport:"tcp",RealityTarget:"www.example.org:443",
  ServerNames:[]string{"www.example.org"},
  RealityPrivateKey:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x33},32)),
  ShortIDs:[]string{"0123456789abcdef"},
  Users:[]xrayconfig.User{{UUID:"af469f46-e18a-45f4-bf57-c7f649b10db5",Email:"lab@xray.invalid"}},
 }
 candidate,err:=xrayconfig.Render(preview,[]xrayconfig.Profile{profile})
 if err!=nil{t.Fatal(err)}
 staged,err:=configstage.Open(filepath.Join(dir,"encrypted","staged.json"),bytes.Repeat([]byte{0x66},32),nil)
 if err!=nil{t.Fatal(err)}
 if _,err:=staged.Create(operation,candidate,trustedSHA);err!=nil{t.Fatal(err)}
 hub,err:=api.NewServer(telemetry.NewStore(0,nil),reg,owner)
 if err!=nil{t.Fatal(err)}
 if err:=hub.EnableReplacementLab(j);err!=nil{t.Fatal(err)}
 if err:=hub.EnableConfigStaging(staged);err!=nil{t.Fatal(err)}
 server:=httptest.NewTLSServer(hub.AgentHandler())
 sum:=sha256.Sum256(server.Certificate().Raw)
 ca:=pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:server.Certificate().Raw})
 client,endpoint,err:=agenttransport.New(agenttransport.Config{
  Endpoint:server.URL,PinSHA256:hex.EncodeToString(sum[:]),CABundle:ca,
 })
 if err!=nil{server.Close();t.Fatal(err)}
 return stageFixture{
  client:client,endpoint:endpoint,store:staged,operation:operation,
  cleanup:func(){client.CloseIdleConnections();server.Close()},
 }
}

func TestPairedAgentValidatesPrivateStageWithoutApplyingConfig(t *testing.T){
 binary,pin:=executableFixture(t,true)
 f:=isolatedStageFixture(t,pin)
 defer f.cleanup()
 ctx,cancel:=context.WithTimeout(context.Background(),6*time.Second)
 defer cancel()
 if err:=pollConfigStage(ctx,f.client,f.endpoint,"new-de",freshInstall,token,binary,pin);err!=nil{
  t.Fatalf("verified TLS delivery and offline Xray test failed: %v",err)
 }
 meta,err:=f.store.Get(f.operation.ID)
 if err!=nil{t.Fatal(err)}
 if meta.Status!=configstage.Verified||meta.Revision!=2||meta.CanActivate||
  !meta.NeedsDataPlaneProof||!meta.NeedsSubscriptionCommit{
  t.Fatalf("syntax proof illegally deployed customer config: %+v",meta)
 }
 if err:=pollConfigStage(ctx,f.client,f.endpoint,"new-de",freshInstall,token,binary,pin);err!=nil{
  t.Fatalf("completed stage should not be delivered again: %v",err)
 }
 if len(f.store.List())!=1{t.Fatal("duplicate stages after lost receipt")}
}

func TestFailedOfflineValidationCannotBecomeHealthyOrActive(t *testing.T){
 binary,pin:=executableFixture(t,false)
 f:=isolatedStageFixture(t,pin)
 defer f.cleanup()
 ctx,cancel:=context.WithTimeout(context.Background(),6*time.Second)
 defer cancel()
 if err:=pollConfigStage(ctx,f.client,f.endpoint,"new-de",freshInstall,token,binary,pin);err!=nil{
  t.Fatalf("rejected Xray syntax did not record safe negative proof: %v",err)
 }
 meta,err:=f.store.Get(f.operation.ID)
 if err!=nil{t.Fatal(err)}
 if meta.Status!=configstage.Rejected||meta.CanActivate||
  !meta.NeedsDataPlaneProof||!meta.NeedsSubscriptionCommit{
  t.Fatalf("failed Xray validator bypassed release gate: %+v",meta)
 }
}

func TestOldNodeOrMismatchedPinnedCoreCannotReadOrApproveStage(t *testing.T){
 binary,pin:=executableFixture(t,true)
 f:=isolatedStageFixture(t,pin)
 defer f.cleanup()
 ctx,cancel:=context.WithTimeout(context.Background(),6*time.Second)
 defer cancel()
 if err:=pollConfigStage(ctx,f.client,f.endpoint,"new-de",freshInstall,oldToken,binary,pin);err==nil{
  t.Fatal("retiring Node impersonated fresh Agent and retrieved private config")
 }
 if err:=pollConfigStage(ctx,f.client,f.endpoint,"new-de",freshInstall,token,binary,
  "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff");err==nil{
  t.Fatal("mismatched trusted Xray executable accepted")
 }
 meta,err:=f.store.Get(f.operation.ID)
 if err!=nil||meta.Status!=configstage.Pending{
  t.Fatalf("stage changed after untrusted request: %+v %v",meta,err)
 }
 if err:=pollConfigStage(ctx,f.client,"http://127.0.0.1:2087","new-de",freshInstall,token,binary,pin);err==nil{
  t.Fatal("plaintext transport could retrieve private Reality config")
 }
}
