package nodeagent

import (
	"bytes"
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
