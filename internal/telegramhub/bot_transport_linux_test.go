//go:build linux

package telegramhub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

const fakeOwnerBotToken = "123456:LOCAL_FAKE_TOKEN_NOT_A_CREDENTIAL_000000"

type ownerBotRoundTrip func(*http.Request) (*http.Response, error)

func (f ownerBotRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func ownerBotBinding() botBinding {
	return botBinding{BotID: 123456, OwnerUserID: 900001, OwnerChatID: 900001, BotUsername: "DarkLabHubBot"}
}

func ownerBotHTTP(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func ownerBotJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}

func ownerBotUpdate(id int64) map[string]any {
	return map[string]any{"update_id": id, "message": map[string]any{
		"message_id": int64(200), "date": int64(1800000000), "text": "/rep_credit repA",
		"from":     map[string]any{"id": int64(900001), "is_bot": false, "first_name": "Owner"},
		"chat":     map[string]any{"id": int64(900001), "type": "private"},
		"entities": []any{map[string]any{"type": "bot_command", "offset": 0, "length": 11}},
	}}
}

func ownerBotSentMessage() map[string]any {
	return map[string]any{"message_id": int64(301), "date": int64(1800000001),
		"from":             map[string]any{"id": int64(123456), "is_bot": true},
		"chat":             map[string]any{"id": int64(900001), "type": "private"},
		"text":             "Rendered text without the submitted HTML tags",
		"reply_to_message": map[string]any{"message_id": int64(200)},
	}
}

func ownerBotAPIFor(t *testing.T, transport ownerBotRoundTrip) *botAPI {
	t.Helper()
	api, err := newBotAPI(fakeOwnerBotToken, ownerBotBinding(), transport)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.close)
	return api
}

func assertOwnerBotRequest(t *testing.T, request *http.Request, method string) map[string]json.RawMessage {
	t.Helper()
	if request.URL.Scheme != "https" || request.URL.Host != "api.telegram.org" || request.URL.RawQuery != "" ||
		request.URL.Path != "/bot"+fakeOwnerBotToken+"/"+method || request.Method != http.MethodPost ||
		request.Header.Get("Content-Type") != "application/json" {
		t.Fatal("request escaped the fixed bot API protocol")
	}
	data, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if decodeBotJSON(data, &fields) != nil {
		t.Fatal("invalid request body")
	}
	return fields
}

