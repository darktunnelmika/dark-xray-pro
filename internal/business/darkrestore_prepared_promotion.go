package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
)

const DarkRestorePreparedPromotionScopeV1 = "darkrestore_prepared_native_promotion_v1"

type DarkRestorePreparedPromotionInputV1 struct {
	EventID   string                          `json:"event_id"`
	ClientID  string                          `json:"client_id"`
	InboundID uint64                          `json:"inbound_id"`
	Promotion NativeInboundPlanPromotionInput `json:"promotion"`
}

type DarkRestorePreparedPromotionReceiptV1 struct {
	Scope                     string `json:"scope"`
	EventID                   string `json:"event_id"`
	ActorUserID               string `json:"actor_user_id"`
	ClientID                  string `json:"client_id"`
	InboundID                 uint64 `json:"inbound_id"`
	TransitionSHA             string `json:"transition_sha256"`
	PreparedAuthoritySHA      string `json:"prepared_authority_sha256"`
	PreparedContentSHA        string `json:"prepared_content_sha256"`
	PlanRevision              uint64 `json:"plan_revision"`
	PreparedInboundRevision   uint64 `json:"prepared_inbound_revision"`
	PromotedInboundRevision   uint64 `json:"promoted_inbound_revision"`
	PromotionEventID          string `json:"promotion_event_id"`
	PromotionReceiptSHA       string `json:"promotion_receipt_sha256"`
	PreStateSHA               string `json:"pre_state_sha256"`
	PostStateSHA              string `json:"post_state_sha256"`
	RequestSHA                string `json:"request_sha256"`
	CreatedUnix               int64  `json:"created_unix"`
	CreatedUnixNano           int64  `json:"created_unix_nano"`
	NativePromotionRecorded   bool   `json:"native_promotion_recorded"`
	NativeApplyRecorded       bool   `json:"native_apply_recorded"`
	SubscriptionActivated     bool   `json:"subscription_activated"`
	AuthorizesFurtherMutation bool   `json:"authorizes_further_mutation"`
	Replayed                  bool   `json:"replayed"`
}

type DarkRestorePreparedPromotionPreviewV1 struct {
	Scope              string                              `json:"scope"`
	ClientID           string                              `json:"client_id"`
	InboundID          uint64                              `json:"inbound_id"`
	TransitionSHA      string                              `json:"transition_sha256"`
	PreStateSHA        string                              `json:"pre_state_sha256"`
	PostStateSHA       string                              `json:"post_state_sha256"`
	Promotion          NativeInboundPlanPromotionPreview   `json:"promotion"`
	ReviewSHA256       string                              `json:"review_sha256"`
	Eligible           bool                                `json:"eligible"`
	Blockers           []NativeInboundPlanPromotionBlocker `json:"blockers"`
	AuthorizesMutation bool                                `json:"authorizes_mutation"`
}

func preparedPromotionRequestSHA(in DarkRestorePreparedPromotionInputV1) string {
	copy := in
	copy.Promotion.ReviewSHA256 = ""
	return fingerprint("darkrestore-prepared-native-promotion-request-v1", copy)
}

func preparedPromotionPostSnapshot(pre DarkRestoreCutoverSnapshotV1) (DarkRestoreCutoverSnapshotV1, error) {
	post := pre
	post.DeploymentState = "pending_node_apply"
	return hashDarkRestoreCutoverSnapshotV1(post)
}

