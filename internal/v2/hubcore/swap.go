package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// Dark Swap relays an inbound through a second node: the source node runs a
// dedicated TCP forwarder (dokodemo-door, no clients) on its own port that
// points at the exit inbound's public address. Clients connect to the entry
// address (usually an Iran server forwarding to the source port), and
// authentication and accounting stay on the exit node, so no user or quota
// is created on the source. A Swap only adds a subscription endpoint and a
// forwarder: Direct and Tunnel endpoints of the inbound are untouched.

const swapSchema = `
CREATE TABLE IF NOT EXISTS swaps (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL DEFAULT 'DARK SWAP',
  inbound_tag TEXT NOT NULL REFERENCES inbounds(tag) ON DELETE CASCADE,
  source_node TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  source_port INTEGER NOT NULL CHECK(source_port BETWEEN 1 AND 65535),
  entry_address TEXT NOT NULL,
  entry_port INTEGER NOT NULL CHECK(entry_port BETWEEN 1 AND 65535),
  enabled INTEGER NOT NULL DEFAULT 1,
  created_unix INTEGER NOT NULL,
  UNIQUE(source_node, source_port)
);
`

// Swap is one relay route.
type Swap struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	InboundTag   string `json:"inbound_tag"`
	DestNode     string `json:"dest_node"` // the inbound's node the relay reaches ("" = its first node)
	SourceNode   string `json:"source_node"`
	SourcePort   int    `json:"source_port"`
	EntryAddress string `json:"entry_address"`
	EntryPort    int    `json:"entry_port"`
	// TargetAddress overrides where the relay connects (default: the
	// destination node's public IP as seen by the HUB).
	TargetAddress string `json:"target_address"`
	Enabled       bool   `json:"enabled"`
	// Read-only, filled by Swaps: where the forwarder sends traffic.
	Target string `json:"target,omitempty"`
}

func (s *Store) ensureSwapSchema() error {
	_, err := s.db.Exec(swapSchema)
	return err
}

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// swapDest resolves where a relay forwards: the destination route's Tunnel
// port if it has one (so relayed traffic is counted with Tunnel), else its
// Direct port, at the target address, the node's public IP, or its first
// Direct host.
func swapDest(ctx context.Context, q rowQuerier, tag, dest, target string) (string, int, error) {
	var direct, tunnel int
	if err := q.QueryRowContext(ctx, `SELECT direct_port,tunnel_port FROM inbound_routes WHERE tag=? AND node_id=?`, tag, dest).Scan(&direct, &tunnel); err != nil {
		return "", 0, fmt.Errorf("%w: the inbound is not placed on node %s", ErrInvalid, dest)
	}
	port := direct
	if tunnel > 0 {
		port = tunnel
	}
	addr := target
	if addr == "" {
		_ = q.QueryRowContext(ctx, `SELECT public_ip FROM nodes WHERE id=?`, dest).Scan(&addr)
	}
	if addr == "" {
		_ = q.QueryRowContext(ctx, `SELECT address FROM inbound_hosts WHERE tag=? AND node_id=? AND path='direct' ORDER BY sort,id LIMIT 1`, tag, dest).Scan(&addr)
	}
	if addr == "" {
		return "", 0, fmt.Errorf("%w: node %s has no known address yet; set the relay target address", ErrInvalid, dest)
	}
	return addr, port, nil
}

