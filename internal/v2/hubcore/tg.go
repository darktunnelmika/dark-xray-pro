package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Storage for the Telegram bot: known chats, chat↔client links, support
// tickets, bot settings and which orders were already announced.

const tgSchema = `
CREATE TABLE IF NOT EXISTS tg_users (
  chat_id INTEGER PRIMARY KEY,
  name TEXT NOT NULL DEFAULT '',
  username TEXT NOT NULL DEFAULT '',
  first_unix INTEGER NOT NULL,
  last_unix INTEGER NOT NULL,
  blocked INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS tg_links (
  chat_id INTEGER NOT NULL,
  email TEXT NOT NULL,
  PRIMARY KEY(chat_id, email)
);
CREATE TABLE IF NOT EXISTS tg_tickets (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  chat_id INTEGER NOT NULL,
  text TEXT NOT NULL,
  unix INTEGER NOT NULL,
  answered INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS tg_msgmap (
  owner_chat INTEGER NOT NULL,
  message_id INTEGER NOT NULL,
  ticket_id INTEGER NOT NULL,
  PRIMARY KEY(owner_chat, message_id)
);
CREATE TABLE IF NOT EXISTS settings (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

func (s *Store) ensureTgSchema() error {
	if _, err := s.db.Exec(tgSchema); err != nil {
		return err
	}
	for _, c := range []string{"tg_owner_notified", "tg_buyer_notified"} {
		if err := s.addColumn("orders", c, "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return err
		}
	}
	return nil
}

// Setting reads a key ("" if unset); SetSetting writes it.
func (s *Store) Setting(ctx context.Context, key string) string {
	var v string
	_ = s.ro.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, key).Scan(&v)
	return v
}

func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// TgTouch records a chat that talked to the bot.
func (s *Store) TgTouch(ctx context.Context, chat int64, name, username string) error {
	now := s.now().Unix()
	_, err := s.db.ExecContext(ctx, `INSERT INTO tg_users(chat_id,name,username,first_unix,last_unix) VALUES(?,?,?,?,?)
ON CONFLICT(chat_id) DO UPDATE SET name=excluded.name, username=excluded.username, last_unix=excluded.last_unix, blocked=0`,
		chat, name, username, now, now)
	return err
}

// TgName is the first name a Telegram user last showed the bot ("" = unknown).
func (s *Store) TgName(ctx context.Context, chat int64) string {
	var name string
	_ = s.ro.QueryRowContext(ctx, `SELECT name FROM tg_users WHERE chat_id=?`, chat).Scan(&name)
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return ""
}

// ClientPlanName is the name of the product a client was last bought or
// renewed with ("" = it was not sold through the shop).
func (s *Store) ClientPlanName(ctx context.Context, email string) string {
	var name string
	_ = s.ro.QueryRowContext(ctx, `SELECT p.name FROM orders o JOIN products p ON p.id=o.product_id WHERE o.client_email=? AND o.status='fulfilled' ORDER BY o.id DESC LIMIT 1`, email).Scan(&name)
	return name
}

// BuyerOrder is one line of a customer's purchase history.
type BuyerOrder struct {
	ID     int64
	Name   string
	Amount int64
	Status string
	Unix   int64
	Renew  bool
}

// TgOrdersOf lists a Telegram customer's latest orders in a seller's shop.
func (s *Store) TgOrdersOf(ctx context.Context, chat int64, seller string, limit int) []BuyerOrder {
	rows, err := s.ro.QueryContext(ctx, `SELECT o.id,COALESCE(p.name,''),o.amount,o.status,o.created_unix,o.renew FROM orders o
