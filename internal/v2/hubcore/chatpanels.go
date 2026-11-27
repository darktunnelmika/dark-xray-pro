package hubcore

import (
	"context"
	"net/http"
	"strconv"
)

// One Telegram account may run many panels (up to the buyer cap). The first
// stays its main panel (reps.tg_chat / rep_tg_owners: order cards, the bot's
// own owner list); every other panel the account bought is kept here, and
// the account picks which panel the bot and the Mini App work on.

const chatPanelsSchema = `CREATE TABLE IF NOT EXISTS rep_chat_panels (
  chat_id INTEGER NOT NULL,
  rep_id TEXT NOT NULL REFERENCES reps(id) ON DELETE CASCADE,
  added_unix INTEGER NOT NULL,
  PRIMARY KEY(chat_id, rep_id)
);
CREATE INDEX IF NOT EXISTS rep_chat_panels_rep ON rep_chat_panels(rep_id);`

func (s *Store) ensureChatPanelsSchema() error {
	if _, err := s.db.Exec(chatPanelsSchema); err != nil {
		return err
	}
	// Panels bought before this table: the buyer of a panel nobody else is
	// connected to keeps it.
	_, err := s.db.Exec(`INSERT OR IGNORE INTO rep_chat_panels(chat_id,rep_id,added_unix)
SELECT o.chat_id, o.rep_id, o.decided_unix FROM rep_orders o JOIN reps r ON r.id=o.rep_id
WHERE o.kind='new' AND o.status='approved' AND o.chat_id<>0 AND r.tg_chat=0
  AND NOT EXISTS (SELECT 1 FROM rep_tg_owners t WHERE t.rep_id=o.rep_id)`)
	return err
}

