package telegrambot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const apiOrigin = "https://api.telegram.org"

var tokenPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}:[A-Za-z0-9_-]{20,200}$`)
var botUsernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,31}$`)

type api struct {
	botID    int64
	token    string
	client   *http.Client
	verified bool // set before the polling/dispatch goroutines are started
	username string
}

func newAPI(botID int64, token string, transport http.RoundTripper) (*api, error) {
	if botID <= 0 || !tokenPattern.MatchString(token) {
		return nil, ErrConfig
	}
	if transport == nil {
		transport = &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second,
			IdleConnTimeout: 30 * time.Second, MaxIdleConns: 3, MaxConnsPerHost: 3, DisableCompression: true,
		}
	}
	return &api{botID: botID, token: token, client: &http.Client{
		Transport: transport, Timeout: 35 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (a *api) close() { a.client.CloseIdleConnections() }

func (a *api) call(ctx context.Context, method string, input any) (json.RawMessage, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, &APIError{Definitive: true, Reason: "request_not_started"}
	}
	switch method {
	case "getMe", "getWebhookInfo", "getUpdates", "sendMessage", "editMessageText", "sendInvoice", "sendPhoto", "answerPreCheckoutQuery", "answerCallbackQuery":
	default:
		return nil, ErrProtocol
	}
	body, err := json.Marshal(input)
	if err != nil || len(body) > 64<<10 {
		return nil, ErrProtocol
	}
	return a.request(ctx, method, body, "application/json")
}

func (a *api) request(ctx context.Context, method string, body []byte, contentType string) (json.RawMessage, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, &APIError{Definitive: true, Reason: "request_not_started"}
	}
	if len(body) > 128<<10 {
		return nil, ErrProtocol
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiOrigin+"/bot"+a.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, ErrProtocol
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, &APIError{Reason: "transport_uncertain"}
	} // Never wrap a credential-bearing URL.
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(data) > maxResponseBytes {
		return nil, &APIError{Reason: "response_uncertain"}
	}
	var envelope map[string]json.RawMessage
	var ok bool
	if decodeJSON(data, &envelope) != nil || required(envelope, "ok", &ok) != nil {
		return nil, &APIError{Reason: "response_uncertain"}
	}
	if ok && resp.StatusCode == http.StatusOK {
		raw := envelope["result"]
		if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
			return nil, &APIError{Reason: "response_uncertain"}
		}
		return raw, nil
	}
	failure := &APIError{Reason: "response_uncertain"}
	var code int
	if ok || required(envelope, "error_code", &code) != nil || code < 400 || code >= 500 || (resp.StatusCode != 200 && resp.StatusCode != code) {
		return nil, failure
	}
	failure.Code = code
	failure.Definitive = true
	failure.Reason = "telegram_rejected"
	failure.Fatal = code == 401 || (method == "getUpdates" && code != 429) || method == "getMe" || method == "getWebhookInfo"
	if code == 429 {
		var p map[string]json.RawMessage
		var seconds int64
		if json.Unmarshal(envelope["parameters"], &p) != nil || required(p, "retry_after", &seconds) != nil || seconds < 1 || seconds > 3600 {
			failure.Fatal = true
			return nil, failure
		}
		failure.Reason = "telegram_rate_limited"
		failure.RetryAfter = time.Duration(seconds) * time.Second
	}
	return nil, failure
}

func (a *api) verify(ctx context.Context) error {
	raw, err := a.call(ctx, "getMe", struct{}{})
	if err != nil {
		return err
	}
	id, isBot, _, err := readUser(raw)
	if err != nil || id != a.botID || !isBot {
		return ErrIdentity
	}
	var user map[string]json.RawMessage
	if json.Unmarshal(raw, &user) != nil {
		return ErrProtocol
	}
	if value, ok := user["username"]; ok {
		if json.Unmarshal(value, &a.username) != nil || !botUsernamePattern.MatchString(a.username) {
			return ErrProtocol
		}
	}
	raw, err = a.call(ctx, "getWebhookInfo", struct{}{})
	if err != nil {
		return err
	}
	var info map[string]json.RawMessage
	var url string
	var pending int64
	if decodeJSON(raw, &info) != nil || required(info, "url", &url) != nil || required(info, "pending_update_count", &pending) != nil || pending < 0 {
		return ErrProtocol
	}
	// Never delete an existing webhook or consume updates belonging to it.
	if url != "" {
		return ErrWebhook
	}
	a.verified = true
	return nil
}

