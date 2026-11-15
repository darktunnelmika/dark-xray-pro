package hubcore

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Owner alerts on Telegram beyond the disk warning: every sign-in to the
// panel, and CPU or memory above a threshold on the HUB or any node (two
// checks in a row, at most once an hour per server, with a note when it
// calms down).

// AlertSettings are the owner's choices (Settings → Telegram).
type AlertSettings struct {
	Login       bool `json:"login"`        // tell owners about every sign-in
	CPUPercent  int  `json:"cpu_percent"`  // 0 = off
	RAMPercent  int  `json:"ram_percent"`  // 0 = off
	ReportHours int  `json:"report_hours"` // the bot's report: every N hours (24 = daily)
	Nodes       bool `json:"nodes"`        // a node or its Xray going down and coming back (on by default)
}

func (s *Store) Alerts(ctx context.Context) AlertSettings {
	a := AlertSettings{Login: s.Setting(ctx, "alert_login") == "1", ReportHours: 24, Nodes: s.Setting(ctx, "alert_nodes") != "0"}
	a.CPUPercent, _ = strconv.Atoi(s.Setting(ctx, "alert_cpu"))
	a.RAMPercent, _ = strconv.Atoi(s.Setting(ctx, "alert_ram"))
	if h, err := strconv.Atoi(s.Setting(ctx, "tg_report_hours")); err == nil && h > 0 {
		a.ReportHours = h
	}
	return a
}

