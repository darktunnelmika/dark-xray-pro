package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"github.com/darktunnelmika/dark-xray-pro/internal/nodehistory"
	"github.com/darktunnelmika/dark-xray-pro/internal/probackup"
)

func TestCLIInspectAndRestoreValidateAuxiliarySchemaBeforePublication(t *testing.T) {
	t.Setenv("DARK_PRO_LAB_MODE", "1")
	ctx := context.Background()
	sourceRoot, work, destinationParent := privateTestDir(t), privateTestDir(t), privateTestDir(t)
	businessPath := filepath.Join(sourceRoot, "business.sqlite")
	store, err := business.Open(businessPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(sourceRoot, "history.sqlite")
	history, err := nodehistory.Open(historyPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := history.Close(); err != nil {
		t.Fatal(err)
	}
	lock, err := hubstate.Acquire(sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	inv, err := hubstate.BuildInventory(hubstate.Options{
		Root: sourceRoot, Registry: filepath.Join(sourceRoot, "registry.json"),
		Replacements: filepath.Join(sourceRoot, "replacements.json"),
		Staging:      filepath.Join(sourceRoot, "configs.json"),
		LabRevisions: filepath.Join(sourceRoot, "lab-revisions.json"),
		BusinessDB:   businessPath, NodeHistoryDB: historyPath,
		Listen: "127.0.0.1:2087", SourceRevision: strings.Repeat("a", 40),
		Environment: []string{
			"DARK_PRO_LAB_MODE=1", "DARK_PRO_LAB_TOKEN=" + strings.Repeat("f", 64),
			"DARK_PRO_ENABLE_BUSINESS_CORE=1", "DARK_PRO_ENABLE_NODE_HISTORY=1",
			"DARK_PRO_ENABLE_OFFLINE_BACKUP_V1=1",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := hubstate.WriteInventory(lock, inv); err != nil {
		t.Fatal(err)
	}
	inv, err = hubstate.LoadInventory(lock)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := hubstate.Snapshot(ctx, lock, inv, work)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(destinationParent, "recovery-key")
	if err := os.WriteFile(keyPath, []byte(identity.String()), 0600); err != nil {
		t.Fatal(err)
	}
	meta := probackup.Metadata{
		BackupID: "cli-compatibility-acceptance", CreatedUnix: time.Now().Unix(),
		SourceRevision: inv.SourceRevision, BusinessSchemaVersion: 8, FeatureVersions: features(inv),
	}
	archive := func(name string) string {
		t.Helper()
		path := filepath.Join(destinationParent, name)
		err := publishFile(path, func(f *os.File) error {
			_, err := probackup.Write(ctx, f, identity.Recipient().String(), meta, sourcesForArchive(sources), probackup.DefaultLimits())
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	validArchive := archive("valid.age")
	var output, diagnostic bytes.Buffer
	if code := execute(ctx, []string{"inspect", "--in", validArchive, "--identity-file", keyPath}, &output, &diagnostic); code != 0 {
		t.Fatalf("valid native inspection refused code=%d diagnostic=%s", code, diagnostic.String())
	}
	var inspected map[string]any
	if json.Unmarshal(output.Bytes(), &inspected) != nil || inspected["status"] != "compatible_archive_verified" ||
		inspected["database_compatibility_checked"] != true || inspected["activation_enabled"] != false {
		t.Fatal("inspection did not report real compatibility and inactive state")
	}
	validDestination := filepath.Join(destinationParent, "valid-restore")
	output.Reset()
	diagnostic.Reset()
	if code := execute(ctx, []string{"restore", "--in", validArchive, "--identity-file", keyPath, "--destination", validDestination},
		&output, &diagnostic); code != 0 {
		t.Fatalf("valid restore refused code=%d", code)
	}
	if _, err := os.Stat(filepath.Join(validDestination, hubstate.QuarantineName)); err != nil {
		t.Fatal("quarantine marker missing")
	}

	// Construct an otherwise authenticated native archive with a damaged
	// auxiliary schema. Verify is allowed to report container integrity;
	// inspect/restore must invoke shared semantic checks and refuse it.
	var stagedHistory string
	for _, source := range sources {
		if source.Role == string(probackup.RoleNodeHistoryDB) {
			stagedHistory = source.Path
		}
	}
	if stagedHistory == "" {
		t.Fatal("fixture lost its auxiliary store")
	}
	db, err := sql.Open("sqlite", stagedHistory)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA user_version=999"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	damagedArchive := archive("wrong-auxiliary-schema.age")
	output.Reset()
	diagnostic.Reset()
	if code := execute(ctx, []string{"verify", "--in", damagedArchive, "--identity-file", keyPath}, &output, &diagnostic); code != 0 {
		t.Fatal("fixture is not a fully authenticated archive", code)
	}
	var verified map[string]any
	if json.Unmarshal(output.Bytes(), &verified) != nil || verified["database_compatibility_checked"] != false {
		t.Fatal("container-only verification overstated compatibility")
	}
	for _, action := range []string{"inspect", "restore"} {
		output.Reset()
		diagnostic.Reset()
		args := []string{action, "--in", damagedArchive, "--identity-file", keyPath}
		badDestination := filepath.Join(destinationParent, "must-not-publish")
		if action == "restore" {
			args = append(args, "--destination", badDestination)
		}
		if code := execute(ctx, args, &output, &diagnostic); code != 3 {
			t.Fatal("unsupported auxiliary schema accepted", action, code)
		}
		if _, err := os.Lstat(badDestination); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("incompatible archive published")
		}
		if strings.Contains(output.String()+diagnostic.String(), identity.String()) {
			t.Fatal("private recovery identity leaked")
		}
	}
}
