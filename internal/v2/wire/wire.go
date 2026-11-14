// Package wire defines the v2 HUB <-> Node protocol.
//
// Design rule: the data plane never depends on the HUB being reachable.
// A Node keeps running Xray with its last good config and keeps enforcing
// the last limits it received; usage is spooled locally and delivered later.
// Every message is idempotent so a lost response can be retried verbatim.
package wire

import "encoding/json"

const (
	// PathSync is polled by the Node: it reports its state and receives the
	// desired config revision and current per-user limits.
	PathSync = "/api/v2/node/sync"
	// PathUsage delivers one spooled usage batch. The HUB acknowledges the
	// batch sequence; re-delivery of an acknowledged sequence is a no-op.
	PathUsage = "/api/v2/node/usage"

	// HeaderNodeID and HeaderAuthorization authenticate every Node request.
	HeaderNodeID = "X-Dark-Node"
)

// SyncRequest is sent by the Node on every poll.
type SyncRequest struct {
	NodeID         string `json:"node_id"`
	AgentVersion   string `json:"agent_version"`
	ConfigRevision uint64 `json:"config_revision"`
	XrayRunning    bool   `json:"xray_running"`
	XrayRestarts   uint64 `json:"xray_restarts"`
	LastError      string `json:"last_error,omitempty"`
	SpooledBatches int    `json:"spooled_batches"`
	UnixTime       int64  `json:"unix_time"`
}

// SyncResponse carries the desired state. Config is only present when
// ConfigRevision differs from the revision the Node reported.
type SyncResponse struct {
	ConfigRevision uint64          `json:"config_revision"`
	Config         json.RawMessage `json:"config,omitempty"`
	Users          []UserLimit     `json:"users"`
	// UsageSeq: RemainingBytes already accounts for every usage batch from
	// this Node with sequence <= UsageSeq. The Node subtracts only newer
	// traffic, so local enforcement neither double counts nor misses bytes.
	UsageSeq    uint64 `json:"usage_seq"`
	PollSeconds int    `json:"poll_seconds"`
	UnixTime       int64           `json:"unix_time"`
}

// UserLimit is the HUB's view of one client at the time of the sync.
// RemainingBytes < 0 means unlimited volume. ExpiresUnix == 0 means no
// expiry yet (for example: starts on first connection). MaxIPs == 0 means
// no IP limit.
type UserLimit struct {
	Email          string `json:"email"`
	Enabled        bool   `json:"enabled"`
	ExpiresUnix    int64  `json:"expires_unix"`
	RemainingBytes int64  `json:"remaining_bytes"`
	MaxIPs         int    `json:"max_ips"`
}

// UsageBatch is one durable, idempotent delivery from a Node.
type UsageBatch struct {
	NodeID      string        `json:"node_id"`
	Seq         uint64        `json:"seq"`
	FromUnix    int64         `json:"from_unix"`
	ToUnix      int64         `json:"to_unix"`
	Traffic     []UserTraffic `json:"traffic"`
	Online      []UserOnline  `json:"online,omitempty"`
	Disconnects []Disconnect  `json:"disconnects,omitempty"`
}

// UserTraffic is a byte delta since the previous batch.
type UserTraffic struct {
	Email string `json:"email"`
	Up    int64  `json:"up"`
	Down  int64  `json:"down"`
}

// UserOnline lists the source IPs Xray currently sees for a user.
type UserOnline struct {
	Email string   `json:"email"`
	IPs   []string `json:"ips"`
}

// Disconnect records a local enforcement decision taken by the Node.
type Disconnect struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
	Unix   int64  `json:"unix"`
}

// UsageAck acknowledges every batch with sequence <= Seq.
type UsageAck struct {
	Seq uint64 `json:"seq"`
}
