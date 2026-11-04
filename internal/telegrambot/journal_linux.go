//go:build linux

package telegrambot

import (
	"context"
	"crypto/rand"
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
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

const journalApplicationID = 0x44505442
const maxJournalBytes int64 = 32 << 20

var schema = []string{
	`CREATE TABLE bot_meta (id INTEGER PRIMARY KEY CHECK(id=1), version INTEGER NOT NULL CHECK(version=1), binding TEXT NOT NULL)`,
	`CREATE TABLE bot_cursors (bot_id INTEGER PRIMARY KEY CHECK(bot_id>0), cursor INTEGER NOT NULL CHECK(cursor>=0))`,
	`CREATE TABLE bot_inbox (bot_id INTEGER NOT NULL, update_id INTEGER NOT NULL CHECK(update_id>=0), fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64), kind TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('pending','processing','done','rejected','review')), input_json BLOB, created_unix INTEGER NOT NULL, reason TEXT NOT NULL DEFAULT '', attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0), next_unix_ms INTEGER NOT NULL DEFAULT 0 CHECK(next_unix_ms>=0), PRIMARY KEY(bot_id,update_id), FOREIGN KEY(bot_id) REFERENCES bot_cursors(bot_id), CHECK(input_json IS NULL OR length(input_json)<=32768))`,
	`CREATE INDEX bot_inbox_pending ON bot_inbox(bot_id,state,update_id)`,
	`CREATE TABLE bot_outbox (id TEXT PRIMARY KEY, bot_id INTEGER NOT NULL, update_id INTEGER NOT NULL, chat_id INTEGER NOT NULL CHECK(chat_id>0), kind TEXT NOT NULL CHECK(kind IN ('message','edit','invoice','document','checkout','callback_answer')), state TEXT NOT NULL CHECK(state IN ('pending','dispatching','sent','failed','unknown','cancelled')), payload_json BLOB, access_json BLOB NOT NULL, anonymous INTEGER NOT NULL CHECK(anonymous IN (0,1)), next_unix_ms INTEGER NOT NULL, expires_unix INTEGER NOT NULL, created_unix INTEGER NOT NULL, attempts INTEGER NOT NULL DEFAULT 0 CHECK(attempts>=0), reason TEXT NOT NULL DEFAULT '', sent_message_id INTEGER NOT NULL DEFAULT 0 CHECK(sent_message_id>=0), FOREIGN KEY(bot_id) REFERENCES bot_cursors(bot_id), CHECK(payload_json IS NULL OR length(payload_json)<=65536), CHECK(length(access_json)<=8192))`,
	`CREATE INDEX bot_outbox_pending ON bot_outbox(bot_id,state,next_unix_ms)`,
	`CREATE TABLE bot_callbacks (token_hash TEXT PRIMARY KEY CHECK(length(token_hash)=64), bot_id INTEGER NOT NULL, user_id INTEGER NOT NULL CHECK(user_id>0), chat_id INTEGER NOT NULL CHECK(chat_id>0), message_id INTEGER NOT NULL DEFAULT 0 CHECK(message_id>=0), access_digest TEXT NOT NULL CHECK(length(access_digest)=64), action_json BLOB NOT NULL CHECK(length(action_json)<=8192), outbox_id TEXT NOT NULL, expires_unix INTEGER NOT NULL, FOREIGN KEY(bot_id) REFERENCES bot_cursors(bot_id))`,
	`CREATE INDEX bot_callbacks_message ON bot_callbacks(bot_id,chat_id,message_id)`,
	`CREATE TABLE bot_preferences (bot_id INTEGER NOT NULL, user_id INTEGER NOT NULL CHECK(user_id>0), language TEXT NOT NULL CHECK(language IN ('fa','en')), PRIMARY KEY(bot_id,user_id), FOREIGN KEY(bot_id) REFERENCES bot_cursors(bot_id))`,
	`CREATE TABLE bot_delivery_proofs (outbox_id TEXT PRIMARY KEY REFERENCES bot_outbox(id), bot_id INTEGER NOT NULL, user_id INTEGER NOT NULL, client_id TEXT NOT NULL, sha256 TEXT NOT NULL CHECK(length(sha256)=64), terms_sha256 TEXT NOT NULL CHECK(length(terms_sha256)=64), expires_unix_nano INTEGER NOT NULL, message_id INTEGER NOT NULL DEFAULT 0, state TEXT NOT NULL CHECK(state IN ('waiting','pending','acknowledged','review')), next_ack_ms INTEGER NOT NULL DEFAULT 0)`,
	`CREATE TABLE bot_conversations (bot_id INTEGER NOT NULL, user_id INTEGER NOT NULL CHECK(user_id>0), access_digest TEXT NOT NULL CHECK(length(access_digest)=64), action_json BLOB NOT NULL CHECK(length(action_json)<=8192), expires_unix INTEGER NOT NULL, PRIMARY KEY(bot_id,user_id), FOREIGN KEY(bot_id) REFERENCES bot_cursors(bot_id))`,
}

type journal struct {
	mu      sync.Mutex
	db      *sql.DB
	lock    *os.File
	binding string
}

func bindingDigest(ids []int64) string {
	copyIDs := append([]int64{}, ids...)
	sort.Slice(copyIDs, func(i, j int) bool { return copyIDs[i] < copyIDs[j] })
	data, _ := json.Marshal(copyIDs)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func openJournal(path string, ids []int64) (*journal, error) {
	return openJournalMode(path, ids, false)
}

func openManagedJournal(path string, ids []int64) (*journal, error) {
	return openJournalMode(path, ids, true)
}

func openJournalMode(path string, ids []int64, managed bool) (*journal, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || privateParent(path) != nil || (!managed && len(ids) == 0) || len(ids) > maxBots {
		return nil, ErrConfig
	}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 || seen[id] {
			return nil, ErrConfig
		}
		seen[id] = true
	}
	lock, _, err := openPrivate(path+".lock", true)
	if err != nil {
		return nil, err
	}
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		lock.Close()
		return nil, ErrConflict
	}
	j := &journal{lock: lock, binding: bindingDigest(ids)}
	ok := false
	defer func() {
		if !ok {
			j.Close()
		}
	}()
	for _, suffix := range []string{"-wal", "-shm"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrJournal
		}
	}
	if info, err := os.Lstat(path + "-journal"); err == nil {
		if !privateRegular(info) || info.Size() > maxJournalBytes {
			return nil, ErrJournal
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrJournal
	}
	f, created, err := openPrivate(path, true)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || (!created && info.Size() == 0) || info.Size() > maxJournalBytes {
		return nil, ErrJournal
	}
	uri := (&url.URL{Scheme: "file", Path: path}).String() + "?mode=rw&_pragma=foreign_keys(1)&_pragma=trusted_schema(0)&_pragma=busy_timeout(3000)"
	j.db, err = sql.Open("sqlite", uri)
	if err != nil {
		return nil, ErrJournal
	}
	j.db.SetMaxOpenConns(1)
	j.db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, statement := range []string{"PRAGMA journal_mode=DELETE", "PRAGMA synchronous=FULL", "PRAGMA max_page_count=8192"} {
		if _, err = j.db.ExecContext(ctx, statement); err != nil {
			return nil, ErrJournal
		}
	}
	if created {
		tx, err := j.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, ErrJournal
		}
		defer tx.Rollback()
		for _, statement := range schema {
			if _, err = tx.ExecContext(ctx, statement); err != nil {
				return nil, ErrJournal
			}
		}
		if _, err = tx.ExecContext(ctx, "PRAGMA application_id=1146115138"); err != nil {
			return nil, ErrJournal
		}
		versionSQL := "PRAGMA user_version=1"
		if managed {
			versionSQL = "PRAGMA user_version=2"
		}
		if _, err = tx.ExecContext(ctx, versionSQL); err != nil {
			return nil, ErrJournal
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO bot_meta VALUES(1,1,?)", j.binding); err != nil {
			return nil, ErrJournal
		}
		for _, id := range ids {
			if _, err = tx.ExecContext(ctx, "INSERT INTO bot_cursors VALUES(?,0)", id); err != nil {
				return nil, ErrJournal
			}
		}
		if tx.Commit() != nil {
			return nil, ErrJournal
		}
	} else {
		if validateJournal(ctx, j.db, nil) != nil {
			return nil, ErrJournal
		}
		known, err := knownBotIDs(ctx, j.db)
		if err != nil {
			return nil, err
		}
		// An empty managed configuration is an onboarding state, not an
		// instruction to discard credentials for historical bot activity.
		if managed && len(ids) == 0 && len(known) != 0 {
			return nil, ErrConflict
		}
		present := map[int64]bool{}
		for _, id := range known {
			present[id] = true
		}
		missing := []int64{}
		for _, id := range ids {
			if !present[id] {
				missing = append(missing, id)
				known = append(known, id)
			}
		}
		if len(known) > 256 {
			return nil, ErrCapacity
		}
		if len(missing) > 0 {
			tx, err := j.db.BeginTx(ctx, nil)
			if err != nil {
				return nil, ErrJournal
			}
			defer tx.Rollback()
			for _, id := range missing {
				if _, err = tx.ExecContext(ctx, "INSERT INTO bot_cursors(bot_id,cursor) VALUES(?,0)", id); err != nil {
					return nil, ErrJournal
				}
			}
			j.binding = bindingDigest(known)
			if _, err = tx.ExecContext(ctx, "UPDATE bot_meta SET binding=? WHERE id=1", j.binding); err != nil {
				return nil, ErrJournal
			}
			if tx.Commit() != nil {
				return nil, ErrJournal
			}
		} else {
			j.binding = bindingDigest(known)
		}
		if managed {
			if _, err = j.db.ExecContext(ctx, "PRAGMA user_version=2"); err != nil {
				return nil, ErrJournal
			}
		}
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(info, current) || !privateRegular(current) {
		return nil, ErrJournal
	}
	if _, err = j.db.ExecContext(ctx, "UPDATE bot_outbox SET state='unknown',reason='restart_during_dispatch' WHERE state='dispatching'"); err != nil {
		return nil, ErrJournal
	}
	if _, err = j.db.ExecContext(ctx, "UPDATE bot_delivery_proofs SET state='review' WHERE state='waiting' AND outbox_id IN (SELECT id FROM bot_outbox WHERE state='unknown')"); err != nil {
		return nil, ErrJournal
	}
	if _, err = j.db.ExecContext(ctx, "UPDATE bot_inbox SET state='pending' WHERE state='processing'"); err != nil {
		return nil, ErrJournal
	}
	if created {
		if f.Sync() != nil {
			return nil, ErrJournal
		}
		dir, e := os.Open(filepath.Dir(path))
		if e != nil {
			return nil, ErrJournal
		}
		e = dir.Sync()
		dir.Close()
		if e != nil {
			return nil, ErrJournal
		}
	}
	ok = true
	return j, nil
}

