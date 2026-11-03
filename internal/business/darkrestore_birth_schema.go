package business

import (
	"context"
	"strings"
)

var darkRestoreBirthDDL = []string{
	"CREATE TABLE darkrestore_native_birth_v1(plan_sha TEXT PRIMARY KEY CHECK(length(plan_sha)=64),client_id TEXT NOT NULL REFERENCES clients(id),epoch_id TEXT NOT NULL UNIQUE REFERENCES pro_usage_epochs(epoch_id),receipt_json BLOB NOT NULL)",
	"CREATE TRIGGER darkrestore_native_birth_v1_no_update BEFORE UPDATE ON darkrestore_native_birth_v1 BEGIN SELECT RAISE(ABORT,'native birth immutable'); END",
	"CREATE TRIGGER darkrestore_native_birth_v1_no_delete BEFORE DELETE ON darkrestore_native_birth_v1 BEGIN SELECT RAISE(ABORT,'native birth immutable'); END",
}

func birthSchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_native_birth_v1*'").Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreBirthDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreBirthDDL {
		var actual string
		name := strings.Fields(ddl)[2]
		name = strings.SplitN(name, "(", 2)[0]
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil || actual != ddl {
			return false, ErrUnavailable
		}
	}
	return true, nil
}
