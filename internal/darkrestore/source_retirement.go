package darkrestore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

const (
	SourceRetirementScopeV1        = "darkrestore_declared_source_retirement_v1"
	SourceRetirementWitnessScopeV1 = "darkrestore_declared_source_retirement_witness_v1"
	maxRetirementWitnessDelayNano  = int64(5 * 60 * 1_000_000_000)
	retirementDomainV1             = "DARK-PRO-DECLARED-SOURCE-RETIREMENT-v1\x00"
	retirementWitnessDomainV1      = "DARK-PRO-DECLARED-SOURCE-RETIREMENT-WITNESS-v1\x00"
)

// SourceRetirementStatementV1 closes exactly one declared inventory entry. A
// valid source signature identifies the signer; it does not discover other
// installations or prove the operational truth of the asserted fence.
type SourceRetirementStatementV1 struct {
	Version               int    `json:"version"`
	Scope                 string `json:"scope"`
	SourceSHA             string `json:"source_sha256"`
	SourceID              string `json:"source_id"`
	InventorySHA          string `json:"inventory_sha256"`
	DestinationSHA        string `json:"destination_sha256"`
	EpochID               string `json:"epoch_id"`
	Sequence              uint64 `json:"sequence,string"`
	TerminalReceiptSHA    string `json:"terminal_receipt_sha256"`
	AuthoritySHA          string `json:"authority_sha256"`
	JournalSHA            string `json:"journal_sha256"`
	HandoffSHA            string `json:"handoff_sha256"`
	SourceSignerPublicSHA string `json:"source_signer_public_key_sha256"`
	RetiredUnixNano       int64  `json:"retired_unix_nano,string"`
	FenceDocumentSHA      string `json:"active_fence_document_sha256"`
	FenceObservedUnixNano int64  `json:"active_fence_observed_unix_nano,string"`
}

type SignedSourceRetirementV1 struct {
	Version   int                         `json:"version"`
	Statement SourceRetirementStatementV1 `json:"statement"`
	Signature []byte                      `json:"signature"`
}

// SourceRetirementWitnessV1 is a separately signed review of the exact source
// statement. It remains an assertion and therefore grants no activation.
type SourceRetirementWitnessV1 struct {
	Version               int    `json:"version"`
	Scope                 string `json:"scope"`
	RetirementDocumentSHA string `json:"retirement_document_sha256"`
	SourcePublicKeySHA    string `json:"source_public_key_sha256"`
	InventorySHA          string `json:"inventory_sha256"`
	DestinationSHA        string `json:"destination_sha256"`
	WitnessedUnixNano     int64  `json:"witnessed_unix_nano,string"`
}

type SignedSourceRetirementWitnessV1 struct {
	Version   int                       `json:"version"`
	Witness   SourceRetirementWitnessV1 `json:"witness"`
	Signature []byte                    `json:"signature"`
}

// SourceRetirementPinsV1 must come from independent trusted configuration.
// None of these digests may be copied from a browser request.
type SourceRetirementPinsV1 struct {
	RetirementDocumentSHA string `json:"retirement_document_sha256"`
	SourcePublicKeySHA    string `json:"source_public_key_sha256"`
	WitnessDocumentSHA    string `json:"witness_document_sha256"`
	WitnessPublicKeySHA   string `json:"witness_public_key_sha256"`
	FenceDocumentSHA      string `json:"active_fence_document_sha256"`
	InventorySHA          string `json:"inventory_sha256"`
	DestinationSHA        string `json:"destination_sha256"`
}

type VerifiedSourceRetirementV1 struct {
	statement SourceRetirementStatementV1
	witness   SourceRetirementWitnessV1
	pins      SourceRetirementPinsV1
	valid     bool
}

