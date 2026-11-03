//go:build linux

package business

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func preparedCutoverFixture(t *testing.T) (ownershipFixture, DarkRestoreCutoverEvidenceReceiptV1, []byte, NativeInboundPlanInput) {
	t.Helper()
	return preparedCutoverWithFiles(t, nil)
}

type PreparedHandoffFiles struct{ Path, SHA, PublicPath, PublicSHA string }

func BrowserPreparedFixture(t *testing.T) (*Store, string, []byte, PreparedHandoffFiles, string) {
	t.Helper()
	var files PreparedHandoffFiles
	f, ev, key, _ := preparedCutoverWithFiles(t, &files)
	return f.s, ev.ClientID, key, files, ownershipPassword
}

func preparedCutoverWithFiles(t *testing.T, files *PreparedHandoffFiles) (ownershipFixture, DarkRestoreCutoverEvidenceReceiptV1, []byte, NativeInboundPlanInput) {
	t.Helper()
	f := importSetup(t)
	ctx := context.Background()
	if err := f.s.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableDarkRestoreCutoverV1(ctx, f.owner.Token); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x61}, 32)
	plan := NativeInboundPlanInput{EventID: "m5-prepared-profile-0001", Profile: InboundInput{ID: 801, Name: "M5 prepared export", Spec: &inboundspec.Spec{Version: 1, Protocol: "vless", Network: "tcp", Security: "none", Settings: map[string]any{"decryption": "none"}}},
		Nodes: []NativeInboundPlanNode{{NodeID: "prepared-node", InstallationID: strings.Repeat("a", 32), ListenIP: "198.51.100.10", DirectPort: 21185, DirectHosts: []string{"prepared.example.org"}, TunnelHosts: []string{}}}, Reason: "Synthetic authored profile, never applied"}
	if _, err := f.s.SaveNativeInboundPlan(ctx, key, plan); err != nil {
		t.Fatal(err)
	}
	intent := syntheticImportIntent(1)
	req := importRequest(t, f, intent, "m5-prepared-import-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	sha, source := intent.Review.SourceSHA256, intent.Review.Clients[0].SourceID
	proof := darkrestore.LocalSourceSettlement{Scope: "single_local_source_epoch", SourceSHA: sha, SourceID: source, AuthoritySHA: strings.Repeat("b", 64), JournalSHA: strings.Repeat("c", 64),
		Terminal: uc.Receipt{EpochID: strings.Repeat("d", 32), Sequence: 1, SHA: strings.Repeat("e", 64)}, PriorBytes: 7, Up: 3, Down: 4, Historical: 14, Remaining: 999986}
	handoff, pin := signFixtureHandoff(t, proof, files)
	if _, err := f.s.AttachSignedLocalSettlementV1(ctx, f.owner.Token, handoff); err != nil {
		t.Fatal(err)
	}
	before, err := f.s.DarkRestoreCutoverSnapshotV1(ctx, sha, source)
	if err != nil {
		t.Fatal(err)
	}
	destination, attribution := strings.Repeat("1", 64), strings.Repeat("2", 64)
	native, nativePins := businessSignedNativeBaseline(t, before, destination)
	prepared, err := f.s.PrepareDarkRestoreNativeBaselineV1(ctx, f.owner.Token, sha, source, native, nativePins)
	if err != nil {
		t.Fatal(err)
	}
	projected := prepared.ProjectedSnapshot
	inventory := darkrestore.LocalHandoffInventoryV1{Version: 1, SourceSHA: sha, SourceID: source, Sources: []darkrestore.LocalHandoffSourcePinV1{pin}}
	inventorySHA, err := darkrestore.DigestLocalHandoffInventoryV1(inventory)
	if err != nil {
		t.Fatal(err)
	}
	approval := businessSignedCoverage(t, darkrestore.HandoffCoverageApprovalV1{Version: 1, Scope: darkrestore.HandoffCoverageScopeV1, Statement: darkrestore.HandoffCoverageStatementV1,
		InventorySHA: inventorySHA, SourceSHA: sha, SourceID: source, DestinationSHA: destination, ImportReceiptSHA: before.ImportReceiptSHA, DestinationStateSHA: projected.StateSHA, NativeBaselineSHA: projected.NativeBaselineSHA,
		BillingAttributionSHA: attribution, BillingAttribution: "disjoint_source_epoch_deltas", DeclaredSources: 1, EpochIDs: []string{pin.EpochID}, InventoryCompleteAsserted: true, DeclaredSourcesRetired: true, ApprovedUnixNano: time.Now().UnixNano()})
	evidence, err := f.s.OpenDarkRestoreCutoverV1(ctx, f.owner.Token, inventory, inventorySHA, []darkrestore.VerifiedLocalHandoff{handoff}, approval, destination, attribution, native, nativePins)
	if err != nil {
		t.Fatal(err)
	}
	return f, evidence, key, plan
}

