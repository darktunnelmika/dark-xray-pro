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
	"strconv"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"golang.org/x/sys/unix"
)

type Receipt struct {
	Status            string           `json:"status"`
	BackupID          string           `json:"backup_id"`
	CreatedUnix       int64            `json:"created_unix"`
	SourceRevision    string           `json:"source_revision"`
	SchemaVersion     int64            `json:"business_schema_version"`
	FileCount         int              `json:"file_count"`
	PayloadBytes      int64            `json:"payload_bytes"`
	ActivationEnabled bool             `json:"activation_enabled"`
	CiphertextSHA256  string           `json:"ciphertext_sha256"`
	SchemaFingerprint string           `json:"schema_fingerprint"`
	TableCounts       map[string]int64 `json:"table_counts"`
}

type limitedFileWriter struct {
	mu        sync.Mutex
	f         *os.File
	remaining int64
	exceeded  bool
}

func (w *limitedFileWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if int64(len(p)) > w.remaining {
		w.exceeded = true
		return 0, ErrReceipt
	}
	n, e := w.f.Write(p)
	w.remaining -= int64(n)
	return n, e
}

func (w *limitedFileWriter) Exceeded() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.exceeded }

func (r *LinuxRuntime) receiptPath(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", ErrUnsafe
	}
	return filepath.Join(r.receipts, id+".json"), nil
}

func (r *LinuxRuntime) readReceipt(id string) (Receipt, error) {
	var receipt Receipt
	p, e := r.receiptPath(id)
	if e != nil {
		return receipt, e
	}
	f, e := openChecked(p, r.rootUID, true, 256<<10)
	if e != nil {
		return receipt, ErrReceipt
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 256<<10))
	if e != nil {
		return receipt, ErrReceipt
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(b, &keys) != nil || len(keys) != 11 || strictJSON(b, &receipt) != nil {
		return receipt, ErrReceipt
	}
	for _, k := range []string{"status", "backup_id", "created_unix", "source_revision", "business_schema_version", "file_count", "payload_bytes", "activation_enabled", "ciphertext_sha256", "schema_fingerprint", "table_counts"} {
		if _, ok := keys[k]; !ok {
			return receipt, ErrReceipt
		}
	}
	if receipt.Status != "created" || !idPattern.MatchString(receipt.BackupID) || receipt.CreatedUnix <= 0 || receipt.SourceRevision != r.config.HubRevision ||
		(receipt.SchemaVersion != 8 && receipt.SchemaVersion != 16) || receipt.FileCount < 2 || receipt.FileCount > 4096 || receipt.PayloadBytes <= 0 || receipt.PayloadBytes > r.config.MaxPayloadBytes ||
		receipt.ActivationEnabled || !hex64.MatchString(receipt.CiphertextSHA256) || !hex64.MatchString(receipt.SchemaFingerprint) || len(receipt.TableCounts) == 0 || len(receipt.TableCounts) > 1024 {
		return receipt, ErrReceipt
	}
	for k, n := range receipt.TableCounts {
		if k == "" || len(k) > 128 || n < 0 {
			return receipt, ErrReceipt
		}
	}
	return receipt, nil
}

// waitOwnedGroup deliberately does not reap before all signalling is finished.
// A zombie child retains its PID, preventing a reused process-group ID from
// being signalled. Persisted PIDs are never accepted by this function.
func waitOwnedGroup(ctx context.Context, cmd *exec.Cmd) error {
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	var cancelledAt time.Time
	killed := false
	for {
		var info unix.Siginfo
		e := unix.Waitid(unix.P_PID, cmd.Process.Pid, &info, unix.WEXITED|unix.WNOHANG|unix.WNOWAIT, nil)
		if errors.Is(e, unix.EINTR) {
			continue
		}
		if e != nil {
			// The command is ours but state cannot be proven. Do not signal any
			// potentially reused PID; leave reaping to its direct process handle.
			go cmd.Wait()
			return ErrCreate
		}
		if info.Signo != 0 {
			_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
			return cmd.Wait()
		}
		if ctx.Err() != nil && cancelledAt.IsZero() {
			cancelledAt = time.Now()
			_ = unix.Kill(-cmd.Process.Pid, unix.SIGTERM)
		}
		if !cancelledAt.IsZero() && time.Since(cancelledAt) >= 3*time.Second && !killed {
			_ = unix.Kill(-cmd.Process.Pid, unix.SIGKILL)
			killed = true
		}
		if !cancelledAt.IsZero() && time.Since(cancelledAt) >= 15*time.Second {
			// An uninterruptible child cannot safely be treated as stopped.
			// Systemd's control-group cleanup is the recovery backstop.
			go cmd.Wait()
			return ErrCreate
		}
		<-tick.C
	}
}