func (s *Store) assessDarkRestorePreparedPromotion(ctx context.Context, tx *sql.Tx, key []byte, actor string, in DarkRestorePreparedPromotionInputV1) (DarkRestorePreparedPromotionPreviewV1, *DarkRestoreCutoverReceiptV1, promotionAssessment, error) {
	var empty promotionAssessment
	out := DarkRestorePreparedPromotionPreviewV1{Scope: DarkRestorePreparedPromotionScopeV1, ClientID: in.ClientID, InboundID: in.InboundID, Blockers: []NativeInboundPlanPromotionBlocker{}}
	if !idRE.MatchString(actor) || !idRE.MatchString(in.ClientID) || !eventRE.MatchString(in.EventID) || in.InboundID == 0 || in.InboundID >= routeMax || in.Promotion.ClientIDs == nil || len(in.Promotion.ClientIDs) != 1 || in.Promotion.ClientIDs[0] != in.ClientID {
		return out, nil, empty, ErrInvalid
	}
	normalized, err := normalizedPromotionInput(in.InboundID, in.Promotion, false)
	if err != nil {
		return out, nil, empty, err
	}
	in.Promotion = normalized
	final, err := readCutoverReceipt(ctx, tx, in.ClientID)
	if err != nil || final == nil || final.Prepared.InboundID != in.InboundID || verifyPreparedPlanReceipt(ctx, tx, final) != nil {
		return out, nil, empty, ErrDarkRestoreCutover
	}
	evidence, err := readCutoverEvidence(ctx, tx, in.ClientID)
	if err != nil || evidence == nil || evidence.TransitionSHA != final.TransitionSHA || verifyCutoverHistoricalBudget(ctx, tx, evidence) != nil {
		return out, nil, empty, ErrDarkRestoreCutover
	}
	pre, remaining, err := cutoverSnapshotMatchesEvidence(ctx, tx, evidence)
	if err != nil || pre.DeploymentState != "disabled" || final.CurrentFiniteEntitlement != pre.CurrentEntitlement || final.NativePostMigrationBytes != *pre.NativeUplink+*pre.NativeDownlink || final.RemainingBudgetBytes != remaining {
		return out, nil, empty, ErrDarkRestoreCutover
	}
	post, err := preparedPromotionPostSnapshot(pre)
	if err != nil {
		return out, nil, empty, err
	}
	out.TransitionSHA, out.PreStateSHA, out.PostStateSHA = final.TransitionSHA, pre.StateSHA, post.StateSHA
	if _, err = tx.ExecContext(ctx, "UPDATE clients SET deployment_state='pending_node_apply' WHERE id=? AND deployment_state='disabled'", in.ClientID); err != nil {
		return out, nil, empty, err
	}
	assessment, err := s.assessNativeInboundPromotion(ctx, tx, key, in.InboundID, &in.Promotion)
	if err != nil {
		return out, nil, empty, err
	}
	out.Promotion = assessment.preview
	out.Blockers = append(out.Blockers, assessment.preview.Blockers...)
	out.Eligible = assessment.preview.Eligible && len(out.Blockers) == 0
	out.ReviewSHA256 = fingerprint("darkrestore-prepared-native-promotion-review-v1", struct {
		Actor, RequestSHA, TransitionSHA, PreStateSHA, PostStateSHA, NativeReviewSHA string
	}{actor, preparedPromotionRequestSHA(in), final.TransitionSHA, pre.StateSHA, post.StateSHA, assessment.preview.ReviewSHA256})
	return out, final, assessment, nil
}

// PreviewDarkRestorePreparedPromotionV1 performs the eligibility transition in
// a transaction that is always rolled back. It is owner-only and returns no
// credentials. The caller must submit both review hashes to the commit method.
func (s *Store) PreviewDarkRestorePreparedPromotionV1(ctx context.Context, key []byte, token string, in DarkRestorePreparedPromotionInputV1) (DarkRestorePreparedPromotionPreviewV1, error) {
	var zero DarkRestorePreparedPromotionPreviewV1
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	out, _, _, err := s.assessDarkRestorePreparedPromotion(ctx, tx, key, actor, in)
	return out, err
}

