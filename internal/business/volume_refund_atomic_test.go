package business

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestVolumeRefundExactBytePrecisionAndCreditCeiling(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	const purchased int64 = 9007199254740993 // 2^53 + 1, above JS exact integers.
	grant(t, s, "refund-rep", "refund-exact-credit-0001", purchased, 0)
	c := refundClient(t, s, "exact-byte-volume", purchased)
	refundNative(t, s, c, "", 2, true)
	c = refundExpire(t, s, c)
	before, _ := s.Representative(ctx, c.RepresentativeID)
	res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || !res.Applied || res.Receipt.RefundedBytes != purchased-2 || res.Receipt.VolumeAfter != 2 {
		t.Fatal("unused bytes rounded", res, err)
	}
	after, _ := s.Representative(ctx, c.RepresentativeID)
	if after.VolumeCreditBytes-before.VolumeCreditBytes != purchased-2 {
		t.Fatal("credited bytes lost precision", before, after)
	}
	raw, err := json.Marshal(res)
	if err != nil || !strings.Contains(string(raw), `"charged_volume_bytes":"9007199254740993"`) || !strings.Contains(string(raw), `"refunded_bytes":"9007199254740991"`) {
		t.Fatal("public receipt lost decimal string encoding", string(raw), err)
	}
	ceiling := refundExpire(t, s, refundClient(t, s, "credit-ceiling", 100))
	before, _ = s.Representative(ctx, ceiling.RepresentativeID)
	grant(t, s, "refund-rep", "refund-fill-ceiling-0001", MaxVolumeCredit-before.VolumeCreditBytes, 0)
	res, err = s.RefundExpiredVolume(ctx, nil, refundRequestFor(ceiling))
	if err != nil || res.Applied || res.Status.State != "credit_limit" || res.Receipt != nil {
		t.Fatal("credit ceiling overflow or lost pending amount", res, err)
	}
	current, _ := s.Client(ctx, ceiling.ID)
	after, _ = s.Representative(ctx, ceiling.RepresentativeID)
	if current != ceiling || after.VolumeCreditBytes != MaxVolumeCredit {
		t.Fatal("credit limit failure partially changed accounting", current, after)
	}
}

func refundRetrySQLiteBusy(run func() error) error {
	for i := 0; i < 20; i++ {
		err := run()
		var code interface{ Code() int }
		if !errors.As(err, &code) || code.Code()&255 != 5 {
			return err
		}
		// Another independent writer may hold a WAL snapshot. Retry only the
		// SQLite BUSY outcome, exactly as a later automatic sweep would do.
		time.Sleep(time.Millisecond)
	}
	return errors.New("SQLite busy after bounded retry")
}

func TestVolumeRefundAndRenewalSerializeAcrossIndependentConnections(t *testing.T) {
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			s, path := refundStore(t)
			c := refundClient(t, s, "parallel-renew-refund", 100)
			refundNative(t, s, c, "", 40, true)
			c = refundExpire(t, s, c)
			other, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			before, _ := s.Representative(ctx, c.RepresentativeID)
			start := make(chan struct{})
			refundDone, renewDone := make(chan error, 1), make(chan error, 1)
			var refunded VolumeExpiryRefundResult
			go func() {
				<-start
				refundDone <- refundRetrySQLiteBusy(func() error {
					var err error
					refunded, err = s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
					return err
				})
			}()
			go func() {
				<-start
				renewDone <- refundRetrySQLiteBusy(func() error {
					_, err := other.ExtendClient(ctx, ExtendClient{EventID: "refund-racing-renewal-0001", ClientID: c.ID, AdditionalDays: 1, Reason: "Concurrent time-only renewal"})
					return err
				})
			}()
			close(start)
			refundErr, renewErr := <-refundDone, <-renewDone
			if renewErr != nil || refundErr != nil && !errors.Is(refundErr, ErrConflict) {
				t.Fatal("independent transaction failed", refundErr, renewErr)
			}
			current, _ := s.Client(ctx, c.ID)
			after, _ := s.Representative(ctx, c.RepresentativeID)
			var receipts int
			if err = s.db.QueryRow(`SELECT COUNT(*) FROM volume_refund_receipts WHERE client_id=?`, c.ID).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if current.ExpiresUnix <= time.Now().Unix() || current.ChargeVolumeBytes != 100 || current.UUID != c.UUID || after.UnlimitedCreditUnits != before.UnlimitedCreditUnits {
				t.Fatal("renewal history lost in race", current, before, after)
			}
			if refunded.Applied {
				if current.VolumeBytes != 40 || after.VolumeCreditBytes != before.VolumeCreditBytes+60 || receipts != 1 {
					t.Fatal("refund-first renewal resurrected returned capacity", current, after, receipts)
				}
			} else if !errors.Is(refundErr, ErrConflict) || current.VolumeBytes != 100 || after.VolumeCreditBytes != before.VolumeCreditBytes || receipts != 0 {
				t.Fatal("renewal-first stale snapshot returned credit", refundErr, current, after, receipts)
			}
		})
	}
}
