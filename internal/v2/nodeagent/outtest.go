package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// OutboundTestArgs is what the HUB sends for an outbound check.
type OutboundTestArgs struct {
	Outbounds []json.RawMessage `json:"outbounds"` // the node's outbounds (chains need them all)
	Tag       string            `json:"tag"`
	URL       string            `json:"url"` // default: a 204 probe
}

// OutboundTestResult is the outcome shown in the panel.
type OutboundTestResult struct {
	OK     bool   `json:"ok"`
	Ms     int64  `json:"ms"`     // best of two requests, connection included
	Status int    `json:"status"` // HTTP status of the probe
	IP     string `json:"ip,omitempty"`
	Loc    string `json:"loc,omitempty"`  // country the traffic comes out in (two letters)
	Colo   string `json:"colo,omitempty"` // Cloudflare's site that answered
	Warp   string `json:"warp,omitempty"` // "on" / "plus" when the exit is Cloudflare WARP
	Error  string `json:"error,omitempty"`
}

// parseTrace reads Cloudflare's /cdn-cgi/trace: key=value lines.
func parseTrace(body string) map[string]string {
	out := map[string]string{}
	for _, l := range strings.Split(body, "\n") {
		if k, v, ok := strings.Cut(strings.TrimSpace(l), "="); ok {
			out[k] = v
		}
	}
	return out
}

