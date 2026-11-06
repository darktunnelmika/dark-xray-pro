//go:build linux

package inboundruntime

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	xraystats "github.com/darktunnelmika/dark-xray-pro/internal/xraystats"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/status"
)

type nativeStatsTestServer struct {
	xraystats.UnimplementedStatsServiceServer
	query func(context.Context, *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error)
}

func (s *nativeStatsTestServer) QueryStats(ctx context.Context, request *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) {
	if request.Reset_ { return nil, status.Error(codes.InvalidArgument, "reset forbidden") }
	return s.query(ctx, request)
}

type nativeStatsTestListener struct {
	net.Listener
	mu sync.Mutex
	connections []net.Conn
	accepted atomic.Int64
}

func (l *nativeStatsTestListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		l.mu.Lock()
		l.connections = append(l.connections, conn)
		l.accepted.Add(1)
		l.mu.Unlock()
	}
	return conn, err
}

func (l *nativeStatsTestListener) dropConnections() {
	l.mu.Lock()
	connections := l.connections
	l.connections = nil
	l.mu.Unlock()
	for _, conn := range connections { _ = conn.Close() }
}

type nativeStatsTestFixture struct {
	p *Native
	listener *nativeStatsTestListener
}

// This in-process peer is used only for transport tests. Cleanup deliberately
// never calls Native.Stop: that method must only be tested with an owned child.
func newNativeStatsTestFixture(t *testing.T, query func(context.Context, *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error)) *nativeStatsTestFixture {
	t.Helper()
	dir, err := os.MkdirTemp("", "dxp-stats-")
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0700); err != nil { t.Fatal(err) }
	socket := filepath.Join(dir, "stats.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil { t.Fatal(err) }
	listener.SetUnlinkOnClose(false)
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(socket, 0600); err != nil { t.Fatal(err) }
	directory, err := os.Open(dir)
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = directory.Close() })
	identity, err := usagecheckpoint.Process(os.Getpid())
	if err != nil { t.Fatal(err) }
	process, err := os.FindProcess(os.Getpid())
	if err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = process.Release() })
	p := &Native{cmd: &exec.Cmd{Process: process}, done: make(chan struct{}), dir: dir, apiDirectory: directory, apiSocket: socket, identity: identity, seen: map[string]bool{}, last: map[string]inboundexec.Counter{}, principals: []inboundexec.Principal{{ID: "synthetic", Engine: "xray", Counter: "user>>>synthetic>>>traffic"}}}
	tracked := &nativeStatsTestListener{Listener: listener}
	server := grpc.NewServer()
	xraystats.RegisterStatsServiceServer(server, &nativeStatsTestServer{query: query})
	serveDone := make(chan struct{})
	go func() { defer close(serveDone); _ = server.Serve(tracked) }()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		select { case <-serveDone: case <-time.After(2*time.Second): t.Error("synthetic server did not stop") }
	})
	p.stats, err = newNativeStatsClient(p)
	if err != nil { t.Fatal(err) }
	t.Cleanup(p.stats.close)
	if p.checkIdentity() != nil || !p.ownsUnixControl() { t.Fatal("synthetic peer is not owned") }
	return &nativeStatsTestFixture{p: p, listener: tracked}
}

func nativeStatsTestResponse(values map[string]int64) *xraystats.QueryStatsResponse {
	response := &xraystats.QueryStatsResponse{}
	for name, value := range values { response.Stat = append(response.Stat, &xraystats.Stat{Name: name, Value: value}) }
	return response
}

func nativeStatsTestWaitError(t *testing.T, result <-chan error) error {
	t.Helper()
	select { case err := <-result: return err; case <-time.After(4*time.Second): t.Fatal("bounded query did not return"); return nil }
}

