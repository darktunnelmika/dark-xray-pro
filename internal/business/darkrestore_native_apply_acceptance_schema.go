package business

import (
	"context"
	"database/sql"
	"strings"
)

// Native Apply acceptance is optional and append-only. Projected evidence,
// health receipts, or a prepared promotion cannot create it implicitly.
var darkRestoreNativeApplyAcceptanceDDL = []string{
	"CREATE TABLE darkrestore_native_apply_acceptances_v1(client_id TEXT PRIMARY KEY REFERENCES darkrestore_cutover_receipts_v1(client_id),inbound_id INTEGER NOT NULL UNIQUE REFERENCES native_inbound_plan_promotions(inbound_id),event_id TEXT NOT NULL UNIQUE,actor_user_id TEXT NOT NULL REFERENCES auth_users(id),transition_sha256 TEXT NOT NULL CHECK(length(transition_sha256)=64),evidence_sha256 TEXT NOT NULL CHECK(length(evidence_sha256)=64),request_sha256 TEXT NOT NULL CHECK(length(request_sha256)=64),receipt_json BLOB NOT NULL,recorded_unix_nano INTEGER NOT NULL CHECK(recorded_unix_nano>0))",
	"CREATE TRIGGER darkrestore_native_apply_acceptances_v1_no_update BEFORE UPDATE ON darkrestore_native_apply_acceptances_v1 BEGIN SELECT RAISE(ABORT,'dark restore native apply acceptance immutable'); END",
	"CREATE TRIGGER darkrestore_native_apply_acceptances_v1_no_delete BEFORE DELETE ON darkrestore_native_apply_acceptances_v1 BEGIN SELECT RAISE(ABORT,'dark restore native apply acceptance immutable'); END",
}

func darkRestoreNativeApplyAcceptanceSchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_native_apply_acceptances_v1*'").Scan(&n); err != nil {
		return false, ErrUnavailable
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreNativeApplyAcceptanceDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreNativeApplyAcceptanceDDL {
		fields := strings.Fields(ddl)
		name := strings.SplitN(fields[2], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(ddl), " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

func ensureDarkRestoreNativeApplyAcceptanceSchema(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreNativeApplyAcceptanceSchema(ctx, tx)
	if err != nil || present {
		return err
	}
	promotions, err := darkRestorePreparedPromotionSchema(ctx, tx)
	if err != nil || !promotions {
		return ErrUnavailable
	}
	for _, ddl := range darkRestoreNativeApplyAcceptanceDDL {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	return nil
}
