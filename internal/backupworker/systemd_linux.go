//go:build linux

package backupworker

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
)

type LinuxRuntime struct {
	config             Config
	uid, gid           uint32
	rootUID, rootGID   uint32 // fixed zero in production; injectable only in package filesystem tests
	archives, receipts string
	healthClient       *http.Client
}

func newRecoveryRuntime(c Config) (*LinuxRuntime, error) {
	if os.Geteuid() != 0 || c.Validate() != nil {
		return nil, ErrUnsafe
	}
	u, e := user.Lookup("darkprolab")
	if e != nil {
		return nil, ErrUnsafe
	}
	uid, e := strconv.ParseUint(u.Uid, 10, 32)
	if e != nil || uid == 0 {
		return nil, ErrUnsafe
	}
	gid, e := strconv.ParseUint(u.Gid, 10, 32)
	if e != nil || gid == 0 {
		return nil, ErrUnsafe
	}
	r := &LinuxRuntime{config: c, uid: uint32(uid), gid: uint32(gid), archives: ArchiveDirectory,
		receipts: filepath.Join(StateDirectory, "receipts"),
		healthClient: &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
	if validateBinary(c.HubBinary, c.HubSHA256, c.HubRevision) != nil {
		return nil, ErrUnsafe
	}
	return r, nil
}

// NewRecoveryRuntime deliberately does not require the backup writer or its
// archive filesystem. Their failure must never suppress a durable HUB resume.
func NewRecoveryRuntime(c Config) (*LinuxRuntime, error) { return newRecoveryRuntime(c) }

func NewLinuxRuntime(c Config) (*LinuxRuntime, error) {
	r, e := newRecoveryRuntime(c)
	if e != nil {
		return nil, e
	}
	if e = r.verifyExecutables(); e != nil {
		return nil, e
	}
	if canonical(r.archives) != nil {
		return nil, ErrUnsafe
	}
	st, e := os.Stat(r.archives)
	if e != nil || !st.IsDir() || !owner(st, 0) || st.Mode().Perm() != 0710 {
		return nil, ErrUnsafe
	}
	if st.Sys().(*syscall.Stat_t).Gid != r.gid || trustedAncestors(r.archives) != nil {
		return nil, ErrUnsafe
	}
	if e = os.Mkdir(r.receipts, 0700); e != nil && !os.IsExist(e) {
		return nil, ErrUnsafe
	}
	st, e = os.Lstat(r.receipts)
	if e != nil || !st.IsDir() || st.Mode().Perm()&0077 != 0 || !owner(st, 0) || canonical(r.receipts) != nil {
		return nil, ErrUnsafe
	}
	return r, nil
}

func (r *LinuxRuntime) UID() uint32 { return r.uid }
func (r *LinuxRuntime) verifyExecutables() error {
	if validateBinary(r.config.HubBinary, r.config.HubSHA256, r.config.HubRevision) != nil ||
		validateBinary(r.config.BackupBinary, r.config.BackupSHA256, r.config.BackupRevision) != nil {
		return ErrUnsafe
	}
	return nil
}

func systemctl(ctx context.Context, action string, extra ...string) (string, error) {
	// Neither action nor the fixed service target originates from API input.
	a := append([]string{action, UnitName}, extra...)
	c := exec.CommandContext(ctx, "/usr/bin/systemctl", a...)
	c.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	b, e := c.Output()
	if e != nil {
		return "", ErrUnsafe
	}
	return strings.TrimSpace(string(b)), nil
}

func unitState(ctx context.Context) (string, int, error) {
	b, e := systemctl(ctx, "show", "--property=ActiveState,MainPID", "--no-pager")
	if e != nil {
		return "", 0, e
	}
	var state string
	pid := -1
	for _, line := range strings.Split(b, "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if k == "ActiveState" {
			state = v
		}
		if k == "MainPID" {
			pid, _ = strconv.Atoi(v)
		}
	}
	if state == "" || pid < 0 {
		return "", 0, ErrUnsafe
	}
	return state, pid, nil
}

