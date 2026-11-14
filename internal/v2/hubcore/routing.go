package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
)

// Routing is the owner's outbound and routing policy, applied to every
// node's Xray config. Safe defaults are on from the start: private
// networks and the node's own address are unreachable through the proxy,
// BitTorrent is blocked (abuse complaints get servers suspended) and
// sniffing only informs routing instead of rewriting destinations.
//
// Rules and outbounds can be limited to some nodes. Rules that need geo
// files (geoip:/geosite:) are left out of a node's config until that node
// reports having the files, because Xray refuses to start without them.
type Routing struct {
	BlockTorrent     bool            `json:"block_torrent"`
	BlockPrivate     bool            `json:"block_private"`
	BlockAds         bool            `json:"block_ads"`
	Iran             string          `json:"iran"`            // "" (nothing), "block", or an outbound tag
	RouteOnly        bool            `json:"route_only"`      // sniffing for routing only
	DomainStrategy   string          `json:"domain_strategy"` // freedom: AsIs, UseIP, UseIPv4, UseIPv6
	DNS              []string        `json:"dns"`
	ClientIranDirect bool            `json:"client_iran_direct"` // app profiles send Iranian traffic direct
	Outbounds        []RouteOutbound `json:"outbounds"`
	Balancers        []RouteBalancer `json:"balancers"`
	Rules            []RouteRule     `json:"rules"`
}

// RouteOutbound is an extra Xray outbound (WARP, a chained server, ...).
type RouteOutbound struct {
	Tag    string          `json:"tag"`
	Remark string          `json:"remark,omitempty"`
	Kind   string          `json:"kind"` // warp, custom
	Nodes  []string        `json:"nodes,omitempty"`
	Config json.RawMessage `json:"config"` // an Xray outbound object; tag is set from Tag
}

// RouteBalancer spreads traffic over several outbounds.
type RouteBalancer struct {
	Tag       string   `json:"tag"`
	Outbounds []string `json:"outbounds"`
	Strategy  string   `json:"strategy,omitempty"` // random, roundRobin, leastPing, leastLoad
}

// RouteRule sends matching traffic to an outbound or balancer.
type RouteRule struct {
	Enabled   bool     `json:"enabled"`
	Remark    string   `json:"remark,omitempty"`
	Domains   []string `json:"domains,omitempty"`
	IPs       []string `json:"ips,omitempty"`
	Ports     string   `json:"ports,omitempty"`
	Protocols []string `json:"protocols,omitempty"` // http, tls, quic, bittorrent
	Network   string   `json:"network,omitempty"`   // tcp, udp, tcp,udp
	Users     []string `json:"users,omitempty"`
	Inbounds  []string `json:"inbounds,omitempty"`
	Outbound  string   `json:"outbound"` // direct, block, an outbound or balancer tag
	Nodes     []string `json:"nodes,omitempty"`
}

// privateNets are never reachable through the proxy (the node's own
// services, cloud metadata, LANs).
var privateNets = []string{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
	"192.0.0.0/24", "192.168.0.0/16", "198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4", "::1/128", "fc00::/7", "fe80::/10"}

const routingKey = "routing"

func defaultRouting() Routing {
	return Routing{BlockTorrent: true, BlockPrivate: true, RouteOnly: true, DomainStrategy: "AsIs", ClientIranDirect: true}
}

// Routing returns the stored policy, or the safe defaults.
func (s *Store) Routing(ctx context.Context) Routing {
	r := defaultRouting()
	if raw := s.Setting(ctx, routingKey); raw != "" {
		_ = json.Unmarshal([]byte(raw), &r)
	}
	return r
}

var outTagRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

// PutRouting validates and stores the policy. Every node's config is
// rendered with it first, so a policy that cannot render is refused.
func (s *Store) PutRouting(ctx context.Context, r Routing) error {
	if err := validateRouting(r); err != nil {
		return err
	}
	raw, _ := json.Marshal(r)
	old := s.Setting(ctx, routingKey)
	if err := s.SetSetting(ctx, routingKey, string(raw)); err != nil {
		return err
	}
	if err := s.checkAllNodes(ctx); err != nil {
		_ = s.SetSetting(ctx, routingKey, old)
		return err
	}
	return nil
}

