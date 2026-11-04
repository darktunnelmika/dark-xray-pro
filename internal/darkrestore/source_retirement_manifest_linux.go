//go:build linux

package darkrestore

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// SourceRetirementManifestSourceV1 names one already-pinned source/witness
// pair. Every path remains trusted local configuration, never browser input.
type SourceRetirementManifestSourceV1 struct {
	RetirementDocumentPath string                 `json:"retirement_document_path"`
	SourcePublicKeyPath    string                 `json:"source_public_key_path"`
	WitnessDocumentPath    string                 `json:"witness_document_path"`
	WitnessPublicKeyPath   string                 `json:"witness_public_key_path"`
	Pins                   SourceRetirementPinsV1 `json:"pins"`
}

// SourceRetirementManifestV1 is an exact owner-private operator selection. Its
// external file digest pins the entire declared inventory, destination and
// source/witness selection. It cannot establish inventory completeness.
type SourceRetirementManifestV1 struct {
	Version        int                                `json:"version"`
	Inventory      LocalHandoffInventoryV1            `json:"inventory"`
	InventorySHA   string                             `json:"inventory_sha256"`
	DestinationSHA string                             `json:"destination_sha256"`
	Sources        []SourceRetirementManifestSourceV1 `json:"sources"`
}

type SourceRetirementManifestReportV1 struct {
	ManifestSHA          string                           `json:"manifest_sha256"`
	Coverage             LocalHandoffRetirementCoverageV1 `json:"coverage"`
	AuthorizesActivation bool                             `json:"authorizes_activation"`
}

// VerifySourceRetirementManifestFileV1 loads one canonical, owner-private,
// independently pinned manifest and verifies every referenced source/witness
// file. It is a read-only preflight and never manufactures pins or authority.
func VerifySourceRetirementManifestFileV1(path, manifestSHA string) (SourceRetirementManifestReportV1, error) {
	var zero SourceRetirementManifestReportV1
	if !inventoryDigestValid(manifestSHA) {
		return zero, ErrHandoffInventory
	}
	raw, err := privateHandoffBytes(path, 256<<10)
	if err != nil || !bytes.HasSuffix(raw, []byte("\n")) {
		return zero, ErrHandoffInventory
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != manifestSHA {
		return zero, ErrHandoffInventory
	}
	var manifest SourceRetirementManifestV1
	if json.Unmarshal(raw, &manifest) != nil || manifest.Version != 1 || len(manifest.Sources) == 0 || len(manifest.Sources) > 256 {
		return zero, ErrHandoffInventory
	}
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) {
		return zero, ErrHandoffInventory
	}
	files := make([]SourceRetirementFilesV1, 0, len(manifest.Sources))
	for _, source := range manifest.Sources {
		if source.RetirementDocumentPath == "" || source.SourcePublicKeyPath == "" ||
			source.WitnessDocumentPath == "" || source.WitnessPublicKeyPath == "" {
			return zero, ErrHandoffInventory
		}
		files = append(files, SourceRetirementFilesV1{
			RetirementDocumentPath: source.RetirementDocumentPath,
			SourcePublicKeyPath:    source.SourcePublicKeyPath,
			WitnessDocumentPath:    source.WitnessDocumentPath,
			WitnessPublicKeyPath:   source.WitnessPublicKeyPath,
			Pins:                   source.Pins,
		})
	}
	_, coverage, err := VerifySourceRetirementBundleFilesV1(SourceRetirementBundleFilesV1{
		Inventory: manifest.Inventory, InventorySHA: manifest.InventorySHA,
		DestinationSHA: manifest.DestinationSHA, Sources: files,
	})
	if err != nil || coverage.AuthorizesActivation {
		return zero, ErrHandoffInventory
	}
	return SourceRetirementManifestReportV1{
		ManifestSHA: manifestSHA, Coverage: coverage, AuthorizesActivation: false,
	}, nil
}
