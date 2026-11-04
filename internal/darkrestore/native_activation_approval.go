package darkrestore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const maxNativeActivationApprovalLeaseNano int64 = 5 * 60 * 1_000_000_000

// NativeActivationApprovalPinsV1 is independent trusted operator
// configuration. None of these values may be selected by a browser request or
// copied from the incoming approval envelope.
type NativeActivationApprovalPinsV1 struct {
	DocumentSHA           string `json:"document_sha256"`
	PublicKeySHA          string `json:"public_key_sha256"`
	AuthoritySHA          string `json:"authority_sha256"`
	ActivationEvidenceSHA string `json:"activation_evidence_sha256"`
	DestinationSHA        string `json:"destination_sha256"`
	InventorySHA          string `json:"inventory_sha256"`
}

// NativeActivationApprovalV1 is an operator's bounded assertion about one
// exact, already prepared native activation. It deliberately repeats the
// financial and delivery state instead of approving a loose client ID.
//
// A valid signature identifies the claimant. It does not discover undeclared
// serving sources, prove that retirement really occurred, or itself authorize
// a Store mutation.
type NativeActivationApprovalV1 struct {
	Version                     int    `json:"version"`
	Scope                       string `json:"scope"`
	ApprovalID                  string `json:"approval_id"`
	OperatorID                  string `json:"operator_id"`
	AuthoritySHA                string `json:"authority_sha256"`
	ApprovedUnixNano            int64  `json:"approved_unix_nano,string"`
	ExpiresUnixNano             int64  `json:"expires_unix_nano,string"`
	ActivationEvidenceSHA       string `json:"activation_evidence_sha256"`
	DestinationSHA              string `json:"destination_sha256"`
	DestinationStateSHA         string `json:"destination_state_sha256"`
	InventorySHA                string `json:"inventory_sha256"`
	CoverageApprovalSHA         string `json:"coverage_approval_sha256"`
	BillingAttributionSHA       string `json:"billing_attribution_sha256"`
	ImportReceiptSHA            string `json:"import_receipt_sha256"`
	NativeBaselineSHA           string `json:"native_baseline_sha256"`
	PreparedAuthoritySHA        string `json:"prepared_authority_sha256"`
	TransitionSHA               string `json:"transition_sha256"`
	NativeApplyEvidenceSHA      string `json:"native_apply_evidence_sha256"`
	RepresentativeID            string `json:"representative_id"`
	CurrentFiniteEntitlement    int64  `json:"current_finite_entitlement,string"`
	SettledHistoricalBytes      int64  `json:"settled_historical_bytes,string"`
	NativePostMigrationBytes    int64  `json:"native_post_migration_bytes,string"`
	RemainingBudgetBytes        int64  `json:"remaining_budget_bytes,string"`
	InboundID                   uint64 `json:"inbound_id"`
	InitialApplySHA             string `json:"initial_apply_sha256"`
	HealthRoundID               string `json:"health_round_id"`
	DeliveryRevision            uint64 `json:"delivery_revision"`
	SnapshotID                  string `json:"snapshot_id"`
	SnapshotEventID             string `json:"snapshot_event_id"`
	SnapshotRevision            uint64 `json:"snapshot_revision"`
	ContentSHA                  string `json:"content_sha256"`
	DeliveredUnixNano           int64  `json:"delivered_unix_nano,string"`
	SubscriptionExpiresUnixNano int64  `json:"subscription_expires_unix_nano,string"`
	InventoryComplete           bool   `json:"inventory_complete"`
	DeclaredSourcesRetired      bool   `json:"declared_sources_retired"`
	LegacyEntitlementResolved   bool   `json:"legacy_entitlement_resolved"`
}

type SignedNativeActivationApprovalV1 struct {
	Version   int                        `json:"version"`
	Approval  NativeActivationApprovalV1 `json:"approval"`
	Signature []byte                     `json:"signature"`
}

type VerifiedNativeActivationApprovalV1 struct {
	approval NativeActivationApprovalV1
	raw      []byte
	public   []byte
	pins     NativeActivationApprovalPinsV1
	valid    bool
}

func nativeActivationApprovalMessageV1(v NativeActivationApprovalV1) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, ErrHandoffInventory
	}
	return append([]byte("DARK-PRO-NATIVE-ACTIVATION-OPERATOR-APPROVAL-v1\x00"), raw...), nil
}

// NativeActivationApprovalSigningMessageV1 returns the exact domain-separated
// message an offline operator signs. It validates no evidence and grants no
// authority; the destination must still call the verifier with independent
// pins and an authoritative clock.
func NativeActivationApprovalSigningMessageV1(v NativeActivationApprovalV1) ([]byte, error) {
	return nativeActivationApprovalMessageV1(v)
}

