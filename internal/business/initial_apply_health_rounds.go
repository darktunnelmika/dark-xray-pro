package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type initialHealthManifest struct {
	Version          int                      `json:"version"`
	PromotionEventID string                   `json:"promotion_event_id"`
	Request          mx.ProtocolHealthRequest `json:"request"`
}

type initialHealthRoundRow struct {
	InitialApplyHealthRound
	ActorID   string
	manifest  initialHealthManifest
	challenge *mx.ProtocolHealthRequest
	receipt   *mx.ProtocolHealthReceipt
}

func initialManifest(q mx.ProtocolHealthRequest, promotion string) initialHealthManifest {
	q.ID, q.Signature, q.EndpointSHA = "", "", ""
	q.Issued, q.Expires = 0, 0
	return initialHealthManifest{Version: 1, PromotionEventID: promotion, Request: q}
}

func initialHealthIDUnused(ctx context.Context, tx *sql.Tx, id string) error {
	for _, pair := range [][2]string{{"pro_initial_rounds", "id"}, {"pro_initial_challenges", "id"}, {"pro_initial_preparations", "event_id"}, {"pro_initial_snapshots", "event_id"}, {"pro_health_rounds", "id"}, {"pro_health_intents", "id"}, {"pro_health_challenges", "id"}, {"pro_health_challenges", "intent_id"}, {"pro_subscription_token_events", "event_id"}, {"pro_subscription_snapshots", "event_id"}, {"pro_consumer_manifests", "event_id"}, {"native_inbound_plan_promotions", "event_id"}, {"native_inbound_plan_events", "event_id"}} {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+pair[0]+` WHERE `+pair[1]+`=?`, id).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
	}
	return nil
}

// Called by the old path when v15 is present. A persisted untyped Agent lookup
// must never be ambiguous between an old challenge and the new proof scope.
func healthIDNotInitial(ctx context.Context, tx *sql.Tx, id string) error {
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if !usageledger.HasInitialApplyHealthSchema(version) {
		return nil
	}
	for _, pair := range [][2]string{{"pro_initial_rounds", "id"}, {"pro_initial_challenges", "id"}, {"pro_initial_preparations", "event_id"}, {"pro_initial_snapshots", "event_id"}} {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+pair[0]+` WHERE `+pair[1]+`=?`, id).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
	}
	return nil
}