func TestNativeStatsReusesOwnedConnectionAndCustomProtocol(t *testing.T) {
	var mu sync.Mutex
	active, sealed := false, false
	var patterns []string
	fixture := newNativeStatsTestFixture(t, func(_ context.Context, request *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) {
		mu.Lock()
		defer mu.Unlock()
		patterns = append(patterns, request.Pattern)
		values := map[string]int64{}
		switch {
		case request.Pattern == nativeProtocol+"capabilities": values[nativeProtocol+"scope/native-inbounds-v2"] = 1
		case request.Pattern == nativeProtocol+"health":
			values[nativeProtocol+"engine-ready"] = 1
			if active { values[nativeProtocol+"admission-open"] = 1 }
			if sealed { values[nativeProtocol+"final-sealed"] = 1 }
		case strings.HasPrefix(request.Pattern, nativeProtocol+"activate/"):
			if sealed { return nil, status.Error(codes.FailedPrecondition, "sealed") }
			active = true
			values[strings.Replace(request.Pattern, "activate/", "activated/", 1)] = 1
		case request.Pattern == "" || strings.HasPrefix(request.Pattern, nativeProtocol+"final/"):
			values["user>>>synthetic>>>traffic>>>uplink"] = 9
			values["user>>>synthetic>>>traffic>>>downlink"] = 13
			if request.Pattern != "" { sealed = true; active = false; values[request.Pattern] = 1 }
		default: return nil, status.Error(codes.InvalidArgument, "pattern")
		}
		return nativeStatsTestResponse(values), nil
	})
	p := fixture.p
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if values, err := p.query(ctx, nativeProtocol+"capabilities"); err != nil || values[nativeProtocol+"scope/native-inbounds-v2"] != 1 { t.Fatal("capabilities", err) }
	if values, err := p.query(ctx, nativeProtocol+"health"); err != nil || values[nativeProtocol+"admission-open"] != 0 { t.Fatal("pre-activation health", err) }
	if err := p.Activate(ctx, strings.Repeat("a", 64)); err != nil { t.Fatal(err) }
	if err := p.Health(ctx); err != nil { t.Fatal(err) }
	ordinary, err := p.Counters(ctx)
	if err != nil || len(ordinary) != 1 || ordinary[0].Upload != 9 || ordinary[0].Download != 13 { t.Fatalf("ordinary counters: %v %v", ordinary, err) }
	final, err := p.Final(ctx, strings.Repeat("b", 64))
	if err != nil || !reflect.DeepEqual(ordinary, final) { t.Fatal("final counters", err) }
	replay, err := p.Final(ctx, strings.Repeat("b", 64))
	if err != nil || !reflect.DeepEqual(final, replay) { t.Fatal("immutable final replay", err) }
	if err := p.Activate(ctx, strings.Repeat("a", 64)); err == nil { t.Fatal("sealed process reopened") }
	if fixture.listener.accepted.Load() != 1 { t.Fatalf("connection not reused: %d", fixture.listener.accepted.Load()) }
	mu.Lock()
	defer mu.Unlock()
	want := []string{nativeProtocol+"capabilities", nativeProtocol+"health", nativeProtocol+"activate/"+strings.Repeat("a", 64), nativeProtocol+"health", "", nativeProtocol+"final/"+strings.Repeat("b", 64), nativeProtocol+"final/"+strings.Repeat("b", 64), nativeProtocol+"activate/"+strings.Repeat("a", 64)}
	if !reflect.DeepEqual(patterns, want) { t.Fatal("custom protocol was changed or replayed") }
}

func TestNativeStatsCancellationAndReconnectStayWithinBirth(t *testing.T) {
	entered := make(chan struct{})
	var calls atomic.Int64
	fixture := newNativeStatsTestFixture(t, func(ctx context.Context, _ *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) {
		if calls.Add(1) == 1 { close(entered); <-ctx.Done(); return nil, ctx.Err() }
		return nativeStatsTestResponse(map[string]int64{"alive": 1}), nil
	})
	p := fixture.p
	p.deadline = time.Now().Add(25*time.Second)
	deadline := p.deadline
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := p.query(ctx, nativeProtocol+"health"); result <- err }()
	select { case <-entered: case <-time.After(3*time.Second): t.Fatal("request did not reach owned peer") }
	cancel()
	if err := nativeStatsTestWaitError(t, result); !errors.Is(err, ErrProof) { t.Fatal("canceled call was accepted", err) }
	if values, err := p.query(context.Background(), ""); err != nil || values["alive"] != 1 { t.Fatal("cancellation poisoned connection", err) }
	if fixture.listener.accepted.Load() != 1 { t.Fatal("cancellation unnecessarily replaced the connection") }
	fixture.listener.dropConnections()
	if values, err := p.query(context.Background(), ""); err != nil || values["alive"] != 1 { t.Fatal("bounded ready wait did not reconnect", err) }
	if fixture.listener.accepted.Load() != 2 { t.Fatalf("unexpected reconnect count: %d", fixture.listener.accepted.Load()) }
	if p.deadline != deadline || p.ending || p.stats.closed.Load() || p.checkIdentity() != nil { t.Fatal("query cancellation changed birth or lease") }
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if conn, err := p.stats.dial(canceled, "ignored"); err == nil || conn != nil { if conn != nil { _ = conn.Close() }; t.Fatal("canceled dial connected") }
}

