package noderegistry

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testInstall = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	otherInstall = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testToken = "1111111111111111111111111111111111111111111111111111111111111111"
	otherToken = "2222222222222222222222222222222222222222222222222222222222222222"
)

func openTest(t *testing.T, now *time.Time) (*Registry, string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "private", "registry.json")
	reg, err := Open(file, func() time.Time { return *now })
	if err != nil {
		t.Fatal(err)
	}
	return reg, file
}

func TestOneTimePairingPersistsAndAuthorizesOnlyMatchingInstallation(t *testing.T) {
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	reg, path := openTest(t, &now)
	pair, err := reg.Issue("de-01", "Germany 01")
	if err != nil {
		t.Fatal(err)
	}
	if len(pair.PairCode) != 64 || !pair.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("bad one-time invite: %+v", pair)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), pair.PairCode) || strings.Contains(string(raw), testToken) {
		t.Fatal("registry leaked a secret in plaintext")
	}
	info, err := reg.Claim(pair.PairCode, "de-01", testInstall, testToken)
	if err != nil {
		t.Fatal(err)
	}
	if info.Name != "Germany 01" || info.NodeID != "de-01" {
		t.Fatalf("wrong binding: %+v", info)
	}
	if _, ok := reg.Authenticate("de-01", testInstall, testToken); !ok {
		t.Fatal("correct token must authenticate")
	}
	for _, wrong := range [][3]string{
		{"de-01", otherInstall, testToken},
		{"de-01", testInstall, otherToken},
		{"us-01", testInstall, testToken},
	} {
		if _, ok := reg.Authenticate(wrong[0], wrong[1], wrong[2]); ok {
			t.Fatalf("cross-Node or wrong-installation token authenticated: %v", wrong)
		}
	}
	if _, err := reg.Claim(pair.PairCode, "de-01", testInstall, otherToken); !errors.Is(err, ErrInvite) {
		t.Fatalf("replayed invite changed a Node credential: %v", err)
	}
	if _, err := reg.Claim(pair.PairCode, "de-01", otherInstall, testToken); !errors.Is(err, ErrInvite) {
		t.Fatalf("replayed invite changed installation: %v", err)
	}
	if _, err := reg.Claim(pair.PairCode, "de-01", testInstall, testToken); err != nil {
		t.Fatalf("an identical lost-response retry must succeed: %v", err)
	}
	if _, err := reg.Issue("de-01", "another"); !errors.Is(err, ErrExists) {
		t.Fatalf("existing binding silently replaced: %v", err)
	}
	restored, err := Open(path, func() time.Time { return now.Add(time.Hour) })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := restored.Authenticate("de-01", testInstall, testToken); !ok {
		t.Fatal("identity lost across restart")
	}
	if len(restored.List()) != 1 || restored.List()[0].NodeID != "de-01" {
		t.Fatal("roster changed across restart")
	}
	infoOnDisk, _ := os.Stat(path)
	if infoOnDisk.Mode().Perm() != 0600 {
		t.Fatalf("file permissions %o", infoOnDisk.Mode().Perm())
	}
}

func TestExpiredInviteNeverCreatesNode(t *testing.T) {
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	reg, _ := openTest(t, &now)
	ticket, err := reg.Issue("nl-01", "Netherlands")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Issue("nl-01", "Second"); !errors.Is(err, ErrPending) {
		t.Fatalf("double invite accepted: %v", err)
	}
	now = now.Add(11*time.Minute)
	if _, err := reg.Claim(ticket.PairCode, "nl-01", testInstall, testToken); !errors.Is(err, ErrInvite) {
		t.Fatalf("expired invite accepted: %v", err)
	}
	reissued, err := reg.Issue("nl-01", "Netherlands")
	if err != nil {
		t.Fatal(err)
	}
	if reissued.PairCode == ticket.PairCode {
		t.Fatal("new invite reuses expired code")
	}
	if len(reg.List()) != 0 {
		t.Fatal("unclaimed invite published a live Node")
	}
}

func TestPairingInputValidation(t *testing.T) {
	now := time.Now()
	reg, _ := openTest(t, &now)
	for _, id := range []string{"", "../etc", "node id", "-bad", "a/b", strings.Repeat("a", 65)} {
		if _, err := reg.Issue(id, "Safe"); !errors.Is(err, ErrInvalid) {
			t.Fatalf("unexpected allowed ID %q: %v", id, err)
		}
	}
	if _, err := reg.Issue("valid", "line\nbreak"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("control character accepted: %v", err)
	}
	if _, err := reg.Claim("short", "de-01", testInstall, testToken); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short token accepted: %v", err)
	}
}

func TestConcurrentIssueGetsOnlyOneReservation(t *testing.T) {
	now := time.Now()
	reg, _ := openTest(t, &now)
	var success int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := reg.Issue("only-one", "Only one")
			if err == nil {
				mu.Lock()
				success++
				mu.Unlock()
			} else if !errors.Is(err, ErrPending) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if success != 1 {
		t.Fatalf("issued %d simultaneous invites", success)
	}
}

func TestUnsafeStateIsRejectedBeforeLoading(t *testing.T) {
	now := time.Now()
	reg, path := openTest(t, &now)
	if _, err := reg.Issue("de-01", "Germany"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, nil); !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("world-readable registry was accepted: %v", err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "alias.json")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(link, nil); !errors.Is(err, ErrUnsafeState) {
		t.Fatalf("symlink registry was accepted: %v", err)
	}
}
