//go:build linux

package inboundruntime

import (
	"context"
	"testing"
	"time"
)

type boundedRenewalProcess struct {
	*fixtureProcess
	deadline time.Time
}

func (p *boundedRenewalProcess) Renew(deadline time.Time) bool {
	p.deadline = deadline
	d := time.Until(deadline)
	if d <= 0 || d > 31*time.Second {
		return false
	}
	p.renewals++
	return true
}
func TestVerifiedLeaseDeadlineNeverExtendsAuthority(t *testing.T) {
	now := time.Unix(1800000000, 0)
	for _, offset := range []time.Duration{-25 * time.Second, -time.Second, 0, time.Second, 1500 * time.Millisecond, 2 * time.Second} {
		for _, ttl := range []time.Duration{10 * time.Second, 30 * time.Second} {
			l := Lease{Issued: now.Add(offset).UnixNano(), Expires: now.Add(offset + ttl).UnixNano()}
			d := verifiedLeaseDeadline(l, now)
			if d.After(time.Unix(0, l.Expires)) || d.After(now.Add(ttl)) {
				t.Fatalf("extended authority offset=%v ttl=%v", offset, ttl)
			}
			if offset <= 0 && !d.Equal(time.Unix(0, l.Expires)) {
				t.Fatal("delayed/expired authority was renewed")
			}
			if offset > 0 && !d.Equal(now.Add(ttl)) {
				t.Fatal("allowed skew not bounded")
			}
		}
	}
}
func TestWorkerVerifiedClockSkewAdmitsAndRenewsWithinNativeBound(t *testing.T) {
	for _, phase := range []string{"prepared", "active"} {
		t.Run(phase, func(t *testing.T) {
			w, p, x := fixtureWorker(t, phase)
			bounded := &boundedRenewalProcess{fixtureProcess: p}
			w.process = bounded
			l := x.delivery.Lease
			now := time.Now()
			l.Issued = now.Add(1500 * time.Millisecond).UnixNano()
			l.Expires = now.Add(31500 * time.Millisecond).UnixNano()
			var err error
			x.delivery.Lease, err = SignLease(w.cfg.Key, l)
			if err != nil {
				t.Fatal(err)
			}
			if err = VerifyDelivery(w.cfg.Key, *x.delivery, w.cfg.NodeID, w.cfg.InstallationID, now); err != nil {
				t.Fatal(err)
			}
			if err = w.Step(context.Background(), x); err != nil {
				t.Fatal("valid signed lease rejected by native duration bound", err)
			}
			if w.state.Phase != "active" || p.renewals != 1 || p.finals != 0 || p.stops != 0 || bounded.deadline.After(time.Now().Add(30*time.Second)) {
				t.Fatal("clock skew drained or extended runtime", w.state.Phase, bounded.deadline)
			}
			// The change must not accept larger skew or bypass signature/expiry checks.
			l.Issued = time.Now().Add(3 * time.Second).UnixNano()
			l.Expires = l.Issued + int64(30*time.Second)
			x.delivery.Lease, err = SignLease(w.cfg.Key, l)
			if err != nil {
				t.Fatal(err)
			}
			if err = VerifyDelivery(w.cfg.Key, *x.delivery, w.cfg.NodeID, w.cfg.InstallationID, time.Now()); err == nil {
				t.Fatal("expanded allowed clock skew")
			}
		})
	}
}
