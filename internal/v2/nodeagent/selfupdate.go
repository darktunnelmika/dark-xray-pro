package nodeagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// FileSHA returns the hex sha256 of a file.
func FileSHA(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Fetch downloads one HUB distribution file to w.
func (h *HTTPHub) Fetch(ctx context.Context, file string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.BaseURL+wire.PathDist+file, nil)
	if err != nil {
		return err
	}
	req.Header.Set(wire.HeaderNodeID, h.NodeID)
	req.Header.Set("Authorization", "Bearer "+h.Token)
	// The shared client has a short timeout meant for small JSON calls.
	c := &http.Client{Transport: h.Client.Transport}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hub %s: %s", file, resp.Status)
	}
	_, err = io.Copy(w, io.LimitReader(resp.Body, 256<<20))
	return err
}

// Fetcher downloads distribution files.
type Fetcher interface {
	Fetch(ctx context.Context, file string, w io.Writer) error
}

// SelfUpdateTask replaces exe with the HUB's dark-node after checking its
// sha256 and that it runs on this machine. The old binary is kept as
// exe+".prev". The result asks the agent to restart.
func SelfUpdateTask(f Fetcher, exe string) TaskFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var a struct {
			SHA256 string `json:"sha256"`
		}
		if err := json.Unmarshal(raw, &a); err != nil || len(a.SHA256) != 64 {
			return nil, errors.New("bad update arguments")
		}
		cur, _ := FileSHA(exe)
		if strings.EqualFold(cur, a.SHA256) {
			return map[string]string{"from": cur, "to": cur, "note": "already current"}, nil
		}
		tmp := exe + ".new"
		out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			if errors.Is(err, syscall.EROFS) {
				return nil, errors.New("agent directory is read-only (installed by an older installer): use the new install link once")
			}
			return nil, err
		}
		h := sha256.New()
		err = f.Fetch(ctx, "dark-node", io.MultiWriter(out, h))
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(tmp)
			return nil, fmt.Errorf("download: %w", err)
		}
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.SHA256) {
			os.Remove(tmp)
			return nil, fmt.Errorf("checksum mismatch: got %s", got[:12])
		}
		// A binary for the wrong architecture or a broken build fails here,
		// before anything is replaced.
		if b, err := exec.CommandContext(ctx, tmp, "-version").CombinedOutput(); err != nil {
			os.Remove(tmp)
			return nil, fmt.Errorf("new binary does not run: %v %s", err, strings.TrimSpace(string(b)))
		}
		_ = os.Remove(exe + ".prev")
		if err := os.Link(exe, exe+".prev"); err != nil {
			_ = copyFile(exe, exe+".prev")
		}
		if err := os.Rename(tmp, exe); err != nil {
			os.Remove(tmp)
			return nil, err
		}
		if d, err := os.Open(filepath.Dir(exe)); err == nil {
			_ = d.Sync()
			d.Close()
		}
		return Restart{Value: map[string]string{"from": cur, "to": a.SHA256}}, nil
	}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
