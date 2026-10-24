package business

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
)

const initialSnapshotColumns = `id,client_id,inbound_id,promotion_event_id,round_id,initial_apply_sha,node_id,installation_id,revision,created,expires,event_id,actor_id,fingerprint,endpoint_sha,manifest_sha,binding_sha,body_sha,payload_enc`

func readInitialSubscriptionSnapshot(ctx context.Context, tx *sql.Tx, column, value string) (subscriptionSnapshotRow, error) {
	var row subscriptionSnapshotRow
	if column != "id" && column != "event_id" {
		return row, ErrInvalid
	}
	err := tx.QueryRowContext(ctx, `SELECT `+initialSnapshotColumns+` FROM pro_initial_snapshots WHERE `+column+`=?`, value).Scan(&row.ID, &row.ClientID, &row.InboundID, &row.PromotionEventID, &row.RoundID, &row.InitialApplySHA, &row.NodeID, &row.InstallationID, &row.Revision, &row.CreatedUnixNano, &row.ExpiresUnixNano, &row.EventID, &row.ActorID, &row.Fingerprint, &row.EndpointSHA, &row.ManifestSHA, &row.BindingSHA, &row.BodySHA, &row.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return row, ErrNotFound
	}
	if err != nil {
		return row, err
	}
	row.Scope, row.RouteCount = InitialApplyHealthScope, 1
	if !routeID.MatchString(row.ID) || !idRE.MatchString(row.ClientID) || row.InboundID == 0 || !eventRE.MatchString(row.PromotionEventID) || !routeID.MatchString(row.RoundID) || !idRE.MatchString(row.NodeID) || !routeID.MatchString(row.InstallationID) || !idRE.MatchString(row.ActorID) || !eventRE.MatchString(row.EventID) || row.Revision == 0 || row.Revision >= routeMax || row.CreatedUnixNano <= 0 || row.ExpiresUnixNano <= row.CreatedUnixNano || row.ExpiresUnixNano-row.CreatedUnixNano > int64(8*time.Second) || len(row.Payload) > subscriptioncodec.MaxDocumentBytes+64 {
		return row, ErrUnavailable
	}
	for _, sha := range []string{row.InitialApplySHA, row.Fingerprint, row.EndpointSHA, row.ManifestSHA, row.BindingSHA, row.BodySHA} {
		if !routeSHA.MatchString(sha) {
			return row, ErrUnavailable
		}
	}
	round, err := readInitialHealthRound(ctx, tx, row.RoundID)
	if err != nil || round.InboundID != row.InboundID || round.PromotionEventID != row.PromotionEventID || round.InitialApplySHA != row.InitialApplySHA || round.NodeID != row.NodeID || round.InstallationID != row.InstallationID || round.EndpointSHA != row.EndpointSHA || round.ManifestSHA != row.ManifestSHA || round.receipt == nil || row.CreatedUnixNano < round.Members[0].Accepted || row.ExpiresUnixNano > round.Expires {
		return row, ErrUnavailable
	}
	return row, nil
}

