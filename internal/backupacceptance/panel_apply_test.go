//go:build linux

package backupacceptance_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"github.com/darktunnelmika/dark-xray-pro/internal/restoreapply"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestPanelReplacementRestoresFullEconomicStateNotOnlyQuarantine(t *testing.T) {
	ctx := context.Background()
	f := newEconomicFixture(t)
	wantRows := databaseRows(t, f.path)
	staging := t.TempDir()
	require(t, os.Chmod(staging, 0700))
	sources, e := hubstate.Snapshot(ctx, f.lock, f.inventory, staging)
	require(t, e)
	var snapshot string
	for _, v := range sources {
		if v.Role == "business" {
			snapshot = v.Path
		}
	}
	if snapshot == "" {
		t.Fatal("no financial snapshot")
	}
	// Add a real post-snapshot transaction to prove the installation changes.
	s, e := business.Open(f.path)
	require(t, e)
	_, e = s.AdjustCredits(ctx, business.CreditChange{EventID: "after-snapshot-credit-0001", RepresentativeID: "backup-rep-a", VolumeDeltaBytes: 999, Reason: "Synthetic later transaction"})
	require(t, e)
	require(t, s.Close())
	require(t, f.lock.Close())
	if reflect.DeepEqual(wantRows, databaseRows(t, f.path)) {
		t.Fatal("fixture did not change target")
	}
	candidate, e := os.ReadFile(snapshot)
	require(t, e)
	sum := sha256.Sum256(candidate)
	journal := t.TempDir()
	require(t, os.Chmod(journal, 0700))
	id := strings.Repeat("7", 32)
	tx, e := restoreapply.Open(f.root, journal, id, os.Geteuid(), os.Getegid())
	require(t, e)
	replacements := []restoreapply.Replacement{{Destination: "business.sqlite", Source: snapshot, Present: true, Size: int64(len(candidate)), SHA256: hex.EncodeToString(sum[:])}}
	for _, suffix := range []string{"-wal", "-journal", "-shm"} {
		replacements = append(replacements, restoreapply.Replacement{Destination: "business.sqlite" + suffix})
	}
	require(t, tx.Prepare(ctx, replacements))
	require(t, tx.Apply(ctx))
	if _, e = hubstate.Acquire(f.root); !errors.Is(e, hubstate.ErrQuarantined) {
		t.Fatal("partial replacement admitted writers", e)
	}
	require(t, tx.ReleaseForStart(true))
	require(t, tx.Close())
	lock, e := hubstate.Acquire(f.root)
	require(t, e)
	defer lock.Close()
	assertRows(t, wantRows, databaseRows(t, f.path))
	s, e = business.Open(f.path)
	require(t, e)
	defer s.Close()
	if !reflect.DeepEqual(f.state, captureEconomicState(t, s)) {
		t.Fatal("clients, UUIDs, consumption or exact int64 balances did not return")
	}
	assertCatalogRecoveryReplay(t, s, f)
	paid, e := s.RefundExpiredVolume(ctx, nil, f.refund)
	require(t, e)
	if paid.Applied || !paid.Replayed {
		t.Fatal("restored refund paid twice")
	}
	receipt, e := s.UsageLedger().Accept(ctx, f.principal, f.report)
	require(t, e)
	if receipt != f.usage {
		t.Fatal("restored metering receipt changed")
	}
	if !reflect.DeepEqual(f.state, captureEconomicState(t, s)) {
		t.Fatal("restored replays changed accounting")
	}
}
