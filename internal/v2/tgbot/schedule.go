package tgbot

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // Asia/Tehran without the host's zoneinfo

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/hubcore"
)

// Scheduled broadcasts: the owner prepares a message as usual, then picks
// "schedule" instead of "send now", a time (buttons, or typed: 21:30,
// +2h, 1405/08/25 21:30 in the Persian calendar, or 2026-11-20 21:30) and
// whether it repeats daily or weekly. Times are Tehran time.

var tehran = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Tehran"); err == nil {
		return l
	}
	return time.FixedZone("IRST", 3*3600+1800)
}()

var faDigits = strings.NewReplacer("۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4", "۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
	"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4", "٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9", "：", ":")

var (
	relRE  = regexp.MustCompile(`^\+\s*(\d{1,4})\s*([mhd])$`)
	clkRE  = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	dateRE = regexp.MustCompile(`^(\d{4})[-/](\d{1,2})[-/](\d{1,2})(?:\s+(\d{1,2}):(\d{2}))?$`)
)

// parseWhen reads a time typed by the owner, relative to now (Tehran).
func parseWhen(text string, now time.Time) (time.Time, error) {
	s := strings.ToLower(strings.TrimSpace(faDigits.Replace(text)))
	now = now.In(tehran)
	if m := relRE.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
		return now.Add(time.Duration(n) * unit), nil
	}
	if m := clkRE.FindStringSubmatch(s); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		if h > 23 || mi > 59 {
			return time.Time{}, errors.New("bad time")
		}
		t := time.Date(now.Year(), now.Month(), now.Day(), h, mi, 0, 0, tehran)
		if !t.After(now) {
			t = t.AddDate(0, 0, 1)
		}
		return t, nil
	}
	if m := dateRE.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		d, _ := strconv.Atoi(m[3])
		h, mi := 10, 0
		if m[4] != "" {
			h, _ = strconv.Atoi(m[4])
			mi, _ = strconv.Atoi(m[5])
		}
		if mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 {
			return time.Time{}, errors.New("bad date")
		}
		if y < 1700 { // Persian calendar
			if mo > 6 && d > 30 {
				return time.Time{}, errors.New("bad date")
			}
			y, mo, d = jalaliToGregorian(y, mo, d)
		}
		t := time.Date(y, time.Month(mo), d, h, mi, 0, 0, tehran)
		if t.Day() != d {
			return time.Time{}, errors.New("bad date")
		}
		return t, nil
	}
	return time.Time{}, errors.New("unknown format")
}

func jalaliToGregorian(jy, jm, jd int) (int, int, int) {
	jy += 1595
	days := -355668 + 365*jy + (jy/33)*8 + ((jy%33)+3)/4 + jd
	if jm < 7 {
		days += (jm - 1) * 31
	} else {
		days += (jm-7)*30 + 186
	}
	gy := 400 * (days / 146097)
	days %= 146097
	if days > 36524 {
		days--
		gy += 100 * (days / 36524)
		days %= 36524
		if days >= 365 {
			days++
		}
	}
	gy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		gy += (days - 1) / 365
		days = (days - 1) % 365
	}
	gd := days + 1
	feb := 28
	if gy%4 == 0 && gy%100 != 0 || gy%400 == 0 {
		feb = 29
	}
	months := []int{31, feb, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
	gm := 0
	for gm < 12 && gd > months[gm] {
		gd -= months[gm]
		gm++
	}
	return gy, gm + 1, gd
}

func gregorianToJalali(gy, gm, gd int) (int, int, int) {
	gdm := []int{0, 31, 59, 90, 120, 151, 181, 212, 243, 273, 304, 334}
	gy2 := gy
	if gm > 2 {
		gy2 = gy + 1
	}
	days := 355666 + 365*gy + (gy2+3)/4 - (gy2+99)/100 + (gy2+399)/400 + gd + gdm[gm-1]
	jy := -1595 + 33*(days/12053)
	days %= 12053
	jy += 4 * (days / 1461)
	days %= 1461
	if days > 365 {
		jy += (days - 1) / 365
		days = (days - 1) % 365
	}
	if days < 186 {
		return jy, 1 + days/31, 1 + days%31
	}
	return jy, 7 + (days-186)/30, 1 + (days-186)%30
}

