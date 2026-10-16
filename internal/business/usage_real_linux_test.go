//go:build linux

package business

import (
 "bytes"
 "context"
 "crypto/ecdsa"
 "crypto/elliptic"
 "crypto/rand"
 "crypto/sha256"
 "crypto/tls"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/hex"
 "encoding/json"
 "encoding/pem"
 "errors"
 "io"
 "log"
 "math/big"
 "net"
 "net/http"
 "net/http/httptest"
 "os"
 "os/exec"
 "path/filepath"
 "strconv"
 "strings"
 "testing"
 "time"

 "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
 uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
 "github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
func meterPort(t *testing.T)uint16{t.Helper();l,e:=net.Listen("tcp4","127.0.0.1:0");if e!=nil{t.Fatal(e)};defer l.Close();return uint16(l.Addr().(*net.TCPAddr).Port)}
func meterWait(t *testing.T,fn func()bool){t.Helper();until:=time.Now().Add(4*time.Second);for time.Now().Before(until){if fn(){return};time.Sleep(25*time.Millisecond)};t.Fatal("bounded real-core condition timed out")}
func meterSign(t *testing.T,o managedxray.Options,sequence uint64,c xrayconfig.Candidate)managedxray.Lease{
 t.Helper();now:=time.Now();l,e:=managedxray.SignLease(o.LeaseKey,managedxray.Lease{NodeID:o.NodeID,InstallationID:o.InstallationID,Sequence:sequence,ConfigSHA:c.SHA256,ManifestSHA:c.ManifestSHA256,Issued:now.UnixNano(),Expires:now.Add(40*time.Second).UnixNano()});if e!=nil{t.Fatal(e)};return l
}

// A controlled native pipeline, not a production metering rollout. The test
// injects ONLY disposable loopback StatsService and a local REALITY target.
func TestRealPinnedUsageCountersSQLiteQuotaAndXrayRevocation(t *testing.T){exerciseRealPinnedUsage(t,false)}
func TestRealPinnedContinuousUsageObserverTLSReplay(t *testing.T){exerciseRealPinnedUsage(t,true)}
func exerciseRealPinnedUsage(t *testing.T,continuous bool){
 binary,pin:=os.Getenv("DARK_PRO_TEST_XRAY_BINARY"),os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
 if binary==""&&pin==""{t.Skip("explicit independently pinned Xray required")};if binary==""||pin==""{t.Fatal("incomplete core pin")}
 if os.Geteuid()==0{t.Skip("Xray QA must be unprivileged")}
 s,_,key,c,cfg:=usageSetup(t);ctx:=context.Background();target:=meterRealityTarget(t)
 proof:=strings.Repeat("native-metered-payload-",300)
 origin:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){_,_=io.WriteString(w,proof)}));defer origin.Close()
 serverPort,statsPort,socksPort:=meterPort(t),meterPort(t),meterPort(t)
 for statsPort==serverPort{statsPort=meterPort(t)};for socksPort==statsPort||socksPort==serverPort{socksPort=meterPort(t)}
 if e:=s.PutInboundTarget(ctx,InboundTarget{InboundID:10,NodeID:cfg.NodeID,InstallationID:cfg.InstallationID,ListenIP:"127.0.0.1",DirectPort:serverPort});e!=nil{t.Fatal(e)}
 cfg,e:=s.BuildNodeConfig(ctx,key,cfg.NodeID,cfg.InstallationID);if e!=nil{t.Fatal(e)}
 statsAddress:=net.JoinHostPort("127.0.0.1",strconv.Itoa(int(statsPort)))
 seal:=func(c xrayconfig.Candidate)xrayconfig.Candidate{
  var doc map[string]any;if json.Unmarshal(c.Config,&doc)!=nil{t.Fatal("invalid core document")}
  in:=doc["inbounds"].([]any)[0].(map[string]any)
  reality:=in["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
  reality["dest"]=target.Listener.Addr().String();reality["serverNames"]=[]string{"one.example.org"}
  // TEST ONLY: no production config gains an API listener or localhost bypass.
  doc["api"]=map[string]any{"tag":"meter-only","listen":statsAddress,"services":[]string{"StatsService"}}
  originPort:=origin.Listener.Addr().(*net.TCPAddr).Port
  doc["outbounds"].([]any)[0].(map[string]any)["settings"]=map[string]any{"finalRules":[]any{map[string]any{"action":"allow","network":"tcp","ip":[]string{"127.0.0.1/32"},"port":strconv.Itoa(originPort)}}}
  raw,e:=json.Marshal(doc);if e!=nil{t.Fatal(e)};sum:=sha256.Sum256(raw);c.Config=raw;c.SHA256=hex.EncodeToString(sum[:]);m:=sha256.Sum256([]byte("TEST-native-usage:"+c.SHA256));c.ManifestSHA256=hex.EncodeToString(m[:]);return c
 }
 cfg.Candidate=seal(cfg.Candidate);cfg.ConfigSHA=cfg.Candidate.SHA256;cfg.ManifestSHA=cfg.Candidate.ManifestSHA256
 options:=managedxray.Options{Directory:filepath.Join(t.TempDir(),"runtime"),StateKey:bytes.Repeat([]byte{2},32),LeaseKey:bytes.Repeat([]byte{3},64),NodeID:cfg.NodeID,InstallationID:cfg.InstallationID,ListenIP:"127.0.0.1",Binary:binary,BinarySHA:pin}
 runtime,e:=managedxray.Open(options);if e!=nil{t.Fatal(e)};defer runtime.Close()
 lease:=meterSign(t,options,cfg.Sequence,cfg.Candidate)
 if _,e=runtime.Apply(ctx,managedxray.Revision{Sequence:cfg.Sequence,Candidate:cfg.Candidate},lease);e!=nil{t.Fatal(e)}
 if e=s.RecordNativeDelivery(ctx,cfg,lease);e!=nil{t.Fatal(e)}
 birth,e:=runtime.ObservationProcess();if e!=nil{t.Fatal(e)}
 epoch,e:=s.RegisterNativeUsageEpoch(ctx,cfg,UsageBirth{EpochID:strings.Repeat("9",32),KernelBootID:birth.BootID,PID:birth.PID,StartTicks:birth.StartTicks});if e!=nil{t.Fatal(e)}
 profiles,e:=s.ListInbounds(ctx,key);if e!=nil{t.Fatal(e)}
 clientDoc:=map[string]any{"log":map[string]string{"loglevel":"none"},
  "inbounds":[]any{map[string]any{"listen":"127.0.0.1","port":socksPort,"protocol":"socks","settings":map[string]any{"auth":"noauth","udp":false}}},
  "outbounds":[]any{map[string]any{"protocol":"vless","settings":map[string]any{"vnext":[]any{map[string]any{"address":"127.0.0.1","port":serverPort,"users":[]any{map[string]any{"id":c.UUID,"encryption":"none","flow":"xtls-rprx-vision"}}}}},"streamSettings":map[string]any{"network":"tcp","security":"reality","realitySettings":map[string]any{"serverName":"one.example.org","fingerprint":"chrome","publicKey":profiles[0].PublicKey,"shortId":"aa11"}}}},
 }
 raw,e:=json.Marshal(clientDoc);if e!=nil{t.Fatal(e)};path:=filepath.Join(t.TempDir(),"client.json");if e=os.WriteFile(path,raw,0600);e!=nil{t.Fatal(e)}
 clientCtx,cancel:=context.WithCancel(ctx);defer cancel();client:=exec.CommandContext(clientCtx,binary,"run","-config",path);client.Stdout=io.Discard;client.Stderr=io.Discard
 if e=client.Start();e!=nil{t.Fatal(e)};defer func(){cancel();_=client.Wait()}()
 socks:=net.JoinHostPort("127.0.0.1",strconv.Itoa(int(socksPort)))
 meterWait(t,func()bool{conn,e:=net.DialTimeout("tcp4",socks,100*time.Millisecond);if e==nil{conn.Close()};return e==nil})
 fetch:=func()(string,error){transport:=&http.Transport{Proxy:nil,DisableKeepAlives:true,DialContext:func(ctx context.Context,network,address string)(net.Conn,error){return meterSOCKS(ctx,socks,address)}};defer transport.CloseIdleConnections();httpClient:=&http.Client{Transport:transport,Timeout:4*time.Second};res,e:=httpClient.Get(origin.URL);if e!=nil{return "",e};defer res.Body.Close();body,e:=io.ReadAll(io.LimitReader(res.Body,10000));return string(body),e}
 got,e:=fetch();if e!=nil||got!=proof{t.Fatalf("real VLESS/REALITY payload failed: %v",e)}
 var counters []uc.Usage
 meterWait(t,func()bool{sample,e:=uc.Collect(ctx,binary,pin,statsAddress,epoch);if e==nil&&len(sample)==1&&sample[0].Downlink>=int64(len(proof)){counters=sample;return true};return false})
 second,e:=uc.Collect(ctx,binary,pin,statsAddress,epoch);if e!=nil||second[0].Uplink<counters[0].Uplink||second[0].Downlink<counters[0].Downlink{t.Fatal("stats reset or collector failed",e)}
 if continuous{exerciseContinuousUsage(t,s,runtime,options,cfg,epoch,statsAddress,fetch,proof,c.ID);return}
 box,e:=uc.Open(uc.Options{Directory:filepath.Join(t.TempDir(),"outbox"),Key:bytes.Repeat([]byte{6},32),Epoch:epoch,Create:true});if e!=nil{t.Fatal(e)};defer box.Close()
 report,e:=box.Observe(second,false);if e!=nil{t.Fatal(e)};receipt,e:=s.UsageLedger().Accept(ctx,usageledger.Principal{NodeID:epoch.NodeID,InstallationID:epoch.InstallationID},report);if e!=nil{t.Fatal(e)}
 if e=box.Acknowledge(receipt);e!=nil{t.Fatal(e)}
 account,e:=s.ClientUsage(ctx,c.ID);if e!=nil||account.Total!=second[0].Uplink+second[0].Downlink||account.Eligible||!account.Exhausted{t.Fatalf("actual counters were not recorded: %+v %v",account,e)}
 next,e:=s.BuildNodeConfig(ctx,key,cfg.NodeID,cfg.InstallationID);if e!=nil||next.UserCount!=0||bytes.Contains(next.Candidate.Config,[]byte(c.UUID)){t.Fatal("exhausted native credential survived",e)}
 next.Candidate=seal(next.Candidate)
 if _,e=runtime.Apply(ctx,managedxray.Revision{Sequence:next.Sequence,Candidate:next.Candidate},meterSign(t,options,next.Sequence,next.Candidate));e!=nil{t.Fatal(e)}
 got,e=fetch();if e==nil&&got==proof{t.Fatal("exhausted UUID still passed application traffic")}
 kept,e:=s.Client(ctx,c.ID);if e!=nil||kept.UUID!=c.UUID{t.Fatal("quota cutoff deleted customer",e)}
 t.Logf("Pinned real Xray: captured %d uplink + %d downlink bytes; exact SQLite total %d; next native revision rejected the same UUID",second[0].Uplink,second[0].Downlink,account.Total)
}

