package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/telegrambot"
)

const telegramCredentialSentinel = "101:SYNTHETIC_PRIVATE_TOKEN_SENTINEL_0123456789"

type telegramManagerFixture struct {
	mu              sync.Mutex
	state           telegrambot.ManagementStatus
	statuses        []telegrambot.BotStatus
	identity        telegrambot.BotIdentity
	failure         error
	verifyCalls     int
	configureCalls  int
	checkCalls      int
	commits         int
	guardCalls      int
	badDeadline     bool
	lastBot         int64
	lastToken       string
	beforeVerify    func(context.Context) error
	beforeConfigure func(context.Context) error
	beforeCheck     func(context.Context) error
}

func (m *telegramManagerFixture) snapshotLocked() telegrambot.ManagementStatus {
	return telegrambot.ManagementStatus{Revision: m.state.Revision, Connections: append([]telegrambot.ManagedConnection{}, m.state.Connections...)}
}

func (m *telegramManagerFixture) Management() telegrambot.ManagementStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.snapshotLocked()
}

func (m *telegramManagerFixture) Statuses() []telegrambot.BotStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]telegrambot.BotStatus{}, m.statuses...)
}

func (m *telegramManagerFixture) deadlineLocked(ctx context.Context) {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) <= 0 || time.Until(deadline) > telegramConnectionTimeout {
		m.badDeadline = true
	}
}

func (m *telegramManagerFixture) Verify(ctx context.Context, token string) (telegrambot.BotIdentity, error) {
	m.mu.Lock()
	m.verifyCalls++
	m.lastToken = token
	m.deadlineLocked(ctx)
	hook, failure, identity := m.beforeVerify, m.failure, m.identity
	m.mu.Unlock()
	if hook != nil {
		if err := hook(ctx); err != nil {
			return telegrambot.BotIdentity{}, err
		}
	}
	return identity, failure
}

func (m *telegramManagerFixture) ConfigureAuthorized(ctx context.Context, botID int64, token string, revision int64, guard func(context.Context) error) (telegrambot.ManagementStatus, error) {
	m.mu.Lock()
	m.configureCalls++
	m.lastBot, m.lastToken = botID, token
	m.deadlineLocked(ctx)
	hook, failure := m.beforeConfigure, m.failure
	m.mu.Unlock()
	if hook != nil {
		if err := hook(ctx); err != nil {
			return telegrambot.ManagementStatus{}, err
		}
	}
	if failure != nil {
		return telegrambot.ManagementStatus{}, failure
	}
	if guard == nil {
		return telegrambot.ManagementStatus{}, telegrambot.ErrAccess
	}
	m.mu.Lock()
	m.guardCalls++
	m.mu.Unlock()
	if guard(ctx) != nil {
		return telegrambot.ManagementStatus{}, telegrambot.ErrAccess
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if revision != m.state.Revision {
		return telegrambot.ManagementStatus{}, telegrambot.ErrConflict
	}
	index := -1
	for i, connection := range m.state.Connections {
		if connection.BotID == botID {
			index = i
		}
	}
	if token == "" && index == -1 {
		return telegrambot.ManagementStatus{}, telegrambot.ErrConfig
	}
	connection := telegrambot.ManagedConnection{BotID: botID, Configured: true, Username: "LabStoreBot", State: "configured"}
	if index == -1 {
		m.state.Connections = append(m.state.Connections, connection)
	} else {
		m.state.Connections[index] = connection
	}
	m.state.Revision++
	m.commits++
	return m.snapshotLocked(), nil
}

func (m *telegramManagerFixture) Check(ctx context.Context, botID int64) (telegrambot.BotIdentity, error) {
	m.mu.Lock()
	m.checkCalls++
	m.lastBot = botID
	m.deadlineLocked(ctx)
	hook, failure, identity := m.beforeCheck, m.failure, m.identity
	m.mu.Unlock()
	if hook != nil {
		if err := hook(ctx); err != nil {
			return telegrambot.BotIdentity{}, err
		}
	}
	return identity, failure
}

func (m *telegramManagerFixture) calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.verifyCalls + m.configureCalls + m.checkCalls
}

