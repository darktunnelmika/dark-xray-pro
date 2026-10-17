// Package nodehistory stores bounded, non-billable host monitoring samples
// in an independent private SQLite database. It never contains customer IDs,
// subscription tokens, Agent credentials or accounting records.
package nodehistory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	_ "modernc.org/sqlite"
)

var ErrInvalid = errors.New("invalid history input")
var ErrUnsafe = errors.New("unsafe private telemetry history path")
var ErrWindow = errors.New("unsupported history window")
var identityRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
var installRE = regexp.MustCompile(`^[a-f0-9]{32}$`)

const MaxHistoryDBSize int64 = 256 * 1024 * 1024
const minutesPerDay = 24 * 60

// Point is a TIME-BUCKETED HOST-ONLY observation, never billable traffic.
// Sparse/missing periods are left missing, not filled with fabricated zeros.
type Point struct {
	TSUnix          int64    `json:"ts_unix"`
	CPUPercent      float64  `json:"cpu_percent"`
	RAMUsedBytes    float64  `json:"ram_used_bytes"`
	RAMTotalBytes   float64  `json:"ram_total_bytes"`
	DiskUsedBytes   *float64 `json:"disk_used_bytes"`
	DiskTotalBytes  *float64 `json:"disk_total_bytes"`
	Load1           *float64 `json:"load_1"`
	TCPInUse        *float64 `json:"tcp_inuse"`
	RXBitsPerSecond *float64 `json:"rx_bits_per_second"`
	TXBitsPerSecond *float64 `json:"tx_bits_per_second"`
	Observations    int      `json:"observations"`
}

type Store struct {
	db        *sql.DB
	mu        sync.Mutex
	clock     func() time.Time
	latest    map[string]int64
	lastPurge int64
}

func safePath(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Ext(path) != ".sqlite" {
		return "", ErrUnsafe
	}
	for dir := filepath.Dir(path); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return "", ErrUnsafe
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return "", err
	}
	parent, err := os.Lstat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0077 != 0 {
		return "", ErrUnsafe
	}
	fd, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err == nil {
		if e := fd.Close(); e != nil {
			return "", e
		}
	} else if errors.Is(err, os.ErrExist) {
		info, e := os.Lstat(path)
		if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > MaxHistoryDBSize {
			return "", ErrUnsafe
		}
	} else {
		return "", err
	}
	return path, nil
}

// Open checks permissions and refuses existing foreign databases or schemas.
// Only a newly requested private PRO path may be used by the HUB.
func Open(path string, clock func() time.Time) (*Store, error) {
	abs, err := safePath(path)
	if err != nil {
		return nil, err
	}
	if clock == nil {
		clock = time.Now
	}
	u := url.URL{Scheme: "file", Path: abs, RawQuery: "_pragma=busy_timeout(4000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) { _ = db.Close(); return nil, e }
	for _, pragma := range []string{
		"PRAGMA busy_timeout=4000",
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
		"PRAGMA trusted_schema=OFF",
		"PRAGMA wal_autocheckpoint=100",
		"PRAGMA max_page_count=65536",
	} {
		if _, err = db.Exec(pragma); err != nil {
			return fail(err)
		}
	}
	var version int
	if err = db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fail(err)
	}
	if version != 0 && version != 1 {
		return fail(ErrUnsafe)
	}
	if version == 0 {
		var foreign string
		e := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' LIMIT 1").Scan(&foreign)
		if e == nil {
			return fail(ErrUnsafe)
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return fail(e)
		}
	}
	// A pre-existing v1 SQLite file must be exactly our history schema.
	// Refuse unrelated version-1 tables rather than silently adopting them.
	if version == 1 {
		rows, e := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")
		if e != nil {
			return fail(e)
		}
		count := 0
		for rows.Next() {
			var name string
			if e = rows.Scan(&name); e != nil {
				break
			}
			if name != "samples" {
				e = ErrUnsafe
				break
			}
			count++
		}
		if e == nil {
			e = rows.Err()
		}
		_ = rows.Close()
		if e != nil {
			return fail(e)
		}
		if count != 1 {
			return fail(ErrUnsafe)
		}
	}
	statements := []string{
		`CREATE TABLE IF NOT EXISTS samples(
    node_id TEXT NOT NULL, installation_id TEXT NOT NULL, minute INTEGER NOT NULL,
    cpu REAL NOT NULL CHECK(cpu BETWEEN 0 AND 100),
    ram_used INTEGER NOT NULL, ram_total INTEGER NOT NULL,
    disk_used INTEGER, disk_total INTEGER,
    load1 REAL, tcp_inuse INTEGER, rx_bps REAL, tx_bps REAL,
    PRIMARY KEY(node_id,installation_id,minute)
  ) WITHOUT ROWID`,
		"CREATE INDEX IF NOT EXISTS samples_minute_idx ON samples(minute)",
		"PRAGMA user_version=1",
	}
	for _, stmt := range statements {
		if _, err = db.Exec(stmt); err != nil {
			return fail(err)
		}
	}
	h := &Store{db: db, clock: clock, latest: make(map[string]int64)}
	return h, nil
}
func (h *Store) Close() error { return h.db.Close() }