func validateJournal(ctx context.Context, db *sql.DB, ids []int64) error {
	var app, version int
	var binding string
	if db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app) != nil || app != journalApplicationID || db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version) != nil || (version != 1 && version != 2) {
		return ErrJournal
	}
	known, err := knownBotIDs(ctx, db)
	if err != nil {
		return err
	}
	if len(known) == 0 && version != 2 {
		return ErrJournal
	}
	if db.QueryRowContext(ctx, "SELECT binding FROM bot_meta WHERE id=1 AND version=1").Scan(&binding) != nil || binding != bindingDigest(known) {
		return ErrConflict
	}
	present := map[int64]bool{}
	for _, id := range known {
		present[id] = true
	}
	for _, id := range ids {
		if !present[id] {
			return ErrConflict
		}
	}
	rows, err := db.QueryContext(ctx, "SELECT sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		return ErrJournal
	}
	want := map[string]bool{}
	for _, s := range schema {
		want[s] = true
	}
	for rows.Next() {
		var s string
		if rows.Scan(&s) != nil || !want[s] {
			rows.Close()
			return ErrJournal
		}
		delete(want, s)
	}
	e := rows.Err()
	rows.Close()
	if e != nil || len(want) != 0 {
		return ErrJournal
	}
	if len(known) == 0 {
		for _, table := range []string{"bot_inbox", "bot_outbox", "bot_callbacks", "bot_preferences", "bot_delivery_proofs", "bot_conversations"} {
			var count int64
			if db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count) != nil || count != 0 {
				return ErrJournal
			}
		}
	}
	for _, id := range known {
		var cursor int64
		if db.QueryRowContext(ctx, "SELECT cursor FROM bot_cursors WHERE bot_id=?", id).Scan(&cursor) != nil || cursor < 0 {
			return ErrJournal
		}
	}
	var result string
	if db.QueryRowContext(ctx, "PRAGMA quick_check(1)").Scan(&result) != nil || result != "ok" {
		return ErrJournal
	}
	return validateJournalRows(ctx, db)
}

