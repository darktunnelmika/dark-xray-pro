package business

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func appliedPublicationFixture(t *testing.T) (*executionFixture, ix.Bundle, ix.EpochInput, SubscriptionSecret, ix.Subscription) {
	t.Helper()
	f := newExecutionFixture(t)
	f.command(t, "publication-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "publication-initial-epoch-0001")
	f.ready(t, b, e)
	ctx := context.Background()
	if _, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID,
		SubscriptionTokenMutation{EventID: "publication-issue-0001", Action: "issue"}); err != nil {
		t.Fatal(err)
	}
	secret, err := f.s.RevealInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri")
	if err != nil || len(doc.Routes) != 1 || !doc.RuntimeReady || !doc.PathVerified {
		t.Fatal("initial applied publication", err)
	}
	if doc.ReadinessExpiresUnixNano <= time.Now().UnixNano() || doc.ExpiresUnixNano <= doc.ReadinessExpiresUnixNano {
		t.Fatal("missing independent readiness deadline")
	}
	return f, b, e, secret, doc
}

// Store a synthetic report in this test's private SQLite fixture to model
// elapsed transport/lease time without sleeping or changing a production clock.
func publicationObservation(t *testing.T, f *executionFixture, b ix.Bundle, o ix.Observation) {
	t.Helper()
	enc, err := sealPlanValue(f.key, executionAAD("observation", b.NodeID+"/"+b.InstallationID, b.Generation), o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`UPDATE inbound_execution_nodes SET observation_enc=? WHERE node_id=?`, enc, b.NodeID); err != nil {
		t.Fatal(err)
	}
}

func TestManagedInboundPublicationHealthEvidenceExpiresSeparately(t *testing.T) {
	cases := []struct {
		name   string
		change func(*ix.Observation)
	}{
		{"stale_observation", func(o *ix.Observation) { o.ObservedUnixNano = time.Now().Add(-16 * time.Second).UnixNano() }},
		{"expired_lease", func(o *ix.Observation) { o.LeaseExpires = time.Now().Add(-time.Second).UnixNano() }},
		{"stopped", func(o *ix.Observation) { o.State = "stopped"; o.ProcessReady = false; o.AccountingState = "settled" }},
		{"not_ready", func(o *ix.Observation) { o.State = "failed"; o.ProcessReady = false; o.HealthState = "unhealthy" }},
		{"unconfirmed_accounting", func(o *ix.Observation) { o.AccountingState = "pending" }},
		{"unknown_epoch", func(o *ix.Observation) { o.EpochID = "publication-unknown-epoch-0001" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, b, e, secret, initial := appliedPublicationFixture(t)
			o := executionObservation(b, e)
			tc.change(&o)
			publicationObservation(t, f, b, o)
			before := publicationStateDigest(t, f)
			got, err := f.s.ReadInboundExecutionSubscription(context.Background(), f.key, secret.Token, "uri")
			if err != nil || !bytes.Equal(got.Content, initial.Content) || got.SHA256 != initial.SHA256 || got.ExpiresUnixNano != initial.ExpiresUnixNano || got.RuntimeReady || got.PathVerified || got.ReadinessExpiresUnixNano != 0 {
				t.Fatal("health evidence changed credential authority or was overstated", err)
			}
			if publicationStateDigest(t, f) != before {
				t.Fatal("read renewed admission or changed accounting")
			}
		})
	}
	t.Run("engine_ready_is_not_path_proof", func(t *testing.T) {
		f, b, e, secret, _ := appliedPublicationFixture(t)
		o := executionObservation(b, e)
		o.HealthState = "engine_ready"
		publicationObservation(t, f, b, o)
		got, err := f.s.ReadInboundExecutionSubscription(context.Background(), f.key, secret.Token, "uri")
		if err != nil || !got.RuntimeReady || got.PathVerified || got.ReadinessExpiresUnixNano <= time.Now().UnixNano() {
			t.Fatal("engine readiness confused with verified path", err)
		}
	})
	t.Run("readiness_does_not_outlive_earliest_evidence", func(t *testing.T) {
		f, b, e, secret, _ := appliedPublicationFixture(t)
		o := executionObservation(b, e)
		o.LeaseExpires = time.Now().Add(2 * time.Second).UnixNano()
		publicationObservation(t, f, b, o)
		got, err := f.s.ReadInboundExecutionSubscription(context.Background(), f.key, secret.Token, "uri")
		if err != nil || !got.RuntimeReady || got.ReadinessExpiresUnixNano != o.LeaseExpires || got.ExpiresUnixNano <= got.ReadinessExpiresUnixNano {
			t.Fatal("lease deadline extended or used as credential expiry", err)
		}
	})
}

