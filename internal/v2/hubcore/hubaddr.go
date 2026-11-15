package hubcore

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// The address nodes use to reach the HUB. Set it to a domain (or a new
// server's address) and every node is told, checks that the address
// serves this HUB's certificate, and moves onto it. Nodes check the
// certificate by a fixed name ("dark-hub2", always in the HUB
// certificate), so the HUB can move to a new IP with a full backup.

const (
	hubAddrMinAgent = "v2.11.0"
	defaultHubName  = "dark-hub2"
)

type NodeHubAddr struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"`
}

// NodeHubAddr returns the address set for nodes, if any.
func (s *Store) NodeHubAddr(ctx context.Context) (NodeHubAddr, bool) {
	u := s.Setting(ctx, "node_hub_url")
	if u == "" {
		return NodeHubAddr{}, false
	}
	return NodeHubAddr{URL: u, Name: s.Setting(ctx, "node_hub_name")}, true
}

type HubMove struct {
	Node   string `json:"node"`
	Task   string `json:"task,omitempty"`
	Reason string `json:"reason,omitempty"` // why it was skipped
}

// MoveNodes sets the HUB address for nodes and tells every node that can
// move. Each node checks the address before switching, so a wrong one
// leaves it where it is (the task result says why).
func (s *Store) MoveNodes(ctx context.Context, raw, name string) ([]HubMove, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.Port() == "" || u.Path != "" {
		return nil, fmt.Errorf("%w: the address must look like https://hub.example.com:28443", ErrInvalid)
	}
	if name = strings.TrimSpace(name); name == "" {
		name = defaultHubName
	}
	if err := s.SetSetting(ctx, "node_hub_url", raw); err != nil {
		return nil, err
	}
	if err := s.SetSetting(ctx, "node_hub_name", name); err != nil {
		return nil, err
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT id,agent_version FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	type nv struct{ id, ver string }
	var nodes []nv
	for rows.Next() {
		var n nv
		if rows.Scan(&n.id, &n.ver) == nil {
			nodes = append(nodes, n)
		}
	}
	rows.Close()
	out := []HubMove{}
	for _, n := range nodes {
		if semver(n.ver, hubAddrMinAgent) < 0 {
			out = append(out, HubMove{Node: n.id, Reason: "update the node to " + hubAddrMinAgent + " first"})
			continue
		}
		id, err := s.QueueTask(ctx, n.id, wire.TaskSetHub, NodeHubAddr{URL: raw, Name: name})
		if err != nil {
			out = append(out, HubMove{Node: n.id, Reason: err.Error()})
			continue
		}
		out = append(out, HubMove{Node: n.id, Task: id})
	}
	return out, nil
}
