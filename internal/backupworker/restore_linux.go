//go:build linux

package backupworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"

	"filippo.io/age"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"github.com/darktunnelmika/dark-xray-pro/internal/probackup"
	"github.com/darktunnelmika/dark-xray-pro/internal/restoreapply"
)

const RestoreIdentityPath = "/etc/dark-xray-pro-backup-worker/recovery-identity"

type LinuxRestoreRunner struct {
	runtime *LinuxRuntime
	store   *backupcontrol.Store
	inbox   backupcontrol.UploadStore
}

func NewLinuxRestoreRunner(r *LinuxRuntime, s *backupcontrol.Store, inbox backupcontrol.UploadStore) *LinuxRestoreRunner {
	return &LinuxRestoreRunner{runtime: r, store: s, inbox: inbox}
}
func (r *LinuxRestoreRunner) Available() bool {
	if r == nil || r.runtime == nil || !r.runtime.config.RestoreEnabled || !restoreBuildMatches(r.runtime.config) {
		return false
	}
	var fs syscall.Statfs_t
	if syscall.Statfs(HubStateDirectory, &fs) != nil || fs.Flags&syscall.MS_RDONLY != 0 {
		return false
	}
	f, e := openChecked(RestoreIdentityPath, 0, true, 4096)
	if e != nil {
		return false
	}
	f.Close()
	return true
}
func (r *LinuxRestoreRunner) CheckSource(ctx context.Context, q backupcontrol.RestoreRequest) error {
	if q.SourceKind == "archive" {
		_, e := r.store.ArchiveForRestore(ctx, q.SourceID, q.SHA256)
		return e
	}
	if r.inbox == nil {
		return backupcontrol.ErrUnavailable
	}
	v, e := r.inbox.Get(ctx, q.SourceID)
	if e != nil {
		return backupcontrol.ErrNotFound
	}
	if v.SHA256 != q.SHA256 {
		return backupcontrol.ErrConflict
	}
	if v.State != "received" || !v.Downloadable || v.Verification != "ciphertext_checksum_verified" {
		return backupcontrol.ErrBusy
	}
	return nil
}
func (r *LinuxRestoreRunner) jobDir(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", ErrUnsafe
	}
	return filepath.Join(ArchiveDirectory, "restore-"+id), nil
}
func privateRestoreDir(path string) error {
	if e := os.Mkdir(path, 0700); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}
