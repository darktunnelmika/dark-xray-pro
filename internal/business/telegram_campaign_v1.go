package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Campaigns store a consented audience snapshot and dispatch journal in the
// same private business DB. A dispatched request with an unknown outcome is
// never retried automatically: Telegram sendMessage has no idempotency key.
type TelegramCampaignButtonV1 struct {
	Text  string `json:"text"`
	URL   string `json:"url"`
	Style string `json:"style,omitempty"`
}

type TelegramCampaignContentV1 struct {
	Text        string                       `json:"text"`
	PhotoFileID string                       `json:"photo_file_id,omitempty"`
	Buttons     [][]TelegramCampaignButtonV1 `json:"buttons,omitempty"`
}

type TelegramCampaignV1 struct {
	ID             string                    `json:"id"`
	StoreID        string                    `json:"store_id"`
	Title          string                    `json:"title"`
	Content        TelegramCampaignContentV1 `json:"content"`
	Audience       string                    `json:"audience"`
	ExpiringDays   int64                     `json:"expiring_days,omitempty"`
	ScheduledUnix  int64                     `json:"scheduled_unix"`
	Revision       int64                     `json:"revision"`
	State          string                    `json:"state"`
	CreatorUserID  int64                     `json:"creator_user_id"`
	ApproverUserID int64                     `json:"approver_user_id,omitempty"`
	ApprovalDigest string                    `json:"approval_digest,omitempty"`
	RecipientCount int64                     `json:"recipient_count"`
	CreatedUnix    int64                     `json:"created_unix"`
	UpdatedUnix    int64                     `json:"updated_unix"`
}

type TelegramCampaignSaveV1 struct {
	EventID          string                    `json:"event_id"`
	ID               string                    `json:"id,omitempty"`
	ExpectedRevision int64                     `json:"expected_revision"`
	Title            string                    `json:"title"`
	Content          TelegramCampaignContentV1 `json:"content"`
	Audience         string                    `json:"audience"`
	ExpiringDays     int64                     `json:"expiring_days,omitempty"`
	ScheduledUnix    int64                     `json:"scheduled_unix"`
}

type TelegramCampaignPreviewV1 struct {
	ID             string             `json:"id"`
	Campaign       TelegramCampaignV1 `json:"campaign"`
	Digest         string             `json:"digest"`
	RecipientCount int64              `json:"recipient_count"`
	ExpiresUnix    int64              `json:"expires_unix"`
}

