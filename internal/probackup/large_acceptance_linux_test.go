//go:build linux

package probackup_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/darktunnelmika/dark-xray-pro/internal/probackup"
)

// This explicit gate exercises the container above 3 GiB without allocating a
// payload-sized buffer. Semantic SQLite/accounting roundtrips are separate.
const largePayloadBytes int64 = 3<<30 + 65537

type repeatingBackupReader struct{ remaining int64 }

func (r *repeatingBackupReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	for i := range p {
		p[i] = 0x5a
	}
	r.remaining -= int64(len(p))
	return len(p), nil
}

func TestNativeBackupLargeStreamingAcceptance(t *testing.T) {
	if os.Getenv("DARK_PRO_BACKUP_LARGE_ACCEPTANCE") != "1" {
		t.Skip("explicit >3 GiB disk/RAM/interruption gate; enabled in Foundation CI")
	}
	if dir := os.Getenv("DARK_PRO_BACKUP_ACCEPTANCE_CHILD"); dir != "" {
		runInterruptedRestoreChild(t, dir)
		return
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "identity")
	if err := os.WriteFile(keyPath, []byte(identity.String()), 0600); err != nil {
		t.Fatal(err)
	}
	archivePath := filepath.Join(dir, "large.age")
	file, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	meta := probackup.Metadata{BackupID: "large-stream-acceptance", CreatedUnix: 1780000000,
		SourceRevision: strings.Repeat("a", 40), BusinessSchemaVersion: 16}
	sources := []probackup.FileSource{{Name: "runtime/large.bin", Role: probackup.RoleInboundRuntime, Size: largePayloadBytes,
		Open: func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(&repeatingBackupReader{remaining: largePayloadBytes}), nil
		}}}
	manifest, err := probackup.Write(context.Background(), file, identity.Recipient().String(), meta, sources, probackup.DefaultLimits())
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	archiveInfo, err := os.Stat(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Files) != 1 || manifest.Files[0].Size != largePayloadBytes {
		t.Fatal("large file length truncated")
	}
	verify, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := probackup.Verify(context.Background(), verify, identity.String(), probackup.DefaultLimits()); err != nil {
		verify.Close()
		t.Fatal(err)
	}
	verify.Close()

	// Kill a real independent restore process after every plaintext byte has
	// been staged but before validation/publication. No partial destination may
	// appear. A fresh retry reads the original ciphertext and publishes once.
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	childCtx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	child := exec.CommandContext(childCtx, executable, "-test.run=^TestNativeBackupLargeStreamingAcceptance$", "-test.v")
	child.Env = append(os.Environ(), "DARK_PRO_BACKUP_ACCEPTANCE_CHILD="+dir)
	output, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	child.Stderr = child.Stdout
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			if scanner.Text() == "RESTORE_VALIDATION_ENTERED" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			_ = child.Process.Kill()
			_ = child.Wait()
			t.Fatal("child never reached pre-publication validation")
		}
	case <-childCtx.Done():
		_ = child.Process.Kill()
		_ = child.Wait()
		t.Fatal("child restore timeout")
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = child.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !exit.Sys().(syscall.WaitStatus).Signaled() || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatal("restore was not interrupted by SIGKILL")
	}
	destination := filepath.Join(dir, "restored")
	if _, err := os.Lstat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("interrupted restore published a partial destination")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	staleStages := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".probackup-stage-") {
			info, err := entry.Info()
			if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
				t.Fatal("interrupted stage is not private")
			}
			staleStages++
		}
	}
	if staleStages != 1 {
		t.Fatal("expected one identifiable private interrupted stage")
	}
	retry, err := os.Open(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := probackup.Restore(context.Background(), retry, identity.String(), destination,
		probackup.DefaultLimits(), func(stage string, m probackup.Manifest) error {
			info, err := os.Stat(filepath.Join(stage, "runtime/large.bin"))
			if err != nil || info.Size() != largePayloadBytes || len(m.Files) != 1 || m.Files[0] != manifest.Files[0] {
				return errors.New("large-file verification failed")
			}
			return nil
		})
	retry.Close()
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Files[0] != manifest.Files[0] {
		t.Fatal("retry changed file digest or length")
	}
	if _, err := os.Stat(filepath.Join(destination, probackup.QuarantineName)); err != nil {
		t.Fatal("missing quarantine marker")
	}
	payload, err := os.Open(filepath.Join(destination, "runtime/large.bin"))
	if err != nil {
		t.Fatal(err)
	}
	var tail [32]byte
	if _, err := payload.ReadAt(tail[:], largePayloadBytes-int64(len(tail))); err != nil {
		t.Fatal(err)
	}
	payload.Close()
	for _, b := range tail {
		if b != 0x5a {
			t.Fatal("large file tail corrupted")
		}
	}
	var own, children syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &own) != nil || syscall.Getrusage(syscall.RUSAGE_CHILDREN, &children) != nil {
		t.Fatal("RSS measurement unavailable")
	}
	if own.Maxrss > 256<<10 || children.Maxrss > 256<<10 {
		t.Fatal("streaming exceeded 256 MiB peak process RSS")
	}
	t.Logf("payload_bytes=%d ciphertext_bytes=%d peak_self_rss_kib=%d peak_child_rss_kib=%d elapsed_ms=%d interrupted=SIGKILL destination_unpublished=true retry_exact=true quarantine=true",
		largePayloadBytes, archiveInfo.Size(), own.Maxrss, children.Maxrss, time.Since(started).Milliseconds())
}

func runInterruptedRestoreChild(t *testing.T, dir string) {
	key, err := os.ReadFile(filepath.Join(dir, "identity"))
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.Open(filepath.Join(dir, "large.age"))
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	_, err = probackup.Restore(context.Background(), archive, string(key), filepath.Join(dir, "restored"),
		probackup.DefaultLimits(), func(_ string, _ probackup.Manifest) error {
			fmt.Fprintln(os.Stdout, "RESTORE_VALIDATION_ENTERED")
			<-time.After(10 * time.Minute)
			return errors.New("parent failed to interrupt restore")
		})
	t.Fatal("interrupted restore unexpectedly returned", err)
}
