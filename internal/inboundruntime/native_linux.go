//go:build linux

// Package inboundruntime owns the processes of one explicit PRO installation.
// It never discovers or adopts an existing Xray process or configuration.
package inboundruntime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

const nativeProtocol = "darkpro-inbound-v2/"
const apiTag = "dark-pro-usage-api"

var (
	ErrRuntime     = errors.New("inbound_engine_unavailable")
	ErrProof       = errors.New("inbound_accounting_proof_unavailable")
	ErrLease       = errors.New("inbound_execution_lease_expired")
	ErrIdentity    = errors.New("inbound_process_identity_mismatch")
	ErrUncertain   = errors.New("inbound_accounting_tail_uncertain")
	ErrPersistence = errors.New("inbound_journal_durability_failed")
)

// Binary pins an open descriptor, so a path replacement cannot change the
// executable between validation, configuration checks and process creation.
type Binary struct {
	file             *os.File
	SHA256           string
	featureMu        sync.Mutex
	featuresChecked  bool
	tunDNSActivation bool
}

func OpenBinary(path, digest string) (*Binary, error) {
	if !filepath.IsAbs(path) || len(digest) != 64 || strings.ToLower(digest) != digest {
		return nil, ErrRuntime
	}
	if _, e := hex.DecodeString(digest); e != nil {
		return nil, ErrRuntime
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if e != nil {
		return nil, ErrRuntime
	}
	f := os.NewFile(uintptr(fd), path)
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
		}
	}()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0022 != 0 || st.Mode().Perm()&0111 == 0 {
		return nil, ErrRuntime
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil || hex.EncodeToString(h.Sum(nil)) != digest {
		return nil, ErrRuntime
	}
	ok = true
	return &Binary{file: f, SHA256: digest}, nil
}
func (b *Binary) Close() error {
	if b == nil || b.file == nil {
		return nil
	}
	return b.file.Close()
}
func (b *Binary) command(ctx context.Context, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, "/proc/self/fd/3", args...)
	c.ExtraFiles = []*os.File{b.file}
	return c
}

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > b.limit {
		return 0, errors.New("engine_output_limit")
	}
	_, e := b.Buffer.Write(p)
	return n, e
}

type Native struct {
	mu           sync.Mutex
	binary       *Binary
	cmd          *exec.Cmd
	done         chan struct{}
	dir          string
	apiDirectory *os.File
	apiSocket    string
	endpoint     string
	identity     usagecheckpoint.ProcessToken
	configSHA    string
	principals   []inboundexec.Principal
	seen         map[string]bool
	last         map[string]inboundexec.Counter
	deadline     time.Time
	leaseSerial  uint64
	timer        *time.Timer
	ending       bool
	activated    bool
}

