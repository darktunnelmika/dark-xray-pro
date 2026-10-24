package business

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func TestInitialApplySchemaMigrationPreservesHandoverCiphertextAndRevisions(t *testing.T) {
	for _, version := range []int{13, 14} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			f, round, secret := labSubscriptionFixture(t, true)
			ctx := context.Background()
			first := materializeSubscription(t, f, round)
			secondInput := LabSubscriptionMaterialization{EventID: "migration-old-snapshot-0002", ClientID: f.client.ID, RoundID: round.ID, ExpectedRevision: 1}
			second, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, secondInput, healthEndpoint())
			if err != nil || second.Revision != 2 {
				t.Fatal(err)
			}
			before, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
			if err != nil {
				t.Fatal(err)
			}
			var cipher []byte
			if err = f.s.db.QueryRow(`SELECT payload_enc FROM pro_subscription_snapshots WHERE id=?`, second.ID).Scan(&cipher); err != nil {
				t.Fatal(err)
			}
			if version == 14 {
				if err = f.s.usage.EnableConsumerManifests(ctx); err != nil {
					t.Fatal(err)
				}
			}
			var actual int
			if err = f.s.db.QueryRow(`PRAGMA user_version`).Scan(&actual); err != nil || actual != version {
				t.Fatal("wrong migration source", actual, err)
			}
			if err = f.s.EnableInitialApplyHealth(ctx); err != nil {
				t.Fatal("migration", err)
			}
			if f.s.consumerPolicy != nil {
				t.Fatal("empty consumer schema enabled endpoint policy")
			}
			if err = f.s.db.QueryRow(`PRAGMA user_version`).Scan(&actual); err != nil || actual != 15 {
				t.Fatal("migration version", actual, err)
			}
			if planCount(t, f.s, "pro_initial_preparations") != 0 || planCount(t, f.s, "pro_initial_rounds") != 0 || planCount(t, f.s, "pro_consumer_manifests") != 0 {
				t.Fatal("migration created operational authority")
			}
			var afterCipher []byte
			if err = f.s.db.QueryRow(`SELECT payload_enc FROM pro_subscription_snapshots WHERE id=?`, second.ID).Scan(&afterCipher); err != nil || !bytes.Equal(cipher, afterCipher) {
				t.Fatal("migration changed legacy AAD/ciphertext", err)
			}
			after, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
			if err != nil || !bytes.Equal(before.Raw, after.Raw) || after.Snapshot.ID != second.ID || after.Snapshot.Revision != 2 || after.Snapshot.DeliveryRevision != 1 || after.Snapshot.Scope != HandoverSubscriptionScope {
				t.Fatal("backfilled delivery", after.Snapshot, err)
			}
			view, err := f.s.LabSubscription(ctx, f.key, "owner", f.client.ID, healthEndpoint())
			if err != nil || view.HandoverRevision != 2 || view.InitialApplyRevision != 0 || view.DeliveryRevision != 1 || view.DeliveryScope != HandoverSubscriptionScope {
				t.Fatal("separate revisions", view, err)
			}
			// An exact absent-field request from v13 retains its fingerprint.
			replayed, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, secondInput, healthEndpoint())
			if err != nil || !replayed.Replayed || replayed.DeliveryRevision != 1 || replayed.ID != second.ID {
				t.Fatal("legacy absent-field replay changed", replayed, err)
			}
			oldReplay, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: routeTestID(903), ClientID: f.client.ID, RoundID: round.ID}, healthEndpoint())
			if err != nil || !oldReplay.Replayed || oldReplay.Current || oldReplay.DeliveryRevision != 0 || oldReplay.ID != first.ID {
				t.Fatal("pre-migration historical receipt gained authority", oldReplay, err)
			}
			third, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, LabSubscriptionMaterialization{EventID: "migration-old-snapshot-0003", ClientID: f.client.ID, RoundID: round.ID, ExpectedRevision: 2}, healthEndpoint())
			if err != nil || third.Revision != 3 || third.DeliveryRevision != 2 {
				t.Fatal("legacy current-origin CAS", third, err)
			}
			if err = f.s.EnableInitialApplyHealth(ctx); err != nil || planCount(t, f.s, "pro_delivery_events") != 2 {
				t.Fatal("migration replay duplicated authority", err)
			}
		})
	}
}