func knownBotIDs(ctx context.Context, db *sql.DB) ([]int64, error) {
	rows, err := db.QueryContext(ctx, "SELECT bot_id FROM bot_cursors ORDER BY bot_id")
	if err != nil {
		return nil, ErrJournal
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) != nil || id <= 0 || len(ids) >= 256 {
			return nil, ErrJournal
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil, ErrJournal
	}
	return ids, nil
}

func (j *journal) Close() error {
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
			err = ErrJournal
		}
		if j.lock.Close() != nil {
			err = ErrJournal
		}
		j.lock = nil
	}
	if err != nil {
		return ErrJournal
	}
	return nil
}

func (j *journal) cursor(ctx context.Context, botID int64) (int64, error) {
	var cursor int64
	if j.db.QueryRowContext(ctx, "SELECT cursor FROM bot_cursors WHERE bot_id=?", botID).Scan(&cursor) != nil {
		return 0, ErrJournal
	}
	return cursor, nil
}

func (j *journal) ingest(ctx context.Context, botID int64, updates []received) error {
	if len(updates) > maxBatch {
		return ErrProtocol
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrJournal
	}
	defer tx.Rollback()
	var cursor int64
	var pending, total int
	if tx.QueryRowContext(ctx, "SELECT cursor FROM bot_cursors WHERE bot_id=?", botID).Scan(&cursor) != nil || tx.QueryRowContext(ctx, "SELECT COUNT(*),COALESCE(SUM(state IN ('pending','processing','review')),0) FROM bot_inbox WHERE bot_id=?", botID).Scan(&total, &pending) != nil {
		return ErrJournal
	}
	for _, u := range updates {
		if u.ID < 0 || u.ID == math.MaxInt64 || !hexDigest(u.Fingerprint) {
			return ErrProtocol
		}
		var previous string
		err = tx.QueryRowContext(ctx, "SELECT fingerprint FROM bot_inbox WHERE bot_id=? AND update_id=?", botID, u.ID).Scan(&previous)
		if err == nil {
			if previous != u.Fingerprint {
				return ErrConflict
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ErrJournal
		}
		if u.ID < cursor {
			return ErrConflict
		}
		if pending >= 1000 || total >= 20000 {
			return ErrCapacity
		}
		var input []byte
		kind := "unsupported"
		state := "rejected"
		reason := u.Rejection
		if u.Input != nil {
			if reason != "" || u.Input.Identity.BotID != botID || u.Input.UpdateID != u.ID {
				return ErrProtocol
			}
			input, err = json.Marshal(u.Input)
			if err != nil || len(input) > maxUpdateBytes {
				return ErrProtocol
			}
			kind = u.Input.Kind
			state = "pending"
			pending++
		} else if !validReason(reason) {
			return ErrProtocol
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO bot_inbox(bot_id,update_id,fingerprint,kind,state,input_json,created_unix,reason) VALUES(?,?,?,?,?,?,?,?)", botID, u.ID, u.Fingerprint, kind, state, input, time.Now().Unix(), reason); err != nil {
			return ErrJournal
		}
		total++
		if u.ID >= cursor {
			cursor = u.ID + 1
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE bot_cursors SET cursor=? WHERE bot_id=?", cursor, botID); err != nil {
		return ErrJournal
	}
	if tx.Commit() != nil {
		return ErrJournal
	}
	return nil
}

func (j *journal) nextInput(ctx context.Context, botID int64) (Incoming, bool, error) {
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return Incoming{}, false, ErrJournal
	}
	defer tx.Rollback()
	var raw []byte
	var id int64
	err = tx.QueryRowContext(ctx, "SELECT update_id,input_json FROM bot_inbox WHERE bot_id=? AND state='pending' AND next_unix_ms<=? ORDER BY CASE kind WHEN 'pre_checkout' THEN 0 WHEN 'payment' THEN 1 WHEN 'refund' THEN 1 WHEN 'callback' THEN 2 ELSE 3 END,update_id LIMIT 1", botID, time.Now().UnixMilli()).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Incoming{}, false, nil
	}
	var in Incoming
	if err != nil || decodeJSON(raw, &in) != nil || in.UpdateID != id || in.Identity.BotID != botID {
		return Incoming{}, false, ErrJournal
	}
	if _, err = tx.ExecContext(ctx, "UPDATE bot_inbox SET state='processing',attempts=MIN(attempts+1,1000000) WHERE bot_id=? AND update_id=? AND state='pending'", botID, id); err != nil {
		return Incoming{}, false, ErrJournal
	}
	if tx.Commit() != nil {
		return Incoming{}, false, ErrJournal
	}
	return in, true, nil
}

