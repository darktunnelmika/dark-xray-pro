package business

import (
	"context"
	"strings"
)

var darkRestoreListenerDDL = []string{
	"CREATE TABLE darkrestore_listener_apply_v1(plan_sha TEXT PRIMARY KEY REFERENCES darkrestore_native_birth_v1(plan_sha),epoch_id TEXT NOT NULL UNIQUE REFERENCES darkrestore_native_admission_v1(epoch_id),receipt_json BLOB NOT NULL)",
	"CREATE TRIGGER darkrestore_listener_apply_v1_no_update BEFORE UPDATE ON darkrestore_listener_apply_v1 BEGIN SELECT RAISE(ABORT,'listener apply immutable'); END",
	"CREATE TRIGGER darkrestore_listener_apply_v1_no_delete BEFORE DELETE ON darkrestore_listener_apply_v1 BEGIN SELECT RAISE(ABORT,'listener apply immutable'); END",
}

func listenerApplySchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_listener_apply_v1*'").Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreListenerDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreListenerDDL {
		name := strings.SplitN(strings.Fields(ddl)[2], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil || actual != ddl {
			return false, ErrUnavailable
		}
	}
	return true, nil
}
