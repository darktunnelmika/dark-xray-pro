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
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
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
func (f *fakeAPI) Answer(context.Context, string, string) error { return nil }
func (f *fakeAPI) SendPhotoID(_ context.Context, chat int64, _ string, caption string, kb *Keyboard) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out = append(f.out, sent{chat: chat, text: caption, photo: true, kb: kb})
	return nil
}
func (f *fakeAPI) SendDocument(_ context.Context, chat int64, name string, data []byte, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.out = append(f.out, sent{chat: chat, text: "doc:" + name})
	return nil
}
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
	m := find(t, api.take(), buyerID, "بسته‌ها")
	if m.kb == nil || m.kb.Rows[0][0].Data != "prod:p30" {
		t.Fatalf("product button: %+v", m.kb)
	}
	b.Handle(ctx, cb(buyerID, "prod:p30"))
	m = find(t, api.take(), buyerID, "۱۲۰٬۰۰۰")
	if m.kb == nil || m.kb.Rows[0][0].Data != "buy:p30" {
		t.Fatalf("buy button: %+v", m.kb)
	}

	b.Handle(ctx, cb(buyerID, "buy:p30"))
	out := api.take()
	find(t, out, buyerID, "#1")
	card := find(t, out, ownerID, "سفارش <b>#۱</b>")
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
	find(t, api.take(), ownerID, "چه کسانی")
	b.Handle(ctx, cb(ownerID, "a:bca:active"))
	find(t, api.take(), ownerID, "1 نفر")
	b.Handle(ctx, cb(ownerID, "a:bcok"))
	time.Sleep(300 * time.Millisecond)
	out = api.take()
	find(t, out, buyerID, "hello &lt;all&gt;")
	find(t, out, ownerID, "تمام شد")
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
	// The reseller invites a sales helper and a support helper.
	const salesChat, supportChat = int64(3100), int64(3200)
	invite := func(role string, who int64) {
		b.Handle(ctx, msg(repChat, "/invite "+role))
		m := find(t, api.take(), repChat, "start=adm_")
		code := m.text[strings.Index(m.text, "adm_")+4:]
		b.Handle(ctx, msg(who, "/start adm_"+code))
		find(t, api.take(), who, "ادمین ربات")
	}
	invite("sales", salesChat)
	invite("support", supportChat)
	b.Handle(ctx, cb(supportChat, "ok:"+strconv.FormatInt(id, 10)))
	if o, _ := s.Orders(ctx, owner, "pending", 0); len(o) != 1 {
		t.Fatal("support must not deliver")
	}
	b.Handle(ctx, msg(supportChat, "/credit"))
	find(t, api.take(), supportChat, "مجاز نیست")
	b.Handle(ctx, cb(salesChat, "ok:"+strconv.FormatInt(id, 10)))
	find(t, api.take(), salesChat, "تحویل شد")
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
	find(t, api.take(), ownerID, "۱۵٫۰۰ گیگ")
	b.Handle(ctx, cb(ownerID, "a:toggle:p10-1"))
	find(t, api.take(), ownerID, "غیرفعال")
	b.Handle(ctx, cb(ownerID, "a:find"))
	api.take()
	b.Handle(ctx, msg(ownerID, "p10"))
	find(t, api.take(), ownerID, "p10-1")
	// Broadcast needs a confirmation.
	b.Handle(ctx, cb(ownerID, "a:bc"))
	api.take()
	b.Handle(ctx, msg(ownerID, "hello all\n[Open](https://example.com)"))
	find(t, api.take(), ownerID, "چه کسانی")
	b.Handle(ctx, cb(ownerID, "a:bca:all"))
	out = api.take()
	prev := find(t, out, ownerID, "hello all")
	if prev.kb == nil || prev.kb.Rows[0][0].URL != "https://example.com" {
		t.Fatalf("button: %+v", prev.kb)
	}
	find(t, out, ownerID, "پیش‌نمایش")
	// Backup: asks for a password first, then sends an encrypted file.
	b.Handle(ctx, cb(ownerID, "a:backup"))
	find(t, api.take(), ownerID, "رمز بکاپ")
	b.Handle(ctx, msg(ownerID, "super-secret-1"))
	api.take()
	b.Handle(ctx, cb(ownerID, "a:backup"))
	find(t, api.take(), ownerID, ".dkb")
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

