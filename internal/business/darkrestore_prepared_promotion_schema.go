package business

import (
	"context"
	"database/sql"
	"strings"
)

// The prepared promotion receipt is deliberately optional. Import, M5
// evidence, and a prepared export do not create this schema and therefore
// cannot move a client out of quarantine by themselves.
var darkRestorePreparedPromotionDDL = []string{
	"CREATE TABLE darkrestore_prepared_promotions_v1(client_id TEXT PRIMARY KEY REFERENCES darkrestore_cutover_receipts_v1(client_id),inbound_id INTEGER NOT NULL UNIQUE REFERENCES native_inbound_plan_promotions(inbound_id),event_id TEXT NOT NULL UNIQUE,promotion_event_id TEXT NOT NULL UNIQUE,actor_user_id TEXT NOT NULL REFERENCES auth_users(id),transition_sha256 TEXT NOT NULL CHECK(length(transition_sha256)=64),prepared_authority_sha256 TEXT NOT NULL CHECK(length(prepared_authority_sha256)=64),prepared_content_sha256 TEXT NOT NULL CHECK(length(prepared_content_sha256)=64),promotion_receipt_sha256 TEXT NOT NULL CHECK(length(promotion_receipt_sha256)=64),pre_state_sha256 TEXT NOT NULL CHECK(length(pre_state_sha256)=64),post_state_sha256 TEXT NOT NULL CHECK(length(post_state_sha256)=64),request_sha256 TEXT NOT NULL CHECK(length(request_sha256)=64),receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL,created_unix_nano INTEGER NOT NULL CHECK(created_unix_nano>0))",
	"CREATE TRIGGER darkrestore_prepared_promotions_v1_no_update BEFORE UPDATE ON darkrestore_prepared_promotions_v1 BEGIN SELECT RAISE(ABORT,'dark restore prepared promotion immutable'); END",
	"CREATE TRIGGER darkrestore_prepared_promotions_v1_no_delete BEFORE DELETE ON darkrestore_prepared_promotions_v1 BEGIN SELECT RAISE(ABORT,'dark restore prepared promotion immutable'); END",
}

func darkRestorePreparedPromotionSchema(ctx context.Context, q sellerQueryV2) (bool, error) {
	var n int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'darkrestore_prepared_promotions_v1*'").Scan(&n); err != nil {
		return false, ErrUnavailable
	}
	if n == 0 {
		return false, nil
	}
	if n != len(darkRestorePreparedPromotionDDL) {
		return false, ErrUnavailable
	}
	for _, ddl := range darkRestorePreparedPromotionDDL {
		fields := strings.Fields(ddl)
		name := strings.SplitN(fields[2], "(", 2)[0]
		var actual string
		if err := q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual); err != nil ||
			strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(ddl), " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

func ensureDarkRestorePreparedPromotionSchema(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestorePreparedPromotionSchema(ctx, tx)
	if err != nil || present {
		return err
	}
	cutover, err := darkRestoreCutoverSchemaState(ctx, tx)
	if err != nil || !cutover {
		return ErrUnavailable
	}
	for _, ddl := range darkRestorePreparedPromotionDDL {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	return nil
}
