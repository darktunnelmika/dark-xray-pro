package api

import (
 "net/http"

 "github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// Owners can inspect finance inside the authenticated private workspace.
// This is a prerequisite for a MAIN-HUB Telegram bot, NOT a public webhook.
// The Telegram bot must separately prove owner sender identity and route via
// a trusted internal service; callback or user-supplied rep IDs grant nothing.
func (s *Server) attachOwnerRepLedgerReports(mux *http.ServeMux) {
 if s.business==nil{return}
 mux.HandleFunc("GET /api/v1/workspace/owner/reports/representatives/{id}",
  func(w http.ResponseWriter,r *http.Request){
   if _,ok:=s.authorizeBrowser(w,r,false,true);!ok{return}
   if len(r.URL.Query())!=0 {businessError(w,business.ErrInvalid);return}
   report,err:=s.business.OwnerRepresentativeLedgerReport(r.Context(),r.PathValue("id"))
   if err!=nil{businessError(w,err);return}
   respond(w,http.StatusOK,report)
  })
}