func (j *journal) retryInput(ctx context.Context, in Incoming) error {
	if in.Kind != "payment" && in.Kind != "refund" {
		return ErrProtocol
	}
	var attempts int
	if j.db.QueryRowContext(ctx, "SELECT attempts FROM bot_inbox WHERE bot_id=? AND update_id=? AND state='processing'", in.Identity.BotID, in.UpdateID).Scan(&attempts) != nil {
		return ErrJournal
	}
	delay := time.Duration(min(300, 1<<min(max(attempts, 1), 9))) * time.Second
	r, err := j.db.ExecContext(ctx, "UPDATE bot_inbox SET state='pending',reason='financial_capture_retry',next_unix_ms=? WHERE bot_id=? AND update_id=? AND state='processing'", time.Now().Add(delay).UnixMilli(), in.Identity.BotID, in.UpdateID)
	if err != nil {
		return ErrJournal
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return ErrConflict
	}
	return nil
}

func (j *journal) backlog(ctx context.Context, bot int64) (BotStatus, error) {
	s := BotStatus{BotID: bot}
	if j.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(state IN('pending','processing')),0),COALESCE(SUM(state='pending' AND reason='financial_capture_retry'),0),COALESCE(SUM(state='review'),0) FROM bot_inbox WHERE bot_id=?", bot).Scan(&s.InboxPending, &s.FinancialRetry, &s.ReviewPending) != nil {
		return s, ErrJournal
	}
	if j.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(state IN('pending','dispatching')),0),COALESCE(SUM(state='unknown'),0) FROM bot_outbox WHERE bot_id=?", bot).Scan(&s.OutboxPending, &s.OutboxUnknown) != nil {
		return s, ErrJournal
	}
	if j.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM bot_delivery_proofs WHERE bot_id=? AND state='review'", bot).Scan(&s.DeliveryReview) != nil {
		return s, ErrJournal
	}
	return s, nil
}

func (j *journal) rejectInput(ctx context.Context, in Incoming, reason string, review bool) error {
	if !validReason(reason) {
		return ErrProtocol
	}
	state := "rejected"
	if review {
		state = "review"
	}
	var raw any
	if review {
		b, _ := json.Marshal(in)
		raw = b
	}
	if _, err := j.db.ExecContext(ctx, "UPDATE bot_inbox SET state=?,reason=?,input_json=? WHERE bot_id=? AND update_id=? AND state='processing'", state, reason, raw, in.Identity.BotID, in.UpdateID); err != nil {
		return ErrJournal
	}
	return nil
}

func (j *journal) callback(ctx context.Context, in Incoming, access Access) (Action, string, error) {
	if !access.valid() || access.Identity != in.Identity || !strings.HasPrefix(in.CallbackData, "d1_") || len(in.CallbackData) != 35 {
		return Action{}, "", ErrCallback
	}
	sum := sha256.Sum256([]byte(in.CallbackData))
	hash := hex.EncodeToString(sum[:])
	var bot, user, chat, message, expires int64
	var digest string
	var raw []byte
	err := j.db.QueryRowContext(ctx, "SELECT bot_id,user_id,chat_id,message_id,access_digest,action_json,expires_unix FROM bot_callbacks WHERE token_hash=?", hash).Scan(&bot, &user, &chat, &message, &digest, &raw, &expires)
	var action Action
	if err != nil || bot != in.Identity.BotID || user != in.Identity.UserID || chat != in.Identity.ChatID || message != in.MessageID || message <= 0 || digest != access.digest() || expires <= time.Now().Unix() || decodeJSON(raw, &action) != nil || !validAction(action) {
		return Action{}, "", ErrCallback
	}
	return action, stableEvent(bot, hash), nil
}

func (j *journal) preference(ctx context.Context, id Identity, fallback string) string {
	var value string
	if j.db.QueryRowContext(ctx, "SELECT language FROM bot_preferences WHERE bot_id=? AND user_id=?", id.BotID, id.UserID).Scan(&value) == nil {
		return language(value)
	}
	return language(fallback)
}
func (j *journal) setPreference(ctx context.Context, id Identity, value string) error {
	if value != "fa" && value != "en" {
		return ErrProtocol
	}
	if _, err := j.db.ExecContext(ctx, "INSERT INTO bot_preferences(bot_id,user_id,language) VALUES(?,?,?) ON CONFLICT(bot_id,user_id) DO UPDATE SET language=excluded.language", id.BotID, id.UserID, value); err != nil {
		return ErrJournal
	}
	return nil
}