func readDarkRestorePreparedPromotion(ctx context.Context, q sellerQueryV2, client string) (*DarkRestorePreparedPromotionReceiptV1, error) {
	present, err := darkRestorePreparedPromotionSchema(ctx, q)
	if err != nil || !present {
		return nil, err
	}
	var raw []byte
	var inbound uint64
	var event, promotionEvent, actor, transition, authority, content, promotionSHA, preSHA, postSHA, requestSHA string
	var created, createdNano int64
	if err = q.QueryRowContext(ctx, `SELECT inbound_id,event_id,promotion_event_id,actor_user_id,transition_sha256,prepared_authority_sha256,prepared_content_sha256,promotion_receipt_sha256,pre_state_sha256,post_state_sha256,request_sha256,receipt_json,created_unix,created_unix_nano FROM darkrestore_prepared_promotions_v1 WHERE client_id=?`, client).
		Scan(&inbound, &event, &promotionEvent, &actor, &transition, &authority, &content, &promotionSHA, &preSHA, &postSHA, &requestSHA, &raw, &created, &createdNano); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	var out DarkRestorePreparedPromotionReceiptV1
	if len(raw) > 131072 || json.Unmarshal(raw, &out) != nil {
		return nil, ErrUnavailable
	}
	canonical, _ := json.Marshal(out)
	if !reflect.DeepEqual(canonical, raw) || out.ClientID != client || out.InboundID != inbound || out.EventID != event || out.PromotionEventID != promotionEvent || out.ActorUserID != actor || out.TransitionSHA != transition || out.PreparedAuthoritySHA != authority || out.PreparedContentSHA != content || out.PromotionReceiptSHA != promotionSHA || out.PreStateSHA != preSHA || out.PostStateSHA != postSHA || out.RequestSHA != requestSHA || out.CreatedUnix != created || out.CreatedUnixNano != createdNano {
		return nil, ErrUnavailable
	}
	return &out, nil
}

func (s *Store) CommitDarkRestorePreparedPromotionV1(ctx context.Context, key []byte, token, reviewSHA string, in DarkRestorePreparedPromotionInputV1) (DarkRestorePreparedPromotionReceiptV1, error) {
	var zero DarkRestorePreparedPromotionReceiptV1
	if !routeSHA.MatchString(reviewSHA) || !routeSHA.MatchString(in.Promotion.ReviewSHA256) {
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
	if present, e := darkRestorePreparedPromotionSchema(ctx, tx); e != nil {
		return zero, e
	} else if present {
		if prior, e := readDarkRestorePreparedPromotion(ctx, tx, in.ClientID); e != nil {
			return zero, e
		} else if prior != nil {
			if prior.ActorUserID != actor || prior.EventID != in.EventID || prior.InboundID != in.InboundID || prior.RequestSHA != preparedPromotionRequestSHA(in) || prior.PromotionEventID != in.Promotion.EventID {
				return zero, ErrConflict
			}
			prior.Replayed = true
			return *prior, tx.Commit()
		}
	}
	preview, final, assessment, err := s.assessDarkRestorePreparedPromotion(ctx, tx, key, actor, in)
	if err != nil {
		return zero, err
	}
	if !preview.Eligible || len(preview.Blockers) != 0 || preview.ReviewSHA256 != reviewSHA || assessment.preview.ReviewSHA256 != in.Promotion.ReviewSHA256 {
		return zero, ErrConflict
	}
	promotion, err := s.promoteNativeInboundPlanTx(ctx, tx, key, in.InboundID, in.Promotion, &assessment)
	if err != nil {
		return zero, err
	}
	promotionSHA := meteredHash(promotion)
	createdNano, err := readNativeInboundPromotionUnixNano(ctx, tx, in.InboundID, promotion.EventID, promotion.CreatedUnix)
	if err != nil {
		return zero, err
	}
	out := DarkRestorePreparedPromotionReceiptV1{
		Scope: DarkRestorePreparedPromotionScopeV1, EventID: in.EventID, ActorUserID: actor, ClientID: in.ClientID, InboundID: in.InboundID,
		TransitionSHA: final.TransitionSHA, PreparedAuthoritySHA: final.Prepared.AuthoritySHA, PreparedContentSHA: final.Prepared.ContentSHA,
		PlanRevision: promotion.PlanRevision, PreparedInboundRevision: promotion.SourceInboundRevision, PromotedInboundRevision: promotion.InboundRevision,
		PromotionEventID: promotion.EventID, PromotionReceiptSHA: promotionSHA, PreStateSHA: preview.PreStateSHA, PostStateSHA: preview.PostStateSHA,
		RequestSHA: preparedPromotionRequestSHA(in), CreatedUnix: promotion.CreatedUnix, CreatedUnixNano: createdNano, NativePromotionRecorded: true,
	}
	if out.NativeApplyRecorded || out.SubscriptionActivated || out.AuthorizesFurtherMutation {
		return zero, ErrDarkRestoreCutover
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return zero, err
	}
	if err = ensureDarkRestorePreparedPromotionSchema(ctx, tx); err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO darkrestore_prepared_promotions_v1(client_id,inbound_id,event_id,promotion_event_id,actor_user_id,transition_sha256,prepared_authority_sha256,prepared_content_sha256,promotion_receipt_sha256,pre_state_sha256,post_state_sha256,request_sha256,receipt_json,created_unix,created_unix_nano) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		out.ClientID, out.InboundID, out.EventID, out.PromotionEventID, out.ActorUserID, out.TransitionSHA, out.PreparedAuthoritySHA, out.PreparedContentSHA, out.PromotionReceiptSHA, out.PreStateSHA, out.PostStateSHA, out.RequestSHA, raw, out.CreatedUnix, out.CreatedUnixNano); err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}

