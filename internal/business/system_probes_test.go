package business

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestSystemProbeProvisionRenderReopenAndRevocation(t *testing.T) {
	ctx := context.Background()
	s, path, key, client, p := meteredQueueFixture(t)
	if err := s.EnableSystemProbeAccounting(ctx); err != nil {
		t.Fatal(err)
	}
	probe, err := s.ProvisionMeteredProbe(ctx, key, p.NodeID, p.InstallationID, 10)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.ProvisionMeteredProbe(ctx, key, p.NodeID, p.InstallationID, 10)
	if err != nil || again != probe {
		t.Fatal("identity replay changed private credential", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	users, err := meteredRouteUsers(ctx, tx, 10, p.NodeID, p.InstallationID)
	tx.Rollback()
	if err != nil || len(users) != 2 || !usersContain(users, probe.UUID) || !usersContain(users, client.UUID) {
		t.Fatal("dedicated probe absent or replaced customer", err)
	}
	if _, err = s.Client(ctx, probe.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("system identity appears as customer", err)
	}
	if _, err = s.ProvisionMeteredProbe(ctx, key, p.NodeID, strings.Repeat("f", 32), 10); !errors.Is(err, ErrConflict) {
		t.Fatal("changed installation admitted", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal("v9 reopen", err)
	}
	defer s.Close()
	var version int
	if err = s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 9 {
		t.Fatal("v9 silently downgraded", version, err)
	}
	if _, err = s.SetClientState(ctx, SetClientState{EventID: "system-probe-revoke-customer-001", ClientID: client.ID, Action: "disable", Reason: "System probe must not reopen an empty customer route"}); err != nil {
		t.Fatal(err)
	}
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	users, err = meteredRouteUsers(ctx, tx, 10, p.NodeID, p.InstallationID)
	tx.Rollback()
	if err != nil || len(users) != 0 {
		t.Fatal("probe kept an otherwise empty route eligible", err)
	}
}

func usersContain(users []xrayconfig.User, uuid string) bool {
	for _, u := range users {
		if u.UUID == uuid {
			return true
		}
	}
	return false
}

func TestSystemProbeBindingDerivedByHUBAndUnsettledProvisionBlocked(t *testing.T) {
	ctx := context.Background()
	s, _, key, client, p := meteredQueueFixture(t)
	if err := s.EnableSystemProbeAccounting(ctx); err != nil {
		t.Fatal(err)
	}
	probe, err := s.ProvisionMeteredProbe(ctx, key, p.NodeID, p.InstallationID, 10)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := meteredRouteCandidate(ctx, tx, key, p.NodeID, p.InstallationID, []RouteDraftRef{{InboundID: 10, InboundRevision: 1, DraftRevision: 1}})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if !bytes.Contains(candidate.Config, []byte(probe.UUID)) || !bytes.Contains(candidate.Config, []byte(client.UUID)) {
		tx.Rollback()
		t.Fatal("staged candidate lost customer/system identity")
	}
	cfg := NodeConfig{NodeID: p.NodeID, InstallationID: p.InstallationID, ConfigSHA: candidate.SHA256, Candidate: candidate}
	e, err := s.registerMeteredRouteEpoch(ctx, tx, cfg, UsageBirth{EpochID: strings.Repeat("1", 32), KernelBootID: "11111111-1111-4111-8111-111111111111", PID: 12345, StartTicks: 77})
	if err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if len(e.Bindings) != 2 {
		t.Fatal("incomplete mixed bindings")
	}
	for _, b := range e.Bindings {
		if b.ClientID == probe.ID && b.Billable || b.ClientID == client.ID && !b.Billable {
			t.Fatal("billing role substituted")
		}
	}
	// A second inbound on this claimed installation must not gain a new
	// system identity while an earlier epoch has an unsettled traffic tail.
	input := nativeInput()
	input.ID = 11
	if _, err = s.CreateInbound(ctx, key, input); err != nil {
		t.Fatal(err)
	}
	if err = s.PutInboundTarget(ctx, InboundTarget{InboundID: 11, NodeID: p.NodeID, InstallationID: p.InstallationID, ListenIP: "127.0.0.1", DirectPort: 19590}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ProvisionMeteredProbe(ctx, key, p.NodeID, p.InstallationID, 11); !errors.Is(err, ErrConflict) {
		t.Fatal("new identity raced an unsettled epoch", err)
	}
	if _, err = s.usage.Account(ctx, probe.ID); !errors.Is(err, usageledger.ErrMissing) {
		t.Fatal("system binding has financial account")
	}
}

func TestSystemProbeOptInDoesNotModifyOrdinaryNativeProfiles(t *testing.T) {
	ctx := context.Background()
	s, _, key, client, cfg := usageSetup(t)
	if err := s.EnableSystemProbeAccounting(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProvisionMeteredProbe(ctx, key, cfg.NodeID, cfg.InstallationID, 10); !errors.Is(err, ErrConflict) {
		t.Fatal("ordinary runtime got a probe identity", err)
	}
	after, err := s.BuildNodeConfig(ctx, key, cfg.NodeID, cfg.InstallationID)
	if err != nil || after.UserCount != 1 || !bytes.Contains(after.Candidate.Config, []byte(client.UUID)) {
		t.Fatal("ordinary native profile changed", err)
	}
}