func (j *journal) complete(ctx context.Context, in Incoming, access Access, result Result) error {
	if !access.valid() || access.Identity != in.Identity {
		return ErrAccess
	}
	n := 0
	if result.Screen != nil {
		n++
	}
	if result.Invoice != nil {
		n++
	}
	if result.Checkout != nil {
		n++
	}
	if result.Document != nil {
		n++
	}
	if n != 1 {
		return ErrProtocol
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrJournal
	}
	defer tx.Rollback()
	var state string
	if tx.QueryRowContext(ctx, "SELECT state FROM bot_inbox WHERE bot_id=? AND update_id=?", in.Identity.BotID, in.UpdateID).Scan(&state) != nil || state != "processing" {
		return ErrConflict
	}
	var pending int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM bot_outbox WHERE bot_id=? AND state IN ('pending','dispatching','unknown')", in.Identity.BotID).Scan(&pending) != nil {
		return ErrJournal
	}
	if pending >= 1000 {
		return ErrCapacity
	}
	now := time.Now().Unix()
	id := fmt.Sprintf("%d:%d:result", in.Identity.BotID, in.UpdateID)
	kind := "message"
	expires := now + 600
	var payload []byte
	if result.Screen != nil {
		if !validText(result.Screen.Text, 4096) {
			return ErrProtocol
		}
		m := wireMessage{ChatID: in.Identity.ChatID, Text: result.Screen.Text, ParseMode: "HTML"}
		m.Preview.Disabled = true
		if in.Kind == "callback" && !result.Receipt {
			kind = "edit"
			m.MessageID = in.MessageID
			if _, err = tx.ExecContext(ctx, "DELETE FROM bot_callbacks WHERE bot_id=? AND chat_id=? AND message_id=?", in.Identity.BotID, in.Identity.ChatID, in.MessageID); err != nil {
				return ErrJournal
			}
		}
		m.Markup, err = prepareKeyboard(ctx, tx, id, access, result.Screen.Buttons, now)
		if err != nil {
			return err
		}
		payload, err = json.Marshal(m)
	} else if result.Document != nil {
		v := result.Document
		if !validDocument(*v) {
			return ErrProtocol
		}
		if v.ContentType == "image/png" && (result.Delivery == nil || v.SourceSHA256 != result.Delivery.SHA256) {
			return ErrProtocol
		}
		kind = "document"
		payload, err = json.Marshal(wireDocument{ChatID: in.Identity.ChatID, Name: v.Name, Content: v.Content, Caption: v.Caption, ContentType: v.ContentType, SourceContent: v.SourceContent, SourceSHA256: v.SourceSHA256, ContentSHA256: v.ContentSHA256})
	} else if result.Invoice != nil {
		v := result.Invoice
		if v.Currency != "XTR" || v.Amount <= 0 || v.Amount > 1_000_000_000 || !validText(v.Title, 32) || !validText(v.Description, 255) || !validOpaque(v.Payload, 128) {
			return ErrProtocol
		}
		kind = "invoice"
		payload, err = json.Marshal(wireInvoice{ChatID: in.Identity.ChatID, Title: v.Title, Description: v.Description, Payload: v.Payload, Currency: "XTR", Prices: []wirePrice{{Label: v.Title, Amount: v.Amount}}})
	} else {
		if in.Kind != "pre_checkout" || !validOpaque(in.QueryID, 256) {
			return ErrProtocol
		}
		v := result.Checkout
		if (!v.OK && !validText(v.ErrorMessage, 200)) || (v.OK && v.ErrorMessage != "") {
			return ErrProtocol
		}
		kind = "checkout"
		expires = in.ReceivedUnix + 8
		if in.Payment == nil || !validPayment(*in.Payment, false) {
			return ErrProtocol
		}
		payload, err = json.Marshal(wireCheckout{QueryID: in.QueryID, OK: v.OK, Error: v.ErrorMessage, Binding: in.Payment})
	}
	if err != nil || len(payload) > 64<<10 {
		return ErrProtocol
	}
	scope, _ := json.Marshal(access)
	if _, err = tx.ExecContext(ctx, "INSERT INTO bot_outbox(id,bot_id,update_id,chat_id,kind,state,payload_json,access_json,anonymous,next_unix_ms,expires_unix,created_unix) VALUES(?,?,?,?,?,'pending',?,?,0,?,?,?)", id, in.Identity.BotID, in.UpdateID, in.Identity.ChatID, kind, payload, scope, time.Now().UnixMilli(), expires, now); err != nil {
		return ErrJournal
	}
	if result.Delivery != nil {
		p := result.Delivery
		if p.ClientID == "" || len(p.ClientID) > 128 || !hexDigest(p.SHA256) || !hexDigest(p.TermsSHA256) || p.ExpiresUnixNano <= time.Now().UnixNano() || (kind != "message" && kind != "document") {
			return ErrProtocol
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO bot_delivery_proofs(outbox_id,bot_id,user_id,client_id,sha256,terms_sha256,expires_unix_nano,state) VALUES(?,?,?,?,?,?,?,'waiting')", id, in.Identity.BotID, in.Identity.UserID, p.ClientID, p.SHA256, p.TermsSHA256, p.ExpiresUnixNano); err != nil {
			return ErrJournal
		}
	}
	if result.ClearInput {
		if _, err = tx.ExecContext(ctx, "DELETE FROM bot_conversations WHERE bot_id=? AND user_id=?", in.Identity.BotID, in.Identity.UserID); err != nil {
			return ErrJournal
		}
	}
	if result.Input != nil {
		if !validAction(*result.Input) {
			return ErrProtocol
		}
		raw, _ := json.Marshal(result.Input)
		if _, err = tx.ExecContext(ctx, "INSERT INTO bot_conversations(bot_id,user_id,access_digest,action_json,expires_unix) VALUES(?,?,?,?,?) ON CONFLICT(bot_id,user_id) DO UPDATE SET access_digest=excluded.access_digest,action_json=excluded.action_json,expires_unix=excluded.expires_unix", in.Identity.BotID, in.Identity.UserID, access.digest(), raw, now+1200); err != nil {
			return ErrJournal
		}
	}
	if in.Kind == "callback" && result.Receipt {
		// A committed receipt consumes only the pressed mutation button. A
		// duplicate update/click cannot create another invoice for its order.
		// This is in the same transaction as enqueue, so crashes/rollbacks
		// before enqueue preserve the original stable mutation event.
		if !strings.HasPrefix(in.CallbackData, "d1_") || len(in.CallbackData) != 35 {
			return ErrCallback
		}
		sum := sha256.Sum256([]byte(in.CallbackData))
		deleted, err := tx.ExecContext(ctx, "DELETE FROM bot_callbacks WHERE token_hash=? AND bot_id=? AND user_id=? AND chat_id=? AND message_id=?", hex.EncodeToString(sum[:]), in.Identity.BotID, in.Identity.UserID, in.Identity.ChatID, in.MessageID)
		if err != nil {
			return ErrJournal
		}
		count, err := deleted.RowsAffected()
		if err != nil || count != 1 {
			return ErrCallback
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE bot_inbox SET state='done',input_json=NULL WHERE bot_id=? AND update_id=? AND state='processing'", in.Identity.BotID, in.UpdateID); err != nil {
		return ErrJournal
	}
	if tx.Commit() != nil {
		return ErrJournal
	}
	return nil
}

