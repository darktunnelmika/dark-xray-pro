package hubcore

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// Shapes of the external API (partner and reseller). They are snake_case
// and stable: the panel's own API may change with the panel, these do not.

// APIPolicy is a reseller's rules as the external API shows and takes them.
// Sale prices are the owner's business and never appear here.
type APIPolicy struct {
	MaxClients     int64    `json:"max_clients"`   // 0: no cap
	MaxDays        int64    `json:"max_days"`      // longest term; 0: no cap
	MaxClientGB    int64    `json:"max_client_gb"` // largest volume per client; 0: no cap
	AllowUnlimited bool     `json:"allow_unlimited"`
	CanDelete      bool     `json:"can_delete"`
	RefundUnused   bool     `json:"refund_unused"`
	Inbounds       []string `json:"inbounds"` // empty: every inbound
	DenyCreate     bool     `json:"deny_create"`
	DenyRenew      bool     `json:"deny_renew"`
	DenyEdit       bool     `json:"deny_edit"`
}

func apiPolicyOf(p RepPolicy) APIPolicy {
	in := p.Inbounds
	if in == nil {
		in = []string{}
	}
	return APIPolicy{MaxClients: p.MaxClients, MaxDays: p.MaxDays, MaxClientGB: p.MaxClientGB, AllowUnlimited: p.AllowUnlimited,
		CanDelete: p.CanDelete, RefundUnused: p.RefundUnused, Inbounds: in, DenyCreate: p.DenyCreate, DenyRenew: p.DenyRenew, DenyEdit: p.DenyEdit}
}

// APIPolicyIn changes some of a policy; fields left out stay.
type APIPolicyIn struct {
	MaxClients     *int64    `json:"max_clients"`
	MaxDays        *int64    `json:"max_days"`
	MaxClientGB    *int64    `json:"max_client_gb"`
	AllowUnlimited *bool     `json:"allow_unlimited"`
	CanDelete      *bool     `json:"can_delete"`
	RefundUnused   *bool     `json:"refund_unused"`
	Inbounds       *[]string `json:"inbounds"`
	DenyCreate     *bool     `json:"deny_create"`
	DenyRenew      *bool     `json:"deny_renew"`
	DenyEdit       *bool     `json:"deny_edit"`
}

// over applies the change to p.
func (in *APIPolicyIn) over(p RepPolicy) (RepPolicy, error) {
	if in == nil {
		return p, nil
	}
	for _, v := range []*int64{in.MaxClients, in.MaxDays, in.MaxClientGB} {
		if v != nil && (*v < 0 || *v > 1_000_000) {
			return p, fmt.Errorf("%w: policy limits are 0 (no cap) or more", ErrInvalid)
		}
	}
	setI := func(dst *int64, v *int64) {
		if v != nil {
			*dst = *v
		}
	}
	setB := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	setI(&p.MaxClients, in.MaxClients)
	setI(&p.MaxDays, in.MaxDays)
	setI(&p.MaxClientGB, in.MaxClientGB)
	setB(&p.AllowUnlimited, in.AllowUnlimited)
	setB(&p.CanDelete, in.CanDelete)
	setB(&p.RefundUnused, in.RefundUnused)
	setB(&p.DenyCreate, in.DenyCreate)
	setB(&p.DenyRenew, in.DenyRenew)
	setB(&p.DenyEdit, in.DenyEdit)
	if in.Inbounds != nil {
		p.Inbounds = slices.Clone(*in.Inbounds)
	}
	return p, nil
}

// clampPolicy keeps p within ceiling: a cap is never raised or removed, a
// permission never given, a ban never lifted, an inbound never added.
// With sent nil (a new panel) every field is clamped and the prices come
// from the ceiling. With sent set (a change) only the fields that were
// sent are clamped and the prices stay, so whatever the owner changed on
// the panel since is kept.
func (s *Store) clampPolicy(ctx context.Context, p, ceiling RepPolicy, sent *APIPolicyIn) (RepPolicy, error) {
	all := sent == nil
	if all {
		sent = &APIPolicyIn{}
	}
	clamp := func(v, limit int64) int64 {
		if limit > 0 && (v == 0 || v > limit) {
			return limit
		}
		return v
	}
	if all || sent.MaxClients != nil {
		p.MaxClients = clamp(p.MaxClients, ceiling.MaxClients)
	}
	if all || sent.MaxDays != nil {
		p.MaxDays = clamp(p.MaxDays, ceiling.MaxDays)
	}
	if all || sent.MaxClientGB != nil {
		p.MaxClientGB = clamp(p.MaxClientGB, ceiling.MaxClientGB)
	}
	if all || sent.AllowUnlimited != nil {
		p.AllowUnlimited = p.AllowUnlimited && ceiling.AllowUnlimited
	}
	if all || sent.CanDelete != nil {
		p.CanDelete = p.CanDelete && ceiling.CanDelete
	}
	if all || sent.RefundUnused != nil {
		p.RefundUnused = p.RefundUnused && ceiling.RefundUnused
	}
	if all || sent.DenyCreate != nil {
		p.DenyCreate = p.DenyCreate || ceiling.DenyCreate
	}
	if all || sent.DenyRenew != nil {
		p.DenyRenew = p.DenyRenew || ceiling.DenyRenew
	}
	if all || sent.DenyEdit != nil {
		p.DenyEdit = p.DenyEdit || ceiling.DenyEdit
	}
	if all {
		p.PriceGB, p.PriceUnit = ceiling.PriceGB, ceiling.PriceUnit
	}
	if !all && sent.Inbounds == nil {
		return p, nil
	}
	tags := []string{}
	for _, t := range p.Inbounds {
		if t = strings.TrimSpace(t); t != "" && !slices.Contains(tags, t) {
			tags = append(tags, t)
		}
	}
	if len(ceiling.Inbounds) > 0 {
		kept := []string{}
		for _, t := range tags {
			if slices.Contains(ceiling.Inbounds, t) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			kept = slices.Clone(ceiling.Inbounds)
		}
		tags = kept
	} else {
		for _, t := range tags {
			var x int
			if s.ro.QueryRowContext(ctx, `SELECT 1 FROM inbounds WHERE tag=?`, t).Scan(&x) != nil {
				return p, fmt.Errorf("%w: inbound %q does not exist", ErrInvalid, t)
			}
		}
	}
	p.Inbounds = tags
	return p, nil
}