func randomID(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

// nativeConfig freezes explicitly typed local TLS files and adds the owned
// filesystem Unix StatsService. The actual derived bytes are bound to the process
// incarnation; the separately signed reviewed bundle retains its own hash.
func nativeConfig(raw []byte, apiSocket string) ([]byte, error) {
	if !filepath.IsAbs(apiSocket) || len(apiSocket) > 100 || strings.ContainsAny(apiSocket, "\x00\r\n") {
		return nil, ErrRuntime
	}
	var cfg map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&cfg) != nil || cfg == nil {
		return nil, ErrRuntime
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, ErrRuntime
	}
	if err := freezeNativeTLSAssets(cfg); err != nil {
		return nil, err
	}
	if _, ok := cfg["api"]; ok {
		return nil, ErrRuntime
	}
	ins, _ := cfg["inbounds"].([]any)
	for _, v := range ins {
		m, ok := v.(map[string]any)
		if !ok || m["tag"] == apiTag {
			return nil, ErrRuntime
		}
	}
	ins = append(ins, map[string]any{"tag": apiTag, "listen": apiSocket, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}})
	cfg["inbounds"] = ins
	cfg["api"] = map[string]any{"tag": apiTag, "services": []string{"StatsService"}}
	cfg["stats"] = map[string]any{}
	policy, _ := cfg["policy"].(map[string]any)
	if policy == nil {
		policy = map[string]any{}
	}
	levels, _ := policy["levels"].(map[string]any)
	if levels == nil {
		levels = map[string]any{}
	}
	level, _ := levels["0"].(map[string]any)
	if level == nil {
		level = map[string]any{}
	}
	level["statsUserUplink"] = true
	level["statsUserDownlink"] = true
	levels["0"] = level
	policy["levels"] = levels
	system, _ := policy["system"].(map[string]any)
	if system == nil {
		system = map[string]any{}
	}
	system["statsInboundUplink"] = true
	system["statsInboundDownlink"] = true
	policy["system"] = system
	cfg["policy"] = policy
	routing, _ := cfg["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
	}
	rules, _ := routing["rules"].([]any)
	routing["rules"] = append([]any{map[string]any{"type": "field", "inboundTag": []string{apiTag}, "outboundTag": apiTag}}, rules...)
	cfg["routing"] = routing
	// Never print customer destinations or credentials to the service log.
	cfg["log"] = map[string]any{"loglevel": "none"}
	return json.Marshal(cfg)
}

func reservePort() (uint16, error) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return 0, e
	}
	p := uint16(l.Addr().(*net.TCPAddr).Port)
	e = l.Close()
	return p, e
}

// ValidateNative is an offline check against the exact descriptor used for
// execution. It does not bind customer listeners or activate the admission gate.
func ValidateNative(ctx context.Context, b *Binary, raw []byte) error {
	if b == nil {
		return ErrRuntime
	}
	dir, e := os.MkdirTemp("", "dark-pro-inbound-check-")
	if e != nil {
		return ErrRuntime
	}
	defer os.RemoveAll(dir)
	cfg, e := nativeConfig(raw, filepath.Join(dir, "stats.sock"))
	if e != nil {
		return e
	}
	path := filepath.Join(dir, "candidate.json")
	if e = os.WriteFile(path, cfg, 0600); e != nil {
		return ErrRuntime
	}
	check, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	cmd := b.command(check, "run", "-test", "-config", path)
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + dir}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if cmd.Run() != nil {
		return ErrRuntime
	}
	return nil
}

