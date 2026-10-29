package api

import (
 "net/http"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// The authenticated workspace is separate from the legacy-style localhost
// Bearer admin API. Representatives are ALWAYS scoped server-side to their
// own clients, balances and immutable events, never to browser input alone.
func(s *Server) attachWorkspace(mux *http.ServeMux){
 if s.business==nil{return}

 mux.HandleFunc("GET /api/v1/workspace/me",func(w http.ResponseWriter,r *http.Request){
  session,ok:=s.authorizeBrowser(w,r,false,false);if !ok{return}
  rep,err:=s.business.Representative(r.Context(),session.User.RepresentativeID)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{
   "user":session.User,"balance":rep,
   "data_plane":"not_activated",
   "capabilities":map[string]bool{"seller_orders_v1":s.sellerOrdersV1Enabled,"backup_control_v1":s.backupControl!=nil&&session.User.Role=="owner"},
  })
 })
 mux.HandleFunc("GET /api/v1/workspace/representatives",func(w http.ResponseWriter,r *http.Request){
  _,ok:=s.authorizeBrowser(w,r,false,true);if !ok{return}
  opts,err:=representativeSearchParams(r)
  if err!=nil{businessError(w,business.ErrInvalid);return}
  page,err:=s.business.SearchRepresentatives(r.Context(),opts)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{
   "representatives":page.Representatives,"total":page.Total,
   "limit":page.Limit,"offset":page.Offset,"has_more":page.HasMore,
   "q":opts.Query,"state":opts.State,"sort":opts.Sort,
  })
 })
 mux.HandleFunc("POST /api/v1/workspace/representatives",func(w http.ResponseWriter,r *http.Request){
  _,ok:=s.authorizeBrowser(w,r,true,true);if !ok{return}
  var input struct{ID string `json:"id"`; Name string `json:"name"`}
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  result,err:=s.business.CreateRepresentative(r.Context(),input.ID,input.Name)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusCreated,result)
 })
 mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/login",func(w http.ResponseWriter,r *http.Request){
  _,ok:=s.authorizeBrowser(w,r,true,true);if !ok{return}
  var input struct{
   Username string `json:"username"`
   Password string `json:"password"`
  }
  if err:=decode(w,r,1024,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  result,err:=s.business.ProvisionRepresentativeLogin(r.Context(),r.PathValue("id"),input.Username,input.Password)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusCreated,result)
 })
 mux.HandleFunc("POST /api/v1/workspace/representatives/{id}/credits",func(w http.ResponseWriter,r *http.Request){
  _,ok:=s.authorizeBrowser(w,r,true,true);if !ok{return}
  var input business.CreditChange
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  if r.PathValue("id")!=input.RepresentativeID{
   businessError(w,business.ErrInvalid);return
  }
  result,err:=s.business.AdjustCredits(r.Context(),input)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,result)
 })
 mux.HandleFunc("GET /api/v1/workspace/clients",func(w http.ResponseWriter,r *http.Request){
  session,ok:=s.authorizeBrowser(w,r,false,false);if !ok{return}
  options,err:=clientSearchParams(r)
  if err!=nil{businessError(w,business.ErrInvalid);return}
  if session.User.Role=="representative"{
   if options.RepresentativeID!=""&&options.RepresentativeID!=session.User.RepresentativeID{
    respond(w,http.StatusForbidden,map[string]string{"error":"representative_scope_denied"});return
   }
   options.RepresentativeID=session.User.RepresentativeID
  }
  page,err:=s.business.SearchClients(r.Context(),options)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{
   "clients":page.Clients,"limit":page.Limit,"offset":page.Offset,
   "total":page.Total,"has_more":page.HasMore,
   "q":options.Query,"state":options.State,"plan":options.Plan,
   "sort":options.Sort,"representative_id":options.RepresentativeID,
   "activation":"pending_real_node_and_inbound_apply",
  })
 })
 mux.HandleFunc("POST /api/v1/workspace/clients",func(w http.ResponseWriter,r *http.Request){
  session,ok:=s.authorizeBrowser(w,r,true,false);if !ok{return}
  var input business.NewClient
  if err:=decode(w,r,maxBusinessRequestBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  if session.User.Role=="representative" && input.RepresentativeID!=session.User.RepresentativeID{
   respond(w,http.StatusForbidden,map[string]string{"error":"representative_scope_denied"});return
  }
  result,err:=s.business.CreateClient(r.Context(),input)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusCreated,result)
 })
 mux.HandleFunc("GET /api/v1/workspace/events",func(w http.ResponseWriter,r *http.Request){
  session,ok:=s.authorizeBrowser(w,r,false,false);if !ok{return}
  limit,offset,err:=pageParams(r)
  if err!=nil{businessError(w,business.ErrInvalid);return}
  repID:=r.URL.Query().Get("representative_id")
  if session.User.Role=="representative"{
   if repID!=""&&repID!=session.User.RepresentativeID{
    respond(w,http.StatusForbidden,map[string]string{"error":"representative_scope_denied"});return
   }
   repID=session.User.RepresentativeID
  }
  rows,err:=s.business.ListEvents(r.Context(),repID,limit,offset)
  if err!=nil{businessError(w,err);return}
  respond(w,http.StatusOK,map[string]any{"events":rows,"limit":limit,"offset":offset})
 })
}
