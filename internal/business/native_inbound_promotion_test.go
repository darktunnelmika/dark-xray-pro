package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type promotionFixture struct {
	s         *Store
	path      string
	key       []byte
	planInput NativeInboundPlanInput
	plan      NativeInboundPlanReceipt
	node      MeteredRouteNode
	clients   []Client
	poll      mx.TransactionPoll
	input     NativeInboundPlanPromotionInput
}

func newPromotionFixture(t *testing.T, change func(*NativeInboundPlanInput)) *promotionFixture {
	t.Helper()
	s, path := opened(t)
	ctx := context.Background()
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	f := &promotionFixture{s: s, path: path, key: bytes.Repeat([]byte{0xe2}, 32),
		node: MeteredRouteNode{NodeID: "promotion-node", InstallationID: strings.Repeat("a", 32), BinarySHA: strings.Repeat("b", 64)}}
	f.clients = []Client{usageClient(t, s, "promotion-b", 1<<20), usageClient(t, s, "promotion-a", 1<<20)}
	f.planInput = NativeInboundPlanInput{EventID: "promotion-source-create-0001", Profile: nativeInput(), Reason: "Saved complete source plan",
		Nodes: []NativeInboundPlanNode{{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID, ListenIP: "127.0.0.1", DirectPort: 19589, DirectHosts: []string{"127.0.0.1"}, TunnelHosts: []string{}}}}
	f.planInput.Profile.ID = 501
	if change != nil {
		change(&f.planInput)
	}
	var err error
	f.plan, err = s.SaveNativeInboundPlan(ctx, f.key, f.planInput)
	if err != nil {
		t.Fatal("source fixture", err)
	}
	if err = s.ReserveMeteredRouteChannelsForPlanPromotion(ctx, []MeteredRouteNode{f.node}); err != nil {
		t.Fatal("startup reservation", err)
	}
	f.poll = mx.TransactionPoll{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID, BinarySHA: f.node.BinarySHA,
		Status: mx.TransactionStatus{Instance: routeTestInstance, Usage: &mx.MeteredCondition{}, Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}}
	f.refresh(t)
	f.input = NativeInboundPlanPromotionInput{EventID: "promotion-operation-0001", ExpectedPlanRevision: 1, ExpectedInboundRevision: 1,
		ClientIDs: []string{f.clients[0].ID, f.clients[1].ID}, Reason: "Explicitly prepare selected customer bindings"}
	return f
}

func (f *promotionFixture) refresh(t *testing.T) {
	t.Helper()
	f.poll.At = time.Now().UnixNano()
	f.poll = mx.SignMeteredTransactionPoll(routeTestToken, f.poll)
	if _, found, err := f.s.PollMeteredRouteCommand(context.Background(), f.key, routeTestToken, f.poll); err != nil || found {
		t.Fatal("empty authenticated poll", found, err)
	}
}

func (f *promotionFixture) reviewed(t *testing.T) NativeInboundPlanPromotionInput {
	t.Helper()
	preview, err := f.s.PreviewNativeInboundPlanPromotion(context.Background(), f.key, 501, f.input)
	if err != nil || !preview.Eligible || len(preview.Blockers) != 0 || !routeSHA.MatchString(preview.ReviewSHA256) || preview.InboundRevision != 1 || preview.PlanRevision != 1 {
		t.Fatal("promotion preview", preview, err)
	}
	in := f.input
	in.ReviewSHA256 = preview.ReviewSHA256
	return in
}

func promotionCiphertexts(t *testing.T, f *promotionFixture) [][]byte {
	t.Helper()
	out := [][]byte{}
	for _, query := range []string{`SELECT profile_enc FROM native_inbounds WHERE id=501`, `SELECT payload_enc FROM native_inbound_plans WHERE inbound_id=501`, `SELECT receipt_enc FROM native_inbound_plan_events WHERE inbound_id=501`} {
		var raw []byte
		if err := f.s.db.QueryRow(query).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		out = append(out, append([]byte(nil), raw...))
	}
	return out
}

