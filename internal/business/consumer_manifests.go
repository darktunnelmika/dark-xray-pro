package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type ConsumerManifestMember struct {
	InboundID       uint64 `json:"inbound_id"`
	OriginPort      uint16 `json:"origin_port,omitempty"`
	ConsumerAddress string `json:"consumer_address"`
	ConsumerPort    uint16 `json:"consumer_port"`
}

type ConsumerManifestMutation struct {
	EventID          string                   `json:"event_id"`
	Action           string                   `json:"action"`
	ExpectedRevision uint64                   `json:"expected_revision"`
	Members          []ConsumerManifestMember `json:"members,omitempty"`
}

// A manifest is owner intent for an exact consumer path, not proof that the
// path works. Only a new complete v3 round can authorize bounded delivery.
type ConsumerManifest struct {
	ID                  string                   `json:"id"`
	EventID             string                   `json:"event_id"`
	HandoverID          string                   `json:"handover_id"`
	NodeID              string                   `json:"node_id"`
	InstallationID      string                   `json:"installation_id"`
	Revision            uint64                   `json:"revision"`
	State               string                   `json:"state"`
	ConsumerManifestSHA string                   `json:"consumer_manifest_sha"`
	CreatedUnixNano     int64                    `json:"created_unix_nano"`
	Members             []ConsumerManifestMember `json:"members"`
	Current             bool                     `json:"current"`
	Replayed            bool                     `json:"replayed,omitempty"`
}

type ConsumerManifestInbound struct {
	InboundID  uint64 `json:"inbound_id"`
	OriginPort uint16 `json:"origin_port"`
}

type ConsumerManifestView struct {
	HandoverID        string                        `json:"handover_id"`
	Revision          uint64                        `json:"revision"`
	State             string                        `json:"state"`
	Manifest          *ConsumerManifest             `json:"manifest,omitempty"`
	RequiredInbounds  []ConsumerManifestInbound     `json:"required_inbounds"`
	ApprovedEndpoints []mx.ConsumerEndpointApproval `json:"approved_endpoints"`
}

type consumerManifestPolicy struct {
	approvals []mx.ConsumerEndpointApproval
	sha       string
}

// Startup-only. Approval policy is copied and cannot be changed on a running
// Store. Reopening without explicit approvals never authorizes a retained path.
func (s *Store) EnableConsumerManifests(ctx context.Context, approvals []mx.ConsumerEndpointApproval) error {
	if s.usage == nil || mx.ValidateConsumerEndpointApprovals(approvals) != nil {
		return ErrInvalid
	}
	copyApprovals := append([]mx.ConsumerEndpointApproval(nil), approvals...)
	sort.Slice(copyApprovals, func(i, j int) bool {
		a, b := copyApprovals[i], copyApprovals[j]
		return fmt.Sprintf("%s/%s/%020d/%s/%05d", a.NodeID, a.InstallationID, a.InboundID, a.Address, a.Port) < fmt.Sprintf("%s/%s/%020d/%s/%05d", b.NodeID, b.InstallationID, b.InboundID, b.Address, b.Port)
	})
	sha := fingerprint("consumer-endpoint-approvals-v1", copyApprovals)
	if s.consumerPolicy != nil {
		if s.consumerPolicy.sha != sha {
			return ErrConflict
		}
	}
	if err := s.usage.EnableConsumerManifests(ctx); err != nil {
		return err
	}
	if s.consumerPolicy == nil {
		s.consumerPolicy = &consumerManifestPolicy{approvals: copyApprovals, sha: sha}
	}
	return nil
}

type consumerManifestRow struct {
	ConsumerManifest
	ActorID     string `json:"actor_id"`
	Fingerprint string `json:"fingerprint"`
	BindingSHA  string `json:"binding_sha"`
}

func consumerManifestDigest(row consumerManifestRow) string {
	row.ConsumerManifestSHA, row.Current, row.Replayed = "", false, false
	return fingerprint("metered-consumer-manifest-v1", row)
}

