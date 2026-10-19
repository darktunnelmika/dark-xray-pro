//go:build linux

package managedxray

import (
	"context"
	"os"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type transactionRealityExecutor struct {
	t       *testing.T
	runtime *TransactionRuntime
	options Options
	serial  uint64
}

func (e *transactionRealityExecutor) Apply(ctx context.Context, v Revision, lease Lease) (Snapshot, error) {
	e.serial++
	stage := transactionCommand(e.t, e.runtime, e.options, "stage", e.serial, v.Sequence, "", v.Candidate)
	if _, err := e.runtime.Execute(ctx, stage, v.Candidate); err != nil {
		return Snapshot{}, err
	}
	e.serial++
	apply := transactionCommand(e.t, e.runtime, e.options, "apply", e.serial, v.Sequence, stage.ID, v.Candidate)
	apply.Lease = lease
	apply, _ = SignTransactionCommand(e.options.LeaseKey, apply)
	ack, err := e.runtime.Execute(ctx, apply, xrayconfig.Candidate{})
	if err == nil && !VerifyTransactionReceipt(e.options.LeaseKey, apply, ack) {
		e.t.Fatal("real Apply receipt signature rejected")
	}
	return ack.Observed, err
}

func (e *transactionRealityExecutor) Rollback(ctx context.Context, sequence uint64, lease Lease) (Snapshot, error) {
	e.serial++
	previous := e.runtime.state.Previous
	if previous == nil {
		return Snapshot{}, ErrStale
	}
	cmd := transactionCommand(e.t, e.runtime, e.options, "rollback", e.serial, sequence, previous.ID, previous.Revision.candidate())
	cmd.Lease = lease
	cmd, _ = SignTransactionCommand(e.options.LeaseKey, cmd)
	ack, err := e.runtime.Execute(ctx, cmd, xrayconfig.Candidate{})
	if err == nil && !VerifyTransactionReceipt(e.options.LeaseKey, cmd, ack) {
		e.t.Fatal("real Rollback receipt signature rejected")
	}
	return ack.Observed, err
}

func TestRealPinnedRouteTransactionsRealityRollback(t *testing.T) {
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if binary == "" && pin == "" {
		t.Skip("explicit pinned real Xray required")
	}
	if binary == "" || pin == "" {
		t.Fatal("incomplete pinned core")
	}
	o := testOptions(t)
	o.Binary = binary
	o.BinarySHA = pin
	r, err := OpenTransactions(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	executor := &transactionRealityExecutor{t: t, runtime: r, options: o}
	exerciseRealityRevokeRollback(t, o, executor)
}
