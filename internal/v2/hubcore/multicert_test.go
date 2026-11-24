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
		"certificates": []any{map[string]any{"dark_cert": "a"}, map[string]any{"dark_cert": "b"}}}}}
	if err := s.resolveCerts(ctx, tpl); err != nil {
		t.Fatal(err)
	}
	list := tpl["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"].([]any)
	if len(list) != 2 {
		t.Fatal("both certificates reach Xray", len(list))
	}
	for _, c := range list {
		if m := c.(map[string]any); m["certificate"] == nil || m["key"] == nil {
			t.Fatal("inline certificate and key", m)
		}
	}
}