var weekdayFa = []string{"یکشنبه", "دوشنبه", "سه‌شنبه", "چهارشنبه", "پنجشنبه", "جمعه", "شنبه"}

// faTime shows a moment as "شنبه ۱۴۰۵/۰۸/۲۳ ساعت ۲۱:۰۰" (Tehran).
func faTime(u int64) string {
	t := time.Unix(u, 0).In(tehran)
	y, m, d := gregorianToJalali(t.Year(), int(t.Month()), t.Day())
	return faNum(fmt.Sprintf("%s %d/%02d/%02d ساعت %02d:%02d", weekdayFa[t.Weekday()], y, m, d, t.Hour(), t.Minute()))
}

func faNum(s string) string {
	return strings.NewReplacer("0", "۰", "1", "۱", "2", "۲", "3", "۳", "4", "۴", "5", "۵", "6", "۶", "7", "۷", "8", "۸", "9", "۹").Replace(s)
}

var repeatName = map[string]string{"": "یک‌بار", "daily": "هر روز", "weekly": "هر هفته"}

func (b *Bot) askWhen(ctx context.Context, chat int64) {
	b.setAwait(chat, "schedtime")
	now := b.now().In(tehran)
	tonight := "امشب ۲۱:۰۰"
	if now.Hour() >= 21 {
		tonight = "فردا شب ۲۱:۰۰"
	}
	b.send(ctx, chat, "⏰ چه زمانی ارسال شود؟ (به وقت تهران)\nیا خودتان بنویسید، مثلاً:\n<code>21:30</code> · <code>+2h</code> · <code>1405/08/25 21:30</code>", &Keyboard{Rows: [][]Button{
		{{Text: "۱ ساعت بعد", Data: "a:bcat:r60"}, {Text: "۳ ساعت بعد", Data: "a:bcat:r180"}},
		{{Text: tonight, Data: "a:bcat:c2100"}, {Text: "فردا ۱۰:۰۰", Data: "a:bcat:c1000"}},
		{{Text: "✖️ لغو", Data: "a:bcno"}}}})
}

// quickWhen resolves a time button.
func quickWhen(code string, now time.Time) (time.Time, bool) {
	if len(code) < 2 {
		return time.Time{}, false
	}
	n, err := strconv.Atoi(code[1:])
	if err != nil {
		return time.Time{}, false
	}
	switch code[0] {
	case 'r':
		return now.Add(time.Duration(n) * time.Minute), true
	case 'c':
		t, err := parseWhen(fmt.Sprintf("%02d:%02d", n/100, n%100), now)
		return t, err == nil
	}
	return time.Time{}, false
}

func (b *Bot) setWhen(ctx context.Context, chat int64, at time.Time) {
	if !at.After(b.now().Add(30 * time.Second)) {
		b.send(ctx, chat, "این زمان گذشته است. یک زمان در آینده بدهید.", nil)
		b.setAwait(chat, "schedtime")
		return
	}
	b.mu.Lock()
	d := b.draft[chat]
	if d != nil {
		d.at = at.Unix()
	}
	b.mu.Unlock()
	if d == nil {
		b.send(ctx, chat, "پیش‌نویسی نیست.", nil)
		return
	}
	b.send(ctx, chat, fmt.Sprintf("🗓 %s\nتکرار شود؟", faTime(d.at)), &Keyboard{Rows: [][]Button{
		{{Text: "یک‌بار", Data: "a:bcrep:once"}, {Text: "هر روز", Data: "a:bcrep:daily"}, {Text: "هر هفته", Data: "a:bcrep:weekly"}},
		{{Text: "تغییر زمان", Data: "a:bcsch"}, {Text: "✖️ لغو", Data: "a:bcno"}}}})
}

