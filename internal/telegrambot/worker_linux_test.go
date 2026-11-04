//go:build linux

package telegrambot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeBackend struct {
	denied                                          atomic.Bool
	captureCount, handleCount, ackCount, guardCount atomic.Int32
	captureError, guardError                        error
	afterHandle                                     func()
	result                                          Result
}

func (b *fakeBackend) Resolve(_ context.Context, id Identity, _ bool) (Access, error) {
	if b.denied.Load() {
		return Access{}, ErrAccess
	}
	return testAccess(id.BotID, id.UserID), nil
}
func (b *fakeBackend) Handle(context.Context, Request) (Result, error) {
	b.handleCount.Add(1)
	if b.afterHandle != nil {
		b.afterHandle()
	}
	if b.result.Screen != nil || b.result.Invoice != nil || b.result.Checkout != nil {
		return b.result, nil
	}
	return oneScreen("Welcome", [][]Button{{actionButton("Buy", "success", "products", "", "")}}), nil
}
func (b *fakeBackend) CaptureFinancialEvent(context.Context, Incoming) error {
	b.captureCount.Add(1)
	return b.captureError
}
func (b *fakeBackend) AcknowledgeDelivery(context.Context, Identity, DeliveryProof, int64) error {
	b.ackCount.Add(1)
	return nil
}
func (b *fakeBackend) ValidateFinancialDispatch(context.Context, Identity, Payment) error {
	b.guardCount.Add(1)
	return b.guardError
}

func newFakeWorker(t *testing.T, b Backend, rt http.RoundTripper) (*Worker, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.sqlite")
	w, err := openWorker(fileConfig{Version: 1, Purpose: ConfigPurpose, Bots: []botConfig{{BotID: 100, Token: syntheticToken}}}, path, b, rt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w, path
}
func waitFor(t *testing.T, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for durable worker state")
}

func TestWorkerCapturesMoneyBeforeRevokedAuthority(t *testing.T) {
	for _, kind := range []string{"payment", "refund"} {
		t.Run(kind, func(t *testing.T) {
			b := &fakeBackend{}
			b.denied.Store(true)
			w, _ := newFakeWorker(t, b, fakeRoundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("network attempted"); return nil, nil }))
			in := Incoming{UpdateID: 1, Identity: Identity{100, 2001, 2001}, Kind: kind, Payment: &Payment{Payload: "order", Currency: "XTR", Amount: 25, TelegramChargeID: "charge"}, ReceivedUnix: time.Now().Unix()}
			persistInput(t, w.journal, in)
			if err := w.processInput(context.Background(), in); err != nil {
				t.Fatal(err)
			}
			if b.captureCount.Load() != 1 || b.handleCount.Load() != 0 {
				t.Fatal("financial capture behind authorization")
			}
			var state string
			var raw []byte
			if w.journal.db.QueryRow("SELECT state,input_json FROM bot_inbox").Scan(&state, &raw) != nil || state != "review" || len(raw) == 0 {
				t.Fatal("revoked financial receipt lost")
			}
		})
	}
	b := &fakeBackend{captureError: ErrAccess}
	w, _ := newFakeWorker(t, b, fakeRoundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("network attempted"); return nil, nil }))
	in := Incoming{UpdateID: 2, Identity: Identity{100, 2001, 2001}, Kind: "payment", Payment: &Payment{Payload: "unmatched", Currency: "XTR", Amount: 99, TelegramChargeID: "charge"}, ReceivedUnix: time.Now().Unix()}
	persistInput(t, w.journal, in)
	if err := w.processInput(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if b.handleCount.Load() != 0 {
		t.Fatal("failed capture reached fulfillment")
	}
}

