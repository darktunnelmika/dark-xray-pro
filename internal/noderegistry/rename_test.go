package noderegistry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func pairedRenameRegistry(t *testing.T) (*Registry, string, NodeInfo) {
	t.Helper()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	r, path := openTest(t, &now)
	ticket, err := r.Issue("lab-1", "Original")
	if err != nil {
		t.Fatal(err)
	}
	node, err := r.Claim(ticket.PairCode, "lab-1", testInstall, testToken)
	if err != nil {
		t.Fatal(err)
	}
	return r, path, node
}

func renameInput(event, name string, rev uint64) RenameRequest {
	return RenameRequest{EventID: strings.Repeat(event, 32), InstallationID: testInstall, ExpectedRevision: rev, Name: name}
}

func TestRenameDurableHistoricalReplayAndIdentity(t *testing.T) {
	r, path, original := pairedRenameRegistry(t)
	first := renameInput("1", "Changed", 1)
	receipt, err := r.Rename("lab-1", "owner", first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Rename("lab-1", "owner", renameInput("2", "Original", 2)); err != nil {
		t.Fatal(err)
	}
	r, err = Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := r.Rename("lab-1", "owner", first)
	if err != nil || retry != receipt {
		t.Fatalf("historical replay: %+v %v", retry, err)
	}
	state, err := r.NameState("lab-1")
	if err != nil || state.Revision != 3 || state.AuditCount != 2 || state.Node != original {
		t.Fatalf("state: %+v %v", state, err)
	}
	if node, ok := r.Authenticate("lab-1", testInstall, testToken); !ok || node != original {
		t.Fatal("identity changed")
	}
	for _, actor := range []string{"owner", "other-owner"} {
		changed := first
		if actor == "owner" {
			changed.Name = "Different"
		}
		if _, err := r.Rename("lab-1", actor, changed); !errors.Is(err, ErrNameConflict) {
			t.Fatalf("event substitution: %v", err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), testToken) {
		t.Fatal("secret leaked or unreadable registry")
	}
}

func TestRenameConcurrentCAS(t *testing.T) {
	r, _, _ := pairedRenameRegistry(t)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, event := range []string{"1", "2"} {
		wg.Add(1)
		go func(e string) {
			defer wg.Done()
			_, err := r.Rename("lab-1", "owner", renameInput(e, "Name "+e, 1))
			results <- err
		}(event)
	}
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, ErrNameConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
}

func TestRenameRejectsInvalidAndFailedPersistence(t *testing.T) {
	r, _, original := pairedRenameRegistry(t)
	for _, name := range []string{"", " Original", "Original", "bad\nname", string([]byte{0xff}), strings.Repeat("ن", 81)} {
		if _, err := r.Rename("lab-1", "owner", renameInput("1", name, 1)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid name accepted: %v", err)
		}
	}
	x := renameInput("1", "Valid", 1)
	x.InstallationID = otherInstall
	if _, err := r.Rename("lab-1", "owner", x); !errors.Is(err, ErrNameConflict) {
		t.Fatal(err)
	}
	x.InstallationID = testInstall
	if _, err := r.Rename("missing", "owner", x); !errors.Is(err, ErrNodeNotFound) {
		t.Fatal(err)
	}
	r.path = filepath.Join(t.TempDir(), "missing-parent", "registry.json")
	if _, err := r.Rename("lab-1", "owner", x); err == nil {
		t.Fatal("failed storage accepted")
	}
	state, _ := r.NameState("lab-1")
	if state.Revision != 1 || state.Node != original {
		t.Fatal("unpersisted rename exposed")
	}
}

func TestRenameOpenRejectsCorruptAudit(t *testing.T) {
	for _, mutate := range []func(*diskState){
		func(s *diskState) { x := s.Renames[strings.Repeat("1", 32)]; x.Revision = 9; s.Renames[x.EventID] = x },
		func(s *diskState) {
			x := s.Renames[strings.Repeat("1", 32)]
			x.InstallationID = otherInstall
			s.Renames[x.EventID] = x
		},
		func(s *diskState) { b := s.Bindings["lab-1"]; b.Info.Name = "tampered"; s.Bindings["lab-1"] = b },
	} {
		r, path, _ := pairedRenameRegistry(t)
		if _, err := r.Rename("lab-1", "owner", renameInput("1", "New", 1)); err != nil {
			t.Fatal(err)
		}
		next := clone(r.state)
		mutate(&next)
		b, err := json.Marshal(next)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path, nil); !errors.Is(err, ErrUnsafeState) {
			t.Fatalf("corrupt state accepted: %v", err)
		}
	}
}

func TestRenameLegacyStateAuditTailAndReplayDurability(t *testing.T) {
	r, path, _ := pairedRenameRegistry(t)
	var err error
	r, err = Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := r.NameState("lab-1")
	if err != nil || initial.Revision != 1 || initial.AuditCount != 0 || initial.Recent == nil {
		t.Fatalf("legacy state: %+v %v", initial, err)
	}
	for i := 1; i <= 25; i++ {
		x := RenameRequest{EventID: fmt.Sprintf("%032x", i), InstallationID: testInstall, ExpectedRevision: uint64(i), Name: fmt.Sprintf("Name %d", i)}
		if _, err := r.Rename("lab-1", "owner", x); err != nil {
			t.Fatal(err)
		}
	}
	state, err := r.NameState("lab-1")
	if err != nil || state.Revision != 26 || state.AuditCount != 25 || len(state.Recent) != 20 || state.Recent[0].Revision != 7 {
		t.Fatalf("tail: %+v %v", state, err)
	}
	first := RenameRequest{EventID: fmt.Sprintf("%032x", 1), InstallationID: testInstall, ExpectedRevision: 1, Name: "Name 1"}
	r.path = filepath.Join(t.TempDir(), "missing", "registry.json")
	if _, err := r.Rename("lab-1", "owner", first); err == nil {
		t.Fatal("replay acknowledged without re-establishing durability")
	}
	r.path = path
	if receipt, err := r.Rename("lab-1", "owner", first); err != nil || receipt.Revision != 2 {
		t.Fatalf("old receipt lost: %+v %v", receipt, err)
	}
	if _, err := Open(path, nil); err != nil {
		t.Fatal(err)
	}
}
