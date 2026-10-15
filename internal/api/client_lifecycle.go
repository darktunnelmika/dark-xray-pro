package api

import (
 "net/http"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// New PRO client lifecycle is a durable billing operation ONLY; no route,
// Node runtime, Xray process or subscription link is ever mutated here.
func(s *Server) attachClientLifecycle(mux *http.ServeMux){
 if s.business==nil{return}
 ownedClient:=func(w http.ResponseWriter,r *http.Request,mutate bool)(business.Client,bool){
  session,ok:=s.authorizeBrowser(w,r,mutate,false)
  if !ok{return business.Client{},false}
  c,err:=s.business.Client(r.Context(),r.PathValue("id"))
  if err!=nil{businessError(w,err);return business.Client{},false}
  if session.User.Role!="owner" && c.RepresentativeID!=session.User.RepresentativeID{
   respond(w,http.StatusForbidden,map[string]string{"error":"representative_scope_denied"})
   return business.Client{},false
  }
  return c,true
 }
 mux.HandleFunc("GET /api/v1/workspace/clients/{id}",func(w http.ResponseWriter,r *http.Request){
  c,ok:=ownedClient(w,r,false);if !ok{return}
  respond(w,http.StatusOK,c)
 })
 mux.HandleFunc("POST /api/v1/workspace/clients/{id}/extend",func(w http.ResponseWriter,r *http.Request){
  c,ok:=ownedClient(w,r,true);if !ok{return}
  var input business.ExtendClient
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  if c.ID!=input.ClientID{businessError(w,business.ErrInvalid);return}
  result,err:=s.business.ExtendClient(r.Context(),input)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{
   "client":result,"deployment":"pending_external_node_apply",
   "subscription_changed":false,"customer_traffic_changed":false,
  })
 })
 mux.HandleFunc("POST /api/v1/workspace/clients/{id}/state",func(w http.ResponseWriter,r *http.Request){
  c,ok:=ownedClient(w,r,true);if !ok{return}
  var input business.SetClientState
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  if c.ID!=input.ClientID{businessError(w,business.ErrInvalid);return}
  result,err:=s.business.SetClientState(r.Context(),input)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{
   "client":result,"applied_to_xray":false,
   "billing_refunded":false,"group_history_deleted":false,
  })
 })
 mux.HandleFunc("GET /api/v1/workspace/client-operations",func(w http.ResponseWriter,r *http.Request){
  session,ok:=s.authorizeBrowser(w,r,false,false);if !ok{return}
  limit,offset,err:=pageParams(r)
  if err!=nil{businessError(w,business.ErrInvalid);return}
  rep:=r.URL.Query().Get("representative_id")
  if session.User.Role!="owner"{
   if rep!=""&&rep!=session.User.RepresentativeID{
    respond(w,http.StatusForbidden,map[string]string{"error":"representative_scope_denied"});return
   }
   rep=session.User.RepresentativeID
  }
  events,err:=s.business.ListClientOperations(r.Context(),rep,limit,offset)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{
   "operations":events,"limit":limit,"offset":offset,
   "audit":"append_only","customer_data_plane":"not_enabled",
  })
 })
}
