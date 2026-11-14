package tgbot

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// Customer notifications, sent to every chat linked to a client: first
// connection, volume almost used (90%) or used up, term ending within a day
// or ended. Each fires once per "version" of the plan (its volume and expiry),
// so a renewal re-arms them without repeating old ones.

func (b *Bot) notifyClients(ctx context.Context) {
	b.mu.Lock()
	due := b.now().Sub(b.lastNotify) >= 2*time.Minute
	if due {
		b.lastNotify = b.now()
	}
	b.mu.Unlock()
	if !due {
		return
	}
	links, err := b.Store.TgLinkedChats(ctx)
	if err != nil || len(links) == 0 {
		return
	}
	all, err := b.Store.Clients(ctx)
	if err != nil {
		return
	}
	now := b.now().Unix()
	for _, c := range all {
		chats := links[c.Email]
		if len(chats) == 0 {
			continue
		}
		version := strconv.FormatInt(c.VolumeBytes, 10) + "/" + strconv.FormatInt(c.EffectiveUntil, 10) + "/" + strconv.FormatInt(c.DurationSeconds, 10)
		used := c.Usage.Up + c.Usage.Down
		var events [][2]string
		if c.Usage.FirstSeen > 0 {
			events = append(events, [2]string{"first", "🎉 سرویس <b>%s</b> وصل شد و فعال است. استفادهٔ خوبی داشته باشید!"})
		}
		if c.VolumeBytes > 0 {
			switch {
			case used >= c.VolumeBytes:
				events = append(events, [2]string{"vol100", "⛔ حجم سرویس <b>%s</b> تمام شد. برای ادامه، از «سرویس‌های من» تمدید کنید."})
			case used*10 >= c.VolumeBytes*9:
				events = append(events, [2]string{"vol90", "⚠️ بیش از ۹۰٪ حجم سرویس <b>%s</b> مصرف شده است."})
			}
		}
		if c.EffectiveUntil > 0 {
			switch {
			case c.EffectiveUntil <= now:
				events = append(events, [2]string{"exp", "⛔ مدت سرویس <b>%s</b> تمام شد. برای ادامه، از «سرویس‌های من» تمدید کنید."})
			case c.EffectiveUntil-now <= 86400:
				events = append(events, [2]string{"exp1", "⏰ کمتر از یک روز از سرویس <b>%s</b> باقی مانده است."})
			}
		}
		for _, e := range events {
			key := "tgn:" + c.Email + ":" + e[0]
			mark := version
			if e[0] == "first" {
				mark = "1" // first connection is announced once per client
			}
			if b.Store.Setting(ctx, key) == mark {
				continue
			}
			_ = b.Store.SetSetting(ctx, key, mark)
			if e[0] == "first" && now-c.Usage.FirstSeen > 3600 {
				continue // old news (bot started later): record only
			}
			for _, chat := range chats {
				b.send(ctx, chat, fmt.Sprintf(e[1], esc(c.Email)), &Keyboard{Rows: [][]Button{{{Text: "📊 سرویس‌های من", Data: "mine"}}}})
			}
		}
	}
}
