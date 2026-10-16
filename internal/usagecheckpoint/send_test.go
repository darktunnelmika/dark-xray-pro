//go:build linux

package usagecheckpoint

import (
 "context"
 "crypto/sha256"
 "crypto/tls"
 "encoding/hex"
 "encoding/json"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"
)
func TestPinnedSenderNeverSendsCredentialsBeforeCertificatePinCheck(t *testing.T){
 box,options:=testBox(t);if _,e:=box.Observe(values(2),false);e!=nil{t.Fatal(e)}
 var calls atomic.Int64
 server:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){calls.Add(1);w.WriteHeader(500)}));server.TLS=&tls.Config{MinVersion:tls.VersionTLS13};server.StartTLS();defer server.Close()
 sender,e:=NewSender(SenderOptions{Endpoint:server.URL,NodeID:options.Epoch.NodeID,InstallationID:options.Epoch.InstallationID,Token:strings.Repeat("t",64),PinSHA256:strings.Repeat("a",64),Client:server.Client()});if e!=nil{t.Fatal(e)};defer sender.Close()
 if _,e=sender.FlushOne(context.Background(),box);e==nil{t.Fatal("wrong pin accepted")}
 if calls.Load()!=0{t.Fatal("bearer request escaped before pin validation")};q,e:=box.Pending();if e!=nil||len(q)!=1{t.Fatal("pin failure discarded pending report")}
 unsafe:=&http.Client{Transport:&http.Transport{TLSClientConfig:&tls.Config{InsecureSkipVerify:true}}}
 if _,e=NewSender(SenderOptions{Endpoint:server.URL,NodeID:options.Epoch.NodeID,InstallationID:options.Epoch.InstallationID,Token:strings.Repeat("t",64),PinSHA256:strings.Repeat("a",64),Client:unsafe});e==nil{t.Fatal("insecure CA bypass accepted")}
}
func TestSenderRejectsRedirectMalformedAndWrongReceiptsWithoutDroppingData(t *testing.T){
 for _,mode:=range []string{"redirect","wrong","duplicate","oversized","valid"}{t.Run(mode,func(t *testing.T){
  box,o:=testBox(t);report,e:=box.Observe(values(3),false);if e!=nil{t.Fatal(e)}
  server:=httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){
   if r.Header.Get("Authorization")!="Bearer "+strings.Repeat("t",64){t.Error("wrong private auth")}
   switch mode{
   case "redirect":w.Header().Set("Location","https://invalid.example/");w.WriteHeader(302)
   case "wrong":json.NewEncoder(w).Encode(Receipt{EpochID:report.EpochID,Sequence:report.Sequence+1,SHA:report.SHA})
   case "duplicate":w.Write([]byte(`{"epoch_id":"x","epoch_id":"y","sequence":"1","sha256":"x"}`))
   case "oversized":w.Write([]byte(strings.Repeat("x",5000)))
   default:json.NewEncoder(w).Encode(receipt(report))
   }
  }));server.TLS=&tls.Config{MinVersion:tls.VersionTLS13};server.StartTLS();defer server.Close()
  hash:=sha256.Sum256(server.Certificate().Raw);sender,e:=NewSender(SenderOptions{Endpoint:server.URL,NodeID:o.Epoch.NodeID,InstallationID:o.Epoch.InstallationID,Token:strings.Repeat("t",64),PinSHA256:hex.EncodeToString(hash[:]),Client:server.Client()});if e!=nil{t.Fatal(e)};defer sender.Close()
  sent,e:=sender.FlushOne(context.Background(),box);if mode=="valid"{if e!=nil||!sent{t.Fatal("valid committed receipt rejected",e)}}else if e==nil||sent{t.Fatal("untrusted receipt accepted")}
  q,e:=box.Pending();if e!=nil{t.Fatal(e)};want:=1;if mode=="valid"{want=0};if len(q)!=want{t.Fatal("unexpected pending queue length")}
 })}
}
func TestStatsCLIReadOnlyAndLoopbackEndpoint(t *testing.T){
 args,e:=StatsArguments("127.0.0.1:19880");if e!=nil||!strings.Contains(strings.Join(args," "),"-reset=false"){t.Fatal("collector can reset runtime counters")}
 for _,endpoint:=range []string{"localhost:19880","0.0.0.0:19880","127.0.0.1:80","127.0.0.1:019880","example.org:19880","127.0.0.1:65536"}{if _,e:=StatsArguments(endpoint);e==nil{t.Fatalf("unexpected stats endpoint accepted: %s",endpoint)}}
}
