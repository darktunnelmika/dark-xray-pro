//go:build linux

package telegrambot

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func managerTestToken(id int64, suffix string) string {
	return fmt.Sprintf("%d:ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789%s", id, suffix)
}

func managerTestPaths(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "credentials", "config.json"), filepath.Join(dir, "state", "journal.sqlite")
}

func newTestManager(t *testing.T, backend Backend, rt http.RoundTripper) (*Manager, string, string) {
	t.Helper()
	configPath, journalPath := managerTestPaths(t)
	m, err := openManager(configPath, journalPath, backend, rt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { m.Close() })
	return m, configPath, journalPath
}

func managerIdentityTransport(t *testing.T, calls *atomic.Int32) http.RoundTripper {
	t.Helper()
	return fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Scheme != "https" || r.URL.Host != "api.telegram.org" || r.URL.RawQuery != "" || r.Method != http.MethodPost {
			return nil, errors.New("unexpected Telegram origin or method")
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
		if len(parts) != 2 {
			return nil, errors.New("unexpected Telegram path")
		}
		id, err := strconv.ParseInt(strings.SplitN(parts[0], ":", 2)[0], 10, 64)
		if err != nil {
			return nil, err
		}
		switch parts[1] {
		case "getMe":
			return fakeResponse(200, fmt.Sprintf(`{"ok":true,"result":{"id":%d,"is_bot":true,"first_name":"Test","username":"unit_bot_%d"}}`, id, id)), nil
		case "getWebhookInfo":
			return fakeResponse(200, `{"ok":true,"result":{"url":"","pending_update_count":0}}`), nil
		default:
			return nil, errors.New("verification attempted a polling or dispatch method")
		}
	})
}

