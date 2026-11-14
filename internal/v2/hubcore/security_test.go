package hubcore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRealIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Forwarded-For", "9.9.9.9, 5.6.7.8")
	if ip := realIP(r); ip != "5.6.7.8" {
		t.Fatal("behind a local proxy the last forwarded address counts", ip)
	}
	r.RemoteAddr = "8.8.4.4:5555"
	if ip := realIP(r); ip != "8.8.4.4" {
		t.Fatal("a public client must not choose its own address", ip)
	}
}

func TestLoginLockout(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	if err := s.SetAdmin(ctx, "boss", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandler(s, ""))
	defer h.Close()
	try := func(ip, pass string) int {
		req, _ := http.NewRequest("POST", h.URL+"/api/v2/auth/login", strings.NewReader(`{"Username":"boss","Password":"`+pass+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Forwarded-For", ip)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for i := 0; i < 5; i++ {
		if c := try("6.6.6.6", "nope"); c != 401 {
			t.Fatalf("attempt %d: %d", i, c)
		}
	}
	if c := try("6.6.6.6", "correct horse battery"); c != 429 {
		t.Fatal("after 5 wrong passwords the address must wait, even with the right one", c)
	}
	if c := try("7.7.7.7", "correct horse battery"); c != 200 {
		t.Fatal("the owner from another address must still get in", c)
	}
	locks := s.logins.list(*now)
	if len(locks) == 0 || locks[0].IP != "6.6.6.6" {
		t.Fatal(locks)
	}
	if n := s.logins.unlock("6.6.6.6"); n == 0 {
		t.Fatal("unlock")
	}
	if c := try("6.6.6.6", "correct horse battery"); c != 200 {
		t.Fatal("unlocked", c)
	}
	// The lock ends by itself, and a repeat offender waits longer.
	for i := 0; i < 5; i++ {
		try("6.6.6.6", "nope")
	}
	*now = now.Add(16 * time.Minute)
	if c := try("6.6.6.6", "correct horse battery"); c != 200 {
		t.Fatal("lock must expire", c)
	}
	for i := 0; i < 5; i++ {
		try("6.6.6.6", "nope")
	}
	if l := s.logins.list(*now); len(l) == 0 || l[0].Until-now.Unix() < int64(29*60) {
		t.Fatal("second lock should be longer", l)
	}
}

func TestSessions(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.SetAdmin(ctx, "boss", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(AdminHandler(s, ""))
	defer h.Close()
	login := func(agent string) *http.Cookie {
		req, _ := http.NewRequest("POST", h.URL+"/api/v2/auth/login", strings.NewReader(`{"Username":"boss","Password":"correct horse battery"}`))
		req.Header.Set("User-Agent", agent)
		resp, err := http.DefaultClient.Do(req)
		if err != nil || resp.StatusCode != 200 {
			t.Fatal(err, resp.StatusCode)
		}
		return resp.Cookies()[0]
	}
	a, b := login("laptop"), login("phone")
	list, err := s.Sessions(ctx, "boss", a.Value)
	if err != nil || len(list) != 2 {
		t.Fatal(list, err)
	}
	cur := 0
	for _, x := range list {
		if x.Current {
			cur++
			if x.Agent != "laptop" || x.IP == "" {
				t.Fatal(x)
			}
		}
	}
	if cur != 1 {
		t.Fatal("exactly one current session", list)
	}
	req, _ := http.NewRequest("POST", h.URL+"/api/v2/auth/sessions/end-others", nil)
	req.AddCookie(a)
	req.Header.Set("X-Dark-Request", "1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err, resp.StatusCode)
	}
	if _, err := s.SessionUser(ctx, b.Value); err == nil {
		t.Fatal("the phone session must be gone")
	}
	if _, err := s.SessionUser(ctx, a.Value); err != nil {
		t.Fatal("this session stays")
	}
}
