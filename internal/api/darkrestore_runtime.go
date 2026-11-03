package api

import (
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"net/http"
	"time"
)

func (s *Server) darkRestoreRuntimeAgent(w http.ResponseWriter, r *http.Request) {
	if s.business == nil {
		businessError(w, business.ErrUnavailable)
		return
	}
	var req struct {
		NodeID, InstallationID, PlanSHA, Phase string
		Birth                                  *business.DarkRestoreQuarantineBirth
		ListenerApply                          *business.DarkRestoreListenerApply
	}
	if decode(w, r, 16384, &req) != nil {
		businessError(w, business.ErrInvalid)
		return
	}
	if _, ok := s.registry.Authenticate(req.NodeID, req.InstallationID, bearer(r)); !ok {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if req.Phase == "applied" {
		if req.ListenerApply == nil || req.Birth != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		sha, err := s.business.RegisterDarkRestoreListenerApply(r.Context(), req.NodeID, req.InstallationID, req.PlanSHA, *req.ListenerApply)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, map[string]any{"receipt_sha256": sha})
		return
	}
	if req.Phase == "recovery" {
		epoch, err := s.business.DarkRestoreRegisteredEpochForNode(r.Context(), req.NodeID, req.InstallationID, req.PlanSHA)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, map[string]any{"epoch": epoch})
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
	if !q.runtime || req.PlanSHA != q.planSHA || req.NodeID != q.plan.NodeID || req.InstallationID != q.plan.InstallationID {
		businessError(w, business.ErrOwnershipDenied)
		return
	}
	switch req.Phase {
	case "prepare":
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
		if q.lease == nil {
			now := time.Now()
			lease, err := mx.SignLease([]byte(bearer(r)), mx.Lease{NodeID: p.NodeID, InstallationID: p.InstallationID, Sequence: 1, ConfigSHA: p.ConfigSHA, ManifestSHA: p.ManifestSHA, Issued: now.UnixNano(), Expires: now.Add(40 * time.Second).UnixNano()})
			if err != nil {
				businessError(w, err)
				return
			}
			q.lease = &lease
		}
		respond(w, 200, map[string]any{"delivery": p.PrivateDelivery(), "lease": q.lease})
	case "birth":
		if req.Birth == nil || q.lease == nil || req.Birth.Lease != *q.lease || req.Birth.CoreSHA != q.coreSHA {
			businessError(w, business.ErrDarkRestoreCutover)
			return
		}
		registered, err := s.business.RegisterDarkRestoreBirth(r.Context(), s.inboundKey, q.token, q.client, q.planSHA, q.public, *q.lease, *req.Birth)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, map[string]any{"epoch": registered.Epoch})
	case "admit", "renew":
		epoch, err := s.business.DarkRestoreAdmissionEpochForNode(r.Context(), s.inboundKey, req.NodeID, req.InstallationID, q.client, q.planSHA, req.Phase == "renew")
		if err != nil {
			businessError(w, err)
			return
		}
		lease := q.lease
		if lease == nil {
			businessError(w, business.ErrDarkRestoreCutover)
			return
		}
		if req.Phase == "renew" {
			now := time.Now()
			next, err := mx.SignLease([]byte(bearer(r)), mx.Lease{NodeID: q.plan.NodeID, InstallationID: q.plan.InstallationID, Sequence: lease.Sequence, ConfigSHA: q.plan.ConfigSHA, ManifestSHA: q.plan.ManifestSHA, Issued: now.UnixNano(), Expires: now.Add(20 * time.Second).UnixNano()})
			if err != nil {
				businessError(w, err)
				return
			}
			lease = &next
		}
		respond(w, 200, map[string]any{"epoch": epoch, "lease": lease})
	default:
		businessError(w, business.ErrInvalid)
	}
}