func prepareKeyboard(ctx context.Context, tx *sql.Tx, outbox string, access Access, rows [][]Button, now int64) (*wireKeyboard, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	if len(rows) > 24 {
		return nil, ErrProtocol
	}
	out := &wireKeyboard{}
	var count int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM bot_callbacks").Scan(&count) != nil {
		return nil, ErrJournal
	}
	if count+len(rows)*3 > 10000 {
		return nil, ErrCapacity
	}
	for _, row := range rows {
		if len(row) < 1 || len(row) > 3 {
			return nil, ErrProtocol
		}
		rendered := []wireButton{}
		for _, b := range row {
			w := wireButton{Text: b.Text, Style: b.Style, URL: b.URL}
			n := 0
			if b.Action != nil {
				n++
				if !validAction(*b.Action) {
					return nil, ErrProtocol
				}
				random := make([]byte, 16)
				if _, err := rand.Read(random); err != nil {
					return nil, ErrJournal
				}
				w.Callback = "d1_" + hex.EncodeToString(random)
				sum := sha256.Sum256([]byte(w.Callback))
				raw, _ := json.Marshal(b.Action)
				if _, err := tx.ExecContext(ctx, "INSERT INTO bot_callbacks(token_hash,bot_id,user_id,chat_id,access_digest,action_json,outbox_id,expires_unix) VALUES(?,?,?,?,?,?,?,?)", hex.EncodeToString(sum[:]), access.Identity.BotID, access.Identity.UserID, access.Identity.ChatID, access.digest(), raw, outbox, now+int64(callbackLifetime/time.Second)); err != nil {
					return nil, ErrJournal
				}
			}
			if b.URL != "" {
				n++
			}
			if b.CopyText != "" {
				n++
				w.Copy = &copyText{Text: b.CopyText}
			}
			if n != 1 {
				return nil, ErrProtocol
			}
			rendered = append(rendered, w)
		}
		out.Rows = append(out.Rows, rendered)
	}
	if validateWireKeyboard(out) != nil {
		return nil, ErrProtocol
	}
	return out, nil
}

func (j *journal) answer(ctx context.Context, in Incoming, text string, checkout bool) error {
	if in.Identity.BotID <= 0 || in.Identity.ChatID != in.Identity.UserID || !validOpaque(in.QueryID, 256) {
		return ErrProtocol
	}
	kind := "callback_answer"
	suffix := "ack"
	expires := in.ReceivedUnix + 15
	var payload []byte
	if checkout {
		kind = "checkout"
		suffix = "denied"
		expires = in.ReceivedUnix + 8
		payload, _ = json.Marshal(wireCheckout{QueryID: in.QueryID, Error: text})
	} else {
		payload, _ = json.Marshal(wireCallbackAnswer{QueryID: in.QueryID, Text: text, ShowAlert: text != ""})
	}
	scope, _ := json.Marshal(Access{Identity: in.Identity})
	id := fmt.Sprintf("%d:%d:%s", in.Identity.BotID, in.UpdateID, suffix)
	if _, err := j.db.ExecContext(ctx, "INSERT OR IGNORE INTO bot_outbox(id,bot_id,update_id,chat_id,kind,state,payload_json,access_json,anonymous,next_unix_ms,expires_unix,created_unix) VALUES(?,?,?,?,?,'pending',?,?,1,?,?,?)", id, in.Identity.BotID, in.UpdateID, in.Identity.ChatID, kind, payload, scope, time.Now().UnixMilli(), expires, time.Now().Unix()); err != nil {
		return ErrJournal
	}
	return nil
}

