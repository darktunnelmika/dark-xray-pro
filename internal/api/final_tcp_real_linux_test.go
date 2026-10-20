//go:build linux

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func activeTCPClient(t *testing.T, ctx context.Context, binary, uuid, key string, port uint16, origin *httptest.Server) *http.Client {
	t.Helper()
	socksPort := liveUsagePort(t)
	doc := map[string]any{"log": map[string]string{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": port, "users": []any{map[string]any{"id": uuid, "encryption": "none", "flow": "xtls-rprx-vision"}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": "one.example.org", "fingerprint": "chrome", "publicKey": key, "shortId": "aa11"}}}}}
	raw, _ := json.Marshal(doc)
	path := filepath.Join(t.TempDir(), "active-client.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, binary, "run", "-config", path)
	cmd.Env = []string{"PATH=/usr/bin:/bin"} // client is never in server drain mode
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = cmd.Wait() })
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
	liveUsageWait(t, func() bool {
		c, e := net.DialTimeout("tcp4", socks, 100*time.Millisecond)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	transport := &http.Transport{TLSClientConfig: origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone(), DisableKeepAlives: true, DialContext: func(c context.Context, _ string, address string) (net.Conn, error) {
		return liveUsageSOCKS(c, socks, address)
	}}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 20 * time.Second}
}

