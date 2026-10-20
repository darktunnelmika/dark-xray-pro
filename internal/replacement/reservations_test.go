package replacement

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)

func reservationPlan(t *testing.T, old, next string) routeplan.Preview {
	t.Helper()
	p, err := routeplan.New(old, next)
	if err != nil {
		t.Fatal(err)
	}
	if err = p.SetDirect(1, 18080); err != nil {
		t.Fatal(err)
	}
	v, err := p.Preview()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestReplacementReservesBothNodeRoles(t *testing.T) {
	for _, pair := range [][2]string{{"a", "c"}, {"c", "b"}, {"b", "c"}, {"c", "a"}, {"b", "a"}} {
		t.Run(pair[0]+"-"+pair[1], func(t *testing.T) {
			now := time.Now()
			j, path := journalForTest(t, &now)
			op, err := j.Create(reservationPlan(t, "a", "b"), installID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = j.Create(reservationPlan(t, pair[0], pair[1]), installID); !errors.Is(err, ErrConflict) {
				t.Fatal("shared node admitted", err)
			}
			restarted, err := Open(path, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = restarted.Create(reservationPlan(t, pair[0], pair[1]), installID); !errors.Is(err, ErrConflict) {
				t.Fatal("restart lost reservation", err)
			}
			if _, err = restarted.Cancel(op.ID, op.Revision); err != nil {
				t.Fatal(err)
			}
			if _, err = restarted.Create(reservationPlan(t, pair[0], pair[1]), installID); err != nil {
				t.Fatal("cancel did not release both roles", err)
			}
			if len(restarted.List()) != 2 {
				t.Fatal("history removed")
			}
		})
	}
}

func TestReplacementConcurrentCrossRoleReservation(t *testing.T) {
	now := time.Now()
	j, _ := journalForTest(t, &now)
	plans := []routeplan.Preview{reservationPlan(t, "a", "b"), reservationPlan(t, "b", "c")}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, p := range plans {
		wg.Add(1)
		go func(p routeplan.Preview) { defer wg.Done(); <-start; _, err := j.Create(p, installID); results <- err }(p)
	}
	close(start)
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 || len(j.List()) != 1 {
		t.Fatal("concurrent reservation admitted twice")
	}
	if _, err := j.Create(reservationPlan(t, "x", "y"), installID); err != nil {
		t.Fatal("unrelated nodes blocked", err)
	}
}

func TestReplacementCancelLostResponseReplay(t *testing.T) {
	now := time.Now()
	j, path := journalForTest(t, &now)
	op, err := j.Create(reservationPlan(t, "a", "b"), installID)
	if err != nil {
		t.Fatal(err)
	}
	proof := Evidence{OperationID: op.ID, NewNodeID: op.NewNodeID, InstallationID: installID, ManifestSHA256: op.Preview.ManifestSHA256}
	proof.Probes, _ = Expect(op.Preview)
	for i := range proof.Probes {
		proof.Probes[i].Reachable = true
	}
	op, err = j.Report(proof)
	if err != nil {
		t.Fatal(err)
	}
	done, err := j.Cancel(op.ID, op.Revision)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	for _, rev := range []uint64{op.Revision, done.Revision} {
		replay, err := restarted.Cancel(op.ID, rev)
		if err != nil || !reflect.DeepEqual(replay, done) {
			t.Fatal("cancel receipt changed", err)
		}
	}
	for _, rev := range []uint64{0, op.Revision - 1, done.Revision + 1} {
		if _, err := restarted.Cancel(op.ID, rev); !errors.Is(err, ErrConflict) {
			t.Fatal("unrelated revision accepted", err)
		}
	}
	if _, err := restarted.Report(proof); !errors.Is(err, ErrCancelled) {
		t.Fatal("late proof revived operation", err)
	}
	after, _ := os.ReadFile(path)
	if string(raw) != string(after) {
		t.Fatal("replay rewrote history")
	}
}

func TestReplacementRejectsOverlappingPersistedReservations(t *testing.T) {
	now := time.Now()
	j, path := journalForTest(t, &now)
	if _, err := j.Create(reservationPlan(t, "a", "b"), installID); err != nil {
		t.Fatal(err)
	}
	other, _ := journalForTest(t, &now)
	op, err := other.Create(reservationPlan(t, "b", "c"), installID)
	if err != nil {
		t.Fatal(err)
	}
	bad := clone(j.state)
	bad.Operations[op.ID] = op
	raw, _ := json.Marshal(bad)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path, nil); !errors.Is(err, ErrUnsafe) {
		t.Fatal("overlapping saved operations accepted", err)
	}
}