type telegramConnectionFixture struct {
	s      *Server
	h      http.Handler
	m      *telegramManagerFixture
	cookie *http.Cookie
	csrf   string
	db     string
}

func newTelegramConnectionFixture(t *testing.T) telegramConnectionFixture {
	t.Helper()
	db := filepath.Join(t.TempDir(), "private", "business.sqlite")
	s := businessLabAt(t, db)
	ctx := context.Background()
	if _, err := s.business.BootstrapOwner(ctx, ownerUsername, ownerPassword); err != nil {
		t.Fatal(err)
	}
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableTelegramBotsV1(ctx); err != nil {
		t.Fatal(err)
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	m := &telegramManagerFixture{state: telegrambot.ManagementStatus{Revision: 1}, identity: telegrambot.BotIdentity{BotID: 101, Username: "LabStoreBot"}}
	s.SetTelegramBotManager(m)
	f := telegramConnectionFixture{s: s, h: s.BrowserHandler(), m: m, cookie: cookie, csrf: csrf, db: db}
	f.register(t, "shop-a", 101, "owner")
	return f
}

func (f telegramConnectionFixture) register(t *testing.T, id string, bot int64, rep string) {
	t.Helper()
	w := browserRequest(t, f.h, "POST", "/api/v1/workspace/telegram/stores", business.TelegramStoreRegistration{
		EventID: "telegram-connection-register-" + id, ID: id, BotID: bot, RepresentativeID: rep,
		Name: "Store " + id, ManagerUserID: bot + 1000, Enabled: true,
	}, f.cookie, f.csrf, "https://"+browserHost)
	if w.Code != http.StatusOK {
		t.Fatal("register store", w.Code, w.Body.String())
	}
}

func (f telegramConnectionFixture) post(t *testing.T, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return browserRequest(t, f.h, "POST", path, body, f.cookie, f.csrf, "https://"+browserHost)
}

func telegramRawRequest(t *testing.T, f telegramConnectionFixture, path, raw string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest("POST", "https://"+browserHost+path, strings.NewReader(raw))
	r.AddCookie(f.cookie)
	r.Header.Set("Origin", "https://"+browserHost)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func assertTelegramRedacted(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if strings.Contains(w.Body.String(), telegramCredentialSentinel) || strings.Contains(w.Body.String(), "SYNTHETIC_PRIVATE_TOKEN_SENTINEL") {
		t.Fatal("credential value escaped the private manager")
	}
	if !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("credential route result became cacheable")
	}
}

func TestTelegramConnectionOwnerCSRFAndDisabledBoundaries(t *testing.T) {
	f := newTelegramConnectionFixture(t)
	if _, err := f.s.business.CreateRepresentative(context.Background(), "rep-a", "Rep A"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.ProvisionRepresentativeLogin(context.Background(), "rep-a", "rep-a", repPassword); err != nil {
		t.Fatal(err)
	}
	rep, repCSRF := browserLogin(t, f.s, "rep-a", repPassword)
	for _, route := range []struct {
		path string
		body any
	}{
		{"/api/v1/workspace/telegram/verify", map[string]any{"token": telegramCredentialSentinel}},
		{"/api/v1/workspace/telegram/stores/shop-a/connection", map[string]any{"token": telegramCredentialSentinel, "expected_revision": 1}},
		{"/api/v1/workspace/telegram/stores/shop-a/check", map[string]any{}},
	} {
		for _, attempt := range []struct {
			name   string
			cookie *http.Cookie
			csrf   string
			origin string
			status int
		}{
			{"anonymous", nil, "", "https://" + browserHost, 401},
			{"missing csrf", f.cookie, "", "https://" + browserHost, 403},
			{"wrong origin", f.cookie, f.csrf, "https://other.example", 403},
			{"representative", rep, repCSRF, "https://" + browserHost, 403},
		} {
			w := browserRequest(t, f.h, "POST", route.path, route.body, attempt.cookie, attempt.csrf, attempt.origin)
			if w.Code != attempt.status {
				t.Fatalf("%s %s: %d", attempt.name, route.path, w.Code)
			}
			assertTelegramRedacted(t, w)
		}
		for _, suffix := range []string{"?token=" + telegramCredentialSentinel, "?"} {
			w := f.post(t, route.path+suffix, route.body)
			if w.Code != 400 {
				t.Fatal("query input accepted", w.Code)
			}
			assertTelegramRedacted(t, w)
		}
		if w := browserRequest(t, f.s.AgentHandler(), "POST", route.path, route.body, f.cookie, f.csrf, "https://"+browserHost); w.Code == 200 {
			t.Fatal("Agent listener exposed credential administration")
		}
	}
	if f.m.calls() != 0 {
		t.Fatal("rejected requests reached the private manager")
	}
	f.s.telegramBotsV1Enabled = false
	w := f.post(t, "/api/v1/workspace/telegram/verify", map[string]string{"token": telegramCredentialSentinel})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "telegram_not_enabled") {
		t.Fatal("disabled feature accepted connection", w.Code)
	}
	f.s.telegramBotsV1Enabled = true
	f.s.SetTelegramBotManager(nil)
	w = f.post(t, "/api/v1/workspace/telegram/verify", map[string]string{"token": telegramCredentialSentinel})
	if w.Code != 503 || !strings.Contains(w.Body.String(), "telegram_management_unavailable") || f.m.calls() != 0 {
		t.Fatal("absent private manager claimed connection capability", w.Code)
	}
	view := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, f.cookie, "", "")
	if !strings.Contains(view.Body.String(), `"can_manage_connections":false`) {
		t.Fatal("missing manager was advertised as available")
	}
}

