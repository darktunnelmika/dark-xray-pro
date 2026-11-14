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
	// PathDist + file name serves the Node binaries for self-update.
	PathDist = "/api/v2/node/dist/"

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
	Host           *Host  `json:"host,omitempty"`
	// Results reports finished one-off tasks; the Node resends a result
	// until a sync succeeds, and the HUB ignores results it already has.
	Results []TaskResult `json:"results,omitempty"`
	// AgentSHA is the sha256 of the running dark-node binary.
	AgentSHA string `json:"agent_sha,omitempty"`
}

// Task is a one-off job the HUB asks a Node to run, for example scanning
// for REALITY camouflage domains as seen from the Node's own network.
// A Node runs each ID at most once; older Nodes ignore unknown kinds.
type Task struct {
	ID   string          `json:"id"`
	Kind string          `json:"kind"`
	Args json.RawMessage `json:"args,omitempty"`
}

// TaskResult is the outcome of one Task.
type TaskResult struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// TaskSNIScan probes TLS camouflage candidates (see package sniscan).
const TaskSNIScan = "sni_scan"

// TaskSelfUpdate replaces the dark-node binary with the HUB's copy
// (args: {"sha256": "..."}) and restarts the agent.
const TaskSelfUpdate = "self_update"

// Host is the node machine's health at the time of the sync.
type Host struct {
	CPUPercent  float64 `json:"cpu_percent"`
	RAMUsed     uint64  `json:"ram_used"`
	RAMTotal    uint64  `json:"ram_total"`
	DiskUsed    uint64  `json:"disk_used"`
	DiskTotal   uint64  `json:"disk_total"`
	Load1       float64 `json:"load1"`
	UptimeSec   uint64  `json:"uptime_sec"`
	RXRate      float64 `json:"rx_rate"` // bytes per second on the default interface
	TXRate      float64 `json:"tx_rate"`
	TCP         uint64  `json:"tcp"`
	OnlineUsers int     `json:"online_users"`
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
	UnixTime    int64  `json:"unix_time"`
	Tasks       []Task `json:"tasks,omitempty"`
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
	// Inbounds carries per-inbound byte deltas; Email holds the inbound tag.
	// Older HUBs ignore it.
	Inbounds []UserTraffic `json:"inbounds,omitempty"`
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