type received struct {
	ID          int64
	Fingerprint string
	Input       *Incoming
	Rejection   string
}

func (a *api) updates(ctx context.Context, offset int64) ([]received, error) {
	if !a.verified || offset < 0 {
		return nil, ErrIdentity
	}
	request := struct {
		Offset  int64    `json:"offset"`
		Limit   int      `json:"limit"`
		Timeout int      `json:"timeout"`
		Allowed []string `json:"allowed_updates"`
	}{offset, maxBatch, 20, []string{"message", "callback_query", "pre_checkout_query"}}
	raw, err := a.call(ctx, "getUpdates", request)
	if err != nil {
		return nil, err
	}
	var values []json.RawMessage
	if decodeJSON(raw, &values) != nil || values == nil || len(values) > maxBatch {
		return nil, ErrProtocol
	}
	out := make([]received, 0, len(values))
	for _, value := range values {
		u, err := normalizeUpdate(value, a.botID, time.Now().Unix())
		if err != nil {
			return nil, err
		}
		if u.Input != nil {
			u.Input.BotUsername = a.username
		}
		out = append(out, u)
	}
	return out, nil
}

type wireButton struct {
	Text     string    `json:"text"`
	Style    string    `json:"style,omitempty"`
	Callback string    `json:"callback_data,omitempty"`
	URL      string    `json:"url,omitempty"`
	Copy     *copyText `json:"copy_text,omitempty"`
}
type copyText struct {
	Text string `json:"text"`
}
type wireKeyboard struct {
	Rows [][]wireButton `json:"inline_keyboard"`
}
type wireMessage struct {
	ChatID    int64         `json:"chat_id"`
	MessageID int64         `json:"message_id,omitempty"`
	Text      string        `json:"text"`
	ParseMode string        `json:"parse_mode,omitempty"`
	Markup    *wireKeyboard `json:"reply_markup,omitempty"`
	Preview   struct {
		Disabled bool `json:"is_disabled"`
	} `json:"link_preview_options"`
}
type wirePrice struct {
	Label  string `json:"label"`
	Amount int64  `json:"amount"`
}
type wireInvoice struct {
	ChatID        int64       `json:"chat_id"`
	Title         string      `json:"title"`
	Description   string      `json:"description"`
	Payload       string      `json:"payload"`
	Currency      string      `json:"currency"`
	ProviderToken string      `json:"provider_token"`
	Prices        []wirePrice `json:"prices"`
}
type wireDocument struct {
	ChatID        int64  `json:"chat_id"`
	Name          string `json:"name"`
	Content       []byte `json:"content"`
	Caption       string `json:"caption,omitempty"`
	ContentType   string `json:"content_type,omitempty"`
	SourceContent []byte `json:"source_content,omitempty"`
	SourceSHA256  string `json:"source_sha256,omitempty"`
	ContentSHA256 string `json:"content_sha256,omitempty"`
}

func (m wireDocument) document() Document {
	return Document{Name: m.Name, Content: m.Content, Caption: m.Caption, ContentType: m.ContentType, SourceContent: m.SourceContent, SourceSHA256: m.SourceSHA256, ContentSHA256: m.ContentSHA256}
}

type wirePhoto struct {
	ChatID  int64         `json:"chat_id"`
	Photo   string        `json:"photo"`
	Caption string        `json:"caption,omitempty"`
	Markup  *wireKeyboard `json:"reply_markup,omitempty"`
}
type wireCheckout struct {
	QueryID string `json:"pre_checkout_query_id"`
	OK      bool   `json:"ok"`
	Error   string `json:"error_message,omitempty"`
	// Private journal-only binding; removed before calling the Bot API.
	Binding *Payment `json:"_payment_binding,omitempty"`
}
type wireCallbackAnswer struct {
	QueryID   string `json:"callback_query_id"`
	Text      string `json:"text,omitempty"`
	ShowAlert bool   `json:"show_alert,omitempty"`
	CacheTime int    `json:"cache_time"`
}

