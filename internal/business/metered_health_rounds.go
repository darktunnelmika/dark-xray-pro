package business

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

const maxMeteredHealthMembers = 256

// A round is a complete, immutable set of required destination inbounds. V2
// checks the origin; v3 additionally binds a separately approved consumer path.
// Neither form grants public publication or extends runtime authority.
type MeteredHealthRound struct {
	ID                       string                     `json:"id"`
	HandoverID               string                     `json:"handover_id"`
	NodeID                   string                     `json:"node_id"`
	InstallationID           string                     `json:"installation_id"`
	EndpointSHA              string                     `json:"endpoint_sha"`
	ManifestSHA              string                     `json:"manifest_sha"`
	ConsumerManifestID       string                     `json:"consumer_manifest_id,omitempty"`
	ConsumerManifestRevision uint64                     `json:"consumer_manifest_revision,omitempty"`
	ConsumerManifestSHA      string                     `json:"consumer_manifest_sha,omitempty"`
	Created                  int64                      `json:"created_unix_nano"`
	Expires                  int64                      `json:"expires_unix_nano"`
	Members                  []MeteredHealthRoundMember `json:"members"`
	State                    string                     `json:"state"`
	Current                  bool                       `json:"current"`
}

type MeteredHealthRoundMember struct {
	InboundID       uint64 `json:"inbound_id"`
	IntentID        string `json:"intent_id"`
	Port            uint16 `json:"port"`
	ConsumerAddress string `json:"consumer_address,omitempty"`
	ConsumerPort    uint16 `json:"consumer_port,omitempty"`
	State           string `json:"state"`
	ChallengeID     string `json:"challenge_id,omitempty"`
	Accepted        int64  `json:"accepted_unix_nano,omitempty"`
}

type healthRoundBinding struct {
	InboundID       uint64 `json:"inbound_id"`
	Port            uint16 `json:"port"`
	Binding         string `json:"binding"`
	ConsumerAddress string `json:"consumer_address,omitempty"`
	ConsumerPort    uint16 `json:"consumer_port,omitempty"`
}
type healthRoundManifest struct {
	Version                  int                  `json:"version"`
	Members                  []healthRoundBinding `json:"members"`
	ConsumerManifestID       string               `json:"consumer_manifest_id,omitempty"`
	ConsumerManifestRevision uint64               `json:"consumer_manifest_revision,omitempty"`
	ConsumerManifestSHA      string               `json:"consumer_manifest_sha,omitempty"`
}
type healthRoundRow struct {
	MeteredHealthRound
	manifest healthRoundManifest
}

func (s *Store) EnableMeteredHealthRounds(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	return s.usage.EnableHealthRounds(ctx)
}