func (r *LinuxRuntime) Create(ctx context.Context, j backupcontrol.Job) (Receipt, error) {
	var empty Receipt
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	if !idPattern.MatchString(j.ID) || j.Recipient != r.config.PublicRecipient || r.verifyExecutables() != nil || r.checkSpace() != nil {
		return empty, ErrPreflight
	}
	p, e := r.archivePath(j.ID)
	if e != nil {
		return empty, e
	}
	dir := filepath.Dir(p)
	if e = os.Mkdir(dir, 0700); e != nil {
		return empty, ErrUnknownPublication
	}
	if e = os.Chown(dir, int(r.uid), int(r.gid)); e != nil {
		return empty, ErrUnsafe
	}
	if e = syncDir(r.archives); e != nil {
		return empty, ErrUnsafe
	}
	rp, _ := r.receiptPath(j.ID)
	f, e := os.OpenFile(rp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return empty, ErrUnsafe
	}
	defer f.Close()
	out := &limitedFileWriter{f: f, remaining: 256 << 10}
	cmd := r.backupCommand(dir, "create", "--recipient", j.Recipient, "--out", p, "--max-bytes", strconv.FormatInt(r.config.MaxPayloadBytes, 10))
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return empty, ErrCreate
	}
	e = waitOwnedGroup(ctx, cmd)
	syncErr := f.Sync()
	closeErr := f.Close()
	dirErr := syncDir(r.receipts)
	if e != nil || syncErr != nil || closeErr != nil || dirErr != nil || out.Exceeded() {
		if _, statErr := os.Lstat(p); statErr == nil {
			return empty, ErrUnknownPublication
		}
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		return empty, ErrCreate
	}
	receipt, e := r.readReceipt(j.ID)
	if e != nil {
		return empty, ErrUnknownPublication
	}
	return receipt, nil
}

// Finalize seals a newly root-owned inode, not merely a chmod/chown of a
// formerly unprivileged inode. Retained writable descriptors cannot mutate it.
func (r *LinuxRuntime) Finalize(ctx context.Context, j backupcontrol.Job) (backupcontrol.Archive, error) {
	var archive backupcontrol.Archive
	receipt, e := r.readReceipt(j.ID)
	if e != nil {
		return archive, ErrUnknownPublication
	}
	p, e := r.archivePath(j.ID)
	if e != nil {
		return archive, e
	}
	dir := filepath.Dir(p)
	if canonical(dir) != nil {
		return archive, ErrUnsafe
	}
	ds, e := os.Lstat(dir)
	if e != nil || !ds.IsDir() || ds.Mode().Perm()&0077 != 0 || (!owner(ds, r.uid) && !owner(ds, r.rootUID)) {
		return archive, ErrUnsafe
	}
	if e = os.Chown(dir, int(r.rootUID), int(r.rootGID)); e != nil {
		return archive, ErrUnsafe
	}
	if e = os.Chmod(dir, 0700); e != nil {
		return archive, ErrUnsafe
	}
	limit := r.config.ciphertextLimit()
	in, e := openChecked(p, r.uid, true, limit)
	alreadySealed := false
	if e != nil {
		in, e = openChecked(p, r.rootUID, true, limit)
		alreadySealed = e == nil
	}
	if e != nil {
		return archive, ErrUnknownPublication
	}
	defer in.Close()
	before, e := in.Stat()
	if e != nil || before.Size() < 1 {
		return archive, ErrUnknownPublication
	}
	if !alreadySealed {
		out, e := os.CreateTemp(dir, ".sealed-")
		if e != nil {
			return archive, ErrUnsafe
		}
		name := out.Name()
		published := false
		defer func() {
			out.Close()
			if !published {
				os.Remove(name)
			}
		}()
		if out.Chmod(0600) != nil {
			return archive, ErrUnsafe
		}
		h := sha256.New()
		buf := make([]byte, 128<<10)
		var size int64
		for {
			if ctx.Err() != nil {
				return archive, ctx.Err()
			}
			n, re := in.Read(buf)
			if n > 0 {
				size += int64(n)
				if size > limit {
					return archive, ErrUnsafe
				}
				if _, e = out.Write(buf[:n]); e != nil {
					return archive, ErrUnsafe
				}
				h.Write(buf[:n])
			}
			if re == io.EOF {
				break
			}
			if re != nil {
				return archive, ErrUnsafe
			}
		}
		current, e := os.Lstat(p)
		after, se := in.Stat()
		if e != nil || se != nil || !os.SameFile(before, current) || before.Size() != after.Size() || before.ModTime() != after.ModTime() || size != before.Size() || hex.EncodeToString(h.Sum(nil)) != receipt.CiphertextSHA256 {
			return archive, ErrUnknownPublication
		}
		if out.Sync() != nil || out.Close() != nil {
			return archive, ErrUnsafe
		}
		if os.Rename(name, p) != nil {
			return archive, ErrUnsafe
		}
		published = true
	}
	final, e := openChecked(p, r.rootUID, true, limit)
	if e != nil {
		return archive, ErrUnsafe
	}
	defer final.Close()
	hash, size, e := hashFile(ctx, final)
	if e != nil || hash != receipt.CiphertextSHA256 || size < 1 {
		return archive, ErrUnknownPublication
	}
	if final.Sync() != nil || syncDir(dir) != nil || syncDir(r.archives) != nil {
		return archive, ErrUnknownPublication
	}
	archive = backupcontrol.Archive{ID: j.ID, JobID: j.ID, CreatedUnix: receipt.CreatedUnix, SizeBytes: size, PayloadBytes: receipt.PayloadBytes, SHA256: hash,
		SourceRevision: receipt.SourceRevision, SchemaVersion: receipt.SchemaVersion, Verification: "checksum_verified", Downloadable: true}
	return archive, nil
}

func (r *LinuxRuntime) HasPublication(id string) bool {
	p, e := r.archivePath(id)
	if e != nil {
		return false
	}
	_, e = os.Lstat(p)
	return e == nil || !errors.Is(e, os.ErrNotExist)
}
