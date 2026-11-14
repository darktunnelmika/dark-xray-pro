package nodeagent

import (
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