type TelegramCampaignApprovalV1 struct {
	EventID          string `json:"event_id"`
	CampaignID       string `json:"campaign_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	PreviewID        string `json:"preview_id"`
	Digest           string `json:"digest"`
}

type TelegramCampaignControlV1 struct {
	EventID          string `json:"event_id"`
	CampaignID       string `json:"campaign_id"`
	ExpectedRevision int64  `json:"expected_revision"`
	Action           string `json:"action"`
	ApprovalDigest   string `json:"approval_digest,omitempty"`
}

type TelegramCampaignReceiptV1 struct {
	Campaign TelegramCampaignV1 `json:"campaign"`
	Replayed bool               `json:"replayed"`
}

type TelegramCampaignReportV1 struct {
	Campaign    TelegramCampaignV1 `json:"campaign"`
	Pending     int64              `json:"pending"`
	Dispatching int64              `json:"dispatching"`
	Sent        int64              `json:"sent"`
	Failed      int64              `json:"failed"`
	Unknown     int64              `json:"unknown"`
	Skipped     int64              `json:"skipped"`
	Cancelled   int64              `json:"cancelled"`
}

type TelegramCampaignDeliveryV1 struct {
	DeliveryID     string                    `json:"delivery_id"`
	AttemptID      string                    `json:"attempt_id"`
	CampaignID     string                    `json:"campaign_id"`
	StoreID        string                    `json:"store_id"`
	BotID          int64                     `json:"bot_id"`
	ChatID         int64                     `json:"chat_id"`
	Content        TelegramCampaignContentV1 `json:"content"`
	LeaseUntilUnix int64                     `json:"lease_until_unix"`
}

type TelegramCampaignDeliveryResultV1 struct {
	DeliveryID        string `json:"delivery_id"`
	AttemptID         string `json:"attempt_id"`
	Outcome           string `json:"outcome"`
	MessageID         int64  `json:"message_id,omitempty"`
	ErrorCode         int    `json:"error_code,omitempty"`
	RetryAfterSeconds int64  `json:"retry_after_seconds,omitempty"`
}

type TelegramMarketingConsentV1 struct {
	StoreID     string `json:"store_id"`
	CustomerID  string `json:"customer_id"`
	Enabled     bool   `json:"enabled"`
	Revision    int64  `json:"revision"`
	UpdatedUnix int64  `json:"updated_unix"`
}

var telegramCampaignSchemaV1 = []string{
	`CREATE TABLE telegram_campaign_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`CREATE TABLE telegram_campaign_consent(customer_id TEXT PRIMARY KEY REFERENCES telegram_commerce_customers(customer_id),store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),enabled INTEGER NOT NULL CHECK(enabled IN(0,1)),revision INTEGER NOT NULL CHECK(revision>0),updated_unix INTEGER NOT NULL)`,
	`CREATE TABLE telegram_campaign_drafts(
 id TEXT PRIMARY KEY,store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),title TEXT NOT NULL,content_json BLOB NOT NULL,
 audience TEXT NOT NULL CHECK(audience IN('all','active','expired','expiring')),expiring_days INTEGER NOT NULL CHECK(expiring_days BETWEEN 0 AND 30),scheduled_unix INTEGER NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0),state TEXT NOT NULL CHECK(state IN('draft','queued','paused','completed','cancelled')),
 creator_user_id INTEGER NOT NULL CHECK(creator_user_id>0),approver_user_id INTEGER NOT NULL DEFAULT 0 CHECK(approver_user_id>=0),
 approval_digest TEXT NOT NULL DEFAULT '',recipient_count INTEGER NOT NULL DEFAULT 0 CHECK(recipient_count>=0),created_unix INTEGER NOT NULL,updated_unix INTEGER NOT NULL)`,
	`CREATE INDEX telegram_campaign_store ON telegram_campaign_drafts(store_id,created_unix DESC,id)`,
	`CREATE TABLE telegram_campaign_previews(
 id TEXT PRIMARY KEY,campaign_id TEXT NOT NULL REFERENCES telegram_campaign_drafts(id),revision INTEGER NOT NULL CHECK(revision>0),
 digest TEXT NOT NULL CHECK(length(digest)=64),audience_digest TEXT NOT NULL CHECK(length(audience_digest)=64),recipient_count INTEGER NOT NULL CHECK(recipient_count>=0),selection_unix INTEGER NOT NULL,expires_unix INTEGER NOT NULL)`,
	`CREATE TABLE telegram_campaign_deliveries(
 id TEXT PRIMARY KEY,campaign_id TEXT NOT NULL REFERENCES telegram_campaign_drafts(id),customer_id TEXT NOT NULL REFERENCES telegram_commerce_customers(customer_id),
 bot_id INTEGER NOT NULL CHECK(bot_id>0),chat_id INTEGER NOT NULL CHECK(chat_id>0),consent_revision INTEGER NOT NULL CHECK(consent_revision>0),
 state TEXT NOT NULL CHECK(state IN('pending','dispatching','sent','permanent_failure','unknown','skipped','cancelled')),
 attempt_id TEXT NOT NULL DEFAULT '',attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count BETWEEN 0 AND 8),
 next_attempt_unix INTEGER NOT NULL,lease_until_unix INTEGER NOT NULL DEFAULT 0,message_id INTEGER NOT NULL DEFAULT 0,last_error_code INTEGER NOT NULL DEFAULT 0,updated_unix INTEGER NOT NULL,
 UNIQUE(campaign_id,customer_id))`,
	`CREATE INDEX telegram_campaign_dispatch_queue ON telegram_campaign_deliveries(bot_id,state,next_attempt_unix,campaign_id,id)`,
	`CREATE TRIGGER telegram_campaign_deliveries_immutable_target BEFORE UPDATE OF id,campaign_id,customer_id,bot_id,chat_id,consent_revision ON telegram_campaign_deliveries BEGIN SELECT RAISE(ABORT,'approved campaign audience is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_deliveries_no_delete BEFORE DELETE ON telegram_campaign_deliveries BEGIN SELECT RAISE(ABORT,'campaign delivery history is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_drafts_approved_content BEFORE UPDATE OF id,store_id,title,content_json,audience,expiring_days,scheduled_unix,creator_user_id,created_unix ON telegram_campaign_drafts WHEN OLD.state!='draft' BEGIN SELECT RAISE(ABORT,'approved campaign content is immutable'); END`,
	`CREATE TABLE telegram_campaign_attempts(attempt_id TEXT PRIMARY KEY,delivery_id TEXT NOT NULL REFERENCES telegram_campaign_deliveries(id),started_unix INTEGER NOT NULL,lease_until_unix INTEGER NOT NULL)`,
	`CREATE TABLE telegram_campaign_outcomes(attempt_id TEXT PRIMARY KEY REFERENCES telegram_campaign_attempts(attempt_id),outcome TEXT NOT NULL CHECK(outcome IN('sent','permanent_failure','rate_limited','unknown')),message_id INTEGER NOT NULL,error_code INTEGER NOT NULL,retry_after_seconds INTEGER NOT NULL,finished_unix INTEGER NOT NULL)`,
	`CREATE TABLE telegram_campaign_pacing(bot_id INTEGER NOT NULL,chat_id INTEGER NOT NULL,next_unix_nano INTEGER NOT NULL,PRIMARY KEY(bot_id,chat_id))`,
	`CREATE TABLE telegram_campaign_events(event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),kind TEXT NOT NULL,store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),actor_user_id INTEGER NOT NULL CHECK(actor_user_id>0),object_id TEXT NOT NULL,receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)`,
	`CREATE TRIGGER telegram_campaign_events_no_update BEFORE UPDATE ON telegram_campaign_events BEGIN SELECT RAISE(ABORT,'campaign audit is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_events_no_delete BEFORE DELETE ON telegram_campaign_events BEGIN SELECT RAISE(ABORT,'campaign audit is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_attempts_no_update BEFORE UPDATE ON telegram_campaign_attempts BEGIN SELECT RAISE(ABORT,'campaign dispatch history is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_attempts_no_delete BEFORE DELETE ON telegram_campaign_attempts BEGIN SELECT RAISE(ABORT,'campaign dispatch history is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_outcomes_no_update BEFORE UPDATE ON telegram_campaign_outcomes BEGIN SELECT RAISE(ABORT,'campaign outcomes are immutable'); END`,
	`CREATE TRIGGER telegram_campaign_outcomes_no_delete BEFORE DELETE ON telegram_campaign_outcomes BEGIN SELECT RAISE(ABORT,'campaign outcomes are immutable'); END`,
	`CREATE TRIGGER telegram_campaign_previews_no_update BEFORE UPDATE ON telegram_campaign_previews BEGIN SELECT RAISE(ABORT,'campaign preview is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_previews_no_delete BEFORE DELETE ON telegram_campaign_previews BEGIN SELECT RAISE(ABORT,'campaign preview is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_meta_no_update BEFORE UPDATE ON telegram_campaign_meta BEGIN SELECT RAISE(ABORT,'campaign schema is immutable'); END`,
	`CREATE TRIGGER telegram_campaign_meta_no_delete BEFORE DELETE ON telegram_campaign_meta BEGIN SELECT RAISE(ABORT,'campaign schema is immutable'); END`,
}

func (s *Store) EnableTelegramCampaignsV1(ctx context.Context) error {
	return s.enableTelegramExtension(ctx, "telegram_campaign_", telegramCampaignSchemaV1)
}

