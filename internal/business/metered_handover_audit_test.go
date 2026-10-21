package business

import (
	"context"
	"testing"
)

func TestMeteredHandoverAuditPaginationAndSafeCancel(t *testing.T) {
	f := newHandoverFixture(t)
	ctx := context.Background()
	for i := 0; i < 55; i++ {
		if i%10 == 0 {
			meteredQueuePoll(t, f.s, f.key, &f.source, false)
			meteredQueuePoll(t, f.s, f.key, &f.target, false)
		}
		f.in.ID = routeTestID(100 + i)
		f.create(t)
		if _, err := f.s.CancelMeteredHandover(ctx, f.in.ID); err != nil {
			t.Fatal(err)
		}
	}
	first, err := f.s.ListMeteredHandovers(ctx, "")
	if err != nil || len(first.Operations) != 50 || first.NextCursor != first.Operations[49].ID {
		t.Fatal("first page", len(first.Operations), err)
	}
	second, err := f.s.ListMeteredHandovers(ctx, first.NextCursor)
	if err != nil || len(second.Operations) != 5 || second.NextCursor != "" {
		t.Fatal("second page", len(second.Operations), err)
	}
	seen := map[string]bool{}
	for _, page := range []MeteredHandoverPage{first, second} {
		for _, op := range page.Operations {
			if seen[op.ID] || !op.Cancelled || op.CanPublish {
				t.Fatal("bad history", op.ID)
			}
			seen[op.ID] = true
		}
	}
	audit, err := f.s.MeteredHandoverAudit(ctx, f.key, first.Operations[0].ID)
	if err != nil || audit.DrainCommand != nil || audit.ApplyCommand != nil || !audit.Cancelled {
		t.Fatal("cancel audit", err)
	}
	if _, err = f.s.ListMeteredHandovers(ctx, "bad-cursor"); err == nil {
		t.Fatal("malformed cursor")
	}
	if _, err = f.s.ListMeteredHandovers(ctx, routeTestID(999)); err == nil {
		t.Fatal("unknown cursor")
	}
}
