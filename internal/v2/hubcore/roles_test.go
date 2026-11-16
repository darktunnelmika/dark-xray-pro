package hubcore

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The role sweep: every route of the panel, called by every role, against
// everyone else's data. It reads the routes from the source, so a route
// added later is swept too.

type route struct{ method, path, wrap string }

func panelRoutes(t *testing.T) []route {
	t.Helper()
	files, _ := filepath.Glob("*.go")
	re := regexp.MustCompile(`HandleFunc\("([A-Z]+) ([^"]+)",\s*([a-zA-Z]+)`)
	seen := map[string]bool{}
	var out []route
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, _ := os.ReadFile(f)
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			if k := m[1] + " " + m[2]; !seen[k] {
				seen[k] = true
				out = append(out, route{m[1], m[2], m[3]})
			}
		}
	}
	if len(out) < 100 {
		t.Fatalf("only %d routes found", len(out))
	}
	return out
}

// dbSnapshot: every row of every table except the ones a call may
// legitimately touch (logs, sessions) and rows that mention skip.
func dbSnapshot(t *testing.T, s *Store, skip ...string) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := s.ro.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	volatile := map[string]bool{"audit_log": true, "sessions": true, "events": true, "node_history": true, "online": true, "ip_log": true}
	out := map[string]string{}
	for _, tb := range tables {
		if volatile[tb] {
			continue
		}
		r, err := s.ro.QueryContext(ctx, "SELECT * FROM "+tb)
		if err != nil {
			t.Fatal(err)
		}
		cols, _ := r.Columns()
		var lines []string
		for r.Next() {
			vals := make([]sql.RawBytes, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			_ = r.Scan(ptrs...)
			var b strings.Builder
			for i, v := range vals {
				fmt.Fprintf(&b, "%s=%s|", cols[i], v)
			}
			line := b.String()
			keep := true
			for _, sk := range skip {
				if strings.Contains(line, sk) {
					keep = false
				}
			}
			if keep {
				lines = append(lines, line)
			}
		}
		r.Close()
		sort.Strings(lines)
		out[tb] = strings.Join(lines, "\n")
	}
	return out
}

