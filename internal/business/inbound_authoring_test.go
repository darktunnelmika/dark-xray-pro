package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

func authoringPlanFixture(t *testing.T) (*Store, []byte, NativeInboundPlanInput) {
	t.Helper()
	s, key, in := creationPlanFixture(t)
	in.Profile = InboundInput{ID: 501, Name: "Explicit protocol", Spec: &inboundspec.Spec{Version: 1, Protocol: "vless", Network: "tcp", Security: "reality",
		Settings: map[string]any{"decryption": "none"}, Reality: map[string]any{"target": "www.example.org:443", "serverNames": []any{"www.example.org"}, "shortIds": []any{"ab12"}}}}
	return s, key, in
}

func TestAuthoringPlanEncryptedSecretsReplayAndEdit(t *testing.T) {
	s, key, in := authoringPlanFixture(t)
	ctx := context.Background()
	preview, err := s.PreviewNativeInboundPlan(ctx, key, in)
	if err != nil || !preview.Valid || planCount(t, s, "native_inbounds") != 0 {
		t.Fatal("preview failed or mutated storage", err)
	}
	result, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil {
		t.Fatal(err)
	}
	if result.Plan.Profile.Spec == nil || result.Plan.Profile.PublicKey == "" || result.Plan.Profile.PublicKey == preview.Plan.Profile.PublicKey {
		t.Fatal("actual key not generated independently")
	}
	public, err := json.Marshal(result)
	if err != nil || strings.Contains(string(public), `"privateKey":`) {
		t.Fatal("private key in response")
	}
	var ciphertext []byte
	if err = s.db.QueryRow(`SELECT profile_enc FROM native_inbounds WHERE id=501`).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(ciphertext, profileV6Prefix) {
		t.Fatal("missing versioned encrypted envelope")
	}
	private, err := openInboundProfile(key, ciphertext, 501)
	if err != nil || private.Spec == nil {
		t.Fatal("cannot reopen private spec", err)
	}
	secret := specString(private.Spec.Reality, "privateKey")
	if secret == "" || bytes.Contains(ciphertext, []byte(secret)) || bytes.Contains(public, []byte(secret)) {
		t.Fatal("secret stored or returned in plaintext")
	}
	replay, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil || !replay.Replayed || replay.Plan.Profile.PublicKey != result.Plan.Profile.PublicKey {
		t.Fatal("retry rotated identity", err)
	}
	loaded, err := s.NativeInboundPlan(ctx, key, 501)
	if err != nil || !loaded.Editable {
		t.Fatal("saved spec cannot reopen", err, loaded.EditBlocker)
	}
	edit := copiedPlan(in)
	edit.EventID = "authoring-plan-edit-0001"
	edit.ExpectedPlanRevision = 1
	edit.ExpectedInboundRevision = 1
	edit.Profile = loaded.Profile.InboundInput
	edit.Profile.Name = "Renamed rich inbound"
	updated, err := s.SaveNativeInboundPlan(ctx, key, edit)
	if err != nil || updated.Plan.Profile.PublicKey != result.Plan.Profile.PublicKey || updated.Plan.InboundRevision != 2 {
		t.Fatal("redacted edit lost secret or revision", err)
	}
	if err = s.db.QueryRow(`SELECT profile_enc FROM native_inbounds WHERE id=501`).Scan(&ciphertext); err != nil {
		t.Fatal(err)
	}
	private, err = openInboundProfile(key, ciphertext, 501)
	if err != nil || specString(private.Spec.Reality, "privateKey") != secret {
		t.Fatal("edit changed key", err)
	}
	edit.EventID = "authoring-plan-clear-key-0001"
	edit.ExpectedPlanRevision = 2
	edit.ExpectedInboundRevision = 2
	edit.Profile.Spec.Reality["privateKey"] = ""
	if _, err = s.SaveNativeInboundPlan(ctx, key, edit); !errors.Is(err, ErrInvalid) {
		t.Fatal("explicit clearing reused required secret", err)
	}
	legacy := copiedPlan(in)
	legacy.Profile.Spec = nil
	legacy.Profile.Transport = "tcp"
	legacy.Profile.Security = "reality"
	legacy.Profile.RealityTarget = "www.example.org:443"
	legacy.Profile.ServerNames = []string{"www.example.org"}
	legacy.Profile.ShortIDs = []string{"ab12"}
	legacy.EventID = "authoring-legacy-editor-0001"
	legacy.ExpectedPlanRevision = 2
	legacy.ExpectedInboundRevision = 2
	if _, err = s.SaveNativeInboundPlan(ctx, key, legacy); !errors.Is(err, ErrConflict) {
		t.Fatal("legacy editor dropped spec", err)
	}
}

