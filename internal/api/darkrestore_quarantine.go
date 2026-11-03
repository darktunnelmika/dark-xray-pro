package api

import (
	"crypto/ed25519"
	"encoding/hex"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"net/http"
	"sync"
)

type quarantineTransport struct {
	mu      sync.Mutex
	token   string
	client  string
	planSHA string
	coreSHA string
	public  ed25519.PublicKey
	plan    business.DarkRestoreQuarantinePlan
}

// Owner must approve again after HUB restart. Node registration remains
// durable on the node and the exact same plan can recover its receipt.
func (s *Server) approveQuarantine(w http.ResponseWriter, r *http.Request, token, client string) {
	var req struct {
		CoreSHA   string
		PublicKey string
	}
	if decode(w, r, 2048, &req) != nil {
		businessError(w, business.ErrInvalid)
		return
	}
	pub, err := hex.DecodeString(req.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize || len(req.CoreSHA) != 64 {
		businessError(w, business.ErrInvalid)
		return
	}
	core, err := hex.DecodeString(req.CoreSHA)
	if err != nil || len(core) != 32 {
		businessError(w, business.ErrInvalid)
		return
	}
	p, err := s.business.DarkRestoreQuarantinePlan(r.Context(), s.inboundKey, token, client)
	if err != nil {
		businessError(w, err)
		return
	}
	sha, err := p.Digest()
	if err != nil {
		businessError(w, err)
		return
	}
	s.quarantineMu.Lock()
	defer s.quarantineMu.Unlock()
	if s.quarantine != nil && (s.quarantine.planSHA != sha || s.quarantine.coreSHA != req.CoreSHA || hex.EncodeToString(s.quarantine.public) != req.PublicKey) {
		businessError(w, business.ErrConflict)
		return
	}
	s.quarantine = &quarantineTransport{token: token, client: client, planSHA: sha, coreSHA: req.CoreSHA, public: pub, plan: p}
	respond(w, 200, map[string]any{"plan_sha256": sha, "node_id": p.NodeID, "installation_id": p.InstallationID, "runtime_changed": false})
}
func (s *Server) quarantineAgent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		NodeID         string
		InstallationID string
		PlanSHA        string
		Receipt        *business.DarkRestoreQuarantineRegistration
	}
	if decode(w, r, 8192, &req) != nil {
		businessError(w, business.ErrInvalid)
		return
	}
	if _, ok := s.registry.Authenticate(req.NodeID, req.InstallationID, bearer(r)); !ok {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	s.quarantineMu.Lock()
	q := s.quarantine
	s.quarantineMu.Unlock()
	if q == nil {
		w.WriteHeader(204)
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if req.PlanSHA != q.planSHA || req.NodeID != q.plan.NodeID || req.InstallationID != q.plan.InstallationID {
		businessError(w, business.ErrOwnershipDenied)
		return
	}
	p, err := s.business.DarkRestoreQuarantinePlan(r.Context(), s.inboundKey, q.token, q.client)
	if err != nil {
		businessError(w, err)
		return
	}
	sha, err := p.Digest()
	if err != nil || sha != q.planSHA {
		businessError(w, business.ErrDarkRestoreCutover)
		return
	}
	if req.Receipt == nil {
		respond(w, 200, p.PrivateDelivery())
		return
	}
	if _, err = s.business.ReviewDarkRestoreQuarantineRegistration(r.Context(), s.inboundKey, q.token, q.client, q.public, q.coreSHA, *req.Receipt); err != nil {
		businessError(w, err)
		return
	}
	respond(w, 200, map[string]any{"registered": true, "plan_sha256": sha, "scope": "offline_syntax_only_v1", "runtime_changed": false})
}
