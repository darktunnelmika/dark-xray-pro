//go:build linux

package business

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

const DarkRestoreSystemProbeRoundScopeV1 = "darkrestore_system_probe_round_v1"

type DarkRestoreSystemProbeInputV1 struct {
	EventID string `json:"event_id"`
	PlanSHA string `json:"plan_sha256"`
}

type DarkRestoreSystemProbeStatusV1 struct {
	Scope              string `json:"scope"`
	ClientID           string `json:"client_id"`
	PlanSHA            string `json:"plan_sha256,omitempty"`
	ChallengeID        string `json:"challenge_id,omitempty"`
	State              string `json:"state"`
	ServingHealth      bool   `json:"serving_health_recorded"`
	AuthorizesMutation bool   `json:"authorizes_mutation"`
}

type darkRestoreSystemProbeIntentV1 struct {
	ID, EventID, OwnerID, ClientID string
	Created, Expires               int64
	Binding                        mx.ProtocolHealthRequest
}

func systemProbeJSON(v any) []byte { b, _ := json.Marshal(v); return b }

func systemProbeDerivedID(domain string, values ...any) string {
	h := sha256.New()
	h.Write([]byte("DARK-PRO-DARKRESTORE-SYSTEM-PROBE-" + domain + "-v1\x00"))
	for _, value := range values {
		raw, _ := json.Marshal(value)
		h.Write(raw)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:32]
}

func verifyDarkRestoreSystemProbeIntentTx(ctx context.Context, tx *sql.Tx, in darkRestoreSystemProbeIntentV1) error {
	admission, registered, listener, err := servingHealthSourcesTx(ctx, tx, in.ClientID)
	if err != nil {
		return err
	}
	original, originalListener, err := connectionParent(ctx, tx, admission.PlanSHA)
	if err != nil || originalListener.Digest() != listener.Digest() {
		return ErrDarkRestoreCutover
	}
	b := in.Binding
	plan := registered.Plan
	if !routeID.MatchString(in.ID) || !eventRE.MatchString(in.EventID) || in.OwnerID != original.ActorID ||
		in.ClientID != admission.ClientID || in.ClientID != original.Snapshot.ClientID || in.Created <= listener.AppliedUnixNano ||
		in.Expires-in.Created != int64(2*time.Minute) || b.Version != mx.InitialApplyProtocolHealthVersion ||
		b.ID != in.ID || b.HandoverID != "" || b.NodeID != plan.NodeID || b.InstallationID != plan.InstallationID ||
		b.InboundID != plan.Prepared.InboundID || b.Port < 1024 || b.CredentialSHA != plan.SystemProbeCredentialSHA ||
		b.SystemProbeID != plan.SystemProbeID || b.EndpointSHA == "" || b.InitialApplySHA != listener.Digest() ||
		b.RuntimeInstance != systemProbeDerivedID("RUNTIME", registered.Birth.Process) ||
		b.StageID != systemProbeDerivedID("STAGE", admission.PlanSHA) ||
		b.ApplyCommandID != systemProbeDerivedID("APPLY", listener.Digest()) ||
		b.Expected != (mx.RuntimeCondition{}) || b.Usage != (mx.MeteredCondition{}) ||
		b.Issued != 0 || b.Expires != 0 || b.Signature != "" ||
		b.ConsumerManifestSHA != "" || b.ConsumerAddress != "" || b.ConsumerPort != 0 {
		return ErrDarkRestoreCutover
	}
	return nil
}

