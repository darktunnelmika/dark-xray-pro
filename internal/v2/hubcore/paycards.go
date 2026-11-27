package hubcore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Several cards per seller, the safe-card options and receipt checks.
//
// A seller (the owner "" or a reseller) keeps up to five cards. Each order or
// top-up is shown one card and keeps it ("sticky"): the card with the most
// room under its daily cap is chosen, so payments spread over the cards. A
// card can be switched off, given a daily cap in toman, or made temporary (it
// hides itself after a time).
//
// The first usable card is mirrored into the old single-card settings, so
// every "is card payment on?" check, older code paths and a rollback to an
// older binary keep working unchanged.
//
// Safe-card options (each on its own): the customer shares their phone
// number before seeing a card (optionally Iranian numbers only), and is
// asked for the last four digits of the card they paid from.
//
// Receipts: every receipt photo is remembered by Telegram's file_unique_id
// and, when the bot could download it, the SHA-256 of its bytes. A photo
// sent before for another payment is flagged to the seller.

const MaxPayCards = 5

const payCardsSchema = `
CREATE TABLE IF NOT EXISTS pay_cards (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  seller TEXT NOT NULL DEFAULT '',
  pos INTEGER NOT NULL DEFAULT 0,
  number TEXT NOT NULL,
  holder TEXT NOT NULL DEFAULT '',
  bank TEXT NOT NULL DEFAULT '',
  enabled INTEGER NOT NULL DEFAULT 1,
  daily_cap INTEGER NOT NULL DEFAULT 0,
  until_unix INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS pay_cards_seller ON pay_cards(seller, pos);
CREATE TABLE IF NOT EXISTS pay_card_use (
  ref TEXT PRIMARY KEY,
  seller TEXT NOT NULL DEFAULT '',
  card_id INTEGER NOT NULL,
  amount INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT 'shown' CHECK(state IN ('shown','paid','void')),
  unix INTEGER NOT NULL,
  last4 TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS pay_card_use_card ON pay_card_use(card_id, unix);
CREATE TABLE IF NOT EXISTS receipt_seen (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ref TEXT NOT NULL,
  seller TEXT NOT NULL DEFAULT '',
  chat_id INTEGER NOT NULL DEFAULT 0,
  uniq TEXT NOT NULL DEFAULT '',
  sha TEXT NOT NULL DEFAULT '',
  unix INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS receipt_seen_uniq ON receipt_seen(uniq);
CREATE INDEX IF NOT EXISTS receipt_seen_sha ON receipt_seen(sha);
CREATE TABLE IF NOT EXISTS tg_phones (
  chat_id INTEGER PRIMARY KEY,
  phone TEXT NOT NULL,
  unix INTEGER NOT NULL
);
`

func (s *Store) ensurePayCardsSchema() error {
	_, err := s.db.Exec(payCardsSchema)
	return err
}

// PayCard is one of a seller's cards.
type PayCard struct {
	ID       int64  `json:"id"`
	Number   string `json:"number"`
	Holder   string `json:"holder"`
	Bank     string `json:"bank"`
	Enabled  bool   `json:"enabled"`
	DailyCap int64  `json:"daily_cap"`  // toman a day, 0 = no cap
	Until    int64  `json:"until"`      // temporary: hidden after this unix time, 0 = always
	Used     int64  `json:"used_today"` // read only: toman taken today (paid + open invoices)
	Live     bool   `json:"live"`       // read only: on and not expired
}

// ErrNoCard means no card can take this payment now (all off, expired or
// over their daily cap).
var ErrNoCard = errors.New("no card can take this payment now")

// pendingHold is how long an invoice that was shown but not paid still
// counts against its card's daily cap.
const pendingHold = 30 * 60

func cleanCardNumber(n string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r >= '۰' && r <= '۹' {
			return '0' + (r - '۰')
		}
		if r >= '٠' && r <= '٩' {
			return '0' + (r - '٠')
		}
		return -1
	}, n)
}

func (s *Store) tehranToday() int64 {
	now := s.now().Unix()
	return (now+tehranOffset)/86400*86400 - tehranOffset
}

// cardUsedToday is what a card took today: paid receipts plus invoices
// shown in the last half hour.
func (s *Store) cardUsedToday(ctx context.Context, q queryer, id int64) int64 {
	var n int64
	now := s.now().Unix()
	_ = q.QueryRowContext(ctx, `SELECT COALESCE(SUM(amount),0) FROM pay_card_use WHERE card_id=? AND unix>=? AND (state='paid' OR (state='shown' AND unix>=?))`,
		id, s.tehranToday(), now-pendingHold).Scan(&n)
	return n
}