func TestRoleSweep(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.SetAdmin(ctx, "boss", "correct horse battery"))
	must(s.PutStaff(ctx, "watcher", "viewer", "viewer password 1"))
	// Reseller ali with a sub-seller kid; reseller zed is the intruder.
	must(s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true}))
	must(s.PutRep(ctx, Rep{ID: "zed", Name: "Zed", Enabled: true}))
	must(s.AdjustCredit(ctx, "o", "ali", 500<<30, 5, ""))
	must(s.AdjustCredit(ctx, "o", "zed", 500<<30, 5, ""))
	must(s.SetRepLogin(ctx, "aliuser", "ali password 123", "ali"))
	must(s.SetRepLogin(ctx, "zeduser", "zed password 123", "zed"))
	must(s.PutSeller(ctx, "ali", Rep{ID: "kid", Name: "Kid", Enabled: true}, "kiduser", "kid password 123"))
	must(s.TransferCredit(ctx, "ali", "ali", "kid", 50<<30, 0, ""))
	ali := Principal{User: "aliuser", Role: "rep", RepID: "ali"}
	owner := Principal{User: "boss", Role: "owner"}
	must(s.RepPutClient(ctx, "aliuser", "ali", Client{Email: "ali-c1", Enabled: true, VolumeBytes: 10 << 30, DurationSeconds: 30 * 86400, Inbounds: []string{"de-vless"}}, "volume"))
	must(s.PutProduct(ctx, ali, Product{ID: "ali-p1", Name: "Ali 10", Plan: "volume", GB: 10, Days: 30, Price: 100, Status: "published", Category: "VIP"}))
	must(s.PutProduct(ctx, ali, Product{ID: "kid-p1", Seller: "kid", Name: "Kid 5", Plan: "volume", GB: 5, Days: 30, Price: 50, Status: "published"}))
	must(s.PutProduct(ctx, owner, Product{ID: "own-p1", Name: "Owner 20", Plan: "volume", GB: 20, Days: 30, Price: 200, Status: "published", Inbounds: []string{"de-vless"}}))
	aliOrder, err := s.CreateOrder(ctx, ali, "ali-p1", TgBuyer(7001), "", "")
	must(err)
	ownOrder, err := s.CreateOrder(ctx, owner, "own-p1", TgBuyer(7002), "", "")
	must(err)
	aliTicket, _, err := s.CustomerTicket(ctx, "ali", 7001, "cust", "help")
	must(err)
	ownTicket, _, err := s.CustomerTicket(ctx, "", 7002, "cust", "help")
	must(err)

	h := httptest.NewServer(AdminHandler(s, ""))
	defer h.Close()
	login := func(u, p string) *http.Cookie {
		resp, err := http.Post(h.URL+"/api/v2/auth/login", "application/json", strings.NewReader(`{"Username":"`+u+`","Password":"`+p+`"}`))
		if err != nil || resp.StatusCode != 200 {
			t.Fatalf("login %s: %v %v", u, resp.Status, err)
		}
		return resp.Cookies()[0]
	}
	call := func(c *http.Cookie, method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, h.URL+path, strings.NewReader(body))
		if c != nil {
			req.AddCookie(c)
		}
		req.Header.Set("X-Dark-Request", "1")
		req.Header.Set("Content-Type", "application/json")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, string(b)
	}
	zed, kid, watcher := login("zeduser", "zed password 123"), login("kiduser", "kid password 123"), login("watcher", "viewer password 1")
	routes := panelRoutes(t)
	fillers := map[string][]string{
		"email": {"ali-c1", "alice"}, "hwid": {"x"},
		"id": {"ali-p1", "kid-p1", "own-p1", strconv.FormatInt(aliOrder, 10), strconv.FormatInt(ownOrder, 10),
			strconv.FormatInt(aliTicket, 10), strconv.FormatInt(ownTicket, 10), "kid", "ali", "1"},
		"verb": {"approve", "reject"},
	}
	param := regexp.MustCompile(`\{([a-z]+)\}`)
	expand := func(p string) []string {
		out := []string{p}
		for _, m := range param.FindAllStringSubmatch(p, -1) {
			vals := fillers[m[1]]
			if len(vals) == 0 {
				vals = []string{"x"}
			}
			var next []string
			for _, o := range out {
				for _, v := range vals {
					next = append(next, strings.Replace(o, m[0], v, 1))
				}
			}
			out = next
		}
		return out
	}
	evil := `{"Enabled":false,"VolumeBytes":1,"Email":"ali-c1","status":"draft","price":1,"name":"hacked","seller":"ali","ids":["ali-p1","own-p1"],"text":"hacked","plan":"volume","gb":999,"days":999,"limit":0,"note":"hacked","Name":"hacked","amount":1}`
	mutating := func(r route) bool { return r.method != "GET" && r.method != "HEAD" }
	authRoute := func(p string) bool { return strings.HasPrefix(p, "/api/v2/auth/") }

	// Strangers: every guarded route answers 401.
	for _, r := range routes {
		if r.wrap != "guard" && r.wrap != "member" && r.wrap != "repOnly" {
			continue
		}
		for _, p := range expand(r.path)[:1] {
			if code, _ := call(nil, r.method, p, evil); code != 401 && code != 403 {
				t.Errorf("anonymous %s %s: %d", r.method, p, code)
			}
		}
	}
	// Resellers never pass an owner-only route.
	for _, r := range routes {
		if r.wrap != "guard" {
			continue
		}
		for _, c := range []*http.Cookie{zed, kid} {
			if code, b := call(c, r.method, expand(r.path)[0], evil); code != 403 {
				t.Errorf("reseller on owner route %s %s: %d %s", r.method, r.path, code, b)
			}
		}
	}
	// A viewer changes nothing.
	before := dbSnapshot(t, s)
	for _, r := range routes {
		if !mutating(r) || authRoute(r.path) || (r.wrap != "guard" && r.wrap != "member" && r.wrap != "repOnly") {
			continue
		}
		for _, p := range expand(r.path) {
			if code, b := call(watcher, r.method, p, evil); code == 200 {
				t.Errorf("viewer changed %s %s: %s", r.method, p, b)
			}
		}
	}
	diffSnap(t, "viewer", before, dbSnapshot(t, s))
	// Reseller zed tries every member route on ali's and the owner's things:
	// nothing outside zed's own rows may change.
	before = dbSnapshot(t, s, "zed")
	for _, r := range routes {
		if !mutating(r) || authRoute(r.path) || (r.wrap != "member" && r.wrap != "repOnly") {
			continue
		}
		for _, p := range expand(r.path) {
			call(zed, r.method, p, evil)
		}
	}
	diffSnap(t, "reseller zed", before, dbSnapshot(t, s, "zed"))
	// Sub-seller kid: the same against its reseller and the owner.
	before = dbSnapshot(t, s, "kid")
	for _, r := range routes {
		if !mutating(r) || authRoute(r.path) || (r.wrap != "member" && r.wrap != "repOnly") {
			continue
		}
		for _, p := range expand(r.path) {
			call(kid, r.method, p, evil)
		}
	}
	diffSnap(t, "sub-seller kid", before, dbSnapshot(t, s, "kid"))
	// Control: the same calls by ali itself do change ali's things, so the
	// sweep above would have seen a leak.
	aliC := login("aliuser", "ali password 123")
	before = dbSnapshot(t, s)
	if code, b := call(aliC, "PUT", "/api/v2/shop/products/ali-p1", evil); code != 200 {
		t.Fatalf("control: ali edits its product: %d %s", code, b)
	}
	if after := dbSnapshot(t, s); after["products"] == before["products"] {
		t.Fatal("control: the snapshot did not see ali's own change")
	}
}

func diffSnap(t *testing.T, who string, a, b map[string]string) {
	t.Helper()
	for tb, rows := range a {
		if b[tb] != rows {
			al, bl := strings.Split(rows, "\n"), strings.Split(b[tb], "\n")
			set := map[string]bool{}
			for _, l := range al {
				set[l] = true
			}
			var added []string
			for _, l := range bl {
				if !set[l] {
					added = append(added, l)
				}
			}
			t.Errorf("%s changed table %s; new/changed rows: %v", who, tb, added)
		}
	}
	for tb := range b {
		if _, ok := a[tb]; !ok {
			t.Errorf("%s created table %s", who, tb)
		}
	}
}
