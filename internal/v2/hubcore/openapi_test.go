package hubcore

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

type routeRecorder struct{ patterns []string }

func (r *routeRecorder) HandleFunc(pattern string, _ func(http.ResponseWriter, *http.Request)) {
	r.patterns = append(r.patterns, pattern)
}

// Every external route is described, and every description is a route.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	s, _ := newStore(t)
	rec := &routeRecorder{}
	partnerRoutes(s, rec)
	resellerRoutes(s, rec, "")
	registered := map[string]bool{}
	for _, p := range rec.patterns {
		if strings.Contains(p, "/api/v2/partner") || strings.Contains(p, "/api/v2/reseller") {
			registered[p] = true
		}
	}
	documented := map[string]bool{}
	for _, op := range apiOps() {
		documented[op.Method+" "+op.Path] = true
	}
	var missing, extra []string
	for p := range registered {
		if !documented[p] {
			missing = append(missing, p)
		}
	}
	for p := range documented {
		if !registered[p] {
			extra = append(extra, p)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("routes not described: %v\ndescribed but not routes: %v", missing, extra)
	}
}

func TestOpenAPISpec(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	h := httptest.NewServer(AdminHandlerWith(s, "admintok", "", nil))
	defer h.Close()
	if r := apiCall(t, h.URL, "GET", "/api/v2/openapi.json", "", nil); r.Code != 401 {
		t.Fatalf("not for strangers: %d", r.Code)
	}
	k, _ := s.CreatePartnerKey(ctx, PartnerKeyIn{Name: "site"})
	r := apiCall(t, h.URL, "GET", "/api/v2/openapi.json", k.Key, nil)
	if r.Code != 200 || r.str("openapi") != "3.0.3" {
		t.Fatalf("spec %d", r.Code)
	}
	paths := r.JSON["paths"].(map[string]any)
	if _, ok := paths["/api/v2/partner/reps/{id}"]; !ok {
		t.Fatal("a partner key gets the partner description")
	}
	if _, ok := paths["/api/v2/reseller/me"]; ok {
		t.Fatal("only the partner part")
	}
	all := apiCall(t, h.URL, "GET", "/api/v2/openapi.json?api=", "admintok", nil)
	allPaths := all.JSON["paths"].(map[string]any)
	if _, ok := allPaths["/api/v2/reseller/clients/{name}/renew"]; !ok || len(allPaths) < 50 {
		t.Fatalf("the owner sees both: %d paths", len(allPaths))
	}
	// Every $ref points at a schema that exists.
	schemas := all.JSON["components"].(map[string]any)["schemas"].(map[string]any)
	var refs []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, val := range x {
				if k == "$ref" {
					refs = append(refs, val.(string))
				}
				walk(val)
			}
		case []any:
			for _, val := range x {
				walk(val)
			}
		}
	}
	walk(all.JSON)
	for _, ref := range refs {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		if _, ok := schemas[name]; !ok {
			t.Fatalf("dangling %s", ref)
		}
	}
	panel := schemas["PartnerPanel"].(map[string]any)["properties"].(map[string]any)
	for _, f := range []string{"id", "credit_gb", "external_ref", "policy", "telegram_ids"} {
		if _, ok := panel[f]; !ok {
			t.Fatalf("PartnerPanel has %s", f)
		}
	}
	create := schemas["ClientCreate"].(map[string]any)["properties"].(map[string]any)
	if _, ok := create["start_on_first_use"]; !ok {
		t.Fatal("ClientCreate fields")
	}
	if _, ok := schemas["BulkCreate"].(map[string]any)["properties"].(map[string]any)["prefix"]; !ok {
		t.Fatal("embedded fields are flattened, own fields kept")
	}
	b, _ := json.Marshal(all.JSON)
	if strings.Contains(string(b), `"doc`) {
		t.Fatal("internal type names leak into the spec")
	}
}