func (j *journal) nextDelivery(ctx context.Context, botID int64) (delivery, bool, error) {
	var d delivery
	var scope []byte
	var anonymous int
	err := j.db.QueryRowContext(ctx, "SELECT id,bot_id,update_id,chat_id,kind,payload_json,access_json,anonymous,expires_unix,attempts FROM bot_outbox WHERE bot_id=? AND state='pending' AND next_unix_ms<=? ORDER BY CASE kind WHEN 'checkout' THEN 0 WHEN 'callback_answer' THEN 1 ELSE 2 END,created_unix,id LIMIT 1", botID, time.Now().UnixMilli()).Scan(&d.ID, &d.BotID, &d.UpdateID, &d.ChatID, &d.Kind, &d.Payload, &scope, &anonymous, &d.ExpiresUnix, &d.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return d, false, nil
	}
	if err != nil || decodeJSON(scope, &d.Access) != nil || d.Access.Identity.BotID != botID || d.Access.Identity.ChatID != d.ChatID || d.Access.Identity.UserID != d.ChatID {
		return delivery{}, false, ErrJournal
	}
	d.Anonymous = anonymous == 1
	if d.Anonymous && d.Kind != "callback_answer" && d.Kind != "checkout" {
		return delivery{}, false, ErrJournal
	}
	var proof DeliveryProof
	var proofBot, proofUser int64
	var proofState string
	err = j.db.QueryRowContext(ctx, "SELECT client_id,sha256,terms_sha256,expires_unix_nano,bot_id,user_id,state FROM bot_delivery_proofs WHERE outbox_id=?", d.ID).Scan(&proof.ClientID, &proof.SHA256, &proof.TermsSHA256, &proof.ExpiresUnixNano, &proofBot, &proofUser, &proofState)
	if err == nil {
		if proofBot != d.BotID || proofUser != d.ChatID || proofState != "waiting" || !hexDigest(proof.SHA256) || !hexDigest(proof.TermsSHA256) || proof.ExpiresUnixNano <= 0 || !validOpaque(proof.ClientID, 128) || (d.Kind != "message" && d.Kind != "document") {
			return delivery{}, false, ErrJournal
		}
		d.Proof = &proof
	} else if !errors.Is(err, sql.ErrNoRows) {
		return delivery{}, false, ErrJournal
	}
	if d.Kind == "document" {
		var m wireDocument
		if decodeJSON(d.Payload, &m) != nil || !validDocument(m.document()) {
			return delivery{}, false, ErrJournal
		}
		if m.ContentType == "image/png" && (d.Proof == nil || m.SourceSHA256 != d.Proof.SHA256) {
			return delivery{}, false, ErrJournal
		}
	}
	return d, true, nil
}

func (j *journal) claim(ctx context.Context, d delivery) error {
	r, err := j.db.ExecContext(ctx, "UPDATE bot_outbox SET state='dispatching',attempts=attempts+1 WHERE id=? AND bot_id=? AND state='pending' AND payload_json=?", d.ID, d.BotID, d.Payload)
	if err != nil {
		return ErrJournal
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return ErrConflict
	}
	return nil
}
func (j *journal) cancelDelivery(ctx context.Context, d delivery, reason string) error {
	if !validReason(reason) {
		return ErrProtocol
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrJournal
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "UPDATE bot_outbox SET state='cancelled',reason=?,payload_json=NULL WHERE id=? AND state='pending'", reason, d.ID); err != nil {
		return ErrJournal
	}
	if _, err = tx.ExecContext(ctx, "UPDATE bot_delivery_proofs SET state='review' WHERE outbox_id=? AND state='waiting'", d.ID); err != nil {
		return ErrJournal
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM bot_callbacks WHERE outbox_id=?", d.ID); err != nil {
		return ErrJournal
	}
	if tx.Commit() != nil {
		return ErrJournal
	}
	return nil
}
func (j *journal) delay(ctx context.Context, d delivery, until time.Time) error {
	if _, err := j.db.ExecContext(ctx, "UPDATE bot_outbox SET next_unix_ms=? WHERE id=? AND state='pending'", until.UnixMilli(), d.ID); err != nil {
		return ErrJournal
	}
	return nil
}

func (j *journal) finish(ctx context.Context, d delivery, out DeliveryOutcome) error {
	if out.State != "sent" && out.State != "failed" && out.State != "unknown" && out.State != "retry" {
		return ErrProtocol
	}
	if out.State != "sent" && !validReason(out.Reason) {
		return ErrProtocol
	}
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrJournal
	}
	defer tx.Rollback()
	state := out.State
	var payload any
	next := time.Now().UnixMilli()
	reason := out.Reason
	if state == "unknown" {
		payload = d.Payload
	}
	if state == "retry" {
		if out.RetryAfter <= 0 || out.RetryAfter > time.Hour {
			return ErrProtocol
		}
		if d.Attempts >= 4 || time.Now().Add(out.RetryAfter).Unix() >= d.ExpiresUnix {
			state = "failed"
			reason = "retry_expired"
		} else {
			state = "pending"
			next = time.Now().Add(out.RetryAfter).UnixMilli()
			payload = d.Payload
		}
	}
	r, err := tx.ExecContext(ctx, "UPDATE bot_outbox SET state=?,reason=?,sent_message_id=?,payload_json=?,next_unix_ms=? WHERE id=? AND state='dispatching'", state, reason, out.MessageID, payload, next, d.ID)
	if err != nil {
		return ErrJournal
	}
	n, err := r.RowsAffected()
	if err != nil || n != 1 {
		return ErrConflict
	}
	if state == "sent" && out.MessageID > 0 {
		if _, err = tx.ExecContext(ctx, "UPDATE bot_callbacks SET message_id=? WHERE outbox_id=? AND message_id=0", out.MessageID, d.ID); err != nil {
			return ErrJournal
		}
	}
	if state == "sent" && out.MessageID > 0 {
		if _, err = tx.ExecContext(ctx, "UPDATE bot_delivery_proofs SET state='pending',message_id=? WHERE outbox_id=? AND state='waiting'", out.MessageID, d.ID); err != nil {
			return ErrJournal
		}
	}
	if state == "failed" || state == "unknown" {
		if _, err = tx.ExecContext(ctx, "UPDATE bot_delivery_proofs SET state='review' WHERE outbox_id=? AND state='waiting'", d.ID); err != nil {
			return ErrJournal
		}
	}
	if state == "failed" || state == "unknown" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM bot_callbacks WHERE outbox_id=?", d.ID); err != nil {
			return ErrJournal
		}
	}
	if tx.Commit() != nil {
		return ErrJournal
	}
	return nil
}

