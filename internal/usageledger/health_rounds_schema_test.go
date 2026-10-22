//go:build linux

package usageledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func healthRoundsSchemaLedger(t *testing.T) (*sql.DB, string, *Ledger, ProbeIdentity, uc.Epoch) {
	t.Helper()
	db, path, l, p, e := probeTestLedger(t)
	for _, enable := range []func(context.Context) error{l.EnableHealthChallenges, l.EnableHealthQueue} {
		if err := enable(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE TABLE native_metered_handovers(id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	return db, path, l, p, e
}

func healthRoundsSchemaRows(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var result [][]any
	for rows.Next() {
		values, destinations := make([]any, len(columns)), make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			t.Fatal(err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMeteredHealthRoundsSchemaAtomicMigrationRetainsAuditAndBalances(t *testing.T) {
	ctx := context.Background()
	db, path, l, p, e := healthRoundsSchemaLedger(t)
	for _, q := range []string{
		`INSERT INTO native_metered_handovers VALUES('legacy-op')`,
		`INSERT INTO pro_health_intents VALUES('legacy-intent','legacy-op','node-one','installation',7612,'endpoint','binding',200,100)`,
		`INSERT INTO pro_health_challenges VALUES('legacy-challenge','legacy-intent','legacy-op','fingerprint','{ "retained" : "request" }',200)`,
		`INSERT INTO pro_health_receipts VALUES('legacy-challenge','legacy-op','{ "retained" : "receipt" }',150)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.RegisterEpoch(ctx, e); err != nil {
		t.Fatal(err)
	}
	for _, r := range []uc.Report{
		probeTestReport(t, e, 1, false, uc.Usage{ClientID: "customer"}, uc.Usage{ClientID: p.ID}),
		probeTestReport(t, e, 2, true, uc.Usage{ClientID: "customer", Uplink: 3, Downlink: 5}, uc.Usage{ClientID: p.ID, Uplink: 7, Downlink: 11}),
	} {
		if _, err := l.Accept(ctx, principal(e), r); err != nil {
			t.Fatal(err)
		}
	}
	queries := []string{
		`SELECT name,type,sql FROM sqlite_master WHERE name GLOB 'pro_health_*' AND name NOT GLOB 'pro_health_round*' ORDER BY name`,
		`SELECT * FROM pro_health_meta ORDER BY id`,
		`SELECT * FROM pro_health_intents ORDER BY id`,
		`SELECT * FROM pro_health_challenges ORDER BY id`,
		`SELECT * FROM pro_health_receipts ORDER BY challenge_id`,
		`SELECT * FROM pro_usage_accounts ORDER BY client_id`,
		`SELECT * FROM pro_probe_accounts ORDER BY probe_id`,
		`SELECT * FROM native_config_generation ORDER BY id`,
	}
	before := make(map[string]string)
	for _, q := range queries {
		before[q] = healthRoundsSchemaRows(t, db, q)
	}
	failure := errors.New("interrupted rounds migration")
	l.checkpoint = func(at string) error {
		if at == "before_commit" {
			return failure
		}
		return nil
	}
	if err := l.EnableHealthRounds(ctx); !errors.Is(err, failure) {
		t.Fatal("before-commit failure lost", err)
	}
	l.checkpoint = nil
	var version, count int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 11 {
		t.Fatal("partial version upgrade", version, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'pro_health_round*'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("partial round schema", count, err)
	}
	l.checkpoint = func(at string) error {
		if at == "after_commit" {
			return failure
		}
		return nil
	}
	if err := l.EnableHealthRounds(ctx); !errors.Is(err, failure) {
		t.Fatal("committed reply loss not surfaced", err)
	}
	l.checkpoint = nil
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = connectDB(t, path)
	var err error
	l, err = Open(ctx, db)
	if err != nil {
		t.Fatal("committed v12 failed reopen", err)
	}
	for _, enable := range []func(context.Context) error{l.EnableHealthRounds, l.EnableHealthQueue, l.EnableHealthChallenges, l.EnableProbeAccounting} {
		if err := enable(ctx); err != nil {
			t.Fatal("idempotent opt-in failed", err)
		}
	}
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 12 {
		t.Fatal("old opt-in downgraded v12", version, err)
	}
	for _, q := range queries {
		if after := healthRoundsSchemaRows(t, db, q); after != before[q] {
			t.Fatal("migration changed retained data or policy", q, before[q], after)
		}
	}
	if err := db.QueryRow(`SELECT (SELECT COUNT(*) FROM pro_health_rounds)+(SELECT COUNT(*) FROM pro_health_round_members)+(SELECT COUNT(*) FROM pro_health_round_receipts)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("migration invented round evidence", count, err)
	}
	a, err := l.Account(ctx, "customer")
	if err != nil || a.Total != 8 {
		t.Fatal("v12 changed customer charge", a.Total, err)
	}
	probe, err := l.ProbeUsage(ctx, p.ID)
	if err != nil || probe.Total != 18 {
		t.Fatal("v12 changed nonbillable probe usage", probe.Total, err)
	}
}

func TestMeteredHealthRoundsRequireReviewedQueueOptIn(t *testing.T) {
	ctx := context.Background()
	for _, version := range []int{8, 9, 10} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db, _ := testDB(t)
			if _, err := db.Exec(`PRAGMA user_version=8`); err != nil {
				t.Fatal(err)
			}
			l, err := Open(ctx, db)
			if err != nil {
				t.Fatal(err)
			}
			if version >= 9 {
				if err = l.EnableProbeAccounting(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if version >= 10 {
				if err = l.EnableHealthChallenges(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if err = l.EnableHealthRounds(ctx); !errors.Is(err, ErrSchema) {
				t.Fatal("unreviewed queue implicitly enabled", err)
			}
			var got int
			if err = db.QueryRow(`PRAGMA user_version`).Scan(&got); err != nil || got != version {
				t.Fatal("rejected migration changed version", got, err)
			}
		})
	}
}

func TestMeteredHealthRoundsSchemaRejectsDamagedOrDowngradedPolicy(t *testing.T) {
	cases := []struct {
		name string
		sql  []string
	}{
		{"missing-receipts", []string{`DROP TABLE pro_health_round_receipts`}},
		{"missing-immutability", []string{`DROP TRIGGER pro_health_round_members_no_update`}},
		{"altered-scope", []string{`DROP TRIGGER pro_health_round_receipt_scope`, `CREATE TRIGGER pro_health_round_receipt_scope BEFORE INSERT ON pro_health_round_receipts BEGIN SELECT 1; END`}},
		{"extra-column", []string{`ALTER TABLE pro_health_rounds ADD COLUMN unreviewed TEXT`}},
		{"unexpected-object", []string{`CREATE TABLE pro_health_unreviewed(id TEXT)`}},
		{"missing-index", []string{`DROP INDEX pro_health_rounds_handover`}},
	}
	for _, version := range []int{8, 9, 10, 11, 13} {
		cases = append(cases, struct {
			name string
			sql  []string
		}{fmt.Sprintf("marker-%d", version), []string{fmt.Sprintf("PRAGMA user_version=%d", version)}})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, _, l, _, _ := healthRoundsSchemaLedger(t)
			if err := l.EnableHealthRounds(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, q := range tc.sql {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Open(context.Background(), db); !errors.Is(err, ErrSchema) {
				t.Fatal("unreviewed round policy accepted", err)
			}
			if err := l.EnableHealthRounds(context.Background()); !errors.Is(err, ErrSchema) {
				t.Fatal("round opt-in repaired or accepted damaged policy", err)
			}
		})
	}
	t.Run("marker-only-upgrade", func(t *testing.T) {
		db, _, _, _, _ := healthRoundsSchemaLedger(t)
		if _, err := db.Exec(`PRAGMA user_version=12`); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(context.Background(), db); !errors.Is(err, ErrSchema) {
			t.Fatal("v12 marker silently created missing round policy", err)
		}
	})
}

func TestMeteredHealthRoundsMigrationSerializesIndependentConnections(t *testing.T) {
	ctx := context.Background()
	db, path, first, _, _ := healthRoundsSchemaLedger(t)
	second, err := Open(ctx, connectDB(t, path))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, ledger := range []*Ledger{first, second} {
		wg.Add(1)
		go func(l *Ledger) {
			defer wg.Done()
			errs <- l.EnableHealthRounds(ctx)
		}(ledger)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("concurrent migration did not converge", err)
		}
	}
	if _, err := Open(ctx, db); err != nil {
		t.Fatal("concurrent migration left unreviewed schema", err)
	}
}

func TestMeteredHealthRoundsReceiptScopeAndImmutableAudit(t *testing.T) {
	ctx := context.Background()
	db, _, l, _, _ := healthRoundsSchemaLedger(t)
	if err := l.EnableHealthRounds(ctx); err != nil {
		t.Fatal(err)
	}
	type scope struct {
		challengeIntent, challengeOp, intentOp, node, installation, endpoint, binding string
		intentInbound, receiptInbound, accepted                                       int
	}
	cases := []struct {
		name   string
		change func(*scope)
		ok     bool
	}{
		{"matching", func(*scope) {}, true},
		{"wrong-challenge-intent", func(s *scope) { s.challengeIntent = "other-intent" }, false},
		{"wrong-challenge-handover", func(s *scope) { s.challengeOp = "other-op" }, false},
		{"wrong-intent-handover", func(s *scope) { s.intentOp = "other-op" }, false},
		{"wrong-node", func(s *scope) { s.node = "other-node" }, false},
		{"wrong-installation", func(s *scope) { s.installation = "other-installation" }, false},
		{"wrong-inbound", func(s *scope) { s.intentInbound = 11 }, false},
		{"wrong-receipt-inbound", func(s *scope) { s.receiptInbound = 11 }, false},
		{"wrong-binding", func(s *scope) { s.binding = "other-binding" }, false},
		{"wrong-endpoint", func(s *scope) { s.endpoint = "other-endpoint" }, false},
		{"before-round", func(s *scope) { s.accepted = 99 }, false},
		{"at-expiry", func(s *scope) { s.accepted = 200 }, false},
		{"after-expiry", func(s *scope) { s.accepted = 201 }, false},
		{"at-creation", func(s *scope) { s.accepted = 100 }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := tx.ExecContext(ctx, q, args...); err != nil {
					t.Fatal(err)
				}
			}
			s := scope{"intent", "op", "op", "node", "installation", "endpoint", "binding", 10, 10, 150}
			tc.change(&s)
			exec(`INSERT INTO native_metered_handovers VALUES('op'),('other-op')`)
			exec(`INSERT INTO pro_health_rounds VALUES('round','op','node','installation','endpoint','[]','manifest',100,200)`)
			exec(`INSERT INTO pro_health_intents VALUES('intent',?,?,?,?,?,?,200,100)`, s.intentOp, s.node, s.installation, s.intentInbound, s.endpoint, s.binding)
			exec(`INSERT INTO pro_health_round_members VALUES('round',10,'intent','binding')`)
			exec(`INSERT INTO pro_health_challenges VALUES('challenge',?,?,'fingerprint','request',200)`, s.challengeIntent, s.challengeOp)
			_, err = tx.ExecContext(ctx, `INSERT INTO pro_health_round_receipts VALUES('challenge','round',?,'receipt',?)`, s.receiptInbound, s.accepted)
			if tc.ok != (err == nil) {
				t.Fatal("round receipt scope decision", tc.ok, err)
			}
			if tc.name != "matching" {
				return
			}
			// Another round may prove the same inbound without rewriting history.
			exec(`INSERT INTO pro_health_rounds VALUES('round-2','op','node','installation','endpoint','[]','manifest',200,300)`)
			exec(`INSERT INTO pro_health_intents VALUES('intent-2','op','node','installation',10,'endpoint','binding',300,200)`)
			exec(`INSERT INTO pro_health_round_members VALUES('round-2',10,'intent-2','binding')`)
			exec(`INSERT INTO pro_health_challenges VALUES('challenge-2','intent-2','op','fingerprint','request',300)`)
			exec(`INSERT INTO pro_health_round_receipts VALUES('challenge-2','round-2',10,'receipt-2',250)`)
			for _, table := range []string{"pro_health_rounds", "pro_health_round_members", "pro_health_round_receipts"} {
				column := "round_id"
				if table == "pro_health_rounds" {
					column = "id"
				}
				for _, q := range []string{"UPDATE " + table + " SET " + column + "=" + column, "DELETE FROM " + table} {
					if _, err := tx.ExecContext(ctx, q); err == nil || !strings.Contains(err.Error(), "health round") {
						t.Fatal("round audit mutation not rejected by policy", q, err)
					}
				}
			}
		})
	}
}