func TestTelegramConnectionStrictCredentialJSONAndStoreBinding(t *testing.T) {
	f := newTelegramConnectionFixture(t)
	for _, route := range []struct {
		path   string
		bodies []string
	}{
		{"/api/v1/workspace/telegram/verify", []string{
			`{}`, `null`, `[]`, `{"token":null}`, `{"Token":"ignored"}`, `{"token":123}`,
			`{"token":"first","token":"second"}`, `{"token":"value","url":"https://other.example"}`,
			`{"token":"value"} {}`, `{"token":"` + strings.Repeat("x", 2000) + `"}`,
		}},
		{"/api/v1/workspace/telegram/stores/shop-a/connection", []string{
			`{"token":"value"}`, `{"token":null,"expected_revision":1}`, `{"token":"value","expected_revision":null}`,
			`{"token":"value","expected_revision":1.5}`, `{"token":"value","expected_revision":-1}`,
			`{"token":"value","expected_revision":9007199254740992}`,
			`{"token":"value","expected_revision":1,"bot_id":999}`,
			`{"token":"value","expected_revision":1,"Expected_revision":1}`,
			`{"token":"first","token":"second","expected_revision":1}`,
		}},
		{"/api/v1/workspace/telegram/stores/shop-a/check", []string{`null`, `[]`, `{"token":"value"}`, `{} {}`}},
	} {
		for _, body := range route.bodies {
			w := telegramRawRequest(t, f, route.path, body)
			if w.Code != 400 {
				t.Fatalf("ambiguous/oversized request accepted on %s: %d", route.path, w.Code)
			}
			assertTelegramRedacted(t, w)
		}
	}
	for _, path := range []string{"/api/v1/workspace/telegram/stores/missing/check", "/api/v1/workspace/telegram/stores/missing/connection"} {
		body := map[string]any{}
		if strings.HasSuffix(path, "/connection") {
			body = map[string]any{"token": telegramCredentialSentinel, "expected_revision": 1}
		}
		if w := f.post(t, path, body); w.Code != 404 || !strings.Contains(w.Body.String(), "telegram_store_not_found") {
			t.Fatal("unregistered store reached private credentials", w.Code)
		}
	}
	if w := f.post(t, "/api/v1/workspace/telegram/stores/_invalid/check", map[string]any{}); w.Code != 400 {
		t.Fatal("invalid store identifier accepted", w.Code)
	}
	if f.m.calls() != 0 {
		t.Fatal("invalid body or binding reached private runtime")
	}
}