func (a *api) dispatch(ctx context.Context, d delivery) (DeliveryOutcome, error) {
	out := DeliveryOutcome{State: "unknown", Reason: "response_uncertain"}
	if !a.verified || d.BotID != a.botID || d.ChatID <= 0 {
		return DeliveryOutcome{State: "failed", Reason: "scope_rejected"}, ErrIdentity
	}
	var input any
	var method string
	expectedMessage := int64(0)
	var document *wireDocument
	switch d.Kind {
	case "message", "edit":
		var m wireMessage
		if decodeJSON(d.Payload, &m) != nil || m.ChatID != d.ChatID || m.ParseMode != "HTML" || !validText(m.Text, 4096) || validateWireKeyboard(m.Markup) != nil {
			return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
		}
		method = "sendMessage"
		if d.Kind == "edit" {
			if m.MessageID <= 0 {
				return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
			}
			method = "editMessageText"
			expectedMessage = m.MessageID
		} else if m.MessageID != 0 {
			return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
		}
		input = m
	case "invoice":
		var m wireInvoice
		if decodeJSON(d.Payload, &m) != nil || m.ChatID != d.ChatID || m.Currency != "XTR" || m.ProviderToken != "" || !validText(m.Title, 32) || !validText(m.Description, 255) || !validText(m.Payload, 128) || len(m.Prices) != 1 || m.Prices[0].Amount <= 0 || m.Prices[0].Amount > 1_000_000_000 {
			return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
		}
		method = "sendInvoice"
		input = m
	case "document":
		var m wireDocument
		if decodeJSON(d.Payload, &m) != nil || m.ChatID != d.ChatID || !validDocument(m.document()) || (m.ContentType == "image/png" && (d.Proof == nil || m.SourceSHA256 != d.Proof.SHA256 || !hexDigest(d.Proof.TermsSHA256) || d.Proof.ExpiresUnixNano <= time.Now().UnixNano())) {
			return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
		}
		method = "sendDocument"
		document = &m
	case "campaign_message":
		var m wireMessage
		if decodeJSON(d.Payload, &m) != nil || m.ChatID != d.ChatID || m.MessageID != 0 || m.ParseMode != "" || !validText(m.Text, 4096) || validateCampaignKeyboard(m.Markup) != nil {
			return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
		}
		method = "sendMessage"
		input = m
	case "campaign_photo":
		var m wirePhoto
		if decodeJSON(d.Payload, &m) != nil || m.ChatID != d.ChatID || !validOpaque(m.Photo, 512) || strings.ContainsAny(m.Photo, ".:/") || (m.Caption != "" && !validText(m.Caption, 1024)) || validateCampaignKeyboard(m.Markup) != nil {
			return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
		}
		method = "sendPhoto"
		input = m
	case "checkout":
		var m wireCheckout
		if decodeJSON(d.Payload, &m) != nil || !validOpaque(m.QueryID, 256) || (m.OK && m.Error != "") || (!m.OK && !validText(m.Error, 200)) {
			return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
		}
		method = "answerPreCheckoutQuery"
		m.Binding = nil
		input = m
	case "callback_answer":
		var m wireCallbackAnswer
		if decodeJSON(d.Payload, &m) != nil || !validOpaque(m.QueryID, 256) || (m.Text != "" && !validText(m.Text, 200)) || m.CacheTime != 0 {
			return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
		}
		method = "answerCallbackQuery"
		input = m
	default:
		return DeliveryOutcome{State: "failed", Reason: "payload_rejected"}, ErrProtocol
	}
	var raw json.RawMessage
	var err error
	if document != nil {
		raw, err = a.sendDocument(ctx, *document)
	} else {
		raw, err = a.call(ctx, method, input)
	}
	if err != nil {
		var failure *APIError
		if errors.As(err, &failure) {
			out.Reason = failure.Reason
			if failure.Definitive {
				out.State = "failed"
			}
			if failure.Definitive && failure.RetryAfter > 0 && !failure.Fatal {
				out.State = "retry"
				out.RetryAfter = failure.RetryAfter
			}
		}
		return out, err
	}
	if method == "answerCallbackQuery" || method == "answerPreCheckoutQuery" {
		var ok bool
		if decodeJSON(raw, &ok) != nil || !ok {
			return out, ErrProtocol
		}
		return DeliveryOutcome{State: "sent"}, nil
	}
	var message map[string]json.RawMessage
	var id int64
	if decodeJSON(raw, &message) != nil || required(message, "message_id", &id) != nil || id <= 0 || forbiddenMessageOrigin(message, false) {
		return out, ErrProtocol
	}
	from, isBot, _, e1 := readUser(message["from"])
	chat, kind, e2 := readChat(message["chat"])
	if e1 != nil || e2 != nil || from != a.botID || !isBot || chat != d.ChatID || kind != "private" || (expectedMessage != 0 && id != expectedMessage) {
		return out, ErrProtocol
	}
	return DeliveryOutcome{State: "sent", MessageID: id}, nil
}