func TestManagedInboundPublicationStaleAccountingDoesNotRenewAdmission(t *testing.T) {
	f, b, e, secret, initial := appliedPublicationFixture(t)
	ctx := context.Background()
	future := time.Now().Add(46 * time.Second).UnixNano()
	o := executionObservation(b, e)
	o.ObservedUnixNano = future
	o.LeaseExpires = future + int64(25*time.Second)
	publicationObservation(t, f, b, o)
	before := publicationStateDigest(t, f)
	tx, err := executionTx(f.s, ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := executionClientHasStaleEpoch(ctx, tx, f.client.ID, future)
	if err != nil || !stale {
		tx.Rollback()
		t.Fatal("runtime accounting guard did not remain closed", err)
	}
	routes, err := executionSubscriptionRoutesAt(ctx, tx, f.key, f.client.ID, future)
	if err != nil {
		tx.Rollback()
		t.Fatal("stale accounting withdrew an otherwise authorized document", err)
	}
	ready, verified, until := executionSubscriptionReadiness(routes, future)
	tx.Rollback()
	if ready || verified || until != 0 {
		t.Fatal("stale accounting reported runtime readiness")
	}
	got, err := renderExecutionSubscription(f.client.ID, "uri", routes)
	if err != nil || !bytes.Equal(got.Content, initial.Content) || got.RuntimeReady || got.ReadinessExpiresUnixNano != 0 {
		t.Fatal("stale accounting altered document", err)
	}
	if publicationStateDigest(t, f) != before {
		t.Fatal("publication renewed or settled a stale epoch")
	}
	// The existing real liveness transition still installs its account hold.
	if _, err = futureExecutionSweep(t, f); err != nil {
		t.Fatal(err)
	}
	if err = f.s.InboundExecutionAuthority(ctx, f.key, b.NodeID, b.InstallationID, b.Generation); !errors.Is(err, ErrConflict) {
		t.Fatal("held accounting gained execution authority", err)
	}
	if _, err = f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri"); !errors.Is(err, ErrConflict) {
		t.Fatal("real accounting hold was ignored", err)
	}
}