func consumerAppliedBinding(state meteredHealthSnapshot) string {
	return fingerprint("metered-consumer-applied-v1", struct {
		Runtime                         mx.ProtocolHealthRequest
		Generation                      uint64
		ConfigSHA, CandidateManifestSHA string
	}{state.base, state.apply.payload.Generation, state.apply.payload.Candidate.SHA256, state.apply.payload.Candidate.ManifestSHA256})
}

func consumerManifestOwner(ctx context.Context, tx *sql.Tx, actorID string) error {
	if !idRE.MatchString(actorID) {
		return ErrInvalid
	}
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users a JOIN representatives r ON r.id=a.representative_id WHERE a.id=? AND a.role='owner' AND a.disabled=0 AND r.kind='owner' AND r.enabled=1`, actorID).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return ErrUnauthenticated
	}
	return nil
}

func (s *Store) consumerTx(ctx context.Context, key []byte) (*sql.Tx, error) {
	if len(key) != 32 || s.consumerPolicy == nil {
		return nil, ErrUnavailable
	}
	tx, err := s.subscriptionTx(ctx, key)
	if err != nil {
		return nil, err
	}
	var version int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil || !usageledger.HasConsumerManifestsSchema(version) {
		tx.Rollback()
		return nil, ErrUnavailable
	}
	return tx, nil
}

func readConsumerManifest(ctx context.Context, tx *sql.Tx, column, value string) (consumerManifestRow, error) {
	var out consumerManifestRow
	if column != "id" && column != "event_id" {
		return out, ErrInvalid
	}
	var raw string
	var id, event, actor, fp, op, node, install, state, sha, binding string
	var revision uint64
	var created int64
	err := tx.QueryRowContext(ctx, `SELECT id,event_id,actor_id,fingerprint,handover_id,node_id,installation_id,revision,state,manifest,manifest_sha,binding_sha,created FROM pro_consumer_manifests WHERE `+column+`=?`, value).Scan(&id, &event, &actor, &fp, &op, &node, &install, &revision, &state, &raw, &sha, &binding, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if len(raw) > 128<<10 || json.Unmarshal([]byte(raw), &out) != nil || !routeID.MatchString(id) || !eventRE.MatchString(event) || !idRE.MatchString(actor) || !routeSHA.MatchString(fp) || !routeID.MatchString(op) || !routeNodeID.MatchString(node) || !routeID.MatchString(install) || revision == 0 || revision >= routeMax || (state != "active" && state != "revoked") || !routeSHA.MatchString(sha) || !routeSHA.MatchString(binding) || created <= 0 || out.ID != id || out.EventID != event || out.ActorID != actor || out.Fingerprint != fp || out.HandoverID != op || out.NodeID != node || out.InstallationID != install || out.Revision != revision || out.State != state || out.ConsumerManifestSHA != sha || out.BindingSHA != binding || out.CreatedUnixNano != created || out.Current || out.Replayed || consumerManifestDigest(out) != sha || len(out.Members) == 0 || len(out.Members) > maxMeteredHealthMembers {
		return consumerManifestRow{}, ErrUnavailable
	}
	seen := map[string]bool{}
	for i, m := range out.Members {
		address := fmt.Sprintf("%s:%d", m.ConsumerAddress, m.ConsumerPort)
		if m.InboundID == 0 || m.OriginPort < 1024 || !mx.ValidateConsumerProtocolEndpoint(m.ConsumerAddress, m.ConsumerPort) || seen[address] || (i > 0 && out.Members[i-1].InboundID >= m.InboundID) {
			return consumerManifestRow{}, ErrUnavailable
		}
		seen[address] = true
	}
	return out, nil
}

func readConsumerManifestHead(ctx context.Context, tx *sql.Tx, opID string) (consumerManifestRow, error) {
	var id string
	var revision, latest uint64
	err := tx.QueryRowContext(ctx, `SELECT manifest_id,revision FROM pro_consumer_manifest_heads WHERE handover_id=?`, opID).Scan(&id, &revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return consumerManifestRow{}, err
	}
	if e := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM pro_consumer_manifests WHERE handover_id=?`, opID).Scan(&latest); e != nil {
		return consumerManifestRow{}, e
	}
	if latest != revision {
		return consumerManifestRow{}, ErrUnavailable
	}
	if errors.Is(err, sql.ErrNoRows) {
		return consumerManifestRow{}, ErrNotFound
	}
	r, err := readConsumerManifest(ctx, tx, "id", id)
	if err == nil && (r.HandoverID != opID || r.Revision != revision) {
		err = ErrUnavailable
	}
	return r, err
}