func readInitialHealthRound(ctx context.Context, tx *sql.Tx, id string) (initialHealthRoundRow, error) {
	var row initialHealthRoundRow
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT id,actor_id,inbound_id,promotion_event_id,node_id,installation_id,apply_command_id,initial_apply_sha,endpoint_sha,manifest,manifest_sha,created,expires FROM pro_initial_rounds WHERE id=?`, id).Scan(&row.ID, &row.ActorID, &row.InboundID, &row.PromotionEventID, &row.NodeID, &row.InstallationID, &row.ApplyCommandID, &row.InitialApplySHA, &row.EndpointSHA, &raw, &row.ManifestSHA, &row.Created, &row.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, err
	}
	if len(raw) > 16384 || json.Unmarshal([]byte(raw), &row.manifest) != nil || meteredHash(row.manifest) != row.ManifestSHA || row.manifest.Version != 1 || row.manifest.PromotionEventID != row.PromotionEventID || !eventRE.MatchString(row.PromotionEventID) || !routeID.MatchString(row.ID) || !idRE.MatchString(row.ActorID) || row.Created <= 0 || row.Expires <= row.Created || row.Expires-row.Created > int64(8*time.Second) || !routeSHA.MatchString(row.EndpointSHA) {
		return row, ErrUnavailable
	}
	q := row.manifest.Request
	if q.Version != mx.InitialApplyProtocolHealthVersion || q.HandoverID != "" || q.InitialApplySHA != row.InitialApplySHA || !routeSHA.MatchString(q.InitialApplySHA) || q.NodeID != row.NodeID || !idRE.MatchString(q.NodeID) || q.InstallationID != row.InstallationID || !routeID.MatchString(q.InstallationID) || q.InboundID != row.InboundID || q.InboundID == 0 || q.Port < 1024 || q.ApplyCommandID != row.ApplyCommandID || !routeID.MatchString(q.ApplyCommandID) || !routeID.MatchString(q.StageID) || !routeID.MatchString(q.RuntimeInstance) || !routeSHA.MatchString(q.CredentialSHA) || !idRE.MatchString(q.SystemProbeID) || !routeID.MatchString(q.Usage.EpochID) || !routeSHA.MatchString(q.Usage.EpochSHA) || !routeSHA.MatchString(q.Usage.AdmissionSHA) || !q.Expected.ProcessReady || q.Expected.ActiveSequence == 0 || !routeSHA.MatchString(q.Expected.ActiveSHA) || q.ID != "" || q.Signature != "" || q.EndpointSHA != "" || q.Issued != 0 || q.Expires != 0 || q.ConsumerManifestSHA != "" || q.ConsumerAddress != "" || q.ConsumerPort != 0 {
		return row, ErrUnavailable
	}
	row.Scope, row.StageID, row.EpochID = InitialApplyHealthScope, q.StageID, q.Usage.EpochID
	member := MeteredHealthRoundMember{InboundID: row.InboundID, IntentID: row.ID, Port: q.Port, State: "pending"}
	var challengeID, request string
	var expires int64
	err = tx.QueryRowContext(ctx, `SELECT id,request,expires FROM pro_initial_challenges WHERE round_id=?`, id).Scan(&challengeID, &request, &expires)
	if err == nil {
		var issued mx.ProtocolHealthRequest
		if len(request) > 16384 || json.Unmarshal([]byte(request), &issued) != nil || !routeID.MatchString(challengeID) || issued.ID != challengeID || issued.Expires != row.Expires || expires != row.Expires || issued.Issued < row.Created || issued.Issued >= row.Expires || !routeSHA.MatchString(issued.Signature) || issued.EndpointSHA != row.EndpointSHA || healthBinding(issued) != healthBinding(q) {
			return row, ErrUnavailable
		}
		row.challenge = &issued
		member.ChallengeID, member.State = challengeID, "issued"
		var receiptRaw string
		var accepted int64
		err = tx.QueryRowContext(ctx, `SELECT receipt,accepted FROM pro_initial_receipts WHERE round_id=? AND challenge_id=?`, id, challengeID).Scan(&receiptRaw, &accepted)
		if err == nil {
			var p mx.ProtocolHealthReceipt
			responseSHA := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + issued.ID + "\n"))
			if len(receiptRaw) > 4096 || json.Unmarshal([]byte(receiptRaw), &p) != nil || p.Version != issued.Version || p.RequestSHA != meteredHash(issued) || p.ResponseSHA != hex.EncodeToString(responseSHA[:]) || !routeSHA.MatchString(p.Signature) || p.Completed < issued.Issued || p.Completed >= issued.Expires || p.Completed > accepted || accepted < row.Created || accepted >= row.Expires {
				return row, ErrUnavailable
			}
			row.receipt = &p
			member.Accepted, member.State = accepted, "accepted"
		} else if !errors.Is(err, sql.ErrNoRows) {
			return row, err
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return row, err
	}
	row.Members = []MeteredHealthRoundMember{member}
	// An orphaned/moved receipt is corruption, never an unconsumed round.
	var receipts int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_initial_receipts WHERE round_id=?`, row.ID).Scan(&receipts); err != nil {
		return row, err
	}
	if (row.receipt == nil && receipts != 0) || (row.receipt != nil && receipts != 1) {
		return row, ErrUnavailable
	}
	return row, nil
}

