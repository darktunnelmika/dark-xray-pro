package hubcore

import (
	"context"
	"database/sql"
	"errors"
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

func (s *Store) TgLink(ctx context.Context, chat int64, email string) error {
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

// TgTicket stores a support message and returns its id.
func (s *Store) TgTicket(ctx context.Context, chat int64, text string) (int64, error) {
	r, err := s.db.ExecContext(ctx, `INSERT INTO tg_tickets(chat_id,text,unix) VALUES(?,?,?)`, chat, text, s.now().Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
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
	return s.ordersWhere(ctx, `o.status='pending' AND o.tg_owner_notified=0`)
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

// MarkAllOrdersAnnounced is used once when the bot starts for the first
// time, so it does not replay history.
func (s *Store) MarkAllOrdersAnnounced(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `UPDATE orders SET tg_owner_notified=1, tg_buyer_notified=1`)
}

func (s *Store) ordersWhere(ctx context.Context, where string) ([]Order, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT o.id,o.product_id,COALESCE(p.name,''),o.seller,o.buyer,o.client_email,o.renew,o.amount,o.status,o.note,
o.created_unix,o.done_unix,o.done_by FROM orders o LEFT JOIN products p ON p.id=o.product_id WHERE `+where+` ORDER BY o.id LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.ProductID, &o.ProductName, &o.Seller, &o.Buyer, &o.ClientEmail, &o.Renew, &o.Amount, &o.Status, &o.Note,
			&o.CreatedUnix, &o.DoneUnix, &o.DoneBy); err != nil {
			return nil, err
		}
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
