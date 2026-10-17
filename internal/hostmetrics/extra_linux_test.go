package hostmetrics

import "testing"

func TestOptionalLinuxHostMetricsParse(t *testing.T) {
	a, b, c, err := parseLoad("0.71 1.25 2.15 1/89 123\n")
	if err != nil || *a != 0.71 || *b != 1.25 || *c != 2.15 {
		t.Fatalf("invalid load parse: %v", err)
	}
	for _, bad := range []string{"nan 1 1", "1 Inf 1", "-1 2 3", "garbage", "1"} {
		if _, _, _, err := parseLoad(bad); err == nil {
			t.Fatalf("accepted bad load %q", bad)
		}
	}
	uptime, err := parseUptime("12345.51 22222.11\n")
	if err != nil || *uptime != 12345 {
		t.Fatalf("uptime: %v", err)
	}
	for _, bad := range []string{"", "NaN", "-22", "Infinity"} {
		if _, err := parseUptime(bad); err == nil {
			t.Fatalf("accepted uptime %q", bad)
		}
	}
	tcp, err := parseTCPInUse("sockets: used 49\nTCP: inuse 15 orphan 0 tw 3 alloc 17 mem 2\nUDP: inuse 2 mem 1\n")
	if err != nil || *tcp != 15 {
		t.Fatalf("socket parse: %v", err)
	}
	for _, bad := range []string{"", "TCP: inuse malformed", "TCP: tw 1", "TCP: inuse 100000001"} {
		if _, err := parseTCPInUse(bad); err == nil {
			t.Fatalf("accepted sockstat %q", bad)
		}
	}
}