// PayCards lists a seller's cards. A seller with only the old single card
// (settings) gets it as one card with ID 0.
func (s *Store) PayCards(ctx context.Context, seller string) ([]PayCard, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,number,holder,bank,enabled,daily_cap,until_unix FROM pay_cards WHERE seller=? ORDER BY pos,id`, seller)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PayCard{}
	for rows.Next() {
		var c PayCard
		if err := rows.Scan(&c.ID, &c.Number, &c.Holder, &c.Bank, &c.Enabled, &c.DailyCap, &c.Until); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	now := s.now().Unix()
	for i := range out {
		out[i].Used = s.cardUsedToday(ctx, s.ro, out[i].ID)
		out[i].Live = out[i].Enabled && (out[i].Until == 0 || out[i].Until > now)
	}
	if len(out) == 0 && !s.cardsManaged(ctx, seller) {
		if c := s.legacyCard(ctx, seller); c.Number != "" {
			out = append(out, PayCard{Number: c.Number, Holder: c.Holder, Bank: c.Bank, Enabled: true, Live: true})
		}
	}
	return out, nil
}

// cardsManaged tells whether the seller's cards live in pay_cards (once the
// list was saved, even an empty list means "no card").
func (s *Store) cardsManaged(ctx context.Context, seller string) bool {
	return s.Setting(ctx, cardKey("pay_cards_on", seller)) == "1"
}

func (s *Store) legacyCard(ctx context.Context, seller string) CardInfo {
	return CardInfo{Number: s.Setting(ctx, cardKey("pay_card_number", seller)), Holder: s.Setting(ctx, cardKey("pay_card_holder", seller)),
		Bank: s.Setting(ctx, cardKey("pay_card_bank", seller))}
}

// PutPayCards replaces a seller's cards (at most five).
func (s *Store) PutPayCards(ctx context.Context, seller string, in []PayCard) error {
	if len(in) > MaxPayCards {
		return fmt.Errorf("%w: at most %d cards", ErrInvalid, MaxPayCards)
	}
	seen := map[string]bool{}
	for i := range in {
		in[i].Number = cleanCardNumber(in[i].Number)
		in[i].Holder, in[i].Bank = strings.TrimSpace(in[i].Holder), strings.TrimSpace(in[i].Bank)
		if len(in[i].Number) < 16 || len(in[i].Number) > 26 {
			return fmt.Errorf("%w: card %d: 16 digits (or a 24-digit IBAN)", ErrInvalid, i+1)
		}
		if len(in[i].Holder) > 80 || len(in[i].Bank) > 60 || in[i].DailyCap < 0 || in[i].DailyCap > maxToman || in[i].Until < 0 {
			return ErrInvalid
		}
		if seen[in[i].Number] {
			return fmt.Errorf("%w: card %d is listed twice", ErrInvalid, i+1)
		}
		seen[in[i].Number] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Keep the ids of cards that stay (by id, or else by number), so the
	// usage already counted today still belongs to them.
	old := map[int64]string{}
	byNum := map[string]int64{}
	rows, err := tx.QueryContext(ctx, `SELECT id,number FROM pay_cards WHERE seller=?`, seller)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var n string
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return err
		}
		old[id], byNum[n] = n, id
	}
	rows.Close()
	keep := map[int64]bool{}
	for pos, c := range in {
		id := c.ID
		if _, ok := old[id]; !ok || id == 0 {
			id = byNum[c.Number]
		}
		if id != 0 && !keep[id] {
			keep[id] = true
			if _, err := tx.ExecContext(ctx, `UPDATE pay_cards SET pos=?,number=?,holder=?,bank=?,enabled=?,daily_cap=?,until_unix=? WHERE id=? AND seller=?`,
				pos, c.Number, c.Holder, c.Bank, c.Enabled, c.DailyCap, c.Until, id, seller); err != nil {
				return err
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO pay_cards(seller,pos,number,holder,bank,enabled,daily_cap,until_unix) VALUES(?,?,?,?,?,?,?,?)`,
			seller, pos, c.Number, c.Holder, c.Bank, c.Enabled, c.DailyCap, c.Until); err != nil {
			return err
		}
	}
	for id := range old {
		if !keep[id] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM pay_cards WHERE id=?`, id); err != nil {
				return err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if err := s.SetSetting(ctx, cardKey("pay_cards_on", seller), "1"); err != nil {
		return err
	}
	return s.mirrorCard(ctx, seller)
}

// mirrorCard writes the first usable card into the old single-card
// settings (or clears them when no card is usable).
func (s *Store) mirrorCard(ctx context.Context, seller string) error {
	c := s.firstLiveCard(ctx, seller)
	for k, v := range map[string]string{"pay_card_number": c.Number, "pay_card_holder": c.Holder, "pay_card_bank": c.Bank} {
		if err := s.SetSetting(ctx, cardKey(k, seller), v); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) firstLiveCard(ctx context.Context, seller string) CardInfo {
	var c CardInfo
	_ = s.ro.QueryRowContext(ctx, `SELECT number,holder,bank FROM pay_cards WHERE seller=? AND enabled=1 AND (until_unix=0 OR until_unix>?) ORDER BY pos,id LIMIT 1`,
		seller, s.now().Unix()).Scan(&c.Number, &c.Holder, &c.Bank)
	return c
}

// CardForPay picks the card for one payment and keeps it for that payment
// (ref: "o:<order id>", "t:<top-up id>" …). amount is in toman.
func (s *Store) CardForPay(ctx context.Context, seller, ref string, amount int64) (CardInfo, error) {
	if !s.cardsManaged(ctx, seller) {
		c := s.legacyCard(ctx, seller)
		if c.Number == "" {
			return c, ErrNoCard
		}
		return c, nil
	}
	if ref != "" {
		var c CardInfo
		err := s.ro.QueryRowContext(ctx, `SELECT c.number,c.holder,c.bank FROM pay_card_use u JOIN pay_cards c ON c.id=u.card_id WHERE u.ref=? AND u.seller=? AND u.state!='void'`, ref, seller).
			Scan(&c.Number, &c.Holder, &c.Bank)
		if err == nil {
			return c, nil
		}
	}
	cards, err := s.PayCards(ctx, seller)
	if err != nil {
		return CardInfo{}, err
	}
	best := -1
	var bestRoom int64
	for i, c := range cards {
		if !c.Live || c.ID == 0 {
			continue
		}
		room := int64(1) << 62
		if c.DailyCap > 0 {
			room = c.DailyCap - c.Used
			if room < amount {
				continue
			}
		}
		// The most room wins; on a tie the card used least today, then the
		// first in the list.
		if best < 0 || room > bestRoom || (room == bestRoom && c.Used < cards[best].Used) {
			best, bestRoom = i, room
		}
	}
	if best < 0 {
		return CardInfo{}, ErrNoCard
	}
	c := cards[best]
	if ref != "" {
		_, _ = s.db.ExecContext(ctx, `INSERT INTO pay_card_use(ref,seller,card_id,amount,state,unix) VALUES(?,?,?,?,'shown',?)
ON CONFLICT(ref) DO UPDATE SET card_id=excluded.card_id, amount=excluded.amount, state='shown', unix=excluded.unix, seller=excluded.seller`,
			ref, seller, c.ID, amount, s.now().Unix())
	}
	return CardInfo{Number: c.Number, Holder: c.Holder, Bank: c.Bank}, nil
}

// markCardUse moves a payment's card hold on: "paid" when the receipt came,
// "void" when the payment was cancelled or refused.
func (s *Store) markCardUse(ctx context.Context, ref, state string) {
	if ref == "" {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE pay_card_use SET state=? WHERE ref=?`, state, ref)
}