func StartNative(ctx context.Context, dir string, b *Binary, bundle inboundexec.Bundle, deadline time.Time) (*Native, error) {
	if b == nil || !deadline.After(time.Now()) || deadline.After(time.Now().Add(31*time.Second)) {
		return nil, ErrLease
	}
	if err := guardTUNDNSActivation(ctx, b, bundle); err != nil {
		return nil, err
	}
	childDir, e := os.MkdirTemp(dir, "birth-")
	if e != nil {
		return nil, ErrRuntime
	}
	apiDirectory, e := os.Open(childDir)
	if e != nil {
		_ = os.RemoveAll(childDir)
		return nil, ErrRuntime
	}
	// The held directory descriptor keeps the control pathname short while
	// resolving to the private birth directory. Another UID cannot traverse
	// this process's descriptor directory or the 0700 destination directory.
	apiSocket := fmt.Sprintf("/proc/%d/fd/%d/stats.sock", os.Getpid(), apiDirectory.Fd())
	success := false
	defer func() {
		if !success {
			_ = apiDirectory.Close()
			_ = os.RemoveAll(childDir)
		}
	}()
	raw, e := nativeConfig(bundle.NativeConfig, apiSocket)
	if e != nil {
		return nil, e
	}
	path := filepath.Join(childDir, "native.json")
	if e = os.WriteFile(path, raw, 0600); e != nil {
		return nil, ErrRuntime
	}
	cmd := b.command(context.Background(), "run", "-config", path)
	cmd.Dir = childDir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + childDir, "DARK_PRO_INBOUND_V2=1"}
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	p := &Native{binary: b, cmd: cmd, done: make(chan struct{}), dir: childDir, apiDirectory: apiDirectory, apiSocket: apiSocket, endpoint: "unix://" + apiSocket, principals: bundle.Principals, seen: map[string]bool{}, last: map[string]inboundexec.Counter{}}
	start := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		e := cmd.Start()
		start <- e
		if e == nil {
			_ = cmd.Wait()
		}
		close(p.done)
	}()
	if e = <-start; e != nil {
		<-p.done
		return nil, ErrRuntime
	}
	p.identity, e = usagecheckpoint.Process(cmd.Process.Pid)
	if e != nil {
		p.Stop()
		return nil, ErrIdentity
	}
	h := sha256.Sum256(raw)
	p.configSHA = hex.EncodeToString(h[:])
	if !p.Renew(deadline) {
		p.Stop()
		return nil, ErrLease
	}
	wait, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		if p.checkIdentity() != nil {
			p.Stop()
			return nil, ErrRuntime
		}
		if p.ownsUnixControl() {
			if os.Chmod(filepath.Join(childDir, "stats.sock"), 0600) != nil {
				p.Stop()
				return nil, ErrRuntime
			}
			markers, err := p.query(wait, nativeProtocol+"capabilities")
			if err == nil && len(markers) == 1 && markers[nativeProtocol+"scope/native-inbounds-v2"] == 1 {
				health, err := p.query(wait, nativeProtocol+"health")
				if err == nil && health[nativeProtocol+"engine-ready"] == 1 && health[nativeProtocol+"admission-open"] == 0 && p.resourcesReady(bundle.Listeners) {
					success = true
					return p, nil
				}
			}
		}
		select {
		case <-wait.Done():
			p.Stop()
			return nil, ErrRuntime
		case <-p.done:
			p.Stop()
			return nil, ErrRuntime
		case <-tick.C:
		}
	}
}

func (p *Native) checkIdentity() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return ErrIdentity
	}
	select {
	case <-p.done:
		return ErrIdentity
	default:
	}
	t, e := usagecheckpoint.Process(p.cmd.Process.Pid)
	if e != nil || t != p.identity {
		return ErrIdentity
	}
	return nil
}

func (p *Native) Identity() string {
	raw, _ := json.Marshal(inboundexec.ProcessMember{BootID: p.identity.BootID, PID: p.identity.PID, StartTicks: p.identity.StartTicks, BinarySHA: p.binary.SHA256, ConfigSHA: p.configSHA})
	return string(raw)
}

func (p *Native) Renew(deadline time.Time) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ending || p.checkIdentity() != nil {
		return false
	}
	now := time.Now()
	duration := deadline.Sub(now)
	if duration <= 0 || duration > 31*time.Second {
		return false
	}
	// Wire timestamps have no monotonic component. Convert once at receipt,
	// then let the monotonic timer expire its own renewal generation. A wall
	// clock step must neither extend admission nor turn a fired timer into a
	// permanent no-op.
	p.deadline = now.Add(duration)
	p.leaseSerial++
	serial := p.leaseSerial
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(duration, func() { p.expireLease(serial) })
	return true
}

func (p *Native) expireLease(serial uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.ending && p.leaseSerial == serial {
		p.ending = true
		if p.cmd != nil && p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	}
}

func (p *Native) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.ending = true
	if p.timer != nil {
		p.timer.Stop()
	}
	// This is the handle returned by our own cmd.Start, never an adopted PID.
	// Failing to read /proc must still stop that owned child: otherwise a
	// failed birth measurement would wait forever with a live listener.
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	p.mu.Unlock()
	if p.done != nil {
		<-p.done
	}
	if p.apiDirectory != nil {
		_ = p.apiDirectory.Close()
	}
	_ = os.RemoveAll(p.dir)
}

