package hubcore

import (
	"context"
	"database/sql"
	"strings"
)

// Referrals, the parts beyond the money reward (commerce.go):
//
//   - the invited customer's gift: a discount on their first purchase in the
//     owner's shop. It works like a discount code the customer never typed
//     (InviteCode); a code they do type wins, the two never stack;
//   - the inviter's volume bonus: gigabytes added to their service for each
//     invited customer's first purchase. With no service that can take
//     volume, the bonus waits and goes onto their next purchase;
//   - the inviter's list (who joined, took a trial, bought) and the owner's
//     numbers.

// InviteCode marks an order discounted by the invite gift.
const InviteCode = "INVITE"

const refBonusSchema = `
CREATE TABLE IF NOT EXISTS tg_ref_bonus (
  order_id INTEGER PRIMARY KEY,
  chat_id INTEGER NOT NULL,
  bytes INTEGER NOT NULL,
  email TEXT NOT NULL DEFAULT '',
  unix INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS tg_ref_bonus_chat ON tg_ref_bonus(chat_id, email);
`

// invited is who invited a chat (0 = nobody).
func (s *Store) invited(ctx context.Context, chat int64) int64 {
	var r int64
	_ = s.ro.QueryRowContext(ctx, `SELECT referrer FROM tg_referrals WHERE chat_id=?`, chat).Scan(&r)
	return r
}

