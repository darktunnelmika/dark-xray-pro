package business

import (
	"context"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"testing"
	"time"
)

func TestManagedCompletedOperationRetainsStateOnRepeatedObservation(t *testing.T) {
	for _, action := range []string{"stop", "archive"} {
		t.Run(action, func(t *testing.T) {
			f := newExecutionFixture(t)
			defer f.s.Close()
			ctx := context.Background()
			f.command(t, "repeat-observe-"+action+"-0001", action)
			b := f.bundle(t)
			o := executionObservation(b, ix.EpochInput{})
			o.State = "stopped"
			o.ProcessReady = false
			o.LeaseExpires = 0
			if err := f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
				t.Fatal(err)
			}
			first, err := f.s.InboundExecution(ctx, f.key, 501)
			if err != nil {
				t.Fatal(err)
			}
			o.ObservedUnixNano = time.Now().UnixNano()
			if err = f.s.ObserveInboundExecution(ctx, f.key, o); err != nil {
				t.Fatal(err)
			}
			second, err := f.s.InboundExecution(ctx, f.key, 501)
			if err != nil {
				t.Fatal(err)
			}
			if first.LatestOperation == nil || second.LatestOperation == nil {
				t.Fatal("missing operation")
			}
			if second.LatestOperation.UpdatedUnixNano != first.LatestOperation.UpdatedUnixNano {
				t.Fatal("unchanged operation was rewritten")
			}
			if second.LatestOperation.State != first.LatestOperation.State {
				t.Fatalf("terminal state changed: %s -> %s", first.LatestOperation.State, second.LatestOperation.State)
			}
		})
	}
}
