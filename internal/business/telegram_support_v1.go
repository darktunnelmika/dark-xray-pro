package business

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Telegram support is an optional extension of the native business database.
// Neither opening a database nor receiving an arbitrary Telegram update enables
// it. All actor IDs must come from an authenticated private Telegram update.
type TelegramAttachmentV1 struct {
	Kind   string `json:"kind"`
	FileID string `json:"file_id"`
}

type TelegramTicketV1 struct {
	ID                     string `json:"id"`
	StoreID                string `json:"store_id"`
	CustomerID             string `json:"customer_id"`
	ClientID               string `json:"client_id,omitempty"`
	Subject                string `json:"subject"`
	Status                 string `json:"status"`
	AssignedTelegramUserID int64  `json:"assigned_telegram_user_id,omitempty"`
	Revision               int64  `json:"revision"`
	CreatedUnix            int64  `json:"created_unix"`
	UpdatedUnix            int64  `json:"updated_unix"`
}

type TelegramTicketMessageV1 struct {
	ID          string                `json:"id"`
	TicketID    string                `json:"ticket_id"`
	UserID      int64                 `json:"user_id"`
	FromSupport bool                  `json:"from_support"`
	Text        string                `json:"text"`
	Attachment  *TelegramAttachmentV1 `json:"attachment,omitempty"`
	CreatedUnix int64                 `json:"created_unix"`
}

type TelegramTicketCreateV1 struct {
	EventID    string                `json:"event_id"`
	ClientID   string                `json:"client_id,omitempty"`
	Subject    string                `json:"subject"`
	Text       string                `json:"text"`
	Attachment *TelegramAttachmentV1 `json:"attachment,omitempty"`
}

type TelegramTicketReplyV1 struct {
	EventID    string                `json:"event_id"`
	TicketID   string                `json:"ticket_id"`
	Text       string                `json:"text"`
	Attachment *TelegramAttachmentV1 `json:"attachment,omitempty"`
}

type TelegramTicketChangeV1 struct {
	EventID                string `json:"event_id"`
	TicketID               string `json:"ticket_id"`
	ExpectedRevision       int64  `json:"expected_revision"`
	Status                 string `json:"status"`
	AssignedTelegramUserID int64  `json:"assigned_telegram_user_id"`
}

type TelegramTicketReceiptV1 struct {
	Ticket   TelegramTicketV1         `json:"ticket"`
	Message  *TelegramTicketMessageV1 `json:"message,omitempty"`
	Replayed bool                     `json:"replayed"`
}

type TelegramTicketFilterV1 struct {
	Status string
	Own    bool
	Limit  int
	Offset int
}

