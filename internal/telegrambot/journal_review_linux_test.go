//go:build linux

package telegrambot

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func reviewConfig(t *testing.T) []byte {
	t.Helper()
	raw, err := json.Marshal(fileConfig{Version: 1, Purpose: ConfigPurpose, Bots: []botConfig{{BotID: 100, Token: syntheticToken}, {BotID: 101, Token: "101:ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func reviewSentProof(t *testing.T) (*journal, string, delivery) {
	t.Helper()
	ctx := context.Background()
	j, path := testJournal(t, 100, 101)
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: 71, Identity: a.Identity, Kind: "message", Text: "deliver", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	proof := &DeliveryProof{ClientID: "original-private-client", SHA256: strings.Repeat("a", 64), TermsSHA256: strings.Repeat("b", 64), ExpiresUnixNano: time.Now().Add(time.Minute).UnixNano()}
	if err := j.complete(ctx, in, a, Result{Receipt: true, Delivery: proof, Screen: &Screen{Text: "synthetic configuration"}}); err != nil {
		t.Fatal(err)
	}
	d := nextQueued(t, j, 100)
	if err := j.claim(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := j.finish(ctx, d, DeliveryOutcome{State: "sent", MessageID: 501}); err != nil {
		t.Fatal(err)
	}
	return j, path, d
}

func TestJournalReviewRejectsProofTransplantWithValidForeignKeys(t *testing.T) {
	for name, statement := range map[string]string{
		"other bot and user":     `UPDATE bot_delivery_proofs SET bot_id=101,user_id=3001`,
		"other user":             `UPDATE bot_delivery_proofs SET user_id=3001`,
		"other Telegram receipt": `UPDATE bot_delivery_proofs SET message_id=999`,
		"unconfirmed parent":     `UPDATE bot_outbox SET state='failed'`,
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			j, path, _ := reviewSentProof(t)
			if _, err := j.db.Exec(statement); err != nil {
				t.Fatal(err)
			}
			rows, err := j.db.Query(`PRAGMA foreign_key_check`)
			if err != nil {
				t.Fatal(err)
			}
			if rows.Next() {
				rows.Close()
				t.Fatal("fixture broke FK; it must test semantic proof binding")
			}
			rows.Close()
			if err = j.Close(); err != nil {
				t.Fatal(err)
			}
			if err = ValidateOfflineBackup(ctx, path, reviewConfig(t)); err == nil {
				t.Fatal("offline backup accepted unrelated send as native handoff evidence")
			}
			if reopened, err := openJournal(path, []int64{100, 101}); err == nil {
				reopened.Close()
				t.Fatal("normal reopen accepted proof transplant")
			}
		})
	}
}

func TestJournalReviewValidPendingAcknowledgementSurvivesValidation(t *testing.T) {
	ctx := context.Background()
	j, path, d := reviewSentProof(t)
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(ctx, path, reviewConfig(t)); err != nil {
		t.Fatal("valid proof rejected", err)
	}
	reopened, err := openJournal(path, []int64{100, 101})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	ack, found, err := reopened.nextAck(ctx, 100)
	if err != nil || !found || ack.ID != d.ID || ack.Identity != (Identity{100, 2001, 2001}) || ack.MessageID != 501 {
		t.Fatal("valid handoff evidence lost", ack, found, err)
	}
	if _, found, err = reopened.nextAck(ctx, 101); err != nil || found {
		t.Fatal("valid proof crossed bots", found, err)
	}
}

func TestJournalReviewRejectsInboxIdentityAndKindCorruption(t *testing.T) {
	for name, change := range map[string]func(*Incoming){
		"other bot":    func(i *Incoming) { i.Identity.BotID = 101 },
		"other update": func(i *Incoming) { i.UpdateID = 999 },
		"different input kind": func(i *Incoming) {
			i.Kind = "refund"
			i.Payment = &Payment{Payload: "order", Currency: "XTR", Amount: 25, TelegramChargeID: "charge"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			j, path := testJournal(t, 100, 101)
			in := Incoming{UpdateID: 72, Identity: Identity{100, 2001, 2001}, Kind: "message", Text: "/start", ReceivedUnix: time.Now().Unix()}
			persistInput(t, j, in)
			change(&in)
			raw, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = j.db.Exec(`UPDATE bot_inbox SET input_json=?`, raw); err != nil {
				t.Fatal(err)
			}
			if err = j.Close(); err != nil {
				t.Fatal(err)
			}
			if err = ValidateOfflineBackup(ctx, path, reviewConfig(t)); err == nil {
				t.Fatal("corrupted normalized financial/message identity accepted")
			}
			if reopened, err := openJournal(path, []int64{100, 101}); err == nil {
				reopened.Close()
				t.Fatal("normal reopen accepted corrupted input")
			}
		})
	}
}

func TestJournalReviewCancelledMenuRemainsValidForRestartAndBackup(t *testing.T) {
	ctx := context.Background()
	j, path := testJournal(t, 100, 101)
	a := testAccess(100, 2001)
	in := Incoming{UpdateID: 73, Identity: a.Identity, Kind: "message", Text: "/start", ReceivedUnix: time.Now().Unix()}
	persistInput(t, j, in)
	if err := j.complete(ctx, in, a, oneScreen("Queued menu", [][]Button{{actionButton("Main menu", "primary", "home", "", "")}})); err != nil {
		t.Fatal(err)
	}
	d := nextQueued(t, j, 100)
	if err := j.cancelDelivery(ctx, d, "authority_changed"); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateOfflineBackup(ctx, path, reviewConfig(t)); err != nil {
		t.Fatal("ordinary cancellation made valid journal un-restorable", err)
	}
	reopened, err := openJournal(path, []int64{100, 101})
	if err != nil {
		t.Fatal("ordinary cancellation prevents restart", err)
	}
	defer reopened.Close()
	if _, found, err := reopened.nextDelivery(ctx, 100); err != nil || found {
		t.Fatal("cancelled menu requeued", found, err)
	}
}