LEFT JOIN products p ON p.id=o.product_id WHERE o.buyer=? AND o.seller=? ORDER BY o.id DESC LIMIT ?`, TgBuyer(chat), seller, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []BuyerOrder
	for rows.Next() {
		var o BuyerOrder
		if rows.Scan(&o.ID, &o.Name, &o.Amount, &o.Status, &o.Unix, &o.Renew) == nil {
			out = append(out, o)
		}
	}
	return out
}

// ---- who started which bot (for broadcasts) ----

const tgBotUsersSchema = `CREATE TABLE IF NOT EXISTS tg_bot_users (
  seller TEXT NOT NULL,
  chat_id INTEGER NOT NULL,
  first_unix INTEGER NOT NULL,
  last_unix INTEGER NOT NULL,
  blocked INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (seller, chat_id)
);`

// ensureTgBotUsers creates the per-bot list and fills it once: the main bot
// gets everyone known so far (as before), a reseller's bot the Telegram
// accounts tied to that reseller's clients, orders and wallets.
func (s *Store) ensureTgBotUsers() error {
	if _, err := s.db.Exec(tgBotUsersSchema); err != nil {
		return err
	}
	if s.Setting(context.Background(), "tg_bot_users_filled") == "1" {
		return nil
	}
	for _, q := range []string{
		`INSERT OR IGNORE INTO tg_bot_users(seller,chat_id,first_unix,last_unix,blocked) SELECT '',chat_id,first_unix,last_unix,blocked FROM tg_users`,
		`INSERT OR IGNORE INTO tg_bot_users(seller,chat_id,first_unix,last_unix) SELECT c.rep_id,l.chat_id,0,0 FROM tg_links l JOIN clients c ON c.email=l.email WHERE c.rep_id<>'' AND ` + ownerWorld + ` AND l.chat_id<>0`,
		`INSERT OR IGNORE INTO tg_bot_users(seller,chat_id,first_unix,last_unix) SELECT seller,CAST(substr(buyer,4) AS INTEGER),0,0 FROM orders WHERE seller<>'' AND buyer LIKE 'tg:%' AND CAST(substr(buyer,4) AS INTEGER)<>0`,
		`INSERT OR IGNORE INTO tg_bot_users(seller,chat_id,first_unix,last_unix) SELECT DISTINCT seller,chat_id,0,0 FROM toman_wallet WHERE seller<>'' AND chat_id<>0`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return s.SetSetting(context.Background(), "tg_bot_users_filled", "1")
}

// TgTouchBot notes that a chat wrote to a seller's bot ("" = the main bot).
func (s *Store) TgTouchBot(ctx context.Context, seller string, chat int64) {
	now := s.now().Unix()
	_, _ = s.db.ExecContext(ctx, `INSERT INTO tg_bot_users(seller,chat_id,first_unix,last_unix) VALUES(?,?,?,?)
ON CONFLICT(seller,chat_id) DO UPDATE SET last_unix=excluded.last_unix, blocked=0`, seller, chat, now, now)
}

// TgBotChats lists the chats of one bot that can receive a broadcast.
func (s *Store) TgBotChats(ctx context.Context, seller string) []int64 {
	rows, err := s.ro.QueryContext(ctx, `SELECT chat_id FROM tg_bot_users WHERE seller=? AND blocked=0 ORDER BY chat_id`, seller)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// TgSetBlockedBot marks a chat that blocked one bot so its broadcasts skip it.
func (s *Store) TgSetBlockedBot(ctx context.Context, seller string, chat int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE tg_bot_users SET blocked=1 WHERE seller=? AND chat_id=?`, seller, chat)
}

