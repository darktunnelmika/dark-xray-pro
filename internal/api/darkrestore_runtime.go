package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
		ConnectionState                        *mx.CustomerConnectionState
		ConnectionReceipt                      *mx.CustomerConnectionReceipt
		SystemProbeState                       *mx.DarkRestoreSystemProbeState
		SystemProbeReceipt                     *mx.ProtocolHealthReceipt
	}
	if decode(w, r, 16384, &req) != nil {
		businessError(w, business.ErrInvalid)
		return
	}
	if _, ok := s.registry.Authenticate(req.NodeID, req.InstallationID, bearer(r)); !ok {
		respond(w, 401, map[string]string{"error": "unauthorized"})
		return
	}
	if req.Phase == "system-probe-result" {
		if s.darkRestoreProbeEndpoint == nil || req.SystemProbeReceipt == nil || req.SystemProbeState != nil || req.ConnectionReceipt != nil || req.ConnectionState != nil || req.Birth != nil || req.ListenerApply != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		evidence, err := s.business.RegisterDarkRestoreSystemProbeV1(r.Context(), []byte(bearer(r)), req.NodeID, req.InstallationID, req.PlanSHA, *s.darkRestoreProbeEndpoint, *req.SystemProbeReceipt)
		if err != nil {
			businessError(w, err)
			return
		}
		raw, _ := json.Marshal(evidence.Receipt)
		digest := sha256.Sum256(raw)
		respond(w, 200, map[string]any{"receipt_sha256": hex.EncodeToString(digest[:]), "serving_health_recorded": true})
		return
	}
	if req.Phase == "connection-result" {
		if req.ConnectionReceipt == nil || req.SystemProbeReceipt != nil || req.SystemProbeState != nil || req.Birth != nil || req.ListenerApply != nil || req.ConnectionState != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		sha, err := s.business.RegisterDarkRestoreConnection(r.Context(), []byte(bearer(r)), req.NodeID, req.InstallationID, req.PlanSHA, *req.ConnectionReceipt)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, map[string]string{"receipt_sha256": sha})
		return
	}
	if req.Phase == "applied" {
		if req.ListenerApply == nil || req.Birth != nil || req.SystemProbeReceipt != nil || req.SystemProbeState != nil ||
			req.ConnectionReceipt != nil || req.ConnectionState != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		sha, err := s.business.RegisterDarkRestoreListenerApply(r.Context(), req.NodeID, req.InstallationID, req.PlanSHA, *req.ListenerApply)
		if err != nil {
			businessError(w, err)
			return
		}
		if s.darkRestoreProbeEndpoint != nil {
			s.quarantineMu.Lock()
			q := s.quarantine
			s.quarantineMu.Unlock()
			if q == nil {
				businessError(w, business.ErrDarkRestoreCutover)
				return
			}
			q.mu.Lock()
			if !q.runtime || q.planSHA != req.PlanSHA || q.plan.NodeID != req.NodeID || q.plan.InstallationID != req.InstallationID {
				q.mu.Unlock()
				businessError(w, business.ErrOwnershipDenied)
				return
			}
			eventID := "darkrestore-probe-" + req.PlanSHA[:24]
			_, err = s.business.QueueDarkRestoreSystemProbeV1(r.Context(), s.inboundKey, q.token, q.client, business.DarkRestoreSystemProbeInputV1{EventID: eventID, PlanSHA: req.PlanSHA}, *s.darkRestoreProbeEndpoint)
			q.mu.Unlock()
			if err != nil {
				businessError(w, err)
				return
			}
		}
		respond(w, 200, map[string]any{"receipt_sha256": sha})
		return
	}
	if req.Phase == "recovery" {
		if req.Birth != nil || req.ListenerApply != nil || req.ConnectionState != nil || req.ConnectionReceipt != nil ||
			req.SystemProbeState != nil || req.SystemProbeReceipt != nil {
			businessError(w, business.ErrInvalid)
			return
		}
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
	case "system-probe-poll":
		if s.darkRestoreProbeEndpoint == nil {
			respond(w, 200, map[string]any{"work": false})
			return
		}
		if req.SystemProbeState == nil || req.SystemProbeReceipt != nil || req.ConnectionState != nil || req.ConnectionReceipt != nil || req.Birth != nil || req.ListenerApply != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		challenge, found, err := s.business.PollDarkRestoreSystemProbeV1(r.Context(), s.inboundKey, []byte(bearer(r)), req.NodeID, req.InstallationID, req.PlanSHA, *req.SystemProbeState, *s.darkRestoreProbeEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		if !found {
			respond(w, 200, map[string]any{"work": false})
			return
		}
		respond(w, 200, map[string]any{"work": true, "challenge": challenge, "endpoint": *s.darkRestoreProbeEndpoint})
	case "connection-poll":
		if s.customerConnectionEndpoint == nil {
			respond(w, 200, map[string]any{"work": false})
			return
		}
		if req.ConnectionState == nil || req.ConnectionReceipt != nil || req.Birth != nil || req.ListenerApply != nil ||
			req.SystemProbeState != nil || req.SystemProbeReceipt != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		if _, err := s.business.DarkRestoreAdmissionEpochForNode(r.Context(), s.inboundKey, req.NodeID, req.InstallationID, q.client, q.planSHA, true); err != nil {
			businessError(w, err)
			return
		}
		challenge, found, err := s.business.PollDarkRestoreConnection(r.Context(), s.inboundKey, []byte(bearer(r)), req.NodeID, req.InstallationID, req.PlanSHA, *req.ConnectionState, *s.customerConnectionEndpoint)
		if err != nil {
			businessError(w, err)
			return
		}
		if !found {
			respond(w, 200, map[string]any{"work": false})
			return
		}
		respond(w, 200, map[string]any{"work": true, "challenge": challenge, "endpoint": *s.customerConnectionEndpoint})
	case "prepare":
		if req.Birth != nil || req.ListenerApply != nil || req.ConnectionState != nil || req.ConnectionReceipt != nil ||
			req.SystemProbeState != nil || req.SystemProbeReceipt != nil {
			businessError(w, business.ErrInvalid)
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
		if req.Birth == nil || req.ListenerApply != nil || req.ConnectionState != nil || req.ConnectionReceipt != nil ||
			req.SystemProbeState != nil || req.SystemProbeReceipt != nil ||
			q.lease == nil || req.Birth.Lease != *q.lease || req.Birth.CoreSHA != q.coreSHA {
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
		if req.Birth != nil || req.ListenerApply != nil || req.ConnectionState != nil || req.ConnectionReceipt != nil ||
			req.SystemProbeState != nil || req.SystemProbeReceipt != nil {
			businessError(w, business.ErrInvalid)
			return
		}
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
