package business

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type executionBindings struct {
	ClientIDs []string `json:"client_ids"`
	Mode      string   `json:"mode"`
}
type executionHead struct {
	InboundID, Revision, DesiredRevision, ActiveRevision             uint64
	DesiredState, ObservedState, LastEvent, Pending, ActiveOperation string
	Bindings                                                         executionBindings
}
type executionSnapshot struct {
	Ref                ix.RevisionRef          `json:"ref"`
	Name               string                  `json:"name"`
	Profile            xrayconfig.Profile      `json:"profile"`
	Nodes              []NativeInboundPlanNode `json:"nodes"`
	Bindings           executionBindings       `json:"bindings"`
	EffectiveClientIDs []string                `json:"effective_client_ids"`
	Principals         []ix.Principal          `json:"principals"`
	State              string                  `json:"state"`
	Dormant            bool                    `json:"dormant"`
	ExpiresUnix        int64                   `json:"expires_unix"`
}
type executionOperationPayload struct {
	Input   ix.CommandInput  `json:"input"`
	ActorID string           `json:"actor_id"`
	Targets []ix.TargetState `json:"targets"`
}

func executionAAD(kind string, id any, rev uint64) string {
	return fmt.Sprintf("managed-inbound-execution-v1/%s/%v/%d", kind, id, rev)
}

