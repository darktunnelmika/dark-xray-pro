//go:build linux

package inboundruntime

import (
	"context"
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	xraystats "github.com/darktunnelmika/dark-xray-pro/internal/xraystats"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

const (
	// These are the old CLI's actual limits: gRPC's default receive ceiling,
	// followed by Native.query's bounded JSON stdout, including both newlines.
	nativeStatsWireLimit       = 4 << 20
	nativeStatsJSONLimit       = 8 << 20
	nativeStatsRequestLimit    = 256
	nativeStatsJSONPrefix      = "{\n    \"stat\": [\n"
	nativeStatsJSONSuffix      = "\n    ]\n}\n\n"
	nativeStatsJSONEntryPrefix = "        {\n            \"name\": "
	nativeStatsJSONEntrySuffix = "\n        }"
	nativeStatsJSONValuePrefix = ",\n            \"value\": "
	// A one-byte nonempty name with an omitted zero value is the smallest
	// valid old JSON entry. This derived bound adds no customer-count limit.
	nativeStatsMinimumJSONEntry = len(nativeStatsJSONEntryPrefix) + 3 + len(nativeStatsJSONEntrySuffix)
	nativeStatsMaximumEntries   = (nativeStatsJSONLimit - len(nativeStatsJSONPrefix) - len(nativeStatsJSONSuffix) + 2) / (nativeStatsMinimumJSONEntry + 2)
)

// A client belongs to exactly one Native birth. Its process token, held private
// directory and socket are immutable before it is published. It never adopts a
// service or survives Stop/lease expiry, even if the same pathname reappears.
type nativeStatsClient struct {
	birth     *Native
	conn      *grpc.ClientConn
	client    xraystats.StatsServiceClient
	closed    atomic.Bool
	closeOnce sync.Once
}

func newNativeStatsClient(birth *Native) (*nativeStatsClient, error) {
	if birth == nil || birth.identity.PID <= 0 || birth.apiDirectory == nil || birth.apiSocket == "" {
		return nil, ErrIdentity
	}
	c := &nativeStatsClient{birth: birth}
	conn, err := grpc.NewClient("passthrough:///dark-pro-owned-stats",
		grpc.WithTransportCredentials(insecure.NewCredentials()), // authenticated private UDS, not TCP
		grpc.WithContextDialer(c.dial), grpc.WithNoProxy(),
		grpc.WithDisableServiceConfig(), grpc.WithDisableRetry(), grpc.WithIdleTimeout(0),
		grpc.WithMaxHeaderListSize(16<<10),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true),
			grpc.MaxCallRecvMsgSize(nativeStatsWireLimit), grpc.MaxCallSendMsgSize(nativeStatsRequestLimit),
			grpc.MaxRetryRPCBufferSize(0), grpc.ForceCodec(nativeStatsCodec{})))
	if err != nil {
		return nil, ErrRuntime
	}
	c.conn = conn
	c.client = xraystats.NewStatsServiceClient(conn)
	return c, nil
}

func (c *nativeStatsClient) markClosed() {
	if c != nil {
		c.closed.Store(true)
	}
}

func (c *nativeStatsClient) close() {
	if c == nil {
		return
	}
	c.markClosed()
	c.closeOnce.Do(func() { _ = c.conn.Close() })
}

func (c *nativeStatsClient) ownsBirth() bool {
	return c != nil && !c.closed.Load() && c.birth.checkIdentity() == nil && c.birth.ownsUnixControl()
}

func (c *nativeStatsClient) dial(ctx context.Context, _ string) (net.Conn, error) {
	if !c.ownsBirth() {
		return nil, ErrIdentity
	}
	dial, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(dial, "unix", c.birth.apiSocket)
	if err != nil {
		return nil, ErrProof
	}
	ok := false
	defer func() {
		if !ok {
			_ = conn.Close()
		}
	}()
	owned, isUnix := conn.(*net.UnixConn)
	if !isUnix || !nativeStatsPeer(owned, c.birth.identity.PID, uint32(os.Geteuid())) || !c.ownsBirth() || dial.Err() != nil {
		return nil, ErrIdentity
	}
	ok = true
	return conn, nil
}

func nativeStatsPeer(conn *net.UnixConn, pid int, uid uint32) bool {
	raw, err := conn.SyscallConn()
	if err != nil {
		return false
	}
	var peer *unix.Ucred
	var peerErr error
	err = raw.Control(func(fd uintptr) { peer, peerErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED) })
	return err == nil && peerErr == nil && peer != nil && int(peer.Pid) == pid && peer.Uid == uid
}

