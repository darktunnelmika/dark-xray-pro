package business

import (
	"context"
	"strings"
)

var darkRestoreServingHealthDDL = []string{
	"CREATE TABLE darkrestore_serving_health_v1(client_id TEXT PRIMARY KEY REFERENCES darkrestore_import_entries_v1(client_id),plan_sha TEXT NOT NULL UNIQUE REFERENCES darkrestore_listener_apply_v1(plan_sha),request_sha TEXT NOT NULL CHECK(length(request_sha)=64),receipt_sha TEXT NOT NULL CHECK(length(receipt_sha)=64),evidence_json BLOB NOT NULL,recorded_unix_nano INTEGER NOT NULL CHECK(recorded_unix_nano>0))",
	"CREATE TRIGGER darkrestore_serving_health_v1_no_update BEFORE UPDATE ON darkrestore_serving_health_v1 BEGIN SELECT RAISE(ABORT,'dark restore serving health immutable'); END",
	"CREATE TRIGGER darkrestore_serving_health_v1_no_delete BEFORE DELETE ON darkrestore_serving_health_v1 BEGIN SELECT RAISE(ABORT,'dark restore serving health immutable'); END",
}

func darkRestoreServingHealthSchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_serving_health_v1*'").Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreServingHealthDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreServingHealthDDL {
		name := strings.SplitN(strings.Fields(ddl)[2], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil ||
			strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(ddl), " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}
