//go:build linux

package telegramhub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	botAPIOrigin        = "https://api.telegram.org"
	botMaxResponseBytes = 1 << 20
	botMaxBatch         = 25
	botPollSeconds      = 20
	botRequestTimeout   = 35 * time.Second
)

var (
	ErrBotTransport = errors.New("HUB owner report bot transport unavailable")
	ErrBotProtocol  = errors.New("HUB owner report bot response rejected")
	ErrBotIdentity  = errors.New("HUB owner report bot identity mismatch")
	botTokenFormat  = regexp.MustCompile(`^[1-9][0-9]{0,19}:[A-Za-z0-9_-]{20,200}$`)
)

// botAPIError deliberately retains neither URLs nor Telegram descriptions.
// definitive means Telegram explicitly rejected the request, not that a send
// should be retried. Every dispatched send gets a terminal journal outcome.
type botAPIError struct {
	definitive bool
	fatal      bool
	perMessage bool
	retryAfter time.Duration
	reason     string
}

func (*botAPIError) Error() string { return ErrBotTransport.Error() }

type botAPI struct {
	client  *http.Client
	token   string
	binding botBinding
}

func newBotAPI(token string, binding botBinding, transport http.RoundTripper) (*botAPI, error) {
	if !botTokenFormat.MatchString(token) || binding.BotID <= 0 || binding.OwnerUserID <= 0 || binding.OwnerChatID <= 0 ||
		(binding.BotUsername != "" && !botUsernamePattern.MatchString(binding.BotUsername)) {
		return nil, ErrInvalidConfig
	}
	if transport == nil {
		// No proxy, alternate origin, redirects, cookies or token environment are
		// accepted. The test-only injection uses a fake RoundTripper, no sockets.
		transport = &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: botRequestTimeout,
			IdleConnTimeout: 30 * time.Second, MaxIdleConns: 1, MaxConnsPerHost: 1,
			DisableCompression: true,
		}
	}
	return &botAPI{token: token, binding: binding, client: &http.Client{
		Transport: transport, Timeout: botRequestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (a *botAPI) close() { a.client.CloseIdleConnections() }

func (a *botAPI) call(ctx context.Context, method string, request any) (json.RawMessage, error) {
	if ctx == nil || ctx.Err() != nil {
		return nil, &botAPIError{reason: "request_cancelled"}
	}
	if method != "getMe" && method != "getUpdates" && method != "sendMessage" {
		return nil, ErrBotProtocol
	}
	body, err := json.Marshal(request)
	if err != nil || len(body) > 32768 {
		return nil, ErrBotProtocol
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, botAPIOrigin+"/bot"+a.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return nil, ErrBotProtocol
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		// net/http errors contain the credential-bearing URL. Never wrap them.
		return nil, &botAPIError{reason: "transport_uncertain"}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, botMaxResponseBytes+1))
	if err != nil || len(data) > botMaxResponseBytes {
		return nil, &botAPIError{reason: "response_uncertain"}
	}
	var envelope map[string]json.RawMessage
	if decodeBotJSON(data, &envelope) != nil || envelope == nil {
		return nil, &botAPIError{reason: "response_uncertain"}
	}
	var ok bool
	if readRequired(envelope, "ok", &ok) != nil {
		return nil, &botAPIError{reason: "response_uncertain"}
	}
	if resp.StatusCode == http.StatusOK && ok {
		result := envelope["result"]
		if len(result) == 0 || bytes.Equal(result, []byte("null")) {
			return nil, &botAPIError{reason: "response_uncertain"}
		}
		return result, nil
	}
	failure := &botAPIError{reason: "response_uncertain"}
	var code int
	if ok || readRequired(envelope, "error_code", &code) != nil || code < 400 || code >= 500 ||
		(resp.StatusCode != http.StatusOK && resp.StatusCode != code) {
		return nil, failure
	}
	failure.definitive = true
	failure.perMessage = method == "sendMessage" && (code == http.StatusBadRequest || code == http.StatusForbidden)
	failure.fatal = code != http.StatusTooManyRequests && !failure.perMessage
	failure.reason = "telegram_rejected"
	if code == http.StatusTooManyRequests {
		var parameters map[string]json.RawMessage
		var seconds int64
		if json.Unmarshal(envelope["parameters"], &parameters) != nil ||
			readRequired(parameters, "retry_after", &seconds) != nil || seconds < 1 || seconds > 3600 {
			// Do not shorten an unbounded flood delay and immediately poll again.
			failure.fatal = true
			return nil, failure
		}
		failure.reason = "telegram_rate_limited"
		failure.retryAfter = time.Duration(seconds) * time.Second
	}
	return nil, failure
}

func (a *botAPI) verify(ctx context.Context) error {
	result, err := a.call(ctx, "getMe", struct{}{})
	if err != nil {
		return err
	}
	id, isBot, username, err := readBotUser(result)
	if err != nil || id != a.binding.BotID || !isBot ||
		(a.binding.BotUsername != "" && !strings.EqualFold(username, a.binding.BotUsername)) {
		return ErrBotIdentity
	}
	return nil
}

func (a *botAPI) updates(ctx context.Context, offset int64) ([]receivedUpdate, error) {
	if offset < 0 {
		return nil, ErrBotProtocol
	}
	result, err := a.call(ctx, "getUpdates", struct {
		Offset         int64    `json:"offset"`
		Limit          int      `json:"limit"`
		Timeout        int      `json:"timeout"`
		AllowedUpdates []string `json:"allowed_updates"`
	}{offset, botMaxBatch, botPollSeconds, []string{"message"}})
	if err != nil {
		return nil, err
	}
	var raw []json.RawMessage
	if json.Unmarshal(result, &raw) != nil || raw == nil || len(raw) > botMaxBatch {
		return nil, ErrBotProtocol
	}
	updates := make([]receivedUpdate, 0, len(raw))
	for _, value := range raw {
		update, err := normalizeBotUpdate(value)
		if err != nil {
			return nil, err
		}
		updates = append(updates, update)
	}
	return updates, nil
}

func (a *botAPI) send(ctx context.Context, reply PreparedOwnerReply) (dispatchOutcome, error) {
	outcome := dispatchOutcome{State: "unknown", Reason: "response_uncertain"}
	if reply.OwnerUserID != a.binding.OwnerUserID || reply.ChatID != a.binding.OwnerChatID ||
		reply.ReplyToMessageID <= 0 || reply.UpdateID < 0 || reply.ParseMode != "HTML" ||
		!utf8.ValidString(reply.Text) || len(reply.Text) == 0 || utf8.RuneCountInString(reply.Text) > 4096 {
		return dispatchOutcome{State: "failed", Reason: "reply_scope_rejected"}, ErrBotProtocol
	}
	request := struct {
		ChatID          int64  `json:"chat_id"`
		Text            string `json:"text"`
		ParseMode       string `json:"parse_mode"`
		ReplyParameters struct {
			MessageID int64 `json:"message_id"`
		} `json:"reply_parameters"`
		LinkPreviewOptions struct {
			IsDisabled bool `json:"is_disabled"`
		} `json:"link_preview_options"`
	}{ChatID: reply.ChatID, Text: reply.Text, ParseMode: "HTML"}
	request.ReplyParameters.MessageID = reply.ReplyToMessageID
	request.LinkPreviewOptions.IsDisabled = true
	result, err := a.call(ctx, "sendMessage", request)
	if err != nil {
		var failure *botAPIError
		if errors.As(err, &failure) {
			outcome.Reason = failure.reason
			if failure.definitive {
				outcome.State = "failed"
			}
		}
		return outcome, err
	}
	var sent map[string]json.RawMessage
	var messageID int64
	if json.Unmarshal(result, &sent) != nil || hasUnsupportedOrigin(sent) ||
		readRequired(sent, "message_id", &messageID) != nil || messageID <= 0 {
		return outcome, ErrBotProtocol
	}
	id, isBot, _, err := readBotUser(sent["from"])
	chatID, chatType, chatErr := readBotChat(sent["chat"])
	var original map[string]json.RawMessage
	var replyID int64
	if err != nil || id != a.binding.BotID || !isBot || chatErr != nil ||
		chatID != a.binding.OwnerChatID || chatType != "private" ||
		json.Unmarshal(sent["reply_to_message"], &original) != nil ||
		readRequired(original, "message_id", &replyID) != nil || replyID != reply.ReplyToMessageID {
		return outcome, ErrBotProtocol
	}
	// Telegram returns rendered text and entities, not necessarily the submitted
	// HTML source. Do not compare Message.text against reply.Text.
	return dispatchOutcome{State: "sent", MessageID: messageID}, nil
}

func normalizeBotUpdate(raw json.RawMessage) (receivedUpdate, error) {
	var fields map[string]json.RawMessage
	var id int64
	if decodeBotJSON(raw, &fields) != nil || readRequired(fields, "update_id", &id) != nil || id < 0 {
		return receivedUpdate{}, ErrBotProtocol
	}
	// Stable under insignificant whitespace and object-key order. The strict
	// decoder rejects duplicate keys; UseNumber avoids rounding numeric IDs.
	var canonical any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&canonical) != nil {
		return receivedUpdate{}, ErrBotProtocol
	}
	encoded, _ := json.Marshal(canonical)
	sum := sha256.Sum256(encoded)
	update := receivedUpdate{ID: id, Fingerprint: hex.EncodeToString(sum[:]), Rejection: "unsupported_update"}
	if len(fields) != 2 || fields["message"] == nil {
		return update, nil
	}
	var message map[string]json.RawMessage
	if json.Unmarshal(fields["message"], &message) != nil || message == nil {
		return update, nil
	}
	for key := range message {
		switch key {
		case "message_id", "date", "from", "chat", "text", "entities":
		default:
			// Unknown additions fail closed, including all forward, edit, sender
			// chat, via-bot, business, guest, ephemeral, reply and topic origins.
			return update, nil
		}
	}
	var normalized OwnerReportMessage
	var date int64
	normalized.UpdateID = id
	if readRequired(message, "message_id", &normalized.MessageID) != nil || normalized.MessageID <= 0 ||
		readRequired(message, "date", &date) != nil || date <= 0 ||
		readRequired(message, "text", &normalized.Text) != nil || len(normalized.Text) > MaxOwnerReportCommandBytes {
		return update, nil
	}
	var err error
	normalized.SenderID, normalized.SenderIsBot, _, err = readBotUser(message["from"])
	if err != nil {
		return update, nil
	}
	normalized.ChatID, normalized.ChatType, err = readBotChat(message["chat"])
	if err != nil || !validBotJournalMessage(id, normalized) {
		return update, nil
	}
	update.Message, update.Rejection = &normalized, ""
	return update, nil
}

