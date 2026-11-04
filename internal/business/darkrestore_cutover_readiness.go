package business

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"strconv"
	"time"
)

type DarkRestoreCutoverReadinessV1 struct {
	SubscriptionDeliveryScope            string   `json:"subscription_delivery_scope,omitempty"`
	SettledHistoryBudgetReserved         bool     `json:"settled_history_budget_reserved"`
	ClientID                             string   `json:"client_id"`
	SourceSHA256                         string   `json:"source_sha256"`
	SourceID                             string   `json:"source_id"`
	SourceScope                          string   `json:"source_scope"`
	SourceEpochSettled                   bool     `json:"source_epoch_settled"`
	DeclaredInventoryCoverageVerified    bool     `json:"declared_inventory_coverage_verified"`
	IndependentCoverageAssertionVerified bool     `json:"independent_coverage_assertion_verified"`
	GlobalSourceCustodyVerified          bool     `json:"global_source_custody_verified"`
	NativeBaselineComplete               bool     `json:"native_baseline_complete"`
	NativeApplyRecorded                  bool     `json:"native_apply_recorded"`
	InitialApplySubscriptionDelivered    bool     `json:"initial_apply_subscription_delivered"`
	NativeActivationAuthorized           bool     `json:"native_activation_authorized"`
	SubscriptionDelivered                bool     `json:"subscription_delivered"`
	CutoverReceiptRecorded               bool     `json:"cutover_receipt_recorded"`
	ClientArchived                       bool     `json:"client_archived"`
	ClientExpired                        bool     `json:"client_expired"`
	FiniteQuotaRemainingBytes            string   `json:"finite_quota_remaining_bytes"`
	ActivationAllowed                    bool     `json:"activation_allowed"`
	CutoverVerified                      bool     `json:"cutover_verified"`
	ActivationBlockers                   []string `json:"activation_blockers"`
	VerificationBlockers                 []string `json:"verification_blockers"`
}

func darkRestoreSubscriptionDeliveredAfterSettlement(attachment *DarkRestoreLocalAttachmentReceiptV1, delivery subscriptionDeliveryHead) bool {
	// Delivery timestamps are recorded by the authoritative snapshot journal in
	// Unix nanoseconds. A pre-settlement or simultaneous record is not evidence
	// that the destination configuration was delivered after the source closed.
	return attachment != nil && delivery.Enabled && delivery.Revision > 0 &&
		delivery.Created > attachment.CreatedUnixNano
}