func TestTelegramConnectionVerifyConfigureRetryAndCAS(t *testing.T) {
	f := newTelegramConnectionFixture(t)
	f.m.state.Connections = []telegrambot.ManagedConnection{{BotID: 999, Configured: true, Username: "OtherUnregisteredBot", State: "polling"}}
	w := f.post(t, "/api/v1/workspace/telegram/verify", map[string]any{"token": telegramCredentialSentinel})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"bot_id":101`) || f.m.commits != 0 || f.m.Management().Revision != 1 {
		t.Fatal("preview mutated configuration or returned wrong identity", w.Code)
	}
	assertTelegramRedacted(t, w)
	w = f.post(t, "/api/v1/workspace/telegram/stores/shop-a/connection", map[string]any{"token": telegramCredentialSentinel, "expected_revision": 1})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"revision":2`) || strings.Contains(w.Body.String(), "999") || f.m.lastBot != 101 || f.m.lastToken != telegramCredentialSentinel || f.m.guardCalls != 1 || f.m.commits != 1 {
		t.Fatal("connection did not bind current store or run final authorization", w.Code, w.Body.String())
	}
	assertTelegramRedacted(t, w)
	view := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, f.cookie, "", "")
	for _, required := range []string{`"can_manage_connections":true`, `"runtime_revision":2`, `"credential_configured":true`, `"bot_username":"LabStoreBot"`, `"connection_state":"configured"`} {
		if !strings.Contains(view.Body.String(), required) {
			t.Fatal("missing truthful managed status", required, view.Body.String())
		}
	}
	if strings.Contains(view.Body.String(), "999") || strings.Contains(view.Body.String(), "OtherUnregisteredBot") || strings.Contains(view.Body.String(), `"connection_state":"polling"`) {
		t.Fatal("unregistered credential or fabricated polling exposed")
	}
	w = f.post(t, "/api/v1/workspace/telegram/stores/shop-a/check", map[string]any{})
	if w.Code != 200 || f.m.lastBot != 101 || f.m.checkCalls != 1 || f.m.commits != 1 {
		t.Fatal("saved-token check mutated credentials or wrong store", w.Code)
	}
	w = f.post(t, "/api/v1/workspace/telegram/stores/shop-a/connection", map[string]any{"token": "", "expected_revision": 2})
	if w.Code != 200 || f.m.lastToken != "" || f.m.Management().Revision != 3 || f.m.commits != 2 {
		t.Fatal("saved credential retry did not use scoped manager", w.Code)
	}
	w = f.post(t, "/api/v1/workspace/telegram/stores/shop-a/connection", map[string]any{"token": telegramCredentialSentinel, "expected_revision": 1})
	if w.Code != 409 || !strings.Contains(w.Body.String(), "telegram_revision_conflict") || f.m.commits != 2 || f.m.badDeadline {
		t.Fatal("stale update was committed or deadline missing", w.Code)
	}
	assertTelegramRedacted(t, w)
	for _, path := range []string{f.db, f.db + "-wal", f.db + "-journal"} {
		raw, err := os.ReadFile(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(telegramCredentialSentinel)) {
			t.Fatal("credential was written into business storage")
		}
	}
}