var telegramSupportSchemaV1 = []string{
	`CREATE TABLE telegram_support_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`CREATE TABLE telegram_support_tickets(
 id TEXT PRIMARY KEY,store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),
 customer_id TEXT NOT NULL REFERENCES telegram_commerce_customers(customer_id),client_id TEXT REFERENCES clients(id),
 subject TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN('open','waiting_support','waiting_customer','closed')),
 assigned_user_id INTEGER NOT NULL DEFAULT 0 CHECK(assigned_user_id>=0),revision INTEGER NOT NULL CHECK(revision>0),
 created_unix INTEGER NOT NULL,updated_unix INTEGER NOT NULL)`,
	`CREATE INDEX telegram_support_ticket_scope ON telegram_support_tickets(store_id,customer_id,status,updated_unix DESC,id)`,
	`CREATE TABLE telegram_support_messages(
 id TEXT PRIMARY KEY,ticket_id TEXT NOT NULL REFERENCES telegram_support_tickets(id),user_id INTEGER NOT NULL CHECK(user_id>0),
 from_support INTEGER NOT NULL CHECK(from_support IN(0,1)),body TEXT NOT NULL,attachment_json TEXT NOT NULL,created_unix INTEGER NOT NULL)`,
	`CREATE INDEX telegram_support_message_order ON telegram_support_messages(ticket_id,created_unix,id)`,
	`CREATE TABLE telegram_support_events(
 event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),kind TEXT NOT NULL,
 store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),actor_user_id INTEGER NOT NULL CHECK(actor_user_id>0),
 ticket_id TEXT NOT NULL REFERENCES telegram_support_tickets(id),receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)`,
	`CREATE TRIGGER telegram_support_messages_no_update BEFORE UPDATE ON telegram_support_messages BEGIN SELECT RAISE(ABORT,'support messages are immutable'); END`,
	`CREATE TRIGGER telegram_support_messages_no_delete BEFORE DELETE ON telegram_support_messages BEGIN SELECT RAISE(ABORT,'support messages are immutable'); END`,
	`CREATE TRIGGER telegram_support_events_no_update BEFORE UPDATE ON telegram_support_events BEGIN SELECT RAISE(ABORT,'support audit is immutable'); END`,
	`CREATE TRIGGER telegram_support_events_no_delete BEFORE DELETE ON telegram_support_events BEGIN SELECT RAISE(ABORT,'support audit is immutable'); END`,
	`CREATE TRIGGER telegram_support_meta_no_update BEFORE UPDATE ON telegram_support_meta BEGIN SELECT RAISE(ABORT,'support schema is immutable'); END`,
	`CREATE TRIGGER telegram_support_meta_no_delete BEFORE DELETE ON telegram_support_meta BEGIN SELECT RAISE(ABORT,'support schema is immutable'); END`,
}

type telegramExtensionQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// telegramExtensionState verifies optional schema DDL, including audit guards.
// A partially created, unknown or modified family is never silently repaired.
func telegramExtensionState(ctx context.Context, q telegramExtensionQuery, prefix string, schema []string) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB ?`, prefix+"*").Scan(&count); err != nil {
		return false, ErrUnavailable
	}
	if count == 0 {
		return false, nil
	}
	if count != len(schema) {
		return false, ErrUnavailable
	}
	for _, ddl := range schema {
		parts := strings.Fields(ddl)
		nameIndex := 2
		if len(parts) > 2 && parts[1] == "UNIQUE" {
			nameIndex = 3
		}
		name := strings.SplitN(parts[nameIndex], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&actual); err != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(parts, " ") {
			return false, ErrUnavailable
		}
	}
	var version int
	if err := q.QueryRowContext(ctx, `SELECT version FROM `+prefix+`meta WHERE id=1`).Scan(&version); err != nil || version != 1 {
		return false, ErrUnavailable
	}
	return true, nil
}

