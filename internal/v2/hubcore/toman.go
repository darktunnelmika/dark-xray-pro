package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Selling in toman, per seller. The owner and every reseller (in their own
// Telegram bot) take card-to-card payments to their own card, and each
// keeps a toman wallet for its customers: a customer tops it up by sending
// a receipt, the seller approves, and purchases and renewals (also the
// automatic renewal) are paid from it. Wallets never mix between sellers.

const tomanSchema = `
CREATE TABLE IF NOT EXISTS toman_wallet (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  seller TEXT NOT NULL DEFAULT '',
  chat_id INTEGER NOT NULL,
  unix INTEGER NOT NULL,
  delta INTEGER NOT NULL,
  kind TEXT NOT NULL,
  order_id INTEGER NOT NULL DEFAULT 0,
  note TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS toman_wallet_owner ON toman_wallet(seller, chat_id);
CREATE TABLE IF NOT EXISTS toman_topups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  seller TEXT NOT NULL DEFAULT '',
  chat_id INTEGER NOT NULL,
  amount INTEGER NOT NULL,
  receipt TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'awaiting' CHECK(status IN ('awaiting','review','approved','rejected')),
  created_unix INTEGER NOT NULL,
  done_unix INTEGER NOT NULL DEFAULT 0,
  done_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS toman_topups_seller ON toman_topups(seller, status, id);
`

func (s *Store) ensureTomanSchema() error {
	_, err := s.db.Exec(tomanSchema)
	return err
}

// ---- per-seller card ----

func cardKey(k, seller string) string {
	if seller == "" {
		return k
	}
	return k + ":" + seller
}

// CardFor is the card a seller's customers pay to ("" = the owner).
// With several cards (paycards.go) it is the first usable one: callers
// use it to tell whether card payment is on; the card a payment shows
// comes from CardForPay.
func (s *Store) CardFor(ctx context.Context, seller string) CardInfo {
	if s.cardsManaged(ctx, seller) {
		return s.firstLiveCard(ctx, seller)
	}
	return s.legacyCard(ctx, seller)
}

// PutCardFor sets a seller's card (empty number turns card payment off).
func (s *Store) PutCardFor(ctx context.Context, seller string, c CardInfo) error {
	num := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		if r >= '۰' && r <= '۹' {
			return '0' + (r - '۰')
		}
		return -1
	}, c.Number)
	if num != "" && (len(num) < 16 || len(num) > 26) {
		return fmt.Errorf("%w: card number: 16 digits (or a 24-digit IBAN)", ErrInvalid)
	}
	if len(c.Holder) > 80 || len(c.Bank) > 60 {
		return ErrInvalid
	}
	if s.cardsManaged(ctx, seller) {
		// Several cards are in use: the single-card form sets the list to
		// this one card (or none).
		var list []PayCard
		if num != "" {
			list = []PayCard{{Number: num, Holder: c.Holder, Bank: c.Bank, Enabled: true}}
		}
		return s.PutPayCards(ctx, seller, list)
	}
	for k, v := range map[string]string{"pay_card_number": num, "pay_card_holder": strings.TrimSpace(c.Holder), "pay_card_bank": strings.TrimSpace(c.Bank)} {
		if err := s.SetSetting(ctx, cardKey(k, seller), v); err != nil {
			return err
		}
	}
	return nil
}

// ---- toman wallet ----

// TomanBalance is a customer's balance with one seller.
func (s *Store) TomanBalance(ctx context.Context, seller string, chat int64) int64 {
	var b int64
	_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM toman_wallet WHERE seller=? AND chat_id=?`, seller, chat).Scan(&b)
	return b
}

func tomanBalanceTx(ctx context.Context, tx *sql.Tx, seller string, chat int64) (int64, error) {
	var b int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(delta),0) FROM toman_wallet WHERE seller=? AND chat_id=?`, seller, chat).Scan(&b)
	return b, err
}