func TestManagerEmptyOnboardingAndBackup(t *testing.T) {
	var calls atomic.Int32
	m, configPath, journalPath := newTestManager(t, &fakeBackend{}, managerIdentityTransport(t, &calls))
	status := m.Management()
	if status.Revision != 1 || status.Connections == nil || len(status.Connections) != 0 {
		t.Fatalf("unexpected onboarding state: %+v", status)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config, err := parseConfig(raw)
	if err != nil || config.Version != 2 || config.Revision != 1 || config.Bots == nil || len(config.Bots) != 0 {
		t.Fatal("missing initialized v2 empty configuration", err)
	}
	for _, path := range []string{configPath, journalPath, journalPath + ".lock"} {
		info, err := os.Lstat(path)
		if err != nil || !privateRegular(info) || info.Mode().Perm() != 0600 {
			t.Fatal("private managed resource missing", err)
		}
	}
	if other, err := openManager(configPath, journalPath, &fakeBackend{}, nil); !errors.Is(err, ErrConflict) || other != nil {
		if other != nil {
			other.Close()
		}
		t.Fatal("second manager acquired live journal", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.started })
	select {
	case err := <-done:
		t.Fatal("empty manager exited before cancellation", err)
	default:
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("empty initialization contacted Telegram")
	}
	before, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(context.Background(), journalPath, raw); err != nil {
		t.Fatal("valid empty managed backup rejected", err)
	}
	after, err := os.ReadFile(journalPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("offline validation changed journal", err)
	}
	if err := ValidateOfflineBackup(context.Background(), "", raw); err == nil {
		t.Fatal("managed backup accepted without required journal")
	}
	reopened, err := openManager(configPath, journalPath, &fakeBackend{}, nil)
	if err != nil {
		t.Fatal("empty manager could not reopen", err)
	}
	reopened.Close()
	if err := os.Remove(journalPath); err != nil {
		t.Fatal(err)
	}
	if other, err := openManager(configPath, journalPath, &fakeBackend{}, nil); !errors.Is(err, ErrJournal) || other != nil {
		if other != nil {
			other.Close()
		}
		t.Fatal("missing managed history was recreated", err)
	}
}

func TestManagerVerificationHasNoPersistenceOrPolling(t *testing.T) {
	var calls atomic.Int32
	m, path, _ := newTestManager(t, &fakeBackend{}, managerIdentityTransport(t, &calls))
	before, _ := os.ReadFile(path)
	identity, err := m.Verify(context.Background(), managerTestToken(100, ""))
	if err != nil || identity.BotID != 100 || identity.Username != "unit_bot_100" || calls.Load() != 2 {
		t.Fatal("getMe/webhook verification failed", identity, err, calls.Load())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) || m.Management().Revision != 1 || len(m.Management().Connections) != 0 {
		t.Fatal("verification persisted credentials")
	}
	if _, err := m.Configure(context.Background(), 200, managerTestToken(100, ""), 1); !errors.Is(err, ErrIdentity) || calls.Load() != 2 {
		t.Fatal("token identity mismatch reached persistence", err)
	}
	guardCalls := 0
	if _, err := m.ConfigureAuthorized(context.Background(), 100, managerTestToken(100, ""), 1, func(context.Context) error {
		guardCalls++
		if calls.Load() != 4 {
			t.Fatal("authorization guard ran before Telegram verification")
		}
		return errors.New("secret guard context " + managerTestToken(100, ""))
	}); err != ErrAccess || guardCalls != 1 {
		t.Fatal("lost owner authorization was not redacted", err)
	}
	after, _ = os.ReadFile(path)
	known, err := knownBotIDs(context.Background(), m.worker.journal.db)
	if err != nil || len(known) != 0 || !bytes.Equal(before, after) {
		t.Fatal("denied activation changed credentials or journal", err)
	}
}

func TestManagerVerificationRejectsUnsafeIdentityAndWebhook(t *testing.T) {
	for _, tc := range []struct {
		name, me, webhook string
		failure           error
		want              error
	}{
		{name: "wrong_identity", me: `{"id":101,"is_bot":true,"first_name":"Test"}`, want: ErrIdentity},
		{name: "not_bot", me: `{"id":100,"is_bot":false,"first_name":"Test"}`, want: ErrIdentity},
		{name: "webhook", me: `{"id":100,"is_bot":true,"first_name":"Test"}`, webhook: `{"url":"https://example.invalid/webhook","pending_update_count":0}`, want: ErrWebhook},
		{name: "invalid_username", me: `{"id":100,"is_bot":true,"first_name":"Test","username":"bad/username"}`, want: ErrProtocol},
		{name: "transport_redaction", failure: errors.New("https://api.telegram.org/bot" + syntheticToken + "/getMe")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if tc.failure != nil {
					return nil, tc.failure
				}
				var body string
				switch filepath.Base(r.URL.Path) {
				case "getMe":
					body = tc.me
				case "getWebhookInfo":
					body = tc.webhook
				default:
					t.Error("verification consumed updates or changed webhook")
					return nil, ErrProtocol
				}
				return fakeResponse(200, `{"ok":true,"result":`+body+`}`), nil
			})
			m, _, _ := newTestManager(t, &fakeBackend{}, rt)
			identity, err := m.Verify(context.Background(), syntheticToken)
			if err == nil || identity != (BotIdentity{}) || (tc.want != nil && !errors.Is(err, tc.want)) || strings.Contains(err.Error(), syntheticToken) || strings.Contains(err.Error(), "https://") {
				t.Fatal("verification failure was accepted or leaked details", identity, err)
			}
			if calls.Load() < 1 || calls.Load() > 2 || m.Management().Revision != 1 {
				t.Fatal("verification changed manager or used extra endpoints")
			}
		})
	}
}

