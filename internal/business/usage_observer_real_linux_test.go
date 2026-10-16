//go:build linux

package business

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageingest"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// Uses the actual Xray process, collector, Runtime observer, pinned TLS sender,
// receipt handler and project modernc Store. Only the identity callback is a
// scoped fixture; actual Registry restrictions have separate API tests.
func exerciseContinuousUsage(t *testing.T, s *Store, r *managedxray.Runtime, o managedxray.Options, cfg NodeConfig, epoch uc.Epoch, stats string, fetch func() (string, error), proof, id string) {
	t.Helper()
	ctx := context.Background()
	if _, err := s.ExtendClient(ctx, ExtendClient{EventID: "continuous-usage-fixture-credit", ClientID: id, AdditionalVolumeBytes: 1 << 20, Reason: "Isolated continuous-observer allowance"}); err != nil {
		t.Fatal(err)
	}
	original, err := s.Client(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("u", 64)
	handler, err := usageingest.New(s.UsageLedger(), func(_ context.Context, node, installation, provided string) (usageledger.Principal, bool) {
		return usageledger.Principal{NodeID: node, InstallationID: installation}, node == epoch.NodeID && installation == epoch.InstallationID && provided == token
	})
	if err != nil {
		t.Fatal(err)
	}
	var outage, lost atomic.Bool
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if outage.Load() {
			w.WriteHeader(503)
			return
		}
		if req.URL.Path == usageingest.AgentPath && lost.Swap(false) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != 200 {
				t.Errorf("lost-receipt scenario never committed: %d", recorder.Code)
			}
			w.WriteHeader(503)
			return
		}
		handler.ServeHTTP(w, req)
	}))
	receiver.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	receiver.StartTLS()
	defer receiver.Close()
	digest := sha256.Sum256(receiver.Certificate().Raw)
	sender, err := uc.NewSender(uc.SenderOptions{Endpoint: receiver.URL, NodeID: epoch.NodeID, InstallationID: epoch.InstallationID, Token: token, PinSHA256: hex.EncodeToString(digest[:]), Client: receiver.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	observation := managedxray.UsageObservation{Epoch: epoch, StatsEndpoint: stats, Sender: sender, Interval: 200 * time.Millisecond, MaxSampleGap: 5 * time.Second, MaxPendingAge: 2 * time.Second}
	if err = r.StartUsageObservation(ctx, observation); err != nil {
		t.Fatal(err)
	}
	meterWait(t, func() bool {
		a, e := s.ClientUsage(ctx, id)
		return e == nil && a.Total > 0 && r.UsageStatus().Receipts >= 1
	})
	before, _ := s.ClientUsage(ctx, id)
	lost.Store(true)
	if got, err := fetch(); err != nil || got != proof {
		t.Fatal("real transfer during observer failed", err)
	}
	meterWait(t, func() bool {
		a, e := s.ClientUsage(ctx, id)
		return e == nil && a.Total > before.Total && r.UsageStatus().Pending == 0 && r.UsageStatus().Receipts >= 2
	})
	sample, err := uc.Collect(ctx, o.Binary, o.BinarySHA, stats, epoch)
	if err != nil {
		t.Fatal(err)
	}
	expected := sample[0].Uplink + sample[0].Downlink
	meterWait(t, func() bool { a, e := s.ClientUsage(ctx, id); return e == nil && a.Total == expected })
	// New data is captured durably but its receipt endpoint becomes unavailable.
	outage.Store(true)
	if got, err := fetch(); err != nil || got != proof {
		t.Fatal("real outage-period transfer failed", err)
	}
	sample, err = uc.Collect(ctx, o.Binary, o.BinarySHA, stats, epoch)
	if err != nil {
		t.Fatal(err)
	}
	finalKnown := sample[0].Uplink + sample[0].Downlink
	meterWait(t, func() bool { return r.UsageStatus().Pending > 0 })
	meterWait(t, func() bool { return !r.UsageStatus().Running && !r.Snapshot().ProcessReady })
	if status := r.UsageStatus(); status.Failure != "receipt_deadline" {
		t.Fatal("outage did not stop the exact observed child", status)
	}
	r.Close()
	reopened, err := managedxray.Open(o)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err = reopened.Recover(ctx, meterSign(t, o, cfg.Sequence, cfg.Candidate)); !errors.Is(err, managedxray.ErrUsageSettlement) {
		t.Fatal("reboot silently restarted unobserved traffic", err)
	}
	outage.Store(false)
	sent, err := reopened.ReplayObservedUsage(ctx, sender)
	if err != nil || sent < 1 {
		t.Fatal("pending reports not replayed", sent, err)
	}
	a, err := s.ClientUsage(ctx, id)
	if err != nil || a.Total != finalKnown || a.Known || a.Eligible || a.Holds != 1 {
		t.Fatalf("known bytes or unknown-tail hold wrong: %+v expected=%d error=%v", a, finalKnown, err)
	}
	if sent, err = reopened.ReplayObservedUsage(ctx, sender); err != nil || sent != 0 {
		t.Fatal("second recovery duplicated receipt", sent, err)
	}
	after, err := s.ClientUsage(ctx, id)
	if err != nil || after.Total != a.Total || after.Holds != 1 {
		t.Fatal("replayed interruption changed total/holds", err)
	}
	kept, err := s.Client(ctx, id)
	if err != nil || kept.UUID != original.UUID {
		t.Fatal("recovery changed customer identity", err)
	}
	t.Logf("AUTOMATIC OBSERVER: real Xray %d committed bytes before outage; %d persisted bytes after replay; one immutable unknown-tail hold; UUID preserved; no process recovery without settlement", expected, after.Total)
}
