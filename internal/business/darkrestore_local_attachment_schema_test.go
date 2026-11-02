package business

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestDarkRestoreOptionalLocalSettlementUpgradeAndDowngradeGuard(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	req := importRequest(t, f, syntheticImportIntent(1), "darkrestore-optional-upgrade-0001")
	original, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, ddl := range []string{
		"DROP TRIGGER darkrestore_local_settlements_v1_no_update",
		"DROP TRIGGER darkrestore_local_settlements_v1_no_delete",
		"DROP TABLE darkrestore_local_settlements_v1",
		"PRAGMA application_id=0",
	} {
		if _, err = f.s.db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal("prior M2 schema was rejected", err)
	}
	if err = f.s.EnableDarkRestoreImportV1(ctx); err != nil {
		t.Fatal("in place optional upgrade", err)
	}
	var app int
	if err = f.s.db.QueryRowContext(ctx, "PRAGMA application_id").Scan(&app); err != nil || app != darkRestoreLocalAttachmentApplicationID {
		t.Fatal("new schema has no durable downgrade marker", app, err)
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); err != nil {
		t.Fatal(err)
	}
	replay, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req)
	if err != nil || !replay.Replayed || replay.EventID != original.EventID {
		t.Fatal("original import receipt changed on upgrade", err)
	}
	for _, ddl := range []string{
		"DROP TRIGGER darkrestore_local_settlements_v1_no_update",
		"DROP TRIGGER darkrestore_local_settlements_v1_no_delete",
		"DROP TABLE darkrestore_local_settlements_v1",
	} {
		if _, err = f.s.db.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	if err = validateDarkRestoreImportData(ctx, f.s.db); !errors.Is(err, ErrUnavailable) {
		t.Fatal("lost local epoch table accepted as older schema", err)
	}
}

func TestDarkRestoreUsageRejectsMismatchedLocalAttachmentAtRead(t *testing.T) {
	f := importSetup(t)
	ctx := context.Background()
	intent := syntheticImportIntent(1)
	req := importRequest(t, f, intent, "darkrestore-attachment-read-0001")
	if _, err := f.s.commitVerifiedDarkRestore(ctx, f.owner.Token, req); err != nil {
		t.Fatal(err)
	}
	source := intent.Review.Clients[0]
	id := darkRestoreClientID(intent.Review.SourceSHA256, source.SourceID)
	proof := darkrestore.LocalSourceSettlement{
		Scope: "single_local_source_epoch", SourceID: source.SourceID,
		SourceSHA: intent.Review.SourceSHA256, AuthoritySHA: strings.Repeat("a", 64),
		JournalSHA: strings.Repeat("b", 64),
		Terminal:   uc.Receipt{EpochID: strings.Repeat("c", 32), Sequence: 1, SHA: strings.Repeat("d", 64)},
		PriorBytes: 7, Up: 3, Down: 4, Historical: 14, Remaining: 999986,
	}
	receipt := DarkRestoreLocalAttachmentReceiptV1{
		ClientID: id, OriginalEventID: req.EventID, ActorUserID: "test-owner",
		Settlement: proof, CreatedUnix: time.Now().Unix(),
	}
	encode := func(value DarkRestoreLocalAttachmentReceiptV1) []byte {
		t.Helper()
		out, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if _, err := f.s.db.ExecContext(ctx,
		"INSERT INTO darkrestore_local_settlements_v1(client_id,source_sha256,source_id,terminal_epoch_id,receipt_json,created_unix) VALUES(?,?,?,?,?,?)",
		id, proof.SourceSHA, proof.SourceID, proof.Terminal.EpochID, encode(receipt), receipt.CreatedUnix); err != nil {
		t.Fatal(err)
	}
	if got, err := f.s.DarkRestoreUsageV1(ctx, id); err != nil || got.HistoricalBytes != "14" || !got.SourceEpochSettled {
		t.Fatal("valid local attachment not projected", got, err)
	}
	if _, err := f.s.db.ExecContext(ctx, "DROP TRIGGER darkrestore_local_settlements_v1_no_update"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*DarkRestoreLocalAttachmentReceiptV1){
		func(r *DarkRestoreLocalAttachmentReceiptV1) { r.Settlement.SourceID = "other-source" },
		func(r *DarkRestoreLocalAttachmentReceiptV1) { r.OriginalEventID = "other-import-event" },
		func(r *DarkRestoreLocalAttachmentReceiptV1) {
			r.Settlement.PriorBytes = 6
			r.Settlement.Historical = 13
		},
	} {
		changed := receipt
		mutate(&changed)
		if _, err := f.s.db.ExecContext(ctx, "UPDATE darkrestore_local_settlements_v1 SET receipt_json=? WHERE client_id=?", encode(changed), id); err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.DarkRestoreUsageV1(ctx, id); !errors.Is(err, ErrUnavailable) {
			t.Fatal("mismatched source attachment was displayed", err)
		}
	}
}
