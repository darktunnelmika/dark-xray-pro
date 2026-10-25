package business

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestManagedInboundSubscriptionScopedPermissionsAndRetryReauthorization(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		id := fmt.Sprintf("managed-sub-rep-%d", i)
		newRep(t, f.s, id)
		grant(t, f.s, id, "managed-sub-grant-"+id, 1000, 0)
		if _, err := f.s.ProvisionRepresentativeLogin(ctx, id, id, "synthetic-subscription-password"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.CreateClient(ctx, NewClient{EventID: "managed-sub-client-" + id, ClientID: "client-" + id, RepresentativeID: id, Name: "Managed scoped client", DurationDays: 30, VolumeBytes: 100}); err != nil {
			t.Fatal(err)
		}
	}
	clientID, actor := "client-managed-sub-rep-0", "managed-sub-rep-0"
	in := SubscriptionTokenMutation{EventID: "managed-sub-scope-issue-0001", Action: "issue"}
	if _, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, "managed-sub-rep-1", clientID, in); err == nil {
		t.Fatal("cross-representative token mutation")
	}
	if _, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, actor, clientID, in); err != nil {
		t.Fatal(err)
	}
	permissions := DefaultRepresentativePermissions()
	permissions.ManageClients = false
	change := RepresentativeControlChange{EventID: "managed-sub-scope-permission-0001", RepresentativeID: actor, DisplayName: "Managed scoped rep", Permissions: permissions, Reason: "Verify managed subscription permissions"}
	if _, err := f.s.UpdateRepresentativeControl(ctx, change); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.RevealInboundExecutionSubscription(ctx, f.key, actor, clientID, 1); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("revealed after manage_clients revoked", err)
	}
	if _, err := f.s.ProjectInboundExecutionSubscription(ctx, f.key, actor, clientID, "uri"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("credential export bypassed manage_clients", err)
	}
	if _, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, actor, clientID, in); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("exact replay bypassed current permissions", err)
	}
	if _, err := f.s.InboundExecutionSubscription(ctx, f.key, actor, clientID); err != nil {
		t.Fatal("view_clients lost independent metadata access", err)
	}
	if _, err := f.s.RevealInboundExecutionSubscription(ctx, f.key, "owner", clientID, 1); err != nil {
		t.Fatal("owner lost token scope", err)
	}
	change.EventID, change.ExpectedRevision, change.LoginSuspended = "managed-sub-scope-suspend-0001", 1, true
	if _, err := f.s.UpdateRepresentativeControl(ctx, change); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.InboundExecutionSubscription(ctx, f.key, actor, clientID); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("suspended login read managed metadata", err)
	}
}

func TestManagedInboundSubscriptionProjectionTokensAndWithdrawal(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	issue := SubscriptionTokenMutation{EventID: "managed-subscription-issue-0001", Action: "issue"}
	receipt, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, issue)
	if err != nil || receipt.Generation != 1 || receipt.State != "active" {
		t.Fatal(receipt, err)
	}
	again, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, issue)
	if err != nil || !again.Replayed {
		t.Fatal(again, err)
	}
	secret, err := f.s.RevealInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, 1)
	if err != nil || len(secret.Token) != 43 {
		t.Fatal(err)
	}
	if _, err = f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri"); !errors.Is(err, ErrNotFound) {
		t.Fatal("saved disabled authoring published", err)
	}
	f.command(t, "managed-subscription-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-subscription-0001")
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 1, 0, 0, false)); err != nil {
		t.Fatal(err)
	}
	observation := executionObservation(b, e)
	observation.HealthState = "engine_ready"
	if err = f.s.ObserveInboundExecution(ctx, f.key, observation); err != nil {
		t.Fatal(err)
	}
	view, err := f.s.InboundExecutionSubscription(ctx, f.key, "owner", f.client.ID)
	if err != nil || !view.Available || view.PathVerified || view.RouteCount != 1 {
		t.Fatal("engine readiness became path proof", view, err)
	}
	doc, err := f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri")
	if err != nil || !strings.HasPrefix(string(doc.Content), "vless://") || !strings.Contains(string(doc.Content), "a.example.org:21185") || !strings.Contains(string(doc.Content), f.client.UUID) || doc.PathVerified || doc.SHA256 != ix.BytesSHA(doc.Content) {
		t.Fatal("wrong applied identity endpoint", string(doc.Content), err)
	}
	if _, err = f.s.ProjectInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, "wireguard"); err == nil {
		t.Fatal("unsupported format silently dropped route")
	}
	loaded, _ := f.s.NativeInboundPlan(ctx, f.key, 501)
	edit := copiedPlan(f.plan)
	edit.EventID = "managed-subscription-draft-edit-0001"
	edit.ExpectedPlanRevision = 1
	edit.ExpectedInboundRevision = 1
	edit.Profile = loaded.Profile.InboundInput
	edit.Nodes[0].DirectHosts = []string{"new.example.org"}
	if _, err = f.s.SaveNativeInboundPlan(ctx, f.key, edit); err != nil {
		t.Fatal(err)
	}
	draft, err := f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri")
	if err != nil || string(draft.Content) != string(doc.Content) {
		t.Fatal("unapplied draft exported", err)
	}
	rotated, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: "managed-subscription-rotate-0001", Action: "rotate", ExpectedGeneration: 1})
	if err != nil || rotated.Generation != 2 {
		t.Fatal(err)
	}
	if _, err = f.s.ReadInboundExecutionSubscription(ctx, f.key, secret.Token, "uri"); !errors.Is(err, ErrNotFound) {
		t.Fatal("old rotated token accepted", err)
	}
	newSecret, err := f.s.RevealInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ReadInboundExecutionSubscription(ctx, f.key, newSecret.Token, "xray"); err != nil {
		t.Fatal(err)
	}
	f.command(t, "managed-subscription-apply-edited-0001", "apply")
	if _, err = f.s.ReadInboundExecutionSubscription(ctx, f.key, newSecret.Token, "uri"); !errors.Is(err, ErrConflict) {
		t.Fatal("stale applied generation served during replace", err)
	}
	if _, err = f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: "managed-subscription-revoke-0001", Action: "revoke", ExpectedGeneration: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ReadInboundExecutionSubscription(ctx, f.key, newSecret.Token, "uri"); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked token accepted", err)
	}
}

func TestManagedInboundSubscriptionRejectsStaleHealthAndQuota(t *testing.T) {
	f := newExecutionFixture(t)
	ctx := context.Background()
	f.command(t, "managed-subscription-health-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "epoch-sub-health-0001")
	f.ready(t, b, e)
	o := executionObservation(b, e)
	o.ObservedUnixNano = time.Now().Add(-time.Minute).UnixNano()
	enc, err := sealPlanValue(f.key, executionAAD("observation", b.NodeID+"/"+b.InstallationID, b.Generation), o)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.db.Exec(`UPDATE inbound_execution_nodes SET observation_enc=? WHERE node_id=?`, enc, b.NodeID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProjectInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, "uri"); !errors.Is(err, ErrConflict) {
		t.Fatal("stale observation served", err)
	}
	o = executionObservation(b, e)
	if err = f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 1<<20, 0, false)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProjectInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, "uri"); !errors.Is(err, ErrConflict) {
		t.Fatal("quota exhaustion still exported", err)
	}
}