type LocalHandoffRetirementCoverageV1 struct {
	Scope                   string   `json:"scope"`
	InventorySHA            string   `json:"inventory_sha256"`
	DestinationSHA          string   `json:"destination_sha256"`
	BundleSHA               string   `json:"retirement_bundle_sha256"`
	DeclaredSources         int      `json:"declared_sources"`
	EpochIDs                []string `json:"epoch_ids"`
	RetirementDocumentSHAs  []string `json:"retirement_document_sha256s"`
	WitnessDocumentSHAs     []string `json:"witness_document_sha256s"`
	LatestWitnessedUnixNano int64    `json:"latest_witnessed_unix_nano,string"`
	AuthorizesActivation    bool     `json:"authorizes_activation"`
}

func retirementMessageV1(v SourceRetirementStatementV1) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, ErrHandoffInventory
	}
	return append([]byte(retirementDomainV1), raw...), nil
}

func retirementWitnessMessageV1(v SourceRetirementWitnessV1) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, ErrHandoffInventory
	}
	return append([]byte(retirementWitnessDomainV1), raw...), nil
}

// SourceRetirementSigningMessageV1 returns the domain-separated bytes for an
// offline source signer. It validates no evidence and grants no authority.
func SourceRetirementSigningMessageV1(v SourceRetirementStatementV1) ([]byte, error) {
	return retirementMessageV1(v)
}

// SourceRetirementWitnessSigningMessageV1 returns the domain-separated bytes
// for the independent witness signer. Verification still requires trusted pins.
func SourceRetirementWitnessSigningMessageV1(v SourceRetirementWitnessV1) ([]byte, error) {
	return retirementWitnessMessageV1(v)
}

func validSourceRetirementStatementV1(v SourceRetirementStatementV1) bool {
	for _, digest := range []string{v.SourceSHA, v.InventorySHA, v.DestinationSHA, v.TerminalReceiptSHA, v.AuthoritySHA, v.JournalSHA, v.HandoffSHA, v.SourceSignerPublicSHA, v.FenceDocumentSHA} {
		if !inventoryDigestValid(digest) {
			return false
		}
	}
	return v.Version == 1 && v.Scope == SourceRetirementScopeV1 &&
		v.SourceID != "" && len(v.SourceID) <= 256 &&
		uc.ValidReceipt(uc.Receipt{EpochID: v.EpochID, Sequence: v.Sequence, SHA: v.TerminalReceiptSHA}) &&
		v.RetiredUnixNano > 0 && v.FenceObservedUnixNano >= v.RetiredUnixNano &&
		v.FenceObservedUnixNano-v.RetiredUnixNano <= maxRetirementWitnessDelayNano
}

func validSourceRetirementWitnessV1(v SourceRetirementWitnessV1, statement SourceRetirementStatementV1, pins SourceRetirementPinsV1) bool {
	return v.Version == 1 && v.Scope == SourceRetirementWitnessScopeV1 &&
		v.RetirementDocumentSHA == pins.RetirementDocumentSHA &&
		v.SourcePublicKeySHA == pins.SourcePublicKeySHA &&
		v.InventorySHA == pins.InventorySHA && v.InventorySHA == statement.InventorySHA &&
		v.DestinationSHA == pins.DestinationSHA && v.DestinationSHA == statement.DestinationSHA &&
		v.WitnessedUnixNano >= statement.FenceObservedUnixNano &&
		v.WitnessedUnixNano-statement.FenceObservedUnixNano <= maxRetirementWitnessDelayNano
}

