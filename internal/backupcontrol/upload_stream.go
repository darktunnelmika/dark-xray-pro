package backupcontrol

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

const UploadIdleTimeout = 60 * time.Second
const UploadMaxDuration = 6 * time.Hour

// PrepareUpload changes deadlines for this authenticated streaming request
// only. Other API and Unix metadata requests retain their normal short bounds.
// Full duplex permits a rejected/replayed request to return before draining a
// large body. The body is always closed and is interrupted on cancellation.
func PrepareUpload(w http.ResponseWriter, r *http.Request) (context.Context, io.ReadCloser, func(), error) {
	c := http.NewResponseController(w)
	if err := c.EnableFullDuplex(); err != nil && !(r.ProtoMajor == 2 && errors.Is(err, http.ErrNotSupported)) {
		return nil, nil, nil, ErrUnavailable
	}
	deadline := time.Now().Add(UploadMaxDuration)
	if c.SetWriteDeadline(deadline.Add(30*time.Second)) != nil || c.SetReadDeadline(time.Now().Add(UploadIdleTimeout)) != nil {
		return nil, nil, nil, ErrUnavailable
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	body := &uploadDeadlineBody{body: r.Body, controller: c, ctx: ctx, deadline: deadline}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		_ = body.Close()
	}()
	cleanup := func() {
		cancel()
		<-done
		// Do not leave the just-finished request's cancellation deadline on a
		// reusable HTTP/1 connection. The next request has normal server limits.
		_ = c.SetReadDeadline(time.Time{})
	}
	return ctx, body, cleanup, nil
}

type uploadDeadlineBody struct {
	body       io.ReadCloser
	controller *http.ResponseController
	ctx        context.Context
	deadline   time.Time
	once       sync.Once
	err        error
	deadlineMu sync.Mutex
	eof        bool
	closed     bool
}

func (b *uploadDeadlineBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	b.deadlineMu.Lock()
	if b.closed {
		b.deadlineMu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if b.eof {
		b.deadlineMu.Unlock()
		return 0, io.EOF
	}
	deadline := time.Now().Add(UploadIdleTimeout)
	if deadline.After(b.deadline) {
		deadline = b.deadline
	}
	if err := b.controller.SetReadDeadline(deadline); err != nil {
		b.deadlineMu.Unlock()
		return 0, err
	}
	b.deadlineMu.Unlock()
	n, err := b.body.Read(p)
	if err == io.EOF {
		b.deadlineMu.Lock()
		b.eof = true
		if !b.closed {
			// net/http may now be watching the otherwise idle connection for
			// disconnects while the worker hashes/fsyncs the finished file.
			// The request's absolute context bounds that phase; a surplus-EOF
			// probe must not reinstate the body idle timeout on that connection.
			_ = b.controller.SetReadDeadline(time.Time{})
		}
		b.deadlineMu.Unlock()
	}
	return n, err
}

func (b *uploadDeadlineBody) Close() error {
	b.once.Do(func() {
		// net/http Body.Close may otherwise wait behind a blocked Body.Read.
		b.deadlineMu.Lock()
		b.closed = true
		_ = b.controller.SetReadDeadline(time.Now())
		b.deadlineMu.Unlock()
		b.err = b.body.Close()
	})
	return b.err
}