// InviteGift is the discount an invited customer gets now (0 = none): only
// customers who came with a link, before their first purchase.
func (s *Store) InviteGift(ctx context.Context, chat int64) int64 {
	pol := s.ReferralPolicy(ctx)
	if !pol.Enabled || pol.InviteePercent <= 0 || s.invited(ctx, chat) == 0 {
		return 0
	}
	buyer := TgBuyer(chat)
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE buyer=? AND seller='' AND (status='fulfilled' OR (discount_code=? AND status='pending' AND created_unix>?))`,
		buyer, InviteCode, s.now().Unix()-3600).Scan(&n)
	if n > 0 {
		return 0
	}
	return pol.InviteePercent
}

// inviteDiscount is the invite gift as a discount on one of the owner's
// products.
func (s *Store) inviteDiscount(ctx context.Context, buyer, productID string) (Discount, bool) {
	chat, ok := TgChatOfBuyer(buyer)
	if !ok {
		return Discount{}, false
	}
	if productID != "" {
		var seller string
		if s.ro.QueryRowContext(ctx, `SELECT seller FROM products WHERE id=?`, productID).Scan(&seller) != nil || seller != "" {
			return Discount{}, false
		}
	}
	pct := s.InviteGift(ctx, chat)
	if pct <= 0 {
		return Discount{}, false
	}
	return Discount{Code: InviteCode, Percent: pct, Enabled: true, OncePerBuyer: true}, true
}

// rewardReferralBonus adds the volume bonus for an invited customer's first
// purchase. It returns the inviter, the bytes and the service that got them
// ("" = waiting for the inviter's next purchase).
func (s *Store) rewardReferralBonus(ctx context.Context, orderID int64) (int64, int64, string) {
	pol := s.ReferralPolicy(ctx)
	if !pol.Enabled || pol.BonusGB <= 0 {
		return 0, 0, ""
	}
	var buyer, seller string
	if s.ro.QueryRowContext(ctx, `SELECT buyer,seller FROM orders WHERE id=? AND status='fulfilled'`, orderID).Scan(&buyer, &seller) != nil || seller != "" {
		return 0, 0, ""
	}
	chat, ok := TgChatOfBuyer(buyer)
	if !ok {
		return 0, 0, ""
	}
	referrer := s.invited(ctx, chat)
	if referrer == 0 {
		return 0, 0, ""
	}
	var earlier int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders WHERE buyer=? AND seller='' AND status='fulfilled' AND id<?`, buyer, orderID).Scan(&earlier)
	if earlier > 0 {
		return 0, 0, "" // one bonus per invited customer
	}
	bytes := pol.BonusGB << 30
	r, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO tg_ref_bonus(order_id,chat_id,bytes,unix) VALUES(?,?,?,?)`, orderID, referrer, bytes, s.now().Unix())
	if err != nil {
		return 0, 0, ""
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return 0, 0, ""
	}
	email := s.refBonusTarget(ctx, referrer)
	if email != "" {
		if n, err := s.BulkExtend(ctx, BulkRequest{Emails: []string{email}, AddBytes: bytes}); err != nil || n != 1 {
			email = ""
		} else {
			_, _ = s.db.ExecContext(ctx, `UPDATE tg_ref_bonus SET email=? WHERE order_id=?`, email, orderID)
		}
	}
	return referrer, bytes, email
}

// refBonusTarget is the inviter's service that takes the bonus: a running
// volume service in the owner's shop, the one that lasts longest.
func (s *Store) refBonusTarget(ctx context.Context, chat int64) string {
	emails, err := s.TgClientsOf(ctx, chat, "")
	if err != nil {
		return ""
	}
	now := s.now().Unix()
	best, bestExp := "", int64(-1)
	for _, e := range emails {
		var vol, exp int64
		var on bool
		if s.ro.QueryRowContext(ctx, `SELECT volume_bytes,expires_unix,enabled FROM clients WHERE email=? AND rep_id='' AND trial=0`, e).Scan(&vol, &exp, &on) != nil {
			continue
		}
		if !on || vol <= 0 || (exp > 0 && exp <= now) {
			continue
		}
		rank := exp
		if exp == 0 {
			rank = 1 << 62 // not started yet: lasts longest
		}
		if rank > bestExp {
			best, bestExp = e, rank
		}
	}
	return best
}

// applyPendingRefBonus puts waiting bonuses onto a service the inviter just
// bought or renewed. Returns the bytes added.
func (s *Store) applyPendingRefBonus(ctx context.Context, chat int64, email string) int64 {
	var total int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(bytes),0) FROM tg_ref_bonus WHERE chat_id=? AND email=''`, chat).Scan(&total)
	if total <= 0 {
		return 0
	}
	var vol int64
	if s.ro.QueryRowContext(ctx, `SELECT volume_bytes FROM clients WHERE email=? AND rep_id='' AND trial=0`, email).Scan(&vol) != nil || vol <= 0 {
		return 0 // unlimited: keep it for a volume service
	}
	if n, err := s.BulkExtend(ctx, BulkRequest{Emails: []string{email}, AddBytes: total}); err != nil || n != 1 {
		return 0
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE tg_ref_bonus SET email=? WHERE chat_id=? AND email=''`, email, chat)
	return total
}

// bonusHook tells the bot about a volume bonus: who, how much, and which
// service got it ("" = it waits for the next purchase).
var bonusHook func(chat, bytes int64, email string)

// SetRefBonusHook registers the volume-bonus notification.
func SetRefBonusHook(f func(chat, bytes int64, email string)) { bonusHook = f }

// Invitee is one person a customer invited.
type Invitee struct {
	Chat   int64  `json:"chat"`
	Name   string `json:"name"`
	Unix   int64  `json:"unix"`
	Trial  bool   `json:"trial"`
	Bought int    `json:"bought"`
	Toman  int64  `json:"toman"` // what their purchases brought the inviter
	Stars  int64  `json:"stars"`
	Bytes  int64  `json:"bytes"`
}

// Invitees lists who a customer invited, newest first.
func (s *Store) Invitees(ctx context.Context, chat int64, limit int) ([]Invitee, error) {
	if limit <= 0 || limit > 500 {
		limit = 500
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT r.chat_id, r.unix, COALESCE(u.name,''),
 EXISTS(SELECT 1 FROM trial_claims t WHERE t.seller='' AND t.chat_id=r.chat_id),
 (SELECT COUNT(*) FROM orders o WHERE o.buyer='tg:'||r.chat_id AND o.seller='' AND o.status='fulfilled'),
 (SELECT COALESCE(SUM(w.delta),0) FROM toman_wallet w JOIN orders o ON o.id=w.order_id WHERE w.seller='' AND w.chat_id=r.referrer AND w.kind='gift' AND o.buyer='tg:'||r.chat_id),
 (SELECT COALESCE(SUM(w.delta),0) FROM tg_wallet w JOIN orders o ON o.id=w.order_id WHERE w.chat_id=r.referrer AND w.kind='reward' AND o.buyer='tg:'||r.chat_id),
 (SELECT COALESCE(SUM(b.bytes),0) FROM tg_ref_bonus b JOIN orders o ON o.id=b.order_id WHERE b.chat_id=r.referrer AND o.buyer='tg:'||r.chat_id)
FROM tg_referrals r LEFT JOIN tg_users u ON u.chat_id=r.chat_id WHERE r.referrer=? ORDER BY r.unix DESC LIMIT ?`, chat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitee
	for rows.Next() {
		var v Invitee
		if err := rows.Scan(&v.Chat, &v.Unix, &v.Name, &v.Trial, &v.Bought, &v.Toman, &v.Stars, &v.Bytes); err != nil {
			return nil, err
		}
		if f := strings.Fields(v.Name); len(f) > 0 {
			v.Name = strings.Join(f, " ")
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ReferralStats is the owner's view of invitations.
type ReferralStats struct {
	Inviters  int       `json:"inviters"`  // customers who brought at least one person
	Invited   int       `json:"invited"`   // people who came with a link
	Trial     int       `json:"trial"`     // of them, took a trial
	Bought    int       `json:"bought"`    // of them, bought at least once
	Sales     int64     `json:"sales"`     // toman paid by invited customers
	Toman     int64     `json:"toman"`     // toman rewards paid to inviters
	Stars     int64     `json:"stars"`     // Stars rewards
	Bytes     int64     `json:"bytes"`     // volume bonuses
	Discounts int64     `json:"discounts"` // toman taken off by the invite gift
	Top       []Inviter `json:"top"`
}

// Inviter is one row of the owner's top list.
type Inviter struct {
	Chat    int64  `json:"chat"`
	Name    string `json:"name"`
	Invited int    `json:"invited"`
	Bought  int    `json:"bought"`
	Toman   int64  `json:"toman"`
	Bytes   int64  `json:"bytes"`
}

// ReferralStatsAll sums invitations up for the owner, with the top inviters
// by customers who bought.
func (s *Store) ReferralStatsAll(ctx context.Context, top int) (ReferralStats, error) {
	var st ReferralStats
	q := func(dst any, query string, args ...any) {
		if err := s.ro.QueryRowContext(ctx, query, args...).Scan(dst); err != nil && err != sql.ErrNoRows {
			return
		}
	}
	q(&st.Inviters, `SELECT COUNT(DISTINCT referrer) FROM tg_referrals`)
	q(&st.Invited, `SELECT COUNT(*) FROM tg_referrals`)
	q(&st.Trial, `SELECT COUNT(*) FROM tg_referrals r WHERE EXISTS(SELECT 1 FROM trial_claims t WHERE t.seller='' AND t.chat_id=r.chat_id)`)
	q(&st.Bought, `SELECT COUNT(*) FROM tg_referrals r WHERE EXISTS(SELECT 1 FROM orders o WHERE o.buyer='tg:'||r.chat_id AND o.seller='' AND o.status='fulfilled')`)
	q(&st.Sales, `SELECT COALESCE(SUM(o.amount),0) FROM orders o JOIN tg_referrals r ON o.buyer='tg:'||r.chat_id WHERE o.seller='' AND o.status='fulfilled' AND o.stars=0`)
	q(&st.Toman, `SELECT COALESCE(SUM(delta),0) FROM toman_wallet WHERE seller='' AND kind='gift' AND note='invited customer bought'`)
	q(&st.Stars, `SELECT COALESCE(SUM(delta),0) FROM tg_wallet WHERE kind='reward'`)
	q(&st.Bytes, `SELECT COALESCE(SUM(bytes),0) FROM tg_ref_bonus`)
	q(&st.Discounts, `SELECT COALESCE(SUM(discount),0) FROM orders WHERE discount_code=? AND status='fulfilled'`, InviteCode)
	if top <= 0 || top > 50 {
		top = 10
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT r.referrer, COALESCE(u.name,''), COUNT(*),
 SUM(EXISTS(SELECT 1 FROM orders o WHERE o.buyer='tg:'||r.chat_id AND o.seller='' AND o.status='fulfilled')),
 (SELECT COALESCE(SUM(delta),0) FROM toman_wallet w WHERE w.seller='' AND w.chat_id=r.referrer AND w.kind='gift' AND w.note='invited customer bought'),
 (SELECT COALESCE(SUM(bytes),0) FROM tg_ref_bonus b WHERE b.chat_id=r.referrer)
FROM tg_referrals r LEFT JOIN tg_users u ON u.chat_id=r.referrer GROUP BY r.referrer ORDER BY 4 DESC, 3 DESC LIMIT ?`, top)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Inviter
		if err := rows.Scan(&v.Chat, &v.Name, &v.Invited, &v.Bought, &v.Toman, &v.Bytes); err != nil {
			return st, err
		}
		v.Name = strings.Join(strings.Fields(v.Name), " ")
		st.Top = append(st.Top, v)
	}
	return st, rows.Err()
}
