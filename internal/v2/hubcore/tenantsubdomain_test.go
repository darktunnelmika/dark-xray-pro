package hubcore

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTenantSubDomainOldLinks(t *testing.T) {
	legacyLocalOK, legacyACME, fallbackInsecure = true, false, true
	defer func() { legacyLocalOK, legacyACME, fallbackInsecure = false, true, false }()
	s, ctx := tenantStore(t)
	if err := s.PutTenant(ctx, Tenant{ID: "other", Name: "Other", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	theirs, err := s.TenantCreate(ctx, "other", TenantClientIn{Name: "z1", VolumeBytes: i64(1 << 30)})
	if err != nil {
		t.Fatal(err)
	}
	u1, err := s.TenantClient(ctx, "tish", "u1")
	if err != nil {
		t.Fatal(err)
	}
	// The old panel: knows one customer not brought over yet, and answers
	// another with an empty list.
	var oldHost string
	old := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oldHost = r.Host
		switch r.URL.Path {
		case "/2fmg/notmovedyet00001":
			w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=3; expire=4")
			fmt.Fprint(w, "dmxlc3M6Ly9vbGQ=")
		case "/2fmg/emptyontheold001":
			w.WriteHeader(200)
		default:
			http.NotFound(w, r)
		}
	}))
	defer old.Close()

	bad := []TenantSubDomain{
		{Domain: "sub.example.org", Port: 22, TLS: true},
		{Domain: "sub.example.org", Port: 2096, Prefix: "2fmg"},
		{Domain: "bad_domain", Port: 2096},
		{Domain: "sub.example.org", Port: 2096, Fallback: "ftp://x"},
		{Domain: "sub.example.org", Port: 2096, Fallback: "https://x.example/path"},
	}
	for _, d := range bad {
		if err := s.PutTenantSubDomain(ctx, "tish", d); err == nil {
			t.Fatalf("accepted %+v", d)
		}
	}
	d := TenantSubDomain{Domain: "Sub.Example.org", Port: 2096, TLS: true, Prefix: "/2fmg/", Fallback: old.URL, Enabled: true}
	if err := s.PutTenantSubDomain(ctx, "tish", d); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTenantSubDomain(ctx, "other", TenantSubDomain{Domain: "sub.example.org", Port: 2096, Enabled: true}); err == nil {
		t.Fatal("another shop took the domain")
	}
	if list, err := s.TenantSubDomains(ctx, "tish"); err != nil || len(list) != 1 || list[0].Domain != "sub.example.org" || list[0].Fallback != old.URL {
		t.Fatalf("list %+v %v", list, err)
	}
	if want := s.legacyWanted(ctx); want[2096] != "sub:tls" {
		t.Fatalf("listeners %v", want)
	}

	h := s.legacySubHandler()
	get := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "https://sub.example.org:2096"+path, nil)
		r.Header.Set("User-Agent", "v2rayNG/1.9")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	// A moved customer: its subscription here, on the old path.
	w := get("/2fmg/" + u1.SubToken)
	if w.Code != 200 || w.Header().Get("Subscription-Userinfo") == "" || w.Header().Get("X-Dark-Fallback") != "" {
		t.Fatalf("moved customer %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	// The customer sees the shop's name for itself, never the panel's
	// internal one (tish+u1).
	if pt := w.Header().Get("Profile-Title"); pt != "base64:"+b64("u1") {
		t.Fatalf("profile title %q", pt)
	}
	first := w.Body.String()
	if w := get("/2fmg/" + u1.SubToken); w.Code != 200 || w.Body.String() != first {
		t.Fatalf("second fetch %d", w.Code)
	}
	// Not moved yet: the old panel answers, asked with the domain's name.
	w = get("/2fmg/notmovedyet00001")
	if w.Code != 200 || w.Body.String() != "dmxlc3M6Ly9vbGQ=" || w.Header().Get("X-Dark-Fallback") != "1" || w.Header().Get("Subscription-Userinfo") == "" {
		t.Fatalf("fallback %d %q %v", w.Code, w.Body.String(), w.Header())
	}
	if oldHost != "sub.example.org:2096" {
		t.Fatalf("old panel asked as %q", oldHost)
	}
	// Never an empty list: empty or unknown on the old panel is a 404.
	for _, p := range []string{"/2fmg/emptyontheold001", "/2fmg/unknowneverywhere1", "/elsewhere/" + u1.SubToken, "/2fmg/" + theirs.SubToken} {
		if w := get(p); w.Code != 404 {
			t.Fatalf("%s: %d %q", p, w.Code, w.Body.String())
		}
	}
	// Without a fallback, unknown ids are 404 too.
	d.Fallback = ""
	if err := s.PutTenantSubDomain(ctx, "tish", d); err != nil {
		t.Fatal(err)
	}
	if w := get("/2fmg/notmovedyet00001"); w.Code != 404 {
		t.Fatalf("no fallback: %d", w.Code)
	}
	// Another host on the same port is not the shop's.
	r := httptest.NewRequest("GET", "https://other.example.org:2096/2fmg/"+u1.SubToken, nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, r)
	if rw.Code != 404 {
		t.Fatalf("other host %d", rw.Code)
	}
	// Fetches are counted: u1 fetched, nobody else of the shop.
	rep, err := s.TenantSubFetches(ctx, "tish", s.now().Unix()-60, "not_fetched", "", 0)
	if err != nil || rep.Clients != 1 || rep.Fetched != 1 || rep.NotFetched != 0 || len(rep.Names) != 0 {
		t.Fatalf("report %+v %v", rep, err)
	}
	if c, _ := s.TenantClient(ctx, "tish", "u1"); c.SubFetched == 0 {
		t.Fatal("sub_fetched_unix not set")
	}
	var ua string
	_ = s.db.QueryRow(`SELECT ua FROM sub_fetch WHERE email='tish+u1'`).Scan(&ua)
	if !strings.HasPrefix(ua, "v2rayNG") {
		t.Fatalf("ua %q", ua)
	}
	// Removing the domain closes it.
	if err := s.DeleteTenantSubDomain(ctx, "tish", "sub.example.org"); err != nil {
		t.Fatal(err)
	}
	if want := s.legacyWanted(ctx); want[2096] != "" {
		t.Fatalf("still listening %v", want)
	}
	_ = io.Discard
}

// TestSubTokenLookupUsesIndex: the subscription token lookup must use the
// partial index, or every fetch reads the whole clients table.
func TestSubTokenLookupUsesIndex(t *testing.T) {
	s, _ := tenantStore(t)
	rows, err := s.db.Query(`EXPLAIN QUERY PLAN SELECT email FROM clients WHERE sub_token=? AND sub_token<>''`, "abcdefgh12345678")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plan := ""
	for rows.Next() {
		var id, parent, nu int
		var d string
		_ = rows.Scan(&id, &parent, &nu, &d)
		plan += d + "\n"
	}
	if !strings.Contains(plan, "clients_sub_token") {
		t.Fatalf("plan:\n%s", plan)
	}
}