func TestTelegramConnectionMetadataScopeAndSanitization(t *testing.T) {
	f := newTelegramConnectionFixture(t)
	ctx := context.Background()
	for _, rep := range []string{"rep-a", "rep-b"} {
		if _, err := f.s.business.CreateRepresentative(ctx, rep, rep); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.business.ProvisionRepresentativeLogin(ctx, rep, rep, repPassword); err != nil {
			t.Fatal(err)
		}
	}
	f.register(t, "shop-rep-a", 102, "rep-a")
	f.register(t, "shop-rep-b", 103, "rep-b")
	f.m.state = telegrambot.ManagementStatus{Revision: 17, Connections: []telegrambot.ManagedConnection{
		{BotID: 101, Configured: true, Username: telegramCredentialSentinel, State: telegramCredentialSentinel, Problem: telegramCredentialSentinel},
		{BotID: 102, Configured: true, Username: "RepABot", State: "polling"},
		{BotID: 103, Configured: true, Username: "RepBSecretName", State: "error"},
		{BotID: 999, Configured: true, Username: "UnregisteredSecretName", State: "polling"},
	}}
	f.m.statuses = []telegrambot.BotStatus{{BotID: 101, State: telegramCredentialSentinel, LastError: telegramCredentialSentinel}}
	ownerView := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, f.cookie, "", "")
	assertTelegramRedacted(t, ownerView)
	if !strings.Contains(ownerView.Body.String(), `"connection_problem":"connection_unavailable"`) || !strings.Contains(ownerView.Body.String(), `"connection_state":"unknown"`) || strings.Contains(ownerView.Body.String(), "UnregisteredSecretName") {
		t.Fatal("status text was not constrained", ownerView.Body.String())
	}
	f.m.state.Connections[0].State = "error"
	f.m.state.Connections[0].Problem = "webhook_exists"
	f.m.statuses[0].State, f.m.statuses[0].LastError = "polling", ""
	ownerView = browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, f.cookie, "", "")
	var newest struct {
		Stores []telegramWorkspaceStore `json:"stores"`
	}
	if json.Unmarshal(ownerView.Body.Bytes(), &newest) != nil || len(newest.Stores) != 3 || newest.Stores[0].BotID != 101 || newest.Stores[0].ConnectionState != "error" || newest.Stores[0].ConnectionProblem != "webhook_exists" {
		t.Fatal("older queue snapshot overwrote the newer managed connection state")
	}
	rep, _ := browserLogin(t, f.s, "rep-a", repPassword)
	w := browserRequest(t, f.h, "GET", "/api/v1/workspace/telegram", nil, rep, "", "")
	var payload struct {
		CanManageConnections bool                     `json:"can_manage_connections"`
		RuntimeRevision      int64                    `json:"runtime_revision"`
		Stores               []telegramWorkspaceStore `json:"stores"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &payload) != nil || payload.CanManageConnections || payload.RuntimeRevision != 0 || len(payload.Stores) != 1 || payload.Stores[0].BotID != 102 || !payload.Stores[0].CredentialConfigured || payload.Stores[0].BotUsername != "RepABot" {
		t.Fatal("representative credential metadata scope", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "RepBSecretName") || strings.Contains(w.Body.String(), "UnregisteredSecretName") {
		t.Fatal("foreign managed bot identity exposed")
	}
	f.m.identity = telegrambot.BotIdentity{BotID: 777, Username: telegramCredentialSentinel}
	w = f.post(t, "/api/v1/workspace/telegram/stores/shop-a/check", map[string]any{})
	if w.Code != 422 || !strings.Contains(w.Body.String(), "telegram_identity_mismatch") {
		t.Fatal("saved check accepted another bot")
	}
	assertTelegramRedacted(t, w)
}

func TestTelegramConnectionErrorsStayRedacted(t *testing.T) {
	f := newTelegramConnectionFixture(t)
	for _, tc := range []struct {
		err    error
		status int
		code   string
	}{
		{telegrambot.ErrConfig, 422, "telegram_credentials_invalid"},
		{telegrambot.ErrIdentity, 422, "telegram_identity_mismatch"},
		{telegrambot.ErrWebhook, 409, "telegram_webhook_exists"},
		{telegrambot.ErrConflict, 409, "telegram_revision_conflict"},
		{telegrambot.ErrCapacity, 409, "telegram_capacity_reached"},
		{telegrambot.ErrAccess, 403, "owner_permission_required"},
		{telegrambot.ErrState, 503, "telegram_connection_unavailable"},
		{telegrambot.ErrJournal, 503, "telegram_connection_unavailable"},
		{context.DeadlineExceeded, 503, "telegram_transport_unavailable"},
		{telegrambot.ErrTransport, 503, "telegram_transport_unavailable"},
		{&telegrambot.APIError{Code: 401, Definitive: true, Reason: telegramCredentialSentinel}, 422, "telegram_credentials_invalid"},
		{&telegrambot.APIError{Code: 429, Definitive: true, RetryAfter: 24 * time.Hour, Reason: telegramCredentialSentinel}, 429, "telegram_rate_limited"},
		{errors.New("https://api.telegram.org/bot" + telegramCredentialSentinel + "/getMe"), 503, "telegram_connection_unavailable"},
	} {
		f.m.failure = fmt.Errorf("%s: %w", telegramCredentialSentinel, tc.err)
		w := f.post(t, "/api/v1/workspace/telegram/verify", map[string]string{"token": telegramCredentialSentinel})
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.code) {
			t.Fatalf("wrong redacted error mapping: %d %s", w.Code, w.Body.String())
		}
		assertTelegramRedacted(t, w)
		if tc.status == 429 && w.Header().Get("Retry-After") != "3600" {
			t.Fatal("unbounded retry hint")
		}
	}
}

func TestTelegramConnectionRechecksAuthorityAfterNetwork(t *testing.T) {
	for _, operation := range []string{"verify", "connection", "check"} {
		t.Run(operation, func(t *testing.T) {
			f := newTelegramConnectionFixture(t)
			revoke := func(ctx context.Context) error { return f.s.business.Logout(ctx, f.cookie.Value) }
			path := "/api/v1/workspace/telegram/stores/shop-a/" + operation
			body := map[string]any{}
			switch operation {
			case "verify":
				path = "/api/v1/workspace/telegram/verify"
				body["token"] = telegramCredentialSentinel
				f.m.beforeVerify = revoke
			case "connection":
				body = map[string]any{"token": telegramCredentialSentinel, "expected_revision": 1}
				f.m.beforeConfigure = revoke
			case "check":
				f.m.beforeCheck = revoke
			}
			w := f.post(t, path, body)
			if w.Code != 403 || !strings.Contains(w.Body.String(), "owner_permission_required") || f.m.commits != 0 || f.m.Management().Revision != 1 {
				t.Fatal("revoked owner kept a credential operation", w.Code, w.Body.String())
			}
			assertTelegramRedacted(t, w)
			if operation == "connection" && f.m.guardCalls != 1 {
				t.Fatal("credential persistence lacked final owner guard")
			}
		})
	}
}

func TestTelegramConnectionBoundsConcurrentOutboundChecks(t *testing.T) {
	f := newTelegramConnectionFixture(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	f.m.beforeVerify = func(ctx context.Context) error {
		entered <- struct{}{}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-release:
			return nil
		}
	}
	results := make(chan *httptest.ResponseRecorder, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results <- f.post(t, "/api/v1/workspace/telegram/verify", map[string]any{"token": telegramCredentialSentinel})
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			close(release)
			t.Fatal("parallel verification did not enter its bounded manager")
		}
	}
	w := f.post(t, "/api/v1/workspace/telegram/verify", map[string]any{"token": telegramCredentialSentinel})
	if w.Code != 429 || w.Header().Get("Retry-After") == "" || !strings.Contains(w.Body.String(), "telegram_connection_busy") || f.m.calls() != 2 {
		close(release)
		t.Fatal("unbounded parallel Telegram requests admitted", w.Code)
	}
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case result := <-results:
			if result.Code != 200 {
				t.Fatal("admitted verification failed", result.Code)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("outbound slot was not released")
		}
	}
	f.s.telegramConnectionGate.mu.Lock()
	f.s.telegramConnectionGate.attempts = 60
	f.s.telegramConnectionGate.window = time.Now()
	f.s.telegramConnectionGate.mu.Unlock()
	w = f.post(t, "/api/v1/workspace/telegram/verify", map[string]any{"token": telegramCredentialSentinel})
	if w.Code != 429 || f.m.calls() != 2 || f.m.badDeadline {
		t.Fatal("window limit/deadline did not bound requests", w.Code)
	}
}
