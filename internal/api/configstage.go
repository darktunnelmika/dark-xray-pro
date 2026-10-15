package api

import (
 "crypto/tls"
 "encoding/base64"
 "errors"
 "net/http"

 "github.com/darktunnelmika/dark-xray-pro/internal/configstage"
 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// EnableConfigStaging is opt-in and has no Xray apply/activate endpoint.
// It requires a durable replacement journal created earlier in this process.
func (s *Server) EnableConfigStaging(store *configstage.Store)error{
 if store==nil||s.replacements==nil{return errors.New("staging requires a journal and encrypted store")}
 s.stages=store
 return nil
}

func (s *Server) attachConfigStaging(mux *http.ServeMux){
 if s.stages==nil{return}

 mux.HandleFunc("GET /api/v1/admin/config-stages",func(w http.ResponseWriter,r *http.Request){
  if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
  respond(w,http.StatusOK,map[string]any{"stages":s.stages.List()})
 })

 mux.HandleFunc("GET /api/v1/admin/config-stages/{id}",func(w http.ResponseWriter,r *http.Request){
  if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
  meta,err:=s.stages.Get(r.PathValue("id"))
  if err!=nil{respond(w,http.StatusNotFound,map[string]string{"error":"not_found"});return}
  respond(w,http.StatusOK,meta)
 })

 // A private Reality profile is accepted ONLY on the SSH-forwarded LOCAL
 // owner listener. No API response includes raw private keys or config bytes.
 mux.HandleFunc("POST /api/v1/admin/replacements/{id}/candidate",func(w http.ResponseWriter,r *http.Request){
  if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
  var input struct{
   Profiles []xrayconfig.Profile `json:"profiles"`
   TrustedXraySHA256 string `json:"trusted_xray_sha256"`
  }
  if err:=decode(w,r,xrayconfig.MaxCandidateBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_candidate_request"});return
  }
  op,err:=s.replacements.Get(r.PathValue("id"))
  if err!=nil{respond(w,http.StatusNotFound,map[string]string{"error":"replacement_not_found"});return}
  if op.Phase!=replacement.TCPChecked||op.CanActivate{
   respond(w,http.StatusConflict,map[string]string{"error":"tcp_preflight_required"});return
  }
  _,paired:=pairedNodes(s.registry,op.OldNodeID,op.NewNodeID)
  currentInstall,exists:=pairedNodes(s.registry,op.OldNodeID,op.NewNodeID)
  if !paired||!exists||op.InstallationID!=currentInstall{
   respond(w,http.StatusConflict,map[string]string{"error":"node_installation_changed"});return
  }
  candidate,err:=xrayconfig.Render(op.Preview,input.Profiles)
  if err!=nil{respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_xray_profile"});return}
  meta,err:=s.stages.Create(op,candidate,input.TrustedXraySHA256)
  switch{
  case err==nil:
   respond(w,http.StatusCreated,meta)
  case errors.Is(err,configstage.ErrConflict):
   respond(w,http.StatusConflict,map[string]string{"error":"candidate_conflict"})
  case errors.Is(err,configstage.ErrInvalid):
   respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_staging_prerequisites"})
  default:
   respond(w,http.StatusServiceUnavailable,map[string]string{"error":"encrypted_stage_unavailable"})
  }
 })

 // Secret Xray config bytes are ONLY delivered to a paired installation over
 // direct TLS 1.3, never on the local plaintext admin/API listener.
 mux.HandleFunc("GET /api/v1/agent/config-stage",func(w http.ResponseWriter,r *http.Request){
  if r.TLS==nil||r.TLS.Version<tls.VersionTLS13{
   respond(w,http.StatusUpgradeRequired,map[string]string{"error":"tls13_required"});return
  }
  id,install:=r.URL.Query().Get("node_id"),r.URL.Query().Get("installation_id")
  if _,ok:=s.registry.Authenticate(id,install,bearer(r));!ok{
   respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return
  }
  meta,private,available,err:=s.stages.PendingFor(id,install)
  if err!=nil{respond(w,http.StatusServiceUnavailable,map[string]string{"error":"stage_not_available"});return}
  if !available{w.WriteHeader(http.StatusNoContent);return}
  respond(w,http.StatusOK,configstage.Delivery{
   Metadata:meta,ConfigBase64:base64.StdEncoding.EncodeToString(private),
  })
 })

 mux.HandleFunc("POST /api/v1/agent/config-stage-report",func(w http.ResponseWriter,r *http.Request){
  if r.TLS==nil||r.TLS.Version<tls.VersionTLS13{
   respond(w,http.StatusUpgradeRequired,map[string]string{"error":"tls13_required"});return
  }
  var proof configstage.Proof
  if err:=decode(w,r,4096,&proof);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_validation_proof"});return
  }
  if _,ok:=s.registry.Authenticate(proof.NodeID,proof.InstallationID,bearer(r));!ok{
   respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return
  }
  meta,err:=s.stages.Report(proof)
  switch {
  case err==nil:respond(w,http.StatusOK,meta)
  case errors.Is(err,configstage.ErrNotFound):
   respond(w,http.StatusNotFound,map[string]string{"error":"stage_not_found"})
  case errors.Is(err,configstage.ErrMismatch),errors.Is(err,configstage.ErrConflict):
   respond(w,http.StatusConflict,map[string]string{"error":"proof_conflict"})
  default:
   respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_validation_result"})
  }
 })
}
