package hostmetrics

import "testing"

func TestParseCPU(t *testing.T) {
    total, idle, err := ParseCPU("cpu  100 20 30 400 10 0 0 0 0 0\ncpu0 50 10\n")
    if err != nil || total != 560 || idle != 410 {
        t.Fatalf("bad CPU totals: %d %d %v", total, idle, err)
    }
    if _, _, err := ParseCPU("not-cpu 1 2"); err == nil { t.Fatal("invalid cpu accepted") }
}

func TestMemoryAndInterface(t *testing.T) {
    total, used, err := ParseMemory("MemTotal:        8192 kB\nMemAvailable:   2048 kB\n")
    if err != nil || total != 8192*1024 || used != 6144*1024 {
        t.Fatalf("bad RAM: %d %d %v", total, used, err)
    }
    iface, err := ParseDefaultInterface("Iface Destination Gateway Flags RefCnt Use Metric Mask\nens3 00000000 01010101 0003 0 0 10 00000000\n")
    if err != nil || iface != "ens3" { t.Fatalf("interface %s: %v", iface, err) }
    if _, err := ParseDefaultInterface("Iface Destination Gateway Flags\nlo 00000000 00000000 0001\n"); err == nil {
        t.Fatal("loopback must not be used as network telemetry")
    }
}

func TestCPUPercent(t *testing.T) {
    before := Raw{TotalCPU: 1000, IdleCPU: 700}
    after := Raw{TotalCPU: 2000, IdleCPU: 1500}
    percent, ok := CPUPercent(before, after)
    if !ok || percent != 20 { t.Fatalf("CPU percent %.2f %v", percent, ok) }
    if _, ok := CPUPercent(after, before); ok { t.Fatal("counter rollback accepted") }
}
