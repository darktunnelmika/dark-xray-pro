package hubcore

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Xray versions: the HUB fetches official XTLS releases (checked against
// the release's .dgst checksum), keeps them per version and architecture,
// and a node installs one through task "xray_update". The node keeps the
// previous binary, so going back is the same action with an older version.

var (
	xrayReleasesURL = "https://api.github.com/repos/XTLS/Xray-core/releases?per_page=20"
	xrayDownloadURL = "https://github.com/XTLS/Xray-core/releases/download/"
	xrayTagRE       = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	xrayAssetName   = map[string]string{"amd64": "Xray-linux-64.zip", "arm64": "Xray-linux-arm64-v8a.zip"}
	xrayRelMu       sync.Mutex
	xrayRelCache    []XrayRelease
	xrayRelAt       time.Time
	xrayFetchMu     sync.Mutex
)

// XrayRelease is one published Xray-core version.
type XrayRelease struct {
	Tag        string `json:"tag"`
	Published  string `json:"published"`
	Prerelease bool   `json:"prerelease"`
}

// XrayReleases lists recent releases (cached for 10 minutes).
func XrayReleases(ctx context.Context) ([]XrayRelease, error) {
	xrayRelMu.Lock()
	defer xrayRelMu.Unlock()
	if time.Since(xrayRelAt) < 10*time.Minute && xrayRelCache != nil {
		return xrayRelCache, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, xrayReleasesURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github: HTTP %d", resp.StatusCode)
	}
	var raw []struct {
		Tag        string `json:"tag_name"`
		Published  string `json:"published_at"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&raw); err != nil {
		return nil, err
	}
	out := []XrayRelease{}
	for _, r := range raw {
		if xrayTagRE.MatchString(r.Tag) {
			out = append(out, XrayRelease{Tag: r.Tag, Published: strings.SplitN(r.Published, "T", 2)[0], Prerelease: r.Prerelease})
		}
	}
	xrayRelCache, xrayRelAt = out, time.Now()
	return out, nil
}

func (s *Store) xrayDir() string { return filepath.Join(filepath.Dir(s.path), "xray") }

// xrayDistName is how nodes ask for a stored build: "xray@v26.11.9-amd64".
func xrayDistName(tag, arch string) string { return "xray@" + tag + "-" + arch }

var xrayDistRE = regexp.MustCompile(`^xray@(v\d+\.\d+\.\d+)-(amd64|arm64)$`)

func (s *Store) xrayBuildPath(tag, arch string) string {
	return filepath.Join(s.xrayDir(), tag+"-"+arch, "xray")
}

// ensureXray downloads and verifies a release build; returns its sha256.
func (s *Store) ensureXray(ctx context.Context, tag, arch string) (string, error) {
	if !xrayTagRE.MatchString(tag) {
		return "", fmt.Errorf("%w: version", ErrInvalid)
	}
	asset := xrayAssetName[arch]
	if asset == "" {
		return "", fmt.Errorf("%w: architecture %q is not supported", ErrInvalid, arch)
	}
	xrayFetchMu.Lock()
	defer xrayFetchMu.Unlock()
	path := s.xrayBuildPath(tag, arch)
	if sum := cachedSHA(path); sum != "" {
		return sum, nil
	}
	base := xrayDownloadURL + tag + "/" + asset
	zipData, err := httpGet(ctx, base, 120<<20)
	if err != nil {
		return "", fmt.Errorf("download %s: %w", tag, err)
	}
	dgst, err := httpGet(ctx, base+".dgst", 1<<16)
	if err != nil {
		return "", fmt.Errorf("checksum file: %w", err)
	}
	sum := sha256.Sum256(zipData)
	if !strings.Contains(strings.ToLower(string(dgst)), hex.EncodeToString(sum[:])) {
		return "", fmt.Errorf("%s: checksum does not match the release", tag)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return "", err
	}
	for _, f := range zr.File {
		if f.Name != "xray" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		tmp := path + ".new"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", err
		}
		_, err = io.Copy(out, io.LimitReader(rc, 200<<20))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(tmp)
			return "", err
		}
		if err := os.Rename(tmp, path); err != nil {
			return "", err
		}
		return cachedSHA(path), nil
	}
	return "", fmt.Errorf("%s: no xray binary in the release archive", tag)
}

func httpGet(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// StartXrayUpdate installs an Xray version on a node.
func (s *Store) StartXrayUpdate(ctx context.Context, nodeID, tag string) (string, error) {
	var arch string
	if err := s.ro.QueryRowContext(ctx, `SELECT agent_arch FROM nodes WHERE id=?`, nodeID).Scan(&arch); err != nil {
		return "", ErrNotFound
	}
	if arch == "" {
		return "", fmt.Errorf("%w: update the node agent first (it does not report its architecture yet)", ErrInvalid)
	}
	sum, err := s.ensureXray(ctx, tag, arch)
	if err != nil {
		return "", err
	}
	return s.QueueTask(ctx, nodeID, wire.TaskXrayUpdate, map[string]string{"name": xrayDistName(tag, arch), "sha256": sum, "version": tag})
}

// recordXray stores the Xray version and architecture a node reports.
func (s *Store) recordXray(ctx context.Context, nodeID, version, arch string) {
	if version == "" && arch == "" {
		return
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE nodes SET xray_version=?, agent_arch=? WHERE id=? AND (xray_version<>? OR agent_arch<>?)`,
		version, arch, nodeID, version, arch)
}
