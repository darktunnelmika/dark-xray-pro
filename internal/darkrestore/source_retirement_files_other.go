//go:build !linux

package darkrestore

func VerifySourceRetirementFilesV1(SourceRetirementFilesV1) (VerifiedSourceRetirementV1, error) {
	return VerifiedSourceRetirementV1{}, ErrHandoffInventory
}

func VerifySourceRetirementBundleFilesV1(SourceRetirementBundleFilesV1) ([]VerifiedSourceRetirementV1, LocalHandoffRetirementCoverageV1, error) {
	return nil, LocalHandoffRetirementCoverageV1{}, ErrHandoffInventory
}