// PaymentCard is the card a payment was shown ("" number if none was kept).
func (s *Store) PaymentCard(ctx context.Context, ref string) (CardInfo, string) {
	var c CardInfo
	var last4 string
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(c.number,''),COALESCE(c.holder,''),COALESCE(c.bank,''),u.last4 FROM pay_card_use u LEFT JOIN pay_cards c ON c.id=u.card_id WHERE u.ref=?`, ref).
		Scan(&c.Number, &c.Holder, &c.Bank, &last4)
	return c, last4
}

// SetPayerLast4 keeps the last four digits of the card a customer paid from.
func (s *Store) SetPayerLast4(ctx context.Context, ref, digits string) error {
	d := cleanCardNumber(digits)
	if len(d) != 4 {
		return fmt.Errorf("%w: four digits", ErrInvalid)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE pay_card_use SET last4=? WHERE ref=?`, d, ref)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		// No card hold (old single card): keep it anyway.
		_, err = s.db.ExecContext(ctx, `INSERT INTO pay_card_use(ref,seller,card_id,amount,state,unix,last4) VALUES(?,'',0,0,'paid',?,?)`, ref, s.now().Unix(), d)
	}
	return err
}

// ---- safe-card options ----

// CardSafety are a seller's safe-card options.
type CardSafety struct {
	RequirePhone bool `json:"require_phone"` // share the phone number before seeing a card
	IranOnly     bool `json:"iran_only"`     // only +98 numbers
	AskLast4     bool `json:"ask_last4"`     // ask the last four digits of the paying card
}

