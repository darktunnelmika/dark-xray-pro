//go:build linux

package backupworker

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// The private pipe is inherited only by the pinned archive process. A copied
// snapshot permits an early resume, never early archive publication.
func waitSnapshotOwnedGroup(ctx context.Context, cmd *exec.Cmd, ready *os.File, resume func() error) error {
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		var signal [1]byte
		_, err := io.ReadFull(ready, signal[:])
		if err == nil && signal[0] == 'S' {
			err = resume()
		} else {
			err = ErrCreate
		}
		if err != nil {
			cancel()
		}
		result <- err
	}()
	err := waitOwnedGroup(childCtx, cmd)
	// A failed/uninterruptible child must not keep a pipe reader alive forever.
	// On normal exit the child's descriptor is closed; drain its buffered signal.
	if err != nil {
		_ = ready.Close()
	}
	readyErr := <-result
	if err == nil {
		err = readyErr
	}
	return err
}
