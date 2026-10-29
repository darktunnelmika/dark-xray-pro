//go:build linux

package backupimport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// envelopeCheck is only a bounded plausibility check of the public age header.
// It does not invoke age, authenticate a recipient, decrypt data, or parse the
// native archive. Even success leaves compatibility and restore unverified.
type envelopeCheck struct {
	line                 []byte
	headerBytes, stanzas int
	first, finished      bool
	payloadBytes         int64
}

func (e *envelopeCheck) feed(data []byte) error {
	for i, b := range data {
		if e.finished {
			e.payloadBytes += int64(len(data) - i)
			return nil
		}
		e.headerBytes++
		if e.headerBytes > 64<<10 || len(e.line) >= 1024 {
			return ErrInvalid
		}
		e.line = append(e.line, b)
		if b != '\n' {
			continue
		}
		line := e.line[:len(e.line)-1]
		if !e.first {
			if !bytes.Equal(line, []byte("age-encryption.org/v1")) {
				return ErrInvalid
			}
			e.first = true
		} else if bytes.HasPrefix(line, []byte("-> ")) {
			if len(line) <= 3 {
				return ErrInvalid
			}
			e.stanzas++
			if e.stanzas > 32 {
				return ErrInvalid
			}
			for _, c := range line[3:] {
				if c < 0x21 || c > 0x7e {
					if c != ' ' {
						return ErrInvalid
					}
				}
			}
		} else if bytes.HasPrefix(line, []byte("--- ")) {
			mac, err := base64.RawStdEncoding.DecodeString(string(line[4:]))
			if err != nil || len(mac) != 32 || e.stanzas < 1 {
				return ErrInvalid
			}
			e.finished = true
		} else {
			if e.stanzas < 1 || len(line) > 64 {
				return ErrInvalid
			}
			if _, err := base64.RawStdEncoding.DecodeString(string(line)); err != nil {
				return ErrInvalid
			}
		}
		e.line = e.line[:0]
	}
	return nil
}

func (e *envelopeCheck) complete() bool { return e.finished && e.payloadBytes >= 32 }

func (s *Store) finishActive(a *activeReceive) {
	a.cancel()
	s.mu.Lock()
	if s.active == a {
		s.active = nil
	}
	_ = s.closeResources()
	s.mu.Unlock()
}

func (s *Store) receiveFailure(a *activeReceive, state, code string, cause error) (Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := s.data.Uploads[a.id]
	if u.State == "cancelled" {
		return Response{Upload: u}, context.Canceled
	}
	if a.published {
		state = "publication_unknown"
		code = "publication_unconfirmed"
	}
	if !s.closed && !s.broken {
		u.State = state
		u.Downloadable = false
		u.Verification = "unverified"
		u.ErrorCode = code
		u.ReceivedBytes = min(a.bytes.Load(), u.SizeBytes)
		if err := s.put(u); err != nil {
			return Response{Upload: s.data.Uploads[a.id]}, err
		}
	}
	if cause == nil {
		cause = ErrUnavailable
	}
	return Response{Upload: s.data.Uploads[a.id]}, cause
}