func TestWorkerPollCommitSendReceiptAndNoSecretsInJournal(t *testing.T) {
	b := &fakeBackend{}
	var polls, sends atomic.Int32
	var w *Worker
	rt := fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		switch method {
		case "getMe":
			return fakeResponse(200, `{"ok":true,"result":{"id":100,"is_bot":true,"username":"DarkLabBot"}}`), nil
		case "getWebhookInfo":
			return fakeResponse(200, `{"ok":true,"result":{"url":"","pending_update_count":0}}`), nil
		case "getUpdates":
			body, _ := io.ReadAll(r.Body)
			var p struct {
				Offset int64 `json:"offset"`
			}
			json.Unmarshal(body, &p)
			if polls.Add(1) == 1 {
				if p.Offset != 0 {
					t.Error("initial cursor")
				}
				return fakeResponse(200, `{"ok":true,"result":[`+string(privateUpdate(9, "/start"))+`]}`), nil
			}
			cursor, err := w.journal.cursor(context.Background(), 100)
			if err != nil || p.Offset != 10 || cursor != 10 {
				t.Error("cursor acknowledged before commit", p.Offset, cursor, err)
			}
			<-r.Context().Done()
			return nil, r.Context().Err()
		case "sendMessage":
			sends.Add(1)
			return fakeResponse(200, `{"ok":true,"result":{"message_id":33,"from":{"id":100,"is_bot":true},"chat":{"id":2001,"type":"private"}}}`), nil
		default:
			t.Error("unexpected method", method)
			return nil, errors.New("unsupported fake method")
		}
	})
	var path string
	w, path = newFakeWorker(t, b, rt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	waitFor(t, func() bool {
		var state string
		return w.journal.db.QueryRow("SELECT state FROM bot_outbox WHERE kind='message'").Scan(&state) == nil && state == "sent"
	})
	statuses := w.Statuses()
	if len(statuses) != 1 || statuses[0].State != "polling" || statuses[0].LastPollUnix <= 0 {
		t.Fatal("false or absent polling status", statuses)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := w.Wait(context.Background()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if sends.Load() != 1 || b.handleCount.Load() != 1 {
		t.Fatal("duplicate effect")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(raw), syntheticToken) {
		t.Fatal("credential entered journal", err)
	}
}

func TestWorkerRechecksAuthorityAndFinancialTermsBeforeSend(t *testing.T) {
	for _, mode := range []string{"revoked", "changed_price"} {
		t.Run(mode, func(t *testing.T) {
			b := &fakeBackend{}
			if mode == "revoked" {
				b.afterHandle = func() { b.denied.Store(true) }
			} else {
				b.result = Result{Invoice: &Invoice{Title: "Plan", Description: "30 days", Payload: "order", Currency: "XTR", Amount: 25}}
				b.guardError = ErrAccess
			}
			var sent atomic.Int32
			rt := idleAPI(t, func(r *http.Request) (*http.Response, error) {
				sent.Add(1)
				return fakeResponse(200, `{"ok":true,"result":true}`), nil
			})
			w, _ := newFakeWorker(t, b, rt)
			in := Incoming{UpdateID: 1, Identity: Identity{100, 2001, 2001}, Kind: "message", Text: "/start", ReceivedUnix: time.Now().Unix()}
			persistInput(t, w.journal, in)
			if err := w.processInput(context.Background(), in); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- w.Run(ctx) }()
			waitFor(t, func() bool {
				var state string
				return w.journal.db.QueryRow("SELECT state FROM bot_outbox WHERE id='100:1:result'").Scan(&state) == nil && state == "cancelled"
			})
			cancel()
			<-done
			if sent.Load() != 0 {
				t.Fatal("revoked/stale invoice sent")
			}
			if mode == "changed_price" && b.guardCount.Load() != 1 {
				t.Fatal("invoice terms not rechecked")
			}
		})
	}
}

func idleAPI(t *testing.T, send fakeRoundTripper) http.RoundTripper {
	t.Helper()
	return fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			return fakeResponse(200, `{"ok":true,"result":{"id":100,"is_bot":true}}`), nil
		case strings.HasSuffix(r.URL.Path, "/getWebhookInfo"):
			return fakeResponse(200, `{"ok":true,"result":{"url":"","pending_update_count":0}}`), nil
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			<-r.Context().Done()
			return nil, r.Context().Err()
		default:
			return send(r)
		}
	})
}

func TestWorkerNeverSendsExpiredNativeProofOrAcknowledgesIt(t *testing.T) {
	b := &fakeBackend{}
	var sent atomic.Int32
	w, _ := newFakeWorker(t, b, idleAPI(t, func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		if strings.Contains(string(raw), "synthetic-secret") {
			t.Error("stale credential dispatched")
		}
		sent.Add(1)
		return fakeResponse(200, `{"ok":true,"result":{"message_id":4,"from":{"id":100,"is_bot":true},"chat":{"id":2001,"type":"private"}}}`), nil
	}))
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: 1, Identity: a.Identity, Kind: "message", Text: "deliver", ReceivedUnix: time.Now().Unix()}
	persistInput(t, w.journal, in)
	proof := &DeliveryProof{ClientID: "client", SHA256: strings.Repeat("a", 64), TermsSHA256: strings.Repeat("b", 64), ExpiresUnixNano: time.Now().Add(time.Minute).UnixNano()}
	if err := w.journal.complete(context.Background(), in, a, Result{Receipt: true, Delivery: proof, Screen: &Screen{Text: "synthetic-secret"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := w.journal.db.Exec("UPDATE bot_delivery_proofs SET expires_unix_nano=1"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	waitFor(t, func() bool {
		var state string
		return w.journal.db.QueryRow("SELECT state FROM bot_outbox WHERE id='100:1:result'").Scan(&state) == nil && state == "cancelled"
	})
	cancel()
	<-done
	if b.ackCount.Load() != 0 {
		t.Fatal("expired proof acknowledged")
	}
}

func TestWorkerVerificationFailureReportsKnownRetainedFinancialBacklog(t *testing.T) {
	b := &fakeBackend{}
	var calls atomic.Int32
	w, _ := newFakeWorker(t, b, fakeRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/getMe") {
			t.Error("verified failure reached another API")
		}
		return fakeResponse(401, `{"ok":false,"error_code":401}`), nil
	}))
	in := Incoming{UpdateID: 1, Identity: Identity{100, 2001, 2001}, Kind: "payment", Payment: &Payment{Payload: "bound-order", Currency: "XTR", Amount: 25, TelegramChargeID: "charge"}, ReceivedUnix: time.Now().Unix()}
	persistInput(t, w.journal, in)
	if err := w.journal.rejectInput(context.Background(), in, "requires_review", true); err != nil {
		t.Fatal(err)
	}
	if s := w.Statuses(); len(s) != 1 || s[0].QueueStatusAvailable {
		t.Fatal("unread queue reported as known zero", s)
	}
	if err := w.Run(context.Background()); err == nil {
		t.Fatal("invalid credential verification succeeded")
	}
	s := w.Statuses()
	if len(s) != 1 || s[0].State != "error" || !s[0].QueueStatusAvailable || s[0].ReviewPending != 1 || s[0].LastPollUnix != 0 || calls.Load() != 1 {
		t.Fatal("verification failure hid retained money review", s, calls.Load())
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := w.refreshBacklog(cancelled, 100); err == nil {
		t.Fatal("cancelled read succeeded")
	}
	if w.Statuses()[0].QueueStatusAvailable {
		t.Fatal("failed queue read left stale counts marked current")
	}
}