func (s *Store) currentInitialHealthRound(ctx context.Context, tx *sql.Tx, key []byte, row initialHealthRoundRow, endpoint *mx.ProtocolHealthEndpoint, now int64) (initialApplySnapshot, mx.ProtocolHealthRequest, error) {
	var empty initialApplySnapshot
	var q mx.ProtocolHealthRequest
	if now < row.Created || now >= row.Expires || endpoint != nil && row.EndpointSHA != endpoint.SHA() {
		return empty, q, ErrConflict
	}
	state, err := s.loadInitialApplySnapshot(ctx, tx, key, row.InboundID, now)
	if err != nil {
		return empty, q, err
	}
	q, err = state.request(ctx, tx, row.InboundID)
	if err != nil {
		return empty, q, err
	}
	if q.Expires < row.Expires || meteredHash(initialManifest(q, state.promotion.EventID)) != row.ManifestSHA {
		return empty, q, ErrConflict
	}
	if endpoint != nil {
		if err = mx.ValidateProtocolHealthSupport(state.apply.payload.Candidate, q, *endpoint); err != nil {
			return empty, q, ErrConflict
		}
	}
	return state, q, nil
}

func (s *Store) assessInitialHealthRound(ctx context.Context, tx *sql.Tx, key []byte, row initialHealthRoundRow, endpoint *mx.ProtocolHealthEndpoint, now int64) InitialApplyHealthRound {
	v := row.InitialApplyHealthRound
	v.Current = false
	if now >= v.Expires {
		v.State = "expired"
		return v
	}
	if _, _, err := s.currentInitialHealthRound(ctx, tx, key, row, endpoint, now); err != nil {
		v.State = "state_changed"
		return v
	}
	v.State = "pending"
	if row.receipt != nil {
		v.State = "complete"
	}
	if time.Now().UnixNano() >= v.Expires {
		v.State = "expired"
	} else {
		v.Current = v.State == "complete"
	}
	return v
}

func (s *Store) QueueInitialApplyHealthRound(ctx context.Context, key []byte, actorID string, inbound uint64, in InitialApplyHealthRoundInput, endpoint mx.ProtocolHealthEndpoint) (InitialApplyHealthRound, error) {
	return s.queueInitialApplyHealthRound(ctx, key, actorID, inbound, in, endpoint, time.Now)
}

