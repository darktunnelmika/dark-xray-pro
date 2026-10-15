package api

import (
 "net/http"
 "strconv"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func(s *Server)attachInboundLifecycle(mux *http.ServeMux){
 if s.business==nil||len(s.inboundKey)!=32{return}
 mux.HandleFunc("POST /api/v1/workspace/native-inbounds/{id}/lifecycle",func(w http.ResponseWriter,r *http.Request){
  if _,ok:=s.authorizeBrowser(w,r,true,true);!ok{return}
  id,err:=strconv.ParseUint(r.PathValue("id"),10,53)
  var input business.InboundMutation
  if err!=nil||decode(w,r,8192,&input)!=nil{businessError(w,business.ErrInvalid);return}
  in,err:=s.business.MutateInbound(r.Context(),s.inboundKey,id,input)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{"inbound":in,"awaiting_agent_report":true,
   "clients_deleted":false,"ledger_modified":false,"subscription_published":false})
 })
 mux.HandleFunc("GET /api/v1/workspace/native-inbounds/{id}/operations",func(w http.ResponseWriter,r *http.Request){
  if _,ok:=s.authorizeBrowser(w,r,false,true);!ok{return}
  id,err:=strconv.ParseUint(r.PathValue("id"),10,53)
  limit,offset,pageErr:=pageParams(r)
  if err!=nil||pageErr!=nil||r.URL.Query().Has("representative_id"){businessError(w,business.ErrInvalid);return}
  if _,err=s.business.NativeInbound(r.Context(),s.inboundKey,id);err!=nil{businessError(w,err);return}
  events,err:=s.business.InboundOperations(r.Context(),id,limit,offset)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{"operations":events,"limit":limit,"offset":offset})
 })
}
