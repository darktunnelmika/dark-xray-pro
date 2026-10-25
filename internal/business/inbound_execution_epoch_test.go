package business

import (
	"context"
	"errors"
	"strings"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestManagedInboundUnregisteredClosedBirthSettlesOnlyRealZeroFinal(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-absent-birth-activate-0001", "activate")
	b := f.bundle(t)
	e := executionEpoch(b, "epoch-unregistered-closed-0001")
	if _, found, err := f.s.LookupInboundExecutionEpochReceipt(ctx, f.key, e); err != nil || found {
		t.Fatal("absent birth looked registered", found, err)
	}
	if _, err := f.s.SetClientState(ctx, SetClientState{EventID: "execution-absent-birth-revoke-0001", ClientID: f.client.ID, Action: "disable", Reason: "Withdrawal before the registration reached the HUB"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RefreshInboundExecutionEligibility(ctx, f.key, b.NodeID, b.InstallationID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RegisterInboundExecutionEpoch(ctx, f.key, e); !errors.Is(err, ErrConflict) {
		t.Fatal("historical bundle granted a new admission registration", err)
	}
	final := executionReport(b, e, 1, 0, 0, true)
	final.InitialEpoch = &e
	for _, tc := range []struct {
		name string
		edit func(*ix.UsageReport)
	}{
		{"ordinary_zero", func(r *ix.UsageReport) { r.Final = false; r.FinalNonce = ""; r.FinalProofSHA = "" }},
		{"missing_nonce", func(r *ix.UsageReport) { r.FinalNonce = "" }},
		{"foreign_process", func(r *ix.UsageReport) { r.ProcessIdentity = "foreign"; r.FinalProofSHA = executionFinalSHA(*r) }},
		{"nonzero_without_baseline", func(r *ix.UsageReport) {
			r.Counters = append([]ix.Counter(nil), r.Counters...)
			r.Counters[0].Upload = 7
			r.FinalProofSHA = executionFinalSHA(*r)
		}},
		{"not_initial_sequence", func(r *ix.UsageReport) { r.Sequence = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := final
			tc.edit(&bad)
			if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, bad); err == nil {
				t.Fatal("unproven or nonzero unknown birth was silently settled")
			}
			if _, found, err := f.s.LookupInboundExecutionEpochReceipt(ctx, f.key, e); err != nil || found {
				t.Fatal("rejected final left a partially registered birth", found, err)
			}
		})
	}
	receipt, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final)
	if err != nil || !receipt.Final || receipt.SHA256 != ix.Digest(final) {
		t.Fatal("genuine closed birth could not settle after withdrawal", receipt, err)
	}
	old, found, err := f.s.LookupInboundExecutionEpochReceipt(ctx, f.key, e)
	if err != nil || !found || ix.Digest(old) != ix.Digest(e) {
		t.Fatal("settled historical birth lost its exact identity", err)
	}
	replay, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final)
	if err != nil || !replay.Replayed || replay.SHA256 != receipt.SHA256 {
		t.Fatal("closed birth final replay changed receipt", err)
	}
	if _, err = f.s.RegisterInboundExecutionEpoch(ctx, f.key, e); !errors.Is(err, ErrConflict) {
		t.Fatal("historical lookup reopened a sealed epoch", err)
	}
	wrong := e
	wrong.BinarySHAs = map[string]string{"xray": strings.Repeat("f", 64)}
	if _, _, err = f.s.LookupInboundExecutionEpochReceipt(ctx, f.key, wrong); !errors.Is(err, ErrConflict) {
		t.Fatal("recorded pin provenance accepted a substitution", err)
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Total != 0 || account.Holds != 0 || account.Eligible {
		t.Fatal("settlement granted entitlement or changed measured bytes", account)
	}
}

func TestManagedInboundLostUnregisteredBirthRecordsUncertaintyWithoutBaseline(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-absent-lost-activate-0001", "activate")
	b := f.bundle(t)
	e := executionEpoch(b, "epoch-unregistered-lost-0001")
	o := executionObservation(b, e)
	o.State, o.ProcessReady, o.HealthState = "failed", false, "unknown"
	o.ErrorCode, o.AccountingState, o.InitialEpoch = "accounting_tail_uncertain", "uncertain", &e
	bad := o
	bad.ProcessReady = true
	if err := f.s.ObserveInboundExecution(ctx, f.key, bad); !errors.Is(err, ErrInvalid) {
		t.Fatal("uncertainty metadata granted ready admission", err)
	}
	if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal("unknown crashed incarnation could not be held", err)
	}
	if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal("uncertainty identity replay failed", err)
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 1 || account.Total != 0 || account.Eligible {
		t.Fatal("unknown birth hold was omitted or doubled", account)
	}
	var n int
	if err := f.s.db.QueryRow(`SELECT COUNT(*) FROM inbound_execution_usage_receipts WHERE epoch_id=?`, e.EpochID).Scan(&n); err != nil || n != 0 {
		t.Fatal("crashed birth invented a zero baseline", n, err)
	}
	state, err := f.s.InboundExecution(ctx, f.key, 501)
	if err != nil || len(state.Reconciliations) != 1 || state.Reconciliations[0].HasMeasuredUsage || len(state.Reconciliations[0].KnownUsage) != 0 {
		t.Fatal("owner review did not expose unmeasured loss honestly", err)
	}
	in := reviewedOwnerLoss(t, f, 501, "execution-absent-lost-owner-loss-0001", e.EpochID, 0)
	if _, err = f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in); err != nil {
		t.Fatal(err)
	}
	account, _ = f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 0 || account.Total != 0 {
		t.Fatal("explicit owner-loss decision did not release just this unknown tail", account)
	}
}

