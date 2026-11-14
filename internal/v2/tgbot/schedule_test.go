package tgbot

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

func TestParseWhen(t *testing.T) {
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, tehran) // ۱۴۰۵/۰۸/۲۳
	cases := map[string]string{
		"21:30":            "2026-11-14 21:30",
		"09:15":            "2026-11-15 09:15", // passed today -> tomorrow
		"۲۱:۳۰":            "2026-11-14 21:30",
		"+2h":              "2026-11-14 12:00",
		"+ 30m":            "2026-11-14 10:30",
		"+1d":              "2026-11-15 10:00",
		"1405/08/25 21:30": "2026-11-16 21:30",
		"۱۴۰۵/۰۹/۰۱ ۰۸:۰۰": "2026-11-22 08:00",
		"1405/01/01":       "2026-03-21 10:00",
		"2026-12-31 23:59": "2026-12-31 23:59",
	}
	for in, want := range cases {
		got, err := parseWhen(in, now)
		if err != nil || got.In(tehran).Format("2006-01-02 15:04") != want {
			t.Errorf("%q -> %v %v, want %s", in, got.In(tehran), err, want)
		}
	}
	for _, bad := range []string{"tomorrow", "25:00", "1405/07/31", "2026-02-30", "+h"} {
		if _, err := parseWhen(bad, now); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	for _, g := range [][3]int{{2026, 11, 14}, {2024, 2, 29}, {2025, 3, 20}, {2025, 3, 21}, {2000, 1, 1}} {
		jy, jm, jd := gregorianToJalali(g[0], g[1], g[2])
		if y, m, d := jalaliToGregorian(jy, jm, jd); y != g[0] || m != g[1] || d != g[2] {
			t.Errorf("roundtrip %v -> %d/%d/%d -> %d-%d-%d", g, jy, jm, jd, y, m, d)
		}
	}
	if s := faTime(now.Unix()); !strings.Contains(s, "۱۴۰۵/۰۸/۲۳") || !strings.Contains(s, "شنبه") {
		t.Errorf("faTime %q", s)
	}
}

func TestScheduledBroadcast(t *testing.T) {
	s, err := hubcore.Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	now := time.Date(2026, 11, 14, 10, 0, 0, 0, tehran)
	clock := func() time.Time { return now }
	s.SetClock(clock)
	api := &fakeAPI{}
	b := &Bot{Store: s, API: api, Owners: map[int64]bool{ownerID: true}, SubBase: "https://x", Logf: t.Logf, Now: clock, SyncSchedule: true}
	b.Handle(ctx, msg(buyerID, "/start"))
	api.take()

	b.Handle(ctx, cb(ownerID, "a:bc"))
	b.Handle(ctx, msg(ownerID, "Sale tonight!\n[Buy](https://example.com/shop)"))
	b.Handle(ctx, cb(ownerID, "a:bca:all"))
	prev := find(t, api.take(), ownerID, "پیش‌نمایش")
	if prev.kb.Rows[0][1].Data != "a:bcsch" {
		t.Fatalf("no schedule button: %+v", prev.kb)
	}
	b.Handle(ctx, cb(ownerID, "a:bcsch"))
	api.take()
	b.Handle(ctx, msg(ownerID, "nonsense"))
	find(t, api.take(), ownerID, "نفهمیدم")
	b.Handle(ctx, msg(ownerID, "+2h"))
	find(t, api.take(), ownerID, "تکرار شود")
	b.Handle(ctx, cb(ownerID, "a:bcrep:daily"))
	find(t, api.take(), ownerID, "زمان‌بندی شد (#1)")

	b.Tick(ctx)
	for _, m := range api.take() {
		if m.chat == buyerID {
			t.Fatal("sent before its time")
		}
	}
	b.Handle(ctx, cb(ownerID, "a:schlist"))
	find(t, api.take(), ownerID, "هر روز")

	now = now.Add(2*time.Hour + time.Minute)
	b.Tick(ctx)
	out := api.take()
	find(t, out, buyerID, "Sale tonight!")
	find(t, out, ownerID, "#1 تمام شد")
	b.Tick(ctx) // not again
	for _, m := range api.take() {
		if m.chat == buyerID {
			t.Fatal("sent twice")
		}
	}
	list, _ := s.TgScheduledList(ctx)
	if len(list) != 1 || list[0].Runs != 1 || time.Unix(list[0].At, 0).In(tehran).Format("2006-01-02 15:04") != "2026-11-15 12:00" {
		t.Fatalf("next run %+v", list)
	}
	b.Handle(ctx, cb(ownerID, "a:schcancel:1"))
	find(t, api.take(), ownerID, "لغو شد")
	now = now.Add(48 * time.Hour)
	b.Tick(ctx)
	for _, m := range api.take() {
		if m.chat == buyerID {
			t.Fatal("cancelled broadcast sent")
		}
	}
}
