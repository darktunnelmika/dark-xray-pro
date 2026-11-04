package darkrestore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"unicode"
	"unicode/utf8"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

var ErrHandoffInventory = errors.New("untrusted dark restore handoff inventory")

// Pins must come from an independent operator checkpoint, never browser JSON.
// This inventory describes declared sources; its completeness is not inferred.
type LocalHandoffSourcePinV1 struct {
	EpochID         string `json:"epoch_id"`
	Sequence        uint64 `json:"sequence,string"`
	ReceiptSHA      string `json:"receipt_sha256"`
	AuthoritySHA    string `json:"authority_sha256"`
	JournalSHA      string `json:"journal_sha256"`
	HandoffSHA      string `json:"handoff_sha256"`
	SignerPublicSHA string `json:"signer_public_key_sha256"`
}
type LocalHandoffInventoryV1 struct {
	Version   int                       `json:"version"`
	SourceSHA string                    `json:"source_sha256"`
	SourceID  string                    `json:"source_id"`
	Sources   []LocalHandoffSourcePinV1 `json:"sources"`
}
type LocalHandoffCoverageV1 struct {
	InventorySHA         string   `json:"inventory_sha256"`
	Scope                string   `json:"scope"`
	MatchedSources       int      `json:"matched_sources"`
	EpochIDs             []string `json:"epoch_ids"`
	AuthorizesActivation bool     `json:"authorizes_activation"`
}

func inventoryDigestValid(value string) bool {
	raw, err := hex.DecodeString(value)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}
func canonicalHandoffInventory(in LocalHandoffInventoryV1) (LocalHandoffInventoryV1, error) {
	if in.Version != 1 || !inventoryDigestValid(in.SourceSHA) || in.SourceID == "" || len(in.SourceID) > 256 || !utf8.ValidString(in.SourceID) || len(in.Sources) == 0 || len(in.Sources) > 256 {
		return LocalHandoffInventoryV1{}, ErrHandoffInventory
	}
	for _, r := range in.SourceID {
		if unicode.IsControl(r) {
			return LocalHandoffInventoryV1{}, ErrHandoffInventory
		}
	}
	in.Sources = append([]LocalHandoffSourcePinV1(nil), in.Sources...)
	sort.Slice(in.Sources, func(i, j int) bool { return in.Sources[i].EpochID < in.Sources[j].EpochID })
	for i, s := range in.Sources {
		if !uc.ValidReceipt(uc.Receipt{EpochID: s.EpochID, Sequence: s.Sequence, SHA: s.ReceiptSHA}) || (i > 0 && in.Sources[i-1].EpochID == s.EpochID) {
			return LocalHandoffInventoryV1{}, ErrHandoffInventory
		}
		for _, digest := range []string{s.ReceiptSHA, s.AuthoritySHA, s.JournalSHA, s.HandoffSHA, s.SignerPublicSHA} {
			if !inventoryDigestValid(digest) {
				return LocalHandoffInventoryV1{}, ErrHandoffInventory
			}
		}
	}
	return in, nil
}

// DigestLocalHandoffInventoryV1 canonicalizes order without mutating the caller.
func DigestLocalHandoffInventoryV1(in LocalHandoffInventoryV1) (string, error) {
	canonical, err := canonicalHandoffInventory(in)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", ErrHandoffInventory
	}
	sum := sha256.Sum256(append([]byte("DARK-PRO-HANDOFF-INVENTORY-v1"+string(rune(0))), raw...))
	return hex.EncodeToString(sum[:]), nil
}

// CheckLocalHandoffCoverageV1 checks exact coverage of a separately pinned
// inventory using opaque signature-verified handoffs. It does not attest that
// undeclared installations do not exist, sum possibly overlapping traffic,
// establish a native baseline, alter quarantine or authorize subscriptions.
func CheckLocalHandoffCoverageV1(in LocalHandoffInventoryV1, pin string, handoffs []VerifiedLocalHandoff) (LocalHandoffCoverageV1, error) {
	zero := LocalHandoffCoverageV1{}
	canonical, err := canonicalHandoffInventory(in)
	if err != nil || !inventoryDigestValid(pin) || len(handoffs) != len(canonical.Sources) {
		return zero, ErrHandoffInventory
	}
	digest, err := DigestLocalHandoffInventoryV1(canonical)
	if err != nil || digest != pin {
		return zero, ErrHandoffInventory
	}
	expected := make(map[string]LocalHandoffSourcePinV1, len(canonical.Sources))
	for _, s := range canonical.Sources {
		expected[s.EpochID] = s
	}
	for _, handoff := range handoffs {
		proof, err := handoff.Settlement()
		if err != nil || proof.SourceSHA != canonical.SourceSHA || proof.SourceID != canonical.SourceID {
			return zero, ErrHandoffInventory
		}
		fileSHA, publicSHA, err := handoff.Evidence()
		entry, ok := expected[proof.Terminal.EpochID]
		if err != nil || !ok || entry.Sequence != proof.Terminal.Sequence || entry.ReceiptSHA != proof.Terminal.SHA || entry.AuthoritySHA != proof.AuthoritySHA || entry.JournalSHA != proof.JournalSHA || entry.HandoffSHA != fileSHA || entry.SignerPublicSHA != publicSHA {
			return zero, ErrHandoffInventory
		}
		delete(expected, proof.Terminal.EpochID)
	}
	if len(expected) != 0 {
		return zero, ErrHandoffInventory
	}
	out := LocalHandoffCoverageV1{InventorySHA: digest, Scope: "pinned_inventory_only", MatchedSources: len(canonical.Sources), EpochIDs: make([]string, 0, len(canonical.Sources))}
	for _, s := range canonical.Sources {
		out.EpochIDs = append(out.EpochIDs, s.EpochID)
	}
	return out, nil
}
