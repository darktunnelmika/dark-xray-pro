package hubcore

import (
	"bufio"
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
	"runtime"
	"strconv"
	"strings"
	"time"
)

// One-click HUB update: the HUB reads the project's GitHub releases, and the
// owner installs the newest one with a button. Both binaries are checked
// against the release's SHA256SUMS; the HUB binary then goes through the
// same checks and roll-back as an uploaded one (StageHubUpdate), and the
// release's dark-node replaces the HUB's copy so "update nodes" follows.

var (
	hubReleasesURL = "https://api.github.com/repos/darktunnelmika/dark-xray-pro/releases?per_page=15"
	relClient      = &http.Client{Timeout: 3 * time.Minute}
)

// HubRelease is the newest published release.
type HubRelease struct {
	Tag       string `json:"tag"`
	Published string `json:"published"`
	Notes     string `json:"notes"`
	Current   string `json:"current"`
	Newer     bool   `json:"newer"`
	hubURL    string
	nodeURL   string
	sumsURL   string
}

func assetNames() (hub, node string) {
	return "dark-hub2-linux-" + runtime.GOARCH, "dark-node-linux-" + runtime.GOARCH
}

// semver compares vX.Y.Z tags; -1, 0, 1.
func semver(a, b string) int {
	pa, pb := strings.Split(strings.TrimPrefix(a, "v"), "."), strings.Split(strings.TrimPrefix(b, "v"), ".")
	for i := 0; i < 3; i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// LatestHubRelease finds the newest stable release with binaries for this CPU.
func LatestHubRelease(ctx context.Context) (HubRelease, error) {
	out := HubRelease{Current: HubVersion}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, hubReleasesURL, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := relClient.Do(req)
	if err != nil {
		return out, fmt.Errorf("%w: cannot reach GitHub from the HUB (%v)", ErrInvalid, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("%w: GitHub answered HTTP %d", ErrInvalid, resp.StatusCode)
	}
	var raw []struct {
		Tag        string `json:"tag_name"`
		Published  string `json:"published_at"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&raw); err != nil {
		return out, err
	}
	hubName, nodeName := assetNames()
	best := HubRelease{}
	for _, r := range raw {
		if r.Draft || r.Prerelease || !xrayTagRE.MatchString(r.Tag) {
			continue
		}
		c := HubRelease{Tag: r.Tag, Published: strings.SplitN(r.Published, "T", 2)[0], Notes: r.Body}
		for _, a := range r.Assets {
			switch a.Name {
			case hubName:
				c.hubURL = a.URL
			case nodeName:
				c.nodeURL = a.URL
			case "SHA256SUMS":
				c.sumsURL = a.URL
			}
		}
		if c.hubURL == "" || c.sumsURL == "" {
			continue
		}
		if best.Tag == "" || semver(c.Tag, best.Tag) > 0 {
			best = c
		}
	}
	if best.Tag == "" {
		return out, fmt.Errorf("%w: no release with a HUB build for %s yet", ErrInvalid, runtime.GOARCH)
	}
	if len(best.Notes) > 4000 {
		best.Notes = best.Notes[:4000] + "…"
	}
	best.Current = HubVersion
	best.Newer = semver(best.Tag, HubVersion) > 0
	return best, nil
}

func fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := relClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

func parseSums(b []byte) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			m[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return m
}

func checkSum(b []byte, want string) bool {
	h := sha256.Sum256(b)
	return want != "" && hex.EncodeToString(h[:]) == want
}

// InstallHubRelease downloads, verifies and stages the newest release; the
// caller restarts the HUB. nodeOK tells whether the HUB's dark-node copy was
// replaced too.
func (s *Store) InstallHubRelease(ctx context.Context) (from, to string, nodeOK bool, err error) {
	rel, err := LatestHubRelease(ctx)
	if err != nil {
		return "", "", false, err
	}
	if !rel.Newer {
		return HubVersion, rel.Tag, false, fmt.Errorf("%w: the HUB already runs %s (newest release %s)", ErrInvalid, HubVersion, rel.Tag)
	}
	sumsRaw, err := fetch(ctx, rel.sumsURL, 1<<20)
	if err != nil {
		return "", "", false, err
	}
	sums := parseSums(sumsRaw)
	hubName, nodeName := assetNames()
	hubBin, err := fetch(ctx, rel.hubURL, 200<<20)
	if err != nil {
		return "", "", false, err
	}
	if !checkSum(hubBin, sums[hubName]) {
		return "", "", false, fmt.Errorf("%w: the downloaded HUB does not match the release checksum", ErrInvalid)
	}
	// The agent first, so a failed HUB swap leaves nothing half done that matters.
	if rel.nodeURL != "" {
		if nb, err := fetch(ctx, rel.nodeURL, 200<<20); err == nil && checkSum(nb, sums[nodeName]) {
			s.distMu.Lock()
			dir := s.distDir
			s.distMu.Unlock()
			if dir != "" {
				p := filepath.Join(dir, "dark-node")
				if os.WriteFile(p+".new", nb, 0o755) == nil && os.Rename(p+".new", p) == nil {
					nodeOK = true
				}
			}
		}
	}
	from, to, err = StageHubUpdate(ctx, bytes.NewReader(hubBin))
	return from, to, nodeOK, err
}
