package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func roundEndpoint() mx.ProtocolHealthEndpoint {
	return mx.ProtocolHealthEndpoint{Address: "11.0.0.1:443", ServerName: "verifier.example.com", CertificateSHA: strings.Repeat("a", 64)}
}

// Synthetic signed observations exercise the owner transaction boundary. The
// isolated pinned-core acceptance suite verifies actual protocol traffic.
func newHealthRoundAPI(t *testing.T) *handoverAPI {
	t.Helper()
	ctx := context.Background()
	f := newHandoverAPI(t, func(f *handoverAPI) {
		if err := f.s.EnableMeteredHealth(roundEndpoint()); err != nil {
			t.Fatal(err)
		}
		if err := f.s.EnableMeteredHealthRounds(); err != nil {
			t.Fatal(err)
		}
		for _, p := range f.p {
			if _, err := f.s.business.ProvisionMeteredProbe(ctx, f.key, p.NodeID, p.InstallationID, 7612); err != nil {
				t.Fatal(err)
			}
		}
	})
	if w := f.post(t, handoverBase, f.plan); w.Code != http.StatusCreated {
		t.Fatal("create", w.Code, w.Body.String())
	}
	path := handoverBase + "/" + f.plan.ID + "/commands"
	if w := f.post(t, path, f.input(0, 13, "drain", f.plan.SourceStageID)); w.Code != http.StatusAccepted {
		t.Fatal("drain", w.Code, w.Body.String())
	}
	d := f.poll(t, 0, true)
	_, binding, _ := uc.CanonicalEpoch(f.epoch)
	final := uc.Report{Version: 1, EpochID: f.epoch.ID, BindingSHA: binding, Sequence: 2, Quiesced: true}
	for _, b := range f.epoch.Bindings {
		final.Users = append(final.Users, uc.Usage{ClientID: b.ClientID})
	}
	final.SHA = handoverHash(final)
	seal, err := f.s.business.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: f.p[0].NodeID, InstallationID: f.p[0].InstallationID}, final)
	if err != nil {
		t.Fatal(err)
	}
	u := f.plan.SourceUsage
	u.Settlement = seal
	o := f.p[0].Status.Observed
	o.ProcessReady, o.ActiveSequence, o.ActiveSHA, o.LeaseExpires = false, 0, "", 0
	o.Usage = &mx.UsageObservationStatus{Required: true, Settled: true}
	if err = f.s.business.AcceptMeteredRouteReceipt(ctx, f.key, []byte(f.tokens[0]), handoverACK(f.tokens[0], d.Command, o, "drained", u, seal)); err != nil {
		t.Fatal(err)
	}
	f.p[0].Status.Serial, f.p[0].Status.Observed, f.p[0].Status.Usage = d.Command.Serial, o, &u
	f.poll(t, 0, false)
	if w := f.post(t, path, f.input(1, 22, "apply", f.plan.TargetStageID)); w.Code != http.StatusAccepted {
		t.Fatal("apply", w.Code, w.Body.String())
	}
	d = f.poll(t, 1, true)
	birth, err := mx.SignBirthReport(f.tokens[1], d.Command.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 12346, StartTicks: 78})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := f.s.business.MeteredRouteAuthority(ctx, f.key, []byte(f.tokens[1]), mx.MeteredAuthorityRequest{Command: d.Command, Birth: &birth})
	if err != nil {
		t.Fatal(err)
	}
	_, binding, _ = uc.CanonicalEpoch(epoch)
	zero := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 1}
	for _, b := range epoch.Bindings {
		zero.Users = append(zero.Users, uc.Usage{ClientID: b.ClientID})
	}
	zero.SHA = handoverHash(zero)
	if _, err = f.s.business.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: f.p[1].NodeID, InstallationID: f.p[1].InstallationID}, zero); err != nil {
		t.Fatal(err)
	}
	targetUsage := mx.MeteredCondition{EpochID: epoch.ID, EpochSHA: handoverHash(epoch), AdmissionSHA: strings.Repeat("e", 64)}
	c := d.Command
	o = mx.Snapshot{Phase: "ready_observed_admission", Highwater: c.RuntimeSequence, SavedSequence: c.RuntimeSequence, SavedSHA: c.Lease.ConfigSHA, ActiveSequence: c.RuntimeSequence, ActiveSHA: c.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: c.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	if err = f.s.business.AcceptMeteredRouteReceipt(ctx, f.key, []byte(f.tokens[1]), handoverACK(f.tokens[1], c, o, "applied", targetUsage, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	f.p[1].Status.Serial, f.p[1].Status.CurrentStageID, f.p[1].Status.StagedID, f.p[1].Status.Observed, f.p[1].Status.Usage = c.Serial, c.StageID, "", o, &targetUsage
	f.poll(t, 1, false)
	return f
}

func TestMeteredHealthRoundsRequireSeparateStartupOptIn(t *testing.T) {
	f := newHandoverAPI(t)
	if err := f.s.EnableMeteredHealthRounds(); err == nil || f.s.healthRoundsEnabled {
		t.Fatal("rounds enabled without the pinned transport")
	}
	if err := f.s.EnableMeteredHealth(roundEndpoint()); err != nil {
		t.Fatal(err)
	}
	path := handoverBase + "/" + f.plan.ID + "/health-rounds"
	body := map[string]any{"id": handoverTestID(250), "confirmed": true}
	if w := f.post(t, path, body); w.Code != http.StatusConflict {
		t.Fatal("legacy health enabled rounds implicitly", w.Code)
	}
	capability := func() map[string]any {
		w := browserRequest(t, f.s.BrowserHandler(), "GET", "/api/v1/workspace/metered-route-transactions", nil, f.cookie, "", "")
		var v map[string]any
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("capability", w.Code, w.Body.String())
		}
		return v
	}
	if v := capability(); v["health_enabled"] != true || v["health_rounds_enabled"] != false {
		t.Fatal("separate capability missing", v)
	}
	if err := f.s.EnableMeteredHealthRounds(); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableMeteredHealthRounds(); err == nil {
		t.Fatal("startup opt-in mutated twice")
	}
	if v := capability(); v["health_rounds_enabled"] != true {
		t.Fatal("round capability absent", v)
	}
}

func TestMeteredHealthRoundsOwnerAndInputBoundaries(t *testing.T) {
	f := newHandoverAPI(t)
	if err := f.s.EnableMeteredHealth(roundEndpoint()); err != nil {
		t.Fatal(err)
	}
	if err := f.s.EnableMeteredHealthRounds(); err != nil {
		t.Fatal(err)
	}
	h := f.s.BrowserHandler()
	path := handoverBase + "/" + f.plan.ID + "/health-rounds"
	body := map[string]any{"id": handoverTestID(250), "confirmed": true}
	if w := browserRequest(t, h, "POST", path, body, nil, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatal("anonymous", w.Code)
	}
	if w := request(t, h, "POST", path, ownerToken, body); w.Code != http.StatusUnauthorized {
		t.Fatal("owner bearer substituted for browser session", w.Code)
	}
	for _, headers := range [][2]string{{"", "https://" + browserHost}, {"bad", "https://" + browserHost}, {f.csrf, "https://foreign.invalid"}} {
		if w := browserRequest(t, h, "POST", path, body, f.cookie, headers[0], headers[1]); w.Code != http.StatusForbidden {
			t.Fatal("CSRF/origin", w.Code)
		}
	}
	if _, err := f.s.business.CreateRepresentative(context.Background(), "health-round-rep", "Rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.ProvisionRepresentativeLogin(context.Background(), "health-round-rep", "health-round-rep", repPassword); err != nil {
		t.Fatal(err)
	}
	rep, csrf := browserLogin(t, f.s, "health-round-rep", repPassword)
	if w := browserRequest(t, h, "POST", path, body, rep, csrf, "https://"+browserHost); w.Code != http.StatusForbidden {
		t.Fatal("representative", w.Code)
	}
	for _, in := range []map[string]any{{"id": handoverTestID(250)}, {"id": handoverTestID(250), "confirmed": false}, {"id": "not-an-event", "confirmed": true}, {"id": strings.Repeat("A", 32), "confirmed": true}} {
		if w := f.post(t, path, in); w.Code != http.StatusUnprocessableEntity {
			t.Fatal("invalid/unconfirmed request", w.Code, w.Body.String())
		}
	}
	for _, extra := range []string{"inbound_id", "inbound_ids", "members", "endpoint", "verifier_url", "expires_unix_nano", "node_id", "installation_id"} {
		in := map[string]any{"id": handoverTestID(250), "confirmed": true, extra: "caller-selected"}
		if w := f.post(t, path, in); w.Code != http.StatusUnprocessableEntity {
			t.Fatal("caller selected server scope", extra, w.Code)
		}
	}
	if w := f.post(t, handoverBase+"/invalid/health-rounds", body); w.Code != http.StatusUnprocessableEntity {
		t.Fatal("invalid handover ID", w.Code)
	}
	for _, method := range []string{"GET", "PUT", "PATCH", "DELETE"} {
		if w := browserRequest(t, h, method, path, body, f.cookie, f.csrf, "https://"+browserHost); w.Code != http.StatusMethodNotAllowed {
			t.Fatal("method boundary", method, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "https://"+browserHost+path, strings.NewReader(`{"id":"`+handoverTestID(250)+`","confirmed":true} {}`))
	r.AddCookie(f.cookie)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Origin", "https://"+browserHost)
	r.Host = browserHost
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatal("trailing JSON", w.Code)
	}
	r = httptest.NewRequest("POST", path, strings.NewReader(`{}`))
	r.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
	r.AddCookie(f.cookie)
	w = httptest.NewRecorder()
	f.s.AgentHandler().ServeHTTP(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatal("owner rounds exposed on Agent listener", w.Code)
	}
}

func TestMeteredHealthRoundOwnerRetryAuditAndNodeAllowlist(t *testing.T) {
	f := newHealthRoundAPI(t)
	path := handoverBase + "/" + f.plan.ID + "/health-rounds"
	body := map[string]any{"id": handoverTestID(250), "confirmed": true}
	// An enabled, newly applied handover must advertise an empty array so the
	// owner can verify the complete audit before requesting its first round.
	w := browserRequest(t, f.s.BrowserHandler(), "GET", handoverBase+"/"+f.plan.ID, nil, f.cookie, "", "")
	var initial map[string]json.RawMessage
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &initial) != nil || string(initial["health_rounds"]) != "[]" {
		t.Fatal("missing empty round audit", w.Code, w.Body.String())
	}
	post := func() (business.MeteredHealthRound, string) {
		w := f.post(t, path, body)
		var v business.MeteredHealthRound
		if w.Code != http.StatusAccepted || json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("owner round", w.Code, w.Body.String())
		}
		return v, w.Body.String()
	}
	first, raw := post() // The caller can lose this reply after the durable commit.
	again, _ := post()
	if !reflect.DeepEqual(first, again) || first.ID != body["id"] || first.HandoverID != f.plan.ID || first.NodeID != f.plan.TargetNode || first.InstallationID != f.plan.TargetInstallation || first.EndpointSHA != roundEndpoint().SHA() || first.State != "pending" || first.Current || first.Expires-first.Created > int64(8*time.Second) || first.Expires > f.p[1].Status.Observed.LeaseExpires {
		t.Fatal("retry replaced or broadened the bounded round", first, again)
	}
	if len(first.Members) != 1 || first.Members[0].InboundID != 7612 || first.Members[0].Port != 27613 || first.Members[0].State != "pending" {
		t.Fatal("server did not derive all destination members", first.Members)
	}
	for _, private := range []string{"privateKey", "config_base64", "candidate", "payload_enc", "credential", "system_probe", "signature", "CABundle", "verifier.example.com", f.tokens[1]} {
		if strings.Contains(raw, private) {
			t.Fatal("private health payload leaked", private)
		}
	}
	getAudit := func() business.MeteredHandoverAudit {
		w := browserRequest(t, f.s.BrowserHandler(), "GET", handoverBase+"/"+f.plan.ID, nil, f.cookie, "", "")
		var audit business.MeteredHandoverAudit
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &audit) != nil {
			t.Fatal("audit", w.Code, w.Body.String())
		}
		return audit
	}
	if audit := getAudit(); len(audit.HealthRounds) != 1 || audit.HealthRounds[0].ID != first.ID || audit.HealthRounds[0].Current || audit.CanPublish {
		t.Fatal("round audit or false publication", audit)
	}
	if w := f.post(t, path, map[string]any{"id": handoverTestID(251), "confirmed": true}); w.Code != http.StatusConflict {
		t.Fatal("parallel owner round", w.Code, w.Body.String())
	}
	// Even an exact historical retry must cross the current owner allowlist.
	f.s.healthRoundsEnabled = false
	if w := f.post(t, path, body); w.Code != http.StatusConflict {
		t.Fatal("disabled round opt-in", w.Code)
	}
	f.s.healthRoundsEnabled = true
	for _, node := range []string{f.plan.SourceNode, f.plan.TargetNode} {
		install := f.s.meteredRouteNodes[node]
		f.s.meteredRouteNodes[node] = handoverTestID(999)
		if w := f.post(t, path, body); w.Code != http.StatusConflict {
			t.Fatal("installation outside operator allowlist", node, w.Code)
		}
		f.s.meteredRouteNodes[node] = install
	}
	f.s.meteredRouteNodes = nil
	if w := f.post(t, path, body); w.Code != http.StatusConflict {
		t.Fatal("disabled startup allowlist", w.Code)
	}
	if audit := getAudit(); len(audit.HealthRounds) != 1 || audit.CanPublish {
		t.Fatal("safe history unavailable after operator disable")
	}
}
