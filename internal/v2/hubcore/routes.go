package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// Multi-node inbounds. An inbound is one shared profile (protocol, transport,
// security, keys, clients) placed on any number of nodes. Each placement
// (route) has its own listen IP, a Direct port and an optional separate
// Tunnel port, and its own list of hosts: the addresses clients are given,
// each with optional SNI/Host/Path/ALPN/fingerprint overrides. Dark Swap
// relays point at a destination route. Direct, Tunnel and every Swap are
// separate Xray inbound tags, so each path's traffic is counted on its own.

const routeSchema = `
CREATE TABLE IF NOT EXISTS inbound_routes (
  tag TEXT NOT NULL REFERENCES inbounds(tag) ON DELETE CASCADE,
  node_id TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  listen_ip TEXT NOT NULL DEFAULT '',
  direct_port INTEGER NOT NULL CHECK(direct_port BETWEEN 1 AND 65535),
  tunnel_port INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(tag, node_id)
);
CREATE TABLE IF NOT EXISTS inbound_hosts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  tag TEXT NOT NULL REFERENCES inbounds(tag) ON DELETE CASCADE,
  node_id TEXT NOT NULL,
  path TEXT NOT NULL DEFAULT 'direct' CHECK(path IN ('direct','tunnel')),
  sort INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  remark TEXT NOT NULL DEFAULT '',
  address TEXT NOT NULL,
  port INTEGER NOT NULL CHECK(port BETWEEN 1 AND 65535),
  sni TEXT NOT NULL DEFAULT '',
  host TEXT NOT NULL DEFAULT '',
  path_override TEXT NOT NULL DEFAULT '',
  alpn TEXT NOT NULL DEFAULT '',
  fingerprint TEXT NOT NULL DEFAULT '',
  allow_insecure INTEGER NOT NULL DEFAULT 0,
  exclude TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS inbound_hosts_tag ON inbound_hosts(tag, node_id, path, sort);
CREATE TABLE IF NOT EXISTS route_traffic (
  tag TEXT NOT NULL,
  node_id TEXT NOT NULL,
  day TEXT NOT NULL,
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(tag, node_id, day)
);
`

const tunnelSuffix = "__t"

// Host is one address handed to clients for a route.
type Host struct {
	Path          string   `json:"path"` // direct | tunnel
	Enabled       bool     `json:"enabled"`
	Remark        string   `json:"remark"`
	Address       string   `json:"address"`
	Port          int      `json:"port"`
	SNI           string   `json:"sni"`
	Host          string   `json:"host"`
	PathOverride  string   `json:"path_override"`
	ALPN          string   `json:"alpn"`
	Fingerprint   string   `json:"fingerprint"`
	AllowInsecure bool     `json:"allow_insecure"`
	Exclude       []string `json:"exclude"`
}

// Route is an inbound's placement on one node.
type Route struct {
	NodeID     string `json:"node_id"`
	ListenIP   string `json:"listen_ip"`
	DirectPort int    `json:"direct_port"`
	TunnelPort int    `json:"tunnel_port"`
	Hosts      []Host `json:"hosts"`
	// Read-only traffic, bytes: today and total, per path.
	DirectToday int64 `json:"direct_today"`
	DirectTotal int64 `json:"direct_total"`
	TunnelToday int64 `json:"tunnel_today"`
	TunnelTotal int64 `json:"tunnel_total"`
}

