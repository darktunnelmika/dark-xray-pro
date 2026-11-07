package business

import (
	"context"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// ListInboundExecutionNodeChannels projects the last durable execution
// observation for each managed Node. Transport liveness and host metrics are
// intentionally not inferred here; the owner API joins those independent
// telemetry channels without converting one channel into another.
func (s *Store) ListInboundExecutionNodeChannels(ctx context.Context, key []byte) ([]ix.TargetState, error) {
	tx, err := s.readSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	enabled, err := executionSchemaPresent(ctx, tx)
	if err != nil || !enabled {
		return []ix.TargetState{}, err
	}

	rows, err := tx.QueryContext(ctx, `SELECT node_id,installation_id,desired_generation,observed_generation,observation_enc
		FROM inbound_execution_nodes ORDER BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ix.TargetState{}
	for rows.Next() {
		t := ix.TargetState{
			State:           "unknown",
			HealthState:     "unknown",
			AccountingState: "unknown",
			Listeners:       []ix.Listener{},
			Blockers:        []ix.Blocker{},
		}
		var raw []byte
		if err = rows.Scan(&t.NodeID, &t.InstallationID, &t.DesiredGeneration, &t.ObservedGeneration, &raw); err != nil {
			return nil, err
		}
		if len(raw) == 0 {
			if t.ObservedGeneration != 0 {
				return nil, ErrUnavailable
			}
			out = append(out, t)
			continue
		}
		if t.ObservedGeneration == 0 {
			return nil, ErrUnavailable
		}
		var observed ix.Observation
		if openPlanValue(key, executionAAD("observation", t.NodeID+"/"+t.InstallationID, t.ObservedGeneration), raw, &observed) != nil ||
			observed.NodeID != t.NodeID || observed.InstallationID != t.InstallationID ||
			observed.Generation != t.ObservedGeneration {
			return nil, ErrUnavailable
		}
		t.State = observed.State
		t.ReportedAt = observed.ObservedUnixNano
		t.LeaseExpires = observed.LeaseExpires
		t.ProcessReady = observed.ProcessReady
		t.HealthState = observed.HealthState
		t.AccountingState = observed.AccountingState
		out = append(out, t)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