func TestInboundPlanPromotionAtomicHistoryBindingsAndMeteredStage(t *testing.T) {
	f := newPromotionFixture(t, nil)
	ctx := context.Background()
	before := promotionCiphertexts(t, f)
	clientsBefore, _ := f.s.ListClients(ctx, "", 500, 0)
	eventsBefore, _ := f.s.ListEvents(ctx, "", 500, 0)
	generation := planGeneration(t, f.s)
	state, err := f.s.NativeInboundPlanPromotionState(ctx, f.key, 501)
	if err != nil || state.Promoted || len(state.Blockers) != 0 || state.Scope != NativeInboundPlanPromotionScope {
		t.Fatal("unprepared state", state, err)
	}
	request := f.reviewed(t)
	preview, err := f.s.PreviewNativeInboundPlanPromotion(ctx, f.key, 501, request)
	if err != nil || preview.RuntimeChanged || preview.SubscriptionPublished || !sort.StringsAreSorted(preview.ClientIDs) {
		t.Fatal("unsafe preview", err)
	}
	raw, _ := json.Marshal(preview)
	for _, secret := range []string{f.clients[0].UUID, f.clients[1].UUID, "privateKey", "reality_private_key", "config_base64"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("private preview", secret)
		}
	}
	for _, table := range []string{"native_inbound_targets", "native_inbound_clients", "native_route_drafts", "native_inbound_plan_promotions", "native_metered_route_commands", "pro_usage_epochs"} {
		if planCount(t, f.s, table) != 0 {
			t.Fatal("preview mutation", table)
		}
	}
	if planGeneration(t, f.s) != generation {
		t.Fatal("preview changed generation")
	}
	result, err := f.s.PromoteNativeInboundPlan(ctx, f.key, 501, request)
	if err != nil || !result.Promoted || result.Replayed || result.PlanRevision != 1 || result.SourceInboundRevision != 1 || result.InboundRevision != 2 || result.DraftRevision != 1 || result.RuntimeChanged || result.SubscriptionPublished || result.ReviewSHA256 != request.ReviewSHA256 {
		t.Fatal("promotion result", result, err)
	}
	if !reflect.DeepEqual(before, promotionCiphertexts(t, f)) {
		t.Fatal("promotion rewrote source identity or history")
	}
	public, private, err := readNativeInbound(ctx, f.s.db, f.key, 501)
	if err != nil || !public.Enabled || public.Archived || public.Revision != 2 || public.BoundClients != 2 || public.PublicKey != f.plan.Plan.Profile.PublicKey || len(private.Users) != 0 {
		t.Fatal("prepared profile changed identity", err)
	}
	plan, err := f.s.NativeInboundPlan(ctx, f.key, 501)
	if err != nil || plan.Editable || plan.EditBlocker != "plan_promoted" || plan.Promotion == nil || plan.InboundRevision != 1 || plan.Profile.Enabled || !reflect.DeepEqual(*plan.Promotion, result) {
		t.Fatal("authoring history lost promoted ownership", err)
	}
	summaries, err := f.s.ListNativeInboundPlans(ctx, f.key)
	if err != nil || len(summaries) != 1 || summaries[0].Promotion == nil || !reflect.DeepEqual(*summaries[0].Promotion, result) {
		t.Fatal("summary lost promotion marker", err)
	}
	sourceReplay, err := f.s.SaveNativeInboundPlan(ctx, f.key, f.planInput)
	if err != nil || !sourceReplay.Replayed || !reflect.DeepEqual(sourceReplay.Plan, f.plan.Plan) {
		t.Fatal("source receipt was rewritten by promotion", err)
	}
	draft, err := f.s.NativeRouteDraft(ctx, f.key, 501, f.node.NodeID)
	if err != nil || draft.DraftRevision != 1 || draft.ExpectedInboundRevision != 2 || draft.DirectPort != 19589 || draft.TunnelPort != 0 || draft.SwapEnabled || !reflect.DeepEqual(draft.DirectHosts, []string{"127.0.0.1"}) {
		t.Fatal("prepared route mismatch", err)
	}
	clientsAfter, _ := f.s.ListClients(ctx, "", 500, 0)
	eventsAfter, _ := f.s.ListEvents(ctx, "", 500, 0)
	if !reflect.DeepEqual(clientsBefore, clientsAfter) || !reflect.DeepEqual(eventsBefore, eventsAfter) {
		t.Fatal("promotion charged or edited customers")
	}
	for _, client := range f.clients {
		usage, err := f.s.ClientUsage(ctx, client.ID)
		if err != nil || !usage.Known || usage.Total != 0 || !usage.Eligible {
			t.Fatal("promotion changed accounting", err)
		}
	}
	for _, table := range []string{"native_runtime_delivery", "native_node_revisions", "pro_usage_epochs", "pro_usage_receipts", "native_metered_route_commands"} {
		if planCount(t, f.s, table) != 0 {
			t.Fatal("promotion performed Apply", table)
		}
	}
	stage := MeteredRouteCommandRequest{ID: routeTestID(910), Action: "stage", RuntimeInstance: f.poll.Status.Instance,
		Expected: mx.Condition(f.poll.Status.Observed), Usage: f.poll.Status.Usage, Drafts: []RouteDraftRef{{InboundID: 501, InboundRevision: 2, DraftRevision: 1}}}
	queued, err := f.s.QueueMeteredRouteCommand(ctx, f.key, f.node.NodeID, stage)
	if err != nil || queued.State != "queued" {
		t.Fatal("promoted state could not enter unchanged metered Stage", err)
	}
	f.poll.At = time.Now().UnixNano()
	f.poll = mx.SignMeteredTransactionPoll(routeTestToken, f.poll)
	delivery, found, err := f.s.PollMeteredRouteCommand(ctx, f.key, routeTestToken, f.poll)
	if err != nil || !found || delivery.Command.Version != 2 || delivery.Command.Usage == nil || delivery.Command.Usage.EpochID != "" {
		t.Fatal("first-stage metered delivery", err)
	}
	replay, err := f.s.PromoteNativeInboundPlan(ctx, f.key, 501, request)
	if err != nil || !replay.Replayed {
		t.Fatal("lost promotion response not recoverable after Stage", err)
	}
	replay.Replayed = false
	if !reflect.DeepEqual(replay, result) {
		t.Fatal("promotion replay changed its immutable receipt")
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.s, err = Open(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.Close()
	if err = f.s.ReserveMeteredRouteChannelsForPlanPromotion(ctx, []MeteredRouteNode{f.node}); err != nil {
		t.Fatal("reservation restart lost identity", err)
	}
	replay, err = f.s.PromoteNativeInboundPlan(ctx, f.key, 501, request)
	if err != nil || !replay.Replayed {
		t.Fatal("promotion replay failed after restart", err)
	}
	replay.Replayed = false
	if !reflect.DeepEqual(replay, result) {
		t.Fatal("restart changed receipt")
	}
}

func TestInboundPlanPromotionReservationDoesNotBypassFirstActivation(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	node := MeteredRouteNode{NodeID: "empty-promotion", InstallationID: strings.Repeat("c", 32), BinarySHA: strings.Repeat("d", 64)}
	if err := s.ClaimMeteredRouteChannels(ctx, []MeteredRouteNode{node}); !errors.Is(err, ErrConflict) {
		t.Fatal("old Claim accepted empty target", err)
	}
	if err := s.ReserveMeteredRouteChannelsForPlanPromotion(ctx, []MeteredRouteNode{node, node}); !errors.Is(err, ErrInvalid) {
		t.Fatal("multi-node reservation accepted", err)
	}
	if err := s.ReserveMeteredRouteChannelsForPlanPromotion(ctx, []MeteredRouteNode{node}); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0xc1}, 32)
	if _, err := s.CreateInbound(ctx, key, nativeInput()); err != nil {
		t.Fatal(err)
	}
	target := InboundTarget{InboundID: 10, NodeID: node.NodeID, InstallationID: node.InstallationID, ListenIP: "127.0.0.1", DirectPort: 19589}
	if err := s.PutInboundTarget(ctx, target); !errors.Is(err, ErrConflict) {
		t.Fatal("generic target bypassed reservation", err)
	}
	if _, err := s.db.Exec(`INSERT INTO native_inbound_targets VALUES(?,?,?,?,?,?)`, 10, node.NodeID, node.InstallationID, "127.0.0.1", 19589, 0); err == nil {
		t.Fatal("old-reader SQL bypassed reserved Node")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = meteredRouteEmptyNode(ctx, tx, node.NodeID, node.InstallationID); !errors.Is(err, ErrConflict) {
		t.Fatal("empty candidate became executable", err)
	}
	tx.Rollback()
	if _, err = s.BuildNodeConfig(ctx, key, node.NodeID, node.InstallationID); !errors.Is(err, ErrNotFound) {
		t.Fatal("reservation invented native candidate", err)
	}
	changed := node
	changed.BinarySHA = strings.Repeat("e", 64)
	if err = s.ReserveMeteredRouteChannelsForPlanPromotion(ctx, []MeteredRouteNode{changed}); !errors.Is(err, ErrConflict) {
		t.Fatal("reservation pin changed", err)
	}
	if err = s.ClaimRouteChannel(ctx, node.NodeID, node.InstallationID, node.BinarySHA); err == nil {
		t.Fatal("unmetered channel took reserved Node")
	}
}