func (a *api) sendDocument(ctx context.Context, d wireDocument) (json.RawMessage, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	for _, field := range []struct{ k, v string }{{"chat_id", strconv.FormatInt(d.ChatID, 10)}, {"caption", d.Caption}, {"parse_mode", "HTML"}} {
		if err := form.WriteField(field.k, field.v); err != nil {
			return nil, ErrProtocol
		}
	}
	contentType := d.ContentType
	if contentType == "" {
		contentType = "text/plain; charset=utf-8"
	}
	if contentType == "image/png" {
		if err := form.WriteField("disable_content_type_detection", "true"); err != nil {
			return nil, ErrProtocol
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "document", "filename": d.Name}))
	header.Set("Content-Type", contentType)
	part, err := form.CreatePart(header)
	if err != nil {
		return nil, ErrProtocol
	}
	if _, err = part.Write(d.Content); err != nil {
		return nil, ErrProtocol
	}
	if form.Close() != nil {
		return nil, ErrProtocol
	}
	return a.request(ctx, "sendDocument", body.Bytes(), form.FormDataContentType())
}

func validText(value string, max int) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}
func validOpaque(value string, max int) bool {
	if value == "" || len(value) > max || !utf8.ValidString(value) {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}

func normalizeUpdate(raw []byte, botID, now int64) (received, error) {
	var fields map[string]json.RawMessage
	var id int64
	if len(raw) > maxUpdateBytes || decodeJSON(raw, &fields) != nil || required(fields, "update_id", &id) != nil || id < 0 || id == math.MaxInt64 {
		return received{}, ErrProtocol
	}
	var canonical any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&canonical) != nil {
		return received{}, ErrProtocol
	}
	encoded, _ := json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	u := received{ID: id, Fingerprint: hex.EncodeToString(sum[:]), Rejection: "unsupported_update"}
	if len(fields) != 2 {
		return u, nil
	}
	in := Incoming{UpdateID: id, Identity: Identity{BotID: botID}, ReceivedUnix: now}
	if value, ok := fields["message"]; ok {
		var m map[string]json.RawMessage
		var date int64
		if json.Unmarshal(value, &m) != nil || forbiddenMessageOrigin(m, true) || required(m, "message_id", &in.MessageID) != nil || in.MessageID <= 0 || required(m, "date", &date) != nil || date <= 0 {
			return u, nil
		}
		user, isBot, lang, err := readUser(m["from"])
		chat, kind, chatErr := readChat(m["chat"])
		if err != nil || chatErr != nil || isBot || kind != "private" || chat != user {
			return u, nil
		}
		in.Identity.UserID = user
		in.Identity.ChatID = chat
		in.Language = lang
		payment, paymentPresent := m["successful_payment"]
		refund, refundPresent := m["refunded_payment"]
		if paymentPresent && refundPresent {
			return u, nil
		}
		if refundPresent {
			payment = refund
		}
		if paymentPresent || refundPresent {
			var p map[string]json.RawMessage
			var pay Payment
			if json.Unmarshal(payment, &p) != nil || required(p, "invoice_payload", &pay.Payload) != nil || required(p, "currency", &pay.Currency) != nil || required(p, "total_amount", &pay.Amount) != nil || required(p, "telegram_payment_charge_id", &pay.TelegramChargeID) != nil || !validPayment(pay, true) {
				return u, nil
			}
			in.Kind = "payment"
			if refundPresent {
				in.Kind = "refund"
			}
			in.Payment = &pay
		} else {
			if required(m, "text", &in.Text) != nil || !validText(in.Text, 4096) {
				return u, nil
			}
			in.Kind = "message"
		}
	} else if value, ok := fields["callback_query"]; ok {
		var q, m map[string]json.RawMessage
		if json.Unmarshal(value, &q) != nil || q["inline_message_id"] != nil || q["game_short_name"] != nil || required(q, "id", &in.QueryID) != nil || !validOpaque(in.QueryID, 256) || required(q, "data", &in.CallbackData) != nil || !validOpaque(in.CallbackData, 64) || json.Unmarshal(q["message"], &m) != nil || forbiddenMessageOrigin(m, false) {
			return u, nil
		}
		user, isBot, lang, err := readUser(q["from"])
		from, messageBot, _, fromErr := readUser(m["from"])
		chat, kind, chatErr := readChat(m["chat"])
		var date int64
		if err != nil || fromErr != nil || chatErr != nil || isBot || !messageBot || from != botID || kind != "private" || chat != user || required(m, "message_id", &in.MessageID) != nil || in.MessageID <= 0 || required(m, "date", &date) != nil || date <= 0 {
			return u, nil
		}
		in.Identity.UserID = user
		in.Identity.ChatID = chat
		in.Language = lang
		in.Kind = "callback"
	} else if value, ok := fields["pre_checkout_query"]; ok {
		var q map[string]json.RawMessage
		var pay Payment
		if json.Unmarshal(value, &q) != nil || required(q, "id", &in.QueryID) != nil || !validOpaque(in.QueryID, 256) || required(q, "invoice_payload", &pay.Payload) != nil || required(q, "currency", &pay.Currency) != nil || required(q, "total_amount", &pay.Amount) != nil || !validPayment(pay, false) {
			return u, nil
		}
		user, isBot, lang, err := readUser(q["from"])
		if err != nil || isBot {
			return u, nil
		}
		in.Identity.UserID = user
		in.Identity.ChatID = user
		in.Language = lang
		in.Kind = "pre_checkout"
		in.Payment = &pay
	} else {
		return u, nil
	}
	u.Input = &in
	u.Rejection = ""
	return u, nil
}

