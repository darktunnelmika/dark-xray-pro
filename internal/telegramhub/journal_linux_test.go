//go:build linux

package telegramhub

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func journalTestBinding() botBinding {
	return botBinding{BotID: 123456, OwnerUserID: 900001, OwnerChatID: 900001, BotUsername: "DarkLabHubBot"}
}

func journalTestUpdate(id int64) receivedUpdate {
	m := ownerMessage()
	m.UpdateID = id
	sum := sha256.Sum256([]byte(fmt.Sprintf("synthetic Telegram update %d", id)))
	return receivedUpdate{ID: id, Fingerprint: hex.EncodeToString(sum[:]), Message: &m}
}

func journalTestReply(entry inboxEntry) PreparedOwnerReply {
	return PreparedOwnerReply{OwnerUserID: 900001, ChatID: 900001, UpdateID: entry.UpdateID,
		ReplyToMessageID: entry.Message.MessageID, RepresentativeID: "repA", ParseMode: "HTML", Text: "<b>synthetic report</b>"}
}

func journalPrivateTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newTestInboxJournal(t *testing.T) (*inboxJournal, string) {
	t.Helper()
	path := filepath.Join(journalPrivateTestDir(t), "owner-reports.sqlite")
	j, err := openInboxJournal(path, journalTestBinding())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j, path
}

