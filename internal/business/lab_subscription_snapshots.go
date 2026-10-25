package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/subscriptioncodec"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type subscriptionSnapshotBinding struct {
	LabSubscriptionSnapshot
	ActorID, Fingerprint                          string
	EndpointSHA, ManifestSHA, BindingSHA, BodySHA string
}
type subscriptionSnapshotRow struct {
	subscriptionSnapshotBinding
	Payload []byte
}

const subscriptionSnapshotColumns = `id,client_id,handover_id,round_id,node_id,installation_id,revision,created,expires,route_count,event_id,actor_id,fingerprint,endpoint_sha,manifest_sha,binding_sha,body_sha,payload_enc`

func readSubscriptionSnapshot(ctx context.Context, tx *sql.Tx, column, value string) (subscriptionSnapshotRow, error) {
	var r subscriptionSnapshotRow
	if column != "id" && column != "event_id" {
		return r, ErrInvalid
	}
	err := tx.QueryRowContext(ctx, `SELECT `+subscriptionSnapshotColumns+` FROM pro_subscription_snapshots WHERE `+column+`=?`, value).Scan(&r.ID, &r.ClientID, &r.HandoverID, &r.RoundID, &r.NodeID, &r.InstallationID, &r.Revision, &r.CreatedUnixNano, &r.ExpiresUnixNano, &r.RouteCount, &r.EventID, &r.ActorID, &r.Fingerprint, &r.EndpointSHA, &r.ManifestSHA, &r.BindingSHA, &r.BodySHA, &r.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if !routeID.MatchString(r.ID) || !idRE.MatchString(r.ClientID) || !routeID.MatchString(r.HandoverID) || !routeID.MatchString(r.RoundID) || !routeNodeID.MatchString(r.NodeID) || !routeID.MatchString(r.InstallationID) || !idRE.MatchString(r.ActorID) || !eventRE.MatchString(r.EventID) || r.Revision == 0 || r.Revision >= routeMax || r.CreatedUnixNano <= 0 || r.ExpiresUnixNano <= r.CreatedUnixNano || r.ExpiresUnixNano-r.CreatedUnixNano > int64(8*time.Second) || r.RouteCount < 1 || r.RouteCount > maxMeteredHealthMembers || len(r.Payload) > subscriptioncodec.MaxDocumentBytes+64 {
		return r, ErrUnavailable
	}
	for _, hash := range []string{r.Fingerprint, r.EndpointSHA, r.ManifestSHA, r.BindingSHA, r.BodySHA} {
		if !routeSHA.MatchString(hash) {
			return r, ErrUnavailable
		}
	}
	round, err := readHealthRound(ctx, tx, r.RoundID)
	if err != nil || round.HandoverID != r.HandoverID || round.ManifestSHA != r.ManifestSHA {
		return r, ErrUnavailable
	}
	r.ConsumerManifestID, r.ConsumerManifestRevision, r.ConsumerManifestSHA = round.ConsumerManifestID, round.ConsumerManifestRevision, round.ConsumerManifestSHA
	return r, nil
}
func readSubscriptionSnapshotHead(ctx context.Context, tx *sql.Tx, clientID string) (subscriptionSnapshotRow, error) {
	var id string
	var revision, latest uint64
	err := tx.QueryRowContext(ctx, `SELECT snapshot_id,revision FROM pro_subscription_snapshot_heads WHERE client_id=?`, clientID).Scan(&id, &revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return subscriptionSnapshotRow{}, err
	}
	if e := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0) FROM pro_subscription_snapshots WHERE client_id=?`, clientID).Scan(&latest); e != nil {
		return subscriptionSnapshotRow{}, e
	}
	if latest != revision {
		return subscriptionSnapshotRow{}, ErrUnavailable
	}
	if errors.Is(err, sql.ErrNoRows) {
		return subscriptionSnapshotRow{}, ErrNotFound
	}
	r, err := readSubscriptionSnapshot(ctx, tx, "id", id)
	if err != nil {
		return r, err
	}
	if r.ClientID != clientID || r.Revision != revision {
		return r, ErrUnavailable
	}
	return r, nil
}
func subscriptionBodySHA(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func openSubscriptionSnapshot(key []byte, row subscriptionSnapshotRow) ([]byte, error) {
	// Current/Replayed are assessments, not persisted authority or mutable AAD.
	binding := row.subscriptionSnapshotBinding
	binding.Current, binding.Replayed = false, false
	// v15 response metadata is not part of any pre-v15 ciphertext AAD.
	binding.Scope, binding.DeliveryRevision = "", 0
	binding.InboundID, binding.PromotionEventID, binding.InitialApplySHA = 0, "", ""
	raw, err := openSubscription(key, "snapshot", binding, row.Payload)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > subscriptioncodec.MaxDocumentBytes || subscriptionBodySHA(raw) != row.BodySHA {
		return nil, ErrUnavailable
	}
	return raw, nil
}

type subscriptionProof struct {
	Raw        []byte
	Round      healthRoundRow
	State      meteredHealthSnapshot
	Handover   MeteredHandoverView
	BindingSHA string
	Expires    int64
	RouteCount int
}

// Build only from the authenticated exact applied candidate. The caller picks
// a client and a complete round, never an endpoint or subset of required routes.
// Full round coverage is checked before selecting that client's billable UUID.
func (s *Store) currentSubscriptionProof(ctx context.Context, tx *sql.Tx, key []byte, clientID, handoverID, roundID string, endpoint *mx.ProtocolHealthEndpoint, now int64) (subscriptionProof, error) {
	var out subscriptionProof
	round, err := readHealthRound(ctx, tx, roundID)
	if err != nil {
		return out, err
	}
	if round.HandoverID != handoverID || now < round.Created || now >= round.Expires || (endpoint != nil && round.EndpointSHA != endpoint.SHA()) {
		return out, ErrConflict
	}
	for _, member := range round.Members {
		if member.State != "accepted" {
			return out, ErrConflict
		}
	}
	manifest, _, err := s.currentHealthRound(ctx, tx, key, handoverID, now, endpoint)
	if err != nil {
		return out, err
	}
	if meteredHash(manifest) != round.ManifestSHA {
		return out, ErrConflict
	}
	state, err := s.loadMeteredHealthSnapshot(ctx, tx, key, handoverID, now)
	if err != nil {
		return out, err
	}
	if round.NodeID != state.base.NodeID || round.InstallationID != state.base.InstallationID || state.apply.payload.Candidate.SHA256 != state.apply.payload.Command.Lease.ConfigSHA || state.apply.payload.Candidate.ManifestSHA256 != state.apply.payload.Command.Lease.ManifestSHA {
		return out, ErrConflict
	}
	c, raw, routeCount, err := subscriptionAppliedClientRoutes(ctx, tx, clientID, state, manifest, now)
	if err != nil {
		return out, err
	}
	op, _, err := readMeteredHandover(ctx, tx, handoverID)
	if err != nil {
		return out, err
	}
	expires := round.Expires
	if state.base.Expires < expires {
		expires = state.base.Expires
	}
	if clientExpiry := c.ExpiresUnix * int64(time.Second); clientExpiry < expires {
		expires = clientExpiry
	}
	if end := time.Now().UnixNano(); end < now || end >= expires {
		return out, ErrConflict
	}
	binding := fingerprint("lab-subscription-applied-binding-v1", struct {
		Handover                                                       MeteredHandoverView
		Runtime                                                        mx.ProtocolHealthRequest
		Generation                                                     uint64
		ConfigSHA, ManifestSHA, ClientID, ClientUUID, RepresentativeID string
		ClientExpiry                                                   int64
	}{op, state.base, state.apply.payload.Generation, state.apply.payload.Candidate.SHA256, state.apply.payload.Candidate.ManifestSHA256, c.ID, c.UUID, c.RepresentativeID, c.ExpiresUnix})
	return subscriptionProof{Raw: raw, Round: round, State: state, Handover: op, BindingSHA: binding, Expires: expires, RouteCount: routeCount}, nil
}

// Shared customer/epoch/rendering checks for both proof origins.
func subscriptionAppliedClientRoutes(ctx context.Context, tx *sql.Tx, clientID string, state meteredHealthSnapshot, manifest healthRoundManifest, now int64) (Client, []byte, int, error) {
	c, err := readClient(ctx, tx, clientID)
	if err != nil {
		return Client{}, nil, 0, err
	}
	if c.Archived || c.DeploymentState == "disabled" || c.ExpiresUnix <= now/int64(time.Second) || c.ExpiresUnix > math.MaxInt64/int64(time.Second) {
		return Client{}, nil, 0, ErrConflict
	}
	var eligible int
	if err = tx.QueryRowContext(ctx, `SELECT eligible FROM pro_usage_eligibility WHERE client_id=?`, clientID).Scan(&eligible); err != nil {
		return Client{}, nil, 0, err
	}
	if eligible != 1 {
		return Client{}, nil, 0, ErrConflict
	}
	email, err := uc.CounterEmail(clientID)
	if err != nil {
		return Client{}, nil, 0, ErrConflict
	}
	found := 0
	for _, b := range state.epoch.Bindings {
		if b.ClientID == clientID {
			if !b.Billable || b.Role != "" || b.Email != email {
				return Client{}, nil, 0, ErrConflict
			}
			found++
		}
	}
	if found != 1 {
		return Client{}, nil, 0, ErrConflict
	}
	labels := make(map[uint64]string, len(manifest.Members))
	for _, member := range manifest.Members {
		labels[member.InboundID] = fmt.Sprintf("LAB direct %d", member.InboundID)
	}
	routes, err := xrayconfig.LabSubscriptionRoutes(state.apply.payload.Candidate, c.UUID, labels)
	if err != nil || len(routes) == 0 || len(routes) > maxMeteredHealthMembers {
		return Client{}, nil, 0, ErrConflict
	}
	if manifest.ConsumerManifestID != "" {
		for i := range routes {
			matched := false
			for _, member := range manifest.Members {
				if member.InboundID == routes[i].InboundID && member.Port == routes[i].Port && mx.ValidateConsumerProtocolEndpoint(member.ConsumerAddress, member.ConsumerPort) {
					routes[i].Host, routes[i].Port = member.ConsumerAddress, member.ConsumerPort
					routes[i].Label = fmt.Sprintf("LAB consumer %d", member.InboundID)
					matched = true
					break
				}
			}
			if !matched {
				return Client{}, nil, 0, ErrConflict
			}
		}
	}
	// Bind the UUID selected by the codec to the exact registered accounting
	// email in every applied listener. Membership and billability must describe
	// the same identity, not merely two separately well-formed candidate users.
	var config struct {
		Inbounds []struct {
			Settings struct {
				Clients []struct {
					ID    string `json:"id"`
					Email string `json:"email"`
				} `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(state.apply.payload.Candidate.Config, &config) != nil {
		return Client{}, nil, 0, ErrConflict
	}
	matched := 0
	for _, inbound := range config.Inbounds {
		for _, user := range inbound.Settings.Clients {
			if strings.EqualFold(user.ID, c.UUID) {
				if user.Email != email {
					return Client{}, nil, 0, ErrConflict
				}
				matched++
			}
		}
	}
	if matched != len(routes) {
		return Client{}, nil, 0, ErrConflict
	}
	raw, err := subscriptioncodec.Render(routes, subscriptioncodec.FormatRaw)
	if err != nil {
		return Client{}, nil, 0, ErrConflict
	}
	return c, raw, len(routes), nil
}

