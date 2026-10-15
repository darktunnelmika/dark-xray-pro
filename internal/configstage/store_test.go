package configstage

import (
 "bytes"
 "crypto/rand"
 "encoding/base64"
 "encoding/json"
 "errors"
 "os"
 "reflect"
 "path/filepath"
 "strings"
 "sync"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

const (
 testInstall="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
 testBinarySHA="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func fixture(t *testing.T)(replacement.Operation,xrayconfig.Candidate){
 t.Helper()
 now:=time.Date(2026,10,12,8,0,0,0,time.UTC)
 draft,err:=routeplan.New("retiring-lab","fresh-lab")
 if err!=nil{t.Fatal(err)}
 if err:=draft.SetDirect(1,8569);err!=nil{t.Fatal(err)}
 if err:=draft.SetSwap(1,6800,1185,"10.10.0.2");err!=nil{t.Fatal(err)}
 preview,err:=draft.Preview()
 if err!=nil{t.Fatal(err)}
 j,err:=replacement.Open(filepath.Join(t.TempDir(),"operations","journal.json"),
  func()time.Time{return now})
 if err!=nil{t.Fatal(err)}
 op,err:=j.Create(preview,testInstall)
 if err!=nil{t.Fatal(err)}
 probes,err:=replacement.Expect(preview)
 if err!=nil{t.Fatal(err)}
 for i:=range probes{probes[i].Reachable=true}
 op,err=j.Report(replacement.Evidence{
  OperationID:op.ID,ManifestSHA256:preview.ManifestSHA256,
  NewNodeID:"fresh-lab",InstallationID:testInstall,Probes:probes,
 })
 if err!=nil{t.Fatal(err)}
 profile:=xrayconfig.Profile{
  InboundID:1,Transport:"tcp",RealityTarget:"www.example.org:443",
  ServerNames:[]string{"www.example.org"},
  RealityPrivateKey:base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x11},32)),
  ShortIDs:[]string{"abcdef0123456789"},
  Users:[]xrayconfig.User{
   {UUID:"af469f46-e18a-45f4-bf57-c7f649b10db5",Email:"user@lab.invalid"},
  },
 }
 c,err:=xrayconfig.Render(preview,[]xrayconfig.Profile{profile})
 if err!=nil{t.Fatal(err)}
 return op,c
}

func initStore(t *testing.T)(*Store,[]byte,string){
 t.Helper()
 key:=make([]byte,32)
 if _,err:=rand.Read(key);err!=nil{t.Fatal(err)}
 path:=filepath.Join(t.TempDir(),"secret-dir","stages.json")
 s,err:=Open(path,key,nil)
 if err!=nil{t.Fatal(err)}
 return s,key,path
}

func proofOf(m Metadata,valid bool)Proof{
 p:=Proof{OperationID:m.OperationID,NodeID:m.NodeID,InstallationID:m.InstallationID,
  ConfigSHA256:m.ConfigSHA256,ManifestSHA256:m.ManifestSHA256,
  TrustedXraySHA256:m.TrustedXraySHA256,SyntaxValid:valid}
 if !valid{p.ErrorCode="xray_rejected"}
 return p
}

func TestSealedCandidateDurableWithoutPlaintextLeak(t *testing.T){
 s,key,path:=initStore(t)
 op,c:=fixture(t)
 m,err:=s.Create(op,c,testBinarySHA)
 if err!=nil{t.Fatal(err)}
 if m.Status!=Pending||m.CanActivate||!m.NeedsSubscriptionCommit||
  !m.NeedsDataPlaneProof||m.ConfigSHA256!=c.SHA256||m.InstallationID!=op.InstallationID{
  t.Fatalf("unsafe staging metadata: %+v",m)
 }
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 if bytes.Contains(raw,c.Config)||bytes.Contains(raw,[]byte("privateKey"))||
  bytes.Contains(raw,[]byte("www.example.org")) {
  t.Fatal("private config readable in staged state")
 }
 st,err:=os.Stat(path)
 if err!=nil||st.Mode().Perm()!=0600{t.Fatal("stage file permissions unsafe",err)}
 got,plaintext,ok,err:=s.PendingFor(op.NewNodeID,op.InstallationID)
 if err!=nil||!ok||!reflect.DeepEqual(got,m)||!bytes.Equal(plaintext,c.Config){
  t.Fatal("paired Agent couldn't fetch exactly hashed private candidate",err)
 }
 if _,_,ok,err:=s.PendingFor("old-lab",op.InstallationID);ok||err!=nil{t.Fatal("old node accessed new config")}
 if _,_,ok,err:=s.PendingFor(op.NewNodeID,"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");ok||err!=nil{
  t.Fatal("wrong installation accessed new config")
 }
 reloaded,err:=Open(path,key,nil)
 if err!=nil{t.Fatal("staging did not survive restart",err)}
 _,config,ok,err:=reloaded.PendingFor(op.NewNodeID,op.InstallationID)
 if err!=nil||!ok||!bytes.Equal(config,c.Config){t.Fatal("encrypted candidate lost after restart",err)}
 fakeKey:=bytes.Repeat([]byte{0xff},32)
 if _,err:=Open(path,fakeKey,nil);!errors.Is(err,ErrUnsafe){
  t.Fatalf("wrong key decrypted staging journal: %v",err)
 }
 ownerJSON,err:=json.Marshal(reloaded.List())
 if err!=nil{t.Fatal(err)}
 if bytes.Contains(ownerJSON,[]byte("privateKey"))||
  bytes.Contains(ownerJSON,[]byte("ciphertext"))||
  bytes.Contains(ownerJSON,[]byte("config_base64")){
  t.Fatal("public metadata leaks candidate config")
 }
}