func TestNativeStatsAcceptedControlReplyLossRequiresExplicitReplay(t *testing.T) {
	for _, operation := range []string{"activate/", "final/"} {
		t.Run(strings.TrimSuffix(operation, "/"), func(t *testing.T) {
			var mu sync.Mutex
			calls, captures := 0, 0
			var recorded string
			var fixture *nativeStatsTestFixture
			fixtureReady := make(chan struct{})
			fixture = newNativeStatsTestFixture(t, func(_ context.Context, request *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) {
				<-fixtureReady
				mu.Lock()
				defer mu.Unlock()
				calls++
				if recorded == "" { recorded = request.Pattern; captures++ }
				if request.Pattern != recorded { return nil, status.Error(codes.FailedPrecondition, "nonce changed") }
				marker := request.Pattern
				if operation == "activate/" { marker = strings.Replace(marker, "activate/", "activated/", 1) }
				response := nativeStatsTestResponse(map[string]int64{marker: 1})
				if calls == 1 { fixture.listener.dropConnections() }
				return response, nil
			})
			close(fixtureReady)
			pattern := nativeProtocol + operation + strings.Repeat("c", 64)
			if _, err := fixture.p.query(context.Background(), pattern); !errors.Is(err, ErrProof) { t.Fatal("lost reply was hidden by replay", err) }
			mu.Lock()
			firstCalls := calls
			mu.Unlock()
			if firstCalls != 1 { t.Fatalf("accepted operation automatically replayed %d times", firstCalls) }
			response, err := fixture.p.query(context.Background(), pattern)
			marker := pattern
			if operation == "activate/" { marker = strings.Replace(marker, "activate/", "activated/", 1) }
			if err != nil || response[marker] != 1 { t.Fatal("explicit same-nonce recovery failed", err) }
			mu.Lock()
			defer mu.Unlock()
			if calls != 2 || captures != 1 || recorded != pattern { t.Fatal("recovery changed immutable control outcome") }
		})
	}
}

