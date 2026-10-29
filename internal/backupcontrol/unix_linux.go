//go:build linux

package backupcontrol

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func NewClient(socket string) *Client {
	// The production binding is fixed. A caller cannot turn the privileged
	// control client into an arbitrary TCP/Unix proxy.
	if socket != "" && socket != SocketPath {
		return newClient("", 8*time.Second)
	}
	return newClient(SocketPath, 8*time.Second)
}
func newClient(socket string, metadataTimeout time.Duration) *Client {
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		if socket == "" {
			return nil, ErrInvalid
		}
		return (&net.Dialer{Timeout: 3 * time.Second}).DialContext(ctx, "unix", socket)
	}
	redirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{metadata: &http.Client{Transport: &http.Transport{DialContext: dial, ResponseHeaderTimeout: metadataTimeout, MaxIdleConns: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 30 * time.Second}, Timeout: metadataTimeout, CheckRedirect: redirect}, dialDownload: func(ctx context.Context) (net.Conn, error) { return dial(ctx, "unix", socket) }}
}

type peerListener struct {
	*net.UnixListener
	hubUID uint32
}

func (l *peerListener) Accept() (net.Conn, error) {
	for {
		c, e := l.AcceptUnix()
		if e != nil {
			return nil, e
		}
		raw, e := c.SyscallConn()
		var cred *syscall.Ucred
		var controlErr error
		if e == nil {
			e = raw.Control(func(fd uintptr) {
				cred, controlErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
			})
		}
		if e == nil && controlErr == nil && cred != nil && (cred.Uid == 0 || cred.Uid == l.hubUID) {
			return c, nil
		}
		c.Close()
	}
}

// ServeUnix accepts only root and the explicitly configured HUB UID. The
// socket is connectable but every connection is checked through SO_PEERCRED;
// HTTP headers and claimed actor IDs never establish local process identity.
func ServeUnix(ctx context.Context, path string, hubUID uint32, handler http.Handler) error {
	if ctx == nil || handler == nil || path != SocketPath || os.Geteuid() != 0 {
		return ErrInvalid
	}
	return serveUnixAt(ctx, path, hubUID, 0, handler)
}
func serveUnixAt(ctx context.Context, path string, hubUID, ownerUID uint32, handler http.Handler) error {
	parent := filepath.Dir(path)
	if canonicalPath(path) != nil {
		return ErrUnsafe
	}
	if e := os.Mkdir(parent, 0755); e != nil && !errors.Is(e, os.ErrExist) {
		return ErrUnsafe
	}
	st, e := os.Lstat(parent)
	if e != nil || !st.IsDir() || st.Mode().Perm()&0022 != 0 || !owner(st, ownerUID, false) {
		return ErrUnsafe
	}
	if st, e := os.Lstat(path); e == nil {
		if st.Mode()&os.ModeSocket == 0 || !owner(st, ownerUID, true) {
			return ErrUnsafe
		}
		c, e := net.DialTimeout("unix", path, 200*time.Millisecond)
		if e == nil {
			c.Close()
			return ErrBusy
		}
		if !errors.Is(e, syscall.ECONNREFUSED) {
			return ErrUnsafe
		}
		if os.Remove(path) != nil {
			return ErrUnsafe
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return ErrUnsafe
	}
	l, e := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if e != nil {
		return ErrUnavailable
	}
	l.SetUnlinkOnClose(false)
	if e = os.Chmod(path, 0666); e != nil {
		l.Close()
		os.Remove(path)
		return ErrUnsafe
	}
	published, e := os.Lstat(path)
	if e != nil {
		l.Close()
		return ErrUnsafe
	}
	defer func() {
		l.Close()
		if current, e := os.Lstat(path); e == nil && os.SameFile(published, current) {
			_ = os.Remove(path)
		}
	}()
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	finished := make(chan struct{})
	shutdownDone := make(chan struct{})
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
			_ = server.Close()
		case <-finished:
			_ = server.Close()
		}
	}()
	e = server.Serve(&peerListener{UnixListener: l, hubUID: hubUID})
	// Serve returns when Shutdown closes the listener, before active download
	// leases have drained or been force-closed. Keep the store alive until the
	// bounded shutdown path has completed, including unexpected Serve failures.
	if ctx.Err() == nil {
		close(finished)
		<-shutdownDone
	} else {
		<-shutdownDone
		close(finished)
	}
	if errors.Is(e, http.ErrServerClosed) || ctx.Err() != nil {
		return nil
	}
	return e
}

// PeerUIDLabel is bounded diagnostic metadata, never an authorization input.
func PeerUIDLabel(uid uint32) string { return strconv.FormatUint(uint64(uid), 10) }
