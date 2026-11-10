package business

import (
	"context"
	"database/sql"
	"net/url"
	"testing"
)

func TestExecutionHistoryMaintenanceNoEligibleRowsDoesNotWrite(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	f.command(t, "history-noop-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "history-noop-epoch-0001")
	for seq := uint64(1); seq <= 3; seq++ {
		up, down := uint64(17), uint64(31)
		if seq == 1 {
			up, down = 0, 0
		}
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, seq, up, down, seq == 3)); err != nil {
			t.Fatal(err)
		}
	}
	u := url.URL{Scheme: "file", Path: f.path, RawQuery: "mode=rw&_pragma=busy_timeout(0)&_pragma=synchronous(FULL)&_pragma=foreign_keys(1)&_pragma=trusted_schema(OFF)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	observer, err := f.s.readOnly.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	version := func() (int, int) {
		t.Helper()
		var data, schema int
		if err := observer.QueryRowContext(ctx, "PRAGMA data_version").Scan(&data); err != nil {
			t.Fatal(err)
		}
		if err := observer.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&schema); err != nil {
			t.Fatal(err)
		}
		return data, schema
	}
	dataBefore, schemaBefore := version()
	for pass := 0; pass < 2; pass++ {
		cursor := executionHistoryCursor{}
		for {
			batch := completedHistoryBatch(t, db, cursor)
			if batch.Removed != 0 {
				t.Fatal("recent receipt history removed", batch)
			}
			if batch.Done {
				break
			}
			cursor = batch.Next
		}
	}
	dataAfter, schemaAfter := version()
	if dataBefore != dataAfter || schemaBefore != schemaAfter {
		t.Fatal("ineligible scan committed a write", dataBefore, dataAfter, schemaBefore, schemaAfter)
	}
}