func meterRealityTarget(t *testing.T)*httptest.Server{
 t.Helper();key,e:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);if e!=nil{t.Fatal(e)}
 cert:=&x509.Certificate{SerialNumber:big.NewInt(100),Subject:pkix.Name{CommonName:"one.example.org"},DNSNames:[]string{"one.example.org"},NotBefore:time.Now().Add(-time.Minute),NotAfter:time.Now().Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature,ExtKeyUsage:[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
 der,e:=x509.CreateCertificate(rand.Reader,cert,cert,&key.PublicKey,key);if e!=nil{t.Fatal(e)};kb,e:=x509.MarshalECPrivateKey(key);if e!=nil{t.Fatal(e)}
 pair,e:=tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der}),pem.EncodeToMemory(&pem.Block{Type:"EC PRIVATE KEY",Bytes:kb}));if e!=nil{t.Fatal(e)}
 server:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(http.StatusNoContent)}));server.Config.ErrorLog=log.New(io.Discard,"",0);server.EnableHTTP2=true
 server.TLS=&tls.Config{Certificates:[]tls.Certificate{pair},MinVersion:tls.VersionTLS13,MaxVersion:tls.VersionTLS13};server.StartTLS();t.Cleanup(server.Close);return server
}
func meterSOCKS(ctx context.Context,socks,target string)(net.Conn,error){
 c,e:=(&net.Dialer{}).DialContext(ctx,"tcp4",socks);if e!=nil{return nil,e};ok:=false;defer func(){if !ok{c.Close()}}()
 deadline:=time.Now().Add(4*time.Second);if d,yes:=ctx.Deadline();yes&&d.Before(deadline){deadline=d};_=c.SetDeadline(deadline)
 if _,e=c.Write([]byte{5,1,0});e!=nil{return nil,e};answer:=make([]byte,2);if _,e=io.ReadFull(c,answer);e!=nil{return nil,e};if !bytes.Equal(answer,[]byte{5,0}){return nil,errors.New("test SOCKS connection refused")}
 host,ps,e:=net.SplitHostPort(target);if e!=nil{return nil,e};port,e:=strconv.Atoi(ps);if e!=nil{return nil,e};ip:=net.ParseIP(host).To4();if ip==nil{return nil,ErrInvalid}
 request:=append([]byte{5,1,0,1},ip...);request=append(request,byte(port>>8),byte(port));if _,e=c.Write(request);e!=nil{return nil,e}
 header:=make([]byte,4);if _,e=io.ReadFull(c,header);e!=nil{return nil,e};if header[1]!=0{return nil,errors.New("test SOCKS connection refused")}
 size:=0;switch header[3]{case 1:size=6;case 4:size=18;case 3:b:=make([]byte,1);if _,e=io.ReadFull(c,b);e!=nil{return nil,e};size=int(b[0])+2;default:return nil,errors.New("test SOCKS connection refused")}
 if _,e=io.ReadFull(c,make([]byte,size));e!=nil{return nil,e};ok=true;return c,nil
}
