package api

import (
	"context"
	"testing"
	"time"
)

func TestVolumeRefundRetryDelay(t *testing.T) {
	const interval = 5 * time.Second
	for _, tc := range []struct {
		name     string
		failures int
		want     time.Duration
	}{
		{name: "steady cadence", failures: 0, want: interval},
		{name: "first failure", failures: 1, want: 2 * interval},
		{name: "second failure", failures: 2, want: 4 * interval},
		{name: "third failure", failures: 3, want: 8 * interval},
		{name: "fourth failure caps", failures: 4, want: 12 * interval},
		{name: "later failures stay capped", failures: 20, want: 12 * interval},
		{name: "reset after recovery", failures: 0, want: interval},
		{name: "defensive negative count", failures: -1, want: interval},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := volumeRefundRetryDelay(interval, tc.failures); got != tc.want {
				t.Fatalf("delay = %s; want %s", got, tc.want)
			}
		})
	}
}

func TestVolumeRefundRetryDelayBounds(t *testing.T) {
	if got := volumeRefundRetryDelay(0, 3); got != 0 {
		t.Fatalf("zero interval delay = %s; want 0", got)
	}
	if got := volumeRefundRetryDelay(-time.Second, 3); got != 0 {
		t.Fatalf("negative interval delay = %s; want 0", got)
	}
	const maxDuration = time.Duration(1<<63 - 1)
	if got := volumeRefundRetryDelay(maxDuration/2+1, 1); got != maxDuration {
		t.Fatalf("overflowing delay = %s; want %s", got, maxDuration)
	}
}

func TestWaitVolumeRefundRetryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if waitVolumeRefundRetry(ctx, time.Hour) {
		t.Fatal("cancelled retry wait reported completion")
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("cancelled retry wait took %s", elapsed)
	}
}
