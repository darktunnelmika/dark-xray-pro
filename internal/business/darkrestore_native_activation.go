package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

const DarkRestoreNativeActivationScopeV1 = "darkrestore_native_activation_v1"

type DarkRestoreNativeActivationEvidenceV1 struct {
	Scope                    string `json:"scope"`
	ClientID                 string `json:"client_id"`
	TransitionSHA            string `json:"transition_sha256"`
	NativeApplyEvidenceSHA   string `json:"native_apply_evidence_sha256"`
	RepresentativeID         string `json:"representative_id"`
	CurrentFiniteEntitlement int64  `json:"current_finite_entitlement,string"`
	SettledHistoricalBytes   int64  `json:"settled_historical_bytes,string"`
	NativePostMigrationBytes int64  `json:"native_post_migration_bytes,string"`
	RemainingBudgetBytes     int64  `json:"remaining_budget_bytes,string"`
	InboundID                uint64 `json:"inbound_id"`
	InitialApplySHA          string `json:"initial_apply_sha256"`
	HealthRoundID            string `json:"health_round_id"`
	DeliveryRevision         uint64 `json:"delivery_revision"`
	SnapshotID               string `json:"snapshot_id"`
	SnapshotEventID          string `json:"snapshot_event_id"`
	SnapshotRevision         uint64 `json:"snapshot_revision"`
	ContentSHA               string `json:"content_sha256"`
	DeliveredUnixNano        int64  `json:"delivered_unix_nano"`
	ExpiresUnixNano          int64  `json:"expires_unix_nano"`
}

type DarkRestoreNativeActivationPinsV1 DarkRestoreNativeActivationEvidenceV1

type DarkRestoreNativeActivationInputV1 struct {
	EventID  string                            `json:"event_id"`
	ClientID string                            `json:"client_id"`
	Pins     DarkRestoreNativeActivationPinsV1 `json:"pins"`
}

type DarkRestoreNativeActivationReceiptV1 struct {
	Scope                  string                                `json:"scope"`
	EventID                string                                `json:"event_id"`
	ActorUserID            string                                `json:"actor_user_id"`
	ClientID               string                                `json:"client_id"`
	TransitionSHA          string                                `json:"transition_sha256"`
	ActivationSHA          string                                `json:"activation_sha256"`
	RequestSHA             string                                `json:"request_sha256"`
	Evidence               DarkRestoreNativeActivationEvidenceV1 `json:"evidence"`
	RecordedUnixNano       int64                                 `json:"recorded_unix_nano"`
	AuthorizesFinalCutover bool                                  `json:"authorizes_final_cutover"`
	Replayed               bool                                  `json:"replayed"`
}

func nativeActivationRequestSHA(in DarkRestoreNativeActivationInputV1) string {
	return fingerprint("darkrestore-native-activation-request-v1", in)
}

func projectDarkRestoreNativeActivation(ctx context.Context, tx *sql.Tx, client string) (DarkRestoreNativeActivationEvidenceV1, error) {
	var zero DarkRestoreNativeActivationEvidenceV1
	acceptance, err := currentDarkRestoreNativeApplyAcceptance(ctx, tx, client)
	if err != nil || acceptance == nil {
		return zero, ErrDarkRestoreCutover
	}
	return projectDarkRestoreNativeActivationFromAcceptance(ctx, tx, client, acceptance, time.Now().UnixNano())
}

