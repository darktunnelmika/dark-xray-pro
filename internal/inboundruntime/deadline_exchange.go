package inboundruntime

import (
	"context"
	"encoding/json"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// WithDeadlines isolates the local runtime from store calls whose filesystem
// syscalls cannot be canceled. At most one store call remains in flight.
// A timeout is never a receipt: the worker retains and replays its journal.
func WithDeadlines(next Exchange) Exchange {
	return &deadlineExchange{next: next, gate: make(chan struct{}, 1)}
}

type deadlineExchange struct {
	next Exchange
	gate chan struct{}
}
type exchangeResult[T any] struct {
	value T
	err   error
}

func deadlineCall[I, O any](ctx context.Context, gate chan struct{}, input I, call func(context.Context, I) (O, error)) (O, error) {
	var zero O
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	// Store calls can outlive their caller. Own maps, slices and nested pointers
	// before the worker proceeds to final capture or another journal transition.
	raw, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}
	var owned I
	if err = json.Unmarshal(raw, &owned); err != nil {
		return zero, err
	}
	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	if err = ctx.Err(); err != nil {
		<-gate
		return zero, err
	}
	done := make(chan exchangeResult[O], 1)
	go func() {
		defer func() { <-gate }()
		value, err := call(ctx, owned)
		done <- exchangeResult[O]{value, err}
	}()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case result := <-done:
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return result.value, result.err
	}
}
func (x *deadlineExchange) Poll(ctx context.Context, in PollInput) (*Delivery, error) {
	return deadlineCall(ctx, x.gate, in, x.next.Poll)
}
func (x *deadlineExchange) Register(ctx context.Context, in ix.EpochInput) (EpochReceipt, error) {
	return deadlineCall(ctx, x.gate, in, x.next.Register)
}
func (x *deadlineExchange) Usage(ctx context.Context, in ix.UsageReport) (ix.UsageReceipt, error) {
	return deadlineCall(ctx, x.gate, in, x.next.Usage)
}
func (x *deadlineExchange) Observe(ctx context.Context, in ix.Observation) error {
	_, err := deadlineCall(ctx, x.gate, in, func(ctx context.Context, owned ix.Observation) (struct{}, error) {
		return struct{}{}, x.next.Observe(ctx, owned)
	})
	return err
}
