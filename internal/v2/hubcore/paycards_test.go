package hubcore

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPayCardsLegacyMirrorAndRotation(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	// The old single card still works and is listed as one card.
	if err := s.PutCardFor(ctx, "", CardInfo{Number: "6037-9911-2233-4455", Holder: "Ali"}); err != nil {
		t.Fatal(err)
	}
	cards, _ := s.PayCards(ctx, "")
	if len(cards) != 1 || cards[0].ID != 0 || cards[0].Number != "6037991122334455" {
		t.Fatalf("legacy card: %+v", cards)
	}
	if c, err := s.CardForPay(ctx, "", OrderRef(1), 1000); err != nil || c.Number != "6037991122334455" {
		t.Fatalf("legacy pay: %+v %v", c, err)
	}

	// Two cards, the second with a small daily cap.
	if err := s.PutPayCards(ctx, "", []PayCard{
		{Number: "6037991122334455", Holder: "Ali", Enabled: true},
		{Number: "۶۲۱۹۸۶۱۰۰۰۰۰۰۰۰۱", Holder: "Sara", Enabled: true, DailyCap: 500_000},
	}); err != nil {
		t.Fatal(err)
	}
	// The first usable card is mirrored into the old settings.
	if s.Setting(ctx, "pay_card_number") != "6037991122334455" || s.CardFor(ctx, "").Number != "6037991122334455" {
		t.Fatal("mirror")
	}
	// An uncapped card has the most room: it takes a big payment.
	c1, err := s.CardForPay(ctx, "", OrderRef(10), 2_000_000)
	if err != nil || c1.Number != "6037991122334455" {
		t.Fatalf("big: %+v %v", c1, err)
	}
	// The same payment keeps its card.
	if again, _ := s.CardForPay(ctx, "", OrderRef(10), 2_000_000); again.Number != c1.Number {
		t.Fatal("sticky")
	}
	// Turn the first card off: the capped one takes what fits, then nothing.
	cards, _ = s.PayCards(ctx, "")
	cards[0].Enabled = false
	if err := s.PutPayCards(ctx, "", cards); err != nil {
		t.Fatal(err)
	}
	if s.CardFor(ctx, "").Number != "6219861000000001" {
		t.Fatalf("mirror after off: %+v", s.CardFor(ctx, ""))
	}
	if c, err := s.CardForPay(ctx, "", OrderRef(11), 400_000); err != nil || c.Number != "6219861000000001" {
		t.Fatalf("capped: %+v %v", c, err)
	}
	s.markCardUse(ctx, OrderRef(11), "paid")
	if _, err := s.CardForPay(ctx, "", OrderRef(12), 200_000); !errors.Is(err, ErrNoCard) {
		t.Fatalf("over the cap: %v", err)
	}
	// A cancelled payment frees its share; tomorrow the cap starts again.
	s.markCardUse(ctx, OrderRef(11), "void")
	if _, err := s.CardForPay(ctx, "", OrderRef(12), 200_000); err != nil {
		t.Fatalf("after void: %v", err)
	}
	s.markCardUse(ctx, OrderRef(12), "paid")
	now = now.Add(24 * time.Hour)
	if _, err := s.CardForPay(ctx, "", OrderRef(13), 450_000); err != nil {
		t.Fatalf("next day: %v", err)
	}
	// An unpaid invoice stops counting after half an hour.
	now = now.Add(31 * time.Minute)
	if _, err := s.CardForPay(ctx, "", OrderRef(14), 450_000); err != nil {
		t.Fatalf("stale invoice: %v", err)
	}
}