// Enumerate the exact applied candidate AND reviewed draft set. Every listener
// must be represented once; no caller-selected subset can omit a failing route.
func (s *Store) currentHealthRound(ctx context.Context, tx *sql.Tx, key []byte, opID string, now int64, endpoint *mx.ProtocolHealthEndpoint) (healthRoundManifest, []mx.ProtocolHealthRequest, error) {
	empty := healthRoundManifest{}
	state, err := s.loadMeteredHealthSnapshot(ctx, tx, key, opID, now)
	if err != nil {
		return empty, nil, err
	}
	apply := state.apply
	refs := apply.payload.Request.Drafts
	if len(refs) == 0 || len(refs) > maxMeteredHealthMembers || len(apply.payload.Candidate.Listeners) != len(refs) {
		return empty, nil, ErrConflict
	}
	wanted := make(map[uint64]bool, len(refs))
	for _, r := range refs {
		if r.InboundID == 0 || wanted[r.InboundID] {
			return empty, nil, ErrConflict
		}
		wanted[r.InboundID] = true
	}
	ports := make(map[uint16]bool, len(refs))
	ids := make([]uint64, 0, len(refs))
	for _, l := range apply.payload.Candidate.Listeners {
		if !wanted[l.InboundID] || l.Kind != "direct" || l.NodePort < 1024 || ports[l.NodePort] {
			return empty, nil, ErrConflict
		}
		delete(wanted, l.InboundID)
		ports[l.NodePort] = true
		ids = append(ids, l.InboundID)
	}
	if len(wanted) != 0 {
		return empty, nil, ErrConflict
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	m := healthRoundManifest{Version: 1, Members: make([]healthRoundBinding, 0, len(ids))}
	consumer, err := s.currentConsumerManifest(ctx, tx, state)
	if err != nil {
		return empty, nil, err
	}
	if consumer != nil {
		m.Version = 2
		m.ConsumerManifestID, m.ConsumerManifestRevision, m.ConsumerManifestSHA = consumer.ID, consumer.Revision, consumer.ConsumerManifestSHA
	}
	requests := make([]mx.ProtocolHealthRequest, 0, len(ids))
	for _, id := range ids {
		q, e := state.request(ctx, tx, id)
		if e != nil {
			return empty, nil, e
		}
		q, e = consumerHealthRequest(q, consumer)
		if e != nil {
			return empty, nil, e
		}
		if endpoint != nil {
			if e = mx.ValidateProtocolHealthSupport(apply.payload.Candidate, q, *endpoint); e != nil {
				return empty, nil, ErrConflict
			}
		}
		m.Members = append(m.Members, healthRoundBinding{InboundID: id, Port: q.Port, Binding: healthBinding(q), ConsumerAddress: q.ConsumerAddress, ConsumerPort: q.ConsumerPort})
		requests = append(requests, q)
	}
	return m, requests, nil
}

func readHealthRound(ctx context.Context, tx *sql.Tx, id string) (healthRoundRow, error) {
	var row healthRoundRow
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT id,handover_id,node_id,installation_id,endpoint_sha,manifest,manifest_sha,created,expires FROM pro_health_rounds WHERE id=?`, id).Scan(&row.ID, &row.HandoverID, &row.NodeID, &row.InstallationID, &row.EndpointSHA, &raw, &row.ManifestSHA, &row.Created, &row.Expires)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, err
	}
	if !routeID.MatchString(row.ID) || !routeID.MatchString(row.HandoverID) || !routeNodeID.MatchString(row.NodeID) || !routeID.MatchString(row.InstallationID) || !routeSHA.MatchString(row.EndpointSHA) || json.Unmarshal([]byte(raw), &row.manifest) != nil || (row.manifest.Version != 1 && row.manifest.Version != 2) || meteredHash(row.manifest) != row.ManifestSHA || len(row.manifest.Members) == 0 || len(row.manifest.Members) > maxMeteredHealthMembers || row.Created <= 0 || row.Expires <= row.Created || row.Expires-row.Created > int64(8*time.Second) {
		return row, ErrUnavailable
	}
	expectedProtocolVersion := mx.ProtocolHealthVersion
	if row.manifest.Version == 2 {
		if !routeID.MatchString(row.manifest.ConsumerManifestID) || row.manifest.ConsumerManifestRevision == 0 || row.manifest.ConsumerManifestRevision >= routeMax || !routeSHA.MatchString(row.manifest.ConsumerManifestSHA) {
			return row, ErrUnavailable
		}
		consumer, e := readConsumerManifest(ctx, tx, "id", row.manifest.ConsumerManifestID)
		if e != nil || consumer.State != "active" || consumer.HandoverID != row.HandoverID || consumer.NodeID != row.NodeID || consumer.InstallationID != row.InstallationID || consumer.Revision != row.manifest.ConsumerManifestRevision || consumer.ConsumerManifestSHA != row.manifest.ConsumerManifestSHA || len(consumer.Members) != len(row.manifest.Members) {
			return row, ErrUnavailable
		}
		for i, m := range consumer.Members {
			b := row.manifest.Members[i]
			if m.InboundID != b.InboundID || m.OriginPort != b.Port || m.ConsumerAddress != b.ConsumerAddress || m.ConsumerPort != b.ConsumerPort {
				return row, ErrUnavailable
			}
		}
		row.ConsumerManifestID, row.ConsumerManifestRevision, row.ConsumerManifestSHA = consumer.ID, consumer.Revision, consumer.ConsumerManifestSHA
		expectedProtocolVersion = mx.ConsumerProtocolHealthVersion
	} else if row.manifest.ConsumerManifestID != "" || row.manifest.ConsumerManifestRevision != 0 || row.manifest.ConsumerManifestSHA != "" {
		return row, ErrUnavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT m.inbound_id,m.intent_id,m.binding,i.handover_id,i.node_id,i.installation_id,i.inbound_id,i.endpoint_sha,i.binding,i.created,i.expires,c.id,c.handover_id,c.intent_sha,c.request,c.expires,r.challenge_id,r.receipt,r.accepted FROM pro_health_round_members m JOIN pro_health_intents i ON i.id=m.intent_id LEFT JOIN pro_health_challenges c ON c.intent_id=m.intent_id LEFT JOIN pro_health_round_receipts r ON r.round_id=m.round_id AND r.inbound_id=m.inbound_id WHERE m.round_id=? ORDER BY m.inbound_id`, id)
	if err != nil {
		return row, err
	}
	defer rows.Close()
	ports := make(map[uint16]bool, len(row.manifest.Members))
	for rows.Next() {
		var v MeteredHealthRoundMember
		var binding, op, node, install, endpoint, intentBinding string
		var inbound uint64
		var created, expires int64
		var challengeID, challengeOp, intentSHA, requestRaw, receiptID, receiptRaw sql.NullString
		var challengeExpires, accepted sql.NullInt64
		if err = rows.Scan(&v.InboundID, &v.IntentID, &binding, &op, &node, &install, &inbound, &endpoint, &intentBinding, &created, &expires, &challengeID, &challengeOp, &intentSHA, &requestRaw, &challengeExpires, &receiptID, &receiptRaw, &accepted); err != nil {
			return row, err
		}
		i := len(row.Members)
		if i >= len(row.manifest.Members) {
			return row, ErrUnavailable
		}
		b := row.manifest.Members[i]
		if b.InboundID == 0 || b.Port < 1024 || ports[b.Port] || !routeSHA.MatchString(b.Binding) || v.InboundID != b.InboundID || !routeID.MatchString(v.IntentID) || binding != b.Binding || op != row.HandoverID || node != row.NodeID || install != row.InstallationID || inbound != v.InboundID || endpoint != row.EndpointSHA || intentBinding != binding || created != row.Created || expires != row.Expires || (i > 0 && row.Members[i-1].InboundID >= v.InboundID) {
			return row, ErrUnavailable
		}
		ports[b.Port] = true
		v.Port, v.State = b.Port, "pending"
		v.ConsumerAddress, v.ConsumerPort = b.ConsumerAddress, b.ConsumerPort
		if (expectedProtocolVersion == mx.ProtocolHealthVersion && (b.ConsumerAddress != "" || b.ConsumerPort != 0)) || (expectedProtocolVersion == mx.ConsumerProtocolHealthVersion && !mx.ValidateConsumerProtocolEndpoint(b.ConsumerAddress, b.ConsumerPort)) {
			return row, ErrUnavailable
		}
		var q mx.ProtocolHealthRequest
		if challengeID.Valid {
			v.ChallengeID = challengeID.String
			fp := fingerprint("metered-health-intent-v1", struct {
				Op       string
				Inbound  uint64
				Endpoint string
			}{row.HandoverID, v.InboundID, row.EndpointSHA})
			if !routeID.MatchString(v.ChallengeID) || !challengeOp.Valid || challengeOp.String != row.HandoverID || !intentSHA.Valid || intentSHA.String != fp || !requestRaw.Valid || !challengeExpires.Valid || challengeExpires.Int64 != row.Expires || json.Unmarshal([]byte(requestRaw.String), &q) != nil || q.Version != expectedProtocolVersion || q.ConsumerManifestSHA != row.ConsumerManifestSHA || q.ConsumerAddress != b.ConsumerAddress || q.ConsumerPort != b.ConsumerPort || q.ID != v.ChallengeID || q.HandoverID != row.HandoverID || q.NodeID != row.NodeID || q.InstallationID != row.InstallationID || q.InboundID != v.InboundID || q.Port != v.Port || q.EndpointSHA != row.EndpointSHA || healthBinding(q) != binding || !routeSHA.MatchString(q.Signature) || q.Issued < row.Created || q.Issued >= q.Expires || q.Expires != row.Expires {
				return row, ErrUnavailable
			}
		}
		if receiptID.Valid {
			var p mx.ProtocolHealthReceipt
			responseSHA := sha256.Sum256([]byte("DARK-PRO-PROTOCOL-HEALTH-v1\n" + q.ID + "\n"))
			// These are structural and binding checks over retained evidence.
			// Signature authentication happened on consumption with the Agent's
			// token; that secret is deliberately unavailable to the owner audit.
			if !challengeID.Valid || receiptID.String != v.ChallengeID || !receiptRaw.Valid || !accepted.Valid || accepted.Int64 < row.Created || accepted.Int64 >= row.Expires || json.Unmarshal([]byte(receiptRaw.String), &p) != nil || p.Version != q.Version || p.RequestSHA != meteredHash(q) || p.ResponseSHA != hex.EncodeToString(responseSHA[:]) || !routeSHA.MatchString(p.Signature) || p.Completed < q.Issued || p.Completed >= q.Expires || p.Completed > accepted.Int64 {
				return row, ErrUnavailable
			}
			v.Accepted, v.State = accepted.Int64, "accepted"
		}
		row.Members = append(row.Members, v)
	}
	if err = rows.Err(); err != nil {
		return row, err
	}
	if len(row.Members) != len(row.manifest.Members) {
		return row, ErrUnavailable
	}
	return row, nil
}

