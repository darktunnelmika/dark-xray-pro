//go:build linux

package telegramhub

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

var (
	ErrBotJournal         = errors.New("owner bot journal unavailable")
	ErrBotJournalBinding  = errors.New("owner bot journal binding mismatch")
	ErrBotJournalConflict = errors.New("owner bot journal conflicts with durable history")
	ErrBotJournalFull     = errors.New("owner bot journal capacity reached")
)

const (
	botJournalVersion       = 1
	botJournalApplicationID = 0x44505242
	botJournalBatchLimit    = 25
	botJournalPendingLimit  = 1000
	botJournalHistoryLimit  = 100000
	botJournalMessageLimit  = 2048
	botJournalReplyLimit    = 32768
)

type botBinding struct {
	BotID, OwnerUserID, OwnerChatID int64
	BotUsername                     string
}

func (b botBinding) valid() bool {
	return b.BotID > 0 && b.OwnerUserID > 0 && b.OwnerChatID > 0 &&
		(b.BotUsername == "" || botUsernamePattern.MatchString(b.BotUsername))
}

func (b botBinding) digest() string {
	// Numeric IDs and the restricted username alphabet make this encoding
	// unambiguous. Username case cannot change the authority or journal binding.
	raw := fmt.Sprintf("owner-report-bot-v1\n%d\n%d\n%d\n%s", b.BotID, b.OwnerUserID, b.OwnerChatID, strings.ToLower(b.BotUsername))
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type receivedUpdate struct {
	ID          int64
	Fingerprint string
	Message     *OwnerReportMessage
	Rejection   string
}

type inboxEntry struct {
	UpdateID int64
	Message  OwnerReportMessage
}

type dispatchOutcome struct {
	State     string
	Reason    string
	MessageID int64
}

type inboxJournal struct {
	mu      sync.Mutex
	db      *sql.DB
	lock    *os.File
	binding botBinding
}

const botJournalMetaDDL = `CREATE TABLE owner_bot_meta (
singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
schema_version INTEGER NOT NULL CHECK (schema_version = 1),
binding_digest TEXT NOT NULL CHECK (length(binding_digest) = 64),
cursor INTEGER NOT NULL CHECK (cursor >= 0)
)`

const botJournalInboxDDL = `CREATE TABLE owner_bot_inbox (
update_id INTEGER PRIMARY KEY CHECK (update_id >= 0 AND update_id < 9223372036854775807),
fingerprint TEXT NOT NULL CHECK (length(fingerprint) = 64),
state TEXT NOT NULL CHECK (state IN ('pending','dispatching','sent','failed','unknown','rejected')),
message_json BLOB,
reply_json BLOB,
reason TEXT NOT NULL DEFAULT '',
sent_message_id INTEGER NOT NULL DEFAULT 0 CHECK (sent_message_id >= 0),
CHECK (message_json IS NULL OR length(message_json) <= 2048),
CHECK (reply_json IS NULL OR length(reply_json) <= 32768),
CHECK (length(reason) <= 64),
CHECK (
 (state = 'pending' AND message_json IS NOT NULL AND reply_json IS NULL AND reason = '' AND sent_message_id = 0) OR
 (state = 'dispatching' AND message_json IS NOT NULL AND reply_json IS NOT NULL AND reason = '' AND sent_message_id = 0) OR
 (state = 'sent' AND message_json IS NOT NULL AND reply_json IS NOT NULL AND reason = '' AND sent_message_id > 0) OR
 (state IN ('failed','unknown') AND message_json IS NOT NULL AND reply_json IS NOT NULL AND reason <> '' AND sent_message_id = 0) OR
 (state = 'rejected' AND reply_json IS NULL AND reason <> '' AND sent_message_id = 0)
)
)`

const botJournalIndexDDL = `CREATE INDEX owner_bot_inbox_state ON owner_bot_inbox (state, update_id)`

// openInboxJournal opens only a dedicated, private inbox/outcome database. The
// parent directory must already exist; it never creates or opens a wallet.
// A second process cannot share the journal. Changing any authority binding
// requires a deliberately separate journal and operator review of old outcomes.
func openInboxJournal(path string, binding botBinding) (*inboxJournal, error) {
	if !binding.valid() || !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." {
		return nil, ErrBotJournal
	}
	if err := checkBotJournalParent(filepath.Dir(path)); err != nil {
		return nil, err
	}
	lock, _, err := openBotJournalFile(path+".lock", false)
	if err != nil {
		return nil, err
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		lock.Close()
		return nil, ErrBotJournalConflict
	}
	j := &inboxJournal{lock: lock, binding: binding}
	ok := false
	defer func() {
		if !ok {
			_ = j.Close()
		}
	}()
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		info, statErr := os.Lstat(path + suffix)
		if errors.Is(statErr, os.ErrNotExist) {
			continue
		}
		if statErr != nil || !validBotJournalFile(info) || suffix != "-journal" {
			return nil, ErrBotJournal
		}
	}
	file, created, err := openBotJournalFile(path, true)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	fileInfo, err := file.Stat()
	if err != nil || (!created && fileInfo.Size() == 0) {
		return nil, ErrBotJournal
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=rw"
	j.db, err = sql.Open("sqlite", uri)
	if err != nil {
		return nil, ErrBotJournal
	}
	j.db.SetMaxOpenConns(1)
	j.db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// These are connection settings, not schema changes. Existing schema and
	// identity are checked before any recovery state transition is committed.
	for _, pragma := range []string{"PRAGMA busy_timeout = 1000", "PRAGMA trusted_schema = OFF", "PRAGMA synchronous = FULL", "PRAGMA foreign_keys = ON"} {
		if _, err = j.db.ExecContext(ctx, pragma); err != nil {
			return nil, ErrBotJournal
		}
	}
	if created {
		err = j.initialize(ctx)
	} else {
		err = j.validate(ctx)
	}
	if err != nil {
		return nil, err
	}
	var mode string
	if err = j.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "delete" {
		return nil, ErrBotJournal
	}
	current, err := os.Lstat(path)
	if err != nil || !validBotJournalFile(current) || !os.SameFile(fileInfo, current) {
		return nil, ErrBotJournal
	}
	// A committed dispatch claim means a request may have reached Telegram.
	// Never put it back into the pending queue after a process restart.
	if _, err = j.db.ExecContext(ctx, `UPDATE owner_bot_inbox SET state='unknown', reason='restart_during_dispatch' WHERE state='dispatching'`); err != nil {
		return nil, ErrBotJournal
	}
	if created {
		if err = file.Sync(); err != nil {
			return nil, ErrBotJournal
		}
		dir, openErr := os.Open(filepath.Dir(path))
		if openErr != nil {
			return nil, ErrBotJournal
		}
		syncErr := dir.Sync()
		closeErr := dir.Close()
		if syncErr != nil || closeErr != nil {
			return nil, ErrBotJournal
		}
	}
	ok = true
	return j, nil
}