func executionActor(ctx context.Context, q inboundReader, actor string) error {
	var n int
	if !idRE.MatchString(actor) {
		return ErrInvalid
	}
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users a JOIN representatives r ON r.id=a.representative_id WHERE a.id=? AND a.role='owner' AND a.disabled=0 AND r.kind='owner' AND r.enabled=1`, actor).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

func executionClientIDs(ids []string) ([]string, error) {
	if len(ids) > 20000 {
		return nil, ErrInvalid
	}
	out := append([]string{}, ids...)
	sort.Strings(out)
	for i, id := range out {
		if !idRE.MatchString(id) || i > 0 && id == out[i-1] {
			return nil, ErrInvalid
		}
	}
	return out, nil
}
func executionModes(p xrayconfig.Profile) []string {
	if p.Spec == nil {
		return []string{ix.AccountingPerClient}
	}
	switch p.Spec.Protocol {
	case "tun", "tunnel":
		return []string{ix.AccountingOwnerAggregate}
	case "mixed":
		if specString(p.Spec.Settings, "auth") == "noauth" {
			return []string{ix.AccountingOwnerAggregate}
		}
	}
	return []string{ix.AccountingPerClient, ix.AccountingOwnerAggregate}
}
func executionMode(p xrayconfig.Profile, mode string) (string, error) {
	m := executionModes(p)
	if mode == "" {
		return m[0], nil
	}
	for _, v := range m {
		if v == mode {
			return v, nil
		}
	}
	return "", ErrInvalid
}

func readExecutionHead(ctx context.Context, q inboundReader, key []byte, id uint64) (executionHead, error) {
	h := executionHead{InboundID: id, DesiredState: "stopped", ObservedState: "saved", Bindings: executionBindings{ClientIDs: []string{}}}
	var enc []byte
	err := q.QueryRowContext(ctx, `SELECT revision,desired_revision,active_revision,desired_state,observed_state,binding_enc,last_event_id,pending_operation,active_operation FROM inbound_execution_heads WHERE inbound_id=?`, id).Scan(&h.Revision, &h.DesiredRevision, &h.ActiveRevision, &h.DesiredState, &h.ObservedState, &enc, &h.LastEvent, &h.Pending, &h.ActiveOperation)
	if errors.Is(err, sql.ErrNoRows) {
		return h, nil
	}
	if err != nil {
		return h, err
	}
	if openPlanValue(key, executionAAD("bindings", id, h.Revision), enc, &h.Bindings) != nil {
		return h, ErrUnavailable
	}
	var latest uint64
	var event string
	if err = q.QueryRowContext(ctx, `SELECT revision,event_id FROM inbound_execution_events WHERE inbound_id=? ORDER BY revision DESC LIMIT 1`, id).Scan(&latest, &event); err != nil || latest != h.Revision || event != h.LastEvent {
		return h, ErrUnavailable
	}
	if h.Bindings.ClientIDs == nil {
		h.Bindings.ClientIDs = []string{}
	}
	return h, nil
}
func writeExecutionHead(ctx context.Context, tx *sql.Tx, key []byte, h executionHead, expected uint64) error {
	enc, err := sealPlanValue(key, executionAAD("bindings", h.InboundID, h.Revision), h.Bindings)
	if err != nil {
		return err
	}
	if expected == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_heads(inbound_id,revision,desired_revision,active_revision,desired_state,observed_state,binding_enc,last_event_id,pending_operation,active_operation) VALUES(?,?,?,?,?,?,?,?,?,?)`, h.InboundID, h.Revision, h.DesiredRevision, h.ActiveRevision, h.DesiredState, h.ObservedState, enc, h.LastEvent, h.Pending, h.ActiveOperation)
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE inbound_execution_heads SET revision=?,desired_revision=?,active_revision=?,desired_state=?,observed_state=?,binding_enc=?,last_event_id=?,pending_operation=?,active_operation=? WHERE inbound_id=? AND revision=?`, h.Revision, h.DesiredRevision, h.ActiveRevision, h.DesiredState, h.ObservedState, enc, h.LastEvent, h.Pending, h.ActiveOperation, h.InboundID, expected)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}

func readExecutionSnapshot(ctx context.Context, q inboundReader, key []byte, id, rev uint64) (executionSnapshot, error) {
	var s executionSnapshot
	var enc []byte
	if err := q.QueryRowContext(ctx, `SELECT payload_enc FROM inbound_execution_revisions WHERE inbound_id=? AND revision=?`, id, rev).Scan(&enc); err != nil {
		return s, err
	}
	if openPlanValue(key, executionAAD("revision", id, rev), enc, &s) != nil || s.Ref.InboundID != id || s.Ref.Revision != rev {
		return s, ErrUnavailable
	}
	return s, nil
}

func executionSnapshotFor(ctx context.Context, tx *sql.Tx, key []byte, plan NativeInboundPlan, h executionHead, ids []string, mode, state string, allowIneligible bool) (executionSnapshot, []ix.Blocker, error) {
	_, p, err := editablePlanProfile(ctx, tx, key, plan)
	if err != nil {
		return executionSnapshot{}, nil, err
	}
	if p.Spec == nil {
		return executionSnapshot{}, nil, planError("", "profile.spec", "managed_execution_requires_spec", ErrInvalid)
	}
	mode, err = executionMode(p, mode)
	if err != nil {
		return executionSnapshot{}, nil, err
	}
	s := executionSnapshot{Ref: ix.RevisionRef{InboundID: plan.Profile.ID, Revision: h.Revision + 1, PlanRevision: plan.PlanRevision, InboundRevision: plan.InboundRevision}, Name: plan.Profile.Name, Profile: p, Nodes: plan.Nodes, Bindings: executionBindings{ClientIDs: ids, Mode: mode}, State: state, EffectiveClientIDs: []string{}, Principals: []ix.Principal{}}
	return populateExecutionUsers(ctx, tx, s, allowIneligible)
}

func populateExecutionUsers(ctx context.Context, tx *sql.Tx, s executionSnapshot, allowIneligible bool) (executionSnapshot, []ix.Blocker, error) {
	p := s.Profile
	ids := s.Bindings.ClientIDs
	mode := s.Bindings.Mode
	state := s.State
	var err error
	s.EffectiveClientIDs = []string{}
	s.Principals = []ix.Principal{}
	s.ExpiresUnix = 0
	s.Dormant = false
	s.Profile.Users = []xrayconfig.User{}
	blockers := []ix.Blocker{}
	listenerOnly := false
	engine := "xray"
	for _, info := range inboundspec.Catalog() {
		if info.Protocol == p.Spec.Protocol {
			listenerOnly = info.ListenerOnly
			engine = info.Engine
		}
	}
	if p.Spec.Protocol == "mixed" && specString(p.Spec.Settings, "auth") == "noauth" {
		listenerOnly = true
	}
	if listenerOnly && len(ids) != 0 {
		return s, nil, ErrInvalid
	}
	for _, id := range ids {
		var uuid string
		var eligible int
		var expires int64
		err = tx.QueryRowContext(ctx, `SELECT c.uuid,c.expires_unix,e.eligible FROM clients c JOIN pro_usage_eligibility e ON e.client_id=c.id WHERE c.id=?`, id).Scan(&uuid, &expires, &eligible)
		if errors.Is(err, sql.ErrNoRows) {
			blockers = append(blockers, ix.Blocker{Code: "client_not_found", ClientID: id, Field: "client_ids"})
			continue
		}
		if err != nil {
			return s, nil, err
		}
		if eligible == 1 {
			stale, e := executionClientHasStaleEpoch(ctx, tx, id, time.Now().UnixNano())
			if e != nil {
				return s, nil, e
			}
			if stale {
				eligible = 0
			}
		}
		if eligible != 1 {
			blockers = append(blockers, ix.Blocker{Code: "client_ineligible", ClientID: id, Field: "client_ids"})
			continue
		}
		bindingID := "exec-" + ix.BytesSHA([]byte(fmt.Sprintf("%d\x00%s", s.Ref.InboundID, id)))[:32]
		email, e := uc.CounterEmail(bindingID)
		if e != nil {
			return s, nil, e
		}
		u := xrayconfig.User{UUID: uuid, Email: email}
		s.Profile.Users = append(s.Profile.Users, u)
		s.EffectiveClientIDs = append(s.EffectiveClientIDs, id)
		cred := inboundspec.User{UUID: uuid, Email: email}
		material := inboundspec.PasswordFor(*p.Spec, cred)
		if p.Spec.Protocol == "mtproto" {
			material = inboundspec.UserSecret(*p.Spec, cred)
		}
		if p.Spec.Protocol == "wireguard" || p.Spec.Protocol == "amneziawg" {
			_, material, err = inboundspec.DeriveWireGuardIdentity(*p.Spec, cred)
			if err != nil {
				return s, nil, err
			}
		}
		s.Principals = append(s.Principals, ix.Principal{ID: bindingID, Engine: engine, ClientID: id, Role: "customer", Counter: "user>>>" + email + ">>>traffic", CredentialSHA: ix.BytesSHA([]byte(material)), InboundID: s.Ref.InboundID, Billable: mode == ix.AccountingPerClient})
		if s.ExpiresUnix == 0 || expires < s.ExpiresUnix {
			s.ExpiresUnix = expires
		}
	}
	if state == "active" && !listenerOnly && len(s.Profile.Users) == 0 {
		blockers = append(blockers, ix.Blocker{Code: "eligible_client_required", Field: "client_ids"})
	}
	if allowIneligible && len(s.Profile.Users) == 0 && !listenerOnly {
		s.Dormant = true
	}
	return s, blockers, nil
}

func executionTopology(s executionSnapshot) (xrayconfig.CreationTopology, error) {
	if s.State != "active" || s.Dormant {
		return xrayconfig.CreationTopology{Nodes: []xrayconfig.CreationNodeCandidate{}}, nil
	}
	nodes := make([]xrayconfig.CreationNode, 0, len(s.Nodes))
	swaps := []xrayconfig.CreationSwap{}
	for _, n := range s.Nodes {
		nodes = append(nodes, xrayconfig.CreationNode{NodeID: n.NodeID, InstallationID: n.InstallationID, ListenIP: n.ListenIP, DirectPort: n.DirectPort, TunnelPort: n.TunnelPort})
		if n.SwapEnabled {
			swaps = append(swaps, xrayconfig.CreationSwap{Name: "swap-" + n.NodeID, SourceNodeID: n.SwapSourceNodeID, SourceInstallationID: n.SwapSourceInstallationID, SourceListenIP: n.SwapSourceListenIP, SourcePort: n.SwapSourcePort, DestinationNodeID: n.NodeID, DestinationPort: n.SwapDestinationPort})
		}
	}
	return xrayconfig.RenderCreationTopology(s.Profile, nodes, swaps)
}
func executionSnapshotNodes(s executionSnapshot) map[string]string {
	out := map[string]string{}
	for _, n := range s.Nodes {
		out[n.NodeID] = n.InstallationID
		if n.SwapEnabled {
			out[n.SwapSourceNodeID] = n.SwapSourceInstallationID
		}
	}
	return out
}

func executionTarget(b ix.Bundle) ix.TargetState {
	return ix.TargetState{NodeID: b.NodeID, InstallationID: b.InstallationID, DesiredGeneration: b.Generation, State: "queued", ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, Listeners: append([]ix.Listener{}, b.Listeners...), Blockers: []ix.Blocker{}, HealthState: "unknown", AccountingState: "pending"}
}

// executionBundles compiles complete desired state of every affected node.
// Applied siblings use their immutable deployed snapshot, not an unsent draft.
func executionBundles(ctx context.Context, tx *sql.Tx, key []byte, next executionSnapshot, op string) ([]ix.Bundle, error) {
	return executionBundlesWithOverrides(ctx, tx, key, next, op, nil)
}
func executionBundlesWithOverrides(ctx context.Context, tx *sql.Tx, key []byte, next executionSnapshot, op string, overrides map[uint64]executionSnapshot) ([]ix.Bundle, error) {
	affected := executionSnapshotNodes(next)
	for _, snapshot := range overrides {
		for node, install := range executionSnapshotNodes(snapshot) {
			if prior, ok := affected[node]; ok && prior != install {
				return nil, ErrConflict
			}
			affected[node] = install
		}
	}
	items := []ix.CompileItem{}
	var oldRev uint64
	_ = tx.QueryRowContext(ctx, `SELECT desired_revision FROM inbound_execution_heads WHERE inbound_id=?`, next.Ref.InboundID).Scan(&oldRev)
	if oldRev > 0 {
		old, err := readExecutionSnapshot(ctx, tx, key, next.Ref.InboundID, oldRev)
		if err != nil {
			return nil, err
		}
		for node, install := range executionSnapshotNodes(old) {
			if v, ok := affected[node]; ok && v != install {
				return nil, ErrConflict
			}
			affected[node] = install
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT inbound_id,desired_revision,pending_operation FROM inbound_execution_heads WHERE desired_state='active' AND desired_revision>0 AND inbound_id!=? ORDER BY inbound_id`, next.Ref.InboundID)
	if err != nil {
		return nil, err
	}
	type sibling struct {
		id, rev uint64
		pending string
	}
	siblings := []sibling{}
	for rows.Next() {
		var r sibling
		if err = rows.Scan(&r.id, &r.rev, &r.pending); err != nil {
			rows.Close()
			return nil, err
		}
		siblings = append(siblings, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, r := range siblings {
		s, err := readExecutionSnapshot(ctx, tx, key, r.id, r.rev)
		if err != nil {
			return nil, err
		}
		overlap := false
		for n := range executionSnapshotNodes(s) {
			if _, ok := affected[n]; ok {
				overlap = true
			}
		}
		if !overlap {
			continue
		}
		if replacement, ok := overrides[r.id]; ok {
			s = replacement
		} else if r.pending != "" {
			return nil, ErrConflict
		}
		top, err := executionTopology(s)
		if err != nil {
			return nil, err
		}
		items = append(items, ix.CompileItem{Ref: s.Ref, Profile: s.Profile, Topology: top, AccountingMode: s.Bindings.Mode, Principals: s.Principals})
	}
	top, err := executionTopology(next)
	if err != nil {
		return nil, err
	}
	if next.State == "active" {
		items = append(items, ix.CompileItem{Ref: next.Ref, Profile: next.Profile, Topology: top, AccountingMode: next.Bindings.Mode, Principals: next.Principals})
	}
	nodes := make([]string, 0, len(affected))
	for node := range affected {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	out := []ix.Bundle{}
	for _, node := range nodes {
		var generation uint64
		var install string
		err = tx.QueryRowContext(ctx, `SELECT installation_id,desired_generation FROM inbound_execution_nodes WHERE node_id=?`, node).Scan(&install, &generation)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if install != "" && install != affected[node] {
			return nil, ErrConflict
		}
		if generation >= routeMax-1 {
			return nil, ErrConflict
		}
		b, err := ix.Compile(ctx, ix.CompileRequest{NodeID: node, InstallationID: affected[node], Generation: generation + 1, OperationID: op, Items: items})
		if err != nil {
			return nil, err
		}
		b, err = ix.SealBundle(key, b)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func executionTx(s *Store, ctx context.Context, key []byte) (*sql.Tx, error) {
	if len(key) != 32 || s.usage == nil {
		return nil, ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	enabled, err := executionSchemaPresent(ctx, tx)
	if err != nil || !enabled {
		tx.Rollback()
		return nil, ErrUnavailable
	}
	return tx, nil
}

func readExecutionBundle(ctx context.Context, q inboundReader, key []byte, node, install string, generation uint64) (ix.Bundle, error) {
	var b ix.Bundle
	var enc []byte
	var config, manifest, storedInstall string
	err := q.QueryRowContext(ctx, `SELECT installation_id,config_sha,manifest_sha,payload_enc FROM inbound_execution_bundles WHERE node_id=? AND generation=?`, node, generation).Scan(&storedInstall, &config, &manifest, &enc)
	if err != nil {
		return b, err
	}
	if storedInstall != install || openPlanValue(key, executionAAD("bundle", node+"/"+install, generation), enc, &b) != nil || b.NodeID != node || b.InstallationID != install || b.Generation != generation || b.ConfigSHA != config || b.ManifestSHA != manifest || ix.VerifyBundle(key, b) != nil {
		return b, ErrUnavailable
	}
	return b, nil
}

func (s *Store) ListInboundExecutionNodes(ctx context.Context) ([]ix.TargetState, error) {
	out := []ix.TargetState{}
	enabled, err := executionSchemaPresent(ctx, s.db)
	if err != nil || !enabled {
		return out, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT node_id,installation_id,desired_generation,observed_generation FROM inbound_execution_nodes ORDER BY node_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var t ix.TargetState
		if err = rows.Scan(&t.NodeID, &t.InstallationID, &t.DesiredGeneration, &t.ObservedGeneration); err != nil {
			return nil, err
		}
		t.Listeners = []ix.Listener{}
		t.Blockers = []ix.Blocker{}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) InboundExecutionBundle(ctx context.Context, key []byte, node, install string) (ix.Bundle, bool, error) {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.Bundle{}, false, err
	}
	defer tx.Rollback()
	var generation uint64
	var stored string
	err = tx.QueryRowContext(ctx, `SELECT installation_id,desired_generation FROM inbound_execution_nodes WHERE node_id=?`, node).Scan(&stored, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return ix.Bundle{}, false, nil
	}
	if err != nil {
		return ix.Bundle{}, false, err
	}
	if stored != install {
		return ix.Bundle{}, false, ErrConflict
	}
	b, err := readExecutionBundle(ctx, tx, key, node, install, generation)
	if err != nil {
		return b, false, err
	}
	return b, true, tx.Commit()
}

func (s *Store) InboundExecution(ctx context.Context, key []byte, id uint64) (ix.State, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ix.State{}, err
	}
	defer tx.Rollback()
	v, err := inboundExecutionState(ctx, tx, key, id)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func (s *Store) ListInboundExecutionStates(ctx context.Context, key []byte) ([]ix.State, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT inbound_id FROM native_inbound_plans ORDER BY inbound_id LIMIT 257`)
	if err != nil {
		return nil, err
	}
	ids := []uint64{}
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 256 {
		return nil, ErrInvalid
	}
	out := []ix.State{}
	for _, id := range ids {
		v, e := inboundExecutionState(ctx, tx, key, id)
		if e != nil {
			if errors.Is(e, ErrConflict) {
				continue
			}
			return nil, e
		}
		out = append(out, v)
	}
	return out, tx.Commit()
}

func inboundExecutionState(ctx context.Context, tx *sql.Tx, key []byte, id uint64) (ix.State, error) {
	v := ix.State{Version: ix.Version, InboundID: id, DesiredState: "stopped", ObservedState: "saved", ClientIDs: []string{}, EffectiveClientIDs: []string{}, Targets: []ix.TargetState{}, AllowedActions: []string{}, Blockers: []ix.Blocker{}, AllowedAccountingModes: []string{}, Reconciliations: []ix.ReconciliationState{}, ServerTimeMillis: time.Now().UnixMilli(), PublicationState: "withheld"}
	p, err := readNativeInboundPlan(ctx, tx, key, id)
	if err != nil {
		return v, err
	}
	v.PlanRevision, v.InboundRevision = p.PlanRevision, p.InboundRevision
	_, private, err := editablePlanProfile(ctx, tx, key, p)
	if err != nil {
		return v, err
	}
	v.AllowedAccountingModes = executionModes(private)
	v.AccountingMode = v.AllowedAccountingModes[0]
	enabled, err := executionSchemaPresent(ctx, tx)
	if err != nil {
		return v, err
	}
	if !enabled {
		v.Blockers = append(v.Blockers, ix.Blocker{Code: "execution_disabled"})
		return v, nil
	}
	v.Enabled = true
	v.Reconciliations, err = listExecutionReconciliations(ctx, tx, key, id)
	if err != nil {
		return v, err
	}
	h, err := readExecutionHead(ctx, tx, key, id)
	if err != nil {
		return v, err
	}
	v.Revision, v.DesiredRevision, v.ActiveRevision = h.Revision, h.DesiredRevision, h.ActiveRevision
	v.DesiredState, v.ObservedState = h.DesiredState, h.ObservedState
	v.ClientIDs = h.Bindings.ClientIDs
	if h.Bindings.Mode != "" {
		v.AccountingMode = h.Bindings.Mode
	}
	if h.ActiveRevision > 0 {
		s, e := readExecutionSnapshot(ctx, tx, key, id, h.ActiveRevision)
		if e != nil {
			return v, e
		}
		v.EffectiveClientIDs = append([]string{}, s.EffectiveClientIDs...)
		v.ActivePlanRevision, v.ActiveInboundRevision = s.Ref.PlanRevision, s.Ref.InboundRevision
		v.PendingChanges = s.Ref.PlanRevision != p.PlanRevision || s.Ref.InboundRevision != p.InboundRevision || ix.Digest(s.Bindings) != ix.Digest(h.Bindings)
	}
	for _, client := range v.ClientIDs {
		var eligible int
		if err = tx.QueryRowContext(ctx, `SELECT eligible FROM pro_usage_eligibility WHERE client_id=?`, client).Scan(&eligible); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return v, err
		}
		if eligible != 1 {
			v.Blockers = append(v.Blockers, ix.Blocker{Code: "client_ineligible", Field: "client_ids", ClientID: client})
		}
	}
	if private.Spec == nil {
		v.Blockers = append(v.Blockers, ix.Blocker{Code: "managed_execution_requires_spec", Field: "profile.spec"})
		return v, nil
	}
	if h.Pending != "" {
		v.AllowedActions = []string{"stop"}
		op, e := readExecutionOperation(ctx, tx, key, id, h.Pending)
		if e != nil {
			return v, e
		}
		v.LatestOperation = &op
		v.Targets = op.Targets
	} else if h.DesiredState == "archived" {
		v.AllowedActions = []string{}
	} else if h.DesiredState == "active" {
		v.AllowedActions = []string{"apply", "stop", "restart", "archive"}
	} else {
		v.AllowedActions = []string{"activate", "archive"}
	}
	if h.Pending == "" && h.DesiredRevision > 0 {
		var event string
		if err = tx.QueryRowContext(ctx, `SELECT event_id FROM inbound_execution_revisions WHERE inbound_id=? AND revision=?`, id, h.DesiredRevision).Scan(&event); err != nil {
			return v, err
		}
		op, e := readExecutionOperation(ctx, tx, key, id, event)
		if e != nil {
			return v, e
		}
		v.LatestOperation = &op
		v.Targets = op.Targets
	}
	if h.DesiredRevision > 0 {
		snapshot, e := readExecutionSnapshot(ctx, tx, key, id, h.DesiredRevision)
		if e != nil {
			return v, e
		}
		v.Targets, e = projectExecutionTargets(ctx, tx, key, snapshot)
		if e != nil {
			return v, e
		}
	}
	if len(v.Targets) == 0 {
		for _, n := range p.Nodes {
			v.Targets = append(v.Targets, ix.TargetState{NodeID: n.NodeID, InstallationID: n.InstallationID, State: "saved", Listeners: []ix.Listener{}, Blockers: []ix.Blocker{}, HealthState: "unknown", AccountingState: "pending"})
		}
	}
	for i := range v.Targets {
		t := &v.Targets[i]
		required, err := executionResumeRequired(ctx, tx, t.NodeID, t.InstallationID)
		if err != nil {
			return v, err
		}
		if required {
			v.OwnerResumeRequired = true
			t.State, t.ProcessReady = "blocked", false
			blocker := ix.Blocker{Code: "owner_resume_required", NodeID: t.NodeID, InboundID: id}
			t.Blockers = append(t.Blockers, blocker)
			v.Blockers = append(v.Blockers, blocker)
		}
	}
	if v.OwnerResumeRequired {
		v.ObservedState = "blocked"
		if h.Pending != "" && h.DesiredState == "active" {
			v.AllowedActions = []string{"apply", "restart", "stop"}
		} else if h.Pending != "" && h.DesiredState == "stopped" {
			v.AllowedActions = []string{"activate", "stop"}
		}
	}
	if h.ActiveRevision > 0 && h.ActiveRevision == h.DesiredRevision && h.DesiredState == "active" {
		ready, verified := len(v.Targets) > 0, true
		for _, target := range v.Targets {
			if !target.ProcessReady || target.ObservedGeneration != target.DesiredGeneration || target.LeaseExpires <= time.Now().UnixNano() || target.ReportedAt <= 0 || time.Now().UnixNano()-target.ReportedAt > int64(15*time.Second) || target.AccountingState != "accepted" {
				ready = false
			}
			verified = verified && target.HealthState == "healthy"
		}
		if ready {
			v.PublicationState = "available_unverified_path"
			if verified {
				v.PublicationState = "available_verified"
			}
		}
	}
	if private.Spec != nil && (private.Spec.Protocol == "tun" || private.Spec.Protocol == "tunnel" || private.Spec.Protocol == "mixed" && specString(private.Spec.Settings, "auth") == "noauth") {
		v.PublicationState = "not_applicable"
	}
	return v, nil
}

func projectExecutionTargets(ctx context.Context, tx *sql.Tx, key []byte, snapshot executionSnapshot) ([]ix.TargetState, error) {
	nodes := executionSnapshotNodes(snapshot)
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := []ix.TargetState{}
	for _, node := range ids {
		var desired, observed uint64
		var raw []byte
		if err := tx.QueryRowContext(ctx, `SELECT desired_generation,observed_generation,observation_enc FROM inbound_execution_nodes WHERE node_id=? AND installation_id=?`, node, nodes[node]).Scan(&desired, &observed, &raw); err != nil {
			return nil, err
		}
		b, err := readExecutionBundle(ctx, tx, key, node, nodes[node], desired)
		if err != nil {
			return nil, err
		}
		t := executionTarget(b)
		t.ObservedGeneration = observed
		t.Listeners = []ix.Listener{}
		for _, l := range b.Listeners {
			if l.InboundID == snapshot.Ref.InboundID {
				t.Listeners = append(t.Listeners, l)
			}
		}
		if len(raw) > 0 {
			var o ix.Observation
			if openPlanValue(key, executionAAD("observation", node+"/"+nodes[node], observed), raw, &o) != nil {
				return nil, ErrUnavailable
			}
			if o.Generation == desired && o.ManifestSHA == b.ManifestSHA {
				t.State = o.State
				t.ReportedAt = o.ObservedUnixNano
				t.LeaseExpires = o.LeaseExpires
				t.ProcessReady = o.ProcessReady
				t.HealthState = o.HealthState
				t.AccountingState = o.AccountingState
				if t.ProcessReady {
					if _, e := executionAccountingFreshUntil(ctx, tx, node, nodes[node], desired, o.EpochID, time.Now().UnixNano()); e != nil {
						if !errors.Is(e, ErrConflict) {
							return nil, e
						}
						t.AccountingState = "stale"
						t.Blockers = append(t.Blockers, ix.Blocker{Code: "accounting_stale", NodeID: node})
					}
				}
				if o.ErrorCode != "" {
					t.Blockers = append(t.Blockers, ix.Blocker{Code: o.ErrorCode, NodeID: node})
				}
				if o.ProcessReady && o.LeaseExpires <= time.Now().UnixNano() {
					t.State = "blocked"
					t.ProcessReady = false
					t.Blockers = append(t.Blockers, ix.Blocker{Code: "lease_expired", NodeID: node})
				}
			}
		}
		out = append(out, t)
	}
	return out, nil
}

func readExecutionOperation(ctx context.Context, tx *sql.Tx, key []byte, id uint64, op string) (ix.Operation, error) {
	v := ix.Operation{Targets: []ix.TargetState{}, Blockers: []ix.Blocker{}}
	var enc []byte
	err := tx.QueryRowContext(ctx, `SELECT id,inbound_id,revision,action,state,created,updated,payload_enc FROM inbound_execution_operations WHERE id=? AND inbound_id=?`, op, id).Scan(&v.ID, &v.InboundID, &v.Revision, &v.Action, &v.State, &v.CreatedUnixNano, &v.UpdatedUnixNano, &enc)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	v.EventID = v.ID
	var payload executionOperationPayload
	if openPlanValue(key, executionAAD("operation", v.ID, v.Revision), enc, &payload) != nil {
		return v, ErrUnavailable
	}
	v.Targets = payload.Targets
	for i := range v.Targets {
		t := &v.Targets[i]
		var generation uint64
		var obs []byte
		if err = tx.QueryRowContext(ctx, `SELECT observed_generation,observation_enc FROM inbound_execution_nodes WHERE node_id=? AND installation_id=?`, t.NodeID, t.InstallationID).Scan(&generation, &obs); err != nil {
			return v, err
		}
		t.ObservedGeneration = generation
		if len(obs) == 0 {
			continue
		}
		var o ix.Observation
		if openPlanValue(key, executionAAD("observation", t.NodeID+"/"+t.InstallationID, generation), obs, &o) != nil {
			return v, ErrUnavailable
		}
		if o.Generation == t.DesiredGeneration && o.ManifestSHA == t.ManifestSHA {
			t.State = o.State
			t.ReportedAt = o.ObservedUnixNano
			t.LeaseExpires = o.LeaseExpires
			t.ProcessReady = o.ProcessReady
			t.HealthState = o.HealthState
			t.AccountingState = o.AccountingState
			if o.ErrorCode != "" {
				t.Blockers = append(t.Blockers, ix.Blocker{Code: o.ErrorCode, NodeID: t.NodeID})
			}
			if o.ProcessReady && o.LeaseExpires <= time.Now().UnixNano() {
				t.State = "blocked"
				t.ProcessReady = false
				t.Blockers = append(t.Blockers, ix.Blocker{Code: "lease_expired", NodeID: t.NodeID})
			}
		}
	}
	return v, nil
}
func (s *Store) InboundExecutionOperation(ctx context.Context, key []byte, id uint64, op string) (ix.Operation, error) {
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.Operation{}, err
	}
	defer tx.Rollback()
	v, err := readExecutionOperation(ctx, tx, key, id, op)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func executionEventAvailable(ctx context.Context, tx *sql.Tx, event string) error {
	for _, table := range []string{"events", "native_inbound_operations", "native_inbound_plan_events", "native_inbound_plan_promotions", "inbound_execution_events", "inbound_execution_subscription_events"} {
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE event_id=?`, event).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return ErrConflict
		}
	}
	return nil
}

func (s *Store) BindInboundExecutionClients(ctx context.Context, key []byte, actor string, id uint64, in ix.BindingsInput) (ix.State, error) {
	if !eventRE.MatchString(in.EventID) || !validName(in.Reason, 512) {
		return ix.State{}, ErrInvalid
	}
	ids, err := executionClientIDs(in.ClientIDs)
	if err != nil {
		return ix.State{}, err
	}
	in.ClientIDs = ids
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.State{}, err
	}
	defer tx.Rollback()
	if err = executionActor(ctx, tx, actor); err != nil {
		return ix.State{}, err
	}
	fp := fingerprint("inbound-execution-bindings-v1", struct {
		Actor   string
		Inbound uint64
		Input   ix.BindingsInput
	}{actor, id, in})
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint FROM inbound_execution_events WHERE event_id=? AND inbound_id=?`, in.EventID, id).Scan(&prior)
	if err == nil {
		if prior != fp {
			return ix.State{}, ErrConflict
		}
		v, e := inboundExecutionState(ctx, tx, key, id)
		if e != nil {
			return v, e
		}
		return v, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ix.State{}, err
	}
	if err = executionEventAvailable(ctx, tx, in.EventID); err != nil {
		return ix.State{}, err
	}
	h, err := readExecutionHead(ctx, tx, key, id)
	if err != nil {
		return ix.State{}, err
	}
	if h.Revision != in.ExpectedRevision || h.Pending != "" || h.DesiredState == "archived" {
		return ix.State{}, ErrConflict
	}
	plan, err := readNativeInboundPlan(ctx, tx, key, id)
	if err != nil {
		return ix.State{}, err
	}
	snap, blockers, err := executionSnapshotFor(ctx, tx, key, plan, h, ids, in.AccountingMode, "stopped", false)
	if err != nil {
		return ix.State{}, err
	}
	if len(blockers) > 0 {
		return ix.State{}, ErrConflict
	}
	enc, err := sealPlanValue(key, executionAAD("event", in.EventID, h.Revision+1), in)
	if err != nil {
		return ix.State{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_events(event_id,inbound_id,revision,kind,actor_id,fingerprint,payload_enc,created) VALUES(?,?,?,'bindings',?,?,?,?)`, in.EventID, id, h.Revision+1, actor, fp, enc, time.Now().UnixNano()); err != nil {
		return ix.State{}, err
	}
	old := h.Revision
	h.Revision++
	h.LastEvent = in.EventID
	h.Bindings = snap.Bindings
	if err = writeExecutionHead(ctx, tx, key, h, old); err != nil {
		return ix.State{}, err
	}
	v, err := inboundExecutionState(ctx, tx, key, id)
	if err != nil {
		return v, err
	}
	return v, tx.Commit()
}

