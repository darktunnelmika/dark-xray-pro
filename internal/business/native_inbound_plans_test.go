package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func creationPlanFixture(t *testing.T) (*Store, []byte, NativeInboundPlanInput) {
	t.Helper()
	s, _ := opened(t)
	in := NativeInboundPlanInput{EventID: "creation-plan-event-0001", Profile: nativeInput(), Reason: "Owner-reviewed full setup"}
	in.Profile.ID = 501
	in.Nodes = []NativeInboundPlanNode{
		{NodeID: "nl", InstallationID: strings.Repeat("a", 32), ListenIP: "198.51.100.10",
			DirectPort: 21185, DirectHosts: []string{"nl.example.org"}, TunnelPort: 26185, TunnelHosts: []string{"nl-tunnel.example.org"}},
		{NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20",
			DirectPort: 21186, DirectHosts: []string{"de.example.org"}, TunnelPort: 26186, TunnelHosts: []string{"de-tunnel.example.org"},
			SwapEnabled: true, SwapSourceNodeID: "nl", SwapSourceInstallationID: strings.Repeat("a", 32), SwapSourceListenIP: "198.51.100.10",
			SwapSourcePort: 31000, SwapEntryAddress: "iran-entry.example.org", SwapEntryPort: 443, SwapDestinationPort: 26186},
	}
	return s, bytes.Repeat([]byte{0xa9}, 32), in
}