func readSmall(path string, limit int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, ErrUnsafe
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, ErrUnsafe
	}
	return b, nil
}

func (r *LinuxRuntime) checkRunning(ctx context.Context) error {
	state, pid, e := unitState(ctx)
	if e != nil || state != "active" || pid <= 0 {
		return ErrPreflight
	}
	proc := filepath.Join("/proc", strconv.Itoa(pid))
	got, e := os.Readlink(filepath.Join(proc, "exe"))
	if e != nil || got != r.config.HubBinary {
		return ErrPreflight
	}
	st, e := os.Stat(proc)
	if e != nil || !owner(st, r.uid) {
		return ErrPreflight
	}
	self, e := os.Readlink("/proc/self/ns/pid")
	if e != nil {
		return ErrPreflight
	}
	other, e := os.Readlink(filepath.Join(proc, "ns/pid"))
	if e != nil || self != other {
		return ErrPreflight
	}
	raw, e := readSmall(filepath.Join(proc, "environ"), 256<<10)
	if e != nil {
		return ErrPreflight
	}
	env := map[string]string{}
	for _, v := range strings.Split(string(raw), "\x00") {
		k, v, ok := strings.Cut(v, "=")
		if ok {
			env[k] = v
		}
	}
	if env["DARK_PRO_LAB_MODE"] != "1" || env[hubstate.EnableEnv] != "1" || env["DARK_PRO_ENABLE_OWNER_REPORT_BOT_V1"] != "0" || env["DARK_PRO_INBOUND_RUNTIME_DIR"] != "" {
		return ErrPreflight
	}
	for _, key := range []string{"DARK_PRO_ENABLE_INBOUND_EXECUTION", "DARK_PRO_ENABLE_REMOTE_METERED_ADMISSION", "DARK_PRO_ENABLE_ROUTE_TRANSACTIONS", "DARK_PRO_ENABLE_METERED_ROUTE_TRANSACTIONS"} {
		if env[key] != "" && env[key] != "0" {
			return ErrPreflight
		}
	}
	return nil
}

func (r *LinuxRuntime) Preflight(ctx context.Context) error {
	if e := r.verifyExecutables(); e != nil {
		return e
	}
	if e := r.checkCredentialLaunch(ctx); e != nil {
		return e
	}
	if !labAddressPresent() {
		return ErrPreflight
	}
	if e := r.checkRunning(ctx); e != nil {
		return e
	}
	if !r.safeStateRoot() {
		return ErrPreflight
	}
	return r.checkSpace()
}

func labAddressPresent() bool {
	addresses, e := net.InterfaceAddrs()
	if e != nil {
		return false
	}
	for _, a := range addresses {
		ip, _, e := net.ParseCIDR(a.String())
		if e == nil && ip.String() == LABIPv4 {
			return true
		}
	}
	return false
}

func (r *LinuxRuntime) safeStateRoot() bool {
	if canonical(HubStateDirectory) != nil {
		return false
	}
	st, e := os.Lstat(HubStateDirectory)
	if e != nil || !st.IsDir() || !owner(st, r.uid) || st.Mode().Perm()&0077 != 0 {
		return false
	}
	_, e = os.Lstat(filepath.Join(HubStateDirectory, hubstate.QuarantineName))
	return os.IsNotExist(e)
}

func (r *LinuxRuntime) checkSpace() error {
	f, e := openChecked(filepath.Join(HubStateDirectory, hubstate.InventoryName), r.uid, true, 1<<20)
	if e != nil {
		return ErrPreflight
	}
	defer f.Close()
	var inv hubstate.Inventory
	raw, e := io.ReadAll(io.LimitReader(f, 1<<20))
	if e != nil || strictJSON(raw, &inv) != nil || hubstate.ValidateArchiveInventory(inv) != nil || inv.Root != HubStateDirectory || inv.BuildModified || inv.SourceRevision != r.config.HubRevision {
		return ErrPreflight
	}
	// The rewritten inventory adds bounded per-resource snapshot metadata.
	required, e := r.requiredSpace(inv.Resources, int64(len(raw))+(8<<10))
	if e != nil {
		return e
	}
	var fs syscall.Statfs_t
	if syscall.Statfs(r.archives, &fs) != nil || fs.Bsize <= 0 || fs.Bavail < uint64((required+fs.Bsize-1)/fs.Bsize) {
		return ErrPreflight
	}
	return nil
}

