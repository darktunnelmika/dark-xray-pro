// dark-pro-backup creates native PRO offline backups and restores them into an
// absent, quarantined destination. It has no activation or network operation.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"filippo.io/age"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"github.com/darktunnelmika/dark-xray-pro/internal/probackup"
	"golang.org/x/sys/unix"
)

var errRefused = errors.New("operation refused")
var errPublished = errors.New("publication occurred but durability is unconfirmed")

type options struct {
	command, stateRoot, recipient, input, output, identityFile, destination string
	maxBytes                                                                int64
}

func arguments(args []string) (options, error) {
	var o options
	if len(args) == 0 {
		return o, errRefused
	}
	o.command = args[0]
	f := flag.NewFlagSet("dark-pro-backup", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	switch o.command {
	case "keygen":
		f.StringVar(&o.output, "out", "", "new private recovery identity file")
	case "create":
		f.StringVar(&o.stateRoot, "state-root", hubstate.ProductionRoot, "registered native PRO state root")
		f.StringVar(&o.recipient, "recipient", "", "public age X25519 recipient")
		f.StringVar(&o.output, "out", "", "new encrypted archive file in a private directory")
		f.Int64Var(&o.maxBytes, "max-bytes", probackup.DefaultLimits().MaxPlaintextBytes, "maximum uncompressed payload bytes")
	case "verify", "inspect", "restore":
		f.StringVar(&o.input, "in", "", "encrypted native PRO archive")
		f.StringVar(&o.identityFile, "identity-file", "", "private recovery identity file")
		f.Int64Var(&o.maxBytes, "max-bytes", probackup.DefaultLimits().MaxPlaintextBytes, "maximum uncompressed payload bytes")
		if o.command == "restore" {
			f.StringVar(&o.destination, "destination", "", "absent child of a private existing directory")
		}
	default:
		return o, errRefused
	}
	if f.Parse(args[1:]) != nil || len(f.Args()) != 0 {
		return o, errRefused
	}
	if o.command != "keygen" && (o.maxBytes < 1 || o.maxBytes > 1<<40) {
		return o, errRefused
	}
	if (o.command == "keygen" || o.command == "create") && o.output == "" {
		return o, errRefused
	}
	if o.command == "create" && o.stateRoot != hubstate.ProductionRoot {
		return o, errRefused
	}
	if (o.command == "verify" || o.command == "inspect" || o.command == "restore") &&
		(o.input == "" || o.identityFile == "" || (o.command == "restore" && o.destination == "")) {
		return o, errRefused
	}
	return o, nil
}

// The immediate parent must be private and owned by this process. Reject all
// symlink ancestors as well; publication is always a no-replace operation.
func privateParent(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) == "." || path == "/" {
		return "", errRefused
	}
	parent := filepath.Dir(path)
	for p := parent; ; p = filepath.Dir(p) {
		st, err := os.Lstat(p)
		if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return "", errRefused
		}
		if p == parent {
			stat, ok := st.Sys().(*syscall.Stat_t)
			if !ok || int(stat.Uid) != os.Geteuid() || st.Mode().Perm()&0077 != 0 {
				return "", errRefused
			}
		}
		if p == "/" {
			break
		}
	}
	return parent, nil
}

func openRegular(path string, private bool, maxSize int64) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errRefused
	}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || !st.IsDir() {
			return nil, errRefused
		}
		if p == "/" {
			break
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, errRefused
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || st.Size() < 1 || (maxSize > 0 && st.Size() > maxSize) {
		f.Close()
		return nil, errRefused
	}
	if private {
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || int(stat.Uid) != os.Geteuid() || st.Mode().Perm()&0077 != 0 || stat.Nlink != 1 {
			f.Close()
			return nil, errRefused
		}
	}
	return f, nil
}

