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

func TestCreationPlanProfileFeedbackIdentifiesSubmittedField(t *testing.T) {
	cases := []struct {
		name, field, code string
		change            func(*InboundInput)
	}{
		{"name", "profile.name", "name_invalid", func(p *InboundInput) { p.Name = "bad\x00name" }},
		{"transport", "profile.transport", "transport_invalid", func(p *InboundInput) { p.Transport = "unsupported" }},
		{"grpc service", "profile.grpc_service", "grpc_service_invalid", func(p *InboundInput) { p.Transport, p.GRPCService = "grpc", "bad service" }},
		{"private target", "profile.reality_target", "target_invalid", func(p *InboundInput) { p.RealityTarget = "127.0.0.1:443" }},
		{"target hostname", "profile.reality_target", "target_invalid", func(p *InboundInput) { p.RealityTarget = "localhost:443" }},
		{"IP server name", "profile.server_names", "server_names_invalid", func(p *InboundInput) { p.ServerNames = []string{"127.0.0.1"} }},
		{"duplicate server names", "profile.server_names", "server_names_invalid", func(p *InboundInput) { p.ServerNames = []string{"one.example.org", "ONE.example.org"} }},
		{"too many server names", "profile.server_names", "server_names_invalid", func(p *InboundInput) {
			p.ServerNames = nil
			for i := 0; i < 17; i++ {
				p.ServerNames = append(p.ServerNames, fmt.Sprintf("server-%d.example.org", i))
			}
		}},
		{"short ID", "profile.short_ids", "short_ids_invalid", func(p *InboundInput) { p.ShortIDs = []string{"not-hex"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, key, in := creationPlanFixture(t)
			tc.change(&in.Profile)
			generation := planGeneration(t, s)
			for _, save := range []bool{false, true} {
				var err error
				if save {
					_, err = s.SaveNativeInboundPlan(context.Background(), key, in)
				} else {
					_, err = s.PreviewNativeInboundPlan(context.Background(), key, in)
				}
				var issue *NativeInboundPlanError
				if !errors.Is(err, ErrInvalid) || !errors.As(err, &issue) || issue.Field != tc.field || issue.Code != tc.code {
					t.Fatalf("save=%v: field error = %#v, err=%v", save, issue, err)
				}
				raw, err := json.Marshal(issue)
				if err != nil || string(raw) != fmt.Sprintf(`{"field":%q,"code":%q}`, tc.field, tc.code) {
					t.Fatal("profile error echoed submitted or private material", string(raw), err)
				}
			}
			if planCount(t, s, "native_inbounds") != 0 || planCount(t, s, "native_inbound_plans") != 0 || planGeneration(t, s) != generation {
				t.Fatal("rejected profile changed persistent state")
			}
		})
	}
}

func feedbackRelayPlan(in NativeInboundPlanInput) NativeInboundPlanInput {
	in = copiedPlan(in)
	in.Nodes = in.Nodes[1:]
	second := in.Nodes[0]
	second.NodeID, second.InstallationID, second.ListenIP = "fr", strings.Repeat("c", 32), "198.51.100.30"
	second.SwapSourcePort, second.SwapEntryAddress = 31001, "fr-entry.example.org"
	in.Nodes = append(in.Nodes, second)
	return in
}

func TestCreationPlanExistingCollisionFeedbackNamesActualListener(t *testing.T) {
	cases := []struct {
		name, node, destination, path, field, ip string
		port                                     uint16
	}{
		{"direct", "de", "de", "direct", "direct_port", "198.51.100.20", 21186},
		{"tunnel", "de", "de", "tunnel", "tunnel_port", "198.51.100.20", 26186},
		{"dedicated Swap destination", "de", "de", "swap_destination", "swap_destination_port", "198.51.100.20", 32000},
		{"second destination shared relay", "nl", "fr", "swap_source", "swap_source_port", "198.51.100.10", 31001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, key, in := creationPlanFixture(t)
			in = feedbackRelayPlan(in)
			in.Nodes[0].SwapDestinationPort = 32000
			ctx := context.Background()
			existing := nativeInput()
			existing.ID = 700
			if _, err := s.CreateInbound(ctx, key, existing); err != nil {
				t.Fatal(err)
			}
			installation := strings.Repeat("b", 32)
			if tc.node == "nl" {
				installation = strings.Repeat("a", 32)
			}
			if err := s.PutInboundTarget(ctx, InboundTarget{InboundID: 700, NodeID: tc.node, InstallationID: installation, ListenIP: tc.ip, DirectPort: tc.port}); err != nil {
				t.Fatal(err)
			}
			generation := planGeneration(t, s)
			for _, save := range []bool{false, true} {
				var err error
				if save {
					_, err = s.SaveNativeInboundPlan(ctx, key, in)
				} else {
					_, err = s.PreviewNativeInboundPlan(ctx, key, in)
				}
				var issue *NativeInboundPlanError
				if !errors.Is(err, ErrConflict) || !errors.As(err, &issue) || issue.NodeID != tc.node || issue.DestinationNodeID != tc.destination ||
					issue.Path != tc.path || issue.Port != tc.port || issue.Field != tc.field || issue.Code != "existing_listener_port_collision" {
					t.Fatalf("save=%v: incorrect collision owner: %#v, err=%v", save, issue, err)
				}
			}
			if planCount(t, s, "native_inbounds") != 1 || planCount(t, s, "native_inbound_plans") != 0 || planGeneration(t, s) != generation {
				t.Fatal("rejected reservation changed an existing Inbound or runtime")
			}
		})
	}
}

