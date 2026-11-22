package hubcore

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// HubVersion is the panel's release; the commit comes from the build.
const HubVersion = "v2.43.0"

var hubStarted = time.Now()

// SystemInfo is the HUB server's own health for the dashboard.
type SystemInfo struct {
	Version    string   `json:"version"`
	Commit     string   `json:"commit,omitempty"`
	BuiltAt    string   `json:"built_at,omitempty"`
	GoVersion  string   `json:"go"`
	Uptime     int64    `json:"uptime_sec"`
	HostUptime int64    `json:"host_uptime_sec"`
	CPU        float64  `json:"cpu_percent"`
	RxRate     float64  `json:"rx_rate"` // bytes/s, all interfaces but lo
	TxRate     float64  `json:"tx_rate"`
	Load1      float64  `json:"load1"`
	Cores      int      `json:"cores"`
	RAMUsed    uint64   `json:"ram_used"`
	RAMTotal   uint64   `json:"ram_total"`
	DiskUsed   uint64   `json:"disk_used"`
	DiskTotal  uint64   `json:"disk_total"`
	DiskPath   string   `json:"disk_path"`
	DBSize     int64    `json:"db_size"`
	Backups    int64    `json:"backups_size"`
	BackupN    int      `json:"backups"`
	Warnings   []string `json:"warnings"`
}

// DiskWarnPercent is where the dashboard and the owners' Telegram start
// warning about the HUB's disk.
const DiskWarnPercent = 85

func buildInfo() (commit, at string) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if len(s.Value) > 7 {
				commit = s.Value[:7]
			} else {
				commit = s.Value
			}
		case "vcs.time":
			at = s.Value
		}
	}
	return commit, at
}

var cpuSample struct {
	sync.Mutex
	idle, total uint64
	at          time.Time
	pct         float64
}

// cpuPercent compares /proc/stat with the previous call (≥1s apart).
func cpuPercent() float64 {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line, _, _ := strings.Cut(string(b), "\n")
	f := strings.Fields(line)
	if len(f) < 5 || f[0] != "cpu" {
		return 0
	}
	var total, idle uint64
	for i, x := range f[1:] {
		n, _ := strconv.ParseUint(x, 10, 64)
		total += n
		if i == 3 || i == 4 { // idle + iowait
			idle += n
		}
	}
	cpuSample.Lock()
	defer cpuSample.Unlock()
	if cpuSample.total > 0 && total > cpuSample.total {
		dt, di := total-cpuSample.total, idle-cpuSample.idle
		if time.Since(cpuSample.at) >= time.Second || cpuSample.pct == 0 {
			cpuSample.pct = 100 * float64(dt-di) / float64(dt)
		}
	}
	cpuSample.idle, cpuSample.total, cpuSample.at = idle, total, time.Now()
	return cpuSample.pct
}

var netSample struct {
	sync.Mutex
	rx, tx uint64
	at     time.Time
	rr, tr float64
}

// netRates compares /proc/net/dev with the previous call (≥1s apart).
func netRates() (rx, tx float64) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}
	var r, t uint64
	for _, l := range strings.Split(string(b), "\n")[2:] {
		name, rest, ok := strings.Cut(l, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "lo" {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		a, _ := strconv.ParseUint(f[0], 10, 64)
		c, _ := strconv.ParseUint(f[8], 10, 64)
		r += a
		t += c
	}
	netSample.Lock()
	defer netSample.Unlock()
	now := time.Now()
	if el := now.Sub(netSample.at).Seconds(); netSample.rx > 0 && el >= 1 && r >= netSample.rx && t >= netSample.tx {
		netSample.rr, netSample.tr = float64(r-netSample.rx)/el, float64(t-netSample.tx)/el
		netSample.rx, netSample.tx, netSample.at = r, t, now
	} else if netSample.rx == 0 || r < netSample.rx {
		netSample.rx, netSample.tx, netSample.at = r, t, now
	}
	return netSample.rr, netSample.tr
}

func memInfo() (used, total uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var avail uint64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), ":")
		n, _ := strconv.ParseUint(strings.Fields(v + " 0")[0], 10, 64)
		switch k {
		case "MemTotal":
			total = n * 1024
		case "MemAvailable":
			avail = n * 1024
		}
	}
	if avail > total {
		return 0, total
	}
	return total - avail, total
}

func dirSize(dir string) (size int64, n int) {
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if fi, err := d.Info(); err == nil {
				size += fi.Size()
				n++
			}
		}
		return nil
	})
	return size, n
}

