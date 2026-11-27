package hubcore

import (
	"context"
	"testing"
)

func TestCustomerPause(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	seed(t, s)
	on := func() bool { c, _ := s.ClientByEmail(ctx, "alice"); return c.Enabled }
	if err := s.CustomerSetOn(ctx, "alice", false); err != nil || on() || !s.CustomerPaused(ctx, "alice") {
		t.Fatalf("pause: %v", err)
	}
	if err := s.CustomerSetOn(ctx, "alice", true); err != nil || !on() || s.CustomerPaused(ctx, "alice") {
		t.Fatalf("resume: %v", err)
	}
	// Turned off by the owner: the customer cannot turn it back on.
	off := false
	if _, err := s.BulkExtend(ctx, BulkRequest{Emails: []string{"alice"}, Enable: &off}); err != nil {
		t.Fatal(err)
	}
	if err := s.CustomerSetOn(ctx, "alice", true); err == nil || on() {
		t.Fatal("resumed a service support turned off")
	}
}
