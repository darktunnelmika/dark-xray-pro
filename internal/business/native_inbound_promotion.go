package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

const NativeInboundPlanPromotionScope = "single_direct_tcp_reality_loopback"
const MaxNativeInboundPromotionClients = 500

// Promotion prepares one complete stored plan for the existing metered
// executor. It is not an Apply command and never opens a listener itself.
type NativeInboundPlanPromotionInput struct {
	EventID                 string   `json:"event_id"`
	ExpectedPlanRevision    uint64   `json:"expected_plan_revision"`
	ExpectedInboundRevision uint64   `json:"expected_inbound_revision"`
	ClientIDs               []string `json:"client_ids"`
	Reason                  string   `json:"reason"`
	ReviewSHA256            string   `json:"review_sha256,omitempty"`
}

type NativeInboundPlanPromotionBlocker struct {
	NodeID string `json:"node_id,omitempty"`
	Field  string `json:"field"`
	Code   string `json:"code"`
}

type NativeInboundPlanPromotionPreview struct {
	Eligible              bool                                `json:"eligible"`
	Scope                 string                              `json:"scope"`
	InboundID             uint64                              `json:"inbound_id"`
	PlanRevision          uint64                              `json:"plan_revision"`
	InboundRevision       uint64                              `json:"inbound_revision"`
	NodeID                string                              `json:"node_id"`
	InstallationID        string                              `json:"installation_id"`
	DirectPort            uint16                              `json:"direct_port"`
	DirectHosts           []string                            `json:"direct_hosts"`
	ClientIDs             []string                            `json:"client_ids"`
	ReviewSHA256          string                              `json:"review_sha256"`
	Blockers              []NativeInboundPlanPromotionBlocker `json:"blockers"`
	RuntimeChanged        bool                                `json:"runtime_changed"`
	SubscriptionPublished bool                                `json:"subscription_published"`
}

type NativeInboundPlanPromotionReceipt struct {
	EventID               string   `json:"event_id"`
	InboundID             uint64   `json:"inbound_id"`
	PlanRevision          uint64   `json:"plan_revision"`
	SourceInboundRevision uint64   `json:"source_inbound_revision"`
	InboundRevision       uint64   `json:"inbound_revision"`
	NodeID                string   `json:"node_id"`
	InstallationID        string   `json:"installation_id"`
	DraftRevision         uint64   `json:"draft_revision"`
	ClientIDs             []string `json:"client_ids"`
	ReviewSHA256          string   `json:"review_sha256"`
	Promoted              bool     `json:"promoted"`
	RuntimeChanged        bool     `json:"runtime_changed"`
	SubscriptionPublished bool     `json:"subscription_published"`
	CreatedUnix           int64    `json:"created_unix"`
	Replayed              bool     `json:"replayed"`
}

type NativeInboundPlanPromotionState struct {
	Scope    string                              `json:"scope"`
	Promoted bool                                `json:"promoted"`
	Blockers []NativeInboundPlanPromotionBlocker `json:"blockers"`
	Receipt  *NativeInboundPlanPromotionReceipt  `json:"receipt,omitempty"`
}

type promotionAssessment struct {
	preview NativeInboundPlanPromotionPreview
	plan    NativeInboundPlan
	profile xrayconfig.Profile
	channel MeteredRouteChannelView
	clients []Client
}

func (a *promotionAssessment) block(node, field, code string) {
	a.preview.Blockers = append(a.preview.Blockers, NativeInboundPlanPromotionBlocker{NodeID: node, Field: field, Code: code})
}

func normalizedPromotionInput(id uint64, in NativeInboundPlanPromotionInput, commit bool) (NativeInboundPlanPromotionInput, error) {
	if id == 0 || id > routeMax || !eventRE.MatchString(in.EventID) {
		return in, planError("", "event_id", "event_id_invalid", ErrInvalid)
	}
	if in.ExpectedPlanRevision == 0 || in.ExpectedPlanRevision >= routeMax || in.ExpectedInboundRevision == 0 || in.ExpectedInboundRevision >= routeMax {
		return in, planError("", "expected_plan_revision", "revision_invalid", ErrInvalid)
	}
	if !validName(in.Reason, 200) {
		return in, planError("", "reason", "reason_invalid", ErrInvalid)
	}
	if len(in.ClientIDs) > MaxNativeInboundPromotionClients || !validClientIDBatch(in.ClientIDs) {
		return in, planError("", "client_ids", "promotion_clients_invalid", ErrInvalid)
	}
	in.ClientIDs = append([]string(nil), in.ClientIDs...)
	sort.Strings(in.ClientIDs)
	if commit && !routeSHA.MatchString(in.ReviewSHA256) || in.ReviewSHA256 != "" && !routeSHA.MatchString(in.ReviewSHA256) {
		return in, planError("", "review_sha256", "promotion_review_required", ErrInvalid)
	}
	return in, nil
}

