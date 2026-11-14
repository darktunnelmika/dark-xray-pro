package hubcore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// StartOutboundTest asks a node to check one of its outbounds (WARP, a
// chained server, direct…) with a throwaway Xray: latency and exit IP.
func (s *Store) StartOutboundTest(ctx context.Context, nodeID, tag string) (string, error) {
	cfg, err := s.RenderNode(ctx, nodeID)
	if err != nil {
		return "", err
	}
	var c struct {
		Outbounds []json.RawMessage `json:"outbounds"`
	}
	if err := json.Unmarshal(cfg, &c); err != nil {
		return "", err
	}
	found := false
	for _, o := range c.Outbounds {
		var x struct {
			Tag string `json:"tag"`
		}
		if json.Unmarshal(o, &x) == nil && x.Tag == tag {
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("%w: node %s has no outbound %q", ErrInvalid, nodeID, tag)
	}
	return s.QueueTask(ctx, nodeID, wire.TaskOutboundTest, map[string]any{"outbounds": c.Outbounds, "tag": tag})
}
