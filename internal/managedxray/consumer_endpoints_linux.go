//go:build linux

package managedxray

import (
	"net"
	"net/netip"

	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

const MaxConsumerEndpointApprovals = 256

// ConsumerEndpointApproval is explicit startup authority for one node,
// installation and inbound. A browser manifest can only select these exact
// tuples; neither DNS nor an incoming HTTP Host grants new network authority.
type ConsumerEndpointApproval struct {
	NodeID         string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	InboundID      uint64 `json:"inbound_id"`
	Address        string `json:"address"`
	Port           uint16 `json:"port"`
}

// IsGlobalUnicast includes special-purpose space. N19 deliberately excludes
// local, shared, documentation, benchmarking and reserved destinations as well
// as DNS, mapped IPv6 and noncanonical IPv4. This is an admission boundary, not
// evidence that an address is reachable from the public Internet.
var consumerSpecialPrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
}

func ValidateConsumerProtocolEndpoint(address string, port uint16) bool {
	ip := net.ParseIP(address)
	if port < 1024 || ip == nil || ip.To4() == nil || address != ip.String() ||
		!ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	v4 := ip.To4()
	if v4[0] == 0 || v4[0] >= 224 {
		return false
	}
	addr := netip.AddrFrom4([4]byte{v4[0], v4[1], v4[2], v4[3]})
	for _, prefix := range consumerSpecialPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

func ValidateConsumerEndpointApprovals(approvals []ConsumerEndpointApproval) error {
	if len(approvals) == 0 || len(approvals) > MaxConsumerEndpointApprovals {
		return ErrProtocolHealth
	}
	type tuple struct {
		node, installation, address string
		port                        uint16
	}
	seen := make(map[tuple]bool, len(approvals))
	for _, a := range approvals {
		key := tuple{a.NodeID, a.InstallationID, a.Address, a.Port}
		if !transactionNode.MatchString(a.NodeID) || !transactionID.MatchString(a.InstallationID) ||
			a.InboundID == 0 || a.InboundID > 1<<53-1 || !ValidateConsumerProtocolEndpoint(a.Address, a.Port) || seen[key] {
			return ErrProtocolHealth
		}
		seen[key] = true
	}
	return nil
}

func ParseConsumerEndpointApprovalsJSON(raw string) ([]ConsumerEndpointApproval, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return nil, ErrProtocolHealth
	}
	var approvals []ConsumerEndpointApproval
	if usagewire.Decode([]byte(raw), &approvals) != nil || ValidateConsumerEndpointApprovals(approvals) != nil {
		return nil, ErrProtocolHealth
	}
	return approvals, nil
}

func (t *TransactionRuntime) consumerProtocolApproved(q ProtocolHealthRequest) bool {
	if q.Version == ProtocolHealthVersion || q.Version == InitialApplyProtocolHealthVersion {
		return validProtocolHealthConsumer(q)
	}
	if q.Version != ConsumerProtocolHealthVersion || !validProtocolHealthConsumer(q) || t.metered == nil {
		return false
	}
	for _, a := range t.metered.ConsumerEndpointApprovals {
		if a.NodeID == t.options.NodeID && a.InstallationID == t.options.InstallationID &&
			a.NodeID == q.NodeID && a.InstallationID == q.InstallationID && a.InboundID == q.InboundID &&
			a.Address == q.ConsumerAddress && a.Port == q.ConsumerPort {
			return true
		}
	}
	return false
}
