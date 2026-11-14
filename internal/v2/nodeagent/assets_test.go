package nodeagent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestAssetSync(t *testing.T) {
	dir := t.TempDir()
	b := newAssetBox(dir)
	if len(b.held()) != 0 {
		t.Fatal("nothing held yet")
	}
	body := []byte("geo-data")
	want := []wire.Asset{{Name: "geoip.dat", SHA256: shaOf(body)}, {Name: "../evil.dat", SHA256: shaOf(body)}}
	b.sync(context.Background(), fakeFetch{body}, want, t.Logf)
	deadline := time.Now().Add(2 * time.Second)
	for b.held()["geoip.dat"] == "" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if b.held()["geoip.dat"] != shaOf(body) {
		t.Fatalf("held %v", b.held())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "evil.dat")); err == nil {
		t.Fatal("path traversal")
	}
	// Wrong content: kept out.
	b.sync(context.Background(), fakeFetch{[]byte("other")}, []wire.Asset{{Name: "geosite.dat", SHA256: shaOf(body)}}, t.Logf)
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "geosite.dat")); err == nil {
		t.Fatal("asset with a bad checksum installed")
	}
}