func testActiveTCPOutcome(t *testing.T, ctx context.Context, s *Server, r *managedxray.Runtime, o managedxray.Options, cfg business.NodeConfig, epoch uc.Epoch, sender *uc.Sender, mode string, birth uc.ProcessToken, customers []business.Client, key string, port uint16, origin *httptest.Server, finalReport *atomic.Pointer[uc.Report], assertClosed func()) {
	t.Helper()
	client := activeTCPClient(t, ctx, o.Binary, customers[0].UUID, key, port, origin)
	response, err := client.Get(origin.URL)
	if err != nil {
		t.Fatal("active TLS stream", err)
	}
	defer response.Body.Close()
	if _, err = io.CopyN(io.Discard, response.Body, 64<<10); err != nil {
		t.Fatal("initial TLS stream bytes", err)
	}
	ended := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, response.Body); close(ended) }()
	select {
	case <-ended:
		t.Fatal("stream ended before drain")
	default:
	}
	if mode == "active-no-opt-in" {
		if _, err = r.SettleActiveTCP(ctx, sender); !errors.Is(err, managedxray.ErrUsageSettlement) || !r.UsageStatus().Running {
			t.Fatal("ordinary core admitted active settlement", err)
		}
		return
	}
	if mode == "active-interrupted" {
		r.Close()
		reopened, e := managedxray.Open(o)
		if e != nil {
			t.Fatal(e)
		}
		defer reopened.Close()
		// The fixture deliberately loses the first interruption notification.
		// A transport failure is not a HUB receipt. Exercise the Agent's bounded
		// retry behavior while proving every attempt keeps fresh admission shut.
		var replayErr error
		for attempt := 0; attempt < 3; attempt++ {
			if reopened.UsageStatus().Settled || !reopened.UsageStatus().SettlementRequired {
				t.Fatal("unconfirmed interruption released local fence")
			}
			if _, e := reopened.PrepareUsageAdmission(ctx, managedxray.Revision{Sequence: cfg.Sequence + 1, Candidate: cfg.Candidate}, liveUsageLease(t, o, cfg.Sequence+1, cfg.Candidate)); !errors.Is(e, managedxray.ErrUsageSettlement) {
				t.Fatal("unconfirmed interruption admitted a replacement", e)
			}
			_, replayErr = reopened.ReplayObservedUsage(ctx, sender)
			if replayErr == nil {
				break
			}
			if !errors.Is(replayErr, uc.ErrTransport) && !errors.Is(replayErr, uc.ErrLocked) {
				t.Fatal("interruption replay", replayErr)
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
		if replayErr != nil {
			t.Fatal("interruption receipt never confirmed", replayErr)
		}
		a, e := s.business.ClientUsage(ctx, customers[0].ID)
		if e != nil || a.Holds != 1 || a.Eligible || reopened.UsageStatus().Settled || finalReport.Load() != nil {
			t.Fatal("confirmed interruption did not preserve unknown tail", a, e)
		}
		assertClosed()
		return
	}
	if mode == "active-unknown-tail" {
		if err = s.business.UsageLedger().MarkEpochUncertain(ctx, usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}, epoch.ID, "test existing unknown tail"); err != nil {
			t.Fatal(err)
		}
	}
	_, err = r.SettleActiveTCP(ctx, sender)
	if mode == "active-lost-final-receipt" || mode == "active-lost-confirmation" {
		if err == nil || r.UsageStatus().Settled || !r.UsageStatus().SettlementRequired {
			t.Fatal("lost response released fence", err)
		}
		assertClosed()
		r.Close()
		r, err = managedxray.Open(o)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		_, err = r.ReplayObservedUsage(ctx, sender)
	}
	if mode == "active-unknown-tail" {
		a, e := s.business.ClientUsage(ctx, customers[0].ID)
		if err == nil || e != nil || a.Holds != 1 || a.Eligible || r.UsageStatus().Settled {
			t.Fatal("final counter receipt erased prior uncertainty", a, err, e)
		}
		assertClosed()
		return
	}
	if err != nil || !r.UsageStatus().Settled || r.UsageStatus().Running || r.UsageStatus().SettlementRequired {
		t.Fatal("active final settlement", r.UsageStatus(), err)
	}
	assertClosed()
	if _, e := uc.Process(birth.PID); e == nil {
		t.Fatal("settlement preceded owned process exit")
	}
	select {
	case <-ended:
	case <-ctx.Done():
		t.Fatal("active client did not stop")
	}
	final := finalReport.Load()
	if final == nil || !final.Quiesced || uc.VerifyReport(epoch, *final) != nil {
		t.Fatal("missing exact terminal report")
	}
	for _, u := range final.Users {
		a, e := s.business.ClientUsage(ctx, u.ClientID)
		if e != nil || a.Total != u.Uplink+u.Downlink || a.Holds != 0 {
			t.Fatal("ledger differs from final core counters", a, u, e)
		}
		if u.ClientID == customers[0].ID && (u.Downlink < 64<<10 || u.Uplink == 0) {
			t.Fatal("active TLS bytes lost", u)
		}
		if u.ClientID == customers[1].ID && a.Total != 0 {
			t.Fatal("idle user charged", a)
		}
	}
	if mode == "active-replacement" {
		next := cfg
		next.Sequence++
		lease := liveUsageLease(t, o, next.Sequence, next.Candidate)
		newBirth, e := r.PrepareUsageAdmission(ctx, managedxray.Revision{Sequence: next.Sequence, Candidate: next.Candidate}, lease)
		if e != nil || newBirth == birth {
			t.Fatal("fresh replacement after active seal", e)
		}
		assertClosed()
		if e = s.business.RecordNativeDelivery(ctx, next, lease); e != nil {
			t.Fatal(e)
		}
		newEpoch, e := s.business.RegisterNativeUsageEpoch(ctx, next, business.UsageBirth{EpochID: strings.Repeat("e", 32), KernelBootID: newBirth.BootID, PID: newBirth.PID, StartTicks: newBirth.StartTicks})
		if e != nil {
			t.Fatal("HUB refused replacement after exact active seal", e)
		}
		_, e = r.ActivateUsageAdmission(ctx, managedxray.UsageObservation{Epoch: newEpoch, StatsEndpoint: rAdmissionStats(t, next), Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 8 * time.Second, MaxPendingAge: 8 * time.Second})
		if e != nil {
			t.Fatal("fresh replacement initial receipt", e)
		}
	}
	t.Log("ACTIVE TCP: real TLS stream through VLESS Reality/Vision; final counters durable before process exit; exact HUB seal; idle client zero; ambiguous response replay preserves totals")
}

func rAdmissionStats(t *testing.T, cfg business.NodeConfig) string {
	t.Helper()
	var d struct {
		API struct {
			Listen string `json:"listen"`
		} `json:"api"`
	}
	if json.Unmarshal(cfg.Candidate.Config, &d) != nil || d.API.Listen == "" {
		t.Fatal("missing stats endpoint")
	}
	return d.API.Listen
}
