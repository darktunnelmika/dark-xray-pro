package api

import (
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
)

// NativeReadiness is owner-only explanatory state. It is intentionally NOT an
// authorization token, admission receipt, signed lease, or remote Apply gate.
// No browser-facing step can make a customer listener eligible for runtime.
type NativeReadiness struct {
	Scope                     string `json:"scope"`
	PairedNodes               int    `json:"paired_nodes"`
	OnlineNodes               int    `json:"online_nodes"`
	FreshNodes                int    `json:"fresh_nodes"`
	ConfiguredInbounds        int    `json:"configured_inbounds"`
	ConfiguredTargets         int    `json:"configured_targets"`
	ClientBindings            int    `json:"client_bindings"`
	UsageLedgerEnabled        bool   `json:"usage_ledger_enabled"`
	RemoteMeteredAdmission    string `json:"remote_metered_admission"`
	SubscriptionPublished     bool   `json:"subscription_published"`
	CustomerDataPlaneVerified bool   `json:"customer_data_plane_verified"`
	DeploymentReady           bool   `json:"deployment_ready"`
	BlockingReason            string `json:"blocking_reason"`
}

// This assessment uses only data already cached in memory or read by the
// native owner request. It never contacts Nodes, reads /proc, issues leases,
// creates customer balances, or writes application state.
func (s *Server) nativeReadiness(inbounds []business.NativeInbound, targets []business.InboundTarget, nodes []noderegistry.NodeInfo) NativeReadiness {
	r := NativeReadiness{
		Scope:                  "new_pro_lab_only",
		PairedNodes:            len(nodes),
		ConfiguredTargets:      len(targets),
		RemoteMeteredAdmission: "trusted_remote_epoch_registration_pending",
		BlockingReason:         "remote_metered_admission_not_wired",
	}
	if s.business != nil {
		r.UsageLedgerEnabled = s.business.UsageLedger() != nil
	}
	for _, inbound := range inbounds {
		if inbound.Archived || !inbound.Enabled {
			continue
		}
		r.ConfiguredInbounds++
		r.ClientBindings += inbound.BoundClients
	}
	known := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		known[node.NodeID] = true
	}
	for _, node := range s.fleet.Snapshot().Nodes {
		if !known[node.NodeID] || node.Status != "online" {
			continue
		}
		r.OnlineNodes++
		if node.MetricsCurrent {
			r.FreshNodes++
		}
	}
	// Business eligibility and signed reports do not prove a real customer
	// tunnel/Internet route. Importantly, the metered native Agent delivery
	// endpoint STILL RETURNS 503 until trusted distributed process-birth and
	// zero-receipt admission are implemented; never infer deployment_ready.
	return r
}
