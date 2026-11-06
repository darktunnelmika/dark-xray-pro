//go:build linux

package inboundruntime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type idleJournalFile struct {
	info os.FileInfo
	raw  []byte
}

func readIdleJournalFile(t *testing.T, w *Worker) idleJournalFile {
	t.Helper()
	path := filepath.Join(w.cfg.Directory, "state.enc")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return idleJournalFile{info: info, raw: raw}
}

func requireIdleJournalUnchanged(t *testing.T, before, after idleJournalFile) {
	t.Helper()
	if !os.SameFile(before.info, after.info) || !before.info.ModTime().Equal(after.info.ModTime()) || !bytes.Equal(before.raw, after.raw) {
		t.Fatal("unchanged empty delivery rewrote the journal inode or ciphertext")
	}
}

func requireIdleJournalRewritten(t *testing.T, before, after idleJournalFile) {
	t.Helper()
	if os.SameFile(before.info, after.info) || bytes.Equal(before.raw, after.raw) {
		t.Fatal("changed or unconditional state did not publish a new journal inode and ciphertext")
	}
}

func settledEmptyJournalWorker(t *testing.T) (*Worker, *fixtureExchange) {
	t.Helper()
	w, process, exchange := fixtureWorker(t, "active")
	exchange.delivery = stoppedDelivery(t, w)
	// First settle the real synthetic incarnation, then persist its empty bundle.
	for step := 0; step < 2; step++ {
		if err := w.Step(context.Background(), exchange); err != nil {
			t.Fatal(err)
		}
	}
	if process.finals != 1 || process.stops != 1 || w.process != nil || w.state.Epoch != nil || w.state.Pending != nil || w.state.Phase != "idle" || !w.state.FinalAccepted {
		t.Fatal("fixture did not settle before receiving its empty bundle")
	}
	return w, exchange
}

func TestWorkerUnchangedEmptyDeliveryKeepsJournalAndRefreshesObservation(t *testing.T) {
	w, exchange := settledEmptyJournalWorker(t)
	before := readIdleJournalFile(t, w)
	polls, observed := exchange.polls, len(exchange.observations)
	registered, usage := exchange.registerCalls, exchange.usageCalls
	w.deadline = time.Now().Add(time.Second)
	exchange.delivery = fixtureDelivery(t, w.cfg.Key, *w.state.Bundle)
	wantDeadline := time.Unix(0, exchange.delivery.Lease.Expires)
	if err := w.Step(context.Background(), exchange); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalUnchanged(t, before, readIdleJournalFile(t, w))
	if !w.deadline.Equal(wantDeadline) || exchange.polls != polls+1 || len(exchange.observations) != observed+1 {
		t.Fatal("unchanged persistence skipped fresh polling, deadline renewal or observation")
	}
	last := exchange.observations[len(exchange.observations)-1]
	if last.State != "stopped" || last.ProcessReady || last.Generation != w.state.Generation || exchange.registerCalls != registered || exchange.usageCalls != usage {
		t.Fatal("empty reconciliation changed admission or accounting behavior")
	}
}

func TestWorkerChangedEmptyGenerationMustPersist(t *testing.T) {
	w, exchange := settledEmptyJournalWorker(t)
	before := readIdleJournalFile(t, w)
	exchange.delivery = stoppedDelivery(t, w)
	if err := w.Step(context.Background(), exchange); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalRewritten(t, before, readIdleJournalFile(t, w))
	if w.state.Generation != exchange.delivery.Bundle.Generation || w.state.Bundle.OperationID != exchange.delivery.Bundle.OperationID {
		t.Fatal("new empty generation was not adopted exactly")
	}
	before = readIdleJournalFile(t, w)
	if err := w.Step(context.Background(), exchange); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalUnchanged(t, before, readIdleJournalFile(t, w))
}

func TestWorkerInMemoryEmptyGenerationIsNotPersistence(t *testing.T) {
	w, exchange := settledEmptyJournalWorker(t)
	before := readIdleJournalFile(t, w)
	exchange.delivery = stoppedDelivery(t, w)
	bundle := exchange.delivery.Bundle
	// Match the next valid empty delivery in memory, without saving it. A
	// comparison against w.state would now incorrectly omit the durable write.
	w.state.Generation, w.state.Bundle = bundle.Generation, &bundle
	if err := w.Step(context.Background(), exchange); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalRewritten(t, before, readIdleJournalFile(t, w))
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.state.Generation != bundle.Generation || ix.Digest(reopened.state.Bundle) != ix.Digest(&bundle) {
		t.Fatal("new in-memory generation was not persisted before acknowledgement")
	}
}