func identityFromFile(path string) (string, error) {
	f, err := openRegular(path, true, 4096)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(raw) > 4096 {
		return "", errRefused
	}
	defer func() {
		for i := range raw {
			raw[i] = 0
		}
	}()
	value := strings.TrimSuffix(string(raw), "\n")
	if _, err := age.ParseX25519Identity(value); err != nil {
		return "", errRefused
	}
	return value, nil
}

// A complete file is fsynced before publication; an existing target is never
// replaced, even if it appears after the initial check.
func publishFile(path string, write func(*os.File) error) error {
	parent, err := privateParent(path)
	if err != nil {
		return err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return errRefused
	}
	defer root.Close()
	directory, err := root.Open(".")
	if err != nil {
		return errRefused
	}
	defer directory.Close()
	opened, err := directory.Stat()
	if err != nil || !opened.IsDir() || opened.Mode().Perm()&0077 != 0 {
		return errRefused
	}
	owner, ok := opened.Sys().(*syscall.Stat_t)
	if !ok || int(owner.Uid) != os.Geteuid() {
		return errRefused
	}
	unchanged := func() bool {
		current, err := os.Lstat(parent)
		return err == nil && current.IsDir() && os.SameFile(opened, current)
	}
	if !unchanged() {
		return errRefused
	}
	base := filepath.Base(path)
	if _, err := root.Lstat(base); !errors.Is(err, os.ErrNotExist) {
		return errRefused
	}
	temp := ".pro-backup-file-" + rand.Text()
	f, err := root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer root.Remove(temp)
	defer f.Close()
	if err := write(f); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if !unchanged() {
		return errRefused
	}
	if err := unix.Renameat2(int(directory.Fd()), temp, int(directory.Fd()), base, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	if err := directory.Sync(); err != nil || !unchanged() {
		return errPublished
	}
	return nil
}

func limits(o options) probackup.Limits {
	l := probackup.DefaultLimits()
	l.MaxPlaintextBytes = o.maxBytes
	l.MaxFileBytes = o.maxBytes
	// Compression can expand slightly. Keep a separately bounded ciphertext
	// budget, including age/gzip framing, without an arithmetic overflow.
	l.MaxCiphertextBytes = o.maxBytes + o.maxBytes/16 + 16<<20
	return l
}

func features(inv hubstate.Inventory) map[string]int {
	result := make(map[string]int)
	for name, value := range inv.Environment {
		if strings.HasPrefix(name, "DARK_PRO_ENABLE_") && value == "1" {
			result[name] = 1
		}
	}
	return result
}

func sameFeatures(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for name, value := range a {
		if b[name] != value {
			return false
		}
	}
	return true
}

func sourcesForArchive(sources []hubstate.Source) []probackup.FileSource {
	result := make([]probackup.FileSource, 0, len(sources))
	for _, source := range sources {
		source := source
		result = append(result, probackup.FileSource{
			Name: source.Name, Role: probackup.Role(source.Role), Size: source.Size,
			Open: func(ctx context.Context) (io.ReadCloser, error) {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				return openRegular(source.Path, true, source.Size)
			},
		})
	}
	return result
}

func create(ctx context.Context, o options) (map[string]any, error) {
	if os.Getenv("DARK_PRO_LAB_MODE") != "1" {
		return nil, errRefused
	}
	if _, err := age.ParseX25519Recipient(o.recipient); err != nil {
		return nil, errRefused
	}
	parent, err := privateParent(o.output)
	if err != nil {
		return nil, err
	}
	// Keep output/snapshot work outside the authoritative state root.
	rel, err := filepath.Rel(o.stateRoot, parent)
	if err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, "../")) {
		return nil, errRefused
	}
	lock, err := hubstate.Acquire(o.stateRoot)
	if err != nil {
		return nil, err
	}
	defer lock.Close()
	inv, err := hubstate.LoadInventory(lock)
	if err != nil {
		return nil, err
	}
	if err = hubstate.ValidateInventory(lock, inv); err != nil {
		return nil, err
	}
	work, err := os.MkdirTemp(parent, ".pro-backup-work-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	sources, err := hubstate.Snapshot(ctx, lock, inv, work)
	if err != nil {
		return nil, err
	}
	var dbPath string
	for _, source := range sources {
		if source.Role == string(probackup.RoleBusinessDB) {
			dbPath = source.Path
		}
	}
	if dbPath == "" {
		return nil, errRefused
	}
	check, err := business.ValidateOfflineBackup(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return nil, err
	}
	meta := probackup.Metadata{
		BackupID: hex.EncodeToString(id[:]), CreatedUnix: time.Now().UTC().Unix(),
		SourceRevision: inv.SourceRevision, BusinessSchemaVersion: int64(check.SchemaVersion),
		FeatureVersions: features(inv),
	}
	var manifest probackup.Manifest
	digest := sha256.New()
	err = publishFile(o.output, func(f *os.File) error {
		var writeErr error
		manifest, writeErr = probackup.Write(ctx, io.MultiWriter(f, digest), o.recipient, meta, sourcesForArchive(sources), limits(o))
		return writeErr
	})
	if err != nil {
		return nil, err
	}
	report := archiveReport("created", manifest)
	report["ciphertext_sha256"] = hex.EncodeToString(digest.Sum(nil))
	report["schema_fingerprint"] = check.Fingerprint
	report["table_counts"] = check.TableCounts
	return report, nil
}

