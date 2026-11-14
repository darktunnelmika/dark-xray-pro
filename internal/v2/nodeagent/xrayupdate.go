package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var xrayVerRE = regexp.MustCompile(`Xray (\d+\.\d+\.\d+)`)

// XrayVersion runs "<bin> version" and returns e.g. "26.11.9" ("" if unknown).
func XrayVersion(bin string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "version").Output()
	if err != nil {
		return ""
	}
	if m := xrayVerRE.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}

// XrayUpdateTask installs an Xray build from the HUB: checksum, then the
// new binary must report its version, then it replaces bin (the old one
// stays as bin+".prev") and the agent restarts with it.
func XrayUpdateTask(f Fetcher, bin string) TaskFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var a struct {
			Name    string `json:"name"`
			SHA256  string `json:"sha256"`
			Version string `json:"version"`
		}
		if err := json.Unmarshal(raw, &a); err != nil || !strings.HasPrefix(a.Name, "xray@") || len(a.SHA256) != 64 {
			return nil, errors.New("bad xray update arguments")
		}
		before := XrayVersion(bin)
		if cur, _ := FileSHA(bin); strings.EqualFold(cur, a.SHA256) {
			return map[string]string{"from": before, "to": before, "note": "already installed"}, nil
		}
		tmp := bin + ".new"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		err = f.Fetch(ctx, a.Name, io.MultiWriter(out, h))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err == nil && !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
			err = errors.New("checksum mismatch")
		}
		if err != nil {
			os.Remove(tmp)
			return nil, fmt.Errorf("download: %w", err)
		}
		got := XrayVersion(tmp)
		if got == "" {
			os.Remove(tmp)
			return nil, errors.New("the new Xray does not run on this server")
		}
		_ = os.Remove(bin + ".prev")
		if err := os.Link(bin, bin+".prev"); err != nil {
			_ = copyFile(bin, bin+".prev")
		}
		if err := os.Rename(tmp, bin); err != nil {
			os.Remove(tmp)
			return nil, err
		}
		return Restart{Value: map[string]string{"from": before, "to": got}}, nil
	}
}
