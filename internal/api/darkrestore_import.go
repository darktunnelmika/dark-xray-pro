package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

const restoreBase = "/api/v1/workspace/dark-restore"

type restoreStage struct {
	path   string
	review darkrestore.Review
	digest string
}

// Called once, before the browser listener starts. Only this local configuration
// selects a snapshot. HTTP callers may reference it by its canonical review hash.
func (s *Server) StageDarkRestoreImport(ctx context.Context, path, sha string, asOf int64) error {
	if s.darkRestoreStage != nil || !s.clientOwnershipV1Enabled || s.business == nil || asOf <= 0 || asOf > time.Now().Unix() {
		return errors.New("invalid restore staging configuration")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return darkrestore.ErrUnsafeSource
	}
	dir, err := os.Stat(filepath.Dir(path))
	if err != nil || !dir.IsDir() || dir.Mode().Perm()&0077 != 0 {
		return darkrestore.ErrUnsafeSource
	}
	review, err := darkrestore.BuildReview(ctx, path, sha, time.Unix(asOf, 0))
	if err != nil {
		return err
	}
	digest, err := darkrestore.CanonicalReviewSHA256(review)
	if err != nil {
		return err
	}
	if err = s.business.EnableDarkRestoreImportV1(ctx); err != nil {
		return err
	}
	s.darkRestoreStage = &restoreStage{path, review, digest}
	return nil
}

type restoreQuoteRequest struct {
	ReviewSHA string                               `json:"review_sha256"`
	Mappings  []business.DarkRestoreGroupMappingV1 `json:"mappings"`
}
type restoreCommitRequest struct {
	restoreQuoteRequest
	EventID   string `json:"event_id"`
	QuoteHash string `json:"quote_hash"`
	QuoteUnix int64  `json:"quote_unix"`
}

func (s *Server) attachDarkRestoreImport(mux *http.ServeMux) {
	stage := s.darkRestoreStage
	if stage == nil {
		return
	}
	handle := func(pattern string, write bool, run func(http.ResponseWriter, *http.Request, string)) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			session, ok := s.authorizeBrowser(w, r, write, true)
			if !ok {
				return
			}
			if r.URL.RawQuery != "" || r.URL.ForceQuery {
				businessError(w, business.ErrInvalid)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			run(w, r, session.Token)
		})
	}
	handle("GET "+restoreBase+"/stage", false, func(w http.ResponseWriter, r *http.Request, token string) {
		respond(w, 200, map[string]any{"review_sha256": stage.digest, "review": stage.review})
	})
	read := func(w http.ResponseWriter, r *http.Request, target any) bool {
		var raw json.RawMessage
		if decode(w, r, 4<<20, &raw) != nil || usagewire.Decode(raw, target) != nil {
			businessError(w, business.ErrInvalid)
			return false
		}
		return true
	}
	intent := func(req restoreQuoteRequest) (business.DarkRestoreImportIntentV1, error) {
		if req.ReviewSHA != stage.digest {
			return business.DarkRestoreImportIntentV1{}, business.ErrConflict
		}
		return business.DarkRestoreImportIntentV1{Review: stage.review, Mappings: req.Mappings}, nil
	}
	handle("POST "+restoreBase+"/quote", true, func(w http.ResponseWriter, r *http.Request, token string) {
		var req restoreQuoteRequest
		if !read(w, r, &req) {
			return
		}
		in, err := intent(req)
		if err != nil {
			businessError(w, err)
			return
		}
		result, err := s.business.QuoteDarkRestoreImportV1(r.Context(), token, in)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, result)
	})
	handle("POST "+restoreBase+"/commit", true, func(w http.ResponseWriter, r *http.Request, token string) {
		var req restoreCommitRequest
		if !read(w, r, &req) {
			return
		}
		in, err := intent(req.restoreQuoteRequest)
		if err != nil {
			businessError(w, err)
			return
		}
		result, err := s.business.CommitDarkRestoreImportV1(r.Context(), token, stage.path, business.DarkRestoreImportCommitV1{DarkRestoreImportIntentV1: in, EventID: req.EventID, QuoteHash: req.QuoteHash, QuoteUnix: req.QuoteUnix})
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, result)
	})
	handle("GET "+restoreBase+"/receipts/{event}", false, func(w http.ResponseWriter, r *http.Request, token string) {
		result, err := s.business.DarkRestoreImportReceiptV1(r.Context(), token, r.PathValue("event"))
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, result)
	})
}
