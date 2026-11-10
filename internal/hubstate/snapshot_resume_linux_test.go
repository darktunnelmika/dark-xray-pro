//go:build linux

package hubstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSnapshotReadyReleasesSourceBeforeProjectionAndNeverRereadsIt(t *testing.T) {
	lock, inv := fixture(t)
	root := lock.root
	dest := privateTemp(t)
	ready := false
	var restarted *Lock
	defer func() {
		if restarted != nil {
			restarted.Close()
		}
	}()
	_, err := SnapshotWithBusinessTransform(context.Background(), lock, inv, dest, func(ctx context.Context, path string) error {
		if !ready || path != filepath.Join(dest, "data/business.sqlite") {
			t.Fatal("projection ran before source release")
		}
		return nil
	}, func() error {
		if err := lock.Close(); err != nil {
			return err
		}
		var err error
		restarted, err = Acquire(root)
		if err != nil {
			return err
		}
		if err = os.Rename(filepath.Join(root, "business.sqlite"), filepath.Join(root, "after-copy.sqlite")); err != nil {
			return err
		}
		ready = true
		return nil
	})
	if err != nil || !ready {
		t.Fatal("private snapshot depended on live source", err)
	}
}
func TestSnapshotReadyFailureRefusesProjection(t *testing.T) {
	lock, inv := fixture(t)
	want := errors.New("resume pipe failed")
	projected := false
	_, err := SnapshotWithBusinessTransform(context.Background(), lock, inv, privateTemp(t), func(context.Context, string) error { projected = true; return nil }, func() error { return want })
	if !errors.Is(err, want) || projected {
		t.Fatal("projection continued after failed handshake", err)
	}
}