func (s *Store) enableTelegramExtension(ctx context.Context, prefix string, schema []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return err
	}
	present, err := telegramExtensionState(ctx, tx, prefix, schema)
	if err != nil {
		return err
	}
	if !present {
		for _, ddl := range schema {
			if _, err = tx.ExecContext(ctx, ddl); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO `+prefix+`meta(id,version) VALUES(1,1)`); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func telegramExtensionReady(ctx context.Context, tx *sql.Tx, prefix string) error {
	var ddl []string
	switch prefix {
	case "telegram_support_":
		ddl = telegramSupportSchemaV1
	case "telegram_campaign_":
		ddl = telegramCampaignSchemaV1
	case "telegram_rewards_":
		ddl = telegramRewardsSchemaV1
	default:
		return ErrUnavailable
	}
	present, err := telegramExtensionState(ctx, tx, prefix, ddl)
	if err != nil || !present {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) verifyTelegramExtension(ctx context.Context, prefix string, ddl []string, validate func(context.Context, *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	present, err := telegramExtensionState(ctx, tx, prefix, ddl)
	if err != nil || !present {
		return err
	}
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return err
	}
	return validate(ctx, tx)
}

func (s *Store) EnableTelegramSupportV1(ctx context.Context) error {
	return s.enableTelegramExtension(ctx, "telegram_support_", telegramSupportSchemaV1)
}

// VerifyTelegramSupportV1 accepts absence for older backups; malformed enabled
// state is an error. It performs no writes and does not enable a worker.
func (s *Store) VerifyTelegramSupportV1(ctx context.Context) error {
	return s.verifyTelegramExtension(ctx, "telegram_support_", telegramSupportSchemaV1, validateTelegramSupportRowsV1)
}

func telegramExtensionRowsValid(ctx context.Context, q telegramExtensionQuery, query string) error {
	var invalid int
	if err := q.QueryRowContext(ctx, query).Scan(&invalid); err != nil || invalid != 0 {
		return ErrUnavailable
	}
	return nil
}

func telegramOpaqueID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func telegramValidText(text string, max int, allowEmpty bool) bool {
	if !utf8.ValidString(text) || utf8.RuneCountInString(text) > max || !allowEmpty && strings.TrimSpace(text) == "" {
		return false
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}

func telegramValidAttachment(a *TelegramAttachmentV1) bool {
	if a == nil {
		return true
	}
	if a.Kind != "photo" && a.Kind != "document" && a.Kind != "voice" {
		return false
	}
	if len(a.FileID) < 1 || len(a.FileID) > 512 {
		return false
	}
	for _, r := range a.FileID {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '=') {
			return false
		}
	}
	return true
}

func telegramAccessHasPermissionV1(a TelegramAccess, permission string) bool {
	for _, p := range a.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

func telegramSupportAuthorized(ctx context.Context, tx *sql.Tx, actor TelegramActor) (TelegramAccess, error) {
	if err := telegramExtensionReady(ctx, tx, "telegram_support_"); err != nil {
		return TelegramAccess{}, err
	}
	return authorizeTelegramTx(ctx, tx, actor, "")
}

const telegramTicketColumnsV1 = `id,store_id,customer_id,COALESCE(client_id,''),subject,status,assigned_user_id,revision,created_unix,updated_unix`

func scanTelegramTicketV1(row interface{ Scan(...any) error }) (TelegramTicketV1, error) {
	var t TelegramTicketV1
	err := row.Scan(&t.ID, &t.StoreID, &t.CustomerID, &t.ClientID, &t.Subject, &t.Status, &t.AssignedTelegramUserID, &t.Revision, &t.CreatedUnix, &t.UpdatedUnix)
	return t, err
}

func telegramReadTicketTx(ctx context.Context, tx *sql.Tx, access TelegramAccess, id string) (TelegramTicketV1, error) {
	var out TelegramTicketV1
	if !idRE.MatchString(id) {
		return out, ErrInvalid
	}
	out, err := scanTelegramTicketV1(tx.QueryRowContext(ctx, `SELECT `+telegramTicketColumnsV1+` FROM telegram_support_tickets WHERE id=? AND store_id=?`, id, access.Store.ID))
	if errors.Is(err, sql.ErrNoRows) {
		return TelegramTicketV1{}, ErrNotFound
	}
	if err != nil {
		return TelegramTicketV1{}, err
	}
	if out.CustomerID != access.CustomerID && !telegramAccessHasPermissionV1(access, "support") {
		return TelegramTicketV1{}, ErrNotFound
	}
	return out, nil
}

func telegramSupportReplay(ctx context.Context, tx *sql.Tx, eventID, fp, kind string) (TelegramTicketReceiptV1, bool, error) {
	var actual, actualKind, store, ticketID string
	var actor int64
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT fingerprint,kind,store_id,ticket_id,actor_user_id,receipt_json FROM telegram_support_events WHERE event_id=?`, eventID).Scan(&actual, &actualKind, &store, &ticketID, &actor, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return TelegramTicketReceiptV1{}, false, nil
	}
	if err != nil {
		return TelegramTicketReceiptV1{}, false, err
	}
	var receipt TelegramTicketReceiptV1
	if actual != fp || actualKind != kind || len(raw) > 48<<10 || json.Unmarshal(raw, &receipt) != nil {
		return receipt, false, ErrConflict
	}
	if err = telegramValidateSupportReceipt(ctx, tx, store, ticketID, actor, kind, receipt); err != nil {
		return TelegramTicketReceiptV1{}, false, err
	}
	receipt.Replayed = true
	return receipt, true, nil
}

