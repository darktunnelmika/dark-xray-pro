package business

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func controlView(t *testing.T, s *Store) RouteControlView {
	t.Helper()
	v, e := s.RouteControlChannel(context.Background(), routeTestKey, "route-qa")
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func TestRouteQueueControlsReviewEveryTargetWithoutWriting(t *testing.T) {
	s, _, p := routeQueueFixture(t)
	ctx := context.Background()
	v := controlView(t, s)
	if !v.DraftsValid || !v.Controls.Stage || len(v.Review) != 1 || v.Review[0].DraftRevision != 1 || v.Review[0].DirectPort != 26123 || v.ServerTimeMillis == 0 {
		t.Fatalf("incomplete review: %+v", v)
	}
	if _, e := s.CreateInbound(ctx, routeTestKey, InboundInput{ID: 6124, Name: "Second loopback Inbound", Transport: "tcp", RealityTarget: "qa.invalid:443", ServerNames: []string{"qa.example.org"}, ShortIDs: []string{"aa12"}}); e != nil {
		t.Fatal(e)
	}
	if e := s.PutInboundTarget(ctx, InboundTarget{InboundID: 6124, NodeID: p.NodeID, InstallationID: p.InstallationID, ListenIP: "127.0.0.1", DirectPort: 26124}); e != nil {
		t.Fatal(e)
	}
	v = controlView(t, s)
	if v.DraftsValid || v.Controls.Stage || len(v.Review) != 2 || v.Review[1].DraftRevision != 0 {
		t.Fatal("incomplete Node review was enabled")
	}
	if _, e := s.SaveNativeRouteDraft(ctx, routeTestKey, NativeRouteDraftInput{EventID: "second-inbound-draft", InboundID: 6124, NodeID: p.NodeID, InstallationID: p.InstallationID, ExpectedInboundRevision: 1, DirectPort: 26124, Reason: "Complete Node review"}); e != nil {
		t.Fatal(e)
	}
	v = controlView(t, s)
	if !v.DraftsValid || !v.Controls.Stage || len(v.Commands) != 0 || v.Review[1].DraftRevision != 1 || v.Review[1].DirectPort != 26124 {
		t.Fatal("complete review unavailable or GET created a command")
	}
	// Source edits invalidate draft review even while the Agent's report is fresh.
	if _, e := s.db.Exec("UPDATE native_inbounds SET revision=revision+1 WHERE id=6124"); e != nil {
		t.Fatal(e)
	}
	if v = controlView(t, s); v.DraftsValid || v.Controls.Stage || v.Review[1].InboundRevision != 2 {
		t.Fatal("outdated draft was eligible")
	}
	raw, _ := json.Marshal(v)
	for _, secret := range []string{"privateKey", "profile_enc", "config_base64", "payload_enc"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private field in browser review", secret)
		}
	}
	if _, e := s.db.Exec("UPDATE native_route_channels SET observed_at=?", time.Now().Add(-13*time.Second).UnixNano()); e != nil {
		t.Fatal(e)
	}
	if v = controlView(t, s); v.ObservationFresh || v.Controls != (RouteControls{}) {
		t.Fatal("stale observation enabled controls")
	}
}

func TestRouteQueueControlsSeparateHistoricalACKAndFreshRuntime(t *testing.T) {
	s, _, p := routeQueueFixture(t)
	ctx := context.Background()
	stage := routeRequest(p, 1, "stage", "")
	if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, stage); e != nil {
		t.Fatal(e)
	}
	if v := controlView(t, s); v.Controls != (RouteControls{}) {
		t.Fatal("concurrent command enabled")
	}
	d := routePoll(t, s, &p, true)
	routeStageAck(t, s, &p, d)
	v := controlView(t, s)
	if v.Controls.Apply == nil || v.Controls.Apply.StageID != stage.ID || v.Controls.Apply.ExpiresMillis != d.Command.Expires/int64(time.Millisecond) || len(v.Controls.Apply.Ports) != 1 || v.Controls.Apply.Ports[0] != 26123 {
		t.Fatal("acknowledged Stage not available")
	}
	if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, routeRequest(p, 2, "apply", stage.ID)); e != nil {
		t.Fatal(e)
	}
	ad := routePoll(t, s, &p, true)
	observed := mx.Snapshot{Phase: "ready", Highwater: 1, SavedSequence: 1, SavedSHA: ad.Command.Lease.ConfigSHA, ActiveSequence: 1, ActiveSHA: ad.Command.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: ad.Command.Lease.Expires}
	ack := routeTestSignAck(ad.Command, observed, "applied")
	if e := s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, ack); e != nil {
		t.Fatal(e)
	}
	if v = controlView(t, s); v.Status.Observed.ProcessReady || v.Controls != (RouteControls{}) || v.Commands[0].Receipt == nil {
		t.Fatal("historical ACK enabled current process controls")
	}
	p.Status.Serial, p.Status.CurrentStageID, p.Status.StagedID, p.Status.Observed = ad.Command.Serial, stage.ID, "", observed
	routePoll(t, s, &p, false)
	if v = controlView(t, s); v.Controls.Renew == nil || v.Controls.Renew.StageID != stage.ID {
		t.Fatal("fresh current process unavailable for explicit renewal")
	}
	// An old Stage can leave the latest-50 history without losing explicit
	// restoration. Controls resolve stage identities independently of that list.
	for n := 3; n <= 53; n++ {
		if _, e := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, routeRequest(p, n, "renew", stage.ID)); e != nil {
			t.Fatal(e)
		}
		if e := s.AbandonRouteCommand(ctx, p.NodeID, routeTestID(n)); e != nil {
			t.Fatal(e)
		}
	}
	v = controlView(t, s)
	if len(v.Commands) != 50 || v.Commands[49].ID == stage.ID || v.Controls.Renew == nil {
		t.Fatal("latest-50 truncation hid current Stage")
	}
	// After restart, a late ACK remains historical. Rollback names the saved
	// transaction stage and never grants a Renew for a stopped process.
	p.Status.Instance = strings.Repeat("3", 32)
	p.Status.Observed.ProcessReady = false
	p.Status.Observed.ActiveSequence, p.Status.Observed.ActiveSHA = 0, ""
	p.Status.Observed.Phase = "awaiting_fresh_lease"
	routePoll(t, s, &p, false)
	if e := s.AcceptRouteReceipt(ctx, routeTestKey, routeTestToken, ack); e != nil {
		t.Fatal(e)
	}
	v = controlView(t, s)
	if v.Controls.Renew != nil || v.Controls.Rollback == nil || v.Controls.Rollback.StageID != stage.ID || v.Status.Observed.ProcessReady {
		t.Fatal("restart replay produced false liveness or hid explicit restoration")
	}
	// Editing today's draft blocks renewal/Apply but cannot erase the saved
	// configuration used by an explicit Rollback.
	routeSaveDraft(t, s, 1, 26125)
	if v = controlView(t, s); v.Controls.Apply != nil || v.Controls.Renew != nil || v.Controls.Rollback == nil || v.Controls.Rollback.Ports[0] != 26123 || v.Review[0].DirectPort != 26125 {
		t.Fatal("rollback confused old candidate with today's draft")
	}
}