func addInitialPromotion(t *testing.T, s *Store, key []byte) *promotionFixture {
	t.Helper()
	ctx := context.Background()
	clients, err := s.ListClients(ctx, "", 200, 0)
	if err != nil || len(clients) != 1 {
		t.Fatal("shared-client fixture", err)
	}
	f := &promotionFixture{s: s, key: key, clients: clients, node: MeteredRouteNode{NodeID: "initial-third-node", InstallationID: strings.Repeat("f", 32), BinarySHA: strings.Repeat("b", 64)}}
	f.planInput = NativeInboundPlanInput{EventID: "promotion-source-create-0001", Profile: nativeInput(), Reason: "Prepare a separate promoted loopback proof origin", Nodes: []NativeInboundPlanNode{{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID, ListenIP: "127.0.0.1", DirectPort: 19689, DirectHosts: []string{"127.0.0.1"}, TunnelHosts: []string{}}}}
	f.planInput.Profile.ID = 501
	f.plan, err = s.SaveNativeInboundPlan(ctx, key, f.planInput)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ReserveMeteredRouteChannelsForPlanPromotion(ctx, []MeteredRouteNode{f.node}); err != nil {
		t.Fatal(err)
	}
	f.poll = mx.TransactionPoll{NodeID: f.node.NodeID, InstallationID: f.node.InstallationID, BinarySHA: f.node.BinarySHA, Status: mx.TransactionStatus{Instance: routeTestID(1000), Usage: &mx.MeteredCondition{}, Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}}
	f.refresh(t)
	f.input = NativeInboundPlanPromotionInput{EventID: "promotion-operation-0001", ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, ClientIDs: []string{clients[0].ID}, Reason: "Explicitly review shared customer binding"}
	if _, err = s.PromoteNativeInboundPlan(ctx, key, 501, f.reviewed(t)); err != nil {
		t.Fatal(err)
	}
	initialPrepare(t, f)
	return f
}

func TestInitialApplyDeliveryAuthoritySwitchBothOriginsAndNoFallback(t *testing.T) {
	ctx := context.Background()
	var initial *promotionFixture
	f := healthApplied(t, func(s *Store, key []byte, _, _ mx.TransactionPoll) {
		if _, err := s.BootstrapOwner(ctx, "shared-origin-owner", "synthetic-initial-health-password"); err != nil {
			t.Fatal(err)
		}
		if err := s.EnableInitialApplyHealth(ctx); err != nil {
			t.Fatal(err)
		}
		initial = addInitialPromotion(t, s, key)
	})
	initialActivate(t, initial, 1001)
	if _, err := f.s.MutateLabSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: "shared-origin-token-0001", Action: "issue"}); err != nil {
		t.Fatal(err)
	}
	secret, err := f.s.RevealLabSubscription(ctx, f.key, "owner", f.client.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	oldRound := queuedHealthRound(t, f, 1004)
	completeSubscriptionRound(t, f, oldRound)
	oldInput := LabSubscriptionMaterialization{EventID: "shared-origin-old-snapshot-0001", ClientID: f.client.ID, RoundID: oldRound.ID}
	oldSnapshot, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, oldInput, healthEndpoint())
	if err != nil || oldSnapshot.Revision != 1 || oldSnapshot.DeliveryRevision != 1 {
		t.Fatal("first old delivery", oldSnapshot, err)
	}
	round, _, _ := initialCompleteRound(t, initial, 1005)
	newInput := InitialApplySubscriptionMaterialization{EventID: "shared-origin-new-snapshot-0001", ClientID: f.client.ID, RoundID: round.ID, InitialApplySHA: round.InitialApplySHA, ExpectedDeliveryRevision: 1}
	newSnapshot, err := f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 501, newInput, healthEndpoint())
	if err != nil || newSnapshot.Revision != 1 || newSnapshot.DeliveryRevision != 2 {
		t.Fatal("switch to initial", newSnapshot, err)
	}
	content, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if err != nil || content.Snapshot.ID != newSnapshot.ID || !bytes.Contains(content.Raw, []byte(":19689")) {
		t.Fatal("selected initial delivery", err)
	}
	back := LabSubscriptionMaterialization{EventID: "shared-origin-old-snapshot-0002", ClientID: f.client.ID, RoundID: oldRound.ID, ExpectedRevision: 1}
	if _, err = f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, back, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy absent global CAS overwrote initial", err)
	}
	stale := uint64(1)
	back.ExpectedDeliveryRevision = &stale
	if _, err = f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, back, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("stale global CAS overwrote initial", err)
	}
	// Only the initial origin changes; the old proof is still live. Delivery
	// must fail rather than silently choosing that older valid handover proof.
	if _, err = f.s.QueueMeteredRouteCommand(ctx, f.key, initial.node.NodeID, meteredQueueRequest(initial.poll, 1006, "drain", initial.poll.Status.CurrentStageID)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); err == nil {
		t.Fatal("unavailable initial origin fell back to old snapshot")
	}
	view, err := f.s.LabSubscription(ctx, f.key, "owner", f.client.ID, healthEndpoint())
	if err != nil || view.Snapshot == nil || view.Snapshot.Current || view.Snapshot.ID != newSnapshot.ID || view.HandoverRevision != 1 || view.InitialApplyRevision != 1 || view.DeliveryRevision != 2 {
		t.Fatal("lost selected-origin state", view, err)
	}
	current := uint64(2)
	back.ExpectedDeliveryRevision = &current
	restored, err := f.s.MaterializeLabSubscription(ctx, f.key, "owner", f.in.ID, back, healthEndpoint())
	if err != nil || !restored.Current || restored.Revision != 2 || restored.DeliveryRevision != 3 || restored.Scope != HandoverSubscriptionScope {
		t.Fatal("explicit switch back", restored, err)
	}
	content, err = f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint())
	if err != nil || content.Snapshot.ID != restored.ID || content.SourceNodeID != f.source.NodeID || bytes.Contains(content.Raw, []byte(":19689")) {
		t.Fatal("selected old delivery", err)
	}
	replay, err := f.s.MaterializeInitialApplySubscription(ctx, f.key, "owner", 501, newInput, healthEndpoint())
	if err != nil || !replay.Replayed || replay.Current || replay.DeliveryRevision != 2 || replay.ID != newSnapshot.ID {
		t.Fatal("initial replay reclaimed delivery", replay, err)
	}
	if planCount(t, f.s, "pro_delivery_events") != 3 || planCount(t, f.s, "pro_initial_snapshots") != 1 || planCount(t, f.s, "pro_subscription_snapshots") != 2 {
		t.Fatal("origin histories overwritten")
	}
}

