package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/replacement"
	"github.com/darktunnelmika/dark-xray-pro/internal/routeplan"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

const handoverBase = "/api/v1/workspace/metered-handovers"

func handoverTestID(n int) string { return fmt.Sprintf("%032x", n) }
func handoverHash(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func handoverACK(token string, c mx.TransactionCommand, o mx.Snapshot, outcome string, u mx.MeteredCondition, seal uc.Receipt) mx.TransactionReceipt {
	a := mx.TransactionReceipt{Version: 2, CommandID: c.ID, CommandSHA: handoverHash(c), Serial: c.Serial, Action: c.Action, StageID: c.StageID, NodeID: c.NodeID, InstallationID: c.InstallationID, RuntimeInstance: c.RuntimeInstance, Outcome: outcome, ObservedAt: time.Now().UnixNano(), Observed: o, Metered: &mx.MeteredEvidence{After: u, PriorSettlement: seal}}
	raw, _ := json.Marshal(a)
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("DARK-PRO-ROUTE-RECEIPT-v1\x00"))
	mac.Write(raw)
	a.Signature = hex.EncodeToString(mac.Sum(nil))
	return a
}

type handoverAPI struct {
	s      *Server
	key    []byte
	tokens []string
	p      []mx.TransactionPoll
	plan   meteredHandoverInput
	epoch  uc.Epoch
	cookie *http.Cookie
	csrf   string
}

