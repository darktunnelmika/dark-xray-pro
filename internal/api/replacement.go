package api

import (
 "errors"
 "net/http"

 "github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
 "github.com/darktunnelmika/dark-xray-pro/internal/replacement"
 "github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)

const maxReplacementBytes=16*1024

// EnableReplacementLab is configured BEFORE serving. It adds a private
// persistent staging workflow; no activation or subscription API is exposed.
func (s *Server) EnableReplacementLab(j *replacement.Journal)error{
 if j==nil{return errors.New("replacement journal required")}
 s.replacements=j
 return nil
}

func pairedNodes(reg *noderegistry.Registry, oldID,newID string)(string,bool){
 oldFound:=false
 installation:=""
 for _,node:=range reg.List(){
  if node.NodeID==oldID{oldFound=true}
  if node.NodeID==newID{installation=node.InstallationID}
 }
 return installation,oldFound&&installation!=""
}

func (s *Server) attachReplacement(mux *http.ServeMux){
 if s.replacements==nil{return}

 mux.HandleFunc("GET /api/v1/admin/replacements",func(w http.ResponseWriter,r *http.Request){
  if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
  respond(w,http.StatusOK,map[string]any{"operations":s.replacements.List()})
 })

 mux.HandleFunc("POST /api/v1/admin/replacements",func(w http.ResponseWriter,r *http.Request){
  if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
  var input struct{
   OldNodeID string `json:"old_node_id"`
   NewNodeID string `json:"new_node_id"`
   Deployments []routeplan.Deployment `json:"deployments"`
  }
  if err:=decode(w,r,maxReplacementBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  if len(input.Deployments)==0||len(input.Deployments)>64{
   respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_route_count"});return
  }
  installation,paired:=pairedNodes(s.registry,input.OldNodeID,input.NewNodeID)
  if !paired{
   respond(w,http.StatusConflict,map[string]string{"error":"both_nodes_must_be_newly_paired"});return
  }
  draft,err:=routeplan.New(input.OldNodeID,input.NewNodeID)
  if err!=nil{
   respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_node"});return
  }
  seen:=map[uint64]bool{}
  for _,deployment:=range input.Deployments{
   if deployment.InboundID==0||seen[deployment.InboundID]||
      deployment.Direct==nil&&deployment.Swap==nil{
    respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_deployment"});return
   }
   seen[deployment.InboundID]=true
   if deployment.Direct!=nil{
    if err:=draft.SetDirect(deployment.InboundID,int(deployment.Direct.NodePort));err!=nil{
     respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_direct"});return
    }
   }
   if deployment.Swap!=nil{
    swap:=deployment.Swap
    if err:=draft.SetSwap(deployment.InboundID,int(swap.TunnelPort),
       int(swap.NodePort),swap.TunnelAddress);err!=nil{
     respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_swap"});return
    }
   }
  }
  plan,err:=draft.Preview()
  if err!=nil{
   respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_plan"});return
  }
  op,err:=s.replacements.Create(plan,installation)
  switch{
   case err==nil:
    respond(w,http.StatusCreated,op)
   case errors.Is(err,replacement.ErrConflict):
    respond(w,http.StatusConflict,map[string]string{"error":"replacement_conflict"})
   default:
    respond(w,http.StatusServiceUnavailable,map[string]string{"error":"replacement_journal_unavailable"})
  }
 })

 mux.HandleFunc("GET /api/v1/admin/replacements/{id}",func(w http.ResponseWriter,r *http.Request){
  if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
  op,err:=s.replacements.Get(r.PathValue("id"))
  if err!=nil{respond(w,http.StatusNotFound,map[string]string{"error":"not_found"});return}
  respond(w,http.StatusOK,op)
 })

 mux.HandleFunc("POST /api/v1/admin/replacements/{id}/cancel",func(w http.ResponseWriter,r *http.Request){
  if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
  var input struct{ExpectedRevision uint64 `json:"expected_revision"`}
  if err:=decode(w,r,256,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  op,err:=s.replacements.Cancel(r.PathValue("id"),input.ExpectedRevision)
  switch{
   case err==nil:respond(w,http.StatusOK,op)
   case errors.Is(err,replacement.ErrNotFound):
    respond(w,http.StatusNotFound,map[string]string{"error":"not_found"})
   default:respond(w,http.StatusConflict,map[string]string{"error":"revision_conflict"})
  }
 })

 mux.HandleFunc("GET /api/v1/agent/replacement-task",func(w http.ResponseWriter,r *http.Request){
  id,installation:=r.URL.Query().Get("node_id"),r.URL.Query().Get("installation_id")
  if _,ok:=s.registry.Authenticate(id,installation,bearer(r));!ok{
   respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return
  }
  op,ok:=s.replacements.Next(id,installation)
  if !ok{w.WriteHeader(http.StatusNoContent);return}
  respond(w,http.StatusOK,op)
 })

 mux.HandleFunc("POST /api/v1/agent/replacement-report",func(w http.ResponseWriter,r *http.Request){
  var input replacement.Evidence
  if err:=decode(w,r,maxReplacementBytes,&input);err!=nil{
   respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"});return
  }
  if _,ok:=s.registry.Authenticate(input.NewNodeID,input.InstallationID,bearer(r));!ok{
   respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return
  }
  op,err:=s.replacements.Report(input)
  switch{
   case err==nil:respond(w,http.StatusOK,op)
   case errors.Is(err,replacement.ErrNotFound):
    respond(w,http.StatusNotFound,map[string]string{"error":"not_found"})
   case errors.Is(err,replacement.ErrMismatch),errors.Is(err,replacement.ErrConflict),
        errors.Is(err,replacement.ErrCancelled):
    respond(w,http.StatusConflict,map[string]string{"error":"replacement_mismatch"})
   default:respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"invalid_probe_evidence"})
  }
 })
}
