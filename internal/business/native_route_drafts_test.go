package business

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func routeDraftFixture(t *testing.T) (*Store, []byte, NativeRouteDraftInput) {
	t.Helper()
	ctx := context.Background()
	s, _ := opened(t)
	key := bytes.Repeat([]byte{0xa3}, 32)
	initial := nativeInput()
	initial.ID = 1185
	if _, err := s.CreateInbound(ctx, key, initial); err != nil {
		t.Fatal(err)
	}
	for _, binding := range []InboundTarget{
		{InboundID: 1185, NodeID: "nl", InstallationID: strings.Repeat("a", 32),
			ListenIP: "198.51.100.10", SwapPort: 26000},
		{InboundID: 1185, NodeID: "de", InstallationID: strings.Repeat("b", 32),
			ListenIP: "198.51.100.20", DirectPort: 21185, SwapPort: 26001},
		{InboundID: 1185, NodeID: "pl", InstallationID: strings.Repeat("c", 32),
			ListenIP: "198.51.100.30", DirectPort: 31185},
	} {
		if err := s.PutInboundTarget(ctx, binding); err != nil {
			t.Fatal(err)
		}
	}
	initial.ID = 2000
	initial.Name = "Unrelated inbound"
	if _, err := s.CreateInbound(ctx, key, initial); err != nil {
		t.Fatal(err)
	}
	if err := s.PutInboundTarget(ctx, InboundTarget{InboundID: 2000, NodeID: "de",
		InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20", DirectPort: 28080}); err != nil {
		t.Fatal(err)
	}
	in := NativeRouteDraftInput{
		EventID: "native-route-draft-test-0001", InboundID: 1185,
		NodeID: "de", InstallationID: strings.Repeat("b", 32), ExpectedInboundRevision: 1,
		DirectPort: 21185, DirectHosts: []string{"de.example.org"},
		SwapEnabled: true, SwapUpstreamNodeID: "nl",
		SwapTunnelHost: "nl-tunnel.example.org", SwapTunnelPort: 26000, SwapNodePort: 26001,
		Reason: "Owner-approved draft, without live Xray changes",
	}
	return s, key, in
}
func TestP0ERouteDraftReadOnlyPreviewEncryptedStoreAndReplay(t *testing.T) {
	s, key, in := routeDraftFixture(t)
	ctx := context.Background()
	preview, err := s.PreviewNativeRouteDraft(ctx, key, in)
	if err != nil || !preview.Valid || preview.ApplyAllowed || preview.AgentAckVerified ||
		preview.BillingVerified || preview.DataPlaneVerified {
		t.Fatal("unsafe preview", preview, err)
	}
	result, err := s.SaveNativeRouteDraft(ctx, key, in)
	if err != nil || result.Draft.DraftRevision != 1 || result.Draft.BillingSourceNodeID != "de" ||
		result.Draft.ApplyAllowed || result.Draft.AgentAckVerified || result.Draft.BillingVerified ||
		result.RuntimeChanged || result.CreditChanged || result.ClientsDeleted || result.UsageReset {
		t.Fatal("unsafe stored draft", result, err)
	}
	var cipherText []byte
	if err = s.db.QueryRowContext(ctx, `SELECT payload_enc FROM native_route_drafts
  WHERE inbound_id=1185 AND node_id='de'`).Scan(&cipherText); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(cipherText, []byte("nl-tunnel.example.org")) {
		t.Fatal("plaintext route host")
	}
	read, err := s.NativeRouteDraft(ctx, key, 1185, "de")
	if err != nil || read.SwapTunnelHost != "nl-tunnel.example.org" {
		t.Fatal("decrypt", read, err)
	}
	if _, err = s.NativeRouteDraft(ctx, bytes.Repeat([]byte{7}, 32), 1185, "de"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("wrong key accepted", err)
	}
	again, err := s.SaveNativeRouteDraft(ctx, key, in)
	if err != nil || !again.Replayed || again.Draft.DraftRevision != 1 {
		t.Fatal("idempotency", again, err)
	}
	updated := in
	updated.SwapTunnelHost = "different.example.org"
	if _, err = s.SaveNativeRouteDraft(ctx, key, updated); !errors.Is(err, ErrConflict) {
		t.Fatal("tampered event nonce accepted", err)
	}
	updated = in
	updated.EventID = "native-route-draft-stale-00001"
	if _, err = s.SaveNativeRouteDraft(ctx, key, updated); !errors.Is(err, ErrConflict) {
		t.Fatal("stale version accepted", err)
	}
	updated.ExpectedDraftRevision = 1
	receipt, err := s.SaveNativeRouteDraft(ctx, key, updated)
	if err != nil || receipt.Draft.DraftRevision != 2 {
		t.Fatal("CAS failed", receipt, err)
	}
}

func TestP0ERouteDraftNeverChangesAnyNativeTargetOrLedger(t *testing.T) {
	s, key, in := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []struct {
		node         string
		direct, swap uint16
	}{
		{"nl", 0, 26000}, {"de", 21185, 26001}, {"pl", 31185, 0},
	} {
		var direct, swap uint16
		if err := s.db.QueryRowContext(ctx, `SELECT direct_port,swap_port
   FROM native_inbound_targets WHERE inbound_id=1185 AND node_id=?`, expected.node).Scan(
			&direct, &swap); err != nil || direct != expected.direct || swap != expected.swap {
			t.Fatal("route draft mutated active Node target", expected.node, direct, swap, err)
		}
	}
	for _, table := range []string{"events", "clients", "native_node_revisions", "client_operations"} {
		var count int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&count); err != nil || count != 0 {
			t.Fatal("route draft modified historical data", table, count, err)
		}
	}
	if _, err := s.db.ExecContext(ctx, "DELETE FROM native_route_draft_events"); err == nil {
		t.Fatal("route draft audit deletable")
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE native_route_draft_events SET fingerprint='bad'"); err == nil {
		t.Fatal("route draft audit mutable")
	}
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != SchemaVersion {
		t.Fatal("schema migration changed rollback contract", version, err)
	}
}
func TestP0ERouteDraftRejectsUnsafeTopology(t *testing.T) {
	s, key, in := routeDraftFixture(t)
	for _, tc := range []struct {
		name string
		edit func(*NativeRouteDraftInput)
	}{
		{"swap_loop", func(x *NativeRouteDraftInput) { x.SwapUpstreamNodeID = "de" }},
		{"wrong_upstream_port", func(x *NativeRouteDraftInput) { x.SwapTunnelPort = 26002 }},
		{"shared_port", func(x *NativeRouteDraftInput) { x.SwapNodePort = x.DirectPort }},
		{"occupied_other_inbound", func(x *NativeRouteDraftInput) { x.DirectPort = 28080 }},
		{"reserved_hub_port", func(x *NativeRouteDraftInput) { x.DirectPort = 2087 }},
		{"unsafe_tunnel_host", func(x *NativeRouteDraftInput) { x.SwapTunnelHost = "https://example.org" }},
		{"stale_inbound_revision", func(x *NativeRouteDraftInput) { x.ExpectedInboundRevision = 8 }},
		{"wrong_agent_installation", func(x *NativeRouteDraftInput) { x.InstallationID = strings.Repeat("d", 32) }},
		{"implicit_direct_disable", func(x *NativeRouteDraftInput) { x.DirectPort = 0; x.DirectHosts = nil }},
		{"duplicate_hosts", func(x *NativeRouteDraftInput) { x.DirectHosts = []string{"de.example.org", "DE.EXAMPLE.ORG"} }},
		{"invalid_no_route", func(x *NativeRouteDraftInput) {
			x.DirectPort = 0
			x.DirectHosts = nil
			x.SwapEnabled = false
			x.SwapUpstreamNodeID = ""
			x.SwapTunnelHost = ""
			x.SwapTunnelPort = 0
			x.SwapNodePort = 0
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := in
			tc.edit(&input)
			if _, err := s.PreviewNativeRouteDraft(context.Background(), key, input); err == nil {
				t.Fatal("unsafe draft accepted")
			}
		})
	}
}
func TestP0ERouteDraftStaleRevisionBlocksReapply(t *testing.T) {
	s, key, in := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE native_inbounds SET revision=revision+1 WHERE id=1185`); err != nil {
		t.Fatal(err)
	}
	in.EventID = "native-route-draft-after-change-001"
	in.ExpectedDraftRevision = 1
	if _, err := s.PreviewNativeRouteDraft(ctx, key, in); !errors.Is(err, ErrConflict) {
		t.Fatal("stale input revision did not fail", err)
	}
}