// TgAllRepChats lists the Telegram chats of every enabled reseller (the
// owner's audience "resellers").
func (s *Store) TgAllRepChats(ctx context.Context) []int64 {
	rows, err := s.ro.QueryContext(ctx, `SELECT tg_chat FROM reps WHERE enabled=1 AND tg_chat<>0
UNION SELECT o.chat_id FROM rep_tg_owners o JOIN reps r ON r.id=o.rep_id WHERE r.enabled=1 ORDER BY 1`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out
}

// TgSetBlocked marks a chat that blocked the bot so broadcasts skip it.
func (s *Store) TgSetBlocked(ctx context.Context, chat int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE tg_users SET blocked=1 WHERE chat_id=?`, chat)
}

// TgChats lists chats that can receive a broadcast.
func (s *Store) TgChats(ctx context.Context) ([]int64, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT chat_id FROM tg_users WHERE blocked=0 ORDER BY chat_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var c int64
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// TgLinkByToken links a chat to the client owning a subscription token (or a
// full subscription URL).
func (s *Store) TgLinkByToken(ctx context.Context, chat int64, tokenOrURL string) (string, error) {
	tok := strings.TrimSpace(tokenOrURL)
	if i := strings.LastIndex(tok, "/sub/"); i >= 0 {
		tok = tok[i+5:]
	}
	tok = strings.SplitN(tok, "?", 2)[0]
	email, err := s.EmailBySubToken(ctx, tok)
	if err != nil {
		return "", err
	}
	return email, s.TgLink(ctx, chat, email)
}

// ClientTelegram lists the Telegram chats a client is linked to.
func (s *Store) ClientTelegram(ctx context.Context, email string) ([]int64, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT chat_id FROM tg_links WHERE email=? ORDER BY chat_id`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// SetClientTelegram links a client to one Telegram user (expiry notices,
// "my services" in the bot and the store); 0 removes every link.
func (s *Store) SetClientTelegram(ctx context.Context, email string, chat int64) error {
	var n int
	if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients WHERE email=?`, email).Scan(&n); err != nil || n == 0 {
		return ErrNotFound
	}
	if chat < 0 {
		return ErrInvalid
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM tg_links WHERE email=?`, email); err != nil {
		return err
	}
	if chat == 0 {
		return nil
	}
	return s.TgLink(ctx, chat, email)
}

func (s *Store) TgLink(ctx context.Context, chat int64, email string) error {
	if s.TenantOfClient(ctx, email) != "" {
		return ErrNotFound // a connected shop's customers are not the panel bots' to reach
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO tg_links(chat_id,email) VALUES(?,?)`, chat, email)
	return err
}

// TgClients lists the clients a chat owns: linked ones plus those delivered
// from its bot orders.
func (s *Store) TgClients(ctx context.Context, chat int64) ([]string, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT email FROM tg_links WHERE chat_id=?
UNION SELECT client_email FROM orders WHERE buyer=? AND status='fulfilled' AND client_email<>''`, chat, TgBuyer(chat))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		var x int
		if s.ro.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=?`, e).Scan(&x) == nil {
			out = append(out, e)
		}
	}
	return out, rows.Err()
}

// TgBuyer is how bot orders name their buyer.
func TgBuyer(chat int64) string { return "tg:" + strconv.FormatInt(chat, 10) }

// TgChatOfBuyer parses a TgBuyer value.
func TgChatOfBuyer(b string) (int64, bool) {
	if !strings.HasPrefix(b, "tg:") {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.SplitN(b[3:], " ", 2)[0], 10, 64)
	return n, err == nil
}

// TgMapMessage remembers which ticket an owner-side message belongs to, so a
// reply to it reaches the customer.
func (s *Store) TgMapMessage(ctx context.Context, ownerChat, messageID, ticket int64) {
	_, _ = s.db.ExecContext(ctx, `INSERT OR REPLACE INTO tg_msgmap(owner_chat,message_id,ticket_id) VALUES(?,?,?)`, ownerChat, messageID, ticket)
}

// TgTicketChat resolves an owner-side message to the customer chat.
func (s *Store) TgTicketChat(ctx context.Context, ownerChat, messageID int64) (ticket, chat int64, err error) {
	err = s.ro.QueryRowContext(ctx, `SELECT t.id,t.chat_id FROM tg_msgmap m JOIN tg_tickets t ON t.id=m.ticket_id WHERE m.owner_chat=? AND m.message_id=?`,
		ownerChat, messageID).Scan(&ticket, &chat)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	if err == nil {
		_, _ = s.db.ExecContext(ctx, `UPDATE tg_tickets SET answered=1 WHERE id=?`, ticket)
	}
	return
}

// OrdersToAnnounce returns pending orders the owners were not told about yet.
func (s *Store) OrdersToAnnounce(ctx context.Context) ([]Order, error) {
	// Stars invoices are paid and delivered automatically: nothing to approve.
	return s.ordersWhere(ctx, `o.status='pending' AND o.tg_owner_notified=0 AND o.method='manual'`)
}

// DeliveriesToAnnounce returns fulfilled bot orders whose buyer was not told.
func (s *Store) DeliveriesToAnnounce(ctx context.Context) ([]Order, error) {
	return s.ordersWhere(ctx, `o.status IN ('fulfilled','cancelled') AND o.tg_buyer_notified=0 AND o.buyer LIKE 'tg:%'`)
}

func (s *Store) MarkOrderAnnounced(ctx context.Context, id int64, buyer bool) {
	col := "tg_owner_notified"
	if buyer {
		col = "tg_buyer_notified"
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE orders SET `+col+`=1 WHERE id=?`, id)
}