func writeRestoreRecord(path string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return syncDir(filepath.Dir(path))
}
func readRestoreRecord(path string, v any) error {
	f, e := openChecked(path, 0, true, 2<<20)
	if e != nil {
		return e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if e != nil || len(b) > 2<<20 || strictJSON(b, v) != nil {
		return ErrUnsafe
	}
	return nil
}
func (r *LinuxRestoreRunner) inventory() (hubstate.Inventory, error) {
	var inv hubstate.Inventory
	f, e := openChecked(filepath.Join(HubStateDirectory, hubstate.InventoryName), r.runtime.uid, true, 1<<20)
	if e != nil {
		return inv, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if e != nil || len(b) > 1<<20 || strictJSON(b, &inv) != nil || hubstate.ValidateArchiveInventory(inv) != nil || inv.Root != HubStateDirectory || inv.BuildModified || inv.SourceRevision != r.runtime.config.HubRevision {
		return inv, ErrUnsafe
	}
	return inv, nil
}

type restoredReceipt struct {
	Status                       string           `json:"status"`
	BackupID                     string           `json:"backup_id"`
	CreatedUnix                  int64            `json:"created_unix"`
	SourceRevision               string           `json:"source_revision"`
	SchemaVersion                int64            `json:"business_schema_version"`
	FileCount                    int              `json:"file_count"`
	PayloadBytes                 int64            `json:"payload_bytes"`
	ActivationEnabled            bool             `json:"activation_enabled"`
	DatabaseCompatibilityChecked bool             `json:"database_compatibility_checked"`
	SchemaFingerprint            string           `json:"schema_fingerprint"`
	TableCounts                  map[string]int64 `json:"table_counts"`
	ManifestSHA256               string           `json:"manifest_sha256"`
}
type restoredMarker struct {
	State    string             `json:"state"`
	Manifest probackup.Manifest `json:"manifest"`
}

func sameInstall(a, b hubstate.Inventory) bool {
	if a.Root != b.Root || !reflect.DeepEqual(a.Environment, b.Environment) || !reflect.DeepEqual(a.Flags, b.Flags) || len(a.Resources) != len(b.Resources) {
		return false
	}
	byName := map[string]hubstate.Resource{}
	for _, v := range a.Resources {
		byName[v.Name] = v
	}
	for _, v := range b.Resources {
		old, ok := byName[v.Name]
		if !ok || v.Role != old.Role || v.Required != old.Required {
			return false
		}
	}
	return true
}
func copyCipher(ctx context.Context, dest string, in io.Reader, size int64, sha string) error {
	if size < 1 || size > backupcontrol.MaxArchiveBytes || !hex64.MatchString(sha) {
		return ErrUnsafe
	}
	f, e := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	h := sha256.New()
	buf := make([]byte, 128<<10)
	var total int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, re := in.Read(buf)
		if n > 0 {
			total += int64(n)
			if total > size {
				return ErrUnsafe
			}
			if _, e = f.Write(buf[:n]); e != nil {
				return e
			}
			h.Write(buf[:n])
		}
		if re == io.EOF {
			break
		}
		if re != nil {
			return re
		}
	}
	if total != size || hex.EncodeToString(h.Sum(nil)) != sha {
		return ErrUnsafe
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return syncDir(filepath.Dir(dest))
}
func (r *LinuxRestoreRunner) openSource(ctx context.Context, j backupcontrol.RestoreJob) (io.ReadCloser, int64, error) {
	if j.SourceKind == "archive" {
		a, e := r.store.ArchiveForRestore(ctx, j.SourceID, j.SHA256)
		if e != nil {
			return nil, 0, e
		}
		f, e := r.runtime.OpenArchive(ctx, a)
		return f, a.SizeBytes, e
	}
	if r.inbox == nil {
		return nil, 0, ErrUnsafe
	}
	f, v, e := r.inbox.OpenDownload(ctx, j.SourceID)
	if e != nil {
		return nil, 0, e
	}
	if v.SHA256 != j.SHA256 || !v.Downloadable || v.State != "received" {
		f.Close()
		return nil, 0, ErrUnsafe
	}
	return f, v.SizeBytes, nil
}
func (r *LinuxRestoreRunner) PrepareRestore(ctx context.Context, j backupcontrol.RestoreJob) (int, error) {
	if !r.Available() || r.runtime.verifyExecutables() != nil {
		return 0, ErrPreflight
	}
	current, e := r.inventory()
	if e != nil {
		return 0, e
	}
	keyFile, e := openChecked(RestoreIdentityPath, 0, true, 4096)
	if e != nil {
		return 0, ErrPreflight
	}
	raw, e := io.ReadAll(io.LimitReader(keyFile, 4097))
	keyFile.Close()
	key, parseErr := age.ParseX25519Identity(strings.TrimSpace(string(raw)))
	clear(raw)
	if e != nil || parseErr != nil || key.Recipient().String() != r.runtime.config.PublicRecipient {
		return 0, ErrPreflight
	}
	dir, e := r.jobDir(j.ID)
	if e != nil {
		return 0, e
	}
	in, size, e := r.openSource(ctx, j)
	if e != nil {
		return 0, e
	}
	defer in.Close()
	// Independent expansion and rollback budget; no target is stopped yet.
	var fs syscall.Statfs_t
	if syscall.Statfs(ArchiveDirectory, &fs) != nil || fs.Bsize <= 0 || fs.Bavail > uint64(^uint64(0))/uint64(fs.Bsize) {
		return 0, ErrPreflight
	}
	free := fs.Bavail * uint64(fs.Bsize)
	if free <= uint64(size)+(64<<20)+(3<<20) {
		return 0, ErrPreflight
	}
	expansionLimit := min(r.runtime.config.MaxPayloadBytes, int64((free-uint64(size)-(64<<20))/3))
	if e = privateRestoreDir(dir); e != nil {
		return 0, e
	}
	if e = privateRestoreDir(filepath.Join(dir, "transaction")); e != nil {
		return 0, e
	}
	if e = writeRestoreRecord(filepath.Join(dir, "current-inventory.json"), current); e != nil {
		return 0, e
	}
	input := filepath.Join(dir, "input.backup")
	if e = copyCipher(ctx, input, in, size, j.SHA256); e != nil {
		return 0, e
	}
	in.Close()
	output, e := os.OpenFile(filepath.Join(dir, "cli-receipt.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return 0, e
	}
	defer output.Close()
	limited := &limitedFileWriter{f: output, remaining: 256 << 10}
	// The pinned restore CLI runs inside the coordinator's existing sandbox.
	// Its private key and staging tree remain root-owned; HUB/browser cannot
	// read them. CLI has no service operations and validates before publication.
	cmd := exec.Command(r.runtime.config.BackupBinary, "restore", "--in", input, "--identity-file", RestoreIdentityPath, "--destination", filepath.Join(dir, "candidate"), "--max-bytes", strconv.FormatInt(expansionLimit, 10))
	cmd.Dir = dir
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "DARK_PRO_LAB_MODE=1"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stdout = limited
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return 0, ErrPreflight
	}
	e = waitOwnedGroup(ctx, cmd)
	se := output.Sync()
	ce := output.Close()
	if e != nil || se != nil || ce != nil || limited.Exceeded() {
		return 0, ErrReceipt
	}
	var receipt restoredReceipt
	var marker restoredMarker
	if readRestoreRecord(filepath.Join(dir, "cli-receipt.json"), &receipt) != nil || readRestoreRecord(filepath.Join(dir, "candidate", hubstate.QuarantineName), &marker) != nil {
		return 0, ErrReceipt
	}
	manifestBytes, e := json.Marshal(marker.Manifest)
	if e != nil {
		return 0, ErrReceipt
	}
	h := sha256.Sum256(manifestBytes)
	if receipt.Status != "restored_to_quarantine" || receipt.ActivationEnabled || !receipt.DatabaseCompatibilityChecked || marker.State != "quarantined" ||
		!hex64.MatchString(receipt.ManifestSHA256) || hex.EncodeToString(h[:]) != receipt.ManifestSHA256 || len(receipt.TableCounts) == 0 || len(receipt.TableCounts) > 1024 || receipt.PayloadBytes > r.runtime.config.MaxPayloadBytes {
		return 0, ErrReceipt
	}
	var archived hubstate.Inventory
	if readRestoreRecord(filepath.Join(dir, "candidate/inventory.json"), &archived) != nil || hubstate.ValidateArchiveInventory(archived) != nil || !sameInstall(current, archived) {
		return 0, ErrPreflight
	}
	// The original paths in archived.Resources are deliberately never opened.
	replacements, e := r.replacements(ctx, dir, current, archived, marker.Manifest)
	if e != nil {
		return 0, e
	}
	if e = restoreapply.CheckSpace(HubStateDirectory, filepath.Join(dir, "transaction"), replacements, int(r.runtime.uid)); e != nil {
		return 0, ErrPreflight
	}
	// Check the actual writable mount and UID transition before stopping HUB.
	// This never opens a financial file and temporary names are not inventoried.
	seen := map[string]bool{}
	for _, replacement := range replacements {
		parent := filepath.Dir(filepath.Join(HubStateDirectory, replacement.Destination))
		if !replacement.Present {
			if _, err := os.Stat(parent); os.IsNotExist(err) {
				continue
			}
		}
		if seen[parent] {
			continue
		}
		seen[parent] = true
		if e = r.probeRestoreDirectory(parent); e != nil {
			return 0, e
		}
	}
	return len(receipt.TableCounts), nil
}
func (r *LinuxRestoreRunner) replacements(ctx context.Context, dir string, current, archived hubstate.Inventory, m probackup.Manifest) ([]restoreapply.Replacement, error) {
	files := map[string]probackup.FileEntry{}
	for _, f := range m.Files {
		files[f.Name] = f
	}
	out := []restoreapply.Replacement{}
	for _, v := range current.Resources {
		file, present := files[v.Name]
		rel, e := filepath.Rel(HubStateDirectory, v.Path)
		internal := e == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../")
		candidate := filepath.Join(dir, "candidate", filepath.FromSlash(v.Name))
		if !internal {
			// Credentials/TLS outside the fixed data root are never overwritten by
			// archived path names. This v1 requires matching installation credentials.
			st, se := os.Lstat(v.Path)
			if !present && errors.Is(se, os.ErrNotExist) {
				continue
			}
			if !present || se != nil || !st.Mode().IsRegular() {
				return nil, ErrPreflight
			}
			f, e := openChecked(v.Path, r.runtime.uid, v.Role != "agent_tls_cert", r.runtime.config.MaxPayloadBytes)
			if e != nil {
				return nil, e
			}
			got, size, e := hashFile(ctx, f)
			f.Close()
			if e != nil || got != file.SHA256 || size != file.Size {
				return nil, ErrPreflight
			}
			continue
		}
		replacement := restoreapply.Replacement{Destination: rel, Present: present}
		if present {
			replacement.Source = candidate
			replacement.Size = file.Size
			replacement.SHA256 = file.SHA256
		}
		out = append(out, replacement)
		if v.Role == "business" || v.Role == "node_history" || v.Role == "owner_report_db" || v.Role == "telegram_bot_db" {
			for _, suffix := range []string{"-wal", "-journal", "-shm"} {
				out = append(out, restoreapply.Replacement{Destination: rel + suffix})
			}
		}
	}
	return out, nil
}
func (r *LinuxRestoreRunner) ApplyRestore(ctx context.Context, j backupcontrol.RestoreJob) error {
	dir, e := r.jobDir(j.ID)
	if e != nil {
		return e
	}
	t, e := restoreapply.Open(HubStateDirectory, filepath.Join(dir, "transaction"), j.ID, int(r.runtime.uid), int(r.runtime.gid))
	if e != nil {
		return e
	}
	defer t.Close()
	current, e := r.inventory()
	if e != nil {
		return e
	}
	var saved, archived hubstate.Inventory
	var marker restoredMarker
	var receipt restoredReceipt
	if readRestoreRecord(filepath.Join(dir, "current-inventory.json"), &saved) != nil || !reflect.DeepEqual(saved, current) ||
		readRestoreRecord(filepath.Join(dir, "candidate/inventory.json"), &archived) != nil || !sameInstall(current, archived) ||
		readRestoreRecord(filepath.Join(dir, "candidate", hubstate.QuarantineName), &marker) != nil || readRestoreRecord(filepath.Join(dir, "cli-receipt.json"), &receipt) != nil {
		return ErrUnsafe
	}
	b, _ := json.Marshal(marker.Manifest)
	hash := sha256.Sum256(b)
	if hex.EncodeToString(hash[:]) != receipt.ManifestSHA256 {
		return ErrUnsafe
	}
	replacements, e := r.replacements(ctx, dir, current, archived, marker.Manifest)
	if e != nil {
		return e
	}
	if e = t.Prepare(ctx, replacements); e != nil {
		return e
	}
	return t.Apply(ctx)
}
func (r *LinuxRestoreRunner) RollbackRestore(ctx context.Context, j backupcontrol.RestoreJob) error {
	dir, e := r.jobDir(j.ID)
	if e != nil {
		return e
	}
	t, e := restoreapply.Open(HubStateDirectory, filepath.Join(dir, "transaction"), j.ID, int(r.runtime.uid), int(r.runtime.gid))
	if e != nil {
		return e
	}
	defer t.Close()
	return t.Rollback(ctx)
}
func (r *LinuxRestoreRunner) ReleaseRestore(ctx context.Context, j backupcontrol.RestoreJob, restored bool) error {
	dir, e := r.jobDir(j.ID)
	if e != nil {
		return e
	}
	guard := filepath.Join(HubStateDirectory, hubstate.RestoreGuardName)
	if _, e = os.Lstat(guard); errors.Is(e, os.ErrNotExist) {
		// It may already be serving requests. Never acquire/replace its data lock
		// or roll back on this recovery path; verify the independent start receipt.
		name := "start-original.json"
		if restored {
			name = "start-restored.json"
		}
		var proof map[string]string
		if readRestoreRecord(filepath.Join(dir, "transaction", name), &proof) != nil || len(proof) != 1 || proof["restore_id"] != j.ID {
			return ErrUnsafe
		}
		return nil
	}
	t, e := restoreapply.Open(HubStateDirectory, filepath.Join(dir, "transaction"), j.ID, int(r.runtime.uid), int(r.runtime.gid))
	if e != nil {
		return e
	}
	defer t.Close()
	return t.ReleaseForStart(restored)
}

// A legacy HUB without the restore writer fence must never be paired with a
// newly enabled restore worker. All three binaries must be the same clean tree.
func restoreBuildMatches(c Config) bool {
	info, ok := debug.ReadBuildInfo()
	if !ok || c.HubRevision != c.BackupRevision {
		return false
	}
	revision, modified := "", ""
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			revision = s.Value
		}
		if s.Key == "vcs.modified" {
			modified = s.Value
		}
	}
	return revision == c.HubRevision && modified == "false"
}

func (r *LinuxRestoreRunner) probeRestoreDirectory(path string) error {
	if canonical(path) != nil {
		return ErrPreflight
	}
	var fs syscall.Statfs_t
	if syscall.Statfs(path, &fs) != nil || fs.Flags&syscall.MS_RDONLY != 0 {
		return ErrPreflight
	}
	f, e := os.CreateTemp(path, ".restore-write-probe-")
	if e != nil {
		return ErrPreflight
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return ErrPreflight
	}
	if e = f.Chown(int(r.runtime.uid), int(r.runtime.gid)); e != nil {
		return ErrPreflight
	}
	if e = f.Sync(); e != nil {
		return ErrPreflight
	}
	if e = f.Close(); e != nil {
		return ErrPreflight
	}
	if e = os.Remove(f.Name()); e != nil {
		return ErrPreflight
	}
	return syncDir(path)
}
