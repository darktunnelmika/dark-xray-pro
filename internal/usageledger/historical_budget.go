package usageledger

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
)

// This optional exact-DDL extension keeps external settled history out of native
// counters and financial credit while reserving it against current entitlement.
var historicalBudgetSchema = []string{
	`CREATE TABLE pro_usage_historical_budgets(client_id TEXT PRIMARY KEY REFERENCES pro_usage_accounts(client_id) ON DELETE RESTRICT,historical_bytes INTEGER NOT NULL CHECK(typeof(historical_bytes)='integer' AND historical_bytes>=0),receipt_sha TEXT NOT NULL CHECK(length(receipt_sha)=64))`,
	`CREATE TRIGGER pro_usage_historical_budgets_no_update BEFORE UPDATE ON pro_usage_historical_budgets BEGIN SELECT RAISE(ABORT,'historical budget immutable'); END`,
	`CREATE TRIGGER pro_usage_historical_budgets_no_delete BEFORE DELETE ON pro_usage_historical_budgets BEGIN SELECT RAISE(ABORT,'historical budget immutable'); END`,
	`CREATE TRIGGER pro_usage_historical_budgets_dirty AFTER INSERT ON pro_usage_historical_budgets WHEN EXISTS(SELECT 1 FROM clients WHERE id=NEW.client_id AND deployment_state!='disabled') BEGIN UPDATE native_config_generation SET value=value+1 WHERE id=1 AND value<9007199254740991; SELECT CASE WHEN changes()!=1 THEN RAISE(ABORT,'config generation exhausted') END; END`,
}

const historicalBudgetEligibility = `CREATE VIEW pro_usage_eligibility AS SELECT c.id AS client_id,CASE WHEN a.client_id IS NOT NULL AND a.baseline_complete=1 AND a.holds=0 AND c.archived=0 AND c.deployment_state!='disabled' AND r.enabled=1 AND c.expires_unix>CAST(strftime('%s','now') AS INTEGER) AND ((c.plan_kind='unlimited' AND h.client_id IS NULL) OR (c.plan_kind='volume' AND a.uplink+a.downlink<c.volume_bytes-COALESCE(h.historical_bytes,0))) THEN 1 ELSE 0 END AS eligible FROM clients c JOIN representatives r ON r.id=c.representative_id LEFT JOIN pro_usage_accounts a ON a.client_id=c.id LEFT JOIN pro_usage_historical_budgets h ON h.client_id=c.id`

func historicalUsageSchema() []string {
	out := append([]string(nil), schema...)
	for i, s := range out {
		if strings.HasPrefix(s, "CREATE VIEW pro_usage_eligibility ") {
			out[i] = historicalBudgetEligibility
		}
	}
	return append(out, historicalBudgetSchema...)
}

type budgetReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func historicalBudgetsPresent(ctx context.Context, c budgetReader) (bool, error) {
	var n int
	if err := c.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_usage_historical_*'`).Scan(&n); err != nil {
		return false, ErrSchema
	}
	if n == 0 {
		return false, nil
	}
	if n != len(historicalBudgetSchema) {
		return false, ErrSchema
	}
	return true, nil
}

// EnableHistoricalBudgetsTx is explicit, internal and transactional. Old binaries
// reject the additional objects/changed view instead of silently ignoring them.
func (l *Ledger) EnableHistoricalBudgetsTx(ctx context.Context, tx *sql.Tx) error {
	if l == nil || tx == nil {
		return ErrStorage
	}
	present, err := historicalBudgetsPresent(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return verifySchemaObjects(ctx, tx, historicalUsageSchema(), "pro_usage_*")
	}
	if err = verifySchemaObjects(ctx, tx, schema, "pro_usage_*"); err != nil {
		return err
	}
	for _, ddl := range historicalBudgetSchema {
		if _, err = tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `DROP VIEW pro_usage_eligibility`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, historicalBudgetEligibility); err != nil {
		return err
	}
	return verifySchemaObjects(ctx, tx, historicalUsageSchema(), "pro_usage_*")
}

type HistoricalBudget struct {
	ClientID        string
	HistoricalBytes int64
	ReceiptSHA      string
}

func validBudgetDigest(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == s
}

// ReserveHistoricalBudgetTx records one immutable settlement per imported
// identity. Its caller must independently verify the external evidence and bind
// ReceiptSHA to its complete canonical receipt in this same transaction.
func (l *Ledger) ReserveHistoricalBudgetTx(ctx context.Context, tx *sql.Tx, b HistoricalBudget) error {
	if l == nil || tx == nil || !idRE.MatchString(b.ClientID) || b.HistoricalBytes < 0 || !validBudgetDigest(b.ReceiptSHA) {
		return ErrConflict
	}
	if present, err := historicalBudgetsPresent(ctx, tx); err != nil || !present {
		return ErrSchema
	}
	if err := verifySchemaObjects(ctx, tx, historicalUsageSchema(), "pro_usage_*"); err != nil {
		return err
	}
	var old HistoricalBudget
	old.ClientID = b.ClientID
	err := tx.QueryRowContext(ctx, `SELECT historical_bytes,receipt_sha FROM pro_usage_historical_budgets WHERE client_id=?`, b.ClientID).Scan(&old.HistoricalBytes, &old.ReceiptSHA)
	if err == nil {
		if old == b {
			return nil
		}
		return ErrConflict
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var plan, state string
	if err = tx.QueryRowContext(ctx, `SELECT plan_kind,deployment_state FROM clients WHERE id=?`, b.ClientID).Scan(&plan, &state); err != nil || plan != "volume" || state != "disabled" {
		return ErrConflict
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO pro_usage_historical_budgets(client_id,historical_bytes,receipt_sha) VALUES(?,?,?)`, b.ClientID, b.HistoricalBytes, b.ReceiptSHA)
	return err
}

func ReadHistoricalBudget(ctx context.Context, q budgetReader, id string) (HistoricalBudget, error) {
	b := HistoricalBudget{ClientID: id}
	if present, err := historicalBudgetsPresent(ctx, q); err != nil || !present {
		return b, ErrSchema
	}
	err := q.QueryRowContext(ctx, `SELECT historical_bytes,receipt_sha FROM pro_usage_historical_budgets WHERE client_id=?`, id).Scan(&b.HistoricalBytes, &b.ReceiptSHA)
	if err != nil {
		return b, err
	}
	if b.HistoricalBytes < 0 || !validBudgetDigest(b.ReceiptSHA) {
		return b, ErrSchema
	}
	return b, nil
}

// HistoricalBudgetsEnabled reports the optional family state. Exact DDL is
// validated at ledger startup/upgrade and official offline backup validation.
func HistoricalBudgetsEnabled(ctx context.Context, q budgetReader) (bool, error) {
	return historicalBudgetsPresent(ctx, q)
}
