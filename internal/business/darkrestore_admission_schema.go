package business

import (
	"context"
	"strings"
)

var darkRestoreAdmissionDDL = []string{
	"CREATE TABLE darkrestore_native_admission_v1(epoch_id TEXT PRIMARY KEY REFERENCES pro_usage_epochs(epoch_id),plan_sha TEXT NOT NULL UNIQUE REFERENCES darkrestore_native_birth_v1(plan_sha),event_id TEXT NOT NULL UNIQUE,receipt_json BLOB NOT NULL)",
	"CREATE TRIGGER darkrestore_native_admission_v1_no_update BEFORE UPDATE ON darkrestore_native_admission_v1 BEGIN SELECT RAISE(ABORT,'native admission immutable'); END",
	"CREATE TRIGGER darkrestore_native_admission_v1_no_delete BEFORE DELETE ON darkrestore_native_admission_v1 BEGIN SELECT RAISE(ABORT,'native admission immutable'); END",
}

func admissionSchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_native_admission_v1*'").Scan(&n); err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreAdmissionDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreAdmissionDDL {
		var actual string
		name := strings.SplitN(strings.Fields(ddl)[2], "(", 2)[0]
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil || actual != ddl {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

func hasDarkRestoreNativeAdmission(ctx context.Context, q sellerQueryV2, client string) (bool, error) {
	present, err := admissionSchema(ctx, q)
	if err != nil || !present {
		return false, err
	}
	var found bool
	err = q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM darkrestore_native_admission_v1 a JOIN darkrestore_native_birth_v1 b ON b.plan_sha=a.plan_sha WHERE b.client_id=?)", client).Scan(&found)
	return found, err
}
