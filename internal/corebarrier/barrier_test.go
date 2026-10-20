package corebarrier

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Models task.Run returning before a blocked splice finishes: the final byte
// count is added AFTER cancellation and the root returns. It must be included.
func TestDrainWaitsForLateCounterAndNestedTask(t *testing.T) {
	g := newGate()
	interrupted := make(chan struct{})
	ctx, rootDone, ok := g.enter(context.Background(), func() { close(interrupted) })
	if !ok {
		t.Fatal("enter")
	}
	childDone := Track(ctx)
	var count atomic.Int64
	result := make(chan map[string]int64, 1)
	nonce := strings.Repeat("a", 64)
	go func() {
		v, e := g.snapshot(context.Background(), nonce, func() map[string]int64 { return map[string]int64{"bytes": count.Load()} })
		if e != nil {
			result <- nil
		} else {
			result <- v
		}
	}()
	<-interrupted
	rootDone()
	if _, _, ok = g.enter(context.Background(), func() {}); ok {
		t.Fatal("draining gate admitted traffic")
	}
	lateDone := Track(ctx)
	childDone()
	select {
	case <-result:
		t.Fatal("receipt preceded final counter")
	default:
	}
	count.Add(12345)
	lateDone()
	if got := <-result; got["bytes"] != 12345 {
		t.Fatalf("lost final bytes: %v", got)
	}
	count.Add(1)
	replay, err := g.snapshot(context.Background(), nonce, func() map[string]int64 { t.Fatal("second snapshot"); return nil })
	if err != nil || replay["bytes"] != 12345 {
		t.Fatalf("immutable replay: %v %v", replay, err)
	}
	replay["bytes"] = 0
	again, _ := g.snapshot(context.Background(), nonce, nil)
	if again["bytes"] != 12345 {
		t.Fatal("caller mutated proof")
	}
	if _, err = g.snapshot(context.Background(), strings.Repeat("b", 64), nil); !errors.Is(err, ErrBarrier) {
		t.Fatal("foreign nonce accepted")
	}
}

func TestDrainTimeoutNeverInventsCompletion(t *testing.T) {
	g := newGate()
	stopped := make(chan struct{})
	_, done, _ := g.enter(context.Background(), func() { close(stopped) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	var reads atomic.Int64
	nonce := strings.Repeat("c", 64)
	_, err := g.snapshot(ctx, nonce, func() map[string]int64 { reads.Add(1); return map[string]int64{"bytes": 7} })
	if !errors.Is(err, context.DeadlineExceeded) || reads.Load() != 0 {
		t.Fatalf("false receipt: %v %d", err, reads.Load())
	}
	<-stopped
	done()
	v, err := g.snapshot(context.Background(), nonce, nil)
	if err != nil || v["bytes"] != 7 || reads.Load() != 1 {
		t.Fatalf("lost drain continuation: %v %v", v, err)
	}
}

func TestInvalidNonceCannotCloseGate(t *testing.T) {
	g := newGate()
	for _, n := range []string{"", strings.Repeat("a", 63), strings.Repeat("A", 64), strings.Repeat("g", 64)} {
		if _, e := g.snapshot(context.Background(), n, nil); e == nil {
			t.Fatal("invalid nonce")
		}
	}
	_, done, ok := g.enter(context.Background(), func() {})
	if !ok {
		t.Fatal("invalid request closed gate")
	}
	done()
}