// ClaimOrderAnnounce marks an order as told (to the admins, or with buyer
// to the buyer) and reports whether this call did it: of two bots or two
// runs, only the one that gets true sends the message.
func (s *Store) ClaimOrderAnnounce(ctx context.Context, id int64, buyer bool) bool {
	col := "tg_owner_notified"
	if buyer {
		col = "tg_buyer_notified"
	}
	r, err := s.db.ExecContext(ctx, `UPDATE orders SET `+col+`=1 WHERE id=? AND `+col+`=0`, id)
	if err != nil {
		return false
	}
	n, _ := r.RowsAffected()
	return n == 1
}

// MarkAllOrdersAnnounced is used once when the bot starts for the first
// time, so it does not replay history.
func (s *Store) MarkAllOrdersAnnounced(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE orders SET tg_owner_notified=1, tg_buyer_notified=1`)
}

func (s *Store) ordersWhere(ctx context.Context, where string) ([]Order, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT o.id,o.product_id,COALESCE(p.name,''),o.seller,o.buyer,o.client_email,o.renew,o.amount,o.status,o.note,
o.created_unix,o.done_unix,o.done_by,o.method,o.stars,o.discount_code,o.discount,o.receipt,o.receipt_unix FROM orders o LEFT JOIN products p ON p.id=o.product_id WHERE `+where+` ORDER BY o.id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.ProductID, &o.ProductName, &o.Seller, &o.Buyer, &o.ClientEmail, &o.Renew, &o.Amount, &o.Status, &o.Note,
			&o.CreatedUnix, &o.DoneUnix, &o.DoneBy, &o.Method, &o.Stars, &o.DiscountCode, &o.Discount, &o.Receipt, &o.ReceiptUnix); err != nil {
			return nil, err
		}
		o.HasReceipt = o.Receipt != ""
		out = append(out, o)
	}
	return out, rows.Err()
}

// ClientByEmail returns one client with usage, or ErrNotFound.
func (s *Store) ClientByEmail(ctx context.Context, email string) (ClientView, error) {
	all, err := s.Clients(ctx)
	if err != nil {
		return ClientView{}, err
	}
	for _, c := range all {
		if c.Email == email {
			return c, nil
		}
	}
	return ClientView{}, ErrNotFound
}

// Representatives connect their Telegram with a one-time code from the
// panel; the bot then sends them their own order cards and balances.

func (s *Store) ensureTgRepSchema() error {
	if err := s.addColumn("reps", "tg_chat", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	// More Telegram accounts that are this reseller in the bot, on a par with
	// reps.tg_chat (partners who share one panel). One account, one panel.
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS rep_tg_owners (
  chat_id INTEGER PRIMARY KEY,
  rep_id TEXT NOT NULL REFERENCES reps(id) ON DELETE CASCADE,
  added_unix INTEGER NOT NULL
)`)
	return err
}

// MaxTgRepOwners is how many Telegram accounts one panel takes.
const MaxTgRepOwners = 10

