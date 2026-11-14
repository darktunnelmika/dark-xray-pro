package hubcore

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The Telegram bot lives in package tgbot, which imports this one; the
// binary hands the HUB a controller so the panel can connect, change and
// test the bot without a restart. The token is kept in its own file on the
// HUB (never in the database, so never in backups).
type TelegramControl struct {
	// Apply checks the token with Telegram, stores it and (re)starts the
	// bot with these owners. token "" keeps the current one, "-" turns the
	// bot off. It returns the bot's @username.
	Apply func(ctx context.Context, token string, owners []int64) (string, error)
	// Running tells whether a bot is connected now.
	Running func() bool
	// BackupNow sends an encrypted backup to every owner.
	BackupNow func(ctx context.Context) error
	// Test sends a short message to every owner.
	Test func(ctx context.Context) error
}

var (
	tgCtlMu sync.Mutex
	tgCtl   TelegramControl
)

var tgDefaultOwners []int64

// SetTelegramDefaultOwners sets the owners given on the command line, used
// until owners are saved from the panel.
func SetTelegramDefaultOwners(ids []int64) { tgDefaultOwners = ids }

// SetTelegramControl installs the binary's bot controller.
func SetTelegramControl(c TelegramControl) {
	tgCtlMu.Lock()
	tgCtl = c
	tgCtlMu.Unlock()
}

func telegramControl() TelegramControl {
	tgCtlMu.Lock()
	defer tgCtlMu.Unlock()
	return tgCtl
}

// TelegramOwners returns the owner ids saved from the panel (nil = use the
// ones given on the command line).
func (s *Store) TelegramOwners(ctx context.Context) []int64 {
	var out []int64
	for _, f := range strings.Split(s.Setting(ctx, "tg_owners"), ",") {
		if n, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64); err == nil && n > 0 {
			out = append(out, n)
		}
	}
	return out
}

// TelegramStatus is what the panel shows about the bot.
type TelegramStatus struct {
	Available  bool    `json:"available"` // this binary can run a bot
	Running    bool    `json:"running"`
	TokenSet   bool    `json:"token_set"`
	TokenHint  string  `json:"token_hint,omitempty"` // bot id part only, e.g. 7012345678:…
	Username   string  `json:"username,omitempty"`
	Owners     []int64 `json:"owners"`
	BackupPass bool    `json:"backup_pass"`
	BackupHrs  int     `json:"backup_hours"`
	BackupLast int64   `json:"backup_last"`
	BackupErr  string  `json:"backup_error,omitempty"`
}

// BackupHours is how often the bot sends an encrypted backup (default
// daily; 0 = never by itself).
func (s *Store) BackupHours(ctx context.Context) int {
	v := s.Setting(ctx, "tg_backup_hours")
	if v == "" {
		return 24
	}
	n, _ := strconv.Atoi(v)
	return n
}

func (s *Store) telegramStatus(ctx context.Context, owners []int64) TelegramStatus {
	c := telegramControl()
	st := TelegramStatus{Available: c.Apply != nil, Owners: owners, Username: s.Setting(ctx, "tg_username"),
		BackupPass: s.Setting(ctx, "tg_backup_pass") != "", BackupHrs: s.BackupHours(ctx), BackupErr: s.Setting(ctx, "tg_backup_error")}
	st.BackupLast, _ = strconv.ParseInt(s.Setting(ctx, "tg_backup_last"), 10, 64)
	if c.Running != nil {
		st.Running = c.Running()
	}
	if tgToken != "" {
		st.TokenSet = true
		if id, _, ok := strings.Cut(tgToken, ":"); ok {
			st.TokenHint = id + ":…"
		}
	}
	if st.Owners == nil {
		st.Owners = []int64{}
	}
	return st
}