func (s *Store) assessHealthRound(ctx context.Context, tx *sql.Tx, key []byte, row healthRoundRow, now int64) MeteredHealthRound {
	v := row.MeteredHealthRound
	v.Current = false
	if now >= v.Expires {
		v.State = "expired"
		return v
	}
	m, _, err := s.currentHealthRound(ctx, tx, key, v.HandoverID, now, nil)
	if err != nil || now < v.Created || meteredHash(m) != v.ManifestSHA {
		v.State = "state_changed"
		return v
	}
	v.State = "complete"
	for _, member := range v.Members {
		if member.State != "accepted" {
			v.State = "pending"
			break
		}
	}
	// Derivation can take time. Never label evidence current after its deadline.
	if time.Now().UnixNano() >= v.Expires {
		v.State = "expired"
	} else {
		v.Current = v.State == "complete"
	}
	return v
}

// A caller supplies only its idempotency key. The required set and bindings are
// derived and committed together under the SQLite writer lock. Exact retries
// retain their old deadline; a later round always requires new per-member proof.
func (s *Store) QueueMeteredHealthRound(ctx context.Context, key []byte, id, op string, endpoint mx.ProtocolHealthEndpoint) (MeteredHealthRound, error) {
	return s.queueMeteredHealthRound(ctx, key, id, op, endpoint, time.Now)
}

