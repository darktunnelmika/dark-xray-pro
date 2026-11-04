//go:build linux

package telegrambot

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type managerPollTracker struct {
	mu         sync.Mutex
	active     map[int64]int
	maximum    map[int64]int
	polls      map[int64]int
	lastToken  map[int64]string
	verifyGate <-chan struct{}
	entered    chan struct{}
	once       sync.Once
}

func newManagerPollTracker() *managerPollTracker {
	return &managerPollTracker{active: map[int64]int{}, maximum: map[int64]int{}, polls: map[int64]int{}, lastToken: map[int64]string{}}
}

func (p *managerPollTracker) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Host != "api.telegram.org" || r.URL.Scheme != "https" {
		return nil, ErrProtocol
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/bot"), "/")
	if len(parts) != 2 {
		return nil, ErrProtocol
	}
	id, err := strconv.ParseInt(strings.SplitN(parts[0], ":", 2)[0], 10, 64)
	if err != nil {
		return nil, ErrProtocol
	}
	switch parts[1] {
	case "getMe":
		p.mu.Lock()
		gate, entered := p.verifyGate, p.entered
		p.mu.Unlock()
		if gate != nil {
			p.once.Do(func() { close(entered) })
			select {
			case <-r.Context().Done():
				return nil, r.Context().Err()
			case <-gate:
			}
		}
		return fakeResponse(200, fmt.Sprintf(`{"ok":true,"result":{"id":%d,"is_bot":true,"first_name":"Test","username":"unit_bot_%d"}}`, id, id)), nil
	case "getWebhookInfo":
		return fakeResponse(200, `{"ok":true,"result":{"url":"","pending_update_count":0}}`), nil
	case "getUpdates":
		p.mu.Lock()
		p.active[id]++
		p.maximum[id] = max(p.maximum[id], p.active[id])
		p.polls[id]++
		p.lastToken[id] = parts[0]
		p.mu.Unlock()
		defer func() { p.mu.Lock(); p.active[id]--; p.mu.Unlock() }()
		<-r.Context().Done()
		return nil, r.Context().Err()
	default:
		return nil, ErrProtocol
	}
}

func TestManagerIndependentLoopsReplacementAndSavedRetry(t *testing.T) {
	ctx := context.Background()
	tracker := newManagerPollTracker()
	m, _, _ := newTestManager(t, &fakeBackend{}, tracker)
	if _, err := m.Configure(ctx, 100, managerTestToken(100, ""), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Configure(ctx, 200, managerTestToken(200, ""), 2); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	waitFor(t, func() bool {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		return tracker.active[100] == 1 && tracker.active[200] == 1
	})
	guardCalled := false
	if _, err := m.ConfigureAuthorized(ctx, 100, managerTestToken(100, "denied"), 3, func(context.Context) error {
		guardCalled = true
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		if tracker.active[100] != 0 || tracker.active[200] != 1 {
			t.Error("credential publication was not fenced after only its own loop joined")
		}
		return ErrAccess
	}); err != ErrAccess || !guardCalled || m.Management().Revision != 3 {
		t.Fatal("authorization loss changed durable configuration", err)
	}
	waitFor(t, func() bool {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		return tracker.active[100] == 1 && tracker.polls[100] == 2
	})
	if _, err := m.Configure(ctx, 100, managerTestToken(100, "replacement"), 3); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		return tracker.active[100] == 1 && tracker.polls[100] == 3
	})
	if _, err := m.Configure(ctx, 100, "", 4); err != nil {
		t.Fatal("saved-token activation retry failed", err)
	}
	waitFor(t, func() bool {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		return tracker.active[100] == 1 && tracker.polls[100] == 4
	})
	tracker.mu.Lock()
	if tracker.maximum[100] != 1 || tracker.maximum[200] != 1 || tracker.polls[200] != 1 || tracker.lastToken[100] != managerTestToken(100, "replacement") {
		t.Error("duplicate poller or independent bot restart")
	}
	tracker.mu.Unlock()
	var closes sync.WaitGroup
	for i := 0; i < 4; i++ {
		closes.Add(1)
		go func() {
			defer closes.Done()
			if err := m.Close(); err != nil {
				t.Error(err)
			}
		}()
	}
	closes.Wait()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	if tracker.active[100] != 0 || tracker.active[200] != 0 {
		t.Fatal("close returned before pollers joined")
	}
}

