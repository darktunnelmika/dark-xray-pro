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

func TestCreationXHTTPAndClientOptionsKeepIdentityAndRequireExplicitSections(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	in.Profile.Transport = "xhttp"
	in.Profile.Options = &xrayconfig.ProfileOptions{
		XHTTP:  &xrayconfig.XHTTPOptions{Path: "/transfer", Host: "edge.example.org", Mode: "stream-up", XPaddingBytes: "128-256"},
		Client: &xrayconfig.ClientOptions{Fingerprint: "firefox", SpiderX: "/news"},
	}
	created, err := s.SaveNativeInboundPlan(context.Background(), key, in)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.NativeInboundPlan(context.Background(), key, in.Profile.ID)
	if err != nil || !reflect.DeepEqual(loaded, created.Plan) {
		t.Fatal("XHTTP profile readback changed options", err)
	}
	var encrypted []byte
	if err = s.db.QueryRow(`SELECT profile_enc FROM native_inbounds WHERE id=?`, in.Profile.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(encrypted, []byte("DXPPRF4\x00")) {
		t.Fatal("new profile used old authenticated envelope")
	}
	_, oldPrivate, err := readNativeInbound(context.Background(), s.db, key, in.Profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	edit := copiedPlan(in)
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = 1, 1
	edit.EventID = "network-options-missing-client-001"
	edit.Profile.Options.Client = nil
	if _, err = s.SaveNativeInboundPlan(context.Background(), key, edit); !errors.Is(err, ErrConflict) {
		t.Fatal("missing client metadata erased profile settings", err)
	}
	edit.EventID = "network-options-clear-client-001"
	edit.Profile.Options.Client = &xrayconfig.ClientOptions{}
	updated, err := s.SaveNativeInboundPlan(context.Background(), key, edit)
	if err != nil || updated.Plan.InboundRevision != 2 || updated.Plan.Profile.Options.Client != nil {
		t.Fatal("explicit client metadata clear failed", err)
	}
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = 2, 2
	edit.EventID = "network-options-change-transport-001"
	edit.Profile.Transport = "tcp"
	edit.Profile.Options.XHTTP = nil
	if _, err = s.SaveNativeInboundPlan(context.Background(), key, edit); !errors.Is(err, ErrConflict) {
		t.Fatal("switching transport silently erased XHTTP settings", err)
	}
	edit.Profile.Options.XHTTP = &xrayconfig.XHTTPOptions{}
	cleared, err := s.SaveNativeInboundPlan(context.Background(), key, edit)
	if err != nil || cleared.Plan.Profile.Transport != "tcp" || cleared.Plan.Profile.Options != nil {
		t.Fatal("explicit switch to TCP failed", err)
	}
	_, private, err := readNativeInbound(context.Background(), s.db, key, in.Profile.ID)
	if err != nil || private.RealityPrivateKey != oldPrivate.RealityPrivateKey || cleared.Plan.Profile.PublicKey != created.Plan.Profile.PublicKey {
		t.Fatal("network edit rotated profile identity", err)
	}
	if planCount(t, s, "native_inbound_targets") != 0 || planCount(t, s, "native_inbound_clients") != 0 {
		t.Fatal("network authoring materialized runtime")
	}
}

func TestNetworkOptionsCannotBypassCreationPlanThroughGenericEndpoints(t *testing.T) {
	s, _ := opened(t)
	key := bytes.Repeat([]byte{5}, 32)
	for _, in := range []InboundInput{
		{ID: 51, Name: "XHTTP", Transport: "xhttp", RealityTarget: "www.example.org:443", ServerNames: []string{"www.example.org"}, ShortIDs: []string{"aa11"}},
		{ID: 52, Name: "Client metadata", Transport: "tcp", RealityTarget: "www.example.org:443", ServerNames: []string{"www.example.org"}, ShortIDs: []string{"aa11"}, Options: &xrayconfig.ProfileOptions{Client: &xrayconfig.ClientOptions{Fingerprint: "firefox"}}},
	} {
		if _, err := s.CreateInbound(context.Background(), key, in); !errors.Is(err, ErrInvalid) {
			t.Fatal("generic create accepted unadmitted network options", err)
		}
	}
	ordinary := nativeInput()
	if _, err := s.CreateInbound(context.Background(), key, ordinary); err != nil {
		t.Fatal(err)
	}
	ordinary.Options = &xrayconfig.ProfileOptions{Client: &xrayconfig.ClientOptions{SpiderX: "/metadata"}}
	if _, err := s.MutateInbound(context.Background(), key, ordinary.ID, InboundMutation{EventID: "generic-network-options-edit-001", ExpectedRevision: 1, Action: "edit", Reason: "Test boundary", Profile: &ordinary}); !errors.Is(err, ErrInvalid) {
		t.Fatal("generic lifecycle accepted unadmitted options", err)
	}
}

func TestInboundNetworkEnvelopeVersionBindingAndLegacyNonceCollision(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	_, profile := codecFixtureProfileJSON("")
	for _, transport := range []string{"tcp", "xhttp"} {
		t.Run(transport, func(t *testing.T) {
			p := profile
			p.Transport = transport
			if transport == "tcp" {
				p.Options = &xrayconfig.ProfileOptions{Client: &xrayconfig.ClientOptions{Fingerprint: "firefox"}}
			}
			enc, err := sealInboundProfile(key, p)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(enc, []byte("DXPPRF4\x00")) {
				t.Fatal("v4 marker absent")
			}
			loaded, err := openInboundProfile(key, enc, p.InboundID)
			if err != nil || !reflect.DeepEqual(loaded, p) {
				t.Fatal("v4 roundtrip mismatch", err)
			}
			if _, err = codecFixtureOpenV2(t, key, enc, p.InboundID); !errors.Is(err, ErrUnavailable) {
				t.Fatal("old decoder accepted extended profile", err)
			}
			raw, _ := json.Marshal(p)
			nonce := bytes.Repeat([]byte{0x52}, 12)
			for _, version := range []int{1, 2, 3} {
				wrong := codecFixtureEnvelope(t, key, nonce, raw, version)
				if _, err = openInboundProfile(key, wrong, p.InboundID); !errors.Is(err, ErrUnavailable) {
					t.Fatal("authenticated version downgrade accepted", version, err)
				}
			}
		})
	}
	raw, want := codecFixtureProfileJSON("")
	nonce := append([]byte("DXPPRF4\x00"), 0x11, 0x22, 0x33, 0x44)
	legacy := codecFixtureEnvelope(t, key, nonce, raw, 1)
	loaded, err := openInboundProfile(key, legacy, codecFixtureID)
	if err != nil || !reflect.DeepEqual(loaded, want) {
		t.Fatal("legacy random nonce mistaken for v4 envelope", err)
	}
}

func TestGenericInboundCreateCannotExceedDirectoryCapacity(t *testing.T) {
	s, _ := opened(t)
	key := bytes.Repeat([]byte{7}, 32)
	for id := uint64(1); id <= 256; id++ {
		in := nativeInput()
		in.ID = id
		if _, err := s.CreateInbound(context.Background(), key, in); err != nil {
			t.Fatal(id, err)
		}
	}
	in := nativeInput()
	in.ID = 257
	if _, err := s.CreateInbound(context.Background(), key, in); !errors.Is(err, ErrConflict) {
		t.Fatal("generic create exceeded list capacity", err)
	}
	if rows, err := s.ListInbounds(context.Background(), key); err != nil || len(rows) != 256 {
		t.Fatal("capacity failure broke existing list", err)
	}
	in.ID = 256
	if _, err := s.CreateInbound(context.Background(), key, in); err != nil {
		t.Fatal("exact create replay at capacity failed", err)
	}
}
