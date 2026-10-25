package business

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func futureExecutionSweep(t *testing.T, f *executionFixture) (bool, error) {
	t.Helper()
	ctx := context.Background()
	tx, err := executionTx(f.s, ctx, f.key)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	changed, err := sweepExecutionLiveness(ctx, tx, f.key, time.Now().Add(46*time.Second).UnixNano())
	if err != nil {
		return false, err
	}
	return changed, tx.Commit()
}

func TestManagedInboundOfflineTailHoldsAndAuthenticFinalSettlesAtomically(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-offline-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-offline-0001")
	f.ready(t, b, e)
	tx, err := executionTx(f.s, ctx, f.key)
	if err != nil {
		t.Fatal(err)
	}
	stale, err := executionClientHasStaleEpoch(ctx, tx, f.client.ID, time.Now().Add(46*time.Second).UnixNano())
	tx.Rollback()
	if err != nil || !stale {
		t.Fatal("read-time cross-node admission guard missed an expired incarnation", err)
	}
	changed, err := futureExecutionSweep(t, f)
	if err != nil || !changed {
		t.Fatal("timeout hold missing", changed, err)
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Holds != 1 || account.Eligible {
		t.Fatal("offline tail did not hold the shared account", account, err)
	}
	changed, err = futureExecutionSweep(t, f)
	if err != nil || changed {
		t.Fatal("timeout sweep not idempotent", changed, err)
	}
	if err = f.s.InboundExecutionAuthority(ctx, f.key, b.NodeID, b.InstallationID, b.Generation); !errors.Is(err, ErrConflict) {
		t.Fatal("held account received lease authority", err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 10, 20, false)); err != nil {
		t.Fatal("known cumulative bytes refused after timeout", err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 || account.Total != 30 {
		t.Fatal("ordinary report released uncertainty or lost bytes", account)
	}
	final := executionReport(b, e, 3, 15, 25, true)
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, final); err != nil {
		t.Fatal("durable real final did not settle provisional tail", err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 0 || account.Total != 40 || !account.Eligible {
		t.Fatal("final bytes and hold were not settled together", account)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, final); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 0 || account.Total != 40 {
		t.Fatal("final replay decremented another hold or duplicated bytes", account)
	}
}

func TestManagedInboundOfflineHoldPromotesWithoutForgivingUncertainTail(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-offline-uncertain-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-offline-uncertain-0001")
	f.ready(t, b, e)
	if _, err := futureExecutionSweep(t, f); err != nil {
		t.Fatal(err)
	}
	if err := f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, b.NodeID, b.InstallationID, e.EpochID, "Agent lost its owned process incarnation"); err != nil {
		t.Fatal(err)
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 {
		t.Fatal("provisional hold counted twice on confirmation", account)
	}
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 10, 20, true)); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 || account.Total != 30 || account.Eligible {
		t.Fatal("late known bytes forgave an unknown terminal tail", account)
	}
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 3, 20, 30, false)); !errors.Is(err, ErrConflict) {
		t.Fatal("uncertain state permitted new bytes after a terminal receipt", err)
	}
	var kind string
	if err := f.s.db.QueryRow(`SELECT kind FROM inbound_execution_liveness_resolutions WHERE epoch_id=? AND client_id=?`, e.EpochID, f.client.ID).Scan(&kind); err != nil || kind != "uncertain" {
		t.Fatal("provisional-to-permanent audit missing", kind, err)
	}
}

func TestManagedInboundOfflineFinalFailureRollsBackBytesReceiptAndHoldRelease(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-offline-atomic-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-offline-atomic-0001")
	f.ready(t, b, e)
	if _, err := futureExecutionSweep(t, f); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.db.Exec(`CREATE TRIGGER execution_test_fail_final BEFORE UPDATE OF state ON inbound_execution_epochs WHEN NEW.state='sealed' BEGIN SELECT RAISE(ABORT,'synthetic terminal commit failure'); END`); err != nil {
		t.Fatal(err)
	}
	final := executionReport(b, e, 2, 5, 7, true)
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final); err == nil {
		t.Fatal("failed terminal transaction returned a receipt")
	}
	account, err := f.s.ClientUsage(ctx, f.client.ID)
	if err != nil || account.Total != 0 || account.Holds != 1 {
		t.Fatal("financial state partially committed", account, err)
	}
	var receipts, resolutions int
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=?`, e.EpochID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err = f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_liveness_resolutions WHERE epoch_id=?`, e.EpochID).Scan(&resolutions); err != nil || receipts != 1 || resolutions != 0 {
		t.Fatal("failed final retained a receipt or released its hold", receipts, resolutions, err)
	}
	if _, err = f.s.db.Exec(`DROP TRIGGER execution_test_fail_final`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, final); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Total != 12 || account.Holds != 0 {
		t.Fatal("exact retry failed to settle the complete final transaction", account)
	}
}

func TestManagedInboundFinalSettlesOnlyItsOwnOfflineNodeHold(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-offline-nodes-a-0001", "activate")
	a := f.bundle(t)
	ea := f.birth(t, a, "epoch-offline-nodes-a-0001")
	f.ready(t, a, ea)
	plan := copiedPlan(f.plan)
	plan.EventID = "execution-offline-nodes-plan-b-0001"
	plan.Profile.ID = 502
	plan.Profile.Name = "Second independent installation"
	plan.Nodes[0].NodeID = "remote-b"
	plan.Nodes[0].InstallationID = strings.Repeat("b", 32)
	plan.Nodes[0].ListenIP = "198.51.100.12"
	plan.Nodes[0].DirectHosts = []string{"b.example.org"}
	if _, err := f.s.SaveNativeInboundPlan(ctx, f.key, plan); err != nil {
		t.Fatal(err)
	}
	in := ix.CommandInput{EventID: "execution-offline-nodes-b-0001", Action: "activate", ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Second reviewed installation"}
	preview, err := f.s.PreviewInboundExecution(ctx, f.key, "owner", 502, in)
	if err != nil || !preview.Valid {
		t.Fatal(err, preview.Blockers)
	}
	in.ReviewSHA = preview.ReviewSHA
	if _, err = f.s.QueueInboundExecution(ctx, f.key, "owner", 502, in); err != nil {
		t.Fatal(err)
	}
	b, found, err := f.s.InboundExecutionBundle(ctx, f.key, "remote-b", strings.Repeat("b", 32))
	if err != nil || !found {
		t.Fatal(err)
	}
	eb := f.birth(t, b, "epoch-offline-nodes-b-0001")
	f.ready(t, b, eb)
	if _, err = futureExecutionSweep(t, f); err != nil {
		t.Fatal(err)
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 2 {
		t.Fatal("independent node tails were collapsed", account)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(a, ea, 2, 3, 5, true)); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 || account.Eligible || account.Total != 8 {
		t.Fatal("one final cleared another node's tail", account)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, eb, 2, 7, 11, true)); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 0 || !account.Eligible || account.Total != 26 {
		t.Fatal("independent final receipts were not conserved", account)
	}
}
