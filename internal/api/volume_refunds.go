package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// A single bounded worker handles both expiry and manual archive. Financial
// finality, funding and exactly-once accounting remain inside the Store's
// write transaction; neither a browser read nor an Agent request credits money.
type volumeRefundService struct {
	mu      sync.Mutex
	started bool
	done    chan struct{}
}

// EnableVolumeRefunds is startup-only and uses only independent PRO storage.
// The key is necessary to verify managed execution's encrypted final receipts.
func (s *Server) EnableVolumeRefunds(ctx context.Context) error {
	if s.volumeRefunds != nil || s.business == nil || s.business.UsageLedger() == nil || len(s.inboundKey) != 32 {
		return errors.New("volume_refund_prerequisites_missing")
	}
	if err := s.business.EnableVolumeExpiryRefunds(ctx); err != nil {
		return err
	}
	s.volumeRefunds = &volumeRefundService{done: make(chan struct{})}
	return nil
}

func (s *Server) RunVolumeRefunds(ctx context.Context) {
	s.runVolumeRefunds(ctx, 5*time.Second, 4*time.Second)
}

func (s *Server) runVolumeRefunds(ctx context.Context, interval, budget time.Duration) {
	x := s.volumeRefunds
	if x == nil {
		return
	}
	x.mu.Lock()
	if x.started {
		x.mu.Unlock()
		return
	}
	x.started = true
	x.mu.Unlock()
	defer close(x.done)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	cursor := ""
	failed := false
	for {
		if ctx.Err() != nil {
			return
		}
		attempt, cancel := context.WithTimeout(ctx, budget)
		batch, err := s.business.SweepExpiredVolumeRefunds(attempt, s.inboundKey, cursor, 50)
		cancel()
		if batch.NextClientID != "" {
			cursor = batch.NextClientID
		}
		if err == nil {
			cursor = batch.NextClientID
			if failed {
				log.Print("PRO volume refund worker recovered")
			}
			failed = false
		} else if ctx.Err() == nil && !failed {
			// A failed read/write is never treated as zero usage or a successful
			// refund. Avoid exposing identities or private accounting in logs.
			log.Print("PRO volume refund worker could not verify a batch; retained receipts will be retried")
			failed = true
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Server) WaitVolumeRefunds(ctx context.Context) {
	if s.volumeRefunds == nil {
		return
	}
	select {
	case <-s.volumeRefunds.done:
	case <-ctx.Done():
	}
}

func (s *Server) attachVolumeRefunds(mux *http.ServeMux) {
	if s.volumeRefunds == nil {
		return
	}
	mux.HandleFunc("GET /api/v1/workspace/volume-refunds", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok {
			return
		}
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_query"})
			return
		}
		for key, values := range query {
			if (key != "limit" && key != "offset") || len(values) != 1 {
				respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_query"})
				return
			}
		}
		limit, offset := 25, 0
		for key, dest := range map[string]*int{"limit": &limit, "offset": &offset} {
			if query.Has(key) {
				value, parseErr := strconv.Atoi(query.Get(key))
				if parseErr != nil {
					respond(w, http.StatusBadRequest, map[string]string{"error": "invalid_query"})
					return
				}
				*dest = value
			}
		}
		scope := ""
		if session.User.Role != "owner" {
			scope = session.User.RepresentativeID
		}
		rows, err := s.business.VolumeRefundHistory(r.Context(), scope, limit, offset)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, map[string]any{"representative_id": scope, "refunds": rows, "limit": limit, "offset": offset})
	})
	mux.HandleFunc("GET /api/v1/workspace/clients/{id}/volume-refund", func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.authorizeBrowser(w, r, false, false)
		if !ok {
			return
		}
		if r.URL.RawQuery != "" {
			respond(w, http.StatusBadRequest, map[string]string{"error": "unexpected_query"})
			return
		}
		client, err := s.business.Client(r.Context(), r.PathValue("id"))
		if err != nil {
			businessError(w, err)
			return
		}
		if session.User.Role != "owner" && client.RepresentativeID != session.User.RepresentativeID {
			respond(w, http.StatusForbidden, map[string]string{"error": "representative_scope_denied"})
			return
		}
		status, err := s.business.VolumeExpiryRefundStatus(r.Context(), s.inboundKey, client.ID)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, status)
	})
}