// DarkRestoreCutoverReadinessV1 is evidence only. It never activates a client,
// publishes a subscription or edits representative credit. Global custody stays
// false because even a valid completeness assertion is not mathematical proof
// that an undeclared production source does not exist.
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
		if attachment.Settlement.SourceSHA != sourceSHA ||
			attachment.Settlement.SourceID != sourceID ||
			attachment.Settlement.Remaining < 0 {
			return out, ErrUnavailable
		}
		out.SourceEpochSettled = true
		out.FiniteQuotaRemainingBytes = strconv.FormatInt(attachment.Settlement.Remaining, 10)
	}
	evidence, err := readCutoverEvidence(ctx, s.db, out.ClientID)
	if err != nil {
		return out, err
	}
	if evidence != nil {
		out.SettledHistoryBudgetReserved = verifyCutoverHistoricalBudget(ctx, s.db, evidence) == nil
		if len(evidence.NativeDocument) == 0 {
			out.NativeBaselineComplete = false
		}
		out.SourceScope = "pinned_inventory_plus_independent_coverage_assertion"
		out.SourceEpochSettled = true
		out.DeclaredInventoryCoverageVerified = true
		out.IndependentCoverageAssertionVerified = true
		remaining, budgetErr := cutoverRemaining(client.VolumeBytes, evidence.SettledHistoricalBytes, account.Uplink, account.Downlink)
		if budgetErr == nil {
			out.FiniteQuotaRemainingBytes = strconv.FormatInt(remaining, 10)
		}
	}
	final, err := readCutoverReceipt(ctx, s.db, out.ClientID)
	if err != nil {
		return out, err
	}
	out.CutoverReceiptRecorded = final != nil

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	delivery, err := readSubscriptionDelivery(ctx, tx, out.ClientID)
	if err != nil {
		tx.Rollback()
		return out, err
	}
	if evidence != nil {
		out.SubscriptionDelivered = delivery.Enabled && delivery.Revision > 0 &&
			delivery.Created > evidence.TransitionOpenedUnixNano
	} else {
		out.SubscriptionDelivered = darkRestoreSubscriptionDeliveredAfterSettlement(attachment, delivery)
	}

	preparedAwaitingNativeApply := false
	preparedAwaitingInitialDelivery := false
	preparedAwaitingNativeActivation := false
	if final != nil {
		out.SubscriptionDeliveryScope = final.SubscriptionDeliveryScope
		if final.SubscriptionDeliveryScope == DarkRestorePreparedDeliveryScopeV1 {
			preparedValid := evidence != nil && final.TransitionSHA == evidence.TransitionSHA
			if preparedValid && client.DeploymentState == "disabled" {
				preparedValid = verifyPreparedPlanReceipt(ctx, tx, final) == nil
			} else if preparedValid {
				// The pre-promotion authority is intentionally no longer current.
				// Require the immutable reviewed promotion that replaced it.
				promotion, promotionErr := readDarkRestorePreparedPromotion(ctx, tx, out.ClientID)
				if promotionErr != nil {
					tx.Rollback()
					return out, promotionErr
				}
				preparedValid = promotion != nil && promotion.TransitionSHA == final.TransitionSHA &&
					promotion.PreparedAuthoritySHA == final.Prepared.AuthoritySHA && promotion.PreparedContentSHA == final.Prepared.ContentSHA
			}
			if preparedValid {
				acceptance, acceptanceErr := currentDarkRestoreNativeApplyAcceptance(ctx, tx, out.ClientID)
				if acceptanceErr != nil {
					tx.Rollback()
					return out, acceptanceErr
				}
				out.NativeApplyRecorded = acceptance != nil
				if acceptance != nil {
					_, _, deliveryErr := readDarkRestoreInitialDeliveryAfterAcceptance(ctx, tx, out.ClientID, acceptance)
					out.InitialApplySubscriptionDelivered = deliveryErr == nil
					if deliveryErr == nil {
						activation, activationErr := currentDarkRestoreNativeActivation(ctx, tx, out.ClientID)
						if activationErr != nil {
							tx.Rollback()
							return out, activationErr
						}
						out.NativeActivationAuthorized = activation != nil
					}
				}
			}
			// Preserve the existing meaning: the immutable owner-prepared export
			// was delivered. The separate field records whether a real post-Apply
			// subscription was subsequently materialized.
			out.SubscriptionDelivered = preparedValid
			preparedAwaitingNativeApply = preparedValid && !out.NativeApplyRecorded
			preparedAwaitingInitialDelivery = out.NativeApplyRecorded && !out.InitialApplySubscriptionDelivered
			preparedAwaitingNativeActivation = out.InitialApplySubscriptionDelivered && !out.NativeActivationAuthorized
		}
	}
	tx.Rollback()
	if evidence != nil && client.DeploymentState == "disabled" {
		current, readErr := s.DarkRestoreCutoverSnapshotV1(ctx, sourceSHA, sourceID)
		if readErr != nil {
			return out, readErr
		}
		effectiveStateSHA, stateErr := effectiveCutoverStateSHA(ctx, s.db, evidence)
		if stateErr != nil {
			return out, stateErr
		}
		if current.StateSHA != effectiveStateSHA {
			out.ActivationBlockers = append(out.ActivationBlockers, "approved_destination_state_changed")
		}
	}
	if preparedAwaitingNativeApply {
		// A canonical prepared export proves what the owner downloaded; it does
		// not prove that the exact plan/client binding was promoted or applied.
		out.ActivationBlockers = append(out.ActivationBlockers, "native_apply_not_recorded")
	}
	if preparedAwaitingInitialDelivery {
		out.ActivationBlockers = append(out.ActivationBlockers, "initial_apply_subscription_not_delivered")
	}
	if preparedAwaitingNativeActivation {
		// Delivery is real and bound to the accepted Apply, but activation is a
		// separate owner-controlled transition that is not authorized here.
		out.ActivationBlockers = append(out.ActivationBlockers, "native_activation_not_authorized")
	}
	if !out.SourceEpochSettled {
		out.ActivationBlockers = append(out.ActivationBlockers, "source_epoch_not_settled")
	}
	if !out.DeclaredInventoryCoverageVerified {
		out.ActivationBlockers = append(out.ActivationBlockers, "declared_inventory_coverage_not_verified")
	}
	if !out.IndependentCoverageAssertionVerified {
		out.ActivationBlockers = append(out.ActivationBlockers, "independent_coverage_assertion_not_verified")
	}
	if evidence != nil && !out.SettledHistoryBudgetReserved {
		out.ActivationBlockers = append(out.ActivationBlockers, "settled_history_budget_not_reserved")
	}
	if out.ClientArchived {
		out.ActivationBlockers = append(out.ActivationBlockers, "client_archived")
	}
	if out.ClientExpired {
		out.ActivationBlockers = append(out.ActivationBlockers, "client_expired")
	}
	remaining, ok := new(big.Int).SetString(out.FiniteQuotaRemainingBytes, 10)
	if client.PlanKind != "volume" || !ok || remaining.Sign() <= 0 {
		out.ActivationBlockers = append(out.ActivationBlockers, "finite_quota_exhausted")
	}
	if !out.NativeBaselineComplete {
		out.VerificationBlockers = append(out.VerificationBlockers, "native_baseline_incomplete")
	}
	if !out.SubscriptionDelivered {
		out.VerificationBlockers = append(out.VerificationBlockers, "subscription_not_delivered")
	}
	if !out.CutoverReceiptRecorded {
		out.VerificationBlockers = append(out.VerificationBlockers, "cutover_receipt_not_recorded")
	}
	out.ActivationAllowed = len(out.ActivationBlockers) == 0 && len(out.VerificationBlockers) == 0
	out.CutoverVerified = out.ActivationAllowed
	return out, nil
}