func validPayment(p Payment, paid bool) bool {
	return validOpaque(p.Payload, 128) && p.Currency == "XTR" && p.Amount > 0 && p.Amount <= 1_000_000_000 && (!paid || validOpaque(p.TelegramChargeID, 512))
}

func forbiddenMessageOrigin(m map[string]json.RawMessage, user bool) bool {
	for _, key := range []string{"sender_chat", "sender_business_bot", "business_connection_id", "guest_query_id", "receiver_user", "ephemeral_message_id", "forward_origin", "forward_from", "forward_from_chat", "forward_sender_name", "forward_date", "is_automatic_forward", "via_bot", "message_thread_id", "is_topic_message", "direct_messages_topic"} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	if user {
		if _, ok := m["edit_date"]; ok {
			return true
		}
	}
	return false
}

func readUser(raw json.RawMessage) (int64, bool, string, error) {
	var f map[string]json.RawMessage
	var id int64
	var isBot bool
	var lang string
	if json.Unmarshal(raw, &f) != nil || required(f, "id", &id) != nil || id <= 0 || required(f, "is_bot", &isBot) != nil {
		return 0, false, "", ErrProtocol
	}
	if value, ok := f["language_code"]; ok && json.Unmarshal(value, &lang) != nil {
		return 0, false, "", ErrProtocol
	}
	return id, isBot, language(lang), nil
}
func readChat(raw json.RawMessage) (int64, string, error) {
	var f map[string]json.RawMessage
	var id int64
	var kind string
	if json.Unmarshal(raw, &f) != nil || required(f, "id", &id) != nil || id == 0 || required(f, "type", &kind) != nil {
		return 0, "", ErrProtocol
	}
	return id, kind, nil
}

func validateWireKeyboard(k *wireKeyboard) error {
	if k == nil {
		return nil
	}
	if len(k.Rows) > 24 {
		return ErrProtocol
	}
	for _, row := range k.Rows {
		if len(row) < 1 || len(row) > 4 {
			return ErrProtocol
		}
		for _, b := range row {
			if !validText(b.Text, 100) || !validStyle(b.Style) {
				return ErrProtocol
			}
			n := 0
			if b.Callback != "" {
				n++
				if !validOpaque(b.Callback, 64) {
					return ErrProtocol
				}
			}
			if b.URL != "" {
				n++
				if !safeURL(b.URL) {
					return ErrProtocol
				}
			}
			if b.Copy != nil {
				n++
				if !validText(b.Copy.Text, 256) {
					return ErrProtocol
				}
			}
			if n != 1 {
				return ErrProtocol
			}
		}
	}
	return nil
}

func validStyle(style string) bool {
	return style == "" || style == "primary" || style == "success" || style == "danger"
}
func safeURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && len(value) <= 2048 && !strings.ContainsAny(value, "\r\n\t\x00") && u.Scheme == "https" && u.Hostname() != "" && u.User == nil
}
func validateCampaignKeyboard(k *wireKeyboard) error {
	if validateWireKeyboard(k) != nil {
		return ErrProtocol
	}
	if k == nil {
		return nil
	}
	if len(k.Rows) > 8 {
		return ErrProtocol
	}
	for _, row := range k.Rows {
		for _, b := range row {
			if b.URL == "" || b.Callback != "" || b.Copy != nil {
				return ErrProtocol
			}
		}
	}
	return nil
}