func (f *handoverAPI) post(t *testing.T, path string, in any) *httptest.ResponseRecorder {
	t.Helper()
	return browserRequest(t, f.s.BrowserHandler(), "POST", path, in, f.cookie, f.csrf, "https://"+browserHost)
}
func (f *handoverAPI) poll(t *testing.T, i int, want bool) mx.TransactionDelivery {
	t.Helper()
	p := &f.p[i]
	p.At = time.Now().UnixNano()
	*p = mx.SignMeteredTransactionPoll([]byte(f.tokens[i]), *p)
	d, found, err := f.s.business.PollMeteredRouteCommand(context.Background(), f.key, []byte(f.tokens[i]), *p)
	if err != nil || found != want {
		t.Fatal("poll", i, found, err)
	}
	return d
}
func (f *handoverAPI) input(i, n int, action, stage string) business.MeteredRouteCommandRequest {
	p := f.p[i]
	in := business.MeteredRouteCommandRequest{ID: handoverTestID(n), Action: action, StageID: stage, RuntimeInstance: p.Status.Instance, Expected: mx.Condition(p.Status.Observed), Usage: p.Status.Usage}
	if action == "stage" {
		in.Drafts = []business.RouteDraftRef{{InboundID: 7612, InboundRevision: 1, DraftRevision: 1}}
	}
	return in
}
func newHandoverAPI(t *testing.T, beforeStage ...func(*handoverAPI)) *handoverAPI {
	t.Helper()
	ctx := context.Background()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0x41}, 32)
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.CreateInbound(ctx, key, business.InboundInput{ID: 7612, Name: "Handover LAB", Transport: "tcp", RealityTarget: "qa.invalid:443", ServerNames: []string{"qa.example.org"}, ShortIDs: []string{"aa11"}}); err != nil {
		t.Fatal(err)
	}
	c, err := s.business.CreateClient(ctx, business.NewClient{EventID: "handover-api-customer-001", ClientID: "handover-customer", RepresentativeID: "owner", Name: "Disposable LAB", DurationDays: 30, VolumeBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.business.BindInboundClients(ctx, 7612, []string{c.ID}, true); err != nil {
		t.Fatal(err)
	}
	f := &handoverAPI{s: s, key: key, tokens: []string{nodeToken, newToken}}
	nodes := []business.MeteredRouteNode{}
	for i, id := range []string{"old-de", "new-de"} {
		install := installID
		if i == 1 {
			install = newInstallation
		}
		ticket, err := s.registry.Issue(id, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.registry.Claim(ticket.PairCode, id, install, f.tokens[i]); err != nil {
			t.Fatal(err)
		}
		port := uint16(27612 + i)
		if err = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 7612, NodeID: id, InstallationID: install, ListenIP: "127.0.0.1", DirectPort: port}); err != nil {
			t.Fatal(err)
		}
		if _, err = s.business.SaveNativeRouteDraft(ctx, key, business.NativeRouteDraftInput{EventID: "handover-api-draft-" + id, InboundID: 7612, NodeID: id, InstallationID: install, ExpectedInboundRevision: 1, DirectPort: port, Reason: "Two approved loopback Nodes"}); err != nil {
			t.Fatal(err)
		}
		nodes = append(nodes, business.MeteredRouteNode{NodeID: id, InstallationID: install, BinarySHA: strings.Repeat("a", 64)})
		f.p = append(f.p, mx.TransactionPoll{NodeID: id, InstallationID: install, BinarySHA: strings.Repeat("a", 64), Status: mx.TransactionStatus{Instance: handoverTestID(100 + i), Usage: &mx.MeteredCondition{}, Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}})
	}
	if err = s.EnableMeteredRouteNodes(nodes); err != nil {
		t.Fatal(err)
	}
	for i := range f.p {
		f.poll(t, i, false)
	}
	for _, setup := range beforeStage {
		setup(f)
	}
	for i, n := range []int{11, 21} {
		if _, err = s.business.QueueMeteredRouteCommand(ctx, key, f.p[i].NodeID, f.input(i, n, "stage", "")); err != nil {
			t.Fatal(err)
		}
		d := f.poll(t, i, true)
		ack := handoverACK(f.tokens[i], d.Command, f.p[i].Status.Observed, "staged", *f.p[i].Status.Usage, uc.Receipt{})
		if err = s.business.AcceptMeteredRouteReceipt(ctx, key, []byte(f.tokens[i]), ack); err != nil {
			t.Fatal(err)
		}
		f.p[i].Status.Serial, f.p[i].Status.StagedID = 1, d.Command.StageID
		f.poll(t, i, false)
	}
	if _, err = s.business.QueueMeteredRouteCommand(ctx, key, f.p[0].NodeID, f.input(0, 12, "apply", handoverTestID(11))); err != nil {
		t.Fatal(err)
	}
	d := f.poll(t, 0, true)
	cmd := d.Command
	b, err := mx.SignBirthReport(f.tokens[0], cmd.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 12345, StartTicks: 77})
	if err != nil {
		t.Fatal(err)
	}
	f.epoch, err = s.business.MeteredRouteAuthority(ctx, key, []byte(f.tokens[0]), mx.MeteredAuthorityRequest{Command: cmd, Birth: &b})
	if err != nil {
		t.Fatal(err)
	}
	_, binding, _ := uc.CanonicalEpoch(f.epoch)
	zero := uc.Report{Version: 1, EpochID: f.epoch.ID, BindingSHA: binding, Sequence: 1}
	for _, binding := range f.epoch.Bindings {
		zero.Users = append(zero.Users, uc.Usage{ClientID: binding.ClientID})
	}
	zero.SHA = handoverHash(zero)
	if _, err = s.business.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: f.p[0].NodeID, InstallationID: f.p[0].InstallationID}, zero); err != nil {
		t.Fatal(err)
	}
	u := mx.MeteredCondition{EpochID: f.epoch.ID, EpochSHA: handoverHash(f.epoch), AdmissionSHA: strings.Repeat("d", 64)}
	o := mx.Snapshot{Phase: "ready_observed_admission", Highwater: cmd.RuntimeSequence, SavedSequence: cmd.RuntimeSequence, SavedSHA: cmd.Lease.ConfigSHA, ActiveSequence: cmd.RuntimeSequence, ActiveSHA: cmd.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: cmd.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	if err = s.business.AcceptMeteredRouteReceipt(ctx, key, []byte(f.tokens[0]), handoverACK(f.tokens[0], cmd, o, "applied", u, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	f.p[0].Status.Serial, f.p[0].Status.StagedID, f.p[0].Status.CurrentStageID = 2, "", cmd.StageID
	f.p[0].Status.Observed, f.p[0].Status.Usage = o, &u
	f.poll(t, 0, false)
	f.plan = meteredHandoverInput{MeteredHandoverRequest: business.MeteredHandoverRequest{ID: handoverTestID(31), SourceNode: "old-de", TargetNode: "new-de", SourceStageID: handoverTestID(11), TargetStageID: handoverTestID(21), SourceUsage: u}, SourceInstallation: installID, TargetInstallation: newInstallation}
	f.cookie, f.csrf = browserLogin(t, s, ownerUsername, ownerPassword)
	return f
}

func TestMeteredHandoverOwnerAPIExactStepsAndUnknownACK(t *testing.T) {
	f := newHandoverAPI(t)
	ctx := context.Background()
	h := f.s.BrowserHandler()
	for _, method := range []string{"GET", "POST"} {
		if w := browserRequest(t, h, method, handoverBase, f.plan, nil, "", ""); w.Code != 401 {
			t.Fatal("anonymous", w.Code)
		}
	}
	if _, err := f.s.business.CreateRepresentative(ctx, "handover-rep", "Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.ProvisionRepresentativeLogin(ctx, "handover-rep", "handover-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	rep, csrf := browserLogin(t, f.s, "handover-rep", repPassword)
	for _, method := range []string{"GET", "POST"} {
		if w := browserRequest(t, h, method, handoverBase, f.plan, rep, csrf, "https://"+browserHost); w.Code != 403 {
			t.Fatal("representative", w.Code)
		}
	}
	// Representative creation changes the generation: re-stage both prepared
	// records is intentionally not allowed by the old review. Use a fresh fixture.
	f = newHandoverAPI(t)
	h = f.s.BrowserHandler()
	if w := browserRequest(t, h, "POST", handoverBase, f.plan, f.cookie, "bad", "https://"+browserHost); w.Code != 403 {
		t.Fatal("CSRF", w.Code)
	}
	if w := browserRequest(t, h, "POST", handoverBase, f.plan, f.cookie, f.csrf, "https://foreign.invalid"); w.Code != 403 {
		t.Fatal("origin", w.Code)
	}
	bad := f.plan
	bad.TargetInstallation = installID
	if w := f.post(t, handoverBase, bad); w.Code != 409 {
		t.Fatal("installation substitution", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := f.post(t, handoverBase, f.plan); w.Code != 201 {
			t.Fatal("create/replay", w.Code, w.Body.String())
		}
	}
	path := handoverBase + "/" + f.plan.ID
	apply := f.input(1, 22, "apply", f.plan.TargetStageID)
	if w := f.post(t, path+"/commands", apply); w.Code != 409 {
		t.Fatal("apply before source seal", w.Code)
	}
	drain := f.input(0, 13, "drain", f.plan.SourceStageID)
	for i := 0; i < 2; i++ {
		if w := f.post(t, path+"/commands", drain); w.Code != 202 {
			t.Fatal("drain/replay", w.Code, w.Body.String())
		}
	}
	if w := f.post(t, path+"/cancel", map[string]bool{"confirm_before_drain": true}); w.Code != 409 {
		t.Fatal("cancel uncertain drain", w.Code)
	}
	d := f.poll(t, 0, true)
	_, binding, _ := uc.CanonicalEpoch(f.epoch)
	final := uc.Report{Version: 1, EpochID: f.epoch.ID, BindingSHA: binding, Sequence: 2, Quiesced: true, Users: []uc.Usage{{ClientID: "handover-customer", Uplink: 123, Downlink: 456}}}
	final.SHA = handoverHash(final)
	seal, err := f.s.business.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: "old-de", InstallationID: installID}, final)
	if err != nil {
		t.Fatal(err)
	}
	u := f.plan.SourceUsage
	u.Settlement = seal
	o := f.p[0].Status.Observed
	o.ProcessReady, o.ActiveSequence, o.ActiveSHA, o.LeaseExpires = false, 0, "", 0
	o.Usage = &mx.UsageObservationStatus{Required: true, Settled: true}
	ack := handoverACK(f.tokens[0], d.Command, o, "drained", u, seal)
	if w := f.post(t, path+"/commands", apply); w.Code != 409 {
		t.Fatal("lost source ACK bypassed", w.Code)
	}
	if err = f.s.business.AcceptMeteredRouteReceipt(ctx, f.key, []byte(f.tokens[0]), ack); err != nil {
		t.Fatal(err)
	}
	if w := f.post(t, path+"/commands", apply); w.Code != 409 {
		t.Fatal("no post-ACK observation", w.Code)
	}
	f.p[0].Status.Serial, f.p[0].Status.Observed, f.p[0].Status.Usage = d.Command.Serial, o, &u
	f.poll(t, 0, false)
	for i := 0; i < 2; i++ {
		if w := f.post(t, path+"/commands", apply); w.Code != 202 {
			t.Fatal("apply/replay", w.Code, w.Body.String())
		}
	}
	w := browserRequest(t, h, "GET", path, nil, f.cookie, "", "")
	var audit business.MeteredHandoverAudit
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &audit) != nil || audit.DrainCommand == nil || audit.DrainCommand.Receipt == nil || audit.ApplyCommand == nil || audit.CanPublish || audit.Phase != "target_apply_pending" {
		t.Fatal("audit", w.Code, w.Body.String())
	}
	for _, private := range []string{"privateKey", "config_base64", "candidate", "payload_enc"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("private payload leaked", private)
		}
	}
	// Read history survives operator runtime disable; writes lose authority.
	f.s.meteredRouteNodes = nil
	if w = f.post(t, path+"/commands", apply); w.Code != 409 {
		t.Fatal("disabled startup scope", w.Code)
	}
	if w = browserRequest(t, f.s.BrowserHandler(), "GET", path, nil, f.cookie, "", ""); w.Code != 200 {
		t.Fatal("history lost with flags off", w.Code)
	}
	if w = browserRequest(t, f.s.AgentHandler(), "GET", path, nil, f.cookie, "", ""); w.Code == 200 {
		t.Fatal("Agent surface exposed owner history")
	}
}