func (j *journal) prune(ctx context.Context) error {
	now := time.Now().Unix()
	tx, err := j.db.BeginTx(ctx, nil)
	if err != nil {
		return ErrJournal
	}
	defer tx.Rollback()
	for _, q := range []struct {
		sql string
		arg int64
	}{
		{"DELETE FROM bot_callbacks WHERE expires_unix<=?", now},
		{"DELETE FROM bot_conversations WHERE expires_unix<=?", now},
		{"DELETE FROM bot_delivery_proofs WHERE state='acknowledged' AND outbox_id IN (SELECT id FROM bot_outbox WHERE created_unix<?)", now - 7*86400},
		{"DELETE FROM bot_outbox WHERE state IN ('sent','failed','cancelled') AND created_unix<? AND id NOT IN (SELECT outbox_id FROM bot_delivery_proofs)", now - 7*86400},
		{"DELETE FROM bot_inbox WHERE state IN ('done','rejected') AND created_unix<?", now - 7*86400},
	} {
		if _, err = tx.ExecContext(ctx, q.sql, q.arg); err != nil {
			return ErrJournal
		}
	}
	if tx.Commit() != nil {
		return ErrJournal
	}
	return nil
}

func validAction(a Action) bool {
	return validOpaque(a.Name, 48) && len(a.Resource) <= 128 && len(a.Value) <= 1024 && !strings.ContainsAny(a.Resource+a.Value, "\r\n\x00")
}
func validReason(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

type pendingDeliveryAck struct {
	ID        string
	Identity  Identity
	Proof     DeliveryProof
	MessageID int64
}

func (j *journal) nextAck(ctx context.Context, botID int64) (pendingDeliveryAck, bool, error) {
	var ack pendingDeliveryAck
	ack.Identity.BotID = botID
	err := j.db.QueryRowContext(ctx, "SELECT outbox_id,user_id,client_id,sha256,terms_sha256,expires_unix_nano,message_id FROM bot_delivery_proofs WHERE bot_id=? AND state='pending' AND next_ack_ms<=? ORDER BY outbox_id LIMIT 1", botID, time.Now().UnixMilli()).Scan(&ack.ID, &ack.Identity.UserID, &ack.Proof.ClientID, &ack.Proof.SHA256, &ack.Proof.TermsSHA256, &ack.Proof.ExpiresUnixNano, &ack.MessageID)
	if errors.Is(err, sql.ErrNoRows) {
		return ack, false, nil
	}
	if err != nil || ack.MessageID <= 0 || !hexDigest(ack.Proof.SHA256) || !hexDigest(ack.Proof.TermsSHA256) {
		return pendingDeliveryAck{}, false, ErrJournal
	}
	ack.Identity.ChatID = ack.Identity.UserID
	var parentBot, parentChat, parentMessage int64
	var state, kind string
	var scope []byte
	var anonymous int
	var access Access
	if j.db.QueryRowContext(ctx, "SELECT bot_id,chat_id,sent_message_id,state,kind,access_json,anonymous FROM bot_outbox WHERE id=?", ack.ID).Scan(&parentBot, &parentChat, &parentMessage, &state, &kind, &scope, &anonymous) != nil || parentBot != botID || parentChat != ack.Identity.UserID || parentMessage != ack.MessageID || state != "sent" || anonymous != 0 || (kind != "message" && kind != "document") || decodeJSON(scope, &access) != nil || !access.valid() || access.Identity != ack.Identity {
		return pendingDeliveryAck{}, false, ErrJournal
	}
	return ack, true, nil
}
func (j *journal) finishAck(ctx context.Context, ack pendingDeliveryAck, accepted bool) error {
	state := "pending"
	if accepted {
		state = "acknowledged"
	} else if ack.Proof.ExpiresUnixNano <= time.Now().UnixNano() {
		state = "review"
	}
	if _, err := j.db.ExecContext(ctx, "UPDATE bot_delivery_proofs SET state=?,next_ack_ms=? WHERE outbox_id=? AND state='pending' AND message_id=?", state, time.Now().Add(5*time.Second).UnixMilli(), ack.ID, ack.MessageID); err != nil {
		return ErrJournal
	}
	return nil
}
func (j *journal) expiredProofNotice(ctx context.Context, d delivery) error {
	m := wireMessage{ChatID: d.ChatID, ParseMode: "HTML", Text: "مدرک آماده‌بودن سرویس منقضی شد. برای دریافت تنظیمات تازه دوباره «سرویس‌های من» را باز کنید.\nThe service readiness proof expired. Reopen My services for a fresh configuration."}
	m.Preview.Disabled = true
	payload, _ := json.Marshal(m)
	scope, _ := json.Marshal(d.Access)
	now := time.Now()
	if _, err := j.db.ExecContext(ctx, "INSERT OR IGNORE INTO bot_outbox(id,bot_id,update_id,chat_id,kind,state,payload_json,access_json,anonymous,next_unix_ms,expires_unix,created_unix) VALUES(?,?,?,?,'message','pending',?,?,0,?,?,?)", d.ID+":expired", d.BotID, d.UpdateID, d.ChatID, payload, scope, now.UnixMilli(), now.Add(time.Minute).Unix(), now.Unix()); err != nil {
		return ErrJournal
	}
	return nil
}
func (j *journal) inputAction(ctx context.Context, access Access) (Action, bool, error) {
	var raw []byte
	var digest string
	var expiry int64
	var action Action
	err := j.db.QueryRowContext(ctx, "SELECT access_digest,action_json,expires_unix FROM bot_conversations WHERE bot_id=? AND user_id=?", access.Identity.BotID, access.Identity.UserID).Scan(&digest, &raw, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return action, false, nil
	}
	if err != nil {
		return action, false, ErrJournal
	}
	if digest != access.digest() || expiry <= time.Now().Unix() {
		return action, false, nil
	}
	if decodeJSON(raw, &action) != nil || !validAction(action) {
		return action, false, ErrJournal
	}
	return action, true, nil
}
