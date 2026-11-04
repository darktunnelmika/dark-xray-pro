//go:build linux

package telegrambot

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Validate referential meaning as well as SQLite structure. In particular a
// delivery proof cannot be transplanted onto another bot/user's sent message.
// Both startup and offline restore validation use this read-only boundary.
func validateJournalRows(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return ErrJournal
	}
	bad := rows.Next()
	err = rows.Err()
	rows.Close()
	if bad || err != nil {
		return ErrJournal
	}
	rows, err = db.QueryContext(ctx, "SELECT i.bot_id,i.update_id,i.fingerprint,i.kind,i.state,i.input_json,i.created_unix,i.reason,i.attempts,i.next_unix_ms,c.cursor FROM bot_inbox i JOIN bot_cursors c ON c.bot_id=i.bot_id")
	if err != nil {
		return ErrJournal
	}
	for rows.Next() {
		var bot, id, created, next, cursor int64
		var fp, kind, state, reason string
		var raw []byte
		var attempts int
		if rows.Scan(&bot, &id, &fp, &kind, &state, &raw, &created, &reason, &attempts, &next, &cursor) != nil || bot <= 0 || id < 0 || id >= cursor || !hexDigest(fp) || created <= 0 || attempts < 0 || attempts > 1000000 || next < 0 || reason != "" && !validReason(reason) {
			rows.Close()
			return ErrJournal
		}
		retained := state == "pending" || state == "processing" || state == "review"
		if retained {
			var in Incoming
			if len(raw) == 0 || len(raw) > maxUpdateBytes || decodeJSON(raw, &in) != nil || in.UpdateID != id || in.Identity.BotID != bot || in.Kind != kind || !validStoredIncoming(in) {
				rows.Close()
				return ErrJournal
			}
		} else if (state != "done" && state != "rejected") || len(raw) != 0 {
			rows.Close()
			return ErrJournal
		}
		if reason == "financial_capture_retry" && kind != "payment" && kind != "refund" {
			rows.Close()
			return ErrJournal
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrJournal
	}
	type outboxIdentity struct {
		Bot, Chat, Message int64
		Kind, State        string
		Access             Access
		Anonymous          bool
	}
	parents := map[string]outboxIdentity{}
	qrSources := map[string]string{}
	rows, err = db.QueryContext(ctx, "SELECT id,bot_id,chat_id,kind,state,payload_json,access_json,anonymous,sent_message_id,attempts,created_unix,expires_unix FROM bot_outbox")
	if err != nil {
		return ErrJournal
	}
	for rows.Next() {
		var id string
		var o outboxIdentity
		var payload, scope []byte
		var anonymous, attempts int
		var created, expires int64
		if rows.Scan(&id, &o.Bot, &o.Chat, &o.Kind, &o.State, &payload, &scope, &anonymous, &o.Message, &attempts, &created, &expires) != nil || !strings.HasPrefix(id, strconv.FormatInt(o.Bot, 10)+":") || len(id) > 160 || o.Bot <= 0 || o.Chat <= 0 || created <= 0 || expires <= 0 || attempts < 0 || attempts > 5 || len(scope) > 8192 || decodeJSON(scope, &o.Access) != nil || o.Access.Identity != (Identity{o.Bot, o.Chat, o.Chat}) || anonymous < 0 || anonymous > 1 {
			rows.Close()
			return ErrJournal
		}
		o.Anonymous = anonymous == 1
		if o.Anonymous {
			if o.Kind != "checkout" && o.Kind != "callback_answer" {
				rows.Close()
				return ErrJournal
			}
		} else if !o.Access.valid() {
			rows.Close()
			return ErrJournal
		}
		retained := o.State == "pending" || o.State == "dispatching" || o.State == "unknown"
		if retained {
			if len(payload) == 0 || len(payload) > 64<<10 || validateStoredPayload(o.Kind, o.Chat, payload, o.Anonymous) != nil {
				rows.Close()
				return ErrJournal
			}
			if o.Kind == "document" {
				var m wireDocument
				if decodeJSON(payload, &m) != nil {
					rows.Close()
					return ErrJournal
				}
				if m.ContentType == "image/png" {
					qrSources[id] = m.SourceSHA256
				}
			}
		} else if (o.State != "sent" && o.State != "failed" && o.State != "cancelled") || len(payload) != 0 {
			rows.Close()
			return ErrJournal
		}
		if o.State == "sent" {
			if o.Kind == "checkout" || o.Kind == "callback_answer" {
				if o.Message != 0 {
					rows.Close()
					return ErrJournal
				}
			} else if o.Message <= 0 {
				rows.Close()
				return ErrJournal
			}
		} else if o.Message != 0 {
			rows.Close()
			return ErrJournal
		}
		parents[id] = o
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrJournal
	}
	rows, err = db.QueryContext(ctx, "SELECT outbox_id,bot_id,user_id,client_id,sha256,terms_sha256,expires_unix_nano,message_id,state FROM bot_delivery_proofs")
	if err != nil {
		return ErrJournal
	}
	for rows.Next() {
		var id, client, sha, terms, state string
		var bot, user, expiry, message int64
		if rows.Scan(&id, &bot, &user, &client, &sha, &terms, &expiry, &message, &state) != nil {
			rows.Close()
			return ErrJournal
		}
		p, ok := parents[id]
		if !ok || bot != p.Bot || user != p.Chat || p.Anonymous || (p.Kind != "message" && p.Kind != "document") || !validOpaque(client, 128) || !hexDigest(sha) || !hexDigest(terms) || expiry <= 0 {
			rows.Close()
			return ErrJournal
		}
		if source, present := qrSources[id]; present {
			if source != sha {
				rows.Close()
				return ErrJournal
			}
			delete(qrSources, id)
		}
		switch state {
		case "waiting":
			if (p.State != "pending" && p.State != "dispatching") || message != 0 {
				rows.Close()
				return ErrJournal
			}
		case "pending", "acknowledged":
			if p.State != "sent" || message <= 0 || message != p.Message {
				rows.Close()
				return ErrJournal
			}
		case "review":
			if p.State == "sent" {
				if message <= 0 || message != p.Message {
					rows.Close()
					return ErrJournal
				}
			} else if (p.State != "unknown" && p.State != "failed" && p.State != "cancelled") || message != 0 {
				rows.Close()
				return ErrJournal
			}
		default:
			rows.Close()
			return ErrJournal
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrJournal
	}
	if len(qrSources) != 0 {
		return ErrJournal
	}
	rows, err = db.QueryContext(ctx, "SELECT token_hash,bot_id,user_id,chat_id,message_id,access_digest,action_json,outbox_id,expires_unix FROM bot_callbacks")
	if err != nil {
		return ErrJournal
	}
	for rows.Next() {
		var token, digest, id string
		var bot, user, chat, message, expiry int64
		var raw []byte
		var action Action
		if rows.Scan(&token, &bot, &user, &chat, &message, &digest, &raw, &id, &expiry) != nil {
			rows.Close()
			return ErrJournal
		}
		p, ok := parents[id]
		if !ok || !hexDigest(token) || bot != p.Bot || user != p.Chat || chat != p.Chat || p.Anonymous || (p.Kind != "message" && p.Kind != "edit") || digest != p.Access.digest() || expiry <= 0 || len(raw) > 8192 || decodeJSON(raw, &action) != nil || !validAction(action) {
			rows.Close()
			return ErrJournal
		}
		if p.State == "sent" {
			if message <= 0 || message != p.Message {
				rows.Close()
				return ErrJournal
			}
		} else if (p.State != "pending" && p.State != "dispatching" && p.State != "unknown") || message != 0 {
			rows.Close()
			return ErrJournal
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrJournal
	}
	rows, err = db.QueryContext(ctx, "SELECT bot_id,user_id,access_digest,action_json,expires_unix FROM bot_conversations")
	if err != nil {
		return ErrJournal
	}
	for rows.Next() {
		var bot, user, expiry int64
		var digest string
		var raw []byte
		var action Action
		if rows.Scan(&bot, &user, &digest, &raw, &expiry) != nil || bot <= 0 || user <= 0 || expiry <= 0 || !hexDigest(digest) || len(raw) > 8192 || decodeJSON(raw, &action) != nil || !validAction(action) {
			rows.Close()
			return ErrJournal
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrJournal
	}
	return nil
}

func validStoredIncoming(in Incoming) bool {
	if in.Identity.BotID <= 0 || in.Identity.UserID <= 0 || in.Identity.ChatID != in.Identity.UserID || in.ReceivedUnix <= 0 {
		return false
	}
	switch in.Kind {
	case "message":
		return in.Payment == nil && validText(in.Text, 4096)
	case "callback":
		return in.Payment == nil && in.MessageID > 0 && validOpaque(in.QueryID, 256) && validOpaque(in.CallbackData, 64)
	case "pre_checkout":
		return validOpaque(in.QueryID, 256) && in.Payment != nil && validPayment(*in.Payment, false)
	case "payment", "refund":
		return in.Payment != nil && validPayment(*in.Payment, true)
	default:
		return false
	}
}

func validateStoredPayload(kind string, chat int64, raw []byte, anonymous bool) error {
	switch kind {
	case "message", "edit":
		var m wireMessage
		if decodeJSON(raw, &m) != nil || m.ChatID != chat || m.ParseMode != "HTML" || !validText(m.Text, 4096) || validateWireKeyboard(m.Markup) != nil || (kind == "edit" && m.MessageID <= 0) || (kind == "message" && m.MessageID != 0) {
			return ErrJournal
		}
	case "invoice":
		var m wireInvoice
		if decodeJSON(raw, &m) != nil || m.ChatID != chat || m.Currency != "XTR" || m.ProviderToken != "" || !validText(m.Title, 32) || !validText(m.Description, 255) || !validOpaque(m.Payload, 128) || len(m.Prices) != 1 || m.Prices[0].Amount <= 0 || m.Prices[0].Amount > 1_000_000_000 {
			return ErrJournal
		}
	case "document":
		var m wireDocument
		if decodeJSON(raw, &m) != nil || m.ChatID != chat || !validDocument(m.document()) {
			return ErrJournal
		}
	case "checkout":
		var m wireCheckout
		if decodeJSON(raw, &m) != nil || !validOpaque(m.QueryID, 256) || !m.OK && !validText(m.Error, 200) || m.OK && (anonymous || m.Error != "" || m.Binding == nil || !validPayment(*m.Binding, false)) {
			return ErrJournal
		}
	case "callback_answer":
		var m wireCallbackAnswer
		if decodeJSON(raw, &m) != nil || !validOpaque(m.QueryID, 256) || m.CacheTime != 0 || m.Text != "" && !validText(m.Text, 200) {
			return ErrJournal
		}
	default:
		return ErrJournal
	}
	return nil
}