func TestInboundPlanPromotionPermanentOldReaderAndGenericMutationFences(t *testing.T) {
	f := newPromotionFixture(t, nil)
	ctx := context.Background()
	in := f.reviewed(t)
	if _, err := f.s.PromoteNativeInboundPlan(ctx, f.key, 501, in); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"enable", "disable", "archive", "edit"} {
		m := InboundMutation{EventID: "promotion-generic-" + action, ExpectedRevision: 2, Action: action, Reason: "Must use metered execution controls"}
		if action == "edit" {
			copy := f.planInput.Profile
			m.Profile = &copy
		}
		if _, err := f.s.MutateInbound(ctx, f.key, 501, m); !errors.Is(err, ErrConflict) {
			t.Fatal("generic lifecycle bypass", action, err)
		}
	}
	for _, attach := range []bool{false, true} {
		if err := f.s.BindInboundClients(ctx, 501, []string{f.clients[0].ID}, attach); !errors.Is(err, ErrConflict) {
			t.Fatal("generic binding bypass", attach, err)
		}
	}
	draft, err := f.s.NativeRouteDraft(ctx, f.key, 501, f.node.NodeID)
	if err != nil {
		t.Fatal(err)
	}
	draft.EventID = "promotion-draft-mutation-001"
	draft.ExpectedDraftRevision = 1
	draft.DirectPort++
	if _, err = f.s.SaveNativeRouteDraft(ctx, f.key, draft.NativeRouteDraftInput); !errors.Is(err, ErrConflict) {
		t.Fatal("generic draft mutation bypass", err)
	}
	for _, query := range []string{
		`UPDATE native_inbounds SET enabled=0,revision=revision+1 WHERE id=501`,
		`UPDATE native_inbounds SET name='foreign edit',revision=revision+1 WHERE id=501`,
		`UPDATE native_inbounds SET profile_enc=x'01' WHERE id=501`,
		`DELETE FROM native_inbounds WHERE id=501`,
		`UPDATE native_inbound_plans SET revision=revision+1 WHERE inbound_id=501`,
		`DELETE FROM native_inbound_plans WHERE inbound_id=501`,
		`UPDATE native_inbound_targets SET direct_port=19590 WHERE inbound_id=501`,
		`DELETE FROM native_inbound_targets WHERE inbound_id=501`,
		`INSERT OR REPLACE INTO native_inbound_targets SELECT * FROM native_inbound_targets WHERE inbound_id=501`,
		`DELETE FROM native_inbound_clients WHERE inbound_id=501`,
		`UPDATE native_inbound_clients SET inbound_id=500 WHERE inbound_id=501`,
		`INSERT OR IGNORE INTO native_inbound_clients SELECT * FROM native_inbound_clients WHERE inbound_id=501`,
		`UPDATE native_route_drafts SET direct_port=19590 WHERE inbound_id=501`,
		`DELETE FROM native_route_drafts WHERE inbound_id=501`,
		`INSERT OR REPLACE INTO native_route_drafts SELECT * FROM native_route_drafts WHERE inbound_id=501`,
		`UPDATE native_inbound_plan_promotions SET fingerprint='forged' WHERE inbound_id=501`,
		`DELETE FROM native_inbound_plan_promotions WHERE inbound_id=501`,
		`DELETE FROM native_inbound_plan_promotion_clients WHERE inbound_id=501`,
		`UPDATE native_inbound_plan_promotion_clients SET client_id='other' WHERE inbound_id=501`,
		`INSERT INTO native_inbound_plan_promotion_clients VALUES(501,'additional')`,
		`DELETE FROM native_inbound_plan_promotion_nodes`,
		`UPDATE native_inbound_plan_promotion_nodes SET binary_sha='forged'`,
	} {
		if _, err = f.s.db.Exec(query); err == nil {
			t.Fatal("old-reader SQL changed promoted ownership", query)
		}
	}
	if planCount(t, f.s, "native_inbound_targets") != 1 || planCount(t, f.s, "native_inbound_clients") != 2 || planCount(t, f.s, "native_route_drafts") != 1 {
		t.Fatal("rejected SQL partially changed state")
	}
}

