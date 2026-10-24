package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func advancedInboundOptions() *xrayconfig.ProfileOptions {
	return &xrayconfig.ProfileOptions{
		GRPCAuthority: "grpc.example.org:443", GRPCMultiMode: true,
		RealityXver: 2, RealityMaxTimeDiff: 60000,
		RealityMinClientVer: "1.8.0", RealityMaxClientVer: "255.255.255", RealityShow: true,
	}
}

func TestCreationPlanAdvancedOptionsPersistRenderReplayAndClear(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	in.Profile.Transport, in.Profile.GRPCService = "grpc", "advanced_service"
	in.Profile.Options = advancedInboundOptions()
	want := *in.Profile.Options
	generation := planGeneration(t, s)
	preview, err := s.PreviewNativeInboundPlan(ctx, key, in)
	if err != nil || !reflect.DeepEqual(preview.Plan.Profile.Options, &want) || !preview.TopologyValidated {
		t.Fatal("advanced preview lost options", err)
	}
	for _, node := range preview.Topology.Nodes {
		var config struct {
			Inbounds []struct {
				Protocol string `json:"protocol"`
				Stream   struct {
					GRPC struct {
						Authority string `json:"authority"`
						MultiMode bool   `json:"multiMode"`
					} `json:"grpcSettings"`
					Reality struct {
						Xver        int   `json:"xver"`
						MaxTimeDiff int64 `json:"maxTimeDiff"`
					} `json:"realitySettings"`
				} `json:"streamSettings"`
			} `json:"inbounds"`
		}
		if err = json.Unmarshal(node.Candidate.Config, &config); err != nil {
			t.Fatal(err)
		}
		for _, listener := range config.Inbounds {
			if listener.Protocol == "dokodemo-door" {
				if listener.Stream.GRPC.Authority != "" || listener.Stream.Reality.Xver != 0 {
					t.Fatal("receiver options copied onto transit relay")
				}
				continue
			}
			if listener.Stream.GRPC.Authority != want.GRPCAuthority || !listener.Stream.GRPC.MultiMode || listener.Stream.Reality.Xver != 2 || listener.Stream.Reality.MaxTimeDiff != 60000 {
				t.Fatal("Direct/Tunnel/Swap receiver lost compiled options")
			}
		}
	}
	created, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || !reflect.DeepEqual(created.Plan.Profile.Options, &want) {
		t.Fatal("save options", err)
	}
	_, private, err := readNativeInbound(ctx, s.db, key, in.Profile.ID)
	if err != nil || !reflect.DeepEqual(private.Options, &want) {
		t.Fatal("private profile differs", err)
	}
	originalKey := private.RealityPrivateKey
	loaded, err := s.NativeInboundPlan(ctx, key, in.Profile.ID)
	if err != nil || !reflect.DeepEqual(loaded.Profile.Options, &want) {
		t.Fatal("reopen options", err)
	}
	listed, err := s.ListInbounds(ctx, key)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0].Options, &want) {
		t.Fatal("directory options", err)
	}
	replayed, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || !replayed.Replayed || !reflect.DeepEqual(replayed.Plan, created.Plan) {
		t.Fatal("advanced exact replay changed result", err)
	}
	changed := copiedPlan(in)
	changed.Profile.Options.RealityMaxTimeDiff++
	if _, err = s.SaveNativeInboundPlan(ctx, key, changed); !errors.Is(err, ErrConflict) {
		t.Fatal("same event accepted altered options", err)
	}
	// A caller changing its original input cannot alter the saved receipt.
	in.Profile.Options.RealityMaxTimeDiff = 70000
	if created.Plan.Profile.Options.RealityMaxTimeDiff != want.RealityMaxTimeDiff {
		t.Fatal("receipt aliases caller options")
	}
	edit := copiedPlan(in)
	edit.EventID = "advanced-profile-edit-0001"
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = 1, 1
	staleBrowser := copiedPlan(edit)
	staleBrowser.Profile.Options = nil
	if _, err = s.PreviewNativeInboundPlan(ctx, key, staleBrowser); !errors.Is(err, ErrConflict) {
		t.Fatal("old browser preview silently cleared options", err)
	}
	if _, err = s.SaveNativeInboundPlan(ctx, key, staleBrowser); !errors.Is(err, ErrConflict) {
		t.Fatal("old browser save silently cleared options", err)
	}
	updated, err := s.SaveNativeInboundPlan(ctx, key, edit)
	if err != nil || updated.Plan.InboundRevision != 2 || updated.Plan.PlanRevision != 2 || updated.Plan.Profile.PublicKey != created.Plan.Profile.PublicKey {
		t.Fatal("edit options identity/revision", err)
	}
	_, private, err = readNativeInbound(ctx, s.db, key, in.Profile.ID)
	if err != nil || private.RealityPrivateKey != originalKey || private.Options.RealityMaxTimeDiff != 70000 {
		t.Fatal("edit rotated key or dropped options", err)
	}
	clear := copiedPlan(edit)
	clear.EventID = "advanced-profile-clear-0001"
	clear.ExpectedPlanRevision, clear.ExpectedInboundRevision = 2, 2
	clear.Profile.Options = &xrayconfig.ProfileOptions{}
	cleared, err := s.SaveNativeInboundPlan(ctx, key, clear)
	if err != nil || cleared.Plan.Profile.Options != nil || cleared.Plan.InboundRevision != 3 {
		t.Fatal("explicit defaults did not clear options", err)
	}
	clear.Profile.Options = nil
	defaultReplay, err := s.SaveNativeInboundPlan(ctx, key, clear)
	if err != nil || !defaultReplay.Replayed || !reflect.DeepEqual(defaultReplay.Plan, cleared.Plan) {
		t.Fatal("absent/default options replay differs", err)
	}
	if planGeneration(t, s) != generation {
		t.Fatal("authoring changed runtime generation")
	}
	for _, table := range []string{"native_inbound_targets", "native_inbound_clients", "native_runtime_delivery", "native_node_revisions", "events", "clients"} {
		if planCount(t, s, table) != 0 {
			t.Fatal("authoring mutated operational state", table)
		}
	}
}