func (s *Store) ensureRouteSchema() error {
	if _, err := s.db.Exec(routeSchema); err != nil {
		return err
	}
	for _, c := range []struct{ table, col, def string }{
		{"nodes", "public_ip", "TEXT NOT NULL DEFAULT ''"},
		{"swaps", "dest_node", "TEXT NOT NULL DEFAULT ''"},
		{"swaps", "target_address", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumn(c.table, c.col, c.def); err != nil {
			return err
		}
	}
	return s.migrateRoutes()
}

// migrateRoutes gives every single-node inbound a route and turns its
// subscription endpoints into hosts. It only touches inbounds with no route.
func (s *Store) migrateRoutes() error {
	ctx := context.Background()
	rows, err := s.db.QueryContext(ctx, `SELECT tag,node_id,template,meta FROM inbounds WHERE node_id<>'' AND tag NOT IN (SELECT tag FROM inbound_routes)`)
	if err != nil {
		return err
	}
	type legacy struct{ tag, node, tpl, meta string }
	var list []legacy
	for rows.Next() {
		var l legacy
		if err := rows.Scan(&l.tag, &l.node, &l.tpl, &l.meta); err != nil {
			rows.Close()
			return err
		}
		list = append(list, l)
	}
	rows.Close()
	for _, l := range list {
		var x int
		if s.db.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id=?`, l.node).Scan(&x) != nil {
			continue
		}
		if err := s.legacyRoute(ctx, s.db, l.tag, l.node, []byte(l.tpl), []byte(l.meta)); err != nil {
			return err
		}
	}
	return nil
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// legacyRoute creates a node's route from the template port and, if the
// inbound has no hosts yet, hosts from the meta endpoints.
func (s *Store) legacyRoute(ctx context.Context, q execer, tag, node string, tpl, meta []byte) error {
	var t struct {
		Port   json.Number `json:"port"`
		Listen string      `json:"listen"`
	}
	_ = json.Unmarshal(tpl, &t)
	port, _ := strconv.Atoi(t.Port.String())
	if port < 1 || port > 65535 {
		return nil
	}
	listen := t.Listen
	if listen == "0.0.0.0" || listen == "::" {
		listen = ""
	}
	if _, err := q.ExecContext(ctx, `INSERT OR IGNORE INTO inbound_routes(tag,node_id,listen_ip,direct_port) VALUES(?,?,?,?)`, tag, node, listen, port); err != nil {
		return err
	}
	var n int
	_ = q.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbound_hosts WHERE tag=?`, tag).Scan(&n)
	if n > 0 {
		return nil
	}
	var m sub.Meta
	_ = json.Unmarshal(meta, &m)
	for i, ep := range m.Endpoints {
		path := "tunnel"
		if ep.Name == "" || strings.EqualFold(ep.Name, "direct") {
			path = "direct"
		}
		if _, err := q.ExecContext(ctx, `INSERT INTO inbound_hosts(tag,node_id,path,sort,remark,address,port) VALUES(?,?,?,?,?,?,?)`,
			tag, node, path, i, ep.Name, ep.Address, ep.Port); err != nil {
			return err
		}
	}
	return nil
}

// Routes returns an inbound's placements with hosts and per-path traffic.
func (s *Store) Routes(ctx context.Context, tag string) ([]Route, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT node_id,listen_ip,direct_port,tunnel_port FROM inbound_routes WHERE tag=? ORDER BY node_id`, tag)
	if err != nil {
		return nil, err
	}
	var out []Route
	for rows.Next() {
		var r Route
		if err := rows.Scan(&r.NodeID, &r.ListenIP, &r.DirectPort, &r.TunnelPort); err != nil {
			rows.Close()
			return nil, err
		}
		r.Hosts = []Host{}
		out = append(out, r)
	}
	rows.Close()
	hosts, err := s.hostsOf(ctx, tag)
	if err != nil {
		return nil, err
	}
	day := s.now().UTC().Format("2006-01-02")
	for i := range out {
		for _, h := range hosts[out[i].NodeID] {
			out[i].Hosts = append(out[i].Hosts, h)
		}
		traffic := func(t string) (today, total int64) {
			_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN day=? THEN up+down END),0),COALESCE(SUM(up+down),0)
FROM route_traffic WHERE tag=? AND node_id=?`, day, t, out[i].NodeID).Scan(&today, &total)
			return
		}
		out[i].DirectToday, out[i].DirectTotal = traffic(tag)
		out[i].TunnelToday, out[i].TunnelTotal = traffic(tag + tunnelSuffix)
	}
	if out == nil {
		out = []Route{}
	}
	return out, nil
}

