package business

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// These fixtures reproduce the original wire layout independently of the
// production codec, its AAD helpers, and the current Profile JSON marshaler.
const codecFixtureID uint64 = 91
const codecFixtureMarker = "DXPPRF2\x00"
const codecFixtureMarkerV3 = "DXPPRF3\x00"

const codecFixtureSniffing = `{"sniffing":{"enabled":true,"dest_override":["http","tls"],"route_only":true}}`
const codecFixtureFallbacks = `{"fallbacks":[{"dest":"127.0.0.1:18080"}]}`
const codecFixtureCombined = `{"reality_max_time_diff":60000,"sniffing":{"enabled":true,"dest_override":["http","tls"],"route_only":true},"fallbacks":[{"dest":"127.0.0.1:18080"}]}`

func codecFixtureProfileJSON(options string) ([]byte, xrayconfig.Profile) {
	privateKey := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	optional := ""
	if options != "" {
		optional = `,"options":` + options
	}
	raw := []byte(fmt.Sprintf(`{"inbound_id":91,"transport":"tcp","reality_target":"www.example.org:443","server_names":["www.example.org"],"reality_private_key":%q,"short_ids":["aa11"],"users":[]%s}`, privateKey, optional))
	want := xrayconfig.Profile{
		InboundID: codecFixtureID, Transport: "tcp", RealityTarget: "www.example.org:443",
		ServerNames: []string{"www.example.org"}, RealityPrivateKey: privateKey,
		ShortIDs: []string{"aa11"}, Users: []xrayconfig.User{},
	}
	return raw, want
}

