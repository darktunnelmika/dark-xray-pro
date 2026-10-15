//go:build linux

package managedxray

import (
 "bytes"
 "context"
 "crypto/ecdh"
 "crypto/ecdsa"
 "crypto/elliptic"
 "crypto/rand"
 "crypto/sha256"
 "crypto/tls"
 "crypto/x509"
 "crypto/x509/pkix"
 "encoding/base64"
 "encoding/hex"
 "encoding/json"
 "encoding/pem"
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
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
func realityTestTarget(t *testing.T)*httptest.Server{
 t.Helper();key,e:=ecdsa.GenerateKey(elliptic.P256(),rand.Reader);if e!=nil{t.Fatal(e)}
 cert:=&x509.Certificate{SerialNumber:big.NewInt(100),Subject:pkix.Name{CommonName:"one.example.org"},DNSNames:[]string{"one.example.org"},NotBefore:time.Now().Add(-time.Minute),NotAfter:time.Now().Add(time.Hour),KeyUsage:x509.KeyUsageDigitalSignature,ExtKeyUsage:[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
 der,e:=x509.CreateCertificate(rand.Reader,cert,cert,&key.PublicKey,key);if e!=nil{t.Fatal(e)};kb,e:=x509.MarshalECPrivateKey(key);if e!=nil{t.Fatal(e)}
 pair,e:=tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:der}),pem.EncodeToMemory(&pem.Block{Type:"EC PRIVATE KEY",Bytes:kb}));if e!=nil{t.Fatal(e)}
 server:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){w.WriteHeader(http.StatusNoContent)}));server.Config.ErrorLog=log.New(io.Discard,"",0);server.EnableHTTP2=true
 server.TLS=&tls.Config{Certificates:[]tls.Certificate{pair},MinVersion:tls.VersionTLS13,MaxVersion:tls.VersionTLS13};server.StartTLS();t.Cleanup(server.Close);return server
}
func socksDial(ctx context.Context,socks,target string)(net.Conn,error){
 c,e:=(&net.Dialer{}).DialContext(ctx,"tcp4",socks);if e!=nil{return nil,e};ok:=false;defer func(){if !ok{c.Close()}}()
 deadline:=time.Now().Add(4*time.Second);if d,yes:=ctx.Deadline();yes&&d.Before(deadline){deadline=d};_=c.SetDeadline(deadline)
 if _,e=c.Write([]byte{5,1,0});e!=nil{return nil,e};answer:=make([]byte,2);if _,e=io.ReadFull(c,answer);e!=nil{return nil,e};if !bytes.Equal(answer,[]byte{5,0}){return nil,ErrRuntime}
 host,ps,e:=net.SplitHostPort(target);if e!=nil{return nil,e};port,e:=strconv.Atoi(ps);if e!=nil{return nil,e};ip:=net.ParseIP(host).To4();if ip==nil{return nil,ErrInvalid}
 request:=append([]byte{5,1,0,1},ip...);request=append(request,byte(port>>8),byte(port));if _,e=c.Write(request);e!=nil{return nil,e}
 header:=make([]byte,4);if _,e=io.ReadFull(c,header);e!=nil{return nil,e};if header[1]!=0{return nil,ErrRuntime}
 size:=0;switch header[3]{case 1:size=6;case 4:size=18;case 3:b:=make([]byte,1);if _,e=io.ReadFull(c,b);e!=nil{return nil,e};size=int(b[0])+2;default:return nil,ErrRuntime}
 if _,e=io.ReadFull(c,make([]byte,size));e!=nil{return nil,e};ok=true;return c,nil
}
func TestRealPinnedRealityClientRevokeAndRollback(t *testing.T){
 binary,pin:=os.Getenv("DARK_PRO_TEST_XRAY_BINARY"),os.Getenv("DARK_PRO_TEST_XRAY_SHA256");if binary==""&&pin==""{t.Skip("explicit pinned real Xray required")};if binary==""||pin==""{t.Fatal("incomplete pinned core")}
 o:=testOptions(t);o.Binary=binary;o.BinarySHA=pin;r:=mustOpen(t,o);target:=realityTestTarget(t)
 random:=make([]byte,32);if _,e:=rand.Read(random);e!=nil{t.Fatal(e)};proof:=hex.EncodeToString(random)
 origin:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){_,_=io.WriteString(w,proof)}));defer origin.Close()
 serverPort:=freePort(t);c1:=testCandidate(t,serverPort,"one");var doc map[string]any;if e:=json.Unmarshal(c1.Config,&doc);e!=nil{t.Fatal(e)}
 in:=doc["inbounds"].([]any)[0].(map[string]any);reality:=in["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)
 // TEST ONLY: use a disposable local TLS target instead of third-party traffic.
 reality["dest"]=target.Listener.Addr().String()
 // TEST ONLY: allow this exact disposable echo port. Production configs do
 // not receive a loopback/private-network bypass in their freedom outbound.
 originPort:=origin.Listener.Addr().(*net.TCPAddr).Port
 doc["outbounds"].([]any)[0].(map[string]any)["settings"]=map[string]any{"finalRules":[]any{map[string]any{"action":"allow","network":"tcp","ip":[]string{"127.0.0.1/32"},"port":strconv.Itoa(originPort)}}}
 seal:=func(doc map[string]any,origin xrayconfig.Candidate)xrayconfig.Candidate{
  raw,e:=json.Marshal(doc);if e!=nil{t.Fatal(e)};digest:=sha256.Sum256(raw);origin.Config=raw;origin.SHA256=hex.EncodeToString(digest[:]);m:=sha256.Sum256([]byte("isolated-real-client-test:"+origin.SHA256));origin.ManifestSHA256=hex.EncodeToString(m[:]);return origin
 }
 c1=seal(doc,c1);if _,e:=r.Apply(context.Background(),Revision{1,c1},grant(t,o,1,c1,40*time.Second));e!=nil{t.Fatal(e)}
 private,e:=ecdh.X25519().NewPrivateKey(bytes.Repeat([]byte{0x12},32));if e!=nil{t.Fatal(e)};socksPort:=freePort(t)
 clientDoc:=map[string]any{
  "log":map[string]string{"loglevel":"none"},
  "inbounds":[]any{map[string]any{"listen":"127.0.0.1","port":socksPort,"protocol":"socks","settings":map[string]any{"auth":"noauth","udp":false}}},
  "outbounds":[]any{map[string]any{"protocol":"vless","settings":map[string]any{"vnext":[]any{map[string]any{"address":"127.0.0.1","port":serverPort,"users":[]any{map[string]any{"id":"af469f46-e18a-45f4-bf57-c7f649b10db5","encryption":"none","flow":"xtls-rprx-vision"}}}}},"streamSettings":map[string]any{"network":"tcp","security":"reality","realitySettings":map[string]any{"serverName":"one.example.org","fingerprint":"chrome","publicKey":base64.RawURLEncoding.EncodeToString(private.PublicKey().Bytes()),"shortId":"aa11"}}}},
 }
 raw,e:=json.Marshal(clientDoc);if e!=nil{t.Fatal(e)};file:=filepath.Join(t.TempDir(),"private-test-client.json");if e=os.WriteFile(file,raw,0600);e!=nil{t.Fatal(e)}
 clientCtx,cancel:=context.WithCancel(context.Background());defer cancel();process:=exec.CommandContext(clientCtx,binary,"run","-config",file);process.Stdout=io.Discard;process.Stderr=io.Discard
 if e=process.Start();e!=nil{t.Fatal(e)};t.Cleanup(func(){cancel();_=process.Wait()})
 socks:=net.JoinHostPort("127.0.0.1",strconv.Itoa(int(socksPort)));waitFor(t,func()bool{conn,e:=net.DialTimeout("tcp4",socks,100*time.Millisecond);if e==nil{conn.Close()};return e==nil})
 fetch:=func()(string,error){
  transport:=&http.Transport{Proxy:nil,DisableKeepAlives:true,DialContext:func(ctx context.Context,network,address string)(net.Conn,error){return socksDial(ctx,socks,address)}};defer transport.CloseIdleConnections()
  c:=&http.Client{Transport:transport,Timeout:5*time.Second};res,e:=c.Get(origin.URL+"/authenticated-proof");if e!=nil{return "",e};defer res.Body.Close();b,e:=io.ReadAll(io.LimitReader(res.Body,128));return string(b),e
 }
 got,e:=fetch();if e!=nil||got!=proof{t.Fatalf("actual VLESS/REALITY application payload failed: %v",e)}
 in["settings"].(map[string]any)["clients"]=[]any{};c2:=seal(doc,c1);if _,e=r.Apply(context.Background(),Revision{2,c2},grant(t,o,2,c2,40*time.Second));e!=nil{t.Fatal(e)}
 if got,e=fetch();e==nil&&got==proof{t.Fatal("removed UUID still carried real application traffic")}
 if _,e=r.Rollback(context.Background(),3,grant(t,o,3,c1,40*time.Second));e!=nil{t.Fatal(e)};got,e=fetch();if e!=nil||!strings.EqualFold(got,proof){t.Fatalf("rollback did not restore authenticated traffic: %v",e)}
}