func (p *Native) query(ctx context.Context, pattern string) (map[string]uint64, error) {
	if p.checkIdentity() != nil {
		return nil, ErrIdentity
	}
	if !p.ownsUnixControl() {
		return nil, ErrIdentity
	}
	call, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := p.binary.command(call, "api", "statsquery", "--server="+p.endpoint, "--timeout=3", "-pattern="+pattern, "-reset=false")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + p.dir}
	var output boundedOutput
	output.limit = 8 << 20
	cmd.Stdout = &output
	cmd.Stderr = io.Discard
	if cmd.Run() != nil || p.checkIdentity() != nil {
		return nil, ErrProof
	}
	var body struct {
		Stat []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"`
		} `json:"stat"`
	}
	d := json.NewDecoder(bytes.NewReader(output.Bytes()))
	d.DisallowUnknownFields()
	if d.Decode(&body) != nil {
		return nil, ErrProof
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, ErrProof
	}
	values := map[string]uint64{}
	for _, entry := range body.Stat {
		if entry.Name == "" {
			return nil, ErrProof
		}
		if _, ok := values[entry.Name]; ok {
			return nil, ErrProof
		}
		s := string(entry.Value)
		if s == "" {
			s = "0"
		}
		if strings.HasPrefix(s, "\"") {
			if json.Unmarshal(entry.Value, &s) != nil {
				return nil, ErrProof
			}
		}
		v, e := strconv.ParseUint(s, 10, 63)
		if e != nil {
			return nil, ErrProof
		}
		values[entry.Name] = v
	}
	return values, nil
}

func validNonce(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, e := hex.DecodeString(s)
	return e == nil
}

func (p *Native) Activate(ctx context.Context, nonce string) error {
	if !validNonce(nonce) {
		return ErrProof
	}
	m, e := p.query(ctx, nativeProtocol+"activate/"+nonce)
	if e != nil || len(m) != 1 || m[nativeProtocol+"activated/"+nonce] != 1 {
		return ErrProof
	}
	p.activated = true
	return nil
}

func (p *Native) Health(ctx context.Context) error {
	m, e := p.query(ctx, nativeProtocol+"health")
	if e != nil || m[nativeProtocol+"engine-ready"] != 1 || m[nativeProtocol+"admission-open"] != 1 || m[nativeProtocol+"final-sealed"] != 0 {
		return ErrRuntime
	}
	return nil
}

func (p *Native) Counters(ctx context.Context) ([]inboundexec.Counter, error) {
	m, e := p.query(ctx, "")
	if e != nil {
		return nil, e
	}
	return p.counters(m)
}
func (p *Native) Final(ctx context.Context, nonce string) ([]inboundexec.Counter, error) {
	if !validNonce(nonce) {
		return nil, ErrProof
	}
	m, e := p.query(ctx, nativeProtocol+"final/"+nonce)
	if e != nil || m[nativeProtocol+"final/"+nonce] != 1 {
		return nil, ErrProof
	}
	delete(m, nativeProtocol+"final/"+nonce)
	return p.counters(m)
}

func (p *Native) counters(m map[string]uint64) ([]inboundexec.Counter, error) {
	known := map[string]bool{}
	out := make([]inboundexec.Counter, 0, len(p.principals))
	for _, b := range p.principals {
		if b.Engine != "" && b.Engine != "xray" {
			continue
		}
		if !strings.HasPrefix(b.Counter, "user>>>") && !strings.HasPrefix(b.Counter, "inbound>>>") {
			continue
		}
		prefix := strings.TrimSuffix(b.Counter, ">>>traffic") + ">>>traffic"
		u, d := prefix+">>>uplink", prefix+">>>downlink"
		known[u] = true
		known[d] = true
		up, uok := m[u]
		down, dok := m[d]
		if uok != dok || p.seen[b.ID] && (!uok || !dok) {
			return nil, ErrProof
		}
		// Counters are lazily allocated by Xray. Absence is zero only for a
		// never-observed identity in this continuously owned fresh process.
		if uok {
			p.seen[b.ID] = true
		}
		last := p.last[b.ID]
		if up < last.Upload || down < last.Download {
			return nil, ErrProof
		}
		c := inboundexec.Counter{PrincipalID: b.ID, Upload: up, Download: down}
		p.last[b.ID] = c
		out = append(out, c)
	}
	for name := range m {
		if strings.HasPrefix(name, "user>>>") && !known[name] {
			return nil, ErrProof
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PrincipalID < out[j].PrincipalID })
	return out, nil
}

