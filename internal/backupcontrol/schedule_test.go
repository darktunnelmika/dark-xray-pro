package backupcontrol

import (
	"context"
	"testing"
	"time"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	v, e := time.Parse(time.RFC3339, value)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

func TestCivilScheduleTimezonesAndDST(t *testing.T) {
	for _, tt := range []struct {
		name, frequency, zone, clock, after, want string
		weekday                                   int
	}{
		{"daily Tehran", "daily", "Asia/Tehran", "03:00", "2026-10-01T01:00:00Z", "2026-10-01T23:30:00Z", 0},
		{"weekly Monday", "weekly", "UTC", "14:15", "2026-10-01T00:00:00Z", "2026-10-05T14:15:00Z", 1},
		{"spring gap skipped", "daily", "America/New_York", "02:30", "2026-03-08T05:00:00Z", "2026-03-09T06:30:00Z", 0},
		{"fall overlap once", "daily", "America/New_York", "01:30", "2026-11-01T05:31:00Z", "2026-11-02T06:30:00Z", 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := Schedule{Enabled: true, Frequency: tt.frequency, Timezone: tt.zone, Time: tt.clock, Weekday: tt.weekday}
			got, e := NextRun(s, mustTime(t, tt.after))
			if e != nil || !got.Equal(mustTime(t, tt.want)) {
				t.Fatalf("next %s, want %s: %v", got, tt.want, e)
			}
		})
	}
	for _, bad := range []Schedule{{Frequency: "hourly", Time: "03:00", Timezone: "UTC"}, {Frequency: "daily", Time: "3:00", Timezone: "UTC"}, {Frequency: "daily", Time: "24:00", Timezone: "UTC"}, {Frequency: "daily", Time: "03:00", Timezone: "Local"}, {Frequency: "weekly", Time: "03:00", Timezone: "UTC", Weekday: 7}} {
		if ValidateSchedule(bad) == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func TestScheduleOneCatchupAndDurableSlotDedup(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	settings := DefaultSettings()
	settings.Schedule = Schedule{Enabled: true, Frequency: "daily", Time: "03:00", Timezone: "UTC"}
	_, e := f.s.UpdateSettings(ctx, SettingsRequest{EventID: "enable", ActorID: "owner-1", ExpectedRevision: 1, Schedule: settings.Schedule, Retention: settings.Retention})
	if e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(9*24*time.Hour + 4*time.Hour)
	first, e := f.s.EnqueueScheduled(ctx, f.now)
	if e != nil || first == nil {
		t.Fatalf("catchup: %+v %v", first, e)
	}
	if first.Trigger != "schedule" || first.EventID != "schedule:1791601200" {
		t.Fatalf("should enqueue most recent slot only: %+v", first)
	}
	f.finish(t, first.ID)
	if e = f.s.Close(); e != nil {
		t.Fatal(e)
	}
	f.reopen(t)
	if next, e := f.s.EnqueueScheduled(ctx, f.now); e != nil || next != nil {
		t.Fatalf("duplicate catchup after restart: %+v %v", next, e)
	}
	_, e = f.s.UpdateSettings(ctx, SettingsRequest{EventID: "retention-only", ActorID: "owner-1", ExpectedRevision: 2, Schedule: settings.Schedule, Retention: Retention{KeepLast: 2, MaxAgeDays: 60}})
	if e != nil {
		t.Fatal(e)
	}
	if next, e := f.s.EnqueueScheduled(ctx, f.now); e != nil || next != nil {
		t.Fatalf("retention edit duplicated slot: %+v %v", next, e)
	}
	f.now = f.now.Add(24 * time.Hour)
	next, e := f.s.EnqueueScheduled(ctx, f.now)
	if e != nil || next == nil || next.EventID == first.EventID {
		t.Fatalf("next daily slot: %+v %v", next, e)
	}
}

func TestEnablingScheduleDoesNotRunHistoricalSlot(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.now = f.now.Add(4 * time.Hour)
	settings := DefaultSettings()
	settings.Schedule = Schedule{Enabled: true, Frequency: "daily", Time: "03:00", Timezone: "UTC"}
	_, e := f.s.UpdateSettings(ctx, SettingsRequest{EventID: "enable-after-slot", ActorID: "owner-1", ExpectedRevision: 1, Schedule: settings.Schedule, Retention: settings.Retention})
	if e != nil {
		t.Fatal(e)
	}
	if j, e := f.s.EnqueueScheduled(ctx, f.now); e != nil || j != nil {
		t.Fatalf("unexpected historical run: %+v %v", j, e)
	}
}
