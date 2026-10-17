package hostmetrics

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Raw struct {
	TotalCPU  uint64
	IdleCPU   uint64
	RAMTotal  uint64
	RAMUsed   uint64
	RXBytes   uint64
	TXBytes   uint64
	Interface string
	DiskTotal *uint64
	DiskUsed  *uint64
	Load1     *float64
	Load5     *float64
	Load15    *float64
	UptimeSec *uint64
	TCPInUse  *uint64
}

func ParseCPU(data string) (total, idle uint64, err error) {
	lines := strings.SplitN(data, "\n", 2)
	fields := strings.Fields(lines[0])
	if len(fields) < 8 || fields[0] != "cpu" {
		return 0, 0, errors.New("invalid aggregate cpu line")
	}
	for i := 1; i < len(fields) && i <= 8; i++ {
		value, e := strconv.ParseUint(fields[i], 10, 64)
		if e != nil {
			return 0, 0, errors.New("invalid cpu counter")
		}
		total += value
		if i == 4 || i == 5 {
			idle += value
		}
	}
	if total == 0 || idle > total {
		return 0, 0, errors.New("invalid cpu totals")
	}
	return total, idle, nil
}

func ParseMemory(data string) (total, used uint64, err error) {
	vals := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "kB" {
			continue
		}
		if fields[0] != "MemTotal:" && fields[0] != "MemAvailable:" {
			continue
		}
		v, e := strconv.ParseUint(fields[1], 10, 64)
		if e != nil {
			return 0, 0, e
		}
		vals[fields[0]] = v
	}
	t, hasTotal := vals["MemTotal:"]
	available, hasAvailable := vals["MemAvailable:"]
	if !hasTotal || !hasAvailable || t == 0 || available > t {
		return 0, 0, errors.New("invalid memory counters")
	}
	return t * 1024, (t - available) * 1024, nil
}

func ParseDefaultInterface(data string) (string, error) {
	for i, line := range strings.Split(data, "\n") {
		if i == 0 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[1] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&1 == 0 {
			continue
		}
		name := fields[0]
		if !validIface(name) || name == "lo" {
			continue
		}
		return name, nil
	}
	return "", errors.New("no non-loopback default network interface")
}

func validIface(name string) bool {
	if len(name) == 0 || len(name) > 32 {
		return false
	}
	for _, ch := range name {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '-' || ch == '_' || ch == '.' {
			continue
		}
		return false
	}
	return true
}

func readUint(path string) (uint64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
}

// Read reads real Linux host stats; missing counters are errors, never mock zeroes.
func Read() (Raw, error) {
	var m Raw
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return m, err
	}
	m.TotalCPU, m.IdleCPU, err = ParseCPU(string(data))
	if err != nil {
		return m, err
	}
	data, err = os.ReadFile("/proc/meminfo")
	if err != nil {
		return m, err
	}
	m.RAMTotal, m.RAMUsed, err = ParseMemory(string(data))
	if err != nil {
		return m, err
	}
	data, err = os.ReadFile("/proc/net/route")
	if err != nil {
		return m, err
	}
	m.Interface, err = ParseDefaultInterface(string(data))
	if err != nil {
		return m, err
	}
	dir := filepath.Join("/sys/class/net", m.Interface, "statistics")
	m.RXBytes, err = readUint(filepath.Join(dir, "rx_bytes"))
	if err != nil {
		return m, fmt.Errorf("network RX unavailable: %w", err)
	}
	m.TXBytes, err = readUint(filepath.Join(dir, "tx_bytes"))
	if err != nil {
		return m, fmt.Errorf("network TX unavailable: %w", err)
	}
	enrich(&m)
	return m, nil
}

// CPUPercent compares consecutive aggregate CPU samples and does not claim
// that the first sample or an invalid reset represents an actual 0% CPU.
func CPUPercent(before, after Raw) (float64, bool) {
	if after.TotalCPU <= before.TotalCPU || after.IdleCPU < before.IdleCPU {
		return 0, false
	}
	dt := after.TotalCPU - before.TotalCPU
	idle := after.IdleCPU - before.IdleCPU
	if idle > dt {
		return 0, false
	}
	return 100 * float64(dt-idle) / float64(dt), true
}