func TestOwnerBotTransportVerifiesNumericIdentity(t *testing.T) {
	for _, tc := range []struct {
		name string
		user string
		ok   bool
	}{
		{"matching", `{"id":123456,"is_bot":true,"username":"darklabhubbot"}`, true},
		{"wrong bot", `{"id":123457,"is_bot":true,"username":"DarkLabHubBot"}`, false},
		{"human", `{"id":123456,"is_bot":false,"username":"DarkLabHubBot"}`, false},
		{"missing bot flag", `{"id":123456,"username":"DarkLabHubBot"}`, false},
		{"wrong username", `{"id":123456,"is_bot":true,"username":"AnotherBot"}`, false},
		{"null ID", `{"id":null,"is_bot":true,"username":"DarkLabHubBot"}`, false},
		{"duplicate ID", `{"id":1,"id":123456,"is_bot":true,"username":"DarkLabHubBot"}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			api := ownerBotAPIFor(t, func(request *http.Request) (*http.Response, error) {
				calls++
				if len(assertOwnerBotRequest(t, request, "getMe")) != 0 {
					t.Fatal("getMe must have no parameters")
				}
				return ownerBotHTTP(200, `{"ok":true,"result":`+tc.user+`}`), nil
			})
			if err := api.verify(context.Background()); (err == nil) != tc.ok || calls != 1 {
				t.Fatalf("identity result: success=%v calls=%d", err == nil, calls)
			}
		})
	}
}

func TestOwnerBotTransportNormalizesOnlyOrdinaryOwnerCommands(t *testing.T) {
	for _, field := range []string{"forward_origin", "forward_from", "forward_date", "forward_sender_name", "sender_chat",
		"via_bot", "edit_date", "business_connection_id", "sender_business_bot", "guest_query_id", "receiver_user",
		"ephemeral_message_id", "message_thread_id", "is_topic_message", "direct_messages_topic", "reply_to_message",
		"is_automatic_forward", "unknown_future_origin"} {
		t.Run(field, func(t *testing.T) {
			value := ownerBotUpdate(100)
			value["message"].(map[string]any)[field] = nil // Presence alone is rejected.
			update, err := normalizeBotUpdate([]byte(ownerBotJSON(value)))
			if err != nil || update.Message != nil || update.Rejection != "unsupported_update" {
				t.Fatal("unsupported origin reached normalization", err)
			}
		})
	}
	for _, kind := range []string{"edited_message", "channel_post", "edited_channel_post", "business_message", "edited_business_message", "guest_message", "callback_query"} {
		value := ownerBotUpdate(100)
		value[kind], value["message"] = value["message"], nil
		delete(value, "message")
		update, err := normalizeBotUpdate([]byte(ownerBotJSON(value)))
		if err != nil || update.Message != nil || update.Rejection == "" {
			t.Fatal("unsupported update kind accepted", kind, err)
		}
	}
	update, err := normalizeBotUpdate([]byte(ownerBotJSON(ownerBotUpdate(100))))
	if err != nil || update.Message == nil || *update.Message != ownerMessage() {
		t.Fatal("ordinary message not faithfully normalized", err)
	}
	// Fingerprints are stable across object order/spacing without float rounding.
	other, err := normalizeBotUpdate([]byte(` {"message":{"text":"/rep_credit repA","date":1800000000,"message_id":200,"entities":[{"length":11,"offset":0,"type":"bot_command"}],"chat":{"type":"private","id":900001},"from":{"first_name":"Owner","is_bot":false,"id":900001}},"update_id":100} `))
	if err != nil || other.Fingerprint != update.Fingerprint {
		t.Fatal("semantic duplicate fingerprint changed", err)
	}
	for _, raw := range []string{
		`{"update_id":1,"update_id":100,"message":{}}`, `{"update_id":1.5}`, `{"update_id":-1}`,
		`{"update_id":100,"message":{"from":{"id":1,"id":900001}}}`, `{} {}`,
	} {
		if _, err := normalizeBotUpdate([]byte(raw)); err == nil {
			t.Fatal("ambiguous JSON accepted")
		}
	}
}

func TestOwnerBotTransportBoundsPollingAndRequests(t *testing.T) {
	api := ownerBotAPIFor(t, func(request *http.Request) (*http.Response, error) {
		fields := assertOwnerBotRequest(t, request, "getUpdates")
		var offset int64
		var limit, timeout int
		var allowed []string
		if len(fields) != 4 || readRequired(fields, "offset", &offset) != nil || offset != 9007199254740993 ||
			readRequired(fields, "limit", &limit) != nil || limit != 25 || readRequired(fields, "timeout", &timeout) != nil || timeout != 20 ||
			readRequired(fields, "allowed_updates", &allowed) != nil || !reflect.DeepEqual(allowed, []string{"message"}) {
			t.Fatal("poll parameters or integer precision changed")
		}
		return ownerBotHTTP(200, `{"ok":true,"result":[]}`), nil
	})
	if values, err := api.updates(context.Background(), 9007199254740993); err != nil || len(values) != 0 || api.client.Timeout != 35*time.Second {
		t.Fatal("bounded poll failed", err)
	}
	if _, err := api.updates(context.Background(), -1); err != ErrBotProtocol {
		t.Fatal("negative offset accepted", err)
	}
	for _, body := range []string{
		`{"ok":true,"result":null}`, `{"ok":true,"result":{}}`, `{"ok":true,"result":[]}{}`,
		`{"ok":true,"result":` + strings.Repeat("[", 34) + `0` + strings.Repeat("]", 34) + `}`,
		strings.Repeat("x", botMaxResponseBytes+1),
	} {
		api := ownerBotAPIFor(t, func(*http.Request) (*http.Response, error) { return ownerBotHTTP(200, body), nil })
		if _, err := api.updates(context.Background(), 0); err == nil {
			t.Fatal("unbounded or malformed response accepted")
		}
	}
	batch := make([]any, 26)
	for i := range batch {
		batch[i] = ownerBotUpdate(int64(i + 1))
	}
	oversized := ownerBotAPIFor(t, func(*http.Request) (*http.Response, error) {
		return ownerBotHTTP(200, ownerBotJSON(map[string]any{"ok": true, "result": batch})), nil
	})
	if _, err := oversized.updates(context.Background(), 0); err != ErrBotProtocol {
		t.Fatal("oversized update batch accepted", err)
	}
}

func TestOwnerBotTransportNeverRedirectsOrLeaksCredentials(t *testing.T) {
	calls := 0
	api := ownerBotAPIFor(t, func(*http.Request) (*http.Response, error) {
		calls++
		response := ownerBotHTTP(302, `redirected`)
		response.Header.Set("Location", "https://untrusted.invalid/"+fakeOwnerBotToken)
		return response, nil
	})
	if err := api.verify(context.Background()); err == nil || calls != 1 || strings.Contains(err.Error(), fakeOwnerBotToken) {
		t.Fatal("redirect was followed or error exposed request")
	}
	api = ownerBotAPIFor(t, func(request *http.Request) (*http.Response, error) {
		return nil, &url.Error{Op: "Post", URL: request.URL.String(), Err: errors.New("secret=" + fakeOwnerBotToken)}
	})
	if err := api.verify(context.Background()); err == nil || err.Error() != ErrBotTransport.Error() {
		t.Fatal("raw transport error escaped")
	}
	api = ownerBotAPIFor(t, func(*http.Request) (*http.Response, error) {
		return ownerBotHTTP(401, ownerBotJSON(map[string]any{"ok": false, "error_code": 401, "description": fakeOwnerBotToken})), nil
	})
	if err := api.verify(context.Background()); err == nil || err.Error() != ErrBotTransport.Error() {
		t.Fatal("remote description escaped")
	}
	for _, token := range []string{"", "123:short", fakeOwnerBotToken + "\n", "https://another.invalid/", fakeOwnerBotToken + "/getUpdates"} {
		if _, err := newBotAPI(token, ownerBotBinding(), nil); err != ErrInvalidConfig {
			t.Fatal("malformed credential accepted")
		}
	}
	production, err := newBotAPI(fakeOwnerBotToken, ownerBotBinding(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer production.close() // Constructs only; never calls a network method.
	transport := production.client.Transport.(*http.Transport)
	if transport.Proxy != nil || transport.TLSClientConfig.InsecureSkipVerify || transport.MaxConnsPerHost != 1 {
		t.Fatal("production transport allows credential routing override")
	}
}

func TestOwnerBotTransportSendPreservesExactEscapedTextAndValidatesReceipt(t *testing.T) {
	handler, _ := NewOwnerReportHandler(ownerConfig(), reportReaderFunc(func(context.Context, string) (report business.OwnerRepLedgerReport, err error) {
		return validReport(), nil
	}))
	reply, err := handler.Prepare(context.Background(), ownerMessage())
	if err != nil {
		t.Fatal(err)
	}
	api := ownerBotAPIFor(t, func(request *http.Request) (*http.Response, error) {
		fields := assertOwnerBotRequest(t, request, "sendMessage")
		var text, parseMode string
		var chatID int64
		var parameters map[string]json.RawMessage
		var replyID int64
		if len(fields) != 5 || readRequired(fields, "text", &text) != nil || text != reply.Text ||
			readRequired(fields, "parse_mode", &parseMode) != nil || parseMode != "HTML" ||
			readRequired(fields, "chat_id", &chatID) != nil || chatID != 900001 ||
			readRequired(fields, "reply_parameters", &parameters) != nil || len(parameters) != 1 ||
			readRequired(parameters, "message_id", &replyID) != nil || replyID != 200 {
			t.Fatal("outbound request changed text or authority")
		}
		for _, exact := range []string{"&lt;b&gt;Alice &amp; Bob&lt;/b&gt;", "9007199254740993 بایت", "80 واحد"} {
			if !strings.Contains(text, exact) {
				t.Fatal("exact report field lost")
			}
		}
		return ownerBotHTTP(200, ownerBotJSON(map[string]any{"ok": true, "result": ownerBotSentMessage()})), nil
	})
	outcome, err := api.send(context.Background(), reply)
	if err != nil || outcome != (dispatchOutcome{State: "sent", MessageID: 301}) {
		t.Fatal("validated receipt not accepted", outcome, err)
	}
	for name, mutate := range map[string]func(map[string]any){
		"wrong bot":   func(m map[string]any) { m["from"].(map[string]any)["id"] = 999 },
		"wrong chat":  func(m map[string]any) { m["chat"].(map[string]any)["id"] = 999 },
		"group":       func(m map[string]any) { m["chat"].(map[string]any)["type"] = "group" },
		"unsent":      func(m map[string]any) { m["message_id"] = 0 },
		"wrong reply": func(m map[string]any) { m["reply_to_message"].(map[string]any)["message_id"] = 201 },
		"business":    func(m map[string]any) { m["business_connection_id"] = "foreign" },
	} {
		t.Run(name, func(t *testing.T) {
			message := ownerBotSentMessage()
			mutate(message)
			api := ownerBotAPIFor(t, func(*http.Request) (*http.Response, error) {
				return ownerBotHTTP(200, ownerBotJSON(map[string]any{"ok": true, "result": message})), nil
			})
			if outcome, err := api.send(context.Background(), reply); err == nil || outcome.State != "unknown" || outcome.MessageID != 0 {
				t.Fatal("invalid receipt marked sent", outcome, err)
			}
		})
	}
}

func TestOwnerBotTransportFailureOutcomesAreConservative(t *testing.T) {
	reply := PreparedOwnerReply{OwnerUserID: 900001, ChatID: 900001, UpdateID: 100, ReplyToMessageID: 200, ParseMode: "HTML", Text: "report"}
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"rate limited", `{"ok":false,"error_code":429,"description":"secret","parameters":{"retry_after":10}}`, "failed", 429},
		{"forbidden", `{"ok":false,"error_code":403,"description":"secret"}`, "failed", 403},
		{"server error", `{"ok":false,"error_code":500,"description":"secret"}`, "unknown", 500},
		{"malformed", `{"ok":true}`, "unknown", 200},
		{"invalid definitive error", `{"ok":false,"error_code":401}`, "unknown", 500},
		{"no clear rejection", `bad request`, "unknown", 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			api := ownerBotAPIFor(t, func(*http.Request) (*http.Response, error) {
				calls++
				return ownerBotHTTP(tc.status, tc.body), nil
			})
			outcome, err := api.send(context.Background(), reply)
			if err == nil || outcome.State != tc.want || calls != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatal("unsafe send retry/classification", outcome, err, calls)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	api := ownerBotAPIFor(t, func(request *http.Request) (*http.Response, error) {
		cancel()
		return nil, request.Context().Err()
	})
	if outcome, err := api.send(ctx, reply); err == nil || outcome.State != "unknown" {
		t.Fatal("cancelled request incorrectly marked failed or sent", outcome, err)
	}
	api = ownerBotAPIFor(t, func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	timeout, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if outcome, err := api.send(timeout, reply); err == nil || outcome.State != "unknown" || !errors.Is(timeout.Err(), context.DeadlineExceeded) {
		t.Fatal("timed out send incorrectly marked definite", outcome, err)
	}
}

func TestOwnerBotTransportFatalErrorsDependOnMethod(t *testing.T) {
	for _, tc := range []struct {
		method            string
		code              int
		fatal, perMessage bool
	}{
		{"sendMessage", 400, false, true}, {"sendMessage", 403, false, true},
		{"sendMessage", 401, true, false}, {"getUpdates", 409, true, false},
		{"getUpdates", 400, true, false}, {"getMe", 403, true, false},
	} {
		api := ownerBotAPIFor(t, func(*http.Request) (*http.Response, error) {
			return ownerBotHTTP(tc.code, ownerBotJSON(map[string]any{"ok": false, "error_code": tc.code})), nil
		})
		_, err := api.call(context.Background(), tc.method, struct{}{})
		var failure *botAPIError
		if !errors.As(err, &failure) || !failure.definitive || failure.fatal != tc.fatal || failure.perMessage != tc.perMessage {
			t.Fatal("incorrect operation-specific rejection", tc.method, tc.code, err)
		}
	}
}