// OutboundTestTask runs a throwaway Xray with a local SOCKS port that sends
// everything to one outbound, and measures a probe through it. The node's
// running Xray is never touched.
func OutboundTestTask(s *Supervisor) TaskFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var a OutboundTestArgs
		if err := json.Unmarshal(raw, &a); err != nil || a.Tag == "" || len(a.Outbounds) == 0 {
			return nil, errors.New("bad outbound test arguments")
		}
		if a.URL == "" {
			a.URL = "https://www.gstatic.com/generate_204"
		}
		return runOutboundTest(ctx, s.Binary, s.Dir, a), nil
	}
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func runOutboundTest(ctx context.Context, bin, dir string, a OutboundTestArgs) OutboundTestResult {
	fail := func(f string, x ...any) OutboundTestResult { return OutboundTestResult{Error: fmt.Sprintf(f, x...)} }
	port, err := freePort()
	if err != nil {
		return fail("no local port: %v", err)
	}
	// Rules an earlier, killed test left in the kernel would misroute this one.
	sweepStaleTunRules(ctx)
	defer sweepStaleTunRules(context.WithoutCancel(ctx))
	a.Outbounds = userspaceWireGuard(a.Outbounds)
	cfg := map[string]any{
		"log": map[string]any{"loglevel": "warning"},
		"inbounds": []any{map[string]any{"tag": "dark-test-in", "listen": "127.0.0.1", "port": port, "protocol": "socks",
			"settings": map[string]any{"udp": false}}},
		"outbounds": a.Outbounds,
		"routing":   map[string]any{"rules": []any{map[string]any{"type": "field", "inboundTag": []string{"dark-test-in"}, "outboundTag": a.Tag}}},
	}
	b, _ := json.Marshal(cfg)
	if dir == "" {
		dir = os.TempDir()
	}
	f, err := os.CreateTemp(dir, "outbound-test-*.json")
	if err != nil {
		return fail("temp file: %v", err)
	}
	defer os.Remove(f.Name())
	_, _ = f.Write(b)
	f.Close()
	tctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	cmd := exec.CommandContext(tctx, bin, "run", "-c", f.Name())
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return fail("start xray: %v", err)
	}
	defer func() {
		// Ask first, so Xray can take down what it set up; kill only if it lingers.
		_ = cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
	}()
	ready := false
	for i := 0; i < 40 && !ready; i++ {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(port), 200*time.Millisecond); err == nil {
			c.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		msg := strings.TrimSpace(out.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return fail("xray did not start: %s", msg)
	}
	proxy, _ := url.Parse("socks5h://127.0.0.1:" + strconv.Itoa(port))
	client := &http.Client{Timeout: 12 * time.Second, Transport: &http.Transport{Proxy: http.ProxyURL(proxy), DisableKeepAlives: true}}
	res := OutboundTestResult{}
	var best int64
	var lastErr error
	for i := 0; i < 2; i++ {
		start := time.Now()
		resp, err := client.Get(a.URL)
		if err != nil {
			lastErr = err
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
		ms := time.Since(start).Milliseconds()
		if best == 0 || ms < best {
			best = ms
		}
		res.Status = resp.StatusCode
	}
	if best == 0 {
		msg := "no answer"
		if lastErr != nil {
			msg = lastErr.Error()
		}
		return fail("%s", msg)
	}
	res.OK, res.Ms = res.Status > 0 && res.Status < 500, best
	// Where the traffic comes out, as Cloudflare sees it.
	if resp, err := client.Get("https://www.cloudflare.com/cdn-cgi/trace"); err == nil {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		tr := parseTrace(string(b))
		if net.ParseIP(tr["ip"]) != nil {
			res.IP = tr["ip"]
		}
		if len(tr["loc"]) == 2 {
			res.Loc = strings.ToUpper(tr["loc"])
		}
		res.Colo, res.Warp = tr["colo"], tr["warp"]
		if len(res.Colo) > 8 || len(res.Warp) > 8 {
			res.Colo, res.Warp = "", ""
		}
	}
	if res.IP != "" {
		return res
	}
	if resp, err := client.Get("https://api.ipify.org"); err == nil {
		ip, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
		resp.Body.Close()
		if net.ParseIP(strings.TrimSpace(string(ip))) != nil {
			res.IP = strings.TrimSpace(string(ip))
		}
	}
	return res
}

// userspaceWireGuard makes every WireGuard outbound (WARP is one) run inside
// the test's own Xray instead of a kernel TUN. The node's real Xray already
// holds that TUN with the same addresses; a second one beside it adds
// routing rules of its own, and a test that is killed leaves them behind,
// which then breaks WARP for the real Xray too.
func userspaceWireGuard(outs []json.RawMessage) []json.RawMessage {
	res := make([]json.RawMessage, len(outs))
	for i, o := range outs {
		res[i] = o
		var m map[string]any
		if json.Unmarshal(o, &m) != nil || m["protocol"] != "wireguard" {
			continue
		}
		set, _ := m["settings"].(map[string]any)
		if set == nil {
			continue
		}
		set["noKernelTun"] = true
		if b, err := json.Marshal(m); err == nil {
			res[i] = b
		}
	}
	return res
}

// staleTunRules picks, from "ip rule" output, the priorities of rules that
// point at one of Xray's WireGuard routing tables (10230 and up) which has
// no routes left: what a killed Xray leaves behind.
func staleTunRules(rules string, tableEmpty func(table string) bool) []string {
	var prefs []string
	for _, line := range strings.Split(rules, "\n") {
		pref, rest, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		for i := 0; i+1 < len(f); i++ {
			if f[i] != "lookup" {
				continue
			}
			if n, err := strconv.Atoi(f[i+1]); err == nil && n >= 10230 && n < 11000 && tableEmpty(f[i+1]) {
				prefs = append(prefs, strings.TrimSpace(pref))
			}
		}
	}
	return prefs
}

// sweepStaleTunRules removes those rules, for IPv4 and IPv6.
func sweepStaleTunRules(ctx context.Context) {
	for _, fam := range []string{"-4", "-6"} {
		out, err := exec.CommandContext(ctx, "ip", fam, "rule", "show").Output()
		if err != nil {
			continue
		}
		empty := func(table string) bool {
			r, err := exec.CommandContext(ctx, "ip", fam, "route", "show", "table", table).Output()
			return err != nil || strings.TrimSpace(string(r)) == ""
		}
		for _, pref := range staleTunRules(string(out), empty) {
			_ = exec.CommandContext(ctx, "ip", fam, "rule", "del", "pref", pref).Run()
		}
	}
}
