package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func refundStore(t *testing.T) (*Store, string) {
	t.Helper()
	s, path := opened(t)
	ctx := context.Background()
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	newRep(t, s, "refund-rep")
	grant(t, s, "refund-rep", "refund-allocation-0001", 10000, 1000)
	if err := s.EnableVolumeExpiryRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func refundClient(t *testing.T, s *Store, id string, volume int64) Client {
	t.Helper()
	c, err := s.CreateClient(context.Background(), NewClient{EventID: "refund-create-" + id, ClientID: id,
		RepresentativeID: "refund-rep", Name: id, DurationDays: 30, VolumeBytes: volume, IPCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func refundExpire(t *testing.T, s *Store, c Client) Client {
	t.Helper()
	if _, err := s.db.Exec(`UPDATE clients SET expires_unix=? WHERE id=?`, time.Now().Add(-time.Minute).Unix(), c.ID); err != nil {
		t.Fatal(err)
	}
	c, err := s.Client(context.Background(), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func refundNative(t *testing.T, s *Store, c Client, suffix string, used int64, final bool) (uc.Epoch, *uc.Outbox) {
	t.Helper()
	ctx := context.Background()
	h := sha256.Sum256([]byte(c.ID + suffix))
	id := hex.EncodeToString(h[:])
	email, _ := uc.CounterEmail(c.ID)
	e := uc.Epoch{ID: id[:32], NodeID: "node-" + c.ID + suffix, InstallationID: id[:32], ConfigSHA: id,
		KernelBootID: "00000000-0000-0000-0000-000000000001", PID: 123, ProcessStartTicks: 456,
		Bindings: []uc.Binding{{ClientID: c.ID, Email: email, Billable: true}}}
	if err := s.UsageLedger().RegisterEpoch(ctx, e); err != nil {
		t.Fatal(err)
	}
	box, err := uc.Open(uc.Options{Directory: filepath.Join(t.TempDir(), "box"), Key: bytes.Repeat([]byte{7}, 32), Epoch: e, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = box.Close() })
	r, err := box.Observe([]uc.Usage{{ClientID: c.ID, Uplink: used / 2, Downlink: used - used/2}}, final)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: e.NodeID, InstallationID: e.InstallationID}, r); err != nil {
		t.Fatal(err)
	}
	return e, box
}

func TestVolumeRefundGateAndSchemaFailClosed(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	if err := s.EnableVolumeExpiryRefunds(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("refund implicitly enabled metering", err)
	}
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListVolumeExpiryRefundCandidates(ctx, "", 10); !errors.Is(err, ErrUnavailable) {
		t.Fatal("default-on refund policy", err)
	}
	if err := s.EnableVolumeExpiryRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableVolumeExpiryRefunds(ctx); err != nil {
		t.Fatal("idempotent enable", err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER volume_refund_receipts_no_update`); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableVolumeExpiryRefunds(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal("silently repaired missing immutability policy", err)
	}
	if _, err := s.ListVolumeExpiryRefundCandidates(ctx, "", 10); !errors.Is(err, ErrUnavailable) {
		t.Fatal("continued with partial schema", err)
	}
	t.Run("malformed existing meta", func(t *testing.T) {
		s, _ := opened(t)
		if err := s.EnableUsageLedger(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := s.db.Exec(`CREATE TABLE volume_refund_meta(id INTEGER,version INTEGER)`); err != nil {
			t.Fatal(err)
		}
		if err := s.EnableVolumeExpiryRefunds(ctx); !errors.Is(err, ErrUnavailable) {
			t.Fatal("promoted malformed schema", err)
		}
	})
}

func TestVolumeRefundNativeConservationReplayAndRestart(t *testing.T) {
	ctx := context.Background()
	for _, used := range []int64{0, 40, 100, 110} {
		t.Run(fmt.Sprint(used), func(t *testing.T) {
			s, path := refundStore(t)
			c := refundClient(t, s, "volume-conservation", 100)
			refundNative(t, s, c, "", used, true)
			c = refundExpire(t, s, c)
			before, _ := s.Representative(ctx, c.RepresentativeID)
			want := int64(0)
			if used < 100 {
				want = 100 - used
			}
			res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
			if err != nil || !res.Applied || res.Replayed || res.Receipt == nil || res.Receipt.RefundedBytes != want || res.Receipt.Trigger != "expired" || len(res.Receipt.Proofs) != 1 || res.Receipt.Proofs[0].Kind != "native" {
				t.Fatal("wrong financial settlement", res, err)
			}
			after, _ := s.Representative(ctx, c.RepresentativeID)
			current, _ := s.Client(ctx, c.ID)
			usage, _ := s.ClientUsage(ctx, c.ID)
			if after.VolumeCreditBytes != before.VolumeCreditBytes+want || after.UnlimitedCreditUnits != before.UnlimitedCreditUnits ||
				current.VolumeBytes != c.VolumeBytes-want || current.UUID != c.UUID || current.ChargeVolumeBytes != c.ChargeVolumeBytes || current.ChargeUnlimitedUnits != c.ChargeUnlimitedUnits || current.IPCount != c.IPCount || current.ExpiresUnix != c.ExpiresUnix || usage.Total != used || usage.Eligible {
				t.Fatal("refund changed identity/history/token accounting or revived quota", after, current, usage)
			}
			for _, q := range []string{`UPDATE volume_refund_receipts SET refunded_bytes=0`, `DELETE FROM volume_refund_receipts`} {
				if _, err = s.db.Exec(q); err == nil {
					t.Fatal("mutable financial audit", q)
				}
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for i := 0; i < 3; i++ {
				replay, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
				if err != nil || replay.Applied || !replay.Replayed || replay.Receipt == nil || replay.Receipt.Generation != res.Receipt.Generation || replay.Receipt.RefundedBytes != want {
					t.Fatal("lost durable exactly-once receipt", replay, err)
				}
			}
			afterRestart, _ := s.Representative(ctx, c.RepresentativeID)
			if afterRestart != after {
				t.Fatal("restart replay changed balance", afterRestart, after)
			}
		})
	}
}

func TestVolumeRefundArchiveAndExpiryShareGeneration(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	c := refundClient(t, s, "archive-once", 100)
	if _, err := s.SetClientState(ctx, SetClientState{EventID: "refund-disable-only-0001", ClientID: c.ID, Action: "disable", Reason: "Temporary disable"}); err != nil {
		t.Fatal(err)
	}
	res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || res.Applied || res.Status.State != "not_due" {
		t.Fatal("plain disable refunded", res, err)
	}
	if _, err = s.SetClientState(ctx, SetClientState{EventID: "refund-archive-manual-0001", ClientID: c.ID, Action: "archive", Reason: "Representative manual deletion"}); err != nil {
		t.Fatal(err)
	}
	res, err = s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || !res.Applied || res.Receipt.Trigger != "archived" || res.Receipt.RefundedBytes != 100 || res.Receipt.Proofs[0].Kind != "never_issued" {
		t.Fatal("fresh undeployed archive not refunded", res, err)
	}
	// Time passing does not change entitlement identity or produce a second
	// credit. Evaluating with a future clock exercises expiry after archive.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	current, err := readClient(ctx, tx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	evaluated, err := s.evaluateVolumeRefund(ctx, tx, nil, current, c.ExpiresUnix+1)
	tx.Rollback()
	if err != nil || evaluated.status.State != "already_refunded" || evaluated.receipt.Generation != res.Receipt.Generation {
		t.Fatal("expiry separated archive generation", evaluated.status, err)
	}
	c = refundClient(t, s, "expiry-then-archive", 100)
	c = refundExpire(t, s, c)
	first, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || !first.Applied {
		t.Fatal(first, err)
	}
	if _, err = s.SetClientState(ctx, SetClientState{EventID: "refund-archive-after-expiry-0001", ClientID: c.ID, Action: "archive", Reason: "Manual deletion after expiry"}); err != nil {
		t.Fatal(err)
	}
	replay, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || !replay.Replayed || replay.Receipt.Trigger != "expired" || replay.Receipt.Generation != first.Receipt.Generation {
		t.Fatal("archive duplicated expiry refund", replay, err)
	}
}

func TestVolumeRefundRejectsUnsettledAndUnknownUsage(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	c := refundClient(t, s, "open-native", 100)
	e, box := refundNative(t, s, c, "", 30, false)
	c = refundExpire(t, s, c)
	res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || res.Applied || res.Status.Reason != "native_epoch_unsettled" || res.Status.RefundBytes != nil {
		t.Fatal("open native epoch refunded", res, err)
	}
	principal := usageledger.Principal{NodeID: e.NodeID, InstallationID: e.InstallationID}
	if err = s.UsageLedger().MarkEpochUncertain(ctx, principal, e.ID, "Lost terminal measurement"); err != nil {
		t.Fatal(err)
	}
	r, err := box.Observe([]uc.Usage{{ClientID: c.ID, Uplink: 15, Downlink: 15}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UsageLedger().Accept(ctx, principal, r); err != nil {
		t.Fatal(err)
	}
	res, err = s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || res.Applied || res.Status.Reason != "reconciliation_hold" {
		t.Fatal("later final forgave unknown native tail", res, err)
	}
	legacy, _ := opened(t)
	newRep(t, legacy, "refund-rep")
	grant(t, legacy, "refund-rep", "refund-legacy-credit-0001", 1000, 100)
	old := refundClient(t, legacy, "unknown-old-client", 100)
	if err = legacy.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err = legacy.EnableVolumeExpiryRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	old = refundExpire(t, legacy, old)
	res, err = legacy.RefundExpiredVolume(ctx, nil, refundRequestFor(old))
	if err != nil || res.Applied || res.Status.State != "usage_unknown" || res.Status.UsedBytes != nil || res.Status.RefundBytes != nil {
		t.Fatal("missing imported baseline became zero", res, err)
	}
}

func TestVolumeRefundRenewalRaceHistoricalReplayAndNetCap(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	c := refundClient(t, s, "renewal-race", 100)
	refundNative(t, s, c, "", 40, true)
	c = refundExpire(t, s, c)
	oldRequest := refundRequestFor(c)
	renewed, err := s.ExtendClient(ctx, ExtendClient{EventID: "refund-renewal-wins-0001", ClientID: c.ID, AdditionalDays: 1, Reason: "Renew before sweep"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.RefundExpiredVolume(ctx, nil, oldRequest); !errors.Is(err, ErrConflict) {
		t.Fatal("stale expiry refunded renewed entitlement", err)
	}
	renewed = refundExpire(t, s, renewed)
	res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(renewed))
	if err != nil || !res.Applied || res.Receipt.RefundedBytes != 60 {
		t.Fatal(res, err)
	}
	completed := refundRequestFor(renewed)
	renewed, err = s.ExtendClient(ctx, ExtendClient{EventID: "refund-time-only-after-0001", ClientID: c.ID, AdditionalDays: 1, Reason: "Time-only renewal after refund"})
	if err != nil || renewed.VolumeBytes != 40 || renewed.ChargeVolumeBytes != 100 {
		t.Fatal("time-only renewal revived refunded capacity", renewed, err)
	}
	usage, _ := s.ClientUsage(ctx, c.ID)
	if usage.Eligible || usage.Total != 40 || usage.Limit != 40 {
		t.Fatal("refunded bytes became usable for free", usage)
	}
	replay, err := s.RefundExpiredVolume(ctx, nil, completed)
	if err != nil || !replay.Replayed || replay.Receipt.Generation != res.Receipt.Generation {
		t.Fatal("historical retry lost after renewal", replay, err)
	}
	current, _ := s.Client(ctx, c.ID)
	if current != renewed {
		t.Fatal("old replay changed new entitlement", current, renewed)
	}
	renewed, err = s.ExtendClient(ctx, ExtendClient{EventID: "refund-paid-topup-after-0001", ClientID: c.ID, AdditionalVolumeBytes: 50, Reason: "Buy new capacity"})
	if err != nil || renewed.VolumeBytes != 90 || renewed.ChargeVolumeBytes != 150 {
		t.Fatal("paid top-up did not preserve net cap", renewed, err)
	}
	renewed = refundExpire(t, s, renewed)
	res, err = s.RefundExpiredVolume(ctx, nil, refundRequestFor(renewed))
	if err != nil || !res.Applied || res.Receipt.RefundedBytes != 50 || res.Receipt.PreviouslyRefunded != 60 || res.Receipt.VolumeAfter != 40 {
		t.Fatal("later generation reused refunded capacity", res, err)
	}
}

func TestVolumeRefundConcurrentRetriesAndRollback(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	c := refundExpire(t, s, refundClient(t, s, "concurrent-refund", 100))
	if _, err := s.db.Exec(`CREATE TRIGGER test_refund_failure BEFORE INSERT ON volume_refund_receipts BEGIN SELECT RAISE(ABORT,'injected financial commit failure'); END`); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Representative(ctx, c.RepresentativeID)
	if _, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c)); err == nil {
		t.Fatal("injected receipt failure succeeded")
	}
	after, _ := s.Representative(ctx, c.RepresentativeID)
	current, _ := s.Client(ctx, c.ID)
	if after != before || current != c {
		t.Fatal("credit/cap escaped rollback", before, after, c, current)
	}
	if _, err := s.db.Exec(`DROP TRIGGER test_refund_failure`); err != nil {
		t.Fatal(err)
	}
	results := make(chan VolumeExpiryRefundResult, 12)
	errs := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	applied, replayed := 0, 0
	for r := range results {
		if r.Applied {
			applied++
		}
		if r.Replayed {
			replayed++
		}
	}
	after, _ = s.Representative(ctx, c.RepresentativeID)
	if applied != 1 || replayed != 11 || after.VolumeCreditBytes != before.VolumeCreditBytes+100 {
		t.Fatal("concurrent credits were not exactly once", applied, replayed, before, after)
	}
}
