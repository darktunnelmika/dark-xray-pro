//go:build linux

package probackup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func noStageLeft(t *testing.T, parent string) {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".probackup-stage-") {
			t.Fatal("owned staging directory remained after a recoverable failure")
		}
	}
}

func TestRestoreVerifiesBeforeValidationAndQuarantines(t *testing.T) {
	key := testKey(t)
	data := bytes.Repeat([]byte("private synthetic bytes"), 30000)
	raw, want := testArchive(t, key, []FileSource{bytesSource("data/business.sqlite", data), bytesSource("inventory.json", []byte("{}"))})
	parent := t.TempDir()
	destination := filepath.Join(parent, "restore")
	calls := 0
	got, err := Restore(context.Background(), bytes.NewReader(raw), key.String(), destination, Limits{}, func(stage string, m Manifest) error {
		calls++
		if !reflect.DeepEqual(m, want) {
			t.Fatal("validator did not receive verified manifest")
		}
		if _, e := os.Lstat(destination); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("destination published before validation")
		}
		if _, e := os.Lstat(filepath.Join(stage, QuarantineName)); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("marker written before validation")
		}
		b, e := os.ReadFile(filepath.Join(stage, "data/business.sqlite"))
		if e != nil || !bytes.Equal(b, data) {
			t.Fatal("staged file differs")
		}
		m.Metadata.BackupID = "callback-mutated"
		m.Metadata.FeatureVersions["refund"] = 99
		m.Files[0].Name = "callback-mutated"
		return nil
	})
	if err != nil || calls != 1 || !reflect.DeepEqual(got, want) {
		t.Fatalf("restore failed: %v", err)
	}
	marker, err := os.ReadFile(filepath.Join(destination, QuarantineName))
	if err != nil {
		t.Fatal(err)
	}
	var mark struct {
		State    string   `json:"state"`
		Manifest Manifest `json:"manifest"`
	}
	if json.Unmarshal(marker, &mark) != nil || mark.State != "quarantined" || !reflect.DeepEqual(mark.Manifest, want) {
		t.Fatal("quarantine metadata changed")
	}
	for _, name := range []string{"", "data", "data/business.sqlite", QuarantineName} {
		info, e := os.Stat(filepath.Join(destination, name))
		if e != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatal("restored file permissions not private")
		}
	}
	noStageLeft(t, parent)
}

func TestRestoreMalformedInputNeverPublishesOrValidates(t *testing.T) {
	key := testKey(t)
	raw, _ := testArchive(t, key, []FileSource{bytesSource("data/db", bytes.Repeat([]byte("x"), 100000))})
	wrongManifest := sealCompressed(t, key, compressRecords(t, rawRecords(t, []string{"data/db"}, func(m *Manifest) { m.Files[0].SHA256 = strings.Repeat("0", 64) })))
	for name, b := range map[string][]byte{"truncated": raw[:len(raw)-1], "manifest": wrongManifest, "trailing": append(append([]byte(nil), raw...), 0)} {
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			sentinel := filepath.Join(parent, "unrelated")
			if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(parent, "restore")
			validated := false
			_, err := Restore(context.Background(), bytes.NewReader(b), key.String(), dest, Limits{}, func(string, Manifest) error { validated = true; return nil })
			if err == nil || validated {
				t.Fatal("malformed archive passed to validator")
			}
			if _, e := os.Lstat(dest); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("failed restore left destination")
			}
			kept, e := os.ReadFile(sentinel)
			if e != nil || string(kept) != "keep" {
				t.Fatal("unrelated data changed")
			}
			noStageLeft(t, parent)
		})
	}
}

func TestRestoreExistingDestinationsAndSymlinkParentsAreRefused(t *testing.T) {
	key := testKey(t)
	raw, _ := testArchive(t, key, []FileSource{bytesSource("db", nil)})
	for _, kind := range []string{"file", "empty-directory", "nonempty-directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "restore")
			outside := filepath.Join(parent, "outside")
			if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "file":
				_ = os.WriteFile(dest, []byte("keep"), 0600)
			case "empty-directory":
				_ = os.Mkdir(dest, 0700)
			case "nonempty-directory":
				_ = os.Mkdir(dest, 0700)
				_ = os.WriteFile(filepath.Join(dest, "keep"), nil, 0600)
			case "symlink":
				_ = os.Symlink(outside, dest)
			}
			_, err := Restore(context.Background(), bytes.NewReader(raw), key.String(), dest, Limits{}, func(string, Manifest) error { return nil })
			if !errors.Is(err, ErrDestination) {
				t.Fatalf("existing destination: %v", err)
			}
			if _, err := os.Lstat(dest); err != nil {
				t.Fatal("existing destination removed")
			}
			noStageLeft(t, parent)
		})
	}
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	link := filepath.Join(parent, "link")
	_ = os.Mkdir(real, 0700)
	_ = os.Symlink(real, link)
	if _, err := Restore(context.Background(), bytes.NewReader(raw), key.String(), filepath.Join(link, "restore"), Limits{}, func(string, Manifest) error { return nil }); !errors.Is(err, ErrDestination) {
		t.Fatal("symlink parent accepted")
	}
	if err := os.Chmod(real, 0770); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), bytes.NewReader(raw), key.String(), filepath.Join(real, "restore"), Limits{}, func(string, Manifest) error { return nil }); !errors.Is(err, ErrDestination) {
		t.Fatal("writable shared parent accepted")
	}
}

