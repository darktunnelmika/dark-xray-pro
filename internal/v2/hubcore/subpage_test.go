package hubcore

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubPageAndSettings(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if err := s.PutSubSettings(ctx, SubSettings{RemarkTemplate: "{remark} · {left}", InfoLines: true, Announce: "تمدید با ربات", SupportURL: "https://t.me/help", Title: "Dark VPN"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSubSettings(ctx, SubSettings{SupportURL: "javascript:alert(1)"}); err == nil {
		t.Fatal("unsafe support link accepted")
	}
	list, _ := s.Clients(ctx)
	var tok string
	for _, c := range list {
		if c.Email == "alice" {
			tok = c.SubToken
		}
	}
	h := SubHandler(s)
	get := func(ua, accept, q string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/sub/"+tok+q, nil)
		r.Header.Set("User-Agent", ua)
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	page := get("Mozilla/5.0 (Linux; Android 14) Chrome/130", "text/html,application/xhtml+xml", "")
	body := page.Body.String()
	if page.Code != 200 || !strings.HasPrefix(page.Header().Get("Content-Type"), "text/html") || !strings.Contains(body, "alice") ||
		!strings.Contains(body, "v2rayng://install-sub?url=") || !strings.Contains(body, "تمدید با ربات") || !strings.Contains(page.Header().Get("Content-Security-Policy"), "nonce-") {
		t.Fatalf("page %d %s", page.Code, body[:min(len(body), 400)])
	}
	if strings.Contains(body, "ZgotmplZ") {
		t.Fatal("a link was blocked by template escaping")
	}
	app := get("v2rayNG/1.9.9", "", "")
	if strings.HasPrefix(app.Header().Get("Content-Type"), "text/html") || app.Header().Get("Announce") == "" || app.Header().Get("Support-Url") != "https://t.me/help" {
		t.Fatalf("app response %v", app.Header())
	}
	links, _, _ := s.Subscription(ctx, "alice")
	if len(links) < 2 || !strings.Contains(links[0], "127.0.0.1:1") || !strings.Contains(links[1], "days") {
		t.Fatalf("info lines missing: %v", links)
	}
	if qr := get("Mozilla/5.0", "image/*", "?qr=1"); qr.Header().Get("Content-Type") != "image/png" {
		t.Fatal("qr")
	}
}
