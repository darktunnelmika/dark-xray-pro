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

// This public-shaped address exists ONLY on lo in an otherwise empty disposable
// network namespace. There is no route to its real-world owner or the Internet.
// This lets the exact production renderer/core policies run without a test-only
// egress allow rule, config rewriting, DNS dependency or production hook.
const meteredTrafficAddress = "11.0.0.1"

func TestRealPinnedMeteredRouteActiveTraffic(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_HUB_TRAFFIC") != "1" {
		t.Skip("explicit isolated-network HUB traffic acceptance required")
	}
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" || !filepath.IsAbs(binary) || len(pin) != 64 || os.Geteuid() == 0 {
		t.Fatal("nonroot pinned final TCP core required")
	}
	interfaces, err := net.Interfaces()
	if err != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" {
		t.Fatal("traffic acceptance requires a dedicated loopback-only network namespace", err)
	}
	for _, mode := range []string{"replace-rollback", "lost-ack-before", "lost-ack-after", "lost-final-receipt", "lost-seal", "revoked-after-final", "unknown-tail", "interrupted", "drain", "creation-plan-promotion"} {
		t.Run(mode, func(t *testing.T) { testMeteredRouteActiveTraffic(t, binary, pin, mode) })
	}
}

type routeTrafficStream struct {
	bytes atomic.Int64
	done  chan struct{}
}

func (s *routeTrafficStream) flowing(t *testing.T) {
	t.Helper()
	before := s.bytes.Load()
	liveUsageWait(t, func() bool { return s.bytes.Load() > before })
	select {
	case <-s.done:
		t.Fatal("active stream ended before owner transition")
	default:
	}
}
func (s *routeTrafficStream) stopped(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-s.done:
	case <-ctx.Done():
		t.Fatal("old active stream survived transition")
	}
}

