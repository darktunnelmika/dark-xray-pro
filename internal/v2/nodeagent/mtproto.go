package nodeagent

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/9seconds/mtg/v2/antireplay"
	"github.com/9seconds/mtg/v2/ipblocklist"
	"github.com/9seconds/mtg/v2/logger"
	"github.com/9seconds/mtg/v2/mtglib"
	"github.com/9seconds/mtg/v2/network"
)

// MTProto proxy for Telegram (fake-TLS, mtg's engine) inside the agent.
// It has one shared secret per inbound — the link is handed out like a
// SOCKS proxy — and its traffic is counted per inbound.

type mtpInbound struct {
	Tag      string `json:"tag"`
	Listen   string `json:"listen"`
	Port     int    `json:"port"`
	Settings struct {
		Secret string `json:"secret"` // ee<key><domain> hex, or base64
	} `json:"settings"`
}

type mtpServer struct {
	proxy    *mtglib.Proxy
	ln       net.Listener
	up, down atomic.Uint64
}

// trafficStream counts mtg's traffic events.
type trafficStream struct{ s *mtpServer }

func (t trafficStream) Send(_ context.Context, e mtglib.Event) {
	if ev, ok := e.(mtglib.EventTraffic); ok {
		if ev.IsRead {
			t.s.up.Add(uint64(ev.Traffic))
		} else {
			t.s.down.Add(uint64(ev.Traffic))
		}
	}
}

func startMTP(in mtpInbound, logf func(string, ...any)) (*mtpServer, error) {
	secret, err := mtglib.ParseSecret(in.Settings.Secret)
	if err != nil {
		return nil, fmt.Errorf("secret: %w", err)
	}
	dialer, err := network.NewDefaultDialer(0, 0)
	if err != nil {
		return nil, err
	}
	ntw, err := network.NewNetwork(dialer, "dark-node", "1.1.1.1", 0)
	if err != nil {
		return nil, err
	}
	s := &mtpServer{}
	p, err := mtglib.NewProxy(mtglib.ProxyOpts{
		Secret: secret, Network: ntw, AntiReplayCache: antireplay.NewStableBloomFilter(1<<20, 0.001),
		IPBlocklist: ipblocklist.NewNoop(), IPAllowlist: ipblocklist.NewNoop(), EventStream: trafficStream{s}, Logger: logger.NewNoopLogger(),
		Concurrency: 8192, IdleTimeout: 5 * time.Minute, TolerateTimeSkewness: 5 * time.Second, DomainFrontingPort: 443,
	})
	if err != nil {
		return nil, err
	}
	listen := in.Listen
	if listen == "" {
		listen = "0.0.0.0"
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(listen, strconv.Itoa(in.Port)))
	if err != nil {
		p.Shutdown()
		return nil, err
	}
	s.proxy, s.ln = p, ln
	go func() {
		if err := p.Serve(ln); err != nil {
			logf("mtproto %s stopped: %v", in.Tag, err)
		}
	}()
	return s, nil
}

func (s *mtpServer) take() (up, down uint64) { return s.up.Swap(0), s.down.Swap(0) }

func (s *mtpServer) close() {
	s.proxy.Shutdown()
	s.ln.Close()
}