func TestInboundPlanPromotionLateFailureRollsBackCompleteMaterialization(t *testing.T) {
	f := newPromotionFixture(t, nil)
	ctx := context.Background()
	before := promotionCiphertexts(t, f)
	generation := planGeneration(t, f.s)
	in := f.reviewed(t)
	if _, err := f.s.db.Exec(`CREATE TRIGGER reject_final_promotion_write BEFORE INSERT ON native_route_draft_events BEGIN SELECT RAISE(ABORT,'injected final write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PromoteNativeInboundPlan(ctx, f.key, 501, in); err == nil {
		t.Fatal("injected final write succeeded")
	}
	if !reflect.DeepEqual(before, promotionCiphertexts(t, f)) || planGeneration(t, f.s) != generation {
		t.Fatal("failed promotion changed source/generation")
	}
	for _, table := range []string{"native_inbound_plan_promotions", "native_inbound_plan_promotion_clients", "native_inbound_targets", "native_inbound_clients", "native_route_drafts", "native_route_draft_events"} {
		if planCount(t, f.s, table) != 0 {
			t.Fatal("partial promotion remained", table)
		}
	}
	p, err := f.s.NativeInboundPlan(ctx, f.key, 501)
	if err != nil || !p.Editable || p.Promotion != nil {
		t.Fatal("failure stranded the source plan", err)
	}
}

func TestInboundPlanPromotionReviewRejectsChangedAuthorityAndEligibility(t *testing.T) {
	for _, field := range []string{"reason", "event_id"} {
		t.Run("reviewed "+field, func(t *testing.T) {
			f := newPromotionFixture(t, nil)
			in := f.reviewed(t)
			if field == "reason" {
				in.Reason = "A changed confirmation reason"
			} else {
				in.EventID = "promotion-changed-intent-001"
			}
			_, err := f.s.PromoteNativeInboundPlan(context.Background(), f.key, 501, in)
			var issue *NativeInboundPlanError
			if !errors.Is(err, ErrConflict) || !errors.As(err, &issue) || issue.Code != "promotion_review_changed" {
				t.Fatal("review did not bind exact intent", err)
			}
			if planCount(t, f.s, "native_inbound_plan_promotions") != 0 {
				t.Fatal("changed intent was committed")
			}
		})
	}
	for _, tc := range []struct {
		name, code string
		change     func(*testing.T, *promotionFixture)
	}{
		{"runtime restart", "promotion_review_changed", func(t *testing.T, f *promotionFixture) { f.poll.Status.Instance = routeTestID(999); f.refresh(t) }},
		{"stale observation", "promotion_observation_stale", func(t *testing.T, f *promotionFixture) {
			_, e := f.s.db.Exec(`UPDATE native_metered_route_channels SET observed_at=?`, time.Now().Add(-time.Minute).UnixNano())
			if e != nil {
				t.Fatal(e)
			}
		}},
		{"client UUID", "promotion_review_changed", func(t *testing.T, f *promotionFixture) {
			_, e := f.s.db.Exec(`UPDATE clients SET uuid='11111111-2222-4333-8444-555555555555' WHERE id=?`, f.clients[0].ID)
			if e != nil {
				t.Fatal(e)
			}
		}},
		{"expired client", "promotion_client_ineligible", func(t *testing.T, f *promotionFixture) {
			_, e := f.s.db.Exec(`UPDATE clients SET expires_unix=? WHERE id=?`, time.Now().Add(-time.Hour).Unix(), f.clients[0].ID)
			if e != nil {
				t.Fatal(e)
			}
		}},
		{"quota exhausted", "promotion_client_ineligible", func(t *testing.T, f *promotionFixture) {
			_, e := f.s.db.Exec(`UPDATE pro_usage_accounts SET uplink=? WHERE client_id=?`, 1<<20, f.clients[0].ID)
			if e != nil {
				t.Fatal(e)
			}
		}},
		{"representative disabled", "promotion_client_ineligible", func(t *testing.T, f *promotionFixture) {
			_, e := f.s.db.Exec(`UPDATE representatives SET enabled=0 WHERE id='owner'`)
			if e != nil {
				t.Fatal(e)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPromotionFixture(t, nil)
			in := f.reviewed(t)
			tc.change(t, f)
			_, err := f.s.PromoteNativeInboundPlan(context.Background(), f.key, 501, in)
			var issue *NativeInboundPlanError
			if !errors.Is(err, ErrConflict) || !errors.As(err, &issue) || issue.Code != tc.code {
				t.Fatal("changed authority accepted", err)
			}
			if planCount(t, f.s, "native_inbound_plan_promotions") != 0 || planCount(t, f.s, "native_inbound_targets") != 0 {
				t.Fatal("rejected review mutated state")
			}
		})
	}
}

func TestInboundPlanPromotionUnsupportedWholePlansRemainEditable(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		change     func(*NativeInboundPlanInput)
	}{
		{"grpc", "promotion_transport_unsupported", func(in *NativeInboundPlanInput) {
			in.Profile.Transport = "grpc"
			in.Profile.GRPCService = "test-service"
		}},
		{"advanced scalar", "promotion_options_unsupported", func(in *NativeInboundPlanInput) {
			in.Profile.Options = &xrayconfig.ProfileOptions{RealityMaxTimeDiff: 1000}
		}},
		{"disabled configured sniffing", "promotion_options_unsupported", func(in *NativeInboundPlanInput) {
			in.Profile.Options = &xrayconfig.ProfileOptions{Sniffing: &xrayconfig.SniffingOptions{Enabled: false, RouteOnly: true}}
		}},
		{"nonloopback", "promotion_loopback_required", func(in *NativeInboundPlanInput) { in.Nodes[0].ListenIP = "198.51.100.10" }},
		{"tunnel", "promotion_direct_only", func(in *NativeInboundPlanInput) {
			in.Nodes[0].TunnelPort = 19590
			in.Nodes[0].TunnelHosts = []string{"127.0.0.1"}
		}},
		{"rich endpoint", "promotion_endpoints_unsupported", func(in *NativeInboundPlanInput) {
			in.Nodes[0].DirectEndpoints = []NativeInboundEndpoint{{Address: "127.0.0.1", Port: 19589, Enabled: true, Label: "Custom endpoint"}}
		}},
		{"waiting host", "promotion_hosts_required", func(in *NativeInboundPlanInput) { in.Nodes[0].DirectHosts = []string{} }},
		{"two Nodes", "promotion_single_node_required", func(in *NativeInboundPlanInput) {
			n := in.Nodes[0]
			n.NodeID = "second-node"
			n.InstallationID = strings.Repeat("f", 32)
			in.Nodes = append(in.Nodes, n)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPromotionFixture(t, tc.change)
			preview, err := f.s.PreviewNativeInboundPlanPromotion(context.Background(), f.key, 501, f.input)
			if err != nil || preview.Eligible || preview.ReviewSHA256 != "" {
				t.Fatal("unsupported plan eligible", err)
			}
			found := false
			for _, issue := range preview.Blockers {
				found = found || issue.Code == tc.code
			}
			if !found {
				t.Fatal("missing explicit blocker", tc.code, preview.Blockers)
			}
			plan, err := f.s.NativeInboundPlan(context.Background(), f.key, 501)
			if err != nil || !plan.Editable || plan.Promotion != nil {
				t.Fatal("blocked plan became frozen", err)
			}
			if planCount(t, f.s, "native_inbound_plan_promotions") != 0 || planCount(t, f.s, "native_inbound_targets") != 0 {
				t.Fatal("partial subset was materialized")
			}
		})
	}
}

func TestInboundPlanPromotionConcurrentExactReplayAndConflictingEvent(t *testing.T) {
	for _, sameEvent := range []bool{true, false} {
		t.Run(map[bool]string{true: "exact replay", false: "competing events"}[sameEvent], func(t *testing.T) {
			f := newPromotionFixture(t, nil)
			in := f.reviewed(t)
			var wg sync.WaitGroup
			results := make(chan struct {
				r NativeInboundPlanPromotionReceipt
				e error
			}, 2)
			for i := 0; i < 2; i++ {
				request := in
				if i == 1 && !sameEvent {
					request.EventID = "promotion-operation-0002"
				}
				wg.Add(1)
				go func() {
					defer wg.Done()
					r, e := f.s.PromoteNativeInboundPlan(context.Background(), f.key, 501, request)
					results <- struct {
						r NativeInboundPlanPromotionReceipt
						e error
					}{r, e}
				}()
			}
			wg.Wait()
			close(results)
			success, replayed, conflict := 0, 0, 0
			for result := range results {
				if result.e == nil {
					success++
					if result.r.Replayed {
						replayed++
					}
				} else if errors.Is(result.e, ErrConflict) {
					conflict++
				} else {
					t.Fatal(result.e)
				}
			}
			if sameEvent && (success != 2 || replayed != 1) || !sameEvent && (success != 1 || conflict != 1) {
				t.Fatal("concurrent promotion results", success, replayed, conflict)
			}
			if planCount(t, f.s, "native_inbound_plan_promotions") != 1 || planCount(t, f.s, "native_inbound_targets") != 1 || planCount(t, f.s, "native_inbound_clients") != 2 {
				t.Fatal("concurrent requests duplicated preparation")
			}
		})
	}
}

func TestInboundPlanPromotionAuthenticatedReceiptAndIntentCannotBeRebound(t *testing.T) {
	for _, kind := range []string{"reviewed intent", "prepared assignment", "public receipt"} {
		t.Run(kind, func(t *testing.T) {
			f := newPromotionFixture(t, nil)
			ctx := context.Background()
			input := f.reviewed(t)
			receipt, err := f.s.PromoteNativeInboundPlan(ctx, f.key, 501, input)
			if err != nil {
				t.Fatal(err)
			}
			var encrypted []byte
			if err = f.s.db.QueryRow(`SELECT input_enc FROM native_inbound_plan_promotions WHERE inbound_id=501`).Scan(&encrypted); err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encrypted, []byte(input.Reason)) {
				t.Fatal("private review note stored as plaintext")
			}
			var stored NativeInboundPlanPromotionInput
			if err = openPlanValue(f.key, promotionAAD(501, input.EventID)+":input", encrypted, &stored); err != nil || stored.Reason != input.Reason {
				t.Fatal("audit intent not retained", err)
			}
			if _, err = f.s.db.Exec(`DROP TRIGGER native_promotion_no_update`); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "reviewed intent":
				stored.Reason = "A different unreviewed intent"
				raw, err := sealPlanValue(f.key, promotionAAD(501, input.EventID)+":input", stored)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.s.db.Exec(`UPDATE native_inbound_plan_promotions SET input_enc=? WHERE inbound_id=501`, raw)
				if err != nil {
					t.Fatal(err)
				}
			case "prepared assignment":
				if _, err = f.s.db.Exec(`UPDATE native_inbound_plan_promotions SET direct_port=19590 WHERE inbound_id=501`); err != nil {
					t.Fatal(err)
				}
			case "public receipt":
				receipt.RuntimeChanged = true
				raw, err := sealPlanValue(f.key, promotionAAD(501, input.EventID), receipt)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.s.db.Exec(`UPDATE native_inbound_plan_promotions SET receipt_enc=? WHERE inbound_id=501`, raw); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = f.s.NativeInboundPlanPromotionState(ctx, f.key, 501); !errors.Is(err, ErrUnavailable) {
				t.Fatal("authenticated promotion was rebound", err)
			}
		})
	}
}