// PutSwap creates or updates a route after checking it can work.
func (s *Store) PutSwap(ctx context.Context, w Swap) error {
	if !idRE.MatchString(w.ID) || w.SourcePort < 1 || w.SourcePort > 65535 || w.EntryPort < 1 || w.EntryPort > 65535 || strings.TrimSpace(w.EntryAddress) == "" {
		return ErrInvalid
	}
	if w.Name == "" {
		w.Name = "DARK SWAP"
	}
	if len(w.Name) > 160 {
		return fmt.Errorf("%w: name is too long", ErrInvalid)
	}
	if w.TargetAddress != "" && net.ParseIP(w.TargetAddress) == nil && !hostRE.MatchString(w.TargetAddress) {
		return fmt.Errorf("%w: target address", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var first, tpl string
	if err := tx.QueryRowContext(ctx, `SELECT node_id,template FROM inbounds WHERE tag=?`, w.InboundTag).Scan(&first, &tpl); err != nil {
		return fmt.Errorf("%w: unknown inbound", ErrInvalid)
	}
	if w.DestNode == "" {
		w.DestNode = first
	}
	if w.DestNode == w.SourceNode {
		return fmt.Errorf("%w: the relay node must differ from the destination node", ErrInvalid)
	}
	var x int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id=?`, w.SourceNode).Scan(&x); err != nil {
		return fmt.Errorf("%w: unknown relay node", ErrInvalid)
	}
	if _, _, err := swapDest(ctx, tx, w.InboundTag, w.DestNode, w.TargetAddress); err != nil {
		return err
	}
	var t struct {
		Stream struct {
			Network string `json:"network"`
		} `json:"streamSettings"`
	}
	_ = json.Unmarshal([]byte(tpl), &t)
	if t.Stream.Network == "kcp" || t.Stream.Network == "mkcp" || t.Stream.Network == "quic" {
		return fmt.Errorf("%w: Dark Swap relays TCP transports only", ErrInvalid)
	}
	used, err := usedPorts(ctx, tx, w.SourceNode, "", w.ID)
	if err != nil {
		return err
	}
	if who, taken := used[w.SourcePort]; taken {
		return fmt.Errorf("%w: port %d on %s is already used by %s", ErrInvalid, w.SourcePort, w.SourceNode, who)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO swaps(id,name,inbound_tag,source_node,source_port,entry_address,entry_port,enabled,created_unix,dest_node,target_address)
VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, inbound_tag=excluded.inbound_tag,
source_node=excluded.source_node, source_port=excluded.source_port, entry_address=excluded.entry_address,
entry_port=excluded.entry_port, enabled=excluded.enabled, dest_node=excluded.dest_node, target_address=excluded.target_address`,
		w.ID, w.Name, w.InboundTag, w.SourceNode, w.SourcePort, strings.TrimSpace(w.EntryAddress), w.EntryPort, w.Enabled, s.now().Unix(), w.DestNode, w.TargetAddress)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return fmt.Errorf("%w: port %d is already used by another swap on %s", ErrInvalid, w.SourcePort, w.SourceNode)
		}
		return err
	}
	return tx.Commit()
}

func (s *Store) DeleteSwap(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, `DELETE FROM swaps WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Swaps lists routes, optionally for one inbound.
func (s *Store) Swaps(ctx context.Context, inboundTag string) ([]Swap, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT w.id,w.name,w.inbound_tag,w.source_node,w.source_port,w.entry_address,w.entry_port,w.enabled,
CASE WHEN w.dest_node='' THEN i.node_id ELSE w.dest_node END,w.target_address
FROM swaps w JOIN inbounds i ON i.tag=w.inbound_tag WHERE ?='' OR w.inbound_tag=? ORDER BY w.inbound_tag,w.id`, inboundTag, inboundTag)
	if err != nil {
		return nil, err
	}
	out := []Swap{}
	for rows.Next() {
		var w Swap
		if err := rows.Scan(&w.ID, &w.Name, &w.InboundTag, &w.SourceNode, &w.SourcePort, &w.EntryAddress, &w.EntryPort, &w.Enabled, &w.DestNode, &w.TargetAddress); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, w)
	}
	rows.Close()
	for i := range out {
		if a, p, err := swapDest(ctx, s.ro, out[i].InboundTag, out[i].DestNode, out[i].TargetAddress); err == nil {
			out[i].Target = net.JoinHostPort(a, strconv.Itoa(p))
		}
	}
	return out, nil
}

// swapForwarders renders the relay inbounds a node must run.
func (s *Store) swapForwarders(ctx context.Context, nodeID string) ([]any, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT w.id,w.source_port,w.inbound_tag,CASE WHEN w.dest_node='' THEN i.node_id ELSE w.dest_node END,w.target_address
FROM swaps w JOIN inbounds i ON i.tag=w.inbound_tag WHERE w.source_node=? AND w.enabled=1 AND i.enabled=1 ORDER BY w.id`, nodeID)
	if err != nil {
		return nil, err
	}
	type fw struct {
		id, tag, dest, target string
		port                  int
	}
	var list []fw
	for rows.Next() {
		var f fw
		if err := rows.Scan(&f.id, &f.port, &f.tag, &f.dest, &f.target); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, f)
	}
	rows.Close()
	var out []any
	for _, f := range list {
		addr, tport, err := swapDest(ctx, s.ro, f.tag, f.dest, f.target)
		if err != nil {
			continue // destination unplaced or unknown: the route pauses
		}
		out = append(out, map[string]any{
			"tag": "swap-" + f.id, "listen": "0.0.0.0", "port": f.port, "protocol": "dokodemo-door",
			"settings": map[string]any{"address": addr, "port": tport, "network": "tcp"},
		})
	}
	return out, nil
}

// swapEndpoints adds each enabled route of an inbound to its subscription.
func (s *Store) swapEndpoints(ctx context.Context, tag string) ([]sub.Endpoint, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT name,entry_address,entry_port FROM swaps WHERE inbound_tag=? AND enabled=1 ORDER BY id`, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sub.Endpoint
	for rows.Next() {
		var e sub.Endpoint
		if err := rows.Scan(&e.Name, &e.Address, &e.Port); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

var hostRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
