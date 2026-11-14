package nodeagent

import (
	"context"
	"testing"
	"time"
)

func TestIPTrackerParsesAccessLog(t *testing.T) {
	tr := NewIPTracker()
	now := time.Unix(1_800_000_000, 0)
	tr.now = func() time.Time { return now }
	// Split writes, IPv6, a non-access line and loopback.
	tr.Write([]byte("2026/11/14 07:17:15.104437 from 213.142.156.117:47782 accepted tcp:api.ipify.org:443 [uk-reality >> direct] email: alice\n2026/11/14 from [2001:db8::5]:443 accep"))
	tr.Write([]byte("ted tcp:x.com:443 [r >> direct] email: alice\n[Warning] core: Xray started\nfrom 127.0.0.1:5 accepted tcp:127.0.0.1:10085 [api -> api]\n"))
	tr.Write([]byte("from udp:198.51.100.2:9999 accepted udp:8.8.8.8:53 [hy >> direct] email: bob\n"))
	on := tr.Online()
	if len(on["alice"]) != 2 || on["alice"][0] != "2001:db8::5" || on["alice"][1] != "213.142.156.117" || len(on["bob"]) != 1 {
		t.Fatalf("online %v", on)
	}
	now = now.Add(2 * time.Minute)
	if len(tr.Online()) != 0 {
		t.Fatal("addresses past the window still online")
	}
	ts := &TrackedStats{StatsSource: &fakeStats{}, Tracker: tr}
	tr.Write([]byte("from 203.0.113.7:1 accepted tcp:a:1 [x >> direct] email: carol\n"))
	got, _ := ts.OnlineIPs(context.Background())
	if len(got["carol"]) != 1 {
		t.Fatalf("tracked %v", got)
	}
	if _, ok := unwrapStats(ts).(*fakeStats); !ok {
		t.Fatal("unwrap must reach the inner source")
	}
}
