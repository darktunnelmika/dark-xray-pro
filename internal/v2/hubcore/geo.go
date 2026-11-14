package hubcore

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Geo files (geoip.dat, geosite.dat) live next to the database, are
// refreshed weekly from configurable URLs (default: the Iran-focused
// Chocolate4U lists, which include geosite:ir, geoip:ir and ad lists) and
// are handed to nodes over their authenticated link.

var GeoAssets = []string{"geoip.dat", "geosite.dat"}

var defaultGeoURL = map[string]string{
	"geoip.dat":   "https://github.com/Chocolate4U/Iran-v2ray-rules/releases/latest/download/geoip.dat",
	"geosite.dat": "https://github.com/Chocolate4U/Iran-v2ray-rules/releases/latest/download/geosite.dat",
}

type fileSum struct {
	mod time.Time
	sum string
}

var (
	sumMu    sync.Mutex
	sumCache = map[string]fileSum{}
	geoMu    sync.Mutex // one download at a time
)

// cachedSHA hashes a file once per modification.
func cachedSHA(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	sumMu.Lock()
	c, ok := sumCache[path]
	sumMu.Unlock()
	if ok && c.mod.Equal(st.ModTime()) {
		return c.sum
	}
	sum, err := fileSHA(path)
	if err != nil {
		return ""
	}
	sumMu.Lock()
	sumCache[path] = fileSum{st.ModTime(), sum}
	sumMu.Unlock()
	return sum
}

func (s *Store) geoDir() string { return filepath.Join(filepath.Dir(s.path), "geo") }

// geoWanted lists the geo files nodes should hold.
func (s *Store) geoWanted() []wire.Asset {
	var out []wire.Asset
	for _, n := range GeoAssets {
		if sum := cachedSHA(filepath.Join(s.geoDir(), n)); sum != "" {
			out = append(out, wire.Asset{Name: n, SHA256: sum})
		}
	}
	return out
}

// GeoInfo is shown in the panel.
type GeoInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Updated int64  `json:"updated"`
	URL     string `json:"url"`
}

func (s *Store) GeoInfo(ctx context.Context) []GeoInfo {
	var out []GeoInfo
	for _, n := range GeoAssets {
		g := GeoInfo{Name: n, URL: s.geoURL(ctx, n)}
		if st, err := os.Stat(filepath.Join(s.geoDir(), n)); err == nil {
			g.Size, g.Updated = st.Size(), st.ModTime().Unix()
		}
		out = append(out, g)
	}
	return out
}

func (s *Store) geoURL(ctx context.Context, name string) string {
	if u := s.Setting(ctx, "geo:"+name); u != "" {
		return u
	}
	return defaultGeoURL[name]
}

// SetGeoURL changes a source ("" = default).
func (s *Store) SetGeoURL(ctx context.Context, name, url string) error {
	if _, ok := defaultGeoURL[name]; !ok {
		return ErrNotFound
	}
	if url != "" && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("%w: url must be https", ErrInvalid)
	}
	return s.SetSetting(ctx, "geo:"+name, url)
}

// UpdateGeo downloads every geo file. Files are checked to be loadable by
// Xray (when the HUB has an Xray binary) before replacing the old ones.
func (s *Store) UpdateGeo(ctx context.Context) error {
	geoMu.Lock()
	defer geoMu.Unlock()
	if err := os.MkdirAll(s.geoDir(), 0o755); err != nil {
		return err
	}
	var errs []string
	for _, n := range GeoAssets {
		if err := s.fetchGeo(ctx, n, s.geoURL(ctx, n)); err != nil {
			errs = append(errs, n+": "+err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	return nil
}

func (s *Store) fetchGeo(ctx context.Context, name, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := filepath.Join(s.geoDir(), name+".new")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, 200<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if n < 64<<10 {
		os.Remove(tmp)
		return fmt.Errorf("file too small (%d bytes)", n)
	}
	return os.Rename(tmp, filepath.Join(s.geoDir(), name))
}

// RunGeo refreshes geo files at start (if missing) and weekly.
func (s *Store) RunGeo(ctx context.Context) {
	for {
		oldest := time.Now()
		for _, n := range GeoAssets {
			st, err := os.Stat(filepath.Join(s.geoDir(), n))
			if err != nil {
				oldest = time.Time{}
				break
			}
			if st.ModTime().Before(oldest) {
				oldest = st.ModTime()
			}
		}
		if time.Since(oldest) > 7*24*time.Hour {
			if err := s.UpdateGeo(ctx); err != nil {
				log.Printf("geo update: %v", err)
			} else {
				log.Printf("geo files updated")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(6 * time.Hour):
		}
	}
}

// recordAssets stores which geo files a node holds (name:sha, sorted).
func (s *Store) recordAssets(ctx context.Context, nodeID string, a map[string]string) {
	if a == nil {
		return
	}
	var parts []string
	for k, v := range a {
		parts = append(parts, k+":"+v)
	}
	sort.Strings(parts)
	v := strings.Join(parts, ",")
	_, _ = s.db.ExecContext(ctx, `UPDATE nodes SET agent_assets=? WHERE id=? AND agent_assets<>?`, v, nodeID, v)
}

// testConfig runs "xray run -test" on a rendered config with the HUB's
// Xray and geo files. No Xray on the HUB: nothing to check against.
func (s *Store) testConfig(ctx context.Context, cfg []byte) error {
	s.distMu.Lock()
	dir := s.distDir
	s.distMu.Unlock()
	bin := filepath.Join(dir, "xray")
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(bin); err != nil {
		return nil
	}
	f, err := os.CreateTemp(s.tmpDir(), "xray-test-*.json")
	if err != nil {
		return nil
	}
	defer os.Remove(f.Name())
	_, _ = f.Write(cfg)
	f.Close()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "run", "-test", "-c", f.Name())
	cmd.Env = append(os.Environ(), "XRAY_LOCATION_ASSET="+s.geoDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("xray rejects the config: %s", msg)
	}
	return nil
}
