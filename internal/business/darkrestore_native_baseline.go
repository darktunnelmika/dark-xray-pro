package business

import (
	"context"
	"database/sql"
	"math"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
)

// This is a projection for an approval request, never an assertion that the
// account's baseline has already been completed in the destination database.
type DarkRestoreNativeBaselinePreparationV1 struct {
	BeforeStateSHA    string                       `json:"before_state_sha256"`
	ProjectedSnapshot DarkRestoreCutoverSnapshotV1 `json:"projected_snapshot"`
}

func projectAttestedNativeBaselineV1(before DarkRestoreCutoverSnapshotV1, verified darkrestore.VerifiedNativeBaselineV1, pins darkrestore.NativeBaselinePinsV1, now int64) (DarkRestoreCutoverSnapshotV1, error) {
	proof, _, _, actualPins, err := verified.Evidence()
	if err != nil || actualPins != pins || !before.NativeAccountPresent || before.NativeBaselineComplete ||
		before.PlanKind != "volume" || before.DeploymentState != "disabled" || before.Archived ||
		before.NativeUplink == nil || before.NativeDownlink == nil || before.NativeHolds == nil || *before.NativeHolds != 0 ||
		before.ClientCreatedUnix <= 0 || before.ClientCreatedUnix > math.MaxInt64/1_000_000_000 ||
		before.ExpiresUnix <= now/1_000_000_000 ||
		proof.ClientID != before.ClientID || proof.ClientUUID != before.ClientUUID ||
		proof.SourceSHA != before.SourceSHA || proof.SourceID != before.SourceID ||
		proof.ImportReceiptSHA != before.ImportReceiptSHA ||
		proof.BeforeStateSHA != before.StateSHA || proof.BeforeNativeSHA != before.NativeBaselineSHA ||
		proof.Uplink != *before.NativeUplink || proof.Downlink != *before.NativeDownlink ||
		proof.CoverageFromNano > before.ClientCreatedUnix*1_000_000_000 ||
		proof.MeasuredThroughNano < before.ClientCreatedUnix*1_000_000_000 ||
		proof.MeasuredThroughNano > now || proof.FenceUntilNano <= now ||
		proof.FenceUntilNano-proof.MeasuredThroughNano > int64(5*time.Minute) {
		return DarkRestoreCutoverSnapshotV1{}, ErrDarkRestoreCutover
	}
	// No counter, entitlement, payer or history is changed. The actual flag can
	// change only in the same transaction that persists the signed evidence.
	before.NativeBaselineComplete = true
	return hashDarkRestoreCutoverSnapshotV1(before)
}

func (s *Store) PrepareDarkRestoreNativeBaselineV1(ctx context.Context, token, sourceSHA, sourceID string, native darkrestore.VerifiedNativeBaselineV1, pins darkrestore.NativeBaselinePinsV1) (DarkRestoreNativeBaselinePreparationV1, error) {
	var zero DarkRestoreNativeBaselinePreparationV1
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return zero, err
	}
	present, err := darkRestoreCutoverSchemaState(ctx, tx)
	if err != nil || !present {
		return zero, ErrUnavailable
	}
	before, err := readDarkRestoreCutoverSnapshotV1(ctx, tx, sourceSHA, sourceID)
	if err != nil {
		return zero, err
	}
	projected, err := projectAttestedNativeBaselineV1(before, native, pins, time.Now().UnixNano())
	if err != nil {
		return zero, err
	}
	return DarkRestoreNativeBaselinePreparationV1{BeforeStateSHA: before.StateSHA, ProjectedSnapshot: projected}, nil
}