func hasUnsupportedOrigin(message map[string]json.RawMessage) bool {
	for _, key := range []string{"sender_chat", "sender_business_bot", "business_connection_id", "guest_query_id",
		"receiver_user", "ephemeral_message_id", "forward_origin", "forward_from", "forward_from_chat",
		"forward_sender_name", "forward_date", "is_automatic_forward", "edit_date", "via_bot",
		"message_thread_id", "is_topic_message", "direct_messages_topic"} {
		if _, present := message[key]; present {
			return true
		}
	}
	return false
}

func readBotUser(raw json.RawMessage) (int64, bool, string, error) {
	var user map[string]json.RawMessage
	var id int64
	var isBot bool
	var username string
	if json.Unmarshal(raw, &user) != nil || readRequired(user, "id", &id) != nil || id <= 0 ||
		readRequired(user, "is_bot", &isBot) != nil {
		return 0, false, "", ErrBotProtocol
	}
	if value, exists := user["username"]; exists && json.Unmarshal(value, &username) != nil {
		return 0, false, "", ErrBotProtocol
	}
	return id, isBot, username, nil
}

func readBotChat(raw json.RawMessage) (int64, string, error) {
	var chat map[string]json.RawMessage
	var id int64
	var kind string
	if json.Unmarshal(raw, &chat) != nil || readRequired(chat, "id", &id) != nil || id == 0 ||
		readRequired(chat, "type", &kind) != nil {
		return 0, "", ErrBotProtocol
	}
	return id, kind, nil
}

