package telegrambot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type fakeRoundTripper func(*http.Request) (*http.Response, error)

func (f fakeRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fakeResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const syntheticToken = "100:ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func privateUpdate(id int64, text string) []byte {
	b, _ := json.Marshal(map[string]any{"update_id": id, "message": map[string]any{"message_id": 9, "date": 1700000000, "from": map[string]any{"id": 2001, "is_bot": false, "language_code": "fa"}, "chat": map[string]any{"id": 2001, "type": "private"}, "text": text}})
	return b
}

func TestNormalizeTrustedPrivateIdentityAndFinancialReceipts(t *testing.T) {
	raw := privateUpdate(3, "/start")
	in, err := normalizeUpdate(raw, 100, 1700000100)
	if err != nil || in.Input == nil || in.Input.Identity != (Identity{100, 2001, 2001}) || in.Input.Kind != "message" {
		t.Fatalf("private message rejected: %+v %v", in, err)
	}
	for name, change := range map[string]func(map[string]any){
		"group":              func(m map[string]any) { m["chat"] = map[string]any{"id": 2001, "type": "group"} },
		"chat user mismatch": func(m map[string]any) { m["chat"] = map[string]any{"id": 2002, "type": "private"} },
		"bot sender":         func(m map[string]any) { m["from"] = map[string]any{"id": 2001, "is_bot": true} },
		"forwarded":          func(m map[string]any) { m["forward_origin"] = map[string]any{"type": "hidden_user"} },
		"business":           func(m map[string]any) { m["business_connection_id"] = "connection" },
		"edited":             func(m map[string]any) { m["edit_date"] = 1700000010 },
		"topic":              func(m map[string]any) { m["message_thread_id"] = 1 },
		"guest":              func(m map[string]any) { m["guest_query_id"] = "guest" },
	} {
		t.Run(name, func(t *testing.T) {
			var update map[string]any
			json.Unmarshal(raw, &update)
			change(update["message"].(map[string]any))
			b, _ := json.Marshal(update)
			v, e := normalizeUpdate(b, 100, 1)
			if e != nil || v.Input != nil || v.Rejection == "" {
				t.Fatalf("untrusted origin accepted: %+v %v", v, e)
			}
		})
	}
	for _, kind := range []string{"successful_payment", "refunded_payment"} {
		var update map[string]any
		json.Unmarshal(raw, &update)
		m := update["message"].(map[string]any)
		delete(m, "text")
		m[kind] = map[string]any{"currency": "XTR", "total_amount": 25, "invoice_payload": "bound-order-123", "telegram_payment_charge_id": "known-charge-123"}
		b, _ := json.Marshal(update)
		v, e := normalizeUpdate(b, 100, 1)
		want := "payment"
		if kind == "refunded_payment" {
			want = "refund"
		}
		if e != nil || v.Input == nil || v.Input.Kind != want || v.Input.Payment.Amount != 25 {
			t.Fatalf("financial normalization: %+v %v", v, e)
		}
		m[kind].(map[string]any)["currency"] = "USD"
		b, _ = json.Marshal(update)
		v, e = normalizeUpdate(b, 100, 1)
		if e != nil || v.Input != nil {
			t.Fatal("foreign currency accepted")
		}
	}
	if _, err = normalizeUpdate([]byte(`{"update_id":1,"message":{"from":{"id":1,"id":2}}}`), 100, 1); err == nil {
		t.Fatal("duplicate JSON key accepted")
	}
}

func TestCallbackRequiresAccessibleMessageFromExactBot(t *testing.T) {
	base := map[string]any{"update_id": 2, "callback_query": map[string]any{"id": "query-1", "from": map[string]any{"id": 2001, "is_bot": false}, "data": "d1_01234567890123456789012345678901", "message": map[string]any{"message_id": 99, "date": 1, "from": map[string]any{"id": 100, "is_bot": true}, "chat": map[string]any{"id": 2001, "type": "private"}}}}
	raw, _ := json.Marshal(base)
	v, e := normalizeUpdate(raw, 100, 1)
	if e != nil || v.Input == nil || v.Input.Kind != "callback" {
		t.Fatal("valid callback", e)
	}
	for _, change := range []func(map[string]any){func(q map[string]any) { q["inline_message_id"] = "inline" }, func(q map[string]any) { q["message"].(map[string]any)["date"] = 0 }, func(q map[string]any) {
		q["message"].(map[string]any)["from"] = map[string]any{"id": 101, "is_bot": true}
	}, func(q map[string]any) { q["from"] = map[string]any{"id": 2002, "is_bot": false} }} {
		var x map[string]any
		json.Unmarshal(raw, &x)
		change(x["callback_query"].(map[string]any))
		b, _ := json.Marshal(x)
		v, e := normalizeUpdate(b, 100, 1)
		if e != nil || v.Input != nil {
			t.Fatal("cross identity callback accepted")
		}
	}
}

