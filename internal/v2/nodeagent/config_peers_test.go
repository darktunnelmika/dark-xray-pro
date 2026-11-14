package nodeagent

import (
	"strings"
	"testing"
)

func TestFilterConfigWireGuardPeers(t *testing.T) {
	raw := []byte(`{"api":{"services":["StatsService"]},"stats":{},"inbounds":[{"tag":"wg","protocol":"wireguard","settings":{"secretKey":"k","peers":[{"publicKey":"a","allowedIPs":["10.0.0.2/32"],"email":"ann"},{"publicKey":"b","allowedIPs":["10.0.0.3/32"],"email":"ben"}]}}]}`)
	out, err := FilterConfig(raw, map[string]bool{"ann": true})
	if err != nil || !strings.Contains(string(out), `"ann"`) || strings.Contains(string(out), `"ben"`) {
		t.Fatal(string(out), err)
	}
	emails, _ := ConfigEmails(raw)
	if len(emails) != 2 {
		t.Fatal(emails)
	}
	u, _ := ParseUsers(raw)
	if u["wg"]["ben"].Protocol != "wireguard" {
		t.Fatal(u)
	}
}
