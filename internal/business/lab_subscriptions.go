package business

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type LabSubscriptionView struct {
	ClientID   string                   `json:"client_id"`
	Generation uint64                   `json:"generation"`
	State      string                   `json:"state"`
	Snapshot   *LabSubscriptionSnapshot `json:"snapshot,omitempty"`
}
type SubscriptionTokenMutation struct {
	EventID            string `json:"event_id"`
	Action             string `json:"action"`
	ExpectedGeneration uint64 `json:"expected_generation"`
}
type SubscriptionTokenReceipt struct {
	EventID    string `json:"event_id"`
	ClientID   string `json:"client_id"`
	Generation uint64 `json:"generation"`
	State      string `json:"state"`
	Replayed   bool   `json:"replayed"`
	Superseded bool   `json:"superseded"`
}
type SubscriptionSecret struct {
	ClientID   string `json:"client_id"`
	Generation uint64 `json:"generation"`
	Token      string `json:"token"`
}
type LabSubscriptionMaterialization struct {
	EventID                  string `json:"event_id"`
	ClientID                 string `json:"client_id"`
	RoundID                  string `json:"round_id"`
	ExpectedRevision         uint64 `json:"expected_revision"`
	ConsumerManifestID       string `json:"consumer_manifest_id,omitempty"`
	ConsumerManifestRevision uint64 `json:"consumer_manifest_revision,omitempty"`
}

// One complete experimental LAB snapshot replaces the previous snapshot for
// this client. This is deliberately not a multi-node production subscription.
type LabSubscriptionSnapshot struct {
	ID                       string `json:"id"`
	EventID                  string `json:"event_id"`
	ClientID                 string `json:"client_id"`
	HandoverID               string `json:"handover_id"`
	RoundID                  string `json:"round_id"`
	ConsumerManifestID       string `json:"consumer_manifest_id,omitempty"`
	ConsumerManifestRevision uint64 `json:"consumer_manifest_revision,omitempty"`
	ConsumerManifestSHA      string `json:"consumer_manifest_sha,omitempty"`
	NodeID                   string `json:"node_id"`
	InstallationID           string `json:"installation_id"`
	Revision                 uint64 `json:"revision"`
	CreatedUnixNano          int64  `json:"created_unix_nano"`
	ExpiresUnixNano          int64  `json:"expires_unix_nano"`
	RouteCount               int    `json:"route_count"`
	Current                  bool   `json:"current"`
	Replayed                 bool   `json:"replayed,omitempty"`
}
type LabSubscriptionContent struct {
	Raw                  []byte                  `json:"-"`
	Snapshot             LabSubscriptionSnapshot `json:"snapshot"`
	SourceNodeID         string                  `json:"source_node_id"`
	SourceInstallationID string                  `json:"source_installation_id"`
	Generation           uint64                  `json:"generation"`
}

func (s *Store) EnableLabSubscriptions(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	return s.usage.EnableLabSubscriptions(ctx)
}

