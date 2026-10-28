package noderegistry

import (
	"bytes"
	"encoding/json"
)

// ValidateOfflineBackup validates one canonical persisted registry without
// opening a Registry, creating a directory, expiring invitations or writing.
func ValidateOfflineBackup(raw []byte) error {
	if len(raw) == 0 || len(raw) > maxRegistryBytes {
		return ErrUnsafeState
	}
	var state diskState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil {
		return ErrUnsafeState
	}
	canonical, err := json.Marshal(state)
	if err != nil || !bytes.Equal(raw, canonical) || state.Version != 1 || state.Invites == nil || state.Bindings == nil || state.Receipts == nil || len(state.Invites) > maxPending || len(state.Bindings) > 10000 || len(state.Receipts) > 10000 {
		return ErrUnsafeState
	}
	for id, b := range state.Bindings {
		if !idPattern.MatchString(id) || b.Info.NodeID != id || !hex32Pattern.MatchString(b.Info.InstallationID) || !validName(b.Info.Name) || !hex64Pattern.MatchString(b.TokenHash) || b.Info.PairedAt.IsZero() {
			return ErrUnsafeState
		}
	}
	for digest, invitation := range state.Invites {
		if !hex64Pattern.MatchString(digest) || !idPattern.MatchString(invitation.NodeID) || !validName(invitation.Name) || invitation.ExpiresAt.IsZero() {
			return ErrUnsafeState
		}
	}
	for digest, receipt := range state.Receipts {
		if !hex64Pattern.MatchString(digest) || !idPattern.MatchString(receipt.NodeID) || !hex32Pattern.MatchString(receipt.InstallationID) || !hex64Pattern.MatchString(receipt.TokenHash) {
			return ErrUnsafeState
		}
		b, exists := state.Bindings[receipt.NodeID]
		if !exists || b.Info.InstallationID != receipt.InstallationID || b.TokenHash != receipt.TokenHash {
			return ErrUnsafeState
		}
	}
	return validateRenames(state)
}
