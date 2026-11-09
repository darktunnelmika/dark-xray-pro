package business

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const historicalStaleEpochQuery = "SELECT EXISTS(SELECT 1 FROM inbound_execution_usage_deltas baseline JOIN inbound_execution_epochs e ON e.epoch_id=baseline.epoch_id JOIN inbound_execution_usage_receipts latest ON latest.epoch_id=e.epoch_id AND latest.sequence=e.sequence WHERE baseline.client_id=? AND baseline.sequence=1 AND baseline.role='customer' AND e.state='open' AND latest.created<=?)"

func staleQueryFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "query.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		"CREATE TABLE inbound_execution_epochs(epoch_id TEXT PRIMARY KEY,node_id TEXT,sequence INTEGER,state TEXT)",
		"CREATE UNIQUE INDEX inbound_execution_epoch_open ON inbound_execution_epochs(node_id) WHERE state!='sealed'",
		"CREATE TABLE inbound_execution_usage_deltas(epoch_id TEXT,sequence INTEGER,principal_id TEXT,client_id TEXT,role TEXT,PRIMARY KEY(epoch_id,sequence,principal_id))",
		"CREATE INDEX inbound_execution_baseline_accounts ON inbound_execution_usage_deltas(client_id,epoch_id) WHERE sequence=1 AND role='customer'",
		"CREATE TABLE inbound_execution_usage_receipts(epoch_id TEXT,sequence INTEGER,created INTEGER,PRIMARY KEY(epoch_id,sequence))",
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func TestExecutionStaleQueryUsesOpenEpochsBeforeHistoricalBaselines(t *testing.T) {
	db := staleQueryFixture(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 5000; i++ {
		id := fmt.Sprint(i)
		if _, err = tx.Exec("INSERT INTO inbound_execution_epochs VALUES(?,?,1,'sealed')", id, "node"); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec("INSERT INTO inbound_execution_usage_deltas VALUES(?,1,'p','client','customer')", id); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query("EXPLAIN QUERY PLAN "+executionStaleEpochQuery, "client", 0)
	if err != nil {
		t.Fatal(err)
	}
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	plan := strings.Join(details, "\n")
	openAt := strings.Index(plan, "SCAN e USING INDEX inbound_execution_epoch_open")
	baseAt := strings.Index(plan, "SEARCH baseline USING COVERING INDEX inbound_execution_baseline_accounts")
	if openAt < 0 || baseAt < openAt || !strings.Contains(plan, "client_id=? AND epoch_id=?") {
		t.Fatal("authority lookup traverses historical epochs", plan)
	}
}

func TestExecutionStaleQueryMatchesExistingAccountingBoundaries(t *testing.T) {
	db := staleQueryFixture(t)
	now := int64(1000 * time.Second)
	cutoff := now - int64(executionReportLiveness)
	for _, state := range []string{"open", "sealed", "uncertain"} {
		for _, role := range []string{"customer", "transit"} {
			for _, sequence := range []int{1, 2} {
				for _, received := range []int64{cutoff - 1, cutoff, cutoff + 1, now} {
					t.Run(fmt.Sprintf("%s/%s/%d/%d", state, role, sequence, received), func(t *testing.T) {
						tx, err := db.Begin()
						if err != nil {
							t.Fatal(err)
						}
						defer tx.Rollback()
						for _, q := range []string{
							"DELETE FROM inbound_execution_usage_receipts",
							"DELETE FROM inbound_execution_usage_deltas",
							"DELETE FROM inbound_execution_epochs",
						} {
							if _, err = tx.Exec(q); err != nil {
								t.Fatal(err)
							}
						}
						if _, err = tx.Exec("INSERT INTO inbound_execution_epochs VALUES('e','node',2,?)", state); err != nil {
							t.Fatal(err)
						}
						if _, err = tx.Exec("INSERT INTO inbound_execution_usage_deltas VALUES('e',?,'p','client',?)", sequence, role); err != nil {
							t.Fatal(err)
						}
						if _, err = tx.Exec("INSERT INTO inbound_execution_usage_receipts VALUES('e',2,?)", received); err != nil {
							t.Fatal(err)
						}
						for _, client := range []string{"client", "other"} {
							got, err := executionClientHasStaleEpoch(context.Background(), tx, client, now)
							if err != nil {
								t.Fatal(err)
							}
							var previous bool
							if err = tx.QueryRow(historicalStaleEpochQuery, client, cutoff).Scan(&previous); err != nil {
								t.Fatal(err)
							}
							want := client == "client" && state == "open" && role == "customer" && sequence == 1 && received <= cutoff
							if got != want || got != previous {
								t.Fatal("accounting boundary changed", got, want, previous)
							}
						}
					})
				}
			}
		}
	}
}
