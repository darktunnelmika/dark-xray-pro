//go:build linux

package darkrestore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// SourceRetirementWitnessFileV1 describes one independently signed witness
// document. Destination trust pins are still external and this result never
// authorizes activation.
type SourceRetirementWitnessFileV1 struct {
	SHA                  string                    `json:"sha256"`
	PublicKeySHA         string                    `json:"public_key_sha256"`
	Witness              SourceRetirementWitnessV1 `json:"witness"`
	AuthorizesActivation bool                      `json:"authorizes_activation"`
}

// WriteSourceRetirementWitnessV1 authenticates the pinned source statement and
// opaque fence document before emitting a distinct-key witness assertion. It
// identifies what the witness signed; it cannot establish operational truth or
// the absence of undeclared sources.
func WriteSourceRetirementWitnessV1(retirementPath, sourcePublicPath, fencePath, witnessKeyPath, out string,
	pins SourceRetirementStatementPinsV1, witnessedUnixNano int64) (SourceRetirementWitnessFileV1, error) {
	var zero SourceRetirementWitnessFileV1
	retirementRaw, err := privateHandoffBytes(retirementPath, 16<<10)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	sourcePublic, err := privateHandoffBytes(sourcePublicPath, ed25519.PublicKeySize)
	if err != nil || len(sourcePublic) != ed25519.PublicKeySize {
		return zero, ErrHandoffInventory
	}
	verified, err := VerifySignedSourceRetirementStatementV1(retirementRaw, sourcePublic, pins)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	statement, _, mutation, err := verified.Evidence()
	if err != nil || mutation {
		return zero, ErrHandoffInventory
	}
	fenceRaw, err := privateHandoffBytes(fencePath, 64<<10)
	fenceSum := sha256.Sum256(fenceRaw)
	if err != nil || hex.EncodeToString(fenceSum[:]) != pins.FenceDocumentSHA {
		return zero, ErrHandoffInventory
	}
	key, err := privateHandoffBytes(witnessKeyPath, ed25519.PrivateKeySize)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return zero, ErrHandoffInventory
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	private := ed25519.PrivateKey(key)
	witnessPublic := private.Public().(ed25519.PublicKey)
	if !bytes.Equal(witnessPublic, ed25519.NewKeyFromSeed(key[:ed25519.SeedSize]).Public().(ed25519.PublicKey)) ||
		bytes.Equal(witnessPublic, sourcePublic) {
		return zero, ErrHandoffInventory
	}
	witness := SourceRetirementWitnessV1{
		Version: 1, Scope: SourceRetirementWitnessScopeV1,
		RetirementDocumentSHA: pins.RetirementDocumentSHA, SourcePublicKeySHA: pins.SourcePublicKeySHA,
		InventorySHA: pins.InventorySHA, DestinationSHA: pins.DestinationSHA,
		WitnessedUnixNano: witnessedUnixNano,
	}
	if !validSourceRetirementWitnessV1(witness, statement, SourceRetirementPinsV1{
		RetirementDocumentSHA: pins.RetirementDocumentSHA, SourcePublicKeySHA: pins.SourcePublicKeySHA,
		FenceDocumentSHA: pins.FenceDocumentSHA, InventorySHA: pins.InventorySHA, DestinationSHA: pins.DestinationSHA,
	}) {
		return zero, ErrHandoffInventory
	}
	message, err := retirementWitnessMessageV1(witness)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	signed := SignedSourceRetirementWitnessV1{Version: 1, Witness: witness, Signature: ed25519.Sign(private, message)}
	raw, err := json.Marshal(signed)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	raw = append(raw, '\n')
	if !filepath.IsAbs(out) || filepath.Clean(out) != out {
		return zero, ErrHandoffInventory
	}
	dir := filepath.Dir(out)
	canonical, evalErr := filepath.EvalSymlinks(dir)
	info, statErr := os.Stat(dir)
	if evalErr != nil || statErr != nil || canonical != dir || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return zero, ErrHandoffInventory
	}
	temp, err := os.CreateTemp(dir, ".source-retirement-witness-")
	if err != nil {
		return zero, err
	}
	defer os.Remove(temp.Name())
	if err = temp.Chmod(0600); err != nil {
		temp.Close()
		return zero, err
	}
	if _, err = temp.Write(raw); err != nil {
		temp.Close()
		return zero, err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return zero, err
	}
	if err = temp.Close(); err != nil {
		return zero, err
	}
	if err = os.Link(temp.Name(), out); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return zero, err
		}
		existing, readErr := privateHandoffBytes(out, 16<<10)
		if readErr != nil || !bytes.Equal(existing, raw) {
			return zero, ErrHandoffInventory
		}
	}
	d, err := os.Open(dir)
	if err != nil {
		return zero, err
	}
	err = d.Sync()
	d.Close()
	if err != nil {
		return zero, err
	}
	witnessSum, publicSum := sha256.Sum256(raw), sha256.Sum256(witnessPublic)
	return SourceRetirementWitnessFileV1{
		SHA: hex.EncodeToString(witnessSum[:]), PublicKeySHA: hex.EncodeToString(publicSum[:]),
		Witness: witness, AuthorizesActivation: false,
	}, nil
}
