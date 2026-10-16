//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// This runs the actual core, Registry, TLS, Store, outbox, admission and
// observer. The trusted test supervisor registers the birth using Store APIs;
// ordinary remote metered delivery remains intentionally 503-gated.
func TestRealPinnedPretrafficUsageAdmission(t *testing.T) {
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if binary == "" && pin == "" {
		t.Skip("explicit pinned real core required")
	}
	if !filepath.IsAbs(binary) || len(pin) != 64 || os.Geteuid() == 0 {
		t.Fatal("unprivileged pinned real core required")
	}
	for _, mode := range []string{"receipt-before-first-byte", "lost-zero-receipt", "wrong-pin", "lease-expires-at-barrier", "restart-while-prepared", "cancel-at-barrier"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
			defer cancel()
			s := enabledOwner(t)
			if err := s.business.EnableUsageLedger(ctx); err != nil {
				t.Fatal(err)
			}
			key := bytes.Repeat([]byte{7}, 32)
			if err := s.EnableNativeInbounds(key); err != nil {
				t.Fatal(err)
			}
			customers := make([]business.Client, 2)
			for i, id := range []string{"admission-a", "admission-b"} {
				c, err := s.business.CreateClient(ctx, business.NewClient{EventID: "new-pretraffic-client-" + id, ClientID: id, RepresentativeID: "owner", Name: id, DurationDays: 30, VolumeBytes: 1 << 20})
				if err != nil {
					t.Fatal(err)
				}
				customers[i] = c
			}
			ticket, err := s.registry.Issue("admission-node", "Disposable first-byte acceptance")
			if err != nil {
				t.Fatal(err)
			}
			arrival, release := make(chan struct{}), make(chan struct{})
			var onceArrival, onceRelease sync.Once
			unblock := func() { onceRelease.Do(func() { close(release) }) }
			defer unblock()
			var requests atomic.Int64
			actual := s.AgentHandler()
			receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/api/v1/agent/native-usage" && requests.Add(1) == 1 {
					onceArrival.Do(func() { close(arrival) })
					if mode == "receipt-before-first-byte" || mode == "lease-expires-at-barrier" || mode == "cancel-at-barrier" {
						select {
						case <-release:
						case <-req.Context().Done():
							return
						}
					}
					if mode == "lost-zero-receipt" {
						rec := httptest.NewRecorder()
						actual.ServeHTTP(rec, req)
						if rec.Code != 200 {
							t.Error("baseline was not actually committed", rec.Code)
						}
						w.WriteHeader(503)
						return
					}
				}
				actual.ServeHTTP(w, req)
			}))
			receiver.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			receiver.StartTLS()
			defer receiver.Close()
			enrollment, _ := json.Marshal(map[string]string{"pair_code": ticket.PairCode, "node_id": "admission-node", "installation_id": installID, "agent_token": nodeToken})
			enrolled, err := receiver.Client().Post(receiver.URL+"/api/v1/agent/enroll", "application/json", bytes.NewReader(enrollment))
			if err != nil {
				t.Fatal(err)
			}
			enrolled.Body.Close()
			if enrolled.StatusCode != 200 {
				t.Fatal("real registry rejected enrollment", enrolled.StatusCode)
			}
			target := liveUsageReality(t)
			proof := strings.Repeat("pretraffic-owned-payload-", 200)
			origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, proof) }))
			defer origin.Close()
			port, statsPort := liveUsagePort(t), liveUsagePort(t)
			for statsPort == port {
				statsPort = liveUsagePort(t)
			}
			if _, err = s.business.CreateInbound(ctx, key, business.InboundInput{ID: 889, Name: "Pretraffic", Transport: "tcp", RealityTarget: "one.example.org:443", ServerNames: []string{"one.example.org"}, ShortIDs: []string{"aa11"}}); err != nil {
				t.Fatal(err)
			}
			if err = s.business.BindInboundClients(ctx, 889, []string{customers[0].ID, customers[1].ID}, true); err != nil {
				t.Fatal(err)
			}
			if err = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 889, NodeID: "admission-node", InstallationID: installID, ListenIP: "127.0.0.1", DirectPort: port}); err != nil {
				t.Fatal(err)
			}
			cfg, err := s.business.BuildNodeConfig(ctx, key, "admission-node", installID)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if json.Unmarshal(cfg.Candidate.Config, &doc) != nil {
				t.Fatal("invalid rendered candidate")
			}
			doc["inbounds"].([]any)[0].(map[string]any)["streamSettings"].(map[string]any)["realitySettings"].(map[string]any)["dest"] = target.Listener.Addr().String()
			// Disposable loopback origin exception stays ONLY in test code.
			doc["outbounds"].([]any)[0].(map[string]any)["settings"] = map[string]any{"finalRules": []any{map[string]any{"action": "allow", "network": "tcp", "ip": []string{"127.0.0.1/32"}, "port": strconv.Itoa(origin.Listener.Addr().(*net.TCPAddr).Port)}}}
			raw, _ := json.Marshal(doc)
			digest := sha256.Sum256(raw)
			cfg.Candidate.Config = raw
			cfg.Candidate.SHA256 = hex.EncodeToString(digest[:])
			stats := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(statsPort)))
			cfg.Candidate, err = xrayconfig.WithUsageAPI(cfg.Candidate, stats)
			if err != nil {
				t.Fatal("metered candidate", err)
			}
			cfg.ConfigSHA = cfg.Candidate.SHA256
			cfg.ManifestSHA = cfg.Candidate.ManifestSHA256
			options := managedxray.Options{Directory: filepath.Join(t.TempDir(), "private-runtime"), StateKey: bytes.Repeat([]byte{2}, 32), LeaseKey: []byte(nodeToken), NodeID: cfg.NodeID, InstallationID: installID, ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin}
			r, err := managedxray.Open(options)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			lease := liveUsageLease(t, options, cfg.Sequence, cfg.Candidate)
			if mode == "lease-expires-at-barrier" {
				lease.Expires = time.Now().Add(2 * time.Second).UnixNano()
				lease, err = managedxray.SignLease(options.LeaseKey, lease)
				if err != nil {
					t.Fatal(err)
				}
			}
			birth, err := r.PrepareUsageAdmission(ctx, managedxray.Revision{Sequence: cfg.Sequence, Candidate: cfg.Candidate}, lease)
			if err != nil {
				t.Fatal("API-only prepare", err)
			}
			assertClosed := func() {
				t.Helper()
				c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 100*time.Millisecond)
				if e == nil {
					c.Close()
					t.Fatal("customer listener opened before authorized activation")
				}
			}
			assertClosed()
			if r.Snapshot().ProcessReady {
				t.Fatal("API socket counted as customer readiness")
			}
			if mode == "restart-while-prepared" {
				r.Close()
				reopened, e := managedxray.Open(options)
				if e != nil {
					t.Fatal(e)
				}
				defer reopened.Close()
				if _, e = reopened.Recover(ctx, liveUsageLease(t, options, cfg.Sequence, cfg.Candidate)); !errors.Is(e, managedxray.ErrUsageSettlement) {
					t.Fatal("restart bypassed preparation fence", e)
				}
				assertClosed()
				return
			}
			if err = s.business.RecordNativeDelivery(ctx, cfg, lease); err != nil {
				t.Fatal(err)
			}
			epoch, err := s.business.RegisterNativeUsageEpoch(ctx, cfg, business.UsageBirth{EpochID: strings.Repeat("d", 32), KernelBootID: birth.BootID, PID: birth.PID, StartTicks: birth.StartTicks})
			if err != nil {
				t.Fatal(err)
			}
			cert := sha256.Sum256(receiver.Certificate().Raw)
			certPin := hex.EncodeToString(cert[:])
			if mode == "wrong-pin" {
				certPin = strings.Repeat("a", 64)
			}
			sender, err := uc.NewSender(uc.SenderOptions{Endpoint: receiver.URL, NodeID: cfg.NodeID, InstallationID: installID, Token: nodeToken, PinSHA256: certPin, Client: receiver.Client()})
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()
			observation := managedxray.UsageObservation{Epoch: epoch, StatsEndpoint: stats, Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 3 * time.Second, MaxPendingAge: 3 * time.Second}
			activationCtx, finishActivation := context.WithCancel(ctx)
			defer finishActivation()
			outcome := make(chan error, 1)
			go func() { _, e := r.ActivateUsageAdmission(activationCtx, observation); outcome <- e }()
			if mode == "receipt-before-first-byte" || mode == "lease-expires-at-barrier" || mode == "cancel-at-barrier" {
				select {
				case <-arrival:
				case <-ctx.Done():
					t.Fatal("zero receipt request never reached real HUB")
				}
				assertClosed()
				if mode == "lease-expires-at-barrier" {
					liveUsageWait(t, func() bool { _, e := uc.Process(birth.PID); return e != nil })
					assertClosed()
				}
				if mode == "cancel-at-barrier" {
					finishActivation()
				}
				unblock()
			}
			select {
			case err = <-outcome:
			case <-ctx.Done():
				t.Fatal("activation did not finish")
			}
			if mode != "receipt-before-first-byte" {
				if err == nil {
					t.Fatal("invalid/expired/ambiguous initial authority activated")
				}
				assertClosed()
				if mode == "wrong-pin" && requests.Load() != 0 {
					t.Fatal("credentials escaped before pin validation")
				}
				r.Close()
				reopened, e := managedxray.Open(options)
				if e != nil {
					t.Fatal(e)
				}
				defer reopened.Close()
				if _, e = reopened.Recover(ctx, liveUsageLease(t, options, cfg.Sequence, cfg.Candidate)); !errors.Is(e, managedxray.ErrUsageSettlement) {
					t.Fatal("failed admission recovered unobserved", e)
				}
				if mode == "lost-zero-receipt" {
					if _, e = reopened.ReplayObservedUsage(ctx, sender); e != nil {
						t.Fatal(e)
					}
					a, e := s.business.ClientUsage(ctx, customers[0].ID)
					if e != nil || a.Total != 0 || a.Holds != 1 || a.Eligible {
						t.Fatal("ambiguous start replay changed zero/hold", a, e)
					}
				}
				return
			}
			if err != nil {
				t.Fatal("activation failed", err)
			}
			finishActivation() // the short operation context must not own the observer lifetime.
			observed, err := r.ObservationProcess()
			if err != nil || observed != birth {
				t.Fatal("activation replaced the registered Xray process", err)
			}
			liveUsageWait(t, func() bool { return r.UsageStatus().Samples >= 3 && r.UsageStatus().Running })
			for _, c := range customers {
				a, e := s.business.ClientUsage(ctx, c.ID)
				if e != nil || a.Total != 0 || !a.Known {
					t.Fatal("idle initial state fabricated consumption", a, e)
				}
			}
			profiles, err := s.business.ListInbounds(ctx, key)
			if err != nil {
				t.Fatal(err)
			}
			for i, c := range customers {
				transferAdmissionClient(t, ctx, binary, c.UUID, profiles[0].PublicKey, port, origin.URL, proof)
				liveUsageWait(t, func() bool {
					a, e := s.business.ClientUsage(ctx, c.ID)
					return e == nil && a.Total > 0 && r.UsageStatus().Pending == 0
				})
				if i == 0 {
					idle, e := s.business.ClientUsage(ctx, customers[1].ID)
					if e != nil || idle.Total != 0 || !idle.Known || !r.UsageStatus().Running {
						t.Fatal("idle second customer broke observation", e)
					}
				}
			}
			sample, err := uc.Collect(ctx, binary, pin, stats, epoch)
			if err != nil {
				t.Fatal(err)
			}
			for _, u := range sample {
				liveUsageWait(t, func() bool {
					a, e := s.business.ClientUsage(ctx, u.ClientID)
					return e == nil && a.Total == u.Uplink+u.Downlink
				})
			}
			if r.Snapshot().SubscriptionPublished || r.Snapshot().DataPlaneVerified {
				t.Fatal("test implicitly published production access")
			}
			request, _ := http.NewRequestWithContext(ctx, "GET", receiver.URL+"/api/v1/agent/native-runtime?node_id="+cfg.NodeID+"&installation_id="+installID, nil)
			request.Header.Set("Authorization", "Bearer "+nodeToken)
			blocked, e := receiver.Client().Do(request)
			if e != nil {
				t.Fatal(e)
			}
			blocked.Body.Close()
			if blocked.StatusCode != 503 {
				t.Fatal("ordinary metered admission fence was lifted", blocked.StatusCode)
			}
			t.Logf("PRETRAFFIC REAL CORE: customer port closed while zero receipt withheld; same registered PID activated only after committed baseline and observer; two lazy users counted independently; real Registry/TLS/modernc; continuous remote rollout remains gated")
		})
	}
}