func planGeneration(t *testing.T, s *Store) uint64 {
	t.Helper()
	var n uint64
	if err := s.db.QueryRow(`SELECT value FROM native_config_generation WHERE id=1`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func planCount(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func copiedPlan(in NativeInboundPlanInput) NativeInboundPlanInput {
	raw, _ := json.Marshal(in)
	var out NativeInboundPlanInput
	_ = json.Unmarshal(raw, &out)
	return out
}

func TestCreationPlanAtomicDisabledStoreReadPreviewAndImmutableReplay(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	initialGeneration := planGeneration(t, s)
	preview, err := s.PreviewNativeInboundPlan(ctx, key, in)
	if err != nil || !preview.Valid || !preview.TopologyValidated || !preview.TopologyValidationOnly || preview.TopologyKeyScope != "preview_only" ||
		len(preview.Topology.Nodes) != 2 || preview.Plan.Profile.Enabled || preview.Plan.Profile.PublicKey != "" || preview.RuntimeChanged {
		t.Fatal("unsafe preview", preview, err)
	}
	if planCount(t, s, "native_inbounds") != 0 || planCount(t, s, "native_inbound_plans") != 0 || planCount(t, s, "native_inbound_plan_events") != 0 || planGeneration(t, s) != initialGeneration {
		t.Fatal("preview persisted an identity, plan or runtime change")
	}
	raw, _ := json.Marshal(preview)
	for _, forbidden := range []string{"privateKey", "reality_private_key", "dokodemo-door", "\"config\"", "\"can_activate\":true"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatal("preview leaked private config or claimed activation", forbidden)
		}
	}
	result, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || result.Plan.Profile.Enabled || result.Plan.Profile.Archived || result.Plan.PlanRevision != 1 || result.Plan.InboundRevision != 1 ||
		result.Plan.Profile.PublicKey == "" || result.Plan.Status != "SAVED_DISABLED" || !result.Plan.Editable || result.RuntimeChanged || result.CreditChanged || result.UsageReset || result.ClientsDeleted || result.SubscriptionPublished {
		t.Fatal("invalid creation receipt", result, err)
	}
	for _, table := range []string{"native_inbound_targets", "native_inbound_clients", "native_runtime_delivery", "native_runtime_reports", "native_node_revisions", "events", "clients"} {
		if planCount(t, s, table) != 0 {
			t.Fatal("creation mutated operational table", table)
		}
	}
	if planGeneration(t, s) != initialGeneration {
		t.Fatal("save invalidated existing native generation")
	}
	var ciphertext []byte
	if err = s.db.QueryRow(`SELECT payload_enc FROM native_inbound_plans WHERE inbound_id=501`).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("iran-entry.example.org")) || bytes.Contains(ciphertext, []byte("198.51.100.20")) {
		t.Fatal("plan stored in plaintext")
	}
	if err = s.db.QueryRow(`SELECT receipt_enc FROM native_inbound_plan_events WHERE event_id=?`, in.EventID).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(ciphertext, []byte("iran-entry.example.org")) {
		t.Fatal("receipt stored in plaintext")
	}
	loaded, err := s.NativeInboundPlan(ctx, key, 501)
	if err != nil || !reflect.DeepEqual(loaded, result.Plan) {
		t.Fatal("read changed plan", err)
	}
	if _, err = s.NativeInboundPlan(ctx, bytes.Repeat([]byte{3}, 32), 501); !errors.Is(err, ErrUnavailable) {
		t.Fatal("wrong decryption key accepted", err)
	}
	replay, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(replay.Plan, result.Plan) || planCount(t, s, "native_inbounds") != 1 {
		t.Fatal("exact replay changed identity", replay, err)
	}
	tampered := copiedPlan(in)
	tampered.Nodes[0].DirectPort++
	if _, err = s.SaveNativeInboundPlan(ctx, key, tampered); !errors.Is(err, ErrConflict) {
		t.Fatal("event reused for a different plan", err)
	}
	if _, err = s.db.Exec(`UPDATE native_inbound_plan_events SET fingerprint='changed' WHERE event_id=?`, in.EventID); err == nil {
		t.Fatal("receipt update permitted")
	}
	if _, err = s.db.Exec(`DELETE FROM native_inbound_plan_events WHERE event_id=?`, in.EventID); err == nil {
		t.Fatal("receipt delete permitted")
	}
}

func TestCreationPlanUpdatePreservesKeyAndRuntimeGeneration(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	created, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil {
		t.Fatal(err)
	}
	_, originalPrivate, err := readNativeInbound(ctx, s.db, key, 501)
	if err != nil {
		t.Fatal(err)
	}
	generation := planGeneration(t, s)
	update := copiedPlan(in)
	update.EventID = "creation-plan-route-update-0001"
	update.ExpectedPlanRevision = 1
	update.ExpectedInboundRevision = 1
	update.Nodes[1].SwapEntryAddress = "changed-entry.example.org"
	routeOnly, err := s.SaveNativeInboundPlan(ctx, key, update)
	if err != nil || routeOnly.Plan.PlanRevision != 2 || routeOnly.Plan.InboundRevision != 1 || routeOnly.Plan.Profile.PublicKey != created.Plan.Profile.PublicKey || planGeneration(t, s) != generation {
		t.Fatal("route-only edit touched native profile/runtime", routeOnly, err)
	}
	edit := copiedPlan(update)
	edit.EventID = "creation-plan-profile-update-0001"
	edit.ExpectedPlanRevision = 2
	edit.Profile.Name = "Edited paused creation"
	edit.Profile.Transport = "grpc"
	edit.Profile.GRPCService = "updated_grpc"
	profileEdit, err := s.SaveNativeInboundPlan(ctx, key, edit)
	if err != nil || profileEdit.Plan.PlanRevision != 3 || profileEdit.Plan.InboundRevision != 2 || planGeneration(t, s) != generation {
		t.Fatal("paused unbound profile edit invalidated runtime", profileEdit, err)
	}
	actual, private, err := readNativeInbound(ctx, s.db, key, 501)
	if err != nil || private.RealityPrivateKey != originalPrivate.RealityPrivateKey || actual.PublicKey != created.Plan.Profile.PublicKey || actual.Name != edit.Profile.Name || actual.Enabled {
		t.Fatal("update rotated key or failed to update real profile", actual, err)
	}
	replay, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(replay.Plan, created.Plan) {
		t.Fatal("retry returned a mutable latest receipt", replay, err)
	}
	current, err := s.NativeInboundPlan(ctx, key, 501)
	if err != nil || current.PlanRevision != 3 {
		t.Fatal("historical retry reverted current plan", current, err)
	}
	stale := copiedPlan(edit)
	stale.EventID = "creation-plan-stale-update-0001"
	if _, err = s.SaveNativeInboundPlan(ctx, key, stale); !errors.Is(err, ErrConflict) {
		t.Fatal("stale edit accepted", err)
	}
	if planGeneration(t, s) != generation || planCount(t, s, "native_inbound_plan_events") != 3 {
		t.Fatal("rejected/replayed operation changed state")
	}
	// Ordinary active updates still invalidate delivery evidence.
	active := nativeInput()
	active.ID = 700
	if _, err = s.CreateInbound(ctx, key, active); err != nil {
		t.Fatal(err)
	}
	active.Name = "Active ordinary edit"
	if _, err = s.MutateInbound(ctx, key, 700, InboundMutation{EventID: "active-profile-edit-0001", ExpectedRevision: 1, Action: "edit", Reason: "Test active invalidation", Profile: &active}); err != nil {
		t.Fatal(err)
	}
	if planGeneration(t, s) != generation+1 {
		t.Fatal("active profile invalidation was suppressed")
	}
	if _, err = s.MutateInbound(ctx, key, 700, InboundMutation{EventID: "ordinary-profile-disable-0001", ExpectedRevision: 2, Action: "disable", Reason: "Ordinary profile pause"}); err != nil {
		t.Fatal(err)
	}
	active.Name = "Disabled ordinary profile still follows existing invalidation"
	if _, err = s.MutateInbound(ctx, key, 700, InboundMutation{EventID: "ordinary-disabled-edit-0001", ExpectedRevision: 3, Action: "edit", Reason: "Ordinary profile edit", Profile: &active}); err != nil {
		t.Fatal(err)
	}
	if planGeneration(t, s) != generation+3 {
		t.Fatal("non-plan disabled profile invalidation was suppressed")
	}
}

func TestCreationPlanRollsBackProfileAndReceiptOnLateFailure(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	generation := planGeneration(t, s)
	if _, err := s.db.Exec(`CREATE TRIGGER test_creation_receipt_failure BEFORE INSERT ON native_inbound_plan_events BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveNativeInboundPlan(ctx, key, in); err == nil {
		t.Fatal("injected failure did not fail")
	}
	for _, table := range []string{"native_inbounds", "native_inbound_plans", "native_inbound_plan_events", "native_inbound_targets"} {
		if planCount(t, s, table) != 0 {
			t.Fatal("partial creation survived rollback", table)
		}
	}
	if planGeneration(t, s) != generation {
		t.Fatal("rollback changed runtime generation")
	}
	if _, err := s.db.Exec(`DROP TRIGGER test_creation_receipt_failure`); err != nil {
		t.Fatal(err)
	}
	created, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil {
		t.Fatal(err)
	}
	update := copiedPlan(in)
	update.EventID = "creation-failed-update-0001"
	update.ExpectedPlanRevision = 1
	update.ExpectedInboundRevision = 1
	update.Profile.Name = "Must roll back"
	if _, err = s.db.Exec(`CREATE TRIGGER test_creation_receipt_failure BEFORE INSERT ON native_inbound_plan_events BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveNativeInboundPlan(ctx, key, update); err == nil {
		t.Fatal("late update failure did not fail")
	}
	current, err := s.NativeInboundPlan(ctx, key, 501)
	if err != nil || !reflect.DeepEqual(current, created.Plan) || planGeneration(t, s) != generation {
		t.Fatal("failed update partially committed profile/plan", current, err)
	}
}

func TestCreationPlanWaitingHostsAndUniqueHostLimit(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	in.Nodes[0].TunnelHosts = nil
	in.Nodes[1].SwapEntryAddress = ""
	result, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || result.Plan.Status != "WAITING_HOST" || result.Plan.Profile.Enabled || result.Plan.RuntimeApply {
		t.Fatal("missing explicit host was made ready or discarded", result, err)
	}
	update := copiedPlan(in)
	update.EventID = "creation-host-complete-0001"
	update.ExpectedPlanRevision = 1
	update.ExpectedInboundRevision = 1
	update.Nodes[1].SwapEntryAddress = "entry-complete.example.org"
	hosts := make([]string, 64)
	for i := range hosts {
		hosts[i] = fmt.Sprintf("host-%d.example.org", i)
	}
	update.Nodes[0].DirectHosts = append([]string(nil), hosts...)
	update.Nodes[0].TunnelHosts = append([]string(nil), hosts...)
	ready, err := s.SaveNativeInboundPlan(ctx, key, update)
	if err != nil || ready.Plan.Status != "SAVED_DISABLED" {
		t.Fatal("64 unique hosts used on two paths rejected", err)
	}
	update.EventID = "creation-host-overflow-0001"
	update.ExpectedPlanRevision = 2
	update.Nodes[0].TunnelHosts[0] = "host-extra.example.org"
	if _, err = s.SaveNativeInboundPlan(ctx, key, update); !errors.Is(err, ErrInvalid) {
		t.Fatal("65 unique hosts accepted", err)
	}
}

func TestCreationPlanRejectsWholeTopologyConflictsAndKeepsPathsIndependent(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*NativeInboundPlanInput)
		want   error
	}{
		{"source ordinary tunnel is not relay", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapSourcePort = in.Nodes[0].TunnelPort }, ErrConflict},
		{"destination direct is not swap", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapDestinationPort = in.Nodes[1].DirectPort }, ErrConflict},
		{"ordinary listener conflict", func(in *NativeInboundPlanInput) { in.Nodes[0].TunnelPort = in.Nodes[0].DirectPort }, ErrConflict},
		{"self relay", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapSourceNodeID = "de" }, ErrInvalid},
		{"source installation mismatch", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapSourceInstallationID = strings.Repeat("c", 32) }, ErrConflict},
		{"source bind mismatch", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapSourceListenIP = "198.51.100.99" }, ErrConflict},
		{"source protected port", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapSourcePort = 8443 }, ErrInvalid},
		{"public entry includes port", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapEntryAddress = "entry.example.org:443" }, ErrInvalid},
		{"missing external port", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapEntryPort = 0 }, ErrInvalid},
		{"wildcard source", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapSourceListenIP = "0.0.0.0" }, ErrInvalid},
		{"duplicate node", func(in *NativeInboundPlanInput) { in.Nodes = append(in.Nodes, in.Nodes[0]) }, ErrInvalid},
		{"duplicate host in path", func(in *NativeInboundPlanInput) {
			in.Nodes[0].DirectHosts = []string{"DUP.example.org", "dup.example.org"}
		}, ErrInvalid},
		{"disabled settings", func(in *NativeInboundPlanInput) { in.Nodes[1].SwapEnabled = false }, ErrInvalid},
		{"unsupported transport", func(in *NativeInboundPlanInput) { in.Profile.Transport = "ws" }, ErrInvalid},
		{"node graph cycle", func(in *NativeInboundPlanInput) {
			n := &in.Nodes[0]
			n.SwapEnabled = true
			n.SwapSourceNodeID = "de"
			n.SwapSourceInstallationID = strings.Repeat("b", 32)
			n.SwapSourceListenIP = "198.51.100.20"
			n.SwapSourcePort = 31001
			n.SwapDestinationPort = 26185
			n.SwapEntryAddress = "return-entry.example.org"
			n.SwapEntryPort = 443
		}, ErrConflict},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, key, in := creationPlanFixture(t)
			tc.mutate(&in)
			if _, err := s.SaveNativeInboundPlan(context.Background(), key, in); !errors.Is(err, tc.want) {
				t.Fatal("bad topology accepted or wrong category", err)
			}
			if planCount(t, s, "native_inbounds") != 0 || planCount(t, s, "native_inbound_plans") != 0 {
				t.Fatal("invalid topology partially saved")
			}
		})
	}
	t.Run("dedicated destination coexists with ordinary Tunnel", func(t *testing.T) {
		s, key, in := creationPlanFixture(t)
		in.Nodes[1].SwapDestinationPort = 32000
		preview, err := s.PreviewNativeInboundPlan(context.Background(), key, in)
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range preview.Topology.Nodes {
			if node.NodeID == "de" && len(node.Candidate.Listeners) != 3 {
				t.Fatal("ordinary path was overwritten", node.Candidate.Listeners)
			}
		}
	})
	t.Run("source need not be a selected destination", func(t *testing.T) {
		s, key, in := creationPlanFixture(t)
		in.Nodes = in.Nodes[1:]
		preview, err := s.PreviewNativeInboundPlan(context.Background(), key, in)
		if err != nil || len(preview.Topology.Nodes) != 2 {
			t.Fatal("relay-only source omitted", preview, err)
		}
		for _, node := range preview.Topology.Nodes {
			if node.NodeID == "nl" && (len(node.Candidate.Listeners) != 1 || node.Candidate.Listeners[0].Kind != "swap_relay") {
				t.Fatal("relay-only source became a customer listener", node)
			}
		}
	})
}