func promotionAAD(id uint64, event string) string {
	return fmt.Sprintf("inbound-plan-promotion-v1:%d:%s", id, event)
}

func promotionDraftEvent(event string, id uint64) string {
	return "promotion-draft-" + fingerprint("native-plan-promotion-draft-v1", struct {
		Event string
		ID    uint64
	}{event, id})
}

func readNativeInboundPromotion(ctx context.Context, q planReader, key []byte, id uint64) (*NativeInboundPlanPromotionReceipt, string, error) {
	var event, hash, node, installation, draftEvent string
	var planRevision, sourceRevision, revision uint64
	var directPort uint16
	var created int64
	var encrypted, inputEncrypted []byte
	err := q.QueryRowContext(ctx, `SELECT event_id,fingerprint,plan_revision,source_inbound_revision,inbound_revision,node_id,installation_id,direct_port,draft_event_id,created_unix,input_enc,receipt_enc FROM native_inbound_plan_promotions WHERE inbound_id=?`, id).
		Scan(&event, &hash, &planRevision, &sourceRevision, &revision, &node, &installation, &directPort, &draftEvent, &created, &inputEncrypted, &encrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var receipt NativeInboundPlanPromotionReceipt
	if err = openPlanValue(key, promotionAAD(id, event), encrypted, &receipt); err != nil {
		return nil, "", err
	}
	var input NativeInboundPlanPromotionInput
	if err = openPlanValue(key, promotionAAD(id, event)+":input", inputEncrypted, &input); err != nil {
		return nil, "", err
	}
	normalized, err := normalizedPromotionInput(id, input, true)
	if err != nil || !reflect.DeepEqual(input, normalized) || input.EventID != event || input.ExpectedPlanRevision != planRevision || input.ExpectedInboundRevision != sourceRevision || !reflect.DeepEqual(input.ClientIDs, receipt.ClientIDs) || input.ReviewSHA256 != receipt.ReviewSHA256 || hash != promotionFingerprint(id, input) {
		return nil, "", ErrUnavailable
	}
	if receipt.InboundID != id || receipt.EventID != event || receipt.PlanRevision != planRevision || receipt.SourceInboundRevision != sourceRevision ||
		receipt.InboundRevision != revision || revision != sourceRevision+1 || receipt.NodeID != node || receipt.InstallationID != installation ||
		receipt.DraftRevision != 1 || receipt.CreatedUnix != created || receipt.CreatedUnix <= 0 || !receipt.Promoted || receipt.Replayed ||
		receipt.RuntimeChanged || receipt.SubscriptionPublished || !routeSHA.MatchString(receipt.ReviewSHA256) || !routeSHA.MatchString(hash) ||
		draftEvent != promotionDraftEvent(event, id) || !validClientIDBatch(receipt.ClientIDs) || !sort.StringsAreSorted(receipt.ClientIDs) {
		return nil, "", ErrUnavailable
	}
	plan, err := readNativeInboundPlan(ctx, q, key, id)
	if err != nil {
		return nil, "", err
	}
	if plan.PlanRevision != planRevision || plan.InboundRevision != sourceRevision || len(plan.Nodes) != 1 || plan.Nodes[0].NodeID != node || plan.Nodes[0].InstallationID != installation || plan.Nodes[0].DirectPort != directPort {
		return nil, "", ErrUnavailable
	}
	rows, err := q.QueryContext(ctx, `SELECT client_id FROM native_inbound_plan_promotion_clients WHERE inbound_id=? ORDER BY client_id`, id)
	if err != nil {
		return nil, "", err
	}
	clients := []string{}
	for rows.Next() {
		var client string
		if err = rows.Scan(&client); err != nil {
			rows.Close()
			return nil, "", err
		}
		clients = append(clients, client)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, "", err
	}
	if !reflect.DeepEqual(clients, receipt.ClientIDs) {
		return nil, "", ErrUnavailable
	}
	return &receipt, hash, nil
}

func promotionFingerprint(id uint64, input NativeInboundPlanPromotionInput) string {
	return fingerprint("native-plan-promotion-input-v1", struct {
		ID    uint64
		Input NativeInboundPlanPromotionInput
	}{id, input})
}

func rejectPromotedInboundMutation(ctx context.Context, q inboundReader, id uint64) error {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_inbound_plan_promotions WHERE inbound_id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return planError("", "profile", "plan_promoted", ErrConflict)
	}
	return nil
}