func (s *Store) subscriptionTx(ctx context.Context, key []byte) (*sql.Tx, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	tx, err := s.healthTx(ctx)
	if err != nil {
		return nil, err
	}
	var version, n int
	if err = tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err == nil && usageledger.HasLabSubscriptionsSchema(version) {
		err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_subscription_meta WHERE id=1 AND version=1`).Scan(&n)
	}
	if err != nil || !usageledger.HasLabSubscriptionsSchema(version) || n != 1 {
		tx.Rollback()
		return nil, ErrUnavailable
	}
	return tx, nil
}

// Match the existing login rules, including the deliberate rollback-disabled
// bit on representatives with scoped permissions. Check again inside the same
// writer transaction as secrets/replays; a stale HTTP session is insufficient.
func subscriptionActor(ctx context.Context, tx *sql.Tx, actorID, clientID string, manage, ownerOnly bool) (Client, error) {
	if !idRE.MatchString(actorID) || !idRE.MatchString(clientID) {
		return Client{}, ErrInvalid
	}
	var role, repID, kind string
	var disabled, enabled int
	err := tx.QueryRowContext(ctx, `SELECT a.role,a.representative_id,r.kind,r.enabled,CASE WHEN a.role='representative' AND ctl.representative_id IS NOT NULL THEN ctl.login_suspended ELSE a.disabled END FROM auth_users a JOIN representatives r ON r.id=a.representative_id LEFT JOIN representative_controls ctl ON ctl.representative_id=a.representative_id WHERE a.id=?`, actorID).Scan(&role, &repID, &kind, &enabled, &disabled)
	if err != nil || enabled != 1 || disabled != 0 || role != kind || (role != "owner" && role != "representative") {
		return Client{}, ErrUnauthenticated
	}
	if ownerOnly && role != "owner" {
		return Client{}, ErrUnauthenticated
	}
	c, err := readClient(ctx, tx, clientID)
	if errors.Is(err, sql.ErrNoRows) {
		return Client{}, ErrNotFound
	}
	if err != nil {
		return Client{}, err
	}
	if role == "representative" {
		if c.RepresentativeID != repID {
			return Client{}, ErrNotFound
		}
		control, e := readRepresentativeControl(ctx, tx, repID)
		if e != nil {
			return Client{}, e
		}
		if (manage && !control.Permissions.ManageClients) || (!manage && !control.Permissions.ViewClients) {
			return Client{}, ErrUnauthenticated
		}
	}
	return c, nil
}

func subscriptionCipher(key []byte, purpose string) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	m := hmac.New(sha256.New, key)
	m.Write([]byte("DARK-PRO-LAB-SUBSCRIPTION-v1\x00" + purpose))
	b, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}
func sealSubscription(key []byte, purpose string, aad any, raw []byte) ([]byte, error) {
	a, err := subscriptionCipher(key, purpose)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	binding, err := json.Marshal(aad)
	if err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, raw, binding), nil
}
func openSubscription(key []byte, purpose string, aad any, enc []byte) ([]byte, error) {
	a, err := subscriptionCipher(key, purpose)
	if err != nil {
		return nil, err
	}
	if len(enc) < a.NonceSize()+a.Overhead() {
		return nil, ErrUnavailable
	}
	binding, err := json.Marshal(aad)
	if err != nil {
		return nil, err
	}
	raw, err := a.Open(nil, enc[:a.NonceSize()], enc[a.NonceSize():], binding)
	if err != nil {
		return nil, ErrUnavailable
	}
	return raw, nil
}
func subscriptionTokenHash(token string) string {
	h := sha256.Sum256([]byte("DARK-PRO-LAB-SUBSCRIPTION-TOKEN-v1\x00" + token))
	return hex.EncodeToString(h[:])
}
func validSubscriptionToken(token string) bool {
	if len(token) != 43 {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(token)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == token
}
func subscriptionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

type subscriptionHead struct {
	ClientID     string
	Generation   uint64
	State        string
	CredentialID string
	EventID      string
}

func readSubscriptionHead(ctx context.Context, tx *sql.Tx, clientID string) (subscriptionHead, error) {
	h := subscriptionHead{ClientID: clientID, State: "absent"}
	var credential sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT generation,state,credential_id,event_id FROM pro_subscription_token_heads WHERE client_id=?`, clientID).Scan(&h.Generation, &h.State, &credential, &h.EventID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return h, err
	}
	var latest uint64
	if e := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(generation),0) FROM pro_subscription_token_events WHERE client_id=?`, clientID).Scan(&latest); e != nil {
		return h, e
	}
	if latest != h.Generation {
		return h, ErrUnavailable
	}
	if errors.Is(err, sql.ErrNoRows) {
		return h, nil
	}
	if h.Generation == 0 || h.Generation >= routeMax || !eventRE.MatchString(h.EventID) || (h.State != "active" && h.State != "revoked") || credential.Valid != (h.State == "active") {
		return h, ErrUnavailable
	}
	h.CredentialID = credential.String
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_subscription_token_events WHERE event_id=? AND client_id=? AND generation=? AND state=? AND credential_id IS ?`, h.EventID, clientID, h.Generation, h.State, credential).Scan(&count); err != nil || count != 1 {
		return h, ErrUnavailable
	}
	return h, nil
}

