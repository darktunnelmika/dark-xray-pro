package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type MeteredHealthIntent struct {
	ID             string `json:"id"`
	HandoverID     string `json:"handover_id"`
	NodeID         string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	InboundID      uint64 `json:"inbound_id"`
	EndpointSHA    string `json:"endpoint_sha"`
	Expires        int64  `json:"expires_unix_nano"`
}

func (s *Store) EnableMeteredHealthQueue(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	return s.usage.EnableHealthQueue(ctx)
}

func healthBinding(q mx.ProtocolHealthRequest) string {
	q.ID, q.Signature, q.EndpointSHA = "", "", ""
	q.Issued, q.Expires = 0, 0
	return meteredHash(q)
}

func readHealthIntent(ctx context.Context, tx *sql.Tx, id string) (MeteredHealthIntent, string, error) {
	var v MeteredHealthIntent
	var binding string
	err := tx.QueryRowContext(ctx, `SELECT id,handover_id,node_id,installation_id,inbound_id,endpoint_sha,binding,expires FROM pro_health_intents WHERE id=?`, id).Scan(&v.ID, &v.HandoverID, &v.NodeID, &v.InstallationID, &v.InboundID, &v.EndpointSHA, &binding, &v.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return v, binding, err
}

// Explicit owner intent is durable before an Agent supplies its authentication
// token. Store no plaintext token and grant no execution/publication lease.
func (s *Store) QueueMeteredHealth(ctx context.Context, key []byte, id, op string, inbound uint64, endpoint mx.ProtocolHealthEndpoint) (MeteredHealthIntent, error) {
	var empty MeteredHealthIntent
	if !routeID.MatchString(id) || !routeID.MatchString(op) || inbound == 0 || !endpoint.Valid() {
		return empty, ErrInvalid
	}
	tx, err := s.healthTx(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var version int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return empty, err
	}
	if !usageledger.HasHealthQueueSchema(version) {
		return empty, ErrUnavailable
	}
	v, _, err := readHealthIntent(ctx, tx, id)
	if err == nil {
		if v.HandoverID != op || v.InboundID != inbound || v.EndpointSHA != endpoint.SHA() {
			return empty, ErrConflict
		}
		return v, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return empty, err
	}
	if err = legacyHealthAllowed(ctx, tx, op); err != nil {
		return empty, err
	}
	now := time.Now().UnixNano()
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM pro_health_receipts WHERE handover_id=?)+(SELECT COUNT(*) FROM pro_health_intents WHERE handover_id=? AND expires>?)`, op, op, now).Scan(&n); err != nil {
		return empty, err
	}
	if n != 0 {
		return empty, ErrConflict
	}
	q, err := healthCurrent(ctx, tx, key, op, inbound, now)
	if err != nil {
		return empty, err
	}
	expires := now + int64(8*time.Second)
	if expires > q.Expires {
		expires = q.Expires
	}
	v = MeteredHealthIntent{id, op, q.NodeID, q.InstallationID, inbound, endpoint.SHA(), expires}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_health_intents VALUES(?,?,?,?,?,?,?,?,?)`, v.ID, v.HandoverID, v.NodeID, v.InstallationID, v.InboundID, v.EndpointSHA, healthBinding(q), v.Expires, now); err != nil {
		return empty, err
	}
	return v, tx.Commit()
}

// Poll resolves only this authenticated installation's owner intent. Exact
// delivery retries return the original persisted challenge, never a new nonce.
func (s *Store) PollMeteredHealth(ctx context.Context, key, token []byte, node, install string, endpoint mx.ProtocolHealthEndpoint) (mx.ProtocolHealthRequest, bool, error) {
	var empty mx.ProtocolHealthRequest
	tx, err := s.healthTx(ctx)
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback()
	var id string
	var version int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return empty, false, err
	}
	if !usageledger.HasHealthQueueSchema(version) {
		return empty, false, ErrUnavailable
	}
	now := time.Now().UnixNano()
	if usageledger.HasHealthRoundsSchema(version) {
		err = tx.QueryRowContext(ctx, `SELECT i.id FROM pro_health_intents i LEFT JOIN pro_health_round_members m ON m.intent_id=i.id LEFT JOIN pro_health_rounds hr ON hr.id=m.round_id LEFT JOIN pro_health_round_receipts rr ON rr.round_id=m.round_id AND rr.inbound_id=m.inbound_id LEFT JOIN pro_health_receipts r ON r.handover_id=i.handover_id WHERE i.node_id=? AND i.installation_id=? AND i.expires>? AND ((m.round_id IS NOT NULL AND rr.challenge_id IS NULL AND hr.expires>?) OR (m.round_id IS NULL AND r.challenge_id IS NULL AND NOT EXISTS(SELECT 1 FROM pro_health_rounds all_rounds WHERE all_rounds.handover_id=i.handover_id))) ORDER BY i.created DESC,m.inbound_id,i.id LIMIT 1`, node, install, now, now).Scan(&id)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT i.id FROM pro_health_intents i LEFT JOIN pro_health_receipts r ON r.handover_id=i.handover_id WHERE i.node_id=? AND i.installation_id=? AND i.expires>? AND r.challenge_id IS NULL ORDER BY i.created DESC LIMIT 1`, node, install, now).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return empty, false, tx.Commit()
	}
	if err != nil {
		return empty, false, err
	}
	v, binding, err := readHealthIntent(ctx, tx, id)
	if err != nil {
		return empty, false, err
	}
	if v.EndpointSHA != endpoint.SHA() {
		return empty, false, ErrConflict
	}
	q, err := healthCurrent(ctx, tx, key, v.HandoverID, v.InboundID, time.Now().UnixNano())
	if err != nil {
		return empty, false, err
	}
	if healthBinding(q) != binding {
		return empty, false, ErrConflict
	}
	round, err := healthIntentRound(ctx, tx, id)
	if err != nil {
		return empty, false, err
	}
	if round != "" {
		if _, err = liveHealthRoundMember(ctx, tx, key, round, id, v.HandoverID, v.InboundID, v.EndpointSHA, time.Now().UnixNano()); err != nil {
			return empty, false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return empty, false, err
	}
	// Issue rechecks current state again under its own writer transaction. The
	// immutable intent is compared to the issued binding before any delivery.
	q, err = s.IssueMeteredHealthChallenge(ctx, key, token, id, v.HandoverID, v.InboundID, endpoint)
	if err != nil {
		return empty, false, err
	}
	if q.NodeID != node || q.InstallationID != install || healthBinding(q) != binding {
		return empty, false, ErrConflict
	}
	return q, true, nil
}

// Resolve a proof's installation from the persisted challenge, never Agent input.
func (s *Store) MeteredHealthChallengeScope(ctx context.Context, id string) (string, string, string, error) {
	if !routeID.MatchString(id) {
		return "", "", "", ErrInvalid
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT request FROM pro_health_challenges WHERE id=?`, id).Scan(&raw); err != nil {
		return "", "", "", err
	}
	var q mx.ProtocolHealthRequest
	if json.Unmarshal([]byte(raw), &q) != nil || q.ID != id {
		return "", "", "", ErrUnavailable
	}
	return q.NodeID, q.InstallationID, q.HandoverID, nil
}
