//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type reuseHealthProcess struct {
	*fixtureProcess
	healthErr   error
	healthCalls int
}

func (p *reuseHealthProcess) Health(context.Context) error {
	p.healthCalls++
	return p.healthErr
}

func TestWorkerCurrentDeliveryReusesOwnedBirthAcrossHealthFailure(t *testing.T) {
	w, process, exchange := fixtureWorker(t, "active")
	owned := &reuseHealthProcess{fixtureProcess: process}
	w.process = owned
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	activeBundle := cloneBundle(*w.state.Bundle)
	epochSHA := ix.Digest(*w.state.Epoch)
	processIdentity := process.Identity()
	freshDelivery := func() *Delivery {
		t.Helper()
		delivery := fixtureDelivery(t, w.cfg.Key, activeBundle)
		if err := VerifyDelivery(w.cfg.Key, *delivery, w.cfg.NodeID, w.cfg.InstallationID, time.Now()); err != nil {
			t.Fatal("fresh same-generation authority is invalid", err)
		}
		return delivery
	}
	assertSameBirth := func() {
		t.Helper()
		if w.process != owned || w.state.Epoch == nil || ix.Digest(*w.state.Epoch) != epochSHA || process.Identity() != processIdentity {
			t.Fatal("same-generation reconciliation replaced the owned birth or epoch")
		}
		if w.state.Generation != activeBundle.Generation || w.state.Phase != "active" || process.stops != 0 || process.finals != 0 || process.activations != 0 || exchange.registerCalls != 0 {
			t.Fatal("same-generation reconciliation restarted, re-registered, reactivated, or sealed the birth")
		}
	}

	// Each delivery carries a newly signed lease. A failed local health read
	// must affect the observation, not replace an otherwise owned incarnation.
	for i, failHealth := range []bool{false, false, true, false} {
		process.values[0].Upload += 11
		process.values[0].Download += 29
		exchange.delivery = freshDelivery()
		owned.healthErr = nil
		if failHealth {
			owned.healthErr = ErrRuntime
		}
		sequence := w.state.Sequence + 1
		err := w.Step(ctx, exchange)
		if failHealth && !errors.Is(err, ErrRuntime) || !failHealth && err != nil {
			t.Fatalf("delivery %d returned the wrong health outcome: %v", i, err)
		}
		assertSameBirth()
		if process.renewals != i+1 || owned.healthCalls != i+1 || exchange.polls != i+1 || !w.deadline.Equal(time.Unix(0, exchange.delivery.Lease.Expires)) {
			t.Fatal("fresh authority was not applied to the existing birth")
		}
		report, ok := exchange.receipts[sequence]
		if !ok || report.Final || report.ProcessIdentity != processIdentity || report.EpochID != w.state.Epoch.EpochID || !reflect.DeepEqual(report.Counters, process.values) || w.state.Pending != nil || w.state.Sequence != sequence {
			t.Fatal("ordinary usage did not receive its exact same-incarnation receipt")
		}
		if len(exchange.observations) != i+1 {
			t.Fatal("missing health observation")
		}
		observation := exchange.observations[i]
		if failHealth {
			if observation.State != "degraded" || observation.ErrorCode != "engine_health_failed" || observation.ProcessReady {
				t.Fatal("failed health read claimed readiness")
			}
		} else if observation.State != "active" || observation.ErrorCode != "" || !observation.ProcessReady {
			t.Fatal("recovered health did not report the retained birth ready")
		}
	}

	// Reuse is conditional on authentic authority, even when all bundle and
	// generation fields match. An invalid signature must not renew the child.
	exchange.delivery = freshDelivery()
	exchange.delivery.Lease.Signature = strings.Repeat("0", 64)
	if VerifyDelivery(w.cfg.Key, *exchange.delivery, w.cfg.NodeID, w.cfg.InstallationID, time.Now()) == nil {
		t.Fatal("negative lease fixture is valid")
	}
	deadline, renewals, healthCalls := w.deadline, process.renewals, owned.healthCalls
	if err := w.Step(ctx, exchange); err == nil {
		t.Fatal("matching generation bypassed the signed lease")
	}
	assertSameBirth()
	if process.renewals != renewals || owned.healthCalls != healthCalls || !w.deadline.Equal(deadline) {
		t.Fatal("unauthenticated delivery renewed or probed the birth")
	}

	// A genuine signed withdrawal seals this birth. Offering the previously
	// active generation again cannot reopen it while its final ACK is lost.
	exchange.delivery = stoppedDelivery(t, w)
	exchange.loseFinal = true
	if err := w.Step(ctx, exchange); !errors.Is(err, errLostReceipt) {
		t.Fatal("withdrawal did not retain the lost final receipt", err)
	}
	if w.state.Pending == nil || !w.state.Pending.Final || w.state.Phase != "final_pending" || process.finals != 1 || process.stops != 0 {
		t.Fatal("withdrawal did not durably seal the original birth")
	}
	finalSHA := ReportSHA(*w.state.Pending)
	finalSequence := w.state.Pending.Sequence
	polls := exchange.polls
	exchange.delivery = freshDelivery()
	exchange.loseFinal = true
	if err := w.Step(ctx, exchange); !errors.Is(err, errLostReceipt) {
		t.Fatal("sealed birth bypassed its pending final receipt", err)
	}
	if w.process != owned || w.state.Epoch == nil || ix.Digest(*w.state.Epoch) != epochSHA || w.state.Pending == nil || ReportSHA(*w.state.Pending) != finalSHA {
		t.Fatal("lost final ACK changed the birth or durable report")
	}
	if exchange.polls != polls || process.renewals != renewals || owned.healthCalls != healthCalls || process.activations != 0 || exchange.registerCalls != 0 || process.finals != 1 || process.stops != 0 {
		t.Fatal("sealed birth was reused, reactivated, or sampled a second time")
	}

	// Once the exact ACK arrives, retire the sealed process. No new desired
	// birth is offered here: this synthetic test never executes a core.
	exchange.delivery = nil
	if err := w.Step(ctx, exchange); err != nil {
		t.Fatal("final receipt recovery failed", err)
	}
	if w.process != nil || w.state.Epoch != nil || w.state.Pending != nil || process.stops != 1 || process.finals != 1 || process.activations != 0 || exchange.registerCalls != 0 {
		t.Fatal("settled sealed birth was retained or readmitted")
	}
	if ReportSHA(exchange.receipts[finalSequence]) != finalSHA {
		t.Fatal("final replay changed the accepted accounting report")
	}
}
