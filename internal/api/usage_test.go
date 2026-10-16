//go:build linux

package api

import (
 "bytes"
 "context"
 "crypto/sha256"
 "crypto/tls"
 "encoding/hex"
 "encoding/json"
 "io"
 "net/http"
 "net/http/httptest"
 "os"
 "path/filepath"
 "strings"
 "sync/atomic"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
 "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
 uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)
func readyUsageServer(t *testing.T)(*Server,business.Client,business.NodeConfig,uc.Epoch){
 t.Helper();ctx:=context.Background();s:=enabledOwner(t)
 if e:=s.business.EnableUsageLedger(ctx);e!=nil{t.Fatal(e)}
 key:=bytes.Repeat([]byte{7},32);if e:=s.EnableNativeInbounds(key);e!=nil{t.Fatal(e)}
 invitation,e:=s.registry.Issue("usage-node","New metered test node");if e!=nil{t.Fatal(e)}
 if _,e=s.registry.Claim(invitation.PairCode,"usage-node",installID,nodeToken);e!=nil{t.Fatal(e)}
 if _,e=s.business.CreateRepresentative(ctx,"usage-rep","Usage representative");e!=nil{t.Fatal(e)}
 if _,e=s.business.AdjustCredits(ctx,business.CreditChange{EventID:"initial-credit-usage-rep",RepresentativeID:"usage-rep",VolumeDeltaBytes:10000,Reason:"Synthetic test allocation"});e!=nil{t.Fatal(e)}
 if _,e=s.business.ProvisionRepresentativeLogin(ctx,"usage-rep","usage-rep",repPassword);e!=nil{t.Fatal(e)}
 c,e:=s.business.CreateClient(ctx,business.NewClient{EventID:"create-usage-api-client-01",ClientID:"api-metered-client",RepresentativeID:"usage-rep",Name:"API test",DurationDays:30,VolumeBytes:1000});if e!=nil{t.Fatal(e)}
 if _,e=s.business.CreateInbound(ctx,key,business.InboundInput{ID:188,Name:"Metered",Transport:"tcp",RealityTarget:"example.org:443",ServerNames:[]string{"example.org"},ShortIDs:[]string{"aa11"}});e!=nil{t.Fatal(e)}
 if e=s.business.BindInboundClients(ctx,188,[]string{c.ID},true);e!=nil{t.Fatal(e)}
 if e=s.business.PutInboundTarget(ctx,business.InboundTarget{InboundID:188,NodeID:"usage-node",InstallationID:installID,ListenIP:"127.0.0.1",DirectPort:19844});e!=nil{t.Fatal(e)}
 cfg,e:=s.business.BuildNodeConfig(ctx,key,"usage-node",installID);if e!=nil{t.Fatal(e)}
 now:=time.Now();l,e:=managedxray.SignLease([]byte(nodeToken),managedxray.Lease{NodeID:cfg.NodeID,InstallationID:cfg.InstallationID,Sequence:cfg.Sequence,ConfigSHA:cfg.ConfigSHA,ManifestSHA:cfg.ManifestSHA,Issued:now.UnixNano(),Expires:now.Add(25*time.Second).UnixNano()});if e!=nil{t.Fatal(e)}
 if e=s.business.RecordNativeDelivery(ctx,cfg,l);e!=nil{t.Fatal(e)}
 p,e:=uc.Process(os.Getpid());if e!=nil{t.Fatal(e)}
 epoch,e:=s.business.RegisterNativeUsageEpoch(ctx,cfg,business.UsageBirth{EpochID:strings.Repeat("e",32),KernelBootID:p.BootID,PID:p.PID,StartTicks:p.StartTicks});if e!=nil{t.Fatal(e)}
 return s,c,cfg,epoch
}
func TestRealRegistryUsageTLSCommitReceiptLossQuotaAndPublicDenial(t *testing.T){
 s,c,cfg,epoch:=readyUsageServer(t);ctx:=context.Background();var lost atomic.Bool
 agent:=s.AgentHandler();tlsServer:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
  if r.URL.Path=="/api/v1/agent/native-usage"&&!lost.Swap(true){rec:=httptest.NewRecorder();agent.ServeHTTP(rec,r);if rec.Code==200{http.Error(w,"simulated lost response after COMMIT",503)}else{w.WriteHeader(rec.Code)};return};agent.ServeHTTP(w,r)
 }))
 tlsServer.TLS=&tls.Config{MinVersion:tls.VersionTLS13};tlsServer.StartTLS();defer tlsServer.Close()
 sha:=sha256.Sum256(tlsServer.Certificate().Raw)
 sender,e:=uc.NewSender(uc.SenderOptions{Endpoint:tlsServer.URL,NodeID:epoch.NodeID,InstallationID:epoch.InstallationID,Token:nodeToken,PinSHA256:hex.EncodeToString(sha[:]),Client:tlsServer.Client()});if e!=nil{t.Fatal(e)};defer sender.Close()
 o:=uc.Options{Directory:filepath.Join(t.TempDir(),"outbox"),Epoch:epoch,Key:bytes.Repeat([]byte{4},32),Create:true}
 box,e:=uc.Open(o);if e!=nil{t.Fatal(e)}
 report,e:=box.Observe([]uc.Usage{{ClientID:c.ID,Uplink:600,Downlink:700}},false);if e!=nil{t.Fatal(e)}
 if _,e=sender.FlushOne(ctx,box);e==nil{t.Fatal("lost commit response was acknowledged")}
 a,e:=s.business.ClientUsage(ctx,c.ID);if e!=nil||a.Total!=1300||a.Eligible{t.Fatalf("quota ledger wrong: %+v %v",a,e)}
 pending,e:=box.Pending();if e!=nil||len(pending)!=1{t.Fatal("lost receipt discarded pending",e)}
 if e=box.Close();e!=nil{t.Fatal(e)};o.Create=false;box,e=uc.Open(o);if e!=nil{t.Fatal(e)};defer box.Close()
 if sent,e:=sender.FlushOne(ctx,box);e!=nil||!sent{t.Fatal("idempotent retransmit failed",e)}
 a,e=s.business.ClientUsage(ctx,c.ID);if e!=nil||a.Total!=1300{t.Fatal("resend double charged",e)}
 pending,e=box.Pending();if e!=nil||len(pending)!=0{t.Fatal("matching committed receipt not acknowledged",e)}
 next,e:=s.business.BuildNodeConfig(ctx,s.inboundKey,cfg.NodeID,cfg.InstallationID);if e!=nil||next.UserCount!=0||bytes.Contains(next.Candidate.Config,[]byte(c.UUID)){t.Fatal("receipt did not remove exhausted UUID from next config",e)}
 body,_:=json.Marshal(report)
 path:="/api/v1/agent/native-usage?node_id="+epoch.NodeID+"&installation_id="+epoch.InstallationID
 for _,token:=range []string{ownerToken,otherToken,""}{req,_:=http.NewRequest("POST",tlsServer.URL+path,bytes.NewReader(body));req.Header.Set("Authorization","Bearer "+token);req.Header.Set("Content-Type","application/json");res,e:=tlsServer.Client().Do(req);if e!=nil{t.Fatal(e)};res.Body.Close();if res.StatusCode!=401{t.Fatal("non-node identity could post usage",res.StatusCode)}}
 public:=httptest.NewRequest("POST",path,bytes.NewReader(body));public.Header.Set("Authorization","Bearer "+nodeToken);public.TLS=&tls.ConnectionState{Version:tls.VersionTLS13}
 w:=httptest.NewRecorder();s.Handler().ServeHTTP(w,public);if w.Code!=404{t.Fatal("owner listener exposed usage receiver",w.Code)}
 // Until supervised collection is implemented, metered mode MUST NOT silently
 // issue unmetered customer runtime leases. This is an explicit release fence.
 req,_:=http.NewRequest("GET",tlsServer.URL+"/api/v1/agent/native-runtime?node_id="+epoch.NodeID+"&installation_id="+epoch.InstallationID,nil);req.Header.Set("Authorization","Bearer "+nodeToken)
 res,e:=tlsServer.Client().Do(req);if e!=nil{t.Fatal(e)};raw,_:=io.ReadAll(res.Body);res.Body.Close();if res.StatusCode!=503||!bytes.Contains(raw,[]byte("metered_runtime_admission_pending")){t.Fatal("unmetered runtime lease escaped admission fence")}
}
func TestUsageReadRepresentativeScopeUnknownAndDisabledFeature(t *testing.T){
 s,c,_,_:=readyUsageServer(t);cookie,_:=browserLogin(t,s,"usage-rep",repPassword)
 w:=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/clients/"+c.ID+"/usage",nil,cookie,"","")
 if w.Code!=200||!strings.Contains(w.Body.String(),`"total_bytes":"0"`)||!strings.Contains(w.Body.String(),`"known":true`){t.Fatalf("own usage not readable: %d %s",w.Code,w.Body.String())}
 foreign,e:=s.business.CreateClient(context.Background(),business.NewClient{EventID:"foreign-usage-client-001",ClientID:"foreign-client",RepresentativeID:"owner",Name:"Other tenant",DurationDays:30,VolumeBytes:1000});if e!=nil{t.Fatal(e)}
 w=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/clients/"+foreign.ID+"/usage",nil,cookie,"","");if w.Code!=403{t.Fatal("representative read foreign usage",w.Code)}
 w=browserRequest(t,s.Handler(),"GET","/api/v1/workspace/clients/"+c.ID+"/usage",nil,nil,"","");if w.Code!=401{t.Fatal("anonymous usage disclosure",w.Code)}
 disabled:=enabledOwner(t)
 w=browserRequest(t,disabled.Handler(),"GET","/api/v1/workspace/clients/x/usage",nil,cookie,"","");if w.Code!=404{t.Fatal("disabled usage feature exposes fabricated totals")}
}
