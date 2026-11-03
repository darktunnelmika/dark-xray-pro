package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

const DarkRestorePreparedDeliveryScopeV1 = "darkrestore_prepared_not_applied_v1"

type DarkRestorePreparedBindingV1 struct {
	InboundID       uint64 `json:"inbound_id"`
	PlanRevision    uint64 `json:"plan_revision"`
	InboundRevision uint64 `json:"inbound_revision"`
	AuthoritySHA    string `json:"authority_sha256"`
	ContentSHA      string `json:"content_sha256"`
	Format          string `json:"format"`
}
type DarkRestorePreparedDeliveryInputV1 struct {
	EventID                 string
	InboundID               uint64
	ExpectedPlanRevision    uint64
	ExpectedInboundRevision uint64
	Format                  string
}
type DarkRestorePreparedDeliveryV1 struct {
	Receipt      DarkRestoreCutoverReceiptV1 `json:"receipt"`
	Subscription ix.Subscription             `json:"subscription"`
	Scope        string                      `json:"scope"`
	Applied      bool                        `json:"applied"`
}

func executionClientBinding(inbound uint64, client string) (string, string, error) {
	id := "exec-" + ix.BytesSHA([]byte(fmt.Sprintf("%d\x00%s", inbound, client)))[:32]
	email, err := uc.CounterEmail(id)
	return id, email, err
}

func preparedPlanAuthority(ctx context.Context, q sellerQueryV2, id uint64) (DarkRestorePreparedBindingV1, error) {
	b := DarkRestorePreparedBindingV1{InboundID: id}
	var planEnc, profileEnc []byte
	var current uint64
	var archived bool
	var status string
	err := q.QueryRowContext(ctx, `SELECT p.revision,p.inbound_revision,p.status,p.payload_enc,i.revision,i.profile_enc,i.archived FROM native_inbound_plans p JOIN native_inbounds i ON i.id=p.inbound_id WHERE p.inbound_id=?`, id).Scan(&b.PlanRevision, &b.InboundRevision, &status, &planEnc, &current, &profileEnc, &archived)
	if err != nil {
		return b, err
	}
	if archived || status != "SAVED_DISABLED" || current != b.InboundRevision || b.PlanRevision == 0 || b.PlanRevision >= routeMax || b.InboundRevision == 0 || b.InboundRevision >= routeMax || len(planEnc) == 0 || len(profileEnc) == 0 {
		return b, ErrDarkRestoreCutover
	}
	b.AuthoritySHA, err = cutoverSnapshotDigest("DARK-PRO-PREPARED-PLAN-AUTHORITY-v1", struct {
		InboundID, PlanRevision, InboundRevision uint64
		PlanSHA, ProfileSHA                      string
	}{id, b.PlanRevision, b.InboundRevision, ix.BytesSHA(planEnc), ix.BytesSHA(profileEnc)})
	return b, err
}
func validPreparedBinding(b DarkRestorePreparedBindingV1) bool {
	format := b.Format == "uri" || b.Format == "json" || b.Format == "xray" || b.Format == "wireguard"
	return b.InboundID > 0 && b.InboundID < routeMax && b.PlanRevision > 0 && b.PlanRevision < routeMax && b.InboundRevision > 0 && b.InboundRevision < routeMax && routeSHA.MatchString(b.AuthoritySHA) && routeSHA.MatchString(b.ContentSHA) && format
}
func verifyPreparedPlanReceipt(ctx context.Context, q sellerQueryV2, receipt *DarkRestoreCutoverReceiptV1) error {
	if receipt == nil || receipt.SubscriptionDeliveryScope != DarkRestorePreparedDeliveryScopeV1 || !validPreparedBinding(receipt.Prepared) {
		return ErrDarkRestoreCutover
	}
	current, err := preparedPlanAuthority(ctx, q, receipt.Prepared.InboundID)
	if err != nil || current.PlanRevision != receipt.Prepared.PlanRevision || current.InboundRevision != receipt.Prepared.InboundRevision || current.AuthoritySHA != receipt.Prepared.AuthoritySHA || receipt.SubscriptionSnapshotRevision != current.PlanRevision {
		return ErrDarkRestoreCutover
	}
	if receipt.SubscriptionDeliveredNano <= receipt.TransitionOpenedUnixNano || receipt.SubscriptionDeliveredNano > time.Now().UnixNano() {
		return ErrDarkRestoreCutover
	}
	return nil
}

