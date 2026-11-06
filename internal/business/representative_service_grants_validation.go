package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

func serviceGrantSchemaState(ctx context.Context, db *sql.DB) (bool, error) {
	var n int
	if e := db.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name='representative_service_grants' OR name GLOB 'service_grants_*'`).Scan(&n); e != nil {
		return false, e
	}
	if n == 0 {
		return false, nil
	}
	if n != len(serviceGrantSchema) {
		return false, ErrUnavailable
	}
	for _, ddl := range serviceGrantSchema {
		expected := strings.Replace(ddl, " IF NOT EXISTS", "", 1)
		parts := strings.Fields(expected)
		name := strings.SplitN(parts[2], "(", 2)[0]
		var actual string
		if e := db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&actual); e != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(parts, " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

// Read-only and backward compatible: absent optional grants confer no rights.
func validateServiceGrantData(ctx context.Context, db *sql.DB) error {
	present, e := serviceGrantSchemaState(ctx, db)
	if e != nil || !present {
		return e
	}
	rows, e := db.QueryContext(ctx, `SELECT g.event_id,g.representative_id,g.inbound_id,g.revision,g.enabled,g.request,g.created_unix,COALESCE(r.kind,''),COALESCE(a.role,''),COALESCE(i.id,0) FROM representative_service_grants g LEFT JOIN representatives r ON r.id=g.representative_id LEFT JOIN auth_users a ON a.id=g.actor_id LEFT JOIN native_inbounds i ON i.id=g.inbound_id ORDER BY g.representative_id,g.inbound_id,g.revision`)
	if e != nil {
		return e
	}
	defer rows.Close()
	var previousRep string
	var previousInbound uint64
	var previousRevision int64
	var previousEnabled bool
	for rows.Next() {
		var event, rep, raw, kind, role string
		var inbound, found uint64
		var revision, created int64
		var enabled bool
		if e = rows.Scan(&event, &rep, &inbound, &revision, &enabled, &raw, &created, &kind, &role, &found); e != nil {
			return ErrUnavailable
		}
		if rep != previousRep || inbound != previousInbound {
			previousRevision = 0
			previousEnabled = false
		}
		var request ServiceGrantChange
		if json.Unmarshal([]byte(raw), &request) != nil {
			return ErrUnavailable
		}
		canonical, _ := json.Marshal(request)
		if string(canonical) != raw || kind != "representative" || role != "owner" || found != inbound || inbound == 0 || revision != previousRevision+1 || enabled == previousEnabled || created <= 0 || len(event) < 16 || len(event) > 128 || strings.TrimSpace(request.Reason) == "" || len(request.Reason) > 512 || request.EventID != event || request.RepresentativeID != rep || request.InboundID != inbound || request.ExpectedRevision != revision-1 || request.Enabled != enabled {
			return ErrUnavailable
		}
		previousRep = rep
		previousInbound = inbound
		previousRevision = revision
		previousEnabled = enabled
	}
	return rows.Err()
}
