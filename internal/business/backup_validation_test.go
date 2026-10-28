package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func offlineValidationFixture(t *testing.T, all bool) string {
	t.Helper()
	s, path := opened(t)
	ctx := context.Background()
	newRep(t, s, "backup-representative")
	grant(t, s, "backup-representative", "backup-initial-credit-0001", 9007199254740993, 80)
	if all {
		for _, enable := range []func(context.Context) error{s.EnableUsageLedger, s.EnableSellerQuotaV2,
			s.EnableVolumeExpiryRefunds, s.EnableSellerOrdersV1, s.EnableInboundExecution} {
			if err := enable(ctx); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func mutateOfflineFixture(t *testing.T, path string, statements ...string) {
	t.Helper()
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}

func TestOfflineBackupValidationPreservesSnapshotBytesAndExactFeatureSchema(t *testing.T) {
	for _, all := range []bool{false, true} {
		t.Run(map[bool]string{false: "native-v8", true: "managed-v16-with-seller-refunds"}[all], func(t *testing.T) {
			path := offlineValidationFixture(t, all)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			beforeInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ValidateOfflineBackup(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			wantVersion := 8
			if all {
				wantVersion = 16
			}
			if got.SchemaVersion != wantVersion || got.TableCounts["representatives"] != 2 || len(got.Fingerprint) != 64 {
				t.Fatal("wrong bounded validation metadata", got)
			}
			for _, name := range []string{"representative_sellers_v2", "seller_order_receipts_v1", "volume_refund_receipts", "pro_usage_receipts", "inbound_execution_meta"} {
				_, exists := got.TableCounts[name]
				if exists != all {
					t.Fatalf("optional feature %s presence=%v want=%v", name, exists, all)
				}
			}
			again, err := ValidateOfflineBackup(context.Background(), path)
			if err != nil || !reflect.DeepEqual(got, again) {
				t.Fatal("validation was not stable", got, again, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
				t.Fatal("validator changed snapshot bytes", err)
			}
			afterInfo, err := os.Stat(path)
			if err != nil || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
				t.Fatal("validator changed snapshot mtime", err)
			}
			for _, suffix := range []string{"-wal", "-shm", "-journal"} {
				if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("read-only validation left sidecar", suffix, err)
				}
			}
		})
	}
}

func TestOfflineBackupValidationRejectsSameVersionDamagedPolicyAndForeignKeys(t *testing.T) {
	cases := map[string][]string{
		"old-version":                    {`PRAGMA user_version=7`},
		"future-version":                 {`PRAGMA user_version=999`},
		"missing-ordinary-audit-trigger": {`DROP TRIGGER events_immutable_update`},
		"altered-ordinary-table":         {`ALTER TABLE representatives ADD COLUMN misleading_credit INTEGER DEFAULT 0`},
		"unexpected-table":               {`CREATE TABLE extra_secret(value TEXT)`},
		"usage-view":                     {`DROP VIEW pro_usage_eligibility`, `CREATE VIEW pro_usage_eligibility AS SELECT id AS client_id,1 AS eligible FROM clients`},
		"seller-policy":                  {`DROP TRIGGER seller_v2_events_no_update`},
		"seller-order-policy":            {`DROP TRIGGER seller_order_receipts_no_update_v1`},
		"refund-policy":                  {`DROP TRIGGER volume_refund_receipts_no_update`},
		"managed-policy":                 {`DROP TRIGGER inbound_execution_events_no_update`},
		"foreign-key":                    {`INSERT INTO auth_sessions(token_sha256,user_id,created_unix,expires_unix) VALUES('orphan','absent',1,2)`},
	}
	for name, statements := range cases {
		t.Run(name, func(t *testing.T) {
			path := offlineValidationFixture(t, true)
			mutateOfflineFixture(t, path, statements...)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ValidateOfflineBackup(context.Background(), path)
			if !errors.Is(err, ErrOfflineBackupInvalid) || !reflect.DeepEqual(got, BackupValidation{}) {
				t.Fatalf("damaged source accepted: %+v %v", got, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
				t.Fatal("rejection repaired or modified source", err)
			}
		})
	}
}

func TestOfflineBackupValidationRefusesSidecarsSymlinksAndPublicFiles(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			path := offlineValidationFixture(t, false)
			if err := os.WriteFile(path+suffix, []byte("active or uncheckpointed"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateOfflineBackup(context.Background(), path); !errors.Is(err, ErrOfflineBackupInvalid) {
				t.Fatal("sidecar accepted", err)
			}
		})
	}
	path := offlineValidationFixture(t, false)
	link := filepath.Join(t.TempDir(), "linked.sqlite")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateOfflineBackup(context.Background(), link); !errors.Is(err, ErrOfflineBackupInvalid) {
		t.Fatal("symlink accepted", err)
	}
	parentLink := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(filepath.Dir(path), parentLink); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateOfflineBackup(context.Background(), filepath.Join(parentLink, filepath.Base(path))); !errors.Is(err, ErrOfflineBackupInvalid) {
		t.Fatal("symlink ancestor accepted", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateOfflineBackup(context.Background(), path); !errors.Is(err, ErrOfflineBackupInvalid) {
		t.Fatal("public source accepted", err)
	}
}

func TestOfflineBackupValidationCancellationAndMetadataContainNoRows(t *testing.T) {
	path := offlineValidationFixture(t, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ValidateOfflineBackup(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled validation proceeded", err)
	}
	got, err := ValidateOfflineBackup(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	printed := fmt.Sprint(got)
	for _, private := range []string{"backup-representative", "backup-initial-credit", "9007199254740993", path} {
		if strings.Contains(printed, private) {
			t.Fatal("row/path detail leaked through validation metadata")
		}
	}
}