func projectDarkRestoreNativeActivationFromAcceptance(ctx context.Context, tx *sql.Tx, client string, acceptance *DarkRestoreNativeApplyAcceptanceReceiptV1, now int64) (DarkRestoreNativeActivationEvidenceV1, error) {
	var zero DarkRestoreNativeActivationEvidenceV1
	delivery, row, err := readDarkRestoreInitialDeliveryAfterAcceptanceAt(ctx, tx, client, acceptance, now)
	if err != nil {
		return zero, err
	}
	e := acceptance.Evidence
	return DarkRestoreNativeActivationEvidenceV1{
		Scope: DarkRestoreNativeActivationScopeV1, ClientID: client, TransitionSHA: e.TransitionSHA,
		NativeApplyEvidenceSHA: acceptance.EvidenceSHA, RepresentativeID: e.RepresentativeID,
		CurrentFiniteEntitlement: e.CurrentFiniteEntitlement, SettledHistoricalBytes: e.SettledHistoricalBytes,
		NativePostMigrationBytes: e.NativePostMigrationBytes, RemainingBudgetBytes: e.RemainingBudgetBytes,
		InboundID: e.InboundID, InitialApplySHA: e.InitialApplySHA, HealthRoundID: e.HealthRoundID,
		DeliveryRevision: delivery.Revision, SnapshotID: row.ID, SnapshotEventID: row.EventID,
		SnapshotRevision: row.Revision, ContentSHA: row.BodySHA, DeliveredUnixNano: row.CreatedUnixNano,
		ExpiresUnixNano: row.ExpiresUnixNano,
	}, nil
}

func validateDarkRestoreNativeActivationEvidence(e DarkRestoreNativeActivationEvidenceV1, pins DarkRestoreNativeActivationPinsV1, now int64) (string, error) {
	remaining, remainingErr := cutoverRemaining(e.CurrentFiniteEntitlement, e.SettledHistoricalBytes, e.NativePostMigrationBytes, 0)
	if e.Scope != DarkRestoreNativeActivationScopeV1 || !idRE.MatchString(e.ClientID) || !routeSHA.MatchString(e.TransitionSHA) || !routeSHA.MatchString(e.NativeApplyEvidenceSHA) || !idRE.MatchString(e.RepresentativeID) || remainingErr != nil || e.RemainingBudgetBytes != remaining || e.InboundID == 0 || !routeSHA.MatchString(e.InitialApplySHA) || !routeID.MatchString(e.HealthRoundID) || e.DeliveryRevision == 0 || !routeID.MatchString(e.SnapshotID) || !eventRE.MatchString(e.SnapshotEventID) || e.SnapshotRevision == 0 || !routeSHA.MatchString(e.ContentSHA) || e.DeliveredUnixNano <= 0 || e.ExpiresUnixNano <= now {
		return "", ErrDarkRestoreCutover
	}
	if !reflect.DeepEqual(DarkRestoreNativeActivationEvidenceV1(pins), e) {
		return "", ErrDarkRestoreCutover
	}
	sha, err := cutoverSnapshotDigest("DARK-PRO-NATIVE-ACTIVATION-v1", e)
	if err != nil {
		return "", err
	}
	return sha, nil
}

func readDarkRestoreNativeActivation(ctx context.Context, q sellerQueryV2, client string) (*DarkRestoreNativeActivationReceiptV1, error) {
	present, err := darkRestoreNativeActivationSchema(ctx, q)
	if err != nil || !present {
		return nil, err
	}
	var raw []byte
	var event, actor, transition, activation, request string
	var recorded int64
	err = q.QueryRowContext(ctx, `SELECT event_id,actor_user_id,transition_sha256,activation_sha256,request_sha256,receipt_json,recorded_unix_nano FROM darkrestore_native_activations_v1 WHERE client_id=?`, client).Scan(&event, &actor, &transition, &activation, &request, &raw, &recorded)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out DarkRestoreNativeActivationReceiptV1
	if len(raw) > 262144 || json.Unmarshal(raw, &out) != nil {
		return nil, ErrUnavailable
	}
	canonical, _ := json.Marshal(out)
	if !reflect.DeepEqual(canonical, raw) || out.ClientID != client || out.EventID != event || out.ActorUserID != actor || out.TransitionSHA != transition || out.ActivationSHA != activation || out.RequestSHA != request || out.RecordedUnixNano != recorded {
		return nil, ErrUnavailable
	}
	return &out, nil
}