func (s *Store) VerifyTelegramCampaignsV1(ctx context.Context) error {
	return s.verifyTelegramExtension(ctx, "telegram_campaign_", telegramCampaignSchemaV1, validateTelegramCampaignRowsV1)
}

func telegramCampaignAccess(ctx context.Context, tx *sql.Tx, actor TelegramActor, permission string) (TelegramAccess, error) {
	if err := telegramExtensionReady(ctx, tx, "telegram_campaign_"); err != nil {
		return TelegramAccess{}, err
	}
	access, err := authorizeTelegramTx(ctx, tx, actor, permission)
	if err == nil && permission == "" && !telegramAccessHasPermissionV1(access, "broadcast_draft") && !telegramAccessHasPermissionV1(access, "broadcast_send") {
		return TelegramAccess{}, ErrOwnershipDenied
	}
	return access, err
}

func telegramValidCampaignContent(c TelegramCampaignContentV1) bool {
	limit := 4096
	if c.PhotoFileID != "" {
		limit = 1024
		if !telegramValidAttachment(&TelegramAttachmentV1{Kind: "photo", FileID: c.PhotoFileID}) {
			return false
		}
	}
	if !telegramValidText(c.Text, limit, c.PhotoFileID != "") || len(c.Buttons) > 8 {
		return false
	}
	for _, row := range c.Buttons {
		if len(row) < 1 || len(row) > 4 {
			return false
		}
		for _, b := range row {
			if !validName(b.Text, 64) || len(b.URL) > 2048 || b.Style != "" && b.Style != "primary" && b.Style != "success" && b.Style != "danger" {
				return false
			}
			u, err := url.Parse(b.URL)
			if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || strings.ContainsAny(b.URL, "\r\n\t") {
				return false
			}
		}
	}
	return true
}

const telegramCampaignColumnsV1 = `id,store_id,title,content_json,audience,expiring_days,scheduled_unix,revision,state,creator_user_id,approver_user_id,approval_digest,recipient_count,created_unix,updated_unix`

func scanTelegramCampaignV1(row interface{ Scan(...any) error }) (TelegramCampaignV1, error) {
	var c TelegramCampaignV1
	var raw []byte
	err := row.Scan(&c.ID, &c.StoreID, &c.Title, &raw, &c.Audience, &c.ExpiringDays, &c.ScheduledUnix, &c.Revision, &c.State, &c.CreatorUserID, &c.ApproverUserID, &c.ApprovalDigest, &c.RecipientCount, &c.CreatedUnix, &c.UpdatedUnix)
	if err != nil {
		return c, err
	}
	if len(raw) > 32<<10 || json.Unmarshal(raw, &c.Content) != nil || !telegramValidCampaignContent(c.Content) {
		return TelegramCampaignV1{}, ErrUnavailable
	}
	return c, nil
}

func telegramReadCampaignTx(ctx context.Context, tx *sql.Tx, store, id string) (TelegramCampaignV1, error) {
	if !idRE.MatchString(id) {
		return TelegramCampaignV1{}, ErrInvalid
	}
	c, err := scanTelegramCampaignV1(tx.QueryRowContext(ctx, `SELECT `+telegramCampaignColumnsV1+` FROM telegram_campaign_drafts WHERE id=? AND store_id=?`, id, store))
	if errors.Is(err, sql.ErrNoRows) {
		return TelegramCampaignV1{}, ErrNotFound
	}
	return c, err
}

func telegramCampaignEventReplay(ctx context.Context, tx *sql.Tx, eventID, fp, kind string, out any) (bool, error) {
	var actual, actualKind, store, object string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT fingerprint,kind,store_id,object_id,receipt_json FROM telegram_campaign_events WHERE event_id=?`, eventID).Scan(&actual, &actualKind, &store, &object, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if actual != fp || actualKind != kind || len(raw) > 48<<10 || json.Unmarshal(raw, out) != nil {
		return false, ErrConflict
	}
	switch r := out.(type) {
	case *TelegramMarketingConsentV1:
		var revision int64
		if kind != "consent" || r.StoreID != store || r.CustomerID != object || r.Revision < 1 || r.UpdatedUnix <= 0 {
			return false, ErrUnavailable
		}
		if err = tx.QueryRowContext(ctx, `SELECT revision FROM telegram_campaign_consent WHERE store_id=? AND customer_id=?`, store, object).Scan(&revision); err != nil || r.Revision > revision {
			return false, ErrUnavailable
		}
	case *TelegramCampaignReceiptV1:
		c, err := telegramReadCampaignTx(ctx, tx, store, object)
		if err != nil || r.Replayed || kind != "save" && kind != "approve" && kind != "control" || r.Campaign.ID != c.ID || r.Campaign.StoreID != c.StoreID || r.Campaign.CreatorUserID != c.CreatorUserID || r.Campaign.CreatedUnix != c.CreatedUnix || r.Campaign.Revision < 1 || r.Campaign.Revision > c.Revision || !validName(r.Campaign.Title, 120) || !telegramValidCampaignContent(r.Campaign.Content) {
			return false, ErrUnavailable
		}
	default:
		return false, ErrUnavailable
	}
	return true, nil
}

func telegramRecordCampaignEvent(ctx context.Context, tx *sql.Tx, actor TelegramActor, store, eventID, fp, kind, object string, out any) error {
	raw, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_campaign_events(event_id,fingerprint,kind,store_id,actor_user_id,object_id,receipt_json,created_unix) VALUES(?,?,?,?,?,?,?,?)`, eventID, fp, kind, store, actor.UserID, object, raw, time.Now().UTC().Unix())
	return err
}