func telegramValidateSupportReceipt(ctx context.Context, tx *sql.Tx, store, ticketID string, actor int64, kind string, r TelegramTicketReceiptV1) error {
	t, err := scanTelegramTicketV1(tx.QueryRowContext(ctx, `SELECT `+telegramTicketColumnsV1+` FROM telegram_support_tickets WHERE id=? AND store_id=?`, ticketID, store))
	if err != nil {
		return ErrUnavailable
	}
	if r.Replayed || r.Ticket.ID != t.ID || r.Ticket.StoreID != t.StoreID || r.Ticket.CustomerID != t.CustomerID || r.Ticket.ClientID != t.ClientID || r.Ticket.Subject != t.Subject || r.Ticket.CreatedUnix != t.CreatedUnix || r.Ticket.Revision < 1 || r.Ticket.Revision > t.Revision || r.Ticket.UpdatedUnix < t.CreatedUnix || r.Ticket.AssignedTelegramUserID < 0 {
		return ErrUnavailable
	}
	if kind == "state" {
		if r.Message != nil {
			return ErrUnavailable
		}
		return nil
	}
	if kind != "create" && kind != "reply" || r.Message == nil || r.Message.TicketID != ticketID || r.Message.UserID != actor {
		return ErrUnavailable
	}
	var actual TelegramTicketMessageV1
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT id,ticket_id,user_id,from_support,body,attachment_json,created_unix FROM telegram_support_messages WHERE id=? AND ticket_id=?`, r.Message.ID, ticketID).Scan(&actual.ID, &actual.TicketID, &actual.UserID, &actual.FromSupport, &actual.Text, &raw, &actual.CreatedUnix)
	if err != nil || json.Unmarshal(raw, &actual.Attachment) != nil || fingerprint("telegram-support-message", actual) != fingerprint("telegram-support-message", *r.Message) {
		return ErrUnavailable
	}
	return nil
}

func telegramRecordSupportEvent(ctx context.Context, tx *sql.Tx, actor TelegramActor, eventID, fp, kind string, receipt TelegramTicketReceiptV1) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_support_events(event_id,fingerprint,kind,store_id,actor_user_id,ticket_id,receipt_json,created_unix) VALUES(?,?,?,?,?,?,?,?)`,
		eventID, fp, kind, receipt.Ticket.StoreID, actor.UserID, receipt.Ticket.ID, raw, time.Now().UTC().Unix())
	return err
}

func telegramInsertTicketMessage(ctx context.Context, tx *sql.Tx, actor TelegramActor, ticketID, text string, attachment *TelegramAttachmentV1, fromSupport bool, now int64) (TelegramTicketMessageV1, error) {
	id, err := telegramOpaqueID()
	if err != nil {
		return TelegramTicketMessageV1{}, err
	}
	raw, err := json.Marshal(attachment)
	if err != nil {
		return TelegramTicketMessageV1{}, err
	}
	message := TelegramTicketMessageV1{ID: id, TicketID: ticketID, UserID: actor.UserID, FromSupport: fromSupport, Text: text, Attachment: attachment, CreatedUnix: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_support_messages(id,ticket_id,user_id,from_support,body,attachment_json,created_unix) VALUES(?,?,?,?,?,?,?)`, id, ticketID, actor.UserID, fromSupport, text, string(raw), now)
	return message, err
}

func (s *Store) TelegramCreateTicketV1(ctx context.Context, actor TelegramActor, req TelegramTicketCreateV1) (TelegramTicketReceiptV1, error) {
	var out TelegramTicketReceiptV1
	req.Subject, req.Text = strings.TrimSpace(req.Subject), strings.TrimSpace(req.Text)
	if !eventRE.MatchString(req.EventID) || !validName(req.Subject, 120) || !telegramValidText(req.Text, 4096, req.Attachment != nil) || !telegramValidAttachment(req.Attachment) || req.ClientID != "" && !idRE.MatchString(req.ClientID) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := telegramSupportAuthorized(ctx, tx, actor)
	if err != nil {
		return out, err
	}
	if req.ClientID != "" {
		if err = telegramCustomerOwnsClientTx(ctx, tx, access, req.ClientID); err != nil {
			return out, err
		}
	}
	fp := fingerprint("telegram-ticket-create", struct {
		Actor   TelegramActor
		Request TelegramTicketCreateV1
	}{actor, req})
	if receipt, found, replayErr := telegramSupportReplay(ctx, tx, req.EventID, fp, "create"); found || replayErr != nil {
		return receipt, replayErr
	}
	id, err := telegramOpaqueID()
	if err != nil {
		return out, err
	}
	now := time.Now().UTC().Unix()
	ticket := TelegramTicketV1{ID: id, StoreID: access.Store.ID, CustomerID: access.CustomerID, ClientID: req.ClientID, Subject: req.Subject, Status: "waiting_support", Revision: 1, CreatedUnix: now, UpdatedUnix: now}
	var client any
	if req.ClientID != "" {
		client = req.ClientID
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_support_tickets(id,store_id,customer_id,client_id,subject,status,assigned_user_id,revision,created_unix,updated_unix) VALUES(?,?,?,?,?,?,0,1,?,?)`,
		id, ticket.StoreID, ticket.CustomerID, client, ticket.Subject, ticket.Status, now, now); err != nil {
		return out, err
	}
	message, err := telegramInsertTicketMessage(ctx, tx, actor, id, req.Text, req.Attachment, false, now)
	if err != nil {
		return out, err
	}
	out = TelegramTicketReceiptV1{Ticket: ticket, Message: &message}
	if err = telegramRecordSupportEvent(ctx, tx, actor, req.EventID, fp, "create", out); err != nil {
		return TelegramTicketReceiptV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramTicketReceiptV1{}, err
	}
	return out, nil
}