type subscriptionCredentialBinding struct {
	ID         string
	ClientID   string
	Generation uint64
	TokenSHA   string
	Created    int64
}

func revealSubscriptionHead(ctx context.Context, tx *sql.Tx, key []byte, h subscriptionHead) (string, error) {
	if h.State != "active" {
		return "", ErrNotFound
	}
	var b subscriptionCredentialBinding
	var enc []byte
	err := tx.QueryRowContext(ctx, `SELECT id,client_id,generation,token_sha,created,token_enc FROM pro_subscription_credentials WHERE id=?`, h.CredentialID).Scan(&b.ID, &b.ClientID, &b.Generation, &b.TokenSHA, &b.Created, &enc)
	if err != nil || !routeID.MatchString(b.ID) || b.ClientID != h.ClientID || b.Generation != h.Generation || !routeSHA.MatchString(b.TokenSHA) || b.Created <= 0 || len(enc) > 256 {
		return "", ErrUnavailable
	}
	raw, err := openSubscription(key, "credential", b, enc)
	if err != nil {
		return "", err
	}
	token := string(raw)
	if !validSubscriptionToken(token) || subtle.ConstantTimeCompare([]byte(subscriptionTokenHash(token)), []byte(b.TokenSHA)) != 1 {
		return "", ErrUnavailable
	}
	return token, nil
}