// APILedgerRow is one credit movement.
type APILedgerRow struct {
	ID       int64   `json:"id"`
	Unix     int64   `json:"unix"`
	Panel    string  `json:"panel,omitempty"` // the reseller, in lists over several
	Kind     string  `json:"kind"`            // adjust | charge | refund | delete | transfer
	Actor    string  `json:"actor"`
	Client   string  `json:"client,omitempty"`
	VolumeGB float64 `json:"volume_gb"` // + added, - taken
	Units    int64   `json:"units"`
	Note     string  `json:"note,omitempty"`
}

func apiLedgerRow(e LedgerEntry, actor string) APILedgerRow {
	return APILedgerRow{ID: e.ID, Unix: e.Unix, Panel: e.RepID, Kind: e.Kind, Actor: actor, Client: e.ClientEmail,
		VolumeGB: roundGB(float64(e.VolumeDelta) / gibF), Units: e.UnitsDelta, Note: e.Note}
}

func roundGB(v float64) float64 {
	if v < 0 {
		return -roundGB(-v)
	}
	return float64(int64(v*1000+0.5)) / 1000
}

// APIClient is a customer of a reseller.
type APIClient struct {
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Status      string   `json:"status"`    // active | on_hold | expired | limited | disabled
	Plan        string   `json:"plan"`      // volume | unlimited
	VolumeGB    float64  `json:"volume_gb"` // -1: unlimited
	UsedGB      float64  `json:"used_gb"`
	ExpiresUnix int64    `json:"expires_unix"` // 0: none, or not started yet (on hold)
	TermDays    int64    `json:"term_days"`    // on hold: the term that starts at the first connection
	MaxIPs      int      `json:"max_ips"`      // 0: no limit
	DeviceLimit int      `json:"device_limit"` // 0: no limit
	Online      bool     `json:"online"`
	FirstSeen   int64    `json:"first_seen"`
	LastSeen    int64    `json:"last_seen"`
	Inbounds    []string `json:"inbounds,omitempty"`
	Note        string   `json:"note"`
	Trial       bool     `json:"trial,omitempty"`
	// Only for the client's own seller:
	SubURL string `json:"sub_url,omitempty"`
}

func apiClientOf(c ClientView, now int64) APIClient {
	vol := float64(-1)
	if c.VolumeBytes >= 0 {
		vol = roundGB(float64(c.VolumeBytes) / gibF)
	}
	plan := c.Plan
	if plan == "" {
		plan = "volume"
		if c.VolumeBytes < 0 {
			plan = "unlimited"
		}
	}
	out := APIClient{Name: c.Email, Enabled: c.Enabled, Status: pgStatus(c, now), Plan: plan, VolumeGB: vol,
		UsedGB: roundGB(float64(c.Usage.Up+c.Usage.Down) / gibF), ExpiresUnix: c.EffectiveUntil, MaxIPs: c.MaxIPs,
		DeviceLimit: c.HWIDLimit, Online: c.Online, FirstSeen: c.Usage.FirstSeen, LastSeen: c.Usage.LastSeen, Note: c.Note, Trial: c.Trial}
	if c.ExpiresUnix == 0 && c.DurationSeconds > 0 {
		out.TermDays = (c.DurationSeconds + 86399) / 86400
	}
	return out
}

// clientMatches filters clients by status and a search text.
func clientMatches(c APIClient, status, q string) bool {
	if status != "" && c.Status != status {
		return false
	}
	return q == "" || strings.Contains(strings.ToLower(c.Name+" "+c.Note), strings.ToLower(q))
}
