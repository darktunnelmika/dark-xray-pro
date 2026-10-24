package business

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func trafficInboundOptions() *xrayconfig.ProfileOptions {
	return &xrayconfig.ProfileOptions{
		RealityMaxTimeDiff: 60000,
		Sniffing: &xrayconfig.SniffingOptions{Enabled: true, DestOverride: []string{"http", "tls"}, RouteOnly: true,
			DomainsExcluded: []string{"full:excluded.example.org", "regexp:^private[.]"}, IPsExcluded: []string{"192.0.2.0/24", "2001:db8::/32"}},
		Fallbacks: []xrayconfig.Fallback{{Dest: "8080"}, {Path: "/status", Dest: "127.0.0.1:8081", Xver: 1}},
	}
}

func TestCreationPlanTrafficOptionsPersistCompileGuardAndClear(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	in.Profile.Options = trafficInboundOptions()
	original := copiedPlan(in)
	generation := planGeneration(t, s)
	preview, err := s.PreviewNativeInboundPlan(ctx, key, in)
	if err != nil || !reflect.DeepEqual(preview.Plan.Profile.Options, original.Profile.Options) {
		t.Fatal("traffic options preview", err)
	}
	for _, node := range preview.Topology.Nodes {
		var config struct {
			Inbounds []struct {
				Protocol string          `json:"protocol"`
				Sniffing json.RawMessage `json:"sniffing"`
				Settings struct {
					Fallbacks []json.RawMessage `json:"fallbacks"`
				} `json:"settings"`
			} `json:"inbounds"`
		}
		if err := json.Unmarshal(node.Candidate.Config, &config); err != nil {
			t.Fatal(err)
		}
		for _, listener := range config.Inbounds {
			if listener.Protocol == "dokodemo-door" {
				if len(listener.Sniffing) != 0 || len(listener.Settings.Fallbacks) != 0 {
					t.Fatal("traffic options leaked onto relay")
				}
			} else if len(listener.Sniffing) == 0 || len(listener.Settings.Fallbacks) != 2 {
				t.Fatal("destination listener lost sniffing or fallbacks")
			}
		}
	}
	created, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || !reflect.DeepEqual(created.Plan.Profile.Options, original.Profile.Options) {
		t.Fatal("save", err)
	}
	_, private, err := readNativeInbound(ctx, s.db, key, in.Profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	privateKey := private.RealityPrivateKey
	in.Profile.Options.Sniffing.DestOverride[0] = "quic"
	in.Profile.Options.Sniffing.DomainsExcluded[0] = "changed.example.org"
	in.Profile.Options.Fallbacks[0].Dest = "9999"
	if !reflect.DeepEqual(created.Plan.Profile.Options, original.Profile.Options) {
		t.Fatal("nested receipt aliases caller slices")
	}
	reopened, err := s.NativeInboundPlan(ctx, key, in.Profile.ID)
	if err != nil || !reflect.DeepEqual(reopened, created.Plan) {
		t.Fatal("reopen lost exact saved plan", err)
	}
	if _, err := s.SaveNativeInboundPlan(ctx, key, in); !errors.Is(err, ErrConflict) {
		t.Fatal("altered nested retry accepted", err)
	}
	replayed, err := s.SaveNativeInboundPlan(ctx, key, original)
	if err != nil || !replayed.Replayed || !reflect.DeepEqual(replayed.Plan, created.Plan) {
		t.Fatal("immutable replay", err)
	}
	edit := copiedPlan(original)
	edit.EventID = "traffic-profile-edit-0001"
	edit.ExpectedInboundRevision, edit.ExpectedPlanRevision = 1, 1
	for _, field := range []string{"sniffing", "fallbacks"} {
		stale := copiedPlan(edit)
		if field == "sniffing" {
			stale.Profile.Options.Sniffing = nil
		} else {
			stale.Profile.Options.Fallbacks = nil
		}
		for _, previewOnly := range []bool{true, false} {
			var err error
			if previewOnly {
				_, err = s.PreviewNativeInboundPlan(ctx, key, stale)
			} else {
				_, err = s.SaveNativeInboundPlan(ctx, key, stale)
			}
			var detail *NativeInboundPlanError
			if !errors.Is(err, ErrConflict) || !errors.As(err, &detail) || detail.Code != "profile_"+field+"_required" {
				t.Fatal("older editor dropped a section", field, previewOnly, err)
			}
		}
	}
	// A disabled section retains its configured children; [] expressly clears
	// fallbacks. The normalized input passes through preview and commit checks.
	edit.Profile.Options.Sniffing.Enabled = false
	edit.Profile.Options.Fallbacks = []xrayconfig.Fallback{}
	if _, err = s.PreviewNativeInboundPlan(ctx, key, edit); err != nil {
		t.Fatal("explicit clear preview", err)
	}
	updated, err := s.SaveNativeInboundPlan(ctx, key, edit)
	if err != nil || updated.Plan.Profile.Options.Sniffing.Enabled || len(updated.Plan.Profile.Options.Sniffing.DestOverride) != 2 || len(updated.Plan.Profile.Options.Fallbacks) != 0 {
		t.Fatal("disabled settings or explicit fallback clear lost", err)
	}
	clear := copiedPlan(edit)
	clear.EventID = "traffic-profile-clear-0001"
	clear.ExpectedInboundRevision, clear.ExpectedPlanRevision = 2, 2
	clear.Profile.Options = &xrayconfig.ProfileOptions{Sniffing: &xrayconfig.SniffingOptions{}, Fallbacks: []xrayconfig.Fallback{}}
	cleared, err := s.SaveNativeInboundPlan(ctx, key, clear)
	if err != nil || cleared.Plan.Profile.Options != nil || cleared.Plan.Profile.PublicKey != created.Plan.Profile.PublicKey {
		t.Fatal("explicit reset", err)
	}
	_, private, err = readNativeInbound(ctx, s.db, key, in.Profile.ID)
	if err != nil || private.Options != nil || private.RealityPrivateKey != privateKey {
		t.Fatal("reset changed identity", err)
	}
	// Historical receipts remain exact even after clearing and newer revisions.
	replayed, err = s.SaveNativeInboundPlan(ctx, key, original)
	if err != nil || !replayed.Replayed || !reflect.DeepEqual(replayed.Plan, created.Plan) {
		t.Fatal("historical traffic receipt changed", err)
	}
	if planGeneration(t, s) != generation {
		t.Fatal("authoring changed runtime generation")
	}
	for _, table := range []string{"native_inbound_targets", "native_inbound_clients", "native_runtime_delivery", "native_node_revisions", "events", "clients"} {
		if planCount(t, s, table) != 0 {
			t.Fatal("traffic settings changed operational state", table)
		}
	}
}

func TestTrafficOptionsCannotEnterGenericNativeDelivery(t *testing.T) {
	s, key, _ := creationPlanFixture(t)
	ctx := context.Background()
	in := nativeInput()
	created, err := s.CreateInbound(ctx, key, in)
	if err != nil {
		t.Fatal(err)
	}
	generation := planGeneration(t, s)
	variants := []*xrayconfig.ProfileOptions{
		{Sniffing: &xrayconfig.SniffingOptions{Enabled: true, DestOverride: []string{"http"}}},
		{Sniffing: &xrayconfig.SniffingOptions{DestOverride: []string{"tls"}}},
		{Fallbacks: []xrayconfig.Fallback{{Dest: "8080"}}},
		trafficInboundOptions(),
	}
	for _, options := range variants {
		input := in
		input.ID++
		input.Options = options
		if _, err := s.CreateInbound(ctx, key, input); !errors.Is(err, ErrInvalid) {
			t.Fatal("generic create admitted unproven traffic settings", err)
		}
		input.ID = in.ID
		mutation := InboundMutation{EventID: "traffic-native-reject-0001", ExpectedRevision: 1, Action: "edit", Reason: "Attempt traffic options on existing listener", Profile: &input}
		if _, err := s.MutateInbound(ctx, key, in.ID, mutation); !errors.Is(err, ErrInvalid) {
			t.Fatal("generic edit admitted unproven traffic settings", err)
		}
	}
	current, err := s.NativeInbound(ctx, key, in.ID)
	if err != nil || !reflect.DeepEqual(current, created) {
		t.Fatal("rejected generic settings changed existing profile", err)
	}
	if planGeneration(t, s) != generation || planCount(t, s, "native_inbounds") != 1 || planCount(t, s, "native_inbound_operations") != 0 {
		t.Fatal("rejected generic settings changed runtime or audit state")
	}
}