func TestPayCardsTemporaryLimitsAndSellers(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	until := now.Add(time.Hour).Unix()
	if err := s.PutPayCards(ctx, "ali", []PayCard{{Number: "6037991122334455", Enabled: true, Until: until}}); err != nil {
		t.Fatal(err)
	}
	if s.CardFor(ctx, "ali").Number == "" || s.CardFor(ctx, "").Number != "" {
		t.Fatal("cards are per seller")
	}
	now = now.Add(2 * time.Hour)
	if s.CardFor(ctx, "ali").Number != "" {
		t.Fatal("a temporary card hides itself")
	}
	if _, err := s.CardForPay(ctx, "ali", TopupRef(1), 1000); !errors.Is(err, ErrNoCard) {
		t.Fatal("expired card must not be shown")
	}
	six := make([]PayCard, 6)
	for i := range six {
		six[i] = PayCard{Number: "603799112233445" + string(rune('0'+i)), Enabled: true}
	}
	if err := s.PutPayCards(ctx, "ali", six); err == nil {
		t.Fatal("at most five cards")
	}
	if err := s.PutPayCards(ctx, "ali", []PayCard{{Number: "123"}}); err == nil {
		t.Fatal("short number")
	}
	if err := s.PutPayCards(ctx, "ali", []PayCard{{Number: "6037991122334455"}, {Number: "6037 9911 2233 4455"}}); err == nil {
		t.Fatal("twice")
	}
	// An empty list turns card payment off, also for the legacy check.
	if err := s.PutPayCards(ctx, "ali", nil); err != nil || s.CardFor(ctx, "ali").Number != "" {
		t.Fatal("empty list")
	}
	// The single-card form keeps working once cards are managed.
	if err := s.PutCardFor(ctx, "ali", CardInfo{Number: "6037991122334455"}); err != nil {
		t.Fatal(err)
	}
	if cards, _ := s.PayCards(ctx, "ali"); len(cards) != 1 || cards[0].ID == 0 {
		t.Fatalf("single form: %+v", cards)
	}
}

func TestReceiptDuplicateAndPhone(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	if d := s.NoteReceipt(ctx, "", OrderRef(1), 7, "U1", ReceiptHash([]byte("photo"))); d != nil {
		t.Fatal("first receipt is not a duplicate")
	}
	// The same payment again (an album) is not a duplicate.
	if d := s.NoteReceipt(ctx, "", OrderRef(1), 7, "U1", ""); d != nil {
		t.Fatal("same payment")
	}
	// The same bytes under another upload id, for another payment.
	d := s.NoteReceipt(ctx, "", TopupRef(4), 8, "U2", ReceiptHash([]byte("photo")))
	if d == nil || d.Ref != "o:1" || !d.SameShop || d.SameBuyer {
		t.Fatalf("dup: %+v", d)
	}
	// Another seller sees only that it was used.
	d = s.NoteReceipt(ctx, "ali", OrderRef(9), 7, "U1", "")
	if d == nil || d.Ref != "" || d.SameShop || !d.SameBuyer {
		t.Fatalf("other shop: %+v", d)
	}

	if !s.PhoneOK(ctx, "", 7) {
		t.Fatal("no check by default")
	}
	_ = s.PutCardSafety(ctx, "", CardSafety{RequirePhone: true, IranOnly: true, AskLast4: true})
	if s.PhoneOK(ctx, "", 7) {
		t.Fatal("phone required")
	}
	if err := s.PutTgPhone(ctx, 7, "+1 202 555 0100"); err != nil {
		t.Fatal(err)
	}
	if s.PhoneOK(ctx, "", 7) {
		t.Fatal("Iranian numbers only")
	}
	if err := s.PutTgPhone(ctx, 7, "09121234567"); err != nil || s.TgPhone(ctx, 7) != "+989121234567" || !s.PhoneOK(ctx, "", 7) {
		t.Fatalf("iran phone: %q", s.TgPhone(ctx, 7))
	}
	// Turning the phone check off also turns "Iran only" off.
	_ = s.PutCardSafety(ctx, "", CardSafety{IranOnly: true})
	if cs := s.CardSafetyFor(ctx, ""); cs.IranOnly || cs.RequirePhone {
		t.Fatalf("%+v", cs)
	}
	if err := s.SetPayerLast4(ctx, OrderRef(1), "۱۲۳۴"); err != nil {
		t.Fatal(err)
	}
	if c, l4 := s.PaymentCard(ctx, OrderRef(1)); l4 != "1234" || c.Number != "" {
		t.Fatalf("last4 without a card hold: %q %+v", l4, c)
	}
	if err := s.SetPayerLast4(ctx, OrderRef(1), "12"); err == nil {
		t.Fatal("four digits")
	}
}
