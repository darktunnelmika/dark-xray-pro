package business

import (
	"context"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestInboundExecutionNodeChannelsKeepHealthAndAccountingIndependent(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()

	if _, err := f.s.BindInboundExecutionClients(ctx, f.key, "owner", 501, ix.BindingsInput{
		EventID: "node-channel-bind-0001", ExpectedRevision: 0,
		ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient,
		Reason: "Bind synthetic client for independent Node channel projection",
	}); err != nil {
		t.Fatal(err)
	}
	f.command(t, "node-channel-activate-0001", "activate")
	before, err := f.s.ListInboundExecutionNodeChannels(ctx, f.key)
	if err != nil || len(before) != 1 {
		t.Fatal("missing configured channel", before, err)
	}
	if before[0].HealthState != "unknown" || before[0].AccountingState != "unknown" ||
		before[0].ProcessReady || before[0].ReportedAt != 0 {
		t.Fatalf("unobserved channel fabricated readiness: %+v", before[0])
	}

	b := f.bundle(t)
	epoch := f.birth(t, b, "node-channel-epoch-0001")
	f.ready(t, b, epoch)
	after, err := f.s.ListInboundExecutionNodeChannels(ctx, f.key)
	if err != nil || len(after) != 1 {
		t.Fatal("missing observed channel", after, err)
	}
	got := after[0]
	if got.NodeID != b.NodeID || got.InstallationID != b.InstallationID ||
		got.DesiredGeneration != b.Generation || got.ObservedGeneration != b.Generation ||
		!got.ProcessReady || got.HealthState != "healthy" || got.AccountingState != "accepted" ||
		got.ReportedAt <= 0 {
		t.Fatalf("independent channel projection changed evidence: %+v", got)
	}
}