func (s *Store) TelegramReplyTicketV1(ctx context.Context, actor TelegramActor, req TelegramTicketReplyV1) (TelegramTicketReceiptV1, error) {
	var out TelegramTicketReceiptV1
	req.Text = strings.TrimSpace(req.Text)
	if !eventRE.MatchString(req.EventID) || !idRE.MatchString(req.TicketID) || !telegramValidText(req.Text, 4096, req.Attachment != nil) || !telegramValidAttachment(req.Attachment) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := telegramSupportAuthorized(ctx, tx, actor)
	if err != nil {
		return out, err
	}
	ticket, err := telegramReadTicketTx(ctx, tx, access, req.TicketID)
	if err != nil {
		return out, err
	}
	fp := fingerprint("telegram-ticket-reply", struct {
		Actor   TelegramActor
		Request TelegramTicketReplyV1
	}{actor, req})
	if receipt, found, replayErr := telegramSupportReplay(ctx, tx, req.EventID, fp, "reply"); found || replayErr != nil {
		return receipt, replayErr
	}
	if ticket.Status == "closed" {
		return out, ErrConflict
	}
	fromSupport := ticket.CustomerID != access.CustomerID
	now := time.Now().UTC().Unix()
	message, err := telegramInsertTicketMessage(ctx, tx, actor, ticket.ID, req.Text, req.Attachment, fromSupport, now)
	if err != nil {
		return out, err
	}
	ticket.Status = "waiting_support"
	if fromSupport {
		ticket.Status = "waiting_customer"
	}
	ticket.Revision++
	ticket.UpdatedUnix = now
	if _, err = tx.ExecContext(ctx, `UPDATE telegram_support_tickets SET status=?,revision=?,updated_unix=? WHERE id=? AND store_id=?`, ticket.Status, ticket.Revision, now, ticket.ID, access.Store.ID); err != nil {
		return out, err
	}
	out = TelegramTicketReceiptV1{Ticket: ticket, Message: &message}
	if err = telegramRecordSupportEvent(ctx, tx, actor, req.EventID, fp, "reply", out); err != nil {
		return TelegramTicketReceiptV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramTicketReceiptV1{}, err
	}
	return out, nil
}