func (p *Native) resourcesReady(ls []inboundexec.Listener) bool {
	for _, l := range ls {
		if l.Engine != "xray" {
			continue
		}
		switch l.ResourceKind {
		case "storage", "tun", "interface":
			continue
		}
		network := l.Network
		if network == "" {
			network = "tcp"
		}
		switch network {
		case "tcp,udp", "udp,tcp", "tcp_udp", "both":
			if !p.owns("tcp", l.Listen, l.Port) || !p.owns("udp", l.Listen, l.Port) {
				return false
			}
		case "udp":
			if !p.owns("udp", l.Listen, l.Port) {
				return false
			}
		default:
			if !p.owns("tcp", l.Listen, l.Port) {
				return false
			}
		}
	}
	return true
}

func (p *Native) owns(network, address string, port uint16) bool {
	if port == 0 {
		return false
	}
	owned := p.ownedSocketInodes()
	if owned == nil {
		return false
	}
	for _, suffix := range []string{"", "6"} {
		raw, e := os.ReadFile("/proc/net/" + network + suffix)
		if e != nil {
			continue
		}
		for _, line := range strings.Split(string(raw), "\n") {
			f := strings.Fields(line)
			if len(f) < 10 || !owned[f[9]] || network == "tcp" && f[3] != "0A" {
				continue
			}
			parts := strings.Split(f[1], ":")
			if len(parts) != 2 {
				continue
			}
			v, e := strconv.ParseUint(parts[1], 16, 16)
			if e != nil || uint16(v) != port {
				continue
			}
			addr, e := hex.DecodeString(parts[0])
			if e != nil {
				continue
			}
			for i := 0; i+4 <= len(addr); i += 4 {
				binary.BigEndian.PutUint32(addr[i:i+4], binary.LittleEndian.Uint32(addr[i:i+4]))
			}
			if net.ParseIP(address) != nil && net.IP(addr).Equal(net.ParseIP(address)) {
				return true
			}
		}
	}
	return false
}

func (p *Native) ownedSocketInodes() map[string]bool {
	fds, e := os.ReadDir(fmt.Sprintf("/proc/%d/fd", p.identity.PID))
	if e != nil {
		return nil
	}
	owned := map[string]bool{}
	for _, fd := range fds {
		s, e := os.Readlink(fmt.Sprintf("/proc/%d/fd/%s", p.identity.PID, fd.Name()))
		if e == nil && strings.HasPrefix(s, "socket:[") {
			owned[strings.TrimSuffix(strings.TrimPrefix(s, "socket:["), "]")] = true
		}
	}
	return owned
}

func (p *Native) ownsUnixControl() bool {
	if p.apiDirectory == nil || p.apiSocket == "" {
		return false
	}
	info, err := p.apiDirectory.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return false
	}
	info, err = os.Lstat(filepath.Join(p.dir, "stats.sock"))
	if err != nil || info.Mode()&os.ModeSocket == 0 {
		return false
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	if !ok || identity.Uid != uint32(os.Geteuid()) {
		return false
	}
	owned := p.ownedSocketInodes()
	if owned == nil {
		return false
	}
	raw, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 8 && fields[3] == "00010000" && fields[4] == "0001" && fields[5] == "01" && owned[fields[6]] && fields[7] == p.apiSocket {
			return true
		}
	}
	return false
}