func codecFixtureAEAD(t *testing.T, key []byte) cipher.AEAD {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func codecFixtureEnvelope(t *testing.T, key, nonce, raw []byte, version int) []byte {
	t.Helper()
	a := codecFixtureAEAD(t, key)
	if len(nonce) != a.NonceSize() {
		t.Fatal("fixture nonce has the wrong length")
	}
	aad := []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v%d:91", version))
	out := a.Seal(append([]byte(nil), nonce...), nonce, raw, aad)
	if version == 2 {
		out = append([]byte(codecFixtureMarker), out...)
	} else if version == 3 {
		out = append([]byte(codecFixtureMarkerV3), out...)
	}
	return out
}

// Freeze the pre-v3 JSON model so this compatibility test cannot learn new
// fields merely because the production ProfileOptions type was extended.
type codecFixtureOptionsV2 struct {
	GRPCAuthority       string `json:"grpc_authority,omitempty"`
	GRPCMultiMode       bool   `json:"grpc_multi_mode,omitempty"`
	RealityXver         int    `json:"reality_xver,omitempty"`
	RealityMaxTimeDiff  int64  `json:"reality_max_time_diff,omitempty"`
	RealityMinClientVer string `json:"reality_min_client_ver,omitempty"`
	RealityMaxClientVer string `json:"reality_max_client_ver,omitempty"`
	RealityShow         bool   `json:"reality_show,omitempty"`
}

type codecFixtureProfileV2 struct {
	InboundID         uint64                 `json:"inbound_id"`
	Transport         string                 `json:"transport"`
	GRPCService       string                 `json:"grpc_service,omitempty"`
	RealityTarget     string                 `json:"reality_target"`
	ServerNames       []string               `json:"server_names"`
	RealityPrivateKey string                 `json:"reality_private_key"`
	ShortIDs          []string               `json:"short_ids"`
	Users             []xrayconfig.User      `json:"users"`
	Options           *codecFixtureOptionsV2 `json:"options,omitempty"`
}

// Reproduce PR104's original AAD selection, authenticated nonce-marker fallback,
// permissive JSON decoding, and empty-options check. Fixtures use valid v2
// scalar values, so their rejection must come from the envelope boundary.
func codecFixtureOpenV2(t *testing.T, key, encrypted []byte, id uint64) (codecFixtureProfileV2, error) {
	t.Helper()
	var p codecFixtureProfileV2
	a := codecFixtureAEAD(t, key)
	original := encrypted
	aad := []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v1:%d", id))
	extended := bytes.HasPrefix(encrypted, []byte(codecFixtureMarker))
	if extended {
		encrypted = encrypted[len(codecFixtureMarker):]
		aad = []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v2:%d", id))
	}
	if len(encrypted) < a.NonceSize()+a.Overhead() {
		return p, ErrUnavailable
	}
	raw, err := a.Open(nil, encrypted[:a.NonceSize()], encrypted[a.NonceSize():], aad)
	if err != nil && extended {
		raw, err = a.Open(nil, original[:a.NonceSize()], original[a.NonceSize():], []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v1:%d", id)))
		extended = false
	}
	if err != nil || json.Unmarshal(raw, &p) != nil || p.InboundID != id {
		return codecFixtureProfileV2{}, ErrUnavailable
	}
	if p.Options != nil && *p.Options == (codecFixtureOptionsV2{}) {
		p.Options = nil
	}
	if extended != (p.Options != nil) {
		return codecFixtureProfileV2{}, ErrUnavailable
	}
	return p, nil
}

func TestInboundProfileCodecReadsIndependentlyEncodedLegacyEnvelopes(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	ordinaryNonce := bytes.Repeat([]byte{0x52}, 12)
	markerNonce := append([]byte(codecFixtureMarker), 0x11, 0x22, 0x33, 0x44)
	markerV3Nonce := append([]byte(codecFixtureMarkerV3), 0x11, 0x22, 0x33, 0x44)
	for _, tc := range []struct {
		name    string
		nonce   []byte
		options string
	}{
		{"original-format", ordinaryNonce, ""},
		{"legacy-nonce-matches-new-marker", markerNonce, ""},
		{"legacy-nonce-matches-v3-marker", markerV3Nonce, ""},
		{"explicit-empty-options", ordinaryNonce, `{}`},
		{"explicit-default-options", markerNonce, `{"reality_xver":0,"reality_max_time_diff":0,"reality_show":false}`},
		{"explicit-default-new-sections", markerV3Nonce, `{"sniffing":{},"fallbacks":[]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, want := codecFixtureProfileJSON(tc.options)
			envelope := codecFixtureEnvelope(t, key, tc.nonce, raw, 1)
			got, err := openProfile(key, envelope, codecFixtureID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("existing legacy envelope was not preserved", err)
			}
		})
	}
}

func TestInboundProfileCodecReadsIndependentV2AndV3Envelopes(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	for _, tc := range []struct {
		name    string
		version int
		options string
	}{
		{"v2-scalars", 2, `{"reality_max_time_diff":60000}`},
		{"v3-sniffing", 3, codecFixtureSniffing},
		{"v3-fallbacks", 3, codecFixtureFallbacks},
		{"v3-combined", 3, codecFixtureCombined},
		{"v3-disabled-configured-sniffing", 3, `{"sniffing":{"enabled":false,"dest_override":["http"]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := codecFixtureProfileJSON(tc.options)
			var want xrayconfig.Profile
			if err := json.Unmarshal(raw, &want); err != nil {
				t.Fatal(err)
			}
			// The nonce itself starts with the other version marker. Only the
			// authenticated outer envelope determines the format.
			nonce := append([]byte(codecFixtureMarkerV3), 0x11, 0x22, 0x33, 0x44)
			if tc.version == 3 {
				nonce = append([]byte(codecFixtureMarker), 0x11, 0x22, 0x33, 0x44)
			}
			envelope := codecFixtureEnvelope(t, key, nonce, raw, tc.version)
			got, err := openProfile(key, envelope, codecFixtureID)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("independently authenticated profile lost settings", err)
			}
		})
	}
}

func TestInboundProfileCodecRejectsAuthenticatedFormatOptionMismatch(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	ordinaryNonce := bytes.Repeat([]byte{0x52}, 12)
	markerNonce := append([]byte(codecFixtureMarker), 0x11, 0x22, 0x33, 0x44)
	markerV3Nonce := append([]byte(codecFixtureMarkerV3), 0x11, 0x22, 0x33, 0x44)
	for _, tc := range []struct {
		name    string
		version int
		nonce   []byte
		options string
	}{
		{"v1-extended-options", 1, ordinaryNonce, `{"reality_xver":1}`},
		{"v1-marker-fallback-extended-options", 1, markerNonce, `{"reality_max_time_diff":60000}`},
		{"v1-sniffing", 1, ordinaryNonce, codecFixtureSniffing},
		{"v1-fallbacks", 1, markerV3Nonce, codecFixtureFallbacks},
		{"v2-absent-options", 2, ordinaryNonce, ""},
		{"v2-empty-options", 2, ordinaryNonce, `{}`},
		{"v2-default-options", 2, ordinaryNonce, `{"reality_xver":0,"reality_show":false}`},
		{"v2-invalid-options", 2, ordinaryNonce, `{"reality_xver":3}`},
		{"v2-sniffing", 2, ordinaryNonce, codecFixtureSniffing},
		{"v2-fallbacks", 2, ordinaryNonce, codecFixtureFallbacks},
		{"v2-old-and-new-options", 2, ordinaryNonce, codecFixtureCombined},
		{"v3-absent-options", 3, ordinaryNonce, ""},
		{"v3-empty-options", 3, ordinaryNonce, `{}`},
		{"v3-default-new-sections", 3, ordinaryNonce, `{"sniffing":{},"fallbacks":[]}`},
		{"v3-only-v2-options", 3, ordinaryNonce, `{"reality_max_time_diff":60000}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := codecFixtureProfileJSON(tc.options)
			envelope := codecFixtureEnvelope(t, key, tc.nonce, raw, tc.version)
			got, err := openProfile(key, envelope, codecFixtureID)
			if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(got, xrayconfig.Profile{}) {
				t.Fatal("authenticated but incompatible profile format returned usable data", err)
			}
		})
	}
}

func TestInboundProfileCodecAuthenticatesEveryEnvelopeRegion(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	nonce := bytes.Repeat([]byte{0x52}, 12)
	for _, version := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			options, offset := "", 0
			if version == 2 {
				options, offset = `{"reality_max_time_diff":60000}`, len(codecFixtureMarker)
			} else if version == 3 {
				options, offset = codecFixtureCombined, len(codecFixtureMarkerV3)
			}
			raw, _ := codecFixtureProfileJSON(options)
			envelope := codecFixtureEnvelope(t, key, nonce, raw, version)
			positions := map[string]int{
				"nonce": offset + 2, "ciphertext": offset + len(nonce) + 3, "authentication-tag": len(envelope) - 1,
			}
			if version != 1 {
				positions["version-marker"] = 1
			}
			for name, position := range positions {
				t.Run(name, func(t *testing.T) {
					changed := append([]byte(nil), envelope...)
					changed[position] ^= 0x40
					got, err := openProfile(key, changed, codecFixtureID)
					if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(got, xrayconfig.Profile{}) {
						t.Fatal("modified envelope returned a partial or usable private profile", err)
					}
				})
			}
		})
	}
}

func TestInboundProfileCodecChoosesMinimumCompatibleEnvelope(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	for _, tc := range []struct {
		name    string
		version int
		options string
	}{
		{"ordinary", 1, ""},
		{"empty-options", 1, `{}`},
		{"default-sections", 1, `{"sniffing":{},"fallbacks":[]}`},
		{"original-scalars", 2, `{"reality_max_time_diff":60000}`},
		{"original-scalars-default-sections", 2, `{"reality_max_time_diff":60000,"sniffing":{},"fallbacks":[]}`},
		{"sniffing", 3, codecFixtureSniffing},
		{"fallbacks", 3, codecFixtureFallbacks},
		{"combined", 3, codecFixtureCombined},
		{"disabled-configured-sniffing", 3, `{"sniffing":{"enabled":false,"dest_override":["http"]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := codecFixtureProfileJSON(tc.options)
			var p xrayconfig.Profile
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatal(err)
			}
			sealed, err := sealInboundProfile(key, p)
			if err != nil {
				t.Fatal(err)
			}
			payload := sealed
			if tc.version > 1 {
				marker := codecFixtureMarker
				if tc.version == 3 {
					marker = codecFixtureMarkerV3
				}
				if !bytes.HasPrefix(sealed, []byte(marker)) {
					t.Fatal("wrong authenticated profile version")
				}
				payload = payload[len(marker):]
			}
			a := codecFixtureAEAD(t, key)
			aad := []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v%d:91", tc.version))
			if _, err = a.Open(nil, payload[:a.NonceSize()], payload[a.NonceSize():], aad); err != nil {
				t.Fatal("independent expected-version decoder rejected envelope", err)
			}
			_, oldErr := codecFixtureOpenV2(t, key, sealed, codecFixtureID)
			if (oldErr != nil) != (tc.version == 3) {
				t.Fatal("old decoder compatibility did not match the stored settings", oldErr)
			}
			got, err := openProfile(key, sealed, codecFixtureID)
			if err != nil || got.RealityPrivateKey != p.RealityPrivateKey {
				t.Fatal("new reader failed to preserve private identity", err)
			}
			if tc.version == 1 && got.Options != nil {
				t.Fatal("default fields did not retain the ordinary profile representation")
			}
		})
	}
}

func TestInboundProfileCodecV3PreventsOldV2FieldLoss(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	raw, _ := codecFixtureProfileJSON(codecFixtureCombined)
	// This demonstrates why putting the new fields into a v2 envelope is unsafe:
	// the old decoder authenticates it, retains the known scalar and drops the rest.
	unsafeV2 := codecFixtureEnvelope(t, key, bytes.Repeat([]byte{0x52}, 12), raw, 2)
	old, err := codecFixtureOpenV2(t, key, unsafeV2, codecFixtureID)
	if err != nil || old.Options == nil || old.Options.RealityMaxTimeDiff != 60000 {
		t.Fatal("fixture did not reproduce the old decoder's permissive behavior", err)
	}
	var p xrayconfig.Profile
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	sealed, err := sealInboundProfile(key, p)
	if err != nil {
		t.Fatal(err)
	}
	for name, candidate := range map[string][]byte{
		"as-written":       sealed,
		"stripped-version": sealed[len(codecFixtureMarkerV3):],
		"replaced-with-v2": append([]byte(codecFixtureMarker), sealed[len(codecFixtureMarkerV3):]...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := codecFixtureOpenV2(t, key, candidate, codecFixtureID); !errors.Is(err, ErrUnavailable) {
				t.Fatal("older decoder accepted unsupported settings", err)
			}
		})
	}
}

func TestInboundProfileCodecRejectsAuthenticatedUnknownOrAmbiguousJSON(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	nonce := bytes.Repeat([]byte{0x52}, 12)
	for _, tc := range []struct {
		name    string
		version int
		options string
		ending  string
	}{
		{"unknown-profile-field", 1, "", `,"future":true}`},
		{"duplicate-profile-field", 1, "", `,"inbound_id":91}`},
		{"case-alias-profile-field", 1, "", `,"INBOUND_ID":91}`},
		{"trailing-json", 1, "", `} {}`},
		{"unknown-v2-option", 2, `{"reality_xver":1,"future":true}`, ""},
		{"duplicate-v2-option", 2, `{"reality_xver":1,"reality_xver":1}`, ""},
		{"unknown-sniffing-field", 3, `{"sniffing":{"enabled":true,"future":true}}`, ""},
		{"case-alias-sniffing-field", 3, `{"sniffing":{"enabled":true,"ENABLED":true}}`, ""},
		{"unknown-fallback-field", 3, `{"fallbacks":[{"dest":"127.0.0.1:18080","future":true}]}`, ""},
		{"duplicate-fallback-field", 3, `{"fallbacks":[{"dest":"127.0.0.1:18080","dest":"127.0.0.1:18080"}]}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := codecFixtureProfileJSON(tc.options)
			if tc.ending != "" {
				raw = append(raw[:len(raw)-1], []byte(tc.ending)...)
			}
			envelope := codecFixtureEnvelope(t, key, nonce, raw, tc.version)
			got, err := openProfile(key, envelope, codecFixtureID)
			if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(got, xrayconfig.Profile{}) {
				t.Fatal("unknown or ambiguous authenticated JSON returned private data", err)
			}
		})
	}
}

func TestInboundProfileCodecV3AuthenticatesIdentityAndFormat(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	raw, _ := codecFixtureProfileJSON(codecFixtureCombined)
	sealed := codecFixtureEnvelope(t, key, bytes.Repeat([]byte{0x52}, 12), raw, 3)
	for _, tc := range []struct {
		name       string
		key, value []byte
		id         uint64
	}{
		{"wrong-key", bytes.Repeat([]byte{0x13}, 32), sealed, codecFixtureID},
		{"wrong-id", key, sealed, codecFixtureID + 1},
		{"removed-version", key, sealed[len(codecFixtureMarkerV3):], codecFixtureID},
		{"replaced-with-v2", key, append([]byte(codecFixtureMarker), sealed[len(codecFixtureMarkerV3):]...), codecFixtureID},
		{"added-version", key, append([]byte(codecFixtureMarkerV3), sealed...), codecFixtureID},
		{"truncated-tag", key, sealed[:len(sealed)-1], codecFixtureID},
		{"truncated-nonce", key, sealed[:len(codecFixtureMarkerV3)+4], codecFixtureID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := openProfile(tc.key, tc.value, tc.id)
			if !errors.Is(err, ErrUnavailable) || !reflect.DeepEqual(got, xrayconfig.Profile{}) {
				t.Fatal("v3 authentication failure exposed a private profile", err)
			}
		})
	}
}

func TestInboundProfileCodecClearV3RestoresV2ThenV1(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	raw, _ := codecFixtureProfileJSON(codecFixtureCombined)
	var p xrayconfig.Profile
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	sealed, err := sealInboundProfile(key, p)
	if err != nil || !bytes.HasPrefix(sealed, []byte(codecFixtureMarkerV3)) {
		t.Fatal("new settings did not begin in v3", err)
	}
	original := p
	p.Options = &xrayconfig.ProfileOptions{RealityMaxTimeDiff: 60000}
	for _, step := range []struct {
		name    string
		options *xrayconfig.ProfileOptions
	}{
		{"clear-new-sections", p.Options},
		{"clear-all-options", &xrayconfig.ProfileOptions{}},
	} {
		t.Run(step.name, func(t *testing.T) {
			p.Options = step.options
			sealed, err := sealInboundProfile(key, p)
			if err != nil {
				t.Fatal(err)
			}
			old, err := codecFixtureOpenV2(t, key, sealed, codecFixtureID)
			if err != nil || old.RealityPrivateKey != original.RealityPrivateKey {
				t.Fatal("cleared profile did not regain old-reader compatibility", err)
			}
			got, err := openProfile(key, sealed, codecFixtureID)
			want := original
			want.Options = nil
			if step.options.RealityMaxTimeDiff != 0 {
				want.Options = &xrayconfig.ProfileOptions{RealityMaxTimeDiff: 60000}
			}
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("clearing new settings changed identity or unrelated profile fields", err)
			}
		})
	}
}

func TestInboundProfileExplicitClearRestoresLegacyEnvelopeAndIdentity(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	ctx := context.Background()
	in.Profile.Options = &xrayconfig.ProfileOptions{RealityMaxTimeDiff: 60000}
	created, err := s.SaveNativeInboundPlan(ctx, key, in)
	if err != nil {
		t.Fatal(err)
	}
	_, original, err := readNativeInbound(ctx, s.db, key, in.Profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	var before []byte
	if err = s.db.QueryRowContext(ctx, "SELECT profile_enc FROM native_inbounds WHERE id=?", in.Profile.ID).Scan(&before); err != nil || !bytes.HasPrefix(before, []byte(codecFixtureMarker)) {
		t.Fatal("extended setup did not use a versioned profile envelope", err)
	}
	clear := copiedPlan(in)
	clear.EventID = "codec-explicit-clear-event-001"
	clear.ExpectedPlanRevision = created.Plan.PlanRevision
	clear.ExpectedInboundRevision = created.Plan.InboundRevision
	clear.Profile.Options = &xrayconfig.ProfileOptions{}
	cleared, err := s.SaveNativeInboundPlan(ctx, key, clear)
	if err != nil || cleared.Plan.Profile.Options != nil || cleared.Plan.Profile.PublicKey != created.Plan.Profile.PublicKey {
		t.Fatal("explicit clear changed the public identity or retained options", err)
	}
	var after []byte
	if err = s.db.QueryRowContext(ctx, "SELECT profile_enc FROM native_inbounds WHERE id=?", in.Profile.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	// Decode using only the original AES-GCM layout and literal v1 AAD. This
	// checks the persisted transition, not just the current reader's result.
	a := codecFixtureAEAD(t, key)
	if bytes.HasPrefix(after, []byte(codecFixtureMarker)) || len(after) < a.NonceSize()+a.Overhead() {
		t.Fatal("explicit clear did not restore the ordinary envelope")
	}
	raw, err := a.Open(nil, after[:a.NonceSize()], after[a.NonceSize():], []byte(fmt.Sprintf("DARK-PRO-NATIVE-INBOUND-v1:%d", in.Profile.ID)))
	if err != nil {
		t.Fatal("original decoder cannot authenticate the cleared profile", err)
	}
	var restored xrayconfig.Profile
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	original.Options = nil
	if !reflect.DeepEqual(restored, original) {
		t.Fatal("clearing advanced settings changed the stored identity or basic connection profile")
	}
}