func transferAdmissionClient(t *testing.T, ctx context.Context, binary, uuid, key string, port uint16, origin, proof string) {
	t.Helper()
	socksPort := liveUsagePort(t)
	doc := map[string]any{"log": map[string]string{"loglevel": "none"}, "inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}}, "outbounds": []any{map[string]any{"protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": port, "users": []any{map[string]any{"id": uuid, "encryption": "none", "flow": "xtls-rprx-vision"}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "reality", "realitySettings": map[string]any{"serverName": "one.example.org", "fingerprint": "chrome", "publicKey": key, "shortId": "aa11"}}}}}
	raw, _ := json.Marshal(doc)
	path := filepath.Join(t.TempDir(), "client.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(childCtx, binary, "run", "-config", path)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cancel(); _ = cmd.Wait() }()
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
	liveUsageWait(t, func() bool {
		c, e := net.DialTimeout("tcp4", socks, 100*time.Millisecond)
		if e == nil {
			c.Close()
		}
		return e == nil
	})
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(c context.Context, _ string, address string) (net.Conn, error) {
		return liveUsageSOCKS(c, socks, address)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second}
	res, err := client.Get(origin)
	if err != nil {
		t.Fatal("real client after activation", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || string(body) != proof {
		t.Fatal("REALITY payload mismatch", err)
	}
}
