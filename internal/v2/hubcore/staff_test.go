package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStaffRolesAndAudit(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if err := s.SetAdmin(ctx, "boss", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutStaff(ctx, "helper", "admin", "helper password 1"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutStaff(ctx, "watcher", "viewer", "viewer password 1"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutStaff(ctx, "boss", "admin", ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("demoting the last owner: %v", err)
	}
	if err := s.DeleteStaff(ctx, "boss"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("deleting the last owner: %v", err)
	}
	if err := s.PutStaff(ctx, "newbie", "viewer", "short"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short password: %v", err)
	}
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
		req.AddCookie(c)
		req.Header.Set("X-Dark-Request", "1")
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, string(b)
	}
	boss, helper, watcher := login("boss", "correct horse battery"), login("helper", "helper password 1"), login("watcher", "viewer password 1")
	client := `{"Enabled":true,"VolumeBytes":-1}`
	if code, b := call(helper, "PUT", "/api/v2/admin/clients/amir", client); code != 200 {
		t.Fatalf("helper adds a client: %d %s", code, b)
	}
	if code, _ := call(helper, "GET", "/api/v2/admin/clients", ""); code != 200 {
		t.Fatal("helper lists clients")
	}
	for _, p := range []string{"/api/v2/admin/staff", "/api/v2/admin/audit", "/api/v2/admin/backup"} {
		if code, _ := call(helper, "GET", p, ""); code != 403 {
			t.Fatalf("helper reached %s: %d", p, code)
		}
	}
	if code, _ := call(watcher, "GET", "/api/v2/admin/clients", ""); code != 200 {
		t.Fatal("viewer reads clients")
	}
	if code, _ := call(watcher, "PUT", "/api/v2/admin/clients/sara", client); code != 403 {
		t.Fatal("viewer must not change clients")
	}
	if code, _ := call(watcher, "DELETE", "/api/v2/admin/clients/amir", ""); code != 403 {
		t.Fatal("viewer must not delete clients")
	}
	if code, b := call(watcher, "GET", "/api/v2/auth/me", ""); code != 200 || !strings.Contains(b, `"level":"viewer"`) {
		t.Fatalf("me: %s", b)
	}
	if code, _ := call(boss, "DELETE", "/api/v2/admin/staff/boss", ""); code != 400 {
		t.Fatal("owner deleting own login")
	}
	code, b := call(boss, "GET", "/api/v2/admin/audit?q=amir", "")
	var log []AuditEntry
	if code != 200 || json.Unmarshal([]byte(b), &log) != nil || len(log) != 1 || log[0].Actor != "helper" || log[0].Role != "admin" || log[0].Status != 200 || log[0].Method != "PUT" {
		t.Fatalf("audit %d %s", code, b)
	}
	code, b = call(boss, "GET", "/api/v2/admin/audit?actor=watcher", "")
	if code != 200 || !strings.Contains(b, `"status":403`) {
		t.Fatalf("refused attempts are logged: %s", b)
	}
	staff, _ := s.Staff(ctx)
	if len(staff) != 3 || staff[0].Username != "boss" {
		t.Fatalf("%+v", staff)
	}
	// A role change ends that login's sessions at once.
	if err := s.PutStaff(ctx, "helper", "viewer", ""); err != nil {
		t.Fatal(err)
	}
	if code, _ := call(helper, "GET", "/api/v2/admin/clients", ""); code != 401 {
		t.Fatal("old session survived a role change")
	}
}

func TestTOTPLogin(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	if err := s.SetAdmin(ctx, "boss", "correct horse battery"); err != nil {
		t.Fatal(err)
	}
	// RFC 6238 test vector (SHA-1, T=59s → 94287082, last 6 digits).
	if c, _ := totpAt(b32.EncodeToString([]byte("12345678901234567890")), 59/30); c != "287082" {
		t.Fatalf("rfc vector %s", c)
	}
	secret, uri, err := s.TOTPSetup(ctx, "boss")
	if err != nil || !strings.HasPrefix(uri, "otpauth://totp/") {
		t.Fatal(uri, err)
	}
	if _, err := s.LoginTOTP(ctx, "boss", "correct horse battery", ""); err != nil {
		t.Fatal("pending setup must not lock the login out")
	}
	if err := s.TOTPEnable(ctx, "boss", "000000"); !errors.Is(err, ErrInvalid) {
		t.Fatal("wrong code enabled 2fa")
	}
	code := func() string { c, _ := totpAt(secret, now.Unix()/30); return c }
	if err := s.TOTPEnable(ctx, "boss", code()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoginTOTP(ctx, "boss", "correct horse battery", ""); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("missing code: %v", err)
	}
	if _, err := s.LoginTOTP(ctx, "boss", "wrong password!!", ""); !errors.Is(err, ErrAuth) {
		t.Fatal("wrong password must not reveal 2fa")
	}
	*now = now.Add(30 * time.Second)
	c := code()
	if _, err := s.LoginTOTP(ctx, "boss", "correct horse battery", c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoginTOTP(ctx, "boss", "correct horse battery", c); !errors.Is(err, ErrAuth) {
		t.Fatal("a code must work only once")
	}
	// The HTTP login asks for the code instead of failing outright.
	h := httptest.NewServer(AdminHandler(s, ""))
	defer h.Close()
	resp, _ := http.Post(h.URL+"/api/v2/auth/login", "application/json", strings.NewReader(`{"Username":"boss","Password":"correct horse battery"}`))
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 401 || !strings.Contains(string(b), "need_code") || len(resp.Cookies()) != 0 {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	if err := s.ChangePassword(ctx, "boss", "correct horse battery", "another long password"); err != nil {
		t.Fatal("changing the password must not need a code", err)
	}
	*now = now.Add(30 * time.Second)
	if err := s.TOTPDisable(ctx, "boss", "another long password", code()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx, "boss", "another long password"); err != nil {
		t.Fatal("2fa off", err)
	}
}
