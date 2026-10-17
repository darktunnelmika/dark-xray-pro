package api

import (
	"net/http"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// Exact single-use canary identity, reserved for owner-controlled NEW PRO
// USA loopback acceptance. It does not issue a client URI, publish a
// subscription, create public listeners, activate Xray or erase history.
const (
	ownerCanaryNode                = "usa-pro-01"
	ownerCanaryClient              = "pro-usa-qa01"
	ownerCanaryInbound      uint64 = 901617
	ownerCanaryPort         uint16 = 19666
	ownerCanaryConfirmation        = "USA_PRO_LOOPBACK_QA_ONLY"
)
const ownerCanaryVolume int64 = 1 << 20

func (s *Server) attachOwnerNativeCanary(mux *http.ServeMux) {
	if s.business == nil || len(s.inboundKey) != 32 {
		return
	}
	mux.HandleFunc("POST /api/v1/workspace/native-canary/stage", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, true, true); !ok {
			return
		}
		var input struct {
			Confirmation string `json:"confirmation"`
		}
		if err := decode(w, r, 256, &input); err != nil || input.Confirmation != ownerCanaryConfirmation {
			businessError(w, business.ErrInvalid)
			return
		}
		// This owner-only stage action is NOT an Xray start command. Do
		// not let a LIVE opt-in auto-deploy during multi-step staging.
		if s.meteredNode != "" || s.business.UsageLedger() == nil {
			businessError(w, business.ErrUnavailable)
			return
		}
		installation, ok := s.nativeNode(ownerCanaryNode)
		if !ok {
			businessError(w, business.ErrNotFound)
			return
		}
		online := false
		for _, n := range s.fleet.Snapshot().Nodes {
			if n.NodeID == ownerCanaryNode && n.Status == "online" && n.MetricsCurrent {
				online = true
			}
		}
		if !online {
			businessError(w, business.ErrUnavailable)
			return
		}

		ctx := r.Context()
		clients, err := s.business.ListClients(ctx, "", 200, 0)
		if err != nil {
			businessError(w, err)
			return
		}
		if len(clients) >= 200 {
			businessError(w, business.ErrConflict)
			return
		}
		for _, c := range clients {
			if c.ID != ownerCanaryClient || c.RepresentativeID != "owner" ||
				c.VolumeBytes != ownerCanaryVolume || c.DurationDays != 1 || c.Archived ||
				c.DeploymentState == "disabled" {
				businessError(w, business.ErrConflict)
				return
			}
		}
		inbounds, err := s.business.ListInbounds(ctx, s.inboundKey)
		if err != nil {
			businessError(w, err)
			return
		}
		for _, in := range inbounds {
			if in.ID != ownerCanaryInbound || in.Name != "USA PRO · QA loopback" ||
				in.Archived || !in.Enabled || in.Transport != "tcp" ||
				in.RealityTarget != "www.microsoft.com:443" {
				businessError(w, business.ErrConflict)
				return
			}
		}
		targets, err := s.business.ListInboundTargets(ctx)
		if err != nil {
			businessError(w, err)
			return
		}
		for _, target := range targets {
			if target.InboundID != ownerCanaryInbound || target.NodeID != ownerCanaryNode ||
				target.InstallationID != installation || target.ListenIP != "127.0.0.1" ||
				target.DirectPort != ownerCanaryPort || target.SwapPort != 0 {
				businessError(w, business.ErrConflict)
				return
			}
		}
		// Only the NORMAL audited and idempotent Store functions mutate
		// NEW PRO. Every partial failure remains unusable while the
		// metered runtime flags are OFF; retry preserves exact identities.
		client, err := s.business.CreateClient(ctx, business.NewClient{
			EventID:          "usa-pro-qa-test-20261017-create",
			ClientID:         ownerCanaryClient,
			RepresentativeID: "owner", Name: "USA QA LOOPBACK ONLY",
			DurationDays: 1, IPCount: 1, VolumeBytes: ownerCanaryVolume,
		})
		if err != nil {
			businessError(w, err)
			return
		}
		inbound, err := s.business.CreateInbound(ctx, s.inboundKey, business.InboundInput{
			ID:   ownerCanaryInbound,
			Name: "USA PRO · QA loopback", Transport: "tcp",
			RealityTarget: "www.microsoft.com:443",
			ServerNames:   []string{"www.microsoft.com"},
			ShortIDs:      []string{"ab12"},
		})
		if err != nil {
			businessError(w, err)
			return
		}
		if err = s.business.BindInboundClients(ctx, inbound.ID, []string{client.ID}, true); err != nil {
			businessError(w, err)
			return
		}
		if err = s.business.PutInboundTarget(ctx, business.InboundTarget{
			InboundID: inbound.ID, NodeID: ownerCanaryNode, InstallationID: installation,
			ListenIP: "127.0.0.1", DirectPort: ownerCanaryPort, SwapPort: 0,
		}); err != nil {
			businessError(w, err)
			return
		}
		cfg, err := s.business.BuildNodeConfig(ctx, s.inboundKey, ownerCanaryNode, installation)
		if err != nil {
			businessError(w, err)
			return
		}
		if cfg.UserCount != 1 || cfg.ListenIP != "127.0.0.1" ||
			len(cfg.Candidate.Listeners) != 1 ||
			cfg.Candidate.Listeners[0].NodePort != ownerCanaryPort {
			businessError(w, business.ErrConflict)
			return
		}
		if _, err = xrayconfig.WithUsageAPI(cfg.Candidate, managedxray.MeteredStatsEndpoint); err != nil {
			businessError(w, business.ErrUnavailable)
			return
		}
		account, err := s.business.ClientUsage(ctx, client.ID)
		if err != nil || !account.Known || !account.Eligible || account.Total != 0 ||
			account.Limit != ownerCanaryVolume {
			businessError(w, business.ErrConflict)
			return
		}
		respond(w, http.StatusCreated, map[string]any{
			"stage": "staged_only", "node_id": ownerCanaryNode,
			"client_id": client.ID, "inbound_id": inbound.ID,
			"listen_ip": "127.0.0.1", "port": ownerCanaryPort,
			"volume_bytes":              ownerCanaryVolume,
			"customer_listener_started": false,
			"subscription_published":    false,
			"old_customers_modified":    false,
			"next":                      "separately_reviewed_remote_metered_activation",
		})
	})
}
