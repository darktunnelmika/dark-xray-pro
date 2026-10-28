package replacement

import (
	"bytes"
	"encoding/json"
)

// ValidateOfflineBackup checks the existing persisted contract without opening
// a Journal, taking reservations or modifying operation phases.
func ValidateOfflineBackup(raw []byte) error {
	if len(raw) == 0 || len(raw) > maxFileBytes {
		return ErrUnsafe
	}
	var state disk
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil {
		return ErrUnsafe
	}
	canonical, err := json.Marshal(state)
	if err != nil || !bytes.Equal(raw, canonical) || state.Version != 1 || state.Operations == nil || len(state.Operations) > maxOperations {
		return ErrUnsafe
	}
	reserved := map[string]bool{}
	for id, op := range state.Operations {
		if len(id) != 32 || op.ID != id || op.Revision == 0 || op.NewNodeID == op.OldNodeID || op.Preview.NewNodeID != op.NewNodeID || op.Preview.OldNodeID != op.OldNodeID || op.Preview.ManifestSHA256 == "" || op.CanActivate || !op.NeedsXrayValidation || !op.NeedsDataPlaneProbe || !op.NeedsSubscriptionCommit || len(op.Preview.Deployments) == 0 || len(op.Preview.Deployments) > maxDeployments {
			return ErrUnsafe
		}
		switch op.Phase {
		case Pending, ProbeFailed, TCPChecked, Cancelled:
		default:
			return ErrUnsafe
		}
		if op.Phase != Cancelled {
			if reserved[op.OldNodeID] || reserved[op.NewNodeID] {
				return ErrUnsafe
			}
			reserved[op.OldNodeID], reserved[op.NewNodeID] = true, true
		}
	}
	return nil
}
