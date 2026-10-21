package business

import (
	"context"
	"strings"
	"testing"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func TestMeteredDrainCanStopRevokedClientButCannotInventFinalSeal(t *testing.T) {
	ctx := context.Background()
	s, _, key, client, p := meteredQueueFixture(t)
	d := meteredQueuedApply(t, s, key, &p)
	c := d.Command
	b, err := mx.SignBirthReport(string(routeTestToken), c.Lease, uc.ProcessToken{BootID: "11111111-1111-4111-8111-111111111111", PID: 12345, StartTicks: 77})
	if err != nil {
		t.Fatal(err)
	}
	epoch, err := s.MeteredRouteAuthority(ctx, key, routeTestToken, mx.MeteredAuthorityRequest{Command: c, Birth: &b})
	if err != nil {
		t.Fatal(err)
	}
	_, binding, _ := uc.CanonicalEpoch(epoch)
	zero := uc.Report{Version: 1, EpochID: epoch.ID, BindingSHA: binding, Sequence: 1, Users: []uc.Usage{{ClientID: client.ID}}}
	zero.SHA = meteredHash(zero)
	if _, err = s.usage.Accept(ctx, usageledger.Principal{NodeID: p.NodeID, InstallationID: p.InstallationID}, zero); err != nil {
		t.Fatal(err)
	}
	u := mx.MeteredCondition{EpochID: epoch.ID, EpochSHA: meteredHash(epoch), AdmissionSHA: strings.Repeat("d", 64)}
	o := mx.Snapshot{Phase: "ready_observed_admission", Highwater: c.RuntimeSequence, SavedSequence: c.RuntimeSequence, SavedSHA: c.Lease.ConfigSHA, ActiveSequence: c.RuntimeSequence, ActiveSHA: c.Lease.ConfigSHA, ProcessReady: true, LeaseExpires: c.Lease.Expires, Usage: &mx.UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}}
	if err = s.AcceptMeteredRouteReceipt(ctx, key, routeTestToken, meteredQueueAck(c, o, "applied", u, uc.Receipt{})); err != nil {
		t.Fatal(err)
	}
	p.Status.Serial, p.Status.CurrentStageID, p.Status.StagedID = 2, c.StageID, ""
	p.Status.Usage = &u
	p.Status.Observed = o
	meteredQueuePoll(t, s, key, &p, false)
	if _, err = s.SetClientState(ctx, SetClientState{EventID: "revoke-before-drain-001", ClientID: client.ID, Action: "disable", Reason: "No new access; exact stop remains available"}); err != nil {
		t.Fatal(err)
	}
	view, err := s.MeteredRouteControlChannel(ctx, key, p.NodeID)
	if err != nil || view.Controls.Drain == nil || view.Controls.Stage || view.Controls.Apply != nil || view.Controls.Renew != nil {
		t.Fatal("invalid drain controls after revocation", err)
	}
	in := meteredQueueRequest(p, 3, "drain", c.StageID)
	if _, err = s.QueueMeteredRouteCommand(ctx, key, p.NodeID, in); err != nil {
		t.Fatal("drain blocked by revocation", err)
	}
	drain := meteredQueuePoll(t, s, key, &p, true)
	if drain.Command.Action != "drain" || drain.Command.Lease != (mx.Lease{}) || drain.Command.RuntimeSequence != o.Highwater || drain.ConfigBase64 != "" {
		t.Fatal("drain granted a new execution lease")
	}
	if _, err = s.MeteredRouteAuthority(ctx, key, routeTestToken, mx.MeteredAuthorityRequest{Command: drain.Command}); err == nil {
		t.Fatal("drain granted admission authority")
	}
	fake := uc.Receipt{EpochID: epoch.ID, Sequence: 2, SHA: strings.Repeat("f", 64)}
	stopped := o
	stopped.ProcessReady = false
	stopped.ActiveSequence = 0
	stopped.ActiveSHA = ""
	stopped.LeaseExpires = 0
	stopped.Usage = &mx.UsageObservationStatus{Required: true, Settled: true}
	after := u
	after.Settlement = fake
	ack := meteredQueueAck(drain.Command, stopped, "drained", after, fake)
	if !mx.VerifyMeteredTransactionReceipt(routeTestToken, drain.Command, ack) {
		t.Fatal("invalid signed fixture")
	}
	if err = s.AcceptMeteredRouteReceipt(ctx, key, routeTestToken, ack); err == nil {
		t.Fatal("Agent invented HUB final settlement")
	}
}
