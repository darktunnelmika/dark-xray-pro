package business

import (
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
)

// Import a known zero-valued interval from the old dense v16 representation.
// Maintenance/backup tests must continue exercising historical databases even
// after live receipt ingestion no longer creates these redundant rows.
func seedLegacyZeroHistory(t *testing.T, f *executionFixture, b ix.Bundle, e ix.EpochInput, first, last uint64) {
	t.Helper()
	tx, err := f.s.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for seq := first; seq <= last; seq++ {
		for _, p := range b.Principals {
			if _, err = tx.Exec("INSERT OR IGNORE INTO inbound_execution_usage_deltas(epoch_id,sequence,principal_id,client_id,role,upload,download) VALUES(?,?,?,?,?,0,0)", e.EpochID, seq, p.ID, p.ClientID, p.Role); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
}