func readDarkRestoreSystemProbeIntentTx(ctx context.Context, tx *sql.Tx, plan string) (darkRestoreSystemProbeIntentV1, bool, error) {
	var out darkRestoreSystemProbeIntentV1
	present, err := darkRestoreSystemProbeRoundSchema(ctx, tx)
	if err != nil || !present {
		return out, false, err
	}
	var raw []byte
	var id, event, owner, client string
	var created, expires int64
	err = tx.QueryRowContext(ctx, "SELECT id,event_id,owner_id,client_id,created_ns,expires_ns,intent_json FROM darkrestore_system_probe_v1_intent WHERE plan_sha=?", plan).Scan(&id, &event, &owner, &client, &created, &expires, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	if len(raw) == 0 || len(raw) > 32768 || json.Unmarshal(raw, &out) != nil || !bytes.Equal(raw, systemProbeJSON(out)) ||
		out.ID != id || out.EventID != event || out.OwnerID != owner || out.ClientID != client ||
		out.Created != created || out.Expires != expires ||
		verifyDarkRestoreSystemProbeIntentTx(ctx, tx, out) != nil {
		return out, false, ErrUnavailable
	}
	return out, true, nil
}

func readDarkRestoreSystemProbeRequestTx(ctx context.Context, tx *sql.Tx, in darkRestoreSystemProbeIntentV1) (mx.ProtocolHealthRequest, bool, error) {
	var out mx.ProtocolHealthRequest
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT request_json FROM darkrestore_system_probe_v1_request WHERE id=?", in.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, false, nil
	}
	if err != nil {
		return out, false, err
	}
	base := out
	if len(raw) == 0 || len(raw) > 32768 || json.Unmarshal(raw, &out) != nil || !bytes.Equal(raw, systemProbeJSON(out)) {
		return base, false, ErrUnavailable
	}
	unsigned := out
	unsigned.Expected = mx.RuntimeCondition{}
	unsigned.Usage = mx.MeteredCondition{}
	unsigned.Issued, unsigned.Expires, unsigned.Signature = 0, 0, ""
	if !bytes.Equal(systemProbeJSON(unsigned), systemProbeJSON(in.Binding)) ||
		out.Issued < in.Created || out.Expires > in.Expires || out.Expires-out.Issued > int64(8*time.Second) {
		return base, false, ErrUnavailable
	}
	return out, true, nil
}

