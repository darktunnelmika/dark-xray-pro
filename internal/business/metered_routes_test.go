package business

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"strings"
	"testing"
	"time"
)

func meteredQueueFixture(t *testing.T) (*Store, string, []byte, Client, mx.TransactionPoll) {
	t.Helper()
	s, path, key, client, cfg := usageSetup(t)
	_, e := s.SaveNativeRouteDraft(context.Background(), key, NativeRouteDraftInput{EventID: "metered-command-draft-0001", InboundID: 10, NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, ExpectedInboundRevision: 1, DirectPort: 19589, Reason: "Disposable metered command test"})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ClaimMeteredRouteChannel(context.Background(), cfg.NodeID, cfg.InstallationID, strings.Repeat("a", 64)); e != nil {
		t.Fatal(e)
	}
	p := mx.TransactionPoll{NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, BinarySHA: strings.Repeat("a", 64), Status: mx.TransactionStatus{Instance: routeTestInstance, Usage: &mx.MeteredCondition{}, Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}}
	meteredQueuePoll(t, s, key, &p, false)
	return s, path, key, client, p
}
func meteredQueuePoll(t *testing.T, s *Store, key []byte, p *mx.TransactionPoll, want bool) mx.TransactionDelivery {
	t.Helper()
	p.At = time.Now().UnixNano()
	*p = mx.SignMeteredTransactionPoll(routeTestToken, *p)
	d, found, e := s.PollMeteredRouteCommand(context.Background(), key, routeTestToken, *p)
	if e != nil || found != want {
		t.Fatalf("metered poll found=%v want=%v err=%v", found, want, e)
	}
	return d
}
func meteredQueueRequest(p mx.TransactionPoll, n int, action, stage string) MeteredRouteCommandRequest {
	in := MeteredRouteCommandRequest{ID: routeTestID(n), Action: action, StageID: stage, RuntimeInstance: p.Status.Instance, Expected: mx.Condition(p.Status.Observed), Usage: p.Status.Usage}
	if action == "stage" {
		in.Drafts = []RouteDraftRef{{InboundID: 10, InboundRevision: 1, DraftRevision: 1}}
	}
	return in
}
func meteredQueueAck(c mx.TransactionCommand, o mx.Snapshot, outcome string, u mx.MeteredCondition, prior uc.Receipt) mx.TransactionReceipt {
	a := routeTestSignAck(c, o, outcome)
	a.Version = 2
	a.Metered = &mx.MeteredEvidence{After: u, PriorSettlement: prior}
	a.Signature = ""
	raw, _ := json.Marshal(a)
	mac := hmac.New(sha256.New, routeTestToken)
	mac.Write([]byte("DARK-PRO-ROUTE-RECEIPT-v1\x00"))
	mac.Write(raw)
	a.Signature = hex.EncodeToString(mac.Sum(nil))
	return a
}
func meteredQueuedApply(t *testing.T, s *Store, key []byte, p *mx.TransactionPoll) mx.TransactionDelivery {
	t.Helper()
	ctx := context.Background()
	in := meteredQueueRequest(*p, 1, "stage", "")
	if _, e := s.QueueMeteredRouteCommand(ctx, key, p.NodeID, in); e != nil {
		t.Fatal(e)
	}
	d := meteredQueuePoll(t, s, key, p, true)
	if _, e := s.MeteredRouteAuthority(ctx, key, routeTestToken, mx.MeteredAuthorityRequest{Command: d.Command}); e != nil {
		t.Fatal("stage authority", e)
	}
	ack := meteredQueueAck(d.Command, p.Status.Observed, "staged", *p.Status.Usage, uc.Receipt{})
	if e := s.AcceptMeteredRouteReceipt(ctx, key, routeTestToken, ack); e != nil {
		t.Fatal("stage ack", e)
	}
	p.Status.Serial = 1
	p.Status.StagedID = d.Command.StageID
	meteredQueuePoll(t, s, key, p, false)
	if _, e := s.QueueMeteredRouteCommand(ctx, key, p.NodeID, meteredQueueRequest(*p, 2, "apply", d.Command.StageID)); e != nil {
		t.Fatal(e)
	}
	return meteredQueuePoll(t, s, key, p, true)
}
func TestMeteredRouteQueuePermanentIsolationAndExactReview(t *testing.T) {
	ctx := context.Background()
	s, path, key, _, p := meteredQueueFixture(t)
	if e := s.ClaimRouteChannel(ctx, p.NodeID, p.InstallationID, p.BinarySHA); e == nil {
		t.Fatal("downgraded metered ownership")
	}
	if e := s.ClaimMeteredRouteChannel(ctx, p.NodeID, strings.Repeat("b", 32), p.BinarySHA); e == nil {
		t.Fatal("reassigned installation")
	}
	if e := s.PutInboundTarget(ctx, InboundTarget{InboundID: 10, NodeID: p.NodeID, InstallationID: p.InstallationID, ListenIP: "0.0.0.0", DirectPort: 19589}); e == nil {
		t.Fatal("non-loopback target")
	}
	old := mx.SignTransactionPoll(routeTestToken, p)
	if _, _, e := s.PollMeteredRouteCommand(ctx, key, routeTestToken, old); e == nil {
		t.Fatal("v1 poll accepted")
	}
	if mx.VerifyTransactionPoll(routeTestToken, p) {
		t.Fatal("v2 poll downgraded")
	}
	in := meteredQueueRequest(p, 1, "stage", "")
	bad := in
	bad.Usage = nil
	if _, e := s.QueueMeteredRouteCommand(ctx, key, p.NodeID, bad); e == nil {
		t.Fatal("missing usage review")
	}
	v, e := s.QueueMeteredRouteCommand(ctx, key, p.NodeID, in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := s.QueueMeteredRouteCommand(ctx, key, p.NodeID, in)
	if e != nil || again.ID != v.ID {
		t.Fatal("idempotency", e)
	}
	bad = in
	u := *in.Usage
	u.AdmissionSHA = strings.Repeat("e", 64)
	bad.Usage = &u
	if _, e = s.QueueMeteredRouteCommand(ctx, key, p.NodeID, bad); e == nil {
		t.Fatal("same ID changed review")
	}
	d := meteredQueuePoll(t, s, key, &p, true)
	if d.Command.Version != 2 || d.Command.Usage == nil || d.ConfigBase64 == "" {
		t.Fatal("not distinct v2")
	}
	var encrypted []byte
	if e = s.db.QueryRow("SELECT payload_enc FROM native_metered_route_commands WHERE id=?", v.ID).Scan(&encrypted); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(encrypted, []byte("privateKey")) || bytes.Contains(encrypted, []byte("config_base64")) {
		t.Fatal("private queue plaintext")
	}
	if _, e = s.MeteredRouteAuthority(ctx, key, routeTestToken, mx.MeteredAuthorityRequest{Command: d.Command}); e != nil {
		t.Fatal(e)
	}
	forged := d.Command
	forged.PayloadSHA = strings.Repeat("c", 64)
	forged, _ = mx.SignTransactionCommand(routeTestToken, forged)
	if _, e = s.MeteredRouteAuthority(ctx, key, routeTestToken, mx.MeteredAuthorityRequest{Command: forged}); e == nil {
		t.Fatal("Agent forged persisted command")
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if owned, e := s.RouteChannelOwned(ctx, p.NodeID); e != nil || !owned {
		t.Fatal("ownership lost without flags", e)
	}
	if e = s.ClaimRouteChannel(ctx, p.NodeID, p.InstallationID, p.BinarySHA); e == nil {
		t.Fatal("restart downgrade")
	}
}
func TestMeteredRouteBirthIsAtomicCommandBoundAndReceiptNeedsLedger(t *testing.T) {
	ctx := context.Background()
	s, _, key, client, p := meteredQueueFixture(t)
	d := meteredQueuedApply(t, s, key, &p)
	c := d.Command
	b, e := mx.SignBirthReport(string(routeTestToken), c.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 12345, StartTicks: 77})
	if e != nil {
		t.Fatal(e)
	}
	in := mx.MeteredAuthorityRequest{Command: c, Birth: &b}
	epoch, e := s.MeteredRouteAuthority(ctx, key, routeTestToken, in)
	if e != nil {
		t.Fatal("register", e)
	}
	if len(epoch.Bindings) != 1 || epoch.Bindings[0].ClientID != client.ID || !epoch.Bindings[0].Billable {
		t.Fatal("untrusted billing map")
	}
	same, e := s.MeteredRouteAuthority(ctx, key, routeTestToken, in)
	if e != nil || meteredHash(same) != meteredHash(epoch) {
		t.Fatal("lost registration response", e)
	}
	changed, _ := mx.SignBirthReport(string(routeTestToken), c.Lease, uc.ProcessToken{BootID: b.KernelBootID, PID: b.PID + 1, StartTicks: 78})
	in.Birth = &changed
	if _, e = s.MeteredRouteAuthority(ctx, key, routeTestToken, in); e == nil {
		t.Fatal("second birth for one command")
	}
	u := mx.MeteredCondition{EpochID: epoch.ID, EpochSHA: meteredHash(epoch), AdmissionSHA: strings.Repeat("d", 64)}
	o := mx.Snapshot{Phase: "ready_observed_admission", Highwater: c.RuntimeSequence, SavedSequence: c.RuntimeSequence, SavedSHA: c.Lease.ConfigSHA, ActiveSequence: c.RuntimeSequence, ActiveSHA: c.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: c.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	ack := meteredQueueAck(c, o, "applied", u, uc.Receipt{})
	if !mx.VerifyMeteredTransactionReceipt(routeTestToken, c, ack) {
		t.Fatal("fixture invalid")
	}
	if e = s.AcceptMeteredRouteReceipt(ctx, key, routeTestToken, ack); e == nil {
		t.Fatal("Agent claimed zero receipt missing from HUB")
	}
	_, binding, _ := uc.CanonicalEpoch(epoch)
	report := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 1, Users: []uc.Usage{{ClientID: client.ID}}}
	report.SHA = meteredHash(report)
	if _, e = s.usage.Accept(ctx, usageledger.Principal{NodeID: p.NodeID, InstallationID: p.InstallationID}, report); e != nil {
		t.Fatal("initial report", e)
	}
	if e = s.AcceptMeteredRouteReceipt(ctx, key, routeTestToken, ack); e != nil {
		t.Fatal("verified ack", e)
	}
	if e = s.AcceptMeteredRouteReceipt(ctx, key, routeTestToken, ack); e != nil {
		t.Fatal("exact ack replay", e)
	}
	for _, q := range []string{"DELETE FROM native_metered_route_births", "UPDATE native_metered_route_births SET epoch_json='{}'", "DELETE FROM native_metered_route_channels"} {
		if _, e = s.db.Exec(q); e == nil {
			t.Fatal("audit mutable")
		}
	}
}
func TestMeteredRouteAuthorityRechecksRevocationAndQueuedSnapshot(t *testing.T) {
	ctx := context.Background()
	s, _, key, client, p := meteredQueueFixture(t)
	d := meteredQueuedApply(t, s, key, &p)
	if _, e := s.SetClientState(ctx, SetClientState{EventID: "metered-route-revoke-after-delivery", ClientID: client.ID, Action: "disable", Reason: "Disposable revocation race"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.MeteredRouteAuthority(ctx, key, routeTestToken, mx.MeteredAuthorityRequest{Command: d.Command}); e == nil {
		t.Fatal("delivered command bypassed current eligibility")
	}
	// Exact delivery remains available for historical ACK replay, but cannot
	// grant fresh Check/Register authority after revocation.
	again := meteredQueuePoll(t, s, key, &p, true)
	if meteredHash(again.Command) != meteredHash(d.Command) {
		t.Fatal("delivery mutated")
	}
}
