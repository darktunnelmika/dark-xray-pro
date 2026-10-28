package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"os"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestOfflineBackupAssetsRecoverDisabledPlansAndManagedHistory(t *testing.T) {
	ctx := context.Background()
	f := newExecutionFixture(t)
	f.command(t, "backup-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "backup-epoch-0001")
	f.ready(t, b, e)
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 13, 27, true)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.MutateInboundExecutionSubscription(ctx, f.key, "owner", f.client.ID, SubscriptionTokenMutation{EventID: "backup-subscription-0001", Action: "issue"}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateOfflineBackup(ctx, f.path); err != nil {
		t.Fatal("fixture schema", err)
	}
	if err := ValidateOfflineBackupAssets(ctx, f.path, f.key); err != nil {
		t.Fatal("valid historical native state rejected", err)
	}
	for _, key := range [][]byte{nil, bytes.Repeat([]byte{5}, 32), []byte("invalid")} {
		if err := ValidateOfflineBackupAssets(ctx, f.path, key); !errors.Is(err, ErrOfflineBackupAssets) {
			t.Fatal("unrecoverable key accepted", err)
		}
	}
	after, err := os.ReadFile(f.path)
	if err != nil || sha256.Sum256(before) != sha256.Sum256(after) {
		t.Fatal("asset validation changed snapshot", err)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(f.path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("asset validation left SQLite sidecar", err)
		}
	}
}

func TestOfflineBackupAssetsRequireHistoricalCiphertextEvenWhenCurrentPlanIsSafe(t *testing.T) {
	for _, damage := range []string{"external-old-revision", "corrupt-old-revision", "corrupt-old-plan-receipt"} {
		t.Run(damage, func(t *testing.T) {
			ctx := context.Background()
			f := newExecutionFixture(t)
			f.command(t, "backup-history-activate-0001", "activate")
			// The public current authoring plan remains unchanged and has no
			// external paths. Only an immutable historical record is damaged.
			if damage == "corrupt-old-plan-receipt" {
				var ddl string
				if err := f.s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='native_inbound_plan_events_no_update'`).Scan(&ddl); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.db.Exec(`DROP TRIGGER native_inbound_plan_events_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.db.Exec(`UPDATE native_inbound_plan_events SET receipt_enc=X'01'`); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.db.Exec(ddl); err != nil {
					t.Fatal(err)
				}
			} else {
				var revision uint64
				if err := f.s.db.QueryRow(`SELECT MIN(revision) FROM inbound_execution_revisions`).Scan(&revision); err != nil {
					t.Fatal(err)
				}
				raw := []byte{1}
				if damage == "external-old-revision" {
					old, err := readExecutionSnapshot(ctx, f.s.db, f.key, 501, revision)
					if err != nil {
						t.Fatal(err)
					}
					old.Profile.Spec.Reality = map[string]any{"masterKeyLog": "/outside-backup/private-key-log"}
					raw, err = sealPlanValue(f.key, executionAAD("revision", 501, revision), old)
					if err != nil {
						t.Fatal(err)
					}
				}
				var ddl string
				if err := f.s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name='inbound_execution_revisions_no_update'`).Scan(&ddl); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.db.Exec(`DROP TRIGGER inbound_execution_revisions_no_update`); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.db.Exec(`UPDATE inbound_execution_revisions SET payload_enc=? WHERE inbound_id=501 AND revision=?`, raw, revision); err != nil {
					t.Fatal(err)
				}
				if _, err := f.s.db.Exec(ddl); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.s.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := ValidateOfflineBackup(ctx, f.path); err != nil {
				t.Fatal("schema must still be valid for a ciphertext failure", err)
			}
			if err := ValidateOfflineBackupAssets(ctx, f.path, f.key); !errors.Is(err, ErrOfflineBackupAssets) {
				t.Fatal("damaged history accepted", err)
			}
		})
	}
}

func TestOfflineBackupAssetsRefuseExternalDependencies(t *testing.T) {
	for name, raw := range map[string]string{
		"tls-certificate":  `{"tls":{"certificates":[{"certificateFile":"/external/cert.pem"}]}}`,
		"tls-key":          `{"tls":{"certificates":[{"keyFile":"/external/key.pem"}]}}`,
		"tls-master-log":   `{"tls":{"masterKeyLog":"/external/master.log"}}`,
		"reality-log":      `{"reality":{"masterKeyLog":"/external/master.log"}}`,
		"xdrive":           `{"network":"xdrive","stream":{"service":"local"}}`,
		"compiled-xdrive":  `{"streamSettings":{"xdriveSettings":{"remoteFolder":"backup-needed"}}}`,
		"hysteria-files":   `{"masquerade":{"type":"file","dir":"/external/website"}}`,
		"tuic-files":       `{"server":{"certificate":"/external/cert.pem","private_key":"/external/key.pem"}}`,
		"adapter-manifest": `{"file_references":["/external/cert.pem"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := backupCheckJSON([]byte(raw)); !errors.Is(err, ErrOfflineBackupAssets) {
				t.Fatal("external asset accepted", err)
			}
		})
	}
	if err := backupCheckJSON([]byte(`{"tls":{"certificates":[{"certificate":["inline certificate"],"key":["inline key"]}]},"reality":{"privateKey":"inline-key"}}`)); err != nil {
		t.Fatal("self-contained material rejected", err)
	}
}

func TestOfflineBackupAssetsEmptyDatabaseNeedsNoInboundKey(t *testing.T) {
	for _, all := range []bool{false, true} {
		path := offlineValidationFixture(t, all)
		if err := ValidateOfflineBackupAssets(context.Background(), path, nil); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ValidateOfflineBackupAssets(ctx, "/not-opened", nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

// Compile-time assertion that the managed fixture is a pure in-process contract;
// no transport, external engine or production process is needed by these tests.
var _ ix.Compiler