func TestNativeStatsRejectsInvalidCountersAndPostReplyIdentityLoss(t *testing.T) {
	for _, tc := range []struct{name string; stats []*xraystats.Stat}{
		{"negative", []*xraystats.Stat{{Name: "counter", Value: -1}}},
		{"duplicate", []*xraystats.Stat{{Name: "counter", Value: 1}, {Name: "counter", Value: 2}}},
		{"unknown-principal", []*xraystats.Stat{{Name: "user>>>foreign>>>traffic>>>uplink", Value: 1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newNativeStatsTestFixture(t, func(context.Context, *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) { return &xraystats.QueryStatsResponse{Stat: tc.stats}, nil })
			if _, err := fixture.p.Counters(context.Background()); !errors.Is(err, ErrProof) { t.Fatal("invalid counters accepted", err) }
		})
	}
	t.Run("post-reply-identity", func(t *testing.T) {
		var fixture *nativeStatsTestFixture
		fixtureReady := make(chan struct{})
		fixture = newNativeStatsTestFixture(t, func(context.Context, *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) {
			<-fixtureReady
			close(fixture.p.done)
			return nativeStatsTestResponse(map[string]int64{"old-birth": 1}), nil
		})
		close(fixtureReady)
		if _, err := fixture.p.query(context.Background(), ""); !errors.Is(err, ErrProof) { t.Fatal("response outlived owned birth", err) }
	})
}

func TestNativeStatsTerminalCloseCancelsCallAndPreventsReopen(t *testing.T) {
	entered := make(chan struct{})
	fixture := newNativeStatsTestFixture(t, func(ctx context.Context, _ *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	})
	result := make(chan error, 1)
	go func() { _, err := fixture.p.query(context.Background(), ""); result <- err }()
	select { case <-entered: case <-time.After(3*time.Second): t.Fatal("query did not enter peer") }
	var closers sync.WaitGroup
	for i := 0; i < 8; i++ { closers.Add(1); go func() { defer closers.Done(); fixture.p.stats.close() }() }
	joined := make(chan struct{})
	go func() { closers.Wait(); close(joined) }()
	select { case <-joined: case <-time.After(2*time.Second): t.Fatal("terminal close blocked") }
	if err := nativeStatsTestWaitError(t, result); !errors.Is(err, ErrProof) { t.Fatal("closed query accepted", err) }
	if !fixture.p.stats.closed.Load() || fixture.p.stats.conn.GetState() != connectivity.Shutdown { t.Fatal("close not terminal") }
	if _, err := fixture.p.query(context.Background(), ""); !errors.Is(err, ErrProof) { t.Fatal("closed birth reopened", err) }
	if conn, err := fixture.p.stats.dial(context.Background(), "ignored"); !errors.Is(err, ErrIdentity) || conn != nil { if conn != nil { _ = conn.Close() }; t.Fatal("closed birth redialed", err) }
	if fixture.listener.accepted.Load() != 1 { t.Fatal("terminal close adopted a new connection") }
}

// This entrypoint is invoked only as an owned synthetic child. Its socket and
// stdin/stdout are test-owned; it receives no application configuration or data.
func TestNativeStatsProcessHelper(t *testing.T) {
	mode := os.Getenv("DXP_STATS_PROCESS_HELPER")
	if mode == "" { return }
	if mode != "foreign" && mode != "blocking" { t.Fatal("unknown helper mode") }
	socket := os.Getenv("DXP_STATS_TEST_SOCKET")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: socket, Net: "unix"})
	if err != nil { t.Fatal(err) }
	defer listener.Close()
	if err := os.Chmod(socket, 0600); err != nil { t.Fatal(err) }
	var calls atomic.Int64
	server := grpc.NewServer()
	xraystats.RegisterStatsServiceServer(server, &nativeStatsTestServer{query: func(ctx context.Context, _ *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) {
		calls.Add(1)
		if mode == "blocking" { _, _ = io.WriteString(os.Stdout, "called\n"); <-ctx.Done(); return nil, ctx.Err() }
		return nativeStatsTestResponse(map[string]int64{"foreign": 1}), nil
	}})
	defer server.Stop()
	go func() { _ = server.Serve(listener) }()
	_, _ = io.WriteString(os.Stdout, "ready\n")
	_, _ = io.Copy(io.Discard, os.Stdin)
	if mode == "foreign" && calls.Load() != 0 { t.Fatal("foreign peer received an application query") }
}

func nativeStatsTestLine(t *testing.T, reader *bufio.Reader, want string) {
	t.Helper()
	result := make(chan string, 1)
	go func() { line, _ := reader.ReadString('\n'); result <- line }()
	select { case line := <-result: if line != want+"\n" { t.Fatalf("helper status mismatch: %q", line) }; case <-time.After(3*time.Second): t.Fatal("helper status timed out") }
}

func TestNativeStatsRejectsForeignSameUIDSocketReplacement(t *testing.T) {
	fixture := newNativeStatsTestFixture(t, func(context.Context, *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) { return nativeStatsTestResponse(map[string]int64{"owned": 1}), nil })
	if _, err := fixture.p.query(context.Background(), ""); err != nil { t.Fatal(err) }
	// The old listener remains alive and owned in /proc; the same pathname now
	// leads to a different same-UID process. Peer credentials must reject it.
	if err := os.Rename(fixture.p.apiSocket, fixture.p.apiSocket+".old"); err != nil { t.Fatal(err) }
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	t.Cleanup(cancel)
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeStatsProcessHelper$")
	command.Env = []string{"DXP_STATS_PROCESS_HELPER=foreign", "DXP_STATS_TEST_SOCKET="+fixture.p.apiSocket}
	input, err := command.StdinPipe()
	if err != nil { t.Fatal(err) }
	output, err := command.StdoutPipe()
	if err != nil { t.Fatal(err) }
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil { t.Fatal(err) }
	t.Cleanup(func() { _ = input.Close(); if err := command.Wait(); err != nil { t.Errorf("foreign helper failed: %v %s", err, stderr.String()) } })
	nativeStatsTestLine(t, bufio.NewReader(output), "ready")
	if !fixture.p.ownsUnixControl() { t.Fatal("fixture did not exercise the peer-credential boundary") }
	conn, err := fixture.p.stats.dial(context.Background(), "ignored")
	if conn != nil { _ = conn.Close() }
	if !errors.Is(err, ErrIdentity) || conn != nil { t.Fatal("same-UID foreign process adopted", err) }
	fixture.listener.dropConnections()
	call, stop := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer stop()
	if _, err := fixture.p.query(call, ""); !errors.Is(err, ErrProof) { t.Fatal("replacement path reached foreign server", err) }
	if fixture.p.checkIdentity() != nil { t.Fatal("foreign pathname changed original birth identity") }
}