func normalizeExecutionCommand(in ix.CommandInput) (ix.CommandInput, error) {
	if !eventRE.MatchString(in.EventID) || !validName(in.Reason, 512) || in.ExpectedRevision >= routeMax-1 {
		return in, ErrInvalid
	}
	switch in.Action {
	case "activate", "apply", "stop", "restart", "archive":
	default:
		return in, ErrInvalid
	}
	var err error
	in.ClientIDs, err = executionClientIDs(in.ClientIDs)
	return in, err
}

type executionAssessment struct {
	preview  ix.Preview
	head     executionHead
	snapshot executionSnapshot
	bundles  []ix.Bundle
}

func assessInboundExecution(ctx context.Context, tx *sql.Tx, key []byte, actor string, id uint64, in ix.CommandInput) (executionAssessment, error) {
	a := executionAssessment{preview: ix.Preview{Action: in.Action, Targets: []ix.TargetState{}, Blockers: []ix.Blocker{}}}
	if err := executionActor(ctx, tx, actor); err != nil {
		return a, err
	}
	state, err := inboundExecutionState(ctx, tx, key, id)
	if err != nil {
		return a, err
	}
	a.preview.State = state
	h, err := readExecutionHead(ctx, tx, key, id)
	if err != nil {
		return a, err
	}
	a.head = h
	if h.Revision != in.ExpectedRevision || state.PlanRevision != in.ExpectedPlanRevision || state.InboundRevision != in.ExpectedInboundRevision {
		return a, ErrConflict
	}
	resumeAction := state.OwnerResumeRequired && (in.Action == "activate" || in.Action == "apply" || in.Action == "restart")
	if h.DesiredState == "archived" || h.Pending != "" && in.Action != "stop" && !resumeAction {
		return a, ErrConflict
	}
	if in.Action == "activate" && h.DesiredState == "active" || (in.Action == "apply" || in.Action == "restart") && h.DesiredState != "active" {
		return a, ErrConflict
	}
	plan, err := readNativeInboundPlan(ctx, tx, key, id)
	if err != nil {
		return a, err
	}
	desired := "active"
	if in.Action == "stop" {
		desired = "stopped"
	}
	if in.Action == "archive" {
		desired = "archived"
	}
	ids := in.ClientIDs
	mode := in.AccountingMode
	if mode == "" {
		mode = h.Bindings.Mode
	}
	if in.Action == "stop" || in.Action == "archive" || in.Action == "restart" {
		ids = h.Bindings.ClientIDs
		mode = h.Bindings.Mode
	}
	var snap executionSnapshot
	blockers := []ix.Blocker{}
	if desired != "active" && h.DesiredRevision > 0 {
		// Stopping a deployment operates on its reviewed snapshot. An unsent
		// authoring edit may select unrelated nodes or a different protocol;
		// it must neither claim those nodes nor prevent the old process stopping.
		snap, err = readExecutionSnapshot(ctx, tx, key, id, h.DesiredRevision)
		if err != nil {
			return a, err
		}
		snap.Ref.Revision = h.Revision + 1
		snap.State = desired
		snap.Dormant = false
		snap.Bindings = h.Bindings
		snap.EffectiveClientIDs = []string{}
		snap.Principals = []ix.Principal{}
		snap.Profile.Users = nil
	} else {
		snap, blockers, err = executionSnapshotFor(ctx, tx, key, plan, h, ids, mode, desired, false)
		if err != nil {
			return a, err
		}
		if desired != "active" {
			blockers = []ix.Blocker{}
		}
	}
	a.snapshot = snap
	a.preview.Blockers = blockers
	if len(blockers) > 0 {
		return a, nil
	}
	if in.Action == "restart" && h.DesiredRevision > 0 {
		old, e := readExecutionSnapshot(ctx, tx, key, id, h.DesiredRevision)
		if e != nil {
			return a, e
		}
		if old.Ref.PlanRevision != plan.PlanRevision || old.Ref.InboundRevision != plan.InboundRevision {
			return a, ErrConflict
		}
	}
	bundles, err := executionBundles(ctx, tx, key, snap, in.EventID)
	if err != nil {
		var field *inboundspec.FieldError
		switch {
		case errors.As(err, &field):
			a.preview.Blockers = append(a.preview.Blockers, ix.Blocker{Code: field.Code, Field: field.Path, InboundID: id})
		case errors.Is(err, ix.ErrConflict):
			a.preview.Blockers = append(a.preview.Blockers, ix.Blocker{Code: "execution_resource_conflict", InboundID: id})
		case errors.Is(err, ix.ErrInvalid):
			a.preview.Blockers = append(a.preview.Blockers, ix.Blocker{Code: "execution_configuration_invalid", InboundID: id})
		default:
			return a, err
		}
		return a, nil
	}
	a.bundles = bundles
	for _, b := range bundles {
		a.preview.Targets = append(a.preview.Targets, executionTarget(b))
	}
	review := in
	review.ReviewSHA = ""
	a.preview.ReviewSHA = fingerprint("managed-inbound-execution-review-v1", struct {
		Actor    string
		Inbound  uint64
		Input    ix.CommandInput
		Snapshot executionSnapshot
		Bundles  []ix.Bundle
	}{actor, id, review, snap, bundles})
	a.preview.Valid = true
	return a, nil
}
func (s *Store) PreviewInboundExecution(ctx context.Context, key []byte, actor string, id uint64, input ix.CommandInput) (ix.Preview, error) {
	in, err := normalizeExecutionCommand(input)
	if err != nil {
		return ix.Preview{}, err
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.Preview{}, err
	}
	defer tx.Rollback()
	a, err := assessInboundExecution(ctx, tx, key, actor, id, in)
	if err != nil {
		return a.preview, err
	}
	return a.preview, tx.Commit()
}