func validateRouting(r Routing) error {
	bad := func(f string, a ...any) error { return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(f, a...)) }
	tags := map[string]bool{"direct": true, "block": true}
	for _, o := range r.Outbounds {
		if !outTagRE.MatchString(o.Tag) || tags[o.Tag] || o.Tag == apiTag {
			return bad("outbound tag %q", o.Tag)
		}
		var m map[string]any
		if json.Unmarshal(o.Config, &m) != nil || m["protocol"] == nil {
			return bad("outbound %s: config must be an Xray outbound with a protocol", o.Tag)
		}
		tags[o.Tag] = true
	}
	for _, b := range r.Balancers {
		if !outTagRE.MatchString(b.Tag) || tags[b.Tag] {
			return bad("balancer tag %q", b.Tag)
		}
		if len(b.Outbounds) == 0 {
			return bad("balancer %s has no outbounds", b.Tag)
		}
		for _, o := range b.Outbounds {
			if !tags[o] {
				return bad("balancer %s: unknown outbound %s", b.Tag, o)
			}
		}
		switch b.Strategy {
		case "", "random", "roundRobin", "leastPing", "leastLoad":
		default:
			return bad("balancer %s strategy", b.Tag)
		}
		tags[b.Tag] = true
	}
	if r.Iran != "" && r.Iran != "block" && !tags[r.Iran] {
		return bad("iran: unknown outbound %s", r.Iran)
	}
	switch r.DomainStrategy {
	case "", "AsIs", "UseIP", "UseIPv4", "UseIPv6", "ForceIP", "ForceIPv4", "ForceIPv6":
	default:
		return bad("domain strategy")
	}
	for _, d := range r.DNS {
		if strings.TrimSpace(d) == "" || strings.ContainsAny(d, " \"") {
			return bad("dns server %q", d)
		}
	}
	for i, ru := range r.Rules {
		if !tags[ru.Outbound] {
			return bad("rule %d: unknown outbound %q", i+1, ru.Outbound)
		}
		if len(ru.Domains)+len(ru.IPs)+len(ru.Protocols)+len(ru.Users)+len(ru.Inbounds) == 0 && ru.Ports == "" && ru.Network == "" {
			return bad("rule %d matches nothing", i+1)
		}
		for _, ip := range ru.IPs {
			if !strings.HasPrefix(ip, "geoip:") && !strings.HasPrefix(ip, "ext:") && net.ParseIP(ip) == nil {
				if _, _, err := net.ParseCIDR(ip); err != nil {
					return bad("rule %d: ip %q", i+1, ip)
				}
			}
		}
		for _, p := range ru.Protocols {
			switch p {
			case "http", "tls", "quic", "bittorrent":
			default:
				return bad("rule %d: protocol %q", i+1, p)
			}
		}
		switch ru.Network {
		case "", "tcp", "udp", "tcp,udp":
		default:
			return bad("rule %d: network", i+1)
		}
	}
	return nil
}