// Use the initial-Apply codec for the single loopback profile accepted by
// metered promotion. Every extra endpoint or authoring option needs its own
// correspondence proof before a prepared export can describe that profile.
func renderPreparedLoopbackReality(plan NativeInboundPlan, c Client, format string) (ix.Subscription, error) {
	p := plan.Profile
	if format != "uri" || p.Transport != "tcp" || p.Security != "" || p.Options != nil || p.TLS != nil || len(p.ServerNames) == 0 || len(p.ShortIDs) == 0 || len(plan.Nodes) != 1 {
		return ix.Subscription{}, ErrDarkRestoreCutover
	}
	n := plan.Nodes[0]
	expected := NativeInboundPlanNode{NodeID: n.NodeID, InstallationID: n.InstallationID, ListenIP: "127.0.0.1", DirectPort: n.DirectPort, DirectHosts: []string{"127.0.0.1"}, TunnelHosts: n.TunnelHosts, DirectEndpoints: n.DirectEndpoints, TunnelEndpoints: n.TunnelEndpoints}
	if !planListenerPort(n.DirectPort) || len(n.TunnelHosts)+len(n.DirectEndpoints)+len(n.TunnelEndpoints) != 0 || !reflect.DeepEqual(n, expected) {
		return ix.Subscription{}, ErrDarkRestoreCutover
	}
	label := fmt.Sprintf("LAB direct %d", p.ID)
	route := subscriptioncodec.Route{InboundID: p.ID, Kind: "direct", Host: n.ListenIP, Port: n.DirectPort, Label: label, UUID: c.UUID, Transport: "tcp", ServerName: p.ServerNames[0], ShortID: p.ShortIDs[0], PublicKey: p.PublicKey}
	raw, err := subscriptioncodec.Render([]subscriptioncodec.Route{route}, subscriptioncodec.FormatRaw)
	if err != nil {
		return ix.Subscription{}, err
	}
	return ix.Subscription{Version: ix.Version, ClientID: c.ID, Format: format, ContentType: "text/plain; charset=utf-8", Content: raw, SHA256: ix.BytesSHA(raw), ExpiresUnixNano: c.ExpiresUnix * int64(time.Second), Routes: []ix.SubscriptionRoute{{InboundID: p.ID, Revision: plan.InboundRevision, NodeID: n.NodeID, Path: "direct", Protocol: "vless", Name: label}}}, nil
}

func renderPreparedSubscription(ctx context.Context, tx *sql.Tx, key []byte, client string, binding DarkRestorePreparedBindingV1) (ix.Subscription, error) {
	c, err := readClient(ctx, tx, client)
	if err != nil {
		return ix.Subscription{}, err
	}
	if c.Archived || c.ExpiresUnix <= time.Now().Unix() || c.ExpiresUnix > math.MaxInt64/int64(time.Second) {
		return ix.Subscription{}, ErrDarkRestoreCutover
	}
	plan, err := readNativeInboundPlan(ctx, tx, key, binding.InboundID)
	if err != nil {
		return ix.Subscription{}, err
	}
	if plan.PlanRevision != binding.PlanRevision || plan.InboundRevision != binding.InboundRevision || plan.Status != "SAVED_DISABLED" {
		return ix.Subscription{}, ErrConflict
	}
	if plan.Profile.Spec == nil {
		return renderPreparedLoopbackReality(plan, c, binding.Format)
	}
	_, email, err := executionClientBinding(binding.InboundID, client)
	if err != nil {
		return ix.Subscription{}, err
	}
	user := inboundspec.User{UUID: c.UUID, Email: email}
	formats := []string{}
	for _, info := range inboundspec.Catalog() {
		if info.Protocol == plan.Profile.Spec.Protocol {
			formats = append(formats, info.ExportFormats...)
		}
	}
	routes := []executionSubscriptionRoute{}
	for _, endpoint := range previewPlanEndpoints(plan) {
		if !endpoint.Enabled {
			continue
		}
		raw, err := json.Marshal(*plan.Profile.Spec)
		if err != nil {
			return ix.Subscription{}, err
		}
		var spec inboundspec.Spec
		if err = json.Unmarshal(raw, &spec); err != nil {
			return ix.Subscription{}, err
		}
		if spec.Client == nil {
			spec.Client = map[string]any{}
		}
		if endpoint.ServerName != "" {
			spec.Client["serverName"] = endpoint.ServerName
		}
		if endpoint.Fingerprint != "" {
			spec.Client["fingerprint"] = endpoint.Fingerprint
		}
		if endpoint.SpiderX != "" {
			spec.Client["spiderX"] = endpoint.SpiderX
		}
		routes = append(routes, executionSubscriptionRoute{view: ix.SubscriptionRoute{InboundID: binding.InboundID, Revision: binding.InboundRevision, NodeID: endpoint.NodeID, Path: endpoint.Path, Protocol: spec.Protocol, Name: endpoint.Label},
			spec: spec, user: user, endpoint: inboundspec.Endpoint{Address: endpoint.Address, Port: endpoint.Port, Name: endpoint.Label, SNI: endpoint.ServerName},
			expires: c.ExpiresUnix * int64(time.Second), verified: false, formats: formats})
	}
	if len(routes) == 0 || len(routes) > 256 {
		return ix.Subscription{}, ErrConflict
	}
	return renderExecutionSubscription(client, binding.Format, routes)
}