// System reports the HUB's health.
func (s *Store) System(ctx context.Context) SystemInfo {
	commit, at := buildInfo()
	si := SystemInfo{Version: HubVersion, Commit: commit, BuiltAt: at, GoVersion: runtime.Version(),
		Uptime: int64(time.Since(hubStarted).Seconds()), Cores: runtime.NumCPU(), CPU: cpuPercent(), DiskPath: filepath.Dir(s.path), Warnings: []string{}}
	si.RxRate, si.TxRate = netRates()
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		si.Load1, _ = strconv.ParseFloat(strings.Fields(string(b) + " 0")[0], 64)
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		f, _ := strconv.ParseFloat(strings.Fields(string(b) + " 0")[0], 64)
		si.HostUptime = int64(f)
	}
	si.RAMUsed, si.RAMTotal = memInfo()
	var st syscall.Statfs_t
	if syscall.Statfs(si.DiskPath, &st) == nil {
		si.DiskTotal = st.Blocks * uint64(st.Bsize)
		si.DiskUsed = si.DiskTotal - st.Bavail*uint64(st.Bsize)
	}
	for _, suf := range []string{"", "-wal", "-shm"} {
		if fi, err := os.Stat(s.path + suf); err == nil {
			si.DBSize += fi.Size()
		}
	}
	si.Backups, si.BackupN = dirSize(filepath.Join(si.DiskPath, "backups"))
	if si.DiskTotal > 0 {
		if p := 100 * si.DiskUsed / si.DiskTotal; p >= DiskWarnPercent {
			si.Warnings = append(si.Warnings, "disk:"+strconv.FormatUint(p, 10))
		}
	}
	if si.RAMTotal > 0 && 100*si.RAMUsed/si.RAMTotal >= 92 {
		si.Warnings = append(si.Warnings, "ram:"+strconv.FormatUint(100*si.RAMUsed/si.RAMTotal, 10))
	}
	return si
}

// RunSystemWatch tells the owners (Telegram) once a day while the HUB's
// disk is nearly full.
func (s *Store) RunSystemWatch(ctx context.Context) {
	t := time.NewTicker(2 * time.Minute)
	defer t.Stop()
	for {
		s.watchLoad(ctx)
		si := s.System(ctx)
		if si.DiskTotal > 0 {
			p := 100 * si.DiskUsed / si.DiskTotal
			day := s.now().UTC().Format("2006-01-02")
			if p >= DiskWarnPercent && s.Setting(ctx, "disk_alert_day") != day {
				_ = s.SetSetting(ctx, "disk_alert_day", day)
				notifyOwners(ctx, "⚠️ دیسک سرور هاب "+strconv.FormatUint(p, 10)+"٪ پر است (آزاد: "+humanBytes(si.DiskTotal-si.DiskUsed)+
					"). اگر پر شود دیتابیس نمی‌تواند مصرف کاربرها را ثبت کند. فایل‌های قدیمی سرور را پاک کنید.")
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func humanBytes(n uint64) string {
	u := []string{"B", "KB", "MB", "GB", "TB"}
	f, i := float64(n), 0
	for f >= 1024 && i < len(u)-1 {
		f /= 1024
		i++
	}
	return strconv.FormatFloat(f, 'f', 1, 64) + " " + u[i]
}

// OnlineClient is one client connected right now on one node.
type OnlineClient struct {
	Email  string   `json:"email"`
	NodeID string   `json:"node_id"`
	IPs    []string `json:"ips"`
	Seen   int64    `json:"seen"`
	Today  int64    `json:"today"` // bytes on this node today
	RepID  string   `json:"rep_id,omitempty"`
}

// Online lists who is connected now (seen in the last few polls), newest first.
func (s *Store) Online(ctx context.Context) ([]OnlineClient, error) {
	since := s.now().Unix() - 3*PollSeconds
	day := s.now().UTC().Format("2006-01-02")
	rows, err := s.ro.QueryContext(ctx, `SELECT o.email,o.node_id,o.ips,o.seen_unix,COALESCE(u.up+u.down,0) FROM online o
LEFT JOIN usage_daily u ON u.email=o.email AND u.node_id=o.node_id AND u.day=? WHERE o.seen_unix>=? ORDER BY o.seen_unix DESC, o.email`, day, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OnlineClient{}
	for rows.Next() {
		var o OnlineClient
		var ips string
		if err := rows.Scan(&o.Email, &o.NodeID, &ips, &o.Seen, &o.Today); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(ips), &o.IPs)
		if o.IPs == nil {
			o.IPs = []string{}
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// NodeDay is one node's traffic on one day.
type NodeDay struct {
	Day    string `json:"day"`
	NodeID string `json:"node_id"`
	Up     int64  `json:"up"`
	Down   int64  `json:"down"`
}

// NodeTraffic sums client traffic per node and day.
func (s *Store) NodeTraffic(ctx context.Context, days int) ([]NodeDay, error) {
	if days <= 0 || days > 400 {
		days = 30
	}
	cut := s.now().AddDate(0, 0, -days+1).UTC().Format("2006-01-02")
	rows, err := s.ro.QueryContext(ctx, `SELECT day,node_id,SUM(up),SUM(down) FROM usage_daily WHERE day>=? GROUP BY day,node_id ORDER BY day,node_id`, cut)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NodeDay{}
	for rows.Next() {
		var d NodeDay
		if err := rows.Scan(&d.Day, &d.NodeID, &d.Up, &d.Down); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Day < out[j].Day })
	return out, rows.Err()
}