func TestManagedInboundPublicationRetainsAuthorizationAndIdentityFences(t *testing.T) {
	cases := []struct {
		name   string
		change func(*testing.T, *executionFixture, ix.Bundle, ix.EpochInput)
	}{
		{"client_disabled", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			if _, err := f.s.SetClientState(context.Background(), SetClientState{EventID: "publication-disable-client-0001", ClientID: f.client.ID, Action: "disable", Reason: "Publication authorization regression"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"client_archived", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			if _, err := f.s.SetClientState(context.Background(), SetClientState{EventID: "publication-archive-client-0001", ClientID: f.client.ID, Action: "archive", Reason: "Publication authorization regression"}); err != nil {
				t.Fatal(err)
			}
		}},
		{"client_expired", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			if _, err := f.s.db.Exec(`UPDATE clients SET expires_unix=? WHERE id=?`, time.Now().Add(-time.Minute).Unix(), f.client.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"account_hold", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			if _, err := f.s.db.Exec(`UPDATE pro_usage_accounts SET holds=1 WHERE client_id=?`, f.client.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"unknown_baseline", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			if _, err := f.s.db.Exec(`UPDATE pro_usage_accounts SET baseline_complete=0 WHERE client_id=?`, f.client.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"quota_exhausted", func(t *testing.T, f *executionFixture, b ix.Bundle, e ix.EpochInput) {
			if _, err := f.s.AcceptInboundExecutionUsage(context.Background(), f.key, executionReport(b, e, 2, 1<<20, 0, false)); err != nil {
				t.Fatal(err)
			}
		}},
		{"credential_changed", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			if _, err := f.s.db.Exec(`UPDATE clients SET uuid=? WHERE id=?`, "22222222-2222-4222-8222-222222222222", f.client.ID); err != nil {
				t.Fatal(err)
			}
		}},
		{"operator_stop", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			f.command(t, "publication-operator-stop-0001", "stop")
		}},
		{"operator_archive", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			f.command(t, "publication-operator-archive-0001", "archive")
		}},
		{"new_revision_unaccepted", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			f.command(t, "publication-new-revision-0001", "apply")
		}},
		{"revoked_token", func(t *testing.T, f *executionFixture, _ ix.Bundle, _ ix.EpochInput) {
			if _, err := f.s.MutateInboundExecutionSubscription(context.Background(), f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: "publication-revoke-0001", Action: "revoke", ExpectedGeneration: 1}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, b, e, secret, _ := appliedPublicationFixture(t)
			tc.change(t, f, b, e)
			before := publicationStateDigest(t, f)
			if _, err := f.s.ReadInboundExecutionSubscription(context.Background(), f.key, secret.Token, "uri"); err == nil {
				t.Fatal("publication bypassed withdrawal or identity change")
			}
			if publicationStateDigest(t, f) != before {
				t.Fatal("denied read changed authority/accounting")
			}
		})
	}
}

func TestManagedInboundPublicationDoesNotExposePreparedInitialApply(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "publication-prepared-initial-0001", "activate")
	b := f.bundle(t)
	for _, state := range []string{"prepared", "failed"} {
		o := ix.Observation{NodeID: b.NodeID, InstallationID: b.InstallationID, Generation: b.Generation, OperationID: b.OperationID, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, State: state, ObservedUnixNano: time.Now().UnixNano(), ProcessReady: false}
		if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.ProjectInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, "uri"); err == nil {
			t.Fatal("unaccepted initial snapshot exported", state)
		}
	}
}

func publicationCommand(t *testing.T, f *executionFixture, inbound uint64, event, action string, clients []string) ix.Operation {
	t.Helper()
	ctx := context.Background()
	state, err := f.s.InboundExecution(ctx, f.key, inbound)
	if err != nil {
		t.Fatal(err)
	}
	in := ix.CommandInput{EventID: event, Action: action, ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: clients, AccountingMode: ix.AccountingPerClient, Reason: "Publication isolation regression"}
	preview, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", inbound, in)
	if err != nil || !preview.Valid {
		t.Fatal("preview", action, err, preview.Blockers)
	}
	in.ReviewSHA = preview.ReviewSHA
	op, err := f.s.QueueInboundExecution(ctx, f.key, "owner", inbound, in)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func publicationSibling(t *testing.T, f *executionFixture, otherClient string, secondNode bool) {
	t.Helper()
	plan := copiedPlan(f.plan)
	plan.EventID = "publication-sibling-create-0001"
	plan.Profile.ID = 502
	plan.Profile.Name = "Sibling publication"
	plan.Nodes[0].DirectPort = 21186
	if secondNode {
		plan.Nodes = append(plan.Nodes, NativeInboundPlanNode{NodeID: "remote-y", InstallationID: strings.Repeat("c", 32), ListenIP: "198.51.100.12", DirectPort: 21186, DirectHosts: []string{"y.example.org"}, TunnelHosts: []string{}})
	}
	if _, err := f.s.SaveNativeInboundPlan(context.Background(), f.key, plan); err != nil {
		t.Fatal(err)
	}
	publicationCommand(t, f, 502, "publication-sibling-activate-0001", "activate", []string{otherClient})
}

func publicationHandoff(t *testing.T, f *executionFixture, old ix.Bundle, epoch ix.EpochInput, id string) (ix.Bundle, ix.EpochInput) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(old, epoch, 2, 0, 0, true)); err != nil {
		t.Fatal("seal previous generation", err)
	}
	next := f.bundle(t)
	born := f.birth(t, next, id)
	f.ready(t, next, born)
	return next, born
}

