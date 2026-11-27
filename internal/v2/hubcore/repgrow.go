package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Growing the reseller tree from Telegram, without the owner doing it by
// hand:
//
//   - invite link: a top-level reseller shares a link to its bot; whoever
//     opens it becomes its sub-seller (no credit until the parent gives some);
//   - reseller request: customers tap "become a reseller" in a bot; the bot's
//     seller accepts or refuses with one tap (the owner's main bot makes a
//     reseller, a reseller's bot a sub-seller);
//   - trial panel: the owner lets each Telegram account open one reseller
//     panel with a little free credit, in the main bot.
//
// Every panel made here has a login with a random password (sent to the new
// reseller once) and the chat as its Telegram owner.

const sellerInviteTTL = 7 * 86400

// NewSellerInvite makes an invite code for parent's sub-sellers (7 days,
// any number of uses).
func (s *Store) NewSellerInvite(ctx context.Context, parent string) (string, error) {
	pp, err := s.parentOf(ctx, s.ro, parent)
	if err != nil {
		return "", ErrNotFound
	}
	if pp != "" {
		return "", fmt.Errorf("%w: sub-sellers cannot open sellers", ErrPolicy)
	}
	code := randomHex(6)
	return code, s.SetSetting(ctx, "rsinv:"+code, parent+"|"+strconv.FormatInt(s.now().Unix()+sellerInviteTTL, 10))
}

// NewPanel is a reseller panel made from Telegram, with its one-time login.
type NewPanel struct {
	RepID    string
	Parent   string
	Username string
	Password string
}

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// freeRepID picks an unused reseller id from a Telegram name.
func (s *Store) freeRepID(ctx context.Context, hint string) string {
	base := strings.Trim(slugRE.ReplaceAllString(strings.ToLower(hint), ""), "-")
	if len(base) < 3 || len(base) > 20 {
		base = "r" + randomHex(3)
	}
	if base[0] < 'a' || base[0] > 'z' {
		base = "r" + base
	}
	id := base
	for i := 0; i < 20 && (!idRE.MatchString(id) || !s.RepNameFree(ctx, id) || s.adminExists(ctx, id)); i++ {
		id = base + randomHex(2)
	}
	return id
}

func (s *Store) adminExists(ctx context.Context, user string) bool {
	var n int
	_ = s.ro.QueryRowContext(ctx, `SELECT COUNT(*) FROM admins WHERE username=?`, user).Scan(&n)
	return n > 0
}

// chatFree tells whether a chat may get a panel here (it does not manage
// one already).
func (s *Store) chatFree(ctx context.Context, chat int64) error {
	if rep, _ := s.TgRepRole(ctx, chat); rep != "" {
		return fmt.Errorf("%w: this Telegram account already manages the panel %s", ErrInvalid, rep)
	}
	return nil
}

// makePanel creates a top-level reseller (parent "") or a sub-seller of
// parent, with a login, credit and the chat as Telegram owner.
func (s *Store) makePanel(ctx context.Context, actor, parent string, chat int64, name, hint string, creditGB float64) (NewPanel, error) {
	if err := s.chatFree(ctx, chat); err != nil {
		return NewPanel{}, err
	}
	id := s.freeRepID(ctx, hint)
	np := NewPanel{RepID: id, Parent: parent, Username: id, Password: randomHex(8)}
	name = strings.TrimSpace(name)
	if name == "" {
		name = id
	}
	if len([]rune(name)) > 60 {
		name = string([]rune(name)[:60])
	}
	if parent == "" {
		if err := s.CreateRep(ctx, actor, RepCreate{ID: id, Name: name, Password: np.Password, CreditGB: creditGB, TgOwners: []int64{chat}}); err != nil {
			return NewPanel{}, err
		}
		return np, nil
	}
	if err := s.PutSeller(ctx, parent, Rep{ID: id, Name: name, Enabled: true}, id, np.Password); err != nil {
		return NewPanel{}, err
	}
	if err := s.SetTgRepOwners(ctx, id, []int64{chat}); err != nil {
		return NewPanel{}, err
	}
	return np, nil
}

// ClaimSellerInvite makes the chat a sub-seller of the invite's reseller.
func (s *Store) ClaimSellerInvite(ctx context.Context, code string, chat int64, name, username string) (NewPanel, error) {
	v := s.Setting(ctx, "rsinv:"+strings.TrimSpace(code))
	parent, expS, ok := strings.Cut(v, "|")
	exp, _ := strconv.ParseInt(expS, 10, 64)
	if !ok || parent == "" || exp < s.now().Unix() {
		return NewPanel{}, ErrNotFound
	}
	var enabled bool
	if s.ro.QueryRowContext(ctx, `SELECT enabled FROM reps WHERE id=? AND parent_id=''`, parent).Scan(&enabled) != nil || !enabled {
		return NewPanel{}, ErrNotFound
	}
	return s.makePanel(ctx, "invite:"+parent, parent, chat, name, firstNonEmpty(username, name), 0)
}

