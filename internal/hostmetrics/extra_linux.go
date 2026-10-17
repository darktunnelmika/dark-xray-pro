package hostmetrics

import (
	"errors"
	"math"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// These are HOST indicators, not Xray users, billable usage or subscriber
// connections. Independent read failures produce UNKNOWN (nil), not zero.
func parseLoad(data string) (*float64, *float64, *float64, error) {
	fields := strings.Fields(data)
	if len(fields) < 3 {
		return nil, nil, nil, errors.New("invalid /proc/loadavg")
	}
	vals := [3]float64{}
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseFloat(fields[i], 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e6 {
			return nil, nil, nil, errors.New("invalid system load")
		}
		vals[i] = n
	}
	return &vals[0], &vals[1], &vals[2], nil
}
func parseUptime(data string) (*uint64, error) {
	fields := strings.Fields(data)
	if len(fields) < 1 {
		return nil, errors.New("missing uptime")
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e12 {
		return nil, errors.New("invalid uptime")
	}
	value := uint64(n)
	return &value, nil
}
func parseTCPInUse(data string) (*uint64, error) {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "TCP:" {
			continue
		}
		for i := 1; i+1 < len(fields); i += 2 {
			if fields[i] == "inuse" {
				n, err := strconv.ParseUint(fields[i+1], 10, 64)
				if err != nil || n > 1e8 {
					return nil, errors.New("invalid TCP socket count")
				}
				return &n, nil
			}
		}
	}
	return nil, errors.New("TCP inuse missing")
}
func enrich(m *Raw) {
	if m == nil {
		return
	}
	var info syscall.Statfs_t
	if err := syscall.Statfs("/", &info); err == nil && info.Bsize > 0 && info.Blocks > 0 && info.Bfree <= info.Blocks {
		size := uint64(info.Bsize)
		if info.Blocks <= math.MaxInt64/size {
			total := info.Blocks * size
			used := (info.Blocks - info.Bfree) * size
			m.DiskTotal = &total
			m.DiskUsed = &used
		}
	}
	if bytes, err := os.ReadFile("/proc/loadavg"); err == nil {
		if a, b, c, e := parseLoad(string(bytes)); e == nil {
			m.Load1, m.Load5, m.Load15 = a, b, c
		}
	}
	if bytes, err := os.ReadFile("/proc/uptime"); err == nil {
		if n, e := parseUptime(string(bytes)); e == nil {
			m.UptimeSec = n
		}
	}
	if bytes, err := os.ReadFile("/proc/net/sockstat"); err == nil {
		if n, e := parseTCPInUse(string(bytes)); e == nil {
			m.TCPInUse = n
		}
	}
}
