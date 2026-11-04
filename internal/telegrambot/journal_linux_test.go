//go:build linux

package telegrambot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testJournal(t *testing.T, ids ...int64) (*journal, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal.sqlite")
	j, err := openJournal(path, ids)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return j, path
}
func testAccess(bot, user int64) Access {
	return Access{Identity: Identity{bot, user, user}, StoreID: "shop", BrandName: "DARK", StoreRevision: 1, RepresentativeID: "rep", CustomerID: "customer", Role: "customer"}
}
func persistInput(t *testing.T, j *journal, in Incoming) {
	t.Helper()
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	if err := j.ingest(context.Background(), in.Identity.BotID, []received{{ID: in.UpdateID, Fingerprint: hex.EncodeToString(sum[:]), Input: &in}}); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := j.nextInput(context.Background(), in.Identity.BotID)
	if err != nil || !ok || claimed.UpdateID != in.UpdateID {
		t.Fatal("claim input", ok, err)
	}
}
func nextQueued(t *testing.T, j *journal, bot int64) delivery {
	t.Helper()
	d, ok, err := j.nextDelivery(context.Background(), bot)
	if err != nil || !ok {
		t.Fatal("next delivery", ok, err)
	}
	return d
}

func TestJournalCallbackScopeAndStableMutationEvent(t *testing.T) {
	ctx := context.Background()
	j, _ := testJournal(t, 100, 101)
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: 1, Identity: a.Identity, Kind: "message", Text: "/start", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	result := oneScreen("Menu", [][]Button{{actionButton("Buy", "success", "buy", "product", "")}})
	if err := j.complete(ctx, in, a, result); err != nil {
		t.Fatal(err)
	}
	d := nextQueued(t, j, 100)
	var m wireMessage
	if decodeJSON(d.Payload, &m) != nil {
		t.Fatal("payload")
	}
	token := m.Markup.Rows[0][0].Callback
	if len(token) > 64 || strings.Contains(token, "product") {
		t.Fatal("callback leaked authority or exceeded bytes")
	}
	cb := Incoming{Identity: a.Identity, Kind: "callback", MessageID: 44, CallbackData: token}
	if _, _, err := j.callback(ctx, cb, a); !errors.Is(err, ErrCallback) {
		t.Fatal("unconfirmed menu usable")
	}
	if err := j.claim(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := j.finish(ctx, d, DeliveryOutcome{State: "sent", MessageID: 44}); err != nil {
		t.Fatal(err)
	}
	action, event, err := j.callback(ctx, cb, a)
	if err != nil || action.Name != "buy" {
		t.Fatal(action, err)
	}
	_, again, err := j.callback(ctx, cb, a)
	if err != nil || event != again {
		t.Fatal("repeated button changed mutation event")
	}
	for name, change := range map[string]func(*Incoming, *Access){
		"other user":             func(i *Incoming, a *Access) { i.Identity.UserID++; i.Identity.ChatID++; a.Identity = i.Identity },
		"other bot":              func(i *Incoming, a *Access) { i.Identity.BotID++; a.Identity = i.Identity },
		"other message":          func(i *Incoming, a *Access) { i.MessageID++ },
		"changed store revision": func(i *Incoming, a *Access) { a.StoreRevision++ },
		"changed role":           func(i *Incoming, a *Access) { a.Role = "admin" },
		"changed permissions":    func(i *Incoming, a *Access) { a.Permissions = []string{"finance"} },
	} {
		t.Run(name, func(t *testing.T) {
			i, access := cb, a
			change(&i, &access)
			if _, _, err := j.callback(ctx, i, access); !errors.Is(err, ErrCallback) {
				t.Fatal("scope not rejected", err)
			}
		})
	}
	if _, err := j.db.Exec("UPDATE bot_callbacks SET expires_unix=1"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := j.callback(ctx, cb, a); !errors.Is(err, ErrCallback) {
		t.Fatal("expired callback accepted")
	}
}

func TestJournalCrashUnknownIsNotResentOrAcknowledged(t *testing.T) {
	ctx := context.Background()
	j, path := testJournal(t, 100)
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: 7, Identity: a.Identity, Kind: "message", Text: "deliver", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	proof := &DeliveryProof{ClientID: "client", SHA256: strings.Repeat("a", 64), TermsSHA256: strings.Repeat("b", 64), ExpiresUnixNano: time.Now().Add(time.Minute).UnixNano()}
	if err := j.complete(ctx, in, a, Result{Receipt: true, Delivery: proof, Screen: &Screen{Text: "synthetic-private-config"}}); err != nil {
		t.Fatal(err)
	}
	d := nextQueued(t, j, 100)
	if err := j.claim(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := openJournal(path, []int64{100})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, exists, err := reopened.nextDelivery(ctx, 100); err != nil || exists {
		t.Fatal("ambiguous send requeued", exists, err)
	}
	if _, exists, err := reopened.nextAck(ctx, 100); err != nil || exists {
		t.Fatal("ambiguous delivery acknowledged", exists, err)
	}
	var state, proofState string
	var payload []byte
	if reopened.db.QueryRow("SELECT state,payload_json FROM bot_outbox WHERE id=?", d.ID).Scan(&state, &payload) != nil || state != "unknown" || len(payload) == 0 {
		t.Fatal("unknown evidence lost")
	}
	if reopened.db.QueryRow("SELECT state FROM bot_delivery_proofs WHERE outbox_id=?", d.ID).Scan(&proofState) != nil || proofState != "review" {
		t.Fatal("proof accepted without receipt")
	}
}

func TestJournalConfirmedDeliveryBindsTermsAndClearsSecretPayload(t *testing.T) {
	ctx := context.Background()
	j, _ := testJournal(t, 100)
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: 8, Identity: a.Identity, Kind: "message", Text: "deliver", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	proof := &DeliveryProof{ClientID: "client", SHA256: strings.Repeat("c", 64), TermsSHA256: strings.Repeat("d", 64), ExpiresUnixNano: time.Now().Add(time.Minute).UnixNano()}
	if err := j.complete(ctx, in, a, Result{Receipt: true, Delivery: proof, Document: &Document{Name: "service.txt", Content: []byte("synthetic-secret")}}); err != nil {
		t.Fatal(err)
	}
	d := nextQueued(t, j, 100)
	if d.Proof == nil || d.Proof.TermsSHA256 != proof.TermsSHA256 {
		t.Fatal("terms binding lost")
	}
	if err := j.claim(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := j.finish(ctx, d, DeliveryOutcome{State: "sent", MessageID: 321}); err != nil {
		t.Fatal(err)
	}
	ack, exists, err := j.nextAck(ctx, 100)
	if err != nil || !exists || ack.MessageID != 321 || ack.Proof != *proof || ack.Identity != a.Identity {
		t.Fatal("ack not bound to confirmed receipt", ack, err)
	}
	var raw []byte
	if j.db.QueryRow("SELECT payload_json FROM bot_outbox WHERE id=?", d.ID).Scan(&raw) != nil || raw != nil {
		t.Fatal("confirmed secret retained")
	}
	if err := j.finishAck(ctx, ack, true); err != nil {
		t.Fatal(err)
	}
	if _, exists, err := j.nextAck(ctx, 100); err != nil || exists {
		t.Fatal("ack replayed after success")
	}
}

func TestJournal429BackoffDedupAndPrivateSharedLock(t *testing.T) {
	ctx := context.Background()
	j, path := testJournal(t, 100, 101)
	if _, err := openJournal(path, []int64{100, 101}); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate worker obtained lock", err)
	}
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: 11, Identity: a.Identity, Kind: "message", Text: "hello", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	if err := j.complete(ctx, in, a, oneScreen("Reply", nil)); err != nil {
		t.Fatal(err)
	}
	d := nextQueued(t, j, 100)
	if err := j.claim(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := j.finish(ctx, d, DeliveryOutcome{State: "retry", Reason: "telegram_rate_limited", RetryAfter: 2 * time.Second}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := j.nextDelivery(ctx, 100); err != nil || ok {
		t.Fatal("ignored 429 backoff")
	}
	if _, err := j.db.Exec("UPDATE bot_outbox SET next_unix_ms=0 WHERE id=?", d.ID); err != nil {
		t.Fatal(err)
	}
	again := nextQueued(t, j, 100)
	if again.ID != d.ID || again.Attempts != 1 {
		t.Fatal("retry was a new delivery")
	}
	if err := j.claim(ctx, again); err != nil {
		t.Fatal(err)
	}
	if err := j.finish(ctx, again, DeliveryOutcome{State: "unknown", Reason: "transport_uncertain"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := j.nextDelivery(ctx, 100); err != nil || ok {
		t.Fatal("uncertain 2nd attempt retried")
	}
	if _, ok, err := j.nextDelivery(ctx, 101); err != nil || ok {
		t.Fatal("cross-bot queue")
	}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	item := received{ID: in.UpdateID, Fingerprint: hex.EncodeToString(sum[:]), Input: &in}
	if err := j.ingest(ctx, 100, []received{item}); err != nil {
		t.Fatal("exact duplicate rejected", err)
	}
	item.Fingerprint = strings.Repeat("f", 64)
	if err := j.ingest(ctx, 100, []received{item}); !errors.Is(err, ErrConflict) {
		t.Fatal("conflicting update accepted", err)
	}
	for _, p := range []string{path, path + ".lock"} {
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("private mode", p, err)
		}
	}
}

func TestBackupValidationIsOfflineAndConfigIsStrict(t *testing.T) {
	ctx := context.Background()
	j, path := testJournal(t, 100)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fileConfig{Version: 1, Purpose: ConfigPurpose, Bots: []botConfig{{BotID: 100, Token: syntheticToken}}})
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(ctx, path, raw); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("offline validation wrote database")
	}
	if err := ValidateOfflineBackup(ctx, "", raw); err != nil {
		t.Fatal("configured disabled backup rejected", err)
	}
	if w, err := Open(Options{ConfigPath: "/nonexistent/token.json"}, nil); err != nil || w != nil {
		t.Fatal("disabled startup touched config")
	}
	for _, bad := range [][]byte{[]byte(`{"version":1,"version":1}`), []byte(strings.Replace(string(raw), ConfigPurpose, "production", 1)), []byte(strings.Replace(string(raw), `"bots":[`, `"unknown":1,"bots":[`, 1))} {
		if _, err := parseConfig(bad); err == nil {
			t.Fatal("invalid config accepted")
		}
	}
	if err := os.WriteFile(path+"-wal", []byte("unexpected"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(ctx, path, raw); err == nil {
		t.Fatal("sidecar accepted")
	}
}

func TestServerActionBoundDoesNotExpandWireCallback(t *testing.T) {
	if !validAction(Action{Name: "confirm", Value: strings.Repeat("a", 1024)}) || validAction(Action{Name: "confirm", Value: strings.Repeat("a", 1025)}) {
		t.Fatal("server action value bound")
	}
	for _, s := range []string{"<>&\"سلام", strings.Repeat("<&", 3500), strings.Repeat("A", 9000)} {
		parts := escapedChunks(s, 2800)
		if strings.Join(parts, "") != escaped(s) {
			t.Fatal("text pagination lost content")
		}
		for _, part := range parts {
			if len(part) > 2800 {
				t.Fatal("text part exceeds budget")
			}
		}
	}
}

func TestJournalAllowlistEvolutionRetainsInactiveBotHistory(t *testing.T) {
	ctx := context.Background()
	j, path := testJournal(t, 100)
	in := Incoming{UpdateID: 17, Identity: Identity{100, 2001, 2001}, Kind: "message", Text: "/start", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	if err := j.rejectInput(ctx, in, "retained_review", true); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err := openJournal(path, []int64{100, 101})
	if err != nil {
		t.Fatal("adding bot destroyed shared binding", err)
	}
	if cursor, err := j.cursor(ctx, 100); err != nil || cursor != 18 {
		t.Fatal("existing cursor lost", cursor, err)
	}
	if cursor, err := j.cursor(ctx, 101); err != nil || cursor != 0 {
		t.Fatal("new cursor not isolated", cursor, err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = openJournal(path, []int64{101})
	if err != nil {
		t.Fatal("removing active bot discarded histories", err)
	}
	var count int
	if err = j.db.QueryRow("SELECT COUNT(*) FROM bot_inbox WHERE bot_id=100 AND state='review'").Scan(&count); err != nil || count != 1 {
		t.Fatal("inactive history missing", count, err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(fileConfig{Version: 1, Purpose: ConfigPurpose, Bots: []botConfig{{BotID: 101, Token: "101:ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}}})
	if err = ValidateOfflineBackup(ctx, path, raw); err != nil {
		t.Fatal("active subset backup rejected", err)
	}
	b := &fakeBackend{}
	w, err := openWorker(fileConfig{Version: 1, Purpose: ConfigPurpose, Bots: []botConfig{{BotID: 101, Token: "101:ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}}}, path, b, fakeRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("offline construction contacted Telegram")
		return nil, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if len(w.apis) != 1 || w.apis[101] == nil || w.apis[100] != nil {
		t.Fatal("inactive bot received transport authority")
	}
}

func TestReceiptButtonConsumesAfterAtomicInvoiceEnqueueOnly(t *testing.T) {
	ctx := context.Background()
	j, path := testJournal(t, 100)
	a := testAccess(100, 2001)
	start := Incoming{UpdateID: 1, Identity: a.Identity, Kind: "message", Text: "/start", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, start)
	if err := j.complete(ctx, start, a, oneScreen("Product", [][]Button{{actionButton("Buy", "success", "buy", "product", "")}, {homeButton("en")}})); err != nil {
		t.Fatal(err)
	}
	menu := nextQueued(t, j, 100)
	var m wireMessage
	if decodeJSON(menu.Payload, &m) != nil {
		t.Fatal("decode menu")
	}
	if err := j.claim(ctx, menu); err != nil {
		t.Fatal(err)
	}
	if err := j.finish(ctx, menu, DeliveryOutcome{State: "sent", MessageID: 44}); err != nil {
		t.Fatal(err)
	}
	click := Incoming{UpdateID: 2, Identity: a.Identity, Kind: "callback", QueryID: "query-buy", MessageID: 44, CallbackData: m.Markup.Rows[0][0].Callback, ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, click)
	_, event, err := j.callback(ctx, click, a)
	if err != nil {
		t.Fatal(err)
	}
	bad := Result{Receipt: true, Invoice: &Invoice{Title: "Plan", Description: "30 days", Payload: "same-order", Currency: "USD", Amount: 25}}
	if err := j.complete(ctx, click, a, bad); !errors.Is(err, ErrProtocol) {
		t.Fatal("invalid invoice accepted")
	}
	if _, same, err := j.callback(ctx, click, a); err != nil || same != event {
		t.Fatal("rolled-back enqueue consumed payment button", err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = openJournal(path, []int64{100})
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	retried, ok, err := j.nextInput(ctx, 100)
	if err != nil || !ok || retried.UpdateID != 2 {
		t.Fatal("crash-before-enqueue lost mutation", ok, err)
	}
	if _, same, err := j.callback(ctx, retried, a); err != nil || same != event {
		t.Fatal("mutation replay key changed", err)
	}
	valid := bad
	valid.Invoice = &Invoice{Title: "Plan", Description: "30 days", Payload: "same-order", Currency: "XTR", Amount: 25}
	if err := j.complete(ctx, retried, a, valid); err != nil {
		t.Fatal(err)
	}
	if _, _, err := j.callback(ctx, retried, a); !errors.Is(err, ErrCallback) {
		t.Fatal("successful invoice enqueue left payment token live")
	}
	home := click
	home.CallbackData = m.Markup.Rows[1][0].Callback
	if action, _, err := j.callback(ctx, home, a); err != nil || action.Name != "home" {
		t.Fatal("unpressed navigation token consumed", err)
	}
	duplicate := click
	duplicate.UpdateID = 3
	duplicate.QueryID = "query-buy-again"
	persistInput(t, j, duplicate)
	b := &fakeBackend{}
	w := &Worker{journal: j, backend: b}
	if err := w.processInput(ctx, duplicate); err != nil {
		t.Fatal(err)
	}
	var invoices int
	if err := j.db.QueryRow("SELECT COUNT(*) FROM bot_outbox WHERE kind='invoice'").Scan(&invoices); err != nil || invoices != 1 || b.handleCount.Load() != 0 {
		t.Fatal("duplicate click queued a second invoice", invoices, err)
	}
}
