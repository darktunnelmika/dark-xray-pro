package business

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// Model a real second-boundary gap between identity creation and its trusted
// seed write without editing immutable seed or financial history. Potential
// issuance inside that gap remains unclosed even when its lease has expired.
func TestVolumeRefundSeedAfterBirthCannotHideEarlierIssuance(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	newRep(t, s, "refund-rep")
	grant(t, s, "refund-rep", "refund-late-seed-allocation-0001", 1000, 100)
	c := refundClient(t, s, "late-seed-issued", 100)
	time.Sleep(time.Until(time.Unix(c.CreatedUnix+1, 0)))
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("new-native-usage:" + c.ID + ":" + c.UUID))
	seed := usageledger.Seed{ClientID: c.ID, EventID: "new-usage-" + hex.EncodeToString(sum[:]), Complete: true, Reason: "New PRO identity, before first activation"}
	if err := s.UsageLedger().InitializeAccount(ctx, seed); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableVolumeExpiryRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	var seedCreated int64
	if err := s.db.QueryRow(`SELECT created_unix FROM pro_usage_seeds WHERE client_id=?`, c.ID).Scan(&seedCreated); err != nil || seedCreated <= c.CreatedUnix {
		t.Fatal("fixture did not establish later seed", seedCreated, c.CreatedUnix, err)
	}
	c = refundExpire(t, s, c)
	if _, err := s.db.Exec(`INSERT INTO native_runtime_delivery(node_id,installation_id,issued,expires,sequence,sha,manifest,generation) VALUES(?,?,?,?,1,?,?,1)`,
		"birth-boundary-node", strings.Repeat("a", 32), c.CreatedUnix*int64(time.Second), c.CreatedUnix*int64(time.Second)+1, strings.Repeat("b", 64), strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	before, err := s.Representative(ctx, "refund-rep")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || result.Applied || result.Status.Reason != "runtime_issuance_unsettled" || result.Status.RefundBytes != nil {
		t.Fatal("later seed hid potentially served bytes", result, err)
	}
	after, err := s.Representative(ctx, "refund-rep")
	if err != nil || before.VolumeCreditBytes != after.VolumeCreditBytes {
		t.Fatal("unsettled issuance credited representative", err)
	}
}
