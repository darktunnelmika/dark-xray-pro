package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"modernc.org/sqlite"
)

type crashImportScenario struct {
	DB, Source, Token, Point string
	Request                  DarkRestoreImportCommitV1
}

func TestDarkRestoreProcessInterrupted(t *testing.T) {
	if name := os.Getenv("DARK_RESTORE_TEST_SCENARIO"); name != "" {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var sc crashImportScenario
		if err = json.Unmarshal(data, &sc); err != nil {
			t.Fatal(err)
		}
		if sc.Point == "during" {
			err = sqlite.RegisterScalarFunction("interrupt_test_writer", 0, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) { os.Exit(86); return nil, nil })
			if err != nil {
				t.Fatal(err)
			}
		}
		store, err := Open(sc.DB)
		if err != nil {
			t.Fatal(err)
		}
		if sc.Point == "during" {
			store.db.SetMaxOpenConns(1)
			_, err = store.db.Exec("CREATE TEMP TRIGGER interrupt_test_import BEFORE INSERT ON main.darkrestore_import_entries_v1 BEGIN SELECT interrupt_test_writer(); END")
			if err != nil {
				t.Fatal(err)
			}
		}
		if _, err = store.CommitDarkRestoreImportV1(context.Background(), sc.Token, sc.Source, make([]byte, 32), sc.Request); err != nil {
			t.Fatal(err)
		}
		if sc.Point == "after" {
			os.Exit(87)
		}
		t.Fatal("interruption point was not reached")
	}
	for _, point := range []string{"during", "after"} {
		t.Run(point, func(t *testing.T) {
			ctx := context.Background()
			f := importSetup(t)
			source := filepath.Join(t.TempDir(), "legacy.sqlite")
			db, err := sql.Open("sqlite", source)
			if err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile("../../testdata/darkrestore/single-customer.sql")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(string(fixture)); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			if err = os.Chmod(source, 0600); err != nil {
				t.Fatal(err)
			}
			original, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(original)
			review, err := darkrestore.BuildReview(ctx, source, hex.EncodeToString(digest[:]), time.Now())
			if err != nil {
				t.Fatal(err)
			}
			req := importRequest(t, f, DarkRestoreImportIntentV1{Review: review, Mappings: []DarkRestoreGroupMappingV1{{"synthetic-group", "rep-target"}}}, "restore-crash-event-0001")
			sc := crashImportScenario{f.path, source, f.owner.Token, point, req}
			data, err := json.Marshal(sc)
			if err != nil {
				t.Fatal(err)
			}
			name := filepath.Join(t.TempDir(), "private.json")
			if err = os.WriteFile(name, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err = f.s.Close(); err != nil {
				t.Fatal(err)
			}
			timeout, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			child := exec.CommandContext(timeout, os.Args[0], "-test.run=^TestDarkRestoreProcessInterrupted$")
			child.Env = append(os.Environ(), "DARK_RESTORE_TEST_SCENARIO="+name)
			output, err := child.CombinedOutput()
			wantExit, wantRows := 86, 0
			if point == "after" {
				wantExit, wantRows = 87, 1
			}
			var stopped *exec.ExitError
			if !errors.As(err, &stopped) || stopped.ExitCode() != wantExit {
				t.Fatalf("writer did not stop at %s: %v %s", point, err, output)
			}
			store, err := Open(f.path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			for _, table := range []string{"clients", "darkrestore_import_receipts_v1", "darkrestore_event_archives_v1", "darkrestore_group_history_v1", "pro_usage_accounts"} {
				var n int
				if err = store.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != wantRows {
					t.Fatalf("unexpected %s rows after %s: %d %v", table, point, n, err)
				}
			}
			r, err := store.CommitDarkRestoreImportV1(ctx, sc.Token, source, make([]byte, 32), req)
			if err != nil || r.Count != 1 || r.LegacyEvents != 1 || r.Replayed != (point == "after") {
				t.Fatal("recovery failed", err)
			}
			if err = validateDarkRestoreImportData(ctx, store.db); err != nil {
				t.Fatal(err)
			}
		})
	}
}
