package business

import (
	"context"
	"errors"
	"strings"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func reviewedOwnerLoss(t *testing.T, f *executionFixture, inbound uint64, event, epoch string, revision uint64) ix.ReconciliationInput {
	t.Helper()
	in := ix.ReconciliationInput{EventID: event, EpochID: epoch, ExpectedRevision: revision, Action: "accept_owner_loss", Reason: "Accept only this unmeasured terminal tail as owner loss; preserve measured usage"}
	preview, err := f.s.PreviewInboundExecutionReconciliation(context.Background(), f.key, "owner", inbound, in)
	if err != nil || !preview.Valid {
		t.Fatal(preview, err)
	}
	in.ReviewSHA = preview.ReviewSHA
	return in
}

func TestManagedInboundOwnerLossReleasesOnlyReviewedEpochHold(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-owner-loss-scope-a-0001", "activate")
	a := f.bundle(t)
	ea := f.birth(t, a, "epoch-owner-loss-scope-a-0001")
	f.ready(t, a, ea)
	plan := copiedPlan(f.plan)
	plan.EventID = "execution-owner-loss-scope-plan-b-0001"
	plan.Profile.ID = 502
	plan.Profile.Name = "Independent second installation"
	plan.Nodes[0].NodeID, plan.Nodes[0].InstallationID = "remote-b", strings.Repeat("b", 32)
	plan.Nodes[0].ListenIP, plan.Nodes[0].DirectHosts = "198.51.100.12", []string{"b.example.org"}
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, plan); err != nil {
		t.Fatal(err)
	}
	cmd := ix.CommandInput{EventID: "execution-owner-loss-scope-b-0001", Action: "activate", ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Second reviewed installation"}
	p, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", 502, cmd)
	if err != nil || !p.Valid {
		t.Fatal(p, err)
	}
	cmd.ReviewSHA = p.ReviewSHA
	if _, err = f.s.QueueInboundExecution(ctx, f.key, "owner", 502, cmd); err != nil {
		t.Fatal(err)
	}
	b, found, err := f.s.InboundExecutionBundle(ctx, f.key, "remote-b", strings.Repeat("b", 32))
	if err != nil || !found {
		t.Fatal(err)
	}
	eb := f.birth(t, b, "epoch-owner-loss-scope-b-0001")
	f.ready(t, b, eb)
	for _, e := range []ix.EpochInput{ea, eb} {
		if err = f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, e.NodeID, e.InstallationID, e.EpochID, "This owned process lost its final observation"); err != nil {
			t.Fatal(err)
		}
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 2 {
		t.Fatal("independent lost tails were collapsed", account)
	}
	in := reviewedOwnerLoss(t, f, 501, "execution-owner-loss-scope-decision-0001", ea.EpochID, 1)
	if _, err = f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 || account.Eligible {
		t.Fatal("one owner's scoped decision released another epoch hold", account)
	}
	if _, found, err = f.s.InboundExecutionClearance(ctx, f.key, b.NodeID, b.InstallationID, eb.EpochID); err != nil || found {
		t.Fatal("clearance was manufactured for the unreviewed epoch", err)
	}
}

func TestManagedInboundOwnerLossReconciliationPreservesMeasuredUsageAndLateReports(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-reconcile-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-reconcile-0001")
	f.ready(t, b, e)
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 100, 200, false)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, b.NodeID, b.InstallationID, e.EpochID, "Owned process disappeared without terminal proof"); err != nil {
		t.Fatal(err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || len(state.Reconciliations) != 1 {
		t.Fatal("missing actionable uncertainty evidence", err)
	}
	view := state.Reconciliations[0]
	if view.Revision != 2 || !view.HasMeasuredUsage || len(view.KnownUsage) != 1 || view.KnownUsage[0].Upload != 100 || !view.KnownUsage[0].Held || len(view.AffectedClientIDs) != 1 || len(view.AffectedInboundIDs) != 1 || view.UnknownUsagePolicy != "unmeasured" {
		t.Fatal("uncertainty was replaced by invented final values", view)
	}
	in := reviewedOwnerLoss(t, f, 501, "execution-owner-loss-0001", e.EpochID, 2)
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 3, 110, 220, false)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in); !errors.Is(err, ErrConflict) {
		t.Fatal("stale financial review committed", err)
	}
	in = reviewedOwnerLoss(t, f, 501, "execution-owner-loss-0001", e.EpochID, 3)
	receipt, err := f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in)
	if err != nil || receipt.Revision != 3 || receipt.State != "accepted_owner_loss" || receipt.UnknownUsagePolicy != "owner_loss" || receipt.KnownUsage[0].Upload != 110 {
		t.Fatal(receipt, err)
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Total != 330 || account.Holds != 0 || !account.Eligible {
		t.Fatal("owner loss changed measured usage or wrong holds", account, err)
	}
	var finals int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=? AND final=1`, e.EpochID).Scan(&finals); err != nil || finals != 0 {
		t.Fatal("owner action fabricated a final receipt", finals, err)
	}
	clearance, found, err := f.s.InboundExecutionClearance(ctx, f.key, b.NodeID, b.InstallationID, e.EpochID)
	if err != nil || !found || ix.VerifyClearance(f.key, clearance) != nil || clearance.EpochSHA != ix.Digest(e) || clearance.KnownSequence != 3 {
		t.Fatal("missing exact scoped clearance", clearance, err)
	}
	if _, _, err = f.s.InboundExecutionClearance(ctx, f.key, "foreign-node", b.InstallationID, e.EpochID); !errors.Is(err, ErrNotFound) {
		t.Fatal("clearance escaped installation scope", err)
	}
	replay, err := f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in)
	if err != nil || !replay.Replayed || replay.EventID != receipt.EventID {
		t.Fatal("reconciliation replay", err)
	}
	conflict := in
	conflict.Reason = "A different owner decision"
	if _, err = f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, conflict); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting owner event replay", err)
	}
	late := executionReport(b, e, 4, 115, 230, false)
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, late); err != nil {
		t.Fatal("owner loss discarded a later recovered measured sample", err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, late); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 5, 117, 235, true)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 6, 118, 236, false)); !errors.Is(err, ErrConflict) {
		t.Fatal("physical final receipt was reopened", err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Total != 352 || account.Holds != 0 {
		t.Fatal("known replay duplicated or dropped bytes", account)
	}
	stored, err := f.s.InboundExecutionReconciliationReceipt(ctx, f.key, "owner", 501, in.EventID)
	if err != nil || stored.Revision != 3 || stored.KnownUsage[0].Upload != 110 || stored.KnownUsage[0].Download != 220 {
		t.Fatal("later reports mutated the owner's immutable review", stored, err)
	}
	state, err = f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || len(state.Reconciliations) != 0 || state.PublicationState != "withheld" {
		t.Fatal("reconciled stopped incarnation became live evidence", state, err)
	}
	if _, err = f.s.RegisterInboundExecutionEpoch(ctx, f.key, executionEpoch(b, "epoch-after-reconciliation-0001")); err != nil {
		t.Fatal("explicit resolution did not release the old incarnation fence", err)
	}
}

func TestManagedInboundOwnerLossWithoutBaselineNeverFabricatesZero(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-reconcile-no-baseline-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-reconcile-no-baseline-0001")
	if err := f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, b.NodeID, b.InstallationID, e.EpochID, "No accepted baseline or terminal observation survived"); err != nil {
		t.Fatal(err)
	}
	in := reviewedOwnerLoss(t, f, 501, "execution-owner-loss-no-baseline-0001", e.EpochID, 0)
	preview, err := f.s.PreviewInboundExecutionReconciliation(ctx, f.key, "owner", 501, in)
	if err != nil || preview.State.HasMeasuredUsage || len(preview.State.KnownUsage) != 0 || len(preview.State.AffectedClientIDs) != 1 {
		t.Fatal("unobserved counters were presented as measured zero", preview, err)
	}
	if _, err = f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=?`, e.EpochID).Scan(&count); err != nil || count != 0 {
		t.Fatal("missing baseline was replaced by a synthetic receipt", count, err)
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 0 || account.Total != 0 {
		t.Fatal(account)
	}
}

