package probackup

import (
	"context"
	"errors"
	"io"
)

const copyBufferSize = 64 << 10

// Unlike io.LimitReader, reaching this limit never manufactures a successful
// EOF. One byte is probed so exact-limit input and over-limit input differ.
type budgetReader struct {
	ctx       context.Context
	r         io.Reader
	remaining int64
}

func (r *budgetReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var b [1]byte
		n, err := r.r.Read(b[:])
		if n != 0 {
			return 0, ErrLimit
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.r.Read(p)
	if n < 0 || n > len(p) {
		return 0, ErrIO
	}
	r.remaining -= int64(n)
	return n, err
}

type budgetWriter struct {
	ctx       context.Context
	w         io.Writer
	remaining int64
	err       error
}

func (w *budgetWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	if err := w.ctx.Err(); err != nil {
		w.err = err
		return 0, err
	}
	if int64(len(p)) > w.remaining {
		w.err = ErrLimit
		return 0, w.err
	}
	n, err := w.w.Write(p)
	if n < 0 || n > len(p) {
		w.err = ErrIO
		return 0, w.err
	}
	w.remaining -= int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = safeError(err, ErrIO)
	}
	return n, w.err
}

// Readers and writers supplied by callers must themselves be finite or
// cancellable. No goroutine is detached to interrupt caller-owned I/O.
func copyExact(ctx context.Context, dst io.Writer, src io.Reader, size int64, buf []byte) error {
	for size > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		want := int64(len(buf))
		if size < want {
			want = size
		}
		n, err := io.ReadFull(src, buf[:int(want)])
		if n != 0 {
			written, writeErr := dst.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}
			if written != n {
				return ErrIO
			}
			size -= int64(n)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func requireEOF(ctx context.Context, r io.Reader) error {
	var b [1]byte
	for attempts := 0; attempts < 100; attempts++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(b[:])
		if n != 0 {
			return ErrFormat
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return safeError(err, ErrIntegrity)
		}
	}
	return ErrIO
}

func safeError(err, fallback error) error {
	for _, known := range []error{context.Canceled, context.DeadlineExceeded, ErrLimit, ErrFormat, ErrIntegrity, ErrIO} {
		if errors.Is(err, known) {
			return known
		}
	}
	return fallback
}
