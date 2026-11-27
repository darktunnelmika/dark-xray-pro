package hubcore

import (
	"context"
	"path/filepath"
	"testing"
)

func TestInboundWithSeveralCertificates(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	for _, id := range []string{"a", "b"} {
		if _, err := s.SelfSignedCert(ctx, id, id+".example.com"); err != nil {
			t.Fatal(err)
		}
	}
	tpl := map[string]any{"streamSettings": map[string]any{"security": "tls", "tlsSettings": map[string]any{
		"certificates": []any{map[string]any{"dark_cert": "a"}, map[string]any{"dark_cert": "b"},
			map[string]any{"certificateFile": "/etc/letsencrypt/live/c.example.com/fullchain.pem", "keyFile": "/etc/letsencrypt/live/c.example.com/privkey.pem"}}}}}
	if err := s.resolveCerts(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	list := tpl["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"].([]any)
	if len(list) != 3 || list[2].(map[string]any)["certificateFile"] == nil {
		t.Fatal("stored certificates and a file on the node all reach Xray", list)
	}
	for _, c := range list[:2] {
		if m := c.(map[string]any); m["certificate"] == nil || m["key"] == nil {
			t.Fatal("inline certificate and key", m)
		}
	}
}

func TestInboundAcceptsCertificateFilesOnTheNode(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "v2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	_, _ = s.CreateNode(ctx, "n1", "N")
	tpl := []byte(`{"protocol":"vless","port":443,"settings":{"decryption":"none"},"streamSettings":{"network":"tcp","security":"tls","tlsSettings":{"serverName":"c.example.com","certificates":[{"certificateFile":"/etc/letsencrypt/live/c.example.com/fullchain.pem","keyFile":"/etc/letsencrypt/live/c.example.com/privkey.pem"}]}}}`)
	if err := s.PutInbound(ctx, "in1", "n1", tpl, true); err != nil {
		t.Fatal("an inbound with certificate files is accepted", err)
	}
}