func (s *Store) NativeInboundPlanPromotionState(ctx context.Context, key []byte, id uint64) (NativeInboundPlanPromotionState, error) {
	out := NativeInboundPlanPromotionState{Scope: NativeInboundPlanPromotionScope, Blockers: []NativeInboundPlanPromotionBlocker{}}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	receipt, _, err := readNativeInboundPromotion(ctx, tx, key, id)
	if err != nil {
		return out, err
	}
	if receipt != nil {
		out.Promoted, out.Receipt = true, receipt
		return out, nil
	}
	a, err := s.assessNativeInboundPromotion(ctx, tx, key, id, nil)
	out.Blockers = a.preview.Blockers
	return out, err
}

func (s *Store) PreviewNativeInboundPlanPromotion(ctx context.Context, key []byte, id uint64, input NativeInboundPlanPromotionInput) (NativeInboundPlanPromotionPreview, error) {
	in, err := normalizedPromotionInput(id, input, false)
	if err != nil {
		return NativeInboundPlanPromotionPreview{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return NativeInboundPlanPromotionPreview{}, err
	}
	defer tx.Rollback()
	a, err := s.assessNativeInboundPromotion(ctx, tx, key, id, &in)
	return a.preview, err
}

func (s *Store) assessNativeInboundPromotion(ctx context.Context, tx *sql.Tx, key []byte, id uint64, in *NativeInboundPlanPromotionInput) (promotionAssessment, error) {
	a := promotionAssessment{preview: NativeInboundPlanPromotionPreview{Scope: NativeInboundPlanPromotionScope, InboundID: id,
		DirectHosts: []string{}, ClientIDs: []string{}, Blockers: []NativeInboundPlanPromotionBlocker{}}}
	plan, err := readNativeInboundPlan(ctx, tx, key, id)
	if err != nil {
		return a, err
	}
	a.plan = plan
	a.preview.PlanRevision, a.preview.InboundRevision = plan.PlanRevision, plan.InboundRevision
	if receipt, _, err := readNativeInboundPromotion(ctx, tx, key, id); err != nil {
		return a, err
	} else if receipt != nil {
		a.block(receipt.NodeID, "profile", "plan_promoted")
		return a, nil
	}
	if in != nil {
		a.preview.ClientIDs = append([]string(nil), in.ClientIDs...)
		if in.ExpectedPlanRevision != plan.PlanRevision || in.ExpectedInboundRevision != plan.InboundRevision {
			a.block("", "expected_plan_revision", "promotion_revision_changed")
		}
	}
	_, profile, err := editablePlanProfile(ctx, tx, key, plan)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			a.block("", "profile", "profile_changed_or_materialized")
			return a, nil
		}
		return a, err
	}
	a.profile = profile
	if profile.Security != "" || profile.Transport != "tcp" || profile.TLS != nil {
		a.block("", "profile.transport", "promotion_transport_unsupported")
	}
	if profile.Options != nil {
		a.block("", "profile.options", "promotion_options_unsupported")
	}
	if len(plan.Nodes) != 1 {
		a.block("", "nodes", "promotion_single_node_required")
		return a, nil
	}
	node := plan.Nodes[0]
	a.preview.NodeID, a.preview.InstallationID, a.preview.DirectPort = node.NodeID, node.InstallationID, node.DirectPort
	a.preview.DirectHosts = append([]string{}, node.DirectHosts...)
	if node.ListenIP != "127.0.0.1" {
		a.block(node.NodeID, "listen_ip", "promotion_loopback_required")
	}
	if node.DirectPort == 0 || !planListenerPort(node.DirectPort) {
		a.block(node.NodeID, "direct_port", "promotion_direct_required")
	}
	if node.TunnelPort != 0 || len(node.TunnelHosts) != 0 || len(node.TunnelEndpoints) != 0 || node.SwapEnabled || node.SwapSourceNodeID != "" || node.SwapSourceInstallationID != "" || node.SwapSourceListenIP != "" || node.SwapSourcePort != 0 || node.SwapEntryAddress != "" || node.SwapEntryPort != 0 || node.SwapDestinationPort != 0 {
		a.block(node.NodeID, "tunnel_port", "promotion_direct_only")
	}
	if len(node.DirectEndpoints) != 0 {
		a.block(node.NodeID, "direct_endpoints", "promotion_endpoints_unsupported")
	}
	if plan.Status != "SAVED_DISABLED" || len(node.DirectHosts) == 0 {
		a.block(node.NodeID, "direct_hosts", "promotion_hosts_required")
	}
	if s.usage == nil {
		a.block(node.NodeID, "node_id", "promotion_metering_unavailable")
		return a, nil
	}
	var installation, binary string
	err = tx.QueryRowContext(ctx, `SELECT installation_id,binary_sha FROM native_inbound_plan_promotion_nodes WHERE node_id=?`, node.NodeID).Scan(&installation, &binary)
	if errors.Is(err, sql.ErrNoRows) || err == nil && installation != node.InstallationID {
		a.block(node.NodeID, "node_id", "promotion_node_not_reserved")
	} else if err != nil {
		return a, err
	} else {
		channel, serial, err := meteredRouteObservation(ctx, tx, node.NodeID)
		if err != nil {
			return a, err
		}
		a.channel = channel
		if channel.InstallationID != installation || channel.BinarySHA != binary {
			return a, ErrUnavailable
		}
		status, observed := channel.Status, channel.Status.Observed
		if !channel.ObservationFresh {
			a.block(node.NodeID, "node_id", "promotion_observation_stale")
		} else if serial != 0 || status.Serial != 0 || !routeID.MatchString(status.Instance) || status.Usage == nil || *status.Usage != (mx.MeteredCondition{}) || status.StagedID != "" || status.CurrentStageID != "" || status.PreviousStageID != "" || status.InterruptedCommandSHA != "" || mx.Condition(observed) != (mx.RuntimeCondition{}) || observed.PreviousSHA != "" || observed.LeaseExpires != 0 || observed.Usage != nil || observed.Phase != "awaiting_fresh_lease" || observed.DataPlaneVerified || observed.SubscriptionPublished {
			a.block(node.NodeID, "node_id", "promotion_runtime_not_empty")
		}
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM native_inbound_targets WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_route_drafts WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_metered_route_commands WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_runtime_delivery WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_node_revisions WHERE node_id=?`,
		`SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=?`,
		`SELECT COUNT(*) FROM native_inbound_plan_promotions WHERE node_id=?`,
	} {
		var count int
		if err = tx.QueryRowContext(ctx, query, node.NodeID).Scan(&count); err != nil {
			return a, err
		}
		if count != 0 {
			a.block(node.NodeID, "node_id", "promotion_node_not_empty")
			break
		}
	}
	// Reservations belonging to other paused plans still reserve their ports.
	if len(a.preview.Blockers) == 0 {
		if err = verifyPlanCollisions(ctx, tx, key, NativeInboundPlanInput{Profile: plan.Profile.InboundInput, Nodes: plan.Nodes}); err != nil {
			var detail *NativeInboundPlanError
			if errors.As(err, &detail) {
				a.block(detail.NodeID, detail.Field, detail.Code)
			} else {
				return a, err
			}
		}
	}
	if in == nil {
		return a, nil
	}
	for _, id := range in.ClientIDs {
		client, err := readClient(ctx, tx, id)
		if errors.Is(err, sql.ErrNoRows) {
			a.block("", "client_ids", "promotion_client_ineligible")
			continue
		}
		if err != nil {
			return a, err
		}
		var eligible int
		if err = tx.QueryRowContext(ctx, `SELECT eligible FROM pro_usage_eligibility WHERE client_id=?`, id).Scan(&eligible); err != nil {
			return a, err
		}
		if eligible != 1 || client.Archived || client.DeploymentState == "disabled" || client.ExpiresUnix <= time.Now().Unix() {
			a.block("", "client_ids", "promotion_client_ineligible")
		}
		a.clients = append(a.clients, client)
	}
	if len(a.preview.Blockers) != 0 {
		return a, nil
	}
	profile.Users = make([]xrayconfig.User, 0, len(a.clients))
	for _, client := range a.clients {
		email, err := uc.CounterEmail(client.ID)
		if err != nil {
			return a, ErrUnavailable
		}
		profile.Users = append(profile.Users, xrayconfig.User{UUID: client.UUID, Email: email})
	}
	candidate, err := xrayconfig.RenderAssigned(node.NodeID, node.InstallationID, node.ListenIP, []xrayconfig.Assignment{{Profile: profile, DirectPort: node.DirectPort}})
	if err != nil {
		return a, ErrUnavailable
	}
	candidate, err = xrayconfig.WithUsageAPI(candidate, mx.MeteredStatsEndpoint)
	if err != nil {
		return a, ErrUnavailable
	}
	if _, _, _, err = xrayconfig.UsageBootstrap(candidate); err != nil {
		return a, ErrUnavailable
	}
	a.preview.Eligible = true
	// Poll timestamps are deliberately excluded: a newer identical signed
	// observation refreshes liveness without changing the reviewed operation.
	intent := *in
	intent.ReviewSHA256 = ""
	a.preview.ReviewSHA256 = fingerprint("native-plan-promotion-review-v1", struct {
		Plan                                               NativeInboundPlan
		Input                                              NativeInboundPlanPromotionInput
		Clients                                            []Client
		Binary, RuntimeInstance, CandidateSHA, ManifestSHA string
	}{plan, intent, a.clients, a.channel.BinarySHA, a.channel.Status.Instance, candidate.SHA256, candidate.ManifestSHA256})
	return a, nil
}

func (s *Store) PromoteNativeInboundPlan(ctx context.Context, key []byte, id uint64, input NativeInboundPlanPromotionInput) (NativeInboundPlanPromotionReceipt, error) {
	var empty NativeInboundPlanPromotionReceipt
	in, err := normalizedPromotionInput(id, input, true)
	if err != nil {
		return empty, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	fp := promotionFingerprint(id, in)
	var priorID uint64
	err = tx.QueryRowContext(ctx, `SELECT inbound_id FROM native_inbound_plan_promotions WHERE event_id=?`, in.EventID).Scan(&priorID)
	if err == nil {
		receipt, hash, err := readNativeInboundPromotion(ctx, tx, key, priorID)
		if err != nil {
			return empty, err
		}
		if receipt == nil || priorID != id || hash != fp {
			return empty, ErrConflict
		}
		receipt.Replayed = true
		return *receipt, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	a, err := s.assessNativeInboundPromotion(ctx, tx, key, id, &in)
	if err != nil {
		return empty, err
	}
	if !a.preview.Eligible || len(a.preview.Blockers) != 0 {
		if len(a.preview.Blockers) != 0 {
			b := a.preview.Blockers[0]
			return empty, planError(b.NodeID, b.Field, b.Code, ErrConflict)
		}
		return empty, ErrConflict
	}
	if in.ReviewSHA256 != a.preview.ReviewSHA256 {
		return empty, planError(a.preview.NodeID, "review_sha256", "promotion_review_changed", ErrConflict)
	}
	for _, table := range []string{"events", "client_operations", "client_profile_edits", "representative_control_events", "catalog_events", "catalog_policy_events", "native_inbound_operations", "native_inbound_plan_events", "native_route_draft_events"} {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE event_id=?", in.EventID).Scan(&count); err != nil {
			return empty, err
		}
		if count != 0 {
			return empty, ErrConflict
		}
	}
	node := a.plan.Nodes[0]
	now := time.Now().Unix()
	out := NativeInboundPlanPromotionReceipt{EventID: in.EventID, InboundID: id, PlanRevision: a.plan.PlanRevision,
		SourceInboundRevision: a.plan.InboundRevision, InboundRevision: a.plan.InboundRevision + 1, NodeID: node.NodeID,
		InstallationID: node.InstallationID, DraftRevision: 1, ClientIDs: append([]string(nil), in.ClientIDs...),
		ReviewSHA256: in.ReviewSHA256, Promoted: true, CreatedUnix: now}
	enc, err := sealPlanValue(key, promotionAAD(id, in.EventID), out)
	if err != nil {
		return empty, err
	}
	inputEnc, err := sealPlanValue(key, promotionAAD(id, in.EventID)+":input", in)
	if err != nil {
		return empty, err
	}
	draftEvent := promotionDraftEvent(in.EventID, id)
	if _, err = tx.ExecContext(ctx, `INSERT INTO native_inbound_plan_promotions(inbound_id,event_id,fingerprint,plan_revision,source_inbound_revision,inbound_revision,node_id,installation_id,direct_port,draft_event_id,created_unix,input_enc,receipt_enc) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, in.EventID, fp, out.PlanRevision, out.SourceInboundRevision, out.InboundRevision, node.NodeID, node.InstallationID, node.DirectPort, draftEvent, now, inputEnc, enc); err != nil {
		return empty, err
	}
	for _, client := range in.ClientIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO native_inbound_plan_promotion_clients(inbound_id,client_id) VALUES(?,?)`, id, client); err != nil {
			return empty, err
		}
	}
	result, err := tx.ExecContext(ctx, `UPDATE native_inbounds SET enabled=1,revision=? WHERE id=? AND revision=? AND enabled=0 AND archived=0`, out.InboundRevision, id, out.SourceInboundRevision)
	if err != nil {
		return empty, err
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		if err != nil {
			return empty, err
		}
		return empty, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO native_inbound_targets(inbound_id,node_id,installation_id,listen_ip,direct_port,swap_port) VALUES(?,?,?,?,?,0)`, id, node.NodeID, node.InstallationID, node.ListenIP, node.DirectPort); err != nil {
		return empty, err
	}
	for _, client := range in.ClientIDs {
		if _, err = tx.ExecContext(ctx, `INSERT INTO native_inbound_clients(inbound_id,client_id) VALUES(?,?)`, id, client); err != nil {
			return empty, err
		}
	}
	draft := NativeRouteDraft{NativeRouteDraftInput: NativeRouteDraftInput{EventID: draftEvent, InboundID: id, NodeID: node.NodeID,
		InstallationID: node.InstallationID, ExpectedInboundRevision: out.InboundRevision, DirectPort: node.DirectPort,
		DirectHosts: append([]string{}, node.DirectHosts...), TunnelHosts: []string{}, Reason: in.Reason},
		DraftRevision: 1, ListenIP: node.ListenIP, NativeDirectPort: node.DirectPort, BillingSourceNodeID: node.NodeID, CreatedUnix: now, UpdatedUnix: now}
	raw, err := json.Marshal(draft)
	if err != nil {
		return empty, err
	}
	draftEnc, err := sealRouteDraft(key, id, node.NodeID, 1, raw)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO native_route_drafts(inbound_id,node_id,installation_id,revision,inbound_revision,listen_ip,original_direct_port,original_swap_port,direct_port,tunnel_port,swap_node_port,last_event_id,payload_enc,created_unix,updated_unix) VALUES(?,?,?,1,?,?,?,0,?,0,0,?,?,?,?)`,
		id, node.NodeID, node.InstallationID, out.InboundRevision, node.ListenIP, node.DirectPort, node.DirectPort, draftEvent, draftEnc, now, now); err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO native_route_draft_events(event_id,fingerprint,inbound_id,node_id,revision,created_unix) VALUES(?,?,?,?,1,?)`, draftEvent, fingerprint("native-route-draft-v1", draft.NativeRouteDraftInput), id, node.NodeID, now); err != nil {
		return empty, err
	}
	// Verify the same ordinary metered compiler and references that Stage will
	// use, inside the writer transaction, before any prepared rows become visible.
	if _, err = meteredRouteCandidate(ctx, tx, key, node.NodeID, node.InstallationID, []RouteDraftRef{{InboundID: id, InboundRevision: out.InboundRevision, DraftRevision: 1}}); err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return out, nil
}
