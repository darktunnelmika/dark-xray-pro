//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type observerFixture struct {
	runtime      *Runtime
	options      Options
	candidate    xrayconfig.Candidate
	observation  UsageObservation
	mode, sample string
	outage       atomic.Bool
	accepted     atomic.Int64
}

func (f *observerFixture) writeSample(t *testing.T, up, down int64) {
	t.Helper()
	email, _ := uc.CounterEmail("one")
	body, _ := json.Marshal(map[string]any{"stat": []any{map[string]any{"name": "user>>>" + email + ">>>traffic>>>uplink", "value": fmt.Sprint(up)}, map[string]any{"name": "user>>>" + email + ">>>traffic>>>downlink", "value": fmt.Sprint(down)}}})
	temp := f.sample + ".tmp"
	if err := os.WriteFile(temp, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(temp, f.sample); err != nil {
		t.Fatal(err)
	}
}
func newObserverFixture(t *testing.T) *observerFixture {
	t.Helper()
	o := testOptions(t)
	root := t.TempDir()
	f := &observerFixture{options: o, mode: filepath.Join(root, "mode"), sample: filepath.Join(root, "sample")}
	f.writeSample(t, 1, 2)
	script := fmt.Sprintf(`#!/usr/bin/python3
import json,socket,sys,select,time,pathlib
if sys.argv[1]=='api':
 mode=pathlib.Path(%q)
 if mode.exists() and mode.read_text()=='blocked': time.sleep(30)
 if mode.exists() and mode.read_text()=='invalid': print('{}')
 else: print(pathlib.Path(%q).read_text())
 sys.exit(0)
if '-test' in sys.argv: sys.exit(0)
c=json.load(open(sys.argv[sys.argv.index('-config')+1]))
servers=[]
addresses=[(x['listen'],x['port']) for x in c['inbounds']]
if c.get('api',{}).get('listen'):
 host,port=c['api']['listen'].split(':');addresses.append((host,int(port)))
for address in addresses:
 s=socket.socket();s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
 s.bind(address);s.listen(64);servers.append(s)
while True:
 ready,_,_=select.select(servers,[],[],0.1)
 for s in ready:
  con,_=s.accept();con.close()
`, f.mode, f.sample)
	path := filepath.Join(root, "controlled-core")
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(script))
	f.options.Binary = path
	f.options.BinarySHA = hex.EncodeToString(sum[:])
	port := freePort(t)
	statsPort := freePort(t)
	for statsPort == port {
		statsPort = freePort(t)
	}
	c := testCandidate(t, port, "observer")
	var doc map[string]any
	if json.Unmarshal(c.Config, &doc) != nil {
		t.Fatal("candidate")
	}
	email, _ := uc.CounterEmail("one")
	in := doc["inbounds"].([]any)[0].(map[string]any)
	in["settings"].(map[string]any)["clients"].([]any)[0].(map[string]any)["email"] = email
	endpoint := fmt.Sprintf("127.0.0.1:%d", statsPort)
	doc["api"] = map[string]any{"listen": endpoint, "tag": "observer-test", "services": []string{"StatsService"}}
	raw, _ := json.Marshal(doc)
	c.Config = raw
	sum = sha256.Sum256(raw)
	c.SHA256 = hex.EncodeToString(sum[:])
	sum = sha256.Sum256([]byte("test-observer:" + c.SHA256))
	c.ManifestSHA256 = hex.EncodeToString(sum[:])
	f.candidate = c
	f.runtime = mustOpen(t, f.options)
	if _, err := f.runtime.Apply(context.Background(), Revision{1, c}, grant(t, f.options, 1, c, 40*time.Second)); err != nil {
		t.Fatal(err)
	}
	birth, err := f.runtime.ObservationProcess()
	if err != nil {
		t.Fatal(err)
	}
	epoch := uc.Epoch{NodeID: o.NodeID, InstallationID: o.InstallationID, ID: strings.Repeat("e", 32), ConfigSHA: c.SHA256, KernelBootID: birth.BootID, PID: birth.PID, ProcessStartTicks: birth.StartTicks, Bindings: []uc.Binding{{ClientID: "one", Email: email, Billable: true}}}
	sink := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.outage.Load() {
			w.WriteHeader(503)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/interrupted") {
			var n uc.Interruption
			_ = json.NewDecoder(r.Body).Decode(&n)
			_ = json.NewEncoder(w).Encode(uc.InterruptionReceipt{EpochID: n.EpochID, ReconciliationRequired: true})
			return
		}
		var report uc.Report
		if json.NewDecoder(r.Body).Decode(&report) != nil {
			w.WriteHeader(400)
			return
		}
		f.accepted.Add(1)
		json.NewEncoder(w).Encode(uc.Receipt{EpochID: report.EpochID, Sequence: report.Sequence, SHA: report.SHA})
	}))
	sink.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	sink.StartTLS()
	t.Cleanup(sink.Close)
	sum = sha256.Sum256(sink.Certificate().Raw)
	sender, err := uc.NewSender(uc.SenderOptions{Endpoint: sink.URL, NodeID: epoch.NodeID, InstallationID: epoch.InstallationID, Token: strings.Repeat("a", 64), PinSHA256: hex.EncodeToString(sum[:]), Client: sink.Client()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sender.Close)
	f.observation = UsageObservation{Epoch: epoch, StatsEndpoint: endpoint, Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 2 * time.Second, MaxPendingAge: time.Second}
	return f
}
func (f *observerFixture) start(t *testing.T) {
	t.Helper()
	if err := f.runtime.StartUsageObservation(context.Background(), f.observation); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return f.runtime.UsageStatus().Receipts >= 1 })
}
func TestUsageObserverPeriodicityAndStickyReplay(t *testing.T) {
	f := newObserverFixture(t)
	f.start(t)
	f.writeSample(t, 10, 20)
	waitFor(t, func() bool { return f.runtime.UsageStatus().Receipts >= 2 && f.runtime.UsageStatus().Samples >= 3 })
	status := f.runtime.Snapshot()
	if status.Usage == nil || !status.Usage.Running || status.DataPlaneVerified || status.SubscriptionPublished {
		t.Fatal("observation invented data-plane proof")
	}
	if err := f.runtime.StartUsageObservation(context.Background(), f.observation); !errors.Is(err, ErrUsageSettlement) {
		t.Fatal("second epoch reset accepted", err)
	}
	f.runtime.Close()
	restored := mustOpen(t, f.options)
	if !restored.UsageStatus().SettlementRequired || restored.UsageStatus().Running {
		t.Fatal("restart invented live collection")
	}
	if _, err := restored.Recover(context.Background(), grant(t, f.options, 1, f.candidate, 40*time.Second)); !errors.Is(err, ErrUsageSettlement) {
		t.Fatal("fresh lease bypassed unresolved metering", err)
	}
	if _, err := restored.Apply(context.Background(), Revision{2, f.candidate}, grant(t, f.options, 2, f.candidate, 40*time.Second)); !errors.Is(err, ErrUsageSettlement) {
		t.Fatal("new revision bypassed unresolved metering", err)
	}
	if sent, err := restored.ReplayObservedUsage(context.Background(), f.observation.Sender); err != nil || sent != 0 {
		t.Fatal("empty committed queue replay", sent, err)
	}
	if !restored.UsageStatus().SettlementRequired {
		t.Fatal("receipt alone claimed known final tail")
	}
	raw, err := os.ReadFile(filepath.Join(f.options.Directory, "runtime.enc"))
	if err != nil || bytes.Contains(raw, []byte("stats_endpoint")) || bytes.Contains(raw, []byte("@pro.invalid")) {
		t.Fatal("plaintext scope leaked", err)
	}
}
func TestUsageObserverReceiptAgeStopsOnlyOwnedChildAndReplays(t *testing.T) {
	f := newObserverFixture(t)
	other := testOptions(t)
	unrelated := mustOpen(t, other)
	port := freePort(t)
	c := testCandidate(t, port, "other")
	if _, err := unrelated.Apply(context.Background(), Revision{1, c}, grant(t, other, 1, c, 40*time.Second)); err != nil {
		t.Fatal(err)
	}
	f.start(t)
	f.outage.Store(true)
	f.writeSample(t, 10, 20)
	waitFor(t, func() bool { return f.runtime.UsageStatus().Pending > 0 })
	f.writeSample(t, 20, 40)
	waitFor(t, func() bool { return !f.runtime.UsageStatus().Running && !f.runtime.Snapshot().ProcessReady })
	if status := f.runtime.UsageStatus(); status.Failure != "receipt_deadline" || status.Samples < 2 {
		t.Fatal("pending age not independently bounded", status)
	}
	assertPort(t, f.candidate.Listeners[0].NodePort, false)
	assertPort(t, port, true)
	f.runtime.Close()
	restored := mustOpen(t, f.options)
	f.outage.Store(false)
	sent, err := restored.ReplayObservedUsage(context.Background(), f.observation.Sender)
	if err != nil || sent < 1 {
		t.Fatal("unacknowledged reports lost across restart", sent, err)
	}
	if sent, err = restored.ReplayObservedUsage(context.Background(), f.observation.Sender); err != nil || sent != 0 {
		t.Fatal("acknowledged queue reappeared", sent, err)
	}
	if !restored.UsageStatus().SettlementRequired {
		t.Fatal("crash tail forgiven")
	}
}
func TestUsageObserverStalledCollectorDoesNotWaitForRuntimeMutex(t *testing.T) {
	f := newObserverFixture(t)
	f.start(t)
	if err := os.WriteFile(f.mode, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	r := f.runtime
	r.mu.Lock()
	p := r.process
	select {
	case <-p.done:
	case <-time.After(4 * time.Second):
		r.mu.Unlock()
		t.Fatal("stalled collector defeated independent guard")
	}
	r.mu.Unlock()
	// Failure fences lease renewal immediately, while Running=false certifies
	// the owned child has finished stopping and its socket is no longer ours.
	waitFor(t, func() bool {
		status := r.UsageStatus()
		return status.Failure == "sample_deadline" && !status.Running && !r.Snapshot().ProcessReady
	})
	assertPort(t, f.candidate.Listeners[0].NodePort, false)
}
func TestUsageObserverIncompleteStatsStopsWithoutInventingZero(t *testing.T) {
	f := newObserverFixture(t)
	f.start(t)
	before := f.runtime.UsageStatus().Receipts
	if err := os.WriteFile(f.mode, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return !f.runtime.UsageStatus().Running && !f.runtime.Snapshot().ProcessReady })
	if s := f.runtime.UsageStatus(); s.Failure != "observation_failed" || s.Receipts != before {
		t.Fatal("incomplete sample became valid", s)
	}
	assertPort(t, f.candidate.Listeners[0].NodePort, false)
}
func TestUsageObserverScopeMismatchHasNoSideEffects(t *testing.T) {
	f := newObserverFixture(t)
	cases := []func(*UsageObservation){func(o *UsageObservation) { o.Epoch.PID = os.Getpid() }, func(o *UsageObservation) { o.Epoch.ProcessStartTicks++ }, func(o *UsageObservation) { o.Epoch.ConfigSHA = strings.Repeat("a", 64) }, func(o *UsageObservation) { o.Epoch.Bindings[0].Billable = false }, func(o *UsageObservation) { o.StatsEndpoint = "127.0.0.1:65500" }}
	for _, edit := range cases {
		o := f.observation
		o.Epoch.Bindings = append([]uc.Binding(nil), o.Epoch.Bindings...)
		edit(&o)
		if err := f.runtime.StartUsageObservation(context.Background(), o); err == nil {
			t.Fatal("foreign authority accepted")
		}
		if !f.runtime.Snapshot().ProcessReady || f.runtime.UsageStatus().Required {
			t.Fatal("invalid request changed owned runtime")
		}
	}
}
func TestUsageObserverTransitionDoesNotSilentlyRestartUnmetered(t *testing.T) {
	f := newObserverFixture(t)
	f.start(t)
	pid, _ := f.runtime.ObservationProcess()
	if _, err := f.runtime.Apply(context.Background(), Revision{2, f.candidate}, grant(t, f.options, 2, f.candidate, 40*time.Second)); !errors.Is(err, ErrUsageSettlement) {
		t.Fatal("transition discarded observer", err)
	}
	after, err := f.runtime.ObservationProcess()
	if err != nil || pid != after {
		t.Fatal("refused transition disrupted old process", err)
	}
	if _, err := f.runtime.Refresh(grant(t, f.options, 1, f.candidate, 40*time.Second)); err != nil {
		t.Fatal("healthy same-epoch lease refresh refused", err)
	}
}