func TestNativeInboundAdvancedOptionsLifecyclePreservesIdentity(t *testing.T) {
	s, key, _ := creationPlanFixture(t)
	ctx := context.Background()
	in := nativeInput()
	in.Transport, in.GRPCService, in.Options = "grpc", "native_service", advancedInboundOptions()
	created, err := s.CreateInbound(ctx, key, in)
	if err != nil || !reflect.DeepEqual(created.Options, in.Options) {
		t.Fatal("native create", err)
	}
	_, original, err := readNativeInbound(ctx, s.db, key, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.CreateInbound(ctx, key, in)
	if err != nil || replay.PublicKey != created.PublicKey || !reflect.DeepEqual(replay.Options, in.Options) {
		t.Fatal("native retry", err)
	}
	updatedInput := in
	updatedOptions := *in.Options
	updatedOptions.RealityMaxTimeDiff = 75000
	updatedInput.Options = &updatedOptions
	mutation := InboundMutation{EventID: "advanced-native-edit-0001", ExpectedRevision: 1, Action: "edit", Reason: "Edit advanced options", Profile: &updatedInput}
	staleInput := updatedInput
	staleInput.Options = nil
	staleMutation := mutation
	staleMutation.Profile = &staleInput
	if _, err = s.MutateInbound(ctx, key, in.ID, staleMutation); !errors.Is(err, ErrConflict) {
		t.Fatal("old native editor silently cleared options", err)
	}
	updated, err := s.MutateInbound(ctx, key, in.ID, mutation)
	if err != nil || updated.Revision != 2 || updated.PublicKey != created.PublicKey || updated.Options.RealityMaxTimeDiff != 75000 {
		t.Fatal("native edit", err)
	}
	_, private, err := readNativeInbound(ctx, s.db, key, in.ID)
	if err != nil || private.RealityPrivateKey != original.RealityPrivateKey || !reflect.DeepEqual(private.Options, updated.Options) {
		t.Fatal("native edit changed identity", err)
	}
	if _, err = s.MutateInbound(ctx, key, in.ID, mutation); err != nil {
		t.Fatal("native mutation replay", err)
	}
	updatedInput.Options = &xrayconfig.ProfileOptions{RealityXver: 3}
	mutation.EventID, mutation.ExpectedRevision = "advanced-native-invalid-0001", 2
	if _, err = s.MutateInbound(ctx, key, in.ID, mutation); !errors.Is(err, ErrInvalid) {
		t.Fatal("invalid options accepted", err)
	}
	current, err := s.NativeInbound(ctx, key, in.ID)
	if err != nil || !reflect.DeepEqual(current, updated) {
		t.Fatal("invalid options changed persisted profile", err)
	}
}

func TestInboundProfileEnvelopeLegacyCompatibilityAndDowngradeProtection(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	if _, err := s.SaveNativeInboundPlan(ctx, key, in); err != nil {
		t.Fatal(err)
	}
	_, p, err := readNativeInbound(ctx, s.db, key, in.Profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	a, err := profileAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	legacyOpen := func(ciphertext []byte) error {
		if len(ciphertext) < a.NonceSize()+a.Overhead() {
			return ErrUnavailable
		}
		_, err := a.Open(nil, ciphertext[:a.NonceSize()], ciphertext[a.NonceSize():], profileAAD(p.InboundID))
		return err
	}
	plain, err := sealInboundProfile(key, p)
	if err != nil || bytes.HasPrefix(plain, extendedProfilePrefix) || legacyOpen(plain) != nil {
		t.Fatal("default profile lost old-binary compatibility", err)
	}
	p.Options = &xrayconfig.ProfileOptions{RealityMaxTimeDiff: 60000}
	extended, err := sealInboundProfile(key, p)
	if err != nil || !bytes.HasPrefix(extended, extendedProfilePrefix) {
		t.Fatal("missing extended envelope", err)
	}
	if legacyOpen(extended) == nil {
		t.Fatal("old binary could silently drop advanced fields")
	}
	loaded, err := openProfile(key, extended, p.InboundID)
	if err != nil || !reflect.DeepEqual(loaded, p) {
		t.Fatal("extended envelope did not roundtrip", err)
	}
	for _, tc := range []struct {
		label      string
		key, value []byte
		id         uint64
	}{
		{"wrong-key", bytes.Repeat([]byte{0x13}, 32), extended, p.InboundID},
		{"wrong-id", key, extended, p.InboundID + 1},
		{"removed-version", key, extended[len(extendedProfilePrefix):], p.InboundID},
		{"added-version", key, append(append([]byte(nil), extendedProfilePrefix...), plain...), p.InboundID},
		{"truncated", key, extended[:len(extended)-1], p.InboundID},
	} {
		t.Run(tc.label, func(t *testing.T) {
			got, err := openProfile(tc.key, tc.value, tc.id)
			if !errors.Is(err, ErrUnavailable) || got.RealityPrivateKey != "" {
				t.Fatal("tampered envelope exposed usable profile", err)
			}
		})
	}
}
