package tgbot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// fakeForum is fakeAPI that can also post into forum topics; a thread's
// messages are recorded with chat = group and thread kept apart.
type fakeForum struct {
	fakeAPI
	topics  map[string]int64
	threads map[int64]int64 // message id -> thread
}

func (f *fakeForum) CreateForumTopic(_ context.Context, chat int64, name string, _ int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.topics == nil {
		f.topics = map[string]int64{}
	}
	id := int64(100 + len(f.topics))
	f.topics[name] = id
	return id, nil
}

func (f *fakeForum) SendThread(ctx context.Context, chat, thread int64, text string, kb *Keyboard) (int64, error) {
	id, err := f.Send(ctx, chat, text, kb, 0)
	f.mu.Lock()
	if f.threads == nil {
		f.threads = map[int64]int64{}
	}
	f.threads[id] = thread
	f.mu.Unlock()
	return id, err
}

func (f *fakeForum) SendPhotoIDThread(ctx context.Context, chat, thread int64, fileID, caption string, kb *Keyboard) error {
	return f.SendPhotoID(ctx, chat, fileID, caption, kb)
}

func (f *fakeForum) SendDocumentThread(ctx context.Context, chat, thread int64, name string, data []byte, caption string) error {
	return f.SendDocument(ctx, chat, name, data, caption)
}

func groupMsg(group, user int64, text string, replyTo int64) Update {
	m := &Message{ID: 900, From: &User{ID: user}, Chat: Chat{ID: group, Type: "supergroup", IsForum: true, Title: "Dark staff"}, Text: text}
	if replyTo != 0 {
		m.ReplyTo = &Message{ID: replyTo}
	}
	return Update{Message: m}
}

func TestForumNotices(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	api := &fakeForum{}
	const owner, group, cust = int64(1), int64(-1001), int64(5000)
	b := &Bot{Store: s, API: api, SubBase: "https://x", Logf: t.Logf, Owners: map[int64]bool{owner: true}}
	// A stranger cannot set the group up; the owner can.
	b.Handle(ctx, groupMsg(group, 777, "/setup", 0))
	if len(api.topics) != 0 {
		t.Fatal("stranger set it up")
	}
	b.Handle(ctx, groupMsg(group, owner, "/setup@test_bot", 0))
	if len(api.topics) != len(ownerTopics) {
		t.Fatalf("topics: %v", api.topics)
	}
	find(t, api.take(), group, "اعلان‌ها از این به بعد")
	// A ticket lands in the group's ticket topic, not the private chat.
	b.Handle(ctx, cb(cust, "support"))
	b.Handle(ctx, msg(cust, "help me"))
	out := api.take()
	m := find(t, out, group, "help me")
	for _, x := range out {
		if x.chat == owner {
			t.Fatal("notice also went to the private chat")
		}
	}
	var noticeID int64
	for id, th := range api.threads {
		if th == api.topics["🎫 تیکت‌ها"] && id > noticeID {
			noticeID = id
		}
	}
	_ = m
	// The owner answers by Reply in the group; others' replies do nothing.
	b.Handle(ctx, groupMsg(group, 777, "spam", noticeID))
	for _, x := range api.take() {
		if x.chat == cust {
			t.Fatal("a stranger answered")
		}
	}
	b.Handle(ctx, groupMsg(group, owner, "try again now", noticeID))
	out = api.take()
	find(t, out, cust, "try again now")
	find(t, out, group, "ارسال شد")
	// Node notices go to their topic too; /unsetup brings them back.
	b.notify(ctx, "nodes", "node down", nil)
	find(t, api.take(), group, "node down")
	b.Handle(ctx, groupMsg(group, owner, "/unsetup", 0))
	api.take()
	b.notify(ctx, "nodes", "node up", nil)
	find(t, api.take(), owner, "node up")
}

func TestNodeAlertsWaitForGrace(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	clock := time.Unix(1794800000, 0)
	s.SetClock(func() time.Time { return clock })
	_, _ = s.CreateNode(ctx, "tr-1", "TR")
	sync := func(up bool, since int64) {
		h := wire.Host{Tunnels: []wire.Tunnel{{Name: "turkey", Iface: "dgr1", Peer: "10.77.10.1", Up: up, Since: since}}}
		if _, err := s.Sync(ctx, "tr-1", wire.SyncRequest{AgentVersion: "v", Host: &h}); err != nil {
			t.Fatal(err)
		}
	}
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{1: true}, Logf: t.Logf, Now: func() time.Time { return clock }}
	sync(true, clock.Unix())
	b.watchNodes(ctx)
	step := func(sec int64, synced bool) []sent {
		clock = clock.Add(time.Duration(sec) * time.Second)
		if synced {
			sync(true, clock.Unix()-1000)
		}
		b.watchNodes(ctx)
		return api.take()
	}
	// A 40-second hiccup says nothing, before or after.
	if out := step(40, false); len(out) != 0 {
		t.Fatalf("hiccup alerted: %+v", out)
	}
	if out := step(5, true); len(out) != 0 {
		t.Fatalf("hiccup recovery alerted: %+v", out)
	}
	// Three minutes down: one alert, then one when it is back.
	step(60, false)
	if out := step(130, false); len(out) != 1 || !strings.Contains(out[0].text, "به پنل وصل نیست") {
		t.Fatalf("down: %+v", out)
	}
	if out := step(60, false); len(out) != 0 {
		t.Fatal("repeated down alert")
	}
	if out := step(5, true); len(out) != 1 || !strings.Contains(out[0].text, "دوباره") {
		t.Fatalf("back: %+v", out)
	}
	// A tunnel down for a minute is quiet; three minutes alerts once.
	clock = clock.Add(5 * time.Second)
	sync(false, clock.Unix()-60)
	b.watchNodes(ctx)
	if out := api.take(); len(out) != 0 {
		t.Fatalf("short tunnel drop alerted: %+v", out)
	}
	clock = clock.Add(5 * time.Second)
	sync(false, clock.Unix()-200)
	b.watchNodes(ctx)
	if out := api.take(); len(out) != 1 || !strings.Contains(out[0].text, "تونل") {
		t.Fatalf("tunnel down: %+v", out)
	}
	clock = clock.Add(5 * time.Second)
	sync(true, clock.Unix())
	b.watchNodes(ctx)
	if out := api.take(); len(out) != 1 || !strings.Contains(out[0].text, "دوباره وصل") {
		t.Fatalf("tunnel back: %+v", out)
	}
}