func (s *Store) currentSubscriptionSnapshot(ctx context.Context, tx *sql.Tx, key []byte, row subscriptionSnapshotRow, endpoint *mx.ProtocolHealthEndpoint, now int64) (subscriptionProof, error) {
	if now < row.CreatedUnixNano || now >= row.ExpiresUnixNano {
		return subscriptionProof{}, ErrConflict
	}
	proof, err := s.currentSubscriptionProof(ctx, tx, key, row.ClientID, row.HandoverID, row.RoundID, endpoint, now)
	if err != nil {
		return proof, err
	}
	if row.NodeID != proof.Round.NodeID || row.InstallationID != proof.Round.InstallationID || row.EndpointSHA != proof.Round.EndpointSHA || row.ManifestSHA != proof.Round.ManifestSHA || row.ConsumerManifestID != proof.Round.ConsumerManifestID || row.ConsumerManifestRevision != proof.Round.ConsumerManifestRevision || row.ConsumerManifestSHA != proof.Round.ConsumerManifestSHA || row.BindingSHA != proof.BindingSHA || row.ExpiresUnixNano != proof.Expires || row.RouteCount != proof.RouteCount || row.BodySHA != subscriptionBodySHA(proof.Raw) {
		return proof, ErrConflict
	}
	return proof, nil
}