func readInitialSubscriptionHead(ctx context.Context, tx *sql.Tx, client string) (subscriptionSnapshotRow, error) {
	var id string
	var revision, latest uint64
	err := tx.QueryRowContext(ctx, `SELECT snapshot_id,revision FROM pro_initial_snapshot_heads WHERE client_id=?`, client).Scan(&id, &revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return subscriptionSnapshotRow{}, err
	}
	if e := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM pro_initial_snapshots WHERE client_id=?`, client).Scan(&latest); e != nil {
		return subscriptionSnapshotRow{}, e
	}
	if latest != revision {
		return subscriptionSnapshotRow{}, ErrUnavailable
	}
	if errors.Is(err, sql.ErrNoRows) {
		return subscriptionSnapshotRow{}, ErrNotFound
	}
	row, err := readInitialSubscriptionSnapshot(ctx, tx, "id", id)
	if err != nil {
		return row, err
	}
	if row.ClientID != client || row.Revision != revision {
		return row, ErrUnavailable
	}
	return row, nil
}

func openInitialSubscriptionSnapshot(key []byte, row subscriptionSnapshotRow) ([]byte, error) {
	binding := row.subscriptionSnapshotBinding
	binding.Current, binding.Replayed, binding.DeliveryRevision = false, false, 0
	raw, err := openSubscription(key, "initial-snapshot", binding, row.Payload)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > subscriptioncodec.MaxDocumentBytes || subscriptionBodySHA(raw) != row.BodySHA {
		return nil, ErrUnavailable
	}
	return raw, nil
}

type initialSubscriptionProof struct {
	Raw        []byte
	Round      initialHealthRoundRow
	State      initialApplySnapshot
	BindingSHA string
	Expires    int64
}

func (s *Store) currentInitialSubscriptionProof(ctx context.Context, tx *sql.Tx, key []byte, client string, inbound uint64, roundID, initialSHA string, endpoint *mx.ProtocolHealthEndpoint, now int64) (initialSubscriptionProof, error) {
	var out initialSubscriptionProof
	if !s.initialApplyHealthEnabled {
		return out, ErrUnavailable
	}
	round, err := readInitialHealthRound(ctx, tx, roundID)
	if err != nil {
		return out, err
	}
	if round.InboundID != inbound || round.InitialApplySHA != initialSHA || round.receipt == nil {
		return out, ErrConflict
	}
	state, _, err := s.currentInitialHealthRound(ctx, tx, key, round, endpoint, now)
	if err != nil {
		return out, err
	}
	i := sort.SearchStrings(state.promotion.ClientIDs, client)
	if i >= len(state.promotion.ClientIDs) || state.promotion.ClientIDs[i] != client {
		return out, ErrConflict
	}
	manifest := healthRoundManifest{Version: 1, Members: []healthRoundBinding{{InboundID: inbound, Port: round.Members[0].Port}}}
	c, raw, count, err := subscriptionAppliedClientRoutes(ctx, tx, client, state.meteredHealthSnapshot, manifest, now)
	if err != nil {
		return out, err
	}
	if count != 1 {
		return out, ErrConflict
	}
	expires := round.Expires
	if state.base.Expires < expires {
		expires = state.base.Expires
	}
	if expiry := c.ExpiresUnix * int64(time.Second); expiry < expires {
		expires = expiry
	}
	if end := time.Now().UnixNano(); end < now || end >= expires {
		return out, ErrConflict
	}
	binding := fingerprint("initial-subscription-applied-binding-v1", struct {
		InitialApplySHA                                                string
		Runtime                                                        mx.ProtocolHealthRequest
		Generation                                                     uint64
		ConfigSHA, ManifestSHA, ClientID, ClientUUID, RepresentativeID string
		ClientExpiry                                                   int64
	}{initialSHA, state.base, state.apply.payload.Generation, state.apply.payload.Candidate.SHA256, state.apply.payload.Candidate.ManifestSHA256, c.ID, c.UUID, c.RepresentativeID, c.ExpiresUnix})
	return initialSubscriptionProof{Raw: raw, Round: round, State: state, BindingSHA: binding, Expires: expires}, nil
}

func (s *Store) currentInitialSubscriptionSnapshot(ctx context.Context, tx *sql.Tx, key []byte, row subscriptionSnapshotRow, endpoint *mx.ProtocolHealthEndpoint, now int64) (initialSubscriptionProof, error) {
	if now < row.CreatedUnixNano || now >= row.ExpiresUnixNano {
		return initialSubscriptionProof{}, ErrConflict
	}
	proof, err := s.currentInitialSubscriptionProof(ctx, tx, key, row.ClientID, row.InboundID, row.RoundID, row.InitialApplySHA, endpoint, now)
	if err != nil {
		return proof, err
	}
	if row.NodeID != proof.Round.NodeID || row.InstallationID != proof.Round.InstallationID || row.PromotionEventID != proof.Round.PromotionEventID || row.EndpointSHA != proof.Round.EndpointSHA || row.ManifestSHA != proof.Round.ManifestSHA || row.BindingSHA != proof.BindingSHA || row.ExpiresUnixNano != proof.Expires || row.RouteCount != 1 || row.BodySHA != subscriptionBodySHA(proof.Raw) {
		return proof, ErrConflict
	}
	return proof, nil
}

func (s *Store) MaterializeInitialApplySubscription(ctx context.Context, key []byte, actorID string, inbound uint64, in InitialApplySubscriptionMaterialization, endpoint mx.ProtocolHealthEndpoint) (LabSubscriptionSnapshot, error) {
	var out LabSubscriptionSnapshot
	if inbound == 0 || !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.ClientID) || !routeID.MatchString(in.RoundID) || !routeSHA.MatchString(in.InitialApplySHA) || in.ExpectedRevision >= routeMax-1 || in.ExpectedDeliveryRevision >= routeMax-1 || !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := s.initialHealthTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = subscriptionActor(ctx, tx, actorID, in.ClientID, true, true); err != nil {
		return out, err
	}
	fp := fingerprint("initial-subscription-snapshot-v1", struct {
		ActorID     string
		InboundID   uint64
		EndpointSHA string
		Input       InitialApplySubscriptionMaterialization
	}{actorID, inbound, endpoint.SHA(), in})
	delivery, err := readSubscriptionDelivery(ctx, tx, in.ClientID)
	if err != nil {
		return out, err
	}
	head, headErr := readInitialSubscriptionHead(ctx, tx, in.ClientID)
	if headErr != nil && !errors.Is(headErr, ErrNotFound) {
		return out, headErr
	}
	prior, err := readInitialSubscriptionSnapshot(ctx, tx, "event_id", in.EventID)
	if err == nil {
		if prior.Fingerprint != fp || prior.ClientID != in.ClientID || prior.InboundID != inbound {
			return out, ErrConflict
		}
		if _, err = openInitialSubscriptionSnapshot(key, prior); err != nil {
			return out, err
		}
		if delivery.Scope == InitialApplyHealthScope && delivery.SnapshotID == prior.ID {
			_, e := s.currentInitialSubscriptionSnapshot(ctx, tx, key, prior, &endpoint, time.Now().UnixNano())
			prior.Current = e == nil
		}
		prior.Replayed = true
		if err = snapshotDeliveryMetadata(ctx, tx, delivery, InitialApplyHealthScope, &prior.LabSubscriptionSnapshot); err != nil {
			return out, err
		}
		return prior.LabSubscriptionSnapshot, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return out, err
	}
	if err = initialEventUnused(ctx, tx, in.EventID); err != nil {
		return out, err
	}
	if err = initialHealthIDUnused(ctx, tx, in.EventID); err != nil {
		return out, err
	}
	if !delivery.Enabled || delivery.Revision != in.ExpectedDeliveryRevision || head.Revision != in.ExpectedRevision {
		return out, ErrConflict
	}
	now := time.Now().UnixNano()
	proof, err := s.currentInitialSubscriptionProof(ctx, tx, key, in.ClientID, inbound, in.RoundID, in.InitialApplySHA, &endpoint, now)
	if err != nil {
		return out, err
	}
	id, err := subscriptionID()
	if err != nil {
		return out, err
	}
	row := subscriptionSnapshotRow{subscriptionSnapshotBinding: subscriptionSnapshotBinding{
		LabSubscriptionSnapshot: LabSubscriptionSnapshot{ID: id, EventID: in.EventID, ClientID: in.ClientID, RoundID: in.RoundID, NodeID: proof.Round.NodeID, InstallationID: proof.Round.InstallationID, Revision: head.Revision + 1, CreatedUnixNano: now, ExpiresUnixNano: proof.Expires, RouteCount: 1, Scope: InitialApplyHealthScope, InboundID: inbound, PromotionEventID: proof.Round.PromotionEventID, InitialApplySHA: in.InitialApplySHA},
		ActorID:                 actorID, Fingerprint: fp, EndpointSHA: proof.Round.EndpointSHA, ManifestSHA: proof.Round.ManifestSHA, BindingSHA: proof.BindingSHA, BodySHA: subscriptionBodySHA(proof.Raw),
	}}
	row.Payload, err = sealSubscription(key, "initial-snapshot", row.subscriptionSnapshotBinding, proof.Raw)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_initial_snapshots(`+initialSnapshotColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.ID, row.ClientID, row.InboundID, row.PromotionEventID, row.RoundID, row.InitialApplySHA, row.NodeID, row.InstallationID, row.Revision, row.CreatedUnixNano, row.ExpiresUnixNano, row.EventID, row.ActorID, row.Fingerprint, row.EndpointSHA, row.ManifestSHA, row.BindingSHA, row.BodySHA, row.Payload); err != nil {
		return out, err
	}
	if head.Revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO pro_initial_snapshot_heads(client_id,revision,snapshot_id) VALUES(?,?,?)`, row.ClientID, row.Revision, row.ID)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE pro_initial_snapshot_heads SET revision=?,snapshot_id=? WHERE client_id=? AND revision=?`, row.Revision, row.ID, row.ClientID, head.Revision)
		if err == nil {
			if n, e := result.RowsAffected(); e != nil || n != 1 {
				return out, ErrConflict
			}
		}
	}
	if err != nil {
		return out, err
	}
	row.DeliveryRevision, err = appendSubscriptionDelivery(ctx, tx, delivery, InitialApplyHealthScope, row.LabSubscriptionSnapshot)
	if err != nil {
		return out, err
	}
	if end := time.Now().UnixNano(); end < now || end >= row.ExpiresUnixNano {
		return out, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	row.Current = time.Now().UnixNano() < row.ExpiresUnixNano
	return row.LabSubscriptionSnapshot, nil
}

