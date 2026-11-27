package hubcore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/nodeagent"
)

// Xray treats "Saeed" and "saeed" as one client and refuses the whole
// node config, which froze every change on every node.
func TestCaseClashNeverReachesXray(t *testing.T) {
	s, now := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if err := s.PutClient(ctx, Client{Email: "Alice", Enabled: true, Inbounds: []string{"de-vless"}}); !errors.Is(err, ErrInvalid) ||
		!strings.Contains(err.Error(), "client already exists") {
		t.Fatalf("a name that differs only in case must be refused, got %v", err)
	}
	if err := s.CloneClient(ctx, "bob", "BOB"); err == nil {
		t.Fatal("a clone named like an existing client in other case must be refused")
	}
	// Pairs an older panel let in: the newer one stays out of the config.
	*now = now.Add(1e9)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO clients(email,uuid,secret,enabled,volume_bytes,expires_unix,duration_seconds,max_ips,sub_token,created_unix)
VALUES('ALICE','11111111-2222-4333-8444-555555555555','s',1,0,0,0,0,'tokALICE00000000',?)`, now.Unix()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO client_inbounds(email,tag) VALUES('ALICE','de-vless')`); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.RenderNode(ctx, "de-1")
	if err != nil {
		t.Fatal(err)
	}
	emails, _ := nodeagent.ConfigEmails(cfg)
	seen := map[string]bool{}
	for _, e := range emails {
		if seen[strings.ToLower(e)] {
			t.Fatalf("config holds %s twice: %v", e, emails)
		}
		seen[strings.ToLower(e)] = true
	}
	if !seen["alice"] || strings.Contains(string(cfg), `"ALICE"`) {
		t.Fatalf("the oldest alice must be kept, the newer ALICE left out: %v", emails)
	}
	cl, err := s.CaseClashes(ctx)
	if err != nil || len(cl) != 1 || strings.Join(cl[0], ",") != "alice,ALICE" {
		t.Fatalf("clashes %v %v", cl, err)
	}
	again, _ := s.RenderNode(ctx, "de-1")
	if string(again) != string(cfg) {
		t.Fatal("render must stay deterministic")
	}
}
