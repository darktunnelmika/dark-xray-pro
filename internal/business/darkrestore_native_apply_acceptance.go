package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

const DarkRestoreNativeApplyAcceptanceScopeV1 = "darkrestore_native_apply_acceptance_v1"

type DarkRestoreNativeApplyAcceptanceInputV1 struct {
	EventID   string                       `json:"event_id"`
	ClientID  string                       `json:"client_id"`
	InboundID uint64                       `json:"inbound_id"`
	RoundID   string                       `json:"round_id"`
	Pins      DarkRestoreNativeApplyPinsV1 `json:"pins"`
}

type DarkRestoreNativeApplyAcceptanceReceiptV1 struct {
	Scope                     string                           `json:"scope"`
	EventID                   string                           `json:"event_id"`
	ActorUserID               string                           `json:"actor_user_id"`
	ClientID                  string                           `json:"client_id"`
	InboundID                 uint64                           `json:"inbound_id"`
	TransitionSHA             string                           `json:"transition_sha256"`
	EvidenceSHA               string                           `json:"evidence_sha256"`
	RequestSHA                string                           `json:"request_sha256"`
	Evidence                  DarkRestoreNativeApplyEvidenceV1 `json:"evidence"`
	RecordedUnixNano          int64                            `json:"recorded_unix_nano"`
	NativeApplyRecorded       bool                             `json:"native_apply_recorded"`
	SubscriptionDelivered     bool                             `json:"subscription_delivered"`
	SubscriptionActivated     bool                             `json:"subscription_activated"`
	AuthorizesFurtherMutation bool                             `json:"authorizes_further_mutation"`
	Replayed                  bool                             `json:"replayed"`
}

func nativeApplyAcceptanceRequestSHA(in DarkRestoreNativeApplyAcceptanceInputV1) string {
	return fingerprint("darkrestore-native-apply-acceptance-request-v1", in)
}

func nativeApplyPinsFromEvidence(e DarkRestoreNativeApplyEvidenceV1, documentSHA string) DarkRestoreNativeApplyPinsV1 {
	return DarkRestoreNativeApplyPinsV1{
		ClientID: e.ClientID, TransitionSHA: e.TransitionSHA, PreparedContentSHA: e.PreparedContentSHA,
		PreparedAuthoritySHA: e.PreparedAuthoritySHA, InboundID: e.InboundID, PlanRevision: e.PlanRevision,
		PreparedInboundRevision: e.PreparedInboundRevision, PromotionEventID: e.PromotionEventID,
		PromotionReceiptSHA: e.PromotionReceiptSHA, InitialApplySHA: e.InitialApplySHA,
		ApplyCommandID: e.ApplyCommandID, StageID: e.StageID, HealthRoundID: e.HealthRoundID,
		HealthReceiptSHA: e.HealthReceiptSHA, RepresentativeID: e.RepresentativeID,
		ImportReceiptSHA: e.ImportReceiptSHA, NativeBaselineSHA: e.NativeBaselineSHA,
		CurrentFiniteEntitlement: e.CurrentFiniteEntitlement, SettledHistoricalBytes: e.SettledHistoricalBytes,
		NativePostMigrationBytes: e.NativePostMigrationBytes, RemainingBudgetBytes: e.RemainingBudgetBytes,
		DocumentSHA: documentSHA,
	}
}

