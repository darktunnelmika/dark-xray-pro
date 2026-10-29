package backupcontrol

import (
	"strconv"
	"time"
	_ "time/tzdata"
)

func ValidateSettings(s Settings) error {
	if s.Revision == 0 || s.Revision > 9007199254740991 || s.Retention.KeepLast < 1 || s.Retention.KeepLast > 100 || s.Retention.MaxAgeDays < 1 || s.Retention.MaxAgeDays > 365 {
		return ErrInvalid
	}
	return ValidateSchedule(s.Schedule)
}
func ValidateSchedule(s Schedule) error {
	if s.Frequency != "daily" && s.Frequency != "weekly" || s.Weekday < 0 || s.Weekday > 6 || len(s.Time) != 5 || s.Time[2] != ':' || len(s.Timezone) < 1 || len(s.Timezone) > 80 || s.Timezone == "Local" {
		return ErrInvalid
	}
	h, e1 := strconv.Atoi(s.Time[:2])
	m, e2 := strconv.Atoi(s.Time[3:])
	if e1 != nil || e2 != nil || h < 0 || h > 23 || m < 0 || m > 59 || s.Time[:2] != twoDigits(h) || s.Time[3:] != twoDigits(m) {
		return ErrInvalid
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return ErrInvalid
	}
	return nil
}
func twoDigits(n int) string { return string([]byte{byte('0' + n/10), byte('0' + n%10)}) }

func civilSlot(s Schedule, date time.Time, loc *time.Location) (time.Time, bool) {
	h, _ := strconv.Atoi(s.Time[:2])
	m, _ := strconv.Atoi(s.Time[3:])
	y, month, day := date.Date()
	t := time.Date(y, month, day, h, m, 0, 0, loc)
	// A skipped spring-forward civil time must not silently run at another hour.
	if t.Year() != y || t.Month() != month || t.Day() != day || t.Hour() != h || t.Minute() != m {
		return time.Time{}, false
	}
	if s.Frequency == "weekly" && int(t.Weekday()) != s.Weekday {
		return time.Time{}, false
	}
	return t, true
}

// NextRun computes one civil occurrence. Fall-back ambiguity is resolved by
// time.Date consistently, so one scheduled local time cannot fire twice.
func NextRun(s Schedule, after time.Time) (time.Time, error) {
	if ValidateSchedule(s) != nil {
		return time.Time{}, ErrInvalid
	}
	if !s.Enabled {
		return time.Time{}, nil
	}
	loc, _ := time.LoadLocation(s.Timezone)
	local := after.In(loc)
	date := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc)
	for day := 0; day < 370; day++ {
		if slot, ok := civilSlot(s, date.AddDate(0, 0, day), loc); ok && slot.After(after) {
			return slot, nil
		}
	}
	return time.Time{}, ErrInvalid
}

func latestRun(s Schedule, now time.Time) (time.Time, error) {
	if ValidateSchedule(s) != nil {
		return time.Time{}, ErrInvalid
	}
	if !s.Enabled {
		return time.Time{}, nil
	}
	loc, _ := time.LoadLocation(s.Timezone)
	local := now.In(loc)
	date := time.Date(local.Year(), local.Month(), local.Day(), 12, 0, 0, 0, loc)
	for day := 0; day < 370; day++ {
		if slot, ok := civilSlot(s, date.AddDate(0, 0, -day), loc); ok && !slot.After(now) {
			return slot, nil
		}
	}
	return time.Time{}, ErrInvalid
}
