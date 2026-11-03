package business

import (
	"context"
	"strings"
)

var darkRestoreSystemProbeRoundDDL = []string{
	"CREATE TABLE darkrestore_system_probe_v1_intent(id TEXT PRIMARY KEY,plan_sha TEXT NOT NULL UNIQUE REFERENCES darkrestore_listener_apply_v1(plan_sha),event_id TEXT NOT NULL UNIQUE,owner_id TEXT NOT NULL REFERENCES auth_users(id),client_id TEXT NOT NULL REFERENCES clients(id),created_ns INTEGER NOT NULL,expires_ns INTEGER NOT NULL,intent_json BLOB NOT NULL)",
	"CREATE TRIGGER darkrestore_system_probe_v1_intent_no_update BEFORE UPDATE ON darkrestore_system_probe_v1_intent BEGIN SELECT RAISE(ABORT,'system probe intent immutable'); END",
	"CREATE TRIGGER darkrestore_system_probe_v1_intent_no_delete BEFORE DELETE ON darkrestore_system_probe_v1_intent BEGIN SELECT RAISE(ABORT,'system probe intent immutable'); END",
	"CREATE TABLE darkrestore_system_probe_v1_request(id TEXT PRIMARY KEY REFERENCES darkrestore_system_probe_v1_intent(id),request_json BLOB NOT NULL)",
	"CREATE TRIGGER darkrestore_system_probe_v1_request_no_update BEFORE UPDATE ON darkrestore_system_probe_v1_request BEGIN SELECT RAISE(ABORT,'system probe request immutable'); END",
	"CREATE TRIGGER darkrestore_system_probe_v1_request_no_delete BEFORE DELETE ON darkrestore_system_probe_v1_request BEGIN SELECT RAISE(ABORT,'system probe request immutable'); END",
}

func darkRestoreSystemProbeRoundSchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_system_probe_v1_*'").Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreSystemProbeRoundDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreSystemProbeRoundDDL {
		name := strings.SplitN(strings.Fields(ddl)[2], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil || actual != ddl {
			return false, ErrUnavailable
		}
	}
	return true, nil
}
