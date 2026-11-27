package hubcore

import (
	"context"
	"strconv"
	"strings"
)

// A seller's customers (the chats that started its bot) and the ones it
// banned: a banned chat gets nothing from that seller's bot or store.

const tgBansSchema = `CREATE TABLE IF NOT EXISTS tg_bans (
  seller TEXT NOT NULL,
  chat_id INTEGER NOT NULL,
  unix INTEGER NOT NULL,
  PRIMARY KEY (seller, chat_id)
);`

func (s *Store) ensureTgBansSchema() error {
	_, err := s.db.Exec(tgBansSchema)
	return err
}

// TgBanned tells whether the seller banned the chat.
func (s *Store) TgBanned(ctx context.Context, seller string, chat int64) bool {
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tg_bans WHERE seller=? AND chat_id=?`, seller, chat).Scan(&n)
	return n > 0
}

func (s *Store) SetTgBan(ctx context.Context, seller string, chat int64, on bool) error {
	if !on {
		_, err := s.db.ExecContext(ctx, `DELETE FROM tg_bans WHERE seller=? AND chat_id=?`, seller, chat)
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO tg_bans(seller,chat_id,unix) VALUES(?,?,?)`, seller, chat, s.now().Unix())
	return err
}

// Customer is one chat of a seller's bot.
type Customer struct {
	Chat     int64  `json:"chat"`
	Name     string `json:"name"`
	Username string `json:"username"`
	First    int64  `json:"first"`
	Last     int64  `json:"last"`
	Services int    `json:"services"`
	Wallet   int64  `json:"wallet"`
	Banned   bool   `json:"banned"`
	Left     bool   `json:"left"` // blocked the bot
}

// SellerCustomers lists the seller's bot chats, newest activity first; q
// matches a name, a username or a chat id.
func (s *Store) SellerCustomers(ctx context.Context, seller, q string, limit int) ([]Customer, int, error) {
	if limit <= 0 || limit > 300 {
		limit = 200
	}
	q = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(q), "@"))
	like := "%" + q + "%"
	where := `b.seller=?`
	args := []any{seller}
	if q != "" {
		where += ` AND (LOWER(COALESCE(u.name,'')) LIKE ? OR LOWER(COALESCE(u.username,'')) LIKE ? OR CAST(b.chat_id AS TEXT) LIKE ?)`
		args = append(args, like, like, like)
	}
	var total int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tg_bot_users b LEFT JOIN tg_users u ON u.chat_id=b.chat_id WHERE `+where, args...).Scan(&total)
	rows, err := s.ro.QueryContext(ctx, `SELECT b.chat_id,COALESCE(u.name,''),COALESCE(u.username,''),b.first_unix,b.last_unix,b.blocked,
(SELECT COUNT(*) FROM tg_links l JOIN clients c ON c.email=l.email WHERE l.chat_id=b.chat_id AND c.rep_id=b.seller),
COALESCE((SELECT SUM(delta) FROM toman_wallet w WHERE w.seller=b.seller AND w.chat_id=b.chat_id),0),
EXISTS(SELECT 1 FROM tg_bans x WHERE x.seller=b.seller AND x.chat_id=b.chat_id)
FROM tg_bot_users b LEFT JOIN tg_users u ON u.chat_id=b.chat_id WHERE `+where+` ORDER BY b.last_unix DESC LIMIT `+strconv.Itoa(limit), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Customer{}
	for rows.Next() {
		var c Customer
		if err := rows.Scan(&c.Chat, &c.Name, &c.Username, &c.First, &c.Last, &c.Left, &c.Services, &c.Wallet, &c.Banned); err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// IsCustomer tells whether the chat started the seller's bot.
func (s *Store) IsCustomer(ctx context.Context, seller string, chat int64) bool {
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tg_bot_users WHERE seller=? AND chat_id=?`, seller, chat).Scan(&n)
	return n > 0
}