func (s *Store) QueueInboundExecution(ctx context.Context, key []byte, actor string, id uint64, input ix.CommandInput) (ix.Operation, error) {
	in, err := normalizeExecutionCommand(input)
	if err != nil || !routeSHA.MatchString(in.ReviewSHA) {
		return ix.Operation{}, ErrInvalid
	}
	tx, err := executionTx(s, ctx, key)
	if err != nil {
		return ix.Operation{}, err
	}
	defer tx.Rollback()
	if err = executionActor(ctx, tx, actor); err != nil {
		return ix.Operation{}, err
	}
	fp := fingerprint("managed-inbound-execution-command-v1", struct {
		Actor   string
		Inbound uint64
		Input   ix.CommandInput
	}{actor, id, in})
	var prior string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint FROM inbound_execution_events WHERE event_id=?`, in.EventID).Scan(&prior)
	if err == nil {
		if prior != fp {
			return ix.Operation{}, ErrConflict
		}
		v, e := readExecutionOperation(ctx, tx, key, id, in.EventID)
		if e != nil {
			return v, e
		}
		v.Replayed = true
		return v, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ix.Operation{}, err
	}
	if err = executionEventAvailable(ctx, tx, in.EventID); err != nil {
		return ix.Operation{}, err
	}
	a, err := assessInboundExecution(ctx, tx, key, actor, id, in)
	if err != nil {
		return ix.Operation{}, err
	}
	if !a.preview.Valid || a.preview.ReviewSHA != in.ReviewSHA {
		return ix.Operation{}, ErrConflict
	}
	v, err := commitExecutionAssessment(ctx, tx, key, actor, id, in, a, fp)
	if err != nil {
		return v, err
	}
	if in.Action == "activate" || in.Action == "apply" || in.Action == "restart" {
		if err = resolveExecutionResume(ctx, tx, in.EventID, a.bundles); err != nil {
			return ix.Operation{}, err
		}
	}
	return v, tx.Commit()
}

func commitExecutionAssessment(ctx context.Context, tx *sql.Tx, key []byte, actor string, id uint64, in ix.CommandInput, a executionAssessment, fp string) (ix.Operation, error) {
	now := time.Now().UnixNano()
	h := a.head
	revision := h.Revision + 1
	enc, err := sealPlanValue(key, executionAAD("event", in.EventID, revision), in)
	if err != nil {
		return ix.Operation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_events(event_id,inbound_id,revision,kind,actor_id,fingerprint,payload_enc,created) VALUES(?,?,?,'command',?,?,?,?)`, in.EventID, id, revision, actor, fp, enc, now); err != nil {
		return ix.Operation{}, err
	}
	enc, err = sealPlanValue(key, executionAAD("revision", id, revision), a.snapshot)
	if err != nil {
		return ix.Operation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_revisions(inbound_id,revision,event_id,payload_enc) VALUES(?,?,?,?)`, id, revision, in.EventID, enc); err != nil {
		return ix.Operation{}, err
	}
	payload := executionOperationPayload{Input: in, ActorID: actor, Targets: a.preview.Targets}
	enc, err = sealPlanValue(key, executionAAD("operation", in.EventID, revision), payload)
	if err != nil {
		return ix.Operation{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_operations(id,inbound_id,revision,action,state,created,updated,payload_enc) VALUES(?,?,?,?,'queued',?,?,?)`, in.EventID, id, revision, in.Action, now, now, enc); err != nil {
		return ix.Operation{}, err
	}
	for _, b := range a.bundles {
		if err = executionNodeClaim(ctx, tx, b.NodeID, b.InstallationID); err != nil {
			return ix.Operation{}, err
		}
		existing, e := readExecutionBundle(ctx, tx, key, b.NodeID, b.InstallationID, b.Generation)
		if e == nil {
			if ix.Digest(existing) != ix.Digest(b) {
				return ix.Operation{}, ErrConflict
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_operation_nodes(operation_id,node_id,installation_id,generation) VALUES(?,?,?,?)`, in.EventID, b.NodeID, b.InstallationID, b.Generation); err != nil {
				return ix.Operation{}, err
			}
			continue
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return ix.Operation{}, e
		}
		enc, err = sealPlanValue(key, executionAAD("bundle", b.NodeID+"/"+b.InstallationID, b.Generation), b)
		if err != nil {
			return ix.Operation{}, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_bundles(node_id,installation_id,generation,operation_id,config_sha,manifest_sha,payload_enc) VALUES(?,?,?,?,?,?,?)`, b.NodeID, b.InstallationID, b.Generation, b.OperationID, b.ConfigSHA, b.ManifestSHA, enc); err != nil {
			return ix.Operation{}, err
		}
		res, e := tx.ExecContext(ctx, `UPDATE inbound_execution_nodes SET desired_generation=? WHERE node_id=? AND installation_id=? AND desired_generation=?`, b.Generation, b.NodeID, b.InstallationID, b.Generation-1)
		if e != nil {
			return ix.Operation{}, e
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return ix.Operation{}, ErrConflict
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO inbound_execution_operation_nodes(operation_id,node_id,installation_id,generation) VALUES(?,?,?,?)`, in.EventID, b.NodeID, b.InstallationID, b.Generation); err != nil {
			return ix.Operation{}, err
		}
	}
	if h.Pending != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE inbound_execution_operations SET state='superseded',updated=? WHERE id=?`, now, h.Pending); err != nil {
			return ix.Operation{}, err
		}
	}
	old := h.Revision
	h.Revision = revision
	h.DesiredRevision = revision
	h.DesiredState = a.snapshot.State
	h.ObservedState = "queued"
	h.Bindings = a.snapshot.Bindings
	h.LastEvent = in.EventID
	h.Pending = in.EventID
	if err = writeExecutionHead(ctx, tx, key, h, old); err != nil {
		return ix.Operation{}, err
	}
	v, err := readExecutionOperation(ctx, tx, key, id, in.EventID)
	if err != nil {
		return v, err
	}
	return v, nil
}

// Draft saves remain possible while a managed revision is active. A pending
// reviewed operation and an archived managed identity fence concurrent edits.
func rejectManagedPlanEdit(ctx context.Context, tx *sql.Tx, id uint64) error {
	enabled, err := executionSchemaPresent(ctx, tx)
	if err != nil || !enabled {
		return err
	}
	var pending, state string
	err = tx.QueryRowContext(ctx, `SELECT pending_operation,desired_state FROM inbound_execution_heads WHERE inbound_id=?`, id).Scan(&pending, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if pending != "" || state == "archived" {
		return planError("", "profile", "managed_execution_busy_or_archived", ErrConflict)
	}
	return nil
}

func rejectManagedLegacyInboundMutation(ctx context.Context, tx *sql.Tx, id uint64) error {
	enabled, err := executionSchemaPresent(ctx, tx)
	if err != nil || !enabled {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM inbound_execution_heads WHERE inbound_id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return planError("", "profile", "managed_execution_lifecycle_required", ErrConflict)
	}
	return nil
}
