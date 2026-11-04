package api

import (
 "net/http"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// Every Client Group route requires a verified browser session; the Agent
// handler, private Bearer-only admin routes and public unpaired callers never
// receive tenant-managed membership operations.
func(s *Server) attachGroups(mux *http.ServeMux){
 if s.business==nil{return}
 scope:=func(w http.ResponseWriter,r *http.Request,mutate bool)(business.LoginSession,bool){
  return s.authorizeBrowser(w,r,mutate,false)
 }
 allowedGroup:=func(w http.ResponseWriter,r *http.Request,session business.LoginSession)(business.Group,bool){
  g,err:=s.business.Group(r.Context(),r.PathValue("id"))
  if err!=nil{businessError(w,err);return business.Group{},false}
  if session.User.Role!="owner"&&g.RepresentativeID!=session.User.RepresentativeID{
   respond(w,http.StatusForbidden,map[string]string{"error":"representative_scope_denied"})
   return business.Group{},false
  }
  return g,true
 }
 mux.HandleFunc("GET /api/v1/workspace/groups",func(w http.ResponseWriter,r *http.Request){
  session,ok:=scope(w,r,false);if !ok{return}
  limit,offset,err:=pageParams(r)
  if err!=nil{businessError(w,business.ErrInvalid);return}
  rep:=r.URL.Query().Get("representative_id")
  if session.User.Role!="owner"{
   if rep!=""&&rep!=session.User.RepresentativeID{
    respond(w,http.StatusForbidden,map[string]string{"error":"representative_scope_denied"});return
   }
   rep=session.User.RepresentativeID
  }
  groups,err:=s.business.ListGroups(r.Context(),rep,limit,offset)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{"groups":groups,"limit":limit,"offset":offset,"billable_usage_scope":"separate_ledger"})
 })
 mux.HandleFunc("POST /api/v1/workspace/groups",func(w http.ResponseWriter,r *http.Request){
  session,ok:=scope(w,r,true);if !ok{return}
  var input business.NewGroup
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  if session.User.Role!="owner"&&input.RepresentativeID!=session.User.RepresentativeID{
   respond(w,http.StatusForbidden,map[string]string{"error":"representative_scope_denied"});return
  }
  g,err:=s.business.CreateGroup(r.Context(),input)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusCreated,g)
 })
 mux.HandleFunc("PATCH /api/v1/workspace/groups/{id}",func(w http.ResponseWriter,r *http.Request){
  session,ok:=scope(w,r,true);if !ok{return}
  g,ok:=allowedGroup(w,r,session);if !ok{return}
  var input struct{Name string `json:"name"`;Color string `json:"color"`}
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  next,err:=s.business.UpdateGroup(r.Context(),g.ID,g.RepresentativeID,input.Name,input.Color)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,next)
 })
 mux.HandleFunc("DELETE /api/v1/workspace/groups/{id}",func(w http.ResponseWriter,r *http.Request){
  session,ok:=scope(w,r,true);if !ok{return}
  g,ok:=allowedGroup(w,r,session);if !ok{return}
  count,err:=s.business.DeleteGroup(r.Context(),g.ID,g.RepresentativeID)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{
   "group_deleted":true,"clients_deleted":false,"ledger_modified":false,
   "detached_member_count":count,
  })
 })
 mux.HandleFunc("GET /api/v1/workspace/groups/{id}/members",func(w http.ResponseWriter,r *http.Request){
  session,ok:=scope(w,r,false);if !ok{return}
  g,ok:=allowedGroup(w,r,session);if !ok{return}
  limit,offset,err:=pageParams(r)
  if err!=nil||r.URL.Query().Has("representative_id"){
   businessError(w,business.ErrInvalid);return
  }
  ids,err:=s.business.ListGroupMembers(r.Context(),g.ID,g.RepresentativeID,limit,offset)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{"client_ids":ids,"limit":limit,"offset":offset})
 })
 mux.HandleFunc("POST /api/v1/workspace/groups/{id}/members",func(w http.ResponseWriter,r *http.Request){
  session,ok:=scope(w,r,true);if !ok{return}
  g,ok:=allowedGroup(w,r,session);if !ok{return}
  var input business.AssignGroupMembers
  if err:=decode(w,r,65536,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  next,err:=s.business.AssignGroupMembers(r.Context(),g.ID,g.RepresentativeID,input.ClientIDs)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,next)
 })
 mux.HandleFunc("DELETE /api/v1/workspace/groups/{id}/members/{client}",func(w http.ResponseWriter,r *http.Request){
  session,ok:=scope(w,r,true);if !ok{return}
  g,ok:=allowedGroup(w,r,session);if !ok{return}
  if err:=s.business.RemoveGroupMember(r.Context(),g.ID,g.RepresentativeID,r.PathValue("client"));err!=nil{
   businessError(w,err);return
  }
  respond(w,http.StatusOK,map[string]bool{"member_removed":true,"client_deleted":false})
 })
}