func TestCreationPlanReservationsCoverNativeDraftAndOtherPlanRelayPorts(t *testing.T) {
	for _, source := range []string{"native", "route_draft", "creation_plan"} {
		t.Run(source, func(t *testing.T) {
			s, key, in := creationPlanFixture(t)
			ctx := context.Background()
			switch source {
			case "native", "route_draft":
				profile := nativeInput()
				profile.ID = 600
				if _, err := s.CreateInbound(ctx, key, profile); err != nil {
					t.Fatal(err)
				}
				port := uint16(31000)
				if source == "route_draft" {
					port = 35000
				}
				if err := s.PutInboundTarget(ctx, InboundTarget{InboundID: 600, NodeID: "nl", InstallationID: strings.Repeat("a", 32), ListenIP: "198.51.100.10", DirectPort: port}); err != nil {
					t.Fatal(err)
				}
				if source == "route_draft" {
					_, err := s.SaveNativeRouteDraft(ctx, key, NativeRouteDraftInput{EventID: "creation-reserve-draft-0001", InboundID: 600, NodeID: "nl", InstallationID: strings.Repeat("a", 32),
						ExpectedInboundRevision: 1, DirectPort: 35000, DirectHosts: []string{"existing.example.org"}, TunnelPort: 31000, TunnelHosts: []string{"existing-tunnel.example.org"}, Reason: "Existing route reservation"})
					if err != nil {
						t.Fatal(err)
					}
				}
			case "creation_plan":
				other := copiedPlan(in)
				other.Profile.ID = 600
				other.EventID = "other-creation-plan-0001"
				other.Nodes = other.Nodes[1:]
				other.Nodes[0].DirectPort = 35000
				other.Nodes[0].TunnelPort = 35001
				other.Nodes[0].SwapDestinationPort = 35001
				if _, err := s.SaveNativeInboundPlan(ctx, key, other); err != nil {
					t.Fatal(err)
				}
			}
			beforeGeneration := planGeneration(t, s)
			if _, err := s.SaveNativeInboundPlan(ctx, key, in); !errors.Is(err, ErrConflict) {
				t.Fatal("relay source collided with reservation", source, err)
			}
			if _, err := s.NativeInbound(ctx, key, in.Profile.ID); !errors.Is(err, ErrNotFound) {
				t.Fatal("conflict left a native profile", err)
			}
			if planGeneration(t, s) != beforeGeneration {
				t.Fatal("collision validation changed runtime")
			}
		})
	}
}

