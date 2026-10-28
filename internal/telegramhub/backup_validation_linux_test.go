//go:build linux

package telegramhub

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func offlineBotConfig() []byte { return []byte(ownerBotJSON(ownerBotFileConfigFixture())) }

func offlineBotFixture(t *testing.T) string {
	t.Helper()
	j, source := newTestInboxJournal(t)
	ctx := context.Background()
	updates := make([]receivedUpdate, 7)
	for i := range updates {
		updates[i] = journalTestUpdate(int64(i + 1))
	}
	updates[5].Message, updates[5].Rejection = nil, "unsupported_origin"
	// Auth rejection is deferred until processing; retaining this pending
	// foreign-origin update must not turn a valid backup into a false failure.
	updates[6].Message.SenderID++
	if err := j.Ingest(ctx, updates); err != nil {
		t.Fatal(err)
	}
	for id := int64(2); id <= 5; id++ {
		entry := inboxEntry{UpdateID: id, Message: *updates[id-1].Message}
		if err := j.BeginDispatch(ctx, entry, journalTestReply(entry)); err != nil {
			t.Fatal(err)
		}
		if id == 2 {
			continue
		}
		state, reason, sent := "sent", "", int64(777)
		if id == 4 {
			state, reason, sent = "failed", "http_400", 0
		}
		if id == 5 {
			state, reason, sent = "unknown", "request_uncertain", 0
		}
		if err := j.Finish(ctx, id, state, reason, sent); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	// An archive contains only the standalone database, never its live lock.
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(journalPrivateTestDir(t), "snapshot.sqlite")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func mutateOfflineBot(t *testing.T, path, query string, args ...any) {
	t.Helper()
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineBotValidationPreservesAllDispatchOutcomes(t *testing.T) {
	path := offlineBotFixture(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := ValidateOfflineBackup(context.Background(), path, offlineBotConfig(), []byte(fakeOwnerBotToken+"\n")); err != nil {
			t.Fatal(err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("delivery state was mutated")
	}
	current, err := os.Stat(path)
	if err != nil || !info.ModTime().Equal(current.ModTime()) || info.Mode() != current.Mode() {
		t.Fatal("snapshot metadata changed")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(path) {
		t.Fatal("validator created a sidecar or writer lock")
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var state string
	if db.QueryRow("SELECT state FROM owner_bot_inbox WHERE update_id=2").Scan(&state) != nil || state != "dispatching" {
		t.Fatal("read-only validation performed startup recovery")
	}
}

func TestOfflineBotValidationRequiresSavedBindingAndExactSchema(t *testing.T) {
	for name, query := range map[string]string{
		"wrong-version":         "PRAGMA user_version=2",
		"wrong-app":             "PRAGMA application_id=0",
		"missing-index":         "DROP INDEX owner_bot_inbox_state",
		"unknown-table":         "CREATE TABLE foreign_secret(value TEXT)",
		"similar-system-prefix": "CREATE TABLE sqliteXextra(value TEXT)",
		"unknown-trigger":       "CREATE TRIGGER foreign_trigger AFTER INSERT ON owner_bot_inbox BEGIN SELECT 1; END",
		"wrong-binding":         "UPDATE owner_bot_meta SET binding_digest='aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa'",
		"wrong-cursor":          "UPDATE owner_bot_meta SET cursor=99",
		"malformed-pending":     "UPDATE owner_bot_inbox SET message_json='{' WHERE update_id=1",
		"bad-fingerprint":       "UPDATE owner_bot_inbox SET fingerprint='ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ' WHERE update_id=1",
		"private-reason":        "UPDATE owner_bot_inbox SET reason='DO NOT ECHO SECRET' WHERE update_id=4",
	} {
		t.Run(name, func(t *testing.T) {
			path := offlineBotFixture(t)
			mutateOfflineBot(t, path, query)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			err = ValidateOfflineBackup(context.Background(), path, offlineBotConfig(), []byte(fakeOwnerBotToken))
			if err != ErrBotJournal && err != ErrBotJournalBinding {
				t.Fatal("invalid journal accepted", err)
			}
			if strings.Contains(err.Error(), "DO NOT ECHO SECRET") {
				t.Fatal("private reason leaked")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("invalid snapshot was repaired")
			}
		})
	}
	path := offlineBotFixture(t)
	changed := ownerBotFileConfigFixture()
	changed["owner_user_id"] = int64(900002)
	if err := ValidateOfflineBackup(context.Background(), path, []byte(ownerBotJSON(changed)), []byte(fakeOwnerBotToken)); err != ErrBotJournalBinding {
		t.Fatal("changed authority accepted", err)
	}
	entry := inboxEntry{UpdateID: 3, Message: *journalTestUpdate(3).Message}
	reply := journalTestReply(entry)
	reply.ChatID++
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	mutateOfflineBot(t, path, "UPDATE owner_bot_inbox SET reply_json=? WHERE update_id=3", raw)
	if err := ValidateOfflineBackup(context.Background(), path, offlineBotConfig(), []byte(fakeOwnerBotToken)); err != ErrBotJournal {
		t.Fatal("historical dispatch to another chat accepted", err)
	}
}

func TestOfflineBotConfigOnlyAndInvalidCredentialsNeverOpenOriginalPaths(t *testing.T) {
	if err := ValidateOfflineBackup(context.Background(), "", offlineBotConfig(), []byte(fakeOwnerBotToken)); err != nil {
		t.Fatal("configured bot without a journal rejected", err)
	}
	for _, token := range [][]byte{nil, []byte("secret"), []byte(fakeOwnerBotToken + "\n\n"), []byte(strings.Repeat("x", 257))} {
		if err := ValidateOfflineBackup(context.Background(), "/not-opened", offlineBotConfig(), token); err != ErrBotLABConfig {
			t.Fatal("invalid token syntax accepted", err)
		}
	}
	for _, raw := range [][]byte{nil, []byte("SECRET CONFIG"), append(offlineBotConfig(), []byte(` {}`)...)} {
		if err := ValidateOfflineBackup(context.Background(), "/not-opened", raw, []byte(fakeOwnerBotToken)); err != ErrBotLABConfig {
			t.Fatal("invalid config accepted", err)
		}
	}
}

func TestOfflineBotRejectsUnsafePathsAndSidecars(t *testing.T) {
	for _, kind := range []string{"missing", "empty", "fifo", "file-symlink", "parent-symlink", "hardlink", "public-file", "public-parent", "corrupt", "-wal", "-shm", "-journal"} {
		t.Run(kind, func(t *testing.T) {
			path := offlineBotFixture(t)
			switch kind {
			case "missing":
				path += ".missing"
			case "empty":
				if err := os.Truncate(path, 0); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				path += ".fifo"
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "file-symlink":
				link := path + ".link"
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			case "parent-symlink":
				link := filepath.Join(journalPrivateTestDir(t), "alias")
				if err := os.Symlink(filepath.Dir(path), link); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(link, filepath.Base(path))
			case "hardlink":
				if err := os.Link(path, path+".link"); err != nil {
					t.Fatal(err)
				}
			case "public-file":
				if err := os.Chmod(path, 0644); err != nil {
					t.Fatal(err)
				}
			case "public-parent":
				if err := os.Chmod(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(path, []byte("SECRET CORRUPT SQLITE"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				if err := os.WriteFile(path+kind, []byte("uncheckpointed"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := ValidateOfflineBackup(context.Background(), path, offlineBotConfig(), []byte(fakeOwnerBotToken)); err != ErrBotJournal {
				t.Fatal("unsafe journal accepted", err)
			}
			if kind == "missing" {
				if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("missing database was created")
				}
				if _, err := os.Lstat(path + ".lock"); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("writer lock was created")
				}
			}
		})
	}
}

func TestOfflineBotCancellationAndStaticErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateOfflineBackup(ctx, "/not-opened", nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(nil, "/not-opened", nil, nil); err != ErrBotJournal {
		t.Fatal(err)
	}
	secret := "/private/DO_NOT_ECHO_ROW_OR_PATH.sqlite"
	err := ValidateOfflineBackup(context.Background(), secret, offlineBotConfig(), []byte(fakeOwnerBotToken))
	if err != ErrBotJournal || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), fakeOwnerBotToken) {
		t.Fatal("raw error exposed", err)
	}
}
