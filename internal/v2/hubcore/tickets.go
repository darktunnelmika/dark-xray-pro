package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Support tickets. A customer's message in a bot opens a ticket for that
// bot's seller ("" = the owner) or joins the customer's open one. Staff
// answer from Telegram or from the panel; a panel answer waits in an
// outbox until the seller's bot delivers it.

const ticketSchema = `
CREATE TABLE IF NOT EXISTS tg_ticket_msgs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ticket_id INTEGER NOT NULL,
  staff INTEGER NOT NULL DEFAULT 0,
  author TEXT NOT NULL DEFAULT '',
  text TEXT NOT NULL,
  unix INTEGER NOT NULL,
  delivered INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS tg_ticket_msgs_t ON tg_ticket_msgs(ticket_id, id);
CREATE INDEX IF NOT EXISTS tg_ticket_msgs_out ON tg_ticket_msgs(delivered) WHERE delivered=0;
`

func (s *Store) ensureTicketSchema() error {
	for _, c := range [][3]string{{"tg_tickets", "seller", "TEXT NOT NULL DEFAULT ''"}, {"tg_tickets", "status", "TEXT NOT NULL DEFAULT 'open'"},
		{"tg_tickets", "updated_unix", "INTEGER NOT NULL DEFAULT 0"}, {"tg_tickets", "name", "TEXT NOT NULL DEFAULT ''"},
		{"tg_tickets", "close_notice", "INTEGER NOT NULL DEFAULT 0"}} {
		if err := s.addColumn(c[0], c[1], c[2]); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(ticketSchema); err != nil {
		return err
	}
	// Tickets from before threads: their text becomes the first message;
	// answered or older than a week ones start closed.
	_, err := s.db.Exec(`INSERT INTO tg_ticket_msgs(ticket_id,staff,text,unix) SELECT t.id,0,t.text,t.unix FROM tg_tickets t
WHERE NOT EXISTS (SELECT 1 FROM tg_ticket_msgs m WHERE m.ticket_id=t.id);
UPDATE tg_tickets SET updated_unix=unix, status=CASE WHEN answered=1 OR unix<strftime('%s','now')-7*86400 THEN 'closed' ELSE 'open' END WHERE updated_unix=0`)
	return err
}

// Ticket is one conversation.
type Ticket struct {
	ID      int64       `json:"id"`
	Seller  string      `json:"seller"`
	Chat    int64       `json:"chat"`
	Name    string      `json:"name"`
	Status  string      `json:"status"` // open | answered | closed
	Created int64       `json:"created"`
	Updated int64       `json:"updated"`
	Last    string      `json:"last"`
	Count   int         `json:"count"`
	Clients []string    `json:"clients,omitempty"`
	Msgs    []TicketMsg `json:"msgs,omitempty"`
}

// TicketMsg is one message in a ticket.
type TicketMsg struct {
	ID        int64  `json:"id"`
	Staff     bool   `json:"staff"`
	Author    string `json:"author"`
	Text      string `json:"text"`
	Unix      int64  `json:"unix"`
	Delivered bool   `json:"delivered"`
}

// TicketOut is a panel answer a bot still has to send.
type TicketOut struct {
	MsgID, Ticket, Chat int64
	Text                string
	Close               bool // a "closed" notice instead of a message
}

func canTicket(by Principal, seller string) bool {
	return by.Role == "owner" || (by.Role == "rep" && by.RepID != "" && by.RepID == seller)
}

// CustomerTicket adds a customer's message to their open ticket with this
// seller, or opens one. It reports the ticket and whether it is new.
func (s *Store) CustomerTicket(ctx context.Context, seller string, chat int64, name, text string) (int64, bool, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > 4000 {
		return 0, false, ErrInvalid
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	var id int64
	isNew := false
	err = tx.QueryRowContext(ctx, `SELECT id FROM tg_tickets WHERE chat_id=? AND seller=? AND status<>'closed' ORDER BY id DESC LIMIT 1`, chat, seller).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		r, err := tx.ExecContext(ctx, `INSERT INTO tg_tickets(chat_id,text,unix,seller,status,updated_unix,name) VALUES(?,?,?,?,'open',?,?)`, chat, text, now, seller, now, name)
		if err != nil {
			return 0, false, err
		}
		id, _ = r.LastInsertId()
		isNew = true
	case err != nil:
		return 0, false, err
	default:
		if _, err := tx.ExecContext(ctx, `UPDATE tg_tickets SET status='open', updated_unix=?, name=CASE WHEN ?<>'' THEN ? ELSE name END WHERE id=?`, now, name, name, id); err != nil {
			return 0, false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO tg_ticket_msgs(ticket_id,staff,text,unix) VALUES(?,0,?,?)`, id, text, now); err != nil {
		return 0, false, err
	}
	return id, isNew, tx.Commit()
}

// TgTicket keeps the old call: a message to the owner's support.
func (s *Store) TgTicket(ctx context.Context, chat int64, text string) (int64, error) {
	id, _, err := s.CustomerTicket(ctx, "", chat, "", text)
	return id, err
}

// Tickets lists what by may see, newest activity first; status "" = all,
// "active" = open and answered.
func (s *Store) Tickets(ctx context.Context, by Principal, status string, limit int) ([]Ticket, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT t.id,t.seller,t.chat_id,t.name,t.status,t.unix,t.updated_unix,
COALESCE((SELECT text FROM tg_ticket_msgs m WHERE m.ticket_id=t.id ORDER BY m.id DESC LIMIT 1),t.text),
(SELECT COUNT(*) FROM tg_ticket_msgs m WHERE m.ticket_id=t.id)
FROM tg_tickets t WHERE (?='owner' OR t.seller=?) AND (?='' OR t.status=? OR (?='active' AND t.status<>'closed'))
ORDER BY t.updated_unix DESC, t.id DESC LIMIT ?`, by.Role, by.RepID, status, status, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ticket{}
	for rows.Next() {
		var t Ticket
		if err := rows.Scan(&t.ID, &t.Seller, &t.Chat, &t.Name, &t.Status, &t.Created, &t.Updated, &t.Last, &t.Count); err != nil {
			return nil, err
		}
		if by.Role == "rep" && t.Seller == "" {
			continue
		}
		if r := []rune(t.Last); len(r) > 140 {
			t.Last = string(r[:140]) + "…"
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TicketByID returns one ticket with its messages and the customer's services.
func (s *Store) TicketByID(ctx context.Context, by Principal, id int64) (Ticket, error) {
	var t Ticket
	err := s.ro.QueryRowContext(ctx, `SELECT id,seller,chat_id,name,status,unix,updated_unix FROM tg_tickets WHERE id=?`, id).
		Scan(&t.ID, &t.Seller, &t.Chat, &t.Name, &t.Status, &t.Created, &t.Updated)
	if err != nil || !canTicket(by, t.Seller) {
		return Ticket{}, ErrNotFound
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT id,staff,author,text,unix,delivered FROM tg_ticket_msgs WHERE ticket_id=? ORDER BY id`, id)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	for rows.Next() {
		var m TicketMsg
		if err := rows.Scan(&m.ID, &m.Staff, &m.Author, &m.Text, &m.Unix, &m.Delivered); err != nil {
			return t, err
		}
		t.Msgs = append(t.Msgs, m)
	}
	t.Count = len(t.Msgs)
	t.Clients, _ = s.TgClientsOf(ctx, t.Chat, t.Seller)
	return t, rows.Err()
}

// TicketReply records a staff answer. delivered=false leaves it for the
// seller's bot to send (an answer typed in the panel).
func (s *Store) TicketReply(ctx context.Context, by Principal, id int64, author, text string, delivered bool) (Ticket, error) {
	text = strings.TrimSpace(text)
	if text == "" || utf8.RuneCountInString(text) > 4000 {
		return Ticket{}, fmt.Errorf("%w: the answer is empty or too long", ErrInvalid)
	}
	t, err := s.TicketByID(ctx, by, id)
	if err != nil {
		return t, err
	}
	now := s.now().Unix()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO tg_ticket_msgs(ticket_id,staff,author,text,unix,delivered) VALUES(?,1,?,?,?,?)`, id, author, text, now, delivered); err != nil {
		return t, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE tg_tickets SET status='answered', answered=1, updated_unix=? WHERE id=?`, now, id); err != nil {
		return t, err
	}
	return t, tx.Commit()
}

// TicketClose closes a ticket; notice=true has the bot tell the customer.
func (s *Store) TicketClose(ctx context.Context, by Principal, id int64, notice bool) (Ticket, error) {
	t, err := s.TicketByID(ctx, by, id)
	if err != nil {
		return t, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE tg_tickets SET status='closed', updated_unix=?, close_notice=? WHERE id=?`, s.now().Unix(), notice, id)
	return t, err
}

// TicketOutbox lists panel answers and close notices a seller's bot must send.
func (s *Store) TicketOutbox(ctx context.Context, seller string) []TicketOut {
	var out []TicketOut
	rows, err := s.ro.QueryContext(ctx, `SELECT m.id,t.id,t.chat_id,m.text FROM tg_ticket_msgs m JOIN tg_tickets t ON t.id=m.ticket_id
WHERE m.delivered=0 AND t.seller=? ORDER BY m.id LIMIT 50`, seller)
	if err == nil {
		for rows.Next() {
			var o TicketOut
			if rows.Scan(&o.MsgID, &o.Ticket, &o.Chat, &o.Text) == nil {
				out = append(out, o)
			}
		}
		rows.Close()
	}
	rows, err = s.ro.QueryContext(ctx, `SELECT id,chat_id FROM tg_tickets WHERE close_notice=1 AND seller=? LIMIT 50`, seller)
	if err == nil {
		for rows.Next() {
			o := TicketOut{Close: true}
			if rows.Scan(&o.Ticket, &o.Chat) == nil {
				out = append(out, o)
			}
		}
		rows.Close()
	}
	return out
}

// TicketSent marks an outbox item done.
func (s *Store) TicketSent(ctx context.Context, o TicketOut) {
	if o.Close {
		_, _ = s.db.ExecContext(ctx, `UPDATE tg_tickets SET close_notice=0 WHERE id=?`, o.Ticket)
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE tg_ticket_msgs SET delivered=1 WHERE id=?`, o.MsgID)
}

// TicketCounts: open tickets waiting for staff, for a badge.
func (s *Store) TicketWaiting(ctx context.Context, by Principal) int {
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM tg_tickets WHERE status='open' AND (?='owner' OR seller=?)`, by.Role, by.RepID).Scan(&n)
	return n
}
