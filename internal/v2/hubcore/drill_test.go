package hubcore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupDrill(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	etc := t.TempDir()
	for _, f := range DefaultHubFiles {
		_ = os.WriteFile(filepath.Join(etc, f), []byte("x-"+f), 0o600)
	}
	SetHubFiles(etc, DefaultHubFiles...)
	defer SetHubFiles("")
	for i := 0; i < 5; i++ {
		_ = s.PutClient(ctx, Client{Email: "drill-" + string(rune('a'+i)), Enabled: true, Inbounds: []string{"de-vless"}})
	}
	r := s.BackupDrill(ctx, "password-1234")
	if !r.OK || r.Counts["clients"][0] == 0 || r.Counts["clients"][1] != r.Counts["clients"][0] || r.Bytes == 0 {
		t.Fatalf("drill: %+v", r)
	}
	if got := s.LastDrill(ctx); !got.OK || got.Unix != r.Unix {
		t.Fatalf("recorded: %+v", got)
	}
	// The live HUB files were not touched by the throwaway restore.
	if b, _ := os.ReadFile(filepath.Join(etc, "admin-token")); string(b) != "x-admin-token" {
		t.Fatal("live files changed")
	}
	// A missing key fails the drill.
	_ = os.Remove(filepath.Join(etc, "server.key"))
	if r := s.BackupDrill(ctx, "password-1234"); r.OK || r.Error == "" {
		t.Fatalf("drill without server.key: %+v", r)
	}
}
