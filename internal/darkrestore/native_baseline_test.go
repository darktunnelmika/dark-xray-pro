package darkrestore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func nativeFixtureV1(t *testing.T, change func(*NativeBaselineV1)) ([]byte, []byte, NativeBaselinePinsV1) {
	t.Helper()
	v := NativeBaselineV1{Version: 1, Scope: "destination_identity_lifetime_with_active_fence",
		DestinationSHA: strings.Repeat("1", 64), SourceSHA: strings.Repeat("2", 64), SourceID: "synthetic-source", ClientID: "synthetic-client", ClientUUID: "synthetic-uuid",
		ImportReceiptSHA: strings.Repeat("3", 64), BeforeStateSHA: strings.Repeat("4", 64), BeforeNativeSHA: strings.Repeat("5", 64),
		InventorySHA: strings.Repeat("6", 64), MeasurementSHA: strings.Repeat("7", 64), FenceSHA: strings.Repeat("8", 64),
		CoverageFromNano: 100, MeasuredThroughNano: 200, FenceUntilNano: 300, Complete: true, Fenced: true}
	if change != nil {
		change(&v)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := nativeBaselineMessageV1(v)
	raw, err := json.Marshal(SignedNativeBaselineV1{Version: 1, Baseline: v, Signature: ed25519.Sign(key, msg)})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	d, k := sha256.Sum256(raw), sha256.Sum256(pub)
	return raw, pub, NativeBaselinePinsV1{DocumentSHA: hex.EncodeToString(d[:]), PublicKeySHA: hex.EncodeToString(k[:]), DestinationSHA: v.DestinationSHA, InventorySHA: v.InventorySHA, MeasurementSHA: v.MeasurementSHA, FenceSHA: v.FenceSHA}
}

func TestNativeBaselineSignedEvidenceAndCopies(t *testing.T) {
	for _, amount := range []int64{0, 9007199254740993} {
		raw, pub, pins := nativeFixtureV1(t, func(v *NativeBaselineV1) { v.Uplink = amount })
		verified, err := VerifySignedNativeBaselineV1(raw, pub, pins)
		if err != nil {
			t.Fatal(err)
		}
		value, body, key, actual, err := verified.Evidence()
		if err != nil || value.Uplink != amount || actual != pins {
			t.Fatal("lost measurement", err)
		}
		body[0] ^= 1
		key[0] ^= 1
		raw[0] ^= 1
		pub[0] ^= 1
		if _, body2, key2, _, err := verified.Evidence(); err != nil || body2[0] != '{' || len(key2) != ed25519.PublicKeySize {
			t.Fatal("verified receipt aliased caller", err)
		}
	}
	if _, _, _, _, err := (VerifiedNativeBaselineV1{}).Evidence(); !errors.Is(err, ErrHandoffInventory) {
		t.Fatal("unverified native evidence accepted")
	}
}

func TestNativeBaselineRejectsIndependentPinMismatch(t *testing.T) {
	raw, pub, pins := nativeFixtureV1(t, nil)
	for _, name := range []string{"document", "key", "destination", "inventory", "measurement", "fence"} {
		t.Run(name, func(t *testing.T) {
			p := pins
			wrong := strings.Repeat("e", 64)
			switch name {
			case "document":
				p.DocumentSHA = wrong
			case "key":
				p.PublicKeySHA = wrong
			case "destination":
				p.DestinationSHA = wrong
			case "inventory":
				p.InventorySHA = wrong
			case "measurement":
				p.MeasurementSHA = wrong
			case "fence":
				p.FenceSHA = wrong
			}
			if _, err := VerifySignedNativeBaselineV1(raw, pub, p); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("foreign pin accepted", err)
			}
		})
	}
}
func TestNativeBaselineRejectsUnknownOrInvalidCoverage(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*NativeBaselineV1)
	}{
		{"incomplete", func(v *NativeBaselineV1) { v.Complete = false }},
		{"unfenced", func(v *NativeBaselineV1) { v.Fenced = false }},
		{"unknown scope", func(v *NativeBaselineV1) { v.Scope = "zero_rows" }},
		{"missing inventory", func(v *NativeBaselineV1) { v.InventorySHA = "" }},
		{"missing measurement", func(v *NativeBaselineV1) { v.MeasurementSHA = "" }},
		{"negative", func(v *NativeBaselineV1) { v.Uplink = -1 }},
		{"overflow", func(v *NativeBaselineV1) { v.Uplink = 9223372036854775807; v.Downlink = 1 }},
		{"reversed interval", func(v *NativeBaselineV1) { v.CoverageFromNano = 201 }},
		{"expired fence interval", func(v *NativeBaselineV1) { v.FenceUntilNano = v.MeasuredThroughNano }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, pub, pins := nativeFixtureV1(t, test.change)
			if _, err := VerifySignedNativeBaselineV1(raw, pub, pins); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("invalid signed measurement accepted", err)
			}
		})
	}
}
func TestNativeBaselineRejectsRepinnedTampering(t *testing.T) {
	raw, pub, pins := nativeFixtureV1(t, nil)
	var doc SignedNativeBaselineV1
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	doc.Baseline.Uplink = 1
	tampered, _ := json.Marshal(doc)
	tampered = append(tampered, '\n')
	unknown := append([]byte("{\"unknown\":1,"), raw[1:]...)
	for _, body := range [][]byte{tampered, unknown, append([]byte(" "), raw...), raw[:len(raw)-1]} {
		d := sha256.Sum256(body)
		p := pins
		p.DocumentSHA = hex.EncodeToString(d[:])
		if _, err := VerifySignedNativeBaselineV1(body, pub, p); !errors.Is(err, ErrHandoffInventory) {
			t.Fatal("invalid re-pinned envelope accepted", err)
		}
	}
}