func (s *Store) queueInitialApplyHealthRound(ctx context.Context, key []byte, actorID string, inbound uint64, in InitialApplyHealthRoundInput, endpoint mx.ProtocolHealthEndpoint, clock func() time.Time) (InitialApplyHealthRound, error) {
	var out InitialApplyHealthRound
	if inbound == 0 || !routeID.MatchString(in.ID) || !routeSHA.MatchString(in.InitialApplySHA) || !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := s.initialHealthTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = consumerManifestOwner(ctx, tx, actorID); err != nil {
		return out, err
	}
	prior, err := readInitialHealthRound(ctx, tx, in.ID)
	if err == nil {
		if prior.ActorID != actorID || prior.InboundID != inbound || prior.InitialApplySHA != in.InitialApplySHA || prior.EndpointSHA != endpoint.SHA() {
			return out, ErrConflict
		}
		out = s.assessInitialHealthRound(ctx, tx, key, prior, &endpoint, clock().UnixNano())
		out.Replayed = true
		return out, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return out, err
	}
	if err = initialHealthIDUnused(ctx, tx, in.ID); err != nil {
		return out, err
	}
	now := clock().UnixNano()
	state, err := s.loadInitialApplySnapshot(ctx, tx, key, inbound, now)
	if err != nil {
		return out, err
	}
	q, err := state.request(ctx, tx, inbound)
	if err != nil {
		return out, err
	}
	if q.InitialApplySHA != in.InitialApplySHA {
		return out, ErrConflict
	}
	if err = mx.ValidateProtocolHealthSupport(state.apply.payload.Candidate, q, endpoint); err != nil {
		return out, ErrConflict
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_initial_rounds WHERE inbound_id=? AND expires>?`, inbound, now).Scan(&active); err != nil {
		return out, err
	}
	if active != 0 {
		return out, ErrConflict
	}
	expires := now + int64(8*time.Second)
	if q.Expires < expires {
		expires = q.Expires
	}
	if expires <= now {
		return out, ErrConflict
	}
	manifest := initialManifest(q, state.promotion.EventID)
	raw, err := json.Marshal(manifest)
	if err != nil {
		return out, err
	}
	sha := meteredHash(manifest)
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_initial_rounds(id,actor_id,inbound_id,promotion_event_id,node_id,installation_id,apply_command_id,initial_apply_sha,endpoint_sha,manifest,manifest_sha,created,expires) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, in.ID, actorID, inbound, state.promotion.EventID, q.NodeID, q.InstallationID, q.ApplyCommandID, q.InitialApplySHA, endpoint.SHA(), string(raw), sha, now, expires); err != nil {
		return out, err
	}
	if end := clock().UnixNano(); end < now || end >= expires {
		return out, ErrConflict
	}
	row, err := readInitialHealthRound(ctx, tx, in.ID)
	if err != nil {
		return out, err
	}
	out = s.assessInitialHealthRound(ctx, tx, key, row, &endpoint, clock().UnixNano())
	return out, tx.Commit()
}

func (s *Store) InitialApplyHealthRound(ctx context.Context, key []byte, actorID string, inbound uint64, id string, endpoint mx.ProtocolHealthEndpoint) (InitialApplyHealthRound, error) {
	var out InitialApplyHealthRound
	if inbound == 0 || !routeID.MatchString(id) || !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := s.initialHealthTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = consumerManifestOwner(ctx, tx, actorID); err != nil {
		return out, err
	}
	row, err := readInitialHealthRound(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if row.InboundID != inbound {
		return out, ErrNotFound
	}
	out = s.assessInitialHealthRound(ctx, tx, key, row, &endpoint, time.Now().UnixNano())
	return out, tx.Commit()
}

func (s *Store) PollInitialApplyHealth(ctx context.Context, key, token []byte, node, installation string, endpoint mx.ProtocolHealthEndpoint) (mx.ProtocolHealthRequest, bool, error) {
	var q mx.ProtocolHealthRequest
	if len(token) < 32 || !idRE.MatchString(node) || !routeID.MatchString(installation) || !endpoint.Valid() {
		return q, false, ErrInvalid
	}
	tx, err := s.initialHealthTx(ctx, key)
	if err != nil {
		return q, false, err
	}
	defer tx.Rollback()
	now := time.Now().UnixNano()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT r.id FROM pro_initial_rounds r LEFT JOIN pro_initial_receipts p ON p.round_id=r.id WHERE r.node_id=? AND r.installation_id=? AND r.expires>? AND p.challenge_id IS NULL ORDER BY r.created LIMIT 1`, node, installation, now).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return q, false, tx.Commit()
	}
	if err != nil {
		return q, false, err
	}
	row, err := readInitialHealthRound(ctx, tx, id)
	if err != nil {
		return q, false, err
	}
	if err = consumerManifestOwner(ctx, tx, row.ActorID); err != nil {
		return q, false, err
	}
	_, q, err = s.currentInitialHealthRound(ctx, tx, key, row, &endpoint, now)
	if err != nil {
		return q, false, err
	}
	if row.challenge != nil {
		want, e := mx.SignProtocolHealthRequest(token, *row.challenge)
		if e != nil || want != *row.challenge {
			return mx.ProtocolHealthRequest{}, false, ErrConflict
		}
		q = *row.challenge
	} else {
		q.ID, err = subscriptionID()
		if err != nil {
			return q, false, err
		}
		if err = initialHealthIDUnused(ctx, tx, q.ID); err != nil {
			return q, false, err
		}
		q.EndpointSHA, q.Issued, q.Expires = endpoint.SHA(), now, row.Expires
		q, err = mx.SignProtocolHealthRequest(token, q)
		if err != nil {
			return q, false, err
		}
		raw, e := json.Marshal(q)
		if e != nil {
			return q, false, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pro_initial_challenges(id,round_id,request,expires) VALUES(?,?,?,?)`, q.ID, row.ID, string(raw), q.Expires); err != nil {
			return q, false, err
		}
	}
	if end := time.Now().UnixNano(); end < now || end >= q.Expires {
		return mx.ProtocolHealthRequest{}, false, ErrConflict
	}
	return q, true, tx.Commit()
}

func (s *Store) InitialApplyHealthChallengeScope(ctx context.Context, id string) (InitialApplyHealthChallengeTarget, error) {
	var out InitialApplyHealthChallengeTarget
	if !s.initialApplyHealthEnabled {
		return out, ErrUnavailable
	}
	if !routeID.MatchString(id) {
		return out, ErrInvalid
	}
	tx, err := s.healthTx(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var round string
	err = tx.QueryRowContext(ctx, `SELECT round_id FROM pro_initial_challenges WHERE id=?`, id).Scan(&round)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	row, err := readInitialHealthRound(ctx, tx, round)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return out, ErrUnavailable
		}
		return out, err
	}
	if row.challenge == nil || row.challenge.ID != id {
		return out, ErrUnavailable
	}
	out = InitialApplyHealthChallengeTarget{InboundID: row.InboundID, NodeID: row.NodeID, InstallationID: row.InstallationID, InitialApplySHA: row.InitialApplySHA}
	return out, tx.Commit()
}

func (s *Store) ConsumeInitialApplyHealthProof(ctx context.Context, key, token []byte, id string, endpoint mx.ProtocolHealthEndpoint, p mx.ProtocolHealthReceipt) (InitialApplyHealthEvidence, error) {
	var out InitialApplyHealthEvidence
	if !routeID.MatchString(id) || len(token) < 32 || !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := s.initialHealthTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var round string
	err = tx.QueryRowContext(ctx, `SELECT round_id FROM pro_initial_challenges WHERE id=?`, id).Scan(&round)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	row, err := readInitialHealthRound(ctx, tx, round)
	if err != nil {
		return out, err
	}
	if row.challenge == nil || row.challenge.ID != id || row.EndpointSHA != endpoint.SHA() {
		return out, ErrConflict
	}
	q := *row.challenge
	want, e := mx.SignProtocolHealthRequest(token, q)
	if e != nil || want != q {
		return out, ErrConflict
	}
	if row.receipt != nil {
		if *row.receipt != p {
			return out, ErrConflict
		}
		out = InitialApplyHealthEvidence{ChallengeID: id, RoundID: round, InboundID: row.InboundID, InitialApplySHA: row.InitialApplySHA, RequestSHA: meteredHash(q), ReceiptSHA: meteredHash(p), Accepted: row.Members[0].Accepted, Replayed: true}
		return out, tx.Commit()
	}
	now := time.Now()
	if !mx.VerifyProtocolHealthReceipt(token, q, p, now) {
		return out, ErrConflict
	}
	if err = consumerManifestOwner(ctx, tx, row.ActorID); err != nil {
		return out, err
	}
	if _, _, err = s.currentInitialHealthRound(ctx, tx, key, row, &endpoint, now.UnixNano()); err != nil {
		return out, err
	}
	raw, e := json.Marshal(p)
	if e != nil {
		return out, e
	}
	accepted := time.Now().UnixNano()
	if accepted < now.UnixNano() || accepted >= row.Expires {
		return out, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_initial_receipts(challenge_id,round_id,receipt,accepted) VALUES(?,?,?,?)`, id, round, string(raw), accepted); err != nil {
		return out, err
	}
	if time.Now().UnixNano() >= row.Expires {
		return out, ErrConflict
	}
	out = InitialApplyHealthEvidence{ChallengeID: id, RoundID: round, InboundID: row.InboundID, InitialApplySHA: row.InitialApplySHA, RequestSHA: meteredHash(q), ReceiptSHA: meteredHash(p), Accepted: accepted}
	return out, tx.Commit()
}
