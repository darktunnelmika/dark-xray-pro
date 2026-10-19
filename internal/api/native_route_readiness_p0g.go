package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

// RouteReadiness is a strictly advisory TWO-HOP snapshot. Even a fresh,
// authenticated native Agent report is not evidence of this *proposed*
// route, multi-hop forwarding, metering, or safe operational Apply.
type RouteHopReadiness struct {
	NodeID                 string `json:"node_id"`
	PairingMatched         bool   `json:"pairing_matched"`
	Online                 bool   `json:"online"`
	MetricsCurrent         bool   `json:"metrics_current"`
	NativeRuntimeState     string `json:"native_runtime_state"`
	NativeProcessReady     bool   `json:"native_process_ready"`
	NativeReportedAt       string `json:"native_reported_at,omitempty"`
	NativeDesiredSequence  uint64 `json:"native_desired_sequence"`
	NativeReportedSequence uint64 `json:"native_reported_sequence"`
}
type NativeRouteReadiness struct {
	Scope                     string             `json:"scope"`
	AsOfUTC                   string             `json:"as_of_utc"`
	InboundID                 uint64             `json:"inbound_id"`
	NodeID                    string             `json:"node_id"`
	DraftRevision             uint64             `json:"draft_revision"`
	SwapEnabled               bool               `json:"swap_enabled"`
	PlanConsistent            bool               `json:"plan_consistent"`
	Target                    RouteHopReadiness  `json:"target"`
	Upstream                  *RouteHopReadiness `json:"upstream,omitempty"`
	Blockers                  []string           `json:"blockers"`
	ApplyAllowed              bool               `json:"apply_allowed"`
	RouteSignedAckVerified    bool               `json:"route_signed_ack_verified"`
	SwapForwardingVerified    bool               `json:"swap_forwarding_verified"`
	CustomerDataPlaneVerified bool               `json:"customer_data_plane_verified"`
	UniqueBillingVerified     bool               `json:"unique_billing_verified"`
	AutomaticRollbackVerified bool               `json:"automatic_rollback_verified"`
	SubscriptionPublished     bool               `json:"subscription_published"`
}

func routeHopFromState(ctx context.Context, s *Server, nodeID, installation string,
	paired map[string]noderegistry.NodeInfo, cached map[string]telemetry.Node) (RouteHopReadiness, error) {
	out := RouteHopReadiness{NodeID: nodeID, NativeRuntimeState: "unavailable"}
	info, exists := paired[nodeID]
	out.PairingMatched = exists && info.InstallationID == installation && installation != ""
	if observed, ok := cached[nodeID]; ok && out.PairingMatched {
		out.Online = observed.Status == "online"
		out.MetricsCurrent = out.Online && observed.MetricsCurrent
	}
	if !out.PairingMatched {
		return out, nil
	}
	native, err := s.business.NativeRuntimeStatus(ctx, nodeID, installation)
	if err != nil {
		return out, err
	}
	out.NativeRuntimeState = native.State
	// A native runtime report can certify only that the *original* native
	// desired config was observed. It CANNOT ACK an unapplied route draft.
	out.NativeProcessReady = native.State == "applied" && native.ProcessReady
	out.NativeReportedAt = native.ReportedAt
	out.NativeDesiredSequence = native.DesiredSequence
	out.NativeReportedSequence = native.ReportedSequence
	return out, nil
}