func TestTransportFixedOriginRefusesWebhookAndRedactsErrors(t *testing.T) {
	for _, webhook := range []string{"", "https://production.invalid/hook"} {
		methods := []string{}
		a, err := newAPI(100, syntheticToken, fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
			if r.URL.Scheme != "https" || r.URL.Host != "api.telegram.org" || r.Method != "POST" {
				t.Fatal("unfixed request origin")
			}
			method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			methods = append(methods, method)
			switch method {
			case "getMe":
				return fakeResponse(200, `{"ok":true,"result":{"id":100,"is_bot":true,"username":"DarkLabBot"}}`), nil
			case "getWebhookInfo":
				return fakeResponse(200, fmt.Sprintf(`{"ok":true,"result":{"url":%q,"pending_update_count":0}}`, webhook)), nil
			default:
				t.Fatalf("unexpected mutation %s", method)
				return nil, nil
			}
		}))
		if err != nil {
			t.Fatal(err)
		}
		err = a.verify(context.Background())
		if webhook == "" && (err != nil || !a.verified) {
			t.Fatal(err)
		}
		if webhook != "" && (!errors.Is(err, ErrWebhook) || a.verified) {
			t.Fatal("webhook takeover allowed", err)
		}
		if strings.Join(methods, ",") != "getMe,getWebhookInfo" {
			t.Fatal(methods)
		}
		a.close()
	}
	secretError := "https://api.telegram.org/bot" + syntheticToken + "/sendMessage"
	a, _ := newAPI(100, syntheticToken, fakeRoundTripper(func(*http.Request) (*http.Response, error) { return nil, errors.New(secretError) }))
	a.verified = true
	body, _ := json.Marshal(wireMessage{ChatID: 2001, Text: "private content", ParseMode: "HTML"})
	out, err := a.dispatch(context.Background(), delivery{BotID: 100, ChatID: 2001, Kind: "message", Payload: body})
	if err == nil || out.State != "unknown" || strings.Contains(fmt.Sprintf("%+v", err), syntheticToken) {
		t.Fatalf("secret or ambiguous retry: %+v %v", out, err)
	}
}

func TestTransportOnlyExplicit429RetriesAndReceiptScope(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body, state string
		retry       bool
	}{
		{"confirmed", 200, `{"ok":true,"result":{"message_id":7,"from":{"id":100,"is_bot":true},"chat":{"id":2001,"type":"private"}}}`, "sent", false},
		{"wrong recipient", 200, `{"ok":true,"result":{"message_id":7,"from":{"id":100,"is_bot":true},"chat":{"id":2002,"type":"private"}}}`, "unknown", false},
		{"explicit rate limit", 429, `{"ok":false,"error_code":429,"description":"token must never escape","parameters":{"retry_after":2}}`, "retry", true},
		{"server error", 502, `{"ok":false,"error_code":502}`, "unknown", false},
		{"unstructured rejection", 429, `rate limited`, "unknown", false},
		{"blocked", 403, `{"ok":false,"error_code":403}`, "failed", false},
		{"redirect", 302, `{"ok":false,"error_code":302}`, "unknown", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newAPI(100, syntheticToken, fakeRoundTripper(func(*http.Request) (*http.Response, error) { return fakeResponse(tc.status, tc.body), nil }))
			a.verified = true
			body, _ := json.Marshal(wireMessage{ChatID: 2001, Text: "hello", ParseMode: "HTML"})
			out, _ := a.dispatch(context.Background(), delivery{BotID: 100, ChatID: 2001, Kind: "message", Payload: body})
			if out.State != tc.state || (out.RetryAfter > 0) != tc.retry {
				t.Fatalf("got %+v", out)
			}
		})
	}
}

func TestCheckoutBindingIsPrivateAndColorsRemainNative(t *testing.T) {
	p := Payment{Payload: "order-123", Currency: "XTR", Amount: 25}
	a, _ := newAPI(100, syntheticToken, fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "_payment_binding") || strings.Contains(string(body), "order-123") {
			t.Fatal("private binding leaked to query answer")
		}
		var m wireCheckout
		if json.Unmarshal(body, &m) != nil || !m.OK || m.QueryID != "query-1" {
			t.Fatal("wrong checkout answer")
		}
		return fakeResponse(200, `{"ok":true,"result":true}`), nil
	}))
	a.verified = true
	raw, _ := json.Marshal(wireCheckout{QueryID: "query-1", OK: true, Binding: &p})
	out, err := a.dispatch(context.Background(), delivery{BotID: 100, ChatID: 2001, Kind: "checkout", Payload: raw})
	if err != nil || out.State != "sent" {
		t.Fatal(out, err)
	}
	k := &wireKeyboard{Rows: [][]wireButton{{{Text: "خرید", Style: "success", Callback: "d1_token"}, {Text: "خانه", Style: "primary", Callback: "d1_home"}}, {{Text: "کپی", Copy: &copyText{Text: "link"}}, {Text: "لغو", Style: "danger", URL: "https://t.me/DarkLabBot"}}}}
	if validateWireKeyboard(k) != nil {
		t.Fatal("native styled keyboard rejected")
	}
	k.Rows[0][0].Style = "purple"
	if validateWireKeyboard(k) == nil {
		t.Fatal("unsupported color accepted")
	}
}