func readRequired(fields map[string]json.RawMessage, key string, target any) error {
	value, exists := fields[key]
	if !exists || bytes.Equal(value, []byte("null")) || json.Unmarshal(value, target) != nil {
		return ErrBotProtocol
	}
	return nil
}

// decodeBotJSON rejects duplicate keys, trailing values and excessive nesting,
// including inside ignored objects. No decoder error escapes this package.
func decodeBotJSON(data []byte, target any) error {
	if len(data) == 0 || len(data) > botMaxResponseBytes || !utf8.Valid(data) {
		return ErrBotProtocol
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var visit func(int) error
	visit = func(depth int) error {
		if depth > 32 {
			return ErrBotProtocol
		}
		token, err := d.Token()
		if err != nil {
			return ErrBotProtocol
		}
		delim, complex := token.(json.Delim)
		if !complex {
			return nil
		}
		switch delim {
		case '{':
			seen := make(map[string]bool)
			for d.More() {
				keyToken, err := d.Token()
				key, isString := keyToken.(string)
				if err != nil || !isString || seen[key] {
					return ErrBotProtocol
				}
				seen[key] = true
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := visit(depth + 1); err != nil {
					return err
				}
			}
		default:
			return ErrBotProtocol
		}
		if _, err := d.Token(); err != nil {
			return ErrBotProtocol
		}
		return nil
	}
	if visit(0) != nil {
		return ErrBotProtocol
	}
	if _, err := d.Token(); err != io.EOF || json.Unmarshal(data, target) != nil {
		return ErrBotProtocol
	}
	return nil
}