func (s *Store) queueMeteredHealthRound(ctx context.Context, key []byte, id, op string, endpoint mx.ProtocolHealthEndpoint, clock func() time.Time) (MeteredHealthRound, error) {
	return s.queueHealthRound(ctx, key, id, op, "", 0, endpoint, clock)
}

func (s *Store) QueueMeteredConsumerHealthRound(ctx context.Context, key []byte, id, op, consumerManifestID string, consumerManifestRevision uint64, endpoint mx.ProtocolHealthEndpoint) (MeteredHealthRound, error) {
	if !routeID.MatchString(consumerManifestID) || consumerManifestRevision == 0 || consumerManifestRevision >= routeMax {
		return MeteredHealthRound{}, ErrInvalid
	}
	return s.queueHealthRound(ctx, key, id, op, consumerManifestID, consumerManifestRevision, endpoint, time.Now)
}

func (s *Store) queueHealthRound(ctx context.Context, key []byte, id, op, consumerManifestID string, consumerManifestRevision uint64, endpoint mx.ProtocolHealthEndpoint, clock func() time.Time) (MeteredHealthRound, error) {
	empty := MeteredHealthRound{}
	if len(key) != 32 || !routeID.MatchString(id) || !routeID.MatchString(op) || !endpoint.Valid() {
		return empty, ErrInvalid
	}
	tx, err := s.healthTx(ctx)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	if err = healthIDNotInitial(ctx, tx, id); err != nil {
		return empty, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return empty, err
	}
	if !usageledger.HasHealthRoundsSchema(version) {
		return empty, ErrUnavailable
	}
	if consumerManifestID == "" {
		if exists, e := consumerManifestExists(ctx, tx, op); e != nil || exists {
			if e != nil {
				return empty, e
			}
			return empty, ErrConflict
		}
	} else if !usageledger.HasConsumerManifestsSchema(version) || s.consumerPolicy == nil {
		return empty, ErrUnavailable
	}
	row, err := readHealthRound(ctx, tx, id)
	if err == nil {
		if row.HandoverID != op || row.EndpointSHA != endpoint.SHA() || row.ConsumerManifestID != consumerManifestID || row.ConsumerManifestRevision != consumerManifestRevision {
			return empty, ErrConflict
		}
		out := s.assessHealthRound(ctx, tx, key, row, clock().UnixNano())
		return out, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return empty, err
	}
	now := clock().UnixNano()
	var n int
	// Historical legacy receipts are preserved but cannot count as round proof.
	// Pending legacy work cannot compete with a round on the same target.
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM pro_health_rounds WHERE handover_id=? AND expires>?)+(SELECT COUNT(*) FROM pro_health_intents WHERE handover_id=? AND expires>?)+(SELECT COUNT(*) FROM pro_health_challenges WHERE handover_id=? AND expires>?)`, op, now, op, now, op, now).Scan(&n); err != nil {
		return empty, err
	}
	if n != 0 {
		return empty, ErrConflict
	}
	m, requests, err := s.currentHealthRound(ctx, tx, key, op, now, &endpoint)
	if err != nil {
		return empty, err
	}
	if m.ConsumerManifestID != consumerManifestID || m.ConsumerManifestRevision != consumerManifestRevision {
		return empty, ErrConflict
	}
	expires := now + int64(8*time.Second)
	for _, q := range requests {
		if expires > q.Expires {
			expires = q.Expires
		}
	}
	v := MeteredHealthRound{ID: id, HandoverID: op, NodeID: requests[0].NodeID, InstallationID: requests[0].InstallationID, EndpointSHA: endpoint.SHA(), ManifestSHA: meteredHash(m), ConsumerManifestID: m.ConsumerManifestID, ConsumerManifestRevision: m.ConsumerManifestRevision, ConsumerManifestSHA: m.ConsumerManifestSHA, Created: now, Expires: expires, State: "pending", Members: make([]MeteredHealthRoundMember, 0, len(m.Members))}
	raw, _ := json.Marshal(m)
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_health_rounds(id,handover_id,node_id,installation_id,endpoint_sha,manifest,manifest_sha,created,expires) VALUES(?,?,?,?,?,?,?,?,?)`, id, op, v.NodeID, v.InstallationID, v.EndpointSHA, string(raw), v.ManifestSHA, now, expires); err != nil {
		return empty, err
	}
	for _, b := range m.Members {
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			return empty, err
		}
		intent := hex.EncodeToString(nonce[:])
		if err = healthIDNotInitial(ctx, tx, intent); err != nil {
			return empty, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pro_health_intents(id,handover_id,node_id,installation_id,inbound_id,endpoint_sha,binding,expires,created) VALUES(?,?,?,?,?,?,?,?,?)`, intent, op, v.NodeID, v.InstallationID, b.InboundID, v.EndpointSHA, b.Binding, expires, now); err != nil {
			return empty, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pro_health_round_members(round_id,inbound_id,intent_id,binding) VALUES(?,?,?,?)`, id, b.InboundID, intent, b.Binding); err != nil {
			return empty, err
		}
		v.Members = append(v.Members, MeteredHealthRoundMember{InboundID: b.InboundID, IntentID: intent, Port: b.Port, ConsumerAddress: b.ConsumerAddress, ConsumerPort: b.ConsumerPort, State: "pending"})
	}
	if end := clock().UnixNano(); end < now || end >= expires {
		return empty, ErrConflict
	}
	return v, tx.Commit()
}

