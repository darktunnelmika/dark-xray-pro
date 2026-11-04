package business

import (
	"context"
	"database/sql"
	"time"
)

type DarkRestoreNativeApplyReviewInputV1 struct {
	EventID   string
	ClientID  string
	InboundID uint64
	RoundID   string
}
type DarkRestoreNativeApplyReviewV1 struct {
	Scope              string                           `json:"scope"`
	EventID            string                           `json:"event_id"`
	ReviewSHA          string                           `json:"review_sha256"`
	EvidenceSHA        string                           `json:"evidence_sha256"`
	Evidence           DarkRestoreNativeApplyEvidenceV1 `json:"evidence"`
	AuthorizesMutation bool                             `json:"authorizes_mutation"`
}

func nativeApplyReviewSHA(actor string, in DarkRestoreNativeApplyReviewInputV1, evidenceSHA string) string {
	return fingerprint("darkrestore-owner-apply-review-v1", struct {
		Actor       string
		Input       DarkRestoreNativeApplyReviewInputV1
		EvidenceSHA string
	}{actor, in, evidenceSHA})
}
func validNativeApplyReview(in DarkRestoreNativeApplyReviewInputV1) bool {
	return eventRE.MatchString(in.EventID) && idRE.MatchString(in.ClientID) && in.InboundID > 0 && in.InboundID < routeMax && routeID.MatchString(in.RoundID)
}

// Preview obtains the reviewed document from authenticated persisted receipts.
// This adapter accepts no caller-supplied accounting or success assertions.
// The original independently pinned projection API retains its strict checks.
func (s *Store) PreviewDarkRestoreNativeApplyReviewV1(ctx context.Context, key []byte, token string, in DarkRestoreNativeApplyReviewInputV1) (DarkRestoreNativeApplyReviewV1, error) {
	var zero DarkRestoreNativeApplyReviewV1
	if !validNativeApplyReview(in) {
		return zero, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	evidence, err := s.deriveDarkRestoreNativeApplyEvidenceV1(ctx, tx, key, in.ClientID, in.InboundID, in.RoundID, time.Now().UnixNano())
	if err != nil {
		return zero, err
	}
	sha, err := cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", evidence)
	if err != nil {
		return zero, err
	}
	if _, err = ValidateDarkRestoreNativeApplyEvidenceV1(evidence, nativeApplyPinsFromEvidence(evidence, sha), time.Now().UnixNano()); err != nil {
		return zero, err
	}
	return DarkRestoreNativeApplyReviewV1{Scope: "darkrestore_owner_apply_review_v1", EventID: in.EventID, ReviewSHA: nativeApplyReviewSHA(actor, in, sha), EvidenceSHA: sha, Evidence: evidence}, nil
}

// Commit freezes the reviewed pins before the existing recorder re-derives the
// full snapshot in its write transaction. A state change between transactions
// is rejected by that recorder. Lost responses replay the immutable receipt.
func (s *Store) CommitDarkRestoreNativeApplyReviewV1(ctx context.Context, key []byte, token string, in DarkRestoreNativeApplyReviewInputV1, reviewSHA string) (DarkRestoreNativeApplyAcceptanceReceiptV1, error) {
	var zero DarkRestoreNativeApplyAcceptanceReceiptV1
	if !validNativeApplyReview(in) || !routeSHA.MatchString(reviewSHA) {
		return zero, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		tx.Rollback()
		return zero, err
	}
	prior, err := readDarkRestoreNativeApplyAcceptance(ctx, tx, in.ClientID)
	tx.Rollback()
	if err != nil {
		return zero, err
	}
	if prior != nil {
		if prior.ActorUserID != actor || prior.EventID != in.EventID || prior.InboundID != in.InboundID || prior.Evidence.HealthRoundID != in.RoundID || reviewSHA != nativeApplyReviewSHA(actor, in, prior.EvidenceSHA) {
			return zero, ErrConflict
		}
		prior.Replayed = true
		return *prior, nil
	}
	preview, err := s.PreviewDarkRestoreNativeApplyReviewV1(ctx, key, token, in)
	if err != nil {
		return zero, err
	}
	if preview.ReviewSHA != reviewSHA {
		return zero, ErrConflict
	}
	return s.RecordDarkRestoreNativeApplyAcceptanceV1(ctx, key, token, DarkRestoreNativeApplyAcceptanceInputV1{EventID: in.EventID, ClientID: in.ClientID, InboundID: in.InboundID, RoundID: in.RoundID, Pins: nativeApplyPinsFromEvidence(preview.Evidence, preview.EvidenceSHA)})
}
func (s *Store) DarkRestoreNativeApplyAcceptanceReceipt(ctx context.Context, token, client string) (DarkRestoreNativeApplyAcceptanceReceiptV1, error) {
	var zero DarkRestoreNativeApplyAcceptanceReceiptV1
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	if _, err = ownershipOwner(ctx, tx, token); err != nil {
		return zero, err
	}
	if !idRE.MatchString(client) {
		return zero, ErrInvalid
	}
	receipt, err := readDarkRestoreNativeApplyAcceptance(ctx, tx, client)
	if err != nil {
		return zero, err
	}
	if receipt == nil {
		return zero, ErrNotFound
	}
	return *receipt, nil
}