// ---- reseller requests ----

// RepRequestsOn tells whether a seller's bot shows "become a reseller".
func (s *Store) RepRequestsOn(ctx context.Context, seller string) bool {
	if seller != "" && s.IsSubSeller(ctx, seller) {
		return false // a sub-seller cannot open sellers
	}
	return s.Setting(ctx, cardKey("rep_requests", seller)) == "1"
}

func (s *Store) SetRepRequestsOn(ctx context.Context, seller string, on bool) error {
	if seller != "" && s.IsSubSeller(ctx, seller) {
		return fmt.Errorf("%w: sub-sellers cannot open sellers", ErrPolicy)
	}
	v := ""
	if on {
		v = "1"
	}
	return s.SetSetting(ctx, cardKey("rep_requests", seller), v)
}

// RepRequest is a customer asking a seller to become its reseller.
type RepRequest struct {
	Chat     int64  `json:"chat"`
	Name     string `json:"name"`
	Username string `json:"username"`
	Unix     int64  `json:"unix"`
}

func reqKey(seller string, chat int64) string {
	return "rsreq:" + seller + ":" + strconv.FormatInt(chat, 10)
}

// AddRepRequest records a request (one open request per chat and seller).
// It reports false when the chat had already asked.
func (s *Store) AddRepRequest(ctx context.Context, seller string, chat int64, name, username string) (bool, error) {
	if !s.RepRequestsOn(ctx, seller) {
		return false, ErrNotFound
	}
	if err := s.chatFree(ctx, chat); err != nil {
		return false, err
	}
	if s.Setting(ctx, reqKey(seller, chat)) != "" {
		return false, nil
	}
	b, _ := json.Marshal(RepRequest{Chat: chat, Name: name, Username: username, Unix: s.now().Unix()})
	return true, s.SetSetting(ctx, reqKey(seller, chat), string(b))
}

// DecideRepRequest accepts (makes the panel) or refuses a request.
func (s *Store) DecideRepRequest(ctx context.Context, seller string, chat int64, accept bool, actor string) (NewPanel, error) {
	var rq RepRequest
	if json.Unmarshal([]byte(s.Setting(ctx, reqKey(seller, chat))), &rq) != nil || rq.Chat != chat {
		return NewPanel{}, ErrNotFound
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM settings WHERE key=?`, reqKey(seller, chat))
	if !accept {
		return NewPanel{}, nil
	}
	return s.makePanel(ctx, actor, seller, chat, rq.Name, firstNonEmpty(rq.Username, rq.Name), 0)
}

// ---- trial panel ----

// RepTrial is the owner's free trial panel (main bot).
type RepTrial struct {
	On bool    `json:"on"`
	GB float64 `json:"gb"` // free credit, 1–100 GB
}

func (s *Store) RepTrialPolicy(ctx context.Context) RepTrial {
	var t RepTrial
	_ = json.Unmarshal([]byte(s.Setting(ctx, "rep_trial_panel")), &t)
	if t.GB <= 0 {
		t.GB = 5
	}
	return t
}

func (s *Store) PutRepTrialPolicy(ctx context.Context, t RepTrial) error {
	if t.GB < 1 || t.GB > 100 {
		return fmt.Errorf("%w: trial credit: 1 to 100 GB", ErrInvalid)
	}
	b, _ := json.Marshal(t)
	return s.SetSetting(ctx, "rep_trial_panel", string(b))
}

// RepTrialUsed tells whether a chat already opened its trial panel.
func (s *Store) RepTrialUsed(ctx context.Context, chat int64) bool {
	return s.Setting(ctx, "rep_trial_used:"+strconv.FormatInt(chat, 10)) != ""
}

// ClaimRepTrial opens the chat's one trial panel.
func (s *Store) ClaimRepTrial(ctx context.Context, chat int64, name, username string) (NewPanel, error) {
	t := s.RepTrialPolicy(ctx)
	if !t.On {
		return NewPanel{}, ErrNotFound
	}
	if s.RepTrialUsed(ctx, chat) {
		return NewPanel{}, ErrTrialUsed
	}
	np, err := s.makePanel(ctx, "trial", "", chat, name, firstNonEmpty(username, name), t.GB)
	if err != nil {
		return np, err
	}
	_ = s.SetSetting(ctx, "rep_trial_used:"+strconv.FormatInt(chat, 10), np.RepID)
	_ = s.SetSetting(ctx, "rep_trial_of:"+np.RepID, strconv.FormatInt(s.now().Unix(), 10))
	return np, nil
}

// IsTrialPanel tells whether a reseller came from the trial panel.
func (s *Store) IsTrialPanel(ctx context.Context, rep string) bool {
	return s.Setting(ctx, "rep_trial_of:"+rep) != ""
}