func (r *LinuxRuntime) requiredSpace(resources []hubstate.Resource, metadataBytes int64) (int64, error) {
	if metadataBytes < 1 || metadataBytes > 2<<20 || metadataBytes > r.config.MaxPayloadBytes {
		return 0, ErrPreflight
	}
	payload := metadataBytes
	var sqliteWork, sidecarBytes int64
	for _, v := range resources {
		// Present is startup provenance. An optional registered file may have
		// been created since startup and the CLI will include it if it exists.
		if _, e := os.Lstat(v.Path); os.IsNotExist(e) && !v.Required {
			continue
		} else if e != nil {
			return 0, ErrPreflight
		}
		in, e := openChecked(v.Path, r.uid, v.Role != "agent_tls_cert", 64<<30)
		if e != nil {
			return 0, ErrPreflight
		}
		st, e := in.Stat()
		in.Close()
		if e != nil || st.Size() < 0 || payload > r.config.MaxPayloadBytes-st.Size() {
			return 0, ErrPreflight
		}
		payload += st.Size()
		if v.Role == "business" || v.Role == "node_history" || v.Role == "owner_report_db" {
			sqliteWork += st.Size()
			for _, suffix := range []string{"-wal", "-journal"} {
				p := v.Path + suffix
				st, e := os.Lstat(p)
				if os.IsNotExist(e) {
					continue
				}
				if e != nil || canonical(p) != nil || !st.Mode().IsRegular() || !owner(st, r.uid) || st.Mode().Perm()&0077 != 0 || st.Size() < 0 || st.Size() > 64<<30 {
					return 0, ErrPreflight
				}
				sqliteWork += st.Size()
				sidecarBytes += st.Size()
			}
		}
	}
	if payload <= 0 || sqliteWork > 128<<30 || sidecarBytes > r.config.MaxPayloadBytes-payload {
		return 0, ErrPreflight
	}
	// WAL/journal recovery can enlarge a snapshot beyond the main DB size.
	// Budget every sidecar as additional payload, all SQLite copy/VACUUM work,
	// incompressible ciphertext, and the later second inode used for sealing.
	snapshotBudget := payload + sidecarBytes
	cipherBudget := snapshotBudget + snapshotBudget/16 + 16<<20
	return max(snapshotBudget+2*sqliteWork+cipherBudget, 2*cipherBudget) + 64<<20, nil
}

func (r *LinuxRuntime) Stop(ctx context.Context) error {
	_, e := systemctl(ctx, "stop")
	if e != nil {
		return ErrStop
	}
	state, pid, e := unitState(ctx)
	if e != nil || pid != 0 || (state != "inactive" && state != "failed") {
		return ErrStop
	}
	return nil
}

func (r *LinuxRuntime) Start(ctx context.Context) error {
	// The target and immutable executable remain pinned across recovery.
	if !labAddressPresent() || !r.safeStateRoot() || validateBinary(r.config.HubBinary, r.config.HubSHA256, r.config.HubRevision) != nil {
		return ErrResume
	}
	if _, e := systemctl(ctx, "start"); e != nil {
		return ErrResume
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if r.checkRunning(ctx) == nil && r.healthy(ctx) == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrResume
		case <-tick.C:
		}
	}
}

func (r *LinuxRuntime) healthy(ctx context.Context) error {
	req, e := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:2087/api/v1/health", nil)
	if e != nil {
		return ErrResume
	}
	resp, e := r.healthClient.Do(req)
	if e != nil {
		return ErrResume
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrResume
	}
	var out struct {
		Status string `json:"status"`
	}
	d := json.NewDecoder(io.LimitReader(resp.Body, 64<<10))
	if d.Decode(&out) != nil || out.Status != "ok" {
		return ErrResume
	}
	return nil
}
