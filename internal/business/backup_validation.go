package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// ErrOfflineBackupInvalid deliberately contains no database row, secret, SQL
// error, source path or identity. Native backup validation never performs repair.
var ErrOfflineBackupInvalid = errors.New("invalid or unsupported offline PRO business snapshot")

type BackupValidation struct {
	SchemaVersion int              `json:"schema_version"`
	TableCounts   map[string]int64 `json:"table_counts"`
	Fingerprint   string           `json:"schema_fingerprint"`
}

// These hashes pin the exact current native business and seller-management DDL.
// They are computed from sorted [type,name,table,normalized SQL] JSON records.
// A future schema change requires a reviewed compatibility update, not a version
// number alone. Independently versioned feature families are verified below.
const backupBaseSchemaFingerprint = "369badcdf1e5396b6bc0910cf2d545290002cbc66968059ecbd8f766680a966f"
const backupSellerSchemaFingerprint = "d8c1b17e3de0bb5bbef143eed3b946fbd0cfcb43541e0f38fbe7cfb863169548"

var backupObjectName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

type backupSchemaObject [4]string

func backupSchemaGroup(name string) string {
	switch {
	case strings.HasPrefix(name, "seller_order_"):
		return "orders"
	case strings.HasPrefix(name, "volume_refund_"):
		return "refunds"
	case strings.HasPrefix(name, "inbound_execution_"):
		return "execution"
	case strings.HasPrefix(name, "seller_") || strings.HasPrefix(name, "representative_seller"):
		return "sellers"
	}
	for _, prefix := range []string{"pro_usage_", "pro_probe_", "pro_health_", "pro_subscription_", "pro_consumer_", "pro_initial_", "pro_delivery_"} {
		if strings.HasPrefix(name, prefix) {
			return "usage"
		}
	}
	return "base"
}

func backupSchemaDigest(objects []backupSchemaObject) string {
	raw, _ := json.Marshal(objects)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func backupSnapshotPath(path string) (os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrOfflineBackupInvalid
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return nil, ErrOfflineBackupInvalid
		}
		if current == path {
			if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() < 512 {
				return nil, ErrOfflineBackupInvalid
			}
		} else if !info.IsDir() {
			return nil, ErrOfflineBackupInvalid
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Lstat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrOfflineBackupInvalid
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, ErrOfflineBackupInvalid
	}
	return info, nil
}

