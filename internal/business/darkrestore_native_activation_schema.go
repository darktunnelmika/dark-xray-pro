package business

import (
	"context"
	"database/sql"
	"strings"
)

var darkRestoreNativeActivationDDL = []string{
	"CREATE TABLE darkrestore_native_activations_v1(client_id TEXT PRIMARY KEY REFERENCES darkrestore_native_apply_acceptances_v1(client_id),event_id TEXT NOT NULL UNIQUE,actor_user_id TEXT NOT NULL REFERENCES auth_users(id),transition_sha256 TEXT NOT NULL CHECK(length(transition_sha256)=64),activation_sha256 TEXT NOT NULL CHECK(length(activation_sha256)=64),request_sha256 TEXT NOT NULL CHECK(length(request_sha256)=64),receipt_json BLOB NOT NULL,recorded_unix_nano INTEGER NOT NULL CHECK(recorded_unix_nano>0))",
	"CREATE TRIGGER darkrestore_native_activations_v1_no_update BEFORE UPDATE ON darkrestore_native_activations_v1 BEGIN SELECT RAISE(ABORT,'dark restore native activation immutable'); END",
	"CREATE TRIGGER darkrestore_native_activations_v1_no_delete BEFORE DELETE ON darkrestore_native_activations_v1 BEGIN SELECT RAISE(ABORT,'dark restore native activation immutable'); END",
}

func darkRestoreNativeActivationSchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_native_activations_v1*'").Scan(&n); err != nil {
		return false, ErrUnavailable
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestoreNativeActivationDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestoreNativeActivationDDL {
		name := strings.SplitN(strings.Fields(ddl)[2], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(ddl), " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

func ensureDarkRestoreNativeActivationSchema(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreNativeActivationSchema(ctx, tx)
	if err != nil || present {
		return err
	}
	accepted, err := darkRestoreNativeApplyAcceptanceSchema(ctx, tx)
	if err != nil || !accepted {
		return ErrUnavailable
	}
	for _, ddl := range darkRestoreNativeActivationDDL {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	return nil
}