// AddChatPanel records another panel of this account.
func (s *Store) AddChatPanel(ctx context.Context, chat int64, rep string) error {
	if chat == 0 || rep == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO rep_chat_panels(chat_id,rep_id,added_unix) VALUES(?,?,?)`, chat, rep, s.now().Unix())
	return err
}

// ChatPanel is one panel an account runs or helps run.
type ChatPanel struct {
	Rep    string `json:"id"`
	Name   string `json:"name"`
	Role   string `json:"role"` // owner | manager | sales | support | custom:…
	Parent string `json:"-"`
}

// tgRepMain is the account's main panel ("" = none).
func (s *Store) tgRepMain(ctx context.Context, chat int64) string {
	var id string
	_ = s.ro.QueryRowContext(ctx, `SELECT id FROM reps WHERE tg_chat=? AND enabled=1`, chat).Scan(&id)
	if id == "" {
		_ = s.ro.QueryRowContext(ctx, `SELECT r.id FROM rep_tg_owners o JOIN reps r ON r.id=o.rep_id WHERE o.chat_id=? AND r.enabled=1`, chat).Scan(&id)
	}
	return id
}

// ChatRoles lists every enabled panel of the account: its main panel, the
// others it bought, then the ones it is a bot admin of.
func (s *Store) ChatRoles(ctx context.Context, chat int64) []ChatPanel {
	if chat == 0 {
		return nil
	}
	var out []ChatPanel
	seen := map[string]bool{}
	add := func(rep, role string) {
		if rep != "" && !seen[rep] {
			seen[rep] = true
			out = append(out, ChatPanel{Rep: rep, Role: role})
		}
	}
	add(s.tgRepMain(ctx, chat), "owner")
	if rows, err := s.ro.QueryContext(ctx, `SELECT c.rep_id FROM rep_chat_panels c JOIN reps r ON r.id=c.rep_id
WHERE c.chat_id=? AND r.enabled=1 ORDER BY c.added_unix, c.rep_id`, chat); err == nil {
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				add(id, "owner")
			}
		}
		rows.Close()
	}
	if rows, err := s.ro.QueryContext(ctx, `SELECT a.rep_id,a.role,a.perms FROM rep_tg_admins a JOIN reps r ON r.id=a.rep_id
WHERE a.chat_id=? AND r.enabled=1 ORDER BY a.added_unix, a.rep_id`, chat); err == nil {
		for rows.Next() {
			var id, role, perms string
			if rows.Scan(&id, &role, &perms) == nil {
				if perms != "" {
					role = "custom:" + perms // ticked rights (CustomCan) instead of the role's
				}
				add(id, role)
			}
		}
		rows.Close()
	}
	for i := range out {
		_ = s.ro.QueryRowContext(ctx, `SELECT name, COALESCE(parent_id,'') FROM reps WHERE id=?`, out[i].Rep).Scan(&out[i].Name, &out[i].Parent)
	}
	return out
}

func activeKey(chat int64) string { return "tg_panel:" + strconv.FormatInt(chat, 10) }

// ActivePanel is the panel the account picked ("" = its main one).
func (s *Store) ActivePanel(ctx context.Context, chat int64) string {
	return s.Setting(ctx, activeKey(chat))
}

// SetActivePanel picks one of the account's panels.
func (s *Store) SetActivePanel(ctx context.Context, chat int64, rep string) error {
	for _, p := range s.ChatRoles(ctx, chat) {
		if p.Rep == rep {
			return s.SetSetting(ctx, activeKey(chat), rep)
		}
	}
	return ErrNotFound
}

// TgRepRoleIn is the panel this account works on where only panels in
// accepts the scope (nil: any): prefer first if it has it, then the picked
// panel, then its panels in order.
func (s *Store) TgRepRoleIn(ctx context.Context, chat int64, prefer string, in func(rep string) bool) (rep, role string) {
	list := s.ChatRoles(ctx, chat)
	if len(list) == 0 {
		return "", ""
	}
	ok := func(p ChatPanel) bool { return in == nil || in(p.Rep) }
	for _, want := range []string{prefer, s.ActivePanel(ctx, chat)} {
		if want == "" {
			continue
		}
		for _, p := range list {
			if p.Rep == want && ok(p) {
				return p.Rep, p.Role
			}
		}
	}
	for _, p := range list {
		if ok(p) {
			return p.Rep, p.Role
		}
	}
	return "", ""
}

// ChatOwnsPanel reports whether the account owns rep (not as a bot admin).
func (s *Store) ChatOwnsPanel(ctx context.Context, chat int64, rep string) bool {
	for _, p := range s.ChatRoles(ctx, chat) {
		if p.Rep == rep {
			return p.Role == "owner"
		}
	}
	return false
}

// chatPanelChats are the accounts that own rep besides its main owners.
func (s *Store) chatPanelChats(ctx context.Context, rep string) []int64 {
	var out []int64
	rows, err := s.ro.QueryContext(ctx, `SELECT chat_id FROM rep_chat_panels WHERE rep_id=? ORDER BY added_unix, chat_id`, rep)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// inBotScope is which panels a bot (seller "" = the main bot) manages: all in
// the main bot; the reseller itself and its sub-sellers in its own bot.
func (s *Store) inBotScope(ctx context.Context, seller string) func(string) bool {
	return func(rep string) bool {
		if seller == "" || rep == seller {
			return true
		}
		var parent string
		return s.ro.QueryRowContext(ctx, `SELECT parent_id FROM reps WHERE id=?`, rep).Scan(&parent) == nil && parent == seller
	}
}

// mgrPanels are the manager's panels this Mini App reaches.
func (s *Store) mgrPanels(ctx context.Context, m miniMgr) []ChatPanel {
	in := s.inBotScope(ctx, m.Bot)
	out := []ChatPanel{}
	for _, p := range s.ChatRoles(ctx, m.Chat) {
		if in(p.Rep) {
			out = append(out, p)
		}
	}
	return out
}

func registerChatPanels(mux *http.ServeMux, s *Store, mgr func(http.ResponseWriter, *http.Request) (miniMgr, bool)) {
	// Which of the account's panels the Mini App and the bot work on.
	mux.HandleFunc("POST /app/api/m/panel", func(w http.ResponseWriter, r *http.Request) {
		m, ok := mgr(w, r)
		if !ok {
			return
		}
		var in struct {
			ID string `json:"id"`
		}
		if !decode(w, r, &in) {
			return
		}
		for _, p := range s.mgrPanels(r.Context(), m) {
			if p.Rep == in.ID {
				reply(w, map[string]bool{"ok": true}, s.SetActivePanel(r.Context(), m.Chat, in.ID))
				return
			}
		}
		http.Error(w, "این پنل در دسترس نیست.", http.StatusForbidden)
	})
}