func (s *Store) MaterializeLabSubscription(ctx context.Context, key []byte, actorID, handoverID string, in LabSubscriptionMaterialization, endpoint mx.ProtocolHealthEndpoint) (LabSubscriptionSnapshot, error) {
	var out LabSubscriptionSnapshot
	if in.ExpectedDeliveryRevision != nil && *in.ExpectedDeliveryRevision >= routeMax-1 {
		return out, ErrInvalid
	}
	if !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.ClientID) || !routeID.MatchString(in.RoundID) || !routeID.MatchString(handoverID) || in.ExpectedRevision >= routeMax-1 || !endpoint.Valid() || (in.ConsumerManifestID == "" && in.ConsumerManifestRevision != 0) || (in.ConsumerManifestID != "" && (!routeID.MatchString(in.ConsumerManifestID) || in.ConsumerManifestRevision == 0 || in.ConsumerManifestRevision >= routeMax)) {
		return out, ErrInvalid
	}
	tx, err := s.subscriptionTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = subscriptionActor(ctx, tx, actorID, in.ClientID, true, true); err != nil {
		return out, err
	}
	fp := fingerprint("lab-subscription-snapshot-v1", struct {
		ActorID, HandoverID, EndpointSHA string
		Input                            LabSubscriptionMaterialization
	}{actorID, handoverID, endpoint.SHA(), in})
	delivery, err := readSubscriptionDelivery(ctx, tx, in.ClientID)
	if err != nil {
		return out, err
	}
	head, headErr := readSubscriptionSnapshotHead(ctx, tx, in.ClientID)
	if headErr != nil && !errors.Is(headErr, ErrNotFound) {
		return out, headErr
	}
	prior, err := readSubscriptionSnapshot(ctx, tx, "event_id", in.EventID)
	if err == nil {
		if prior.Fingerprint != fp || prior.ClientID != in.ClientID {
			return out, ErrConflict
		}
		if _, err = openSubscriptionSnapshot(key, prior); err != nil {
			return out, err
		}
		if headErr == nil && head.ID == prior.ID {
			_, e := s.currentSubscriptionSnapshot(ctx, tx, key, prior, &endpoint, time.Now().UnixNano())
			prior.Current = e == nil
		}
		prior.Replayed = true
		if err = snapshotDeliveryMetadata(ctx, tx, delivery, HandoverSubscriptionScope, &prior.LabSubscriptionSnapshot); err != nil {
			return out, err
		}
		return prior.LabSubscriptionSnapshot, tx.Commit()
	}
	if !errors.Is(err, ErrNotFound) {
		return out, err
	}
	if err = consumerEventUnused(ctx, tx, in.EventID); err != nil {
		return out, err
	}
	var used int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_subscription_token_events WHERE event_id=?`, in.EventID).Scan(&used); err != nil {
		return out, err
	}
	if used != 0 || in.ExpectedRevision != head.Revision {
		return out, ErrConflict
	}
	if in.ExpectedDeliveryRevision != nil {
		if *in.ExpectedDeliveryRevision != delivery.Revision {
			return out, ErrConflict
		}
	} else if delivery.Enabled && delivery.Scope == InitialApplyHealthScope {
		return out, ErrConflict // Old clients cannot overwrite a foreign origin.
	}
	now := time.Now().UnixNano()
	proof, err := s.currentSubscriptionProof(ctx, tx, key, in.ClientID, handoverID, in.RoundID, &endpoint, now)
	if err != nil {
		return out, err
	}
	if proof.Round.ConsumerManifestID != in.ConsumerManifestID || proof.Round.ConsumerManifestRevision != in.ConsumerManifestRevision {
		return out, ErrConflict
	}
	id, err := subscriptionID()
	if err != nil {
		return out, err
	}
	row := subscriptionSnapshotRow{subscriptionSnapshotBinding: subscriptionSnapshotBinding{
		LabSubscriptionSnapshot: LabSubscriptionSnapshot{ID: id, EventID: in.EventID, ClientID: in.ClientID, HandoverID: handoverID, RoundID: in.RoundID, ConsumerManifestID: proof.Round.ConsumerManifestID, ConsumerManifestRevision: proof.Round.ConsumerManifestRevision, ConsumerManifestSHA: proof.Round.ConsumerManifestSHA, NodeID: proof.Round.NodeID, InstallationID: proof.Round.InstallationID, Revision: head.Revision + 1, CreatedUnixNano: now, ExpiresUnixNano: proof.Expires, RouteCount: proof.RouteCount},
		ActorID:                 actorID, Fingerprint: fp, EndpointSHA: proof.Round.EndpointSHA, ManifestSHA: proof.Round.ManifestSHA, BindingSHA: proof.BindingSHA, BodySHA: subscriptionBodySHA(proof.Raw),
	}}
	row.Payload, err = sealSubscription(key, "snapshot", row.subscriptionSnapshotBinding, proof.Raw)
	if err != nil {
		return out, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO pro_subscription_snapshots(`+subscriptionSnapshotColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, row.ID, row.ClientID, row.HandoverID, row.RoundID, row.NodeID, row.InstallationID, row.Revision, row.CreatedUnixNano, row.ExpiresUnixNano, row.RouteCount, row.EventID, row.ActorID, row.Fingerprint, row.EndpointSHA, row.ManifestSHA, row.BindingSHA, row.BodySHA, row.Payload)
	if err != nil {
		return out, err
	}
	if head.Revision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO pro_subscription_snapshot_heads(client_id,revision,snapshot_id) VALUES(?,?,?)`, row.ClientID, row.Revision, row.ID)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE pro_subscription_snapshot_heads SET revision=?,snapshot_id=? WHERE client_id=? AND revision=?`, row.Revision, row.ID, row.ClientID, head.Revision)
		if err == nil {
			if n, e := result.RowsAffected(); e != nil || n != 1 {
				return out, ErrConflict
			}
		}
	}
	if err != nil {
		return out, err
	}
	row.DeliveryRevision, err = appendSubscriptionDelivery(ctx, tx, delivery, HandoverSubscriptionScope, row.LabSubscriptionSnapshot)
	if err != nil {
		return out, err
	}
	if delivery.Enabled {
		row.Scope = HandoverSubscriptionScope
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

// Read linearizes against token rotation/revocation and policy changes using
// the SQLite writer lock. A caller must still check its node registry and this
// fixed deadline immediately before writing the bounded HTTP response.
func (s *Store) ReadLabSubscription(ctx context.Context, key []byte, token string, endpoint mx.ProtocolHealthEndpoint) (LabSubscriptionContent, error) {
	var out LabSubscriptionContent
	if !validSubscriptionToken(token) {
		return out, ErrNotFound
	}
	if !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := s.subscriptionTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var clientID, credentialID string
	err = tx.QueryRowContext(ctx, `SELECT client_id,id FROM pro_subscription_credentials WHERE token_sha=?`, subscriptionTokenHash(token)).Scan(&clientID, &credentialID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	h, err := readSubscriptionHead(ctx, tx, clientID)
	if err != nil {
		return out, err
	}
	if h.State != "active" || h.CredentialID != credentialID {
		return out, ErrNotFound
	}
	currentToken, err := revealSubscriptionHead(ctx, tx, key, h)
	if err != nil {
		return out, err
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(currentToken)) != 1 {
		return out, ErrNotFound
	}
	delivery, err := readSubscriptionDelivery(ctx, tx, clientID)
	if err != nil {
		return out, err
	}
	if delivery.Enabled {
		if delivery.Revision == 0 {
			return out, ErrNotFound
		}
		if delivery.Scope == InitialApplyHealthScope {
			return s.readInitialSubscriptionDelivery(ctx, tx, key, delivery, endpoint, h.Generation)
		}
		if delivery.Scope != HandoverSubscriptionScope {
			return out, ErrUnavailable
		}
	}
	row, err := readSubscriptionSnapshotHead(ctx, tx, clientID)
	if err != nil {
		return out, err
	}
	if delivery.Enabled && (row.ID != delivery.SnapshotID || row.Revision != delivery.SnapshotRevision) {
		return out, ErrUnavailable
	}
	raw, err := openSubscriptionSnapshot(key, row)
	if err != nil {
		return out, err
	}
	now := time.Now().UnixNano()
	proof, err := s.currentSubscriptionSnapshot(ctx, tx, key, row, &endpoint, now)
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
	if err = snapshotDeliveryMetadata(ctx, tx, delivery, HandoverSubscriptionScope, &row.LabSubscriptionSnapshot); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	if time.Now().UnixNano() >= row.ExpiresUnixNano {
		return out, ErrConflict
	}
	row.Current = true
	return LabSubscriptionContent{Raw: raw, Snapshot: row.LabSubscriptionSnapshot, SourceNodeID: proof.Handover.SourceNode, SourceInstallationID: proof.Handover.SourceInstallation, Generation: h.Generation}, nil
}
