package hubcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// Configs of many clients at once, and a version of the configs.
//
// A shop that serves its own subscription (for example from a bridge server
// at home) reads its clients' configs in batches with POST /bulk/links and
// keeps them. The configs change in two ways:
//   - one client changes (credentials, inbounds, on/off, usage in the
//     names): its rev in /changes moves;
//   - the owner changes an inbound, a host or a route, which changes the
//     configs of every client on it: links_version moves.
//
// links_version is a short hash of everything a config is made from that is
// not the client itself: the inbounds the shop's clients are on (template,
// meta, on/off), their hosts and swap endpoints, the shop's look and the
// client-side routing. A shop compares it with the version it last saw and,
// when it differs, reads every client's configs again.

const tenantLinksMax = 500

// TenantLinksItem is one client's configs and usage, as its subscription
// would give them.
type TenantLinksItem struct {
	Name        string       `json:"name"`
	OK          bool         `json:"ok"`
	Error       string       `json:"error,omitempty"`
	ExternalRef string       `json:"external_ref,omitempty"`
	Status      string       `json:"status,omitempty"`
	Enabled     bool         `json:"enabled"`
	Rev         int64        `json:"rev,omitempty"`
	SubToken    string       `json:"sub_token,omitempty"`
	SubURL      string       `json:"subscription_url,omitempty"`
	Userinfo    string       `json:"userinfo,omitempty"` // the Subscription-Userinfo header value
	UpBytes     int64        `json:"up_bytes"`
	DownBytes   int64        `json:"down_bytes"`
	TotalBytes  int64        `json:"total_bytes"` // 0 = unlimited, as in Subscription-Userinfo
	ExpireUnix  int64        `json:"expire_unix"` // effective expiry, 0 = none
	SubFetched  int64        `json:"sub_fetched_unix"`
	Links       []ClientLink `json:"links"` // empty when the client is off, expired or out of volume
}

// TenantBulkLinks reads up to 500 clients' configs. Unknown names come back
// with ok=false and error "not_found"; they never fail the call.
func (s *Store) TenantBulkLinks(ctx context.Context, tenant string, names []string) ([]TenantLinksItem, error) {
	if len(names) == 0 {
		return []TenantLinksItem{}, nil
	}
	if len(names) > tenantLinksMax {
		return nil, fmt.Errorf("%w: at most %d names per call", ErrInvalid, tenantLinksMax)
	}
	var emails []any
	seen := map[string]bool{}
	for _, n := range names {
		if tenantNameRE.MatchString(n) && !seen[n] {
			seen[n] = true
			emails = append(emails, tenantEmail(tenant, n))
		}
	}
	found := map[string]TenantClient{}
	if len(emails) > 0 {
		args := append([]any{tenantRep(tenant)}, emails...)
		list, err := s.clientViews(ctx, "c.rep_id=? AND c.email IN (?"+strings.Repeat(",?", len(emails)-1)+")", args...)
		if err != nil {
			return nil, err
		}
		for _, tc := range s.tenantClientViews(ctx, tenant, list) {
			found[tc.Name] = tc
		}
	}
	out := make([]TenantLinksItem, 0, len(names))
	for _, n := range names {
		tc, ok := found[n]
		if !ok {
			out = append(out, TenantLinksItem{Name: n, Error: "not_found", Links: []ClientLink{}})
			continue
		}
		links, err := s.ClientLinks(ctx, tenantEmail(tenant, n))
		if err != nil {
			out = append(out, TenantLinksItem{Name: n, Error: err.Error(), Links: []ClientLink{}})
			continue
		}
		if links == nil {
			links = []ClientLink{}
		}
		total := tc.VolumeBytes
		if total < 0 {
			total = 0
		}
		out = append(out, TenantLinksItem{
			Name: n, OK: true, ExternalRef: tc.ExternalRef, Status: tc.Status, Enabled: tc.Enabled, Rev: tc.Rev,
			SubToken: tc.SubToken, SubFetched: tc.SubFetched, UpBytes: tc.UpBytes, DownBytes: tc.DownBytes, TotalBytes: total, ExpireUnix: tc.EffectiveExpiry,
			Userinfo: sub.Userinfo(tc.UpBytes, tc.DownBytes, total, tc.EffectiveExpiry), Links: links,
		})
	}
	return out, nil
}

// linksVersionCache keeps each shop's links_version for a few seconds: the
// feed is read often and the version is the same between owner edits.
var linksVersionCache = struct {
	sync.Mutex
	m map[*Store]map[string]linksVersionEntry
}{m: map[*Store]map[string]linksVersionEntry{}}

type linksVersionEntry struct {
	v  string
	at time.Time
}

const linksVersionTTL = 15 * time.Second

// TenantLinksVersion is the shop's current links_version (see above).
func (s *Store) TenantLinksVersion(ctx context.Context, tenant string) (string, error) {
	linksVersionCache.Lock()
	if e, ok := linksVersionCache.m[s][tenant]; ok && s.now().Sub(e.at) < linksVersionTTL && s.now().Sub(e.at) >= 0 {
		linksVersionCache.Unlock()
		return e.v, nil
	}
	linksVersionCache.Unlock()
	v, err := s.tenantLinksVersion(ctx, tenant)
	if err != nil {
		return "", err
	}
	linksVersionCache.Lock()
	if linksVersionCache.m[s] == nil {
		linksVersionCache.m[s] = map[string]linksVersionEntry{}
	}
	linksVersionCache.m[s][tenant] = linksVersionEntry{v: v, at: s.now()}
	linksVersionCache.Unlock()
	return v, nil
}

func (s *Store) tenantLinksVersion(ctx context.Context, tenant string) (string, error) {
	h := sha256.New()
	put := func(v any) {
		b, _ := json.Marshal(v)
		h.Write(b)
		h.Write([]byte{0})
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT DISTINCT ci.tag FROM client_inbounds ci JOIN clients c ON c.email=ci.email WHERE c.rep_id=? ORDER BY 1`, tenantRep(tenant))
	if err != nil {
		return "", err
	}
	var tags []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return "", err
		}
		tags = append(tags, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", err
	}
	for _, tag := range tags {
		var tpl, meta string
		var on bool
		if s.ro.QueryRowContext(ctx, `SELECT template,meta,enabled FROM inbounds WHERE tag=?`, tag).Scan(&tpl, &meta, &on) != nil {
			put([]any{tag, "missing"})
			continue
		}
		eps, err := s.routeEndpoints(ctx, tag)
		if err != nil {
			return "", err
		}
		extra, err := s.swapEndpoints(ctx, tag)
		if err != nil {
			return "", err
		}
		put([]any{tag, tpl, meta, on, eps, extra})
	}
	put(s.SubSettings(ctx))
	put(s.TenantBrand(ctx, tenant))
	put(s.Routing(ctx).ClientIranDirect)
	return hex.EncodeToString(h.Sum(nil))[:16], nil
}