func readDarkRestoreNativeApplyAcceptance(ctx context.Context, q sellerQueryV2, client string) (*DarkRestoreNativeApplyAcceptanceReceiptV1, error) {
	present, err := darkRestoreNativeApplyAcceptanceSchema(ctx, q)
	if err != nil || !present {
		return nil, err
	}
	var raw []byte
	var inbound uint64
	var event, actor, transition, evidenceSHA, requestSHA string
	var recorded int64
	err = q.QueryRowContext(ctx, `SELECT inbound_id,event_id,actor_user_id,transition_sha256,evidence_sha256,request_sha256,receipt_json,recorded_unix_nano FROM darkrestore_native_apply_acceptances_v1 WHERE client_id=?`, client).
		Scan(&inbound, &event, &actor, &transition, &evidenceSHA, &requestSHA, &raw, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out DarkRestoreNativeApplyAcceptanceReceiptV1
	if len(raw) > 262144 || json.Unmarshal(raw, &out) != nil {
		return nil, ErrUnavailable
	}
	canonical, _ := json.Marshal(out)
	if !reflect.DeepEqual(canonical, raw) || out.ClientID != client || out.InboundID != inbound || out.EventID != event || out.ActorUserID != actor || out.TransitionSHA != transition || out.EvidenceSHA != evidenceSHA || out.RequestSHA != requestSHA || out.RecordedUnixNano != recorded {
		return nil, ErrUnavailable
	}
	return &out, nil
}

// RecordDarkRestoreNativeApplyAcceptanceV1 persists the independently pinned
// point-in-time projection in the same transaction that re-derives it. It does
// not issue or deliver a subscription and grants no activation authority.
func (s *Store) RecordDarkRestoreNativeApplyAcceptanceV1(ctx context.Context, key []byte, token string, in DarkRestoreNativeApplyAcceptanceInputV1) (DarkRestoreNativeApplyAcceptanceReceiptV1, error) {
	var zero DarkRestoreNativeApplyAcceptanceReceiptV1
	if !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.ClientID) || in.InboundID == 0 || in.InboundID >= routeMax || !routeID.MatchString(in.RoundID) {
		return zero, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	requestSHA := nativeApplyAcceptanceRequestSHA(in)
	if prior, e := readDarkRestoreNativeApplyAcceptance(ctx, tx, in.ClientID); e != nil {
		return zero, e
	} else if prior != nil {
		if prior.ActorUserID != actor || prior.EventID != in.EventID || prior.InboundID != in.InboundID || prior.RequestSHA != requestSHA {
			return zero, ErrConflict
		}
		prior.Replayed = true
		return *prior, tx.Commit()
	}
	now := time.Now().UnixNano()
	evidence, err := s.projectDarkRestoreNativeApplyEvidenceV1(ctx, tx, key, in.ClientID, in.InboundID, in.RoundID, in.Pins, now)
	if err != nil {
		return zero, err
	}
	validated, err := ValidateDarkRestoreNativeApplyEvidenceV1(evidence, in.Pins, now)
	if err != nil || !validated.NativeApplyRecorded || !validated.HealthAccepted || validated.AuthorizesMutation || validated.EvidenceSHA != in.Pins.DocumentSHA {
		return zero, ErrDarkRestoreCutover
	}
	out := DarkRestoreNativeApplyAcceptanceReceiptV1{
		Scope: DarkRestoreNativeApplyAcceptanceScopeV1, EventID: in.EventID, ActorUserID: actor,
		ClientID: in.ClientID, InboundID: in.InboundID, TransitionSHA: evidence.TransitionSHA,
		EvidenceSHA: validated.EvidenceSHA, RequestSHA: requestSHA, Evidence: evidence,
		RecordedUnixNano: now, NativeApplyRecorded: true,
	}
	if out.RecordedUnixNano <= evidence.HealthAcceptedUnixNano || out.SubscriptionDelivered || out.SubscriptionActivated || out.AuthorizesFurtherMutation {
		return zero, ErrDarkRestoreCutover
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return zero, err
	}
	if err = ensureDarkRestoreNativeApplyAcceptanceSchema(ctx, tx); err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO darkrestore_native_apply_acceptances_v1(client_id,inbound_id,event_id,actor_user_id,transition_sha256,evidence_sha256,request_sha256,receipt_json,recorded_unix_nano) VALUES(?,?,?,?,?,?,?,?,?)`, out.ClientID, out.InboundID, out.EventID, out.ActorUserID, out.TransitionSHA, out.EvidenceSHA, out.RequestSHA, raw, out.RecordedUnixNano); err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}

func validateDarkRestoreNativeApplyAcceptances(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreNativeApplyAcceptanceSchema(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT client_id FROM darkrestore_native_apply_acceptances_v1 ORDER BY client_id`)
	if err != nil {
		return ErrUnavailable
	}
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return ErrUnavailable
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		rows.Close()
		return ErrUnavailable
	}
	rows.Close()
	for _, id := range ids {
		r, e := readDarkRestoreNativeApplyAcceptance(ctx, tx, id)
		if e != nil || r == nil || r.Scope != DarkRestoreNativeApplyAcceptanceScopeV1 || r.ClientID != id || !r.NativeApplyRecorded || r.SubscriptionDelivered || r.SubscriptionActivated || r.AuthorizesFurtherMutation || r.Replayed || r.RecordedUnixNano <= r.Evidence.HealthAcceptedUnixNano {
			return ErrUnavailable
		}
		pins := nativeApplyPinsFromEvidence(r.Evidence, r.EvidenceSHA)
		v, e := ValidateDarkRestoreNativeApplyEvidenceV1(r.Evidence, pins, r.RecordedUnixNano)
		if e != nil || v.EvidenceSHA != r.EvidenceSHA || r.TransitionSHA != r.Evidence.TransitionSHA || r.InboundID != r.Evidence.InboundID {
			return ErrUnavailable
		}
		promotion, e := readDarkRestorePreparedPromotion(ctx, tx, id)
		if e != nil || promotion == nil || promotion.TransitionSHA != r.TransitionSHA || promotion.InboundID != r.InboundID || promotion.PromotionReceiptSHA != r.Evidence.PromotionReceiptSHA || promotion.CreatedUnixNano != r.Evidence.PromotedUnixNano {
			return ErrUnavailable
		}
		round, e := readInitialHealthRound(ctx, tx, r.Evidence.HealthRoundID)
		if e != nil || round.receipt == nil || meteredHash(*round.receipt) != r.Evidence.HealthReceiptSHA || len(round.Members) != 1 || round.Members[0].Accepted != r.Evidence.HealthAcceptedUnixNano {
			return ErrUnavailable
		}
	}
	return nil
}

