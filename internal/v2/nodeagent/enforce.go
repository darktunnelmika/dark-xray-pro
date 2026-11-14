package nodeagent

import (
	"sort"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Block reasons reported to the HUB and shown in the panel.
const (
	ReasonDisabled = "disabled"
	ReasonExpired  = "expired"
	ReasonVolume   = "volume_exhausted"
	ReasonIPLimit  = "ip_limit"
)

// Decision lists users the Node must not serve right now, with a reason.
type Decision map[string]string

// Evaluate decides locally which users to block. It needs no HUB:
//   - limits: last limits received from the HUB
//   - localUsed: bytes this Node counted that the HUB had not yet included
//     in RemainingBytes (see wire.SyncResponse.UsageSeq)
//   - online: source IPs currently seen per user
//
// Unknown users (not in limits) are always blocked: the Node never serves a
// user the HUB did not explicitly authorize.
func Evaluate(now int64, limits []wire.UserLimit, localUsed map[string]int64, online map[string][]string) Decision {
	d := Decision{}
	for _, l := range limits {
		switch {
		case !l.Enabled:
			d[l.Email] = ReasonDisabled
		case l.ExpiresUnix > 0 && now >= l.ExpiresUnix:
			d[l.Email] = ReasonExpired
		case l.RemainingBytes >= 0 && localUsed[l.Email] >= l.RemainingBytes:
			d[l.Email] = ReasonVolume
		case l.MaxIPs > 0 && len(online[l.Email]) > l.MaxIPs:
			d[l.Email] = ReasonIPLimit
		}
	}
	return d
}

// Allowed returns the sorted emails from limits that are not blocked.
func (d Decision) Allowed(limits []wire.UserLimit) []string {
	var out []string
	for _, l := range limits {
		if _, blocked := d[l.Email]; !blocked {
			out = append(out, l.Email)
		}
	}
	sort.Strings(out)
	return out
}
