package business

import (
	"context"
	"strings"
)

var darkRestoreConnectionDDL = []string{
	"CREATE TABLE darkrestore_connection_v1_intent(id TEXT PRIMARY KEY,plan_sha TEXT NOT NULL UNIQUE REFERENCES darkrestore_listener_apply_v1(plan_sha),event_id TEXT NOT NULL UNIQUE,owner_id TEXT NOT NULL REFERENCES auth_users(id),client_id TEXT NOT NULL REFERENCES clients(id),intent_json BLOB NOT NULL)",
	"CREATE TRIGGER darkrestore_connection_v1_intent_no_update BEFORE UPDATE ON darkrestore_connection_v1_intent BEGIN SELECT RAISE(ABORT,'connection intent immutable'); END",
	"CREATE TRIGGER darkrestore_connection_v1_intent_no_delete BEFORE DELETE ON darkrestore_connection_v1_intent BEGIN SELECT RAISE(ABORT,'connection intent immutable'); END",
	"CREATE TABLE darkrestore_connection_v1_request(id TEXT PRIMARY KEY REFERENCES darkrestore_connection_v1_intent(id),request_json BLOB NOT NULL)",
	"CREATE TRIGGER darkrestore_connection_v1_request_no_update BEFORE UPDATE ON darkrestore_connection_v1_request BEGIN SELECT RAISE(ABORT,'connection request immutable'); END",
	"CREATE TRIGGER darkrestore_connection_v1_request_no_delete BEFORE DELETE ON darkrestore_connection_v1_request BEGIN SELECT RAISE(ABORT,'connection request immutable'); END",
	"CREATE TABLE darkrestore_connection_v1_receipt(id TEXT PRIMARY KEY REFERENCES darkrestore_connection_v1_request(id),receipt_json BLOB NOT NULL,recorded_ns INTEGER NOT NULL)",
	"CREATE TRIGGER darkrestore_connection_v1_receipt_no_update BEFORE UPDATE ON darkrestore_connection_v1_receipt BEGIN SELECT RAISE(ABORT,'connection receipt immutable'); END",
	"CREATE TRIGGER darkrestore_connection_v1_receipt_no_delete BEFORE DELETE ON darkrestore_connection_v1_receipt BEGIN SELECT RAISE(ABORT,'connection receipt immutable'); END",
}

func connectionSchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if e := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_connection_v1_*'").Scan(&n); e != nil {
		return false, e
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreConnectionDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreConnectionDDL {
		name := strings.SplitN(strings.Fields(ddl)[2], "(", 2)[0]
		var actual string
		if e := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); e != nil || actual != ddl {
			return false, ErrUnavailable
		}
	}
	return true, nil
}
