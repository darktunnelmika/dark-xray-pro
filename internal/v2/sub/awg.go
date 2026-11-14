package sub

import (
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

// AWGConf is one AmneziaWG client config (wg-quick format with the
// AmneziaWG lines), imported by the AmneziaWG and AmneziaVPN apps as a
// file or a QR code. AmneziaWG has no share-link format, so it is not in
// the plain subscription.
type AWGConf struct {
	Name string
	Conf string
}

// AWGConfs builds the client's AmneziaWG configs, one per address.
func AWGConfs(a Account, ins []Inbound) []AWGConf {
	sorted := append([]Inbound(nil), ins...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Tag < sorted[j].Tag })
	var out []AWGConf
	for _, in := range sorted {
		var t struct {
			Protocol string         `json:"protocol"`
			Settings map[string]any `json:"settings"`
		}
		if json.Unmarshal(in.Template, &t) != nil || t.Protocol != "amneziawg" || in.Meta.WGPrivateKey == "" {
			continue
		}
		num := func(k string) int64 {
			f, _ := t.Settings[k].(float64)
			return int64(f)
		}
		mtu := num("mtu")
		if mtu == 0 {
			mtu = int64(or0(in.Meta.MTU, 1280))
		}
		for _, ep := range in.Meta.Endpoints {
			if ep.excluded("links") {
				continue
			}
			var b strings.Builder
			fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nDNS = 1.1.1.1, 8.8.8.8\nMTU = %d\n", in.Meta.WGPrivateKey, in.Meta.WGAddress, mtu)
			for _, k := range []string{"Jc", "Jmin", "Jmax", "S1", "S2", "H1", "H2", "H3", "H4"} {
				if v := num(strings.ToLower(k)); v > 0 {
					fmt.Fprintf(&b, "%s = %d\n", k, v)
				}
			}
			fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = %s\nPersistentKeepalive = 25\n",
				in.Meta.PublicKey, net.JoinHostPort(ep.Address, strconv.Itoa(ep.Port)))
			out = append(out, AWGConf{Name: remark(a, in, ep), Conf: b.String()})
		}
	}
	return out
}
