package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

const ownershipBase = "/api/v1/workspace/clients/ownership"

func (s *Server) EnableClientOwnershipV1Lab(ctx context.Context) error {
	if s.business == nil || s.business.UsageLedger() == nil || s.clientOwnershipV1Enabled {
		return errors.New("client_ownership_prerequisites_missing")
	}
	if e := s.business.EnableClientOwnershipV1(ctx); e != nil {
		return e
	}
	installed, err := s.business.DarkRestoreHistoryAvailable(ctx)
	if err != nil {
		return err
	}
	s.darkRestoreReceipts = installed
	s.clientOwnershipV1Enabled = true
	return nil
}
func decodeOwnership(w http.ResponseWriter, r *http.Request, out any, commit bool) bool {
	bad := func() bool { respond(w, 400, map[string]string{"error": "invalid_request"}); return false }
	var raw json.RawMessage
	if decode(w, r, 64<<10, &raw) != nil || usagewire.Decode(raw, out) != nil {
		return bad()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return bad()
	}
	names := []string{"client_ids", "source_group_id", "representative_id", "group_id", "reason"}
	if commit {
		names = append(names, "event_id", "quote_hash", "quote_unix")
	}
	if len(fields) != len(names) {
		return bad()
	}
	for _, name := range names {
		v, ok := fields[name]
		if !ok || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return bad()
		}
	}
	return true
}
func (s *Server) attachClientOwnership(mux *http.ServeMux) {
	if s.business == nil || !s.clientOwnershipV1Enabled {
		return
	}
	for _, action := range []string{"quote", "commit"} {
		mux.HandleFunc("POST "+ownershipBase+"/"+action, func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, true, true)
			if !ok {
				return
			}
			if r.URL.RawQuery != "" || r.URL.ForceQuery {
				businessError(w, business.ErrInvalid)
				return
			}
			if action == "quote" {
				var in business.ClientOwnershipIntent
				if !decodeOwnership(w, r, &in, false) {
					return
				}
				result, e := s.business.QuoteClientOwnership(r.Context(), session.Token, in)
				if e != nil {
					businessError(w, e)
					return
				}
				respond(w, 200, result)
			} else {
				var in business.ClientOwnershipCommit
				if !decodeOwnership(w, r, &in, true) {
					return
				}
				result, e := s.business.CommitClientOwnership(r.Context(), session.Token, in)
				if e != nil {
					businessError(w, e)
					return
				}
				respond(w, 200, result)
			}
		})
	}
	mux.HandleFunc("GET "+ownershipBase+"/receipts/{event}", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, true)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			businessError(w, business.ErrInvalid)
			return
		}
		result, e := s.business.ClientOwnershipReceipt(r.Context(), session.Token, r.PathValue("event"))
		if e != nil {
			businessError(w, e)
			return
		}
		respond(w, 200, result)
	})
}