func TestManagedInboundOwnerLossRequiresOwnerAndRollsBackAllFinancialEffects(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-reconcile-atomic-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-reconcile-atomic-0001")
	f.ready(t, b, e)
	if err := f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, b.NodeID, b.InstallationID, e.EpochID, "Lost process incarnation"); err != nil {
		t.Fatal(err)
	}
	in := reviewedOwnerLoss(t, f, 501, "execution-owner-loss-atomic-0001", e.EpochID, 1)
	newRep(t, f.s, "reconcile-rep")
	if _, err := f.s.ProvisionRepresentativeLogin(ctx, "reconcile-rep", "reconcile-rep", "synthetic-reconciliation-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.PreviewInboundExecutionReconciliation(ctx, f.key, "reconcile-rep", 501, in); !errors.Is(err, ErrConflict) {
		t.Fatal("representative reviewed owner financial loss", err)
	}
	if _, err := f.s.ReconcileInboundExecution(ctx, f.key, "reconcile-rep", 501, in); !errors.Is(err, ErrConflict) {
		t.Fatal("representative released financial hold", err)
	}
	if _, err := f.s.db.Exec(`CREATE TRIGGER execution_test_reconciliation_fail BEFORE UPDATE OF state ON inbound_execution_epochs WHEN NEW.state='sealed' BEGIN SELECT RAISE(ABORT,'synthetic reconciliation commit failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in); err == nil {
		t.Fatal("failed transaction returned a reconciliation receipt")
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 {
		t.Fatal("hold release escaped failed transaction", account)
	}
	if _, err := f.s.InboundExecutionReconciliationReceipt(ctx, f.key, "owner", 501, in.EventID); !errors.Is(err, ErrNotFound) {
		t.Fatal("failed transaction retained owner receipt", err)
	}
	if _, err := f.s.db.Exec(`DROP TRIGGER execution_test_reconciliation_fail`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec(`UPDATE inbound_execution_reconciliations SET actor_id='another' WHERE event_id=?`, in.EventID); err == nil {
		t.Fatal("owner loss audit was mutable")
	}
	if _, err := f.s.db.Exec(`UPDATE auth_users SET disabled=1 WHERE id='owner'`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in); !errors.Is(err, ErrConflict) {
		t.Fatal("historical replay bypassed present owner authorization", err)
	}
}