func (s *Store) TelegramSetMarketingConsentV1(ctx context.Context, actor TelegramActor, eventID string, enabled bool) (TelegramMarketingConsentV1, error) {
	var out TelegramMarketingConsentV1
	if !eventRE.MatchString(eventID) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_campaign_"); err != nil {
		return out, err
	}
	a, err := authorizeTelegramTx(ctx, tx, actor, "")
	if err != nil {
		return out, err
	}
	fp := fingerprint("telegram-consent", struct {
		Actor   TelegramActor
		Enabled bool
	}{actor, enabled})
	if found, e := telegramCampaignEventReplay(ctx, tx, eventID, fp, "consent", &out); found || e != nil {
		return out, e
	}
	var revision int64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM telegram_campaign_consent WHERE customer_id=? AND store_id=?`, a.CustomerID, a.Store.ID).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	out = TelegramMarketingConsentV1{StoreID: a.Store.ID, CustomerID: a.CustomerID, Enabled: enabled, Revision: revision + 1, UpdatedUnix: time.Now().UTC().Unix()}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_campaign_consent(customer_id,store_id,enabled,revision,updated_unix) VALUES(?,?,?,?,?) ON CONFLICT(customer_id) DO UPDATE SET enabled=excluded.enabled,revision=excluded.revision,updated_unix=excluded.updated_unix`, out.CustomerID, out.StoreID, out.Enabled, out.Revision, out.UpdatedUnix)
	if err != nil {
		return TelegramMarketingConsentV1{}, err
	}
	if err = telegramRecordCampaignEvent(ctx, tx, actor, a.Store.ID, eventID, fp, "consent", a.CustomerID, out); err != nil {
		return TelegramMarketingConsentV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramMarketingConsentV1{}, err
	}
	return out, nil
}

func (s *Store) TelegramMarketingConsentV1(ctx context.Context, actor TelegramActor) (TelegramMarketingConsentV1, error) {
	var out TelegramMarketingConsentV1
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_campaign_"); err != nil {
		return out, err
	}
	a, err := authorizeTelegramTx(ctx, tx, actor, "")
	if err != nil {
		return out, err
	}
	out.StoreID, out.CustomerID = a.Store.ID, a.CustomerID
	err = tx.QueryRowContext(ctx, `SELECT enabled,revision,updated_unix FROM telegram_campaign_consent WHERE customer_id=? AND store_id=?`, a.CustomerID, a.Store.ID).Scan(&out.Enabled, &out.Revision, &out.UpdatedUnix)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

func (s *Store) TelegramSaveCampaignV1(ctx context.Context, actor TelegramActor, req TelegramCampaignSaveV1) (TelegramCampaignReceiptV1, error) {
	var out TelegramCampaignReceiptV1
	req.Title = strings.TrimSpace(req.Title)
	if !eventRE.MatchString(req.EventID) || !validName(req.Title, 120) || !telegramValidCampaignContent(req.Content) || req.ID != "" && !idRE.MatchString(req.ID) || req.ExpectedRevision < 0 || req.ScheduledUnix < 0 || req.ScheduledUnix > time.Now().UTC().Add(366*24*time.Hour).Unix() {
		return out, ErrInvalid
	}
	if req.Audience != "all" && req.Audience != "active" && req.Audience != "expired" && req.Audience != "expiring" {
		return out, ErrInvalid
	}
	if req.Audience == "expiring" && (req.ExpiringDays < 1 || req.ExpiringDays > 30) || req.Audience != "expiring" && req.ExpiringDays != 0 {
		return out, ErrInvalid
	}
	if req.ID == "" && req.ExpectedRevision != 0 || req.ID != "" && req.ExpectedRevision < 1 {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	a, err := telegramCampaignAccess(ctx, tx, actor, "broadcast_draft")
	if err != nil {
		return out, err
	}
	fp := fingerprint("telegram-campaign-save", struct {
		Actor   TelegramActor
		Request TelegramCampaignSaveV1
	}{actor, req})
	if found, e := telegramCampaignEventReplay(ctx, tx, req.EventID, fp, "save", &out); found || e != nil {
		out.Replayed = found
		return out, e
	}
	now := time.Now().UTC().Unix()
	var c TelegramCampaignV1
	if req.ID != "" {
		c, err = telegramReadCampaignTx(ctx, tx, a.Store.ID, req.ID)
		if err != nil {
			return out, err
		}
		if c.State != "draft" || c.Revision != req.ExpectedRevision {
			return out, ErrConflict
		}
	} else {
		c.ID, err = telegramOpaqueID()
		if err != nil {
			return out, err
		}
		c.StoreID, c.State, c.CreatorUserID, c.CreatedUnix = a.Store.ID, "draft", actor.UserID, now
	}
	c.Title, c.Content, c.Audience, c.ExpiringDays, c.ScheduledUnix = req.Title, req.Content, req.Audience, req.ExpiringDays, req.ScheduledUnix
	c.Revision++
	c.UpdatedUnix = now
	raw, err := json.Marshal(c.Content)
	if err != nil {
		return out, err
	}
	if req.ID == "" {
		_, err = tx.ExecContext(ctx, `INSERT INTO telegram_campaign_drafts(id,store_id,title,content_json,audience,expiring_days,scheduled_unix,revision,state,creator_user_id,created_unix,updated_unix) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.StoreID, c.Title, raw, c.Audience, c.ExpiringDays, c.ScheduledUnix, c.Revision, c.State, c.CreatorUserID, c.CreatedUnix, c.UpdatedUnix)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE telegram_campaign_drafts SET title=?,content_json=?,audience=?,expiring_days=?,scheduled_unix=?,revision=?,updated_unix=? WHERE id=? AND store_id=?`, c.Title, raw, c.Audience, c.ExpiringDays, c.ScheduledUnix, c.Revision, c.UpdatedUnix, c.ID, c.StoreID)
	}
	if err != nil {
		return out, err
	}
	out.Campaign = c
	if err = telegramRecordCampaignEvent(ctx, tx, actor, a.Store.ID, req.EventID, fp, "save", c.ID, out); err != nil {
		return TelegramCampaignReceiptV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramCampaignReceiptV1{}, err
	}
	return out, nil
}

func telegramCampaignAudienceSQL(c TelegramCampaignV1, at int64) (string, []any) {
	query := `SELECT c.customer_id,c.telegram_user_id,c.private_chat_id,m.revision FROM telegram_commerce_customers c JOIN telegram_campaign_consent m ON m.customer_id=c.customer_id AND m.store_id=c.store_id WHERE c.store_id=? AND m.enabled=1 AND c.private_chat_id=c.telegram_user_id AND c.telegram_user_id>0`
	args := []any{c.StoreID}
	if c.Audience != "all" {
		query += ` AND EXISTS(SELECT 1 FROM telegram_commerce_customer_clients cc JOIN clients cl ON cl.id=cc.client_id WHERE cc.customer_id=c.customer_id AND cc.store_id=c.store_id AND cl.archived=0`
		switch c.Audience {
		case "active":
			query += ` AND cl.deployment_state!='disabled' AND cl.expires_unix>?`
			args = append(args, at)
		case "expired":
			query += ` AND cl.expires_unix<=?`
			args = append(args, at)
		case "expiring":
			query += ` AND cl.deployment_state!='disabled' AND cl.expires_unix>? AND cl.expires_unix<=?`
			args = append(args, at, at+c.ExpiringDays*86400)
		}
		query += `)`
	}
	return query + ` ORDER BY c.customer_id`, args
}

func telegramCampaignAudienceDigest(ctx context.Context, tx *sql.Tx, c TelegramCampaignV1, at int64) (string, int64, error) {
	query, args := telegramCampaignAudienceSQL(c, at)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	h := sha256.New()
	var count int64
	for rows.Next() {
		var customer string
		var user, chat, revision int64
		if err = rows.Scan(&customer, &user, &chat, &revision); err != nil {
			return "", 0, err
		}
		if !idRE.MatchString(customer) || user <= 0 || chat != user || revision < 1 {
			return "", 0, ErrUnavailable
		}
		fmt.Fprintf(h, "%s:%d:%d:%d\n", customer, user, chat, revision)
		count++
	}
	if err = rows.Err(); err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), count, nil
}

