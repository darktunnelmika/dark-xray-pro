//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// This acceptance starts at the real saved-plan owner APIs. No target, route
// draft, binding, epoch, health evidence or handover is seeded by the fixture.
func TestRealPinnedInitialApplyProtocolHealth(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_HUB_TRAFFIC") != "1" {
		t.Skip("explicit isolated-network initial Apply acceptance required")
	}
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	interfaces, err := net.Interfaces()
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" || !filepath.IsAbs(binary) || len(pin) != 64 || os.Geteuid() == 0 || err != nil || len(interfaces) != 1 || interfaces[0].Name != "lo" {
		t.Fatal("nonroot pinned core in an isolated loopback-only namespace required")
	}
	for _, mode := range []string{"verified-subscription", "lost-receipt-before", "lost-receipt-after", "drain-during-probe", "reject-wrong-verifier", "expired-restart"} {
		t.Run(mode, func(t *testing.T) { testInitialApplyProtocolHealth(t, binary, pin, mode) })
	}
}

type initialHealthRealFixture struct {
	t             *testing.T
	ctx           context.Context
	server        *Server
	agent         http.Handler
	key           []byte
	origin        *httptest.Server
	public        *httptest.Server
	client        *http.Client
	endpoint      string
	verifier      mx.ProtocolHealthEndpoint
	runtime       *mx.TransactionRuntime
	options       mx.Options
	metered       mx.MeteredTransactionOptions
	cookie        *http.Cookie
	csrf          string
	saved         business.NativeInboundPlanReceipt
	customers     []business.Client
	port          uint16
	inboundRev    uint64
	stage, apply  business.MeteredRouteCommandView
	appliedStatus mx.TransactionStatus
	round         business.InitialApplyHealthRound
	tokenPath     string
	drained       bool
	customerBytes bool

	mu                             sync.Mutex
	epoch                          uc.Epoch
	final                          uc.Report
	zeros, births                  int
	stageSHA, probeSHA, probeEmail string
	probeID                        string
	healthRequests                 atomic.Int64
	blockVerifier                  atomic.Bool
	healthStarted                  chan struct{}
	healthRelease                  chan struct{}
	releaseOnce                    sync.Once
	loseBefore, loseAfter          atomic.Bool
	healthDeliveries               []mx.ProtocolHealthRequest
	healthReceipts                 []string
	trace                          []string
}

func initialHealthRealID(n int) string { return fmt.Sprintf("%032x", n) }

func newInitialHealthRealFixture(t *testing.T, binary, pin, mode string) *initialHealthRealFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	t.Cleanup(cancel)
	f := &initialHealthRealFixture{t: t, ctx: ctx, key: bytes.Repeat([]byte{0x41}, 32), healthStarted: make(chan struct{}, 1), healthRelease: make(chan struct{})}
	t.Cleanup(func() { f.releaseOnce.Do(func() { close(f.healthRelease) }) })
	target := liveUsageRealityAt(t, net.JoinHostPort(meteredTrafficAddress, "0"))
	f.origin = httptest.NewUnstartedServer(http.HandlerFunc(f.serveVerifier))
	f.origin.Listener.Close()
	var err error
	f.origin.Listener, err = net.Listen("tcp4", net.JoinHostPort(meteredTrafficAddress, "0"))
	if err != nil {
		t.Fatal(err)
	}
	f.origin.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	f.origin.StartTLS()
	f.origin.Client().Transport.(*http.Transport).TLSClientConfig.ServerName = "example.com"
	t.Cleanup(func() {
		f.releaseOnce.Do(func() { close(f.healthRelease) })
		f.origin.Close()
	})
	cert := f.origin.Certificate()
	digest := sha256.Sum256(cert.Raw)
	f.verifier = mx.ProtocolHealthEndpoint{Address: f.origin.Listener.Addr().String(), ServerName: "example.com", CertificateSHA: hex.EncodeToString(digest[:]), CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})}
	if mode == "reject-wrong-verifier" {
		f.verifier.CertificateSHA = strings.Repeat("a", 64)
		if f.verifier.CertificateSHA == hex.EncodeToString(digest[:]) {
			t.Fatal("wrong-pin fixture reused the real verifier certificate")
		}
	}
	f.port, _ = meteredTrafficCustomerPorts(t)
	f.server = enabledOwner(t)
	if err = f.server.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.server.EnableNativeInbounds(f.key); err != nil {
		t.Fatal(err)
	}
	ticket, err := f.server.registry.Issue("route-qa", "Disposable first Apply health")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.server.registry.Claim(ticket.PairCode, "route-qa", installID, nodeToken); err != nil {
		t.Fatal(err)
	}
	if err = f.server.EnableMeteredRouteNodesForPlanPromotion([]business.MeteredRouteNode{{NodeID: "route-qa", InstallationID: installID, BinarySHA: pin}}); err != nil {
		t.Fatal(err)
	}
	if err = f.server.EnableInboundInitialHealth(f.verifier); err != nil {
		t.Fatal("explicit single-Node initial-health startup", err)
	}
	if err = f.server.EnableLabSubscriptions(); err != nil {
		t.Fatal("explicit LAB subscription startup", err)
	}
	profile := business.InboundInput{ID: 7612, Name: "Disposable saved first Apply", Transport: "tcp", RealityTarget: target.Listener.Addr().String(), ServerNames: []string{"one.example.org"}, ShortIDs: []string{"aa11"}}
	f.saved = saveMeteredTrafficCreationPlan(t, f.server, profile, f.port)
	for _, id := range []string{"initial-health-customer", "initial-health-idle"} {
		customer, err := f.server.business.CreateClient(ctx, business.NewClient{ClientID: id, EventID: "create-" + id, RepresentativeID: "owner", Name: id, DurationDays: 30, VolumeBytes: 128 << 20})
		if err != nil {
			t.Fatal(err)
		}
		f.customers = append(f.customers, customer)
	}
	f.agent = f.server.AgentHandler()
	receiver := httptest.NewUnstartedServer(http.HandlerFunc(f.serveAgent))
	receiver.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	receiver.StartTLS()
	t.Cleanup(receiver.Close)
	cert = receiver.Certificate()
	digest = sha256.Sum256(cert.Raw)
	pinTLS := hex.EncodeToString(digest[:])
	f.client, f.endpoint, err = agenttransport.New(agenttransport.Config{Endpoint: receiver.URL, PinSHA256: pinTLS, CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})})
	if err != nil {
		t.Fatal(err)
	}
	sender, err := uc.NewSender(uc.SenderOptions{Endpoint: f.endpoint, NodeID: "route-qa", InstallationID: installID, Token: nodeToken, PinSHA256: pinTLS, Client: f.client})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sender.Close)
	authority, err := mx.NewMeteredRemoteAuthority(f.client, f.endpoint, nodeToken, "route-qa", installID)
	if err != nil {
		t.Fatal(err)
	}
	f.options = mx.Options{Directory: filepath.Join(t.TempDir(), "initial-health-runtime"), StateKey: bytes.Repeat([]byte{0x63}, 32), LeaseKey: []byte(nodeToken), NodeID: "route-qa", InstallationID: installID, ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin, ExperimentalFinalTCP: true}
	f.metered = mx.MeteredTransactionOptions{Authority: authority, Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 8 * time.Second, MaxPendingAge: 8 * time.Second}
	f.runtime, err = mx.OpenMeteredTransactions(f.options, f.metered)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if f.runtime != nil {
			f.runtime.Close()
		}
	})
	f.cookie, f.csrf = browserLogin(t, f.server, ownerUsername, ownerPassword)
	f.public = httptest.NewServer(f.server.BrowserHandler())
	t.Cleanup(f.public.Close)
	t.Cleanup(func() {
		if t.Failed() {
			f.mu.Lock()
			defer f.mu.Unlock()
			t.Log("sanitized initial-health HTTP trace", f.trace)
		}
	})
	return f
}