func TestManagerCASReplacementPreservesHistoryAndRestarts(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	rt := managerIdentityTransport(t, &calls)
	m, configPath, journalPath := newTestManager(t, &fakeBackend{}, rt)
	if _, err := m.Configure(ctx, 100, managerTestToken(100, ""), 1); err != nil {
		t.Fatal(err)
	}
	// Preserve both an ambiguous delivery and an exact captured money event.
	in := Incoming{UpdateID: 1, Identity: Identity{100, 2001, 2001}, Kind: "message", Text: "/start", ReceivedUnix: time.Now().Unix()}
	persistInput(t, m.worker.journal, in)
	if err := m.worker.journal.complete(ctx, in, testAccess(100, 2001), Result{Receipt: true, Screen: &Screen{Text: "durable receipt"}}); err != nil {
		t.Fatal(err)
	}
	delivery := nextQueued(t, m.worker.journal, 100)
	if err := m.worker.journal.claim(ctx, delivery); err != nil {
		t.Fatal(err)
	}
	payment := financialRetryInput("payment")
	persistInput(t, m.worker.journal, payment)
	var paymentBefore []byte
	if err := m.worker.journal.db.QueryRow("SELECT input_json FROM bot_inbox WHERE bot_id=100 AND update_id=51").Scan(&paymentBefore); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Configure(ctx, 200, managerTestToken(200, ""), 2); err != nil {
		t.Fatal(err)
	}
	before, _ := os.Stat(configPath)
	status, err := m.Configure(ctx, 100, managerTestToken(100, "replacement"), 3)
	if err != nil || status.Revision != 4 || len(status.Connections) != 2 {
		t.Fatal(status, err)
	}
	after, _ := os.Stat(configPath)
	if os.SameFile(before, after) || !privateRegular(after) || after.Mode().Perm() != 0600 {
		t.Fatal("credential replacement was not a private new inode")
	}
	var state string
	var paymentAfter []byte
	if err := m.worker.journal.db.QueryRow("SELECT state,input_json FROM bot_inbox WHERE bot_id=100 AND update_id=51").Scan(&state, &paymentAfter); err != nil || state != "pending" || !bytes.Equal(paymentBefore, paymentAfter) {
		t.Fatal("replacement lost or changed payment capture evidence", state, err)
	}
	if err := m.worker.journal.db.QueryRow("SELECT state FROM bot_outbox WHERE id=?", delivery.ID).Scan(&state); err != nil || state != "unknown" {
		t.Fatal("replacement requeued an uncertain delivery", state, err)
	}
	if cursor, err := m.worker.journal.cursor(ctx, 100); err != nil || cursor != 52 {
		t.Fatal("replacement reset poll cursor", cursor, err)
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, id := range []int64{201, 202} {
		group.Add(1)
		go func(id int64) {
			defer group.Done()
			_, err := m.Configure(ctx, id, managerTestToken(id, ""), 4)
			results <- err
		}(id)
	}
	group.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 || m.Management().Revision != 5 {
		t.Fatal("concurrent stale revision was accepted", success, conflict)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config, err := parseConfig(raw)
	if err != nil || len(config.Bots) != 3 || config.Bots[0].Token != managerTestToken(100, "replacement") || config.Bots[1].Token != managerTestToken(200, "") {
		t.Fatal("configuration discarded an independent bot", err)
	}
	// External edits, including valid JSON with the same revision, are not
	// silently overwritten by an already-open manager.
	external := append(append([]byte(nil), raw...), '\n')
	if err := os.WriteFile(configPath, external, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Configure(ctx, 100, "", 5); !errors.Is(err, ErrConflict) {
		t.Fatal("external file CAS conflict accepted", err)
	}
	current, _ := os.ReadFile(configPath)
	if !bytes.Equal(current, external) {
		t.Fatal("conflicting file was overwritten")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	journalRaw, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	statusRaw, _ := json.Marshal(m.Management())
	for _, bot := range config.Bots {
		if bytes.Contains(journalRaw, []byte(bot.Token)) || bytes.Contains(statusRaw, []byte(bot.Token)) {
			t.Fatal("credential reached journal or management report")
		}
	}
	if err := ValidateOfflineBackup(ctx, journalPath, current); err != nil {
		t.Fatal("managed history backup rejected", err)
	}
	reopened, err := openManager(configPath, journalPath, &fakeBackend{}, rt)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Management().Revision != 5 || len(reopened.Management().Connections) != 3 {
		t.Fatal("durable configuration did not survive restart")
	}
	if cursor, err := reopened.worker.journal.cursor(ctx, 100); err != nil || cursor != 52 {
		t.Fatal("restart lost financial cursor", cursor, err)
	}
	if identity, err := reopened.Check(ctx, 100); err != nil || identity.BotID != 100 {
		t.Fatal("saved credential check failed", identity, err)
	}
	entries, err := os.ReadDir(filepath.Dir(configPath))
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatal("credential publication left extra files", err)
	}
}

func TestManagedEmptyJournalRejectsLegacyAndCorruptStates(t *testing.T) {
	for _, tc := range []struct{ name, mutate string }{
		{"legacy_empty", "PRAGMA user_version=1"},
		{"wrong_binding", "UPDATE bot_meta SET binding='incorrect'"},
		{"wrong_metadata", "PRAGMA application_id=1"},
		{"orphan_row", "INSERT INTO bot_preferences(bot_id,user_id,language) VALUES(100,1,'en')"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, configPath, journalPath := newTestManager(t, &fakeBackend{}, nil)
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", journalPath)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(tc.mutate); err != nil {
				db.Close()
				t.Fatal(err)
			}
			db.Close()
			raw, _ := os.ReadFile(configPath)
			if err := ValidateOfflineBackup(context.Background(), journalPath, raw); err == nil {
				t.Fatal("corrupt/legacy empty backup accepted")
			}
			if other, err := openManager(configPath, journalPath, &fakeBackend{}, nil); err == nil || other != nil {
				if other != nil {
					other.Close()
				}
				t.Fatal("corrupt/legacy empty journal opened", err)
			}
		})
	}
	for _, raw := range []string{
		`{"version":1,"purpose":"` + ConfigPurpose + `","bots":[]}`,
		`{"version":2,"purpose":"` + ConfigPurpose + `","bots":[]}`,
		`{"version":2,"purpose":"` + ConfigPurpose + `","revision":0,"bots":[]}`,
		`{"version":2,"purpose":"` + ConfigPurpose + `","revision":1,"bots":null}`,
	} {
		if _, err := parseConfig([]byte(raw)); err == nil {
			t.Fatal("uninitialized empty configuration accepted")
		}
	}
}

func TestManagerLegacyMigrationAndExplicitEnableBoundary(t *testing.T) {
	for _, flag := range []string{"", "0"} {
		if m, err := OpenManager(Options{EnableFlag: flag, ConfigPath: "/does-not-exist"}, nil); err != nil || m != nil {
			t.Fatal("disabled manager accessed configuration", err)
		}
	}
	for _, options := range []Options{
		{EnableFlag: "1", LABMode: true},
		{EnableFlag: "1", LABMode: false, ConfigPath: LABConfigPath},
		{EnableFlag: "1", LABMode: true, ConfigPath: "/unreviewed/config.json"},
		{EnableFlag: "true", LABMode: true, ConfigPath: LABConfigPath},
	} {
		if m, err := OpenManager(options, &fakeBackend{}); err != ErrConfig || m != nil {
			t.Fatal("manager widened explicit LAB path/enable boundary", err)
		}
	}
	configPath, journalPath := managerTestPaths(t)
	if ensureManagedParent(configPath) != nil || ensureManagedParent(journalPath) != nil {
		t.Fatal("private test parents")
	}
	raw := checkConfigFixture(t, 2)
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	j, err := openJournal(journalPath, []int64{100, 101})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := j.db.Exec("UPDATE bot_cursors SET cursor=42 WHERE bot_id=100"); err != nil {
		j.Close()
		t.Fatal(err)
	}
	j.Close()
	var calls atomic.Int32
	m, err := openManager(configPath, journalPath, &fakeBackend{}, managerIdentityTransport(t, &calls))
	if err != nil {
		t.Fatal("valid legacy pair was rejected", err)
	}
	defer m.Close()
	if m.Management().Revision != 0 || len(m.Management().Connections) != 2 || calls.Load() != 0 {
		t.Fatal("legacy open performed network or lost credentials")
	}
	var version int
	if err := m.worker.journal.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 2 {
		t.Fatal("validated legacy journal was not marked managed", version, err)
	}
	if status, err := m.Configure(context.Background(), 100, "", 0); err != nil || status.Revision != 1 || len(status.Connections) != 2 {
		t.Fatal("legacy credential retry migration failed", status, err)
	}
	if cursor, err := m.worker.journal.cursor(context.Background(), 100); err != nil || cursor != 42 {
		t.Fatal("migration reset a legacy cursor", cursor, err)
	}
	raw, _ = os.ReadFile(configPath)
	config, err := parseConfig(raw)
	if err != nil || config.Version != 2 || config.Revision != 1 || len(config.Bots) != 2 || config.Bots[1].BotID != 101 {
		t.Fatal("migration did not preserve another legacy bot", err)
	}
	m.Close()
	if err := ValidateOfflineBackup(context.Background(), journalPath, raw); err != nil {
		t.Fatal("migrated pair is not a valid offline backup", err)
	}
}
