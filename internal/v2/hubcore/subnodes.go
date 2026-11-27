package hubcore

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// The customer's own choice of countries: a service's subscription can
// leave out some of the servers (nodes) it would list. Only the list in the
// app changes — access is the same, and a node added later shows up unless
// the customer hid it.

func subHideKey(email string) string { return "sub_hide:" + email }

// SubHidden is the set of nodes the service's subscription leaves out.
func (s *Store) SubHidden(ctx context.Context, email string) map[string]bool {
	v := s.Setting(ctx, subHideKey(email))
	if v == "" {
		return nil
	}
	out := map[string]bool{}
	for _, n := range strings.Split(v, ",") {
		if n = strings.TrimSpace(n); n != "" {
			out[n] = true
		}
	}
	return out
}

// SubNode is one server a service can list, for the customer's picker.
type SubNode struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Flag   string `json:"flag"`
	Online bool   `json:"online"`
	Hidden bool   `json:"hidden"`
}

var flagByCode = map[string]string{"uk": "🇬🇧", "gb": "🇬🇧", "de": "🇩🇪", "pl": "🇵🇱", "us": "🇺🇸", "nl": "🇳🇱", "fi": "🇫🇮", "tr": "🇹🇷", "fr": "🇫🇷",
	"bg": "🇧🇬", "az": "🇦🇿", "ae": "🇦🇪", "am": "🇦🇲", "at": "🇦🇹", "ca": "🇨🇦", "ch": "🇨🇭", "cz": "🇨🇿", "dk": "🇩🇰", "es": "🇪🇸", "ee": "🇪🇪",
	"hk": "🇭🇰", "hu": "🇭🇺", "ie": "🇮🇪", "in": "🇮🇳", "it": "🇮🇹", "jp": "🇯🇵", "kz": "🇰🇿", "lt": "🇱🇹", "lv": "🇱🇻", "md": "🇲🇩", "no": "🇳🇴",
	"ro": "🇷🇴", "rs": "🇷🇸", "ru": "🇷🇺", "se": "🇸🇪", "sg": "🇸🇬", "ua": "🇺🇦", "ge": "🇬🇪", "ir": "🇮🇷", "qa": "🇶🇦", "sa": "🇸🇦", "om": "🇴🇲"}

var flagByWord = []struct{ word, flag string }{
	{"germany", "🇩🇪"}, {"آلمان", "🇩🇪"}, {"المان", "🇩🇪"}, {"netherland", "🇳🇱"}, {"holland", "🇳🇱"}, {"هلند", "🇳🇱"}, {"turkey", "🇹🇷"}, {"ترکیه", "🇹🇷"},
	{"finland", "🇫🇮"}, {"فنلاند", "🇫🇮"}, {"france", "🇫🇷"}, {"فرانسه", "🇫🇷"}, {"poland", "🇵🇱"}, {"لهستان", "🇵🇱"}, {"usa", "🇺🇸"}, {"america", "🇺🇸"},
	{"آمریکا", "🇺🇸"}, {"امریکا", "🇺🇸"}, {"uk", "🇬🇧"}, {"england", "🇬🇧"}, {"انگلیس", "🇬🇧"}, {"bulgaria", "🇧🇬"}, {"بلغارستان", "🇧🇬"},
	{"azerbaijan", "🇦🇿"}, {"آذربایجان", "🇦🇿"}, {"armenia", "🇦🇲"}, {"ارمنستان", "🇦🇲"}, {"uae", "🇦🇪"}, {"dubai", "🇦🇪"}, {"امارات", "🇦🇪"},
	{"sweden", "🇸🇪"}, {"سوئد", "🇸🇪"}, {"russia", "🇷🇺"}, {"روسیه", "🇷🇺"}, {"canada", "🇨🇦"}, {"کانادا", "🇨🇦"}, {"iran", "🇮🇷"}, {"ایران", "🇮🇷"},
}

// nodeFlag guesses a node's flag from its name, else from its id ("de-1").
func nodeFlag(id, name string) string {
	low := strings.ToLower(name)
	for _, f := range flagByWord {
		if strings.Contains(low, f.word) {
			return f.flag
		}
	}
	code := strings.ToLower(id)
	if len(code) >= 2 {
		if f, ok := flagByCode[code[:2]]; ok && (len(code) == 2 || code[2] < 'a' || code[2] > 'z') {
			return f
		}
	}
	return "🌐"
}

// SubNodes lists the servers the service's subscription can list.
func (s *Store) SubNodes(ctx context.Context, email string) ([]SubNode, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT ci.tag FROM client_inbounds ci JOIN inbounds i ON i.tag=ci.tag WHERE ci.email=? AND i.enabled=1`, email)
	if err != nil {
		return nil, err
	}
	var tags []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil {
			tags = append(tags, t)
		}
	}
	rows.Close()
	seen := map[string]bool{}
	for _, t := range tags {
		eps, _ := s.routeEndpoints(ctx, t)
		extra, _ := s.swapEndpoints(ctx, t)
		for _, e := range append(eps, extra...) {
			if e.Node != "" {
				seen[e.Node] = true
			}
		}
	}
	hidden := s.SubHidden(ctx, email)
	nodes, err := s.Nodes(ctx)
	if err != nil {
		return nil, err
	}
	now := s.now().Unix()
	out := []SubNode{}
	for _, n := range nodes {
		if !seen[n.ID] {
			continue
		}
		out = append(out, SubNode{ID: n.ID, Name: n.Name, Flag: nodeFlag(n.ID, n.Name), Online: now-n.LastSeen < 180, Hidden: hidden[n.ID]})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Online && !out[j].Online })
	return out, nil
}

// PutSubHidden saves the servers to leave out; at least one stays.
func (s *Store) PutSubHidden(ctx context.Context, email string, hide []string) error {
	nodes, err := s.SubNodes(ctx, email)
	if err != nil {
		return err
	}
	var keep []string
	left := 0
	for _, n := range nodes {
		if slices.Contains(hide, n.ID) {
			keep = append(keep, n.ID)
		} else {
			left++
		}
	}
	if left == 0 && len(nodes) > 0 {
		return fmt.Errorf("%w: keep at least one server", ErrInvalid)
	}
	return s.SetSetting(ctx, subHideKey(email), strings.Join(keep, ","))
}

// hideNodes drops the hidden servers' addresses; an inbound left with none
// of its addresses is dropped too.
func hideNodes(ins []sub.Inbound, hidden map[string]bool) []sub.Inbound {
	if len(hidden) == 0 {
		return ins
	}
	out := ins[:0]
	for _, in := range ins {
		had := len(in.Meta.Endpoints)
		var eps []sub.Endpoint
		for _, e := range in.Meta.Endpoints {
			if !hidden[e.Node] {
				eps = append(eps, e)
			}
		}
		if had > 0 && len(eps) == 0 {
			continue
		}
		in.Meta.Endpoints = eps
		out = append(out, in)
	}
	return out
}
