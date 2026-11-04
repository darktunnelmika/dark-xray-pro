package darkrestore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
)

// NativeBaselinePinsV1 is trusted local operator configuration, never browser
// input. Each digest must be obtained independently of the incoming document.
type NativeBaselinePinsV1 struct {
	DocumentSHA    string `json:"document_sha256"`
	PublicKeySHA   string `json:"public_key_sha256"`
	DestinationSHA string `json:"destination_sha256"`
	InventorySHA   string `json:"inventory_sha256"`
	MeasurementSHA string `json:"measurement_sha256"`
	FenceSHA       string `json:"fence_sha256"`
}

// NativeBaselineV1 records a native supervisor's explicit measurement and fence
// assertion. A signature identifies the claimant; it does not independently
// discover undeclared runtimes or prove that the asserted measurements are true.
type NativeBaselineV1 struct {
	Version             int    `json:"version"`
	Scope               string `json:"scope"`
	DestinationSHA      string `json:"destination_sha256"`
	SourceSHA           string `json:"source_sha256"`
	SourceID            string `json:"source_id"`
	ClientID            string `json:"client_id"`
	ClientUUID          string `json:"client_uuid"`
	ImportReceiptSHA    string `json:"import_receipt_sha256"`
	BeforeStateSHA      string `json:"before_state_sha256"`
	BeforeNativeSHA     string `json:"before_native_sha256"`
	InventorySHA        string `json:"inventory_sha256"`
	MeasurementSHA      string `json:"measurement_sha256"`
	FenceSHA            string `json:"fence_sha256"`
	CoverageFromNano    int64  `json:"coverage_from_nano,string"`
	MeasuredThroughNano int64  `json:"measured_through_nano,string"`
	FenceUntilNano      int64  `json:"fence_until_nano,string"`
	Uplink              int64  `json:"uplink,string"`
	Downlink            int64  `json:"downlink,string"`
	Complete            bool   `json:"complete"`
	Fenced              bool   `json:"fenced"`
}
type SignedNativeBaselineV1 struct {
	Version   int              `json:"version"`
	Baseline  NativeBaselineV1 `json:"baseline"`
	Signature []byte           `json:"signature"`
}
type VerifiedNativeBaselineV1 struct {
	baseline NativeBaselineV1
	raw      []byte
	public   []byte
	pins     NativeBaselinePinsV1
	valid    bool
}

func nativeBaselineMessageV1(v NativeBaselineV1) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, ErrHandoffInventory
	}
	return append([]byte("DARK-PRO-NATIVE-BASELINE-ATTESTATION-v1\x00"), raw...), nil
}
func nativeBaselineValidV1(v NativeBaselineV1) bool {
	for _, s := range []string{v.DestinationSHA, v.SourceSHA, v.ImportReceiptSHA, v.BeforeStateSHA, v.BeforeNativeSHA, v.InventorySHA, v.MeasurementSHA, v.FenceSHA} {
		if !inventoryDigestValid(s) {
			return false
		}
	}
	return v.Version == 1 && v.Scope == "destination_identity_lifetime_with_active_fence" &&
		v.SourceID != "" && len(v.SourceID) <= 256 && v.ClientID != "" && len(v.ClientID) <= 256 &&
		v.ClientUUID != "" && len(v.ClientUUID) <= 256 && v.Complete && v.Fenced &&
		v.CoverageFromNano > 0 && v.MeasuredThroughNano >= v.CoverageFromNano &&
		v.FenceUntilNano > v.MeasuredThroughNano && v.Uplink >= 0 && v.Downlink >= 0 &&
		v.Uplink <= math.MaxInt64-v.Downlink
}

// VerifySignedNativeBaselineV1 checks a canonical signed receipt against all
// independent pins. It grants no activation/issuance authority. The destination
// transaction must additionally bind the current identity, counters and clock.
func VerifySignedNativeBaselineV1(raw, public []byte, pins NativeBaselinePinsV1) (VerifiedNativeBaselineV1, error) {
	var zero VerifiedNativeBaselineV1
	for _, p := range []string{pins.DocumentSHA, pins.PublicKeySHA, pins.DestinationSHA, pins.InventorySHA, pins.MeasurementSHA, pins.FenceSHA} {
		if !inventoryDigestValid(p) {
			return zero, ErrHandoffInventory
		}
	}
	if len(raw) == 0 || len(raw) > 16<<10 || len(public) != ed25519.PublicKeySize || !bytes.HasSuffix(raw, []byte("\n")) {
		return zero, ErrHandoffInventory
	}
	d, k := sha256.Sum256(raw), sha256.Sum256(public)
	if hex.EncodeToString(d[:]) != pins.DocumentSHA || hex.EncodeToString(k[:]) != pins.PublicKeySHA {
		return zero, ErrHandoffInventory
	}
	var signed SignedNativeBaselineV1
	if json.Unmarshal(raw, &signed) != nil || signed.Version != 1 || !nativeBaselineValidV1(signed.Baseline) || len(signed.Signature) != ed25519.SignatureSize {
		return zero, ErrHandoffInventory
	}
	canonical, err := json.Marshal(signed)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) {
		return zero, ErrHandoffInventory
	}
	b := signed.Baseline
	if b.DestinationSHA != pins.DestinationSHA || b.InventorySHA != pins.InventorySHA || b.MeasurementSHA != pins.MeasurementSHA || b.FenceSHA != pins.FenceSHA {
		return zero, ErrHandoffInventory
	}
	message, err := nativeBaselineMessageV1(b)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(public), message, signed.Signature) {
		return zero, ErrHandoffInventory
	}
	return VerifiedNativeBaselineV1{baseline: b, raw: append([]byte(nil), raw...), public: append([]byte(nil), public...), pins: pins, valid: true}, nil
}

// Evidence returns copies so a verified receipt cannot be changed by its caller.
func (v VerifiedNativeBaselineV1) Evidence() (NativeBaselineV1, []byte, []byte, NativeBaselinePinsV1, error) {
	if !v.valid {
		return NativeBaselineV1{}, nil, nil, NativeBaselinePinsV1{}, ErrHandoffInventory
	}
	return v.baseline, append([]byte(nil), v.raw...), append([]byte(nil), v.public...), v.pins, nil
}