// All cross-file compatibility checks occur inside the unpublished staging
// directory. Original source paths in inventory are never opened on restore.
func validateRestored(ctx context.Context, stage string, manifest probackup.Manifest) (business.BackupValidation, error) {
	var check business.BackupValidation
	f, err := openRegular(filepath.Join(stage, "inventory.json"), true, 1<<20)
	if err != nil {
		return check, err
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 1<<20))
	decoder.DisallowUnknownFields()
	var inv hubstate.Inventory
	if decoder.Decode(&inv) != nil {
		return check, errRefused
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return check, errRefused
	}
	if hubstate.ValidateArchiveInventory(inv) != nil || inv.SourceRevision != manifest.Metadata.SourceRevision ||
		!sameFeatures(features(inv), manifest.Metadata.FeatureVersions) {
		return check, errRefused
	}
	expected := make(map[string]hubstate.SnapshotFile, len(inv.SnapshotFiles))
	for _, file := range inv.SnapshotFiles {
		if _, exists := expected[file.Name]; exists || file.Name == "inventory.json" {
			return check, errRefused
		}
		expected[file.Name] = file
	}
	if len(manifest.Files) != len(expected)+1 {
		return check, errRefused
	}
	var dbPath string
	inventorySeen := false
	for _, file := range manifest.Files {
		if file.Name == "inventory.json" {
			if inventorySeen || file.Role != probackup.RoleInventory {
				return check, errRefused
			}
			inventorySeen = true
			continue
		}
		want, exists := expected[file.Name]
		if !exists || want.Role != string(file.Role) || want.Size != file.Size {
			return check, errRefused
		}
		if file.Role == probackup.RoleBusinessDB {
			if dbPath != "" || file.Name != "data/business.sqlite" {
				return check, errRefused
			}
			dbPath = filepath.Join(stage, file.Name)
		}
		delete(expected, file.Name)
	}
	if !inventorySeen || len(expected) != 0 || dbPath == "" {
		return check, errRefused
	}
	if err := hubstate.ValidateSnapshot(ctx, stage, inv); err != nil {
		return check, errRefused
	}
	check, err = business.ValidateOfflineBackup(ctx, dbPath)
	if err != nil || int64(check.SchemaVersion) != manifest.Metadata.BusinessSchemaVersion {
		return check, errRefused
	}
	key, err := hex.DecodeString(inv.Environment["DARK_PRO_NATIVE_INBOUND_KEY_HEX"])
	if err != nil {
		return check, errRefused
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	if err := business.ValidateOfflineBackupAssets(ctx, dbPath, key); err != nil {
		return check, errRefused
	}
	return check, nil
}

