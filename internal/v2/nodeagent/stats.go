package nodeagent

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	xstats "github.com/darktunnelmika/dark-xray-pro/internal/xraystats"
	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// StatsSource reads per-user counters from Xray.
type StatsSource interface {
	// TakeTraffic returns per-user byte deltas and resets the counters.
	TakeTraffic(ctx context.Context) ([]wire.UserTraffic, error)
	// OnlineIPs returns the source IPs currently seen per user.
	OnlineIPs(ctx context.Context) (map[string][]string, error)
}

// XrayStats talks to the stock Xray StatsService on loopback.
type XrayStats struct {
	Addr string

	mu   sync.Mutex
	conn *grpc.ClientConn
}

func (x *XrayStats) client() (xstats.StatsServiceClient, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.conn == nil {
		conn, err := grpc.NewClient(x.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		x.conn = conn
	}
	return xstats.NewStatsServiceClient(x.conn), nil
}

// ParseUserStat splits "user>>>EMAIL>>>traffic>>>uplink". Emails may not
// contain ">>>", which the HUB guarantees when it names clients.
func ParseUserStat(name string) (email, direction string, ok bool) {
	parts := strings.Split(name, ">>>")
	if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" || parts[1] == "" {
		return "", "", false
	}
	if parts[3] != "uplink" && parts[3] != "downlink" {
		return "", "", false
	}
	return parts[1], parts[3], true
}

// SumUserStats folds raw stat entries into per-user deltas, sorted by email.
func SumUserStats(stats []*xstats.Stat) []wire.UserTraffic {
	sums := map[string]*wire.UserTraffic{}
	for _, s := range stats {
		if s == nil || s.Value <= 0 {
			continue
		}
		email, dir, ok := ParseUserStat(s.Name)
		if !ok {
			continue
		}
		t := sums[email]
		if t == nil {
			t = &wire.UserTraffic{Email: email}
			sums[email] = t
		}
		if dir == "uplink" {
			t.Up += s.Value
		} else {
			t.Down += s.Value
		}
	}
	out := make([]wire.UserTraffic, 0, len(sums))
	for _, t := range sums {
		out = append(out, *t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out
}

func (x *XrayStats) TakeTraffic(ctx context.Context) ([]wire.UserTraffic, error) {
	c, err := x.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	resp, err := c.QueryStats(ctx, &xstats.QueryStatsRequest{Pattern: "user>>>", Reset_: true})
	if err != nil {
		return nil, err
	}
	return SumUserStats(resp.Stat), nil
}

func onlineEmail(name string) string {
	parts := strings.Split(name, ">>>")
	if len(parts) >= 2 && parts[0] == "user" {
		return parts[1]
	}
	return name
}

func (x *XrayStats) OnlineIPs(ctx context.Context) (map[string][]string, error) {
	c, err := x.client()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	users, err := c.GetAllOnlineUsers(ctx, &xstats.GetAllOnlineUsersRequest{})
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	for _, name := range users.Users {
		email := onlineEmail(name)
		r, err := c.GetStatsOnlineIpList(ctx, &xstats.GetStatsRequest{Name: "user>>>" + email + ">>>online"})
		if err != nil {
			continue
		}
		ips := make([]string, 0, len(r.Ips))
		for ip := range r.Ips {
			ips = append(ips, ip)
		}
		sort.Strings(ips)
		if len(ips) > 0 {
			out[email] = ips
		}
	}
	return out, nil
}

func (x *XrayStats) Close() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.conn == nil {
		return nil
	}
	err := x.conn.Close()
	x.conn = nil
	return err
}