func TestBotJournalDurableQueueCursorAndOutcomes(t *testing.T) {
	ctx := context.Background()
	j, path := newTestInboxJournal(t)
	updates := []receivedUpdate{journalTestUpdate(103), journalTestUpdate(101), journalTestUpdate(102)}
	updates[2].Message = nil
	updates[2].Rejection = "unsupported_origin"
	if err := j.Ingest(ctx, updates); err != nil {
		t.Fatal(err)
	}
	if cursor, err := j.Cursor(ctx); err != nil || cursor != 104 {
		t.Fatalf("cursor=%d err=%v", cursor, err)
	}
	entry, found, err := j.Next(ctx)
	if err != nil || !found || entry.UpdateID != 101 {
		t.Fatalf("next=%+v found=%v err=%v", entry, found, err)
	}
	if err := j.Finish(ctx, 101, "sent", "", 777); !errors.Is(err, ErrBotJournalConflict) {
		t.Fatalf("pending was counted as sent: %v", err)
	}
	if err := j.BeginDispatch(ctx, entry, journalTestReply(entry)); err != nil {
		t.Fatal(err)
	}
	if outcome, err := j.Outcome(ctx, 101); err != nil || outcome.State != "dispatching" {
		t.Fatalf("claim not durable: %+v %v", outcome, err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = openInboxJournal(path, journalTestBinding())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if outcome, err := j.Outcome(ctx, 101); err != nil || outcome != (dispatchOutcome{State: "unknown", Reason: "restart_during_dispatch"}) {
		t.Fatalf("ambiguous claim was lost: %+v %v", outcome, err)
	}
	if err := j.Ingest(ctx, updates); err != nil {
		t.Fatalf("duplicate poll batch: %v", err)
	}
	entry, found, err = j.Next(ctx)
	if err != nil || !found || entry.UpdateID != 103 {
		t.Fatalf("replayed a nonpending entry: %+v %v %v", entry, found, err)
	}
	if err := j.BeginDispatch(ctx, entry, journalTestReply(entry)); err != nil {
		t.Fatal(err)
	}
	if err := j.Finish(ctx, 103, "sent", "", 778); err != nil {
		t.Fatal(err)
	}
	if err := j.Finish(ctx, 103, "sent", "", 778); err != nil {
		t.Fatalf("identical durable completion should be idempotent: %v", err)
	}
	for _, id := range []int64{101, 103} {
		if err := j.Finish(ctx, id, "failed", "http_429", 0); !errors.Is(err, ErrBotJournalConflict) {
			t.Fatalf("terminal outcome overwritten: %v", err)
		}
		if err := j.Reject(ctx, id, "invalid_command"); !errors.Is(err, ErrBotJournalConflict) {
			t.Fatalf("terminal outcome rejected: %v", err)
		}
	}
	if outcome, err := j.Outcome(ctx, 102); err != nil || outcome.State != "rejected" || outcome.Reason != "unsupported_origin" {
		t.Fatalf("rejection missing: %+v %v", outcome, err)
	}
	if _, found, err = j.Next(ctx); err != nil || found {
		t.Fatalf("queue not drained: found=%v err=%v", found, err)
	}
	if cursor, err := j.Cursor(ctx); err != nil || cursor != 104 {
		t.Fatalf("replay moved cursor: %d %v", cursor, err)
	}
}

func TestBotJournalBindingAndExclusiveLock(t *testing.T) {
	ctx := context.Background()
	j, path := newTestInboxJournal(t)
	if other, err := openInboxJournal(path, journalTestBinding()); !errors.Is(err, ErrBotJournalConflict) || other != nil {
		if other != nil {
			other.Close()
		}
		t.Fatalf("second worker admitted: %v", err)
	}
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(1)}); err != nil {
		t.Fatal(err)
	}
	entry, _, _ := j.Next(ctx)
	if err := j.BeginDispatch(ctx, entry, journalTestReply(entry)); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*botBinding){
		func(b *botBinding) { b.BotID++ },
		func(b *botBinding) { b.OwnerUserID++ },
		func(b *botBinding) { b.OwnerChatID++ },
		func(b *botBinding) { b.BotUsername = "OtherLabBot" },
	} {
		binding := journalTestBinding()
		change(&binding)
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if other, err := openInboxJournal(path, binding); !errors.Is(err, ErrBotJournalBinding) || other != nil {
			if other != nil {
				other.Close()
			}
			t.Fatalf("different binding admitted: %v", err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(before) != string(after) {
			t.Fatal("binding rejection mutated journal")
		}
	}
	binding := journalTestBinding()
	binding.BotUsername = strings.ToLower(binding.BotUsername)
	j, err := openInboxJournal(path, binding)
	if err != nil {
		t.Fatalf("username case changed authority: %v", err)
	}
	defer j.Close()
	if outcome, err := j.Outcome(ctx, 1); err != nil || outcome.State != "unknown" {
		t.Fatalf("correctly bound restart did not recover: %+v %v", outcome, err)
	}
}

func TestBotJournalAtomicDuplicatesAndMonotoneCursor(t *testing.T) {
	ctx := context.Background()
	j, _ := newTestInboxJournal(t)
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(10)}); err != nil {
		t.Fatal(err)
	}
	conflict := journalTestUpdate(10)
	conflict.Fingerprint = strings.Repeat("f", 64)
	for _, batch := range [][]receivedUpdate{
		{journalTestUpdate(11), conflict},
		{journalTestUpdate(11), journalTestUpdate(9)},
	} {
		if err := j.Ingest(ctx, batch); !errors.Is(err, ErrBotJournalConflict) {
			t.Fatalf("conflicting batch: %v", err)
		}
		if cursor, err := j.Cursor(ctx); err != nil || cursor != 11 {
			t.Fatalf("failed batch moved cursor: %d %v", cursor, err)
		}
		if _, err := j.Outcome(ctx, 11); !errors.Is(err, ErrBotJournal) {
			t.Fatal("failed batch inserted partial history")
		}
	}
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(10), journalTestUpdate(12), journalTestUpdate(12)}); err != nil {
		t.Fatal(err)
	}
	if cursor, err := j.Cursor(ctx); err != nil || cursor != 13 {
		t.Fatalf("new cursor: %d %v", cursor, err)
	}
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(math.MaxInt64)}); !errors.Is(err, ErrBotJournal) {
		t.Fatalf("overflow accepted: %v", err)
	}
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(math.MaxInt64 - 1)}); err != nil {
		t.Fatal(err)
	}
	if cursor, err := j.Cursor(ctx); err != nil || cursor != math.MaxInt64 {
		t.Fatalf("largest safe cursor: %d %v", cursor, err)
	}
}

func TestBotJournalDispatchRevalidatesScopeAndStoredInput(t *testing.T) {
	ctx := context.Background()
	j, _ := newTestInboxJournal(t)
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(1)}); err != nil {
		t.Fatal(err)
	}
	entry, _, _ := j.Next(ctx)
	for _, mutate := range []func(*inboxEntry, *PreparedOwnerReply){
		func(e *inboxEntry, r *PreparedOwnerReply) { r.OwnerUserID++ },
		func(e *inboxEntry, r *PreparedOwnerReply) { r.ChatID++ },
		func(e *inboxEntry, r *PreparedOwnerReply) { r.UpdateID++ },
		func(e *inboxEntry, r *PreparedOwnerReply) { r.ReplyToMessageID++ },
		func(e *inboxEntry, r *PreparedOwnerReply) { r.ParseMode = "MarkdownV2" },
		func(e *inboxEntry, r *PreparedOwnerReply) { r.Text = "" },
		func(e *inboxEntry, r *PreparedOwnerReply) { r.RepresentativeID = "owner" },
		func(e *inboxEntry, r *PreparedOwnerReply) { e.Message.SenderID++ },
		func(e *inboxEntry, r *PreparedOwnerReply) { e.Message.ChatType = "group" },
		func(e *inboxEntry, r *PreparedOwnerReply) { e.Message.Forwarded = true },
		func(e *inboxEntry, r *PreparedOwnerReply) { e.Message.Edited = true },
		func(e *inboxEntry, r *PreparedOwnerReply) { e.Message.ViaBot = true },
		func(e *inboxEntry, r *PreparedOwnerReply) { e.Message.SenderIsBot = true },
		func(e *inboxEntry, r *PreparedOwnerReply) { e.Message.SenderChatID = 123 },
		func(e *inboxEntry, r *PreparedOwnerReply) { e.Message.Text = "/rep_credit repB" },
	} {
		copy := entry
		reply := journalTestReply(copy)
		mutate(&copy, &reply)
		if err := j.BeginDispatch(ctx, copy, reply); err == nil {
			t.Fatalf("changed scope or input was dispatched: %+v %+v", copy, reply)
		}
		if outcome, err := j.Outcome(ctx, 1); err != nil || outcome.State != "pending" {
			t.Fatalf("invalid claim consumed input: %+v %v", outcome, err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- j.BeginDispatch(ctx, entry, journalTestReply(entry))
		}()
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, ErrBotJournalConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("claim race: successful=%d conflicts=%d", success, conflict)
	}
}

