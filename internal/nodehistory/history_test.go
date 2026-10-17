package nodehistory

import (
	"context"
	"database/sql"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func metricNode(now time.Time) telemetry.Node {
	u := uint64(1000)
	disk := uint64(2000)
	load := 1.25
	tcp := uint64(12)
	return telemetry.Node{NodeID: "new-pro-de", Status: "online", MetricsCurrent: true, MetricsAt: &now,
		CPUPercent: 24.5, RAMUsed: 4 << 30, RAMTotal: 8 << 30, DiskUsedBytes: &u, DiskTotalBytes: &disk, Load1: &load, TCPInUse: &tcp}
}
func historyPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, "host-history.sqlite")
}
func TestDurableMinuteBucketsAndInstallationIsolation(t *testing.T) {
	now := time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)
	path := historyPath(t)
	h, err := Open(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	install := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	n := metricNode(now)
	ctx := context.Background()
	if err = h.Record(ctx, n, install); err != nil {
		t.Fatal(err)
	}
	// Multiple 5-second heartbeats in one minute must not inflate samples.
	now = now.Add(5 * time.Second)
	n.CPUPercent = 99
	if err = h.Record(ctx, n, install); err != nil {
		t.Fatal(err)
	}
	pts, err := h.Query(ctx, n.NodeID, install, "1h")
	if err != nil || len(pts) != 1 || pts[0].CPUPercent != 24.5 || pts[0].Observations != 1 {
		t.Fatalf("duplicated/overwrote minute: %+v %v", pts, err)
	}
	if pts[0].RXBitsPerSecond != nil || pts[0].TXBitsPerSecond != nil {
		t.Fatal("missing network readings fabricated zero")
	}
	now = now.Add(65 * time.Second)
	n.CPUPercent = 60.5
	if err = h.Record(ctx, n, install); err != nil {
		t.Fatal(err)
	}
	pts, err = h.Query(ctx, n.NodeID, install, "1h")
	if err != nil || len(pts) != 2 {
		t.Fatalf("two minutes expected: %v %v", pts, err)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	pts, err = reopened.Query(ctx, n.NodeID, install, "1h")
	if err != nil || len(pts) != 2 {
		t.Fatalf("lost samples across restart: %v %v", pts, err)
	}
	other, err := reopened.Query(ctx, n.NodeID, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "1h")
	if err != nil || len(other) != 0 {
		t.Fatalf("leaked previous installation: %v %v", other, err)
	}
	if _, err := reopened.Query(ctx, n.NodeID, install, "all"); !errors.Is(err, ErrWindow) {
		t.Fatalf("accepted unlimited history request: %v", err)
	}
}
func TestHistoryRetention24hAndFiveMinuteBuckets(t *testing.T) {
	now := time.Date(2026, 10, 17, 1, 0, 0, 0, time.UTC)
	h, err := Open(historyPath(t), func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	n := metricNode(now)
	install := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ctx := context.Background()
	for i := 0; i < 26; i++ {
		n.MetricsAt = &now
		n.CPUPercent = float64(i)
		if err = h.Record(ctx, n, install); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Hour)
	}
	now = now.Add(-10 * time.Minute)
	recent, err := h.Query(ctx, n.NodeID, install, "1h")
	if err != nil || len(recent) != 1 {
		t.Fatalf("wrong recent bucket count: %v %v", recent, err)
	}
	day, err := h.Query(ctx, n.NodeID, install, "24h")
	if err != nil || len(day) != 24 {
		t.Fatalf("wrong bounded day points: %d %v", len(day), err)
	}
	for _, p := range day {
		if p.TSUnix < now.Add(-24*time.Hour).Unix() {
			t.Fatal("out-of-window data leaked")
		}
	}
}
func TestHistoryMissingMetricsAndPrivatePath(t *testing.T) {
	path := historyPath(t)
	h, err := Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	install := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	now := time.Now()
	n := metricNode(now)
	n.MetricsCurrent = false
	if err = h.Record(context.Background(), n, install); !errors.Is(err, ErrInvalid) {
		t.Fatal("stale metrics accepted")
	}
	n.MetricsCurrent = true
	tooLarge := uint64(3000)
	n.DiskUsedBytes = &tooLarge
	if err = h.Record(context.Background(), n, install); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid disk telemetry accepted")
	}
	n.DiskUsedBytes = nil
	n.DiskTotalBytes = nil
	if err = h.Record(context.Background(), n, install); err != nil {
		t.Fatal(err)
	}
	points, err := h.Query(context.Background(), n.NodeID, install, "1h")
	if err != nil || len(points) != 1 || points[0].DiskUsedBytes != nil || points[0].DiskTotalBytes != nil {
		t.Fatal("invalid missing-disk reporting")
	}
	public := t.TempDir()
	if err = os.Chmod(public, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(filepath.Join(public, "world.sqlite"), nil); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("unsafe dir accepted: %v", err)
	}
	link := filepath.Join(t.TempDir(), "linked")
	if err = os.Symlink(filepath.Dir(path), link); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(filepath.Join(link, "sneaky.sqlite"), nil); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("symlink accepted: %v", err)
	}
}

func TestHistoryRefusesUnrelatedVersionOneSQLite(t *testing.T) {
	path := historyPath(t)
	d, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec("CREATE TABLE unrelated(id INTEGER); PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Open(path, nil); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("accepted unrelated v1 database: %v", err)
	}
}
