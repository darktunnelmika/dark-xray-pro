package business

import (
	"context"
	"math/big"
)

// DarkRestoreGroupHistory keeps the original source group accounting separate
// from mutable membership and from native post-migration usage.
type DarkRestoreGroupHistory struct {
	SourceSHA256     string `json:"source_sha256"`
	SourceGroupID    string `json:"source_group_id"`
	SourceName       string `json:"source_name"`
	SourceCount      int64  `json:"source_count"`
	ArchivedCount    int64  `json:"archived_count"`
	PromotedCount    int64  `json:"promoted_count"`
	SnapshotBytes    string `json:"snapshot_bytes"`
	ClosedEpochBytes string `json:"closed_epoch_bytes"`
	HistoricalBytes  string `json:"historical_bytes"`
}

// GroupHistory is an owner-only API read. It does not infer history from current
// members: archived users, promoted users and later assignments cannot reduce it.
func (s *Store) GroupHistory(ctx context.Context, groupID string) ([]DarkRestoreGroupHistory, error) {
	present, err := darkRestoreSchemaState(ctx, s.db)
	if err != nil {
		return nil, err
	}
	result := []DarkRestoreGroupHistory{}
	if !present {
		return result, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT source_sha256,source_group_id,source_name,source_count,archived_count,promoted_count,historical_bytes FROM darkrestore_group_history_v1 WHERE target_group_id=? ORDER BY source_sha256,source_group_id`, groupID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h DarkRestoreGroupHistory
		if err = rows.Scan(&h.SourceSHA256, &h.SourceGroupID, &h.SourceName, &h.SourceCount, &h.ArchivedCount, &h.PromotedCount, &h.SnapshotBytes); err != nil {
			break
		}
		result = append(result, h)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	attached, err := darkRestoreLocalAttachmentSchemaState(ctx, s.db)
	if err != nil {
		return nil, err
	}
	for i := range result {
		h := &result[i]
		snapshot, ok := new(big.Int).SetString(h.SnapshotBytes, 10)
		if !ok || snapshot.Sign() < 0 || h.SourceCount < 0 || h.ArchivedCount < 0 || h.PromotedCount < 0 {
			return nil, ErrUnavailable
		}
		if !attached {
			h.ClosedEpochBytes = "0"
			h.HistoricalBytes = h.SnapshotBytes
			continue
		}
		ids := []string{}
		entries, e := s.db.QueryContext(ctx, `SELECT a.client_id FROM darkrestore_local_settlements_v1 a JOIN darkrestore_import_entries_v1 e ON e.client_id=a.client_id WHERE e.source_sha256=? AND e.source_group_id=? ORDER BY a.client_id`, h.SourceSHA256, h.SourceGroupID)
		if e != nil {
			return nil, e
		}
		for entries.Next() {
			var id string
			if e = entries.Scan(&id); e != nil {
				break
			}
			ids = append(ids, id)
		}
		if e == nil {
			e = entries.Err()
		}
		entries.Close()
		if e != nil {
			return nil, e
		}
		closed := new(big.Int)
		for _, id := range ids {
			attachment, e := readDarkRestoreLocalAttachment(ctx, s.db, id)
			if e != nil {
				return nil, e
			}
			if attachment == nil {
				return nil, ErrUnavailable
			}
			{
				delta := attachment.Settlement.Historical - attachment.Settlement.PriorBytes
				if delta < 0 {
					return nil, ErrUnavailable
				}
				closed.Add(closed, big.NewInt(delta))
			}
		}
		h.ClosedEpochBytes = closed.String()
		h.HistoricalBytes = new(big.Int).Add(snapshot, closed).String()
	}
	return result, nil
}