func TestManagedInboundMigrationRefusesLegacyGenerationNine(t *testing.T) {
	s, _ := opened(t)
	ctx := context.Background()
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableSystemProbeAccounting(ctx); err != nil {
		t.Fatal(err)
	}
	var before, after int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&before); err != nil || before != 9 {
		t.Fatal("fixture did not create a real legacy schema", before, err)
	}
	if err := s.EnableInboundExecution(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("managed execution silently migrated a legacy production contract", err)
	}
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&after); err != nil || after != before {
		t.Fatal("refused migration changed the legacy reader fence", after, err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'inbound_execution_*'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("refused migration left managed namespace debris", count, err)
	}
}

func TestManagedInboundBinaryApprovalPreservesHistoricalSettlementOnly(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "execution-pins-activate-0001", "activate")
	b := f.bundle(t)
	e := executionEpoch(b, "epoch-historical-approved-0001")
	if found, err := f.s.LookupInboundExecutionBinaryApproval(ctx, f.key, e); err != nil || found {
		t.Fatal("an undelivered bundle invented engine approval", found, err)
	}
	if err := f.s.RecordInboundExecutionBinaryApproval(ctx, f.key, b.NodeID, b.InstallationID, b.Generation, e.BinarySHAs); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecordInboundExecutionBinaryApproval(ctx, f.key, b.NodeID, b.InstallationID, b.Generation, e.BinarySHAs); err != nil {
		t.Fatal("exact delivery pin replay failed", err)
	}
	if found, err := f.s.LookupInboundExecutionBinaryApproval(ctx, f.key, e); err != nil || !found {
		t.Fatal("delivered binary provenance missing", found, err)
	}
	if _, err := f.s.db.Exec(`UPDATE inbound_execution_binary_approvals SET config_sha=?`, strings.Repeat("a", 64)); err == nil {
		t.Fatal("delivered binary provenance was mutable")
	}
	if _, err := f.s.SetClientState(ctx, SetClientState{EventID: "execution-pins-revoke-0001", ClientID: f.client.ID, Action: "disable", Reason: "Withdraw admission before recovery"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RefreshInboundExecutionEligibility(ctx, f.key, b.NodeID, b.InstallationID); err != nil {
		t.Fatal(err)
	}
	if found, err := f.s.LookupInboundExecutionBinaryApproval(ctx, f.key, e); err != nil || !found {
		t.Fatal("withdrawal erased historical settlement provenance", found, err)
	}
	if err := f.s.RecordInboundExecutionBinaryApproval(ctx, f.key, b.NodeID, b.InstallationID, b.Generation, e.BinarySHAs); !errors.Is(err, ErrConflict) {
		t.Fatal("historical approval renewed a withdrawn lease", err)
	}
	if _, err := f.s.RegisterInboundExecutionEpoch(ctx, f.key, e); !errors.Is(err, ErrConflict) {
		t.Fatal("settlement provenance granted new admission", err)
	}
	final := executionReport(b, e, 1, 0, 0, true)
	final.InitialEpoch = &e
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, final); err != nil {
		t.Fatal("historically approved closed birth could not settle", err)
	}
	wrong := e
	wrong.NodeID = "foreign-node"
	if found, err := f.s.LookupInboundExecutionBinaryApproval(ctx, f.key, wrong); err == nil && found {
		t.Fatal("delivered pins escaped their exact node bundle")
	}
}
