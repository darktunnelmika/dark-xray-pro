//go:build linux

package darkrestore

import "crypto/ed25519"

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
