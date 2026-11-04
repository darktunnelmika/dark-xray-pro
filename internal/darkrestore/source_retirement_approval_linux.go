//go:build linux

package darkrestore

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
)

// SourceRetirementApprovalReportV1 binds one independently pinned coverage
// approval to one already verified manifest. It remains a read-only report and
// cannot authorize a Store mutation or establish operational truth.
type SourceRetirementApprovalReportV1 struct {
	Manifest                   SourceRetirementManifestReportV1 `json:"manifest"`
	CoverageApprovalSHA        string                           `json:"coverage_approval_sha256"`
	CoverageAuthorityPublicSHA string                           `json:"coverage_authority_public_key_sha256"`
	ApprovedUnixNano           int64                            `json:"approved_unix_nano,string"`
	AuthorizesActivation       bool                             `json:"authorizes_activation"`
}

// SourceRetirementApprovalEvidenceReportV1 proves only that the four opaque
// destination documents named by the signed approval were present as exact,
// independently pinned owner-private files. Their semantics and operational
// truth are intentionally outside this read-only verifier.
type SourceRetirementApprovalEvidenceReportV1 struct {
	Approval              SourceRetirementApprovalReportV1 `json:"approval"`
	DestinationStateSHA   string                           `json:"destination_state_sha256"`
	NativeBaselineSHA     string                           `json:"native_baseline_sha256"`
	ImportReceiptSHA      string                           `json:"import_receipt_sha256"`
	BillingAttributionSHA string                           `json:"billing_attribution_sha256"`
	AuthorizesActivation  bool                             `json:"authorizes_activation"`
}

// VerifySourceRetirementApprovalFilesV1 loads one owner-private manifest,
// approval document and approval key through independently supplied pins. The
// approval must cover the exact declared bundle and follow every witness. This
// verifies an assertion; it does not prove inventory completeness or source
// retirement and never grants activation.
func VerifySourceRetirementApprovalFilesV1(manifestPath, manifestSHA, approvalPath, approvalSHA, approvalPublicKeyPath, approvalPublicKeySHA string) (SourceRetirementApprovalReportV1, error) {
	var zero SourceRetirementApprovalReportV1
	manifest, report, err := loadSourceRetirementManifestFileV1(manifestPath, manifestSHA)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	approvalRaw, err := privateHandoffBytes(approvalPath, 16<<10)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	approvalPublic, err := privateHandoffBytes(approvalPublicKeyPath, ed25519.PublicKeySize)
	if err != nil || len(approvalPublic) != ed25519.PublicKeySize {
		return zero, ErrHandoffInventory
	}
	verified, err := VerifySignedHandoffCoverageApprovalV1(approvalRaw, approvalSHA, approvalPublic, approvalPublicKeySHA)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	approval, documentSHA, publicSHA, err := verified.Evidence()
	if err != nil || approval.InventorySHA != report.Coverage.InventorySHA ||
		approval.SourceSHA != manifest.Inventory.SourceSHA || approval.SourceID != manifest.Inventory.SourceID ||
		approval.DestinationSHA != report.Coverage.DestinationSHA ||
		approval.RetirementBundleSHA != report.Coverage.BundleSHA ||
		approval.DeclaredSources != report.Coverage.DeclaredSources ||
		!equalStrings(approval.EpochIDs, report.Coverage.EpochIDs) ||
		approval.ApprovedUnixNano <= report.Coverage.LatestWitnessedUnixNano {
		return zero, ErrHandoffInventory
	}
	return SourceRetirementApprovalReportV1{
		Manifest: report, CoverageApprovalSHA: documentSHA,
		CoverageAuthorityPublicSHA: publicSHA, ApprovedUnixNano: approval.ApprovedUnixNano,
		AuthorizesActivation: false,
	}, nil
}

func verifyOpaqueApprovalDocumentV1(path, pin string) error {
	if !inventoryDigestValid(pin) {
		return ErrHandoffInventory
	}
	raw, err := privateHandoffBytes(path, 1<<20)
	if err != nil {
		return ErrHandoffInventory
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin {
		return ErrHandoffInventory
	}
	return nil
}

// VerifySourceRetirementApprovalEvidenceFilesV1 adds availability and exact
// byte binding for every opaque destination document named by the approval.
// It neither interprets those documents nor grants Store mutation authority.
func VerifySourceRetirementApprovalEvidenceFilesV1(
	manifestPath, manifestSHA, approvalPath, approvalSHA, approvalPublicKeyPath, approvalPublicKeySHA string,
	destinationStatePath, destinationStateSHA, nativeBaselinePath, nativeBaselineSHA,
	importReceiptPath, importReceiptSHA, billingAttributionPath, billingAttributionSHA string,
) (SourceRetirementApprovalEvidenceReportV1, error) {
	var zero SourceRetirementApprovalEvidenceReportV1
	report, err := VerifySourceRetirementApprovalFilesV1(
		manifestPath, manifestSHA, approvalPath, approvalSHA, approvalPublicKeyPath, approvalPublicKeySHA,
	)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	approvalRaw, err := privateHandoffBytes(approvalPath, 16<<10)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	approvalPublic, err := privateHandoffBytes(approvalPublicKeyPath, ed25519.PublicKeySize)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	verified, err := VerifySignedHandoffCoverageApprovalV1(approvalRaw, approvalSHA, approvalPublic, approvalPublicKeySHA)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	approval, _, _, err := verified.Evidence()
	if err != nil || approval.DestinationStateSHA != destinationStateSHA ||
		approval.NativeBaselineSHA != nativeBaselineSHA || approval.ImportReceiptSHA != importReceiptSHA ||
		approval.BillingAttributionSHA != billingAttributionSHA {
		return zero, ErrHandoffInventory
	}
	for _, document := range []struct{ path, pin string }{
		{destinationStatePath, destinationStateSHA}, {nativeBaselinePath, nativeBaselineSHA},
		{importReceiptPath, importReceiptSHA}, {billingAttributionPath, billingAttributionSHA},
	} {
		if verifyOpaqueApprovalDocumentV1(document.path, document.pin) != nil {
			return zero, ErrHandoffInventory
		}
	}
	return SourceRetirementApprovalEvidenceReportV1{
		Approval: report, DestinationStateSHA: destinationStateSHA, NativeBaselineSHA: nativeBaselineSHA,
		ImportReceiptSHA: importReceiptSHA, BillingAttributionSHA: billingAttributionSHA,
		AuthorizesActivation: false,
	}, nil
}
