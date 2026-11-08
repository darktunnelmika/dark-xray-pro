package inboundruntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestDeadlineExchangeBlockedStoreReturnsAndBoundsInFlight(t *testing.T) {
	gate := make(chan struct{}, 1)
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	input := map[string][]int{"counters": {7}}
	go func() {
		_, err := deadlineCall(ctx, gate, input, func(_ context.Context, owned map[string][]int) (int, error) {
			close(entered)
			<-release // Model fsync ignoring context cancellation.
			defer close(finished)
			return owned["counters"][0], nil
		})
		result <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("store blocked local lease drain")
	}
	input["counters"][0] = 99
	retry, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	_, err := deadlineCall(retry, gate, input, func(context.Context, map[string][]int) (int, error) {
		t.Error("second store call entered while first was blocked")
		return 0, nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	close(release)
	<-finished
	// The late result is discarded; it cannot acknowledge the next request.
	value, err := deadlineCall(context.Background(), gate, 42, func(_ context.Context, v int) (int, error) { return v, nil })
	if err != nil || value != 42 {
		t.Fatal(value, err)
	}
}
func TestDeadlineExchangeOwnsPayloadAndPreservesErrors(t *testing.T) {
	gate := make(chan struct{}, 1)
	input := map[string][]int{"counter": {8}}
	sentinel := errors.New("receipt rejected")
	value, err := deadlineCall(context.Background(), gate, input, func(_ context.Context, owned map[string][]int) (int, error) {
		owned["counter"][0] = 9
		return 3, sentinel
	})
	if value != 3 || !errors.Is(err, sentinel) || input["counter"][0] != 8 {
		t.Fatal(value, err, input)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = deadlineCall(ctx, gate, 1, func(context.Context, int) (int, error) { t.Fatal("canceled call ran"); return 0, nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