func TestCustomerNotifications(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	_ = s.PutClient(ctx, hubcore.Client{Email: "neda", Enabled: true, VolumeBytes: 1000, Inbounds: []string{"in1"}})
	_ = s.TgLink(ctx, buyerID, "neda")
	api := &fakeAPI{}
	now := time.Now()
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{}, Now: func() time.Time { return now }, Logf: t.Logf}
	_, _ = s.ApplyUsage(ctx, "n1", wireBatch(1, "neda", 950))
	b.Tick(ctx)
	out := api.take()
	find(t, out, buyerID, "وصل شد و فعال است")
	find(t, out, buyerID, "۹۰٪")
	now = now.Add(3 * time.Minute)
	b.Tick(ctx)
	for _, m := range api.take() {
		if m.chat == buyerID {
			t.Fatalf("repeated notification: %s", m.text)
		}
	}
	_, _ = s.ApplyUsage(ctx, "n1", wireBatch(2, "neda", 100))
	now = now.Add(3 * time.Minute)
	b.Tick(ctx)
	find(t, api.take(), buyerID, "حجم سرویس")
}

func wireBatch(seq uint64, email string, n int64) wire.UsageBatch {
	return wire.UsageBatch{NodeID: "n1", Seq: seq, ToUnix: time.Now().Unix(), Traffic: []wire.UserTraffic{{Email: email, Down: n}}}
}

// editAPI also redraws messages, like the real Bot API.
type editAPI struct {
	fakeAPI
	edits []sent
}

func (e *editAPI) EditText(_ context.Context, chat, _ int64, text string, kb *Keyboard) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.edits = append(e.edits, sent{chat: chat, text: text, kb: kb})
	return nil
}

func TestButtonsRedrawTheScreen(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	api := &editAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{}, SubBase: "https://x", Logf: t.Logf}
	press := func(data string) {
		b.Handle(ctx, Update{Callback: &CallbackQuery{ID: "q", From: User{ID: buyerID}, Data: data, Message: &Message{ID: 7, Text: "old screen"}}})
	}
	press("guide")
	press("guide:android")
	press("status")
	if len(api.take()) != 0 || len(api.edits) != 3 {
		t.Fatalf("buttons must redraw, not send: sent %d, edits %d", len(api.out), len(api.edits))
	}
	if !strings.Contains(api.edits[1].text, "v2rayNG") || api.edits[1].kb.Rows[0][0].URL == "" {
		t.Fatalf("android guide: %+v", api.edits[1])
	}
	// Typed messages still get a new message.
	b.Handle(ctx, msg(buyerID, "/start"))
	if len(api.take()) != 1 {
		t.Fatal("typed /start must send")
	}
}