func TestInitialApplyDeliverySchemaRejectsPartialHeadsAndDowngrade(t *testing.T) {
	for _, damage := range []string{"head_missing", "head_scope", "event_missing", "downgrade"} {
		t.Run(damage, func(t *testing.T) {
			f, round, secret := labSubscriptionFixture(t, true)
			materializeSubscription(t, f, round)
			ctx := context.Background()
			if err := f.s.EnableInitialApplyHealth(ctx); err != nil {
				t.Fatal(err)
			}
			// Ordinary SQL mutations must hit the append-only/CAS policy.
			for _, query := range []string{`DELETE FROM pro_delivery_heads`, `UPDATE pro_delivery_heads SET scope='lab_initial_apply'`, `DELETE FROM pro_delivery_events`, `UPDATE pro_delivery_events SET delivery_revision=2`} {
				if _, err := f.s.db.Exec(query); err == nil {
					t.Fatal("delivery SQL fence missing", query)
				}
			}
			if damage == "downgrade" {
				if _, err := f.s.db.Exec(`PRAGMA user_version=14`); err != nil {
					t.Fatal(err)
				}
				if err := f.s.Close(); err != nil {
					t.Fatal(err)
				}
				if reopened, err := Open(f.path); err == nil {
					reopened.Close()
					t.Fatal("v15 history opened as older schema")
				}
				return
			}
			trigger, statement := "pro_delivery_heads_no_delete", `DELETE FROM pro_delivery_heads`
			if damage == "head_scope" {
				trigger, statement = "pro_delivery_head_update", `UPDATE pro_delivery_heads SET scope='lab_initial_apply'`
			}
			if damage == "event_missing" {
				trigger, statement = "pro_delivery_events_no_delete", `DELETE FROM pro_delivery_events`
			}
			var guard string
			if err := f.s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name=?`, trigger).Scan(&guard); err != nil {
				t.Fatal(err)
			}
			for _, query := range []string{`PRAGMA foreign_keys=OFF`, `DROP TRIGGER ` + trigger, statement, guard, `PRAGMA foreign_keys=ON`} {
				if _, err := f.s.db.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.s.ReadLabSubscription(ctx, f.key, secret.Token, healthEndpoint()); !errors.Is(err, ErrUnavailable) {
				t.Fatal("partial/ambiguous delivery authority read", err)
			}
		})
	}
}

func TestInitialApplyRoundDeadlineAndScopeRebinding(t *testing.T) {
	f, _ := initialApplied(t)
	ctx := context.Background()
	state, err := f.s.InitialApplyHealth(ctx, f.key, "owner", 501, healthEndpoint())
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	calls := 0
	clock := func() time.Time {
		calls++
		if calls == 1 {
			return start
		}
		return start.Add(8 * time.Second)
	}
	if _, err = f.s.queueInitialApplyHealthRound(ctx, f.key, "owner", 501, InitialApplyHealthRoundInput{ID: routeTestID(1100), InitialApplySHA: state.InitialApplySHA}, healthEndpoint(), clock); !errors.Is(err, ErrConflict) {
		t.Fatal("late round committed", err)
	}
	if planCount(t, f.s, "pro_initial_rounds") != 0 {
		t.Fatal("late round partial row survived")
	}
	round, _, _ := initialCompleteRound(t, f, 1101)
	bad := InitialApplyHealthRoundInput{ID: round.ID, InitialApplySHA: strings.Repeat("f", 64)}
	if _, err = f.s.QueueInitialApplyHealthRound(ctx, f.key, "owner", 501, bad, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("round rebound to different apply", err)
	}
	// Any new protocol-health member nonce must belong to exactly one scope.
	if _, err = f.s.QueueMeteredHealth(ctx, f.key, round.ID, routeTestID(90), 501, healthEndpoint()); !errors.Is(err, ErrConflict) {
		t.Fatal("initial ID reused by old queue", err)
	}
}

func TestInitialApplyPersistedScopeNeverFallsBackAndLegacyIntentCollision(t *testing.T) {
	t.Run("persisted orphan is unavailable", func(t *testing.T) {
		f, _ := initialApplied(t)
		round, q, _ := initialCompleteRound(t, f, 1200)
		var guard string
		if err := f.s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='pro_initial_challenges_no_update'`).Scan(&guard); err != nil {
			t.Fatal(err)
		}
		for _, query := range []string{`PRAGMA foreign_keys=OFF`, `DROP TRIGGER pro_initial_challenges_no_update`, `UPDATE pro_initial_challenges SET round_id='ffffffffffffffffffffffffffffffff'`, guard, `PRAGMA foreign_keys=ON`} {
			if _, err := f.s.db.Exec(query); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := f.s.InitialApplyHealthChallengeScope(context.Background(), q.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatal("persisted initial challenge was treated as absent", err)
		}
		if _, err := f.s.InitialApplyHealthRound(context.Background(), f.key, "owner", 501, round.ID, healthEndpoint()); !errors.Is(err, ErrUnavailable) {
			t.Fatal("orphan receipt was treated as pending round", err)
		}
	})
	t.Run("legacy direct intent has no queue row", func(t *testing.T) {
		ctx := context.Background()
		f := healthApplied(t)
		if _, err := f.s.BootstrapOwner(ctx, "namespace-owner", "synthetic-initial-health-password"); err != nil {
			t.Fatal(err)
		}
		if err := f.s.EnableInitialApplyHealth(ctx); err != nil {
			t.Fatal(err)
		}
		id := routeTestID(1201)
		if _, err := f.s.IssueMeteredHealthChallenge(ctx, f.key, routeTestToken, id, f.in.ID, 10, healthEndpoint()); err != nil {
			t.Fatal(err)
		}
		if planCount(t, f.s, "pro_health_intents") != 0 {
			t.Fatal("fixture unexpectedly queued legacy intent")
		}
		_, err := f.s.QueueInitialApplyHealthRound(ctx, f.key, "owner", 501, InitialApplyHealthRoundInput{ID: id, InitialApplySHA: strings.Repeat("a", 64)}, healthEndpoint())
		if !errors.Is(err, ErrConflict) {
			t.Fatal("first-Apply ID reused a legacy direct intent", err)
		}
	})
}