// Look up membership from the immutable intent, never from an Agent field.
func healthIntentRound(ctx context.Context, tx *sql.Tx, intent string) (string, error) {
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return "", err
	}
	if !usageledger.HasHealthRoundsSchema(version) {
		return "", nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT round_id FROM pro_health_round_members WHERE intent_id=?`, intent).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// Once the owner uses complete rounds, old single-inbound work may only be
// replayed historically. It must not create a second path to partial approval.
func legacyHealthAllowed(ctx context.Context, tx *sql.Tx, op string) error {
	if exists, err := consumerManifestExists(ctx, tx, op); err != nil || exists {
		if err != nil {
			return err
		}
		return ErrConflict
	}
	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if !usageledger.HasHealthRoundsSchema(version) {
		return nil
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_health_rounds WHERE handover_id=?`, op).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return ErrConflict
	}
	return nil
}

// An individual result is admissible only while its entire original set still
// matches. Members from different rounds are never combined into coverage.
func (s *Store) liveHealthRoundMember(ctx context.Context, tx *sql.Tx, key []byte, round, intent, op string, inbound uint64, endpoint string, now int64) (int64, error) {
	row, err := readHealthRound(ctx, tx, round)
	if err != nil {
		return 0, err
	}
	if row.HandoverID != op || row.EndpointSHA != endpoint || now < row.Created || now >= row.Expires {
		return 0, ErrConflict
	}
	member := false
	for _, m := range row.Members {
		if m.IntentID == intent && m.InboundID == inbound && m.State == "pending" {
			member = true
		}
	}
	if !member {
		return 0, ErrConflict
	}
	m, _, err := s.currentHealthRound(ctx, tx, key, op, now, nil)
	if err != nil {
		return 0, err
	}
	if meteredHash(m) != row.ManifestSHA {
		return 0, ErrConflict
	}
	return row.Expires, nil
}
