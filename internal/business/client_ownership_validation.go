package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
)

// Reopening and offline backup validation reject incomplete or contradictory
// lineage. No source financial row is repaired, reattributed or initialized.
func validateOwnershipData(ctx context.Context, db *sql.DB) error {
	present, e := ownershipSchemaState(ctx, db)
	if e != nil || !present {
		return e
	}
	tx, e := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return e
	}
	defer tx.Rollback()
	after := ""
	for {
		rows, e := tx.QueryContext(ctx, `SELECT client_id FROM client_ownership_origins_v1 WHERE client_id>? ORDER BY client_id LIMIT 64`, after)
		if e != nil {
			return ErrUnavailable
		}
		ids := []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return ErrUnavailable
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return ErrUnavailable
		}
		for _, id := range ids {
			// Ownership lineage needs only stable identity fields. Historical
			// snapshots may predate optional client contact tables; validation
			// must never join those tables or migrate the archive.
			var c Client
			e := tx.QueryRowContext(ctx, `SELECT id,uuid,representative_id FROM clients WHERE id=?`, id).Scan(&c.ID, &c.UUID, &c.RepresentativeID)
			if e != nil {
				return ErrUnavailable
			}
			if _, _, e = ownershipOriginKnown(ctx, tx, c); e != nil {
				return ErrUnavailable
			}
			after = id
		}
		if len(ids) < 64 {
			break
		}
	}
	after = ""
	for {
		rows, e := tx.QueryContext(ctx, `SELECT event_id,fingerprint,actor_user_id,request_json,receipt_json,created_unix FROM client_ownership_receipts_v1 WHERE event_id>? ORDER BY event_id LIMIT 32`, after)
		if e != nil {
			return ErrUnavailable
		}
		type saved struct {
			event, fp, actor string
			request, receipt []byte
			at               int64
		}
		page := []saved{}
		for rows.Next() {
			var r saved
			if e = rows.Scan(&r.event, &r.fp, &r.actor, &r.request, &r.receipt, &r.at); e != nil {
				rows.Close()
				return ErrUnavailable
			}
			page = append(page, r)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return ErrUnavailable
		}
		for _, r := range page {
			var req ClientOwnershipCommit
			var receipt ClientOwnershipReceipt
			if len(r.request) > 64<<10 || len(r.receipt) > 2<<20 || json.Unmarshal(r.request, &req) != nil || json.Unmarshal(r.receipt, &receipt) != nil {
				return ErrUnavailable
			}
			intent, e := canonicalOwnership(req.ClientOwnershipIntent)
			if e != nil {
				return ErrUnavailable
			}
			req.ClientOwnershipIntent = intent
			canonical, _ := json.Marshal(req)
			if !bytes.Equal(canonical, r.request) || !eventRE.MatchString(r.event) || req.EventID != r.event || !idRE.MatchString(r.actor) || ownershipRequestFingerprint(r.actor, req) != r.fp {
				return ErrUnavailable
			}
			canonical, _ = json.Marshal(receipt)
			if !bytes.Equal(canonical, r.receipt) || receipt.EventID != r.event || receipt.ActorUserID != r.actor || receipt.RepresentativeID != intent.RepresentativeID || receipt.GroupID != intent.GroupID || receipt.CreatedUnix != r.at || r.at <= 0 || receipt.Replayed || receipt.Count != len(receipt.Clients) || receipt.Count < 1 || receipt.Count > MaxOwnershipBatch || receipt.BalanceChanged || receipt.UsageReset || receipt.CredentialsChanged {
				return ErrUnavailable
			}
			var count int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM client_ownership_entries_v1 WHERE event_id=?`, r.event).Scan(&count); e != nil || count != receipt.Count {
				return ErrUnavailable
			}
			last := ""
			for _, item := range receipt.Clients {
				if !idRE.MatchString(item.ClientID) || item.ClientID <= last || item.RepresentativeID != receipt.RepresentativeID || item.PreviousRepresentativeID == item.RepresentativeID || !idRE.MatchString(item.PreviousRepresentativeID) || item.GroupID != intent.GroupID || item.PreviousGroupID != "" && !idRE.MatchString(item.PreviousGroupID) || item.Revision < 1 || item.Revision > 9007199254740991 || !validName(item.Name, 200) {
					return ErrUnavailable
				}
				stored := ClientOwnershipItem{ClientID: item.ClientID, Name: item.Name}
				if e = tx.QueryRowContext(ctx, `SELECT revision,previous_representative_id,representative_id,previous_group_id,group_id,usage_present,usage_complete,uplink,downlink,holds FROM client_ownership_entries_v1 WHERE event_id=? AND client_id=?`, r.event, item.ClientID).Scan(&stored.Revision, &stored.PreviousRepresentativeID, &stored.RepresentativeID, &stored.PreviousGroupID, &stored.GroupID, &stored.UsagePresent, &stored.UsageComplete, &stored.Uplink, &stored.Downlink, &stored.Holds); e != nil || stored != item {
					return ErrUnavailable
				}
				if !item.UsagePresent && (item.UsageComplete || item.Uplink != 0 || item.Downlink != 0 || item.Holds != 0) {
					return ErrUnavailable
				}
				if intent.SourceGroupID != "" && item.PreviousGroupID != intent.SourceGroupID {
					return ErrUnavailable
				}
				last = item.ClientID
			}
			if len(intent.ClientIDs) > 0 {
				if len(intent.ClientIDs) != receipt.Count {
					return ErrUnavailable
				}
				for i, id := range intent.ClientIDs {
					if id != receipt.Clients[i].ClientID {
						return ErrUnavailable
					}
				}
			}
			after = r.event
		}
		if len(page) < 32 {
			break
		}
	}
	return nil
}
