//go:build linux

package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

const DarkRestoreCustomerConnectionScope = "darkrestore_customer_connection_v1"

type DarkRestoreConnectionInput struct {
	EventID          string `json:"event_id"`
	PlanSHA          string `json:"plan_sha256"`
	AcknowledgeUsage bool   `json:"acknowledge_usage"`
}
type DarkRestoreConnectionStatus struct {
	Scope                      string `json:"scope"`
	ClientID                   string `json:"client_id"`
	PlanSHA                    string `json:"plan_sha256,omitempty"`
	ID                         string `json:"id,omitempty"`
	State                      string `json:"state"`
	CustomerConnectionVerified bool   `json:"customer_connection_verified"`
	ReceiptSHA                 string `json:"receipt_sha256,omitempty"`
	Completed                  int64  `json:"completed_unix_nano,omitempty"`
	AuthorizesMutation         bool   `json:"authorizes_mutation"`
}
type connectionIntent struct {
	ID, EventID, OwnerID, ClientID string
	Created, Expires               int64
	Binding                        mx.CustomerConnectionChallenge
}

func connectionJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func connectionParent(ctx context.Context, q sellerQueryV2, plan string) (DarkRestoreAdmission, DarkRestoreListenerApply, error) {
	var a DarkRestoreAdmission
	var l DarkRestoreListenerApply
	var ar, lr []byte
	e := q.QueryRowContext(ctx, "SELECT a.receipt_json,l.receipt_json FROM darkrestore_native_admission_v1 a JOIN darkrestore_listener_apply_v1 l ON l.plan_sha=a.plan_sha WHERE a.plan_sha=?", plan).Scan(&ar, &lr)
	if e != nil || len(ar) > 131072 || len(lr) > 16384 || json.Unmarshal(ar, &a) != nil || json.Unmarshal(lr, &l) != nil || verifyListenerApplyTx(ctx, q, l) != nil {
		return a, l, ErrDarkRestoreCutover
	}
	return a, l, nil
}
func verifyConnectionIntent(ctx context.Context, q sellerQueryV2, i connectionIntent) error {
	a, l, e := connectionParent(ctx, q, i.Binding.PlanSHA)
	if e != nil {
		return e
	}
	b := i.Binding
	r := a.Registered
	if !routeID.MatchString(i.ID) || !eventRE.MatchString(i.EventID) || i.OwnerID != a.ActorID || i.ClientID != a.Snapshot.ClientID ||
		i.Created <= l.AppliedUnixNano || i.Expires-i.Created != int64(2*time.Minute) || b.Version != 1 || b.ID != i.ID || !b.Consent ||
		b.ClientID != i.ClientID || b.EpochID != r.Epoch.ID || b.NodeID != r.Plan.NodeID || b.InstallationID != r.Plan.InstallationID ||
		b.ConfigSHA != r.Plan.ConfigSHA || b.CoreSHA != r.Birth.CoreSHA || b.Process != r.Birth.Process ||
		b.ListenerSHA != l.Digest() || b.CredentialSHA != admissionBytesSHA([]byte(a.Snapshot.ClientUUID)) ||
		b.InboundID != r.Plan.Prepared.InboundID || b.Port < 1024 || !routeSHA.MatchString(b.EndpointSHA) ||
		!bytes.Equal(b.Signer, r.Signer) || b.State != (mx.CustomerConnectionState{}) || b.Issued != 0 || b.Expires != 0 || b.Signature != "" {
		return ErrUnavailable
	}
	return nil
}
func connectionLive(ctx context.Context, q sellerQueryV2, i connectionIntent) error {
	var ok bool
	e := q.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM auth_users a JOIN representatives r ON r.id=a.representative_id JOIN pro_usage_epochs e ON e.epoch_id=? JOIN pro_usage_eligibility p ON p.client_id=? WHERE a.id=? AND a.role='owner' AND a.representative_id='owner' AND a.disabled=0 AND r.enabled=1 AND e.state='open' AND p.eligible=1)", i.Binding.EpochID, i.ClientID, i.OwnerID).Scan(&ok)
	if e != nil {
		return e
	}
	if !ok {
		return ErrOwnershipDenied
	}
	return nil
}
func readConnectionIntent(ctx context.Context, q sellerQueryV2, plan string) (connectionIntent, bool, error) {
	var i connectionIntent
	present, e := connectionSchema(ctx, q)
	if e != nil || !present {
		return i, false, e
	}
	var raw []byte
	var id, event, owner, client string
	e = q.QueryRowContext(ctx, "SELECT id,event_id,owner_id,client_id,intent_json FROM darkrestore_connection_v1_intent WHERE plan_sha=?", plan).Scan(&id, &event, &owner, &client, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return i, false, nil
	}
	if e != nil {
		return i, false, e
	}
	if len(raw) > 32768 || json.Unmarshal(raw, &i) != nil || !bytes.Equal(raw, connectionJSON(i)) || i.ID != id || i.EventID != event || i.OwnerID != owner || i.ClientID != client || i.Binding.PlanSHA != plan || verifyConnectionIntent(ctx, q, i) != nil {
		return i, false, ErrUnavailable
	}
	return i, true, nil
}
func (s *Store) QueueDarkRestoreConnection(ctx context.Context, key []byte, token, client string, in DarkRestoreConnectionInput, endpoint mx.ProtocolHealthEndpoint) (DarkRestoreConnectionStatus, error) {
	var out DarkRestoreConnectionStatus
	if !in.AcknowledgeUsage || !eventRE.MatchString(in.EventID) || !routeSHA.MatchString(in.PlanSHA) || !endpoint.Valid() {
		return out, ErrInvalid
	}
	// Recheck current profile, quota and authority outside and inside the write transaction.
	if _, e := s.DarkRestoreAdmissionEpoch(ctx, key, token, client, in.PlanSHA, true); e != nil {
		return out, e
	}
	tx, e := executionTx(s, ctx, key)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	owner, e := ownershipOwner(ctx, tx, token)
	if e != nil {
		return out, e
	}
	prior, found, e := readConnectionIntent(ctx, tx, in.PlanSHA)
	if e != nil {
		return out, e
	}
	if found {
		if prior.EventID != in.EventID || prior.OwnerID != owner || prior.ClientID != client || prior.Binding.EndpointSHA != endpoint.SHA() {
			return out, ErrConflict
		}
		out, e = connectionStatus(ctx, tx, prior)
		if e != nil {
			return out, e
		}
		return out, tx.Commit()
	}
	a, l, e := connectionParent(ctx, tx, in.PlanSHA)
	if e != nil {
		return out, e
	}
	if a.ActorID != owner || a.Snapshot.ClientID != client {
		return out, ErrOwnershipDenied
	}
	p, e := readNativeInboundPlan(ctx, tx, key, a.Registered.Plan.Prepared.InboundID)
	if e != nil || len(p.Nodes) != 1 || p.Nodes[0].NodeID != a.Registered.Plan.NodeID || p.Nodes[0].InstallationID != a.Registered.Plan.InstallationID {
		return out, ErrDarkRestoreCutover
	}
	id, e := randomUUID()
	if e != nil {
		return out, e
	}
	id = strings.ReplaceAll(id, "-", "")
	now := time.Now().UnixNano()
	r := a.Registered
	i := connectionIntent{ID: id, EventID: in.EventID, OwnerID: owner, ClientID: client, Created: now, Expires: now + int64(2*time.Minute), Binding: mx.CustomerConnectionChallenge{
		Version: 1, ID: id, PlanSHA: in.PlanSHA, ClientID: client, EpochID: r.Epoch.ID, ConfigSHA: r.Plan.ConfigSHA, CoreSHA: r.Birth.CoreSHA,
		CredentialSHA: admissionBytesSHA([]byte(a.Snapshot.ClientUUID)), ListenerSHA: l.Digest(), EndpointSHA: endpoint.SHA(), NodeID: r.Plan.NodeID, InstallationID: r.Plan.InstallationID, Process: r.Birth.Process,
		InboundID: r.Plan.Prepared.InboundID, Port: p.Nodes[0].DirectPort, Signer: append([]byte(nil), r.Signer...), Consent: true}}
	if e = verifyConnectionIntent(ctx, tx, i); e != nil {
		return out, e
	}
	prepared, prepareErr := readCutoverReceipt(ctx, tx, i.ClientID)
	if prepareErr != nil || verifyPreparedPlanReceipt(ctx, tx, prepared) != nil {
		return out, ErrDarkRestoreCutover
	}
	if e = connectionLive(ctx, tx, i); e != nil {
		return out, e
	}
	present, e := connectionSchema(ctx, tx)
	if e != nil {
		return out, e
	}
	if !present {
		for _, ddl := range darkRestoreConnectionDDL {
			if _, e = tx.ExecContext(ctx, ddl); e != nil {
				return out, e
			}
		}
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO darkrestore_connection_v1_intent(id,plan_sha,event_id,owner_id,client_id,intent_json) VALUES(?,?,?,?,?,?)", id, in.PlanSHA, in.EventID, owner, client, connectionJSON(i)); e != nil {
		return out, e
	}
	out, e = connectionStatus(ctx, tx, i)
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func verifyConnectionRequest(ctx context.Context, q sellerQueryV2, i connectionIntent, r mx.CustomerConnectionChallenge) error {
	b := r
	b.State = mx.CustomerConnectionState{}
	b.Issued = 0
	b.Expires = 0
	b.Signature = ""
	a, _, e := connectionParent(ctx, q, i.Binding.PlanSHA)
	if e != nil {
		return e
	}
	epochSHA := admissionBytesSHA(connectionJSON(a.Registered.Epoch))
	if !bytes.Equal(connectionJSON(b), connectionJSON(i.Binding)) || !r.Valid() || r.Issued < i.Created || r.Issued >= i.Expires || r.Expires > i.Expires ||
		r.State.Expected.ActiveSequence != a.Registered.Birth.Lease.Sequence || r.State.Usage.EpochSHA != epochSHA {
		return ErrUnavailable
	}
	return nil
}
func readConnectionRequest(ctx context.Context, q sellerQueryV2, i connectionIntent) (mx.CustomerConnectionChallenge, bool, error) {
	var r mx.CustomerConnectionChallenge
	var raw []byte
	e := q.QueryRowContext(ctx, "SELECT request_json FROM darkrestore_connection_v1_request WHERE id=?", i.ID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return r, false, nil
	}
	if e != nil {
		return r, false, e
	}
	if len(raw) > 32768 || json.Unmarshal(raw, &r) != nil || !bytes.Equal(raw, connectionJSON(r)) || verifyConnectionRequest(ctx, q, i, r) != nil {
		return r, false, ErrUnavailable
	}
	return r, true, nil
}

// Node-authenticated polling signs and persists one exact nonce before delivery.
// Expired or interrupted attempts never receive a renewed request.
func (s *Store) PollDarkRestoreConnection(ctx context.Context, key, nodeKey []byte, node, installation, plan string, state mx.CustomerConnectionState, endpoint mx.ProtocolHealthEndpoint) (mx.CustomerConnectionChallenge, bool, error) {
	var zero mx.CustomerConnectionChallenge
	tx, e := executionTx(s, ctx, key)
	if e != nil {
		return zero, false, e
	}
	defer tx.Rollback()
	i, found, e := readConnectionIntent(ctx, tx, plan)
	if e != nil || !found {
		return zero, false, e
	}
	if i.Binding.NodeID != node || i.Binding.InstallationID != installation || !endpoint.Valid() || endpoint.SHA() != i.Binding.EndpointSHA {
		return zero, false, ErrOwnershipDenied
	}
	r, issued, e := readConnectionRequest(ctx, tx, i)
	if e != nil {
		return zero, false, e
	}
	if issued {
		return r, true, tx.Commit()
	}
	now := time.Now().UnixNano()
	if now >= i.Expires {
		return zero, false, nil
	}
	prepared, prepareErr := readCutoverReceipt(ctx, tx, i.ClientID)
	if prepareErr != nil || verifyPreparedPlanReceipt(ctx, tx, prepared) != nil {
		return zero, false, ErrDarkRestoreCutover
	}
	if e = connectionLive(ctx, tx, i); e != nil {
		return zero, false, e
	}
	r = i.Binding
	r.State = state
	r.Issued = now
	r.Expires = now + int64(8*time.Second)
	if r.Expires > state.LeaseExpires {
		r.Expires = state.LeaseExpires
	}
	if r.Expires > i.Expires {
		r.Expires = i.Expires
	}
	r, e = mx.SignCustomerConnectionChallenge(nodeKey, r)
	if e != nil {
		return zero, false, e
	}
	if e = verifyConnectionRequest(ctx, tx, i, r); e != nil {
		return zero, false, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO darkrestore_connection_v1_request(id,request_json) VALUES(?,?)", i.ID, connectionJSON(r)); e != nil {
		return zero, false, e
	}
	return r, true, tx.Commit()
}
func (s *Store) RegisterDarkRestoreConnection(ctx context.Context, nodeKey []byte, node, installation, plan string, r mx.CustomerConnectionReceipt) (string, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	i, found, e := readConnectionIntent(ctx, tx, plan)
	if e != nil {
		return "", e
	}
	if !found {
		return "", ErrNotFound
	}
	if i.Binding.NodeID != node || i.Binding.InstallationID != installation {
		return "", ErrOwnershipDenied
	}
	request, found, e := readConnectionRequest(ctx, tx, i)
	if e != nil {
		return "", e
	}
	if !found || !r.Verify(request) || !mx.VerifyCustomerConnectionChallenge(nodeKey, request, r.Completed) || r.Completed > time.Now().UnixNano() {
		return "", ErrDarkRestoreCutover
	}
	raw := connectionJSON(r)
	var prior []byte
	e = tx.QueryRowContext(ctx, "SELECT receipt_json FROM darkrestore_connection_v1_receipt WHERE id=?", i.ID).Scan(&prior)
	if e == nil {
		if !bytes.Equal(raw, prior) {
			return "", ErrConflict
		}
		return admissionBytesSHA(prior), tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	// Historical delayed delivery is allowed; the signed attempt itself expired
	// inside its original lease. This insert confers no current authority.
	if _, e = tx.ExecContext(ctx, "INSERT INTO darkrestore_connection_v1_receipt(id,receipt_json,recorded_ns) VALUES(?,?,?)", i.ID, raw, time.Now().UnixNano()); e != nil {
		return "", e
	}
	return admissionBytesSHA(raw), tx.Commit()
}
func connectionStatus(ctx context.Context, q sellerQueryV2, i connectionIntent) (DarkRestoreConnectionStatus, error) {
	out := DarkRestoreConnectionStatus{Scope: DarkRestoreCustomerConnectionScope, ClientID: i.ClientID, PlanSHA: i.Binding.PlanSHA, ID: i.ID, State: "pending"}
	r, found, e := readConnectionRequest(ctx, q, i)
	if e != nil {
		return out, e
	}
	if !found {
		if time.Now().UnixNano() >= i.Expires {
			out.State = "expired_unknown"
		}
		return out, nil
	}
	out.State = "issued"
	if time.Now().UnixNano() >= r.Expires {
		out.State = "expired_unknown"
	}
	var raw []byte
	var recorded int64
	e = q.QueryRowContext(ctx, "SELECT receipt_json,recorded_ns FROM darkrestore_connection_v1_receipt WHERE id=?", i.ID).Scan(&raw, &recorded)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	var receipt mx.CustomerConnectionReceipt
	if len(raw) > 65536 || json.Unmarshal(raw, &receipt) != nil || !bytes.Equal(raw, connectionJSON(receipt)) || !receipt.Verify(r) || recorded < receipt.Completed {
		return out, ErrUnavailable
	}
	out.State = "verified"
	out.CustomerConnectionVerified = true
	out.ReceiptSHA = admissionBytesSHA(raw)
	out.Completed = receipt.Completed
	return out, nil
}
func (s *Store) DarkRestoreConnectionStatus(ctx context.Context, token, client string) (DarkRestoreConnectionStatus, error) {
	out := DarkRestoreConnectionStatus{Scope: DarkRestoreCustomerConnectionScope, ClientID: client, State: "not_requested"}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	owner, e := ownershipOwner(ctx, tx, token)
	if e != nil {
		return out, e
	}
	present, e := connectionSchema(ctx, tx)
	if e != nil || !present {
		return out, e
	}
	var plan string
	e = tx.QueryRowContext(ctx, "SELECT plan_sha FROM darkrestore_connection_v1_intent WHERE client_id=? AND owner_id=?", client, owner).Scan(&plan)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return out, e
	}
	i, _, e := readConnectionIntent(ctx, tx, plan)
	if e != nil {
		return out, e
	}
	return connectionStatus(ctx, tx, i)
}
func validateDarkRestoreConnections(ctx context.Context, tx *sql.Tx) error {
	present, e := connectionSchema(ctx, tx)
	if e != nil || !present {
		return e
	}
	rows, e := tx.QueryContext(ctx, "SELECT plan_sha FROM darkrestore_connection_v1_intent")
	if e != nil {
		return e
	}
	var plans []string
	for rows.Next() {
		var p string
		if e = rows.Scan(&p); e != nil {
			rows.Close()
			return e
		}
		plans = append(plans, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, p := range plans {
		i, _, e := readConnectionIntent(ctx, tx, p)
		if e != nil {
			return e
		}
		if _, e = connectionStatus(ctx, tx, i); e != nil {
			return e
		}
	}
	var orphans int
	if e = tx.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM darkrestore_connection_v1_request r LEFT JOIN darkrestore_connection_v1_intent i ON i.id=r.id WHERE i.id IS NULL)+(SELECT COUNT(*) FROM darkrestore_connection_v1_receipt r LEFT JOIN darkrestore_connection_v1_request q ON q.id=r.id WHERE q.id IS NULL)").Scan(&orphans); e != nil || orphans != 0 {
		return ErrUnavailable
	}
	return nil
}
