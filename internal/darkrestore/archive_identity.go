package darkrestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"

	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

var restoreEmail = regexp.MustCompile("^restore_[0-9a-f]{20}@dark[.]restore$")

// The legacy core keeps only the last deletion for an email. The archive event
// records when the operation began, and the owner audit records when it finished.
// A tombstone outside that interval cannot identify the archived subscription.
func archivedIdentities(ctx context.Context, tx *sql.Tx, asOf int64) (map[string]string, error) {
	var tombstones, audit int
	if err := tx.QueryRowContext(ctx, `SELECT
		EXISTS(SELECT 1 FROM sqlite_schema WHERE name='core_client_tombstones'),
		EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name='live_audit')`).Scan(&tombstones, &audit); err != nil {
		return nil, err
	}
	if tombstones == 0 {
		return nil, nil // Earlier layouts retain the identity in the live core table.
	}
	result := make(map[string]string)
	if audit == 0 {
		return result, nil
	}
	var tables int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM pragma_table_list WHERE schema='main' AND name IN ('core_client_tombstones','live_audit') AND type='table'").Scan(&tables); err != nil || tables != 2 {
		return nil, ErrInspection
	}
	rows, err := tx.QueryContext(ctx, `WITH archives AS (
		SELECT restore_id, MIN(at) AS started FROM restore_events
		WHERE event='restore.archived' GROUP BY restore_id HAVING COUNT(*)=1
	), completions AS (
		SELECT target, MIN(at) AS finished FROM live_audit
		WHERE action='dark_restore.archive' AND actor=owner AND detail='soft delete; history preserved'
		GROUP BY target HAVING COUNT(*)=1
	), unique_emails AS (
		SELECT core_email FROM restore_subscriptions GROUP BY core_email HAVING COUNT(*)=1
	)
	SELECT r.id, r.core_email, r.legacy_total, r.legacy_expire, t.body
	FROM restore_subscriptions r
	JOIN unique_emails u ON u.core_email=r.core_email
	JOIN core_client_tombstones t ON t.email=r.core_email
	JOIN archives a ON a.restore_id=r.id AND a.started=r.deleted_at
	JOIN completions c ON c.target=r.id
	WHERE r.deleted_reason='manual' AND r.deleted_at>0 AND r.deleted_at=r.updated_at
	AND r.promoted_at=0 AND r.enabled=0 AND t.inbounds=r.inbound_ids
	AND r.deleted_at<=t.deleted_at AND t.deleted_at<=c.finished AND c.finished<=?
	AND typeof(t.body)='text' AND length(CAST(t.body AS BLOB)) BETWEEN 1 AND 65536
	AND NOT EXISTS(SELECT 1 FROM core_clients live WHERE live.email=r.core_email)
	ORDER BY r.id`, asOf)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, email, body string
		var quota, expiry int64
		if err = rows.Scan(&id, &email, &quota, &expiry, &body); err != nil {
			return nil, err
		}
		if _, duplicate := result[id]; duplicate {
			return nil, ErrInspection
		}
		result[id] = ""
		if !restoreEmail.MatchString(email) || quota < 0 || expiry < 0 || expiry > maxExpireUnix {
			continue
		}
		var fields map[string]json.RawMessage
		if usagewire.Decode([]byte(body), &fields) != nil {
			continue
		}
		var identityEmail string
		var identityQuota, expiryMillis *int64
		if json.Unmarshal(fields["email"], &identityEmail) != nil || identityEmail != email ||
			json.Unmarshal(fields["totalGB"], &identityQuota) != nil || identityQuota == nil || *identityQuota != quota ||
			json.Unmarshal(fields["expiryTime"], &expiryMillis) != nil || expiryMillis == nil || *expiryMillis != expiry*1000 {
			continue
		}
		result[id] = coreUUID(fields["id"])
	}
	return result, rows.Err()
}