func (b *Bot) saveScheduled(ctx context.Context, chat int64, repeat string) {
	if repeat == "once" {
		repeat = ""
	}
	b.mu.Lock()
	d := b.draft[chat]
	delete(b.draft, chat)
	b.mu.Unlock()
	if d == nil || d.at == 0 || d.audience == "" {
		b.send(ctx, chat, "پیش‌نویسی نیست.", nil)
		return
	}
	m := hubcore.TgScheduled{OwnerChat: chat, Text: d.text, Photo: d.photo, Audience: d.audience, At: d.at, Repeat: repeat}
	if d.button != nil {
		m.BtnText, m.BtnURL = d.button.Text, d.button.URL
	}
	id, err := b.Store.TgSchedule(ctx, m)
	if err != nil {
		b.send(ctx, chat, "ذخیره نشد: "+esc(err.Error()), nil)
		return
	}
	b.send(ctx, chat, fmt.Sprintf("✅ زمان‌بندی شد (#%d)\n🗓 %s · %s\n👥 %s — فهرست مخاطب هنگام ارسال به‌روز می‌شود.", id, faTime(d.at), repeatName[repeat], audienceName[d.audience]),
		&Keyboard{Rows: [][]Button{{{Text: "⏰ زمان‌بندی‌ها", Data: "a:schlist"}}, back("a:home")}})
}

func (b *Bot) scheduledView(ctx context.Context, chat int64) {
	list, _ := b.Store.TgScheduledList(ctx)
	if len(list) == 0 {
		b.send(ctx, chat, "⏰ پیام زمان‌بندی‌شده‌ای ندارید.\nبرای ساختن: «📣 پیام همگانی» ← پیش‌نمایش ← «⏰ زمان‌بندی».", &Keyboard{Rows: [][]Button{{{Text: "📣 پیام همگانی", Data: "a:bc"}}, back("a:home")}})
		return
	}
	var lines []string
	kb := &Keyboard{}
	for _, m := range list {
		preview := m.Text
		if r := []rune(preview); len(r) > 60 {
			preview = string(r[:60]) + "…"
		}
		if m.Photo != "" {
			preview = "🖼 " + preview
		}
		line := fmt.Sprintf("<b>#%d</b> · %s · %s · %s\n%s", m.ID, faTime(m.At), repeatName[m.Repeat], audienceName[m.Audience], esc(preview))
		if m.Runs > 0 {
			line += fmt.Sprintf("\n<i>%d بار ارسال شده — آخرین: %s</i>", m.Runs, esc(m.LastResult))
		}
		lines = append(lines, line)
		kb.Rows = append(kb.Rows, []Button{{Text: fmt.Sprintf("❌ لغو #%d", m.ID), Data: fmt.Sprintf("a:schcancel:%d", m.ID)}})
	}
	kb.Rows = append(kb.Rows, back("a:home"))
	b.send(ctx, chat, "⏰ <b>پیام‌های زمان‌بندی‌شده</b>\n\n"+strings.Join(lines, "\n\n"), kb)
}

// runScheduled sends what is due. Claiming happens in the store, so a
// broadcast is never sent twice, even across restarts.
func (b *Bot) runScheduled(ctx context.Context) {
	b.mu.Lock()
	if b.schedBusy {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()
	due, err := b.Store.TgClaimDue(ctx)
	if err != nil || len(due) == 0 {
		return
	}
	b.mu.Lock()
	b.schedBusy = true
	b.mu.Unlock()
	if b.SyncSchedule {
		b.sendDue(ctx, due)
		return
	}
	go b.sendDue(context.WithoutCancel(ctx), due)
}

// sendDue sends claimed broadcasts one after another, off the tick loop.
func (b *Bot) sendDue(ctx context.Context, due []hubcore.TgScheduled) {
	defer func() {
		b.mu.Lock()
		b.schedBusy = false
		b.mu.Unlock()
	}()
	for _, m := range due {
		d := &bcDraft{text: m.Text, photo: m.Photo, audience: m.Audience, title: fmt.Sprintf("⏰ پیام زمان‌بندی‌شده #%d", m.ID)}
		if m.BtnURL != "" {
			d.button = &Button{Text: m.BtnText, URL: m.BtnURL}
		}
		d.chats = b.audience(ctx, m.Audience)
		id := m.ID
		d.done = func(result string) { _ = b.Store.TgScheduledDone(context.WithoutCancel(ctx), id, result) }
		b.mu.Lock()
		b.bcStop = false
		b.mu.Unlock()
		b.broadcast(ctx, m.OwnerChat, d)
	}
}
