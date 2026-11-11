//go:build linux

package inboundruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// The engine, control socket, traffic and final counters are real. Only the HUB
// exchange uses the existing receipt fixture; this is worker acceptance, not a
// replacement for the separate owner/API/SQLite acceptance.
type realCounterFailureProbe struct {
	ownedProcess
	stopAfterSample             bool
	sampleErr, sampleContextErr error
	finalErr, finalContextErr   error
	finalCalls                  int
	finalBounded                bool
	finalBudget                 time.Duration
}

func (p *realCounterFailureProbe) Counters(ctx context.Context) ([]ix.Counter, error) {
	p.sampleContextErr = ctx.Err()
	values, err := p.ownedProcess.Counters(ctx)
	p.sampleErr = err
	if err != nil && p.stopAfterSample {
		// Model the owned engine exiting between a timed-out sample and final
		// capture. Final still calls the real, now-stopped native process.
		p.ownedProcess.Stop()
	}
	return values, err
}

func (p *realCounterFailureProbe) Final(ctx context.Context, nonce string) ([]ix.Counter, error) {
	p.finalCalls++
	p.finalContextErr = ctx.Err()
	deadline, bounded := ctx.Deadline()
	p.finalBounded = bounded
	p.finalBudget = time.Until(deadline)
	values, err := p.ownedProcess.Final(ctx, nonce)
	p.finalErr = err
	return values, err
}

type realCounterFailureFixture struct {
	worker   *Worker
	exchange *fixtureExchange
	client   *http.Client
	origin   string
	payload  []byte
	hits     atomic.Int64
}

func newRealCounterFailureFixture(t *testing.T) *realCounterFailureFixture {
	return newRealCounterFailureFixtureLease(t, false)
}