func telegramRoutes(s *Store, mux *docMux, guard func(http.HandlerFunc) http.HandlerFunc, defaultOwners func() []int64) {
	owners := func(ctx context.Context) []int64 {
		if o := s.TelegramOwners(ctx); len(o) > 0 {
			return o
		}
		return defaultOwners()
	}
	mux.HandleFunc("GET /api/v2/admin/telegram", guard(func(w http.ResponseWriter, r *http.Request) {
		reply(w, s.telegramStatus(r.Context(), owners(r.Context())), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/telegram", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Token  string  `json:"token"`
			Owners []int64 `json:"owners"`
		}
		if !decode(w, r, &in) {
			return
		}
		c := telegramControl()
		if c.Apply == nil {
			reply(w, nil, fmt.Errorf("%w: this HUB was started without the Telegram bot", ErrInvalid))
			return
		}
		seen := map[int64]bool{}
		var list []string
		var ids []int64
		for _, id := range in.Owners {
			if id > 0 && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			list = append(list, strconv.FormatInt(id, 10))
		}
		if len(ids) == 0 && in.Token != "-" {
			reply(w, nil, fmt.Errorf("%w: add at least one owner (numeric Telegram id)", ErrInvalid))
			return
		}
		name, err := c.Apply(r.Context(), strings.TrimSpace(in.Token), ids)
		if err != nil {
			reply(w, nil, err)
			return
		}
		_ = s.SetSetting(r.Context(), "tg_owners", strings.Join(list, ","))
		if name != "" {
			_ = s.SetSetting(r.Context(), "tg_username", name)
		}
		reply(w, s.telegramStatus(r.Context(), owners(r.Context())), nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/telegram/backup", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Pass  string `json:"pass"` // "" keeps, "-" removes
			Hours int    `json:"hours"`
		}
		if !decode(w, r, &in) {
			return
		}
		switch {
		case in.Hours < 0 || in.Hours > 24*7:
			reply(w, nil, fmt.Errorf("%w: hours must be 0–168", ErrInvalid))
			return
		case in.Pass == "-":
			_ = s.SetSetting(r.Context(), "tg_backup_pass", "")
		case in.Pass != "":
			if len(in.Pass) < 8 {
				reply(w, nil, fmt.Errorf("%w: the backup password needs at least 8 characters", ErrInvalid))
				return
			}
			_ = s.SetSetting(r.Context(), "tg_backup_pass", in.Pass)
		}
		err := s.SetSetting(r.Context(), "tg_backup_hours", strconv.Itoa(in.Hours))
		reply(w, s.telegramStatus(r.Context(), owners(r.Context())), err)
	}))
	mux.HandleFunc("GET /api/v2/admin/telegram/notices", guard(func(w http.ResponseWriter, r *http.Request) {
		pct, _ := strconv.Atoi(s.Setting(r.Context(), "notify_percent"))
		days, _ := strconv.Atoi(s.Setting(r.Context(), "notify_days"))
		if pct == 0 {
			pct = 90
		}
		if days == 0 {
			days = 1
		}
		reply(w, map[string]int{"percent": pct, "days": days}, nil)
	}))
	mux.HandleFunc("PUT /api/v2/admin/telegram/notices", guard(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Percent int `json:"percent"`
			Days    int `json:"days"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.Percent < 50 || in.Percent > 99 || in.Days < 1 || in.Days > 14 {
			reply(w, nil, fmt.Errorf("%w: percent 50–99, days 1–14", ErrInvalid))
			return
		}
		_ = s.SetSetting(r.Context(), "notify_percent", strconv.Itoa(in.Percent))
		err := s.SetSetting(r.Context(), "notify_days", strconv.Itoa(in.Days))
		reply(w, in, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/telegram/backup-now", guard(func(w http.ResponseWriter, r *http.Request) {
		c := telegramControl()
		if c.BackupNow == nil || (c.Running != nil && !c.Running()) {
			reply(w, nil, fmt.Errorf("%w: connect the Telegram bot first", ErrInvalid))
			return
		}
		if s.Setting(r.Context(), "tg_backup_pass") == "" {
			reply(w, nil, fmt.Errorf("%w: set a backup password first", ErrInvalid))
			return
		}
		err := c.BackupNow(r.Context())
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
	mux.HandleFunc("POST /api/v2/admin/telegram/test", guard(func(w http.ResponseWriter, r *http.Request) {
		c := telegramControl()
		if c.Test == nil || (c.Running != nil && !c.Running()) {
			reply(w, nil, fmt.Errorf("%w: connect the Telegram bot first", ErrInvalid))
			return
		}
		err := c.Test(r.Context())
		reply(w, map[string]bool{"ok": err == nil}, err)
	}))
}
