package api

import (
 "crypto/tls"
 "encoding/base64"
 "errors"
 "net/http"
 "strconv"
 "time"
 "github.com/darktunnelmika/dark-xray-pro/internal/business"
 "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)
func(s *Server)EnableNativeInbounds(key []byte)error{
 if s.business==nil||len(key)!=32||len(s.inboundKey)!=0{return errors.New("independent business database and native inbound encryption key required")};s.inboundKey=append([]byte(nil),key...);return nil
}
func(s *Server)nativeNode(id string)(string,bool){for _,n:=range s.registry.List(){if n.NodeID==id{return n.InstallationID,true}};return "",false}
func(s *Server)attachNativeInbounds(mux *http.ServeMux){
 if len(s.inboundKey)!=32||s.business==nil{return}
 mux.HandleFunc("GET /api/v1/workspace/native-inbounds",func(w http.ResponseWriter,r *http.Request){
  if _,ok:=s.authorizeBrowser(w,r,false,true);!ok{return};rows,err:=s.business.ListInbounds(r.Context(),s.inboundKey);if err!=nil{businessError(w,err);return};respond(w,200,map[string]any{"inbounds":rows,"nodes":s.registry.List(),"subscription_publication":false})
 })
 mux.HandleFunc("POST /api/v1/workspace/native-inbounds",func(w http.ResponseWriter,r *http.Request){
  if _,ok:=s.authorizeBrowser(w,r,true,true);!ok{return};var input business.InboundInput;if decode(w,r,8192,&input)!=nil{businessError(w,business.ErrInvalid);return};in,err:=s.business.CreateInbound(r.Context(),s.inboundKey,input);if err!=nil{businessError(w,err);return};respond(w,201,in)
 })
 mux.HandleFunc("POST /api/v1/workspace/native-inbounds/{id}/clients",func(w http.ResponseWriter,r *http.Request){
  if _,ok:=s.authorizeBrowser(w,r,true,true);!ok{return};id,err:=strconv.ParseUint(r.PathValue("id"),10,53);var input struct{ClientIDs []string `json:"client_ids"`;Attach *bool `json:"attach"`}
  if err!=nil||decode(w,r,65536,&input)!=nil||input.Attach==nil{businessError(w,business.ErrInvalid);return};if err=s.business.BindInboundClients(r.Context(),id,input.ClientIDs,*input.Attach);err!=nil{businessError(w,err);return};respond(w,200,map[string]bool{"bindings_saved":true,"clients_deleted":false,"subscription_published":false})
 })
 mux.HandleFunc("POST /api/v1/workspace/native-inbounds/{id}/target",func(w http.ResponseWriter,r *http.Request){
  if _,ok:=s.authorizeBrowser(w,r,true,true);!ok{return};id,err:=strconv.ParseUint(r.PathValue("id"),10,53);var target business.InboundTarget
  if err!=nil||decode(w,r,8192,&target)!=nil||target.InboundID!=id{businessError(w,business.ErrInvalid);return};installation,ok:=s.nativeNode(target.NodeID);if !ok||installation!=target.InstallationID{businessError(w,business.ErrConflict);return}
  for _,port:=range []uint16{target.DirectPort,target.SwapPort}{if port==2087||port==8443||port==24443{businessError(w,business.ErrConflict);return}}
  if err=s.business.PutInboundTarget(r.Context(),target);err!=nil{businessError(w,err);return};respond(w,200,map[string]bool{"target_saved":true,"subscription_published":false})
 })
 mux.HandleFunc("POST /api/v1/workspace/native-nodes/{id}/preview",func(w http.ResponseWriter,r *http.Request){
  if _,ok:=s.authorizeBrowser(w,r,true,true);!ok{return};node:=r.PathValue("id");installation,ok:=s.nativeNode(node);if !ok{businessError(w,business.ErrNotFound);return};cfg,err:=s.business.BuildNodeConfig(r.Context(),s.inboundKey,node,installation);if err!=nil{businessError(w,err);return}
  respond(w,200,cfg)
 })
 mux.HandleFunc("GET /api/v1/agent/native-runtime",func(w http.ResponseWriter,r *http.Request){
  if r.TLS==nil||r.TLS.Version<tls.VersionTLS13{respond(w,426,map[string]string{"error":"tls13_required"});return};node,installation:=r.URL.Query().Get("node_id"),r.URL.Query().Get("installation_id");token:=bearer(r)
  if _,ok:=s.registry.Authenticate(node,installation,token);!ok{respond(w,401,map[string]string{"error":"unauthorized"});return}
  cfg,err:=s.business.BuildNodeConfig(r.Context(),s.inboundKey,node,installation);if errors.Is(err,business.ErrNotFound){w.WriteHeader(204);return};if err!=nil{businessError(w,err);return}
  lease,err:=managedxray.SignLease([]byte(token),managedxray.Lease{NodeID:node,InstallationID:installation,Sequence:cfg.Sequence,ConfigSHA:cfg.ConfigSHA,ManifestSHA:cfg.ManifestSHA,Issued:time.Now().UnixNano(),Expires:cfg.LeaseNotAfter});if err!=nil{businessError(w,business.ErrUnavailable);return}
  if err=s.business.RecordNativeDelivery(r.Context(),cfg,lease);err!=nil{businessError(w,err);return}
  respond(w,200,managedxray.Delivery{Version:1,NodeID:node,InstallationID:installation,ListenIP:cfg.ListenIP,Sequence:cfg.Sequence,Candidate:cfg.Candidate,ConfigBase64:base64.StdEncoding.EncodeToString(cfg.Candidate.Config),Lease:lease})
 })
}