func (f *initialHealthRealFixture) serveVerifier(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/dark-pro/protocol-health/v1/") {
		f.healthRequests.Add(1)
		id := strings.TrimPrefix(r.URL.Path, "/dark-pro/protocol-health/v1/")
		if f.blockVerifier.Load() {
			select {
			case f.healthStarted <- struct{}{}:
			default:
			}
			select {
			case <-f.healthRelease:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		_, _ = io.WriteString(w, "DARK-PRO-PROTOCOL-HEALTH-v1\n"+id+"\n")
		return
	}
	if r.URL.Path != "/dark-pro/lab-subscription-echo" || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, (64<<10)+1))
	if err != nil || len(body) == 0 || len(body) > 64<<10 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

func (f *initialHealthRealFixture) closed() bool {
	connection, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(f.port))), 100*time.Millisecond)
	if err == nil {
		connection.Close()
	}
	return err != nil
}

func (f *initialHealthRealFixture) serveAgent(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	var report uc.Report
	if path == "/api/v1/agent/native-usage" || path == "/api/v1/agent/metered-health/receipt" {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			f.t.Error("Agent request unreadable", path)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		if path == "/api/v1/agent/metered-health/receipt" {
			f.mu.Lock()
			f.healthReceipts = append(f.healthReceipts, string(raw))
			f.mu.Unlock()
			if f.loseBefore.CompareAndSwap(true, false) {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		} else if json.Unmarshal(raw, &report) != nil {
			f.t.Error("invalid usage report")
		}
		if report.Sequence == 1 {
			if !f.closed() || report.Quiesced || len(report.Users) != 3 {
				f.t.Error("first zero receipt followed listener exposure or omitted the prepared probe")
			}
			for _, user := range report.Users {
				if user.Uplink != 0 || user.Downlink != 0 {
					f.t.Error("first admission receipt was not all zero")
				}
			}
		}
	}
	recorder := httptest.NewRecorder()
	f.agent.ServeHTTP(recorder, r)
	f.mu.Lock()
	f.trace = append(f.trace, fmt.Sprintf("%s HTTP %d", path, recorder.Code))
	if recorder.Code < 400 {
		switch path {
		case "/api/v1/agent/metered-route-transactions/poll":
			if recorder.Code == http.StatusOK {
				var delivery mx.TransactionDelivery
				if json.Unmarshal(recorder.Body.Bytes(), &delivery) != nil {
					f.t.Error("invalid first Apply command delivery")
				} else if delivery.Command.Action == "stage" {
					f.recordInitialCandidate(delivery)
				}
			}
		case "/api/v1/agent/metered-route-transactions/register":
			var epoch uc.Epoch
			if json.Unmarshal(recorder.Body.Bytes(), &epoch) != nil || epoch.NodeID != "route-qa" || epoch.InstallationID != installID || len(epoch.Bindings) != 3 || epoch.ConfigSHA != f.stageSHA || !f.closed() || f.births != 0 {
				f.t.Error("first process birth lost prepared candidate scope or preceded a closed listener")
			}
			for _, binding := range epoch.Bindings {
				if binding.Role == uc.SystemProbeRole {
					if binding.Billable || binding.Email != f.probeEmail || f.probeID != "" {
						f.t.Error("first epoch changed or duplicated the prepared nonbillable probe")
					}
					f.probeID = binding.ClientID
				}
			}
			if f.probeID == "" {
				f.t.Error("first admitted epoch omitted system probe accounting")
			}
			f.epoch, f.births = epoch, f.births+1
		case "/api/v1/agent/native-usage":
			if uc.VerifyReport(f.epoch, report) != nil {
				f.t.Error("usage report differs from the registered first process epoch")
			}
			if report.Sequence == 1 {
				f.zeros++
			}
			if report.Quiesced {
				if f.final.EpochID != "" && !reflect.DeepEqual(f.final, report) {
					f.t.Error("final report replay changed exact counters")
				}
				f.final = report
			}
		case "/api/v1/agent/metered-health/poll":
			if recorder.Code == http.StatusOK {
				var delivery mx.ProtocolHealthDelivery
				if json.Unmarshal(recorder.Body.Bytes(), &delivery) != nil {
					f.t.Error("invalid initial health delivery")
				}
				f.healthDeliveries = append(f.healthDeliveries, delivery.Request)
			}
		}
	}
	f.mu.Unlock()
	if path == "/api/v1/agent/metered-health/receipt" && f.loseAfter.CompareAndSwap(true, false) {
		if recorder.Code != http.StatusNoContent {
			f.t.Error("health reply loss did not follow a committed receipt", recorder.Code)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	for key, values := range recorder.Header() {
		w.Header()[key] = values
	}
	w.WriteHeader(recorder.Code)
	_, _ = w.Write(recorder.Body.Bytes())
}

// The first Stage carries the customer and probe credentials already reviewed
// by the owner. Inspect the actual authenticated delivery, without seeding or
// rewriting it, before the runtime may register an epoch or expose a listener.
func (f *initialHealthRealFixture) recordInitialCandidate(delivery mx.TransactionDelivery) {
	raw, err := base64.StdEncoding.Strict().DecodeString(delivery.ConfigBase64)
	delivery.Candidate.Config = raw
	if err != nil || mx.TransactionPayloadSHA(delivery.Candidate) != delivery.Command.PayloadSHA || len(delivery.Candidate.Listeners) != 1 || f.stageSHA != "" || !f.closed() {
		f.t.Error("initial Stage did not carry one immutable closed candidate")
		return
	}
	var config struct {
		Inbounds []struct {
			Listen   string `json:"listen"`
			Port     uint16 `json:"port"`
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []struct {
					ID, Email, Flow string
				} `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(raw, &config) != nil || len(config.Inbounds) != 1 {
		f.t.Error("initial Stage private candidate malformed")
		return
	}
	inbound := config.Inbounds[0]
	if inbound.Listen != "127.0.0.1" || inbound.Port != f.port || inbound.Protocol != "vless" || len(inbound.Settings.Clients) != 3 {
		f.t.Error("initial Stage omitted the complete customer and reserved probe set")
	}
	seen := map[string]bool{}
	for _, user := range inbound.Settings.Clients {
		if user.Flow != "xtls-rprx-vision" || seen[user.ID] {
			f.t.Error("initial candidate changed Vision or duplicated a credential")
		}
		seen[user.ID] = true
		customer := false
		for _, expected := range f.customers {
			if user.ID == expected.UUID {
				customer = true
				email, err := uc.CounterEmail(expected.ID)
				if err != nil || user.Email != email {
					f.t.Error("initial candidate changed customer counter attribution")
				}
			}
		}
		if !customer {
			if f.probeSHA != "" {
				f.t.Error("initial candidate acquired more than one system credential")
			}
			digest := sha256.Sum256([]byte(user.ID))
			f.probeSHA, f.probeEmail = hex.EncodeToString(digest[:]), user.Email
		}
	}
	for _, customer := range f.customers {
		if !seen[customer.UUID] {
			f.t.Error("initial candidate lost a promoted customer credential")
		}
	}
	f.stageSHA = delivery.Candidate.SHA256
}

func (f *initialHealthRealFixture) base() string {
	return fmt.Sprintf("/api/v1/workspace/native-inbound-plans/%d/initial-health", f.saved.Plan.Profile.ID)
}

func (f *initialHealthRealFixture) owner(method, path string, body any, want int, out any) {
	f.t.Helper()
	w := browserRequest(f.t, f.server.BrowserHandler(), method, path, body, f.cookie, f.csrf, "https://"+browserHost)
	if w.Code != want {
		f.t.Fatal("initial-health owner response", method, path, w.Code, "want", want)
	}
	if out != nil && json.Unmarshal(w.Body.Bytes(), out) != nil {
		f.t.Fatal("invalid initial-health owner response")
	}
}

func (f *initialHealthRealFixture) sync() {
	f.t.Helper()
	if err := f.runtime.SyncMeteredTransaction(f.ctx, f.client, f.endpoint); err != nil {
		f.t.Fatal("initial Apply Agent command sync", err)
	}
}

func (f *initialHealthRealFixture) state() business.InitialApplyHealthState {
	f.t.Helper()
	var state business.InitialApplyHealthState
	f.owner(http.MethodGet, f.base(), nil, http.StatusOK, &state)
	return state
}

func (f *initialHealthRealFixture) readRound() business.InitialApplyHealthRound {
	f.t.Helper()
	var round business.InitialApplyHealthRound
	f.owner(http.MethodGet, f.base()+"/rounds/"+f.round.ID, nil, http.StatusOK, &round)
	if round.ID != f.round.ID || round.InitialApplySHA != f.round.InitialApplySHA || round.ManifestSHA != f.round.ManifestSHA || round.Created != f.round.Created || round.Expires != f.round.Expires || round.ApplyCommandID != f.apply.ID || round.StageID != f.stage.ID || len(round.Members) != 1 {
		f.t.Fatal("initial health read changed immutable proof coverage or deadline")
	}
	return round
}

func (f *initialHealthRealFixture) commandRequest(n int, action, stage string, revision uint64) business.MeteredRouteCommandRequest {
	request := meteredRouteOwnerRequest(f.runtime.Status(), n, action, stage, revision)
	for i := range request.Drafts {
		request.Drafts[i].InboundRevision = f.inboundRev
	}
	return request
}

func (f *initialHealthRealFixture) command(n int, action, stage string, revision uint64) business.MeteredRouteCommandView {
	f.t.Helper()
	f.sync()
	var command business.MeteredRouteCommandView
	f.owner(http.MethodPost, "/api/v1/workspace/native-nodes/route-qa/metered-route-transactions", f.commandRequest(n, action, stage, revision), http.StatusAccepted, &command)
	f.sync()
	return command
}

func (f *initialHealthRealFixture) receipt(id, outcome string) mx.TransactionReceipt {
	f.t.Helper()
	channel, err := f.server.business.MeteredRouteChannel(f.ctx, f.key, "route-qa")
	if err != nil {
		f.t.Fatal(err)
	}
	for _, command := range channel.Commands {
		if command.ID == id && command.Receipt != nil && command.Receipt.Outcome == outcome {
			return *command.Receipt
		}
	}
	f.t.Fatal("missing exact owner command receipt", outcome)
	return mx.TransactionReceipt{}
}

func (f *initialHealthRealFixture) prepareAndApply() {
	f.t.Helper()
	f.sync()
	status := f.runtime.Status()
	if status.Usage == nil || *status.Usage != (mx.MeteredCondition{}) || status.Observed.ProcessReady || !f.closed() {
		f.t.Fatal("saved-plan promotion did not start from a fresh signed empty runtime")
	}
	f.inboundRev = promoteMeteredTrafficCreationPlan(f.t, f.server, f.saved, f.customers, f.cookie, f.csrf)
	state := f.state()
	if state.Prepared || !state.CanPrepare || state.Eligible || state.PromotionEventID == "" || state.InitialApplySHA != "" || !f.closed() {
		f.t.Fatal("promotion bypassed explicit initial-health preparation")
	}
	// The real Stage route must enforce preparation; the rejected attempt
	// cannot consume a command serial or force a later re-Apply.
	f.owner(http.MethodPost, "/api/v1/workspace/native-nodes/route-qa/metered-route-transactions", f.commandRequest(1, "stage", "", 1), http.StatusConflict, nil)
	input := map[string]any{"event_id": initialHealthRealID(101), "promotion_event_id": state.PromotionEventID, "confirmed": true}
	var prepared, replay business.InitialApplyHealthPreparation
	f.owner(http.MethodPost, f.base()+"/prepare", input, http.StatusCreated, &prepared)
	f.owner(http.MethodPost, f.base()+"/prepare", input, http.StatusOK, &replay)
	if !prepared.Prepared || prepared.Replayed || prepared.PromotionEventID != state.PromotionEventID || prepared.InboundID != f.saved.Plan.Profile.ID || !replay.Replayed {
		f.t.Fatal("explicit preparation or its exact retry lost promotion identity")
	}
	replay.Replayed = false
	if !reflect.DeepEqual(prepared, replay) || f.runtime.Status().Serial != 0 || !f.closed() {
		f.t.Fatal("preparation retry started or restaged the runtime")
	}
	f.stage = f.command(1, "stage", "", 1)
	f.receipt(f.stage.ID, "staged")
	if !f.closed() {
		f.t.Fatal("Stage exposed a listener before the first zero receipt")
	}
	f.apply = f.command(2, "apply", f.stage.ID, 0)
	f.receipt(f.apply.ID, "applied")
	f.sync() // A fresh signed observation follows the historical Apply ACK.
	f.appliedStatus = f.runtime.Status()
	state = f.state()
	if !state.Prepared || !state.Eligible || len(state.InitialApplySHA) != 64 || state.ApplyCommandID != f.apply.ID || state.StageID != f.stage.ID || f.appliedStatus.Usage == nil || state.EpochID != f.appliedStatus.Usage.EpochID || !f.appliedStatus.Observed.ProcessReady || f.appliedStatus.Observed.DataPlaneVerified || f.appliedStatus.Observed.SubscriptionPublished {
		f.t.Fatal("first Apply did not expose its exact metered health authority")
	}
	f.mu.Lock()
	if f.births != 1 || f.zeros != 1 || f.epoch.ID != state.EpochID || f.probeID == "" || f.probeSHA == "" {
		f.t.Error("initial Apply missed its single zero-first admitted probe epoch")
	}
	f.mu.Unlock()
	// Select the recipient and obtain its existing token before the bounded
	// proof starts. None of these operations can publish desired configuration.
	base := "/api/v1/workspace/clients/" + f.customers[0].ID + "/subscription"
	var issued business.SubscriptionTokenReceipt
	f.owner(http.MethodPost, base+"/issue", map[string]any{"event_id": initialHealthRealID(201), "expected_generation": 0}, http.StatusOK, &issued)
	var secret struct {
		business.SubscriptionSecret
		Path string `json:"path"`
	}
	f.owner(http.MethodPost, base+"/reveal", map[string]any{"expected_generation": issued.Generation}, http.StatusOK, &secret)
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(secret.Token)
	if err != nil || len(decoded) != 32 || secret.ClientID != f.customers[0].ID || secret.Generation != issued.Generation || secret.Path != "/api/v1/subscriptions/"+secret.Token {
		f.t.Fatal("token selection lost exact customer generation")
	}
	f.tokenPath = secret.Path
	f.fetch(f.tokenPath, http.StatusNotFound)
	input = map[string]any{"id": initialHealthRealID(301), "initial_apply_sha": state.InitialApplySHA, "confirmed": true}
	f.owner(http.MethodPost, f.base()+"/rounds", input, http.StatusAccepted, &f.round)
	var retry business.InitialApplyHealthRound
	f.owner(http.MethodPost, f.base()+"/rounds", input, http.StatusAccepted, &retry)
	retry.Replayed = false
	if !reflect.DeepEqual(f.round, retry) || f.round.Scope != business.InitialApplyHealthScope || f.round.Current || f.round.State != "pending" || f.round.InitialApplySHA != state.InitialApplySHA || f.round.ApplyCommandID != f.apply.ID || f.round.StageID != f.stage.ID || f.round.EpochID != state.EpochID || len(f.round.Members) != 1 || f.round.Members[0].InboundID != f.saved.Plan.Profile.ID || f.round.Members[0].Port != f.port || f.round.Expires <= f.round.Created || f.round.Expires-f.round.Created > int64(8*time.Second) || f.round.Expires > f.appliedStatus.Observed.LeaseExpires || f.healthRequests.Load() != 0 {
		f.t.Fatal("owner health round lost single-inbound first-Apply scope or original deadline")
	}
	f.owner(http.MethodPost, f.base()+"/snapshot", f.snapshotInput(), http.StatusConflict, nil)
	f.fetch(f.tokenPath, http.StatusNotFound)
}

func (f *initialHealthRealFixture) snapshotInput() map[string]any {
	return map[string]any{"event_id": initialHealthRealID(401), "client_id": f.customers[0].ID, "round_id": f.round.ID, "initial_apply_sha": f.round.InitialApplySHA, "expected_revision": 0, "expected_delivery_revision": 0, "confirmed": true}
}

func testInitialApplyProtocolHealth(t *testing.T, binary, pin, mode string) {
	f := newInitialHealthRealFixture(t, binary, pin, mode)
	f.prepareAndApply()
	if mode == "drain-during-probe" {
		f.blockVerifier.Store(true)
		done := make(chan error, 1)
		go func() { done <- f.runtime.SyncMeteredHealth(f.ctx, f.client, f.endpoint) }()
		select {
		case <-f.healthStarted:
		case err := <-done:
			t.Fatal("probe stopped before reaching held verifier", err)
		case <-f.ctx.Done():
			t.Fatal("real initial Apply probe did not reach held verifier")
		}
		f.drain()
		if time.Now().UnixNano() >= f.round.Expires {
			t.Fatal("Drain waited for the blocked protocol probe to expire")
		}
		f.releaseOnce.Do(func() { close(f.healthRelease) })
		select {
		case err := <-done:
			if err == nil {
				t.Fatal("proof survived Drain during verifier I/O")
			}
		case <-f.ctx.Done():
			t.Fatal("Drain blocked behind verifier I/O")
		}
		f.assertUnavailable()
	} else {
		f.loseBefore.Store(mode == "lost-receipt-before")
		f.loseAfter.Store(mode == "lost-receipt-after" || mode == "expired-restart")
		err := f.runtime.SyncMeteredHealth(f.ctx, f.client, f.endpoint)
		wantFailure := mode != "verified-subscription"
		if (err != nil) != wantFailure {
			t.Fatal("initial health transport failure contract", mode, err)
		}
		if mode == "reject-wrong-verifier" {
			if f.healthRequests.Load() != 0 {
				t.Fatal("wrong TLS certificate pin reached the verifier request handler")
			}
			f.assertUnavailable()
		} else {
			before := f.readRound()
			if before.Current != (mode != "lost-receipt-before") || f.healthRequests.Load() != 1 {
				t.Fatal("lost receipt changed its commit boundary or repeated a real probe")
			}
			if mode == "lost-receipt-before" {
				f.owner(http.MethodPost, f.base()+"/snapshot", f.snapshotInput(), http.StatusConflict, nil)
				f.fetch(f.tokenPath, http.StatusNotFound)
			}
			if mode == "lost-receipt-before" || mode == "lost-receipt-after" {
				if err = f.runtime.SyncMeteredHealth(f.ctx, f.client, f.endpoint); err != nil {
					t.Fatal("exact saved initial Apply receipt replay", err)
				}
			}
			complete := f.readRound()
			if !complete.Current || complete.State != "complete" || f.healthRequests.Load() != 1 || (before.Current && !reflect.DeepEqual(before, complete)) {
				t.Fatal("receipt retry altered accepted evidence or failed to complete the proof")
			}
			f.consumeSubscription(binary)
			if mode == "expired-restart" {
				timer := time.NewTimer(time.Until(time.Unix(0, f.round.Expires)) + 10*time.Millisecond)
				select {
				case <-timer.C:
				case <-f.ctx.Done():
					timer.Stop()
					t.Fatal("fixture ended before the original proof deadline")
				}
				f.fetch(f.tokenPath, http.StatusNotFound)
				if f.readRound().Current {
					t.Fatal("accepted historical evidence renewed its original deadline")
				}
				f.drain()
				f.runtime.Close()
				f.runtime, err = mx.OpenMeteredTransactions(f.options, f.metered)
				if err != nil {
					t.Fatal("reopen settled initial Apply runtime", err)
				}
				f.sync()
				if err = f.runtime.SyncMeteredHealth(f.ctx, f.client, f.endpoint); err != nil {
					t.Fatal("expired historical v4 outbox replay after restart", err)
				}
				if !f.closed() || f.runtime.Status().Observed.ProcessReady || f.healthRequests.Load() != 1 {
					t.Fatal("restart or historical receipt replay resumed first-Apply traffic")
				}
			}
		}
	}
	if !f.drained {
		f.drain()
	}
	f.fetch(f.tokenPath, http.StatusNotFound)
	f.assertFinalAccounting()
	f.assertHealthTranscript(mode)
	if mode == "verified-subscription" {
		t.Log("INITIAL_APPLY_SUBSCRIPTION_REAL_PASS: owner saved plan, promotion and explicit preparation; first zero receipt before listener; real v4 pinned Reality/Vision proof; actual token URI customer echo; exact separate customer/probe settlement without a handover")
	}
}

func (f *initialHealthRealFixture) fetch(path string, want int) []byte {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 3*time.Second)
	defer cancel()
	if want == http.StatusOK && f.round.Expires != 0 {
		var stop context.CancelFunc
		ctx, stop = context.WithDeadline(ctx, time.Unix(0, f.round.Expires))
		defer stop()
	}
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, f.public.URL+path, nil)
		if err != nil {
			f.t.Fatal("subscription request construction failed")
		}
		response, err := f.public.Client().Do(request)
		if err != nil {
			f.t.Fatal("subscription transport or original delivery deadline failed")
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		response.Body.Close()
		if err != nil || len(body) > 2<<20 || !strings.Contains(response.Header.Get("Cache-Control"), "no-store") {
			f.t.Fatal("subscription response lost its size or cache boundary")
		}
		if response.StatusCode == http.StatusServiceUnavailable {
			timer := time.NewTimer(20 * time.Millisecond)
			select {
			case <-timer.C:
				continue
			case <-ctx.Done():
				timer.Stop()
				f.t.Fatal("subscription stayed busy past its bounded deadline")
			}
		}
		if response.StatusCode != want {
			f.t.Fatal("subscription status", response.StatusCode, "want", want)
		}
		if want == http.StatusOK && (ctx.Err() != nil || time.Now().UnixNano() >= f.round.Expires || response.Header.Get("X-Dark-Pro-Scope") != business.InitialApplyHealthScope) {
			f.t.Fatal("subscription response changed first-Apply scope or extended its deadline")
		}
		return body
	}
}

func (f *initialHealthRealFixture) consumeSubscription(binary string) {
	f.t.Helper()
	var snapshot, replay business.LabSubscriptionSnapshot
	f.owner(http.MethodPost, f.base()+"/snapshot", f.snapshotInput(), http.StatusCreated, &snapshot)
	if snapshot.ID == "" || snapshot.Scope != business.InitialApplyHealthScope || snapshot.HandoverID != "" || snapshot.InboundID != f.saved.Plan.Profile.ID || snapshot.InitialApplySHA != f.round.InitialApplySHA || snapshot.PromotionEventID != f.round.PromotionEventID || snapshot.RoundID != f.round.ID || snapshot.NodeID != "route-qa" || snapshot.InstallationID != installID || snapshot.ClientID != f.customers[0].ID || snapshot.RouteCount != 1 || snapshot.Revision != 1 || snapshot.DeliveryRevision != 1 || !snapshot.Current || snapshot.CreatedUnixNano < f.round.Created || snapshot.ExpiresUnixNano <= snapshot.CreatedUnixNano || snapshot.ExpiresUnixNano > f.round.Expires || snapshot.ExpiresUnixNano > f.appliedStatus.Observed.LeaseExpires || snapshot.ConsumerManifestSHA != "" {
		f.t.Fatal("initial snapshot lost exact promotion, recipient, delivery revision or proof deadline")
	}
	f.owner(http.MethodPost, f.base()+"/snapshot", f.snapshotInput(), http.StatusOK, &replay)
	if !replay.Replayed {
		f.t.Fatal("snapshot retry did not identify immutable receipt replay")
	}
	replay.Replayed = false
	if replay != snapshot {
		f.t.Fatal("snapshot retry replaced its content, recipient or original deadline")
	}
	raw := f.fetch(f.tokenPath+"?format=raw", http.StatusOK)
	for _, format := range []string{"", "?format=base64"} {
		encoded := f.fetch(f.tokenPath+format, http.StatusOK)
		decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(encoded)))
		if err != nil || !bytes.Equal(decoded, raw) {
			f.t.Fatal("raw and base64 delivery differed from the same proven snapshot")
		}
	}
	f.mu.Lock()
	probeID := f.probeID
	f.mu.Unlock()
	if bytes.Contains(raw, []byte(f.customers[1].UUID)) || bytes.Contains(raw, []byte(probeID)) {
		f.t.Fatal("subscription disclosed another customer or the system accounting identity")
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 {
		f.t.Fatal("initial Apply output did not contain exactly its proven loopback route")
	}
	u, err := url.Parse(lines[0])
	if err != nil || u.Scheme != "vless" || u.User == nil || u.User.Username() != f.customers[0].UUID || u.Host != net.JoinHostPort("127.0.0.1", strconv.Itoa(int(f.port))) || u.Path != "" || u.Fragment == "" {
		f.t.Fatal("actual token URI changed customer or applied listener identity")
	}
	if _, password := u.User.Password(); password {
		f.t.Fatal("actual token URI gained an unexpected credential")
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 8 || query.Get("encryption") != "none" || query.Get("flow") != "xtls-rprx-vision" || query.Get("type") != "tcp" || query.Get("security") != "reality" || query.Get("fp") != "chrome" || query.Get("sni") != "one.example.org" || query.Get("sid") != "aa11" || query.Get("pbk") != f.saved.Plan.Profile.PublicKey {
		f.t.Fatal("actual token URI changed the saved Reality/Vision profile")
	}
	for _, values := range query {
		if len(values) != 1 {
			f.t.Fatal("actual token URI contained ambiguous parameters")
		}
	}
	ctx, cancel := context.WithDeadline(f.ctx, time.Unix(0, snapshot.ExpiresUnixNano))
	defer cancel()
	client, stop := labSubscriptionURIClient(f.t, ctx, binary, u, f.origin)
	defer stop()
	payload := bytes.Repeat([]byte("initial Apply subscription echo\n"), 1024)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, f.origin.URL+"/dark-pro/lab-subscription-echo", bytes.NewReader(payload))
	if err != nil {
		f.t.Fatal("real customer echo construction failed")
	}
	response, err := client.Do(request)
	if err != nil {
		f.t.Fatal("actual token URI could not carry pinned-core customer traffic")
	}
	defer response.Body.Close()
	echo, err := io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
	if err != nil || response.StatusCode != http.StatusOK || !bytes.Equal(echo, payload) {
		f.t.Fatal("actual token URI did not preserve exact customer echo bytes")
	}
	f.customerBytes = true
	status := f.runtime.Status()
	if status.Instance != f.appliedStatus.Instance || status.Serial != f.appliedStatus.Serial || status.CurrentStageID != f.appliedStatus.CurrentStageID || status.Usage == nil || *status.Usage != *f.appliedStatus.Usage || mx.Condition(status.Observed) != mx.Condition(f.appliedStatus.Observed) || status.Observed.LeaseExpires != f.appliedStatus.Observed.LeaseExpires || status.Observed.DataPlaneVerified || status.Observed.SubscriptionPublished {
		f.t.Fatal("health or subscription delivery changed Apply, epoch, lease or production publication authority")
	}
}

func (f *initialHealthRealFixture) assertUnavailable() {
	f.t.Helper()
	if f.readRound().Current {
		f.t.Fatal("failed or stopped initial protocol probe became current evidence")
	}
	f.owner(http.MethodPost, f.base()+"/snapshot", f.snapshotInput(), http.StatusConflict, nil)
	f.fetch(f.tokenPath, http.StatusNotFound)
}

func (f *initialHealthRealFixture) drain() {
	f.t.Helper()
	if f.drained {
		f.t.Fatal("fixture attempted a second Drain")
	}
	command := f.command(3, "drain", f.stage.ID, 0)
	ack := f.receipt(command.ID, "drained")
	f.sync()
	status := f.runtime.Status()
	if !f.closed() || status.Observed.ProcessReady || status.Observed.Usage == nil || !status.Observed.Usage.Settled || ack.Metered == nil || ack.Metered.After.EpochID != f.appliedStatus.Usage.EpochID || ack.Metered.PriorSettlement != ack.Metered.After.Settlement {
		f.t.Fatal("owner Drain lost exact final accounting or left the listener open")
	}
	proof, err := f.server.business.UsageLedger().ConfirmSettlement(f.ctx, usageledger.Principal{NodeID: "route-qa", InstallationID: installID}, ack.Metered.PriorSettlement)
	if err != nil || !proof.Sealed {
		f.t.Fatal("owner Drain has no exact HUB final seal", err)
	}
	f.drained = true
}

func (f *initialHealthRealFixture) assertFinalAccounting() {
	f.t.Helper()
	f.mu.Lock()
	epoch, final, births, zeros, probeID := f.epoch, f.final, f.births, f.zeros, f.probeID
	f.mu.Unlock()
	if births != 1 || zeros != 1 || len(epoch.Bindings) != 3 || len(final.Users) != 3 || !final.Quiesced || uc.VerifyReport(epoch, final) != nil || uc.VerifyEpochStopped(epoch) != nil {
		f.t.Fatal("first Apply lost its single birth, complete zero/final report or process stop")
	}
	totals := map[string]uc.Usage{}
	for _, user := range final.Users {
		totals[user.ClientID] = user
	}
	for i, customer := range f.customers {
		want := totals[customer.ID]
		got, err := f.server.business.ClientUsage(f.ctx, customer.ID)
		if err != nil || got.Uplink != want.Uplink || got.Downlink != want.Downlink || got.Total != want.Uplink+want.Downlink {
			f.t.Fatal("initial Apply lost or double billed actual customer counters", err)
		}
		if (i == 1 || !f.customerBytes) && got.Total != 0 {
			f.t.Fatal("system proof traffic debited an idle customer")
		}
		if i == 0 && f.customerBytes && (got.Uplink < 16<<10 || got.Downlink < 16<<10) {
			f.t.Fatal("actual token URI echo was absent from final customer accounting")
		}
		after, err := f.server.business.Client(f.ctx, customer.ID)
		if err != nil || after != customer {
			f.t.Fatal("initial health changed customer UUID, plan, expiry or financial metadata", err)
		}
	}
	want := totals[probeID]
	probe, err := f.server.business.UsageLedger().ProbeUsage(f.ctx, probeID)
	if err != nil || probe.Uplink != want.Uplink || probe.Downlink != want.Downlink || probe.Total != want.Uplink+want.Downlink || probe.Holds != 0 || probe.Uplink == 0 || probe.Downlink == 0 {
		f.t.Fatal("protocol probe traffic was lost or escaped its nonbillable system ledger", err)
	}
	if _, err = f.server.business.Client(f.ctx, probeID); err == nil {
		f.t.Fatal("reserved system probe became a customer")
	}
	if _, err = f.server.business.UsageLedger().Account(f.ctx, probeID); err == nil {
		f.t.Fatal("reserved system probe gained a financial account")
	}
}

func (f *initialHealthRealFixture) assertHealthTranscript(mode string) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.healthDeliveries) == 0 {
		f.t.Fatal("owner round never reached authenticated Agent transport")
	}
	q := f.healthDeliveries[0]
	if q.Version != mx.InitialApplyProtocolHealthVersion || q.HandoverID != "" || q.InitialApplySHA != f.round.InitialApplySHA || q.NodeID != "route-qa" || q.InstallationID != installID || q.RuntimeInstance != f.appliedStatus.Instance || q.StageID != f.stage.ID || q.ApplyCommandID != f.apply.ID || q.InboundID != f.saved.Plan.Profile.ID || q.Port != f.port || q.SystemProbeID != f.probeID || q.CredentialSHA != f.probeSHA || q.EndpointSHA != f.verifier.SHA() || q.ConsumerManifestSHA != "" || q.ConsumerAddress != "" || q.ConsumerPort != 0 || q.Expected != mx.Condition(f.appliedStatus.Observed) || q.Usage != *f.appliedStatus.Usage || q.Expires > f.round.Expires || q.Issued < f.round.Created {
		f.t.Fatal("real initial health delivery crossed promotion, Apply, epoch, endpoint or system identity scope")
	}
	for _, delivery := range f.healthDeliveries {
		if delivery != q {
			f.t.Fatal("health poll retry changed its immutable signed challenge")
		}
	}
	wantReceipts, wantRequests := 1, int64(1)
	if mode == "lost-receipt-before" || mode == "lost-receipt-after" || mode == "expired-restart" {
		wantReceipts = 2
	}
	if mode == "drain-during-probe" || mode == "reject-wrong-verifier" {
		wantReceipts = 0
	}
	if mode == "reject-wrong-verifier" {
		wantRequests = 0
	}
	if len(f.healthReceipts) != wantReceipts || f.healthRequests.Load() != wantRequests {
		f.t.Fatal("initial health executed or submitted evidence an unexpected number of times", len(f.healthReceipts), f.healthRequests.Load())
	}
	for i, raw := range f.healthReceipts {
		var submission mx.ProtocolHealthSubmission
		if json.Unmarshal([]byte(raw), &submission) != nil || submission.ChallengeID != q.ID || !mx.VerifyProtocolHealthReceipt([]byte(nodeToken), q, submission.Receipt, time.Unix(0, submission.Receipt.Completed)) || (i > 0 && raw != f.healthReceipts[0]) {
			f.t.Fatal("saved initial Apply evidence changed during lost-reply or restart replay")
		}
	}
}
