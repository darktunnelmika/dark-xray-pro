//go:build linux

package inboundruntime

import (
	"context"
	"errors"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type failedSampleProcess struct {
	*fixtureProcess
	finalErr       error
	finalContextOK bool
}

func (p *failedSampleProcess) Counters(context.Context) ([]ix.Counter, error) {
	return nil, context.DeadlineExceeded
}

func (p *failedSampleProcess) Final(ctx context.Context, nonce string) ([]ix.Counter, error) {
	_, bounded := ctx.Deadline()
	p.finalContextOK = ctx.Err() == nil && bounded
	if p.finalErr != nil {
		return nil, p.finalErr
	}
	return p.fixtureProcess.Final(ctx, nonce)
}

func TestWorkerSampleTimeoutPersistsRealFinalAcrossRestart(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 41, Download: 73}}
	wrapped := &failedSampleProcess{fixtureProcess: p}
	w.process = wrapped
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.Step(ctx, x); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if !wrapped.finalContextOK || w.state.Phase != "final_pending" || w.state.Pending == nil || !w.state.Pending.Final || len(x.observations) != 0 {
		t.Fatal("final not durably preserved", w.state.Phase)
	}
	sha := ReportSHA(*w.state.Pending)
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	x.delivery = stoppedDelivery(t, reopened)
	if err = reopened.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	got := x.receipts[2]
	if !got.Final || ReportSHA(got) != sha || got.Counters[0].Upload != 41 || got.Counters[0].Download != 73 || reopened.state.Epoch != nil {
		t.Fatal("real final changed during recovery")
	}
}

func TestWorkerSampleAndFinalFailureStillFencesUnknownTail(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	w.process = &failedSampleProcess{fixtureProcess: p, finalErr: ErrRuntime}
	if err := w.Step(context.Background(), x); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if w.state.Phase != "uncertain" || w.process != nil || p.stops != 1 || len(x.receipts) != 0 || len(x.observations) != 1 || x.observations[0].AccountingState != "uncertain" {
		t.Fatal("missing final was forgiven")
	}
}
