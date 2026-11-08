//go:build linux

package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
)

// This opt-in acceptance uses only disposable fixture identities, private
// databases/journals and loopback listeners. The production/LAB service and its
// historical holds are never opened. Failure stops the run and remains a failed
// acceptance; a recovered later attempt cannot rewrite that result.
func TestManagedInboundFixedVersionLiveSoak(t *testing.T) {
	raw := os.Getenv("DARK_PRO_TEST_OBSERVE_SOAK_SECONDS")
	if raw == "" {
		t.Skip("set DARK_PRO_TEST_OBSERVE_SOAK_SECONDS for the isolated fixed-version soak")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 5 || n > 3600 {
		t.Fatal("soak duration must be 5..3600 seconds")
	}
	testManagedInboundOwnerAgent(t, time.Duration(n)*time.Second)
}

type managedSoakWitness struct {
	mu             sync.Mutex
	epoch, process string
	generation     uint64
	changed        bool
}

func (w *managedSoakWitness) record(in ix.UsageReport) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.epoch == "" {
		w.epoch, w.process, w.generation = in.EpochID, in.ProcessIdentity, in.Generation
		return
	}
	if w.epoch != in.EpochID || w.process != in.ProcessIdentity || w.generation != in.Generation {
		w.changed = true
	}
}

func (w *managedSoakWitness) check(t *testing.T, core string) (string, uint64) {
	t.Helper()
	w.mu.Lock()
	epoch, process, generation, changed := w.epoch, w.process, w.generation, w.changed
	w.mu.Unlock()
	if epoch == "" || process == "" || changed {
		t.Fatal("unexpected runtime epoch/process/generation change")
	}
	var members map[string]string
	if err := json.Unmarshal([]byte(process), &members); err != nil || len(members) != 1 {
		t.Fatal("missing exact native process identity", err)
	}
	for _, raw := range members {
		var member ix.ProcessMember
		if err := json.Unmarshal([]byte(raw), &member); err != nil {
			t.Fatal(err)
		}
		proc := filepath.Join("/proc", strconv.FormatInt(int64(member.PID), 10))
		stat, err := os.ReadFile(filepath.Join(proc, "stat"))
		if err != nil {
			t.Fatal("native core stopped during fixed-version soak", err)
		}
		end := strings.LastIndex(string(stat), ")")
		if end < 0 {
			t.Fatal("invalid process identity")
		}
		fields := strings.Fields(string(stat)[end+1:])
		if len(fields) < 20 {
			t.Fatal("missing native process start ticks")
		}
		ticks, err := strconv.ParseUint(fields[19], 10, 64)
		if err != nil || ticks != uint64(member.StartTicks) {
			t.Fatal("native PID was reused", err)
		}
		exe, err := os.Readlink(filepath.Join(proc, "exe"))
		if err != nil || exe != core {
			t.Fatal("native executable changed", err)
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(epoch+"/"+process))), generation
}

func runManagedFixedVersionSoak(t *testing.T, s *Server, worker *inboundruntime.Worker, exchange inboundruntime.Exchange, witness *managedSoakWitness, core string, ctx context.Context, duration time.Duration, port uint16, username string, client business.Client, target string, payload []byte, subscriptionPath string, login func()) int64 {
	t.Helper()
	initial, err := s.business.ClientUsage(ctx, client.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := initial.Total
	identity, generation := witness.check(t, core)
	start := time.Now()
	t.Logf("MANAGED_FIXED_VERSION_SOAK_BEGIN seconds=%d identity_sha=%s generation=%d", int(duration/time.Second), identity, generation)
	nextLogin := start
	stepExchange := inboundruntime.WithDeadlines(exchange)
	samples := 0
	for time.Since(start) < duration {
		if err := ctx.Err(); err != nil {
			t.Fatal("soak context ended", err)
		}
		if !time.Now().Before(nextLogin) {
			login()
			nextLogin = time.Now().Add(time.Minute)
		}
		if err := managedHTTP(port, username, client.UUID, target, payload); err != nil {
			t.Fatal("soak traffic failure", err)
		}
		want += int64(len(payload) * 2)
		attempt, cancel := context.WithTimeout(ctx, 8*time.Second)
		stepAt := time.Now()
		err := worker.Step(attempt, stepExchange)
		stepElapsed := time.Since(stepAt)
		cancel()
		if err != nil {
			t.Fatalf("soak accounting/renewal failure after %s: %v", stepElapsed, err)
		}
		current, err := s.business.ClientUsage(ctx, client.ID)
		if err != nil || current.Total != want || current.Holds != 0 {
			t.Fatalf("soak consumption mismatch got=%d want=%d holds=%d error=%v", current.Total, want, current.Holds, err)
		}
		id, gen := witness.check(t, core)
		if id != identity || gen != generation {
			t.Fatal("fixed runtime identity changed")
		}
		state, err := s.business.InboundExecution(ctx, s.inboundKey, 501)
		if err != nil || len(state.Targets) != 1 || !state.Targets[0].ProcessReady || state.Targets[0].DesiredGeneration != generation {
			t.Fatal("soak runtime unavailable", err)
		}
		view, err := s.business.InboundExecutionSubscription(ctx, s.inboundKey, "owner", client.ID)
		if err != nil || !view.Available || !view.RuntimeReady {
			t.Fatal("soak subscription metadata unavailable", err)
		}
		public := httptest.NewRecorder()
		s.Handler().ServeHTTP(public, httptest.NewRequest("GET", subscriptionPath+"?format=uri", nil))
		if public.Code != 200 || public.Header().Get("Subscription-Userinfo") == "" {
			t.Fatalf("soak public subscription failure HTTP%d", public.Code)
		}
		samples++
		if samples == 1 || samples%5 == 0 {
			t.Logf("MANAGED_FIXED_VERSION_SOAK_SAMPLE elapsed_ms=%d samples=%d committed_bytes=%d identity_sha=%s generation=%d step_ms=%d", time.Since(start).Milliseconds(), samples, want, identity, generation, stepElapsed.Milliseconds())
		}
		remaining := duration - time.Since(start)
		if remaining > time.Second {
			remaining = time.Second
		}
		if remaining > 0 {
			timer := time.NewTimer(remaining)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				t.Fatal(ctx.Err())
			}
		}
	}
	login()
	t.Logf("MANAGED_FIXED_VERSION_SOAK_WINDOW_COMPLETE elapsed_ms=%d samples=%d committed_bytes=%d unexpected_epochs=0", time.Since(start).Milliseconds(), samples, want)
	return want
}
