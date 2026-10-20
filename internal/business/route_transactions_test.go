package business

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

var routeTestKey = bytes.Repeat([]byte{0x56}, 32)
var routeTestToken = bytes.Repeat([]byte{0x67}, 64)

const routeTestInstall = "11111111111111111111111111111111"
const routeTestInstance = "22222222222222222222222222222222"

func routeTestID(n int) string { return fmt.Sprintf("%032x", n) }
func routeQueueFixture(t *testing.T) (*Store, string, mx.TransactionPoll) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "private", "business.sqlite")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	if _, e = s.CreateInbound(ctx, routeTestKey, InboundInput{ID: 6123, Name: "Loopback queue QA", Transport: "tcp", RealityTarget: "qa.invalid:443", ServerNames: []string{"qa.example.org"}, ShortIDs: []string{"aa11"}}); e != nil {
		t.Fatal(e)
	}
	if e = s.PutInboundTarget(ctx, InboundTarget{InboundID: 6123, NodeID: "route-qa", InstallationID: routeTestInstall, ListenIP: "127.0.0.1", DirectPort: 26123}); e != nil {
		t.Fatal(e)
	}
	routeSaveDraft(t, s, 0, 26123)
	if e = s.ClaimRouteChannel(ctx, "route-qa", routeTestInstall, strings.Repeat("a", 64)); e != nil {
		t.Fatal(e)
	}
	p := mx.TransactionPoll{NodeID: "route-qa", InstallationID: routeTestInstall, BinarySHA: strings.Repeat("a", 64), Status: mx.TransactionStatus{Instance: routeTestInstance, Observed: mx.Snapshot{Phase: "awaiting_fresh_lease"}}}
	routePoll(t, s, &p, false)
	return s, path, p
}
func routeSaveDraft(t *testing.T, s *Store, revision uint64, port uint16) {
	t.Helper()
	_, e := s.SaveNativeRouteDraft(context.Background(), routeTestKey, NativeRouteDraftInput{EventID: fmt.Sprintf("route-qa-draft-event-%04d", revision), InboundID: 6123, NodeID: "route-qa", InstallationID: routeTestInstall, ExpectedInboundRevision: 1, ExpectedDraftRevision: revision, DirectPort: port, Reason: "Disposable loopback route review"})
	if e != nil {
		t.Fatal(e)
	}
}
func routePoll(t *testing.T, s *Store, p *mx.TransactionPoll, want bool) mx.TransactionDelivery {
	t.Helper()
	p.At = time.Now().UnixNano()
	*p = mx.SignTransactionPoll(routeTestToken, *p)
	d, found, e := s.PollRouteCommand(context.Background(), routeTestKey, routeTestToken, *p)
	if e != nil || found != want {
		t.Fatalf("poll found=%v want=%v error=%v", found, want, e)
	}
	return d
}
func routeRequest(p mx.TransactionPoll, n int, action, stage string) RouteCommandRequest {
	in := RouteCommandRequest{ID: routeTestID(n), Action: action, StageID: stage, RuntimeInstance: p.Status.Instance, Expected: mx.Condition(p.Status.Observed)}
	if action == "stage" {
		in.Drafts = []RouteDraftRef{{InboundID: 6123, InboundRevision: 1, DraftRevision: 1}}
	}
	return in
}
func routeTestSignAck(c mx.TransactionCommand, status mx.Snapshot, outcome string) mx.TransactionReceipt {
	raw, _ := json.Marshal(c)
	h := sha256.Sum256(raw)
	a := mx.TransactionReceipt{Version: 1, CommandID: c.ID, CommandSHA: hex.EncodeToString(h[:]), Serial: c.Serial, Action: c.Action, StageID: c.StageID, NodeID: c.NodeID, InstallationID: c.InstallationID, RuntimeInstance: c.RuntimeInstance, Outcome: outcome, ObservedAt: time.Now().UnixNano(), Observed: status}
	raw, _ = json.Marshal(a)
	mac := hmac.New(sha256.New, routeTestToken)
	mac.Write([]byte("DARK-PRO-ROUTE-RECEIPT-v1\x00"))
	mac.Write(raw)
	a.Signature = hex.EncodeToString(mac.Sum(nil))
	return a
}
func routeStageAck(t *testing.T, s *Store, p *mx.TransactionPoll, d mx.TransactionDelivery) {
	t.Helper()
	a := routeTestSignAck(d.Command, p.Status.Observed, "staged")
	if e := s.AcceptRouteReceipt(context.Background(), routeTestKey, routeTestToken, a); e != nil {
		t.Fatal(e)
	}
	p.Status.Serial = d.Command.Serial
	p.Status.StagedID = d.Command.StageID
	routePoll(t, s, p, false)
}
func TestRouteQueueDurableFrozenDeliveryExactACKAndReplay(t *testing.T) {
	s, path, p := routeQueueFixture(t)
	ctx := context.Background()
	request := routeRequest(p, 1, "stage", "")
	first, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, request)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, request); e != nil {
		t.Fatal("idempotent queue", e)
	}
	bad := request
	bad.Expected.Highwater++
	if _, e = s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, bad); e != ErrConflict {
		t.Fatal("event rebound", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	d := routePoll(t, s, &p, true)
	again := routePoll(t, s, &p, true)
	a, _ := json.Marshal(d)
	b, _ := json.Marshal(again)
	if !bytes.Equal(a, b) || d.Command.ID != first.ID || d.Command.Signature == "" || d.Command.RuntimeInstance != p.Status.Instance {
		t.Fatal("delivery changed across lost response")
	}
	config, e := base64.StdEncoding.DecodeString(d.ConfigBase64)
	if e != nil || !bytes.Contains(config, []byte("privateKey")) {
		t.Fatal("missing actual private candidate")
	}
	var encrypted []byte
	if e = s.db.QueryRow("SELECT payload_enc FROM native_route_commands WHERE id=?", first.ID).Scan(&encrypted); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(encrypted, []byte("privateKey")) || bytes.Contains(encrypted, []byte(d.ConfigBase64)) {
		t.Fatal("plaintext queue")
	}
	ack := routeTestSignAck(d.Command, p.Status.Observed, "staged")
	tampered := ack
	tampered.Observed.Highwater++
	if e = s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, tampered); e == nil {
		t.Fatal("tampered full ACK accepted")
	}
	if e = s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, ack); e != nil {
		t.Fatal(e)
	}
	if e = s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, ack); e != nil {
		t.Fatal("lost commit response replay", e)
	}
	other := routeTestSignAck(d.Command, p.Status.Observed, "staged")
	if e = s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, other); e != ErrConflict {
		t.Fatal("different valid receipt overwrote immutable ACK", e)
	}
	p.Status.Serial = d.Command.Serial
	p.Status.StagedID = d.Command.StageID
	routePoll(t, s, &p, false)
	apply := routeRequest(p, 2, "apply", d.Command.StageID)
	if _, e = s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, apply); e != nil {
		t.Fatal(e)
	}
	ad := routePoll(t, s, &p, true)
	if ad.ConfigBase64 != "" || ad.Command.Lease.ConfigSHA != d.Candidate.SHA256 {
		t.Fatal("Apply lost frozen candidate")
	}
	observed := mx.Snapshot{Phase: "ready", Highwater: ad.Command.RuntimeSequence, SavedSequence: ad.Command.RuntimeSequence, SavedSHA: ad.Command.Lease.ConfigSHA, ActiveSequence: ad.Command.RuntimeSequence, ActiveSHA: ad.Command.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: ad.Command.Lease.Expires}
	aa := routeTestSignAck(ad.Command, observed, "applied")
	if e = s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, aa); e != nil {
		t.Fatal(e)
	}
	view, e := s.RouteChannel(ctx, routeTestKey, p.NodeID)
	if e != nil || view.Commands[0].State != "acknowledged" || view.Status.Observed.ProcessReady {
		t.Fatal("historical ACK became fresh observation", e)
	}
	raw, _ := json.Marshal(view)
	if bytes.Contains(raw, []byte("privateKey")) || bytes.Contains(raw, []byte("config_base64")) || bytes.Contains(raw, []byte("payload_enc")) {
		t.Fatal("owner view leaked config")
	}
	// A new instance's fresh stopped observation must win over historical ACKs.
	p.Status.Instance = strings.Repeat("3", 32)
	p.Status.Serial = ad.Command.Serial
	p.Status.Observed = observed
	p.Status.Observed.ProcessReady = false
	p.Status.Observed.ActiveSequence = 0
	p.Status.Observed.ActiveSHA = ""
	p.Status.Observed.Phase = "awaiting_fresh_lease"
	routePoll(t, s, &p, false)
	if e = s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, aa); e != nil {
		t.Fatal(e)
	}
	view, e = s.RouteChannel(ctx, routeTestKey, p.NodeID)
	if e != nil || view.Status.Observed.ProcessReady || view.Status.Instance == ack.RuntimeInstance {
		t.Fatal("replay invented post-restart liveness")
	}
}
func TestRouteQueueStaleDraftBlockedAndAbandonedReceiptStaysHistorical(t *testing.T) {
	s, _, p := routeQueueFixture(t)
	ctx := context.Background()
	request := routeRequest(p, 11, "stage", "")
	if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, request); e != nil {
		t.Fatal(e)
	}
	d := routePoll(t, s, &p, true)
	routeSaveDraft(t, s, 1, 26124)
	p.At = time.Now().UnixNano()
	p = mx.SignTransactionPoll(routeTestToken, p)
	if _, _, e := s.PollRouteCommand(ctx, routeTestKey, routeTestToken, p); e != ErrConflict {
		t.Fatal("stale draft was delivered", e)
	}
	view, e := s.RouteChannel(ctx, routeTestKey, p.NodeID)
	if e != nil || view.ReportedAt != p.At {
		t.Fatal("recovery observation rolled back", e)
	}
	if e = s.AbandonRouteCommand(ctx, p.NodeID, request.ID); e != nil {
		t.Fatal(e)
	}
	ack := routeTestSignAck(d.Command, p.Status.Observed, "staged")
	if e = s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, ack); e != nil {
		t.Fatal(e)
	}
	view, e = s.RouteChannel(ctx, routeTestKey, p.NodeID)
	if e != nil || view.Commands[0].State != "abandoned_unknown" || view.Commands[0].Receipt == nil {
		t.Fatal("late ACK revived abandoned action", e)
	}
	fresh := routeRequest(p, 12, "stage", "")
	fresh.Drafts[0].DraftRevision = 2
	if _, e = s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, fresh); e != nil {
		t.Fatal("cannot explicitly recover", e)
	}
}
func TestRouteQueueRejectsWrongScopeConcurrentCommandsAndIncompleteNode(t *testing.T) {
	s, _, p := routeQueueFixture(t)
	ctx := context.Background()
	bad := p
	bad.InstallationID = strings.Repeat("a", 32)
	bad.At = time.Now().UnixNano()
	bad = mx.SignTransactionPoll(routeTestToken, bad)
	if _, _, e := s.PollRouteCommand(ctx, routeTestKey, routeTestToken, bad); e == nil {
		t.Fatal("wrong installation accepted")
	}
	bad = p
	bad.At = time.Now().Add(-20 * time.Second).UnixNano()
	bad = mx.SignTransactionPoll(routeTestToken, bad)
	if _, _, e := s.PollRouteCommand(ctx, routeTestKey, routeTestToken, bad); e == nil {
		t.Fatal("stale observation accepted")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for n := 21; n <= 22; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, routeRequest(p, n, "stage", ""))
			results <- e
		}(n)
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("multiple concurrent commands queued", successes)
	}
	v, _ := s.RouteChannel(ctx, routeTestKey, p.NodeID)
	if e := s.AbandonRouteCommand(ctx, p.NodeID, v.Commands[0].ID); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateInbound(ctx, routeTestKey, InboundInput{ID: 6124, Name: "Other loopback route", Transport: "tcp", RealityTarget: "qa.invalid:443", ServerNames: []string{"qa.example.org"}, ShortIDs: []string{"aa11"}}); e != nil {
		t.Fatal(e)
	}
	if e := s.PutInboundTarget(ctx, InboundTarget{InboundID: 6124, NodeID: p.NodeID, InstallationID: p.InstallationID, ListenIP: "127.0.0.1", DirectPort: 26125}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, routeRequest(p, 23, "stage", "")); e == nil {
		t.Fatal("partial whole-Node snapshot accepted")
	}
}
func TestRouteQueuePermanentOwnershipForbidsCustomersAndOrdinaryDelivery(t *testing.T) {
	s, path, p := routeQueueFixture(t)
	ctx := context.Background()
	c, e := s.CreateClient(ctx, NewClient{EventID: "route-customer-test-001", ClientID: "qa-bound", RepresentativeID: "owner", Name: "Must not enter route executor", DurationDays: 1, VolumeBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.BindInboundClients(ctx, 6123, []string{c.ID}, true); e == nil {
		t.Fatal("customer bound into unmetered channel")
	}
	cfg, e := s.BuildNodeConfig(ctx, routeTestKey, p.NodeID, p.InstallationID)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UnixNano()
	lease, e := mx.SignLease(routeTestToken, mx.Lease{NodeID: p.NodeID, InstallationID: p.InstallationID, Sequence: cfg.Sequence, ConfigSHA: cfg.ConfigSHA, ManifestSHA: cfg.ManifestSHA, Issued: now, Expires: now + int64(20*time.Second)})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.RecordNativeDelivery(ctx, cfg, lease); e == nil {
		t.Fatal("ordinary runtime took over transaction Node")
	}
	if e = s.PutInboundTarget(ctx, InboundTarget{InboundID: 6123, NodeID: p.NodeID, InstallationID: p.InstallationID, ListenIP: "198.51.100.2", DirectPort: 26123}); e == nil {
		t.Fatal("public target accepted")
	}
	s.Close()
	reopened, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	if owned, e := reopened.RouteChannelOwned(ctx, p.NodeID); e != nil || !owned {
		t.Fatal("restart lost exclusive claim")
	}
	if e = reopened.RecordNativeDelivery(ctx, cfg, lease); e == nil {
		t.Fatal("restart bypassed claim")
	}
}
func TestRouteQueueReceiptCommitFailureKeepsExactPendingCommand(t *testing.T) {
	s, _, p := routeQueueFixture(t)
	ctx := context.Background()
	request := routeRequest(p, 31, "stage", "")
	if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, request); e != nil {
		t.Fatal(e)
	}
	d := routePoll(t, s, &p, true)
	ack := routeTestSignAck(d.Command, p.Status.Observed, "staged")
	if _, e := s.db.Exec(`CREATE TRIGGER route_test_storage_failure BEFORE UPDATE OF receipt ON native_route_commands BEGIN SELECT RAISE(ABORT,'fixture full disk'); END`); e != nil {
		t.Fatal(e)
	}
	if e := s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, ack); e == nil {
		t.Fatal("receipt reported committed on storage failure")
	}
	again := routePoll(t, s, &p, true)
	if again.Command != d.Command {
		t.Fatal("storage failure changed command")
	}
	if _, e := s.db.Exec("DROP TRIGGER route_test_storage_failure"); e != nil {
		t.Fatal(e)
	}
	if e := s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, ack); e != nil {
		t.Fatal(e)
	}
}