func TestIdempotentStageAndProofCannotActivate(t *testing.T){
 s,_,_:=initStore(t)
 op,c:=fixture(t)
 a,err:=s.Create(op,c,testBinarySHA)
 if err!=nil{t.Fatal(err)}
 b,err:=s.Create(op,c,testBinarySHA)
 if err!=nil||!reflect.DeepEqual(a,b){t.Fatalf("reissued identical stage was not idempotent: %v",err)}
 modified:=c
 modified.Config=append([]byte(nil),c.Config...)
 modified.Config[0]='!'
 if _,err:=s.Create(op,modified,testBinarySHA);!errors.Is(err,ErrInvalid){
  t.Fatalf("tampered candidate accepted: %v",err)
 }
 if _,err:=s.Create(op,c,strings.Repeat("c",64));!errors.Is(err,ErrConflict){
  t.Fatal("swapped Xray binary pin overwrote pending config",err)
 }
 p:=proofOf(a,true)
 forged:=p
 forged.InstallationID="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
 if _,err:=s.Report(forged);!errors.Is(err,ErrMismatch){t.Fatal("wrong Agent ACK accepted",err)}
 forged=p
 forged.ConfigSHA256=strings.Repeat("f",64)
 if _,err:=s.Report(forged);!errors.Is(err,ErrMismatch){t.Fatal("wrong config ACK accepted",err)}
 result,err:=s.Report(p)
 if err!=nil{t.Fatal(err)}
 if result.Status!=Verified||result.CanActivate||!result.NeedsDataPlaneProof||
  !result.NeedsSubscriptionCommit||result.Revision!=2{
  t.Fatalf("syntax validation illegally published live routes: %+v",result)
 }
 replay,err:=s.Report(p)
 if err!=nil||!reflect.DeepEqual(replay,result){t.Fatalf("lost ACK wasn't idempotent: %v",err)}
 if _,_,ok,err:=s.PendingFor(op.NewNodeID,op.InstallationID);ok||err!=nil{
  t.Fatal("completed syntax proof was reissued for validation")
 }
 wrong:=p;wrong.SyntaxValid=false;wrong.ErrorCode="xray_rejected"
 if _,err:=s.Report(wrong);!errors.Is(err,ErrConflict){
  t.Fatalf("different report rewrote locked evidence: %v",err)
 }
}

func TestNegativeSyntaxAndUnsafeStageInputs(t *testing.T){
 s,_,_:=initStore(t)
 op,c:=fixture(t)
 invalid:=op
 invalid.Phase=replacement.Pending
 if _,err:=s.Create(invalid,c,testBinarySHA);!errors.Is(err,ErrInvalid){
  t.Fatal("TCP-unverified operation accepted",err)
 }
 invalid=op;invalid.CanActivate=true
 if _,err:=s.Create(invalid,c,testBinarySHA);!errors.Is(err,ErrInvalid){
  t.Fatal("pre-activated candidate was accepted",err)
 }
 invalid=op;invalid.Evidence=nil
 if _,err:=s.Create(invalid,c,testBinarySHA);!errors.Is(err,ErrInvalid){
  t.Fatal("missing TCP evidence accepted",err)
 }
 if _,err:=s.Create(op,c,"not-a-trusted-hash");!errors.Is(err,ErrInvalid){
  t.Fatal("invalid pinned Xray identity accepted",err)
 }
 m,err:=s.Create(op,c,testBinarySHA)
 if err!=nil{t.Fatal(err)}
 p:=proofOf(m,false)
 bad:=p;bad.ErrorCode="unbounded remote error or secrets"
 if _,err:=s.Report(bad);!errors.Is(err,ErrInvalid){
  t.Fatal("raw Xray exception could leak into journal",err)
 }
 result,err:=s.Report(p)
 if err!=nil||result.Status!=Rejected||result.CanActivate{
  t.Fatalf("failed Xray claimed success: %+v %v",result,err)
 }
}

