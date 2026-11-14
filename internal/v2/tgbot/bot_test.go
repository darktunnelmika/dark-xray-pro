package tgbot

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

type sent struct {
	chat  int64
	text  string
	kb    *Keyboard
	photo bool
}

type fakeAPI struct {
	mu   sync.Mutex
	out  []sent
	next int64
}

func (f *fakeAPI) Updates(context.Context, int64) ([]Update, error) { return nil, nil }
func (f *fakeAPI) Send(_ context.Context, chat int64, text string, kb *Keyboard, _ int64) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next++
	f.out = append(f.out, sent{chat: chat, text: text, kb: kb})
	return f.next, nil
}
func (f *fakeAPI) SendPhoto(_ context.Context, chat int64, _ []byte, caption string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out = append(f.out, sent{chat: chat, text: caption, photo: true})
	return nil
}
func (f *fakeAPI) Answer(context.Context, string, string) error          { return nil }
func (f *fakeAPI) Me(context.Context) (string, error)                    { return "test_bot", nil }
func (f *fakeAPI) SetCommands(context.Context, int64, [][2]string) error { return nil }

func (f *fakeAPI) take() []sent {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.out
	f.out = nil
	return o
}

func find(t *testing.T, msgs []sent, chat int64, sub string) sent {
	t.Helper()
	for _, m := range msgs {
		if m.chat == chat && strings.Contains(m.text, sub) {
			return m
		}
	}
	t.Fatalf("no message to %d containing %q in %+v", chat, sub, msgs)
	return sent{}
}

const ownerID, buyerID = int64(1000), int64(2000)

func msg(chat int64, text string) Update {
	return Update{Message: &Message{ID: 1, From: &User{ID: chat, FirstName: "U"}, Chat: Chat{ID: chat, Type: "private"}, Text: text}}
}

func cb(chat int64, data string) Update {
	return Update{Callback: &CallbackQuery{ID: "q", From: User{ID: chat}, Data: data}}
}

func TestStorefrontOrderDeliverySupport(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if _, err := s.CreateNode(ctx, "n1", "N"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true); err != nil {
		t.Fatal(err)
	}
	owner := hubcore.Principal{User: "o", Role: "owner"}
	if err := s.PutProduct(ctx, owner, hubcore.Product{ID: "p30", Name: "30GB", Plan: "volume", GB: 30, Days: 30, Inbounds: []string{"in1"}, Price: 120000, Status: "published", FirstUse: true}); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://sub.example", Now: time.Now, Logf: t.Logf}
	b.Tick(ctx) // first tick only records node state

	b.Handle(ctx, msg(buyerID, "/start"))
	find(t, api.take(), buyerID, "خوش آمدید")

	b.Handle(ctx, cb(buyerID, "shop"))
	m := find(t, api.take(), buyerID, "120,000")
	if m.kb == nil || m.kb.Rows[0][0].Data != "buy:p30" {
		t.Fatalf("buy button: %+v", m.kb)
	}

	b.Handle(ctx, cb(buyerID, "buy:p30"))
	out := api.take()
	find(t, out, buyerID, "#1")
	card := find(t, out, ownerID, "سفارش <b>#1</b>")
	if card.kb == nil || card.kb.Rows[0][0].Data != "ok:1" {
		t.Fatalf("owner buttons: %+v", card.kb)
	}

	// A customer cannot press the owner's buttons.
	b.Handle(ctx, cb(buyerID, "ok:1"))
	if orders, _ := s.Orders(ctx, owner, "pending", 0); len(orders) != 1 {
		t.Fatal("customer must not deliver")
	}

	b.Handle(ctx, cb(ownerID, "ok:1"))
	out = api.take()
	find(t, out, ownerID, "تحویل شد")
	find(t, out, buyerID, "آماده شد")
	find(t, out, buyerID, "https://sub.example/sub/")
	photo := false
	for _, x := range out {
		photo = photo || (x.photo && x.chat == buyerID)
	}
	if !photo {
		t.Fatal("QR photo missing")
	}

	// The buyer sees the delivered service.
	b.Handle(ctx, cb(buyerID, "mine"))
	find(t, api.take(), buyerID, "p30-1")

	// Support round trip.
	b.Handle(ctx, cb(buyerID, "support"))
	api.take()
	b.Handle(ctx, msg(buyerID, "connection is slow"))
	out = api.take()
	find(t, out, buyerID, "تیکت #1")
	var ownerMsgID int64
	for i, x := range out {
		if x.chat == ownerID && strings.Contains(x.text, "connection is slow") {
			ownerMsgID = int64(i + 1)
		}
	}
	_ = ownerMsgID
	tick, err := firstTicketMessage(s, ctx)
	if err != nil {
		t.Fatal(err)
	}
	reply := msg(ownerID, "fixed now")
	reply.Message.ReplyTo = &Message{ID: tick}
	b.Handle(ctx, reply)
	find(t, api.take(), buyerID, "fixed now")

	// Owner commands.
	b.Handle(ctx, msg(ownerID, "/status"))
	find(t, api.take(), ownerID, "نودها")
	b.Handle(ctx, msg(ownerID, "/broadcast hello <all>"))
	out = api.take()
	find(t, out, buyerID, "hello &lt;all&gt;")
	find(t, out, ownerID, "ارسال شد")
}

