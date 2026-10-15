package api

import (
 "errors"
 "net/http"
 "strconv"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

const maxBusinessRequestBytes = 4096

// EnableBusinessLab enables an independent PRO-native, private database.
// No old HUB tables, customer keys or legacy Agent identities are consulted.
func (s *Server) EnableBusinessLab(store *business.Store) error {
 if store==nil {return errors.New("business store is required")}
 if s.business!=nil {return errors.New("business store already set")}
 s.business=store
 s.loginGate=newLoginGate()
 return nil
}
func businessError(w http.ResponseWriter,err error){
 switch{
 case errors.Is(err,business.ErrInvalid):
  respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_business_input"})
 case errors.Is(err,business.ErrConflict):
  respond(w,http.StatusConflict,map[string]string{"error":"business_event_or_identity_conflict"})
 case errors.Is(err,business.ErrInsufficient):
  respond(w,http.StatusConflict,map[string]string{"error":"insufficient_credit"})
 case errors.Is(err,business.ErrNotFound):
  respond(w,http.StatusNotFound,map[string]string{"error":"business_object_not_found"})
 default:
  respond(w,http.StatusServiceUnavailable,map[string]string{"error":"business_store_unavailable"})
 }
}
func pageParams(r *http.Request)(int,int,error){
 q:=r.URL.Query()
 for key:=range q{if key!="limit"&&key!="offset"&&key!="representative_id"{return 0,0,business.ErrInvalid}}
 limit,offset:=50,0
 if val:=q.Get("limit");val!=""{
  n,err:=strconv.Atoi(val)
  if err!=nil||n<1||n>200{return 0,0,business.ErrInvalid}
  limit=n
 }
 if val:=q.Get("offset");val!=""{
  n,err:=strconv.Atoi(val)
  if err!=nil||n<0||n>1_000_000{return 0,0,business.ErrInvalid}
  offset=n
 }
 return limit,offset,nil
}
func (s *Server) attachBusiness(mux *http.ServeMux){
 if s.business==nil{return} // never expose a public feature before it is configured
 authorized:=func(w http.ResponseWriter,r *http.Request)bool{
  if s.isOwner(r){return true}
  respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"})
  return false
 }
 mux.HandleFunc("GET /api/v1/admin/business/representatives",func(w http.ResponseWriter,r *http.Request){
  if !authorized(w,r){return}
  limit,offset,err:=pageParams(r)
  if err!=nil||r.URL.Query().Has("representative_id"){businessError(w,business.ErrInvalid);return}
  results,err:=s.business.ListRepresentatives(r.Context(),limit,offset)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{"representatives":results,"limit":limit,"offset":offset})
 })
 mux.HandleFunc("POST /api/v1/admin/business/representatives",func(w http.ResponseWriter,r *http.Request){
  if !authorized(w,r){return}
  var input struct{ ID string `json:"id"`; Name string `json:"name"` }
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  result,err:=s.business.CreateRepresentative(r.Context(),input.ID,input.Name)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusCreated,result)
 })
 mux.HandleFunc("POST /api/v1/admin/business/representatives/{id}/credits",func(w http.ResponseWriter,r *http.Request){
  if !authorized(w,r){return}
  var input business.CreditChange
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  if input.RepresentativeID!=r.PathValue("id"){
   businessError(w,business.ErrInvalid);return
  }
  result,err:=s.business.AdjustCredits(r.Context(),input)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,result)
 })
 mux.HandleFunc("GET /api/v1/admin/business/clients",func(w http.ResponseWriter,r *http.Request){
  if !authorized(w,r){return}
  limit,offset,err:=pageParams(r)
  if err!=nil{businessError(w,business.ErrInvalid);return}
  results,err:=s.business.ListClients(r.Context(),r.URL.Query().Get("representative_id"),limit,offset)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{"clients":results,"limit":limit,"offset":offset,
   "activation":"pending_real_node_and_inbound_apply"})
 })
 mux.HandleFunc("POST /api/v1/admin/business/clients",func(w http.ResponseWriter,r *http.Request){
  if !authorized(w,r){return}
  var input business.NewClient
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  result,err:=s.business.CreateClient(r.Context(),input)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusCreated,result)
 })
 mux.HandleFunc("GET /api/v1/admin/business/events",func(w http.ResponseWriter,r *http.Request){
  if !authorized(w,r){return}
  limit,offset,err:=pageParams(r)
  if err!=nil{businessError(w,business.ErrInvalid);return}
  results,err:=s.business.ListEvents(r.Context(),r.URL.Query().Get("representative_id"),limit,offset)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{"events":results,"limit":limit,"offset":offset})
 })
}
