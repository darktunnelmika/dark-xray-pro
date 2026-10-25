package corebarrierv2

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func activatedGate(t *testing.T) *gate {
	t.Helper()
	g := newGate()
	g.ready = true
	if err := g.activate(strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestStartsClosedAndRequiresReadyNonce(t *testing.T) {
	g := newGate()
	if _, _, ok := g.enter(context.Background(), nil); ok {
		t.Fatal("initial admission is open")
	}
	if g.activate(strings.Repeat("a", 64)) == nil {
		t.Fatal("activated before engine ready")
	}
	g.ready = true
	for _, n := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		if g.activate(n) == nil {
			t.Fatal("invalid nonce admitted")
		}
	}
	if err := g.activate(strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if err := g.activate(strings.Repeat("a", 64)); err != nil {
		t.Fatal("activation replay", err)
	}
	if g.activate(strings.Repeat("b", 64)) == nil {
		t.Fatal("foreign activation accepted")
	}
}

func TestFinalJoinsLateUDPAndRetainsParentInterrupter(t *testing.T) {
	g := activatedGate(t)
	interrupted := make(chan struct{})
	ctx, done, ok := g.enter(context.Background(), func() { close(interrupted) })
	if !ok {
		t.Fatal("enter")
	}
	child := Track(ctx)
	done() // Detached UDP task outlives its original inbound callback.
	var bytes atomic.Int64
	result := make(chan map[string]int64, 1)
	nonce := strings.Repeat("b", 64)
	go func() {
		values, err := g.snapshot(context.Background(), nonce, func() map[string]int64 {
			return map[string]int64{"user>>>u>>>traffic>>>uplink": bytes.Load(), "inbound>>>i>>>traffic>>>downlink": 7}
		})
		if err != nil {
			result <- nil
			return
		}
		result <- values
	}()
	<-interrupted
	late := Track(ctx)
	child()
	select {
	case <-result:
		t.Fatal("proof preceded late counter")
	default:
	}
	bytes.Add(1234)
	late()
	got := <-result
	if got["user>>>u>>>traffic>>>uplink"] != 1234 || got["inbound>>>i>>>traffic>>>downlink"] != 7 {
		t.Fatal(got)
	}
	if _, _, ok := g.enter(context.Background(), nil); ok {
		t.Fatal("final gate reopened")
	}
	if g.activate(strings.Repeat("a", 64)) == nil {
		t.Fatal("activation after final")
	}
	bytes.Add(1)
	replay, err := g.snapshot(context.Background(), nonce, func() map[string]int64 { t.Error("second read"); return nil })
	if err != nil || replay["user>>>u>>>traffic>>>uplink"] != 1234 {
		t.Fatal(replay, err)
	}
	replay["user>>>u>>>traffic>>>uplink"] = 0
	again, _ := g.snapshot(context.Background(), nonce, nil)
	if again["user>>>u>>>traffic>>>uplink"] != 1234 {
		t.Fatal("mutable proof")
	}
	if _, err := g.snapshot(context.Background(), strings.Repeat("c", 64), nil); !errors.Is(err, ErrBarrier) {
		t.Fatal("foreign final nonce")
	}
}

func TestTimedOutFinalStaysFencedAndCompletesSameNonce(t *testing.T) {
	g := activatedGate(t)
	closed := make(chan struct{})
	_, finish, _ := g.enter(context.Background(), func() { close(closed) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var reads atomic.Int64
	nonce := strings.Repeat("c", 64)
	_, err := g.snapshot(ctx, nonce, func() map[string]int64 { reads.Add(1); return map[string]int64{} })
	if !errors.Is(err, context.DeadlineExceeded) || reads.Load() != 0 {
		t.Fatal(err, reads.Load())
	}
	<-closed
	finish()
	if _, err := g.snapshot(context.Background(), nonce, nil); err != nil || reads.Load() != 1 {
		t.Fatal(err, reads.Load())
	}
}

func TestFinalWaitsResourcesAndFailsOnUnclosedResource(t *testing.T) {
	for _, failure := range []bool{false, true} {
		g := activatedGate(t)
		closeResource := make(chan struct{})
		g.resources["tun"] = func() error {
			<-closeResource
			if failure {
				return errors.New("close failed")
			}
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		_, err := g.snapshot(ctx, strings.Repeat("d", 64), func() map[string]int64 { return map[string]int64{} })
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal(err)
		}
		close(closeResource)
		_, err = g.snapshot(context.Background(), strings.Repeat("d", 64), nil)
		if failure != (err != nil) {
			t.Fatal("resource close result ignored", err)
		}
	}
}

func TestConcurrentFinalReadsExactlyOnce(t *testing.T) {
	g := activatedGate(t)
	var reads atomic.Int64
	var callers sync.WaitGroup
	for i := 0; i < 32; i++ {
		callers.Add(1)
		go func() {
			defer callers.Done()
			values, err := g.snapshot(context.Background(), strings.Repeat("e", 64), func() map[string]int64 { reads.Add(1); return map[string]int64{"bytes": 99} })
			if err != nil || values["bytes"] != 99 {
				t.Error(values, err)
			}
		}()
	}
	callers.Wait()
	if reads.Load() != 1 {
		t.Fatal(reads.Load())
	}
}

func TestPreserveDetachedOwnershipAndRejectEscapedTask(t *testing.T) {
	g := activatedGate(t)
	ctx, done, _ := g.enter(context.Background(), nil)
	detached := Preserve(ctx, context.Background())
	child := Track(detached)
	done()
	child()
	defer func() {
		if recover() == nil {
			t.Error("escaped lifetime silently untracked")
		}
	}()
	Track(detached)
}
