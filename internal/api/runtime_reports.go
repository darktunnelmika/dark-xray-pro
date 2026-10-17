package api

import("crypto/tls";"net/http";"strconv";"github.com/darktunnelmika/dark-xray-pro/internal/business";"github.com/darktunnelmika/dark-xray-pro/internal/managedxray")
func(s *Server)attachRuntimeReports(mux *http.ServeMux){
 if s.business==nil||len(s.inboundKey)!=32{return}
 mux.HandleFunc("POST /api/v1/agent/native-runtime-report",func(w http.ResponseWriter,r *http.Request){
 if r.TLS==nil||r.TLS.Version<tls.VersionTLS13{respond(w,426,map[string]string{"error":"tls13_required"});return}
 var report managedxray.RuntimeReport;if decode(w,r,8192,&report)!=nil{businessError(w,business.ErrInvalid);return};token:=bearer(r)
 if _,ok:=s.registry.Authenticate(report.Lease.NodeID,report.Lease.InstallationID,token);!ok||!managedxray.ValidReportSignature(report,token){respond(w,401,map[string]string{"error":"unauthorized"});return}
 if e:=s.business.AcceptNativeReport(r.Context(),report);e!=nil{businessError(w,e);return};w.WriteHeader(http.StatusNoContent)
 })
 mux.HandleFunc("GET /api/v1/workspace/native-inbounds-state",func(w http.ResponseWriter,r *http.Request){
 if _,ok:=s.authorizeBrowser(w,r,false,true);!ok{return};inbounds,e:=s.business.ListInbounds(r.Context(),s.inboundKey);if e!=nil{businessError(w,e);return}
 targets,e:=s.business.ListInboundTargets(r.Context());if e!=nil{businessError(w,e);return};nodes:=s.registry.List();if len(nodes)>200{businessError(w,business.ErrInvalid);return}
 states:=make([]business.RuntimeStatus,0,len(nodes));for _,n:=range nodes{state,e:=s.business.NativeRuntimeStatus(r.Context(),n.NodeID,n.InstallationID);if e!=nil{businessError(w,e);return};states=append(states,state)}
 respond(w,200,map[string]any{"inbounds":inbounds,"targets":targets,"nodes":nodes,"runtime":states,"evidence":"authenticated_agent_report","data_plane_verified":false,"subscription_publication":false,"preflight":s.nativeReadiness(inbounds,targets,nodes)})
 })
 mux.HandleFunc("GET /api/v1/workspace/native-inbounds/{id}/clients",func(w http.ResponseWriter,r *http.Request){
 if _,ok:=s.authorizeBrowser(w,r,false,true);!ok{return};id,e:=strconv.ParseUint(r.PathValue("id"),10,53);if e!=nil||id==0{businessError(w,business.ErrInvalid);return}
 ids,e:=s.business.InboundClientIDs(r.Context(),id);if e!=nil{businessError(w,e);return};respond(w,200,map[string]any{"client_ids":ids})
 })
}
