package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Auto-renew: a customer turns it on for a service in the bot and picks a
// plan; shortly before the service ends (a day left, or under 5% of the
// volume) the HUB renews it from the customer's wallet and tells them.
// Without enough credit they are told once a day.

const autoRenewSchema = `
CREATE TABLE IF NOT EXISTS tg_autorenew (
  email TEXT PRIMARY KEY,
  chat_id INTEGER NOT NULL,
  product_id TEXT NOT NULL,
  last_try_unix INTEGER NOT NULL DEFAULT 0,
  last_note_unix INTEGER NOT NULL DEFAULT 0
);`

func (s *Store) ensureAutoRenewSchema() error {
	_, err := s.db.Exec(autoRenewSchema)
	return err
}

// customerNotify, when set, sends a customer a Telegram message.
var customerNotify func(ctx context.Context, chat int64, text string)

func SetCustomerNotify(f func(ctx context.Context, chat int64, text string)) { customerNotify = f }

// AutoRenew is the plan a service renews with, "" if off.
func (s *Store) AutoRenew(ctx context.Context, email string) string {
	var p string
	_ = s.ro.QueryRowContext(ctx, `SELECT product_id FROM tg_autorenew WHERE email=?`, email).Scan(&p)
	return p
}

// SetAutoRenew turns auto-renew on with productID, or off with "". The
// chat must own the service and the plan must be sold for Stars.
func (s *Store) SetAutoRenew(ctx context.Context, chat int64, email, productID string) error {
	var x int
	if s.ro.QueryRowContext(ctx, `SELECT 1 FROM tg_links WHERE chat_id=? AND email=?`, chat, email).Scan(&x) != nil {
		return ErrNotFound
	}
	if productID == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM tg_autorenew WHERE email=?`, email)
		return err
	}
	var stars int64
	var seller, status string
	if err := s.ro.QueryRowContext(ctx, `SELECT price_stars,seller,status FROM products WHERE id=?`, productID).Scan(&stars, &seller, &status); err != nil || seller != "" || status != "published" {
		return ErrNotFound
	}
	if stars <= 0 {
		return ErrNotForStars
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tg_autorenew(email,chat_id,product_id) VALUES(?,?,?)
ON CONFLICT(email) DO UPDATE SET chat_id=excluded.chat_id, product_id=excluded.product_id, last_note_unix=0`, email, chat, productID)
	return err
}

// autoRenewDue reports whether a service is about to end.
func autoRenewDue(c ClientView, now int64) bool {
	if !c.Enabled {
		return false // turned off by the seller: never renew behind their back
	}
	if c.EffectiveUntil > 0 && c.EffectiveUntil-now <= 24*3600 {
		return true
	}
	return c.VolumeBytes > 0 && c.Remaining <= c.VolumeBytes/20
}

// AutoRenewOnce renews every due service it can; it returns how many.
func (s *Store) AutoRenewOnce(ctx context.Context) int {
	rows, err := s.ro.QueryContext(ctx, `SELECT email,chat_id,product_id,last_try_unix,last_note_unix FROM tg_autorenew`)
	if err != nil {
		return 0
	}
	type item struct {
		email, product   string
		chat, try, noted int64
	}
	var list []item
	for rows.Next() {
		var it item
		if rows.Scan(&it.email, &it.chat, &it.product, &it.try, &it.noted) == nil {
			list = append(list, it)
		}
	}
	rows.Close()
	now := s.now().Unix()
	done := 0
	for _, it := range list {
		if now-it.try < 6*3600 {
			continue
		}
		c, err := s.ClientByEmail(ctx, it.email)
		if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
			_, _ = s.db.ExecContext(ctx, `DELETE FROM tg_autorenew WHERE email=?`, it.email)
			continue
		}
		if err != nil || !autoRenewDue(c, now) {
			continue
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE tg_autorenew SET last_try_unix=? WHERE email=?`, now, it.email)
		_, bal, err := s.WalletPay(ctx, it.chat, it.product, it.email)
		var name string
		_ = s.ro.QueryRowContext(ctx, `SELECT name FROM products WHERE id=?`, it.product).Scan(&name)
		switch {
		case err == nil:
			done++
			s.tell(ctx, it.chat, fmt.Sprintf("🔁 سرویس <b>%s</b> خودکار با «%s» تمدید شد.\n💰 موجودی کیف پول: %d⭐", it.email, name, bal))
			_, _ = s.db.ExecContext(ctx, `INSERT INTO events(unix,node_id,email,kind) VALUES(?,?,?,?)`, now, "", it.email, "autorenew")
		case now-it.noted >= 24*3600:
			_, _ = s.db.ExecContext(ctx, `UPDATE tg_autorenew SET last_note_unix=? WHERE email=?`, now, it.email)
			s.tell(ctx, it.chat, fmt.Sprintf("⚠️ تمدید خودکار سرویس <b>%s</b> انجام نشد: موجودی کیف پول کافی نیست (موجودی: %d⭐). کیف پول را شارژ کنید تا خودکار تمدید شود.", it.email, bal))
		}
	}
	return done
}

func (s *Store) tell(ctx context.Context, chat int64, text string) {
	if customerNotify != nil {
		go customerNotify(context.WithoutCancel(ctx), chat, text)
	}
}

// RunAutoRenew checks every ten minutes.
func (s *Store) RunAutoRenew(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		s.AutoRenewOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}
