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
	}
	return out
}

func TestInboundProfileCodecReadsIndependentlyEncodedLegacyEnvelopes(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	ordinaryNonce := bytes.Repeat([]byte{0x52}, 12)
	markerNonce := append([]byte(codecFixtureMarker), 0x11, 0x22, 0x33, 0x44)
	for _, tc := range []struct {
		name    string
		nonce   []byte
		options string
	}{
		{"original-format", ordinaryNonce, ""},
		{"legacy-nonce-matches-new-marker", markerNonce, ""},
		{"explicit-empty-options", ordinaryNonce, `{}`},
		{"explicit-default-options", markerNonce, `{"reality_xver":0,"reality_max_time_diff":0,"reality_show":false}`},
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

func TestInboundProfileCodecRejectsAuthenticatedFormatOptionMismatch(t *testing.T) {
	key := bytes.Repeat([]byte{0x39}, 32)
	ordinaryNonce := bytes.Repeat([]byte{0x52}, 12)
	markerNonce := append([]byte(codecFixtureMarker), 0x11, 0x22, 0x33, 0x44)
	for _, tc := range []struct {
		name    string
		version int
		nonce   []byte
		options string
	}{
		{"v1-extended-options", 1, ordinaryNonce, `{"reality_xver":1}`},
		{"v1-marker-fallback-extended-options", 1, markerNonce, `{"reality_max_time_diff":60000}`},
		{"v2-absent-options", 2, ordinaryNonce, ""},
		{"v2-empty-options", 2, ordinaryNonce, `{}`},
		{"v2-default-options", 2, ordinaryNonce, `{"reality_xver":0,"reality_show":false}`},
		{"v2-invalid-options", 2, ordinaryNonce, `{"reality_xver":3}`},
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
	for _, version := range []int{1, 2} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			options, offset := "", 0
			if version == 2 {
				options, offset = `{"reality_max_time_diff":60000}`, len(codecFixtureMarker)
			}
			raw, _ := codecFixtureProfileJSON(options)
			envelope := codecFixtureEnvelope(t, key, nonce, raw, version)
			positions := map[string]int{
				"nonce": offset + 2, "ciphertext": offset + len(nonce) + 3, "authentication-tag": len(envelope) - 1,
			}
			if version == 2 {
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