func testMeteredRouteActiveTraffic(t *testing.T, binary, pin, mode string) {
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	promotionMode := mode == "creation-plan-promotion"
	target := liveUsageRealityAt(t, net.JoinHostPort(meteredTrafficAddress, "0"))
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			if _, e := w.Write(bytes.Repeat([]byte{42}, 8192)); e != nil {
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
	// Preserve CA verification while verifying the fixture certificate's DNS
	// identity, not the namespace-local synthetic destination address.
	origin.Client().Transport.(*http.Transport).TLSClientConfig.ServerName = "example.com"
	defer origin.Close()
	portA, portB := meteredTrafficCustomerPorts(t)
	closed := func(port uint16) bool {
		c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 100*time.Millisecond)
		if e == nil {
			c.Close()
		}
		return e != nil
	}
	assertClosed := func() {
		t.Helper()
		if !closed(portA) || !closed(portB) {
			t.Fatal("customer listener remained open at accounting fence")
		}
	}
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0x41}, 32)
	if e := s.business.EnableUsageLedger(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.EnableNativeInbounds(key); e != nil {
		t.Fatal(e)
	}
	ticket, e := s.registry.Issue("route-qa", "Disposable active HUB queue")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.registry.Claim(ticket.PairCode, "route-qa", installID, nodeToken); e != nil {
		t.Fatal(e)
	}
	profileInput := business.InboundInput{ID: 7612, Name: "Disposable active route", Transport: "tcp", RealityTarget: target.Listener.Addr().String(), ServerNames: []string{"one.example.org"}, ShortIDs: []string{"aa11"}}
	var profile business.NativeInbound
	var savedPlan business.NativeInboundPlanReceipt
	if promotionMode {
		savedPlan = saveMeteredTrafficCreationPlan(t, s, profileInput, portA)
		profile = savedPlan.Plan.Profile
	} else {
		profile, e = s.business.CreateInbound(ctx, key, profileInput)
		if e != nil {
			t.Fatal(e)
		}
	}
	inboundRevision := profile.Revision
	var customers []business.Client
	for _, id := range []string{"metered-route-qa", "metered-route-idle"} {
		c, e := s.business.CreateClient(ctx, business.NewClient{ClientID: id, EventID: "active-hub-create-" + id, RepresentativeID: "owner", Name: id, DurationDays: 30, VolumeBytes: 128 << 20})
		if e != nil {
			t.Fatal(e)
		}
		customers = append(customers, c)
	}
	if promotionMode {
		if e = s.EnableMeteredRouteNodesForPlanPromotion([]business.MeteredRouteNode{{NodeID: "route-qa", InstallationID: installID, BinarySHA: pin}}); e != nil {
			t.Fatal("reserve empty promotion channel", e)
		}
		assertClosed()
	} else {
		if e = s.business.BindInboundClients(ctx, 7612, []string{customers[0].ID, customers[1].ID}, true); e != nil {
			t.Fatal(e)
		}
		if e = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 7612, NodeID: "route-qa", InstallationID: installID, ListenIP: "127.0.0.1", DirectPort: portA}); e != nil {
			t.Fatal(e)
		}
		meteredRouteAPIDraft(t, s, key, 0, portA)
		if e = s.EnableMeteredRouteTransactions("route-qa", installID, pin); e != nil {
			t.Fatal(e)
		}
	}
	principal := usageledger.Principal{NodeID: "route-qa", InstallationID: installID}
	var mu sync.Mutex
	epochs := map[string]uc.Epoch{}
	finals := map[string]uc.Report{}
	zeros := map[string]int{}
	var trace []string
	var births atomic.Int64
	var loseBefore, loseAfter, loseFinal, loseSeal, revoke, uncertain, loseInterruption atomic.Bool
	actual := s.AgentHandler()
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		path := req.URL.Path
		if path == "/api/v1/agent/metered-route-transactions/receipt" && loseBefore.CompareAndSwap(true, false) || path == "/api/v1/agent/native-usage/interrupted" && loseInterruption.CompareAndSwap(true, false) {
			w.WriteHeader(503)
			return
		}
		var report uc.Report
		var authority mx.MeteredAuthorityRequest
		if path == "/api/v1/agent/native-usage" || path == "/api/v1/agent/metered-route-transactions/check" {
			raw, e := io.ReadAll(req.Body)
			if e != nil {
				t.Error(e)
				w.WriteHeader(500)
				return
			}
			req.Body = io.NopCloser(bytes.NewReader(raw))
			if path == "/api/v1/agent/native-usage" {
				if e := json.Unmarshal(raw, &report); e != nil {
					t.Error(e)
				}
				if report.Sequence == 1 {
					if !closed(portA) || !closed(portB) || report.Quiesced || len(report.Users) != 2 {
						t.Error("initial receipt arrived after listener admission or with incomplete bindings")
					}
					for _, u := range report.Users {
						if u.Uplink != 0 || u.Downlink != 0 {
							t.Error("nonzero initial receipt")
						}
					}
				}
			} else if e := json.Unmarshal(raw, &authority); e != nil {
				t.Error(e)
			}
		}
		rec := httptest.NewRecorder()
		actual.ServeHTTP(rec, req)
		mu.Lock()
		trace = append(trace, fmt.Sprintf("%s HTTP %d", path, rec.Code))
		if rec.Code < 400 {
			if path == "/api/v1/agent/metered-route-transactions/register" {
				var epoch uc.Epoch
				if json.Unmarshal(rec.Body.Bytes(), &epoch) != nil {
					t.Error("invalid epoch registration response")
				}
				if !closed(portA) || !closed(portB) {
					t.Error("registered birth with an open customer listener")
				}
				for id, old := range epochs {
					final, ok := finals[id]
					seal := uc.Receipt{EpochID: id, Sequence: final.Sequence, SHA: final.SHA}
					proof, e := s.business.UsageLedger().ConfirmSettlement(req.Context(), principal, seal)
					if !ok || e != nil || !proof.Sealed || uc.VerifyEpochStopped(old) != nil || old.PID == epoch.PID && old.ProcessStartTicks == epoch.ProcessStartTicks {
						t.Error("new birth preceded exact prior seal/process stop", e)
					}
				}
				epochs[epoch.ID] = epoch
				births.Add(1)
			}
			if path == "/api/v1/agent/native-usage" {
				if epoch, ok := epochs[report.EpochID]; !ok || uc.VerifyReport(epoch, report) != nil {
					t.Error("report not bound to registered process")
				}
				if report.Sequence == 1 {
					zeros[report.EpochID]++
				}
				if report.Quiesced {
					if old, ok := finals[report.EpochID]; ok && !reflect.DeepEqual(old, report) {
						t.Error("replayed final report changed")
					}
					finals[report.EpochID] = report
				}
			}
		}
		mu.Unlock()
		// Insert competing uncertainty only after a genuine successful Check,
		// before its response allows the Agent to start final settlement.
		if path == "/api/v1/agent/metered-route-transactions/check" && rec.Code == 204 && uncertain.CompareAndSwap(true, false) {
			if e := s.business.UsageLedger().MarkEpochUncertain(req.Context(), principal, authority.Command.Usage.EpochID, "Disposable concurrent uncertainty after reviewed command"); e != nil {
				t.Error(e)
			}
		}
		if path == "/api/v1/agent/native-usage/settled" && rec.Code == 200 && revoke.CompareAndSwap(true, false) {
			if _, e := s.business.SetClientState(req.Context(), business.SetClientState{EventID: "active-hub-revoke-after-final", ClientID: customers[0].ID, Action: "disable", Reason: "Disposable revocation after final seal"}); e != nil {
				t.Error(e)
			}
		}
		discard := path == "/api/v1/agent/metered-route-transactions/receipt" && loseAfter.CompareAndSwap(true, false) ||
			path == "/api/v1/agent/native-usage" && report.Quiesced && loseFinal.CompareAndSwap(true, false) ||
			path == "/api/v1/agent/native-usage/settled" && loseSeal.CompareAndSwap(true, false)
		if discard {
			if rec.Code >= 400 {
				t.Error("loss fixture did not first commit", path, rec.Code)
			}
			w.WriteHeader(503)
			return
		}
		for k, values := range rec.Header() {
			w.Header()[k] = values
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}))
	receiver.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	receiver.StartTLS()
	defer receiver.Close()
	t.Cleanup(func() {
		if t.Failed() {
			mu.Lock()
			defer mu.Unlock()
			t.Log("sanitized HUB traffic trace", trace)
		}
	})
	cert := receiver.Certificate()
	digest := sha256.Sum256(cert.Raw)
	pinTLS := hex.EncodeToString(digest[:])
	client, endpoint, e := agenttransport.New(agenttransport.Config{Endpoint: receiver.URL, PinSHA256: pinTLS, CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})})
	if e != nil {
		t.Fatal(e)
	}
	sender, e := uc.NewSender(uc.SenderOptions{Endpoint: endpoint, NodeID: "route-qa", InstallationID: installID, Token: nodeToken, PinSHA256: pinTLS, Client: client})
	if e != nil {
		t.Fatal(e)
	}
	defer sender.Close()
	authority, e := mx.NewMeteredRemoteAuthority(client, endpoint, nodeToken, "route-qa", installID)
	if e != nil {
		t.Fatal(e)
	}
	options := mx.Options{Directory: filepath.Join(t.TempDir(), "active-hub-runtime"), StateKey: bytes.Repeat([]byte{0x62}, 32), LeaseKey: []byte(nodeToken), NodeID: "route-qa", InstallationID: installID, ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin, ExperimentalFinalTCP: true}
	metered := mx.MeteredTransactionOptions{Authority: authority, Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 8 * time.Second, MaxPendingAge: 8 * time.Second}
	runtime, e := mx.OpenMeteredTransactions(options, metered)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { runtime.Close() }()
	syncNow := func() {
		t.Helper()
		if e := runtime.SyncMeteredTransaction(ctx, client, endpoint); e != nil {
			t.Fatal("HUB Agent sync", e)
		}
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	if promotionMode {
		// Promotion consumes a genuine signed Agent observation from a fresh
		// runtime. Saving the plan or reserving a channel cannot invent it.
		syncNow()
		status := runtime.Status()
		if status.Usage == nil || *status.Usage != (mx.MeteredCondition{}) || status.Observed.ProcessReady || births.Load() != 0 {
			t.Fatal("promotion did not begin from an authenticated empty runtime")
		}
		inboundRevision = promoteMeteredTrafficCreationPlan(t, s, savedPlan, customers, cookie, csrf)
		assertClosed()
		if runtime.Status().Observed.ProcessReady || births.Load() != 0 {
			t.Fatal("promotion opened a listener before Stage and Apply")
		}
	}
	base := "/api/v1/workspace/native-nodes/route-qa/metered-route-transactions"
	enqueue := func(n int, action, stage string, revision uint64) business.MeteredRouteCommandView {
		t.Helper()
		syncNow()
		request := meteredRouteOwnerRequest(runtime.Status(), n, action, stage, revision)
		for i := range request.Drafts {
			request.Drafts[i].InboundRevision = inboundRevision
		}
		w := browserRequest(t, s.Handler(), "POST", base, request, cookie, csrf, "https://"+browserHost)
		if w.Code != 202 {
			t.Fatalf("owner %s HTTP %d: %s", action, w.Code, w.Body.String())
		}
		var out business.MeteredRouteCommandView
		if json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal("invalid owner queue response")
		}
		return out
	}
	receipt := func(id, outcome string) mx.TransactionReceipt {
		t.Helper()
		v, e := s.business.MeteredRouteChannel(ctx, key, "route-qa")
		if e != nil {
			t.Fatal(e)
		}
		for _, c := range v.Commands {
			if c.ID == id && c.Receipt != nil && c.Receipt.Outcome == outcome {
				return *c.Receipt
			}
		}
		t.Fatal("missing exact command receipt", id, outcome)
		return mx.TransactionReceipt{}
	}
	startStream := func(port uint16) *routeTrafficStream {
		t.Helper()
		c := activeTCPClient(t, ctx, binary, customers[0].UUID, profile.PublicKey, port, origin)
		response, e := c.Get(origin.URL)
		if e != nil {
			t.Fatal("real Vision TLS stream", e)
		}
		t.Cleanup(func() { response.Body.Close() })
		if _, e := io.CopyN(io.Discard, response.Body, 64<<10); e != nil {
			t.Fatal("first actual traffic bytes", e)
		}
		stream := &routeTrafficStream{done: make(chan struct{})}
		stream.bytes.Store(64 << 10)
		go func() {
			defer close(stream.done)
			buf := make([]byte, 8192)
			for {
				n, e := response.Body.Read(buf)
				stream.bytes.Add(int64(n))
				if e != nil {
					return
				}
			}
		}()
		stream.flowing(t)
		return stream
	}
	assertAccounting := func(ids ...string) {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		totals := map[string]uc.Usage{}
		for _, id := range ids {
			f, ok := finals[id]
			if !ok || len(f.Users) != 2 || zeros[id] != 1 || uc.VerifyReport(epochs[id], f) != nil {
				t.Fatal("missing complete epoch zero/final evidence", id)
			}
			for _, u := range f.Users {
				if u.ClientID == customers[0].ID && (u.Downlink < 64<<10 || u.Uplink == 0) {
					t.Fatal("active stream counters lost", u)
				}
				if u.ClientID == customers[1].ID && (u.Downlink != 0 || u.Uplink != 0) {
					t.Fatal("idle client charged", u)
				}
				sum := totals[u.ClientID]
				sum.Uplink += u.Uplink
				sum.Downlink += u.Downlink
				totals[u.ClientID] = sum
			}
		}
		for _, c := range customers {
			a, e := s.business.ClientUsage(ctx, c.ID)
			u := totals[c.ID]
			if e != nil || a.Uplink != u.Uplink || a.Downlink != u.Downlink || a.Total != u.Uplink+u.Downlink {
				t.Fatal("HUB ledger lost or double billed exact final bytes", a, u, e)
			}
		}
	}
	assertSeal := func(ack mx.TransactionReceipt, old string) {
		t.Helper()
		mu.Lock()
		f := finals[old]
		mu.Unlock()
		want := uc.Receipt{EpochID: old, Sequence: f.Sequence, SHA: f.SHA}
		proof, e := s.business.UsageLedger().ConfirmSettlement(ctx, principal, want)
		if ack.Metered == nil || ack.Metered.PriorSettlement != want || e != nil || !proof.Sealed || ack.Metered.After.EpochID == old {
			t.Fatal("owner ACK lacks exact HUB seal/distinct epoch", e)
		}
	}
	stage1 := enqueue(1, "stage", "", 1)
	syncNow()
	receipt(stage1.ID, "staged")
	assertClosed()
	apply1 := enqueue(2, "apply", stage1.ID, 0)
	loseBefore.Store(mode == "lost-ack-before")
	loseAfter.Store(mode == "lost-ack-after" || promotionMode)
	e = runtime.SyncMeteredTransaction(ctx, client, endpoint)
	if (mode == "lost-ack-before" || mode == "lost-ack-after" || promotionMode) != (e != nil) {
		t.Fatal("initial Apply response-loss contract", e)
	}
	epoch1 := runtime.Status().Usage.EpochID
	if epoch1 == "" || !runtime.Status().Observed.ProcessReady {
		t.Fatal("first Apply not admitted")
	}
	stream1 := startStream(portA)
	syncNow()
	ack1 := receipt(apply1.ID, "applied")
	if births.Load() != 1 || runtime.Status().Usage.EpochID != epoch1 {
		t.Fatal("lost ACK replay restarted or registered again")
	}
	stream1.flowing(t)
	if mode == "drain" || promotionMode {
		if !promotionMode {
			if _, err := s.business.SetClientState(ctx, business.SetClientState{EventID: "revoke-before-owner-drain", ClientID: customers[0].ID, Action: "disable", Reason: "Stop must still settle a revoked user's real traffic"}); err != nil {
				t.Fatal(err)
			}
		}
		drain := enqueue(3, "drain", stage1.ID, 0)
		loseAfter.Store(true)
		if err := runtime.SyncMeteredTransaction(ctx, client, endpoint); err == nil {
			t.Fatal("lost drain ACK not exercised")
		}
		stream1.stopped(t, ctx)
		assertClosed()
		syncNow()
		ack := receipt(drain.ID, "drained")
		if births.Load() != 1 || !runtime.Status().Observed.Usage.Settled || ack.Metered.After.EpochID != epoch1 || ack.Metered.PriorSettlement != ack.Metered.After.Settlement {
			t.Fatal("drain created a process or lost final seal")
		}
		if _, err := s.business.UsageLedger().ConfirmSettlement(ctx, principal, ack.Metered.PriorSettlement); err != nil {
			t.Fatal("HUB has no exact final seal", err)
		}
		assertAccounting(epoch1)
		runtime.Close()
		runtime, e = mx.OpenMeteredTransactions(options, metered)
		if e != nil {
			t.Fatal(e)
		}
		syncNow()
		assertClosed()
		if !reflect.DeepEqual(receipt(drain.ID, "drained"), ack) || births.Load() != 1 || runtime.Status().Observed.ProcessReady {
			t.Fatal("drain replay after restart changed receipt or resumed traffic")
		}
		assertAccounting(epoch1)
		if promotionMode {
			if ack.Observed.DataPlaneVerified || ack.Observed.SubscriptionPublished {
				t.Fatal("promotion execution claimed health or subscription publication")
			}
			t.Log("PLAN_PROMOTION_ACTIVE_PASS: saved owner plan and reviewed promotion; zero receipt before listener; real customer traffic and idle zero; exact final settlement; Apply and Drain ACK replay without a duplicate birth, debit or resumed listener")
		}
		return
	}
	if mode == "interrupted" {
		before, e := s.business.ClientUsage(ctx, customers[0].ID)
		if e != nil {
			t.Fatal(e)
		}
		runtime.Close()
		stream1.stopped(t, ctx)
		assertClosed()
		runtime, e = mx.OpenMeteredTransactions(options, metered)
		if e != nil {
			t.Fatal(e)
		}
		syncNow()
		if !reflect.DeepEqual(receipt(apply1.ID, "applied"), ack1) || runtime.Status().Observed.ProcessReady || births.Load() != 1 {
			t.Fatal("historical ACK changed or resumed an interrupted listener")
		}
		loseInterruption.Store(true)
		var replayErr error
		for i := 0; i < 3; i++ {
			_, replayErr = runtime.ReplayMeteredUsage(ctx)
			if replayErr == nil {
				break
			}
		}
		a, e := s.business.ClientUsage(ctx, customers[0].ID)
		mu.Lock()
		finalCount := len(finals)
		mu.Unlock()
		if replayErr != nil || e != nil || a.Holds != 1 || a.Eligible || runtime.Status().Observed.Usage.Settled || finalCount != 0 || a.Total < before.Total {
			t.Fatal("interrupted active tail not fenced", a, replayErr, e)
		}
		assertClosed()
		return
	}
	meteredRouteAPIDraft(t, s, key, 1, portB)
	stage2 := enqueue(3, "stage", "", 2)
	syncNow()
	receipt(stage2.ID, "staged")
	stream1.flowing(t)
	if runtime.Status().Usage.EpochID != epoch1 || births.Load() != 1 {
		t.Fatal("offline Stage disturbed active birth")
	}
	apply2 := enqueue(4, "apply", stage2.ID, 0)
	loseFinal.Store(mode == "lost-final-receipt")
	loseSeal.Store(mode == "lost-seal")
	revoke.Store(mode == "revoked-after-final")
	uncertain.Store(mode == "unknown-tail")
	e = runtime.SyncMeteredTransaction(ctx, client, endpoint)
	stream1.stopped(t, ctx)
	if mode == "lost-final-receipt" || mode == "lost-seal" || mode == "revoked-after-final" || mode == "unknown-tail" {
		if e == nil || runtime.Status().Observed.ProcessReady || births.Load() != 1 {
			t.Fatal("failed active transition admitted a new birth", e)
		}
		failed := receipt(apply2.ID, "failed_stopped")
		assertClosed()
		assertAccounting(epoch1)
		if mode == "lost-final-receipt" || mode == "lost-seal" {
			runtime.Close()
			runtime, e = mx.OpenMeteredTransactions(options, metered)
			if e != nil {
				t.Fatal(e)
			}
			if _, e := runtime.ReplayMeteredUsage(ctx); e != nil || !runtime.Status().Observed.Usage.Settled {
				t.Fatal("exact durable final replay", e)
			}
			assertAccounting(epoch1)
			syncNow()
			if !reflect.DeepEqual(receipt(apply2.ID, "failed_stopped"), failed) || runtime.Status().Observed.ProcessReady || births.Load() != 1 {
				t.Fatal("usage recovery retried activation or rewrote historical failure")
			}
			assertClosed()
		}
		if mode == "unknown-tail" {
			a, e := s.business.ClientUsage(ctx, customers[0].ID)
			if e != nil || a.Holds != 1 || a.Eligible || runtime.Status().Observed.Usage.Settled {
				t.Fatal("terminal receipt forgave unknown bytes", a, e)
			}
		}
		return
	}
	if e != nil {
		t.Fatal("active replacement", e)
	}
	ack2 := receipt(apply2.ID, "applied")
	epoch2 := ack2.Metered.After.EpochID
	assertSeal(ack2, epoch1)
	assertAccounting(epoch1)
	if !closed(portA) {
		t.Fatal("old listener still open")
	}
	stream2 := startStream(portB)
	meteredRouteAPIDraft(t, s, key, 2, portA)
	rollback := enqueue(5, "rollback", stage1.ID, 0)
	syncNow()
	stream2.stopped(t, ctx)
	back := receipt(rollback.ID, "rolled_back")
	assertSeal(back, epoch2)
	assertAccounting(epoch1, epoch2)
	if back.Metered.After.EpochID == epoch1 || births.Load() != 3 || !closed(portB) || back.Observed.ActiveSHA != ack1.Observed.ActiveSHA {
		t.Fatal("Rollback reused epoch or did not restore exact reviewed candidate")
	}
	renew := enqueue(6, "renew", stage1.ID, 0)
	syncNow()
	if receipt(renew.ID, "renewed").Metered.After.EpochID != back.Metered.After.EpochID || births.Load() != 3 {
		t.Fatal("explicit Renew created another epoch")
	}
	stream3 := startStream(portA)
	stream3.flowing(t)
	runtime.Close()
	stream3.stopped(t, ctx)
	assertClosed()
	t.Log("HUB ACTIVE: untouched owner-rendered candidates; real Vision TLS streams; initial receipt before listener; exact prior final seal; no duplicate debit; idle client zero; distinct rollback birth")
}

// This fixture runs alone in a loopback-only namespace. Customer ports must
// stay outside the kernel's ephemeral range: otherwise the subsequently bound
// TLS HUB receiver, SOCKS client or an outbound connection can claim an unused
// customer port and make an accounting-fence probe observe the fixture itself.
func meteredTrafficCustomerPorts(t *testing.T) (uint16, uint16) {
	t.Helper()
	raw, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		t.Fatal(err)
	}
	var low, high int
	if n, err := fmt.Sscan(string(raw), &low, &high); err != nil || n != 2 || low < 1 || high > 65535 || low > high {
		t.Fatal("unverified ephemeral port range", err)
	}
	ports := []uint16{}
	for p := 1024; p <= 65535 && len(ports) < 2; p++ {
		if p >= low && p <= high || p == 10085 || p == 29761 {
			continue
		}
		listener, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(p)))
		if err != nil {
			continue
		}
		ports = append(ports, uint16(p))
		listener.Close()
	}
	if len(ports) != 2 {
		t.Fatal("two non-ephemeral customer ports required by isolated traffic fixture")
	}
	return ports[0], ports[1]
}