func TestBotJournalRejectAndFailurePreservedAcrossRestart(t *testing.T) {
	ctx := context.Background()
	j, path := newTestInboxJournal(t)
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(1), journalTestUpdate(2)}); err != nil {
		t.Fatal(err)
	}
	if err := j.Reject(ctx, 1, "invalid_command"); err != nil {
		t.Fatal(err)
	}
	if err := j.Reject(ctx, 1, "invalid_command"); err != nil {
		t.Fatal(err)
	}
	entry, _, _ := j.Next(ctx)
	if err := j.BeginDispatch(ctx, entry, journalTestReply(entry)); err != nil {
		t.Fatal(err)
	}
	if err := j.Finish(ctx, 2, "failed", "http_429", 0); err != nil {
		t.Fatal(err)
	}
	j.Close()
	j, err := openInboxJournal(path, journalTestBinding())
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if _, found, err := j.Next(ctx); err != nil || found {
		t.Fatal("terminal failure was scheduled again")
	}
	if outcome, err := j.Outcome(ctx, 2); err != nil || outcome != (dispatchOutcome{State: "failed", Reason: "http_429"}) {
		t.Fatalf("failure lost: %+v %v", outcome, err)
	}
}

func TestBotJournalBoundsAndRollback(t *testing.T) {
	ctx := context.Background()
	j, _ := newTestInboxJournal(t)
	oversize := make([]receivedUpdate, botJournalBatchLimit+1)
	if err := j.Ingest(ctx, oversize); !errors.Is(err, ErrBotJournal) {
		t.Fatalf("oversize batch accepted: %v", err)
	}
	for offset := 0; offset < botJournalPendingLimit; offset += botJournalBatchLimit {
		batch := make([]receivedUpdate, botJournalBatchLimit)
		for i := range batch {
			batch[i] = journalTestUpdate(int64(offset + i + 1))
		}
		if err := j.Ingest(ctx, batch); err != nil {
			t.Fatal(err)
		}
	}
	rejected := journalTestUpdate(1001)
	rejected.Message = nil
	rejected.Rejection = "unsupported_update"
	if err := j.Ingest(ctx, []receivedUpdate{rejected, journalTestUpdate(1002)}); !errors.Is(err, ErrBotJournalFull) {
		t.Fatalf("pending cap failed: %v", err)
	}
	if cursor, err := j.Cursor(ctx); err != nil || cursor != 1001 {
		t.Fatalf("capacity failure acknowledged a partial batch: %d %v", cursor, err)
	}
	if err := j.Ingest(ctx, []receivedUpdate{rejected}); err != nil {
		t.Fatalf("full pending queue should still journal bounded rejection: %v", err)
	}
	if err := j.Reject(ctx, 1, "invalid_command"); err != nil {
		t.Fatal(err)
	}
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(1002)}); err != nil {
		t.Fatalf("pending queue capacity not released by rejection: %v", err)
	}
}