func TestCardReceiptAndDiscount(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	owner := hubcore.Principal{User: "o", Role: "owner"}
	if err := s.PutProduct(ctx, owner, hubcore.Product{ID: "p30", Name: "30GB", Plan: "volume", GB: 30, Days: 30, Inbounds: []string{"in1"}, Price: 200000, Status: "published"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutCard(ctx, hubcore.CardInfo{Number: "6037-9911-2233-4455", Holder: "Ali", Bank: "Melli"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutDiscount(ctx, hubcore.Discount{Code: "nowruz", Percent: 25, MaxUses: 1, OncePerBuyer: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://sub.example", Now: time.Now, Logf: t.Logf}
	b.Tick(ctx)

	// A wrong code, then the right one (case does not matter).
	b.Handle(ctx, cb(buyerID, "cpn:p30"))
	api.take()
	b.Handle(ctx, msg(buyerID, "WRONG"))
	find(t, api.take(), buyerID, "وجود ندارد")
	b.Handle(ctx, cb(buyerID, "cpn:p30"))
	api.take()
	b.Handle(ctx, msg(buyerID, "NowRuz"))
	out := api.take()
	find(t, out, buyerID, "اعمال شد")
	find(t, out, buyerID, "۱۵۰٬۰۰۰")

	// Buying shows an invoice with the card; the owner hears nothing yet.
	b.Handle(ctx, cb(buyerID, "buy:p30"))
	out = api.take()
	inv := find(t, out, buyerID, "6037-9911-2233-4455")
	if !strings.Contains(inv.text, "۱۵۰٬۰۰۰") || !strings.Contains(inv.text, "NOWRUZ") {
		t.Fatalf("invoice: %s", inv.text)
	}
	for _, m := range out {
		if m.chat == ownerID {
			t.Fatal("owner must wait for the receipt")
		}
	}
	// The receipt photo goes to the owner with approve / refuse.
	photo := msg(buyerID, "")
	photo.Message.Photo = []struct {
		FileID string `json:"file_id"`
	}{{FileID: "small"}, {FileID: "big-photo-id"}}
	b.Handle(ctx, photo)
	out = api.take()
	find(t, out, buyerID, "دریافت شد")
	card := find(t, out, ownerID, "رسید پرداخت")
	if !card.photo || card.kb == nil || card.kb.Rows[0][0].Data != "ok:1" {
		t.Fatalf("receipt card: %+v", card)
	}
	b.Handle(ctx, cb(ownerID, "ok:1"))
	out = api.take()
	find(t, out, buyerID, "آماده شد")
	o, _ := s.OrderByID(ctx, 1)
	if o.Status != "fulfilled" || o.Amount != 150000 || o.Discount != 50000 || !o.HasReceipt {
		t.Fatalf("order: %+v", o)
	}
	// The code was for one use: it is used up now.
	if _, err := s.HoldDiscount(ctx, hubcore.TgBuyer(3000), "NOWRUZ", "p30"); err == nil {
		t.Fatal("a used-up code must be refused")
	}

	// A refused receipt cancels the order and tells the buyer.
	b.Handle(ctx, cb(buyerID, "buy:p30"))
	api.take()
	b.Handle(ctx, photo)
	api.take()
	b.Handle(ctx, cb(ownerID, "no:2"))
	find(t, api.take(), buyerID, "تأیید نشد")
	// The buyer can cancel an order before paying.
	b.Handle(ctx, cb(buyerID, "buy:p30"))
	api.take()
	b.Handle(ctx, cb(buyerID, "ocx:3"))
	find(t, api.take(), buyerID, "لغو شد")
	if o, _ := s.OrderByID(ctx, 3); o.Status != "cancelled" {
		t.Fatal("buyer cancel failed")
	}
}

// A reseller's own bot: its catalog only, its card, its toman wallet
// topped up by receipt, purchases paid from the wallet out of its credit.
func TestResellerBotTomanWallet(t *testing.T) {
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
	_ = s.AdjustCredit(ctx, "o", "ali", 100<<30, 0, "")
	for _, p := range []hubcore.Product{
		{ID: "own10", Name: "Owner 10GB", Plan: "volume", GB: 10, Days: 30, Inbounds: []string{"in1"}, Price: 90000, Status: "published"},
		{ID: "ali20", Seller: "ali", Name: "Ali 20GB", Plan: "volume", GB: 20, Days: 30, Inbounds: []string{"in1"}, Price: 150000, Status: "published"},
	} {
		if err := s.PutProduct(ctx, owner, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.PutCardFor(ctx, "ali", hubcore.CardInfo{Number: "6219861012345678", Holder: "Ali R"}); err != nil {
		t.Fatal(err)
	}
	const repChat = int64(3000)
	code, _ := s.NewTgLinkCode(ctx, "ali")
	if _, err := s.ClaimTgLinkCode(ctx, code, repChat); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{}, SubBase: "https://x", Seller: "ali", Now: time.Now, Logf: t.Logf}
	b.Tick(ctx)
	b.Handle(ctx, cb(buyerID, "shop"))
	out := api.take()
	shop := find(t, out, buyerID, "")
	all := ""
	for _, m := range out {
		all += m.text
		if m.kb != nil {
			for _, r := range m.kb.Rows {
				for _, btn := range r {
					all += btn.Text + btn.Data
				}
			}
		}
	}
	_ = shop
	if !strings.Contains(all, "ali20") || strings.Contains(all, "own10") {
		t.Fatalf("the reseller's bot must sell only its catalog: %s", all)
	}
	// Top up 200,000 toman by receipt; the reseller approves.
	b.Handle(ctx, cb(buyerID, "topup"))
	api.take()
	b.Handle(ctx, msg(buyerID, "۲۰۰٬۰۰۰"))
	inv := find(t, api.take(), buyerID, "6219-8610-1234-5678")
	if !strings.Contains(inv.text, "۲۰۰٬۰۰۰") {
		t.Fatal(inv.text)
	}
	photo := msg(buyerID, "")
	photo.Message.Photo = []struct {
		FileID string `json:"file_id"`
	}{{FileID: "rcpt"}}
	b.Handle(ctx, photo)
	out = api.take()
	find(t, out, buyerID, "دریافت شد")
	card := find(t, out, repChat, "شارژ کیف پول")
	if !card.photo || card.kb.Rows[0][0].Data != "tok:1" {
		t.Fatalf("%+v", card)
	}
	b.Handle(ctx, cb(int64(9999), "tok:1")) // a stranger cannot approve
	if s.TomanBalance(ctx, "ali", buyerID) != 0 {
		t.Fatal("only the reseller approves")
	}
	b.Handle(ctx, cb(repChat, "tok:1"))
	find(t, api.take(), buyerID, "شارژ شد")
	if s.TomanBalance(ctx, "ali", buyerID) != 200000 || s.TomanBalance(ctx, "", buyerID) != 0 {
		t.Fatal("the top-up belongs to the reseller's wallet only")
	}
	// Buy from the wallet: delivered at once, paid from the reseller's credit.
	b.Handle(ctx, cb(buyerID, "tpay:ali20"))
	b.Tick(ctx)
	out = api.take()
	find(t, out, buyerID, "از کیف پول پرداخت شد")
	if s.TomanBalance(ctx, "ali", buyerID) != 50000 {
		t.Fatal("wallet not charged", s.TomanBalance(ctx, "ali", buyerID))
	}
	mine, _ := s.TgClientsOf(ctx, buyerID, "ali")
	if len(mine) != 1 {
		t.Fatal("the client must belong to the reseller", mine)
	}
	reps, _ := s.Reps(ctx)
	for _, r := range reps {
		if r.ID == "ali" && r.VolumeCredit != 80<<30 {
			t.Fatal("the reseller's credit pays for it", r.VolumeCredit)
		}
	}
	// Not enough credit: refused, nothing delivered.
	b.Handle(ctx, cb(buyerID, "tpay:ali20"))
	find(t, api.take(), buyerID, "ممکن نشد")
}

func TestResellerButtons(t *testing.T) {
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
	_ = s.AdjustCredit(ctx, "o", "ali", 100<<30, 0, "")
	_ = s.PutProduct(ctx, owner, hubcore.Product{ID: "a30", Seller: "ali", Name: "30GB", Plan: "volume", GB: 30, Days: 30, MaxIPs: 2, Inbounds: []string{"in1"}, Price: 1, Status: "published", FirstUse: true})
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, SubBase: "https://x", Logf: t.Logf, Seller: "ali"}
	const repChat = int64(3000)
	code, _ := s.NewTgLinkCode(ctx, "ali")
	b.Handle(ctx, msg(repChat, "/start rep_"+code))
	find(t, api.take(), repChat, "اعتبار")
	// Create with a ready plan and an automatic name.
	b.Handle(ctx, msg(repChat, "➕ ساخت کاربر"))
	find(t, api.take(), repChat, "پلن آماده")
	b.Handle(ctx, cb(repChat, "R:p:a30"))
	find(t, api.take(), repChat, "نام کاربر")
	b.Handle(ctx, msg(repChat, "bad name!"))
	find(t, api.take(), repChat, "حروف انگلیسی")
	b.Handle(ctx, msg(repChat, "cust-1"))
	find(t, api.take(), repChat, "مانده بعد از این")
	b.Handle(ctx, cb(repChat, "R:ok"))
	find(t, api.take(), repChat, "ساخته شد")
	c, err := s.ClientByEmail(ctx, "cust-1")
	if err != nil || c.RepID != "ali" || c.VolumeBytes != 30<<30 || c.DurationSeconds != 30*86400 || c.MaxIPs != 2 {
		t.Fatalf("created: %+v %v", c.Client, err)
	}
	// Find it, renew with custom volume and term.
	b.Handle(ctx, cb(repChat, "R:cs"))
	api.take()
	b.Handle(ctx, msg(repChat, "cust"))
	m := find(t, api.take(), repChat, "نتیجهٔ جستجو")
	_ = m
	r := b.ref("cust-1")
	b.Handle(ctx, cb(repChat, "R:rn:"+r))
	api.take()
	b.Handle(ctx, cb(repChat, "R:c"))
	b.Handle(ctx, cb(repChat, "R:g:50"))
	b.Handle(ctx, cb(repChat, "R:d:60"))
	find(t, api.take(), repChat, "دورهٔ جدید از امروز")
	b.Handle(ctx, cb(repChat, "R:ok"))
	find(t, api.take(), repChat, "تمدید شد")
	c, _ = s.ClientByEmail(ctx, "cust-1")
	if c.VolumeBytes != 50<<30 {
		t.Fatal("renewed to the new package", c.VolumeBytes>>30)
	}
	// Switch off, then delete.
	b.Handle(ctx, cb(repChat, "R:tg:"+r))
	api.take()
	if c, _ = s.ClientByEmail(ctx, "cust-1"); c.Enabled {
		t.Fatal("switched off")
	}
	b.Handle(ctx, cb(repChat, "R:dy:"+r))
	find(t, api.take(), repChat, "حذف شد")
	if _, err := s.ClientByEmail(ctx, "cust-1"); err == nil {
		t.Fatal("deleted")
	}
	// A stranger gets nothing.
	b.Handle(ctx, cb(9999, "R:new"))
	find(t, api.take(), 9999, "فقط برای نماینده")
}

func TestShopButtons(t *testing.T) {
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
	_ = s.AdjustCredit(ctx, "o", "ali", 100<<30, 0, "")
	_ = s.PutProduct(ctx, owner, hubcore.Product{ID: "a30", Seller: "ali", Name: "30GB", Plan: "volume", GB: 30, Days: 30, Inbounds: []string{"in1"}, Price: 1, Status: "published"})
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, SubBase: "https://x", Logf: t.Logf, Seller: "ali"}
	const repChat = int64(3100)
	code, _ := s.NewTgLinkCode(ctx, "ali")
	b.Handle(ctx, msg(repChat, "/start rep_"+code))
	api.take()
	b.Handle(ctx, msg(repChat, "🧾 محصولات"))
	find(t, api.take(), repChat, "محصولات فروشگاه")
	// A new product in a few steps; the price typed the Persian way.
	b.Handle(ctx, cb(repChat, "S:n"))
	b.Handle(ctx, msg(repChat, "۲۰ گیگ ویژه"))
	b.Handle(ctx, cb(repChat, "S:ng:20"))
	b.Handle(ctx, cb(repChat, "S:nd:30"))
	b.Handle(ctx, cb(repChat, "S:ni:2"))
	b.Handle(ctx, msg(repChat, "۱۵۰ هزار"))
	find(t, api.take(), repChat, "150,000")
	b.Handle(ctx, cb(repChat, "S:nm:pub"))
	find(t, api.take(), repChat, "ساخته شد")
	var made hubcore.Product
	list, _ := s.Products(ctx, owner, false)
	for _, p := range list {
		if p.Name == "۲۰ گیگ ویژه" {
			made = p
		}
	}
	if made.ID == "" || made.Seller != "ali" || made.GB != 20 || made.Price != 150000 || made.Status != "published" || len(made.Inbounds) != 0 {
		t.Fatalf("made: %+v", made)
	}
	// Change the price, switch it off and on.
	b.Handle(ctx, cb(repChat, "S:e:"+made.ID+":price"))
	b.Handle(ctx, msg(repChat, "200,000"))
	find(t, api.take(), repChat, "ذخیره شد")
	b.Handle(ctx, cb(repChat, "S:t:"+made.ID))
	api.take()
	if p, _ := s.ProductByID(ctx, owner, made.ID); p.Price != 200000 || p.Status != "draft" {
		t.Fatalf("edited: %+v", p)
	}
	b.Handle(ctx, cb(repChat, "S:t:"+made.ID))
	// Sold with no inbounds picked: the customer gets every allowed one.
	rp := hubcore.Principal{User: "r", Role: "rep", RepID: "ali"}
	id, err := s.CreateOrder(ctx, rp, made.ID, "x", "", "")
	if err != nil {
		t.Fatal(err)
	}
	email, err := s.FulfillOrder(ctx, rp, id)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := s.ClientByEmail(ctx, email); len(c.Inbounds) != 1 || c.Inbounds[0] != "in1" {
		t.Fatalf("inbounds: %+v", c.Inbounds)
	}
	// Never sold: deleted for good; sold: only taken off the shop.
	b.Handle(ctx, cb(repChat, "S:y:a30"))
	find(t, api.take(), repChat, "حذف شد")
	if _, err := s.ProductByID(ctx, owner, "a30"); err == nil {
		t.Fatal("a30 deleted")
	}
	b.Handle(ctx, cb(repChat, "S:y:"+made.ID))
	find(t, api.take(), repChat, "برداشته شد")
	if p, _ := s.ProductByID(ctx, owner, made.ID); p.Status != "archived" {
		t.Fatal("archived", p.Status)
	}
	// Restored from the card.
	b.Handle(ctx, cb(repChat, "S:t:"+made.ID))
	if p, _ := s.ProductByID(ctx, owner, made.ID); p.Status != "draft" {
		t.Fatal("restored", p.Status)
	}
	// Without the grant nothing changes.
	_ = s.SetManageProducts(ctx, "ali", false)
	b.Handle(ctx, cb(repChat, "S:t:"+made.ID))
	find(t, api.take(), repChat, "فعال نیست")
	// The owner manages the main shop from the main bot.
	ob := &Bot{Store: s, API: api, SubBase: "https://x", Logf: t.Logf, Owners: map[int64]bool{1: true}}
	ob.Handle(ctx, cb(1, "S:h"))
	find(t, api.take(), 1, "محصولات فروشگاه")
	// A stranger is refused.
	ob.Handle(ctx, cb(9999, "S:h"))
	find(t, api.take(), 9999, "مجاز نیست")
}
