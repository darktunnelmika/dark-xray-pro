package hubcore

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyUsersMoveAndPayForUse(t *testing.T) {
	legacyLocalOK, legacyACME = true, false
	defer func() { legacyLocalOK, legacyACME = false, true }()
	s, err := Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "ali", 5<<30, 0, "")
	const id = "11111111-2222-3333-4444-555555555555"
	old := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sub/abcdefgh123" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Subscription-Userinfo", fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", 1<<30, 2<<30, 10<<30, time.Now().Add(30*24*time.Hour).Unix()))
		body := "vless://" + id + "@127.0.0.1:47249?security=tls&type=grpc#inb1-ali.user-x\nvless://" + id + "@127.0.0.1:47249?type=grpc#inb2-ali.user-y"
		fmt.Fprint(w, base64.StdEncoding.EncodeToString([]byte(body)))
	}))
	defer old.Close()
	link := old.URL + "/sub/abcdefgh123"
	if err := s.StartLegacyImport(ctx, "ali", []string{link}); !errors.Is(err, ErrPolicy) {
		t.Fatal("not allowed yet", err)
	}
	_ = s.PutLegacyAllowed(ctx, "ali", true)
	wait := func() LegacyJob {
		for i := 0; i < 200; i++ {
			if j := legacyJob("ali"); !j.Running {
				return j
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("import hangs")
		return LegacyJob{}
	}
	if err := s.StartLegacyImport(ctx, "ali", []string{link, "https://example.invalid/nothing"}); err != nil {
		t.Fatal(err)
	}
	if j := wait(); j.Added != 1 || len(j.Failed) != 1 {
		t.Fatalf("job %+v", j)
	}
	list, _ := s.Clients(ctx)
	var c ClientView
	for _, x := range list {
		if x.Email == "ali.user" {
			c = x
		}
	}
	if c.RepID != "ali" || !c.Legacy || c.SubToken != "abcdefgh123" || c.UUID != id || c.VolumeBytes != 10<<30 || c.Usage.Up+c.Usage.Down != 3<<30 || c.ExpiresUnix == 0 {
		t.Fatalf("imported: %+v", c)
	}
	lr := s.LegacyOf(ctx, "ali")
	u, _ := url.Parse(old.URL)
	if lr.Domain != "127.0.0.1" || lr.OldIP != "127.0.0.1" || len(lr.FwdPorts) != 1 || lr.FwdPorts[0] != 47249 || lr.SubPort == 0 || u.Port() == "" {
		t.Fatalf("move: %+v", lr)
	}
	if s.RepCredit(ctx, "ali") != 5<<30 {
		t.Fatal("bringing users costs nothing")
	}
	// Again: already here.
	_ = s.StartLegacyImport(ctx, "ali", []string{link})
	if j := wait(); j.Already != 1 || j.Added != 0 {
		t.Fatalf("again %+v", j)
	}
	// No renewal; switching off is fine.
	if _, err := s.RenewClient(ctx, Principal{User: "ali", Role: "rep", RepID: "ali"}, "ali.user", "volume", 10, 30); !errors.Is(err, ErrPolicy) {
		t.Fatal("renewed an old user", err)
	}
	cl := c.Client
	cl.VolumeBytes = 20 << 30
	if err := s.RepPutClient(ctx, "ali", "ali", cl, "volume"); !errors.Is(err, ErrPolicy) {
		t.Fatal("more volume for an old user", err)
	}
	// Use is paid as it happens.
	use := func(gb int64) {
		_, _ = s.db.Exec(`UPDATE client_usage SET down=down+? WHERE email='ali.user'`, gb<<30)
		s.chargeLegacy(ctx)
	}
	use(1)
	if s.RepCredit(ctx, "ali") != 4<<30 {
		t.Fatal("1 GB used, 1 GB paid", s.RepCredit(ctx, "ali"))
	}
	use(5)
	var on bool
	_ = s.db.QueryRow(`SELECT enabled FROM clients WHERE email='ali.user'`).Scan(&on)
	if s.RepCredit(ctx, "ali") != 0 || on {
		t.Fatal("credit gone: switched off")
	}
	cl = c.Client
	cl.Enabled = true
	if err := s.RepPutClient(ctx, "ali", "ali", cl, "volume"); !errors.Is(err, ErrCredit) {
		t.Fatal("switched on without credit", err)
	}
	_ = s.AdjustCredit(ctx, "o", "ali", 1<<30, 0, "")
	s.chargeLegacy(ctx)
	_ = s.db.QueryRow(`SELECT enabled FROM clients WHERE email='ali.user'`).Scan(&on)
	if !on {
		t.Fatal("credit back: switched on")
	}
	// The old address answers with the subscription here.
	h := s.legacySubHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "https://127.0.0.1:2096/sub/abcdefgh123", nil)
	req.Header.Set("User-Agent", "v2rayNG/1.9")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		var em string
		e1 := s.ro.QueryRow(`SELECT c.email FROM clients c JOIN legacy_clients l ON l.email=c.email JOIN legacy_reps lr ON lr.rep_id=l.rep_id WHERE c.sub_token=? AND lr.domain=?`, "abcdefgh123", "127.0.0.1").Scan(&em)
		e2, e3 := s.EmailBySubToken(ctx, "abcdefgh123")
		t.Log("diag", em, e1, e2, e3)
	}
	if rec.Code != 200 || s.LegacyStatusOf(ctx, "ali").Updated != 1 {
		t.Fatal("old link", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "https://other.example:2096/sub/abcdefgh123", nil))
	if rec.Code != 404 {
		t.Fatal("only on its own domain")
	}
	// Domain step: once it points here, the old ports open.
	if err := s.LegacyStep(ctx, "ali", 1); err != nil {
		t.Fatal(err)
	}
	SetHubIPs("10.9.9.9")
	if ok, _, _ := s.LegacyCheckDNS(ctx, "ali"); ok {
		t.Fatal("not pointing here yet")
	}
	SetHubIPs("127.0.0.1")
	defer SetHubIPs("")
	if ok, why, err := s.LegacyCheckDNS(ctx, "ali"); !ok || err != nil {
		t.Fatal("pointing here", why, err)
	}
	want := s.legacyWanted(ctx)
	if want[lr.SubPort] != "sub:tls" || want[47249] != "fwd:127.0.0.1:47249" {
		t.Fatal("listeners", want)
	}
	if err := s.StartLegacyImport(ctx, "ali", []string{link}); err == nil {
		t.Fatal("no more links after the move")
	}
}
