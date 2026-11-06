package business

import (
	"context"
	"errors"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

func TestRepresentativeServiceAdmissionPreservesMembersAndRevocation(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	s := f.s
	newRep(t, s, "delegate")
	grant(t, s, "delegate", "delegate-credit-event-001", 1<<30, 10)
	c, e := s.CreateClient(ctx, NewClient{EventID: "delegate-create-event-001", ClientID: "delegate-client", RepresentativeID: "delegate", Name: "Delegate", DurationDays: 30, VolumeBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.ProvisionRepresentativeLogin(ctx, "delegate", "delegate-login", "delegate-password-12345")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.BindInboundExecutionClients(ctx, f.key, "owner", 501, ix.BindingsInput{EventID: "delegate-bind-owner-001", ExpectedRevision: 0, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Initial owner client"}); e != nil {
		t.Fatal(e)
	}
	f.command(t, "delegate-activate-001", "activate")
	b := f.bundle(t)
	epoch := f.birth(t, b, "delegate-epoch-001")
	f.ready(t, b, epoch)
	g := ServiceGrantChange{EventID: "delegate-grant-001", RepresentativeID: "delegate", InboundID: 501, Enabled: true, Reason: "Delegate selected service"}
	if _, e = s.ChangeRepresentativeServiceGrant(ctx, "owner", g); e != nil {
		t.Fatal(e)
	}
	choices, e := s.RepresentativeClientServices(ctx, f.key, u.ID, c.ID)
	if e != nil || len(choices.Services) != 1 || choices.Services[0].Assigned || !choices.Services[0].Ready {
		t.Fatalf("scoped services %v %v", choices, e)
	}
	if _, e = s.RepresentativeClientServices(ctx, f.key, u.ID, f.client.ID); e == nil {
		t.Fatal("foreign services leaked")
	}
	state, e := s.InboundExecution(ctx, f.key, 501)
	if e != nil {
		t.Fatal(e)
	}
	in := RepresentativeServiceChange{EventID: "delegate-apply-001", ClientID: c.ID, InboundID: 501, GrantRevision: 1, ExecutionRevision: state.Revision, PlanRevision: state.PlanRevision, InboundRevision: state.InboundRevision, Add: true, Reason: "Add own client"}
	preview, e := s.ChangeRepresentativeService(ctx, f.key, u.ID, in, false, nil)
	if e != nil || !preview.Valid {
		t.Fatalf("preview %v %v", preview, e)
	}
	in.ReviewSHA = preview.ReviewSHA
	foreign := in
	foreign.ClientID = f.client.ID
	if _, e = s.ChangeRepresentativeService(ctx, f.key, u.ID, foreign, true, nil); e == nil {
		t.Fatal("foreign client accepted")
	}
	g.EventID = "delegate-revoke-001"
	g.ExpectedRevision = 1
	g.Enabled = false
	if _, e = s.ChangeRepresentativeServiceGrant(ctx, "owner", g); e != nil {
		t.Fatal(e)
	}
	choices, e = s.RepresentativeClientServices(ctx, f.key, u.ID, c.ID)
	if e != nil || len(choices.Services) != 0 {
		t.Fatalf("revoked service listed %v %v", choices, e)
	}
	if _, e = s.ChangeRepresentativeService(ctx, f.key, u.ID, in, true, nil); !errors.Is(e, ErrConflict) {
		t.Fatalf("revoked preview accepted: %v", e)
	}
	g.EventID = "delegate-regrant-001"
	g.ExpectedRevision = 2
	g.Enabled = true
	if _, e = s.ChangeRepresentativeServiceGrant(ctx, "owner", g); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ChangeRepresentativeService(ctx, f.key, u.ID, in, true, nil); !errors.Is(e, ErrConflict) {
		t.Fatalf("old grant generation reused: %v", e)
	}
	in.GrantRevision = 3
	in.ReviewSHA = ""
	preview, e = s.ChangeRepresentativeService(ctx, f.key, u.ID, in, false, nil)
	if e != nil || !preview.Valid {
		t.Fatal(preview, e)
	}
	in.ReviewSHA = preview.ReviewSHA
	if _, e = s.ChangeRepresentativeService(ctx, f.key, u.ID, in, true, func(ix.Preview) bool { return false }); !errors.Is(e, ErrConflict) {
		t.Fatalf("host gate bypassed %v", e)
	}
	result, e := s.ChangeRepresentativeService(ctx, f.key, u.ID, in, true, nil)
	if e != nil || result.OperationID != in.EventID {
		t.Fatalf("admission %v %v", result, e)
	}
	replay, e := s.ChangeRepresentativeService(ctx, f.key, u.ID, in, true, nil)
	if e != nil || !replay.Replayed {
		t.Fatalf("replay %v %v", replay, e)
	}
	state, e = s.InboundExecution(ctx, f.key, 501)
	if e != nil {
		t.Fatal(e)
	}
	found := map[string]bool{}
	for _, id := range state.ClientIDs {
		found[id] = true
	}
	if len(found) != 2 || !found[c.ID] || !found[f.client.ID] {
		t.Fatal("other members lost", state.ClientIDs)
	}
	var actor string
	if e = s.db.QueryRow("SELECT actor_id FROM inbound_execution_events WHERE event_id=?", in.EventID).Scan(&actor); e != nil || actor != u.ID {
		t.Fatal("actor impersonated", actor, e)
	}
	g.EventID = "revoke-after-admission-001"
	g.ExpectedRevision = 3
	g.Enabled = false
	if _, e = s.ChangeRepresentativeServiceGrant(ctx, "owner", g); e != nil {
		t.Fatal(e)
	}
	afterRevoke, e := s.InboundExecution(ctx, f.key, 501)
	if e != nil || len(afterRevoke.ClientIDs) != 2 {
		t.Fatal("revocation removed accepted customer membership", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = ValidateOfflineBackup(ctx, f.path); e != nil {
		t.Fatalf("delegated command backup: %v", e)
	}

}

func TestRepresentativeServiceConcurrentRevokePreventsStaleAdmission(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	s := f.s
	newRep(t, s, "delegate")
	grant(t, s, "delegate", "delegate-credit-event-001", 1<<30, 10)
	c, e := s.CreateClient(ctx, NewClient{EventID: "delegate-create-event-001", ClientID: "delegate-client", RepresentativeID: "delegate", Name: "Delegate", DurationDays: 30, VolumeBytes: 1 << 20})
	if e != nil {
		t.Fatal(e)
	}
	u, e := s.ProvisionRepresentativeLogin(ctx, "delegate", "delegate-login", "delegate-password-12345")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.BindInboundExecutionClients(ctx, f.key, "owner", 501, ix.BindingsInput{EventID: "delegate-bind-owner-001", ExpectedRevision: 0, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Initial owner client"}); e != nil {
		t.Fatal(e)
	}
	f.command(t, "delegate-activate-001", "activate")
	b := f.bundle(t)
	epoch := f.birth(t, b, "delegate-epoch-001")
	f.ready(t, b, epoch)
	g := ServiceGrantChange{EventID: "delegate-grant-001", RepresentativeID: "delegate", InboundID: 501, Enabled: true, Reason: "Delegate selected service"}
	if _, e = s.ChangeRepresentativeServiceGrant(ctx, "owner", g); e != nil {
		t.Fatal(e)
	}
	choices, e := s.RepresentativeClientServices(ctx, f.key, u.ID, c.ID)
	if e != nil || len(choices.Services) != 1 || choices.Services[0].Assigned || !choices.Services[0].Ready {
		t.Fatalf("scoped services %v %v", choices, e)
	}
	if _, e = s.RepresentativeClientServices(ctx, f.key, u.ID, f.client.ID); e == nil {
		t.Fatal("foreign services leaked")
	}
	state, e := s.InboundExecution(ctx, f.key, 501)
	if e != nil {
		t.Fatal(e)
	}
	in := RepresentativeServiceChange{EventID: "delegate-apply-001", ClientID: c.ID, InboundID: 501, GrantRevision: 1, ExecutionRevision: state.Revision, PlanRevision: state.PlanRevision, InboundRevision: state.InboundRevision, Add: true, Reason: "Add own client"}
	preview, e := s.ChangeRepresentativeService(ctx, f.key, u.ID, in, false, nil)
	if e != nil || !preview.Valid {
		t.Fatalf("preview %v %v", preview, e)
	}
	in.ReviewSHA = preview.ReviewSHA
	other, e := Open(f.path)
	if e != nil {
		t.Fatal(e)
	}
	defer other.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	go func() {
		_, err := s.ChangeRepresentativeService(ctx, f.key, u.ID, in, true, func(ix.Preview) bool {
			close(entered)
			select {
			case <-release:
				return true
			case <-ctx.Done():
				return false
			}
		})
		done <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("admission did not reach host gate")
	}
	g.EventID = "concurrent-revoke-event-001"
	g.ExpectedRevision = 1
	g.Enabled = false
	_, revokeErr := other.ChangeRepresentativeServiceGrant(ctx, "owner", g)
	close(release)
	select {
	case admissionErr := <-done:
		if revokeErr != nil {
			t.Fatalf("concurrent revocation: %v", revokeErr)
		}
		if admissionErr == nil {
			t.Fatal("stale read snapshot committed after revocation")
		}
	case <-ctx.Done():
		t.Fatal("concurrent admission stalled")
	}
	var count int
	if e = s.db.QueryRow("SELECT count(*) FROM inbound_execution_events WHERE event_id=?", in.EventID).Scan(&count); e != nil || count != 0 {
		t.Fatalf("rejected admission left an event: %d %v", count, e)
	}
	state, e = s.InboundExecution(context.Background(), f.key, 501)
	if e != nil || len(state.ClientIDs) != 1 || state.ClientIDs[0] != f.client.ID {
		t.Fatalf("failed admission changed membership %v %v", state.ClientIDs, e)
	}
}