func TestCreationPlanSharedSourceConflictNamesSecondDestination(t *testing.T) {
	for _, binding := range []bool{false, true} {
		t.Run(fmt.Sprintf("binding=%v", binding), func(t *testing.T) {
			s, key, in := creationPlanFixture(t)
			in = feedbackRelayPlan(in)
			field, code := "swap_source_port", "listener_port_collision"
			if binding {
				in.Nodes[1].SwapSourceListenIP = "198.51.100.99"
				field, code = "swap_source_listen_ip", "node_binding_conflict"
			} else {
				in.Nodes[1].SwapSourcePort = in.Nodes[0].SwapSourcePort
			}
			_, err := s.PreviewNativeInboundPlan(context.Background(), key, in)
			var issue *NativeInboundPlanError
			if !errors.As(err, &issue) || issue.NodeID != "nl" || issue.DestinationNodeID != "fr" || issue.Path != "swap_source" || issue.Field != field || issue.Code != code {
				t.Fatalf("shared source did not identify second owner: %#v, err=%v", issue, err)
			}
		})
	}
}

func TestCreationPlanIndexAuthenticatesPlannedNodesWithoutMaterialization(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	in = feedbackRelayPlan(in)
	ctx := context.Background()
	if _, err := s.SaveNativeInboundPlan(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	want := []string{"de", "fr", "nl"}
	check := func() {
		t.Helper()
		index, err := s.ListNativeInboundPlans(ctx, key)
		if err != nil || len(index) != 1 || index[0].InboundID != in.Profile.ID || !reflect.DeepEqual(index[0].NodeIDs, want) {
			t.Fatal("planned destination/relay identity missing", index, err)
		}
		raw, err := json.Marshal(index)
		if err != nil || bytes.Contains(raw, []byte("example.org")) || bytes.Contains(raw, []byte("198.51.100")) || bytes.Contains(raw, []byte("public_key")) {
			t.Fatal("plan index exposed configuration", string(raw), err)
		}
		if planCount(t, s, "native_inbound_targets") != 0 || planCount(t, s, "native_inbound_clients") != 0 {
			t.Fatal("planned Node discovery materialized assignments")
		}
	}
	check()
	if result, err := s.ListNativeInboundPlans(ctx, bytes.Repeat([]byte{3}, 32)); !errors.Is(err, ErrUnavailable) || result != nil {
		t.Fatal("unauthenticated plan index accepted", result, err)
	}
	if _, err := s.MutateInbound(ctx, key, in.Profile.ID, InboundMutation{EventID: "feedback-archive-0001", ExpectedRevision: 1, Action: "archive", Reason: "Archive without losing plan discovery"}); err != nil {
		t.Fatal(err)
	}
	check()
	var encrypted []byte
	if err := s.db.QueryRow(`SELECT payload_enc FROM native_inbound_plans WHERE inbound_id=?`, in.Profile.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	encrypted[len(encrypted)-1] ^= 1
	if _, err := s.db.Exec(`UPDATE native_inbound_plans SET payload_enc=? WHERE inbound_id=?`, encrypted, in.Profile.ID); err != nil {
		t.Fatal(err)
	}
	if result, err := s.ListNativeInboundPlans(ctx, key); !errors.Is(err, ErrUnavailable) || result != nil {
		t.Fatal("tampered plan index accepted", result, err)
	}
}
