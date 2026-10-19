package business

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func p0fSwapInput(base NativeRouteDraftInput, node, install string, direct, nodePort uint16, upstream string, upstreamPort uint16, event string) NativeRouteDraftInput {
	return NativeRouteDraftInput{
		InboundID: base.InboundID,
		NodeID:    node, InstallationID: install,
		EventID: event, ExpectedInboundRevision: 1,
		DirectPort:  direct,
		SwapEnabled: true, SwapUpstreamNodeID: upstream,
		SwapTunnelHost: upstream + "-tunnel.example.org",
		SwapTunnelPort: upstreamPort, SwapNodePort: nodePort,
		Reason: "Owner reviewed protected multi-node draft graph",
	}
}
func TestP0FTwoNodeSwapLoopRejectedBeforeWrite(t *testing.T) {
	s, key, original := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, original); err != nil {
		t.Fatal(err)
	}
	reverse := p0fSwapInput(original, "nl", strings.Repeat("a", 32), 0, 26000, "de", 26001, "route-graph-reverse-0001")
	if _, err := s.PreviewNativeRouteDraft(ctx, key, reverse); !errors.Is(err, ErrConflict) {
		t.Fatalf("preview accepted circular Swap: %v", err)
	}
	if _, err := s.SaveNativeRouteDraft(ctx, key, reverse); !errors.Is(err, ErrConflict) {
		t.Fatalf("persisted circular Swap: %v", err)
	}
	var events int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM native_route_draft_events").Scan(&events); err != nil || events != 1 {
		t.Fatal("rejected graph unexpectedly changed committed draft audit", events, err)
	}
}
func TestP0FThreeNodeSwapLoopRejectedBeforeWrite(t *testing.T) {
	s, key, original := routeDraftFixture(t)
	ctx := context.Background()
	if err := s.PutInboundTarget(ctx, InboundTarget{InboundID: 1185, NodeID: "pl",
		InstallationID: strings.Repeat("c", 32), ListenIP: "198.51.100.30",
		DirectPort: 31185, SwapPort: 36000}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveNativeRouteDraft(ctx, key, original); err != nil {
		t.Fatal(err)
	}
	second := p0fSwapInput(original, "nl", strings.Repeat("a", 32), 0, 26000, "pl", 36000, "route-graph-second-0001")
	if _, err := s.SaveNativeRouteDraft(ctx, key, second); err != nil {
		t.Fatal(err)
	}
	third := p0fSwapInput(original, "pl", strings.Repeat("c", 32), 31185, 36000, "de", 26001, "route-graph-third-0001")
	if _, err := s.PreviewNativeRouteDraft(ctx, key, third); !errors.Is(err, ErrConflict) {
		t.Fatal("three-hop circular path accepted", err)
	}
	if _, err := s.SaveNativeRouteDraft(ctx, key, third); !errors.Is(err, ErrConflict) {
		t.Fatal("three-hop circular path persisted", err)
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM native_route_draft_events").Scan(&count); err != nil || count != 2 {
		t.Fatal("unexpected metadata write after blocked multi-hop path", count, err)
	}
}

func TestP0FModifiedEncryptedUpstreamBlocksNewDraft(t *testing.T) {
	s, key, original := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, original); err != nil {
		t.Fatal(err)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE native_route_drafts
 SET payload_enc=zeroblob(length(payload_enc))
 WHERE inbound_id=1185 AND node_id='de'`)
	if err != nil {
		t.Fatal(err)
	}
	reverse := p0fSwapInput(original, "nl", strings.Repeat("a", 32), 0, 26000, "de", 26001, "route-graph-tampered-0001")
	if _, err = s.PreviewNativeRouteDraft(ctx, key, reverse); !errors.Is(err, ErrUnavailable) {
		t.Fatal("tampered encrypted saved graph should fail closed", err)
	}
	if _, err = s.SaveNativeRouteDraft(ctx, key, reverse); !errors.Is(err, ErrUnavailable) {
		t.Fatal("tampered graph accepted for write", err)
	}
}
func p0fIdentityFingerprint(ctx context.Context, s *Store) (string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,uuid,charged_volume_bytes,charged_unlimited_units
 FROM clients ORDER BY id`)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	hash := sha256.New()
	for rows.Next() {
		var id, uuid string
		var vol, units int64
		if err = rows.Scan(&id, &uuid, &vol, &units); err != nil {
			return "", err
		}
		_, _ = fmt.Fprintf(hash, "%s|%s|%d|%d\n", id, uuid, vol, units)
	}
	if err = rows.Err(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}
func TestP0F750NativeClientIdentitiesUnchangedByRouteDraft(t *testing.T) {
	s, key, in := routeDraftFixture(t)
	ctx := context.Background()
	ids := make([]string, 0, 750)
	for i := 0; i < 750; i++ {
		id := fmt.Sprintf("p0f-client-%04d", i)
		created, err := s.CreateClient(ctx, NewClient{
			EventID:  fmt.Sprintf("p0f-client-created-%04d", i),
			ClientID: id, RepresentativeID: "owner",
			Name: "Synthetic client", DurationDays: 30, VolumeBytes: 1024,
		})
		if err != nil {
			t.Fatal("synthetic client create", i, err)
		}
		ids = append(ids, created.ID)
	}
	// Respect the existing production safety limit of 500 IDs per bulk action.
	for first := 0; first < len(ids); first += maxGroupMembersPerRequest {
		last := first + maxGroupMembersPerRequest
		if last > len(ids) {
			last = len(ids)
		}
		if err := s.BindInboundClients(ctx, 1185, ids[first:last], true); err != nil {
			t.Fatal(err)
		}
	}
	before, err := p0fIdentityFingerprint(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	var debitEvents int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&debitEvents); err != nil || debitEvents != 750 {
		t.Fatal("synthetic financial baseline missing", err, debitEvents)
	}
	if _, err = s.SaveNativeRouteDraft(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	after, err := p0fIdentityFingerprint(ctx, s)
	if err != nil || before != after {
		t.Fatal("route draft mutated client identity or charge baseline", err)
	}
	var events, count, revisionCount int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events`).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_inbound_clients`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_node_revisions`).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	if events != debitEvents || count != 750 || revisionCount != 0 {
		t.Fatal("route draft unexpectedly altered ledger, bindings or runtime", events, count, revisionCount)
	}
}

func TestP0FTamperedPortIndexRejectedBeforePlanning(t *testing.T) {
	s, key, original := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, original); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE native_route_drafts SET swap_node_port=28080 WHERE inbound_id=1185 AND node_id='de'"); err != nil {
		t.Fatal(err)
	}
	reverse := p0fSwapInput(original, "nl", strings.Repeat("a", 32), 0, 26000, "de", 26001, "route-graph-tampered-ports-001")
	if _, err := s.PreviewNativeRouteDraft(ctx, key, reverse); !errors.Is(err, ErrUnavailable) {
		t.Fatal("changed index bypassed signed route metadata", err)
	}
	if _, err := s.SaveNativeRouteDraft(ctx, key, reverse); !errors.Is(err, ErrUnavailable) {
		t.Fatal("changed indexed port was committed", err)
	}
}
func TestP0FReplacedPreviouslyDraftedNodeFailsClosed(t *testing.T) {
	s, key, original := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, original); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "UPDATE native_inbound_targets SET installation_id=? WHERE inbound_id=1185 AND node_id='de'", strings.Repeat("d", 32)); err != nil {
		t.Fatal(err)
	}
	reverse := p0fSwapInput(original, "nl", strings.Repeat("a", 32), 0, 26000, "de", 26001, "route-graph-replaced-old-0001")
	if _, err := s.PreviewNativeRouteDraft(ctx, key, reverse); !errors.Is(err, ErrConflict) {
		t.Fatal("stale previously paired installation was trusted", err)
	}
}
func TestP0FUnrelatedPreexistingCycleBlocksNewRoute(t *testing.T) {
	s, key, original := routeDraftFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeRouteDraft(ctx, key, original); err != nil {
		t.Fatal(err)
	}
	// A corrupt or pre-versioned graph might have been imported from a
	// previous internal snapshot. It must not be ignored if a third Node is edited.
	malformed := p0fSwapInput(original, "nl", strings.Repeat("a", 32), 0, 26000, "de", 26001, "route-graph-preexisting-cycle01")
	draft := NativeRouteDraft{NativeRouteDraftInput: malformed, DraftRevision: 1,
		ListenIP: "198.51.100.10", NativeDirectPort: 0, NativeSwapPort: 26000,
		BillingSourceNodeID: "nl", CreatedUnix: 1, UpdatedUnix: 1}
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := sealRouteDraft(key, 1185, "nl", 1, raw)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO native_route_drafts
  (inbound_id,node_id,installation_id,revision,inbound_revision,listen_ip,
   original_direct_port,original_swap_port,direct_port,tunnel_port,swap_node_port,
   last_event_id,payload_enc,created_unix,updated_unix)
  VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		1185, "nl", strings.Repeat("a", 32), 1, 1, "198.51.100.10",
		0, 26000, 0, 0, 26000, malformed.EventID, cipher, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	// PL->NL itself has no circular path. The independent DE<->NL cycle
	// should nevertheless block all new route planning.
	candidate := p0fSwapInput(original, "pl", strings.Repeat("c", 32), 31185, 36000, "nl", 26000, "route-graph-unrelated-0001")
	if _, err := s.PreviewNativeRouteDraft(ctx, key, candidate); !errors.Is(err, ErrConflict) {
		t.Fatal("unrelated existing cycle was silently ignored", err)
	}
}