func TestNativeStatsStopAndLeaseExpiryKillOwnedChildDuringRPC(t *testing.T) {
	for _, action := range []string{"stop", "lease-expiry"} {
		t.Run(action, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "dxp-stats-child-")
			if err != nil { t.Fatal(err) }
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			if err := os.Chmod(dir, 0700); err != nil { t.Fatal(err) }
			socket := filepath.Join(dir, "stats.sock")
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestNativeStatsProcessHelper$")
			command.Env = []string{"DXP_STATS_PROCESS_HELPER=blocking", "DXP_STATS_TEST_SOCKET="+socket}
			input, err := command.StdinPipe()
			if err != nil { t.Fatal(err) }
			defer input.Close()
			output, err := command.StdoutPipe()
			if err != nil { t.Fatal(err) }
			command.Stderr = io.Discard
			if err := command.Start(); err != nil { t.Fatal(err) }
			p := &Native{cmd: command, done: make(chan struct{}), dir: dir, apiSocket: socket}
			go func() { _ = command.Wait(); close(p.done) }()
			t.Cleanup(p.Stop)
			reader := bufio.NewReader(output)
			nativeStatsTestLine(t, reader, "ready")
			p.identity, err = usagecheckpoint.Process(command.Process.Pid)
			if err != nil { t.Fatal(err) }
			p.apiDirectory, err = os.Open(dir)
			if err != nil { t.Fatal(err) }
			p.stats, err = newNativeStatsClient(p)
			if err != nil { t.Fatal(err) }
			result := make(chan error, 1)
			go func() { _, err := p.query(context.Background(), ""); result <- err }()
			nativeStatsTestLine(t, reader, "called")
			if action == "stop" {
				stopped := make(chan struct{})
				go func() { p.Stop(); close(stopped) }()
				select { case <-stopped: case <-time.After(2*time.Second): t.Fatal("Stop waited on the RPC") }
			} else {
				if !p.Renew(time.Now().Add(50*time.Millisecond)) { t.Fatal("lease was not armed") }
				select { case <-p.done: case <-time.After(2*time.Second): t.Fatal("hard expiry waited on the RPC") }
			}
			if err := nativeStatsTestWaitError(t, result); !errors.Is(err, ErrProof) { t.Fatal("reply survived terminal stop", err) }
			p.mu.Lock()
			ending := p.ending
			p.mu.Unlock()
			if !ending || !p.stats.closed.Load() { t.Fatal("owned birth was not terminal") }
			if err := p.checkIdentity(); !errors.Is(err, ErrIdentity) { t.Fatal("owned child remains live") }
			if conn, err := p.stats.dial(context.Background(), "ignored"); !errors.Is(err, ErrIdentity) || conn != nil { if conn != nil { _ = conn.Close() }; t.Fatal("terminal child redialed", err) }
		})
	}
}

func TestNativeStatsPeerChecksPIDAndUID(t *testing.T) {
	fixture := newNativeStatsTestFixture(t, func(context.Context, *xraystats.QueryStatsRequest) (*xraystats.QueryStatsResponse, error) { return &xraystats.QueryStatsResponse{}, nil })
	connection, err := fixture.p.stats.dial(context.Background(), "ignored")
	if err != nil { t.Fatal(err) }
	defer connection.Close()
	conn, ok := connection.(*net.UnixConn)
	if !ok { t.Fatal(fmt.Sprintf("unexpected synthetic connection kind %T", connection)) }
	if !nativeStatsPeer(conn, os.Getpid(), uint32(os.Geteuid())) || nativeStatsPeer(conn, os.Getpid()+1, uint32(os.Geteuid())) || nativeStatsPeer(conn, os.Getpid(), uint32(os.Geteuid())+1) { t.Fatal("peer PID/UID identity not enforced") }
}