func TestManagerRunCannotRacePrestartCredentialPublication(t *testing.T) {
	tracker := newManagerPollTracker()
	m, _, _ := newTestManager(t, &fakeBackend{}, tracker)
	if _, err := m.Configure(context.Background(), 100, managerTestToken(100, "old"), 1); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	entered := make(chan struct{})
	tracker.mu.Lock()
	tracker.verifyGate, tracker.entered = gate, entered
	tracker.mu.Unlock()
	configured := make(chan error, 1)
	go func() {
		_, err := m.Configure(context.Background(), 100, managerTestToken(100, "new"), 2)
		configured <- err
	}()
	<-entered
	runDone := make(chan error, 1)
	go func() { runDone <- m.Run(context.Background()) }()
	close(gate)
	if err := <-configured; err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		tracker.mu.Lock()
		defer tracker.mu.Unlock()
		return tracker.active[100] == 1
	})
	tracker.mu.Lock()
	if tracker.maximum[100] != 1 || tracker.polls[100] != 1 || tracker.lastToken[100] != managerTestToken(100, "new") {
		t.Error("startup polled with a replaced credential")
	}
	tracker.mu.Unlock()
	m.Close()
	<-runDone
}

func TestManagerQueuedVerificationObservesCancellationAndClose(t *testing.T) {
	tracker := newManagerPollTracker()
	gate, entered := make(chan struct{}), make(chan struct{})
	tracker.verifyGate, tracker.entered = gate, entered
	m, _, _ := newTestManager(t, &fakeBackend{}, tracker)
	first := make(chan error, 1)
	go func() {
		_, err := m.Verify(context.Background(), managerTestToken(100, ""))
		first <- err
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	queued := make(chan error, 1)
	go func() {
		_, err := m.Verify(ctx, managerTestToken(200, ""))
		queued <- err
	}()
	cancel()
	select {
	case err := <-queued:
		if err != ErrState {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("queued verification ignored request cancellation")
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel in-flight verification")
	}
	if err := <-first; err == nil || strings.Contains(err.Error(), managerTestToken(100, "")) {
		t.Fatal("verification shutdown was not safely rejected", err)
	}
	if _, err := m.Verify(context.Background(), managerTestToken(100, "")); err != ErrState {
		t.Fatal("closed manager accepted verification", err)
	}
}

type managerBlockedCapture struct {
	fakeBackend
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (b *managerBlockedCapture) CaptureFinancialEvent(context.Context, Incoming) error {
	b.once.Do(func() { close(b.entered) })
	<-b.release
	return nil
}

func TestManagerRetainsJournalLockUntilFinancialWriterJoins(t *testing.T) {
	backend := &managerBlockedCapture{entered: make(chan struct{}), release: make(chan struct{})}
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(backend.release) })
	tracker := newManagerPollTracker()
	m, _, journalPath := newTestManager(t, backend, tracker)
	ctx := context.Background()
	if _, err := m.Configure(ctx, 100, managerTestToken(100, ""), 1); err != nil {
		t.Fatal(err)
	}
	persistInput(t, m.worker.journal, financialRetryInput("payment"))
	if err := m.worker.journal.recoverManagedBot(ctx, 100); err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- m.Run(ctx) }()
	select {
	case <-backend.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("financial capture did not begin")
	}
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	waitFor(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.closed })
	lock, _, err := openPrivate(journalPath+".lock", false)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
		syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		t.Error("journal fence released while financial writer was alive")
	}
	select {
	case err := <-closed:
		t.Error("manager closed before financial writer joined", err)
	default:
	}
	releaseOnce.Do(func() { close(backend.release) })
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	<-runDone
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal("journal fence remained after joined shutdown", err)
	}
	syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
}