func newRealCounterFailureFixtureLease(t *testing.T, required bool) *realCounterFailureFixture {
	t.Helper()
	path := os.Getenv("DARK_PRO_TEST_INBOUND_CORE")
	if path == "" {
		t.Skip("set the separately pinned native acceptance executable")
	}
	if !filepath.IsAbs(path) {
		t.Fatal("DARK_PRO_TEST_INBOUND_CORE must be an absolute path")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sha := hashBytes(raw)
	if expected := os.Getenv("DXP_INBOUND_CORE_V2_SHA256"); expected == "" || sha != expected {
		t.Fatal("DXP_INBOUND_CORE_V2_SHA256 must match the verified test artifact")
	}
	port, err := reservePort()
	if err != nil {
		t.Fatal(err)
	}
	const user = "counter-recovery-user"
	const password = "synthetic-counter-recovery-password"
	config, err := json.Marshal(map[string]any{
		"inbounds": []any{map[string]any{
			"tag": "counter-recovery-http", "protocol": "http", "listen": "127.0.0.1", "port": port,
			"settings": map[string]any{"accounts": []any{map[string]any{"user": user, "pass": password}}, "userLevel": 0},
		}},
		"outbounds": []any{map[string]any{"tag": "direct", "protocol": "freedom"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Directory: filepath.Join(t.TempDir(), "journal"), NodeID: "counter-recovery-node",
		InstallationID: strings.Repeat("a", 32), Key: []byte(strings.Repeat("k", 32)),
		Native: BinaryConfig{Path: path, SHA256: sha, RequireAutonomousLease: required},
	}
	w, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	bundle := ix.Bundle{
		Version: 1, NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, Generation: 1,
		OperationID: "counter-recovery-real-0001", NativeConfig: config,
		Revisions:  []ix.RevisionRef{{InboundID: 501, Revision: 1, PlanRevision: 1, InboundRevision: 1}},
		Listeners:  []ix.Listener{{InboundID: 501, Tag: "counter-recovery-http", Engine: "xray", ResourceKind: "socket", Listen: "127.0.0.1", Port: port, Network: "tcp"}},
		Principals: []ix.Principal{{ID: "counter-recovery-principal", Engine: "xray", ClientID: "counter-recovery-client", Role: "customer", Counter: "user>>>" + user + ">>>traffic", Billable: true}},
	}
	x := &fixtureExchange{delivery: fixtureDelivery(t, cfg.Key, bundle), receipts: map[uint64]ix.UsageReport{}}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	err = w.Step(ctx, x)
	cancel()
	if err != nil || w.state.Phase != "active" || w.process == nil || w.state.Sequence != 1 {
		t.Fatalf("real native worker did not activate after a zero receipt: phase=%s sequence=%d error=%v", w.state.Phase, w.state.Sequence, err)
	}
	zero, ok := x.receipts[1]
	if !ok || zero.Final || len(zero.Counters) != 1 || zero.Counters[0].Upload != 0 || zero.Counters[0].Download != 0 {
		t.Fatalf("real native zero receipt missing: %+v", zero)
	}
	f := &realCounterFailureFixture{worker: w, exchange: x, payload: bytes.Repeat([]byte("real-counter-tail\n"), 4096)}
	origin := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		f.hits.Add(1)
		_, _ = rw.Write(f.payload)
	}))
	t.Cleanup(origin.Close)
	f.origin = origin.URL
	proxy, err := url.Parse("http://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	proxy.User = url.UserPassword(user, password)
	transport := &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	f.client = &http.Client{Transport: transport, Timeout: time.Second}
	return f
}

func (f *realCounterFailureFixture) traffic(t *testing.T, allowed bool) {
	t.Helper()
	before := f.hits.Load()
	response, err := f.client.Get(f.origin)
	if allowed {
		if err != nil {
			t.Fatal("real loopback payload failed", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK || !bytes.Equal(body, f.payload) || f.hits.Load() != before+1 {
			t.Fatal("real loopback payload was not delivered exactly", readErr)
		}
		return
	}
	if response != nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		_ = response.Body.Close()
	}
	if f.hits.Load() != before || err == nil && response != nil && response.StatusCode == http.StatusOK {
		t.Fatal("closed native admission still forwarded a new request")
	}
}

func (f *realCounterFailureFixture) knownAndUnreportedTraffic(t *testing.T) ix.UsageReport {
	t.Helper()
	f.traffic(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	err := f.worker.Step(ctx, f.exchange)
	cancel()
	if err != nil {
		t.Fatal("real ordinary counter receipt failed", err)
	}
	known := f.exchange.receipts[f.worker.state.Sequence]
	if known.Final || len(known.Counters) != 1 || known.Counters[0].Upload == 0 || known.Counters[0].Download < uint64(len(f.payload)) {
		t.Fatalf("ordinary real counters are missing: %+v", known)
	}
	f.traffic(t, true)
	return known
}

func runRealCounterDeadline(t *testing.T, f *realCounterFailureFixture, stop bool) *realCounterFailureProbe {
	t.Helper()
	probe := &realCounterFailureProbe{ownedProcess: f.worker.process, stopAfterSample: stop}
	f.worker.process = probe
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	err := f.worker.Step(ctx, f.exchange)
	cancel()
	if !errors.Is(probe.sampleContextErr, context.DeadlineExceeded) || probe.sampleErr == nil || !errors.Is(err, probe.sampleErr) {
		t.Fatalf("did not exercise a real expired native counter query: context=%v sample=%v step=%v", probe.sampleContextErr, probe.sampleErr, err)
	}
	if probe.finalCalls != 1 || probe.finalContextErr != nil || !probe.finalBounded || probe.finalBudget <= 0 || probe.finalBudget > 3*time.Second {
		t.Fatalf("final capture did not receive an independent three-second budget: calls=%d error=%v bounded=%v budget=%s", probe.finalCalls, probe.finalContextErr, probe.finalBounded, probe.finalBudget)
	}
	return probe
}

func TestWorkerRealCounterDeadlineFinalReplay(t *testing.T) {
	f := newRealCounterFailureFixture(t)
	known := f.knownAndUnreportedTraffic(t)
	probe := runRealCounterDeadline(t, f, false)
	w, x := f.worker, f.exchange
	if probe.finalErr != nil || w.state.Phase != "final_pending" || w.state.Pending == nil || !w.state.Pending.Final {
		t.Fatalf("real final capture was not durably retained: phase=%s error=%v", w.state.Phase, probe.finalErr)
	}
	final := *w.state.Pending
	if final.Sequence != known.Sequence+1 || len(final.Counters) != 1 || final.Counters[0].Upload < known.Counters[0].Upload || final.Counters[0].Download <= known.Counters[0].Download {
		t.Fatalf("unreported real tail was lost: known=%+v final=%+v", known.Counters, final.Counters)
	}
	f.traffic(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	sealed, err := probe.ownedProcess.Final(ctx, final.FinalNonce)
	cancel()
	if err != nil || !reflect.DeepEqual(sealed, final.Counters) {
		t.Fatal("durable report differs from the native engine's immutable final counters", err)
	}
	sha, priorReceipts := ReportSHA(final), len(x.receipts)
	x.loseFinal = true
	if err = w.Step(context.Background(), x); !errors.Is(err, errLostReceipt) || w.state.Pending == nil || ReportSHA(*w.state.Pending) != sha {
		t.Fatal("lost final acknowledgement did not retain the exact real report", err)
	}
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	x.delivery = stoppedDelivery(t, reopened)
	if err = reopened.Step(context.Background(), x); err != nil {
		t.Fatal("real final report did not replay after worker restart", err)
	}
	got, ok := x.receipts[final.Sequence]
	if !ok || ReportSHA(got) != sha || len(x.receipts) != priorReceipts+1 || reopened.state.Epoch != nil || reopened.state.Pending != nil || reopened.state.Phase != "idle" {
		t.Fatal("real final replay changed counters, duplicated a receipt, or lost settlement")
	}
	f.traffic(t, false)
}

func TestWorkerRealCounterDeadlineAndEngineLossFencesTail(t *testing.T) {
	f := newRealCounterFailureFixture(t)
	known := f.knownAndUnreportedTraffic(t)
	probe := runRealCounterDeadline(t, f, true)
	w, x := f.worker, f.exchange
	if probe.finalErr == nil || w.state.Phase != "uncertain" || w.process != nil || w.state.Epoch == nil || w.state.Pending != nil || w.state.FinalAccepted {
		t.Fatalf("failed real final was not fenced: phase=%s error=%v", w.state.Phase, probe.finalErr)
	}
	if !reflect.DeepEqual(w.state.LastCounters, known.Counters) || ReportSHA(x.receipts[known.Sequence]) != ReportSHA(known) || len(x.receipts) != int(known.Sequence) {
		t.Fatal("engine loss altered known counters or fabricated a final receipt")
	}
	f.traffic(t, false)
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Close)
	if err = reopened.Step(context.Background(), x); !errors.Is(err, ErrUncertain) || reopened.state.Phase != "uncertain" || reopened.process != nil || reopened.state.Pending != nil {
		t.Fatal("restart silently cleared an unknown real accounting tail", err)
	}
	if !reflect.DeepEqual(reopened.state.LastCounters, known.Counters) || len(x.receipts) != int(known.Sequence) || len(x.observations) == 0 || x.observations[len(x.observations)-1].AccountingState != "uncertain" {
		t.Fatal("restart lost known counters or failed to report the uncertainty fence")
	}
	f.traffic(t, false)
}
