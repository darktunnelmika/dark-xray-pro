package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Subscription domains: the owner lists every address that reaches the
// HUB's subscription port (all of them answer /sub/<token> the same way, so
// a client's link works on any of them) and picks the default one that new
// links are built with. A reseller's links use its own pick (the brand's
// address), else the default. A filtered domain is moved away from with one
// call: every reseller on it, and the default, go to another domain; links
// keep their token, so the same customer link works on the new domain.
// Connected shops keep their own addresses and are not touched.

// SubDomain is one subscription address.
type SubDomain struct {
	URL     string `json:"url"`     // https://sub.example.com[:port]
	Label   string `json:"label"`   // a name for the list
	Retired bool   `json:"retired"` // still answers, but no new links are built with it
	Default bool   `json:"default"` // new links (owner and resellers without a pick)
	Reps    int    `json:"reps"`    // read only: resellers whose links use it
}

const maxSubDomains = 20

func cleanSubURL(raw string) (string, error) {
	v := strings.TrimRight(strings.TrimSpace(raw), "/")
	if v != "" && !strings.Contains(v, "://") {
		v = "https://" + v
	}
	u, err := url.Parse(v)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return "", fmt.Errorf("%w: the address looks like https://sub.example.com (a port is fine)", ErrInvalid)
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// SubDomains lists the addresses with how many resellers use each.
func (s *Store) SubDomains(ctx context.Context) []SubDomain {
	var list []SubDomain
	_ = json.Unmarshal([]byte(s.Setting(ctx, "sub_domains")), &list)
	if list == nil {
		list = []SubDomain{}
	}
	use := map[string]int{}
	if reps, err := s.Reps(ctx); err == nil {
		for _, r := range reps {
			if b := s.RepBrand(ctx, r.ID).BaseURL; b != "" {
				use[strings.ToLower(b)]++
			}
		}
	}
	for i := range list {
		list[i].Reps = use[strings.ToLower(list[i].URL)]
	}
	return list
}

// PutSubDomains replaces the list (at most one default, never a retired one).
func (s *Store) PutSubDomains(ctx context.Context, in []SubDomain) error {
	if len(in) > maxSubDomains {
		return fmt.Errorf("%w: at most %d addresses", ErrInvalid, maxSubDomains)
	}
	seen := map[string]bool{}
	defaults := 0
	for i := range in {
		u, err := cleanSubURL(in[i].URL)
		if err != nil {
			return err
		}
		if seen[u] {
			return fmt.Errorf("%w: %s is listed twice", ErrInvalid, u)
		}
		seen[u] = true
		in[i].URL, in[i].Label, in[i].Reps = u, strings.TrimSpace(in[i].Label), 0
		if len(in[i].Label) > 60 {
			return fmt.Errorf("%w: label too long", ErrInvalid)
		}
		if in[i].Default {
			if in[i].Retired {
				return fmt.Errorf("%w: a retired address cannot be the default", ErrInvalid)
			}
			defaults++
		}
	}
	if defaults > 1 {
		return fmt.Errorf("%w: only one default address", ErrInvalid)
	}
	b, _ := json.Marshal(in)
	return s.SetSetting(ctx, "sub_domains", string(b))
}

// defaultBase is the default subscription address, else def (the HUB's
// configured public address).
func (s *Store) defaultBase(ctx context.Context, def string) string {
	var list []SubDomain
	_ = json.Unmarshal([]byte(s.Setting(ctx, "sub_domains")), &list)
	for _, d := range list {
		if d.Default && !d.Retired {
			return d.URL
		}
	}
	return def
}

// DefaultSubBase is the address new links are built with when a reseller
// has no own address (def: the HUB's configured public address).
func (s *Store) DefaultSubBase(ctx context.Context, def string) string {
	return s.defaultBase(ctx, def)
}

// SubDomainChoices are the addresses a reseller may pick (not retired).
func (s *Store) SubDomainChoices(ctx context.Context) []SubDomain {
	out := []SubDomain{}
	for _, d := range s.SubDomains(ctx) {
		if !d.Retired {
			d.Reps = 0
			out = append(out, d)
		}
	}
	return out
}

// PickRepDomain sets a reseller's link address to one of the listed
// addresses ("" = back to the default).
func (s *Store) PickRepDomain(ctx context.Context, repID, raw string) error {
	b := s.RepBrand(ctx, repID)
	if raw == "" {
		b.BaseURL = ""
		return s.PutRepBrand(ctx, repID, b)
	}
	u, err := cleanSubURL(raw)
	if err != nil {
		return err
	}
	ok := false
	for _, d := range s.SubDomainChoices(ctx) {
		ok = ok || d.URL == u
	}
	if !ok {
		return fmt.Errorf("%w: pick one of the panel's subscription addresses", ErrInvalid)
	}
	b.BaseURL = u
	return s.PutRepBrand(ctx, repID, b)
}

// MoveSubDomain moves every reseller whose links use from (and the default,
// if it is from) to another listed address, and retires from. It returns
// how many resellers moved.
func (s *Store) MoveSubDomain(ctx context.Context, fromRaw, toRaw string) (int, error) {
	from, err := cleanSubURL(fromRaw)
	if err != nil {
		return 0, err
	}
	to, err := cleanSubURL(toRaw)
	if err != nil {
		return 0, err
	}
	if from == to {
		return 0, fmt.Errorf("%w: pick another address", ErrInvalid)
	}
	list := s.SubDomains(ctx)
	hasTo := false
	for _, d := range list {
		hasTo = hasTo || (d.URL == to && !d.Retired)
	}
	if !hasTo {
		return 0, fmt.Errorf("%w: the new address must be in the list (and not retired)", ErrInvalid)
	}
	reps, err := s.Reps(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, r := range reps {
		if _, shop := tenantOfRep(r.ID); shop {
			continue // connected shops keep their own addresses
		}
		b := s.RepBrand(ctx, r.ID)
		if strings.EqualFold(b.BaseURL, from) {
			b.BaseURL = to
			if err := s.PutRepBrand(ctx, r.ID, b); err != nil {
				return n, err
			}
			n++
		}
	}
	for i := range list {
		switch list[i].URL {
		case from:
			if list[i].Default {
				list[i].Default = false
				for j := range list {
					if list[j].URL == to {
						list[j].Default = true
					}
				}
			}
			list[i].Retired = true
		}
	}
	return n, s.PutSubDomains(ctx, list)
}