func (s *Store) MutateLabSubscription(ctx context.Context, key []byte, actorID, clientID string, in SubscriptionTokenMutation) (SubscriptionTokenReceipt, error) {
	var out SubscriptionTokenReceipt
	if !eventRE.MatchString(in.EventID) || in.ExpectedGeneration >= routeMax-1 || (in.Action != "issue" && in.Action != "rotate" && in.Action != "revoke") {
		return out, ErrInvalid
	}
	tx, err := s.subscriptionTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	c, err := subscriptionActor(ctx, tx, actorID, clientID, true, false)
	if err != nil {
		return out, err
	}
	h, err := readSubscriptionHead(ctx, tx, clientID)
	if err != nil {
		return out, err
	}
	fp := fingerprint("lab-subscription-token-v1", struct {
		ActorID, ClientID string
		Mutation          SubscriptionTokenMutation
	}{actorID, clientID, in})
	var previousFP string
	err = tx.QueryRowContext(ctx, `SELECT event_id,client_id,generation,state,fingerprint FROM pro_subscription_token_events WHERE event_id=?`, in.EventID).Scan(&out.EventID, &out.ClientID, &out.Generation, &out.State, &previousFP)
	if err == nil {
		if fp != previousFP || out.ClientID != clientID {
			return SubscriptionTokenReceipt{}, ErrConflict
		}
		out.Replayed, out.Superseded = true, h.Generation != out.Generation
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return SubscriptionTokenReceipt{}, err
	}
	if err = consumerEventUnused(ctx, tx, in.EventID); err != nil {
		return out, err
	}
	var used int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM pro_subscription_snapshots WHERE event_id=?`, in.EventID).Scan(&used); err != nil {
		return out, err
	}
	if used != 0 || h.Generation != in.ExpectedGeneration || (in.Action == "issue" && h.State != "absent") || (in.Action == "rotate" && h.State == "absent") || (in.Action == "revoke" && h.State != "active") {
		return SubscriptionTokenReceipt{}, ErrConflict
	}
	if c.Archived && in.Action != "revoke" {
		return SubscriptionTokenReceipt{}, ErrConflict
	}
	// A wrong encryption key cannot supersede an existing credential, including
	// rotation from a revoked head. Revocation does not discard the key binding.
	if h.State == "active" {
		if _, err = revealSubscriptionHead(ctx, tx, key, h); err != nil {
			return out, err
		}
	} else if h.State == "revoked" {
		prior := subscriptionHead{ClientID: clientID, State: "active"}
		if err = tx.QueryRowContext(ctx, `SELECT id,generation FROM pro_subscription_credentials WHERE client_id=? ORDER BY generation DESC LIMIT 1`, clientID).Scan(&prior.CredentialID, &prior.Generation); err != nil {
			return out, ErrUnavailable
		}
		if _, err = revealSubscriptionHead(ctx, tx, key, prior); err != nil {
			return out, err
		}
	}
	out = SubscriptionTokenReceipt{EventID: in.EventID, ClientID: clientID, Generation: h.Generation + 1, State: "revoked"}
	var credential any
	now := time.Now().UnixNano()
	if in.Action != "revoke" {
		id, e := subscriptionID()
		if e != nil {
			return out, e
		}
		b := make([]byte, 32)
		if _, e = rand.Read(b); e != nil {
			return out, e
		}
		token := base64.RawURLEncoding.EncodeToString(b)
		binding := subscriptionCredentialBinding{ID: id, ClientID: clientID, Generation: out.Generation, TokenSHA: subscriptionTokenHash(token), Created: now}
		enc, e := sealSubscription(key, "credential", binding, []byte(token))
		if e != nil {
			return out, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO pro_subscription_credentials(id,client_id,generation,token_sha,token_enc,created) VALUES(?,?,?,?,?,?)`, id, clientID, out.Generation, binding.TokenSHA, enc, now); err != nil {
			return out, err
		}
		credential, out.State = id, "active"
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO pro_subscription_token_events(event_id,actor_id,client_id,fingerprint,action,generation,state,credential_id,created) VALUES(?,?,?,?,?,?,?,?,?)`, in.EventID, actorID, clientID, fp, in.Action, out.Generation, out.State, credential, now); err != nil {
		return out, err
	}
	if h.Generation == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO pro_subscription_token_heads(client_id,generation,state,credential_id,event_id) VALUES(?,?,?,?,?)`, clientID, out.Generation, out.State, credential, in.EventID)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE pro_subscription_token_heads SET generation=?,state=?,credential_id=?,event_id=? WHERE client_id=? AND generation=?`, out.Generation, out.State, credential, in.EventID, clientID, h.Generation)
		if err == nil {
			if n, e := result.RowsAffected(); e != nil || n != 1 {
				return out, ErrConflict
			}
		}
	}
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return SubscriptionTokenReceipt{}, err
	}
	return out, nil
}

func (s *Store) RevealLabSubscription(ctx context.Context, key []byte, actorID, clientID string, expectedGeneration uint64) (SubscriptionSecret, error) {
	var out SubscriptionSecret
	if expectedGeneration == 0 || expectedGeneration >= routeMax {
		return out, ErrInvalid
	}
	tx, err := s.subscriptionTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	c, err := subscriptionActor(ctx, tx, actorID, clientID, true, false)
	if err != nil {
		return out, err
	}
	if c.Archived {
		return out, ErrNotFound
	}
	h, err := readSubscriptionHead(ctx, tx, clientID)
	if err != nil {
		return out, err
	}
	if h.Generation != expectedGeneration {
		return out, ErrConflict
	}
	token, err := revealSubscriptionHead(ctx, tx, key, h)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return SubscriptionSecret{ClientID: clientID, Generation: h.Generation, Token: token}, nil
}

func (s *Store) LabSubscription(ctx context.Context, key []byte, actorID, clientID string, endpoint mx.ProtocolHealthEndpoint) (LabSubscriptionView, error) {
	var out LabSubscriptionView
	if !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := s.subscriptionTx(ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if _, err = subscriptionActor(ctx, tx, actorID, clientID, false, false); err != nil {
		return out, err
	}
	h, err := readSubscriptionHead(ctx, tx, clientID)
	if err != nil {
		return out, err
	}
	out = LabSubscriptionView{ClientID: clientID, Generation: h.Generation, State: h.State}
	row, err := readSubscriptionSnapshotHead(ctx, tx, clientID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return out, err
	}
	if err == nil {
		if _, err = openSubscriptionSnapshot(key, row); err != nil {
			return out, err
		}
		_, proofErr := s.currentSubscriptionSnapshot(ctx, tx, key, row, &endpoint, time.Now().UnixNano())
		row.Current = proofErr == nil
		out.Snapshot = &row.LabSubscriptionSnapshot
	}
	if err = tx.Commit(); err != nil {
		return LabSubscriptionView{}, err
	}
	return out, nil
}