func numeric(p *uint64) any {
	if p == nil {
		return nil
	}
	return int64(*p)
}
func floatNumeric(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func validate(node telemetry.Node, installation string) error {
	if !identityRE.MatchString(node.NodeID) || !installRE.MatchString(installation) ||
		node.Status != "online" || !node.MetricsCurrent || node.MetricsAt == nil ||
		math.IsNaN(node.CPUPercent) || math.IsInf(node.CPUPercent, 0) || node.CPUPercent < 0 || node.CPUPercent > 100 ||
		node.RAMTotal == 0 || node.RAMTotal > math.MaxInt64 || node.RAMUsed > node.RAMTotal {
		return ErrInvalid
	}
	if (node.DiskTotalBytes == nil) != (node.DiskUsedBytes == nil) {
		return ErrInvalid
	}
	if node.DiskTotalBytes != nil && (*node.DiskTotalBytes == 0 || *node.DiskTotalBytes > math.MaxInt64 || *node.DiskUsedBytes > *node.DiskTotalBytes) {
		return ErrInvalid
	}
	if node.TCPInUse != nil && *node.TCPInUse > 1e8 {
		return ErrInvalid
	}
	if node.Load1 != nil && (math.IsNaN(*node.Load1) || math.IsInf(*node.Load1, 0) || *node.Load1 < 0 || *node.Load1 > 1e6) {
		return ErrInvalid
	}
	for _, n := range []*float64{node.RXBitsPerSec, node.TXBitsPerSec} {
		if n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0) || *n < 0 || *n > 1e15) {
			return ErrInvalid
		}
	}
	return nil
}

// Record writes at most once per minute per installation, without affecting
// the live pulse/heartbeat ACK if the noncritical history disk is unavailable.
func (h *Store) Record(ctx context.Context, node telemetry.Node, installation string) error {
	if err := validate(node, installation); err != nil {
		return err
	}
	now := h.clock().UTC()
	minute := now.Unix() / 60
	key := node.NodeID + ":" + installation
	h.mu.Lock()
	defer h.mu.Unlock()
	if last, ok := h.latest[key]; ok && minute <= last {
		return nil
	}
	_, err := h.db.ExecContext(ctx, `INSERT OR IGNORE INTO samples(node_id,installation_id,minute,cpu,ram_used,ram_total,disk_used,disk_total,load1,tcp_inuse,rx_bps,tx_bps)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, node.NodeID, installation, minute, node.CPUPercent, int64(node.RAMUsed), int64(node.RAMTotal),
		numeric(node.DiskUsedBytes), numeric(node.DiskTotalBytes), floatNumeric(node.Load1),
		numeric(node.TCPInUse), floatNumeric(node.RXBitsPerSec), floatNumeric(node.TXBitsPerSec))
	if err != nil {
		return err
	}
	h.latest[key] = minute
	if h.lastPurge == 0 || minute-h.lastPurge >= 60 {
		cutoff := minute - minutesPerDay
		if _, err = h.db.ExecContext(ctx, "DELETE FROM samples WHERE minute<?", cutoff); err != nil {
			return fmt.Errorf("nonbilling telemetry retention: %w", err)
		}
		h.lastPurge = minute
	}
	return nil
}

func (h *Store) Query(ctx context.Context, nodeID, installation, window string) ([]Point, error) {
	if !identityRE.MatchString(nodeID) || !installRE.MatchString(installation) {
		return nil, ErrInvalid
	}
	minutes, bucket := 60, int64(1)
	switch window {
	case "1h":
		minutes = 60
	case "24h":
		minutes = minutesPerDay
		bucket = 5
	default:
		return nil, ErrWindow
	}
	cutoff := h.clock().UTC().Unix()/60 - int64(minutes) + 1
	rows, err := h.db.QueryContext(ctx, `
 SELECT MAX(minute)*60,AVG(cpu),AVG(ram_used),AVG(ram_total),
 AVG(disk_used),AVG(disk_total),AVG(load1),AVG(tcp_inuse),
 AVG(rx_bps),AVG(tx_bps),COUNT(*)
 FROM samples WHERE node_id=? AND installation_id=? AND minute>=? AND minute<=?
 GROUP BY (minute/?) ORDER BY MAX(minute) ASC LIMIT 290
 `, nodeID, installation, cutoff, h.clock().UTC().Unix()/60, bucket)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Point, 0, 60)
	for rows.Next() {
		var p Point
		var diskUsed, diskTotal, load, tcp, rx, tx sql.NullFloat64
		if err = rows.Scan(&p.TSUnix, &p.CPUPercent, &p.RAMUsedBytes, &p.RAMTotalBytes,
			&diskUsed, &diskTotal, &load, &tcp, &rx, &tx, &p.Observations); err != nil {
			return nil, err
		}
		if diskUsed.Valid {
			p.DiskUsedBytes = &diskUsed.Float64
		}
		if diskTotal.Valid {
			p.DiskTotalBytes = &diskTotal.Float64
		}
		if load.Valid {
			p.Load1 = &load.Float64
		}
		if tcp.Valid {
			p.TCPInUse = &tcp.Float64
		}
		if rx.Valid {
			p.RXBitsPerSecond = &rx.Float64
		}
		if tx.Valid {
			p.TXBitsPerSecond = &tx.Float64
		}
		out = append(out, p)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
