package api

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundsecurity"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

type inboundProbeJob struct {
	ID           string                       `json:"id"`
	Request      inboundsecurity.ProbeRequest `json:"request"`
	Expires      int64                        `json:"expires_unix"`
	installation string
	delivered    bool
	result       chan inboundsecurity.ProbeResult
}
type inboundProbeQueue struct {
	sync.Mutex
	jobs map[string]*inboundProbeJob
}

// ProbeSelectedNode queues an ephemeral owner-requested public TLS probe. The
// authenticated selected installation performs its own DNS and TLS connection;
// an offline Node times out instead of silently using the HUB's network.
func (s *Server) ProbeSelectedNode(ctx context.Context, node string, request inboundsecurity.ProbeRequest) (inboundsecurity.ProbeResult, error) {
	var empty inboundsecurity.ProbeResult
	if ctx.Err() != nil {
		return empty, errors.New("selected_node_probe_cancelled")
	}
	installation, found := s.nativeNode(node)
	if !found || node == localHubNodeID || request.Scope != "node" || request.NodeID != node {
		return empty, errors.New("selected_node_unavailable")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return empty, errors.New("probe_unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	deadline, _ := ctx.Deadline()
	job := &inboundProbeJob{ID: hex.EncodeToString(nonce[:]), Request: request, Expires: deadline.Unix(), installation: installation, result: make(chan inboundsecurity.ProbeResult, 1)}
	q := &s.inboundProbeJobs
	q.Lock()
	if q.jobs == nil {
		q.jobs = map[string]*inboundProbeJob{}
	}
	if len(q.jobs) >= 256 {
		q.Unlock()
		return empty, errors.New("node_probe_busy")
	}
	q.jobs[job.ID] = job
	q.Unlock()
	defer func() { q.Lock(); delete(q.jobs, job.ID); q.Unlock() }()
	select {
	case <-ctx.Done():
		return empty, errors.New("selected_node_probe_timeout")
	case result := <-job.result:
		current, ok := s.nativeNode(node)
		if !ok || current != installation {
			return empty, errors.New("selected_node_identity_changed")
		}
		return result, nil
	}
}

func (s *Server) handleInboundProbeAgent(w http.ResponseWriter, r *http.Request) {
	if r.TLS == nil || r.TLS.Version < tls.VersionTLS13 {
		respond(w, 426, map[string]string{"error": "tls13_required"})
		return
	}
	if s.inboundSecurity == nil {
		http.NotFound(w, r)
		return
	}
	var input struct {
		NodeID         string                       `json:"node_id"`
		InstallationID string                       `json:"installation_id"`
		ID             string                       `json:"id,omitempty"`
		Result         *inboundsecurity.ProbeResult `json:"result,omitempty"`
	}
	var raw json.RawMessage
	if decode(w, r, 64<<10, &raw) != nil || usagewire.Decode(raw, &input) != nil {
		respond(w, 422, map[string]string{"error": "invalid_probe_request"})
		return
	}
	if _, ok := s.registry.Authenticate(input.NodeID, input.InstallationID, bearer(r)); !ok {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	q := &s.inboundProbeJobs
	q.Lock()
	defer q.Unlock()
	if r.URL.Path == "/api/v1/agent/inbound-security/poll" {
		if input.ID != "" || input.Result != nil {
			respond(w, 422, map[string]string{"error": "invalid_probe_poll"})
			return
		}
		for _, job := range q.jobs {
			if job.Request.NodeID != input.NodeID || job.installation != input.InstallationID || job.delivered || job.Expires <= time.Now().Unix() {
				continue
			}
			job.delivered = true
			respond(w, 200, job)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	job, ok := q.jobs[input.ID]
	if !ok || !job.delivered || job.Expires <= time.Now().Unix() || job.Request.NodeID != input.NodeID || job.installation != input.InstallationID || input.Result == nil {
		respond(w, 409, map[string]string{"error": "probe_job_expired_or_mismatched"})
		return
	}
	result := *input.Result
	if result.Scope != "node" || result.NodeID != input.NodeID || result.Target != job.Request.Target || result.ServerName != job.Request.ServerName && job.Request.ServerName != "" {
		respond(w, 422, map[string]string{"error": "probe_result_identity_mismatch"})
		return
	}
	select {
	case job.result <- result:
		delete(q.jobs, input.ID)
		w.WriteHeader(http.StatusNoContent)
	default:
		respond(w, 409, map[string]string{"error": "probe_result_already_received"})
	}
}
