package business

import (
	"context"
	"errors"
	"testing"
)

func setupRepresentativeV9(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s, _ := opened(t)
	ctx := context.Background()
	for _, id := range []string{"repV9A", "repV9B"} {
		newRep(t, s, id)
		grant(t, s, id, "v9-grant-event-"+id+"-0001", 5<<30, 100)
		if _, err := s.CreateClient(ctx, NewClient{
			EventID:          "v9-create-customer-" + id + "-0001",
			ClientID:         "v9-client-" + id,
			RepresentativeID: id, Name: "Original " + id,
			DurationDays: 30, VolumeBytes: 1 << 30,
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ProvisionRepresentativeLogin(ctx, id, "v9-login-"+id, "v9-safe-password-012345"); err != nil {
			t.Fatal(err)
		}
	}
	return s, ctx
}
func TestV9OwnerRepControlSuspendRevokesSessionsPreservesCustomers(t *testing.T) {
	s, ctx := setupRepresentativeV9(t)
	original, err := s.Client(ctx, "v9-client-repV9A")
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Representative(ctx, "repV9A")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := s.Login(ctx, "v9-login-repV9A", "v9-safe-password-012345")
	if err != nil {
		t.Fatal(err)
	}
	if !auth.User.Permissions.AllEnabled() {
		t.Fatal("old reps did not preserve prior capabilities")
	}
	prior, err := s.RepresentativeControl(ctx, "repV9A")
	if err != nil || prior.LoginSuspended || prior.Revision != 0 || prior.ActiveSessions != 1 {
		t.Fatalf("default controls incorrect %+v %v", prior, err)
	}
	input := RepresentativeControlChange{
		EventID: "v9-suspend-login-and-rename-00001", RepresentativeID: "repV9A",
		ExpectedRevision: 0, DisplayName: "نماینده ویژه آزمایشی", LoginSuspended: true,
		Permissions: DefaultRepresentativePermissions(), Reason: "Temporarily suspend account login",
	}
	result, err := s.UpdateRepresentativeControl(ctx, input)
	if err != nil || result.Replayed || !result.SessionsRevoked || result.Control.Revision != 1 ||
		!result.Control.LoginSuspended || result.Control.ActiveSessions != 0 ||
		result.BalanceChanged || result.ClientsDeleted || result.CustomerTrafficChanged {
		t.Fatalf("suspend failed %+v %v", result, err)
	}
	if _, err = s.VerifySession(ctx, auth.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old cookie survived %v", err)
	}
	if _, err = s.Login(ctx, "v9-login-repV9A", "v9-safe-password-012345"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("suspended rep logged in %v", err)
	}
	// V8 uses auth_users.disabled. Suspended accounts remain denied even if V9
	// binary is rolled back, without disabling active customer records.
	var disabled int
	if err = s.db.QueryRowContext(ctx, "SELECT disabled FROM auth_users WHERE id='repV9A'").Scan(&disabled); err != nil || disabled != 1 {
		t.Fatal("V8 rollback sentinel missing", disabled, err)
	}
	after, err := s.Representative(ctx, "repV9A")
	if err != nil || !after.Enabled || after.VolumeCreditBytes != before.VolumeCreditBytes ||
		after.UnlimitedCreditUnits != before.UnlimitedCreditUnits || after.DisplayName != input.DisplayName {
		t.Fatal("suspend affected credits or business-enabled state", after, err)
	}
	customer, err := s.Client(ctx, original.ID)
	if err != nil || customer.UUID != original.UUID || customer.RepresentativeID != original.RepresentativeID ||
		customer.VolumeBytes != original.VolumeBytes || customer.DeploymentState != original.DeploymentState {
		t.Fatal("suspend changed saved customers", customer, err)
	}
	replay, err := s.UpdateRepresentativeControl(ctx, input)
	if err != nil || !replay.Replayed || replay.Control.Revision != 1 {
		t.Fatalf("exact control retry not idempotent %+v %v", replay, err)
	}
	altered := input
	altered.DisplayName = "Different"
	if _, err = s.UpdateRepresentativeControl(ctx, altered); !errors.Is(err, ErrConflict) {
		t.Fatalf("nonce reuse was allowed %v", err)
	}
	stale := input
	stale.EventID = "v9-control-stale-review-00000001"
	if _, err = s.UpdateRepresentativeControl(ctx, stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale revision accepted", err)
	}
	events, err := s.ListRepresentativeControlEvents(ctx, "repV9A", 25, 0)
	if err != nil || len(events) != 1 || events[0].PreviousName != before.DisplayName ||
		events[0].NewName != input.DisplayName || !events[0].NewLoginSuspended {
		t.Fatal("append-only control history invalid", events, err)
	}
	if _, err = s.db.ExecContext(ctx, "DELETE FROM representative_control_events"); err == nil {
		t.Fatal("control history was deleted")
	}
	if _, err = s.db.ExecContext(ctx, "UPDATE representative_control_events SET reason='tamper'"); err == nil {
		t.Fatal("control history was editable")
	}
	input.EventID = "v9-resume-representative-0000001"
	input.ExpectedRevision = 1
	input.LoginSuspended = false
	resumed, err := s.UpdateRepresentativeControl(ctx, input)
	if err != nil || resumed.Control.Revision != 2 || resumed.Control.LoginSuspended {
		t.Fatal("re-enabling rep failed", resumed, err)
	}
	if _, err = s.Login(ctx, "v9-login-repV9A", "v9-safe-password-012345"); err != nil {
		t.Fatal("login not restored", err)
	}
	if _, err = s.VerifySession(ctx, auth.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("pre-suspension cookie resurrected")
	}
}
func TestV9RestrictedCapabilitiesAreEnforcedAndRollbackFailsClosed(t *testing.T) {
	s, ctx := setupRepresentativeV9(t)
	rights := DefaultRepresentativePermissions()
	rights.CreateClients = false
	rights.ViewFinance = false
	rights.ManageGroups = false
	edit := RepresentativeControlChange{
		EventID:          "v9-restrict-role-access-0000001",
		RepresentativeID: "repV9A", DisplayName: "Limited staff",
		ExpectedRevision: 0, Permissions: rights, Reason: "Owner-approved scoped roles",
	}
	result, err := s.UpdateRepresentativeControl(ctx, edit)
	if err != nil || result.Control.Permissions.CreateClients || result.Control.Permissions.ViewFinance ||
		result.Control.Permissions.ManageGroups || result.Control.LoginSuspended {
		t.Fatal("restricted permissions not saved", result, err)
	}
	// New V9 sessions can be created for a limited representative, but
	// V8 rollback must deny its entire account rather than widen access.
	session, err := s.Login(ctx, "v9-login-repV9A", "v9-safe-password-012345")
	if err != nil || session.User.Permissions.CreateClients || session.User.Permissions.ViewFinance ||
		!session.User.Permissions.RenewClients {
		t.Fatal("role login permissions incorrect", session, err)
	}
	valid, err := s.VerifySession(ctx, session.Token)
	if err != nil || valid.User.Permissions.ManageGroups {
		t.Fatal("VerifySession ignored revocation", valid, err)
	}
	var fallback int
	if err = s.db.QueryRowContext(ctx, "SELECT disabled FROM auth_users WHERE id='repV9A'").Scan(&fallback); err != nil || fallback != 1 {
		t.Fatal("V8 rollback would permit restricted account", fallback, err)
	}
	// The role is a pure authentication boundary; it does not change balance.
	rep, err := s.Representative(ctx, "repV9A")
	if err != nil || !rep.Enabled {
		t.Fatal("role changed customer entitlement", rep, err)
	}
	// No other representative is affected.
	foreign, err := s.RepresentativeControl(ctx, "repV9B")
	if err != nil || foreign.Revision != 0 || !foreign.Permissions.AllEnabled() {
		t.Fatal("permission leaked to unrelated representative", foreign, err)
	}
}
