package business

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

// ProjectDarkRestoreNativeApplyEvidenceV1 is an owner-only, read-only adapter.
// It re-derives every field from one SQLite snapshot and then requires the
// independently supplied pins. It records nothing and cannot enable a client.
func (s *Store) ProjectDarkRestoreNativeApplyEvidenceV1(ctx context.Context, key []byte, token, client string, inbound uint64, roundID string, pins DarkRestoreNativeApplyPinsV1) (DarkRestoreNativeApplyEvidenceV1, error) {
	var zero DarkRestoreNativeApplyEvidenceV1
	if !idRE.MatchString(client) || inbound == 0 || inbound >= routeMax || !routeID.MatchString(roundID) {
		return zero, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return zero, err
	}
	out, err := s.projectDarkRestoreNativeApplyEvidenceV1(ctx, tx, key, client, inbound, roundID, pins, time.Now().UnixNano())
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}

func (s *Store) projectDarkRestoreNativeApplyEvidenceV1(ctx context.Context, tx *sql.Tx, key []byte, client string, inbound uint64, roundID string, pins DarkRestoreNativeApplyPinsV1, now int64) (DarkRestoreNativeApplyEvidenceV1, error) {
	var zero DarkRestoreNativeApplyEvidenceV1
	final, err := readCutoverReceipt(ctx, tx, client)
	if err != nil || final == nil || final.Prepared.InboundID != inbound {
		return zero, ErrDarkRestoreCutover
	}
	evidence, err := readCutoverEvidence(ctx, tx, client)
	if err != nil || evidence == nil || evidence.TransitionSHA != final.TransitionSHA || verifyCutoverHistoricalBudget(ctx, tx, evidence) != nil {
		return zero, ErrDarkRestoreCutover
	}
	promotion, _, err := readNativeInboundPromotion(ctx, tx, key, inbound)
	if err != nil || promotion == nil || verifyPreparedPlanReceiptAtPromotion(ctx, tx, final, promotion) != nil {
		return zero, ErrDarkRestoreCutover
	}
	promotionReceiptSHA := meteredHash(*promotion)
	transition, err := readDarkRestorePreparedPromotion(ctx, tx, client)
	if err != nil || transition == nil || transition.Scope != DarkRestorePreparedPromotionScopeV1 ||
		transition.InboundID != inbound || transition.TransitionSHA != final.TransitionSHA ||
		transition.PreparedAuthoritySHA != final.Prepared.AuthoritySHA || transition.PreparedContentSHA != final.Prepared.ContentSHA ||
		transition.PlanRevision != promotion.PlanRevision || transition.PreparedInboundRevision != promotion.SourceInboundRevision ||
		transition.PromotedInboundRevision != promotion.InboundRevision || transition.PromotionEventID != promotion.EventID ||
		transition.PromotionReceiptSHA != promotionReceiptSHA || !transition.NativePromotionRecorded || transition.NativeApplyRecorded ||
		transition.SubscriptionActivated || transition.AuthorizesFurtherMutation || transition.Replayed {
		return zero, ErrDarkRestoreCutover
	}
	clients := append([]string(nil), promotion.ClientIDs...)
	sort.Strings(clients)
	if !sort.StringsAreSorted(promotion.ClientIDs) || !containsSortedClient(clients, client) {
		return zero, ErrDarkRestoreCutover
	}
	promotedAt, err := readNativeInboundPromotionUnixNano(ctx, tx, inbound, promotion.EventID, promotion.CreatedUnix)
	if err != nil || promotedAt != transition.CreatedUnixNano {
		return zero, err
	}

	// Promotion is the only expected state delta at this boundary. Reconstruct
	// the exact disabled pre-state and reject payer, entitlement, expiry,
	// counter, hold, receipt, archive or identity drift.
	current, err := readDarkRestoreCutoverSnapshotV1(ctx, tx, evidence.SourceSHA256, evidence.SourceID)
	if err != nil || current.ClientID != client || current.DeploymentState != "pending_node_apply" || current.Archived ||
		!current.NativeAccountPresent || !current.NativeBaselineComplete || current.NativeUplink == nil || current.NativeDownlink == nil || current.NativeHolds == nil || *current.NativeHolds != 0 {
		return zero, ErrDarkRestoreCutover
	}
	pre := current
	pre.DeploymentState = "disabled"
	pre, err = hashDarkRestoreCutoverSnapshotV1(pre)
	effective, stateErr := effectiveCutoverStateSHA(ctx, tx, evidence)
	remaining, budgetErr := cutoverRemaining(current.CurrentEntitlement, evidence.SettledHistoricalBytes, *current.NativeUplink, *current.NativeDownlink)
	if err != nil || stateErr != nil || budgetErr != nil || current.StateSHA != transition.PostStateSHA || pre.StateSHA != effective || pre.StateSHA != transition.PreStateSHA ||
		current.NativeBaselineSHA != evidence.NativeBaselineSHA || current.ImportReceiptSHA != evidence.ImportReceiptSHA ||
		final.CurrentFiniteEntitlement != current.CurrentEntitlement || final.NativePostMigrationBytes != *current.NativeUplink+*current.NativeDownlink || final.RemainingBudgetBytes != remaining {
		return zero, ErrDarkRestoreCutover
	}

	state, err := s.loadInitialApplySnapshot(ctx, tx, key, inbound, now)
	if err != nil || state.promotion.EventID != promotion.EventID || state.promotion.InboundRevision != promotion.InboundRevision ||
		state.base.ApplyCommandID == "" || state.base.StageID == "" || state.apply.view.Receipt == nil {
		return zero, ErrDarkRestoreCutover
	}
	round, err := readInitialHealthRound(ctx, tx, roundID)
	if err != nil || round.receipt == nil || len(round.Members) != 1 || round.Members[0].State != "accepted" ||
		round.InboundID != inbound || round.PromotionEventID != promotion.EventID ||
		round.InitialApplySHA != state.base.InitialApplySHA || round.ApplyCommandID != state.base.ApplyCommandID ||
		round.StageID != state.base.StageID {
		return zero, ErrDarkRestoreCutover
	}

	out := DarkRestoreNativeApplyEvidenceV1{
		Scope: DarkRestoreNativeApplyEvidenceScopeV1, ClientID: client,
		TransitionSHA: final.TransitionSHA, PreparedContentSHA: final.Prepared.ContentSHA,
		PreparedAuthoritySHA: final.Prepared.AuthoritySHA, InboundID: inbound,
		PlanRevision: promotion.PlanRevision, PreparedInboundRevision: promotion.SourceInboundRevision,
		PromotionEventID: promotion.EventID, PromotionReceiptSHA: promotionReceiptSHA,
		PromotedInboundRevision: promotion.InboundRevision, InitialApplySHA: state.base.InitialApplySHA,
		ApplyCommandID: state.base.ApplyCommandID, StageID: state.base.StageID,
		HealthRoundID: round.ID, HealthReceiptSHA: meteredHash(*round.receipt),
		RepresentativeID: current.RepresentativeID, ImportReceiptSHA: current.ImportReceiptSHA,
		NativeBaselineSHA: current.NativeBaselineSHA, CurrentFiniteEntitlement: current.CurrentEntitlement,
		SettledHistoricalBytes:   evidence.SettledHistoricalBytes,
		NativePostMigrationBytes: *current.NativeUplink + *current.NativeDownlink, RemainingBudgetBytes: remaining,
		PreparedUnixNano: final.SubscriptionDeliveredNano, PromotedUnixNano: promotedAt,
		AppliedUnixNano: state.apply.view.Receipt.ObservedAt, HealthAcceptedUnixNano: round.Members[0].Accepted,
	}
	if _, err = ValidateDarkRestoreNativeApplyEvidenceV1(out, pins, now); err != nil {
		return zero, err
	}
	return out, nil
}

func containsSortedClient(ids []string, id string) bool {
	i := sort.SearchStrings(ids, id)
	return i < len(ids) && ids[i] == id
}
