package noderegistry

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestCancelPendingRevokesOnlyUnclaimedInvitation(t *testing.T) {
	r, err := Open(filepath.Join(t.TempDir(), "registry.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	first, err := r.Issue("fresh-qa-node", "New independent Node")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CancelPending("fresh-qa-node"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Claim(first.PairCode, "fresh-qa-node", strings.Repeat("1", 32), strings.Repeat("a", 64)); !errors.Is(err, ErrInvite) {
		t.Fatalf("revoked invite was accepted: %v", err)
	}
	second, err := r.Issue("fresh-qa-node", "New independent Node")
	if err != nil {
		t.Fatal(err)
	}
	if first.PairCode == second.PairCode {
		t.Fatal("reused cancelled secret")
	}
	if _, err := r.Claim(second.PairCode, "fresh-qa-node", strings.Repeat("1", 32), strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := r.CancelPending("fresh-qa-node"); !errors.Is(err, ErrInvite) {
		t.Fatalf("cannot cancel claimed node: %v", err)
	}
	if got := r.List(); len(got) != 1 || got[0].NodeID != "fresh-qa-node" {
		t.Fatalf("paired node unexpectedly removed: %+v", got)
	}
	if err := r.CancelPending("../bad"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad ID: %v", err)
	}
}