// TomanHistory is a customer's wallet history with one seller.
func (s *Store) TomanHistory(ctx context.Context, seller string, chat int64, limit int) ([]WalletEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT unix,delta,kind,order_id,note FROM toman_wallet WHERE seller=? AND chat_id=? ORDER BY id DESC LIMIT ?`, seller, chat, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WalletEntry{}
	for rows.Next() {
		var e WalletEntry
		if err := rows.Scan(&e.Unix, &e.Delta, &e.Kind, &e.OrderID, &e.Note); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

const maxToman = 1_000_000_000 // 1 billion toman

// TomanAdjust gives or takes credit by hand (the seller, from the panel).
func (s *Store) TomanAdjust(ctx context.Context, seller string, chat, delta int64, note, by string) (int64, error) {
	if delta == 0 || delta > maxToman || delta < -maxToman || len(note) > 300 || chat == 0 {
		return 0, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	bal, err := tomanBalanceTx(ctx, tx, seller, chat)
	if err != nil {
		return 0, err
	}
	if bal+delta < 0 {
		return 0, fmt.Errorf("%w: the balance is only %d toman", ErrInvalid, bal)
	}
	kind := "gift"
	if delta < 0 {
		kind = "correction"
	}
	if by != "" {
		note = strings.TrimSpace(note + " (" + by + ")")
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO toman_wallet(seller,chat_id,unix,delta,kind,note) VALUES(?,?,?,?,?,?)`, seller, chat, s.now().Unix(), delta, kind, note); err != nil {
		return 0, err
	}
	return bal + delta, tx.Commit()
}

// ---- top-ups by receipt ----

type Topup struct {
	ID         int64  `json:"id"`
	Seller     string `json:"seller"`
	Chat       int64  `json:"chat"`
	Amount     int64  `json:"amount"`
	Status     string `json:"status"`
	HasReceipt bool   `json:"has_receipt"`
	Receipt    string `json:"-"`
	Created    int64  `json:"created"`
	Done       int64  `json:"done,omitempty"`
	DoneBy     string `json:"done_by,omitempty"`
	Balance    int64  `json:"balance"`
}

