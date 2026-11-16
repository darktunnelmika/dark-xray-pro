package tgbot

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
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