// TgRepOwners lists the Telegram accounts that are a reseller in the bot.
func (s *Store) TgRepOwners(ctx context.Context, repID string) []int64 {
	out := []int64{}
	if c := s.TgRepChat(ctx, repID); c != 0 {
		out = append(out, c)
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT chat_id FROM rep_tg_owners WHERE rep_id=? ORDER BY added_unix, chat_id`, repID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil && (len(out) == 0 || c != out[0]) {
			out = append(out, c)
		}
	}
	return out
}

// SetTgRepOwners replaces a reseller's Telegram accounts. An account given
// here stops being any other reseller's.
func (s *Store) SetTgRepOwners(ctx context.Context, repID string, ids []int64) error {
	seen := map[int64]bool{}
	var list []int64
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			list = append(list, id)
		}
	}
	if len(list) > MaxTgRepOwners {
		return fmt.Errorf("%w: at most %d Telegram ids", ErrInvalid, MaxTgRepOwners)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, repID).Scan(&n); err != nil || n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM rep_tg_owners WHERE rep_id=?`, repID); err != nil {
		return err
	}
	first := int64(0)
	now := s.now().Unix()
	for i, id := range list {
		if _, err := tx.ExecContext(ctx, `UPDATE reps SET tg_chat=0 WHERE tg_chat=? AND id<>?`, id, repID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM rep_tg_owners WHERE chat_id=?`, id); err != nil {
			return err
		}
		if i == 0 {
			first = id
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO rep_tg_owners(chat_id,rep_id,added_unix) VALUES(?,?,?)`, id, repID, now+int64(i)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE reps SET tg_chat=? WHERE id=?`, first, repID); err != nil {
		return err
	}
	return tx.Commit()
}

// NewTgLinkCode returns a code valid for 15 minutes.
func (s *Store) NewTgLinkCode(ctx context.Context, repID string) (string, error) {
	return s.newTgLinkCode(ctx, repID, 900)
}

func (s *Store) newTgLinkCode(ctx context.Context, repID string, ttl int64) (string, error) {
	code := randomHex(8)
	exp := s.now().Unix() + ttl
	return code, s.SetSetting(ctx, "tglink:"+code, repID+"|"+strconv.FormatInt(exp, 10))
}

// ClaimTgLinkCode binds chat to the representative that owns the code.
func (s *Store) ClaimTgLinkCode(ctx context.Context, code string, chat int64) (string, error) {
	v := s.Setting(ctx, "tglink:"+code)
	rep, expS, ok := strings.Cut(v, "|")
	exp, _ := strconv.ParseInt(expS, 10, 64)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, "tglink:"+code)
	if !ok || exp < s.now().Unix() {
		return "", ErrNotFound
	}
	// An account that already runs another panel keeps it as its main one
	// and gets this one beside it, picked (chatpanels.go).
	if main := s.tgRepMain(ctx, chat); main != "" && main != rep {
		if err := s.AddChatPanel(ctx, chat, rep); err != nil {
			return "", err
		}
		return rep, s.SetSetting(ctx, activeKey(chat), rep)
	}
	// A panel that already has a Telegram account keeps it: this one joins it.
	owners := s.TgRepOwners(ctx, rep)
	for _, o := range owners {
		if o == chat {
			return rep, nil
		}
	}
	if len(owners) >= MaxTgRepOwners {
		return "", fmt.Errorf("%w: this panel already has %d Telegram accounts", ErrInvalid, MaxTgRepOwners)
	}
	if err := s.SetTgRepOwners(ctx, rep, append(owners, chat)); err != nil {
		return "", err
	}
	return rep, nil
}

// TgRep returns the representative bound to a chat ("" if none).
func (s *Store) TgRep(ctx context.Context, chat int64) string {
	list := s.ChatRoles(ctx, chat)
	if a := s.ActivePanel(ctx, chat); a != "" {
		for _, p := range list {
			if p.Rep == a && p.Role == "owner" {
				return a
			}
		}
	}
	for _, p := range list {
		if p.Role == "owner" {
			return p.Rep
		}
	}
	return ""
}

// TgRepChat returns a representative's chat (0 if not connected).
func (s *Store) TgRepChat(ctx context.Context, repID string) int64 {
	var c int64
	_ = s.ro.QueryRowContext(ctx, `SELECT tg_chat FROM reps WHERE id=?`, repID).Scan(&c)
	return c
}

// TgLinkedChats maps each client to the chats that own it (links and bot
// orders), for notifications.
func (s *Store) TgLinkedChats(ctx context.Context) (map[string][]int64, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT email,chat_id FROM tg_links
UNION SELECT client_email, CAST(substr(buyer,4) AS INTEGER) FROM orders WHERE buyer LIKE 'tg:%' AND status='fulfilled' AND client_email<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]int64{}
	for rows.Next() {
		var e string
		var c int64
		if err := rows.Scan(&e, &c); err != nil {
			return nil, err
		}
		if c != 0 {
			out[e] = append(out[e], c)
		}
	}
	return out, rows.Err()
}

// Reseller bot admins: a representative invites helpers to its own scope.
// Roles: manager (everything the reseller can do in the bot), sales
// (orders and clients) and support (look up clients). Nobody gets more
// than the reseller itself, and only the reseller manages the list.

func (s *Store) ensureTgAdminSchema() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS rep_tg_admins (
  rep_id TEXT NOT NULL REFERENCES reps(id) ON DELETE CASCADE,
  chat_id INTEGER NOT NULL,
  role TEXT NOT NULL CHECK(role IN ('manager','sales','support')),
  name TEXT NOT NULL DEFAULT '',
  added_unix INTEGER NOT NULL,
  PRIMARY KEY(rep_id, chat_id)
)`)
	if err != nil {
		return err
	}
	// perms: a helper's own ticked rights (comma list); "" = its role's.
	return s.addColumn("rep_tg_admins", "perms", "TEXT NOT NULL DEFAULT ''")
}

// AdminPerms are the rights a helper can be given one by one.
var AdminPerms = []string{"view", "link", "new", "renew", "edit", "delete", "orders", "credit", "tickets", "plans", "cards", "settings", "reports", "sellers"}

// SetTgRepAdminPerms ticks a helper's rights; none = back to its role's.
func (s *Store) SetTgRepAdminPerms(ctx context.Context, repID string, chat int64, perms []string) error {
	ok := map[string]bool{}
	for _, p := range AdminPerms {
		ok[p] = true
	}
	var keep []string
	seen := map[string]bool{}
	for _, p := range perms {
		if ok[p] && !seen[p] {
			seen[p] = true
			keep = append(keep, p)
		}
	}
	r, err := s.db.ExecContext(ctx, `UPDATE rep_tg_admins SET perms=? WHERE rep_id=? AND chat_id=?`, strings.Join(keep, ","), repID, chat)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TgRepAdmin is one helper of a reseller.
type TgRepAdmin struct {
	ChatID int64
	Role   string
	Name   string
	Perms  []string // ticked rights; empty = the role's
}

func (s *Store) NewTgAdminInvite(ctx context.Context, repID, role string) (string, error) {
	if role != "manager" && role != "sales" && role != "support" {
		return "", ErrInvalid
	}
	code := randomHex(8)
	exp := s.now().Unix() + 3600
	return code, s.SetSetting(ctx, "tgadm:"+code, repID+"|"+role+"|"+strconv.FormatInt(exp, 10))
}

func (s *Store) ClaimTgAdminInvite(ctx context.Context, code string, chat int64, name string) (rep, role string, err error) {
	v := s.Setting(ctx, "tgadm:"+code)
	_, _ = s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, "tgadm:"+code)
	parts := strings.Split(v, "|")
	if len(parts) != 3 {
		return "", "", ErrNotFound
	}
	exp, _ := strconv.ParseInt(parts[2], 10, 64)
	if exp < s.now().Unix() {
		return "", "", ErrNotFound
	}
	rep, role = parts[0], parts[1]
	_, err = s.db.ExecContext(ctx, `INSERT INTO rep_tg_admins(rep_id,chat_id,role,name,added_unix) VALUES(?,?,?,?,?)
ON CONFLICT(rep_id,chat_id) DO UPDATE SET role=excluded.role, name=excluded.name`, rep, chat, role, name, s.now().Unix())
	return rep, role, err
}

func (s *Store) TgRepAdmins(ctx context.Context, repID string) ([]TgRepAdmin, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT chat_id,role,name,perms FROM rep_tg_admins WHERE rep_id=? ORDER BY added_unix`, repID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TgRepAdmin
	for rows.Next() {
		var a TgRepAdmin
		var perms string
		if err := rows.Scan(&a.ChatID, &a.Role, &a.Name, &perms); err != nil {
			return nil, err
		}
		if perms != "" {
			a.Perms = strings.Split(perms, ",")
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) RemoveTgRepAdmin(ctx context.Context, repID string, chat int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rep_tg_admins WHERE rep_id=? AND chat_id=?`, repID, chat)
	return err
}