func archiveReport(status string, manifest probackup.Manifest) map[string]any {
	var total int64
	for _, f := range manifest.Files {
		total += f.Size
	}
	return map[string]any{
		"status": status, "backup_id": manifest.Metadata.BackupID,
		"created_unix": manifest.Metadata.CreatedUnix, "source_revision": manifest.Metadata.SourceRevision,
		"business_schema_version": manifest.Metadata.BusinessSchemaVersion,
		"file_count":              len(manifest.Files), "payload_bytes": total,
		"activation_enabled": false,
	}
}

func readArchive(ctx context.Context, o options) (map[string]any, error) {
	key, err := identityFromFile(o.identityFile)
	if err != nil {
		return nil, err
	}
	f, err := openRegular(o.input, false, limits(o).MaxCiphertextBytes)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if o.command == "verify" {
		manifest, err := probackup.Verify(ctx, f, key, limits(o))
		if err != nil {
			return nil, err
		}
		report := archiveReport("archive_integrity_verified", manifest)
		report["database_compatibility_checked"] = false
		return report, nil
	}
	destination := o.destination
	if o.command == "inspect" {
		parent, err := privateParent(o.identityFile)
		if err != nil {
			return nil, err
		}
		work, err := os.MkdirTemp(parent, ".pro-backup-inspect-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(work)
		destination = filepath.Join(work, "quarantine")
	} else {
		if os.Getenv("DARK_PRO_LAB_MODE") != "1" {
			return nil, errRefused
		}
		if _, err := privateParent(destination); err != nil {
			return nil, err
		}
	}
	var check business.BackupValidation
	manifest, err := probackup.Restore(ctx, f, key, destination, limits(o), func(stage string, m probackup.Manifest) error {
		var validateErr error
		check, validateErr = validateRestored(ctx, stage, m)
		return validateErr
	})
	if err != nil {
		return nil, err
	}
	status := "restored_to_quarantine"
	if o.command == "inspect" {
		status = "compatible_archive_verified"
	}
	report := archiveReport(status, manifest)
	report["database_compatibility_checked"] = true
	report["schema_fingerprint"] = check.Fingerprint
	report["table_counts"] = check.TableCounts
	return report, nil
}

func execute(ctx context.Context, args []string, out, errOut io.Writer) int {
	o, err := arguments(args)
	if err != nil {
		fmt.Fprintln(errOut, "ERROR: invalid arguments; use keygen, create, verify, inspect, or restore with explicit file paths")
		return 2
	}
	var report map[string]any
	switch o.command {
	case "keygen":
		identity, e := age.GenerateX25519Identity()
		err = e
		if err == nil {
			err = publishFile(o.output, func(f *os.File) error {
				_, err := io.WriteString(f, identity.String()+"\n")
				return err
			})
			if err == nil {
				report = map[string]any{"status": "recovery_identity_created", "recipient": identity.Recipient().String()}
			}
		}
	case "create":
		report, err = create(ctx, o)
	default:
		report, err = readArchive(ctx, o)
	}
	if err != nil {
		if errors.Is(err, errPublished) || errors.Is(err, probackup.ErrPublicationUncertain) {
			fmt.Fprintln(errOut, "ERROR: publication occurred but storage durability is unconfirmed; the complete destination was retained for inspection before retry")
			return 5
		}
		// Do not echo raw decoder/SQLite/file errors: inputs can contain secrets.
		fmt.Fprintln(errOut, "ERROR: operation refused or interrupted; check private paths, offline writer locks, recovery identity, archive integrity, and schema compatibility")
		return 3
	}
	if json.NewEncoder(out).Encode(report) != nil {
		fmt.Fprintln(errOut, "ERROR: operation completed but result could not be written; inspect the requested destination before retrying")
		return 4
	}
	return 0
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(execute(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
