//go:build linux

package managedxray

import (
 "context"
 "crypto/tls"
 "encoding/base64"
 "encoding/json"
 "errors"
 "io"
 "net/http"
 "net/url"
 "strings"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)
// Delivery contains secrets and is confined to the authenticated Agent TLS route.
type Delivery struct{Version int `json:"version"`;NodeID string `json:"node_id"`;InstallationID string `json:"installation_id"`;ListenIP string `json:"listen_ip"`;Sequence uint64 `json:"sequence"`;Candidate xrayconfig.Candidate `json:"candidate"`;ConfigBase64 string `json:"config_base64"`;Lease Lease `json:"lease"`}
func(r *Runtime)Sync(ctx context.Context,client *http.Client,endpoint string)(Snapshot,error){
 u,err:=url.Parse(endpoint);if err!=nil||u.Scheme!="https"||u.Host==""||u.User!=nil||u.RawQuery!=""||u.Fragment!=""||client==nil{return r.Snapshot(),ErrLease}
 q:=url.Values{"node_id":{r.options.NodeID},"installation_id":{r.options.InstallationID}}
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,strings.TrimRight(endpoint,"/")+"/api/v1/agent/native-runtime?"+q.Encode(),nil);if err!=nil{return r.Snapshot(),ErrLease}
 req.Header.Set("Authorization","Bearer "+string(r.options.LeaseKey));secure:=*client;secure.CheckRedirect=func(*http.Request,[]*http.Request)error{return errors.New("runtime redirect refused")}
 res,err:=secure.Do(req);if err!=nil{return r.Snapshot(),errors.New("native runtime transport unavailable")};defer res.Body.Close()
 if res.TLS==nil||res.TLS.Version<tls.VersionTLS13||len(res.TLS.VerifiedChains)==0{return r.Snapshot(),ErrLease}
 if res.StatusCode==http.StatusNoContent{return r.Snapshot(),nil};if res.StatusCode!=http.StatusOK{return r.Snapshot(),errors.New("native runtime delivery refused")}
 var d Delivery;dec:=json.NewDecoder(io.LimitReader(res.Body,2*xrayconfig.MaxCandidateBytes+65536));dec.DisallowUnknownFields();if dec.Decode(&d)!=nil{return r.Snapshot(),ErrInvalid}
 var trailing any;if !errors.Is(dec.Decode(&trailing),io.EOF){return r.Snapshot(),ErrInvalid}
 if d.Version!=1||d.NodeID!=r.options.NodeID||d.InstallationID!=r.options.InstallationID||d.ListenIP!=r.options.ListenIP{return r.Snapshot(),ErrLease}
 raw,err:=base64.StdEncoding.Strict().DecodeString(d.ConfigBase64);if err!=nil||len(raw)>xrayconfig.MaxCandidateBytes{return r.Snapshot(),ErrInvalid}
 d.Candidate.Config=raw;rev:=Revision{Sequence:d.Sequence,Candidate:d.Candidate};state:=r.Snapshot()
 if state.ProcessReady&&state.ActiveSequence==rev.Sequence&&state.ActiveSHA==rev.Candidate.SHA256{return r.Refresh(d.Lease)}
 if !state.ProcessReady&&state.SavedSequence==rev.Sequence&&state.SavedSHA==rev.Candidate.SHA256{return r.Recover(ctx,d.Lease)}
 return r.Apply(ctx,rev,d.Lease)
}
