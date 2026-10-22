//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// Two independently authenticated Agent runtimes, with real sequential cores
// and real Vision traffic. Neither the LAB host nor any external Node is used.
func TestRealPinnedMeteredHandoverActiveTraffic(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_HUB_TRAFFIC") != "1" {
		t.Skip("explicit isolated-network HUB traffic acceptance required")
	}
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" || !filepath.IsAbs(binary) || len(pin) != 64 || os.Geteuid() == 0 {
		t.Fatal("nonroot pinned final TCP core required")
	}
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" {
		t.Fatal("handover requires a dedicated loopback-only network namespace", err)
	}
	for _, mode := range []string{"normal", "lost-drain-ack-before", "lost-drain-ack-after", "lost-final-receipt"} {
		t.Run(mode, func(t *testing.T) { testMeteredHandoverTraffic(t, binary, pin, mode) })
	}
}

func testMeteredHandoverTraffic(t *testing.T, binary, pin, mode string, afterApply ...func(handoverHealthFixture)) {
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	target := liveUsageRealityAt(t, net.JoinHostPort(meteredTrafficAddress, "0"))
	var healthBehavior atomic.Int32
	healthStarted, healthRelease := make(chan struct{}, 1), make(chan struct{})
	var healthRequests atomic.Int64
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect-must-not-be-followed" {
			t.Error("protocol health followed a redirect")
			http.Error(w, "redirect forbidden", 500)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/dark-pro/protocol-health/v1/") {
			healthRequests.Add(1)
			id := strings.TrimPrefix(r.URL.Path, "/dark-pro/protocol-health/v1/")
			switch healthBehavior.Load() {
			case 1:
				_, _ = io.WriteString(w, "wrong challenge response")
				return
			case 2:
				http.Redirect(w, r, "/redirect-must-not-be-followed", 302)
				return
			case 3:
				select {
				case healthStarted <- struct{}{}:
				default:
				}
				select {
				case <-healthRelease:
				case <-r.Context().Done():
					return
				}
			}
			w.Header().Set("Cache-Control", "no-store")
			_, _ = io.WriteString(w, "DARK-PRO-PROTOCOL-HEALTH-v1\n"+id+"\n")
			return
		}
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			if _, err := w.Write(bytes.Repeat([]byte{42}, 8192)); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-tick.C:
			}
		}
	}))
	origin.Listener.Close()
	var err error
	origin.Listener, err = net.Listen("tcp4", net.JoinHostPort(meteredTrafficAddress, "0"))
	if err != nil {
		t.Fatal(err)
	}
	origin.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	origin.StartTLS()
	origin.Client().Transport.(*http.Transport).TLSClientConfig.ServerName = "example.com"
	defer origin.Close()
	portA, portB := meteredTrafficCustomerPorts(t)
	ports := []uint16{portA, portB}
	closed := func(port uint16) bool {
		c, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 100*time.Millisecond)
		if err == nil {
			c.Close()
		}
		return err != nil
	}
	assertClosed := func() {
		t.Helper()
		if !closed(portA) || !closed(portB) {
			t.Fatal("customer listener crossed handover accounting fence")
		}
	}
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0x41}, 32)
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	profile, err := s.business.CreateInbound(ctx, key, business.InboundInput{ID: 7612, Name: "Disposable two Node traffic", Transport: "tcp", RealityTarget: target.Listener.Addr().String(), ServerNames: []string{"one.example.org"}, ShortIDs: []string{"aa11"}})
	if err != nil {
		t.Fatal(err)
	}
	var customers []business.Client
	for _, id := range []string{"handover-active", "handover-idle"} {
		c, err := s.business.CreateClient(ctx, business.NewClient{ClientID: id, EventID: "handover-real-create-" + id, RepresentativeID: "owner", Name: id, DurationDays: 30, VolumeBytes: 128 << 20})
		if err != nil {
			t.Fatal(err)
		}
		customers = append(customers, c)
	}
	if err := s.business.BindInboundClients(ctx, 7612, []string{customers[0].ID, customers[1].ID}, true); err != nil {
		t.Fatal(err)
	}
	nodes := []string{"old-de", "new-de"}
	installs := []string{installID, newInstallation}
	tokens := []string{nodeToken, newToken}
	var approved []business.MeteredRouteNode
	for i, id := range nodes {
		ticket, err := s.registry.Issue(id, "Disposable CI handover")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.registry.Claim(ticket.PairCode, id, installs[i], tokens[i]); err != nil {
			t.Fatal(err)
		}
		if err := s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 7612, NodeID: id, InstallationID: installs[i], ListenIP: "127.0.0.1", DirectPort: ports[i]}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.SaveNativeRouteDraft(ctx, key, business.NativeRouteDraftInput{EventID: "handover-real-draft-" + id, InboundID: 7612, NodeID: id, InstallationID: installs[i], ExpectedInboundRevision: 1, DirectPort: ports[i], Reason: "Two disposable loopback Agents"}); err != nil {
			t.Fatal(err)
		}
		approved = append(approved, business.MeteredRouteNode{NodeID: id, InstallationID: installs[i], BinarySHA: pin})
	}
	if err := s.EnableMeteredRouteNodes(approved); err != nil {
		t.Fatal(err)
	}
	var probes []usageledger.ProbeIdentity
	expectedUsers := 2
	if len(afterApply) > 0 {
		if err := s.business.EnableSystemProbeAccounting(ctx); err != nil {
			t.Fatal(err)
		}
		if err := s.business.EnableMeteredHealthChallenges(ctx); err != nil {
			t.Fatal(err)
		}
		cert := origin.Certificate()
		h := sha256.Sum256(cert.Raw)
		if err := s.EnableMeteredHealth(mx.ProtocolHealthEndpoint{Address: origin.Listener.Addr().String(), ServerName: "example.com", CertificateSHA: hex.EncodeToString(h[:]), CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})}); err != nil {
			t.Fatal(err)
		}
		for i, node := range nodes {
			probe, err := s.business.ProvisionMeteredProbe(ctx, key, node, installs[i], 7612)
			if err != nil {
				t.Fatal("dedicated system probe identity", err)
			}
			probes = append(probes, probe)
		}
		expectedUsers = 3
	}
	var mu sync.Mutex
	epochs := map[string]uc.Epoch{}
	finals := map[string]uc.Report{}
	zeros := map[string]int{}
	var births atomic.Int64
	var loseBefore, loseAfter, loseFinal atomic.Bool
	var healthLoseBefore, healthLoseAfter atomic.Bool
	actual := s.AgentHandler()
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/api/v1/agent/metered-health/receipt" && healthLoseBefore.CompareAndSwap(true, false) {
			w.WriteHeader(503)
			return
		}
		if path == "/api/v1/agent/metered-route-transactions/receipt" && loseBefore.CompareAndSwap(true, false) {
			w.WriteHeader(503)
			return
		}
		var report uc.Report
		if path == "/api/v1/agent/native-usage" {
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(raw))
			if err := json.Unmarshal(raw, &report); err != nil {
				t.Error(err)
			}
			if report.Sequence == 1 {
				if !closed(portA) || !closed(portB) || report.Quiesced || len(report.Users) != expectedUsers {
					t.Error("initial receipt crossed listener fence")
				}
				for _, u := range report.Users {
					if u.Uplink != 0 || u.Downlink != 0 {
						t.Error("nonzero initial receipt")
					}
				}
			}
		}
		rec := httptest.NewRecorder()
		actual.ServeHTTP(rec, r)
		mu.Lock()
		if rec.Code < 400 {
			if path == "/api/v1/agent/metered-route-transactions/register" {
				var epoch uc.Epoch
				if err := json.Unmarshal(rec.Body.Bytes(), &epoch); err != nil {
					t.Error(err)
				}
				if !closed(portA) || !closed(portB) {
					t.Error("birth preceded stop of both listeners")
				}
				for id, old := range epochs {
					final, ok := finals[id]
					proof, err := s.business.UsageLedger().ConfirmSettlement(r.Context(), usageledger.Principal{NodeID: nodes[0], InstallationID: installs[0]}, uc.Receipt{EpochID: id, Sequence: final.Sequence, SHA: final.SHA})
					if !ok || err != nil || !proof.Sealed || uc.VerifyEpochStopped(old) != nil || old.PID == epoch.PID && old.ProcessStartTicks == epoch.ProcessStartTicks {
						t.Error("target birth preceded exact source seal/process stop", err)
					}
				}
				epochs[epoch.ID] = epoch
				births.Add(1)
			}
			if path == "/api/v1/agent/native-usage" {
				if epoch, ok := epochs[report.EpochID]; !ok || uc.VerifyReport(epoch, report) != nil {
					t.Error("unbound report")
				}
				if report.Sequence == 1 {
					zeros[report.EpochID]++
				}
				if report.Quiesced {
					if old, ok := finals[report.EpochID]; ok && !reflect.DeepEqual(old, report) {
						t.Error("final replay changed bytes")
					}
					finals[report.EpochID] = report
				}
			}
		}
		mu.Unlock()
		if path == "/api/v1/agent/metered-route-transactions/receipt" && loseAfter.CompareAndSwap(true, false) || path == "/api/v1/agent/native-usage" && report.Quiesced && loseFinal.CompareAndSwap(true, false) || path == "/api/v1/agent/metered-health/receipt" && healthLoseAfter.CompareAndSwap(true, false) {
			if rec.Code >= 400 {
				t.Error("loss did not follow real commit", rec.Code)
			}
			w.WriteHeader(503)
			return
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	receiver.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	receiver.StartTLS()
	defer receiver.Close()
	cert := receiver.Certificate()
	digest := sha256.Sum256(cert.Raw)
	pinTLS := hex.EncodeToString(digest[:])
	client, endpoint, err := agenttransport.New(agenttransport.Config{Endpoint: receiver.URL, PinSHA256: pinTLS, CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})})
	if err != nil {
		t.Fatal(err)
	}
	var runtimes []*mx.TransactionRuntime
	for i, id := range nodes {
		sender, err := uc.NewSender(uc.SenderOptions{Endpoint: endpoint, NodeID: id, InstallationID: installs[i], Token: tokens[i], PinSHA256: pinTLS, Client: client})
		if err != nil {
			t.Fatal(err)
		}
		defer sender.Close()
		authority, err := mx.NewMeteredRemoteAuthority(client, endpoint, tokens[i], id, installs[i])
		if err != nil {
			t.Fatal(err)
		}
		runtime, err := mx.OpenMeteredTransactions(mx.Options{Directory: filepath.Join(t.TempDir(), id), StateKey: bytes.Repeat([]byte{byte(0x62 + i)}, 32), LeaseKey: []byte(tokens[i]), NodeID: id, InstallationID: installs[i], ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin, ExperimentalFinalTCP: true}, mx.MeteredTransactionOptions{Authority: authority, Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 8 * time.Second, MaxPendingAge: 8 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		defer runtime.Close()
		runtimes = append(runtimes, runtime)
	}
	syncNow := func(i int) {
		t.Helper()
		if err := runtimes[i].SyncMeteredTransaction(ctx, client, endpoint); err != nil {
			t.Fatal("Agent sync", i, err)
		}
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	post := func(path string, in any, status int) {
		t.Helper()
		w := browserRequest(t, s.BrowserHandler(), "POST", path, in, cookie, csrf, "https://"+browserHost)
		if w.Code != status {
			t.Fatalf("owner %s HTTP %d want %d: %s", path, w.Code, status, w.Body.String())
		}
	}
	queue := func(i, n int, action, stage string, rev uint64) {
		t.Helper()
		syncNow(i)
		post("/api/v1/workspace/native-nodes/"+nodes[i]+"/metered-route-transactions", meteredRouteOwnerRequest(runtimes[i].Status(), n, action, stage, rev), 202)
		syncNow(i)
	}
	for i, n := range []int{11, 21} {
		queue(i, n, "stage", "", 1)
	}
	assertClosed()
	queue(0, 12, "apply", handoverTestID(11), 0)
	startStream := func(i int) *routeTrafficStream {
		t.Helper()
		c := activeTCPClient(t, ctx, binary, customers[0].UUID, profile.PublicKey, ports[i], origin)
		response, err := c.Get(origin.URL)
		if err != nil {
			t.Fatal("real Vision stream", err)
		}
		deferOnCleanup := response.Body
		t.Cleanup(func() { deferOnCleanup.Close() })
		if _, err := io.CopyN(io.Discard, response.Body, 64<<10); err != nil {
			t.Fatal(err)
		}
		stream := &routeTrafficStream{done: make(chan struct{})}
		stream.bytes.Store(64 << 10)
		go func() {
			defer close(stream.done)
			buf := make([]byte, 8192)
			for {
				n, err := response.Body.Read(buf)
				stream.bytes.Add(int64(n))
				if err != nil {
					return
				}
			}
		}()
		stream.flowing(t)
		return stream
	}
	stream := startStream(0)
	syncNow(0)
	syncNow(1)
	sourceEpoch := runtimes[0].Status().Usage.EpochID
	plan := meteredHandoverInput{MeteredHandoverRequest: business.MeteredHandoverRequest{ID: handoverTestID(31), SourceNode: nodes[0], TargetNode: nodes[1], SourceStageID: handoverTestID(11), TargetStageID: handoverTestID(21), SourceUsage: *runtimes[0].Status().Usage}, SourceInstallation: installs[0], TargetInstallation: installs[1]}
	post(handoverBase, plan, 201)
	commands := handoverBase + "/" + plan.ID + "/commands"
	apply := meteredRouteOwnerRequest(runtimes[1].Status(), 22, "apply", plan.TargetStageID, 0)
	post(commands, apply, 409)
	stream.flowing(t)
	drain := meteredRouteOwnerRequest(runtimes[0].Status(), 13, "drain", plan.SourceStageID, 0)
	post(commands, drain, 202)
	loseBefore.Store(mode == "lost-drain-ack-before")
	loseAfter.Store(mode == "lost-drain-ack-after")
	loseFinal.Store(mode == "lost-final-receipt")
	err = runtimes[0].SyncMeteredTransaction(ctx, client, endpoint)
	if (mode != "normal") != (err != nil) {
		t.Fatal("loss injection was not exercised", mode, err)
	}
	stream.stopped(t, ctx)
	assertClosed()
	post(commands, apply, 409) // ACK alone, or uncertainty, never admits the target.
	if births.Load() != 1 {
		t.Fatal("target born before source barrier")
	}
	if mode == "lost-final-receipt" {
		// Unknown settlement stops the transfer permanently. Replaying usage can
		// recover billing evidence, but must not upgrade the failed drain ACK.
		for i := 0; i < 3; i++ {
			if _, err = runtimes[0].ReplayMeteredUsage(ctx); err == nil {
				break
			}
		}
		syncNow(0)
		post(commands, apply, 409)
		assertClosed()
		if births.Load() != 1 {
			t.Fatal("usage replay silently resumed a failed handover")
		}
		return
	}
	syncNow(0) // exact ACK replay and a fresh signed stopped observation
	syncNow(0)
	post(commands, drain, 202) // immutable owner retry cannot create another stop
	post(commands, apply, 202)
	post(commands, apply, 202)
	syncNow(1)
	if births.Load() != 2 || runtimes[1].Status().Usage.EpochID == sourceEpoch || !closed(portA) {
		t.Fatal("target admission lost distinct epoch/source stop")
	}
	newStream := startStream(1)
	syncNow(1)
	view, err := s.business.MeteredHandover(ctx, plan.ID)
	if err != nil || view.Phase != "target_applied_health_required" || view.CanPublish {
		t.Fatal("execution ACK claimed health/publication", view, err)
	}
	// An explicit emergency drain is still allowed under the reservation. It
	// closes the second real stream and gives exact final accounting evidence.
	drained := false
	probeCredential := customers[0].UUID
	probeID := ""
	if len(probes) > 0 {
		probeCredential = probes[1].UUID
		probeID = probes[1].ID
	}
	for _, check := range afterApply {
		check(handoverHealthFixture{Server: s, Cookie: cookie, CSRF: csrf, Client: client, Endpoint: endpoint, DropBefore: &healthLoseBefore, DropAfter: &healthLoseAfter, Store: s.business, Key: key, Runtime: runtimes[1], Token: tokens[1], Plan: plan, Origin: origin, Port: portB, Credential: probeCredential, SystemProbeID: probeID, CustomerCredential: customers[0].UUID, CustomerID: customers[0].ID, Behavior: &healthBehavior, Requests: &healthRequests, Started: healthStarted, Release: healthRelease, Drain: func() { queue(1, 23, "drain", plan.TargetStageID, 0); drained = true }})
	}
	if !drained {
		queue(1, 23, "drain", plan.TargetStageID, 0)
	}
	newStream.stopped(t, ctx)
	assertClosed()
	syncNow(0)
	syncNow(1)
	mu.Lock()
	defer mu.Unlock()
	if len(epochs) != 2 || len(finals) != 2 || births.Load() != 2 {
		t.Fatal("missing two stopped, settled epochs")
	}
	totals := map[string]uc.Usage{}
	for id, epoch := range epochs {
		f, ok := finals[id]
		if !ok || len(f.Users) != expectedUsers || zeros[id] != 1 || uc.VerifyReport(epoch, f) != nil || uc.VerifyEpochStopped(epoch) != nil {
			t.Fatal("incomplete zero/final/process-stop evidence")
		}
		i := 0
		if id != sourceEpoch {
			i = 1
		}
		proof, err := s.business.UsageLedger().ConfirmSettlement(ctx, usageledger.Principal{NodeID: nodes[i], InstallationID: installs[i]}, uc.Receipt{EpochID: id, Sequence: f.Sequence, SHA: f.SHA})
		if err != nil || !proof.Sealed {
			t.Fatal("missing per-Node final HUB seal", err)
		}
		for _, u := range f.Users {
			if u.ClientID == customers[0].ID && (u.Downlink < 64<<10 || u.Uplink == 0) {
				t.Fatal("active handover bytes lost")
			}
			if u.ClientID == customers[1].ID && (u.Downlink != 0 || u.Uplink != 0) {
				t.Fatal("idle customer billed")
			}
			sum := totals[u.ClientID]
			sum.Uplink += u.Uplink
			sum.Downlink += u.Downlink
			totals[u.ClientID] = sum
		}
	}
	for _, c := range customers {
		got, err := s.business.ClientUsage(ctx, c.ID)
		want := totals[c.ID]
		if err != nil || got.Uplink != want.Uplink || got.Downlink != want.Downlink || got.Total != want.Uplink+want.Downlink {
			t.Fatal(fmt.Sprintf("handover lost or double billed customer %s", c.ID), got, want, err)
		}
	}
	for i, p := range probes {
		got, err := s.business.UsageLedger().ProbeUsage(ctx, p.ID)
		want := totals[p.ID]
		if err != nil || got.Uplink != want.Uplink || got.Downlink != want.Downlink || got.Total != want.Uplink+want.Downlink || got.Holds != 0 {
			t.Fatal("system probe bytes lost or charged outside its ledger", got, want, err)
		}
		if i == 1 && (got.Uplink == 0 || got.Downlink == 0) {
			t.Fatal("protocol check did not use the dedicated system credential")
		}
		if i == 0 && got.Total != 0 {
			t.Fatal("unused source system probe billed")
		}
		if _, err = s.business.Client(ctx, p.ID); err == nil {
			t.Fatal("system probe became a customer")
		}
		if _, err = s.business.UsageLedger().Account(ctx, p.ID); err == nil {
			t.Fatal("system probe gained a financial account")
		}
	}
}