func validateDarkRestorePreparedPromotions(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestorePreparedPromotionSchema(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT client_id FROM darkrestore_prepared_promotions_v1 ORDER BY client_id")
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
		r, e := readDarkRestorePreparedPromotion(ctx, tx, id)
		if e != nil || r == nil || r.Scope != DarkRestorePreparedPromotionScopeV1 || r.ClientID != id || !r.NativePromotionRecorded || r.NativeApplyRecorded || r.SubscriptionActivated || r.AuthorizesFurtherMutation || r.Replayed || r.CreatedUnixNano/1e9 != r.CreatedUnix {
			return ErrUnavailable
		}
		final, e := readCutoverReceipt(ctx, tx, id)
		if e != nil || final == nil || final.TransitionSHA != r.TransitionSHA || final.Prepared.InboundID != r.InboundID || final.Prepared.AuthoritySHA != r.PreparedAuthoritySHA || final.Prepared.ContentSHA != r.PreparedContentSHA {
			return ErrUnavailable
		}
		var event string
		var planRev, sourceRev, promotedRev uint64
		if e = tx.QueryRowContext(ctx, "SELECT event_id,plan_revision,source_inbound_revision,inbound_revision FROM native_inbound_plan_promotions WHERE inbound_id=?", r.InboundID).Scan(&event, &planRev, &sourceRev, &promotedRev); e != nil || event != r.PromotionEventID || planRev != r.PlanRevision || sourceRev != r.PreparedInboundRevision || promotedRev != r.PromotedInboundRevision {
			return ErrUnavailable
		}
		evidence, e := readCutoverEvidence(ctx, tx, id)
		if e != nil || evidence == nil || verifyCutoverHistoricalBudget(ctx, tx, evidence) != nil {
			return ErrUnavailable
		}
		current, e := readDarkRestoreCutoverSnapshotV1(ctx, tx, evidence.SourceSHA256, evidence.SourceID)
		if e != nil || current.ClientID != id || current.DeploymentState != "pending_node_apply" {
			return ErrUnavailable
		}
		effective, e := effectiveCutoverStateSHA(ctx, tx, evidence)
		if e != nil || effective != r.PreStateSHA || r.PostStateSHA == r.PreStateSHA || !routeSHA.MatchString(r.PostStateSHA) {
			return ErrUnavailable
		}
		clients := append([]string(nil), promotionClientIDs(ctx, tx, r.InboundID)...)
		sort.Strings(clients)
		if len(clients) != 1 || clients[0] != id {
			return ErrUnavailable
		}
	}
	return nil
}

func promotionClientIDs(ctx context.Context, q *sql.Tx, inbound uint64) []string {
	rows, err := q.QueryContext(ctx, "SELECT client_id FROM native_inbound_plan_promotion_clients WHERE inbound_id=? ORDER BY client_id", inbound)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			return nil
		}
		out = append(out, id)
	}
	return out
}