// VerifySignedSourceRetirementV1 requires two canonical documents and two
// independently pinned Ed25519 authorities. It proves only that the two
// identified signers made matching assertions about one declared source.
func VerifySignedSourceRetirementV1(retirementRaw, sourcePublic, witnessRaw, witnessPublic []byte, pins SourceRetirementPinsV1) (VerifiedSourceRetirementV1, error) {
	var zero VerifiedSourceRetirementV1
	for _, digest := range []string{pins.RetirementDocumentSHA, pins.SourcePublicKeySHA, pins.WitnessDocumentSHA, pins.WitnessPublicKeySHA, pins.FenceDocumentSHA, pins.InventorySHA, pins.DestinationSHA} {
		if !inventoryDigestValid(digest) {
			return zero, ErrHandoffInventory
		}
	}
	if len(retirementRaw) == 0 || len(retirementRaw) > 16<<10 || len(witnessRaw) == 0 || len(witnessRaw) > 16<<10 ||
		len(sourcePublic) != ed25519.PublicKeySize || len(witnessPublic) != ed25519.PublicKeySize || bytes.Equal(sourcePublic, witnessPublic) ||
		!bytes.HasSuffix(retirementRaw, []byte("\n")) || !bytes.HasSuffix(witnessRaw, []byte("\n")) {
		return zero, ErrHandoffInventory
	}
	retirementSum, sourceSum := sha256.Sum256(retirementRaw), sha256.Sum256(sourcePublic)
	witnessSum, witnessKeySum := sha256.Sum256(witnessRaw), sha256.Sum256(witnessPublic)
	if hex.EncodeToString(retirementSum[:]) != pins.RetirementDocumentSHA || hex.EncodeToString(sourceSum[:]) != pins.SourcePublicKeySHA ||
		hex.EncodeToString(witnessSum[:]) != pins.WitnessDocumentSHA || hex.EncodeToString(witnessKeySum[:]) != pins.WitnessPublicKeySHA {
		return zero, ErrHandoffInventory
	}
	var signed SignedSourceRetirementV1
	if json.Unmarshal(retirementRaw, &signed) != nil || signed.Version != 1 || len(signed.Signature) != ed25519.SignatureSize || !validSourceRetirementStatementV1(signed.Statement) {
		return zero, ErrHandoffInventory
	}
	canonical, err := json.Marshal(signed)
	if err != nil || !bytes.Equal(retirementRaw, append(canonical, '\n')) || signed.Statement.SourceSignerPublicSHA != pins.SourcePublicKeySHA ||
		signed.Statement.FenceDocumentSHA != pins.FenceDocumentSHA ||
		signed.Statement.InventorySHA != pins.InventorySHA || signed.Statement.DestinationSHA != pins.DestinationSHA {
		return zero, ErrHandoffInventory
	}
	message, err := retirementMessageV1(signed.Statement)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(sourcePublic), message, signed.Signature) {
		return zero, ErrHandoffInventory
	}
	var signedWitness SignedSourceRetirementWitnessV1
	if json.Unmarshal(witnessRaw, &signedWitness) != nil || signedWitness.Version != 1 || len(signedWitness.Signature) != ed25519.SignatureSize ||
		!validSourceRetirementWitnessV1(signedWitness.Witness, signed.Statement, pins) {
		return zero, ErrHandoffInventory
	}
	canonical, err = json.Marshal(signedWitness)
	if err != nil || !bytes.Equal(witnessRaw, append(canonical, '\n')) {
		return zero, ErrHandoffInventory
	}
	message, err = retirementWitnessMessageV1(signedWitness.Witness)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(witnessPublic), message, signedWitness.Signature) {
		return zero, ErrHandoffInventory
	}
	return VerifiedSourceRetirementV1{statement: signed.Statement, witness: signedWitness.Witness, pins: pins, valid: true}, nil
}

func (v VerifiedSourceRetirementV1) Evidence() (SourceRetirementStatementV1, SourceRetirementWitnessV1, SourceRetirementPinsV1, bool, error) {
	if !v.valid {
		return SourceRetirementStatementV1{}, SourceRetirementWitnessV1{}, SourceRetirementPinsV1{}, false, ErrHandoffInventory
	}
	return v.statement, v.witness, v.pins, false, nil
}

func retirementCoverageDigestV1(v LocalHandoffRetirementCoverageV1) (string, error) {
	v.BundleSHA = ""
	raw, err := json.Marshal(v)
	if err != nil {
		return "", ErrHandoffInventory
	}
	sum := sha256.Sum256(append([]byte("DARK-PRO-DECLARED-SOURCE-RETIREMENT-BUNDLE-v1\x00"), raw...))
	return hex.EncodeToString(sum[:]), nil
}

