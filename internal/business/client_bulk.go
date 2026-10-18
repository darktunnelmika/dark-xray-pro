package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"sort"
	"time"
)

// BulkClientState is deliberately limited to 50 individually identified
// client records. It never deletes accounts, changes usage, resets volume,
// publishes subscriptions or starts Xray.
type BulkClientState struct {
	EventID   string   `json:"event_id"`
	ClientIDs []string `json:"client_ids"`
	Action    string   `json:"action"`
	Reason    string   `json:"reason"`
}
type BulkClientResult struct {
	Clients  []Client
	Count    int
	Replayed bool
}

// The *first*, sorted client carries the parent batch event ID and a hash of
// the ENTIRE sorted selection. Others receive deterministic derived IDs.
// This reuses the immutable client_operations event namespace without any
// new database schema, and makes an altered selection with the same batch
// ID fail-closed, including entirely disjoint selections.
func (s *Store) BulkSetClientState(ctx context.Context, input BulkClientState, scope string) (BulkClientResult, error) {
	var out BulkClientResult
	if !eventRE.MatchString(input.EventID) || !validName(input.Reason, 200) ||
		(input.Action != "disable" && input.Action != "enable" && input.Action != "archive") ||
		len(input.ClientIDs) < 1 || len(input.ClientIDs) > 50 ||
		(scope != "" && !idRE.MatchString(scope)) {
		return out, ErrInvalid
	}
	ids := append([]string(nil), input.ClientIDs...)
	sort.Strings(ids)
	for i, id := range ids {
		if !idRE.MatchString(id) || (i > 0 && id == ids[i-1]) {
			return out, ErrInvalid
		}
	}
	normalized := BulkClientState{EventID: input.EventID, ClientIDs: ids, Action: input.Action, Reason: input.Reason}
	batchFingerprint := fingerprint("bulk-"+input.Action, normalized)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	rows := make([]Client, 0, len(ids))
	var allReplay bool
	for i, id := range ids {
		eventID := input.EventID
		hash := batchFingerprint
		if i > 0 {
			seed := sha256.Sum256([]byte(input.EventID + "\x00" + id))
			eventID = "bulk-" + hex.EncodeToString(seed[:20])
			op := SetClientState{EventID: eventID, ClientID: id, Action: input.Action, Reason: input.Reason}
			hash = fingerprint(input.Action, op)
		}
		replayed, e := checkClientOp(ctx, tx, eventID, hash, input.Action)
		if e != nil {
			return BulkClientResult{}, e
		}
		if i == 0 {
			allReplay = replayed
		} else if allReplay != replayed {
			// An interrupted or tampered batch may never be partly replayed.
			return BulkClientResult{}, ErrConflict
		}
		c, e := readClient(ctx, tx, id)
		if errors.Is(e, sql.ErrNoRows) {
			return BulkClientResult{}, ErrNotFound
		}
		if e != nil {
			return BulkClientResult{}, e
		}
		if scope != "" && c.RepresentativeID != scope {
			// Authenticated representatives may never alter another tenant.
			return BulkClientResult{}, ErrConflict
		}
		if !allReplay {
			if c.Archived {
				return BulkClientResult{}, ErrConflict
			}
			newState, newArchived := c.DeploymentState, 0
			switch input.Action {
			case "disable":
				if c.DeploymentState == "disabled" {
					return BulkClientResult{}, ErrConflict
				}
				newState = "disabled"
			case "enable":
				if c.DeploymentState != "disabled" {
					return BulkClientResult{}, ErrConflict
				}
				newState = "pending_node_apply"
			case "archive":
				newState = "disabled"
				newArchived = 1
			}
			if _, e = tx.ExecContext(ctx,
				"UPDATE clients SET deployment_state=?,archived=? WHERE id=?",
				newState, newArchived, id); e != nil {
				return BulkClientResult{}, e
			}
			if _, e = tx.ExecContext(ctx,
				"INSERT INTO client_operations(event_id,fingerprint,client_id,representative_id,kind,reason,added_days,added_volume_bytes,volume_credit_debit,unlimited_credit_debit,created_unix) VALUES(?,?,?,?,?,?,0,0,0,0,?)",
				eventID, hash, id, c.RepresentativeID, input.Action, input.Reason, time.Now().UTC().Unix()); e != nil {
				return BulkClientResult{}, e
			}
			c.DeploymentState = newState
			c.Archived = newArchived == 1
		}
		rows = append(rows, c)
	}
	if err = tx.Commit(); err != nil {
		return BulkClientResult{}, err
	}
	return BulkClientResult{Clients: rows, Count: len(rows), Replayed: allReplay}, nil
}