// Receive is a full-file attempt. The allocation's event, actor, exact length,
// and SHA-256 remain immutable across every retry. A completed allocation is a
// receipt replay and never causes its bytes to be overwritten or re-read here.
// Callers must make a blocking HTTP body interruptible before passing it in;
// cancellation invokes Close outside the store mutex when available.
func (s *Store) Receive(ctx context.Context, id, actor string, size int64, body io.Reader) (Response, error) {
	s.mu.Lock()
	u, err := s.actorUpload(ctx, id, actor)
	if err != nil {
		s.mu.Unlock()
		return Response{}, err
	}
	if body == nil || size != u.SizeBytes {
		s.mu.Unlock()
		return Response{}, ErrInvalid
	}
	if u.State == "received" {
		s.mu.Unlock()
		return Response{Upload: u, Replayed: true}, nil
	}
	if u.State != "awaiting_upload" && u.State != "interrupted" && u.State != "rejected" {
		s.mu.Unlock()
		return Response{}, ErrConflict
	}
	if s.active != nil {
		s.mu.Unlock()
		return Response{}, ErrBusy
	}
	if u.Attempts >= 1000000 {
		s.mu.Unlock()
		return Response{}, ErrCapacity
	}
	if err := s.space(0); err != nil {
		s.mu.Unlock()
		return Response{}, err
	}
	transfer, cancel := context.WithCancel(ctx)
	a := &activeReceive{id: id, cancel: cancel}
	if closer, ok := body.(io.Closer); ok {
		a.closer = closer
	}
	u.State = "receiving"
	u.ReceivedBytes = 0
	u.Attempts++
	u.ErrorCode = ""
	u.Downloadable = false
	u.Verification = "unverified"
	if err = s.put(u); err != nil {
		cancel()
		s.mu.Unlock()
		return Response{}, err
	}
	u = s.data.Uploads[id]
	s.active = a
	s.mu.Unlock()
	defer s.finishActive(a)
	// Parent-request cancellation must also interrupt an otherwise blocked Read.
	stopWatch := make(chan struct{})
	watchDone := make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-transfer.Done():
			a.stop()
		case <-stopWatch:
		}
	}()
	// Join the watcher before finishActive cancels its private context. A
	// successful EOF must not asynchronously close the caller's HTTP body and
	// interrupt the connection that still has to deliver the receipt.
	defer func() { close(stopWatch); <-watchDone }()
	d, err := s.uploadDirectory(id, true)
	if err != nil {
		return s.receiveFailure(a, "interrupted", "storage_unavailable", err)
	}
	defer d.Close()
	_, _, hp, hf, err := s.inspectDirectory(d, u)
	if err != nil {
		return s.receiveFailure(a, "interrupted", "unsafe_upload_directory", ErrUnsafe)
	}
	if hf {
		s.mu.Lock()
		a.published = true
		s.mu.Unlock()
		return s.receiveFailure(a, "publication_unknown", "unexpected_publication", ErrUnsafe)
	}
	if hp {
		// Only the exact registered partial is removed for a from-zero retry.
		f, e := openFileAt(d, partialName, s.uid, u.SizeBytes)
		if e != nil {
			return s.receiveFailure(a, "interrupted", "unsafe_partial_file", ErrUnsafe)
		}
		st, e := f.Stat()
		if e != nil || !unchangedAt(d, partialName, st) {
			_ = f.Close()
			return s.receiveFailure(a, "interrupted", "unsafe_partial_file", ErrUnsafe)
		}
		e = unix.Unlinkat(int(d.Fd()), partialName, 0)
		_ = f.Close()
		if e != nil || s.syncDir(d) != nil {
			return s.receiveFailure(a, "interrupted", "storage_unavailable", ErrUnavailable)
		}
	}
	if err = transfer.Err(); err != nil {
		return s.receiveFailure(a, "interrupted", "upload_interrupted", err)
	}
	fd, err := unix.Openat(int(d.Fd()), partialName, unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return s.receiveFailure(a, "interrupted", "storage_unavailable", ErrUnavailable)
	}
	f := os.NewFile(uintptr(fd), partialName)
	defer f.Close()
	hash := sha256.New()
	envelope := envelopeCheck{}
	buf := make([]byte, 64<<10)
	var total, checkedSpace int64
	emptyReads := 0
	for {
		if err := transfer.Err(); err != nil {
			return s.receiveFailure(a, "interrupted", "upload_interrupted", err)
		}
		if total == 0 || total-checkedSpace >= 1<<20 {
			free, e := s.available()
			if e != nil || free < s.limits.minFree+min(int64(len(buf)), u.SizeBytes-total) {
				return s.receiveFailure(a, "interrupted", "disk_capacity", ErrCapacity)
			}
			checkedSpace = total
		}
		want := min(int64(len(buf)), u.SizeBytes-total+1)
		n, readErr := body.Read(buf[:int(want)])
		if n < 0 || n > int(want) {
			return s.receiveFailure(a, "rejected", "invalid_upload_reader", ErrInvalid)
		}
		if n > 0 {
			emptyReads = 0
			if total+int64(n) > u.SizeBytes {
				return s.receiveFailure(a, "rejected", "size_mismatch", ErrInvalid)
			}
			if envelope.feed(buf[:n]) != nil {
				return s.receiveFailure(a, "rejected", "unsupported_envelope", ErrInvalid)
			}
			written, e := f.Write(buf[:n])
			total += int64(written)
			a.bytes.Store(total)
			if e != nil || written != n {
				return s.receiveFailure(a, "interrupted", "storage_unavailable", ErrUnavailable)
			}
			_, _ = hash.Write(buf[:n])
		} else if readErr == nil {
			emptyReads++
			if emptyReads >= 100 {
				return s.receiveFailure(a, "interrupted", "upload_no_progress", ErrUnavailable)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return s.receiveFailure(a, "interrupted", "upload_interrupted", ErrUnavailable)
		}
	}
	if total != u.SizeBytes {
		return s.receiveFailure(a, "rejected", "size_mismatch", ErrInvalid)
	}
	if !envelope.complete() {
		return s.receiveFailure(a, "rejected", "unsupported_envelope", ErrInvalid)
	}
	if hex.EncodeToString(hash.Sum(nil)) != u.SHA256 {
		return s.receiveFailure(a, "rejected", "checksum_mismatch", ErrInvalid)
	}
	if err := transfer.Err(); err != nil {
		return s.receiveFailure(a, "interrupted", "upload_interrupted", err)
	}
	if f.Chmod(0400) != nil || s.syncFile(f) != nil || f.Close() != nil {
		return s.receiveFailure(a, "interrupted", "storage_unavailable", ErrUnavailable)
	}
	// Cancellation and no-replace publication share the short metadata lock;
	// no full-file streaming or hashing occurs while this lock is held.
	s.mu.Lock()
	err = s.healthy(transfer)
	u = s.data.Uploads[id]
	if err == nil && u.State != "receiving" {
		err = context.Canceled
	}
	if err == nil {
		u.State = "checking"
		u.ReceivedBytes = total
		err = s.put(u)
	}
	if err == nil {
		before, e := d.Stat()
		if e != nil || !unchangedAt(s.root, id, before) {
			err = ErrUnsafe
		}
	}
	if err == nil {
		err = s.rename(int(d.Fd()), partialName, int(d.Fd()), contentName, unix.RENAME_NOREPLACE)
		if err == nil {
			a.published = true
			err = s.syncDir(d)
		} else if errors.Is(err, unix.EEXIST) {
			a.published = true
			err = ErrUnsafe
		}
	}
	s.mu.Unlock()
	if err != nil {
		return s.receiveFailure(a, "interrupted", "publication_failed", err)
	}
	// Publication is not a checksum receipt until an independent read of the
	// final immutable file and its named identity agrees with the commitment.
	final, err := s.verifiedFile(transfer, d, u)
	if err != nil {
		return s.receiveFailure(a, "publication_unknown", "published_file_unverified", err)
	}
	_ = final.Close()
	s.mu.Lock()
	err = s.healthy(transfer)
	if err == nil {
		u = s.data.Uploads[id]
		if u.State != "checking" {
			err = ErrUnsafe
		} else {
			u.State = "received"
			u.ReceivedBytes = total
			u.Downloadable = true
			u.Verification = "ciphertext_checksum_verified"
			u.ErrorCode = ""
			err = s.put(u)
		}
	}
	result := Response{Upload: s.data.Uploads[id]}
	s.mu.Unlock()
	if err != nil {
		return s.receiveFailure(a, "publication_unknown", "publication_unconfirmed", err)
	}
	return result, nil
}