// CheckLocalHandoffRetirementsV1 requires one independently pinned source
// receipt and one independently pinned witness for every declared inventory
// entry. Coverage remains limited to that declared inventory and cannot prove
// that no undeclared installation exists.
func CheckLocalHandoffRetirementsV1(inventory LocalHandoffInventoryV1, inventoryPin string, handoffs []VerifiedLocalHandoff, retirements []VerifiedSourceRetirementV1, destinationSHA string) (LocalHandoffRetirementCoverageV1, error) {
	var zero LocalHandoffRetirementCoverageV1
	coverage, err := CheckLocalHandoffCoverageV1(inventory, inventoryPin, handoffs)
	canonical, canonicalErr := canonicalHandoffInventory(inventory)
	if err != nil || canonicalErr != nil || !inventoryDigestValid(destinationSHA) || len(retirements) != len(canonical.Sources) {
		return zero, ErrHandoffInventory
	}
	expected := make(map[string]LocalHandoffSourcePinV1, len(canonical.Sources))
	for _, source := range canonical.Sources {
		expected[source.EpochID] = source
	}
	out := LocalHandoffRetirementCoverageV1{
		Scope: "pinned_declared_sources_only", InventorySHA: coverage.InventorySHA,
		DestinationSHA: destinationSHA, DeclaredSources: len(canonical.Sources),
		EpochIDs:               make([]string, 0, len(canonical.Sources)),
		RetirementDocumentSHAs: make([]string, 0, len(canonical.Sources)),
		WitnessDocumentSHAs:    make([]string, 0, len(canonical.Sources)),
	}
	retirementByEpoch := make(map[string]VerifiedSourceRetirementV1, len(retirements))
	for _, verified := range retirements {
		statement, witness, pins, mutation, e := verified.Evidence()
		source, ok := expected[statement.EpochID]
		if e != nil || mutation || !ok || statement.SourceSHA != canonical.SourceSHA || statement.SourceID != canonical.SourceID ||
			statement.InventorySHA != coverage.InventorySHA || statement.DestinationSHA != destinationSHA ||
			statement.Sequence != source.Sequence || statement.TerminalReceiptSHA != source.ReceiptSHA ||
			statement.AuthoritySHA != source.AuthoritySHA || statement.JournalSHA != source.JournalSHA ||
			statement.HandoffSHA != source.HandoffSHA || statement.SourceSignerPublicSHA != source.SignerPublicSHA ||
			witness.InventorySHA != coverage.InventorySHA || witness.DestinationSHA != destinationSHA ||
			pins.SourcePublicKeySHA != source.SignerPublicSHA {
			return zero, ErrHandoffInventory
		}
		if _, exists := retirementByEpoch[statement.EpochID]; exists {
			return zero, ErrHandoffInventory
		}
		retirementByEpoch[statement.EpochID] = verified
	}
	for _, source := range canonical.Sources {
		statement, witness, pins, _, e := retirementByEpoch[source.EpochID].Evidence()
		if e != nil {
			return zero, ErrHandoffInventory
		}
		out.EpochIDs = append(out.EpochIDs, statement.EpochID)
		out.RetirementDocumentSHAs = append(out.RetirementDocumentSHAs, pins.RetirementDocumentSHA)
		out.WitnessDocumentSHAs = append(out.WitnessDocumentSHAs, pins.WitnessDocumentSHA)
		if witness.WitnessedUnixNano > out.LatestWitnessedUnixNano {
			out.LatestWitnessedUnixNano = witness.WitnessedUnixNano
		}
	}
	if !sort.StringsAreSorted(out.EpochIDs) {
		return zero, ErrHandoffInventory
	}
	out.BundleSHA, err = retirementCoverageDigestV1(out)
	if err != nil {
		return zero, err
	}
	return out, nil
}
