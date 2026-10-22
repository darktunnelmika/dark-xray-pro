package business

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

// Explicit startup opt-in only; no HTTP route or agent poll invokes it.
func (s *Store) EnableMeteredHealthChallenges(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	return s.usage.EnableHealthChallenges(ctx)
}

// This is historical evidence, never live health or publication authority.
// Replayed means an exact retry read an already committed result; it did not
// consume the challenge again or reauthorize an expired/stopped runtime.
type MeteredHealthEvidence struct {
	ChallengeID string `json:"challenge_id"`
	HandoverID  string `json:"handover_id"`
	RequestSHA  string `json:"request_sha"`
	ReceiptSHA  string `json:"receipt_sha"`
	Accepted    int64  `json:"accepted_unix_nano"`
	Replayed    bool   `json:"replayed"`
}

func (s *Store) healthTx(ctx context.Context) (*sql.Tx, error) {
	if s.usage == nil {
		return nil, ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	// Acquire the SQLite writer lock BEFORE reading, across independent Store
	// handles as well as goroutines. This does not change config generation.
	_, err = tx.ExecContext(ctx, `UPDATE pro_health_meta SET version=version WHERE id=1 AND version=1`)
	var version int
	if err == nil {
		err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
		if err == nil && !usageledger.HasHealthSchema(version) {
			err = ErrUnavailable
		}
	}
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return tx, nil
}

// healthCurrent repeats the accounting barrier and checks the target's exact
// applied command, fresh signed observation, live lease and registered epoch.
func healthCurrent(ctx context.Context, tx *sql.Tx, key []byte, opID string, inbound uint64, now int64) (mx.ProtocolHealthRequest, error) {
	var q mx.ProtocolHealthRequest
	op, _, err := readMeteredHandover(ctx, tx, opID)
	if err != nil {
		return q, err
	}
	if op.Phase != "target_applied_health_required" {
		return q, ErrConflict
	}
	apply, err := readMeteredRouteCommand(ctx, tx, key, op.ApplyCommandID)
	if err != nil {
		return q, err
	}
	if err = meteredRouteStillCurrent(ctx, tx, key, apply); err != nil {
		return q, err
	}
	ch, err := handoverIdleChannel(ctx, tx, op.TargetNode)
	if err != nil {
		return q, err
	}
	c, ack := apply.payload.Command, apply.view.Receipt
	o := ch.Status.Observed
	if ch.InstallationID != op.TargetInstallation || ch.Status.Instance != c.RuntimeInstance || ch.Status.Serial != c.Serial || ch.Status.CurrentStageID != op.TargetStageID || ch.Status.StagedID != "" || ack == nil || ack.Metered == nil || ch.ReportedAt < ack.ObservedAt || !o.ProcessReady || o.ActiveSequence != c.RuntimeSequence || o.ActiveSHA != c.Lease.ConfigSHA || o.LeaseExpires != c.Lease.Expires || o.LeaseExpires <= now || o.Usage == nil || !o.Usage.Running || o.Usage.Settled || o.Usage.Failure != "" || *ch.Status.Usage != ack.Metered.After || ch.Status.Usage.Settlement != (uc.Receipt{}) {
		return q, ErrConflict
	}
	epoch, err := meteredEpoch(ctx, tx, op.TargetNode, op.TargetInstallation, *ch.Status.Usage)
	if err != nil {
		return q, err
	}
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT state FROM pro_usage_epochs WHERE epoch_id=?`, epoch.ID).Scan(&state); err != nil {
		return q, err
	}
	if state != "open" || epoch.ConfigSHA != o.ActiveSHA || apply.view.RegisteredEpochID != epoch.ID {
		return q, ErrConflict
	}
	probe, err := usageledger.ProbeIdentityTx(ctx, tx, op.TargetNode, op.TargetInstallation, inbound)
	if err != nil {
		return q, err
	}
	email, err := uc.CounterEmail(probe.ID)
	if err != nil {
		return q, err
	}
	found := false
	for _, b := range epoch.Bindings {
		if b.ClientID == probe.ID && b.Email == email && !b.Billable && b.Role == uc.SystemProbeRole {
			found = true
		}
	}
	if !found {
		return q, ErrConflict
	}
	var port uint16
	for _, l := range apply.payload.Candidate.Listeners {
		if l.InboundID == inbound && l.Kind == "direct" {
			port = l.NodePort
		}
	}
	if port < 1024 {
		return q, ErrConflict
	}
	h := sha256.Sum256([]byte(probe.UUID))
	q = mx.ProtocolHealthRequest{Version: mx.ProtocolHealthVersion, HandoverID: opID, NodeID: op.TargetNode, InstallationID: op.TargetInstallation, RuntimeInstance: ch.Status.Instance, StageID: op.TargetStageID, ApplyCommandID: op.ApplyCommandID, InboundID: inbound, Port: port, CredentialSHA: hex.EncodeToString(h[:]), SystemProbeID: probe.ID, Expected: mx.Condition(o), Usage: *ch.Status.Usage}
	q.Expires = o.LeaseExpires
	return q, nil
}

// IssueMeteredHealthChallenge is called only by trusted HUB orchestration with
// an operator-approved endpoint and the target's authentication key. A retry
// intent is caller supplied, but the actual protocol nonce is HUB generated.
// Exact retries return the original signed request, including its expiration;
// they never extend a lease or refresh an expired challenge.
func (s *Store) IssueMeteredHealthChallenge(ctx context.Context, key, token []byte, intent, opID string, inbound uint64, endpoint mx.ProtocolHealthEndpoint) (mx.ProtocolHealthRequest, error) {
	var q mx.ProtocolHealthRequest
	if len(key) != 32 || len(token) < 32 || !routeID.MatchString(intent) || !routeID.MatchString(opID) || inbound == 0 || !endpoint.Valid() {
		return q, ErrInvalid
	}
	tx, err := s.healthTx(ctx)
	if err != nil {
		return q, err
	}
	defer tx.Rollback()
	fp := fingerprint("metered-health-intent-v1", struct {
		Op       string
		Inbound  uint64
		Endpoint string
	}{opID, inbound, endpoint.SHA()})
	var raw, old string
	err = tx.QueryRowContext(ctx, `SELECT intent_sha,request FROM pro_health_challenges WHERE intent_id=?`, intent).Scan(&old, &raw)
	if err == nil {
		if old != fp || json.Unmarshal([]byte(raw), &q) != nil {
			return mx.ProtocolHealthRequest{}, ErrConflict
		}
		want, e := mx.SignProtocolHealthRequest(token, q)
		if e != nil || want != q {
			return mx.ProtocolHealthRequest{}, ErrConflict
		}
		return q, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return q, err
	}
	now := time.Now().UnixNano()
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM pro_health_receipts WHERE handover_id=?)+(SELECT COUNT(*) FROM pro_health_challenges WHERE handover_id=? AND expires>?)`, opID, opID, now).Scan(&n); err != nil {
		return q, err
	}
	if n != 0 {
		return q, ErrConflict
	}
	q, err = healthCurrent(ctx, tx, key, opID, inbound, now)
	if err != nil {
		return mx.ProtocolHealthRequest{}, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return mx.ProtocolHealthRequest{}, err
	}
	if version == usageledger.HealthQueueBusinessSchemaVersion {
		v, binding, e := readHealthIntent(ctx, tx, intent)
		if e == nil {
			if v.HandoverID != opID || v.InboundID != inbound || v.EndpointSHA != endpoint.SHA() || v.NodeID != q.NodeID || v.InstallationID != q.InstallationID || binding != healthBinding(q) || v.Expires <= now {
				return mx.ProtocolHealthRequest{}, ErrConflict
			}
			if q.Expires > v.Expires {
				q.Expires = v.Expires
			}
		} else if !errors.Is(e, ErrNotFound) {
			return mx.ProtocolHealthRequest{}, e
		}
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return mx.ProtocolHealthRequest{}, err
	}
	q.ID, q.EndpointSHA, q.Issued = hex.EncodeToString(nonce[:]), endpoint.SHA(), now
	if max := now + int64(8*time.Second); q.Expires > max {
		q.Expires = max
	}
	q, err = mx.SignProtocolHealthRequest(token, q)
	if err != nil {
		return mx.ProtocolHealthRequest{}, err
	}
	data, _ := json.Marshal(q)
	if time.Now().UnixNano() >= q.Expires {
		return mx.ProtocolHealthRequest{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_health_challenges(id,intent_id,handover_id,intent_sha,request,expires) VALUES(?,?,?,?,?,?)`, q.ID, intent, opID, fp, string(data), q.Expires); err != nil {
		return mx.ProtocolHealthRequest{}, err
	}
	return q, tx.Commit()
}

// ConsumeMeteredHealthProof takes a challenge ID, not an Agent-chosen request.
// First consumption rechecks all live state in the same write transaction.
// An exact committed retry returns historical evidence even after expiration.
func (s *Store) ConsumeMeteredHealthProof(ctx context.Context, key, token []byte, challenge string, endpoint mx.ProtocolHealthEndpoint, proof mx.ProtocolHealthReceipt) (MeteredHealthEvidence, error) {
	return s.consumeMeteredHealthProof(ctx, key, token, challenge, endpoint, proof, time.Now)
}

func (s *Store) consumeMeteredHealthProof(ctx context.Context, key, token []byte, challenge string, endpoint mx.ProtocolHealthEndpoint, proof mx.ProtocolHealthReceipt, clock func() time.Time) (MeteredHealthEvidence, error) {
	var out MeteredHealthEvidence
	if len(key) != 32 || len(token) < 32 || !routeID.MatchString(challenge) || !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := s.healthTx(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var q mx.ProtocolHealthRequest
	var raw string
	if err = tx.QueryRowContext(ctx, `SELECT request FROM pro_health_challenges WHERE id=?`, challenge).Scan(&raw); err != nil {
		return out, err
	}
	if json.Unmarshal([]byte(raw), &q) != nil || q.ID != challenge || q.EndpointSHA != endpoint.SHA() {
		return out, ErrConflict
	}
	want, err := mx.SignProtocolHealthRequest(token, q)
	if err != nil || want != q {
		return out, ErrConflict
	}
	out = MeteredHealthEvidence{ChallengeID: challenge, HandoverID: q.HandoverID, RequestSHA: meteredHash(q), ReceiptSHA: meteredHash(proof)}
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT receipt,accepted FROM pro_health_receipts WHERE challenge_id=?`, challenge).Scan(&stored, &out.Accepted)
	data, _ := json.Marshal(proof)
	if err == nil {
		if stored != string(data) {
			return MeteredHealthEvidence{}, ErrConflict
		}
		out.Replayed = true
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return MeteredHealthEvidence{}, err
	}
	now := clock()
	if !mx.VerifyProtocolHealthReceipt(token, q, proof, now) {
		return MeteredHealthEvidence{}, ErrConflict
	}
	current, err := healthCurrent(ctx, tx, key, q.HandoverID, q.InboundID, now.UnixNano())
	if err != nil {
		return MeteredHealthEvidence{}, err
	}
	if current.Expires < q.Expires {
		return MeteredHealthEvidence{}, ErrConflict
	}
	current.ID, current.EndpointSHA, current.Issued, current.Expires, current.Signature = q.ID, q.EndpointSHA, q.Issued, q.Expires, q.Signature
	if current != q {
		return MeteredHealthEvidence{}, ErrConflict
	}
	out.Accepted = clock().UnixNano()
	if out.Accepted >= q.Expires || out.Accepted < now.UnixNano() {
		return MeteredHealthEvidence{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_health_receipts(challenge_id,handover_id,receipt,accepted) VALUES(?,?,?,?)`, challenge, q.HandoverID, string(data), out.Accepted); err != nil {
		return MeteredHealthEvidence{}, err
	}
	return out, tx.Commit()
}
