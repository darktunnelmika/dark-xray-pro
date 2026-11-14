package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Assets (geo files) live next to the Xray binary, where Xray looks for
// them. The HUB lists what a node should hold; the node reports what it
// holds, and the HUB adds geo-based rules only once a node has the files.

type assetBox struct {
	mu    sync.Mutex
	dir   string
	sums  map[string]assetSum
	busy  map[string]bool
	known map[string]bool
}

type assetSum struct {
	mod time.Time
	sum string
}

func newAssetBox(dir string) *assetBox {
	return &assetBox{dir: dir, sums: map[string]assetSum{}, busy: map[string]bool{}, known: map[string]bool{"geoip.dat": true, "geosite.dat": true}}
}

func safeAssetName(n string) bool {
	return n != "" && !strings.ContainsAny(n, "/\\") && !strings.HasPrefix(n, ".") && strings.HasSuffix(n, ".dat")
}

// held reports name -> sha256 of the assets present.
func (b *assetBox) held() map[string]string {
	if b == nil || b.dir == "" {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string]string{}
	for n := range b.known {
		p := filepath.Join(b.dir, n)
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		c, ok := b.sums[n]
		if !ok || !c.mod.Equal(st.ModTime()) {
			sum, err := FileSHA(p)
			if err != nil {
				continue
			}
			c = assetSum{st.ModTime(), sum}
			b.sums[n] = c
		}
		out[n] = c.sum
	}
	return out
}

// sync downloads assets whose content differs from what the HUB lists.
func (b *assetBox) sync(ctx context.Context, f Fetcher, want []wire.Asset, logf func(string, ...any)) {
	if b == nil || b.dir == "" || f == nil || len(want) == 0 {
		return
	}
	have := b.held()
	for _, a := range want {
		if !safeAssetName(a.Name) || len(a.SHA256) != 64 || strings.EqualFold(have[a.Name], a.SHA256) {
			continue
		}
		b.mu.Lock()
		b.known[a.Name] = true
		if b.busy[a.Name] {
			b.mu.Unlock()
			continue
		}
		b.busy[a.Name] = true
		b.mu.Unlock()
		go func(a wire.Asset) {
			defer func() {
				b.mu.Lock()
				delete(b.busy, a.Name)
				b.mu.Unlock()
			}()
			if err := b.fetch(ctx, f, a); err != nil {
				logf("asset %s not updated: %v", a.Name, err)
				return
			}
			logf("asset %s updated", a.Name)
		}(a)
	}
}

func (b *assetBox) fetch(ctx context.Context, f Fetcher, a wire.Asset) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tmp := filepath.Join(b.dir, a.Name+".new")
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = f.Fetch(ctx, a.Name, io.MultiWriter(out, h))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		err = errChecksum
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, filepath.Join(b.dir, a.Name))
}

type checksumError struct{}

func (checksumError) Error() string { return "checksum mismatch" }

var errChecksum error = checksumError{}