func TestCreationPlanUpdateFencesMaterializationAndAuthenticatesMetadata(t *testing.T) {
	for _, mutation := range []string{"enabled", "archived", "targeted", "bound", "externally_edited"} {
		t.Run(mutation, func(t *testing.T) {
			s, key, in := creationPlanFixture(t)
			ctx := context.Background()
			if _, err := s.SaveNativeInboundPlan(ctx, key, in); err != nil {
				t.Fatal(err)
			}
			generation := planGeneration(t, s)
			switch mutation {
			case "enabled":
				// Simulate foreign historical state predating the durable guard.
				if _, err := s.db.Exec(`DROP TRIGGER native_plan_block_enable`); err != nil {
					t.Fatal(err)
				}
				if _, err := s.db.Exec(`UPDATE native_inbounds SET enabled=1,revision=revision+1 WHERE id=501`); err != nil {
					t.Fatal(err)
				}
			case "archived":
				if _, err := s.MutateInbound(ctx, key, 501, InboundMutation{EventID: "creation-lifecycle-change-0001", ExpectedRevision: 1, Action: "archive", Reason: "Explicit logical cancellation"}); err != nil {
					t.Fatal(err)
				}
				if planGeneration(t, s) != generation+1 {
					t.Fatal("lifecycle invalidation suppressed")
				}
			case "targeted":
				if _, err := s.db.Exec(`DROP TRIGGER native_plan_block_target_insert`); err != nil {
					t.Fatal(err)
				}
				if _, err := s.db.Exec(`INSERT INTO native_inbound_targets VALUES(501,'nl',?,'198.51.100.10',21185,0)`, strings.Repeat("a", 32)); err != nil {
					t.Fatal(err)
				}
			case "bound":
				newRep(t, s, "creation-bound-rep")
				grant(t, s, "creation-bound-rep", "creation-bound-credit-0001", 1000, 0)
				client, err := s.CreateClient(ctx, NewClient{EventID: "creation-bound-client-0001", ClientID: "creation-bound-client", RepresentativeID: "creation-bound-rep", Name: "Bound client", DurationDays: 1, VolumeBytes: 10})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = s.db.Exec(`DROP TRIGGER native_plan_block_client_bind`); err != nil {
					t.Fatal(err)
				}
				if _, err = s.db.Exec(`INSERT INTO native_inbound_clients VALUES(501,?)`, client.ID); err != nil {
					t.Fatal(err)
				}
			case "externally_edited":
				if _, err := s.db.Exec(`UPDATE native_inbounds SET name='Foreign profile edit',revision=revision+1 WHERE id=501`); err != nil {
					t.Fatal(err)
				}
			}
			if mutation == "targeted" || mutation == "bound" {
				before := planGeneration(t, s)
				if _, err := s.db.Exec(`UPDATE native_inbounds SET name='Foreign materialized profile edit',revision=revision+1 WHERE id=501`); err != nil {
					t.Fatal(err)
				}
				if planGeneration(t, s) != before+1 {
					t.Fatal("targeted or bound profile invalidation was suppressed")
				}
			}
			loaded, err := s.NativeInboundPlan(ctx, key, 501)
			if err != nil || loaded.Editable || loaded.EditBlocker == "" {
				t.Fatal("changed profile was advertised editable", loaded, err)
			}
			update := copiedPlan(in)
			update.EventID = "creation-fenced-update-0001"
			update.ExpectedPlanRevision = 1
			update.ExpectedInboundRevision = 1
			if _, err = s.SaveNativeInboundPlan(ctx, key, update); !errors.Is(err, ErrConflict) {
				t.Fatal("materialized/external state was overwritten", err)
			}
		})
	}
	t.Run("authenticated metadata", func(t *testing.T) {
		s, key, in := creationPlanFixture(t)
		ctx := context.Background()
		if _, err := s.SaveNativeInboundPlan(ctx, key, in); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`UPDATE native_inbound_plans SET status='WAITING_HOST' WHERE inbound_id=501`); err != nil {
			t.Fatal(err)
		}
		if _, err := s.NativeInboundPlan(ctx, key, 501); !errors.Is(err, ErrUnavailable) {
			t.Fatal("indexed metadata did not match ciphertext but was accepted", err)
		}
	})
}

