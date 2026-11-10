//go:build linux

package backupworker

import (
	"context"
	"io"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestSnapshotPipeHelper(t *testing.T) {
	mode := os.Getenv("DARK_PRO_TEST_SNAPSHOT_PIPE")
	if mode == "" {
		return
	}
	signal := os.NewFile(3, "signal")
	ack := os.NewFile(4, "ack")
	if mode == "good" {
		_, _ = signal.Write([]byte{'S'})
	} else if mode == "invalid" {
		_, _ = signal.Write([]byte{'X'})
	} else if mode == "eof" {
		signal.Close()
		os.Exit(0)
	}
	signal.Close()
	var b [1]byte
	if _, err := io.ReadFull(ack, b[:]); err != nil || b[0] != 'A' {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestSnapshotPipeResumesWhileChildIsStillPacking(t *testing.T) {
	for _, mode := range []string{"good", "invalid", "eof"} {
		t.Run(mode, func(t *testing.T) {
			read, write, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer read.Close()
			defer write.Close()
			ackRead, ackWrite, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer ackRead.Close()
			defer ackWrite.Close()
			cmd := exec.Command(os.Args[0], "-test.run=^TestSnapshotPipeHelper$")
			cmd.Env = append(os.Environ(), "DARK_PRO_TEST_SNAPSHOT_PIPE="+mode)
			cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
			cmd.ExtraFiles = []*os.File{write, ackRead}
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			write.Close()
			ackRead.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			resumed := false
			err = waitSnapshotOwnedGroup(ctx, cmd, read, func() error { resumed = true; _, e := ackWrite.Write([]byte{'A'}); return e })
			if mode == "good" {
				if err != nil || !resumed {
					t.Fatal("child waited for resume", err)
				}
			} else if err == nil || resumed {
				t.Fatal("invalid signal resumed panel", err)
			}
		})
	}
}