func (s *Store) TelegramSetTicketStateV1(ctx context.Context, actor TelegramActor, req TelegramTicketChangeV1) (TelegramTicketReceiptV1, error) {
	var out TelegramTicketReceiptV1
	if !eventRE.MatchString(req.EventID) || !idRE.MatchString(req.TicketID) || req.ExpectedRevision < 1 || req.AssignedTelegramUserID < 0 || req.Status != "open" && req.Status != "closed" && req.Status != "waiting_support" && req.Status != "waiting_customer" {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := telegramSupportAuthorized(ctx, tx, actor)
	if err != nil {
		return out, err
	}
	ticket, err := telegramReadTicketTx(ctx, tx, access, req.TicketID)
	if err != nil {
		return out, err
	}
	isSupport := telegramAccessHasPermissionV1(access, "support")
	if !isSupport && (req.Status != "open" && req.Status != "closed" || req.AssignedTelegramUserID != ticket.AssignedTelegramUserID) {
		return out, ErrOwnershipDenied
	}
	if req.AssignedTelegramUserID > 0 && req.AssignedTelegramUserID != ticket.AssignedTelegramUserID {
		assignee, e := authorizeTelegramTx(ctx, tx, TelegramActor{BotID: actor.BotID, UserID: req.AssignedTelegramUserID, ChatID: req.AssignedTelegramUserID}, "support")
		if e != nil || assignee.Store.ID != access.Store.ID {
			return out, ErrOwnershipDenied
		}
	}
	fp := fingerprint("telegram-ticket-state", struct {
		Actor   TelegramActor
		Request TelegramTicketChangeV1
	}{actor, req})
	if receipt, found, replayErr := telegramSupportReplay(ctx, tx, req.EventID, fp, "state"); found || replayErr != nil {
		return receipt, replayErr
	}
	if ticket.Revision != req.ExpectedRevision {
		return out, ErrConflict
	}
	ticket.Status = req.Status
	ticket.AssignedTelegramUserID = req.AssignedTelegramUserID
	ticket.Revision++
	ticket.UpdatedUnix = time.Now().UTC().Unix()
	if _, err = tx.ExecContext(ctx, `UPDATE telegram_support_tickets SET status=?,assigned_user_id=?,revision=?,updated_unix=? WHERE id=? AND store_id=?`, ticket.Status, ticket.AssignedTelegramUserID, ticket.Revision, ticket.UpdatedUnix, ticket.ID, ticket.StoreID); err != nil {
		return out, err
	}
	out.Ticket = ticket
	if err = telegramRecordSupportEvent(ctx, tx, actor, req.EventID, fp, "state", out); err != nil {
		return TelegramTicketReceiptV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramTicketReceiptV1{}, err
	}
	return out, nil
}

func telegramPageV1(limit, offset int) (int, int, error) {
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1_000_000 {
		return 0, 0, ErrInvalid
	}
	return limit, offset, nil
}

func (s *Store) TelegramTicketsV1(ctx context.Context, actor TelegramActor, filter TelegramTicketFilterV1) ([]TelegramTicketV1, error) {
	limit, offset, err := telegramPageV1(filter.Limit, filter.Offset)
	if err != nil || filter.Status != "" && filter.Status != "open" && filter.Status != "closed" && filter.Status != "waiting_support" && filter.Status != "waiting_customer" {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	access, err := telegramSupportAuthorized(ctx, tx, actor)
	if err != nil {
		return nil, err
	}
	query := `SELECT ` + telegramTicketColumnsV1 + ` FROM telegram_support_tickets WHERE store_id=?`
	args := []any{access.Store.ID}
	if filter.Own || !telegramAccessHasPermissionV1(access, "support") {
		query += ` AND customer_id=?`
		args = append(args, access.CustomerID)
	}
	if filter.Status != "" {
		query += ` AND status=?`
		args = append(args, filter.Status)
	}
	query += ` ORDER BY updated_unix DESC,id LIMIT ? OFFSET ?`
	args = append(args, limit, offset)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TelegramTicketV1, 0)
	for rows.Next() {
		ticket, e := scanTelegramTicketV1(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, ticket)
	}
	return out, rows.Err()
}

func (s *Store) TelegramTicketMessagesV1(ctx context.Context, actor TelegramActor, ticketID string, limit, offset int) ([]TelegramTicketMessageV1, error) {
	limit, offset, err := telegramPageV1(limit, offset)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	access, err := telegramSupportAuthorized(ctx, tx, actor)
	if err != nil {
		return nil, err
	}
	if _, err = telegramReadTicketTx(ctx, tx, access, ticketID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,ticket_id,user_id,from_support,body,attachment_json,created_unix FROM telegram_support_messages WHERE ticket_id=? ORDER BY rowid LIMIT ? OFFSET ?`, ticketID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TelegramTicketMessageV1, 0)
	for rows.Next() {
		var m TelegramTicketMessageV1
		var attachment string
		if err = rows.Scan(&m.ID, &m.TicketID, &m.UserID, &m.FromSupport, &m.Text, &attachment, &m.CreatedUnix); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(attachment), &m.Attachment) != nil || !telegramValidAttachment(m.Attachment) {
			return nil, ErrUnavailable
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TelegramTicketByIDV1 reads the current revision for a detail view or a
// conditional update, with the same tenant/customer boundary as the thread.
func (s *Store) TelegramTicketByIDV1(ctx context.Context, actor TelegramActor, ticketID string) (TelegramTicketV1, error) {
	if !idRE.MatchString(ticketID) {
		return TelegramTicketV1{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramTicketV1{}, err
	}
	defer tx.Rollback()
	access, err := telegramSupportAuthorized(ctx, tx, actor)
	if err != nil {
		return TelegramTicketV1{}, err
	}
	return telegramReadTicketTx(ctx, tx, access, ticketID)
}
