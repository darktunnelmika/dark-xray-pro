package business

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestVolumeRefundNeverIssuedRequiresPositiveSeedAndNoRuntimeAuthority(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	c := refundExpire(t, s, refundClient(t, s, "issued-no-final", 100))
	if _, err := s.CreateInbound(ctx, make([]byte, 32), nativeInput()); err != nil {
		t.Fatal(err)
	}
	if err := s.BindInboundClients(ctx, 10, []string{c.ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.PutInboundTarget(ctx, InboundTarget{InboundID: 10, NodeID: "issued-node", InstallationID: strings.Repeat("a", 32), ListenIP: "127.0.0.1", DirectPort: 19897}); err != nil {
		t.Fatal(err)
	}
	// The latest issued lease is intentionally long expired. Time alone must
	// not turn potentially unobserved traffic into an unused-volume credit.
	if _, err := s.db.Exec(`INSERT INTO native_runtime_delivery(node_id,installation_id,issued,expires,sequence,sha,manifest,generation) VALUES(?,?,?,?,1,?,?,1)`,
		"issued-node", strings.Repeat("a", 32), c.CreatedUnix*int64(time.Second), c.CreatedUnix*int64(time.Second)+1, strings.Repeat("b", 64), strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || res.Applied || res.Status.Reason != "runtime_issuance_unsettled" || res.Status.RefundBytes != nil {
		t.Fatal("missing epoch plus elapsed lease was treated as zero", res, err)
	}
	// A complete seed from an explicit import is still not the server's
	// immutable fresh identity seed, even if its measured total is zero.
	legacy, _ := opened(t)
	newRep(t, legacy, "refund-rep")
	grant(t, legacy, "refund-rep", "refund-import-credit-0001", 1000, 100)
	imported := refundClient(t, legacy, "imported-zero", 100)
	if err = legacy.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err = legacy.EnableVolumeExpiryRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = legacy.db.Exec(`INSERT INTO pro_usage_accounts(client_id,uplink,downlink,baseline_complete) VALUES(?,0,0,1)`, imported.ID); err != nil {
		t.Fatal(err)
	}
	imported = refundExpire(t, legacy, imported)
	res, err = legacy.RefundExpiredVolume(ctx, nil, refundRequestFor(imported))
	if err != nil || res.Applied || res.Status.Reason != "fresh_identity_proof_missing" {
		t.Fatal("complete zero without trusted creation seed refunded", res, err)
	}
}

func TestVolumeRefundProspectiveExposureCannotDisappearOnUnbind(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	bound := refundClient(t, s, "future-bound", 100)
	unbound := refundClient(t, s, "future-unexposed", 100)
	if _, err := s.CreateInbound(ctx, make([]byte, 32), nativeInput()); err != nil {
		t.Fatal(err)
	}
	if err := s.BindInboundClients(ctx, 10, []string{bound.ID}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.PutInboundTarget(ctx, InboundTarget{InboundID: 10, NodeID: "busy-native-node", InstallationID: strings.Repeat("a", 32), ListenIP: "127.0.0.1", DirectPort: 19897}); err != nil {
		t.Fatal(err)
	}
	// An older candidate may still carry bound after current membership is
	// removed. Potential exposure must persist independently of that edit.
	if err := s.BindInboundClients(ctx, 10, []string{bound.ID}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO native_runtime_delivery(node_id,installation_id,issued,expires,sequence,sha,manifest,generation) VALUES(?,?,?,?,1,?,?,1)`,
		"busy-native-node", strings.Repeat("a", 32), time.Now().UnixNano(), time.Now().Add(time.Second).UnixNano(), strings.Repeat("b", 64), strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []Client{bound, unbound} {
		archived, err := s.SetClientState(ctx, SetClientState{EventID: "refund-tracking-delete-" + c.ID, ClientID: c.ID, Action: "archive", Reason: "Manual deletion"})
		if err != nil {
			t.Fatal(err)
		}
		res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(archived))
		if err != nil {
			t.Fatal(err)
		}
		if c.ID == bound.ID && (res.Applied || res.Status.Reason != "runtime_issuance_unsettled") {
			t.Fatal("unbind erased old candidate exposure", res)
		}
		if c.ID == unbound.ID && (!res.Applied || res.Receipt.RefundedBytes != 100 || res.Receipt.Proofs[0].Kind != "never_issued") {
			t.Fatal("unrelated busy native node starved provably unexposed identity", res)
		}
	}
	for _, table := range []string{"volume_refund_created_identities", "volume_refund_native_exposures"} {
		if _, err := s.db.Exec(`DELETE FROM ` + table); err == nil {
			t.Fatal("exposure proof could be erased", table)
		}
		if _, err := s.db.Exec(`UPDATE ` + table + ` SET client_uuid='changed'`); err == nil {
			t.Fatal("exposure identity could be rewritten", table)
		}
	}
}

func TestVolumeRefundTrackingUpgradeDoesNotInventHistoricalProof(t *testing.T) {
	ctx := context.Background()
	s, _ := opened(t)
	if err := s.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	newRep(t, s, "refund-rep")
	grant(t, s, "refund-rep", "refund-upgrade-credit-0001", 10000, 1000)
	for _, ddl := range volumeRefundBaseSchema {
		if _, err := s.db.Exec(ddl); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`INSERT INTO volume_refund_meta(id,version) VALUES(1,1)`); err != nil {
		t.Fatal(err)
	}
	historical := refundClient(t, s, "pre-tracking-client", 100)
	if err := s.EnableVolumeExpiryRefunds(ctx); err != nil {
		t.Fatal("complete original refund policy did not upgrade", err)
	}
	fresh := refundClient(t, s, "post-tracking-client", 100)
	if _, err := s.db.Exec(`INSERT INTO native_runtime_delivery(node_id,installation_id,issued,expires,sequence,sha,manifest,generation) VALUES(?,?,?,?,1,?,?,1)`,
		"historically-busy", strings.Repeat("a", 32), time.Now().UnixNano(), time.Now().Add(time.Second).UnixNano(), strings.Repeat("b", 64), strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	for _, c := range []Client{historical, fresh} {
		c = refundExpire(t, s, c)
		res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
		if err != nil {
			t.Fatal(err)
		}
		if c.ID == historical.ID && (res.Applied || res.Status.Reason != "runtime_issuance_unsettled") {
			t.Fatal("upgrade fabricated historical absence of exposure", res)
		}
		if c.ID == fresh.ID && (!res.Applied || res.Receipt.RefundedBytes != 100) {
			t.Fatal("new unexposed identity not refundable after upgrade", res)
		}
	}
}

func TestVolumeRefundUnlimitedOwnerAndUnprovenFundingUnchanged(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	unlimited, err := s.CreateClient(ctx, NewClient{EventID: "refund-unlimited-create-0001", ClientID: "unlimited-untouched", RepresentativeID: "refund-rep", Name: "Unlimited", Unlimited: true, IPCount: 5, DurationDays: 60})
	if err != nil {
		t.Fatal(err)
	}
	unlimited = refundExpire(t, s, unlimited)
	unlimited, err = s.SetClientState(ctx, SetClientState{EventID: "refund-unlimited-archive-0001", ClientID: unlimited.ID, Action: "archive", Reason: "Manual deletion"})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := s.Representative(ctx, "refund-rep")
	res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(unlimited))
	if err != nil || res.Applied || res.Status.State != "not_volume" || res.Receipt != nil {
		t.Fatal("unlimited entered refund accounting", res, err)
	}
	after, _ := s.Representative(ctx, "refund-rep")
	current, _ := s.Client(ctx, unlimited.ID)
	if after != before || current != unlimited || current.ChargeUnlimitedUnits != 10 || current.IPCount != 5 {
		t.Fatal("unlimited tokens or identity changed", before, after, unlimited, current)
	}
	owner := refundExpire(t, s, usageClient(t, s, "owner-excluded", 100))
	res, err = s.RefundExpiredVolume(ctx, nil, refundRequestFor(owner))
	if err != nil || res.Applied || res.Status.State != "owner_exempt" {
		t.Fatal("owner capacity refunded to representative", res, err)
	}
	paid := refundExpire(t, s, refundClient(t, s, "unproven-imported-cap", 100))
	// Simulate an imported/unexplained cap; current display and charged
	// counters alone cannot manufacture an immutable representative debit.
	if _, err = s.db.Exec(`UPDATE clients SET volume_bytes=150,charged_volume_bytes=150 WHERE id=?`, paid.ID); err != nil {
		t.Fatal(err)
	}
	paid, _ = s.Client(ctx, paid.ID)
	res, err = s.RefundExpiredVolume(ctx, nil, refundRequestFor(paid))
	if err != nil || res.Applied || res.Status.State != "funding_unproven" || res.Status.FundedBytes != nil {
		t.Fatal("display capacity became paid credit", res, err)
	}
	candidates, err := s.ListVolumeExpiryRefundCandidates(ctx, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range candidates {
		if req.ClientID == unlimited.ID || req.ClientID == owner.ID {
			t.Fatal("sweep included non-representative volume", req)
		}
	}
}

func TestVolumeRefundEveryNativeBirthMustBeFinal(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	c := refundClient(t, s, "multiple-births", 100)
	e, _ := refundNative(t, s, c, "", 10, true)
	e.ID, e.NodeID = strings.Repeat("f", 32), "other-pending-node"
	if err := s.UsageLedger().RegisterEpoch(ctx, e); err != nil {
		t.Fatal(err)
	}
	c = refundExpire(t, s, c)
	res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
	if err != nil || res.Applied || res.Status.Reason != "native_epoch_unsettled" {
		t.Fatal("final receipt hid a second registered zero-sequence birth", res, err)
	}
}

func refundExecutionFixture(t *testing.T) *executionFixture {
	t.Helper()
	f := newExecutionFixture(t)
	newRep(t, f.s, "refund-rep")
	grant(t, f.s, "refund-rep", "refund-managed-allocation-0001", 10000, 1000)
	f.client = refundClient(t, f.s, "managed-paid-client", 100)
	if err := f.s.EnableVolumeExpiryRefunds(context.Background()); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestVolumeRefundManagedFinalAndPreparedBirthSafety(t *testing.T) {
	ctx := context.Background()
	f := refundExecutionFixture(t)
	f.command(t, "refund-managed-activate-0001", "activate")
	b := f.bundle(t)
	f.client = refundExpire(t, f.s, f.client)
	res, err := f.s.RefundExpiredVolume(ctx, f.key, refundRequestFor(f.client))
	if err != nil || res.Applied || res.Status.Reason != "runtime_issuance_unsettled" {
		t.Fatal("prepared bundle without terminal proof refunded", res, err)
	}
	// A real closed-gate final birth is the safe path even after expiry.
	e := executionEpoch(b, "refund-managed-closed-birth-0001")
	r := executionReport(b, e, 1, 0, 0, true)
	r.InitialEpoch = &e
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, r); err != nil {
		t.Fatal(err)
	}
	res, err = f.s.RefundExpiredVolume(ctx, f.key, refundRequestFor(f.client))
	if err != nil || !res.Applied || res.Receipt.RefundedBytes != 100 || len(res.Receipt.Proofs) != 1 || res.Receipt.Proofs[0].Kind != "managed" {
		t.Fatal("authenticated zero final did not settle full unused volume", res, err)
	}
	t.Run("active usage", func(t *testing.T) {
		f := refundExecutionFixture(t)
		f.command(t, "refund-managed-active-0001", "activate")
		b := f.bundle(t)
		e := f.birth(t, b, "refund-managed-active-birth-0001")
		f.ready(t, b, e)
		if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 12, 18, false)); err != nil {
			t.Fatal(err)
		}
		c, err := f.s.SetClientState(ctx, SetClientState{EventID: "refund-managed-delete-0001", ClientID: f.client.ID, Action: "archive", Reason: "Representative manual deletion"})
		if err != nil {
			t.Fatal(err)
		}
		res, err := f.s.RefundExpiredVolume(ctx, f.key, refundRequestFor(c))
		if err != nil || res.Applied || res.Status.Reason != "managed_epoch_unsettled" {
			t.Fatal("archive clock closed active managed usage", res, err)
		}
		if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 3, 15, 25, true)); err != nil {
			t.Fatal(err)
		}
		res, err = f.s.RefundExpiredVolume(ctx, f.key, refundRequestFor(c))
		if err != nil || !res.Applied || res.Receipt.RefundedBytes != 60 || res.Receipt.UsedBytes != 40 || res.Receipt.Trigger != "archived" {
			t.Fatal("final tail not included exactly", res, err)
		}
	})
}

func TestVolumeRefundManagedOwnerLossSealNeverForgivesUnknownTail(t *testing.T) {
	ctx := context.Background()
	f := refundExecutionFixture(t)
	f.command(t, "refund-owner-loss-activate-0001", "activate")
	b := f.bundle(t)
	e := f.birth(t, b, "refund-owner-loss-birth-0001")
	f.ready(t, b, e)
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 2, 10, 20, false)); err != nil {
		t.Fatal(err)
	}
	if err := f.s.MarkInboundExecutionEpochUncertain(ctx, f.key, b.NodeID, b.InstallationID, e.EpochID, "Missing unmeasured terminal tail"); err != nil {
		t.Fatal(err)
	}
	in := reviewedOwnerLoss(t, f, 501, "refund-owner-loss-decision-0001", e.EpochID, 2)
	if _, err := f.s.ReconcileInboundExecution(ctx, f.key, "owner", 501, in); err != nil {
		t.Fatal(err)
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Holds != 0 {
		t.Fatal("fixture did not exercise cleared administrative holds", account)
	}
	f.client = refundExpire(t, f.s, f.client)
	for _, lateFinal := range []bool{false, true} {
		if lateFinal {
			if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(b, e, 3, 15, 25, true)); err != nil {
				t.Fatal(err)
			}
		}
		res, err := f.s.RefundExpiredVolume(ctx, f.key, refundRequestFor(f.client))
		if err != nil || res.Applied || res.Status.Reason != "managed_unknown_tail" || res.Status.RefundBytes != nil {
			t.Fatal("owner-loss sealed state manufactured refundable usage", lateFinal, res, err)
		}
	}
}

func TestVolumeRefundPreparedManagedReplacementCannotAdmitAfterRefund(t *testing.T) {
	ctx := context.Background()
	f := refundExecutionFixture(t)
	f.command(t, "refund-managed-old-activate-0001", "activate")
	oldBundle := f.bundle(t)
	oldEpoch := f.birth(t, oldBundle, "refund-managed-old-birth-0001")
	f.ready(t, oldBundle, oldEpoch)
	if _, err := f.s.AcceptInboundExecutionUsage(ctx, f.key, executionReport(oldBundle, oldEpoch, 2, 15, 25, true)); err != nil {
		t.Fatal(err)
	}
	f.command(t, "refund-managed-next-prepare-0001", "restart")
	prepared := f.bundle(t)
	newEpoch := executionEpoch(prepared, "refund-managed-next-birth-0001")
	if prepared.Generation <= oldBundle.Generation {
		t.Fatal("fixture failed to prepare newer candidate")
	}
	f.client = refundExpire(t, f.s, f.client)
	res, err := f.s.RefundExpiredVolume(ctx, f.key, refundRequestFor(f.client))
	if err != nil || !res.Applied || res.Receipt.RefundedBytes != 60 {
		t.Fatal("settled admitted usage did not refund", res, err)
	}
	if _, err = f.s.RegisterInboundExecutionEpoch(ctx, f.key, newEpoch); !errors.Is(err, ErrConflict) {
		t.Fatal("prepared candidate admitted after expiry/refund", err)
	}
	if err = f.s.InboundExecutionAuthority(ctx, f.key, prepared.NodeID, prepared.InstallationID, prepared.Generation); !errors.Is(err, ErrConflict) {
		t.Fatal("prepared candidate renewed lease after expiry/refund", err)
	}
	// Genuine zero final evidence remains admissible to close the unused
	// prepared process, without granting admission or changing paid usage.
	final := executionReport(prepared, newEpoch, 1, 0, 0, true)
	final.InitialEpoch = &newEpoch
	if _, err = f.s.AcceptInboundExecutionUsage(ctx, f.key, final); err != nil {
		t.Fatal(err)
	}
	account, _ := f.s.ClientUsage(ctx, f.client.ID)
	if account.Total != 40 || account.Limit != 40 || account.Eligible {
		t.Fatal("closed prepared birth revived refunded capacity", account)
	}
}

func TestVolumeRefundProfileAndBulkRenewalsCannotReviveCapacity(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"profile", "bulk"} {
		t.Run(mode, func(t *testing.T) {
			s, _ := refundStore(t)
			c := refundExpire(t, s, refundClient(t, s, "quote-and-refund", 100))
			profile := ClientProfileEditIntent{ClientID: c.ID, Name: "Edited", PlanKind: "volume", IPCount: c.IPCount, AdditionalDays: 1, Reason: "Renew time"}
			bulk := BulkExtensionIntent{ClientIDs: []string{c.ID}, AdditionalDays: 1, Reason: "Renew time"}
			pq, err := s.QuoteClientProfileEdit(ctx, profile, c.RepresentativeID)
			if err != nil {
				t.Fatal(err)
			}
			bq, err := s.QuoteBulkExtension(ctx, bulk, c.RepresentativeID)
			if err != nil {
				t.Fatal(err)
			}
			res, err := s.RefundExpiredVolume(ctx, nil, refundRequestFor(c))
			if err != nil || !res.Applied || res.Receipt.VolumeAfter != 0 {
				t.Fatal(res, err)
			}
			if _, err = s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{ClientProfileEditIntent: profile, EventID: "refund-stale-profile-0001", QuoteHash: pq.QuoteHash, QuoteUnix: pq.QuoteUnix}, c.RepresentativeID); !errors.Is(err, ErrConflict) {
				t.Fatal("pre-refund profile quote accepted", err)
			}
			if _, err = s.CommitBulkExtension(ctx, BulkExtensionCommit{BulkExtensionIntent: bulk, EventID: "refund-stale-bulk-0001", QuoteHash: bq.QuoteHash, QuoteUnix: bq.QuoteUnix}, c.RepresentativeID); !errors.Is(err, ErrConflict) {
				t.Fatal("pre-refund bulk quote accepted", err)
			}
			if mode == "profile" {
				pq, err = s.QuoteClientProfileEdit(ctx, profile, c.RepresentativeID)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{ClientProfileEditIntent: profile, EventID: "refund-fresh-profile-0001", QuoteHash: pq.QuoteHash, QuoteUnix: pq.QuoteUnix}, c.RepresentativeID)
			} else {
				bq, err = s.QuoteBulkExtension(ctx, bulk, c.RepresentativeID)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.CommitBulkExtension(ctx, BulkExtensionCommit{BulkExtensionIntent: bulk, EventID: "refund-fresh-bulk-0001", QuoteHash: bq.QuoteHash, QuoteUnix: bq.QuoteUnix}, c.RepresentativeID)
			}
			if err != nil {
				t.Fatal("zero-cap renewal rejected", err)
			}
			current, _ := s.Client(ctx, c.ID)
			account, _ := s.ClientUsage(ctx, c.ID)
			if current.VolumeBytes != 0 || current.ChargeVolumeBytes != 100 || current.ExpiresUnix <= time.Now().Unix() || account.Eligible {
				t.Fatal("time-only edit revived refunded capacity", current, account)
			}
			// Paid edits add only the newly debited capacity and preserve a
			// verifiable immutable funding path for the next refund generation.
			if mode == "profile" {
				profile.AdditionalDays, profile.AdditionalVolumeBytes = 0, 25
				pq, err = s.QuoteClientProfileEdit(ctx, profile, c.RepresentativeID)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.CommitClientProfileEdit(ctx, ClientProfileEditCommit{ClientProfileEditIntent: profile, EventID: "refund-paid-profile-0001", QuoteHash: pq.QuoteHash, QuoteUnix: pq.QuoteUnix}, c.RepresentativeID)
			} else {
				bulk.AdditionalDays, bulk.AdditionalVolumeBytes = 0, 25
				bq, err = s.QuoteBulkExtension(ctx, bulk, c.RepresentativeID)
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.CommitBulkExtension(ctx, BulkExtensionCommit{BulkExtensionIntent: bulk, EventID: "refund-paid-bulk-0001", QuoteHash: bq.QuoteHash, QuoteUnix: bq.QuoteUnix}, c.RepresentativeID)
			}
			if err != nil {
				t.Fatal(err)
			}
			current, _ = s.Client(ctx, c.ID)
			current = refundExpire(t, s, current)
			res, err = s.RefundExpiredVolume(ctx, nil, refundRequestFor(current))
			if err != nil || !res.Applied || res.Receipt.RefundedBytes != 25 || res.Receipt.FundedBytes != 125 || res.Receipt.PreviouslyRefunded != 100 || res.Receipt.VolumeAfter != 0 {
				t.Fatal("paid profile/bulk funding not conserved", res, err)
			}
		})
	}
}

func TestVolumeRefundSweepCursorProgressAndPartialFailure(t *testing.T) {
	ctx := context.Background()
	s, _ := refundStore(t)
	a := refundClient(t, s, "a-waiting", 100)
	refundNative(t, s, a, "", 10, false)
	refundExpire(t, s, a)
	b := refundExpire(t, s, refundClient(t, s, "b-ready", 100))
	c := refundExpire(t, s, refundClient(t, s, "c-ready", 100))
	first, err := s.SweepExpiredVolumeRefunds(ctx, nil, "", 1)
	if err != nil || first.Examined != 1 || first.Applied != 0 || first.NextClientID != a.ID {
		t.Fatal("pending first page failed to advance", first, err)
	}
	second, err := s.SweepExpiredVolumeRefunds(ctx, nil, first.NextClientID, 1)
	if err != nil || second.Applied != 1 || second.NextClientID != b.ID {
		t.Fatal("pending client starved ready later client", second, err)
	}
	if _, err = s.db.Exec(`CREATE TRIGGER test_sweep_failure BEFORE INSERT ON volume_refund_receipts WHEN NEW.client_id='c-ready' BEGIN SELECT RAISE(ABORT,'injected financial commit failure'); END`); err != nil {
		t.Fatal(err)
	}
	failed, err := s.SweepExpiredVolumeRefunds(ctx, nil, second.NextClientID, 1)
	if err == nil || failed.Examined != 1 || failed.Applied != 0 || failed.NextClientID != c.ID {
		t.Fatal("partial failure lost progress cursor", failed, err)
	}
	end, err := s.SweepExpiredVolumeRefunds(ctx, nil, failed.NextClientID, 1)
	if err != nil || end.Examined != 0 || end.NextClientID != "" {
		t.Fatal("end cursor failed to wrap", end, err)
	}
	if _, err = s.db.Exec(`DROP TRIGGER test_sweep_failure`); err != nil {
		t.Fatal(err)
	}
	cycle, err := s.SweepExpiredVolumeRefunds(ctx, nil, "", 100)
	if err != nil || cycle.Examined != 2 || cycle.Applied != 1 || cycle.NextClientID != "" {
		t.Fatal("next cycle failed to retry pending/error rows", cycle, err)
	}
}