func TestCreationPlanGenericAndRollbackSQLCannotMaterialize(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	created, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil {
		t.Fatal(err)
	}
	client, err := s.CreateClient(ctx, NewClient{EventID: "creation-boundary-client-0001", ClientID: "creation-boundary-client", RepresentativeID: "owner", Name: "Existing owner client", DurationDays: 1, VolumeBytes: 10})
	if err != nil {
		t.Fatal(err)
	}
	ordinary := nativeInput()
	ordinary.ID = 700
	if _, err = s.CreateInbound(ctx, key, ordinary); err != nil {
		t.Fatal(err)
	}
	if err = s.PutInboundTarget(ctx, InboundTarget{InboundID: 700, NodeID: "ordinary", InstallationID: strings.Repeat("c", 32), ListenIP: "198.51.100.30", DirectPort: 35000}); err != nil {
		t.Fatal("ordinary target changed", err)
	}
	if err = s.BindInboundClients(ctx, 700, []string{client.ID}, true); err != nil {
		t.Fatal("ordinary binding changed", err)
	}
	generation := planGeneration(t, s)
	for _, action := range []string{"enable", "edit"} {
		mutation := InboundMutation{EventID: "creation-generic-" + action + "-0001", ExpectedRevision: 1, Action: action, Reason: "Attempt generic materialization"}
		if action == "edit" {
			profile := in.Profile
			profile.Name = "Bypassed edit"
			mutation.Profile = &profile
		}
		if _, err = s.MutateInbound(ctx, key, 501, mutation); !errors.Is(err, ErrConflict) {
			t.Fatal("generic lifecycle bypass", action, err)
		}
	}
	if err = s.PutInboundTarget(ctx, InboundTarget{InboundID: 501, NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20", DirectPort: 21186}); !errors.Is(err, ErrConflict) {
		t.Fatal("generic target bypass", err)
	}
	if err = s.BindInboundClients(ctx, 501, []string{client.ID}, true); !errors.Is(err, ErrConflict) {
		t.Fatal("generic binding bypass", err)
	}
	// These are the write forms used by an older binary unaware of plan
	// ownership. Persistent guards must reject them without Store cooperation.
	for _, stmt := range []string{
		`UPDATE native_inbounds SET enabled=1,revision=revision+1 WHERE id=501`,
		`INSERT INTO native_inbound_targets VALUES(501,'de','bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb','198.51.100.20',21186,0)`,
		`UPDATE native_inbound_targets SET inbound_id=501 WHERE inbound_id=700`,
		`INSERT INTO native_inbound_clients VALUES(501,'creation-boundary-client')`,
		`UPDATE native_inbound_clients SET inbound_id=501 WHERE inbound_id=700`,
	} {
		if _, err = s.db.Exec(stmt); err == nil {
			t.Fatal("rollback SQL bypassed staged authoring boundary")
		}
	}
	if planGeneration(t, s) != generation {
		t.Fatal("rejected materialization dirtied runtime")
	}
	loaded, err := s.NativeInboundPlan(ctx, key, 501)
	if err != nil || !reflect.DeepEqual(loaded, created.Plan) {
		t.Fatal("rejected bypass changed saved plan", err)
	}
	// Logical cancellation releases reservations, preserves the record and
	// receipt, and leaves the cancelled plan readable but no longer editable.
	if _, err = s.MutateInbound(ctx, key, 501, InboundMutation{EventID: "creation-logical-cancel-0001", ExpectedRevision: 1, Action: "archive", Reason: "Cancel this staged setup"}); err != nil {
		t.Fatal(err)
	}
	loaded, err = s.NativeInboundPlan(ctx, key, 501)
	if err != nil || loaded.Editable || loaded.EditBlocker == "" {
		t.Fatal("cancelled plan unreadable or editable", loaded, err)
	}
	other := copiedPlan(in)
	other.Profile.ID = 502
	other.EventID = "creation-after-cancel-0001"
	if _, err = s.SaveNativeInboundPlan(ctx, key, other); err != nil {
		t.Fatal("cancelled plan retained port/entry reservations", err)
	}
	if replay, err := s.SaveNativeInboundPlan(ctx, key, in); err != nil || !replay.Replayed || !reflect.DeepEqual(replay.Plan, created.Plan) {
		t.Fatal("cancel erased immutable receipt", replay, err)
	}
}

func TestCreationPlanExternalEntryMappingConflicts(t *testing.T) {
	t.Run("same batch", func(t *testing.T) {
		s, key, in := creationPlanFixture(t)
		third := in.Nodes[1]
		third.NodeID = "fr"
		third.InstallationID = strings.Repeat("c", 32)
		third.ListenIP = "198.51.100.30"
		third.SwapSourcePort = 31001
		in.Nodes = append(in.Nodes, third)
		_, err := s.SaveNativeInboundPlan(context.Background(), key, in)
		var issue *NativeInboundPlanError
		if !errors.As(err, &issue) || issue.Code != "entry_mapping_conflict" {
			t.Fatal("one public endpoint mapped to different relay ports", err)
		}
	})
	t.Run("other saved plan", func(t *testing.T) {
		s, key, in := creationPlanFixture(t)
		ctx := context.Background()
		if _, err := s.SaveNativeInboundPlan(ctx, key, in); err != nil {
			t.Fatal(err)
		}
		other := copiedPlan(in)
		other.Profile.ID = 602
		other.EventID = "creation-entry-second-0001"
		other.Nodes = other.Nodes[1:]
		other.Nodes[0].DirectPort = 35000
		other.Nodes[0].TunnelPort = 35001
		other.Nodes[0].SwapDestinationPort = 35001
		other.Nodes[0].SwapSourcePort = 35002
		_, err := s.SaveNativeInboundPlan(ctx, key, other)
		var issue *NativeInboundPlanError
		if !errors.As(err, &issue) || issue.Code != "entry_mapping_conflict" {
			t.Fatal("saved public mapping was silently contradicted", err)
		}
		other.Nodes[0].SwapEntryPort = 444
		if _, err = s.SaveNativeInboundPlan(ctx, key, other); err != nil {
			t.Fatal("same domain with a different explicit entry port was rejected", err)
		}
	})
}
