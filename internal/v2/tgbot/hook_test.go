package tgbot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// A bot on a webhook handles what the hub hands it, in order.
func TestRunHook(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{}, SubBase: "https://sub.example", Now: time.Now, Logf: t.Logf}
	ctx, cancel := context.WithCancel(context.Background())
	ups := make(chan Update, 4)
	done := make(chan struct{})
	go func() { b.RunHook(ctx, ups); close(done) }()
	ups <- msg(buyerID, "/start")
	deadline := time.Now().Add(5 * time.Second)
	for {
		found := false
		for _, m := range api.take() {
			if m.chat == buyerID && strings.Contains(m.text, "خوش آمدید") {
				found = true
			}
		}
		if found {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no answer to an update from the webhook")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunHook did not stop")
	}
}