func TestRouteQueueInterruptedRecoveryUsesTransactionStageIdentity(t *testing.T) {
	s, _, p := routeQueueFixture(t)
	ctx := context.Background()
	// A is the last acknowledged Stage. After a later ambiguous Apply and
	// explicit restaging, the supervisor's previous SHA may differ from the
	// transaction journal's last acknowledged rollback Stage.
	req := routeRequest(p, 41, "stage", "")
	if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, req); e != nil {
		t.Fatal(e)
	}
	d := routePoll(t, s, &p, true)
	routeStageAck(t, s, &p, d)
	p.Status.Serial = 3
	p.Status.Observed = mx.Snapshot{Phase: "awaiting_fresh_lease", Highwater: 2, SavedSequence: 2, SavedSHA: strings.Repeat("b", 64)}
	p.Status.CurrentStageID = req.ID
	p.Status.InterruptedCommandSHA = strings.Repeat("d", 64)
	p.Status.Instance = strings.Repeat("4", 32)
	routePoll(t, s, &p, false)
	recover := routeRequest(p, 42, "stage", "")
	if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, recover); e != nil {
		t.Fatal(e)
	}
	recovery := routePoll(t, s, &p, true)
	if recovery.Command.InterruptedCommandSHA != p.Status.InterruptedCommandSHA || recovery.Command.RuntimeInstance != p.Status.Instance || recovery.Command.Serial != 4 || recovery.Command.RuntimeSequence != 3 {
		t.Fatal("recovery lost exact interrupted intent/instance binding")
	}
	routeStageAck(t, s, &p, recovery)
	p.Status.InterruptedCommandSHA = ""
	p.Status.StagedID = ""
	p.Status.CurrentStageID = recover.ID
	p.Status.PreviousStageID = req.ID
	p.Status.Serial = 5
	p.Status.Observed = mx.Snapshot{Phase: "ready", Highwater: 3, SavedSequence: 3, SavedSHA: strings.Repeat("b", 64), ActiveSequence: 3, ActiveSHA: strings.Repeat("b", 64), PreviousSHA: strings.Repeat("c", 64), ProcessReady: true, LeaseExpires: time.Now().Add(20 * time.Second).UnixNano()}
	routePoll(t, s, &p, false)
	rollback := routeRequest(p, 43, "rollback", req.ID)
	if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, rollback); e != nil {
		t.Fatal("last acknowledged transaction Stage incorrectly replaced with supervisor SHA", e)
	}
	rb := routePoll(t, s, &p, true)
	if rb.Command.StageID != req.ID || rb.Command.RuntimeSequence != 4 || rb.Command.Lease.ConfigSHA != d.Candidate.SHA256 {
		t.Fatal("wrong explicit rollback authority")
	}
}
