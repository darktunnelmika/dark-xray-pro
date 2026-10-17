package nodebootstrap

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthenticatedBundleContentsAndHashes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dark-pro-agent")
	binary := []byte("new PRO test-only Agent; never a real secret")
	if err := os.WriteFile(path, binary, 0755); err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var out httptest.ResponseRecorder
	out = *httptest.NewRecorder()
	if err := b.Download(&out); err != nil {
		t.Fatal(err)
	}
	if out.Header().Get("Content-Type") != "application/zip" ||
		!strings.Contains(out.Header().Get("Content-Disposition"), "attachment;") {
		t.Fatalf("invalid disposition: %+v", out.Header())
	}
	reader, err := zip.NewReader(bytes.NewReader(out.Body.Bytes()), int64(out.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"dark-pro-agent": true, "install-new-pro-node.sh": true, "SHA256SUMS": true}
	contents := map[string][]byte{}
	for _, f := range reader.File {
		if !want[f.Name] || f.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("untrusted ZIP entry: %s", f.Name)
		}
		in, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(io.LimitReader(in, 128<<20))
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
		contents[f.Name] = data
	}
	if len(contents) != 3 || !bytes.Equal(contents["dark-pro-agent"], binary) {
		t.Fatal("missing or wrong bundle contents")
	}
	manifest := string(contents["SHA256SUMS"])
	for _, name := range []string{"dark-pro-agent", "install-new-pro-node.sh"} {
		sum := sha256.Sum256(contents[name])
		line := hex.EncodeToString(sum[:]) + "  " + name
		if !strings.Contains(manifest, line+"\n") {
			t.Fatalf("manifest incorrect for %s", name)
		}
	}
	for _, forbidden := range []string{"DARK_PRO_PAIR_CODE=", "agent_token", "private_key", "InsecureSkipVerify", "curl | bash"} {
		if strings.Contains(manifest, forbidden) || strings.Contains(string(contents["install-new-pro-node.sh"]), forbidden) && forbidden != "DARK_PRO_PAIR_CODE=" && forbidden != "agent_token" {
			t.Fatalf("installer or manifest contained unsafe executable/secret: %s", forbidden)
		}
	}
	if b.AgentSHA256() == "" {
		t.Fatal("missing bundle fingerprint")
	}
}

func TestChangesToPinnedAgentOrSymlinkAreRefused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dark-pro-agent")
	if err := os.WriteFile(path, []byte("trusted test Agent"), 0755); err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("unreviewed newer file"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := b.Download(httptest.NewRecorder()); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("unreviewed Agent replacement was served: %v", err)
	}
	other := filepath.Join(dir, "other")
	if err := os.WriteFile(other, []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("symlink Agent accepted: %v", err)
	}
	if _, err := Open(other); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("arbitrary filename accepted: %v", err)
	}
}

func TestNewNodeShellGuardRefusesWithoutExplicitHostAcknowledgement(t *testing.T) {
	script, err := Installer()
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	for _, required := range []string{"DARK_PRO_NEW_NODE_ACK", "DARK_PRO_EXPECT_NODE_IP",
		"DARK_PRO_HUB_AGENT_URL", "DARK_PRO_HUB_PIN_SHA256",
		"SHA256SUMS", "-probe-only", "-enroll-only", "I_CONFIRM_NEW_EMPTY_PRO_NODE",
		"dark-pro-node-agent.service", "NoNewPrivileges=yes", "ProtectSystem=strict",
		"read -r -s", "DARK_PRO_PAIR_CODE"} {
		if !strings.Contains(text, required) {
			t.Fatalf("missing installer safety gate %q", required)
		}
	}
	for _, bad := range []string{"InsecureSkipVerify", "curl -k", "-insecure", "curl | bash", "wget | bash"} {
		if strings.Contains(text, bad) {
			t.Fatalf("unsafe bootstrap pattern %q", bad)
		}
	}
}
