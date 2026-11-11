//go:build linux

package inboundruntime

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestOperatorParentPauseLease(t *testing.T) {
	control := os.Getenv("DARK_PRO_PARENT_PAUSE_CONTROL")
	if control == "" {
		t.Skip("isolated operator control only")
	}
	seconds := 15
	if s := os.Getenv("DARK_PRO_PARENT_PAUSE_LEASE_SECONDS"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || n != 15 && n != 30 {
			t.Fatal("unsupported lease duration")
		}
		seconds = n
	}
	required := os.Getenv("DARK_PRO_EXPECT_AUTONOMOUS_LEASE") == "1"
	f := newRealCounterFailureFixtureLease(t, required)
	known := f.knownAndUnreportedTraffic(t)
	g := f.worker.process.(*engineGroup)
	p := g.native
	if required && !p.autonomousLease {
		t.Fatal("candidate did not negotiate autonomous lease")
	}
	proxy, err := f.client.Transport.(*http.Transport).Proxy(nil)
	if err != nil {
		t.Fatal(err)
	}
	pending := os.Getenv("DARK_PRO_PARENT_PAUSE_PENDING") == "1"
	ordinary := f.worker.makeReport(known.Counters, false)
	if pending {
		f.worker.state.Sequence++
		ordinary = f.worker.makeReport(known.Counters, false)
		f.worker.state.Pending = &ordinary
		if e := f.worker.save(); e != nil {
			t.Fatal(e)
		}
	}
	p.binary.diagnostics = &Diagnostics{}
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)
	if !p.Renew(deadline) {
		t.Fatal("lease renewal refused")
	}
	f.worker.deadline = deadline
	write := func(name string, v any) {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(control, name), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write("ready.json", map[string]any{"pid": os.Getpid(), "native_pid": p.cmd.Process.Pid, "proxy": proxy.Host, "deadline_unix_nano": deadline.UnixNano(), "initial_known_upload": known.Counters[0].Upload, "initial_known_download": known.Counters[0].Download, "ordinary_pending": pending})
	time.Sleep(time.Duration(seconds+6) * time.Second)
	result := map[string]any{"native_alive": g.Alive(), "ordinary_pending": pending}
	p.mu.Lock()
	result["parent_ending_at_sample"] = p.ending
	result["parent_deadline_unix_nano"] = p.deadline.UnixNano()
	result["parent_lease_serial"] = p.leaseSerial
	p.mu.Unlock()
	result["sample_unix_nano"] = time.Now().UnixNano()
	result["capture_sealed_at_sample"] = p.hasFinalCapture()
	result["runtime_diagnostics_at_sample"] = p.binary.diagnostics.Snapshot()
	if b, e := os.ReadFile("/proc/" + strconv.Itoa(p.cmd.Process.Pid) + "/status"); e == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "State:") {
				result["native_state_at_sample"] = line
			}
		}
	}
	if pending {
		f.exchange.delivery = stoppedDelivery(t, f.worker)
		for i := 0; i < 6 && f.worker.state.Epoch != nil; i++ {
			if err = f.worker.Step(context.Background(), f.exchange); err != nil {
				break
			}
		}
		final := f.exchange.receipts[ordinary.Sequence+1]
		exact := ReportSHA(f.exchange.receipts[ordinary.Sequence]) == ReportSHA(ordinary)
		good := err == nil && exact && final.Final && len(final.Counters) == 1 && final.Counters[0].Download > known.Counters[0].Download && f.worker.state.Epoch == nil
		result["final_capture_ok"] = good
		result["exact_pending_ordinary_receipt_preserved"] = exact
		result["genuine_final_settled"] = good
		if !good {
			write("worker-result.json", result)
			t.Fatal("dead-worker sealed capture did not settle pending ordinary/final sequence", err)
		}
		cfg := f.worker.cfg
		f.worker.Close()
		reopened, e := Open(cfg)
		if e != nil {
			t.Fatal(e)
		}
		defer reopened.Close()
		f.exchange.delivery = stoppedDelivery(t, reopened)
		if e = reopened.Step(context.Background(), f.exchange); e != nil || reopened.state.Epoch != nil || ReportSHA(f.exchange.receipts[final.Sequence]) != ReportSHA(final) {
			t.Fatal("settled final changed after restart", e)
		}
		result["genuine_positive_final_replay_after_restart"] = true
		result["same_known_usage_retained"] = true
		result["phase"] = reopened.state.Phase
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = f.worker.captureFinal(ctx, diagnosticLeaseExpired)
		cancel()
		final := f.worker.state.Pending != nil && f.worker.state.Pending.Final
		result["final_capture_ok"] = err == nil
		result["genuine_final_pending"] = final
		result["phase"] = f.worker.state.Phase
		result["same_known_usage_retained"] = f.worker.state.LastCounters[0].Download >= known.Counters[0].Download
		if err == nil && final {
			report := *f.worker.state.Pending
			expected := ReportSHA(report)
			cfg := f.worker.cfg
			f.worker.Close()
			reopened, e := Open(cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer reopened.Close()
			f.exchange.delivery = stoppedDelivery(t, reopened)
			if e = reopened.Step(context.Background(), f.exchange); e != nil {
				t.Fatal(e)
			}
			got := f.exchange.receipts[report.Sequence]
			if ReportSHA(got) != expected || !got.Final || got.Counters[0].Download <= known.Counters[0].Download || reopened.state.Epoch != nil {
				t.Fatal("genuine final did not replay exactly after restart")
			}
			result["genuine_positive_final_replay_after_restart"] = true
		}
	}
	result["native_alive_after_settlement"] = g.Alive()
	write("worker-result.json", result)
}