func (s *Store) RecordDarkRestoreNativeActivationV1(ctx context.Context, token string, in DarkRestoreNativeActivationInputV1) (DarkRestoreNativeActivationReceiptV1, error) {
	var zero DarkRestoreNativeActivationReceiptV1
	if !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.ClientID) {
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
	request := nativeActivationRequestSHA(in)
	if prior, e := readDarkRestoreNativeActivation(ctx, tx, in.ClientID); e != nil {
		return zero, e
	} else if prior != nil {
		if prior.ActorUserID != actor || prior.EventID != in.EventID || prior.RequestSHA != request {
			return zero, ErrConflict
		}
		prior.Replayed = true
		return *prior, tx.Commit()
	}
	evidence, err := projectDarkRestoreNativeActivation(ctx, tx, in.ClientID)
	if err != nil {
		return zero, err
	}
	activation, err := validateDarkRestoreNativeActivationEvidence(evidence, in.Pins, time.Now().UnixNano())
	if err != nil {
		return zero, err
	}
	out := DarkRestoreNativeActivationReceiptV1{Scope: DarkRestoreNativeActivationScopeV1, EventID: in.EventID, ActorUserID: actor, ClientID: in.ClientID, TransitionSHA: evidence.TransitionSHA, ActivationSHA: activation, RequestSHA: request, Evidence: evidence, RecordedUnixNano: time.Now().UnixNano(), AuthorizesFinalCutover: true}
	if out.RecordedUnixNano <= evidence.DeliveredUnixNano {
		return zero, ErrDarkRestoreCutover
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return zero, err
	}
	if err = ensureDarkRestoreNativeActivationSchema(ctx, tx); err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO darkrestore_native_activations_v1(client_id,event_id,actor_user_id,transition_sha256,activation_sha256,request_sha256,receipt_json,recorded_unix_nano) VALUES(?,?,?,?,?,?,?,?)`, out.ClientID, out.EventID, out.ActorUserID, out.TransitionSHA, out.ActivationSHA, out.RequestSHA, raw, out.RecordedUnixNano); err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}

func currentDarkRestoreNativeActivation(ctx context.Context, tx *sql.Tx, client string) (*DarkRestoreNativeActivationReceiptV1, error) {
	r, err := readDarkRestoreNativeActivation(ctx, tx, client)
	if err != nil || r == nil {
		return r, err
	}
	current, err := projectDarkRestoreNativeActivation(ctx, tx, client)
	if err != nil {
		return nil, err
	}
	sha, err := validateDarkRestoreNativeActivationEvidence(current, DarkRestoreNativeActivationPinsV1(r.Evidence), time.Now().UnixNano())
	if err != nil || sha != r.ActivationSHA || !reflect.DeepEqual(current, r.Evidence) || !r.AuthorizesFinalCutover || r.Replayed {
		return nil, ErrDarkRestoreCutover
	}
	return r, nil
}

func validateDarkRestoreNativeActivations(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreNativeActivationSchema(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT client_id FROM darkrestore_native_activations_v1 ORDER BY client_id`)
	if err != nil {
		return ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return ErrUnavailable
		}
		r, e := readDarkRestoreNativeActivation(ctx, tx, id)
		if e != nil || r == nil || r.Scope != DarkRestoreNativeActivationScopeV1 || !r.AuthorizesFinalCutover || r.Replayed {
			return ErrUnavailable
		}
		acceptance, e := readDarkRestoreNativeApplyAcceptance(ctx, tx, id)
		if e != nil || acceptance == nil {
			return ErrUnavailable
		}
		projected, e := projectDarkRestoreNativeActivationFromAcceptance(ctx, tx, id, acceptance, r.RecordedUnixNano)
		if e != nil || !reflect.DeepEqual(projected, r.Evidence) {
			return ErrUnavailable
		}
		sha, e := validateDarkRestoreNativeActivationEvidence(r.Evidence, DarkRestoreNativeActivationPinsV1(r.Evidence), r.RecordedUnixNano)
		if e != nil || sha != r.ActivationSHA || r.RecordedUnixNano <= r.Evidence.DeliveredUnixNano {
			return ErrUnavailable
		}
	}
	if rows.Err() != nil {
		return ErrUnavailable
	}
	return nil
}