func (s *Server) routeDraftReadiness(ctx context.Context, inboundID uint64, nodeID string) (NativeRouteReadiness, error) {
	report := NativeRouteReadiness{
		Scope: "new_pro_lab_read_only", AsOfUTC: time.Now().UTC().Format(time.RFC3339Nano),
		InboundID: inboundID, NodeID: nodeID, Blockers: make([]string, 0, 12),
	}
	draft, err := s.business.NativeRouteDraft(ctx, s.inboundKey, inboundID, nodeID)
	if err != nil {
		return report, err
	}
	report.DraftRevision = draft.DraftRevision
	report.SwapEnabled = draft.SwapEnabled
	registry := s.registry.List()
	paired := make(map[string]noderegistry.NodeInfo, len(registry))
	for _, n := range registry {
		paired[n.NodeID] = n
	}
	snapshot := s.fleet.Snapshot()
	cached := make(map[string]telemetry.Node, len(snapshot.Nodes))
	for _, n := range snapshot.Nodes {
		cached[n.NodeID] = n
	}
	bindingUnchanged := true
	target, err := s.business.NativeInboundTarget(ctx, inboundID, nodeID)
	switch {
	case errors.Is(err, business.ErrNotFound):
		bindingUnchanged = false
		report.Blockers = append(report.Blockers, "target_attachment_missing")
	case err != nil:
		return report, err
	default:
		if target.InstallationID != draft.InstallationID ||
			target.ListenIP != draft.ListenIP ||
			target.DirectPort != draft.NativeDirectPort ||
			target.SwapPort != draft.NativeSwapPort {
			bindingUnchanged = false
			report.Blockers = append(report.Blockers, "target_attachment_changed")
		}
	}
	report.Target, err = routeHopFromState(ctx, s, nodeID, draft.InstallationID, paired, cached)
	if err != nil {
		return report, err
	}
	if !report.Target.PairingMatched {
		report.Blockers = append(report.Blockers, "target_pairing_changed")
	}
	if !report.Target.Online {
		report.Blockers = append(report.Blockers, "target_agent_offline")
	}
	if !report.Target.MetricsCurrent {
		report.Blockers = append(report.Blockers, "target_metrics_stale")
	}
	if !report.Target.NativeProcessReady {
		report.Blockers = append(report.Blockers, "target_native_runtime_not_ready")
	}
	if draft.SwapEnabled {
		upstreamID := draft.SwapUpstreamNodeID
		upstream, lookupErr := s.business.NativeInboundTarget(ctx, inboundID, upstreamID)
		if errors.Is(lookupErr, business.ErrNotFound) {
			report.Blockers = append(report.Blockers, "upstream_attachment_missing")
			report.Upstream = &RouteHopReadiness{NodeID: upstreamID, NativeRuntimeState: "unavailable"}
		} else if lookupErr != nil {
			return report, lookupErr
		} else {
			observed, e := routeHopFromState(ctx, s, upstreamID, upstream.InstallationID, paired, cached)
			if e != nil {
				return report, e
			}
			report.Upstream = &observed
			if !observed.PairingMatched {
				report.Blockers = append(report.Blockers, "upstream_pairing_changed")
			}
			if !observed.Online {
				report.Blockers = append(report.Blockers, "upstream_agent_offline")
			}
			if !observed.MetricsCurrent {
				report.Blockers = append(report.Blockers, "upstream_metrics_stale")
			}
			if !observed.NativeProcessReady {
				report.Blockers = append(report.Blockers, "upstream_native_runtime_not_ready")
			}
		}
	}
	// A draft may have become invalid AFTER it was stored (target replaced,
	// upstream changed, different same-Inbound draft, new cyclic graph).
	// Authenticated current state is always revalidated server-side.
	proposed := draft.NativeRouteDraftInput
	proposed.ExpectedDraftRevision = draft.DraftRevision
	preview, err := s.business.PreviewNativeRouteDraft(ctx, s.inboundKey, proposed)
	switch {
	case err == nil:
		upstreamPaired := report.Upstream == nil || report.Upstream.PairingMatched
		report.PlanConsistent = preview.Valid && !preview.ApplyAllowed &&
			bindingUnchanged && report.Target.PairingMatched && upstreamPaired
		if !report.PlanConsistent {
			report.Blockers = append(report.Blockers, "plan_not_consistent")
		}
	case errors.Is(err, business.ErrConflict), errors.Is(err, business.ErrNotFound), errors.Is(err, business.ErrInvalid):
		report.Blockers = append(report.Blockers, "plan_stale_or_conflicting")
	default:
		// Corrupt/authentication-failed encrypted topology is NOT an ordinary
		// block status. Let owner know the private snapshot cannot be trusted.
		return report, err
	}
	// Intentionally unconditional: the existing native RuntimeStatus refers
	// only to its *old* Native desired config, NOT this draft. No browser flag
	// can grant stage/Apply, unique metering or subscription publication.
	report.Blockers = append(report.Blockers, "route_stage_receipt_missing")
	if draft.SwapEnabled {
		report.Blockers = append(report.Blockers, "swap_forwarding_unverified")
	}
	report.Blockers = append(report.Blockers, "customer_data_plane_unverified",
		"unique_billing_unverified", "rollback_unproven")
	return report, nil
}
func (s *Server) attachRouteReadinessP0G(mux *http.ServeMux) {
	if s.business == nil || len(s.inboundKey) != 32 {
		return
	}
	mux.HandleFunc("GET /api/v1/workspace/native-inbounds/{id}/route-drafts/{node}/readiness", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.authorizeBrowser(w, r, false, true); !ok {
			return
		}
		id, err := strconv.ParseUint(r.PathValue("id"), 10, 53)
		node := r.PathValue("node")
		if err != nil || id == 0 || id > (1<<53-1) || len(node) == 0 || len(node) > 64 {
			businessError(w, business.ErrInvalid)
			return
		}
		result, err := s.routeDraftReadiness(r.Context(), id, node)
		if err != nil {
			businessError(w, err)
			return
		}
		respond(w, http.StatusOK, result)
	})
}
