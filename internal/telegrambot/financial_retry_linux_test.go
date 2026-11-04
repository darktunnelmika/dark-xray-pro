//go:build linux

package telegrambot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

type financialRetryBackend struct {
	fakeBackend
	captureErr error
	captured   []Incoming
}

func (b *financialRetryBackend) CaptureFinancialEvent(_ context.Context, in Incoming) error {
	b.captured = append(b.captured, in)
	return b.captureErr
}

func financialRetryInput(kind string) Incoming {
	return Incoming{UpdateID: 51, Identity: Identity{BotID: 100, UserID: 2001, ChatID: 2001}, Kind: kind, Payment: &Payment{Payload: "immutable-order-reference", Currency: "XTR", Amount: 25, TelegramChargeID: "immutable-provider-charge"}, ReceivedUnix: time.Now().Unix()}
}

func TestFinancialCaptureTransientRetryRetainsExactPaymentAndRefund(t *testing.T) {
	for _, kind := range []string{"payment", "refund"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			b := &financialRetryBackend{captureErr: context.DeadlineExceeded}
			w, _ := newFakeWorker(t, b, fakeRoundTripper(func(*http.Request) (*http.Response, error) {
				t.Fatal("network attempted by capture retry test")
				return nil, nil
			}))
			in := financialRetryInput(kind)
			persistInput(t, w.journal, in)
			if err := w.processInput(ctx, in); err != nil {
				t.Fatal(err)
			}
			var state, reason string
			var attempts, next int64
			var raw []byte
			if err := w.journal.db.QueryRow(`SELECT state,reason,attempts,next_unix_ms,input_json FROM bot_inbox WHERE bot_id=100 AND update_id=51`).Scan(&state, &reason, &attempts, &next, &raw); err != nil {
				t.Fatal(err)
			}
			var retained Incoming
			if state != "pending" || reason != "financial_capture_retry" || attempts < 1 || next <= time.Now().UnixMilli() || json.Unmarshal(raw, &retained) != nil || retained.Identity != in.Identity || retained.Payment == nil || *retained.Payment != *in.Payment || retained.Kind != kind {
				t.Fatal("transient financial fact stranded or altered", state, reason, attempts, next)
			}
			if b.handleCount.Load() != 0 {
				t.Fatal("fulfillment ran before financial capture")
			}
			if _, found, err := w.journal.nextInput(ctx, 100); err != nil || found {
				t.Fatal("financial retry ignored durable backoff", found, err)
			}
			if cursor, err := w.journal.cursor(ctx, 100); err != nil || cursor != 52 {
				t.Fatal("durable acknowledgement cursor", cursor, err)
			}
			if _, err := w.journal.db.Exec(`UPDATE bot_inbox SET next_unix_ms=0 WHERE bot_id=100 AND update_id=51`); err != nil {
				t.Fatal(err)
			}
			b.captureErr = nil
			retry, found, err := w.journal.nextInput(ctx, 100)
			if err != nil || !found {
				t.Fatal(found, err)
			}
			if err = w.processInput(ctx, retry); err != nil {
				t.Fatal(err)
			}
			if len(b.captured) != 2 || b.handleCount.Load() != 1 || *b.captured[0].Payment != *b.captured[1].Payment || b.captured[0].Identity != b.captured[1].Identity || b.captured[0].UpdateID != b.captured[1].UpdateID {
				t.Fatal("retry changed financial identity or repeated application")
			}
			if err = w.journal.db.QueryRow(`SELECT state,input_json FROM bot_inbox WHERE bot_id=100 AND update_id=51`).Scan(&state, &raw); err != nil || state != "done" || raw != nil {
				t.Fatal("successful capture not completed", state, err)
			}
			if _, found, err = w.journal.nextInput(ctx, 100); err != nil || found {
				t.Fatal("completed financial event retried", found, err)
			}
		})
	}
}

func TestFinancialCaptureBackoffSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	b := &financialRetryBackend{captureErr: errors.New("synthetic database is locked")}
	noNetwork := fakeRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("network attempted by restart test")
		return nil, nil
	})
	w, path := newFakeWorker(t, b, noNetwork)
	in := financialRetryInput("payment")
	persistInput(t, w.journal, in)
	if err := w.processInput(ctx, in); err != nil {
		t.Fatal(err)
	}
	var attempts, next int64
	if err := w.journal.db.QueryRow(`SELECT attempts,next_unix_ms FROM bot_inbox WHERE bot_id=100 AND update_id=51`).Scan(&attempts, &next); err != nil || attempts < 1 || next <= time.Now().UnixMilli() {
		t.Fatal("missing first backoff", attempts, next, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openWorker(fileConfig{Version: 1, Purpose: ConfigPurpose, Bots: []botConfig{{BotID: 100, Token: syntheticToken}}}, path, b, noNetwork)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var restoredAttempts, restoredNext int64
	if err = reopened.journal.db.QueryRow(`SELECT attempts,next_unix_ms FROM bot_inbox WHERE bot_id=100 AND update_id=51`).Scan(&restoredAttempts, &restoredNext); err != nil || restoredAttempts != attempts || restoredNext != next {
		t.Fatal("restart erased retry state", restoredAttempts, restoredNext, err)
	}
	if _, found, err := reopened.journal.nextInput(ctx, 100); err != nil || found {
		t.Fatal("restart bypassed financial backoff", found, err)
	}
	if _, err = reopened.journal.db.Exec(`UPDATE bot_inbox SET next_unix_ms=0 WHERE bot_id=100 AND update_id=51`); err != nil {
		t.Fatal(err)
	}
	b.captureErr = nil
	retry, found, err := reopened.journal.nextInput(ctx, 100)
	if err != nil || !found {
		t.Fatal(found, err)
	}
	if err = reopened.processInput(ctx, retry); err != nil {
		t.Fatal(err)
	}
	if len(b.captured) != 2 || b.handleCount.Load() != 1 {
		t.Fatal("restart lost or repeated paid work")
	}
}

func TestFinancialCapturePermanentMismatchRetainsReviewEvidence(t *testing.T) {
	for _, failure := range []error{ErrFinancialMismatch, ErrAccess, ErrProtocol} {
		t.Run(failure.Error(), func(t *testing.T) {
			ctx := context.Background()
			b := &financialRetryBackend{captureErr: failure}
			w, _ := newFakeWorker(t, b, fakeRoundTripper(func(*http.Request) (*http.Response, error) { t.Fatal("network attempted"); return nil, nil }))
			in := financialRetryInput("refund")
			persistInput(t, w.journal, in)
			if err := w.processInput(ctx, in); err != nil {
				t.Fatal(err)
			}
			var state string
			var raw []byte
			if err := w.journal.db.QueryRow(`SELECT state,input_json FROM bot_inbox WHERE bot_id=100 AND update_id=51`).Scan(&state, &raw); err != nil || state != "review" || len(raw) == 0 {
				t.Fatal("unmatched refund evidence erased or retried", state, err)
			}
			if b.handleCount.Load() != 0 {
				t.Fatal("unmatched refund reached business handler")
			}
			if _, found, err := w.journal.nextInput(ctx, 100); err != nil || found {
				t.Fatal("permanent mismatch automatically retried", found, err)
			}
		})
	}
}