func (p *Native) query(ctx context.Context, pattern string) (map[string]uint64, error) {
	if p.checkIdentity() != nil || !p.ownsUnixControl() {
		return nil, ErrIdentity
	}
	if p.stats == nil || p.stats.closed.Load() || !validNativeStatsPattern(pattern) {
		return nil, ErrProof
	}
	outer, cancelOuter := context.WithTimeout(ctx, 4*time.Second)
	defer cancelOuter()
	// The old CLI spent one 3s budget on blocking dial plus the unary RPC.
	// WaitForReady preserves that bounded wait while this channel reconnects.
	call, cancel := context.WithTimeout(outer, 3*time.Second)
	defer cancel()
	response, err := p.stats.client.QueryStats(call, &xraystats.QueryStatsRequest{Pattern: pattern, Reset_: false})
	// WithDisableRetry disables configured retry. In pinned gRPC 1.84, the
	// zero retry buffer commits immediately after SendMsg successfully enqueues
	// its header+payload. An accepted control message is not replayed while
	// awaiting a reply. Safe pre-dispatch/unprocessed transport retry can still
	// occur; durable application replay remains the Worker's same-nonce action.
	if err != nil || response == nil || call.Err() != nil || p.stats.closed.Load() {
		return nil, ErrProof
	}
	values := make(map[string]uint64, len(response.Stat))
	for _, entry := range response.Stat {
		if entry == nil || entry.Name == "" || entry.Value < 0 {
			return nil, ErrProof
		}
		if _, exists := values[entry.Name]; exists {
			return nil, ErrProof
		}
		values[entry.Name] = uint64(entry.Value)
	}
	if call.Err() != nil || !p.stats.ownsBirth() {
		return nil, ErrProof
	}
	return values, nil
}

func validNativeStatsPattern(pattern string) bool {
	if pattern == "" || pattern == nativeProtocol+"capabilities" || pattern == nativeProtocol+"health" {
		return true
	}
	for _, operation := range []string{"activate/", "final/"} {
		if nonce, ok := strings.CutPrefix(pattern, nativeProtocol+operation); ok {
			return validNonce(nonce)
		}
	}
	return false
}

// The fixed proto codec pre-scans response structure and the original byte
// envelopes before protobuf allocates repeated entries. It retains no arbitrary
// fields and never turns reset=true or an unrecognized operation into a call.
type nativeStatsCodec struct{}

func (nativeStatsCodec) Name() string { return "proto" }

func (nativeStatsCodec) Marshal(value any) ([]byte, error) {
	request, ok := value.(*xraystats.QueryStatsRequest)
	if !ok || request == nil || request.Reset_ || !validNativeStatsPattern(request.Pattern) {
		return nil, ErrProof
	}
	data, err := proto.Marshal(request)
	if err != nil || len(data) > nativeStatsRequestLimit {
		return nil, ErrProof
	}
	return data, nil
}

func (nativeStatsCodec) Unmarshal(data []byte, value any) error {
	response, ok := value.(*xraystats.QueryStatsResponse)
	if !ok || response == nil {
		return ErrProof
	}
	if _, err := nativeStatsJSONSize(data); err != nil {
		return err
	}
	if err := (proto.UnmarshalOptions{RecursionLimit: 4}).Unmarshal(data, response); err != nil {
		return ErrProof
	}
	return nil
}

func nativeStatsJSONSize(data []byte) (int, error) {
	if len(data) > nativeStatsWireLimit {
		return 0, ErrProof
	}
	if len(data) == 0 {
		return len("{}\n\n"), nil
	}
	total := len(nativeStatsJSONPrefix) + len(nativeStatsJSONSuffix)
	count := 0
	for len(data) > 0 {
		field, kind, n := protowire.ConsumeTag(data)
		if n < 0 || field != 1 || kind != protowire.BytesType {
			return 0, ErrProof
		}
		entry, used := protowire.ConsumeBytes(data[n:])
		if used < 0 {
			return 0, ErrProof
		}
		data = data[n+used:]
		name, value, err := nativeStatsWireEntry(entry)
		if err != nil {
			return 0, err
		}
		if count > 0 {
			total += len(",\n")
		}
		count++
		if count > nativeStatsMaximumEntries {
			return 0, ErrProof
		}
		total += len(nativeStatsJSONEntryPrefix) + nativeStatsQuotedJSONSize(name) + len(nativeStatsJSONEntrySuffix)
		if value != 0 {
			total += len(nativeStatsJSONValuePrefix)
			for ; value > 0; value /= 10 {
				total++
			}
		}
		if total > nativeStatsJSONLimit {
			return 0, ErrProof
		}
	}
	return total, nil
}

func nativeStatsWireEntry(data []byte) ([]byte, uint64, error) {
	var name []byte
	var value uint64
	seenName, seenValue := false, false
	for len(data) > 0 {
		field, kind, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, 0, ErrProof
		}
		data = data[n:]
		switch {
		case field == 1 && kind == protowire.BytesType && !seenName:
			name, n = protowire.ConsumeBytes(data)
			seenName = true
		case field == 2 && kind == protowire.VarintType && !seenValue:
			value, n = protowire.ConsumeVarint(data)
			seenValue = true
		default:
			return nil, 0, ErrProof
		}
		if n < 0 {
			return nil, 0, ErrProof
		}
		data = data[n:]
	}
	if !seenName || len(name) == 0 || !utf8.Valid(name) || value > math.MaxInt64 {
		return nil, 0, ErrProof
	}
	return name, value, nil
}

// Matches encoding/json with EscapeHTML(false), including its mandatory
// U+2028/U+2029 and control-character escaping. Names have already passed UTF-8.
func nativeStatsQuotedJSONSize(name []byte) int {
	n := len(name) + 2
	for i := 0; i < len(name); {
		b := name[i]
		if b < utf8.RuneSelf {
			switch b {
			case '"', '\\', '\b', '\f', '\n', '\r', '\t':
				n++
			default:
				if b < 0x20 {
					n += 5
				}
			}
			i++
			continue
		}
		r, width := utf8.DecodeRune(name[i:])
		if r == '\u2028' || r == '\u2029' {
			n += 3
		}
		i += width
	}
	return n
}
