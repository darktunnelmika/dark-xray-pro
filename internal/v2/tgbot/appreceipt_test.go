package tgbot

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

type uploadAPI struct {
	fakeAPI
	uploads int
	fail    error
}

func (u *uploadAPI) UploadPhoto(_ context.Context, chat int64, _ string, _ []byte, caption string, _ *Keyboard) (string, error) {
	if u.fail != nil {
		return "", u.fail
	}
	u.uploads++
	u.mu.Lock()
	u.out = append(u.out, sent{chat: chat, text: caption, photo: true})
	u.mu.Unlock()
	return "file-app-1", nil
}

func TestAppReceipt(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	api := &uploadAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://sub.example", Now: time.Now, Logf: t.Logf}
	if err := s.PutCard(ctx, hubcore.CardInfo{Number: "6037991234567890"}); err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateTopup(ctx, "", buyerID, 50000)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AppReceipt(ctx, buyerID, "U", "topup", id, []byte("img")); err != nil {
		t.Fatal(err)
	}
	if tp, _ := s.TopupByID(ctx, id); !tp.HasReceipt || api.uploads != 1 {
		t.Fatalf("topup %+v uploads %d", tp, api.uploads)
	}
	out := api.take()
	find(t, out, buyerID, "مینی‌اپ")
	find(t, out, ownerID, "شارژ کیف پول")
	// The same payment again: nothing waits for it any more.
	if err := b.AppReceipt(ctx, buyerID, "U", "topup", id, []byte("img")); !errors.Is(err, hubcore.ErrNotFound) {
		t.Fatalf("second %v", err)
	}
	// A chat the bot cannot write to.
	api.fail = ErrBlocked
	id2, _ := s.CreateTopup(ctx, "", buyerID+1, 50000)
	if err := b.AppReceipt(ctx, buyerID+1, "U", "topup", id2, []byte("img")); !errors.Is(err, hubcore.ErrNoBotChat) {
		t.Fatalf("blocked %v", err)
	}
}