func consumerSchema(ctx context.Context, tx *sql.Tx) (bool, error) {
	var version int
	err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version)
	return usageledger.HasConsumerManifestsSchema(version), err
}

func consumerManifestExists(ctx context.Context, tx *sql.Tx, opID string) (bool, error) {
	enabled, err := consumerSchema(ctx, tx)
	if err != nil || !enabled {
		return false, err
	}
	_, err = readConsumerManifestHead(ctx, tx, opID)
	if errors.Is(err, ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}

func consumerEventUnused(ctx context.Context, tx *sql.Tx, eventID string) error {
	enabled, err := consumerSchema(ctx, tx)
	if err != nil || !enabled {
		return err
	}
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_consumer_manifests WHERE event_id=?`, eventID).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) consumerApproved(node, install string, member ConsumerManifestMember) bool {
	if s.consumerPolicy == nil {
		return false
	}
	for _, a := range s.consumerPolicy.approvals {
		if a.NodeID == node && a.InstallationID == install && a.InboundID == member.InboundID && a.Address == member.ConsumerAddress && a.Port == member.ConsumerPort {
			return true
		}
	}
	return false
}

// A retained head permanently opts this handover out of origin-only evidence.
// Revocation, a missing startup approval, or changed runtime never falls back.
func (s *Store) currentConsumerManifest(ctx context.Context, tx *sql.Tx, state meteredHealthSnapshot) (*consumerManifestRow, error) {
	enabled, err := consumerSchema(ctx, tx)
	if err != nil || !enabled {
		return nil, err
	}
	row, err := readConsumerManifestHead(ctx, tx, state.base.HandoverID)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.State != "active" || row.NodeID != state.base.NodeID || row.InstallationID != state.base.InstallationID || row.BindingSHA != consumerAppliedBinding(state) || len(row.Members) != len(state.apply.payload.Candidate.Listeners) || s.consumerPolicy == nil {
		return nil, ErrConflict
	}
	for _, m := range row.Members {
		found := false
		for _, l := range state.apply.payload.Candidate.Listeners {
			if l.InboundID == m.InboundID && l.Kind == "direct" && l.NodePort == m.OriginPort {
				found = true
			}
		}
		if !found || !s.consumerApproved(row.NodeID, row.InstallationID, m) {
			return nil, ErrConflict
		}
	}
	return &row, nil
}

func consumerHealthRequest(q mx.ProtocolHealthRequest, row *consumerManifestRow) (mx.ProtocolHealthRequest, error) {
	if row == nil {
		return q, nil
	}
	for _, m := range row.Members {
		if m.InboundID == q.InboundID && m.OriginPort == q.Port {
			q.Version = mx.ConsumerProtocolHealthVersion
			q.ConsumerManifestSHA, q.ConsumerAddress, q.ConsumerPort = row.ConsumerManifestSHA, m.ConsumerAddress, m.ConsumerPort
			return q, nil
		}
	}
	return mx.ProtocolHealthRequest{}, ErrConflict
}

func (s *Store) MeteredConsumerManifest(ctx context.Context, key []byte, actorID, handoverID string) (ConsumerManifestView, error) {
	out := ConsumerManifestView{HandoverID: handoverID, State: "absent", RequiredInbounds: []ConsumerManifestInbound{}, ApprovedEndpoints: []mx.ConsumerEndpointApproval{}}
	if !routeID.MatchString(handoverID) {
		return out, ErrInvalid
	}
	tx, err := s.consumerTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = consumerManifestOwner(ctx, tx, actorID); err != nil {
		return out, err
	}
	op, _, err := readMeteredHandover(ctx, tx, handoverID)
	if err != nil {
		return out, err
	}
	for _, a := range s.consumerPolicy.approvals {
		if a.NodeID == op.TargetNode && a.InstallationID == op.TargetInstallation {
			out.ApprovedEndpoints = append(out.ApprovedEndpoints, a)
		}
	}
	state, stateErr := s.loadMeteredHealthSnapshot(ctx, tx, key, handoverID, time.Now().UnixNano())
	if stateErr == nil {
		for _, l := range state.apply.payload.Candidate.Listeners {
			out.RequiredInbounds = append(out.RequiredInbounds, ConsumerManifestInbound{l.InboundID, l.NodePort})
		}
		sort.Slice(out.RequiredInbounds, func(i, j int) bool { return out.RequiredInbounds[i].InboundID < out.RequiredInbounds[j].InboundID })
	}
	row, err := readConsumerManifestHead(ctx, tx, handoverID)
	if err == nil {
		out.Revision, out.State = row.Revision, row.State
		if stateErr == nil {
			current, e := s.currentConsumerManifest(ctx, tx, state)
			row.Current = e == nil && current != nil && current.ID == row.ID
		}
		out.Manifest = &row.ConsumerManifest
	} else if !errors.Is(err, ErrNotFound) {
		return out, err
	}
	return out, tx.Commit()
}

func (s *Store) MutateMeteredConsumerManifest(ctx context.Context, key []byte, actorID, handoverID string, in ConsumerManifestMutation) (ConsumerManifest, error) {
	var out ConsumerManifest
	if !routeID.MatchString(handoverID) || !eventRE.MatchString(in.EventID) || in.ExpectedRevision >= routeMax-1 || (in.Action != "replace" && in.Action != "revoke") || len(in.Members) > maxMeteredHealthMembers || (in.Action == "replace" && len(in.Members) == 0) || (in.Action == "revoke" && len(in.Members) != 0) {
		return out, ErrInvalid
	}
	in.Members = append([]ConsumerManifestMember(nil), in.Members...)
	sort.Slice(in.Members, func(i, j int) bool { return in.Members[i].InboundID < in.Members[j].InboundID })
	seen := map[string]bool{}
	for i, m := range in.Members {
		address := fmt.Sprintf("%s:%d", m.ConsumerAddress, m.ConsumerPort)
		if m.InboundID == 0 || m.OriginPort != 0 || !mx.ValidateConsumerProtocolEndpoint(m.ConsumerAddress, m.ConsumerPort) || seen[address] || (i > 0 && in.Members[i-1].InboundID == m.InboundID) {
			return out, ErrInvalid
		}
		seen[address] = true
	}
	tx, err := s.consumerTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = consumerManifestOwner(ctx, tx, actorID); err != nil {
		return out, err
	}
	fp := fingerprint("metered-consumer-intent-v1", struct {
		ActorID, HandoverID string
		Input               ConsumerManifestMutation
	}{actorID, handoverID, in})
	head, headErr := readConsumerManifestHead(ctx, tx, handoverID)
	if headErr != nil && !errors.Is(headErr, ErrNotFound) {
		return out, headErr
	}
	prior, err := readConsumerManifest(ctx, tx, "event_id", in.EventID)
	if err == nil {
		if prior.Fingerprint != fp || prior.HandoverID != handoverID {
			return out, ErrConflict
		}
		if headErr == nil && head.ID == prior.ID && prior.State == "active" {
			state, e := s.loadMeteredHealthSnapshot(ctx, tx, key, handoverID, time.Now().UnixNano())
			if e == nil {
				current, e := s.currentConsumerManifest(ctx, tx, state)
				prior.Current = e == nil && current != nil && current.ID == prior.ID
			}
		}
		prior.Replayed = true
		return prior.ConsumerManifest, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return out, err
	}
	if head.Revision != in.ExpectedRevision || (in.Action == "revoke" && (headErr != nil || head.State != "active")) {
		return out, ErrConflict
	}
	var used int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM pro_subscription_token_events WHERE event_id=?)+(SELECT COUNT(*) FROM pro_subscription_snapshots WHERE event_id=?)`, in.EventID, in.EventID).Scan(&used); err != nil {
		return out, err
	}
	if used != 0 {
		return out, ErrConflict
	}
	id, err := subscriptionID()
	if err != nil {
		return out, err
	}
	now := time.Now().UnixNano()
	row := consumerManifestRow{ConsumerManifest: ConsumerManifest{ID: id, EventID: in.EventID, HandoverID: handoverID, Revision: head.Revision + 1, State: "revoked", CreatedUnixNano: now}, ActorID: actorID, Fingerprint: fp}
	var lease int64
	if in.Action == "replace" {
		state, e := s.loadMeteredHealthSnapshot(ctx, tx, key, handoverID, now)
		if e != nil {
			return out, e
		}
		if len(in.Members) != len(state.apply.payload.Candidate.Listeners) || len(in.Members) != len(state.apply.payload.Request.Drafts) {
			return out, ErrConflict
		}
		row.State, row.NodeID, row.InstallationID, row.BindingSHA = "active", state.base.NodeID, state.base.InstallationID, consumerAppliedBinding(state)
		row.Members = in.Members
		for i, m := range row.Members {
			for _, l := range state.apply.payload.Candidate.Listeners {
				if l.InboundID == m.InboundID && l.Kind == "direct" {
					row.Members[i].OriginPort = l.NodePort
				}
			}
			if row.Members[i].OriginPort < 1024 || !s.consumerApproved(row.NodeID, row.InstallationID, m) {
				return out, ErrConflict
			}
			if _, e := state.request(ctx, tx, m.InboundID); e != nil {
				return out, e
			}
		}
		lease = state.base.Expires
	} else {
		row.NodeID, row.InstallationID, row.BindingSHA = head.NodeID, head.InstallationID, head.BindingSHA
		row.Members = append([]ConsumerManifestMember(nil), head.Members...)
	}
	row.ConsumerManifestSHA = consumerManifestDigest(row)
	raw, err := json.Marshal(row)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_consumer_manifests(id,event_id,actor_id,fingerprint,handover_id,node_id,installation_id,revision,state,manifest,manifest_sha,binding_sha,created) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.ID, row.EventID, row.ActorID, row.Fingerprint, row.HandoverID, row.NodeID, row.InstallationID, row.Revision, row.State, string(raw), row.ConsumerManifestSHA, row.BindingSHA, row.CreatedUnixNano); err != nil {
		return out, err
	}
	if head.Revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO pro_consumer_manifest_heads(handover_id,revision,manifest_id) VALUES(?,?,?)`, handoverID, row.Revision, row.ID)
	} else {
		var changed sql.Result
		changed, err = tx.ExecContext(ctx, `UPDATE pro_consumer_manifest_heads SET revision=?,manifest_id=? WHERE handover_id=? AND revision=?`, row.Revision, row.ID, handoverID, head.Revision)
		if err == nil {
			if n, e := changed.RowsAffected(); e != nil || n != 1 {
				return out, ErrConflict
			}
		}
	}
	if err != nil {
		return out, err
	}
	if end := time.Now().UnixNano(); end < now || (lease != 0 && end >= lease) {
		return out, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	row.Current = row.State == "active" && time.Now().UnixNano() < lease
	return row.ConsumerManifest, nil
}
