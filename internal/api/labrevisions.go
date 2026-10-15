package api

import (
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/labrevision"
)

// EnableLabRevisions provides an owner-only private LAB config-selection
// journal. No endpoint here applies, restarts or stops ANY Xray service.
func (s *Server) EnableLabRevisions(store *labrevision.Store)error{
	if store==nil||s.stages==nil||s.replacements==nil{
		return errors.New("LAB revisions require replacement and encrypted staging")
	}
	s.labRevisions=store
	return nil
}

func (s *Server) attachLabRevisions(mux *http.ServeMux){
	if s.labRevisions==nil{return}

	mux.HandleFunc("GET /api/v1/admin/lab-revisions",func(w http.ResponseWriter,r *http.Request){
		if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
		respond(w,http.StatusOK,s.labRevisions.Snapshot())
	})

	mux.HandleFunc("POST /api/v1/admin/lab-revisions/{operation}/prepare",func(w http.ResponseWriter,r *http.Request){
		if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
		operation:=r.PathValue("operation")
		metadata,config,err:=s.stages.VerifiedPrivateForLabRevision(operation)
		if err!=nil{
			respond(w,http.StatusConflict,map[string]string{"error":"verified_stopped_canary_required"})
			return
		}
		// Old/retired installations must NEVER prepare a future live config.
		paired:=false
		for _,node:=range s.registry.List(){
			if node.NodeID==metadata.NodeID && node.InstallationID==metadata.InstallationID{
				paired=true
				break
			}
		}
		if !paired{
			respond(w,http.StatusConflict,map[string]string{"error":"node_installation_changed"})
			return
		}
		// The replacement journal must still match the exact staged route
		// and must not be cancelled, switched or superseded.
		op,err:=s.replacements.Get(operation)
		if err!=nil||op.NewNodeID!=metadata.NodeID||
			op.InstallationID!=metadata.InstallationID||
			op.Preview.ManifestSHA256!=metadata.ManifestSHA256||
			op.CanActivate{
			respond(w,http.StatusConflict,map[string]string{"error":"replacement_revision_mismatch"})
			return
		}
		version,err:=s.labRevisions.Prepare(metadata,config)
		switch{
		case err==nil:respond(w,http.StatusCreated,version)
		case errors.Is(err,labrevision.ErrConflict):
			respond(w,http.StatusConflict,map[string]string{"error":"revision_conflict"})
		default:respond(w,http.StatusUnprocessableEntity,map[string]string{"error":"unsafe_lab_selection"})
		}
	})

	mux.HandleFunc("POST /api/v1/admin/lab-revisions/select",func(w http.ResponseWriter,r *http.Request){
		if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
		var input struct{
			VersionID string `json:"version_id"`
			ExpectedGeneration uint64 `json:"expected_generation"`
		}
		if err:=decode(w,r,1024,&input);err!=nil{
			respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"})
			return
		}
		snapshot,err:=s.labRevisions.Select(input.VersionID,input.ExpectedGeneration)
		switch{
		case err==nil:respond(w,http.StatusOK,snapshot)
		case errors.Is(err,labrevision.ErrMissing):
			respond(w,http.StatusNotFound,map[string]string{"error":"version_not_found"})
		default:respond(w,http.StatusConflict,map[string]string{"error":"stale_generation"})
		}
	})

	mux.HandleFunc("POST /api/v1/admin/lab-revisions/rollback-selection",func(w http.ResponseWriter,r *http.Request){
		if !s.isOwner(r){respond(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return}
		var input struct{ExpectedGeneration uint64 `json:"expected_generation"`}
		if err:=decode(w,r,256,&input);err!=nil{
			respond(w,http.StatusBadRequest,map[string]string{"error":"invalid_request"})
			return
		}
		snapshot,err:=s.labRevisions.RollbackSelection(input.ExpectedGeneration)
		switch{
		case err==nil:respond(w,http.StatusOK,snapshot)
		case errors.Is(err,labrevision.ErrNoPrevious):
			respond(w,http.StatusConflict,map[string]string{"error":"no_previous_version"})
		default:respond(w,http.StatusConflict,map[string]string{"error":"stale_generation"})
		}
	})
}