func nativeActivationApprovalValidV1(v NativeActivationApprovalV1, now int64) bool {
	for _, digest := range []string{
		v.AuthoritySHA, v.ActivationEvidenceSHA, v.DestinationSHA, v.DestinationStateSHA,
		v.InventorySHA, v.CoverageApprovalSHA, v.BillingAttributionSHA, v.ImportReceiptSHA,
		v.NativeBaselineSHA, v.PreparedAuthoritySHA, v.TransitionSHA, v.NativeApplyEvidenceSHA,
		v.InitialApplySHA, v.ContentSHA,
	} {
		if !inventoryDigestValid(digest) {
			return false
		}
	}
	remaining := v.CurrentFiniteEntitlement - v.SettledHistoricalBytes
	if v.SettledHistoricalBytes > v.CurrentFiniteEntitlement || v.NativePostMigrationBytes > remaining {
		return false
	}
	remaining -= v.NativePostMigrationBytes
	return v.Version == 1 && v.Scope == "darkrestore_native_activation_operator_approval_v1" &&
		v.ApprovalID != "" && len(v.ApprovalID) <= 128 && v.OperatorID != "" && len(v.OperatorID) <= 128 &&
		v.RepresentativeID != "" && len(v.RepresentativeID) <= 256 &&
		v.HealthRoundID != "" && len(v.HealthRoundID) <= 256 &&
		v.SnapshotID != "" && len(v.SnapshotID) <= 256 &&
		v.SnapshotEventID != "" && len(v.SnapshotEventID) <= 256 &&
		v.ApprovedUnixNano > 0 && v.ApprovedUnixNano >= v.DeliveredUnixNano &&
		v.ExpiresUnixNano > v.ApprovedUnixNano && v.ExpiresUnixNano-v.ApprovedUnixNano <= maxNativeActivationApprovalLeaseNano &&
		now >= v.ApprovedUnixNano && now <= v.ExpiresUnixNano &&
		v.SubscriptionExpiresUnixNano > now && v.CurrentFiniteEntitlement > 0 &&
		v.SettledHistoricalBytes >= 0 && v.NativePostMigrationBytes >= 0 &&
		v.RemainingBudgetBytes == remaining && v.InboundID > 0 && v.DeliveryRevision > 0 &&
		v.SnapshotRevision > 0 && v.DeliveredUnixNano > 0 &&
		v.InventoryComplete && v.DeclaredSourcesRetired && v.LegacyEntitlementResolved
}

// VerifySignedNativeActivationApprovalV1 validates a canonical signed approval
// against independently supplied pins and a precise authoritative clock. The
// returned value remains read-only evidence; AuthorizesMutation is always
// false until a separate trusted Store adapter rechecks current state in one
// transaction.
func VerifySignedNativeActivationApprovalV1(raw, public []byte, pins NativeActivationApprovalPinsV1, now int64) (VerifiedNativeActivationApprovalV1, error) {
	var zero VerifiedNativeActivationApprovalV1
	for _, digest := range []string{pins.DocumentSHA, pins.PublicKeySHA, pins.AuthoritySHA, pins.ActivationEvidenceSHA, pins.DestinationSHA, pins.InventorySHA} {
		if !inventoryDigestValid(digest) {
			return zero, ErrHandoffInventory
		}
	}
	if now <= 0 || len(raw) == 0 || len(raw) > 32<<10 || len(public) != ed25519.PublicKeySize || !bytes.HasSuffix(raw, []byte("\n")) {
		return zero, ErrHandoffInventory
	}
	document, key := sha256.Sum256(raw), sha256.Sum256(public)
	if hex.EncodeToString(document[:]) != pins.DocumentSHA || hex.EncodeToString(key[:]) != pins.PublicKeySHA {
		return zero, ErrHandoffInventory
	}
	var signed SignedNativeActivationApprovalV1
	if json.Unmarshal(raw, &signed) != nil || signed.Version != 1 || len(signed.Signature) != ed25519.SignatureSize || !nativeActivationApprovalValidV1(signed.Approval, now) {
		return zero, ErrHandoffInventory
	}
	canonical, err := json.Marshal(signed)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) {
		return zero, ErrHandoffInventory
	}
	approval := signed.Approval
	if approval.AuthoritySHA != pins.AuthoritySHA || approval.ActivationEvidenceSHA != pins.ActivationEvidenceSHA ||
		approval.DestinationSHA != pins.DestinationSHA || approval.InventorySHA != pins.InventorySHA {
		return zero, ErrHandoffInventory
	}
	message, err := nativeActivationApprovalMessageV1(approval)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(public), message, signed.Signature) {
		return zero, ErrHandoffInventory
	}
	return VerifiedNativeActivationApprovalV1{approval: approval, raw: append([]byte(nil), raw...), public: append([]byte(nil), public...), pins: pins, valid: true}, nil
}

// Evidence returns copies and an explicit false mutation flag. A valid signed
// envelope is necessary evidence, never a complete operational authorization.
func (v VerifiedNativeActivationApprovalV1) Evidence() (NativeActivationApprovalV1, []byte, []byte, NativeActivationApprovalPinsV1, bool, error) {
	if !v.valid {
		return NativeActivationApprovalV1{}, nil, nil, NativeActivationApprovalPinsV1{}, false, ErrHandoffInventory
	}
	return v.approval, append([]byte(nil), v.raw...), append([]byte(nil), v.public...), v.pins, false, nil
}