// DeliverPreparedDarkRestoreSubscriptionV1 returns an actual canonical owner
// export and records its preparation/delivery boundary atomically. It does NOT
// bind a client, enable it, queue a runtime, assert health or claim traffic works.
// Only full M5 evidence can reach this method; normal applied renderers retain
// their eligibility/runtime gates. No browser supplies credential bytes or pins.
func (s *Store) DeliverPreparedDarkRestoreSubscriptionV1(ctx context.Context, key []byte, token, client string, in DarkRestorePreparedDeliveryInputV1) (DarkRestorePreparedDeliveryV1, error) {
	zero := DarkRestorePreparedDeliveryV1{Scope: DarkRestorePreparedDeliveryScopeV1}
	if !eventRE.MatchString(in.EventID) || in.InboundID == 0 || in.ExpectedPlanRevision == 0 || in.ExpectedInboundRevision == 0 {
		return zero, ErrInvalid
	}
	if in.Format == "" {
		in.Format = "uri"
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	evidence, err := readCutoverEvidence(ctx, tx, client)
	if err != nil || evidence == nil {
		return zero, ErrDarkRestoreCutover
	}
	prior, err := readCutoverReceipt(ctx, tx, client)
	if err != nil {
		return zero, err
	}
	if prior != nil {
		if prior.ActorUserID != actor || prior.SubscriptionDeliveryScope != DarkRestorePreparedDeliveryScopeV1 || prior.SubscriptionEventID != in.EventID || prior.Prepared.InboundID != in.InboundID || prior.Prepared.PlanRevision != in.ExpectedPlanRevision || prior.Prepared.InboundRevision != in.ExpectedInboundRevision || prior.Prepared.Format != in.Format {
			return zero, ErrConflict
		}
		if err = verifyPreparedPlanReceipt(ctx, tx, prior); err != nil {
			return zero, err
		}
		if err = verifyCutoverHistoricalBudget(ctx, tx, evidence); err != nil {
			return zero, err
		}
		current, err := readDarkRestoreCutoverSnapshotV1(ctx, tx, evidence.SourceSHA256, evidence.SourceID)
		if err != nil || current.DeploymentState != "disabled" && current.DeploymentState != "pending_node_apply" ||
			current.Archived || current.ExpiresUnix <= time.Now().Unix() ||
			!current.NativeAccountPresent || !current.NativeBaselineComplete ||
			current.NativeUplink == nil || current.NativeDownlink == nil || current.NativeHolds == nil || *current.NativeHolds != 0 ||
			current.NativeBaselineSHA != evidence.NativeBaselineSHA || current.ImportReceiptSHA != evidence.ImportReceiptSHA {
			return zero, ErrConflict
		}
		current.DeploymentState = "disabled"
		current, err = hashDarkRestoreCutoverSnapshotV1(current)
		effectiveStateSHA, stateErr := effectiveCutoverStateSHA(ctx, tx, evidence)
		remaining, budgetErr := cutoverRemaining(current.CurrentEntitlement, evidence.SettledHistoricalBytes, *current.NativeUplink, *current.NativeDownlink)
		if err != nil || stateErr != nil || budgetErr != nil || current.StateSHA != effectiveStateSHA ||
			prior.CurrentFiniteEntitlement != current.CurrentEntitlement ||
			prior.NativePostMigrationBytes != *current.NativeUplink+*current.NativeDownlink ||
			prior.RemainingBudgetBytes != remaining {
			return zero, ErrConflict
		}
		content, err := renderPreparedSubscription(ctx, tx, key, client, prior.Prepared)
		if err != nil || content.SHA256 != prior.Prepared.ContentSHA {
			return zero, ErrConflict
		}
		prior.Replayed = true
		if err = tx.Commit(); err != nil {
			return zero, err
		}
		return DarkRestorePreparedDeliveryV1{Receipt: *prior, Subscription: content, Scope: DarkRestorePreparedDeliveryScopeV1}, nil
	}
	if err = executionEventAvailable(ctx, tx, in.EventID); err != nil {
		return zero, err
	}
	if err = rejectDarkRestoreEvent(ctx, tx, in.EventID); err != nil {
		return zero, err
	}
	var reused int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM darkrestore_cutover_receipts_v1 WHERE json_extract(receipt_json,'$.subscription_event_id')=?`, in.EventID).Scan(&reused); err != nil {
		return zero, err
	}
	if reused != 0 {
		return zero, ErrConflict
	}
	snapshot, remaining, err := cutoverSnapshotMatchesEvidence(ctx, tx, evidence)
	if err != nil {
		return zero, err
	}
	var enabled bool
	if err = tx.QueryRowContext(ctx, `SELECT enabled FROM representatives WHERE id=?`, snapshot.RepresentativeID).Scan(&enabled); err != nil || !enabled {
		return zero, ErrDarkRestoreCutover
	}
	binding, err := preparedPlanAuthority(ctx, tx, in.InboundID)
	if err != nil {
		return zero, err
	}
	if binding.PlanRevision != in.ExpectedPlanRevision || binding.InboundRevision != in.ExpectedInboundRevision {
		return zero, ErrConflict
	}
	binding.Format = in.Format
	content, err := renderPreparedSubscription(ctx, tx, key, client, binding)
	if err != nil {
		return zero, err
	}
	if content.PathVerified {
		return zero, ErrUnavailable
	}
	binding.ContentSHA = content.SHA256
	now := time.Now()
	if now.UnixNano() <= evidence.TransitionOpenedUnixNano {
		return zero, ErrDarkRestoreCutover
	}
	receipt := DarkRestoreCutoverReceiptV1{ClientID: client, ActorUserID: actor, EvidenceInventorySHA256: evidence.InventorySHA256, EvidenceApprovalSHA256: evidence.CoverageApprovalSHA256,
		SettledHistoricalBytes: evidence.SettledHistoricalBytes, NativePostMigrationBytes: *snapshot.NativeUplink + *snapshot.NativeDownlink,
		CurrentFiniteEntitlement: snapshot.CurrentEntitlement, RemainingBudgetBytes: remaining, TransitionOpenedUnixNano: evidence.TransitionOpenedUnixNano, TransitionSHA: evidence.TransitionSHA,
		SubscriptionRevision: 1, SubscriptionDeliveryScope: DarkRestorePreparedDeliveryScopeV1, SubscriptionSnapshotID: content.SHA256[:32], SubscriptionEventID: in.EventID,
		SubscriptionSnapshotRevision: binding.PlanRevision, SubscriptionDeliveredNano: now.UnixNano(), CreatedUnix: now.Unix(), Prepared: binding}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO darkrestore_cutover_receipts_v1(client_id,subscription_revision,subscription_delivered_unix_nano,remaining_budget_bytes,receipt_json,created_unix) VALUES(?,?,?,?,?,?)`,
		client, receipt.SubscriptionRevision, receipt.SubscriptionDeliveredNano, fmt.Sprint(remaining), raw, receipt.CreatedUnix); err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return DarkRestorePreparedDeliveryV1{Receipt: receipt, Subscription: content, Scope: DarkRestorePreparedDeliveryScopeV1}, nil
}