func TestMeteredHandoverAPIExcludesLegacyReplacementRoles(t *testing.T) {
	f := newHandoverAPI(t)
	j, err := replacement.Open(filepath.Join(t.TempDir(), "ops", "journal.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.s.EnableReplacementLab(j); err != nil {
		t.Fatal(err)
	}
	if w := request(t, f.s.Handler(), "POST", "/api/v1/admin/replacements", ownerToken, testPlan()); w.Code != 409 {
		t.Fatal("legacy preflight took metered Nodes", w.Code)
	}
	// A journal present before startup, or changed outside HTTP, also fences
	// owner creation and every subsequent step in either role.
	p, err := routeplan.New("new-de", "old-de")
	if err != nil {
		t.Fatal(err)
	}
	if err = p.SetDirect(7612, 27613); err != nil {
		t.Fatal(err)
	}
	preview, err := p.Preview()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.Create(preview, installID); err != nil {
		t.Fatal(err)
	}
	if w := f.post(t, handoverBase, f.plan); w.Code != 409 {
		t.Fatal("reverse legacy role bypassed", w.Code)
	}
	f.s.meteredRouteNodes = nil
	if err = f.s.EnableMeteredRouteNodes([]business.MeteredRouteNode{{NodeID: "old-de", InstallationID: installID, BinarySHA: strings.Repeat("a", 64)}}); err == nil {
		t.Fatal("startup ignored legacy reservation")
	}
}

func TestMeteredHandoverAPISafeCancelAndOwnerBearerBoundary(t *testing.T) {
	f := newHandoverAPI(t)
	h := f.s.BrowserHandler()
	if w := request(t, h, "GET", handoverBase, ownerToken, nil); w.Code != 401 {
		t.Fatal("owner bearer substituted for browser session", w.Code)
	}
	if w := f.post(t, handoverBase, f.plan); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	path := handoverBase + "/" + f.plan.ID
	if w := f.post(t, path+"/cancel", map[string]bool{"confirm_before_drain": false}); w.Code != 422 {
		t.Fatal("unconfirmed cancel", w.Code)
	}
	for i := 0; i < 2; i++ {
		if w := f.post(t, path+"/cancel", map[string]bool{"confirm_before_drain": true}); w.Code != 200 {
			t.Fatal("cancel/replay", w.Code, w.Body.String())
		}
	}
	if w := f.post(t, path+"/commands", f.input(0, 13, "drain", f.plan.SourceStageID)); w.Code != 409 {
		t.Fatal("cancelled command executed", w.Code)
	}
	w := browserRequest(t, h, "GET", handoverBase, nil, f.cookie, "", "")
	var page business.MeteredHandoverPage
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Operations) != 1 || !page.Operations[0].Cancelled || page.NextCursor != "" {
		t.Fatal("cancel history", w.Code, w.Body.String())
	}
}
