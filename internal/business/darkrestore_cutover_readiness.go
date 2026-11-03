package business

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"time"
)

type DarkRestoreCutoverReadinessV1 struct {
	ClientID                    string   `json:"client_id"`
	SourceSHA256                string   `json:"source_sha256"`
	SourceID                    string   `json:"source_id"`
	SourceScope                 string   `json:"source_scope"`
	SourceEpochSettled          bool     `json:"source_epoch_settled"`
	GlobalSourceCustodyVerified bool     `json:"global_source_custody_verified"`
	NativeBaselineComplete      bool     `json:"native_baseline_complete"`
	SubscriptionDelivered       bool     `json:"subscription_delivered"`
	ClientArchived              bool     `json:"client_archived"`
	ClientExpired               bool     `json:"client_expired"`
	FiniteQuotaRemainingBytes   string   `json:"finite_quota_remaining_bytes"`
	ActivationAllowed           bool     `json:"activation_allowed"`
	CutoverVerified             bool     `json:"cutover_verified"`
	ActivationBlockers          []string `json:"activation_blockers"`
	VerificationBlockers        []string `json:"verification_blockers"`
}

// DarkRestoreCutoverReadinessV1 is evidence only. It never activates a client,
// completes a baseline, publishes a subscription or edits representative credit.
func (s *Store) DarkRestoreCutoverReadinessV1(ctx context.Context, sourceSHA, sourceID string) (DarkRestoreCutoverReadinessV1, error) {
	out := DarkRestoreCutoverReadinessV1{
		SourceSHA256: sourceSHA, SourceID: sourceID,
		SourceScope:               "single_local_source_epoch",
		FiniteQuotaRemainingBytes: "0",
		ActivationBlockers:        []string{}, VerificationBlockers: []string{},
	}
	if len(sourceSHA) != 64 || sourceID == "" {
		return out, ErrInvalid
	}
	present, err := darkRestoreSchemaState(ctx, s.db)
	if err != nil || !present {
		return out, ErrUnavailable
	}
	err = s.db.QueryRowContext(ctx,
		"SELECT client_id FROM darkrestore_import_entries_v1 WHERE source_sha256=? AND source_id=?",
		sourceSHA, sourceID).Scan(&out.ClientID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	client, err := s.Client(ctx, out.ClientID)
	if err != nil {
		return out, err
	}
	out.ClientArchived = client.Archived
	out.ClientExpired = client.ExpiresUnix <= time.Now().Unix()
	account, err := s.ClientUsage(ctx, out.ClientID)
	if err != nil {
		return out, err
	}
	out.NativeBaselineComplete = account.Known
	attachment, err := readDarkRestoreLocalAttachment(ctx, s.db, out.ClientID)
	if err != nil {
		return out, err
	}
	if attachment != nil {
		if attachment.Settlement.SourceSHA != sourceSHA || attachment.Settlement.SourceID != sourceID ||
			attachment.Settlement.Remaining < 0 {
			return out, ErrUnavailable
		}
		out.SourceEpochSettled = true
		out.FiniteQuotaRemainingBytes = big.NewInt(attachment.Settlement.Remaining).String()
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	delivery, err := readSubscriptionDelivery(ctx, tx, out.ClientID)
	tx.Rollback()
	if err != nil {
		return out, err
	}
	out.SubscriptionDelivered = delivery.Enabled && delivery.Revision > 0
	if !out.SourceEpochSettled {
		out.ActivationBlockers = append(out.ActivationBlockers, "source_epoch_not_settled")
	}
	// A signed local handoff proves one epoch only. No current receipt proves
	// that every other installation able to serve this UUID has retired.
	out.ActivationBlockers = append(out.ActivationBlockers, "global_source_custody_not_proven")
	if out.ClientArchived {
		out.ActivationBlockers = append(out.ActivationBlockers, "client_archived")
	}
	if out.ClientExpired {
		out.ActivationBlockers = append(out.ActivationBlockers, "client_expired")
	}
	remaining, ok := new(big.Int).SetString(out.FiniteQuotaRemainingBytes, 10)
	if !ok || remaining.Sign() <= 0 {
		out.ActivationBlockers = append(out.ActivationBlockers, "finite_quota_exhausted")
	}
	if !out.NativeBaselineComplete {
		out.VerificationBlockers = append(out.VerificationBlockers, "native_baseline_incomplete")
	}
	if !out.SubscriptionDelivered {
		out.VerificationBlockers = append(out.VerificationBlockers, "subscription_not_delivered")
	}
	out.ActivationAllowed = len(out.ActivationBlockers) == 0
	out.CutoverVerified = out.ActivationAllowed && len(out.VerificationBlockers) == 0
	return out, nil
}
