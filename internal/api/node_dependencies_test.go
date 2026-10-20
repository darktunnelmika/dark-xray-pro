package api

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
	"github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
)

func TestOwnerNodeDependenciesAuthorizationUnknownAndEmpty(t *testing.T) {
	s := enabledOwner(t)
	h := s.BrowserHandler()
	owner, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	ticket, err := s.registry.Issue("inspect-node", "Inspection")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.registry.Claim(ticket.PairCode, "inspect-node", strings.Repeat("a", 32), strings.Repeat("b", 64)); err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/workspace/nodes/inspect-node/dependencies"
	if w := browserRequest(t, h, "GET", path, nil, nil, "", ""); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if _, err := s.business.CreateRepresentative(context.Background(), "inspect-rep", "Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.ProvisionRepresentativeLogin(context.Background(), "inspect-rep", "inspect-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	rep, _ := browserLogin(t, s, "inspect-rep", repPassword)
	if w := browserRequest(t, h, "GET", path, nil, rep, "", ""); w.Code != 403 {
		t.Fatalf("representative: %d", w.Code)
	}
	read := func() map[string]any {
		t.Helper()
		w := browserRequest(t, h, "GET", path, nil, owner, "", "")
		if w.Code != 200 {
			t.Fatalf("read: %d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), ticket.PairCode) || strings.Contains(w.Body.String(), strings.Repeat("b", 64)) {
			t.Fatal("credential exposed")
		}
		var d map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
			t.Fatal(err)
		}
		return d
	}
	d := read()
	counts := d["counts"].(map[string]any)
	if counts["usage_epochs"] != nil || counts["swap_upstream_references"] != nil || counts["replacement_operations"] != nil {
		t.Fatal("disabled stores claimed zero")
	}
	if d["can_retire"] != false || d["can_replace"] != false || d["atomic_across_stores"] != false {
		t.Fatal("unsafe capability")
	}
	if err := s.EnableNativeInbounds(bytes.Repeat([]byte{1}, 32)); err != nil {
		t.Fatal(err)
	}
	if err := s.business.EnableUsageLedger(context.Background()); err != nil {
		t.Fatal(err)
	}
	d = read()
	counts = d["counts"].(map[string]any)
	if counts["usage_epochs"] != float64(0) || counts["swap_upstream_references"] != float64(0) || d["can_retire"] != false {
		t.Fatal("empty observation unsafe")
	}
	j, err := replacement.Open(filepath.Join(t.TempDir(), "private", "journal.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.EnableReplacementLab(j); err != nil {
		t.Fatal(err)
	}
	plan, err := routeplan.New("inspect-node", "replacement-node")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.SetDirect(1, 20001); err != nil {
		t.Fatal(err)
	}
	preview, err := plan.Preview()
	if err != nil {
		t.Fatal(err)
	}
	op, err := j.Create(preview, strings.Repeat("c", 32))
	if err != nil {
		t.Fatal(err)
	}
	d = read()
	counts = d["counts"].(map[string]any)
	if counts["replacement_operations"] != float64(1) || counts["noncancelled_replacements"] != float64(1) {
		t.Fatal("replacement reference hidden")
	}
	if _, err := j.Cancel(op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	d = read()
	counts = d["counts"].(map[string]any)
	if counts["replacement_operations"] != float64(1) || counts["noncancelled_replacements"] != float64(0) {
		t.Fatal("cancelled history lost")
	}
	if w := browserRequest(t, h, "GET", "/api/v1/workspace/nodes/missing/dependencies", nil, owner, "", ""); w.Code != 404 {
		t.Fatalf("missing: %d", w.Code)
	}
	if w := browserRequest(t, h, "POST", path, map[string]any{}, owner, csrf, "https://"+browserHost); w.Code < 400 {
		t.Fatal("mutation accepted")
	}
	if err := s.business.Close(); err != nil {
		t.Fatal(err)
	}
	// The closed business store also prevents session validation: fail closed
	// at authentication, before dependency inspection can expose any counts.
	if w := browserRequest(t, h, "GET", path, nil, owner, "", ""); w.Code != 401 || strings.Contains(w.Body.String(), "counts") {
		t.Fatalf("failure fabricated counts: %d %s", w.Code, w.Body.String())
	}
}
