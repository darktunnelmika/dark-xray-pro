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
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// This runs the actual core, Registry, TLS, Store, outbox, admission and
// observer. The trusted test supervisor registers the birth using Store APIs;
// ordinary remote metered delivery remains intentionally 503-gated.
func TestRealPinnedPretrafficUsageAdmission(t *testing.T) {
	testRealPinnedPretrafficUsageAdmission(t, []string{"receipt-before-first-byte", "lost-zero-receipt", "wrong-pin", "lease-expires-at-barrier", "restart-while-prepared", "cancel-at-barrier"})
}

func TestRealPinnedPretrafficSettlement(t *testing.T) {
	testRealPinnedPretrafficUsageAdmission(t, []string{"settlement-restart", "settlement-lost-final-receipt", "settlement-lost-confirmation", "settlement-unknown-tail", "settlement-replacement"})
}

func TestRealPinnedActiveTCPSettlement(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" {
		t.Skip("explicit experimental barrier core required")
	}
	testRealPinnedPretrafficUsageAdmission(t, []string{"active-replacement", "active-lost-final-receipt", "active-lost-confirmation", "active-unknown-tail", "active-interrupted", "active-no-opt-in"})
}

func TestRealPinnedMeteredTransactions(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" {
		t.Skip("explicit experimental barrier core required")
	}
	testRealPinnedPretrafficUsageAdmission(t, []string{"transaction-apply-rollback", "transaction-lost-seal", "transaction-revoked-after-seal", "transaction-unknown-tail", "transaction-register-failed", "transaction-lost-ack-restart"})
}