func TestBotJournalHistoryCapacityIsFailClosed(t *testing.T) {
	ctx := context.Background()
	j, _ := newTestInboxJournal(t)
	// Populate only this disposable journal with terminal history to exercise
	// the real production bound without sending 100,000 external messages.
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `WITH RECURSIVE ids(n) AS (VALUES(1) UNION ALL SELECT n+1 FROM ids WHERE n < ?) INSERT INTO owner_bot_inbox(update_id,fingerprint,state,reason) SELECT n,?,'rejected','unsupported_update' FROM ids`, botJournalHistoryLimit, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE owner_bot_meta SET cursor=?`, botJournalHistoryLimit+1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(botJournalHistoryLimit + 1)}); !errors.Is(err, ErrBotJournalFull) {
		t.Fatalf("history cap failed: %v", err)
	}
	if cursor, err := j.Cursor(ctx); err != nil || cursor != botJournalHistoryLimit+1 {
		t.Fatalf("full history moved cursor: %d %v", cursor, err)
	}
}

func TestBotJournalRejectsUnsafeFilesAndSchemas(t *testing.T) {
	for _, scenario := range []string{"public_parent", "public_file", "symlink_file", "symlink_parent", "hardlink_file", "lock_symlink", "journal_symlink", "fifo", "existing_empty", "unrelated_schema", "partial_schema", "extra_trigger", "wrong_version", "wrong_cursor"} {
		t.Run(scenario, func(t *testing.T) {
			dir := journalPrivateTestDir(t)
			path := filepath.Join(dir, "owner-reports.sqlite")
			mustWrite := func(name string, raw []byte, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(name, raw, mode); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "public_parent":
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			case "public_file":
				mustWrite(path, []byte("private data must not leak"), 0644)
			case "existing_empty":
				mustWrite(path, nil, 0600)
			case "fifo":
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink_file", "hardlink_file", "lock_symlink", "journal_symlink":
				target := filepath.Join(dir, "untouched")
				mustWrite(target, []byte("untouched"), 0600)
				name := path
				if scenario == "lock_symlink" {
					name += ".lock"
				} else if scenario == "journal_symlink" {
					name += "-journal"
				}
				if scenario == "hardlink_file" {
					if err := os.Link(target, name); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(target, name); err != nil {
					t.Fatal(err)
				}
			case "symlink_parent":
				link := filepath.Join(t.TempDir(), "linked-parent")
				if err := os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(link, "owner-reports.sqlite")
			default:
				if scenario == "extra_trigger" || scenario == "wrong_version" || scenario == "wrong_cursor" {
					j, err := openInboxJournal(path, journalTestBinding())
					if err != nil {
						t.Fatal(err)
					}
					j.Close()
				} else {
					mustWrite(path, nil, 0600)
				}
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				statements := map[string]string{
					"unrelated_schema": `CREATE TABLE wallet (balance INTEGER)`,
					"partial_schema":   botJournalMetaDDL,
					"extra_trigger":    `CREATE TRIGGER unexpected AFTER INSERT ON owner_bot_inbox BEGIN UPDATE owner_bot_meta SET cursor=0; END`,
					"wrong_version":    `PRAGMA user_version=2`,
					"wrong_cursor":     `UPDATE owner_bot_meta SET cursor=1000`,
				}
				_, err = db.Exec(statements[scenario])
				db.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			var before, after []byte
			if scenario != "fifo" {
				before, _ = os.ReadFile(path)
			}
			j, err := openInboxJournal(path, journalTestBinding())
			if j != nil {
				j.Close()
			}
			if !errors.Is(err, ErrBotJournal) {
				t.Fatalf("unsafe journal admitted: %v", err)
			}
			if scenario != "fifo" {
				after, _ = os.ReadFile(path)
			}
			if string(before) != string(after) {
				t.Fatal("rejected journal contents changed")
			}
		})
	}
}

func TestBotJournalCancellationAndStaticErrors(t *testing.T) {
	j, _ := newTestInboxJournal(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := j.Ingest(ctx, []receivedUpdate{journalTestUpdate(1)}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ingestion: %v", err)
	}
	if cursor, err := j.Cursor(context.Background()); err != nil || cursor != 0 {
		t.Fatal("canceled ingestion moved cursor")
	}
	malformed := journalTestUpdate(1)
	malformed.Fingerprint = "synthetic-token:never-log-this"
	if err := j.Ingest(context.Background(), []receivedUpdate{malformed}); err == nil || strings.Contains(err.Error(), "synthetic-token") {
		t.Fatalf("unsafe fingerprint error: %v", err)
	}
	for _, raw := range []string{"https://api.telegram.org/botsecret/sendMessage", "token:secret", "untrusted error\nline"} {
		if err := j.Reject(context.Background(), 1, raw); !errors.Is(err, ErrBotJournal) || strings.Contains(err.Error(), raw) {
			t.Fatalf("unsafe error reason accepted: %v", err)
		}
	}
	j.Close()
	if _, _, err := j.Next(context.Background()); !errors.Is(err, ErrBotJournal) {
		t.Fatalf("closed journal: %v", err)
	}
}
