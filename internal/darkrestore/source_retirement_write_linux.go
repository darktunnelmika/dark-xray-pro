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

// SourceRetirementFileV1 describes one source-signed retirement document.
// It is intentionally incomplete without a separately signed witness and
// independently supplied pins, and therefore never authorizes activation.
type SourceRetirementFileV1 struct {
	SHA                  string                      `json:"sha256"`
	PublicKeySHA         string                      `json:"public_key_sha256"`
	Statement            SourceRetirementStatementV1 `json:"statement"`
	AuthorizesActivation bool                        `json:"authorizes_activation"`
}

// WriteSourceRetirementV1 re-verifies the exact signed handoff and current
// source settlement before signing a claim about one declared inventory row.
// The active-fence document remains opaque: its digest is bound, but this
// function cannot prove the operational truth of its contents or discover
// undeclared installations. A separate witness and independent pins remain
// mandatory at the destination.
func WriteSourceRetirementV1(expected LocalSourceSettlement, handoffPath, handoffSHA, keyPath,
	fencePath, fenceSHA, inventorySHA, destinationSHA, out string,
	retiredUnixNano, fenceObservedUnixNano int64) (SourceRetirementFileV1, error) {
	var zero SourceRetirementFileV1
	if !inventoryDigestValid(handoffSHA) || !inventoryDigestValid(fenceSHA) ||
		!inventoryDigestValid(inventorySHA) || !inventoryDigestValid(destinationSHA) {
		return zero, ErrHandoffInventory
	}
	key, err := privateHandoffBytes(keyPath, ed25519.PrivateKeySize)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return zero, ErrHandoffInventory
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	private := ed25519.PrivateKey(key)
	public := private.Public().(ed25519.PublicKey)
	if !bytes.Equal(public, ed25519.NewKeyFromSeed(key[:ed25519.SeedSize]).Public().(ed25519.PublicKey)) {
		return zero, ErrHandoffInventory
	}
	verified, err := VerifySignedLocalHandoff(handoffPath, handoffSHA, public)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	settlement, err := verified.Settlement()
	if err != nil || settlement != expected {
		return zero, ErrHandoffInventory
	}
	fence, err := privateHandoffBytes(fencePath, 64<<10)
	fenceSum := sha256.Sum256(fence)
	if err != nil || hex.EncodeToString(fenceSum[:]) != fenceSHA {
		return zero, ErrHandoffInventory
	}
	publicSum := sha256.Sum256(public)
	statement := SourceRetirementStatementV1{
		Version: 1, Scope: SourceRetirementScopeV1,
		SourceSHA: settlement.SourceSHA, SourceID: settlement.SourceID,
		InventorySHA: inventorySHA, DestinationSHA: destinationSHA,
		EpochID: settlement.Terminal.EpochID, Sequence: settlement.Terminal.Sequence,
		TerminalReceiptSHA: settlement.Terminal.SHA, AuthoritySHA: settlement.AuthoritySHA,
		JournalSHA: settlement.JournalSHA, HandoffSHA: handoffSHA,
		SourceSignerPublicSHA: hex.EncodeToString(publicSum[:]),
		RetiredUnixNano:       retiredUnixNano, FenceDocumentSHA: fenceSHA,
		FenceObservedUnixNano: fenceObservedUnixNano,
	}
	if !validSourceRetirementStatementV1(statement) {
		return zero, ErrHandoffInventory
	}
	message, err := retirementMessageV1(statement)
	if err != nil {
		return zero, ErrHandoffInventory
	}
	signed := SignedSourceRetirementV1{Version: 1, Statement: statement, Signature: ed25519.Sign(private, message)}
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
	temp, err := os.CreateTemp(dir, ".source-retirement-")
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
	sum := sha256.Sum256(raw)
	return SourceRetirementFileV1{
		SHA: hex.EncodeToString(sum[:]), PublicKeySHA: hex.EncodeToString(publicSum[:]),
		Statement: statement, AuthorizesActivation: false,
	}, nil
}
