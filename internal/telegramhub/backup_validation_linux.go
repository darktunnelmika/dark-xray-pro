//go:build linux

package telegramhub

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf8"
)

// ValidateOfflineBackup validates a retained config/token pair and, when path
// is nonempty, its existing standalone delivery journal. It never reads the
// original paths embedded in configRaw, contacts Telegram, opens a worker,
// initializes a database or changes dispatching outcomes. An empty path is
// allowed for a configured bot that has not created its journal yet. The caller
// must fence source writers before producing this private immutable snapshot.
// Token syntax is checked locally; this is not a live credential verification.
func ValidateOfflineBackup(ctx context.Context, path string, configRaw, tokenRaw []byte) (result error) {
	if ctx == nil {
		return ErrBotJournal
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	config, err := parseOwnerBotFileConfig(configRaw)
	if err != nil || len(tokenRaw) < 1 || len(tokenRaw) > 256 ||
		!botTokenFormat.Match(bytes.TrimSuffix(tokenRaw, []byte("\n"))) {
		return ErrBotLABConfig
	}
	if path == "" {
		return ctx.Err()
	}
	defer func() {
		if result != nil {
			if ctx.Err() != nil {
				result = ctx.Err()
			} else if !errors.Is(result, ErrBotJournalBinding) {
				result = ErrBotJournal
			}
		}
	}()
	before, err := offlineBotSnapshotPath(path)
	if err != nil {
		return ErrBotJournal
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1&_pragma=query_only(1)&_pragma=trusted_schema(0)&_pragma=foreign_keys(1)&_pragma=busy_timeout(1000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return ErrBotJournal
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer func() {
		if err := db.Close(); err != nil && result == nil {
			result = ErrBotJournal
		}
	}()
	j := &inboxJournal{db: db, binding: config.binding()}
	if err := j.validate(ctx); err != nil {
		return err
	}
	// The existing validator checks exact DDL. Also exclude objects with names
	// such as sqliteXextra, which SQL LIKE 'sqlite_%' would otherwise hide.
	var objects int
	if db.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_schema").Scan(&objects) != nil || objects != 3 {
		return ErrBotJournal
	}
	var integrity string
	if db.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&integrity) != nil || integrity != "ok" {
		return ErrBotJournal
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return ErrBotJournal
	}
	bad := rows.Next()
	err = rows.Err()
	rows.Close()
	if bad || err != nil || validateOfflineBotRows(ctx, j) != nil {
		return ErrBotJournal
	}
	after, err := offlineBotSnapshotPath(path)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return ErrBotJournal
	}
	return ctx.Err()
}

func offlineBotSnapshotPath(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || checkBotJournalParent(filepath.Dir(path)) != nil {
		return nil, ErrBotJournal
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrBotJournal
		}
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrBotJournal
	}
	f := os.NewFile(uintptr(fd), "offline-owner-bot-snapshot")
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !validBotJournalFile(st) || st.Mode().Perm()&0111 != 0 || st.Size() < 1 || st.Size() > 64<<30 {
		return nil, ErrBotJournal
	}
	return st, nil
}

// Read one bounded row at a time. Pending/rejected updates may legitimately be
// unauthorized; only dispatch records must match the configured private owner.
func validateOfflineBotRows(ctx context.Context, j *inboxJournal) error {
	rows, err := j.db.QueryContext(ctx, `SELECT update_id,fingerprint,state,
CASE WHEN length(message_json)<=2048 THEN message_json ELSE NULL END,
CASE WHEN length(reply_json)<=32768 THEN reply_json ELSE NULL END,reason
FROM owner_bot_inbox ORDER BY update_id`)
	if err != nil {
		return ErrBotJournal
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var fingerprint, state, reason string
		var messageRaw, replyRaw []byte
		if rows.Scan(&id, &fingerprint, &state, &messageRaw, &replyRaw, &reason) != nil || !validBotJournalFingerprint(fingerprint) ||
			(reason != "" && !validBotJournalReason(reason)) {
			return ErrBotJournal
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var message OwnerReportMessage
		if messageRaw != nil {
			if decodeBotJSON(messageRaw, &message) != nil || !validBotJournalMessage(id, message) {
				return ErrBotJournal
			}
		} else if state != "rejected" {
			return ErrBotJournal
		}
		if state == "pending" || state == "rejected" {
			continue
		}
		var reply PreparedOwnerReply
		if decodeBotJSON(replyRaw, &reply) != nil || message.ChatType != "private" || message.SenderIsBot || message.SenderChatID != 0 || message.Forwarded || message.Edited || message.ViaBot ||
			message.SenderID != j.binding.OwnerUserID || message.ChatID != j.binding.OwnerChatID ||
			reply.OwnerUserID != j.binding.OwnerUserID || reply.ChatID != j.binding.OwnerChatID || reply.UpdateID != id || reply.ReplyToMessageID != message.MessageID ||
			reply.ParseMode != "HTML" || reply.Text == "" || !utf8.ValidString(reply.Text) || !representativeID.MatchString(reply.RepresentativeID) || reply.RepresentativeID == "owner" {
			return ErrBotJournal
		}
	}
	return rows.Err()
}