// firstTicketMessage finds the owner-side message id mapped to ticket 1.
func firstTicketMessage(s *hubcore.Store, ctx context.Context) (int64, error) {
	for id := int64(1); id < 100; id++ {
		if _, chat, err := s.TgTicketChat(ctx, ownerID, id); err == nil && chat == buyerID {
			return id, nil
		}
	}
	return 0, hubcore.ErrNotFound
}

func TestResellerTelegram(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	owner := hubcore.Principal{User: "o", Role: "owner"}
	_ = s.PutRep(ctx, hubcore.Rep{ID: "ali", Name: "Ali", Enabled: true})
	_ = s.AdjustCredit(ctx, "o", "ali", 10<<30, 0, "")
	if err := s.PutProduct(ctx, owner, hubcore.Product{ID: "a5", Seller: "ali", Name: "5GB", Plan: "volume", GB: 5, Days: 30, Inbounds: []string{"in1"}, Price: 1, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://x", Logf: t.Logf}
	b.Tick(ctx)
	const repChat = int64(3000)
	b.Handle(ctx, msg(repChat, "/start rep_bogus"))
	find(t, api.take(), repChat, "معتبر نیست")
	code, _ := s.NewTgLinkCode(ctx, "ali")
	b.Handle(ctx, msg(repChat, "/start rep_"+code))
	find(t, api.take(), repChat, "وصل شد")
	b.Handle(ctx, msg(repChat, "/start rep_"+code)) // single use
	find(t, api.take(), repChat, "معتبر نیست")

	id, err := s.CreateOrder(ctx, owner, "a5", "walk-in", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b.Tick(ctx)
	out := api.take()
	find(t, out, repChat, "سفارش <b>#")
	for _, m := range out {
		if m.chat == ownerID {
			t.Fatal("a reseller's order must go to the reseller")
		}
	}
	b.Handle(ctx, cb(repChat, "ok:"+strconv.FormatInt(id, 10)))
	find(t, api.take(), repChat, "تحویل شد")
	b.Handle(ctx, msg(repChat, "/credit"))
	find(t, api.take(), repChat, "5.00 GB")
}

func TestOwnerConsole(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	owner := hubcore.Principal{User: "o", Role: "owner"}
	_ = s.PutProduct(ctx, owner, hubcore.Product{ID: "p10", Name: "10GB", Plan: "volume", GB: 10, Days: 30, Inbounds: []string{"in1"}, Price: 5, Status: "published"})
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://x", Logf: t.Logf}
	b.Handle(ctx, msg(ownerID, "/start"))
	home := find(t, api.take(), ownerID, "پنل مدیریت")
	if home.kb == nil || len(home.kb.Rows) < 5 {
		t.Fatal("owner menu missing")
	}
	b.Handle(ctx, cb(ownerID, "a:qs:p10"))
	out := api.take()
	find(t, out, ownerID, "p10-1")
	b.Handle(ctx, cb(ownerID, "a:renew:p10-1:30:5"))
	find(t, api.take(), ownerID, "15.00 GB")
	b.Handle(ctx, cb(ownerID, "a:toggle:p10-1"))
	find(t, api.take(), ownerID, "غیرفعال ⛔")
	b.Handle(ctx, cb(ownerID, "a:find"))
	api.take()
	b.Handle(ctx, msg(ownerID, "p10"))
	find(t, api.take(), ownerID, "p10-1")
	// Broadcast needs a confirmation.
	b.Handle(ctx, cb(ownerID, "a:bc"))
	api.take()
	b.Handle(ctx, msg(ownerID, "hello all"))
	find(t, api.take(), ownerID, "پیش‌نمایش")
	// A customer cannot use owner buttons.
	b.Handle(ctx, cb(buyerID, "a:delok:p10-1"))
	if _, err := s.ClientByEmail(ctx, "p10-1"); err != nil {
		t.Fatal("customer must not delete clients")
	}
	b.Handle(ctx, cb(ownerID, "a:delok:p10-1"))
	if _, err := s.ClientByEmail(ctx, "p10-1"); err == nil {
		t.Fatal("owner delete failed")
	}
}