func testRealPinnedPretrafficUsageAdmission(t *testing.T, modes []string) {
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if binary == "" && pin == "" {
		t.Skip("explicit pinned real core required")
	}
	if !filepath.IsAbs(binary) || len(pin) != 64 || os.Geteuid() == 0 {
		t.Fatal("unprivileged pinned real core required")
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			transactionMode := strings.HasPrefix(mode, "transaction-")
			activeMode := strings.HasPrefix(mode, "active-") || transactionMode
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
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
			volume := int64(1 << 20)
			if activeMode {
				volume = 128 << 20
			}
			for i, id := range []string{"admission-a", "admission-b"} {
				c, err := s.business.CreateClient(ctx, business.NewClient{EventID: "new-pretraffic-client-" + id, ClientID: id, RepresentativeID: "owner", Name: id, DurationDays: 30, VolumeBytes: volume})
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
			var lostConfirmation atomic.Bool
			var lostFinal atomic.Bool
			var lostInterruption atomic.Bool
			var finalReport atomic.Pointer[uc.Report]
			actual := s.AgentHandler()
			receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.URL.Path == "/api/v1/agent/native-usage/interrupted" && mode == "active-interrupted" && !lostInterruption.Swap(true) {
					w.WriteHeader(503)
					return
				}
				if req.URL.Path == "/api/v1/agent/native-usage/settled" && (mode == "settlement-lost-confirmation" || mode == "active-lost-confirmation" || mode == "transaction-lost-seal") && !lostConfirmation.Swap(true) {
					rec := httptest.NewRecorder()
					actual.ServeHTTP(rec, req)
					if rec.Code != 200 {
						t.Error("seal confirmation was not successful", rec.Code)
					}
					w.WriteHeader(503)
					return
				}
				if req.URL.Path == "/api/v1/agent/native-usage" {
					if activeMode {
						raw, e := io.ReadAll(req.Body)
						if e != nil {
							t.Error(e)
							w.WriteHeader(500)
							return
						}
						req.Body = io.NopCloser(bytes.NewReader(raw))
						var report uc.Report
						if json.Unmarshal(raw, &report) == nil && report.Quiesced {
							finalReport.Store(&report)
							if mode == "active-lost-final-receipt" && !lostFinal.Swap(true) {
								rec := httptest.NewRecorder()
								actual.ServeHTTP(rec, req)
								if rec.Code != 200 {
									t.Error("active final report did not commit", rec.Code)
								}
								w.WriteHeader(503)
								return
							}
						}
					}
					requestNumber := requests.Add(1)
					if requestNumber == 3 && mode == "settlement-lost-final-receipt" {
						rec := httptest.NewRecorder()
						actual.ServeHTTP(rec, req)
						if rec.Code != 200 {
							t.Error("final report did not commit", rec.Code)
						}
						w.WriteHeader(503)
						return
					}
					if requestNumber == 1 {
						onceArrival.Do(func() { close(arrival) })
						if mode == "receipt-before-first-byte" || mode == "lease-expires-at-barrier" || mode == "cancel-at-barrier" {
							select {
							case <-release:
							case <-req.Context().Done():
								return
							}
						}
						if mode == "lost-zero-receipt" || strings.HasPrefix(mode, "settlement-") {
							rec := httptest.NewRecorder()
							actual.ServeHTTP(rec, req)
							if rec.Code != 200 {
								t.Error("baseline was not actually committed", rec.Code)
							}
							w.WriteHeader(503)
							return
						}
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
			var origin *httptest.Server
			if activeMode {
				origin = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					w.Header().Set("Content-Type", "application/octet-stream")
					chunk := bytes.Repeat([]byte{42}, 8192)
					tick := time.NewTicker(2 * time.Millisecond)
					defer tick.Stop()
					for {
						if _, e := w.Write(chunk); e != nil {
							return
						}
						w.(http.Flusher).Flush()
						select {
						case <-req.Context().Done():
							return
						case <-tick.C:
						}
					}
				}))
				origin.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
				origin.StartTLS()
			} else {
				origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, proof) }))
			}
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
			options.ExperimentalFinalTCP = activeMode && mode != "active-no-opt-in"
			if transactionMode {
				testMeteredTransactionOutcome(t, ctx, s, options, cfg, receiver, mode, customers, port, origin, &finalReport)
				return
			}
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
			if activeMode {
				observation.MaxSampleGap = 8 * time.Second
				observation.MaxPendingAge = 8 * time.Second
			}
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
			if mode != "receipt-before-first-byte" && !activeMode {
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
				if strings.HasPrefix(mode, "settlement-") {
					testPretrafficSettlementOutcome(t, ctx, s, reopened, options, cfg, epoch, sender, mode, birth, assertClosed)
					return
				}
				if mode == "lost-zero-receipt" {
					if _, e = reopened.ReplayObservedUsage(ctx, sender); e != nil {
						t.Fatal(e)
					}
					a, e := s.business.ClientUsage(ctx, customers[0].ID)
					if e != nil || a.Total != 0 || a.Holds != 0 || !a.Eligible || !reopened.UsageStatus().Settled {
						t.Fatal("proven unopened birth failed exact settlement", a, e)
					}
				}
				return
			}
			if err != nil {
				t.Fatal("activation failed", err)
			}
			if _, e := r.SettleUnopenedUsage(ctx, sender); !errors.Is(e, managedxray.ErrUsageSettlement) {
				t.Fatal("active runtime accepted pretraffic settlement", e)
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
			if activeMode {
				testActiveTCPOutcome(t, ctx, s, r, options, cfg, epoch, sender, mode, birth, customers, profiles[0].PublicKey, port, origin, &finalReport, assertClosed)
				return
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

func testPretrafficSettlementOutcome(t *testing.T, ctx context.Context, s *Server, r *managedxray.Runtime, o managedxray.Options, cfg business.NodeConfig, epoch uc.Epoch, sender *uc.Sender, mode string, oldBirth uc.ProcessToken, assertClosed func()) {
	t.Helper()
	if mode == "settlement-unknown-tail" {
		if err := s.business.UsageLedger().MarkEpochUncertain(ctx, usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}, epoch.ID, "test pre-existing uncertainty"); err != nil {
			t.Fatal(err)
		}
	}
	_, err := r.SettleUnopenedUsage(ctx, sender)
	if mode == "settlement-unknown-tail" {
		a, e := s.business.ClientUsage(ctx, epoch.Bindings[0].ClientID)
		if err == nil || e != nil || a.Holds != 1 || a.Eligible || r.UsageStatus().Settled || !r.UsageStatus().SettlementRequired {
			t.Fatal("unknown tail cleared", a, err, e)
		}
		if _, e = r.PrepareUsageAdmission(ctx, managedxray.Revision{Sequence: cfg.Sequence + 1, Candidate: cfg.Candidate}, liveUsageLease(t, o, cfg.Sequence+1, cfg.Candidate)); !errors.Is(e, managedxray.ErrUsageSettlement) {
			t.Fatal("unknown epoch replaced", e)
		}
		assertClosed()
		return
	}
	if mode == "settlement-lost-final-receipt" || mode == "settlement-lost-confirmation" {
		if err == nil || r.UsageStatus().Settled || !r.UsageStatus().SettlementRequired {
			t.Fatal("ambiguous final response released fence", err)
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
	if err != nil || !r.UsageStatus().Settled || r.UsageStatus().SettlementRequired {
		t.Fatal("exact settlement failed", r.UsageStatus(), err)
	}
	if _, err = uc.Process(oldBirth.PID); err == nil {
		t.Fatal("old process survived terminal receipt")
	}
	assertClosed()
	for _, binding := range epoch.Bindings {
		a, e := s.business.ClientUsage(ctx, binding.ClientID)
		if e != nil || a.Total != 0 || a.Holds != 0 || !a.Known || !a.Eligible {
			t.Fatal("settlement changed bytes/identity/eligibility", a, e)
		}
	}
	if sent, e := r.SettleUnopenedUsage(ctx, sender); e != nil || sent != 0 {
		t.Fatal("settlement replay was not idempotent", sent, e)
	}
	if _, e := r.PrepareUsageAdmission(ctx, managedxray.Revision{Sequence: cfg.Sequence, Candidate: cfg.Candidate}, liveUsageLease(t, o, cfg.Sequence, cfg.Candidate)); !errors.Is(e, managedxray.ErrStale) {
		t.Fatal("old revision reused after settlement", e)
	}
	if mode == "settlement-replacement" {
		// Trusted fixture signs a fresh monotonic revision; the old birth/lease
		// cannot be adopted. The same UUIDs pass a second zero-receipt barrier.
		cfg.Sequence++
		lease := liveUsageLease(t, o, cfg.Sequence, cfg.Candidate)
		birth, e := r.PrepareUsageAdmission(ctx, managedxray.Revision{Sequence: cfg.Sequence, Candidate: cfg.Candidate}, lease)
		if e != nil {
			t.Fatal(e)
		}
		assertClosed()
		if birth == oldBirth {
			t.Fatal("old process birth reused")
		}
		if e = s.business.RecordNativeDelivery(ctx, cfg, lease); e != nil {
			t.Fatal(e)
		}
		fresh, e := s.business.RegisterNativeUsageEpoch(ctx, cfg, business.UsageBirth{EpochID: strings.Repeat("e", 32), KernelBootID: birth.BootID, PID: birth.PID, StartTicks: birth.StartTicks})
		if e != nil {
			t.Fatal("new epoch denied after verified seal", e)
		}
		if _, e = r.ActivateUsageAdmission(ctx, managedxray.UsageObservation{Epoch: fresh, StatsEndpoint: managedStatsEndpoint(t, cfg.Candidate), Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 3 * time.Second, MaxPendingAge: 3 * time.Second}); e != nil {
			t.Fatal("new admission", e)
		}
		if !r.Snapshot().ProcessReady || !r.UsageStatus().Running || r.UsageStatus().Settled {
			t.Fatal("new observer did not replace settled status")
		}
		if _, e = r.SettleUnopenedUsage(ctx, sender); !errors.Is(e, managedxray.ErrUsageSettlement) {
			t.Fatal("activated replacement accepted zero settlement", e)
		}
	}
}
func managedStatsEndpoint(t *testing.T, c xrayconfig.Candidate) string {
	t.Helper()
	_, _, endpoint, err := xrayconfig.UsageBootstrap(c)
	if err != nil {
		t.Fatal(err)
	}
	return endpoint
}
