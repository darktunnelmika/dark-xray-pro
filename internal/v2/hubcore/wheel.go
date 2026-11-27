package hubcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// The daily wheel of fortune: once a Tehran day each customer of a seller's
// bot spins and wins what the seller put on the wheel — GB or days on one
// of the customer's services in this bot, toman in the bot's wallet, or a
// one-time discount code. The server picks the prize; the app only shows it
// landing. A reseller's GB and days come out of its own credit, as any
// volume it adds does.

const wheelSchema = `CREATE TABLE IF NOT EXISTS wheel_spins (
  id INTEGER PRIMARY KEY,
  seller TEXT NOT NULL,
  chat_id INTEGER NOT NULL,
  day TEXT NOT NULL,
  kind TEXT NOT NULL,
  value INTEGER NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  code TEXT NOT NULL DEFAULT '',
  unix INTEGER NOT NULL,
  UNIQUE (seller, chat_id, day)
);
CREATE INDEX IF NOT EXISTS wheel_spins_day ON wheel_spins(seller, day);`

func (s *Store) ensureWheelSchema() error {
	_, err := s.db.Exec(wheelSchema)
	return err
}

// WheelSlice is one prize on the wheel.
type WheelSlice struct {
	Kind   string `json:"kind"`   // gb | days | wallet | discount | none
	Value  int64  `json:"value"`  // GB, days, toman or percent
	Weight int64  `json:"weight"` // relative chance
}

type WheelConfig struct {
	On          bool         `json:"on"`
	Slices      []WheelSlice `json:"slices"`
	NeedService bool         `json:"need_service"` // only chats with a service in this bot spin
	DailyMax    int64        `json:"daily_max"`    // spins a day for the whole bot (0 = no cap)
}

var ErrSpun = errors.New("already spun today")

func wheelKey(seller string) string { return "wheel:" + seller }

func defaultWheel() WheelConfig {
	return WheelConfig{NeedService: true, Slices: []WheelSlice{
		{Kind: "gb", Value: 1, Weight: 30}, {Kind: "wallet", Value: 5000, Weight: 25}, {Kind: "discount", Value: 10, Weight: 20},
		{Kind: "gb", Value: 2, Weight: 12}, {Kind: "days", Value: 2, Weight: 10}, {Kind: "gb", Value: 5, Weight: 3}}}
}

func (s *Store) WheelFor(ctx context.Context, seller string) WheelConfig {
	v := s.Setting(ctx, wheelKey(seller))
	if v == "" {
		return defaultWheel()
	}
	var c WheelConfig
	if json.Unmarshal([]byte(v), &c) != nil {
		return defaultWheel()
	}
	return c
}

func (s *Store) PutWheelFor(ctx context.Context, seller string, c WheelConfig) error {
	if len(c.Slices) < 2 || len(c.Slices) > 12 {
		return fmt.Errorf("%w: the wheel has 2 to 12 prizes", ErrInvalid)
	}
	if c.DailyMax < 0 || c.DailyMax > 1_000_000 {
		return fmt.Errorf("%w: check the daily cap", ErrInvalid)
	}
	for _, x := range c.Slices {
		ok := x.Weight >= 1 && x.Weight <= 1000
		switch x.Kind {
		case "gb":
			ok = ok && x.Value >= 1 && x.Value <= 100
		case "days":
			ok = ok && x.Value >= 1 && x.Value <= 60
		case "wallet":
			ok = ok && x.Value >= 1000 && x.Value <= 5_000_000
		case "discount":
			ok = ok && x.Value >= 1 && x.Value <= 90
		case "none":
			ok = ok && x.Value == 0
		default:
			ok = false
		}
		if !ok {
			return fmt.Errorf("%w: check the prizes", ErrInvalid)
		}
	}
	b, _ := json.Marshal(c)
	return s.SetSetting(ctx, wheelKey(seller), string(b))
}

func tehranDay(unix int64) string { return time.Unix(unix+tehranOffset, 0).UTC().Format("2006-01-02") }

// untilTomorrow is how long until the next Tehran midnight.
func untilTomorrow(unix int64) int64 {
	t := unix + tehranOffset
	return 86400 - t%86400
}

// WheelSpin is a spin's prize.
type WheelSpin struct {
	Index  int    `json:"index"` // the slice it lands on
	Kind   string `json:"kind"`
	Value  int64  `json:"value"`
	Target string `json:"target,omitempty"` // the service that got GB or days
	Code   string `json:"code,omitempty"`   // the discount code won
	Unix   int64  `json:"unix"`
}

// wheelTargets are the chat's services in this seller's bot that can take
// GB (not unlimited) and days, the one with the least left first.
func (s *Store) wheelTargets(ctx context.Context, seller string, chat int64) (gb, days []ClientView, any bool) {
	var emails []string
	if seller != "" {
		emails, _ = s.TgClientsOf(ctx, chat, seller)
	} else {
		emails, _ = s.TgClients(ctx, chat)
	}
	now := s.now().Unix()
	for _, e := range emails {
		c, err := s.ClientByEmail(ctx, e)
		if err != nil || c.RepID != seller || c.Trial {
			continue
		}
		any = true
		if !c.Enabled {
			continue
		}
		days = append(days, c)
		if c.VolumeBytes >= 0 && (c.EffectiveUntil == 0 || c.EffectiveUntil > now) {
			gb = append(gb, c)
		}
	}
	return gb, days, any
}

