package tgbot

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// joinAPI is the test API plus channel membership.
type joinAPI struct {
	*fakeAPI
	in    map[int64]bool // who is in the channel
	admin bool           // is the bot an admin there
	asked int
}

func (j *joinAPI) ChatMember(_ context.Context, channel string, user int64) (string, error) {
	j.asked++
	if !j.admin {
		return "", errors.New("Bad Request: member list is inaccessible")
	}
	if j.in[user] {
		return "member", nil
	}
	return "left", nil
}

func TestRequiredChannel(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	api := &joinAPI{fakeAPI: &fakeAPI{}, in: map[int64]bool{}}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://x", Logf: t.Logf}
	const cust = int64(8100)

	// Off by default: nobody is asked.
	b.Handle(ctx, msg(cust, "/start"))
	find(t, api.take(), cust, "سلام")
	if api.asked != 0 {
		t.Fatal("no membership checks while it is off")
	}
	// The owner adds a channel; the bot must be an admin there.
	b.Handle(ctx, cb(ownerID, "a:settings"))
	if m := find(t, api.take(), ownerID, "تنظیمات ربات"); !hasButton(m.kb, "عضویت اجباری") {
		t.Fatal("bot settings must lead to the required channel")
	}
	b.Handle(ctx, cb(ownerID, "a:join:on"))
	find(t, api.take(), ownerID, "اول یک کانال")
	b.Handle(ctx, cb(ownerID, "a:join:add"))
	api.take()
	b.Handle(ctx, msg(ownerID, "not a channel!"))
	find(t, api.take(), ownerID, "آیدی کانال عمومی")
	b.Handle(ctx, msg(ownerID, "https://t.me/darkvpn"))
	find(t, api.take(), ownerID, "ادمین")
	if len(s.JoinPolicy(ctx, "").Channels) != 0 {
		t.Fatal("a channel the bot cannot check is not saved")
	}
	api.admin = true
	b.Handle(ctx, msg(ownerID, "https://t.me/darkvpn"))
	find(t, api.take(), ownerID, "@darkvpn")
	b.Handle(ctx, cb(ownerID, "a:join:on"))
	find(t, api.take(), ownerID, "🟢 روشن")

	// A customer outside the channel: the menu and buying wait, their own
	// services and support stay open.
	b.Handle(ctx, msg(cust, "/start"))
	m := find(t, api.take(), cust, "عضویت در کانال")
	if !hasButton(m.kb, "عضو شدم") || !hasButton(m.kb, "@darkvpn") || !hasButton(m.kb, "پشتیبانی") {
		t.Fatalf("the prompt: %+v", m.kb)
	}
	b.Handle(ctx, cb(cust, "shop"))
	find(t, api.take(), cust, "عضویت در کانال")
	b.Handle(ctx, cb(cust, "trial"))
	find(t, api.take(), cust, "عضویت در کانال")
	b.Handle(ctx, cb(cust, "mine"))
	for _, m := range api.take() {
		if m.chat == cust && contains(m.text, "عضویت در کانال") {
			t.Fatal("a customer's own services stay open")
		}
	}
	b.Handle(ctx, cb(cust, "support"))
	find(t, api.take(), cust, "پشتیبانی")
	b.Handle(ctx, msg(cust, "سلام، مشکل دارم"))
	for _, m := range api.take() {
		if m.chat == cust && contains(m.text, "عضویت در کانال") {
			t.Fatal("a support message is never held back")
		}
	}
	// "I joined" without joining changes nothing; after joining the bot
	// carries on with what the customer was after (here: support, so the menu).
	b.Handle(ctx, cb(cust, "join:ok"))
	find(t, api.take(), cust, "عضویت در کانال")
	api.in[cust] = true
	b.Handle(ctx, cb(cust, "join:ok"))
	api.take()
	asked := api.asked
	b.Handle(ctx, cb(cust, "shop"))
	api.take()
	if api.asked != asked {
		t.Fatal("a confirmed membership is remembered for a while")
	}
	// The owner is never asked; an error from Telegram lets customers in.
	b.Handle(ctx, msg(ownerID, "/start"))
	find(t, api.take(), ownerID, "پنل مدیریت")
	api.admin = false
	b.Handle(ctx, msg(cust+1, "/start"))
	find(t, api.take(), cust+1, "سلام")

	// The channel post's link: /start trial. Not a member yet: join first,
	// then "عضو شدم" hands over the trial itself, once.
	api.admin = true
	_ = s.PutTrialPolicy(ctx, hubcore.TrialPolicy{Enabled: true, GB: 5, Hours: 168, MaxIPs: 1})
	const friend = int64(8200)
	b.Handle(ctx, msg(friend, "/start trial"))
	find(t, api.take(), friend, "عضویت در کانال")
	if s.TrialUsed(ctx, "", friend) {
		t.Fatal("no trial before joining")
	}
	api.in[friend] = true
	b.Handle(ctx, cb(friend, "join:ok"))
	find(t, api.take(), friend, "5 گیگ برای 7 روز")
	if c, err := s.ClientByEmail(ctx, "trial-8200"); err != nil || !c.Trial || c.VolumeBytes != 5<<30 {
		t.Fatalf("%+v %v", c, err)
	}
	b.Handle(ctx, msg(friend, "/start trial"))
	find(t, api.take(), friend, "قبلاً")
	if st := s.TrialStats(ctx, ""); st.Taken != 1 || st.Running != 1 || st.Connected != 0 {
		t.Fatalf("%+v", st)
	}
	// A day before the end the owner of the trial is reminded, once.
	later := func() time.Time { return time.Now().Add(6*24*time.Hour + time.Hour) }
	b.Now = later
	s.SetClock(later)
	b.remindTrials(ctx)
	find(t, api.take(), friend, "رو به پایان")
	b.remindTrials(ctx)
	if len(api.take()) != 0 {
		t.Fatal("one reminder per trial")
	}
}
