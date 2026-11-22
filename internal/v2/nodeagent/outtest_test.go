package nodeagent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestFilterKeepsProxyAccounts(t *testing.T) {
	raw := []byte(`{"inbounds":[{"tag":"api","protocol":"dokodemo-door","settings":{}},{"tag":"px","protocol":"socks","settings":{"auth":"password","accounts":[{"user":"u","pass":"p"}]}},
{"tag":"v","protocol":"vless","settings":{"clients":[{"id":"x","email":"a"},{"id":"y","email":"b"}]}}],"stats":{},"api":{"tag":"api","services":["StatsService"]},"policy":{"levels":{"0":{"statsUserUplink":true,"statsUserDownlink":true}}}}`)
	out, err := FilterConfig(raw, map[string]bool{"a": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"user":"u"`) {
		t.Fatal("the SOCKS login must survive the filter", string(out))
	}
	if strings.Contains(string(out), `"email":"b"`) {
		t.Fatal("clients are still filtered")
	}
}

func TestSplitAccessKeepsConnectionsOutOfTheLog(t *testing.T) {
	var acc, rest bytes.Buffer
	w := &splitAccess{access: &acc, rest: &rest}
	_, _ = w.Write([]byte("2026/11/15 00:26:30 from 1.2.3.4:5 accepted tcp:x:443 [in >> direct] email: ali\n[Warning] core: Xray started\n2026/11/15 from 5.6.7.8:9 acc"))
	_, _ = w.Write([]byte("epted tcp:y:443 [in >> direct] email: bob\n"))
	if strings.Count(acc.String(), "accepted") != 2 || strings.Contains(rest.String(), "accepted") || !strings.Contains(rest.String(), "Xray started") {
		t.Fatalf("access %q rest %q", acc.String(), rest.String())
	}
}

// A test never opens a second kernel TUN beside the real Xray's, and the
// rules a killed Xray left behind are found by their empty tables.
func TestOutboundTestWireGuardHygiene(t *testing.T) {
	outs := []json.RawMessage{
		json.RawMessage(`{"tag":"direct","protocol":"freedom"}`),
		json.RawMessage(`{"tag":"warp","protocol":"wireguard","settings":{"mtu":1280,"secretKey":"k"}}`),
	}
	got := userspaceWireGuard(outs)
	if string(got[0]) != string(outs[0]) {
		t.Fatal("other outbounds stay as they are")
	}
	var m struct {
		Settings struct {
			NoKernelTun bool   `json:"noKernelTun"`
			MTU         int    `json:"mtu"`
			SecretKey   string `json:"secretKey"`
		} `json:"settings"`
	}
	if err := json.Unmarshal(got[1], &m); err != nil || !m.Settings.NoKernelTun || m.Settings.MTU != 1280 || m.Settings.SecretKey != "k" {
		t.Fatalf("%s %v", got[1], err)
	}
	if strings.Contains(string(outs[1]), "noKernelTun") {
		t.Fatal("the node's own config is not touched")
	}

	rules := `0:	from all lookup local
32728:	from all oif wg1 [detached] lookup 10231
32729:	from 2606:4700:110::1 lookup 10231
32740:	from 2606:4700:110::1 lookup 10230
32750:	from all fwmark 0x1 lookup 9000
32766:	from all lookup main`
	empty := func(table string) bool { return table == "10231" || table == "9000" }
	prefs := staleTunRules(rules, empty)
	if strings.Join(prefs, ",") != "32728,32729" {
		t.Fatal(prefs) // 10230 still has routes; 9000 is not Xray's to touch
	}
}