func checkBotJournalParent(dir string) error {
	for p := dir; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrBotJournal
		}
		if p == dir {
			st, valid := info.Sys().(*syscall.Stat_t)
			if !valid || int(st.Uid) != os.Geteuid() || info.Mode().Perm()&0077 != 0 {
				return ErrBotJournal
			}
		}
		if p == "/" {
			return nil
		}
	}
}

func validBotJournalFile(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid() && st.Nlink == 1
}

func openBotJournalFile(path string, exclusiveCreate bool) (*os.File, bool, error) {
	flags := syscall.O_RDWR | syscall.O_CLOEXEC | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	created := false
	fd, err := syscall.Open(path, flags|syscall.O_CREAT|syscall.O_EXCL, 0600)
	if err == nil {
		created = true
	} else if errors.Is(err, syscall.EEXIST) {
		fd, err = syscall.Open(path, flags, 0)
	}
	if err != nil {
		return nil, false, ErrBotJournal
	}
	f := os.NewFile(uintptr(fd), path)
	info, err := f.Stat()
	if err != nil || !validBotJournalFile(info) || (!exclusiveCreate && info.Size() != 0) {
		f.Close()
		return nil, false, ErrBotJournal
	}
	return f, created, nil
}

func (j *inboxJournal) initialize(ctx context.Context) error {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrBotJournal
	}
	defer tx.Rollback()
	for _, ddl := range []string{botJournalMetaDDL, botJournalInboxDDL, botJournalIndexDDL, "PRAGMA user_version = 1", "PRAGMA application_id = 1146114626"} {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return ErrBotJournal
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO owner_bot_meta(singleton,schema_version,binding_digest,cursor) VALUES(1,1,?,0)`, j.binding.digest()); err != nil {
		return ErrBotJournal
	}
	if tx.Commit() != nil {
		return ErrBotJournal
	}
	return nil
}

func (j *inboxJournal) validate(ctx context.Context) error {
	var version, applicationID int
	if j.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version) != nil || version != botJournalVersion ||
		j.db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&applicationID) != nil || applicationID != botJournalApplicationID {
		return ErrBotJournal
	}
	rows, err := j.db.QueryContext(ctx, `SELECT name,sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return ErrBotJournal
	}
	want := map[string]string{"owner_bot_meta": botJournalMetaDDL, "owner_bot_inbox": botJournalInboxDDL, "owner_bot_inbox_state": botJournalIndexDDL}
	for rows.Next() {
		var name, ddl string
		if rows.Scan(&name, &ddl) != nil || want[name] == "" || strings.TrimSpace(ddl) != want[name] {
			rows.Close()
			return ErrBotJournal
		}
		delete(want, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil || len(want) != 0 {
		return ErrBotJournal
	}
	var binding string
	var cursor int64
	var count, schema int
	if j.db.QueryRowContext(ctx, `SELECT count(*) FROM owner_bot_meta`).Scan(&count) != nil || count != 1 ||
		j.db.QueryRowContext(ctx, `SELECT schema_version,binding_digest,cursor FROM owner_bot_meta WHERE singleton=1`).Scan(&schema, &binding, &cursor) != nil || schema != botJournalVersion || cursor < 0 {
		return ErrBotJournal
	}
	if binding != j.binding.digest() {
		return ErrBotJournalBinding
	}
	var maxID sql.NullInt64
	var pending int
	if j.db.QueryRowContext(ctx, `SELECT count(*),max(update_id),coalesce(sum(state IN ('pending','dispatching')),0) FROM owner_bot_inbox`).Scan(&count, &maxID, &pending) != nil ||
		count > botJournalHistoryLimit || pending > botJournalPendingLimit ||
		(maxID.Valid && (maxID.Int64 < 0 || maxID.Int64 == math.MaxInt64 || maxID.Int64+1 != cursor)) || (!maxID.Valid && cursor != 0) {
		return ErrBotJournal
	}
	return nil
}

func botJournalError(ctx context.Context) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return ErrBotJournal
}