func TestValidatorErrorsAndChangesCannotPublish(t *testing.T) {
	key := testKey(t)
	raw, _ := testArchive(t, key, []FileSource{bytesSource("data/db", []byte("original"))})
	for _, change := range []string{"error", "bytes", "extra-file", "extra-directory", "symlink", "hardlink", "permissions"} {
		t.Run(change, func(t *testing.T) {
			parent := t.TempDir()
			dest := filepath.Join(parent, "restore")
			outside := filepath.Join(parent, "outside")
			_ = os.WriteFile(outside, []byte("original"), 0600)
			_, err := Restore(context.Background(), bytes.NewReader(raw), key.String(), dest, Limits{}, func(stage string, _ Manifest) error {
				file := filepath.Join(stage, "data/db")
				switch change {
				case "error":
					return errors.New("secret-token-from-validator")
				case "bytes":
					return os.WriteFile(file, []byte("tampered"), 0600)
				case "extra-file":
					return os.WriteFile(filepath.Join(stage, "db-shm"), nil, 0600)
				case "extra-directory":
					return os.Mkdir(filepath.Join(stage, "unexpected"), 0700)
				case "symlink":
					if e := os.Remove(file); e != nil {
						return e
					}
					return os.Symlink(outside, file)
				case "hardlink":
					if e := os.Remove(file); e != nil {
						return e
					}
					return os.Link(outside, file)
				case "permissions":
					return os.Chmod(file, 0644)
				}
				return nil
			})
			if !errors.Is(err, ErrValidation) || strings.Contains(err.Error(), "secret-token") {
				t.Fatalf("validator change accepted/leaked: %v", err)
			}
			if _, e := os.Lstat(dest); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("invalid stage published")
			}
			kept, e := os.ReadFile(outside)
			if e != nil || string(kept) != "original" {
				t.Fatal("outside file touched")
			}
			noStageLeft(t, parent)
		})
	}
}

func TestNoReplacePublicationRacePreservesWinner(t *testing.T) {
	key := testKey(t)
	raw, _ := testArchive(t, key, []FileSource{bytesSource("db", []byte("data"))})
	parent := t.TempDir()
	dest := filepath.Join(parent, "restore")
	_, err := Restore(context.Background(), bytes.NewReader(raw), key.String(), dest, Limits{}, func(string, Manifest) error {
		if e := os.Mkdir(dest, 0700); e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(dest, "winner"), []byte("keep"), 0600)
	})
	if !errors.Is(err, ErrDestination) {
		t.Fatalf("racing destination: %v", err)
	}
	b, e := os.ReadFile(filepath.Join(dest, "winner"))
	if e != nil || string(b) != "keep" {
		t.Fatal("concurrent destination overwritten")
	}
	noStageLeft(t, parent)
	parent = t.TempDir()
	dest = filepath.Join(parent, "restore")
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, e := Restore(context.Background(), bytes.NewReader(raw), key.String(), dest, Limits{}, func(string, Manifest) error { return nil })
			results <- e
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		} else if !errors.Is(e, ErrDestination) {
			t.Fatalf("unexpected concurrent restore error: %v", e)
		}
	}
	if wins != 1 {
		t.Fatalf("publication winners=%d", wins)
	}
	noStageLeft(t, parent)
}

type panicReader struct{}

func (panicReader) Read([]byte) (int, error) { panic("must not read") }
func TestRestoreNeedsValidatorAndHonorsCancellation(t *testing.T) {
	parent := t.TempDir()
	dest := filepath.Join(parent, "restore")
	if _, err := Restore(context.Background(), panicReader{}, "", dest, Limits{}, nil); !errors.Is(err, ErrOptions) {
		t.Fatal("missing validator allowed")
	}
	noStageLeft(t, parent)
	key := testKey(t)
	raw, _ := testArchive(t, key, []FileSource{bytesSource("db", []byte("data"))})
	ctx, cancel := context.WithCancel(context.Background())
	_, err := Restore(ctx, bytes.NewReader(raw), key.String(), dest, Limits{}, func(string, Manifest) error { cancel(); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled restore: %v", err)
	}
	noStageLeft(t, parent)
	if _, e := os.Lstat(dest); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("cancelled restore published")
	}
	// An identity error may create a temporary stage, but must remove it.
	if _, err := Restore(context.Background(), io.LimitReader(bytes.NewReader(raw), 10), "invalid-private-secret", dest, Limits{}, func(string, Manifest) error { return nil }); !errors.Is(err, ErrIdentity) {
		t.Fatal("invalid identity accepted")
	}
	noStageLeft(t, parent)
}
