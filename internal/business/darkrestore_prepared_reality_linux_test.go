//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPreparedLoopbackRealityMatchesAppliedIdentity(t *testing.T) {
	ctx := context.Background()
	f := newPromotionFixture(t, nil)
	c := f.clients[0]
	tx, err := f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := preparedPlanAuthority(ctx, tx, 501)
	if err != nil {
		t.Fatal(err)
	}
	binding.Format = "uri"
	prepared, err := renderPreparedSubscription(ctx, tx, f.key, c.ID, binding)
	tx.Rollback()
	if err != nil || prepared.PathVerified {
		t.Fatal("prepared export", err)
	}
	if _, err = f.s.BootstrapOwner(ctx, "initial-owner", "synthetic-initial-health-password"); err != nil {
		t.Fatal(err)
	}
	if err = f.s.EnableInitialApplyHealth(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PromoteNativeInboundPlan(ctx, f.key, 501, f.reviewed(t)); err != nil {
		t.Fatal(err)
	}
	initialPrepare(t, f)
	initialActivate(t, f, 1)
	round, _, _ := initialCompleteRound(t, f, 990)
	tx, err = f.s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	endpoint := healthEndpoint()
	proof, err := f.s.currentInitialSubscriptionProof(ctx, tx, f.key, c.ID, 501, round.ID, round.InitialApplySHA, &endpoint, time.Now().UnixNano())
	if err != nil || !bytes.Equal(prepared.Content, proof.Raw) {
		t.Fatal("prepared and applied routes differ", err)
	}
}

func TestImportedLoopbackRealityStaysQuarantined(t *testing.T) {
	ctx := context.Background()
	f, ev, key, _ := preparedCutoverFixture(t)
	plan := NativeInboundPlanInput{EventID: "prepared-loopback-reality-0001", Profile: nativeInput(), Reason: "Disposable metered loopback", Nodes: []NativeInboundPlanNode{{NodeID: "lab-reality", InstallationID: strings.Repeat("a", 32), ListenIP: "127.0.0.1", DirectPort: 29181, DirectHosts: []string{"127.0.0.1"}}}}
	plan.Profile.ID = 502
	if _, err := f.s.SaveNativeInboundPlan(ctx, key, plan); err != nil {
		t.Fatal(err)
	}
	input := DarkRestorePreparedDeliveryInputV1{EventID: "prepared-loopback-delivery-0001", InboundID: 502, ExpectedPlanRevision: 1, ExpectedInboundRevision: 1, Format: "uri"}
	balances := ownershipBalances(t, f.s)
	delivered, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, ev.ClientID, input)
	if err != nil || delivered.Applied || delivered.Subscription.PathVerified || delivered.Receipt.RemainingBudgetBytes != 999986 {
		t.Fatal("prepared delivery", err)
	}
	client, err := f.s.Client(ctx, ev.ClientID)
	if err != nil || client.DeploymentState != "disabled" || !bytes.Contains(delivered.Subscription.Content, []byte(client.UUID+"@127.0.0.1:29181")) {
		t.Fatal("quarantine or identity mismatch", err)
	}
	replay, err := f.s.DeliverPreparedDarkRestoreSubscriptionV1(ctx, key, f.owner.Token, ev.ClientID, input)
	if err != nil || !replay.Receipt.Replayed || !bytes.Equal(delivered.Subscription.Content, replay.Subscription.Content) || !reflect.DeepEqual(balances, ownershipBalances(t, f.s)) {
		t.Fatal("replay or balance mismatch", err)
	}
	stage, err := f.s.DarkRestoreQuarantinePlan(ctx, key, f.owner.Token, client.ID)
	if err != nil || stage.RemainingBytes != 999986 || stage.Prepared.ContentSHA != delivered.Subscription.SHA256 {
		t.Fatal("quarantine plan", err)
	}
	pin, err := stage.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = stage.ValidateOffline(ctx, "wrong-node", stage.InstallationID, pin, "", ""); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("wrong node passed")
	}
	if _, err = f.s.DarkRestoreQuarantinePlan(ctx, key, f.rep.Token, client.ID); err == nil {
		t.Fatal("representative staged a quarantined plan")
	}
	if binary := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"); binary != "" {
		pub, signer, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		if err = os.Chmod(dir, 0700); err != nil {
			t.Fatal(err)
		}
		core := os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
		receipt, err := stage.RegisterOffline(ctx, stage.NodeID, stage.InstallationID, pin, binary, core, dir, signer)
		if err != nil || !receipt.Verify(pub, pin, core) {
			t.Fatal("real offline registration", err)
		}
		replay, err := stage.RegisterOffline(ctx, stage.NodeID, stage.InstallationID, pin, binary, core, dir, signer)
		if err != nil || !reflect.DeepEqual(replay, receipt) {
			t.Fatal("registration replay", err)
		}
		path := filepath.Join(dir, pin+".json")
		raw, err := os.ReadFile(path)
		var persisted DarkRestoreQuarantineRegistration
		if err != nil || json.Unmarshal(raw, &persisted) != nil || !persisted.Verify(pub, pin, core) || persisted.Verify(pub, strings.Repeat("0", 64), core) {
			t.Fatal("persisted registration binding", err)
		}
		if err = os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err = stage.RegisterOffline(ctx, stage.NodeID, stage.InstallationID, pin, binary, core, dir, signer); !errors.Is(err, ErrConflict) {
			t.Fatal("corrupt receipt overwritten", err)
		}
		t.Log("pinned core accepted quarantined imported identity")
	}
	if _, err = f.s.SetClientStateScoped(ctx, SetClientState{EventID: "prepared-loopback-enable-0001", ClientID: client.ID, Action: "enable", Reason: "Verify imported client guard"}, client.RepresentativeID); !errors.Is(err, ErrDarkRestoreCutover) {
		t.Fatal("missing apply evidence", err)
	}
}

func TestPreparedRealityRejectsUnreviewedRoute(t *testing.T) {
	f := newPromotionFixture(t, nil)
	for _, mode := range []string{"external address", "tunnel", "swap", "other format"} {
		t.Run(mode, func(t *testing.T) {
			p := f.plan.Plan
			p.Nodes = append([]NativeInboundPlanNode(nil), p.Nodes...)
			format := "uri"
			switch mode {
			case "external address":
				p.Nodes[0].DirectHosts = []string{"unapproved.example.org"}
			case "tunnel":
				p.Nodes[0].TunnelPort = 29999
			case "swap":
				p.Nodes[0].SwapEnabled = true
			case "other format":
				format = "json"
			}
			v, err := renderPreparedLoopbackReality(p, f.clients[0], format)
			if !errors.Is(err, ErrDarkRestoreCutover) || len(v.Content) != 0 {
				t.Fatal("unsupported export", err)
			}
		})
	}
}
