//go:build linux

package darkrestore

import "crypto/ed25519"

// VerifySourceRetirementFilesV1 reads only owner-private, canonical absolute
// paths from trusted startup configuration and then applies the ordinary exact
// document/key/pin verifier. It does not discover sources or grant mutation.
func VerifySourceRetirementFilesV1(in SourceRetirementFilesV1) (VerifiedSourceRetirementV1, error) {
	var zero VerifiedSourceRetirementV1
	retirementRaw, err := privateHandoffBytes(in.RetirementDocumentPath, 16<<10)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	sourcePublic, err := privateHandoffBytes(in.SourcePublicKeyPath, ed25519.PublicKeySize)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	witnessRaw, err := privateHandoffBytes(in.WitnessDocumentPath, 16<<10)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	witnessPublic, err := privateHandoffBytes(in.WitnessPublicKeyPath, ed25519.PublicKeySize)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	return VerifySignedSourceRetirementV1(retirementRaw, sourcePublic, witnessRaw, witnessPublic, in.Pins)
}

// VerifySourceRetirementBundleFilesV1 loads the complete trusted selection and
// independently checks that it covers every declared epoch exactly once. The
// returned coverage still authorizes no mutation or activation.
func VerifySourceRetirementBundleFilesV1(in SourceRetirementBundleFilesV1) ([]VerifiedSourceRetirementV1, LocalHandoffRetirementCoverageV1, error) {
	verified := make([]VerifiedSourceRetirementV1, 0, len(in.Sources))
	for _, source := range in.Sources {
		v, err := VerifySourceRetirementFilesV1(source)
		if err != nil {
			return nil, LocalHandoffRetirementCoverageV1{}, ErrHandoffInventory
		}
		verified = append(verified, v)
	}
	coverage, err := CheckDeclaredSourceRetirementsV1(in.Inventory, in.InventorySHA, verified, in.DestinationSHA)
	if err != nil || coverage.AuthorizesActivation {
		return nil, LocalHandoffRetirementCoverageV1{}, ErrHandoffInventory
	}
	return verified, coverage, nil
}