// CustomCan reads a "custom:a,b,c" role: does it give what?
func CustomCan(role, what string) (can, custom bool) {
	list, ok := strings.CutPrefix(role, "custom:")
	if !ok {
		return false, false
	}
	for _, p := range strings.Split(list, ",") {
		if p == what || (what == "shop" && p == "settings") {
			return true, true
		}
	}
	return false, true
}

// TgRepRole returns the reseller a chat works for and its role: "owner" for
// the reseller's own chat, else the helper role ("" if none).
func (s *Store) TgRepRole(ctx context.Context, chat int64) (rep, role string) {
	return s.TgRepRoleIn(ctx, chat, "", nil)
}

// TgRepChats lists every chat that should see a reseller's order cards.
func (s *Store) TgRepChats(ctx context.Context, repID string) []int64 {
	out := append(s.TgRepOwners(ctx, repID), s.chatPanelChats(ctx, repID)...)
	rows, err := s.ro.QueryContext(ctx, `SELECT chat_id FROM rep_tg_admins WHERE rep_id=? AND role IN ('manager','sales')`, repID)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var c int64
		if rows.Scan(&c) == nil {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// ---- required channel membership ----

// JoinPolicy makes a bot's customers join channels before they can buy,
// renew or try. Each seller sets their own ("" = the owner); off by default.
type JoinPolicy struct {
	Enabled  bool     `json:"enabled"`
	Channels []string `json:"channels"` // public usernames, "@name"
}

func joinKey(seller string) string {
	if seller == "" {
		return "tg_join"
	}
	return "tg_join:" + seller
}

func (s *Store) JoinPolicy(ctx context.Context, seller string) JoinPolicy {
	var p JoinPolicy
	_ = json.Unmarshal([]byte(s.Setting(ctx, joinKey(seller))), &p)
	return p
}

// NormalChannel turns "@name", "name" or a t.me link into "@name"; ok is
// false for anything that is not a public channel username.
func NormalChannel(v string) (string, bool) {
	v = strings.TrimSpace(v)
	for _, p := range []string{"https://t.me/", "http://t.me/", "t.me/", "@"} {
		v = strings.TrimPrefix(v, p)
	}
	v = strings.TrimSuffix(v, "/")
	if len(v) < 4 || len(v) > 32 {
		return "", false
	}
	for _, r := range v {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return "", false
		}
	}
	return "@" + v, true
}

func (s *Store) PutJoinPolicy(ctx context.Context, seller string, p JoinPolicy) error {
	seen := map[string]bool{}
	var out []string
	for _, c := range p.Channels {
		n, ok := NormalChannel(c)
		if !ok {
			return fmt.Errorf("%w: a channel is a public username like @name", ErrInvalid)
		}
		if k := strings.ToLower(n); !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	if len(out) > 3 {
		return fmt.Errorf("%w: at most three channels", ErrInvalid)
	}
	if p.Enabled && len(out) == 0 {
		return fmt.Errorf("%w: add a channel first", ErrInvalid)
	}
	p.Channels = out
	b, _ := json.Marshal(p)
	return s.SetSetting(ctx, joinKey(seller), string(b))
}