func telegramCampaignApprovalDigest(c TelegramCampaignV1, audience string, count int64) string {
	return fingerprint("telegram-campaign-approval", struct {
		ID, StoreID, Title                    string
		Content                               TelegramCampaignContentV1
		Audience                              string
		ExpiringDays, ScheduledUnix, Revision int64
		AudienceDigest                        string
		Count                                 int64
	}{c.ID, c.StoreID, c.Title, c.Content, c.Audience, c.ExpiringDays, c.ScheduledUnix, c.Revision, audience, count})
}

func telegramCampaignApprovalValidTx(ctx context.Context, tx *sql.Tx, c TelegramCampaignV1) error {
	var revision, count int64
	var audience string
	if c.ApproverUserID <= 0 || len(c.ApprovalDigest) != 64 {
		return ErrUnavailable
	}
	if err := tx.QueryRowContext(ctx, `SELECT revision,audience_digest,recipient_count FROM telegram_campaign_previews WHERE campaign_id=? AND digest=? ORDER BY rowid DESC LIMIT 1`, c.ID, c.ApprovalDigest).Scan(&revision, &audience, &count); err != nil {
		return ErrUnavailable
	}
	approved := c
	approved.Revision = revision
	if count != c.RecipientCount || telegramCampaignApprovalDigest(approved, audience, count) != c.ApprovalDigest {
		return ErrUnavailable
	}
	return nil
}

func (s *Store) TelegramPreviewCampaignV1(ctx context.Context, actor TelegramActor, id string) (TelegramCampaignPreviewV1, error) {
	var out TelegramCampaignPreviewV1
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	a, err := telegramCampaignAccess(ctx, tx, actor, "")
	if err != nil {
		return out, err
	}
	c, err := telegramReadCampaignTx(ctx, tx, a.Store.ID, id)
	if err != nil {
		return out, err
	}
	if c.State != "draft" {
		return out, ErrConflict
	}
	now := time.Now().UTC().Unix()
	audience, count, err := telegramCampaignAudienceDigest(ctx, tx, c, now)
	if err != nil {
		return out, err
	}
	previewID, err := telegramOpaqueID()
	if err != nil {
		return out, err
	}
	out = TelegramCampaignPreviewV1{ID: previewID, Campaign: c, Digest: telegramCampaignApprovalDigest(c, audience, count), RecipientCount: count, ExpiresUnix: now + 600}
	_, err = tx.ExecContext(ctx, `INSERT INTO telegram_campaign_previews(id,campaign_id,revision,digest,audience_digest,recipient_count,selection_unix,expires_unix) VALUES(?,?,?,?,?,?,?,?)`, out.ID, c.ID, c.Revision, out.Digest, audience, count, now, out.ExpiresUnix)
	if err != nil {
		return TelegramCampaignPreviewV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramCampaignPreviewV1{}, err
	}
	return out, nil
}