func (j *inboxJournal) Close() error {
	if j == nil {
		return nil
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var err error
	if j.db != nil {
		err = j.db.Close()
		j.db = nil
	}
	if j.lock != nil {
		if syscall.Flock(int(j.lock.Fd()), syscall.LOCK_UN) != nil {
			err = ErrBotJournal
		}
		if j.lock.Close() != nil {
			err = ErrBotJournal
		}
		j.lock = nil
	}
	if err != nil {
		return ErrBotJournal
	}
	return nil
}

func (j *inboxJournal) Cursor(ctx context.Context) (int64, error) {
	if j == nil || ctx == nil {
		return 0, ErrBotJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	var cursor int64
	if j.db == nil || j.db.QueryRowContext(ctx, `SELECT cursor FROM owner_bot_meta WHERE singleton=1`).Scan(&cursor) != nil || cursor < 0 {
		return 0, botJournalError(ctx)
	}
	return cursor, nil
}

func validBotJournalFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func validBotJournalReason(reason string) bool {
	if len(reason) == 0 || len(reason) > 64 || reason[0] < 'a' || reason[0] > 'z' {
		return false
	}
	for _, ch := range reason {
		if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '_') {
			return false
		}
	}
	return true
}

func validBotJournalMessage(id int64, m OwnerReportMessage) bool {
	return id >= 0 && id < math.MaxInt64 && m.UpdateID == id && m.MessageID > 0 && m.SenderID > 0 && m.ChatID != 0 &&
		len(m.ChatType) > 0 && len(m.ChatType) <= 16 && len(m.Text) > 0 && len(m.Text) <= MaxOwnerReportCommandBytes && utf8.ValidString(m.Text)
}

// Ingest commits the whole bounded batch and the next offset together. The
// caller must commit this before making a later getUpdates call that confirms
// receipt. Unknown lower IDs never cause an automatic cursor rewind.
func (j *inboxJournal) Ingest(ctx context.Context, updates []receivedUpdate) error {
	if j == nil || ctx == nil || len(updates) > botJournalBatchLimit {
		return ErrBotJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.db == nil {
		return ErrBotJournal
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return botJournalError(ctx)
	}
	defer tx.Rollback()
	var cursor int64
	var total, pending int
	if tx.QueryRowContext(ctx, `SELECT cursor FROM owner_bot_meta WHERE singleton=1`).Scan(&cursor) != nil ||
		tx.QueryRowContext(ctx, `SELECT count(*),coalesce(sum(state IN ('pending','dispatching')),0) FROM owner_bot_inbox`).Scan(&total, &pending) != nil {
		return botJournalError(ctx)
	}
	nextCursor := cursor
	for _, update := range updates {
		if update.ID < 0 || update.ID == math.MaxInt64 || !validBotJournalFingerprint(update.Fingerprint) {
			return ErrBotJournal
		}
		var message []byte
		state, reason := "pending", ""
		if update.Rejection != "" {
			if update.Message != nil || !validBotJournalReason(update.Rejection) {
				return ErrBotJournal
			}
			state, reason = "rejected", update.Rejection
		} else {
			if update.Message == nil || !validBotJournalMessage(update.ID, *update.Message) {
				return ErrBotJournal
			}
			message, err = json.Marshal(update.Message)
			if err != nil || len(message) > botJournalMessageLimit {
				return ErrBotJournal
			}
		}
		var prior string
		err = tx.QueryRowContext(ctx, `SELECT fingerprint FROM owner_bot_inbox WHERE update_id=?`, update.ID).Scan(&prior)
		if err == nil {
			if prior != update.Fingerprint {
				return ErrBotJournalConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return botJournalError(ctx)
		}
		if update.ID < cursor {
			return ErrBotJournalConflict
		}
		if total >= botJournalHistoryLimit || (state == "pending" && pending >= botJournalPendingLimit) {
			return ErrBotJournalFull
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO owner_bot_inbox(update_id,fingerprint,state,message_json,reason) VALUES(?,?,?,?,?)`, update.ID, update.Fingerprint, state, message, reason); err != nil {
			return botJournalError(ctx)
		}
		total++
		if state == "pending" {
			pending++
		}
		if update.ID >= nextCursor {
			nextCursor = update.ID + 1
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE owner_bot_meta SET cursor=? WHERE singleton=1`, nextCursor); err != nil {
		return botJournalError(ctx)
	}
	if tx.Commit() != nil {
		return botJournalError(ctx)
	}
	return nil
}

func (j *inboxJournal) Next(ctx context.Context) (inboxEntry, bool, error) {
	var entry inboxEntry
	if j == nil || ctx == nil {
		return entry, false, ErrBotJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.db == nil {
		return entry, false, ErrBotJournal
	}
	var raw []byte
	err := j.db.QueryRowContext(ctx, `SELECT update_id,message_json FROM owner_bot_inbox WHERE state='pending' ORDER BY update_id LIMIT 1`).Scan(&entry.UpdateID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return entry, false, nil
	}
	if err != nil {
		return entry, false, botJournalError(ctx)
	}
	if len(raw) > botJournalMessageLimit || json.Unmarshal(raw, &entry.Message) != nil || !validBotJournalMessage(entry.UpdateID, entry.Message) {
		return inboxEntry{}, false, ErrBotJournal
	}
	return entry, true, nil
}

// BeginDispatch is durable before a send request starts. It rechecks the exact
// queued message and configured principal instead of trusting a prepared reply.
func (j *inboxJournal) BeginDispatch(ctx context.Context, entry inboxEntry, reply PreparedOwnerReply) error {
	if j == nil || ctx == nil {
		return ErrBotJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	m := entry.Message
	if j.db == nil {
		return ErrBotJournal
	}
	if !validBotJournalMessage(entry.UpdateID, m) || m.ChatType != "private" || m.SenderIsBot || m.SenderChatID != 0 || m.Forwarded || m.Edited || m.ViaBot ||
		m.SenderID != j.binding.OwnerUserID || m.ChatID != j.binding.OwnerChatID ||
		reply.OwnerUserID != j.binding.OwnerUserID || reply.ChatID != j.binding.OwnerChatID || reply.UpdateID != entry.UpdateID || reply.ReplyToMessageID != m.MessageID ||
		reply.ParseMode != "HTML" || reply.Text == "" || !utf8.ValidString(reply.Text) || !representativeID.MatchString(reply.RepresentativeID) || reply.RepresentativeID == "owner" {
		return ErrBotJournalBinding
	}
	message, err := json.Marshal(m)
	if err != nil {
		return ErrBotJournal
	}
	prepared, err := json.Marshal(reply)
	if err != nil || len(prepared) > botJournalReplyLimit {
		return ErrBotJournal
	}
	result, err := j.db.ExecContext(ctx, `UPDATE owner_bot_inbox SET state='dispatching',reply_json=? WHERE update_id=? AND state='pending' AND message_json=?`, prepared, entry.UpdateID, message)
	if err != nil {
		return botJournalError(ctx)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ErrBotJournal
	}
	if changed != 1 {
		return ErrBotJournalConflict
	}
	return nil
}

func (j *inboxJournal) Finish(ctx context.Context, id int64, state, reason string, messageID int64) error {
	if j == nil || ctx == nil || id < 0 || id == math.MaxInt64 ||
		!((state == "sent" && reason == "" && messageID > 0) || ((state == "failed" || state == "unknown") && validBotJournalReason(reason) && messageID == 0)) {
		return ErrBotJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.db == nil {
		return ErrBotJournal
	}
	result, err := j.db.ExecContext(ctx, `UPDATE owner_bot_inbox SET state=?,reason=?,sent_message_id=? WHERE update_id=? AND state='dispatching'`, state, reason, messageID, id)
	if err != nil {
		return botJournalError(ctx)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ErrBotJournal
	}
	if changed == 1 {
		return nil
	}
	var prior dispatchOutcome
	if j.db.QueryRowContext(ctx, `SELECT state,reason,sent_message_id FROM owner_bot_inbox WHERE update_id=?`, id).Scan(&prior.State, &prior.Reason, &prior.MessageID) == nil && prior == (dispatchOutcome{state, reason, messageID}) {
		return nil
	}
	return ErrBotJournalConflict
}

func (j *inboxJournal) Reject(ctx context.Context, id int64, reason string) error {
	if j == nil || ctx == nil || id < 0 || id == math.MaxInt64 || !validBotJournalReason(reason) {
		return ErrBotJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.db == nil {
		return ErrBotJournal
	}
	result, err := j.db.ExecContext(ctx, `UPDATE owner_bot_inbox SET state='rejected',reason=? WHERE update_id=? AND state='pending'`, reason, id)
	if err != nil {
		return botJournalError(ctx)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return ErrBotJournal
	}
	if changed == 1 {
		return nil
	}
	var state, prior string
	if j.db.QueryRowContext(ctx, `SELECT state,reason FROM owner_bot_inbox WHERE update_id=?`, id).Scan(&state, &prior) == nil && state == "rejected" && prior == reason {
		return nil
	}
	return ErrBotJournalConflict
}

func (j *inboxJournal) Outcome(ctx context.Context, id int64) (dispatchOutcome, error) {
	var outcome dispatchOutcome
	if j == nil || ctx == nil || id < 0 || id == math.MaxInt64 {
		return outcome, ErrBotJournal
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.db == nil {
		return outcome, ErrBotJournal
	}
	if j.db.QueryRowContext(ctx, `SELECT state,reason,sent_message_id FROM owner_bot_inbox WHERE update_id=?`, id).Scan(&outcome.State, &outcome.Reason, &outcome.MessageID) != nil {
		return outcome, botJournalError(ctx)
	}
	return outcome, nil
}
