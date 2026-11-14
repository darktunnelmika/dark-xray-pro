package hubcore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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
	SourceNode   string `json:"source_node"`
	SourcePort   int    `json:"source_port"`
	EntryAddress string `json:"entry_address"`
	EntryPort    int    `json:"entry_port"`
	Enabled      bool   `json:"enabled"`
	// Read-only, filled by Swaps: where the forwarder sends traffic.
	Target string `json:"target,omitempty"`
}

func (s *Store) ensureSwapSchema() error {
	_, err := s.db.Exec(swapSchema)
	return err
}

// swapTarget is the exit inbound's public address: its Direct endpoint, or
// the first endpoint when none is named Direct.
func swapTarget(metaJSON string) (string, int, error) {
	var m sub.Meta
	if err := json.Unmarshal([]byte(metaJSON), &m); err != nil || len(m.Endpoints) == 0 {
		return "", 0, fmt.Errorf("%w: the inbound needs a Direct endpoint before it can be swapped", ErrInvalid)
	}
	ep := m.Endpoints[0]
	for _, e := range m.Endpoints {
		if e.Name == "Direct" {
			ep = e
			break
		}
	}
	return ep.Address, ep.Port, nil
}

// PutSwap creates or updates a route after checking it can work.
func (s *Store) PutSwap(ctx context.Context, w Swap) error {
	if !idRE.MatchString(w.ID) || w.SourcePort < 1 || w.SourcePort > 65535 || w.EntryPort < 1 || w.EntryPort > 65535 || w.EntryAddress == "" {
		return ErrInvalid
	}
	if w.Name == "" {
		w.Name = "DARK SWAP"
	}
	if len(w.Name) > 160 {
		return fmt.Errorf("%w: name is too long", ErrInvalid)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exitNode, tpl, meta string
	if err := tx.QueryRowContext(ctx, `SELECT node_id,template,meta FROM inbounds WHERE tag=?`, w.InboundTag).Scan(&exitNode, &tpl, &meta); err != nil {
		return fmt.Errorf("%w: unknown inbound", ErrInvalid)
	}
	if exitNode == w.SourceNode {
		return fmt.Errorf("%w: the relay node must differ from the inbound's node", ErrInvalid)
	}
	var x int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id=?`, w.SourceNode).Scan(&x); err != nil {
		return fmt.Errorf("%w: unknown relay node", ErrInvalid)
	}
	if _, _, err := swapTarget(meta); err != nil {
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
	// The relay port must not collide with an inbound on the relay node.
	rows, err := tx.QueryContext(ctx, `SELECT template FROM inbounds WHERE node_id=?`, w.SourceNode)
	if err != nil {
		return err
	}
	for rows.Next() {
		var other string
		if err := rows.Scan(&other); err != nil {
			rows.Close()
			return err
		}
		var p struct {
			Port json.Number `json:"port"`
		}
		_ = json.Unmarshal([]byte(other), &p)
		if n, _ := strconv.Atoi(p.Port.String()); n == w.SourcePort {
			rows.Close()
			return fmt.Errorf("%w: port %d is already used by an inbound on %s", ErrInvalid, w.SourcePort, w.SourceNode)
		}
	}
	rows.Close()
	if w.SourcePort == apiPort {
		return fmt.Errorf("%w: port %d is reserved", ErrInvalid, apiPort)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO swaps(id,name,inbound_tag,source_node,source_port,entry_address,entry_port,enabled,created_unix)
VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, inbound_tag=excluded.inbound_tag,
source_node=excluded.source_node, source_port=excluded.source_port, entry_address=excluded.entry_address,
entry_port=excluded.entry_port, enabled=excluded.enabled`,
		w.ID, w.Name, w.InboundTag, w.SourceNode, w.SourcePort, w.EntryAddress, w.EntryPort, w.Enabled, s.now().Unix())
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
	rows, err := s.ro.QueryContext(ctx, `SELECT w.id,w.name,w.inbound_tag,w.source_node,w.source_port,w.entry_address,w.entry_port,w.enabled,i.meta
FROM swaps w JOIN inbounds i ON i.tag=w.inbound_tag WHERE ?='' OR w.inbound_tag=? ORDER BY w.inbound_tag,w.id`, inboundTag, inboundTag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Swap{}
	for rows.Next() {
		var w Swap
		var meta string
		if err := rows.Scan(&w.ID, &w.Name, &w.InboundTag, &w.SourceNode, &w.SourcePort, &w.EntryAddress, &w.EntryPort, &w.Enabled, &meta); err != nil {
			return nil, err
		}
		if a, p, err := swapTarget(meta); err == nil {
			w.Target = a + ":" + strconv.Itoa(p)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// swapForwarders renders the relay inbounds a node must run.
func (s *Store) swapForwarders(ctx context.Context, nodeID string) ([]any, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT w.id,w.source_port,i.meta FROM swaps w JOIN inbounds i ON i.tag=w.inbound_tag
WHERE w.source_node=? AND w.enabled=1 AND i.enabled=1 ORDER BY w.id`, nodeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []any
	for rows.Next() {
		var id, meta string
		var port int
		if err := rows.Scan(&id, &port, &meta); err != nil {
			return nil, err
		}
		addr, tport, err := swapTarget(meta)
		if err != nil {
			continue // the inbound lost its endpoint; the route pauses
		}
		out = append(out, map[string]any{
			"tag": "swap-" + id, "listen": "0.0.0.0", "port": port, "protocol": "dokodemo-door",
			"settings": map[string]any{"address": addr, "port": tport, "network": "tcp"},
		})
	}
	return out, rows.Err()
}

// swapEndpoints adds each enabled route of an inbound to its subscription.
func (s *Store) swapEndpoints(ctx context.Context, tag string) ([]sub.Endpoint, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT name,entry_address,entry_port FROM swaps WHERE inbound_tag=? AND enabled=1 ORDER BY id`, tag)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
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
