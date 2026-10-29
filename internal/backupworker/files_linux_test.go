//go:build linux

package backupworker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
)

func filesystemFixture(t *testing.T) (*LinuxRuntime, backupcontrol.Job, Receipt, []byte) {
	t.Helper()
	dir := t.TempDir()
	r := &LinuxRuntime{config: Config{HubRevision: strings.Repeat("c", 40), MaxPayloadBytes: 1 << 20},
		uid: uint32(os.Geteuid()), gid: uint32(os.Getegid()), rootUID: uint32(os.Geteuid()), rootGID: uint32(os.Getegid()),
		archives: filepath.Join(dir, "archives"), receipts: filepath.Join(dir, "receipts")}
	for _, d := range []string{r.archives, r.receipts} {
		if e := os.Mkdir(d, 0700); e != nil {
			t.Fatal(e)
		}
	}
	j := backupcontrol.Job{ID: strings.Repeat("a", 32)}
	p, _ := r.archivePath(j.ID)
	if e := os.Mkdir(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Chown(filepath.Dir(p), int(r.uid), int(r.gid)); e != nil {
		t.Fatal(e)
	}
	data := bytes.Repeat([]byte("encrypted archive fixture\x00"), 1024)
	if e := os.WriteFile(p, data, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.Chown(p, int(r.uid), int(r.gid)); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(data)
	receipt := Receipt{Status: "created", BackupID: strings.Repeat("b", 32), CreatedUnix: time.Now().Unix(), SourceRevision: r.config.HubRevision,
		SchemaVersion: 8, FileCount: 2, PayloadBytes: int64(len(data)), CiphertextSHA256: hex.EncodeToString(h[:]), SchemaFingerprint: strings.Repeat("d", 64), TableCounts: map[string]int64{"clients": 4}}
	writeReceiptFixture(t, r, j.ID, receipt)
	return r, j, receipt, data
}
func writeReceiptFixture(t *testing.T, r *LinuxRuntime, id string, receipt Receipt) {
	t.Helper()
	raw, e := json.Marshal(receipt)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := r.receiptPath(id)
	if e = os.WriteFile(p, raw, 0600); e != nil {
		t.Fatal(e)
	}
}

func TestFinalizeSealsNewInodeAndIndependentlyChecksHash(t *testing.T) {
	r, j, _, want := filesystemFixture(t)
	p, _ := r.archivePath(j.ID)
	retained, e := os.OpenFile(p, os.O_RDWR, 0)
	if e != nil {
		t.Fatal(e)
	}
	defer retained.Close()
	original, _ := retained.Stat()
	a, e := r.Finalize(context.Background(), j)
	if e != nil {
		t.Fatal(e)
	}
	sealed, _ := os.Stat(p)
	if os.SameFile(original, sealed) || !owner(sealed, r.rootUID) || sealed.Mode().Perm() != 0600 {
		t.Fatal("publication did not replace the formerly writable inode")
	}
	if _, e = retained.WriteAt([]byte("tamper"), 0); e != nil {
		t.Fatal(e)
	}
	f, e := r.OpenArchive(context.Background(), a)
	if e != nil {
		t.Fatal(e)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if !bytes.Equal(got, want) || a.Verification != "checksum_verified" || !a.Downloadable {
		t.Fatal("retained source FD corrupted registered archive")
	}
	if _, e = r.Finalize(context.Background(), j); e != nil {
		t.Fatalf("sealed recovery is not idempotent: %v", e)
	}
	// Root-owned metadata is still checked independently before downloading.
	if e = os.WriteFile(p, bytes.Repeat([]byte("x"), len(want)), 0600); e != nil {
		t.Fatal(e)
	}
	if f, e = r.OpenArchive(context.Background(), a); e == nil {
		f.Close()
		t.Fatal("tampered archive accepted")
	}
}

func TestFinalizeRefusesTamperAndPreservesUnknownPublication(t *testing.T) {
	r, j, _, data := filesystemFixture(t)
	p, _ := r.archivePath(j.ID)
	if e := os.WriteFile(p, append(data, 'x'), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Finalize(context.Background(), j); e == nil {
		t.Fatal("wrong hash accepted")
	}
	if !r.HasPublication(j.ID) {
		t.Fatal("unknown ciphertext removed")
	}
}

func TestFailedJobDirectorySealPreservesUnknownStaging(t *testing.T) {
	r, j, _, _ := filesystemFixture(t)
	p, _ := r.archivePath(j.ID)
	stage := filepath.Join(filepath.Dir(p), ".pro-backup-work-unknown")
	if e := os.Mkdir(stage, 0700); e != nil {
		t.Fatal(e)
	}
	unknown := filepath.Join(stage, "partial-snapshot.sqlite")
	if e := os.WriteFile(unknown, []byte("preserved for operator review"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := r.SealJobDirectory(j.ID); e != nil {
		t.Fatal(e)
	}
	st, e := os.Lstat(filepath.Dir(p))
	if e != nil || !owner(st, r.rootUID) || st.Mode().Perm() != 0700 {
		t.Fatal("failed staging directory is not private/root-owned")
	}
	if _, e = os.Stat(unknown); e != nil {
		t.Fatal("unknown staging was removed")
	}
}

func TestReceiptRequiresCompleteSingleInterpretation(t *testing.T) {
	for _, which := range []string{"missing", "duplicate", "nested_duplicate", "unknown", "activation", "null_activation", "trailing", "wrong_revision", "symlink", "hardlink", "public_mode"} {
		t.Run(which, func(t *testing.T) {
			r, j, receipt, _ := filesystemFixture(t)
			p, _ := r.receiptPath(j.ID)
			raw, _ := os.ReadFile(p)
			switch which {
			case "missing":
				raw = bytes.Replace(raw, []byte(`"activation_enabled":false,`), nil, 1)
			case "duplicate":
				raw = append([]byte(`{"status":"created",`), raw[1:]...)
			case "nested_duplicate":
				raw = bytes.Replace(raw, []byte(`"clients":4`), []byte(`"clients":4,"clients":4`), 1)
			case "unknown":
				raw = append([]byte(`{"private_key":"forbidden",`), raw[1:]...)
			case "activation":
				raw = bytes.Replace(raw, []byte(`"activation_enabled":false`), []byte(`"activation_enabled":true`), 1)
			case "null_activation":
				raw = bytes.Replace(raw, []byte(`"activation_enabled":false`), []byte(`"activation_enabled":null`), 1)
			case "trailing":
				raw = append(raw, []byte(`{}`)...)
			case "wrong_revision":
				receipt.SourceRevision = strings.Repeat("f", 40)
				raw, _ = json.Marshal(receipt)
			case "symlink":
				if e := os.Rename(p, p+".other"); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(p+".other", p); e != nil {
					t.Fatal(e)
				}
			case "hardlink":
				if e := os.Link(p, p+".other"); e != nil {
					t.Fatal(e)
				}
			case "public_mode":
				if e := os.Chmod(p, 0644); e != nil {
					t.Fatal(e)
				}
			}
			if which != "symlink" && which != "hardlink" && which != "public_mode" {
				if e := os.WriteFile(p, raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := r.readReceipt(j.ID); !errors.Is(e, ErrReceipt) {
				t.Fatalf("receipt %s accepted: %v", which, e)
			}
		})
	}
}

func TestArchivePathsRejectTraversalSymlinksAndHardlinks(t *testing.T) {
	r, j, _, _ := filesystemFixture(t)
	for _, id := range []string{"../escape", "/tmp/escape", strings.Repeat("A", 32), j.ID + "/../x", ""} {
		if _, e := r.archivePath(id); e == nil {
			t.Fatalf("unsafe ID accepted: %q", id)
		}
	}
	p, _ := r.archivePath(j.ID)
	if e := os.Link(p, p+".alias"); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Finalize(context.Background(), j); e == nil {
		t.Fatal("hardlink accepted")
	}
	if e := os.Remove(p + ".alias"); e != nil {
		t.Fatal(e)
	}
	if e := os.Rename(p, p+".source"); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(p+".source", p); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Finalize(context.Background(), j); e == nil {
		t.Fatal("symlink accepted")
	}
	if _, e := os.Stat(p + ".source"); e != nil {
		t.Fatal("target unexpectedly changed")
	}
}

func TestRetentionOnlyUnlinksRegisteredArchiveAndIsReplaySafe(t *testing.T) {
	r, j, _, _ := filesystemFixture(t)
	a, e := r.Finalize(context.Background(), j)
	if e != nil {
		t.Fatal(e)
	}
	p, _ := r.archivePath(j.ID)
	unknown := filepath.Join(filepath.Dir(p), "unregistered.backup")
	if e = os.WriteFile(unknown, []byte("preserve"), 0600); e != nil {
		t.Fatal(e)
	}
	bad := a
	bad.ID = strings.Repeat("b", 32)
	if e = r.RemoveArchive(bad); e == nil {
		t.Fatal("mismatched registration deleted archive")
	}
	if e = r.RemoveArchive(a); e != nil {
		t.Fatal(e)
	}
	if e = r.RemoveArchive(a); e != nil {
		t.Fatalf("persisted deletion intent is not replay safe: %v", e)
	}
	if _, e = os.Stat(unknown); e != nil {
		t.Fatal("unregistered file removed")
	}
	if e = os.Symlink(unknown, p); e != nil {
		t.Fatal(e)
	}
	if e = r.RemoveArchive(a); e == nil {
		t.Fatal("retention accepted substituted symlink")
	}
	if _, e = os.Stat(unknown); e != nil {
		t.Fatal("symlink target removed")
	}
}

func TestReceiptWriterCapsOutput(t *testing.T) {
	f, e := os.CreateTemp(t.TempDir(), "receipt-")
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	w := limitedFileWriter{f: f, remaining: 4}
	if n, e := w.Write([]byte("123")); n != 3 || e != nil {
		t.Fatal("initial write failed")
	}
	if n, e := w.Write([]byte("45")); n != 0 || !errors.Is(e, ErrReceipt) || !w.exceeded {
		t.Fatal("receipt output cap bypassed")
	}
	st, _ := f.Stat()
	if st.Size() != 3 {
		t.Fatal("oversized bytes persisted")
	}
}

func TestConfigRejectsUnfixedPathsPrivateIdentityAndUnknownFields(t *testing.T) {
	key, e := age.GenerateX25519Identity()
	if e != nil {
		t.Fatal(e)
	}
	rev := strings.Repeat("c", 40)
	c := Config{Version: 1, HubBinary: filepath.Join(ReleaseDirectory, rev, "dark-pro-hub"), BackupBinary: filepath.Join(ReleaseDirectory, rev, "dark-pro-backup"),
		HubRevision: rev, BackupRevision: rev, HubSHA256: strings.Repeat("a", 64), BackupSHA256: strings.Repeat("b", 64), PublicRecipient: key.Recipient().String(),
		MaxPayloadBytes: 1 << 20, BackupTimeoutSeconds: 60, StopTimeoutSeconds: 30, ResumeTimeoutSeconds: 60}
	if c.Validate() != nil {
		t.Fatal("valid fixed config refused")
	}
	for _, mutate := range []func(*Config){func(v *Config) { v.HubBinary = "/bin/sh" }, func(v *Config) { v.BackupBinary = filepath.Join(ReleaseDirectory, rev, "other") }, func(v *Config) { v.PublicRecipient = key.String() }, func(v *Config) { v.MaxPayloadBytes = 1 << 50 }} {
		bad := c
		mutate(&bad)
		if bad.Validate() == nil {
			t.Fatal("unsafe config accepted")
		}
	}
	var out Config
	raw, _ := json.Marshal(c)
	for _, extra := range []string{`"unit":"other.service",`, `"version":1,`, `"command":"/bin/sh",`} {
		bad := append([]byte("{"+extra), raw[1:]...)
		if strictJSON(bad, &out) == nil {
			t.Fatal("ambiguous or arbitrary-action config accepted")
		}
	}
}

func TestCiphertextPublicationLimitIsBoundedAcrossConfigurationSizes(t *testing.T) {
	if got := (Config{MaxPayloadBytes: 64 << 30}).ciphertextLimit(); got != backupcontrol.MaxArchiveBytes {
		t.Fatalf("control archive cap bypassed: %d", got)
	}
	if got := (Config{MaxPayloadBytes: 8 << 30}).ciphertextLimit(); got != (8<<30)+(8<<30)/16+(16<<20) {
		t.Fatalf("smaller configuration lost its own cap: %d", got)
	}
}

func TestSpaceBudgetIncludesLaterOptionalFilesAndWALGrowth(t *testing.T) {
	dir := t.TempDir()
	r := &LinuxRuntime{config: Config{MaxPayloadBytes: 8 << 30}, uid: uint32(os.Geteuid())}
	writeSparse := func(name string, size int64) string {
		p := filepath.Join(dir, name)
		f, e := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if e != nil {
			t.Fatal(e)
		}
		if e = f.Truncate(size); e != nil {
			t.Fatal(e)
		}
		f.Close()
		return p
	}
	business := writeSparse("business.sqlite", 1<<20)
	optional := filepath.Join(dir, "config-stage.json")
	resources := []hubstate.Resource{{Role: "business", Path: business, Required: true, Present: true}, {Role: "config_stage", Path: optional, Present: false}}
	initial, e := r.requiredSpace(resources, 8<<10)
	if e != nil {
		t.Fatal(e)
	}
	writeSparse("config-stage.json", 8<<20)
	withOptional, e := r.requiredSpace(resources, 8<<10)
	if e != nil || withOptional <= initial+8<<20 {
		t.Fatalf("later optional resource not budgeted: %d -> %d (%v)", initial, withOptional, e)
	}
	writeSparse("business.sqlite-wal", 128<<20)
	withWAL, e := r.requiredSpace(resources, 8<<10)
	if e != nil || withWAL < 4*(128<<20) {
		t.Fatalf("WAL recovery/copy/ciphertext growth underbudgeted: %d -> %d (%v)", withOptional, withWAL, e)
	}
	r.config.MaxPayloadBytes = 64 << 20
	if _, e = r.requiredSpace(resources, 8<<10); !errors.Is(e, ErrPreflight) {
		t.Fatalf("expanded WAL payload over limit accepted: %v", e)
	}
}

func TestOwnedGroupHelper(t *testing.T) {
	mode := os.Getenv("DARK_BACKUP_WORKER_TEST_CHILD")
	if mode == "" {
		return
	}
	if mode == "descendant" {
		child := exec.Command(os.Args[0], "-test.run=^TestOwnedGroupHelper$")
		child.Env = []string{"DARK_BACKUP_WORKER_TEST_CHILD=sleep", "GORACE=atexit_sleep_ms=0", "GOMAXPROCS=2"}
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
		if child.Start() != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	time.Sleep(time.Hour)
	os.Exit(0)
}

func TestOwnedProcessGroupCancellationAndDescendantCleanup(t *testing.T) {
	for _, mode := range []string{"sleep", "descendant"} {
		t.Run(mode, func(t *testing.T) {
			timeout := 100 * time.Millisecond
			if mode == "descendant" {
				timeout = 3 * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedGroupHelper$")
			cmd.Env = []string{"DARK_BACKUP_WORKER_TEST_CHILD=" + mode, "GORACE=atexit_sleep_ms=0", "GOMAXPROCS=2"}
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			// Non-file writers cause os/exec copy goroutines. A surviving
			// descendant would hold their pipe open and block Wait.
			cmd.Stdout, cmd.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
			if e := cmd.Start(); e != nil {
				t.Fatal(e)
			}
			start := time.Now()
			e := waitOwnedGroup(ctx, cmd)
			if time.Since(start) > 2*time.Second || cmd.ProcessState == nil {
				t.Fatal("owned group was not reaped promptly")
			}
			if mode == "sleep" && e == nil {
				t.Fatal("cancelled child reported success")
			}
			if mode == "descendant" && e != nil {
				t.Fatalf("clean parent exit failed: %v", e)
			}
		})
	}
}

// Root deployments use the actual durable store; other developer accounts run
// the injected-journal tests above and the store's own unprivileged test suite.
func TestWorkerWithDurableStoreSurvivesRestart(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("production journal ownership requires root")
	}
	key, e := age.GenerateX25519Identity()
	if e != nil {
		t.Fatal(e)
	}
	dir := filepath.Join(t.TempDir(), "journal")
	s, e := backupcontrol.OpenStore(dir, key.Recipient().String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.Close() }()
	response, e := s.CreateJob(context.Background(), backupcontrol.CreateRequest{EventID: "durable-worker-1", ActorID: "owner-1", Action: "create"})
	if e != nil {
		t.Fatal(e)
	}
	h, r := &fakeController{}, &fakeRunner{}
	h.start = func(context.Context) error { return ErrResume }
	c := Config{BackupTimeoutSeconds: 10, StopTimeoutSeconds: 5, ResumeTimeoutSeconds: 5}
	w := New(s, h, r, c)
	if e = w.execute(context.Background(), response.Job.ID); !errors.Is(e, ErrResume) {
		t.Fatalf("expected durable resume failure: %v", e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
	s, e = backupcontrol.OpenStore(dir, key.Recipient().String())
	if e != nil {
		t.Fatal(e)
	}
	h.start = nil
	w = New(s, h, r, c)
	if e = w.Recover(context.Background()); e != nil {
		t.Fatal(e)
	}
	j, e := s.GetJob(context.Background(), response.Job.ID)
	if e != nil || j.State != "succeeded" || j.ResumeRequired || r.creates.Load() != 1 {
		t.Fatalf("journal recovery contract: %+v %v", j, e)
	}
}