func (s *Store) CardSafetyFor(ctx context.Context, seller string) CardSafety {
	var c CardSafety
	_ = json.Unmarshal([]byte(s.Setting(ctx, cardKey("card_safety", seller))), &c)
	if !c.RequirePhone {
		c.IranOnly = false
	}
	return c
}

func (s *Store) PutCardSafety(ctx context.Context, seller string, c CardSafety) error {
	if !c.RequirePhone {
		c.IranOnly = false
	}
	b, _ := json.Marshal(c)
	return s.SetSetting(ctx, cardKey("card_safety", seller), string(b))
}

// NormalizePhone turns a shared contact's number into +<digits>.
func NormalizePhone(p string) string {
	d := cleanCardNumber(p)
	if strings.HasPrefix(d, "00") {
		d = d[2:]
	}
	if strings.HasPrefix(d, "09") && len(d) == 11 {
		d = "98" + d[1:]
	}
	if len(d) < 8 || len(d) > 15 {
		return ""
	}
	return "+" + d
}

// IranianPhone tells whether a normalized number is Iranian.
func IranianPhone(p string) bool { return strings.HasPrefix(p, "+98") }

// TgPhone is the phone number a Telegram user shared ("" = none).
func (s *Store) TgPhone(ctx context.Context, chat int64) string {
	var p string
	_ = s.ro.QueryRowContext(ctx, `SELECT phone FROM tg_phones WHERE chat_id=?`, chat).Scan(&p)
	return p
}

// PutTgPhone keeps the number a user shared about themselves.
func (s *Store) PutTgPhone(ctx context.Context, chat int64, phone string) error {
	p := NormalizePhone(phone)
	if p == "" || chat == 0 {
		return ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO tg_phones(chat_id,phone,unix) VALUES(?,?,?) ON CONFLICT(chat_id) DO UPDATE SET phone=excluded.phone, unix=excluded.unix`,
		chat, p, s.now().Unix())
	return err
}

// PhoneOK tells whether a customer may see the seller's card.
func (s *Store) PhoneOK(ctx context.Context, seller string, chat int64) bool {
	cs := s.CardSafetyFor(ctx, seller)
	if !cs.RequirePhone {
		return true
	}
	p := s.TgPhone(ctx, chat)
	return p != "" && (!cs.IranOnly || IranianPhone(p))
}

// ---- receipts ----

// ReceiptHash is the hex SHA-256 of a receipt photo's bytes.
func ReceiptHash(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ReceiptDup tells about an earlier use of the same receipt photo.
type ReceiptDup struct {
	Ref       string // "o:12", "t:5" … (empty when the earlier one was another seller's)
	Unix      int64
	SameShop  bool
	SameBuyer bool
}

// NoteReceipt remembers a receipt photo for a payment and reports an
// earlier, different payment that used the same photo (nil if none).
func (s *Store) NoteReceipt(ctx context.Context, seller, ref string, chat int64, uniq, sha string) *ReceiptDup {
	if ref == "" || (uniq == "" && sha == "") {
		return nil
	}
	var dup *ReceiptDup
	var q string
	var args []any
	switch {
	case uniq != "" && sha != "":
		q, args = `(uniq=? OR sha=?)`, []any{uniq, sha}
	case uniq != "":
		q, args = `uniq=?`, []any{uniq}
	default:
		q, args = `sha=?`, []any{sha}
	}
	args = append(args, ref)
	var d ReceiptDup
	var rseller string
	var rchat int64
	err := s.ro.QueryRowContext(ctx, `SELECT ref,seller,chat_id,unix FROM receipt_seen WHERE `+q+` AND ref!=? ORDER BY id LIMIT 1`, args...).Scan(&d.Ref, &rseller, &rchat, &d.Unix)
	if err == nil {
		d.SameShop, d.SameBuyer = rseller == seller, rchat == chat
		if !d.SameShop {
			d.Ref = "" // another seller's payment: say only that it was used
		}
		dup = &d
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	_, _ = s.db.ExecContext(ctx, `INSERT INTO receipt_seen(ref,seller,chat_id,uniq,sha,unix) VALUES(?,?,?,?,?,?)`, ref, seller, chat, uniq, sha, s.now().Unix())
	return dup
}

// OrderRef and TopupRef name a payment for the card and receipt records.
func OrderRef(id int64) string { return "o:" + strconv.FormatInt(id, 10) }
func TopupRef(id int64) string { return "t:" + strconv.FormatInt(id, 10) }
