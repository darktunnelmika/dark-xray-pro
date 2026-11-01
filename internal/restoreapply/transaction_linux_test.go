//go:build linux

package restoreapply

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/hubstate"
	"os"
	"path/filepath"
	"testing"
)

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func fixture(t *testing.T) (string, string, []Replacement) {
	t.Helper()
	root, dir, src := t.TempDir(), t.TempDir(), t.TempDir()
	for _, p := range []string{root, dir, src} {
		must(t, os.Chmod(p, 0700))
	}
	lock, e := hubstate.Acquire(root)
	must(t, e)
	must(t, lock.Close())
	must(t, os.WriteFile(filepath.Join(root, "data"), []byte("original financial values"), 0600))
	must(t, os.WriteFile(filepath.Join(root, "data-wal"), []byte("original committed WAL"), 0600))
	b := []byte("verified historical values")
	h := sha256.Sum256(b)
	source := filepath.Join(src, "candidate")
	must(t, os.WriteFile(source, b, 0600))
	return root, dir, []Replacement{{Destination: "data", Source: source, Present: true, Size: int64(len(b)), SHA256: hex.EncodeToString(h[:])}, {Destination: "data-wal"}, {Destination: "optional", Source: source, Present: true, Size: int64(len(b)), SHA256: hex.EncodeToString(h[:])}}
}
func openTX(t *testing.T, root, dir string) *Transaction {
	t.Helper()
	tx, e := Open(root, dir, "11111111111111111111111111111111", os.Geteuid(), os.Getegid())
	must(t, e)
	return tx
}
func TestRestoreFailureAtEveryReplacementRecoversExactOriginal(t *testing.T) {
	for point := 0; point < 3; point++ {
		t.Run(string(rune('a'+point)), func(t *testing.T) {
			root, dir, files := fixture(t)
			tx := openTX(t, root, dir)
			ctx := context.Background()
			must(t, tx.Prepare(ctx, files))
			tx.afterWrite = func(i int) error {
				if i == point {
					return errors.New("simulated power loss")
				}
				return nil
			}
			if e := tx.Apply(ctx); e == nil {
				t.Fatal("failure not injected")
			}
			must(t, tx.Close())
			if _, e := hubstate.Acquire(root); !errors.Is(e, hubstate.ErrQuarantined) {
				t.Fatal("partial data admitted writers", e)
			}
			tx = openTX(t, root, dir)
			must(t, tx.Rollback(ctx))
			must(t, tx.ReleaseForStart(false))
			must(t, tx.Close())
			for name, want := range map[string]string{"data": "original financial values", "data-wal": "original committed WAL"} {
				b, e := os.ReadFile(filepath.Join(root, name))
				must(t, e)
				if string(b) != want {
					t.Fatal("original changed", name)
				}
			}
			if _, e := os.Lstat(filepath.Join(root, "optional")); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("optional remained", e)
			}
			lock, e := hubstate.Acquire(root)
			must(t, e)
			must(t, lock.Close())
		})
	}
}
func TestRestoreStartBarrierNeverRewindsNewFinancialWrites(t *testing.T) {
	root, dir, files := fixture(t)
	ctx := context.Background()
	tx := openTX(t, root, dir)
	must(t, tx.Prepare(ctx, files))
	must(t, tx.Apply(ctx))
	if _, e := hubstate.Acquire(root); !errors.Is(e, hubstate.ErrQuarantined) {
		t.Fatal(e)
	}
	must(t, tx.ReleaseForStart(true))
	must(t, tx.Close())
	must(t, os.WriteFile(filepath.Join(root, "data"), []byte("new transaction accepted after restore"), 0600))
	tx = openTX(t, root, dir)
	defer tx.Close()
	if e := tx.Rollback(ctx); !errors.Is(e, ErrStarted) {
		t.Fatal("old balances replaced newer work", e)
	}
	b, e := os.ReadFile(filepath.Join(root, "data"))
	must(t, e)
	if string(b) != "new transaction accepted after restore" {
		t.Fatal("new data lost")
	}
}
func TestRestoreRefusesSymlinksAndUnexpectedTargetMutation(t *testing.T) {
	root, dir, files := fixture(t)
	ctx := context.Background()
	tx := openTX(t, root, dir)
	defer tx.Close()
	must(t, tx.Prepare(ctx, files))
	must(t, os.WriteFile(filepath.Join(root, "data"), []byte("unregistered writer changed this"), 0600))
	if e := tx.Apply(ctx); e == nil {
		t.Fatal("unexpected target accepted")
	}
	if e := tx.Rollback(ctx); e == nil {
		t.Fatal("unexpected target overwritten on rollback")
	}
}
func TestRestoreValidationFailureDoesNotChangeTarget(t *testing.T) {
	root, dir, files := fixture(t)
	tx := openTX(t, root, dir)
	defer tx.Close()
	files[0].SHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if e := tx.Prepare(context.Background(), files); e == nil {
		t.Fatal("wrong candidate digest accepted")
	}
	must(t, tx.Rollback(context.Background()))
	must(t, tx.ReleaseForStart(false))
	b, e := os.ReadFile(filepath.Join(root, "data"))
	must(t, e)
	if string(b) != "original financial values" {
		t.Fatal("failed preparation touched target")
	}
}

func TestRestoreAbsentOptionalParentNeedsNoFilesystemMutation(t *testing.T) {
	root, dir, files := fixture(t)
	files = append(files, Replacement{Destination: "absent-directory/optional.sqlite"})
	tx := openTX(t, root, dir)
	defer tx.Close()
	must(t, tx.Prepare(context.Background(), files))
	must(t, tx.Apply(context.Background()))
	must(t, tx.Rollback(context.Background()))
	must(t, tx.ReleaseForStart(false))
	if _, e := os.Stat(filepath.Join(root, "absent-directory")); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("optional parent was created", e)
	}
}
