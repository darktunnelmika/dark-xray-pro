//go:build linux

package usagecheckpoint

import("bytes";"context";"crypto/sha256";"crypto/subtle";"crypto/tls";"encoding/hex";"encoding/json";"errors";"io";"net/http";"net/url";"strings";"sync";"time";"github.com/darktunnelmika/dark-xray-pro/internal/usagewire")
var ErrTransport=errors.New("usage receipt transport unavailable or untrusted")
type SenderOptions struct{Endpoint string;NodeID string;InstallationID string;Token string;PinSHA256 string;Client *http.Client}
type Sender struct{mu sync.Mutex;client *http.Client;transport *http.Transport;url,token,node,installation string}
// Pin and normal CA verification both run BEFORE sending bearer credentials.
func NewSender(o SenderOptions)(*Sender,error){
 u,e:=url.Parse(o.Endpoint)
 if e!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||u.Path!=""&&u.Path!="/"||!idPattern.MatchString(o.NodeID)||!hexID(o.InstallationID,32)||!hexID(o.PinSHA256,64)||len(o.Token)<32||len(o.Token)>1024||strings.ContainsAny(o.Token," \r\n\t"){return nil,ErrTransport}
 client:=http.Client{};if o.Client!=nil{client=*o.Client};source:=client.Transport;if source==nil{source=http.DefaultTransport}
 transport,ok:=source.(*http.Transport);if !ok||transport.DialTLS!=nil||transport.DialTLSContext!=nil{return nil,ErrTransport}
 t:=transport.Clone();config:=&tls.Config{MinVersion:tls.VersionTLS13};if t.TLSClientConfig!=nil{config=t.TLSClientConfig.Clone()}
 if config.InsecureSkipVerify||config.MaxVersion!=0&&config.MaxVersion<tls.VersionTLS13{return nil,ErrTransport};config.MinVersion=tls.VersionTLS13
 previous:=config.VerifyConnection;pin,_:=hex.DecodeString(o.PinSHA256)
 config.VerifyConnection=func(state tls.ConnectionState)error{
  if state.Version<tls.VersionTLS13||len(state.VerifiedChains)==0||len(state.PeerCertificates)==0{return ErrTransport}
  if previous!=nil{if err:=previous(state);err!=nil{return ErrTransport}}
  sum:=sha256.Sum256(state.PeerCertificates[0].Raw);if subtle.ConstantTimeCompare(sum[:],pin)!=1{return ErrTransport};return nil
 }
 t.TLSClientConfig=config;t.TLSHandshakeTimeout=4*time.Second;t.ResponseHeaderTimeout=6*time.Second;client.Transport=t
 client.CheckRedirect=func(*http.Request,[]*http.Request)error{return ErrTransport};if client.Timeout==0||client.Timeout>8*time.Second{client.Timeout=8*time.Second}
 u.Path="/api/v1/agent/native-usage";u.RawQuery=url.Values{"node_id":{o.NodeID},"installation_id":{o.InstallationID}}.Encode()
 return &Sender{client:&client,transport:t,url:u.String(),token:o.Token,node:o.NodeID,installation:o.InstallationID},nil
}
func(s *Sender)Close(){s.transport.CloseIdleConnections()}
func(s *Sender)FlushOne(ctx context.Context,outbox *Outbox)(bool,error){
 s.mu.Lock();defer s.mu.Unlock();if outbox==nil{return false,ErrTransport}
 outbox.mu.Lock();same:=outbox.state.Epoch.NodeID==s.node&&outbox.state.Epoch.InstallationID==s.installation;outbox.mu.Unlock();if !same{return false,ErrEpoch}
 pending,err:=outbox.Pending();if err!=nil{return false,err};if len(pending)==0{return false,nil}
 raw,err:=json.Marshal(pending[0]);if err!=nil||len(raw)>MaxStatsBytes{return false,ErrInvalid}
 requestCtx,cancel:=context.WithTimeout(ctx,8*time.Second);defer cancel()
 req,err:=http.NewRequestWithContext(requestCtx,http.MethodPost,s.url,bytes.NewReader(raw));if err!=nil{return false,ErrTransport}
 req.Header.Set("Authorization","Bearer "+s.token);req.Header.Set("Content-Type","application/json");req.Header.Set("Cache-Control","no-store")
 res,err:=s.client.Do(req);if err!=nil{return false,ErrTransport};defer res.Body.Close();if res.StatusCode!=http.StatusOK{return false,ErrTransport}
 raw,err=io.ReadAll(io.LimitReader(res.Body,4097));if err!=nil||len(raw)>4096{return false,ErrTransport}
 var receipt Receipt;if usagewire.Decode(raw,&receipt)!=nil{return false,ErrReceipt};expected:=pending[0]
 if receipt.EpochID!=expected.EpochID||receipt.Sequence!=expected.Sequence||receipt.SHA!=expected.SHA{return false,ErrReceipt}
 if err=outbox.Acknowledge(receipt);err!=nil{return false,err};return true,nil
}