func (s *Store) readInitialSubscriptionDelivery(ctx context.Context, tx *sql.Tx, key []byte, delivery subscriptionDeliveryHead, endpoint mx.ProtocolHealthEndpoint, generation uint64) (LabSubscriptionContent, error) {
	var out LabSubscriptionContent
	if !s.initialApplyHealthEnabled || delivery.Scope != InitialApplyHealthScope || !delivery.Enabled {
		return out, ErrUnavailable
	}
	row, err := readInitialSubscriptionSnapshot(ctx, tx, "id", delivery.SnapshotID)
	if err != nil {
		return out, err
	}
	if row.ClientID != delivery.ClientID || row.Revision != delivery.SnapshotRevision {
		return out, ErrUnavailable
	}
	raw, err := openInitialSubscriptionSnapshot(key, row)
	if err != nil {
		return out, err
	}
	now := time.Now().UnixNano()
	proof, err := s.currentInitialSubscriptionSnapshot(ctx, tx, key, row, &endpoint, now)
	if err != nil {
		return out, err
	}
	if !bytes.Equal(raw, proof.Raw) {
		return out, ErrUnavailable
	}
	if end := time.Now().UnixNano(); end < now || end >= row.ExpiresUnixNano {
		return out, ErrConflict
	}
	row.Current = true
	if err = snapshotDeliveryMetadata(ctx, tx, delivery, InitialApplyHealthScope, &row.LabSubscriptionSnapshot); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	if time.Now().UnixNano() >= row.ExpiresUnixNano {
		return out, ErrConflict
	}
	return LabSubscriptionContent{Raw: raw, Snapshot: row.LabSubscriptionSnapshot, Generation: generation}, nil
}