// WheelStatus is what the app shows before a spin.
func (s *Store) WheelStatus(ctx context.Context, seller string, chat int64) map[string]any {
	c := s.WheelFor(ctx, seller)
	if !c.On || (seller != "" && s.IsSubSeller(ctx, seller)) {
		return map[string]any{"on": false}
	}
	now := s.now().Unix()
	out := map[string]any{"on": true, "slices": c.Slices, "next": untilTomorrow(now)}
	var sp WheelSpin
	err := s.ro.QueryRowContext(ctx, `SELECT kind,value,target,code,unix FROM wheel_spins WHERE seller=? AND chat_id=? AND day=?`, seller, chat, tehranDay(now)).
		Scan(&sp.Kind, &sp.Value, &sp.Target, &sp.Code, &sp.Unix)
	if err == nil {
		out["today"] = sp
	}
	if _, _, any := s.wheelTargets(ctx, seller, chat); c.NeedService && !any {
		out["need_service"] = true
	}
	return out
}

func randN(n int64) int64 {
	v, err := rand.Int(rand.Reader, big.NewInt(n))
	if err != nil {
		return 0
	}
	return v.Int64()
}

// SpinWheel spins once for the chat today and gives the prize.
func (s *Store) SpinWheel(ctx context.Context, seller string, chat int64) (WheelSpin, error) {
	c := s.WheelFor(ctx, seller)
	if !c.On || chat == 0 || (seller != "" && s.IsSubSeller(ctx, seller)) {
		return WheelSpin{}, ErrNotFound
	}
	gbT, dayT, any := s.wheelTargets(ctx, seller, chat)
	if c.NeedService && !any {
		return WheelSpin{}, fmt.Errorf("%w: buy a service to spin the wheel", ErrPolicy)
	}
	now := s.now().Unix()
	day := tehranDay(now)
	if c.DailyMax > 0 {
		var n int64
		_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM wheel_spins WHERE seller=? AND day=?`, seller, day).Scan(&n)
		if n >= c.DailyMax {
			return WheelSpin{}, fmt.Errorf("%w: today's prizes are all given", ErrPolicy)
		}
	}
	// Only prizes this chat can take now.
	var idx []int
	var total int64
	for i, x := range c.Slices {
		if (x.Kind == "gb" && len(gbT) == 0) || (x.Kind == "days" && len(dayT) == 0) {
			continue
		}
		idx = append(idx, i)
		total += x.Weight
	}
	if total == 0 {
		return WheelSpin{}, fmt.Errorf("%w: no prize fits your services now", ErrPolicy)
	}
	r, pick := randN(total), idx[0]
	for _, i := range idx {
		if r < c.Slices[i].Weight {
			pick = i
			break
		}
		r -= c.Slices[i].Weight
	}
	x := c.Slices[pick]
	sp := WheelSpin{Index: pick, Kind: x.Kind, Value: x.Value, Unix: now}
	// The row first: the unique key makes a second spin today fail before any prize is given.
	res, err := s.db.ExecContext(ctx, `INSERT INTO wheel_spins(seller,chat_id,day,kind,value,unix) VALUES(?,?,?,?,?,?)`, seller, chat, day, x.Kind, x.Value, now)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return WheelSpin{}, ErrSpun
		}
		return WheelSpin{}, err
	}
	id, _ := res.LastInsertId()
	undo := func(e error) (WheelSpin, error) {
		_, _ = s.db.ExecContext(ctx, `DELETE FROM wheel_spins WHERE id=?`, id)
		return WheelSpin{}, e
	}
	m := miniMgr{P: Principal{User: "wheel", Role: "rep"}, Seller: seller, Owner: seller == ""}
	switch x.Kind {
	case "gb":
		t := gbT[0]
		for _, c := range gbT[1:] { // the one with the least left
			if c.Remaining < t.Remaining {
				t = c
			}
		}
		sp.Target = t.Email
		if err := s.mgrAdd(ctx, m, t, x.Value<<30, 0); err != nil {
			return undo(err)
		}
	case "days":
		t := dayT[0]
		sp.Target = t.Email
		if err := s.mgrAdd(ctx, m, t, 0, x.Value); err != nil {
			return undo(err)
		}
	case "wallet":
		if _, err := s.TomanAdjust(ctx, seller, chat, x.Value, "🎡 جایزهٔ گردونهٔ شانس", "wheel"); err != nil {
			return undo(err)
		}
	case "discount":
		b := make([]byte, 3)
		_, _ = rand.Read(b)
		sp.Code = "WH" + strings.ToUpper(hex.EncodeToString(b))
		if err := s.PutDiscountFor(ctx, seller, Discount{Code: sp.Code, Percent: x.Value, MaxUses: 1, OncePerBuyer: true, Enabled: true,
			ExpiresUnix: now + 3*86400, Note: fmt.Sprintf("گردونه · %d", chat)}); err != nil {
			return undo(err)
		}
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE wheel_spins SET target=?, code=? WHERE id=?`, sp.Target, sp.Code, id)
	return sp, nil
}

// WheelToday sums today's spins for the seller (management).
func (s *Store) WheelToday(ctx context.Context, seller string) map[string]int64 {
	out := map[string]int64{"spins": 0, "gb": 0, "days": 0, "wallet": 0, "discount": 0}
	rows, err := s.ro.QueryContext(ctx, `SELECT kind, COUNT(*), COALESCE(SUM(value),0) FROM wheel_spins WHERE seller=? AND day=? GROUP BY kind`, seller, tehranDay(s.now().Unix()))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n, v int64
		if rows.Scan(&k, &n, &v) == nil {
			out["spins"] += n
			if k == "discount" {
				out[k] += n
			} else {
				out[k] += v
			}
		}
	}
	return out
}
