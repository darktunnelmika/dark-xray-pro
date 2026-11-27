package tgbot

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func TestCustomerPowerButtons(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	_ = s.PutInbound(ctx, "in1", "n1", []byte(`{"protocol":"vless","port":443,"settings":{}}`), true)
	if err := s.PutClient(ctx, hubcore.Client{Email: "svc1", Enabled: true, VolumeBytes: 10 << 30, Inbounds: []string{"in1"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.TgLink(ctx, buyerID, "svc1"); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://x", Logf: t.Logf}
	b.Handle(ctx, cb(buyerID, "svc:svc1"))
	if m := find(t, api.take(), buyerID, "svc1"); !hasBtn(m.kb, "pwx:svc1") {
		t.Fatalf("no off button: %+v", m.kb)
	}
	b.Handle(ctx, cb(buyerID, "pwx:svc1"))
	if m := find(t, api.take(), buyerID, "خاموش کردن موقت"); !hasBtn(m.kb, "pwoff:svc1") {
		t.Fatalf("no confirm: %+v", m.kb)
	}
	b.Handle(ctx, cb(buyerID, "pwoff:svc1"))
	out := api.take()
	find(t, out, buyerID, "سرویس خاموش شد")
	if m := find(t, out, buyerID, "خودتان موقتاً خاموش"); !hasBtn(m.kb, "pwon:svc1") {
		t.Fatalf("no on button: %+v", m.kb)
	}
	if c, _ := s.ClientByEmail(ctx, "svc1"); c.Enabled {
		t.Fatal("still on")
	}
	// Someone else's service: nothing happens.
	b.Handle(ctx, cb(buyerID+1, "pwon:svc1"))
	if c, _ := s.ClientByEmail(ctx, "svc1"); c.Enabled {
		t.Fatal("a stranger turned it on")
	}
	b.Handle(ctx, cb(buyerID, "pwon:svc1"))
	find(t, api.take(), buyerID, "سرویس روشن شد")
	if c, _ := s.ClientByEmail(ctx, "svc1"); !c.Enabled {
		t.Fatal("not back on")
	}
}