func TestAuthoringPlanUsesSelectedDestinationsAndRealProtocol(t *testing.T) {
	for _, protocol := range []string{"vmess", "shadowsocks", "wireguard", "mtproto", "amneziawg"} {
		t.Run(protocol, func(t *testing.T) {
			s, key, in := authoringPlanFixture(t)
			network := "tcp"
			if protocol == "wireguard" || protocol == "mtproto" || protocol == "amneziawg" {
				network = protocol
			}
			in.Profile.Spec = &inboundspec.Spec{Version: 1, Protocol: protocol, Network: network, Security: "none"}
			in.Nodes = in.Nodes[:1]
			in.Nodes[0].NodeID = "remote-only"
			in.Nodes[0].TunnelPort = 0
			in.Nodes[0].TunnelHosts = nil
			preview, err := s.PreviewNativeInboundPlan(context.Background(), key, in)
			if err != nil {
				t.Fatal(err)
			}
			if len(preview.Topology.Nodes) != 1 || preview.Topology.Nodes[0].NodeID != "remote-only" {
				t.Fatal("implicit destination added")
			}
			candidate := preview.Topology.Nodes[0].Candidate
			if protocol == "mtproto" || protocol == "amneziawg" {
				if len(candidate.EngineConfigs) != 1 || candidate.EngineConfigs[0].Engine != protocol || len(candidate.EngineConfigs[0].Config) == 0 {
					t.Fatal("external engine config missing")
				}
			} else {
				var config struct {
					Inbounds []struct {
						Protocol string `json:"protocol"`
					} `json:"inbounds"`
				}
				if json.Unmarshal(candidate.Config, &config) != nil || len(config.Inbounds) != 1 || config.Inbounds[0].Protocol != protocol {
					t.Fatal("selected protocol ignored")
				}
			}
			if _, err = s.SaveNativeInboundPlan(context.Background(), key, in); err != nil {
				t.Fatal("cannot persist selected protocol", err)
			}
			plan, err := s.NativeInboundPlan(context.Background(), key, in.Profile.ID)
			if err != nil || !plan.Editable || plan.Profile.Spec.Protocol != protocol {
				t.Fatal("protocol lost on reopen", err)
			}
		})
	}
}

func TestAuthoringEveryCatalogProtocolPersistsWithoutMaterializing(t *testing.T) {
	raw, err := os.ReadFile("../inboundspec/testdata/protocols.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []inboundspec.Spec
	if err = json.Unmarshal(raw, &fixtures); err != nil || len(fixtures) != len(inboundspec.Catalog()) {
		t.Fatal("incomplete protocol fixtures", err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Protocol, func(t *testing.T) {
			s, key, in := authoringPlanFixture(t)
			in.Profile.Spec = &fixture
			in.Nodes = in.Nodes[:1]
			in.Nodes[0].TunnelPort = 0
			in.Nodes[0].TunnelHosts = nil
			result, err := s.SaveNativeInboundPlan(context.Background(), key, in)
			if err != nil {
				t.Fatal(err)
			}
			if result.Plan.Profile.Enabled || result.RuntimeChanged || result.Plan.RuntimeApply {
				t.Fatal("save activated runtime")
			}
			loaded, err := s.NativeInboundPlan(context.Background(), key, in.Profile.ID)
			if err != nil || !loaded.Editable || loaded.Profile.Spec == nil || loaded.Profile.Spec.Protocol != fixture.Protocol {
				t.Fatal("roundtrip lost protocol", err)
			}
			for _, table := range []string{"clients", "native_inbound_clients", "native_inbound_targets", "native_runtime_delivery"} {
				if planCount(t, s, table) != 0 {
					t.Fatal("authoring materialized runtime or client", table)
				}
			}
			var cipher []byte
			if err = s.db.QueryRow(`SELECT profile_enc FROM native_inbounds WHERE id=?`, in.Profile.ID).Scan(&cipher); err != nil {
				t.Fatal(err)
			}
			private, err := openInboundProfile(key, cipher, in.Profile.ID)
			if err != nil || private.Spec == nil || len(private.Users) != 0 {
				t.Fatal("validation identity escaped to storage", err)
			}
			clean := inboundspec.Public(*private.Spec)
			actual, _ := json.Marshal(loaded.Profile.Spec)
			expected, _ := json.Marshal(clean.Spec)
			if !bytes.Equal(actual, expected) {
				t.Fatal("public spec not derived from authenticated storage")
			}
		})
	}
}