func TestManagedInboundPublicationUnrelatedPendingTargetDoesNotBlockAppliedRoute(t *testing.T) {
	f, b, e, secret, initial := appliedPublicationFixture(t)
	other := usageClient(t, f.s, "publication-other-client", 1<<20)
	publicationSibling(t, f, other.ID, true)
	current, epoch := publicationHandoff(t, f, b, e, "publication-pending-sibling-epoch")
	pending, err := f.s.InboundExecution(context.Background(), f.key, 502)
	if err != nil || pending.ActiveRevision != 0 || pending.DesiredRevision == 0 {
		t.Fatal("sibling unexpectedly completed", err)
	}
	for _, stale := range []bool{false, true} {
		o := executionObservation(current, epoch)
		if stale {
			o.ObservedUnixNano = time.Now().Add(-16 * time.Second).UnixNano()
		}
		publicationObservation(t, f, current, o)
		got, err := f.s.ReadInboundExecutionSubscription(context.Background(), f.key, secret.Token, "uri")
		if err != nil || len(got.Routes) != 1 || !bytes.Equal(got.Content, initial.Content) || got.RuntimeReady == stale {
			t.Fatal("unrelated pending Y coupled unchanged A(X)", stale, err)
		}
	}
	if _, err = f.s.ProjectInboundExecutionSubscription(context.Background(), f.key, "owner", other.ID, "uri"); err == nil {
		t.Fatal("unaccepted sibling credentials leaked")
	}
}

func TestManagedInboundPublicationSurvivesCompletedSiblingWithdrawal(t *testing.T) {
	for _, action := range []string{"stop", "archive", "dormant"} {
		t.Run(action, func(t *testing.T) {
			f, b, e, secret, initial := appliedPublicationFixture(t)
			ctx := context.Background()
			other := usageClient(t, f.s, "publication-sibling-client", 1<<20)
			publicationSibling(t, f, other.ID, false)
			current, epoch := publicationHandoff(t, f, b, e, "publication-sibling-active-epoch")
			if action == "dormant" {
				if _, err := f.s.SetClientState(context.Background(), SetClientState{EventID: "publication-disable-sibling-0001", ClientID: other.ID, Action: "disable", Reason: "Dormant sibling publication regression"}); err != nil {
					t.Fatal(err)
				}
				changed, err := f.s.RefreshInboundExecutionEligibility(ctx, f.key, current.NodeID, current.InstallationID)
				if err != nil || !changed {
					t.Fatal("dormant refresh", err)
				}
			} else {
				publicationCommand(t, f, 502, "publication-sibling-"+action+"-0001", action, []string{other.ID})
			}
			publicationHandoff(t, f, current, epoch, "publication-sibling-after-"+action)
			sibling, err := f.s.InboundExecution(ctx, f.key, 502)
			if err != nil || sibling.ActiveRevision != 0 {
				t.Fatal("sibling withdrawal not committed", err)
			}
			got, err := f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri")
			if err != nil || !bytes.Equal(got.Content, initial.Content) || !got.RuntimeReady {
				t.Fatal("completed sibling removed unchanged applied credentials", action, err)
			}
		})
	}
}
