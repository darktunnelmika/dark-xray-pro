package hubcore

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPartnerKeys(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	call := func(method, path, key string, body any) (int, map[string]any) {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req, _ := http.NewRequest(method, h.URL+path, rd)
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		out := map[string]any{}
		raw, _ := io.ReadAll(resp.Body)
		_ = json.Unmarshal(raw, &out)
		return resp.StatusCode, out
	}

	if _, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site", Template: "nobody"}); err == nil {
		t.Fatal("a template that does not exist must be refused")
	}
	k, err := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site", MaxGB: 150})
	if err != nil || !strings.HasPrefix(k.Key, "dp_") {
		t.Fatalf("%+v %v", k, err)
	}
	other, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "other"})
	if code, _ := call("GET", "/api/v2/partner", "dp_x_y", nil); code != 401 {
		t.Fatal("a wrong key", code)
	}

	code, p := call("POST", "/api/v2/partner/reps", k.Key, map[string]any{"name": "Shop A", "credit_gb": 100})
	if code != 200 || p["password"] == "" || p["username"] == "" || !strings.Contains(p["message"].(string), p["password"].(string)) || p["credit_gb"].(float64) != 100 {
		t.Fatalf("create %d %v", code, p)
	}
	id := p["id"].(string)
	if len(s.RepLogins(ctx, id)) != 1 {
		t.Fatal("the new panel has its sign-in")
	}
	if code, _ := call("POST", "/api/v2/partner/reps/"+id+"/credit", k.Key, map[string]any{"credit_gb": 60}); code != 403 {
		t.Fatal("over the key's limit", code)
	}
	if code, p := call("POST", "/api/v2/partner/reps/"+id+"/credit", k.Key, map[string]any{"credit_gb": 50}); code != 200 || p["credit_gb"].(float64) != 150 {
		t.Fatalf("credit %d %v", code, p)
	}
	if code, _ := call("POST", "/api/v2/partner/reps/"+id+"/credit", k.Key, map[string]any{"credit_gb": -5}); code != 422 {
		t.Fatal("credit cannot be taken back through the key", code)
	}
	if code, p := call("POST", "/api/v2/partner/reps/"+id+"/disable", k.Key, nil); code != 200 || p["enabled"] != false {
		t.Fatalf("disable %d %v", code, p)
	}
	if code, p := call("POST", "/api/v2/partner/reps/"+id+"/enable", k.Key, nil); code != 200 || p["enabled"] != true {
		t.Fatalf("enable %d %v", code, p)
	}
	if code, p := call("POST", "/api/v2/partner/reps/"+id+"/password", k.Key, nil); code != 200 || p["password"] == "" {
		t.Fatalf("password %d %v", code, p)
	}
	if code, _ := call("GET", "/api/v2/partner/reps/"+id, other.Key, nil); code != 404 {
		t.Fatal("another key does not see this panel", code)
	}
	if code, _ := call("POST", "/api/v2/partner/reps/alice/credit", k.Key, map[string]any{"credit_gb": 1}); code != 404 {
		t.Fatal("a panel the key did not make", code)
	}
	if code, p := call("GET", "/api/v2/partner", k.Key, nil); code != 200 || p["given_gb"].(float64) != 150 || p["panels"].(float64) != 1 {
		t.Fatalf("key %d %v", code, p)
	}
	// A key turned off stops at once; deleting it leaves the panels.
	off := false
	if _, err := s.UpdatePartnerKey(ctx, k.ID, PartnerKeyIn{Name: "site", MaxGB: 150, Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if code, _ := call("GET", "/api/v2/partner/reps", k.Key, nil); code != 401 {
		t.Fatal("a disabled key", code)
	}
	if err := s.DeletePartnerKey(ctx, k.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RepPolicyOf(ctx, id); err != nil {
		t.Fatal("the panel stays after its key is gone")
	}
}