func currentDarkRestoreNativeApplyAcceptance(ctx context.Context, q sellerQueryV2, client string) (*DarkRestoreNativeApplyAcceptanceReceiptV1, error) {
	r, err := readDarkRestoreNativeApplyAcceptance(ctx, q, client)
	if err != nil || r == nil {
		return nil, err
	}
	evidence, err := readCutoverEvidence(ctx, q, client)
	if err != nil || evidence == nil || evidence.TransitionSHA != r.TransitionSHA || verifyCutoverHistoricalBudget(ctx, q, evidence) != nil {
		return nil, ErrDarkRestoreCutover
	}
	current, err := readDarkRestoreCutoverSnapshotV1(ctx, q, evidence.SourceSHA256, evidence.SourceID)
	if err != nil || current.ClientID != client || current.DeploymentState != "pending_node_apply" || current.Archived ||
		current.NativeUplink == nil || current.NativeDownlink == nil || current.NativeHolds == nil || *current.NativeHolds != 0 {
		return nil, ErrDarkRestoreCutover
	}
	remaining, err := cutoverRemaining(current.CurrentEntitlement, evidence.SettledHistoricalBytes, *current.NativeUplink, *current.NativeDownlink)
	if err != nil || r.Evidence.RepresentativeID != current.RepresentativeID || r.Evidence.ImportReceiptSHA != current.ImportReceiptSHA ||
		r.Evidence.NativeBaselineSHA != current.NativeBaselineSHA || r.Evidence.CurrentFiniteEntitlement != current.CurrentEntitlement ||
		r.Evidence.SettledHistoricalBytes != evidence.SettledHistoricalBytes ||
		r.Evidence.NativePostMigrationBytes != *current.NativeUplink+*current.NativeDownlink || r.Evidence.RemainingBudgetBytes != remaining {
		return nil, ErrDarkRestoreCutover
	}
	return r, nil
}

func readDarkRestoreInitialDeliveryAfterAcceptance(ctx context.Context, tx *sql.Tx, client string, acceptance *DarkRestoreNativeApplyAcceptanceReceiptV1) (subscriptionDeliveryHead, subscriptionSnapshotRow, error) {
	var empty subscriptionSnapshotRow
	if acceptance == nil || acceptance.ClientID != client || !acceptance.NativeApplyRecorded || acceptance.SubscriptionDelivered || acceptance.SubscriptionActivated || acceptance.AuthorizesFurtherMutation {
		return subscriptionDeliveryHead{}, empty, ErrDarkRestoreCutover
	}
	delivery, err := readSubscriptionDelivery(ctx, tx, client)
	if err != nil || !delivery.Enabled || delivery.Scope != InitialApplyHealthScope || delivery.Revision == 0 || delivery.Created <= acceptance.RecordedUnixNano {
		return delivery, empty, ErrDarkRestoreCutover
	}
	row, err := readInitialSubscriptionSnapshot(ctx, tx, "id", delivery.SnapshotID)
	if err != nil || row.ClientID != client || row.InboundID != acceptance.InboundID || row.RoundID != acceptance.Evidence.HealthRoundID ||
		row.InitialApplySHA != acceptance.Evidence.InitialApplySHA || row.PromotionEventID != acceptance.Evidence.PromotionEventID ||
		row.BodySHA != acceptance.Evidence.PreparedContentSHA || row.EventID != delivery.EventID || row.Revision != delivery.SnapshotRevision ||
		row.CreatedUnixNano != delivery.Created || row.CreatedUnixNano <= acceptance.RecordedUnixNano || row.ExpiresUnixNano <= time.Now().UnixNano() {
		return delivery, empty, ErrDarkRestoreCutover
	}
	return delivery, row, nil
}
