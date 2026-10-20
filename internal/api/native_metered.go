package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// EnableRemoteMeteredNode is a deliberate NEW PRO only owner deployment
// operation. Empty means the dedicated Agent TLS endpoints remain unavailable.
// No browser request, legacy Node, or runtime report can toggle this.
func (s *Server) EnableRemoteMeteredNode(node string) error {
	if s.business == nil || s.business.UsageLedger() == nil || len(s.inboundKey) != 32 || node == "" || len(node) > 64 {
		return errors.New("native metered protocol requires owner-provisioned Node, private inbound keys and durable usage ledger")
	}
	if _, ok := s.nativeNode(node); !ok {
		return errors.New("explicitly selected new PRO Node must be paired first")
	}
	if owned, e := s.business.RouteChannelOwned(context.Background(), node); e != nil || owned {
		return business.ErrConflict
	}
	s.meteredNode = node
	return nil
}
func (s *Server) meteredCandidate(ctx context.Context, node, installation string) (business.NodeConfig, error) {
	var result business.NodeConfig
	if node != s.meteredNode || node == "" || s.business == nil || s.business.UsageLedger() == nil || len(s.inboundKey) != 32 {
		return result, business.ErrUnavailable
	}
	if owned, e := s.business.RouteChannelOwned(ctx, node); e != nil || owned {
		return result, business.ErrConflict
	}
	cfg, err := s.business.BuildNodeConfig(ctx, s.inboundKey, node, installation)
	if err != nil {
		return result, err
	}
	if cfg.UserCount <= 0 || cfg.Candidate.NoListeners {
		return result, business.ErrUnavailable
	}
	c, err := xrayconfig.WithUsageAPI(cfg.Candidate, managedxray.MeteredStatsEndpoint)
	if err != nil {
		return result, business.ErrInvalid
	}
	cfg.Candidate = c
	cfg.ConfigSHA = c.SHA256
	cfg.ManifestSHA = c.ManifestSHA256
	return cfg, nil
}
func (s *Server) handleRemoteMetered(w http.ResponseWriter, r *http.Request) {
	// AgentHandler already enforces TLS1.3, strict path isolation and
	// Content-Type/no-store on this separate loopback listener.
	if s.meteredNode == "" || s.business == nil || s.business.UsageLedger() == nil {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "remote_metered_admission_disabled"})
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/agent/native-metered-runtime":
		node, installation := r.URL.Query().Get("node_id"), r.URL.Query().Get("installation_id")
		if node != s.meteredNode {
			respond(w, 403, map[string]string{"error": "node_not_allowlisted"})
			return
		}
		token := bearer(r)
		if _, ok := s.registry.Authenticate(node, installation, token); !ok {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		cfg, err := s.meteredCandidate(r.Context(), node, installation)
		if errors.Is(err, business.ErrNotFound) {
			w.WriteHeader(204)
			return
		}
		if err != nil {
			businessError(w, err)
			return
		}
		now := time.Now()
		expiry := now.Add(30 * time.Second).UnixNano()
		if cfg.LeaseNotAfter < expiry {
			expiry = cfg.LeaseNotAfter
		}
		lease, err := managedxray.SignLease([]byte(token), managedxray.Lease{
			NodeID: node, InstallationID: installation, Sequence: cfg.Sequence,
			ConfigSHA: cfg.ConfigSHA, ManifestSHA: cfg.ManifestSHA, Issued: now.UnixNano(), Expires: expiry,
		})
		if err != nil {
			businessError(w, business.ErrUnavailable)
			return
		}
		if err = s.business.RecordNativeDelivery(r.Context(), cfg, lease); err != nil {
			businessError(w, err)
			return
		}
		respond(w, 200, managedxray.Delivery{
			Version: 2, NodeID: node, InstallationID: installation, ListenIP: cfg.ListenIP,
			Sequence: cfg.Sequence, Candidate: cfg.Candidate,
			ConfigBase64: base64.StdEncoding.EncodeToString(cfg.Candidate.Config), Lease: lease,
		})
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/agent/native-metered-register":
		var request managedxray.BirthReport
		if decode(w, r, 8192, &request) != nil {
			businessError(w, business.ErrInvalid)
			return
		}
		lease := request.Lease
		node, installation := lease.NodeID, lease.InstallationID
		if node != s.meteredNode {
			respond(w, 403, map[string]string{"error": "node_not_allowlisted"})
			return
		}
		token := bearer(r)
		if _, ok := s.registry.Authenticate(node, installation, token); !ok ||
			!managedxray.ValidLeaseSignature(lease, token) ||
			!managedxray.ValidBirthReport(token, request) {
			respond(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		now := time.Now().UnixNano()
		if lease.Issued <= 0 || lease.Issued > now+int64(time.Second) || lease.Expires <= now ||
			lease.Expires-lease.Issued > int64(45*time.Second) {
			businessError(w, business.ErrConflict)
			return
		}
		cfg, err := s.meteredCandidate(r.Context(), node, installation)
		if err != nil {
			businessError(w, err)
			return
		}
		if lease.Sequence != cfg.Sequence || lease.ConfigSHA != cfg.ConfigSHA || lease.ManifestSHA != cfg.ManifestSHA ||
			request.PID <= 0 || request.ProcessStartTicks == 0 {
			businessError(w, business.ErrConflict)
			return
		}
		// Server-derived deterministic one-process ID is idempotent on
		// ambiguous acknowledgements. It does NOT authorize a different
		// birth with the same config or reset the usage ledger.
		mac := hmac.New(sha256.New, s.inboundKey)
		for _, v := range []string{node, installation, cfg.ConfigSHA, request.KernelBootID,
			strconv.Itoa(request.PID), strconv.FormatUint(request.ProcessStartTicks, 10)} {
			mac.Write([]byte(strconv.Itoa(len(v))))
			mac.Write([]byte(":"))
			mac.Write([]byte(v))
		}
		epochID := hex.EncodeToString(mac.Sum(nil)[:16])
		epoch, err := s.business.RegisterNativeUsageEpoch(r.Context(), cfg, business.UsageBirth{
			EpochID: epochID, KernelBootID: request.KernelBootID,
			PID: request.PID, StartTicks: request.ProcessStartTicks,
		})
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusCreated, epoch)
	default:
		http.NotFound(w, r)
	}
}

// Hard-coded allowlisted private loopback Stats API endpoint is deliberate:
// no Agent can choose an arbitrary host/port or send the HUB a user-supplied
// counter source. Changing it is a reviewed code change and requires CI.