// checkAllNodes renders every node so a broken policy is caught at save.
func (s *Store) checkAllNodes(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM nodes`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		cfg, err := s.RenderNode(ctx, id)
		if err == nil {
			err = s.testConfig(ctx, cfg)
		}
		if err != nil {
			return fmt.Errorf("%w: node %s: %v", ErrInvalid, id, err)
		}
	}
	return nil
}

func needsGeo(list []string) bool {
	for _, v := range list {
		if strings.HasPrefix(v, "geoip:") || strings.HasPrefix(v, "geosite:") || strings.HasPrefix(v, "ext:") {
			return true
		}
	}
	return false
}

func onNode(nodes []string, id string) bool {
	if len(nodes) == 0 {
		return true
	}
	for _, n := range nodes {
		if n == id {
			return true
		}
	}
	return false
}

// nodeRouting builds the outbounds, routing and dns sections for a node.
// swapTags are Dark Swap forwarders: they relay to other nodes' public
// addresses and are exempt from the private and own-address blocks.
func (s *Store) nodeRouting(ctx context.Context, nodeID string, swapTags []string) (outbounds []any, routing map[string]any, dns map[string]any, routeOnly bool) {
	r := s.Routing(ctx)
	var publicIP, assets string
	_ = s.ro.QueryRowContext(ctx, `SELECT public_ip,agent_assets FROM nodes WHERE id=?`, nodeID).Scan(&publicIP, &assets)
	hasGeo := strings.Contains(assets, "geoip.dat") && strings.Contains(assets, "geosite.dat")

	freedom := map[string]any{"tag": "direct", "protocol": "freedom"}
	if r.DomainStrategy != "" && r.DomainStrategy != "AsIs" {
		freedom["settings"] = map[string]any{"domainStrategy": r.DomainStrategy}
	}
	outbounds = []any{freedom, map[string]any{"tag": "block", "protocol": "blackhole"}}
	present := map[string]bool{"direct": true, "block": true}
	for _, o := range r.Outbounds {
		if !onNode(o.Nodes, nodeID) {
			continue
		}
		var m map[string]any
		if json.Unmarshal(o.Config, &m) != nil {
			continue
		}
		m["tag"] = o.Tag
		outbounds = append(outbounds, m)
		present[o.Tag] = true
	}
	var balancers []any
	for _, b := range r.Balancers {
		var sel []string
		for _, o := range b.Outbounds {
			if present[o] {
				sel = append(sel, o)
			}
		}
		if len(sel) == 0 {
			continue
		}
		bal := map[string]any{"tag": b.Tag, "selector": sel}
		if b.Strategy != "" {
			bal["strategy"] = map[string]any{"type": b.Strategy}
		}
		balancers = append(balancers, bal)
		present[b.Tag] = true
	}
	target := func(tag string) (map[string]any, bool) {
		if !present[tag] {
			return nil, false
		}
		for _, b := range r.Balancers {
			if b.Tag == tag {
				return map[string]any{"balancerTag": tag}, true
			}
		}
		return map[string]any{"outboundTag": tag}, true
	}
	rules := []any{map[string]any{"type": "field", "inboundTag": []string{apiTag}, "outboundTag": apiTag}}
	add := func(m map[string]any, to string) {
		t, ok := target(to)
		if !ok {
			return
		}
		m["type"] = "field"
		for k, v := range t {
			m[k] = v
		}
		rules = append(rules, m)
	}
	if len(swapTags) > 0 {
		add(map[string]any{"inboundTag": swapTags}, "direct")
	}
	if r.BlockPrivate {
		ips := append([]string{}, privateNets...)
		if ip := net.ParseIP(publicIP); ip != nil {
			ips = append(ips, publicIP)
		}
		add(map[string]any{"ip": ips}, "block")
	}
	if r.BlockTorrent {
		add(map[string]any{"protocol": []string{"bittorrent"}}, "block")
	}
	for _, ru := range r.Rules {
		if !ru.Enabled || !onNode(ru.Nodes, nodeID) {
			continue
		}
		if !hasGeo && (needsGeo(ru.Domains) || needsGeo(ru.IPs)) {
			continue
		}
		m := map[string]any{}
		if len(ru.Domains) > 0 {
			m["domain"] = ru.Domains
		}
		if len(ru.IPs) > 0 {
			m["ip"] = ru.IPs
		}
		if ru.Ports != "" {
			m["port"] = ru.Ports
		}
		if len(ru.Protocols) > 0 {
			m["protocol"] = ru.Protocols
		}
		if ru.Network != "" {
			m["network"] = ru.Network
		}
		if len(ru.Users) > 0 {
			m["user"] = ru.Users
		}
		if len(ru.Inbounds) > 0 {
			var tags []string
			for _, t := range ru.Inbounds {
				tags = append(tags, t, t+tunnelSuffix)
			}
			m["inboundTag"] = tags
		}
		add(m, ru.Outbound)
	}
	if hasGeo && r.BlockAds {
		add(map[string]any{"domain": []string{"geosite:category-ads-all"}}, "block")
	}
	if hasGeo && r.Iran != "" {
		to := r.Iran
		add(map[string]any{"domain": []string{"geosite:ir"}}, to)
		add(map[string]any{"ip": []string{"geoip:ir"}}, to)
	}
	routing = map[string]any{"domainStrategy": "AsIs", "rules": rules}
	if needsIPMatch(rules) {
		routing["domainStrategy"] = "IPIfNonMatch"
	}
	if len(balancers) > 0 {
		routing["balancers"] = balancers
	}
	if len(r.DNS) > 0 {
		servers := make([]any, len(r.DNS))
		for i, d := range r.DNS {
			servers[i] = d
		}
		dns = map[string]any{"servers": servers}
	}
	return outbounds, routing, dns, r.RouteOnly
}

// needsIPMatch: with geoip rules for domains (sniffed), let Xray resolve
// domains that match no domain rule so IP rules can still apply.
func needsIPMatch(rules []any) bool {
	for _, x := range rules {
		m, _ := x.(map[string]any)
		ips, _ := m["ip"].([]string)
		for _, ip := range ips {
			if strings.HasPrefix(ip, "geoip:") && ip != "geoip:private" {
				return true
			}
		}
	}
	return false
}

// RoutingStatus tells the panel which nodes can use geo rules yet.
type RoutingStatus struct {
	NodesWithoutGeo []string `json:"nodes_without_geo"`
}

func (s *Store) RoutingStatus(ctx context.Context) RoutingStatus {
	st := RoutingStatus{NodesWithoutGeo: []string{}}
	rows, err := s.ro.QueryContext(ctx, `SELECT id,agent_assets FROM nodes ORDER BY id`)
	if err != nil {
		return st
	}
	defer rows.Close()
	for rows.Next() {
		var id, a string
		if rows.Scan(&id, &a) == nil && !(strings.Contains(a, "geoip.dat") && strings.Contains(a, "geosite.dat")) {
			st.NodesWithoutGeo = append(st.NodesWithoutGeo, id)
		}
	}
	sort.Strings(st.NodesWithoutGeo)
	return st
}