func (s *Store) QueueDarkRestoreSystemProbeV1(ctx context.Context, key []byte, token, client string, in DarkRestoreSystemProbeInputV1, endpoint mx.ProtocolHealthEndpoint) (DarkRestoreSystemProbeStatusV1, error) {
	out := DarkRestoreSystemProbeStatusV1{Scope: DarkRestoreSystemProbeRoundScopeV1, ClientID: client, State: "pending"}
	if !eventRE.MatchString(in.EventID) || !routeSHA.MatchString(in.PlanSHA) || !endpoint.Valid() {
		return out, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	owner, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return out, err
	}
	prior, found, err := readDarkRestoreSystemProbeIntentTx(ctx, tx, in.PlanSHA)
	if err != nil {
		return out, err
	}
	if found {
		if prior.EventID != in.EventID || prior.OwnerID != owner || prior.ClientID != client || prior.Binding.EndpointSHA != endpoint.SHA() {
			return out, ErrConflict
		}
		out.PlanSHA, out.ChallengeID = in.PlanSHA, prior.ID
		if _, requested, e := readDarkRestoreSystemProbeRequestTx(ctx, tx, prior); e != nil {
			return out, e
		} else if requested {
			out.State = "issued"
		}
		if _, e := readDarkRestoreServingHealthTx(ctx, tx, client); e == nil {
			out.State, out.ServingHealth = "recorded", true
		} else if !errors.Is(e, ErrNotFound) && !errors.Is(e, ErrDarkRestoreCutover) {
			return out, e
		}
		return out, tx.Commit()
	}
	admission, registered, listener, err := servingHealthSourcesTx(ctx, tx, client)
	if err != nil || admission.PlanSHA != in.PlanSHA {
		return out, ErrDarkRestoreCutover
	}
	original, originalListener, err := connectionParent(ctx, tx, in.PlanSHA)
	if err != nil || original.ActorID != owner || original.Snapshot.ClientID != client ||
		originalListener.Digest() != listener.Digest() {
		return out, ErrDarkRestoreCutover
	}
	plan := registered.Plan
	candidate, err := registeredQuarantineCandidateTx(ctx, tx, key, client, original.Snapshot.ClientUUID, plan)
	if err != nil {
		return out, ErrDarkRestoreCutover
	}
	native, err := readNativeInboundPlan(ctx, tx, key, plan.Prepared.InboundID)
	if err != nil || len(native.Nodes) != 1 || native.Nodes[0].NodeID != plan.NodeID || native.Nodes[0].InstallationID != plan.InstallationID {
		return out, ErrDarkRestoreCutover
	}
	id, err := randomUUID()
	if err != nil {
		return out, err
	}
	id = strings.ReplaceAll(id, "-", "")
	now := time.Now().UnixNano()
	intent := darkRestoreSystemProbeIntentV1{
		ID: id, EventID: in.EventID, OwnerID: owner, ClientID: client,
		Created: now, Expires: now + int64(2*time.Minute),
		Binding: mx.ProtocolHealthRequest{
			Version: mx.InitialApplyProtocolHealthVersion, ID: id,
			NodeID: plan.NodeID, InstallationID: plan.InstallationID,
			RuntimeInstance: systemProbeDerivedID("RUNTIME", registered.Birth.Process),
			StageID:         systemProbeDerivedID("STAGE", admission.PlanSHA),
			ApplyCommandID:  systemProbeDerivedID("APPLY", listener.Digest()),
			InboundID:       plan.Prepared.InboundID, Port: native.Nodes[0].DirectPort,
			CredentialSHA: plan.SystemProbeCredentialSHA, SystemProbeID: plan.SystemProbeID,
			EndpointSHA: endpoint.SHA(), InitialApplySHA: listener.Digest(),
		},
	}
	if err = verifyDarkRestoreSystemProbeIntentTx(ctx, tx, intent); err != nil ||
		mx.ValidateProtocolHealthSupport(candidate, intent.Binding, endpoint) != nil {
		return out, ErrDarkRestoreCutover
	}
	present, err := darkRestoreSystemProbeRoundSchema(ctx, tx)
	if err != nil {
		return out, err
	}
	if !present {
		for _, ddl := range darkRestoreSystemProbeRoundDDL {
			if _, err = tx.ExecContext(ctx, ddl); err != nil {
				return out, err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_system_probe_v1_intent(id,plan_sha,event_id,owner_id,client_id,created_ns,expires_ns,intent_json) VALUES(?,?,?,?,?,?,?,?)",
		intent.ID, in.PlanSHA, in.EventID, owner, client, intent.Created, intent.Expires, systemProbeJSON(intent)); err != nil {
		return out, err
	}
	out.PlanSHA, out.ChallengeID = in.PlanSHA, id
	return out, tx.Commit()
}

func (s *Store) PollDarkRestoreSystemProbeV1(ctx context.Context, key, nodeKey []byte, node, installation, plan string, state mx.DarkRestoreSystemProbeState, endpoint mx.ProtocolHealthEndpoint) (mx.ProtocolHealthRequest, bool, error) {
	var zero mx.ProtocolHealthRequest
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return zero, false, err
	}
	defer tx.Rollback()
	intent, found, err := readDarkRestoreSystemProbeIntentTx(ctx, tx, plan)
	if err != nil || !found {
		return zero, false, err
	}
	if len(nodeKey) < 32 || intent.Binding.NodeID != node || intent.Binding.InstallationID != installation ||
		!endpoint.Valid() || intent.Binding.EndpointSHA != endpoint.SHA() {
		return zero, false, ErrOwnershipDenied
	}
	if request, issued, e := readDarkRestoreSystemProbeRequestTx(ctx, tx, intent); e != nil || issued {
		return request, issued, e
	}
	now := time.Now().UnixNano()
	if now >= intent.Expires {
		return zero, false, tx.Commit()
	}
	_, registered, _, err := servingHealthSourcesTx(ctx, tx, intent.ClientID)
	if err != nil {
		return zero, false, err
	}
	epochSHA, err := mx.MeteredEpochSHA(registered.Epoch)
	if err != nil {
		return zero, false, err
	}
	expectedUsage := mx.MeteredCondition{EpochID: registered.Epoch.ID, EpochSHA: epochSHA, AdmissionSHA: registered.Plan.PreparedAdmissionSHA}
	expected := state.Expected
	if !expected.ProcessReady || expected.ActiveSequence != registered.Birth.Lease.Sequence ||
		expected.ActiveSHA != registered.Plan.ConfigSHA || expected.SavedSequence != expected.ActiveSequence ||
		expected.SavedSHA != expected.ActiveSHA || expected.Highwater != expected.ActiveSequence ||
		state.Usage != expectedUsage || state.LeaseExpires <= now {
		return zero, false, ErrDarkRestoreCutover
	}
	request := intent.Binding
	request.Expected, request.Usage = state.Expected, state.Usage
	request.Issued, request.Expires = now, now+int64(8*time.Second)
	for _, limit := range []int64{state.LeaseExpires, intent.Expires, registered.Birth.Lease.Expires} {
		if request.Expires > limit {
			request.Expires = limit
		}
	}
	request, err = mx.SignProtocolHealthRequest(nodeKey, request)
	if err != nil {
		return zero, false, ErrDarkRestoreCutover
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO darkrestore_system_probe_v1_request(id,request_json) VALUES(?,?)", intent.ID, systemProbeJSON(request)); err != nil {
		return zero, false, err
	}
	return request, true, tx.Commit()
}

func validDarkRestoreSystemProbeReceiptAt(nodeKey []byte, request mx.ProtocolHealthRequest, receipt mx.ProtocolHealthReceipt, now time.Time) bool {
	return receipt.Completed <= now.UnixNano() &&
		mx.VerifyProtocolHealthReceipt(nodeKey, request, receipt, time.Unix(0, receipt.Completed))
}

func (s *Store) RegisterDarkRestoreSystemProbeV1(ctx context.Context, nodeKey []byte, node, installation, plan string, endpoint mx.ProtocolHealthEndpoint, receipt mx.ProtocolHealthReceipt) (DarkRestoreServingHealthEvidenceV1, error) {
	var zero DarkRestoreServingHealthEvidenceV1
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	intent, found, err := readDarkRestoreSystemProbeIntentTx(ctx, tx, plan)
	if err != nil {
		return zero, err
	}
	if !found {
		return zero, ErrNotFound
	}
	if len(nodeKey) < 32 || intent.Binding.NodeID != node || intent.Binding.InstallationID != installation ||
		!endpoint.Valid() || intent.Binding.EndpointSHA != endpoint.SHA() {
		return zero, ErrOwnershipDenied
	}
	request, found, err := readDarkRestoreSystemProbeRequestTx(ctx, tx, intent)
	if err != nil || !found {
		return zero, ErrDarkRestoreCutover
	}
	now := time.Now()
	if !validDarkRestoreSystemProbeReceiptAt(nodeKey, request, receipt, now) {
		return zero, ErrDarkRestoreCutover
	}
	out, err := recordDarkRestoreServingHealthTx(ctx, tx, intent.ClientID, nodeKey, endpoint, request, receipt, now.UnixNano())
	if err != nil {
		return zero, err
	}
	return out, tx.Commit()
}

func validateDarkRestoreSystemProbeRounds(ctx context.Context, tx *sql.Tx) error {
	present, err := darkRestoreSystemProbeRoundSchema(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, "SELECT plan_sha FROM darkrestore_system_probe_v1_intent")
	if err != nil {
		return err
	}
	var plans []string
	for rows.Next() {
		var plan string
		if err = rows.Scan(&plan); err != nil {
			rows.Close()
			return err
		}
		plans = append(plans, plan)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, plan := range plans {
		intent, found, e := readDarkRestoreSystemProbeIntentTx(ctx, tx, plan)
		if e != nil || !found {
			return ErrUnavailable
		}
		request, requested, e := readDarkRestoreSystemProbeRequestTx(ctx, tx, intent)
		if e != nil {
			return e
		}
		evidence, e := readDarkRestoreServingHealthTx(ctx, tx, intent.ClientID)
		if e == nil {
			if !requested || evidence.Request != request {
				return ErrUnavailable
			}
		} else if !errors.Is(e, ErrNotFound) && !errors.Is(e, ErrDarkRestoreCutover) {
			return e
		}
	}
	return nil
}
