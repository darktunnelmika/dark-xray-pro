package business

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Private management metadata. Never grants Telegram bot/customer authority.
type ClientContactInput struct {
	TelegramUsername string `json:"telegram_username"`
	TelegramID       string `json:"telegram_id"`
	Note             string `json:"note"`
}
type ClientContact struct {
	ClientContactInput
	Revision int64 `json:"revision"`
}
type ClientContactChange struct {
	ClientContactInput
	EventID          string `json:"event_id"`
	ExpectedRevision int64  `json:"expected_revision"`
}
type ClientContactReceipt struct {
	Contact  ClientContact `json:"contact"`
	Replayed bool          `json:"replayed"`
}

var contactUsername = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,31}$`)
var contactID = regexp.MustCompile(`^[1-9][0-9]{0,15}$`)

func normalizeContact(v ClientContactInput) (ClientContactInput, error) {
	v.TelegramUsername = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(v.TelegramUsername), "@"))
	v.TelegramID = strings.TrimSpace(v.TelegramID)
	v.Note = strings.TrimSpace(strings.ReplaceAll(v.Note, "\r\n", "\n"))
	if v.TelegramUsername != "" && !contactUsername.MatchString(v.TelegramUsername) {
		return v, ErrInvalid
	}
	if v.TelegramID != "" {
		n, e := strconv.ParseUint(v.TelegramID, 10, 64)
		if e != nil || !contactID.MatchString(v.TelegramID) || n > 4503599627370495 {
			return v, ErrInvalid
		}
	}
	if !utf8.ValidString(v.Note) || utf8.RuneCountInString(v.Note) > 500 {
		return v, ErrInvalid
	}
	for _, r := range v.Note {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return v, ErrInvalid
		}
	}
	return v, nil
}

var clientContactSchema = []string{
	`CREATE TABLE client_contacts_v1(client_id TEXT PRIMARY KEY REFERENCES clients(id),telegram_username TEXT NOT NULL,telegram_id TEXT NOT NULL,note TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),updated_unix INTEGER NOT NULL)`,
	`CREATE TABLE client_contact_events_v1(event_id TEXT PRIMARY KEY,client_id TEXT NOT NULL REFERENCES clients(id),fingerprint TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>0),telegram_username TEXT NOT NULL,telegram_id TEXT NOT NULL,note TEXT NOT NULL,created_unix INTEGER NOT NULL)`,
	`CREATE INDEX client_contacts_telegram_id_v1 ON client_contacts_v1(telegram_id)`,
	`CREATE INDEX client_contacts_telegram_username_v1 ON client_contacts_v1(telegram_username)`,
	`CREATE TRIGGER client_contact_events_no_update_v1 BEFORE UPDATE ON client_contact_events_v1 BEGIN SELECT RAISE(ABORT,'contact audit immutable'); END`,
	`CREATE TRIGGER client_contact_events_no_delete_v1 BEFORE DELETE ON client_contact_events_v1 BEGIN SELECT RAISE(ABORT,'contact audit immutable'); END`,
}

type contactQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func clientContactSchemaState(ctx context.Context, q contactQuery) (bool, error) {
	var n int
	if e := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'client_contact*'`).Scan(&n); e != nil {
		return false, e
	}
	if n == 0 {
		return false, nil
	}
	if n != len(clientContactSchema) {
		return false, ErrUnavailable
	}
	for _, ddl := range clientContactSchema {
		parts := strings.Fields(ddl)
		name := strings.SplitN(parts[2], "(", 2)[0]
		var actual string
		if e := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&actual); e != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(parts, " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}
func validateClientContacts(ctx context.Context, q contactQuery) error {
	for _, table := range []string{"client_contacts_v1", "client_contact_events_v1"} {
		rows, e := q.QueryContext(ctx, "SELECT client_id,telegram_username,telegram_id,note,revision FROM "+table)
		if e != nil {
			return e
		}
		for rows.Next() {
			var id string
			var v ClientContactInput
			var rev int64
			if e = rows.Scan(&id, &v.TelegramUsername, &v.TelegramID, &v.Note, &rev); e != nil {
				rows.Close()
				return e
			}
			normalized, e := normalizeContact(v)
			if e != nil || normalized != v || !idRE.MatchString(id) || rev < 1 || rev >= 1<<53 {
				rows.Close()
				return ErrUnavailable
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
	}
	var invalid int
	if e := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_contacts_v1 c WHERE c.revision>1 AND NOT EXISTS(SELECT 1 FROM client_contact_events_v1 e WHERE e.client_id=c.client_id AND e.revision=c.revision AND e.telegram_username=c.telegram_username AND e.telegram_id=c.telegram_id AND e.note=c.note)`).Scan(&invalid); e != nil {
		return e
	}
	if invalid != 0 {
		return ErrUnavailable
	}
	return nil
}
func ensureClientContacts(db *sql.DB) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	present, e := clientContactSchemaState(context.Background(), tx)
	if e != nil {
		return e
	}
	if !present {
		for _, ddl := range clientContactSchema {
			if _, e = tx.Exec(ddl); e != nil {
				return e
			}
		}
	}
	if e = validateClientContacts(context.Background(), tx); e != nil {
		return e
	}
	return tx.Commit()
}
func insertInitialContact(ctx context.Context, tx *sql.Tx, id string, v *ClientContactInput) (*ClientContact, error) {
	if v == nil {
		return nil, nil
	}
	n, e := normalizeContact(*v)
	if e != nil {
		return nil, e
	}
	if n == (ClientContactInput{}) {
		return nil, nil
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO client_contacts_v1(client_id,telegram_username,telegram_id,note,revision,updated_unix) VALUES(?,?,?,?,1,?)`, id, n.TelegramUsername, n.TelegramID, n.Note, time.Now().Unix())
	if e != nil {
		return nil, e
	}
	return &ClientContact{ClientContactInput: n, Revision: 1}, nil
}
func (s *Store) SetClientContact(ctx context.Context, id, scope string, in ClientContactChange) (ClientContactReceipt, error) {
	var out ClientContactReceipt
	v, e := normalizeContact(in.ClientContactInput)
	if e != nil || !idRE.MatchString(id) || !eventRE.MatchString(in.EventID) || in.ExpectedRevision < 0 || in.ExpectedRevision >= 1<<53 || (scope != "" && !idRE.MatchString(scope)) {
		return out, ErrInvalid
	}
	in.ClientContactInput = v
	fp := fingerprint("client-contact-v1", struct {
		ID    string
		Scope string
		Input ClientContactChange
	}{id, scope, in})
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	c, e := readClient(ctx, tx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if scope != "" && c.RepresentativeID != scope {
		return out, ErrOwnershipDenied
	}
	var previous string
	e = tx.QueryRowContext(ctx, `SELECT fingerprint,revision,telegram_username,telegram_id,note FROM client_contact_events_v1 WHERE event_id=?`, in.EventID).Scan(&previous, &out.Contact.Revision, &out.Contact.TelegramUsername, &out.Contact.TelegramID, &out.Contact.Note)
	if e == nil {
		if previous != fp {
			return ClientContactReceipt{}, ErrConflict
		}
		out.Replayed = true
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if c.Archived {
		return out, ErrConflict
	}
	revision := int64(0)
	if c.Contact != nil {
		revision = c.Contact.Revision
	}
	if revision != in.ExpectedRevision {
		return out, ErrConflict
	}
	if (c.Contact == nil && v == (ClientContactInput{})) || (c.Contact != nil && v == c.Contact.ClientContactInput) {
		return out, ErrInvalid
	}
	out.Contact = ClientContact{ClientContactInput: v, Revision: revision + 1}
	_, e = tx.ExecContext(ctx, `INSERT INTO client_contacts_v1(client_id,telegram_username,telegram_id,note,revision,updated_unix) VALUES(?,?,?,?,?,?) ON CONFLICT(client_id) DO UPDATE SET telegram_username=excluded.telegram_username,telegram_id=excluded.telegram_id,note=excluded.note,revision=excluded.revision,updated_unix=excluded.updated_unix`, id, v.TelegramUsername, v.TelegramID, v.Note, revision+1, time.Now().Unix())
	if e != nil {
		return out, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO client_contact_events_v1(event_id,client_id,fingerprint,revision,telegram_username,telegram_id,note,created_unix) VALUES(?,?,?,?,?,?,?,?)`, in.EventID, id, fp, revision+1, v.TelegramUsername, v.TelegramID, v.Note, time.Now().Unix())
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