// ValidateOfflineBackup accepts only a private, checkpointed, immutable SQLite
// snapshot supplied under the caller's offline maintenance lock. Absence of WAL
// files is not proof that a live service stopped; this function is not a live
// database backup API. It never calls Store.Open, starts a migration or writes
// sidecars. The result describes schema and row counts, not runtime activation.
func ValidateOfflineBackup(ctx context.Context, path string) (BackupValidation, error) {
	var empty BackupValidation
	if ctx == nil {
		return empty, ErrOfflineBackupInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	before, err := backupSnapshotPath(path)
	if err != nil {
		return empty, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1&_pragma=query_only(1)&_pragma=trusted_schema(0)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return empty, ErrOfflineBackupInvalid
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()
	result, err := validateOfflineBackupDB(ctx, db)
	if err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		return empty, ErrOfflineBackupInvalid
	}
	after, err := backupSnapshotPath(path)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return empty, ErrOfflineBackupInvalid
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}

func validateOfflineBackupDB(ctx context.Context, db *sql.DB) (BackupValidation, error) {
	var result BackupValidation
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&result.SchemaVersion); err != nil ||
		(result.SchemaVersion != SchemaVersion && result.SchemaVersion != 16) {
		return result, ErrOfflineBackupInvalid
	}
	rows, err := db.QueryContext(ctx, `SELECT type,name,tbl_name,COALESCE(sql,'') FROM sqlite_master WHERE name NOT GLOB 'sqlite_*' ORDER BY type,name`)
	if err != nil {
		return result, ErrOfflineBackupInvalid
	}
	groups := make(map[string][]backupSchemaObject)
	objects := make([]backupSchemaObject, 0, 256)
	ddlBytes := 0
	for rows.Next() {
		var object backupSchemaObject
		if err = rows.Scan(&object[0], &object[1], &object[2], &object[3]); err != nil ||
			!backupObjectName.MatchString(object[1]) || !backupObjectName.MatchString(object[2]) ||
			len(object[3]) == 0 || len(object[3]) > 65536 || len(objects) >= 1024 {
			rows.Close()
			return result, ErrOfflineBackupInvalid
		}
		switch object[0] {
		case "table", "index", "trigger", "view":
		default:
			rows.Close()
			return result, ErrOfflineBackupInvalid
		}
		object[3] = strings.Join(strings.Fields(object[3]), " ")
		ddlBytes += len(object[3])
		if ddlBytes > 2*1024*1024 {
			rows.Close()
			return result, ErrOfflineBackupInvalid
		}
		objects = append(objects, object)
		group := backupSchemaGroup(object[1])
		groups[group] = append(groups[group], object)
	}
	err, closeErr := rows.Err(), rows.Close()
	if err != nil || closeErr != nil || backupSchemaDigest(groups["base"]) != backupBaseSchemaFingerprint {
		return result, ErrOfflineBackupInvalid
	}
	if len(groups["sellers"]) > 0 {
		if backupSchemaDigest(groups["sellers"]) != backupSellerSchemaFingerprint || sellerManagementConfiguredV2(ctx, db) != nil {
			return result, ErrOfflineBackupInvalid
		}
	}
	if len(groups["orders"]) > 0 {
		present, err := sellerOrderSchemaStateV1(ctx, db)
		if err != nil || !present || len(groups["sellers"]) == 0 || len(groups["usage"]) == 0 {
			return result, ErrOfflineBackupInvalid
		}
	}
	if len(groups["refunds"]) > 0 && (len(groups["usage"]) == 0 || volumeRefundConfigured(ctx, db) != nil) {
		return result, ErrOfflineBackupInvalid
	}
	if result.SchemaVersion == 16 {
		if len(groups["execution"]) != len(inboundExecutionSchema) || verifyInboundExecutionSchema(ctx, db) != nil {
			return result, ErrOfflineBackupInvalid
		}
	} else if len(groups["execution"]) != 0 {
		return result, ErrOfflineBackupInvalid
	}
	if err := usageledger.ValidateReadOnlySchema(ctx, db); err != nil {
		return result, ErrOfflineBackupInvalid
	}
	checks, err := db.QueryContext(ctx, `PRAGMA integrity_check`)
	if err != nil {
		return result, ErrOfflineBackupInvalid
	}
	var integrity string
	valid := checks.Next() && checks.Scan(&integrity) == nil && integrity == "ok" && !checks.Next() && checks.Err() == nil
	checks.Close()
	if !valid {
		return result, ErrOfflineBackupInvalid
	}
	fks, err := db.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return result, ErrOfflineBackupInvalid
	}
	invalidFK := fks.Next() || fks.Err() != nil
	fks.Close()
	if invalidFK {
		return result, ErrOfflineBackupInvalid
	}
	result.TableCounts = make(map[string]int64)
	for _, object := range objects {
		if object[0] != "table" {
			continue
		}
		if len(result.TableCounts) >= 512 {
			return BackupValidation{}, ErrOfflineBackupInvalid
		}
		var count int64
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM "`+object[1]+`"`).Scan(&count); err != nil || count < 0 {
			return BackupValidation{}, ErrOfflineBackupInvalid
		}
		result.TableCounts[object[1]] = count
	}
	raw, _ := json.Marshal(objects)
	hash := sha256.New()
	fmt.Fprintf(hash, "native-business-schema:%d\n", result.SchemaVersion)
	hash.Write(raw)
	result.Fingerprint = hex.EncodeToString(hash.Sum(nil))
	return result, nil
}