func (s *Store) TelegramApproveCampaignV1(ctx context.Context, actor TelegramActor, req TelegramCampaignApprovalV1) (TelegramCampaignReceiptV1, error) {
	var out TelegramCampaignReceiptV1
	if !eventRE.MatchString(req.EventID) || !idRE.MatchString(req.CampaignID) || !idRE.MatchString(req.PreviewID) || req.ExpectedRevision < 1 || len(req.Digest) != 64 {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	a, err := telegramCampaignAccess(ctx, tx, actor, "broadcast_send")
	if err != nil {
		return out, err
	}
	c, err := telegramReadCampaignTx(ctx, tx, a.Store.ID, req.CampaignID)
	if err != nil {
		return out, err
	}
	fp := fingerprint("telegram-campaign-approve", struct {
		Actor   TelegramActor
		Request TelegramCampaignApprovalV1
	}{actor, req})
	if found, e := telegramCampaignEventReplay(ctx, tx, req.EventID, fp, "approve", &out); found || e != nil {
		out.Replayed = found
		return out, e
	}
	if c.State != "draft" || c.Revision != req.ExpectedRevision {
		return out, ErrConflict
	}
	var revision, count, selection, expires int64
	var digest, audience string
	err = tx.QueryRowContext(ctx, `SELECT revision,digest,audience_digest,recipient_count,selection_unix,expires_unix FROM telegram_campaign_previews WHERE id=? AND campaign_id=?`, req.PreviewID, c.ID).Scan(&revision, &digest, &audience, &count, &selection, &expires)
	now := time.Now().UTC().Unix()
	if err != nil || revision != c.Revision || digest != req.Digest || expires <= now || selection > now {
		return out, ErrConflict
	}
	currentAudience, currentCount, err := telegramCampaignAudienceDigest(ctx, tx, c, selection)
	if err != nil {
		return out, err
	}
	if currentAudience != audience || currentCount != count || telegramCampaignApprovalDigest(c, audience, count) != digest {
		return out, ErrConflict
	}
	query, args := telegramCampaignAudienceSQL(c, selection)
	// The snapshot is inserted within the same transaction as the audience
	// digest and approval; another connection cannot change it between steps.
	insert := `INSERT INTO telegram_campaign_deliveries(id,campaign_id,customer_id,bot_id,chat_id,consent_revision,state,next_attempt_unix,updated_unix) SELECT lower(hex(randomblob(16))),?,a.customer_id,?,a.private_chat_id,a.revision,'pending',?,? FROM (` + query + `) a`
	params := []any{c.ID, actor.BotID, now, now}
	params = append(params, args...)
	if _, err = tx.ExecContext(ctx, insert, params...); err != nil {
		return out, err
	}
	c.ApproverUserID, c.ApprovalDigest, c.RecipientCount = actor.UserID, digest, count
	c.State = "queued"
	if count == 0 {
		c.State = "completed"
	}
	c.Revision++
	c.UpdatedUnix = now
	if _, err = tx.ExecContext(ctx, `UPDATE telegram_campaign_drafts SET state=?,revision=?,approver_user_id=?,approval_digest=?,recipient_count=?,updated_unix=? WHERE id=?`, c.State, c.Revision, c.ApproverUserID, c.ApprovalDigest, c.RecipientCount, now, c.ID); err != nil {
		return out, err
	}
	out.Campaign = c
	if err = telegramRecordCampaignEvent(ctx, tx, actor, c.StoreID, req.EventID, fp, "approve", c.ID, out); err != nil {
		return TelegramCampaignReceiptV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramCampaignReceiptV1{}, err
	}
	return out, nil
}

func (s *Store) TelegramControlCampaignV1(ctx context.Context, actor TelegramActor, req TelegramCampaignControlV1) (TelegramCampaignReceiptV1, error) {
	var out TelegramCampaignReceiptV1
	if !eventRE.MatchString(req.EventID) || !idRE.MatchString(req.CampaignID) || req.ExpectedRevision < 1 || req.Action != "pause" && req.Action != "resume" && req.Action != "cancel" {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	a, err := telegramCampaignAccess(ctx, tx, actor, "broadcast_send")
	if err != nil {
		return out, err
	}
	c, err := telegramReadCampaignTx(ctx, tx, a.Store.ID, req.CampaignID)
	if err != nil {
		return out, err
	}
	fp := fingerprint("telegram-campaign-control", struct {
		Actor   TelegramActor
		Request TelegramCampaignControlV1
	}{actor, req})
	if found, e := telegramCampaignEventReplay(ctx, tx, req.EventID, fp, "control", &out); found || e != nil {
		out.Replayed = found
		return out, e
	}
	if c.Revision != req.ExpectedRevision {
		return out, ErrConflict
	}
	switch req.Action {
	case "pause":
		if c.State != "queued" {
			return out, ErrConflict
		}
		c.State = "paused"
	case "resume":
		if c.State != "paused" || req.ApprovalDigest != c.ApprovalDigest || len(req.ApprovalDigest) != 64 {
			return out, ErrConflict
		}
		c.State = "queued"
		c.ApproverUserID = actor.UserID
	case "cancel":
		if c.State == "completed" || c.State == "cancelled" {
			return out, ErrConflict
		}
		c.State = "cancelled"
	}
	now := time.Now().UTC().Unix()
	c.Revision++
	c.UpdatedUnix = now
	if _, err = tx.ExecContext(ctx, `UPDATE telegram_campaign_drafts SET state=?,revision=?,approver_user_id=?,updated_unix=? WHERE id=?`, c.State, c.Revision, c.ApproverUserID, now, c.ID); err != nil {
		return out, err
	}
	if req.Action == "cancel" {
		if _, err = tx.ExecContext(ctx, `UPDATE telegram_campaign_deliveries SET state='cancelled',updated_unix=? WHERE campaign_id=? AND state='pending'`, now, c.ID); err != nil {
			return out, err
		}
	}
	out.Campaign = c
	if err = telegramRecordCampaignEvent(ctx, tx, actor, c.StoreID, req.EventID, fp, "control", c.ID, out); err != nil {
		return TelegramCampaignReceiptV1{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramCampaignReceiptV1{}, err
	}
	return out, nil
}

// TelegramClaimCampaignDeliveryV1 is a trusted worker boundary, never an HTTP
// customer API. now must be the worker's server clock. A claim is persisted
// before its caller sends anything over the network.
func (s *Store) TelegramClaimCampaignDeliveryV1(ctx context.Context, botID int64, now time.Time) (TelegramCampaignDeliveryV1, bool, error) {
	var out TelegramCampaignDeliveryV1
	if botID <= 0 || now.IsZero() {
		return out, false, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, false, err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_campaign_"); err != nil {
		return out, false, err
	}
	if err = telegramRecoverCampaignDispatches(ctx, tx, botID, now.Unix()); err != nil {
		return out, false, err
	}
	var next int64
	err = tx.QueryRowContext(ctx, `SELECT next_unix_nano FROM telegram_campaign_pacing WHERE bot_id=? AND chat_id=0`, botID).Scan(&next)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, false, err
	}
	if next > now.UnixNano() {
		return out, false, tx.Commit()
	}
	for skipped := 0; skipped < 100; skipped++ {
		var delivery, campaign, customer string
		var chat int64
		err = tx.QueryRowContext(ctx, `SELECT d.id,d.campaign_id,d.customer_id,d.chat_id FROM telegram_campaign_deliveries d JOIN telegram_campaign_drafts c ON c.id=d.campaign_id WHERE d.bot_id=? AND d.state='pending' AND d.next_attempt_unix<=? AND c.state='queued' AND c.scheduled_unix<=? ORDER BY c.created_unix,c.id,d.id LIMIT 1`, botID, now.Unix(), now.Unix()).Scan(&delivery, &campaign, &customer, &chat)
		if errors.Is(err, sql.ErrNoRows) {
			return out, false, tx.Commit()
		}
		if err != nil {
			return out, false, err
		}
		c, err := scanTelegramCampaignV1(tx.QueryRowContext(ctx, `SELECT `+telegramCampaignColumnsV1+` FROM telegram_campaign_drafts WHERE id=?`, campaign))
		if err != nil {
			return out, false, err
		}
		if err = telegramCampaignApprovalValidTx(ctx, tx, c); err != nil {
			return out, false, err
		}
		a, err := authorizeTelegramTx(ctx, tx, TelegramActor{BotID: botID, UserID: c.ApproverUserID, ChatID: c.ApproverUserID}, "broadcast_send")
		if err != nil {
			return out, false, err
		}
		if a.Store.ID != c.StoreID {
			return out, false, ErrOwnershipDenied
		}
		var consent int
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM telegram_commerce_customers cc JOIN telegram_campaign_consent m ON m.customer_id=cc.customer_id AND m.store_id=cc.store_id WHERE cc.customer_id=? AND cc.store_id=? AND cc.private_chat_id=? AND cc.telegram_user_id=? AND m.enabled=1`, customer, c.StoreID, chat, chat).Scan(&consent)
		if err != nil {
			return out, false, err
		}
		if consent != 1 {
			if _, err = tx.ExecContext(ctx, `UPDATE telegram_campaign_deliveries SET state='skipped',updated_unix=? WHERE id=? AND state='pending'`, now.Unix(), delivery); err != nil {
				return out, false, err
			}
			if err = telegramCompleteCampaign(ctx, tx, c.ID, now.Unix()); err != nil {
				return out, false, err
			}
			continue
		}
		next = 0
		err = tx.QueryRowContext(ctx, `SELECT next_unix_nano FROM telegram_campaign_pacing WHERE bot_id=? AND chat_id=?`, botID, chat).Scan(&next)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return out, false, err
		}
		if next > now.UnixNano() {
			if _, err = tx.ExecContext(ctx, `UPDATE telegram_campaign_deliveries SET next_attempt_unix=? WHERE id=?`, (next+int64(time.Second)-1)/int64(time.Second), delivery); err != nil {
				return out, false, err
			}
			continue
		}
		attempt, err := telegramOpaqueID()
		if err != nil {
			return out, false, err
		}
		lease := now.Unix() + 120
		result, err := tx.ExecContext(ctx, `UPDATE telegram_campaign_deliveries SET state='dispatching',attempt_id=?,attempt_count=attempt_count+1,lease_until_unix=?,updated_unix=? WHERE id=? AND state='pending' AND attempt_count<8`, attempt, lease, now.Unix(), delivery)
		if err != nil {
			return out, false, err
		}
		affected, err := result.RowsAffected()
		if err != nil || affected != 1 {
			return out, false, ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_campaign_attempts(attempt_id,delivery_id,started_unix,lease_until_unix) VALUES(?,?,?,?)`, attempt, delivery, now.Unix(), lease); err != nil {
			return out, false, err
		}
		for _, pace := range []struct {
			chat  int64
			delay time.Duration
		}{{0, 75 * time.Millisecond}, {chat, time.Second}} {
			if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_campaign_pacing(bot_id,chat_id,next_unix_nano) VALUES(?,?,?) ON CONFLICT(bot_id,chat_id) DO UPDATE SET next_unix_nano=excluded.next_unix_nano`, botID, pace.chat, now.Add(pace.delay).UnixNano()); err != nil {
				return out, false, err
			}
		}
		out = TelegramCampaignDeliveryV1{DeliveryID: delivery, AttemptID: attempt, CampaignID: campaign, StoreID: c.StoreID, BotID: botID, ChatID: chat, Content: c.Content, LeaseUntilUnix: lease}
		if err = tx.Commit(); err != nil {
			return TelegramCampaignDeliveryV1{}, false, err
		}
		return out, true, nil
	}
	return out, false, tx.Commit()
}

func telegramRecoverCampaignDispatches(ctx context.Context, tx *sql.Tx, botID, now int64) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO telegram_campaign_outcomes(attempt_id,outcome,message_id,error_code,retry_after_seconds,finished_unix) SELECT attempt_id,'unknown',0,0,0,? FROM telegram_campaign_deliveries WHERE bot_id=? AND state='dispatching' AND lease_until_unix<=?`, now, botID, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE telegram_campaign_deliveries SET state='unknown',updated_unix=? WHERE bot_id=? AND state='dispatching' AND lease_until_unix<=?`, now, botID, now); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE telegram_campaign_drafts SET state='completed',updated_unix=? WHERE state IN('queued','paused') AND approver_user_id>0 AND EXISTS(SELECT 1 FROM telegram_commerce_stores s WHERE s.store_id=telegram_campaign_drafts.store_id AND s.bot_id=?) AND NOT EXISTS(SELECT 1 FROM telegram_campaign_deliveries d WHERE d.campaign_id=telegram_campaign_drafts.id AND d.state IN('pending','dispatching'))`, now, botID)
	return err
}

func telegramCompleteCampaign(ctx context.Context, tx *sql.Tx, id string, now int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE telegram_campaign_drafts SET state='completed',updated_unix=? WHERE id=? AND state IN('queued','paused') AND NOT EXISTS(SELECT 1 FROM telegram_campaign_deliveries WHERE campaign_id=? AND state IN('pending','dispatching'))`, now, id, id)
	return err
}

// TelegramFinishCampaignDeliveryV1 records factual transport outcomes only.
// rate_limited requires an explicit Telegram 429 and is the only retry path.
// Server/timeout/network ambiguities are unknown and require human review.
func (s *Store) TelegramFinishCampaignDeliveryV1(ctx context.Context, botID int64, result TelegramCampaignDeliveryResultV1, now time.Time) error {
	if botID <= 0 || !idRE.MatchString(result.DeliveryID) || !idRE.MatchString(result.AttemptID) || now.IsZero() {
		return ErrInvalid
	}
	switch result.Outcome {
	case "sent":
		if result.MessageID <= 0 || result.ErrorCode != 0 || result.RetryAfterSeconds != 0 {
			return ErrInvalid
		}
	case "rate_limited":
		if result.ErrorCode != 429 || result.MessageID != 0 || result.RetryAfterSeconds < 1 || result.RetryAfterSeconds > 3600 {
			return ErrInvalid
		}
	case "permanent_failure":
		if result.MessageID != 0 || result.RetryAfterSeconds != 0 || result.ErrorCode < 400 || result.ErrorCode >= 500 || result.ErrorCode == 429 {
			return ErrInvalid
		}
	case "unknown":
		if result.MessageID != 0 || result.RetryAfterSeconds != 0 || result.ErrorCode < 0 || result.ErrorCode > 599 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = telegramExtensionReady(ctx, tx, "telegram_campaign_"); err != nil {
		return err
	}
	var campaign, state, attempt string
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT campaign_id,state,attempt_id,attempt_count FROM telegram_campaign_deliveries WHERE id=? AND bot_id=?`, result.DeliveryID, botID).Scan(&campaign, &state, &attempt, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if attempt != result.AttemptID {
		return ErrConflict
	}
	var old TelegramCampaignDeliveryResultV1
	err = tx.QueryRowContext(ctx, `SELECT outcome,message_id,error_code,retry_after_seconds FROM telegram_campaign_outcomes WHERE attempt_id=?`, attempt).Scan(&old.Outcome, &old.MessageID, &old.ErrorCode, &old.RetryAfterSeconds)
	if err == nil {
		if old.Outcome == result.Outcome && old.MessageID == result.MessageID && old.ErrorCode == result.ErrorCode && old.RetryAfterSeconds == result.RetryAfterSeconds {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if state != "dispatching" {
		return ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_campaign_outcomes(attempt_id,outcome,message_id,error_code,retry_after_seconds,finished_unix) VALUES(?,?,?,?,?,?)`, attempt, result.Outcome, result.MessageID, result.ErrorCode, result.RetryAfterSeconds, now.Unix()); err != nil {
		return err
	}
	next := now.Unix()
	newState := result.Outcome
	if result.Outcome == "rate_limited" {
		newState = "pending"
		next += result.RetryAfterSeconds
		if attempts >= 8 {
			newState = "permanent_failure"
		}
		var campaignState string
		if err = tx.QueryRowContext(ctx, `SELECT state FROM telegram_campaign_drafts WHERE id=?`, campaign).Scan(&campaignState); err != nil {
			return err
		}
		if campaignState == "cancelled" {
			newState = "cancelled"
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO telegram_campaign_pacing(bot_id,chat_id,next_unix_nano) VALUES(?,0,?) ON CONFLICT(bot_id,chat_id) DO UPDATE SET next_unix_nano=MAX(next_unix_nano,excluded.next_unix_nano)`, botID, now.Add(time.Duration(result.RetryAfterSeconds)*time.Second).UnixNano()); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE telegram_campaign_deliveries SET state=?,next_attempt_unix=?,message_id=?,last_error_code=?,updated_unix=? WHERE id=?`, newState, next, result.MessageID, result.ErrorCode, now.Unix(), result.DeliveryID); err != nil {
		return err
	}
	if err = telegramCompleteCampaign(ctx, tx, campaign, now.Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) TelegramCampaignReportV1(ctx context.Context, actor TelegramActor, id string) (TelegramCampaignReportV1, error) {
	var out TelegramCampaignReportV1
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	a, err := telegramCampaignAccess(ctx, tx, actor, "")
	if err != nil {
		return out, err
	}
	out.Campaign, err = telegramReadCampaignTx(ctx, tx, a.Store.ID, id)
	if err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT state,COUNT(*) FROM telegram_campaign_deliveries WHERE campaign_id=? GROUP BY state`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var count int64
		if err = rows.Scan(&state, &count); err != nil {
			return out, err
		}
		switch state {
		case "pending":
			out.Pending = count
		case "dispatching":
			out.Dispatching = count
		case "sent":
			out.Sent = count
		case "permanent_failure":
			out.Failed = count
		case "unknown":
			out.Unknown = count
		case "skipped":
			out.Skipped = count
		case "cancelled":
			out.Cancelled = count
		default:
			return TelegramCampaignReportV1{}, ErrUnavailable
		}
	}
	return out, rows.Err()
}

func (s *Store) TelegramCampaignsV1(ctx context.Context, actor TelegramActor, limit, offset int) ([]TelegramCampaignV1, error) {
	limit, offset, err := telegramPageV1(limit, offset)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := telegramCampaignAccess(ctx, tx, actor, "")
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+telegramCampaignColumnsV1+` FROM telegram_campaign_drafts WHERE store_id=? ORDER BY created_unix DESC,id LIMIT ? OFFSET ?`, a.Store.ID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TelegramCampaignV1, 0)
	for rows.Next() {
		c, e := scanTelegramCampaignV1(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