// CreateTopup opens a top-up; the customer then sends a receipt.
func (s *Store) CreateTopup(ctx context.Context, seller string, chat, amount int64) (int64, error) {
	if amount < 1000 || amount > 200_000_000 {
		return 0, fmt.Errorf("%w: the amount is 1,000 to 200,000,000 toman", ErrInvalid)
	}
	if s.CardFor(ctx, seller).Number == "" {
		return 0, fmt.Errorf("%w: card payment is off", ErrInvalid)
	}
	// One open top-up per customer and seller: a new one replaces it.
	_, _ = s.db.ExecContext(ctx, `UPDATE toman_topups SET status='rejected', done_unix=?, done_by='replaced' WHERE seller=? AND chat_id=? AND status='awaiting'`, s.now().Unix(), seller, chat)
	r, err := s.db.ExecContext(ctx, `INSERT INTO toman_topups(seller,chat_id,amount,created_unix) VALUES(?,?,?,?)`, seller, chat, amount, s.now().Unix())
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// AwaitingTopup is a customer's open top-up with a seller, if any.
func (s *Store) AwaitingTopup(ctx context.Context, seller string, chat int64) (int64, error) {
	var id int64
	err := s.ro.QueryRowContext(ctx, `SELECT id FROM toman_topups WHERE seller=? AND chat_id=? AND status='awaiting' ORDER BY id DESC LIMIT 1`, seller, chat).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

// AttachTopupReceipt records the receipt photo and sends it for review.
func (s *Store) AttachTopupReceipt(ctx context.Context, id, chat int64, fileID string) (Topup, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE toman_topups SET receipt=?, status='review' WHERE id=? AND chat_id=? AND status IN ('awaiting','review')`, fileID, id, chat)
	if err != nil {
		return Topup{}, err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return Topup{}, ErrNotFound
	}
	s.markCardUse(ctx, TopupRef(id), "paid")
	return s.TopupByID(ctx, id)
}

func (s *Store) TopupByID(ctx context.Context, id int64) (Topup, error) {
	var t Topup
	err := s.ro.QueryRowContext(ctx, `SELECT id,seller,chat_id,amount,receipt,status,created_unix,done_unix,done_by FROM toman_topups WHERE id=?`, id).
		Scan(&t.ID, &t.Seller, &t.Chat, &t.Amount, &t.Receipt, &t.Status, &t.Created, &t.Done, &t.DoneBy)
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	t.HasReceipt = t.Receipt != ""
	return t, err
}

// Topups lists a seller's top-ups (newest first); status "" = all.
func (s *Store) Topups(ctx context.Context, seller, status string, limit int) ([]Topup, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id,seller,chat_id,amount,receipt,status,created_unix,done_unix,done_by FROM toman_topups WHERE seller=?`
	args := []any{seller}
	if status != "" {
		q += ` AND status=?`
		args = append(args, status)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.ro.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Topup{}
	for rows.Next() {
		var t Topup
		if err := rows.Scan(&t.ID, &t.Seller, &t.Chat, &t.Amount, &t.Receipt, &t.Status, &t.Created, &t.Done, &t.DoneBy); err != nil {
			return nil, err
		}
		t.HasReceipt = t.Receipt != ""
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Balance = s.TomanBalance(ctx, out[i].Seller, out[i].Chat)
	}
	return out, nil
}

// DecideTopup approves (credits the wallet) or rejects a top-up. seller
// must match: a reseller decides only its own.
func (s *Store) DecideTopup(ctx context.Context, seller string, id int64, approve bool, by string) (Topup, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Topup{}, err
	}
	defer tx.Rollback()
	var t Topup
	if err := tx.QueryRowContext(ctx, `SELECT id,seller,chat_id,amount,status FROM toman_topups WHERE id=?`, id).Scan(&t.ID, &t.Seller, &t.Chat, &t.Amount, &t.Status); err != nil {
		return t, ErrNotFound
	}
	if t.Seller != seller {
		return t, ErrNotFound
	}
	if t.Status != "review" && t.Status != "awaiting" {
		return t, fmt.Errorf("%w: this top-up was already %s", ErrInvalid, t.Status)
	}
	now := s.now().Unix()
	status := "rejected"
	if approve {
		status = "approved"
		if _, err := tx.ExecContext(ctx, `INSERT INTO toman_wallet(seller,chat_id,unix,delta,kind,note) VALUES(?,?,?,?,?,?)`,
			t.Seller, t.Chat, now, t.Amount, "topup", "top-up #"+strconv.FormatInt(t.ID, 10)); err != nil {
			return t, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE toman_topups SET status=?, done_unix=?, done_by=? WHERE id=?`, status, now, by, id); err != nil {
		return t, err
	}
	if err := tx.Commit(); err != nil {
		return t, err
	}
	t.Status, t.Done, t.DoneBy = status, now, by
	t.Balance = s.TomanBalance(ctx, t.Seller, t.Chat)
	if !approve {
		s.markCardUse(ctx, TopupRef(t.ID), "void")
	}
	return t, nil
}

// ---- paying from the toman wallet ----

// TomanPay buys (or renews with) a product from the customer's wallet with
// the product's seller, and delivers at once. A reseller's sale is paid
// from that reseller's credit, exactly like a hand-made client.
func (s *Store) TomanPay(ctx context.Context, chat int64, productID, renew string) (string, int64, error) {
	var price int64
	var seller, status string
	if err := s.ro.QueryRowContext(ctx, `SELECT price,seller,status FROM products WHERE id=?`, productID).Scan(&price, &seller, &status); err != nil || status != "published" {
		return "", 0, ErrNotFound
	}
	if price <= 0 {
		return "", 0, fmt.Errorf("%w: this product has no toman price", ErrInvalid)
	}
	by := Principal{User: "telegram", Role: "owner"}
	if seller != "" {
		by = Principal{User: "telegram", Role: "rep", RepID: seller}
	}
	id, err := s.CreateOrder(ctx, by, productID, TgBuyer(chat), renew, "wallet")
	if err != nil {
		return "", 0, err
	}
	o, err := s.OrderByID(ctx, id)
	if err != nil {
		return "", 0, err
	}
	amount := o.Amount // after any discount
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	bal, err := tomanBalanceTx(ctx, tx, seller, chat)
	if err != nil {
		return "", 0, err
	}
	if bal < amount {
		_ = tx.Rollback()
		_, _ = s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled', note='wallet: not enough credit' WHERE id=?`, id)
		s.releaseDiscount(ctx, id)
		return "", bal, fmt.Errorf("%w: not enough credit (%d of %d toman)", ErrInvalid, bal, amount)
	}
	if amount > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO toman_wallet(seller,chat_id,unix,delta,kind,order_id) VALUES(?,?,?,?,?,?)`, seller, chat, s.now().Unix(), -amount, "spend", id); err != nil {
			return "", 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE orders SET method='wallet' WHERE id=?`, id); err != nil {
		return "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return "", 0, err
	}
	email, err := s.FulfillOrder(ctx, by, id)
	if err != nil {
		if amount > 0 {
			_, _ = s.db.ExecContext(ctx, `INSERT INTO toman_wallet(seller,chat_id,unix,delta,kind,order_id,note) VALUES(?,?,?,?,?,?,?)`, seller, chat, s.now().Unix(), amount, "refund", id, "delivery failed")
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE orders SET status='cancelled' WHERE id=? AND status='pending'`, id)
		return "", bal, err
	}
	return email, bal - amount, nil
}

// ---- resellers' own Telegram bots ----

// RepBot is a reseller's own sales bot.
type RepBot struct {
	Token    string `json:"token,omitempty"` // write-only in the API
	Enabled  bool   `json:"enabled"`
	Username string `json:"username,omitempty"`
	HasToken bool   `json:"has_token"`
	Running  bool   `json:"running"`
	Error    string `json:"error,omitempty"`
	// MiniApp: the store (Mini App) button in this bot; nil on writes
	// leaves it as it is. Stored apart from the token.
	MiniApp *bool `json:"miniapp,omitempty"`
	// Owners: the numeric Telegram ids that manage this panel in the bot,
	// all on a par; nil on writes leaves them as they are.
	Owners *[]int64 `json:"owners,omitempty"`
}

var botTokenRE = regexp.MustCompile(`^[0-9]{5,15}:[A-Za-z0-9_-]{30,64}$`)

func (s *Store) repBotRaw(ctx context.Context, rep string) RepBot {
	var b RepBot
	_ = json.Unmarshal([]byte(s.Setting(ctx, "rep_bot:"+rep)), &b)
	return b
}

// RepBotOf returns a reseller's bot settings with live status (no token).
func (s *Store) RepBotOf(ctx context.Context, rep string) RepBot {
	b := s.repBotRaw(ctx, rep)
	b.HasToken = b.Token != ""
	b.Token = ""
	if st, ok := repBotStatus.Load(rep); ok {
		v := st.(RepBot)
		b.Running, b.Username, b.Error = v.Running, firstNonEmpty(v.Username, b.Username), v.Error
	}
	on := s.Setting(ctx, "rep_miniapp:"+rep) != "0"
	b.MiniApp = &on
	owners := s.TgRepOwners(ctx, rep)
	b.Owners = &owners
	return b
}

// PutRepBot sets a reseller's bot; an empty token keeps the current one,
// "-" removes it.
func (s *Store) PutRepBot(ctx context.Context, rep string, in RepBot) error {
	var n int
	if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM reps WHERE id=?`, rep).Scan(&n); err != nil || n == 0 {
		return ErrNotFound
	}
	cur := s.repBotRaw(ctx, rep)
	switch tok := strings.TrimSpace(in.Token); {
	case tok == "-":
		cur = RepBot{}
	case tok != "":
		if !botTokenRE.MatchString(tok) {
			return fmt.Errorf("%w: this does not look like a bot token from @BotFather", ErrInvalid)
		}
		// One bot, one seller: two sellers polling the same bot would fight
		// (a reseller selling through Mirza keeps its bot for when it is back).
		if s.repBotTokenUser(ctx, tok, rep) != "" {
			return fmt.Errorf("%w: this bot is already used by another reseller", ErrInvalid)
		}
		if tgToken == tok {
			return fmt.Errorf("%w: this is the panel's main bot", ErrInvalid)
		}
		cur.Token, cur.Username = tok, ""
	}
	if in.Owners != nil {
		if err := s.SetTgRepOwners(ctx, rep, *in.Owners); err != nil {
			return err
		}
	}
	cur.Enabled = in.Enabled && cur.Token != ""
	if in.MiniApp != nil {
		v := "1"
		if !*in.MiniApp {
			v = "0"
		}
		if err := s.SetSetting(ctx, "rep_miniapp:"+rep, v); err != nil {
			return err
		}
	}
	b, _ := json.Marshal(RepBot{Token: cur.Token, Enabled: cur.Enabled, Username: cur.Username})
	return s.SetSetting(ctx, "rep_bot:"+rep, string(b))
}

type RepBotConf struct {
	Rep   string
	Token string
}

// RepBotsAll lists the resellers' bot settings (tokens included; for the
// bot runner only).
func (s *Store) RepBotsAll(ctx context.Context) []RepBotConf {
	rows, err := s.ro.QueryContext(ctx, `SELECT key,value FROM settings WHERE key LIKE 'rep_bot:%'`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []RepBotConf
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) != nil {
			continue
		}
		var b RepBot
		rep := strings.TrimPrefix(k, "rep_bot:")
		if json.Unmarshal([]byte(v), &b) == nil && b.Token != "" && b.Enabled && !s.MirzaMode(ctx, rep) {
			out = append(out, RepBotConf{Rep: rep, Token: b.Token})
		}
	}
	return out
}

// repBotTokenUser is another reseller whose bot settings hold this token,
// running or not ("" = none).
func (s *Store) repBotTokenUser(ctx context.Context, tok, except string) string {
	rows, err := s.ro.QueryContext(ctx, `SELECT key,value FROM settings WHERE key LIKE 'rep_bot:%'`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		var b RepBot
		if rows.Scan(&k, &v) == nil && json.Unmarshal([]byte(v), &b) == nil && b.Token == tok {
			if rep := strings.TrimPrefix(k, "rep_bot:"); rep != except {
				return rep
			}
		}
	}
	return ""
}

var repBotStatus sync.Map // rep id -> RepBot (live state)

// SetRepBotStatus records a reseller bot's live state (from the runner).
func SetRepBotStatus(rep string, running bool, username, errText string) {
	repBotStatus.Store(rep, RepBot{Running: running, Username: username, Error: errText})
}

// TgClientsOf lists the clients a chat owns that belong to one seller.
func (s *Store) TgClientsOf(ctx context.Context, chat int64, seller string) ([]string, error) {
	all, err := s.TgClients(ctx, chat)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range all {
		var rep string
		if s.ro.QueryRowContext(ctx, `SELECT rep_id FROM clients WHERE email=?`, e).Scan(&rep) == nil && rep == seller {
			out = append(out, e)
		}
	}
	return out, nil
}

type TomanWallet struct {
	Chat    int64 `json:"chat"`
	Balance int64 `json:"balance"`
	Last    int64 `json:"last"`
}

// TomanWallets lists a seller's customer wallets with a balance or history.
func (s *Store) TomanWallets(ctx context.Context, seller string) ([]TomanWallet, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT chat_id,SUM(delta),MAX(unix) FROM toman_wallet WHERE seller=? GROUP BY chat_id ORDER BY 2 DESC LIMIT 1000`, seller)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TomanWallet{}
	for rows.Next() {
		var w TomanWallet
		if err := rows.Scan(&w.Chat, &w.Balance, &w.Last); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// RepBotRunning reports whether a reseller's own bot is running.
func RepBotRunning(rep string) bool {
	v, ok := repBotStatus.Load(rep)
	return ok && v.(RepBot).Running
}

// BotUsernameFor is the bot a seller's customers should open: the
// reseller's own bot when it is set up and running, otherwise the main bot.
func (s *Store) BotUsernameFor(ctx context.Context, seller string) string {
	if seller != "" {
		if b := s.RepBotOf(ctx, seller); b.Enabled && b.Running && b.Username != "" {
			return strings.TrimPrefix(b.Username, "@")
		}
	}
	return strings.TrimPrefix(s.Setting(ctx, "tg_username"), "@")
}