func TestDarkRestorePreparedCanonicalDeliveryAndV16Recovery(t *testing.T) {
	for _, mode := range []string{"deliver activate recover", "stale profile stays closed", "stale quota stays closed", "stale identity stays closed"} {
		t.Run(mode, func(t *testing.T) {
			staleProfile := mode == "stale profile stays closed"
			f, evidence, key, plan := preparedCutoverFixture(t)
			ctx := context.Background()
			client, err := f.s.Client(ctx, evidence.ClientID)
			if err != nil {
				t.Fatal(err)
			}
			beforeBalances := ownershipBalances(t, f.s)
			var generation int64
			if err = f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&generation); err != nil {
				t.Fatal(err)
			}
			input := DarkRestorePreparedDeliveryInputV1{EventID: "m5-prepared-delivery-0001", InboundID: 801, ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, Format: "uri"}
			if v, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.rep.Token, client.ID, input); !errors.Is(err, ErrOwnershipDenied) || len(v.Subscription.Content) != 0 {
				t.Fatal("representative received owner-only preparation", err)
			}
			if _, err = f.s.ProjectInboundExecutionSubscription(ctx, key, "owner", client.ID, "uri"); err == nil {
				t.Fatal("applied renderer bypassed disabled identity")
			}
			bad := input
			bad.ExpectedPlanRevision++
			if v, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, bad); !errors.Is(err, ErrConflict) || len(v.Subscription.Content) != 0 {
				t.Fatal("stale plan accepted", err)
			}
			bad = input
			bad.Format = "wireguard"
			if v, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, bad); err == nil || len(v.Subscription.Content) != 0 {
				t.Fatal("unsupported format delivered", err)
			}
			if _, err = f.s.db.Exec(`CREATE TRIGGER synthetic_prepared_failure BEFORE INSERT ON darkrestore_cutover_receipts_v1 BEGIN SELECT RAISE(ABORT,'synthetic delivery failure'); END`); err != nil {
				t.Fatal(err)
			}
			if v, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, input); err == nil || len(v.Subscription.Content) != 0 {
				t.Fatal("failed delivery disclosed content", err)
			}
			if _, err = f.s.db.Exec(`DROP TRIGGER synthetic_prepared_failure`); err != nil {
				t.Fatal(err)
			}
			delivered, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, input)
			if err != nil || delivered.Applied || delivered.Subscription.PathVerified || delivered.Scope != DarkRestorePreparedDeliveryScopeV1 ||
				!strings.HasPrefix(string(delivered.Subscription.Content), "vless://") || !strings.Contains(string(delivered.Subscription.Content), client.UUID) || !strings.Contains(string(delivered.Subscription.Content), "prepared.example.org:21185") ||
				delivered.Subscription.SHA256 != delivered.Receipt.Prepared.ContentSHA || delivered.Receipt.RemainingBudgetBytes != 999986 {
				t.Fatal("canonical prepared export failed", delivered.Receipt, err)
			}
			current, err := f.s.Client(ctx, client.ID)
			if err != nil || current.DeploymentState != "disabled" {
				t.Fatal("preparation enabled identity", err)
			}
			account, err := f.s.ClientUsage(ctx, client.ID)
			if err != nil || account.Eligible {
				t.Fatal("preparation changed native eligibility", err)
			}
			var afterGeneration int64
			if err = f.s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&afterGeneration); err != nil || afterGeneration != generation {
				t.Fatal("preparation queued runtime", err)
			}
			replay, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, input)
			if err != nil || !replay.Receipt.Replayed || !bytes.Equal(replay.Subscription.Content, delivered.Subscription.Content) {
				t.Fatal("preparation replay changed content", err)
			}
			if !reflect.DeepEqual(beforeBalances, ownershipBalances(t, f.s)) {
				t.Fatal("preparation debited representative")
			}
			if mode == "stale quota stays closed" {
				if _, err = f.s.ExtendClientScoped(ctx, ExtendClient{EventID: "m5-prepared-quote-renew-0001", ClientID: client.ID, AdditionalVolumeBytes: 20, Reason: "Synthetic current-payer renewal invalidates pre-enable approval"}, client.RepresentativeID); err != nil {
					t.Fatal(err)
				}
				if v, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, input); err == nil || len(v.Subscription.Content) != 0 {
					t.Fatal("quota drift reused stale preparation", err)
				}
				if _, err = f.s.SetClientStateScoped(ctx, SetClientState{EventID: "m5-prepared-quota-enable-0001", ClientID: client.ID, Action: "enable", Reason: "Synthetic stale quota refusal"}, client.RepresentativeID); !errors.Is(err, ErrDarkRestoreCutover) {
					t.Fatal("stale quota activated", err)
				}
				readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, evidence.SourceSHA256, evidence.SourceID)
				if err != nil || readiness.ActivationAllowed {
					t.Fatal("stale quota readiness lied", readiness, err)
				}
				return
			}
			if mode == "stale identity stays closed" {
				ownershipMove(t, f, "m5-prepared-identity-move-0001", ClientOwnershipIntent{ClientIDs: []string{client.ID}, RepresentativeID: "rep-third", Reason: "Synthetic payer identity drift"})
				if v, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, input); err == nil || len(v.Subscription.Content) != 0 {
					t.Fatal("changed payer identity replayed stale preparation", err)
				}
				if _, err = f.s.SetClientStateScoped(ctx, SetClientState{EventID: "m5-prepared-identity-enable-0001", ClientID: client.ID, Action: "enable", Reason: "Synthetic stale identity refusal"}, "rep-third"); !errors.Is(err, ErrDarkRestoreCutover) {
					t.Fatal("changed payer identity activated", err)
				}
				readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, evidence.SourceSHA256, evidence.SourceID)
				if err != nil || readiness.ActivationAllowed || !strings.Contains(strings.Join(readiness.ActivationBlockers, ","), "approved_destination_state_changed") {
					t.Fatal("stale identity readiness lied", readiness, err)
				}
				if !reflect.DeepEqual(beforeBalances, ownershipBalances(t, f.s)) {
					t.Fatal("identity reassignment changed representative balances")
				}
				return
			}
			if staleProfile {
				plan.EventID = "m5-prepared-edited-profile-0001"
				plan.ExpectedPlanRevision = 1
				plan.ExpectedInboundRevision = 1
				plan.Nodes[0].DirectHosts = []string{"changed.example.org"}
				if _, err = f.s.SaveNativeInboundPlan(ctx, key, plan); err != nil {
					t.Fatal(err)
				}
				if v, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, input); err == nil || len(v.Subscription.Content) != 0 {
					t.Fatal("changed profile replay delivered credentials", err)
				}
				if _, err = f.s.SetClientStateScoped(ctx, SetClientState{EventID: "m5-prepared-stale-enable-0001", ClientID: client.ID, Action: "enable", Reason: "Synthetic stale profile refusal"}, client.RepresentativeID); !errors.Is(err, ErrDarkRestoreCutover) {
					t.Fatal("stale profile activated", err)
				}
				readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, evidence.SourceSHA256, evidence.SourceID)
				if err != nil || readiness.ActivationAllowed || readiness.SubscriptionDelivered {
					t.Fatal("stale profile readiness lied", readiness, err)
				}
				return
			}
			readiness, err := f.s.DarkRestoreCutoverReadinessV1(ctx, evidence.SourceSHA256, evidence.SourceID)
			if err != nil || readiness.ActivationAllowed || !readiness.SubscriptionDelivered || readiness.SubscriptionDeliveryScope != DarkRestorePreparedDeliveryScopeV1 || readiness.GlobalSourceCustodyVerified ||
				!strings.Contains(strings.Join(readiness.ActivationBlockers, ","), "native_apply_not_recorded") {
				t.Fatal("preparation readiness misrepresented apply boundary", readiness, err)
			}
			if _, err = f.s.SetClientStateScoped(ctx, SetClientState{EventID: "m5-prepared-enable-0001", ClientID: client.ID, Action: "enable", Reason: "Synthetic prepared cutover approval"}, client.RepresentativeID); !errors.Is(err, ErrDarkRestoreCutover) {
				t.Fatal("prepared delivery activated before native apply", err)
			}
			kept, err := f.s.Client(ctx, client.ID)
			if err != nil || kept.DeploymentState != "disabled" {
				t.Fatal("failed activation changed quarantine", kept, err)
			}
			if err = f.s.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err = ValidateOfflineBackup(ctx, f.path); err != nil {
				t.Fatal("prepared v16 backup failed", err)
			}
			reopened, err := Open(f.path)
			if err != nil {
				t.Fatal("prepared receipt recovery failed", err)
			}
			defer reopened.Close()
			recovered, err := reopened.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, client.ID, input)
			if err != nil || !recovered.Receipt.Replayed || !bytes.Equal(recovered.Subscription.Content, delivered.Subscription.Content) {
				t.Fatal("prepared receipt lost after reopen", err)
			}
		})
	}
}

func BrowserPreparedQuarantineFixture(t *testing.T) (*Store, string, []byte, PreparedHandoffFiles, string) {
	t.Helper()
	var files PreparedHandoffFiles
	f, ev, key, _ := preparedCutoverWithFiles(t, &files)
	plan := NativeInboundPlanInput{EventID: "quarantine-transport-plan-0001", Profile: nativeInput(), Reason: "Disposable TLS quarantine", Nodes: []NativeInboundPlanNode{{NodeID: "quarantine-node", InstallationID: strings.Repeat("a", 32), ListenIP: "127.0.0.1", DirectPort: 29181, DirectHosts: []string{"127.0.0.1"}}}}
	plan.Profile.ID = 502
	if _, err := f.s.SaveNativeInboundPlan(context.Background(), key, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(context.Background(), key, f.owner.Token, ev.ClientID, DarkRestorePreparedDeliveryInputV1{EventID: "quarantine-transport-delivery-0001", InboundID: 502, ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, Format: "uri"}); err != nil {
		t.Fatal(err)
	}
	return f.s, ev.ClientID, key, files, ownershipPassword
}