func (s *Store) PutAlerts(ctx context.Context, a AlertSettings) error {
	if a.CPUPercent < 0 || a.CPUPercent > 100 || a.RAMPercent < 0 || a.RAMPercent > 100 || (a.CPUPercent > 0 && a.CPUPercent < 50) || (a.RAMPercent > 0 && a.RAMPercent < 50) {
		return fmt.Errorf("%w: thresholds 50–100 (0 = off)", ErrInvalid)
	}
	switch a.ReportHours {
	case 1, 2, 3, 4, 6, 8, 12, 24, 168:
	default:
		return fmt.Errorf("%w: report every 1, 2, 3, 4, 6, 8, 12, 24 or 168 hours", ErrInvalid)
	}
	login := "0"
	if a.Login {
		login = "1"
	}
	nodesOn := "0"
	if a.Nodes {
		nodesOn = "1"
	}
	for k, v := range map[string]string{"alert_nodes": nodesOn, "alert_login": login, "alert_cpu": strconv.Itoa(a.CPUPercent), "alert_ram": strconv.Itoa(a.RAMPercent), "tg_report_hours": strconv.Itoa(a.ReportHours)} {
		if err := s.SetSetting(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}

// noteLogin tells owners about a sign-in when they asked for it.
func (s *Store) noteLogin(ctx context.Context, user string, r *http.Request) {
	if s.Setting(ctx, "alert_login") != "1" {
		return
	}
	notifyOwners(ctx, fmt.Sprintf("🔐 ورود به پنل\n👤 %s\n🌍 %s\n🖥 %s\n🕒 %s", user, realIP(r), shortAgent(r.UserAgent()), s.now().UTC().Format("2006-01-02 15:04 UTC")))
}

func shortAgent(ua string) string {
	switch {
	case ua == "":
		return "—"
	case len(ua) > 90:
		return ua[:90] + "…"
	}
	return ua
}

// loadWatch keeps per-server state between checks.
type loadWatch struct {
	mu      sync.Mutex
	high    map[string]int       // consecutive high readings
	alerted map[string]time.Time // last alert per server+metric
	down    map[string]int       // consecutive bad checks per node problem
	downAt  map[string]time.Time // when an announced outage began
}

var loads = &loadWatch{high: map[string]int{}, alerted: map[string]time.Time{}, down: map[string]int{}, downAt: map[string]time.Time{}}

// checkDown announces a problem after two bad checks in a row and its end.
func (s *Store) checkDown(ctx context.Context, key, downText, upText string, bad bool) {
	loads.mu.Lock()
	defer loads.mu.Unlock()
	if bad {
		loads.down[key]++
		if loads.down[key] == 2 {
			loads.downAt[key] = s.now()
			notifyOwners(ctx, downText)
		}
		return
	}
	if at, ok := loads.downAt[key]; ok {
		mins := int(s.now().Sub(at).Minutes()) + 4 // it was already down for two checks
		notifyOwners(ctx, fmt.Sprintf("%s (حدود %d دقیقه)", upText, mins))
		delete(loads.downAt, key)
	}
	loads.down[key] = 0
}

// checkLoad raises (or clears) an alert for one reading.
func (s *Store) checkLoad(ctx context.Context, key, label, metric string, value float64, limit int) {
	if limit <= 0 {
		return
	}
	loads.mu.Lock()
	defer loads.mu.Unlock()
	k := key + "|" + metric
	if value >= float64(limit) {
		loads.high[k]++
		if loads.high[k] >= 2 && s.now().Sub(loads.alerted[k]) >= time.Hour {
			loads.alerted[k] = s.now()
			notifyOwners(ctx, fmt.Sprintf("🔥 %s روی %s به %d٪ رسید (حد هشدار: %d٪).", metric, label, int(value+0.5), limit))
		}
		return
	}
	if loads.high[k] >= 2 && !loads.alerted[k].IsZero() && value < float64(limit)-10 {
		notifyOwners(ctx, fmt.Sprintf("✅ %s روی %s به حالت عادی برگشت (%d٪).", metric, label, int(value+0.5)))
		loads.alerted[k] = time.Time{}
	}
	if value < float64(limit)-10 {
		loads.high[k] = 0
	}
}

// watchLoad checks the HUB and every online node once.
func (s *Store) watchLoad(ctx context.Context) {
	a := s.Alerts(ctx)
	now := s.now()
	if a.Nodes {
		nodes, _ := s.Nodes(ctx)
		for _, n := range nodes {
			if n.LastSeen == 0 {
				continue // never connected yet
			}
			name := n.Name
			if name == "" {
				name = n.ID
			}
			on := n.Online(now)
			s.checkDown(ctx, "off:"+n.ID, "🔴 نود "+name+" قطع شد: بیش از چند دقیقه است که به هاب وصل نشده. کاربرهای این نود احتمالاً وصل نمی‌شوند.", "🟢 نود "+name+" دوباره وصل شد", !on)
			if on {
				s.checkDown(ctx, "xray:"+n.ID, "⚠️ Xray روی نود "+name+" اجرا نمی‌شود"+lastErr(n.LastError), "✅ Xray روی نود "+name+" دوباره کار می‌کند", !n.XrayRunning)
			}
		}
	}
	if a.CPUPercent == 0 && a.RAMPercent == 0 {
		return
	}
	si := s.System(ctx)
	s.checkLoad(ctx, "hub", "سرور هاب", "پردازنده", si.CPU, a.CPUPercent)
	if si.RAMTotal > 0 {
		s.checkLoad(ctx, "hub", "سرور هاب", "حافظه", 100*float64(si.RAMUsed)/float64(si.RAMTotal), a.RAMPercent)
	}
	nodes, _ := s.Nodes(ctx)
	for _, n := range nodes {
		if !n.Online(now) || n.Host == nil {
			continue
		}
		name := n.Name
		if name == "" {
			name = n.ID
		}
		label := "نود " + name
		s.checkLoad(ctx, "node:"+n.ID, label, "پردازنده", n.Host.CPUPercent, a.CPUPercent)
		if n.Host.RAMTotal > 0 {
			s.checkLoad(ctx, "node:"+n.ID, label, "حافظه", 100*float64(n.Host.RAMUsed)/float64(n.Host.RAMTotal), a.RAMPercent)
		}
	}
}

func lastErr(e string) string {
	if e == "" {
		return "."
	}
	if len(e) > 160 {
		e = e[:160] + "…"
	}
	return ": " + e
}