func (s *Store) hostsOf(ctx context.Context, tag string) (map[string][]Host, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT node_id,path,enabled,remark,address,port,sni,host,path_override,alpn,fingerprint,allow_insecure,exclude
FROM inbound_hosts WHERE tag=? ORDER BY node_id, CASE path WHEN 'direct' THEN 0 ELSE 1 END, sort, id`, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]Host{}
	for rows.Next() {
		var node, ex string
		var h Host
		if err := rows.Scan(&node, &h.Path, &h.Enabled, &h.Remark, &h.Address, &h.Port, &h.SNI, &h.Host, &h.PathOverride, &h.ALPN, &h.Fingerprint, &h.AllowInsecure, &ex); err != nil {
			return nil, err
		}
		h.Exclude = splitTags(ex)
		out[node] = append(out[node], h)
	}
	return out, rows.Err()
}

// usedPorts lists ports taken on a node by every inbound route and swap
// relay, except the inbound being replaced.
func usedPorts(ctx context.Context, tx *sql.Tx, node, exceptTag, exceptSwap string) (map[int]string, error) {
	used := map[int]string{apiPort: "the stats API"}
	rows, err := tx.QueryContext(ctx, `SELECT tag,direct_port,tunnel_port FROM inbound_routes WHERE node_id=? AND tag<>?`, node, exceptTag)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tag string
		var d, t int
		if err := rows.Scan(&tag, &d, &t); err != nil {
			rows.Close()
			return nil, err
		}
		used[d] = "inbound " + tag
		if t > 0 {
			used[t] = "inbound " + tag + " (tunnel)"
		}
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, `SELECT id,source_port FROM swaps WHERE source_node=? AND id<>?`, node, exceptSwap)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var p int
		if err := rows.Scan(&id, &p); err != nil {
			return nil, err
		}
		used[p] = "Dark Swap " + id
	}
	return used, rows.Err()
}

var formatNames = map[string]bool{"links": true, "singbox": true, "clash": true}

// PutRoutes replaces an inbound's placements and hosts in one transaction.
func (s *Store) PutRoutes(ctx context.Context, tag string, routes []Route) error {
	if len(routes) == 0 {
		return fmt.Errorf("%w: select at least one node", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var x int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM inbounds WHERE tag=?`, tag).Scan(&x); err != nil {
		return ErrNotFound
	}
	seen := map[string]bool{}
	for _, r := range routes {
		if seen[r.NodeID] {
			return fmt.Errorf("%w: node %s is listed twice", ErrInvalid, r.NodeID)
		}
		seen[r.NodeID] = true
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id=?`, r.NodeID).Scan(&x); err != nil {
			return fmt.Errorf("%w: unknown node %s", ErrInvalid, r.NodeID)
		}
		if r.ListenIP != "" && net.ParseIP(r.ListenIP) == nil {
			return fmt.Errorf("%w: %s: listen IP %q is not an IP address", ErrInvalid, r.NodeID, r.ListenIP)
		}
		if r.DirectPort < 1 || r.DirectPort > 65535 || r.TunnelPort < 0 || r.TunnelPort > 65535 || r.TunnelPort == r.DirectPort {
			return fmt.Errorf("%w: %s: Direct needs a port, and Tunnel (if used) a different one", ErrInvalid, r.NodeID)
		}
		used, err := usedPorts(ctx, tx, r.NodeID, tag, "")
		if err != nil {
			return err
		}
		for _, p := range []int{r.DirectPort, r.TunnelPort} {
			if who, taken := used[p]; p > 0 && taken {
				return fmt.Errorf("%w: %s: port %d is already used by %s", ErrInvalid, r.NodeID, p, who)
			}
		}
		for _, h := range r.Hosts {
			if h.Path != "direct" && h.Path != "tunnel" || strings.TrimSpace(h.Address) == "" || h.Port < 1 || h.Port > 65535 || len(h.Remark) > 120 {
				return fmt.Errorf("%w: %s: every host needs an address and a port", ErrInvalid, r.NodeID)
			}
			for _, f := range h.Exclude {
				if !formatNames[f] {
					return fmt.Errorf("%w: unknown format %q", ErrInvalid, f)
				}
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM inbound_routes WHERE tag=?`, tag); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM inbound_hosts WHERE tag=?`, tag); err != nil {
		return err
	}
	sort.Slice(routes, func(i, j int) bool { return routes[i].NodeID < routes[j].NodeID })
	for _, r := range routes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_routes(tag,node_id,listen_ip,direct_port,tunnel_port) VALUES(?,?,?,?,?)`,
			tag, r.NodeID, r.ListenIP, r.DirectPort, r.TunnelPort); err != nil {
			return err
		}
		for i, h := range r.Hosts {
			if _, err := tx.ExecContext(ctx, `INSERT INTO inbound_hosts(tag,node_id,path,sort,enabled,remark,address,port,sni,host,path_override,alpn,fingerprint,allow_insecure,exclude)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, tag, r.NodeID, h.Path, i, h.Enabled, h.Remark, strings.TrimSpace(h.Address), h.Port, h.SNI, h.Host, h.PathOverride,
				h.ALPN, h.Fingerprint, h.AllowInsecure, strings.Join(h.Exclude, ",")); err != nil {
				return err
			}
		}
	}
	// Keep the legacy single-node column pointing at a real placement.
	if _, err := tx.ExecContext(ctx, `UPDATE inbounds SET node_id=? WHERE tag=?`, routes[0].NodeID, tag); err != nil {
		return err
	}
	// A swap whose destination route disappeared is paused, not deleted.
	if _, err := tx.ExecContext(ctx, `UPDATE swaps SET enabled=0 WHERE inbound_tag=? AND dest_node NOT IN (SELECT node_id FROM inbound_routes WHERE tag=?)`, tag, tag); err != nil {
		return err
	}
	return tx.Commit()
}

// routeEndpoints turns an inbound's enabled hosts (all nodes) into
// subscription endpoints.
func (s *Store) routeEndpoints(ctx context.Context, tag string) ([]sub.Endpoint, error) {
	hosts, err := s.hostsOf(ctx, tag)
	if err != nil {
		return nil, err
	}
	nodes := make([]string, 0, len(hosts))
	for n := range hosts {
		nodes = append(nodes, n)
	}
	sort.Strings(nodes)
	var out []sub.Endpoint
	for _, n := range nodes {
		var x int
		if s.ro.QueryRowContext(ctx, `SELECT 1 FROM inbound_routes WHERE tag=? AND node_id=?`, tag, n).Scan(&x) != nil {
			continue
		}
		for _, h := range hosts[n] {
			if !h.Enabled {
				continue
			}
			name := h.Remark
			if name == "" {
				name = n
				if h.Path == "tunnel" {
					name += " Tunnel"
				}
			}
			out = append(out, sub.Endpoint{Name: name, Address: h.Address, Port: h.Port, SNI: h.SNI, Host: h.Host, Path: h.PathOverride,
				ALPN: h.ALPN, Fingerprint: h.Fingerprint, AllowInsecure: h.AllowInsecure, Exclude: h.Exclude})
		}
	}
	return out, nil
}

// NodeAddress records the address a node syncs from (its public IP unless
// it sits behind NAT), used as the default Dark Swap target.
func (s *Store) NodeAddress(ctx context.Context, nodeID, remote string) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	if net.ParseIP(host) == nil {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE nodes SET public_ip=? WHERE id=? AND public_ip<>?`, host, nodeID, host)
}

// ComposeInbound creates a preset profile and places it on the given nodes.
func (s *Store) ComposeInbound(ctx context.Context, p PresetRequest, routes []Route) error {
	if len(routes) == 0 {
		return fmt.Errorf("%w: select at least one node", ErrInvalid)
	}
	var x int
	if s.ro.QueryRowContext(ctx, `SELECT 1 FROM inbounds WHERE tag=?`, p.Tag).Scan(&x) == nil {
		return fmt.Errorf("%w: an inbound named %s already exists", ErrInvalid, p.Tag)
	}
	p.NodeID, p.Port = routes[0].NodeID, routes[0].DirectPort
	if len(p.Endpoints) == 0 {
		p.Endpoints = []sub.Endpoint{{Name: "Direct", Address: "0.0.0.0", Port: p.Port}} // replaced by the routes' hosts below
	}
	if err := s.CreatePreset(ctx, p); err != nil {
		return err
	}
	if err := s.PutRoutes(ctx, p.Tag, routes); err != nil {
		_ = s.DeleteInbound(ctx, p.Tag)
		return err
	}
	return nil
}