func TestWorkerEmptyNoopRequiresAuthenticatedReopen(t *testing.T) {
	w, _ := settledEmptyJournalWorker(t)
	cfg := w.cfg
	before := readIdleJournalFile(t, w)
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	exchange := &fixtureExchange{delivery: fixtureDelivery(t, cfg.Key, *reopened.state.Bundle)}
	if err = reopened.Step(context.Background(), exchange); err != nil {
		reopened.Close()
		t.Fatal(err)
	}
	requireIdleJournalUnchanged(t, before, readIdleJournalFile(t, reopened))
	reopened.Close()
	corrupt := append([]byte(nil), before.raw...)
	corrupt[len(corrupt)-1] ^= 1
	if err = os.WriteFile(filepath.Join(cfg.Directory, "state.enc"), corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if untrusted, err := Open(cfg); err == nil {
		untrusted.Close()
		t.Fatal("corrupt journal established a persistence witness")
	}
}

func TestWorkerEmptyNoopPreservesEntireResolution(t *testing.T) {
	w, process, exchange := fixtureWorker(t, "active")
	clearance := fixtureClearance(t, w)
	exchange.delivery, process.dead = &Delivery{Clearance: &clearance}, true
	if err := w.Step(context.Background(), exchange); err != nil {
		t.Fatal(err)
	}
	exchange.delivery = stoppedDelivery(t, w)
	if err := w.Step(context.Background(), exchange); err != nil {
		t.Fatal(err)
	}
	before := readIdleJournalFile(t, w)
	resolutionSHA := ix.Digest(w.state.Resolution)
	if err := w.Step(context.Background(), exchange); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalUnchanged(t, before, readIdleJournalFile(t, w))
	if w.state.Resolution == nil || ix.Digest(w.state.Resolution) != resolutionSHA || w.state.Epoch != nil || w.process != nil {
		t.Fatal("unchanged empty bundle discarded or exercised the resolution barrier")
	}
	// A changed in-memory resolution is not proof that it was persisted.
	changed := *w.state.Resolution
	changed.Clearance.EventID = "fixture-owner-loss-0002"
	var err error
	changed.Clearance, err = ix.SealClearance(w.cfg.Key, changed.Clearance)
	if err != nil {
		t.Fatal(err)
	}
	w.state.Resolution = &changed
	if err = w.Step(context.Background(), exchange); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalRewritten(t, before, readIdleJournalFile(t, w))
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.state.Resolution == nil || ix.Digest(reopened.state.Resolution) != ix.Digest(&changed) {
		t.Fatal("changed resolution was not durable across reopen")
	}
}

func TestJournalFailedSaveInvalidatesPersistedWitness(t *testing.T) {
	w, _ := settledEmptyJournalWorker(t)
	before := readIdleJournalFile(t, w)
	if !w.journal.matchesPersisted(w.state) {
		t.Fatal("successful save did not establish a persistence witness")
	}
	dir := w.journal.dir
	w.journal.dir = filepath.Join(t.TempDir(), "absent")
	err := w.journal.save(w.state)
	w.journal.dir = dir
	if err == nil || w.journal.matchesPersisted(w.state) {
		t.Fatal("failed rewrite retained a persistence witness")
	}
	if err = w.journal.save(w.state); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalRewritten(t, before, readIdleJournalFile(t, w))
	if !w.journal.matchesPersisted(w.state) {
		t.Fatal("complete replacement did not restore the persistence witness")
	}
	// save itself stays unconditional even for identical authenticated idle JSON.
	before = readIdleJournalFile(t, w)
	if err = w.journal.save(w.state); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalRewritten(t, before, readIdleJournalFile(t, w))
}

func TestWorkerEmptySaveFailureStillFencesReconciliation(t *testing.T) {
	w, exchange := settledEmptyJournalWorker(t)
	exchange.delivery = stoppedDelivery(t, w)
	observed := len(exchange.observations)
	dir := w.journal.dir
	w.journal.dir = filepath.Join(t.TempDir(), "absent")
	err := w.Step(context.Background(), exchange)
	w.journal.dir = dir
	if !errors.Is(err, ErrPersistence) || !w.persistFailed || w.journal.matchesPersisted(w.state) || len(exchange.observations) != observed {
		t.Fatal("undurable empty generation was acknowledged or left unfenced")
	}
	polls := exchange.polls
	if err = w.Step(context.Background(), exchange); !errors.Is(err, ErrPersistence) || exchange.polls != polls {
		t.Fatal("persistence failure resumed normal reconciliation")
	}
	last := exchange.observations[len(exchange.observations)-1]
	if last.State != "failed" || last.ProcessReady {
		t.Fatal("persistence failure became a stopped/ready acknowledgement")
	}
}

func TestJournalActiveSaveRemainsUnconditional(t *testing.T) {
	w, _, _ := fixtureWorker(t, "active")
	before := readIdleJournalFile(t, w)
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	requireIdleJournalRewritten(t, before, readIdleJournalFile(t, w))
}

func TestWorkerEmptyNoopDoesNotBypassDeliveryVerification(t *testing.T) {
	for _, kind := range []string{"expired_lease", "conflicting_bundle"} {
		t.Run(kind, func(t *testing.T) {
			w, exchange := settledEmptyJournalWorker(t)
			before := readIdleJournalFile(t, w)
			deadline, observed := w.deadline, len(exchange.observations)
			bundle := cloneBundle(*w.state.Bundle)
			if kind == "conflicting_bundle" {
				bundle.OperationID = "fixture-conflicting-operation"
			}
			exchange.delivery = fixtureDelivery(t, w.cfg.Key, bundle)
			if kind == "expired_lease" {
				now := time.Now()
				exchange.delivery.Lease.Issued = now.Add(-20 * time.Second).UnixNano()
				exchange.delivery.Lease.Expires = now.Add(-time.Second).UnixNano()
				var err error
				exchange.delivery.Lease, err = SignLease(w.cfg.Key, exchange.delivery.Lease)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := w.Step(context.Background(), exchange); err == nil {
				t.Fatal("empty persistence bypassed delivery verification")
			}
			requireIdleJournalUnchanged(t, before, readIdleJournalFile(t, w))
			if !w.deadline.Equal(deadline) || len(exchange.observations) != observed {
				t.Fatal("invalid delivery renewed authority or observation")
			}
		})
	}
}