func TestTamperAndWorldReadableJournalRefused(t *testing.T){
 s,key,path:=initStore(t)
 op,c:=fixture(t)
 if _,err:=s.Create(op,c,testBinarySHA);err!=nil{t.Fatal(err)}
 raw,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 var decoded map[string]any
 if err:=json.Unmarshal(raw,&decoded);err!=nil{t.Fatal(err)}
 stages:=decoded["stages"].(map[string]any)
 entry:=stages[op.ID].(map[string]any)
 entry["ciphertext"]="AAAAAAAAAAAA"
 malicious,err:=json.Marshal(decoded)
 if err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,malicious,0600);err!=nil{t.Fatal(err)}
 if _,err:=Open(path,key,nil);!errors.Is(err,ErrUnsafe){
  t.Fatalf("corrupted cipher was loaded: %v",err)
 }
 if err:=os.WriteFile(path,raw,0644);err!=nil{t.Fatal(err)}
 if err:=os.Chmod(path,0644);err!=nil{t.Fatal(err)}
 if _,err:=Open(path,key,nil);!errors.Is(err,ErrUnsafe){
  t.Fatalf("exposed file loaded: %v",err)
 }
}

func TestConcurrentDuplicateStageNeverChangesIdentity(t *testing.T){
 s,_,_:=initStore(t)
 op,c:=fixture(t)
 var wg sync.WaitGroup
 results:=make(chan Metadata,20)
 errs:=make(chan error,20)
 for i:=0;i<20;i++{
  wg.Add(1)
  go func(){
   defer wg.Done()
   meta,err:=s.Create(op,c,testBinarySHA)
   results<-meta;errs<-err
  }()
 }
 wg.Wait()
 close(results);close(errs)
 for err:=range errs{if err!=nil{t.Fatal(err)}}
 for m:=range results{
  if m.Status!=Pending||m.Revision!=1||m.ConfigSHA256!=c.SHA256{
   t.Fatal("concurrent duplicate stage changed Xray identity")
  }
 }
 if len(s.List())!=1{t.Fatal("concurrency created duplicate staged configs")}
}

func TestListenerMetadataBoundToEncryptedCandidateAndDetachedFromCaller(t *testing.T){
 s,key,path:=initStore(t)
 op,c:=fixture(t)
 meta,err:=s.Create(op,c,testBinarySHA)
 if err!=nil{t.Fatal(err)}
 if len(meta.Listeners)!=2{t.Fatalf("expected independently tagged Direct+Tunnel: %+v",meta.Listeners)}
 meta.Listeners[0].Tag="unauthorized-change"
 fromDisk,err:=s.Get(op.ID)
 if err!=nil{t.Fatal(err)}
 if fromDisk.Listeners[0].Tag=="unauthorized-change"{
  t.Fatal("public metadata changed confidential backing record")
 }
 buf,err:=os.ReadFile(path)
 if err!=nil{t.Fatal(err)}
 var disk map[string]any
 if err:=json.Unmarshal(buf,&disk);err!=nil{t.Fatal(err)}
 entry:=disk["stages"].(map[string]any)[op.ID].(map[string]any)
 record:=entry["metadata"].(map[string]any)
 listeners:=record["listeners"].([]any)
 one:=listeners[0].(map[string]any)
 one["tag"]="forged-route-tag"
 tampered,err:=json.Marshal(disk)
 if err!=nil{t.Fatal(err)}
 if err:=os.WriteFile(path,tampered,0600);err!=nil{t.Fatal(err)}
 if _,err:=Open(path,key,nil);!errors.Is(err,ErrUnsafe){
  t.Fatalf("modifying Listener metadata did not invalidate AES authentication: %v",err)
 }
}
