package business

// Creation plans are an authoring boundary. They deliberately do not create
// native targets, client bindings, runtime deliveries or billing records.
import (
	"context"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

const MaxNativeInboundPlanNodes = 64

// The ordinary Tunnel listener and the Swap source relay are separate roles.
// EntryAddress/EntryPort describe a manually configured external forward; they
// are never inferred from either node's bind address and are not health proof.
type NativeInboundPlanNode struct {
	NodeID                   string   `json:"node_id"`
	InstallationID           string   `json:"installation_id"`
	ListenIP                 string   `json:"listen_ip"`
	DirectPort               uint16   `json:"direct_port"`
	DirectHosts              []string `json:"direct_hosts"`
	TunnelPort               uint16   `json:"tunnel_port"`
	TunnelHosts              []string `json:"tunnel_hosts"`
	SwapEnabled              bool     `json:"swap_enabled"`
	SwapSourceNodeID         string   `json:"swap_source_node_id"`
	SwapSourceInstallationID string   `json:"swap_source_installation_id"`
	SwapSourceListenIP       string   `json:"swap_source_listen_ip"`
	SwapSourcePort           uint16   `json:"swap_source_port"`
	SwapEntryAddress         string   `json:"swap_entry_address"`
	SwapEntryPort            uint16   `json:"swap_entry_port"`
	SwapDestinationPort      uint16   `json:"swap_destination_port"`
}

type NativeInboundPlanInput struct {
	EventID                 string                  `json:"event_id"`
	ExpectedPlanRevision    uint64                  `json:"expected_plan_revision"`
	ExpectedInboundRevision uint64                  `json:"expected_inbound_revision"`
	Profile                 InboundInput            `json:"profile"`
	Nodes                   []NativeInboundPlanNode `json:"nodes"`
	Reason                  string                  `json:"reason"`
	profileOptionsProvided  bool
}

type NativeInboundPlan struct {
	Profile         NativeInbound           `json:"profile"`
	Nodes           []NativeInboundPlanNode `json:"nodes"`
	PlanRevision    uint64                  `json:"plan_revision"`
	InboundRevision uint64                  `json:"inbound_revision"`
	Status          string                  `json:"status"`
	LastEventID     string                  `json:"last_event_id"`
	Reason          string                  `json:"reason"`
	CreatedUnix     int64                   `json:"created_unix"`
	UpdatedUnix     int64                   `json:"updated_unix"`
	RuntimeApply    bool                    `json:"runtime_apply"`
	Editable        bool                    `json:"editable"`
	EditBlocker     string                  `json:"edit_blocker,omitempty"`
}

type NativeInboundPlanSummary struct {
	InboundID       uint64 `json:"inbound_id"`
	PlanRevision    uint64 `json:"plan_revision"`
	InboundRevision uint64 `json:"inbound_revision"`
	Status          string `json:"status"`
}

type NativeInboundPlanReceipt struct {
	Plan                  NativeInboundPlan `json:"plan"`
	EventID               string            `json:"event_id"`
	Replayed              bool              `json:"replayed"`
	RuntimeChanged        bool              `json:"runtime_changed"`
	ClientsDeleted        bool              `json:"clients_deleted"`
	CreditChanged         bool              `json:"credit_changed"`
	UsageReset            bool              `json:"usage_reset"`
	SubscriptionPublished bool              `json:"subscription_published"`
}

type NativeInboundPlanPreview struct {
	Valid                  bool                        `json:"valid"`
	Plan                   NativeInboundPlan           `json:"plan"`
	RuntimeChanged         bool                        `json:"runtime_changed"`
	TopologyValidated      bool                        `json:"topology_validated"`
	TopologyValidationOnly bool                        `json:"topology_validation_only"`
	TopologyKeyScope       string                      `json:"topology_key_scope"`
	Topology               xrayconfig.CreationTopology `json:"topology"`
}

// Field-level failures contain only fixed codes and the submitted node ID, no
// configuration, keys, credentials or underlying SQLite messages.
type NativeInboundPlanError struct {
	NodeID string `json:"node_id,omitempty"`
	Field  string `json:"field"`
	Code   string `json:"code"`
	kind   error
}

func (e *NativeInboundPlanError) Error() string { return e.Code }
func (e *NativeInboundPlanError) Unwrap() error { return e.kind }
func planError(node, field, code string, kind error) error {
	return &NativeInboundPlanError{NodeID: node, Field: field, Code: code, kind: kind}
}

func ensureNativeInboundPlans(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS native_inbound_plans (
 inbound_id INTEGER PRIMARY KEY REFERENCES native_inbounds(id),
 revision INTEGER NOT NULL CHECK(revision>=1 AND revision<=9007199254740991),
 inbound_revision INTEGER NOT NULL CHECK(inbound_revision>=1 AND inbound_revision<=9007199254740991),
 status TEXT NOT NULL CHECK(status IN('SAVED_DISABLED','WAITING_HOST')),
 last_event_id TEXT NOT NULL UNIQUE,payload_enc BLOB NOT NULL,
 created_unix INTEGER NOT NULL,updated_unix INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS native_inbound_plan_events (
 event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN('create','update')),
 inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),
 revision INTEGER NOT NULL,receipt_enc BLOB NOT NULL,created_unix INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS native_inbound_plan_events_scope ON native_inbound_plan_events(inbound_id,revision)`,
		`CREATE TRIGGER IF NOT EXISTS native_inbound_plan_events_no_update BEFORE UPDATE ON native_inbound_plan_events
 BEGIN SELECT RAISE(ABORT,'inbound plan receipts are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_inbound_plan_events_no_delete BEFORE DELETE ON native_inbound_plan_events
 BEGIN SELECT RAISE(ABORT,'inbound plan receipts are immutable'); END`,
		// Persist the authoring boundary in SQLite as well as the current Store
		// methods. An older HUB after binary rollback must not materialize a
		// staged profile through its generic target/binding/enable endpoints.
		`CREATE TRIGGER IF NOT EXISTS native_plan_block_enable BEFORE UPDATE OF enabled ON native_inbounds
 WHEN NEW.enabled<>0 AND EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.id)
 BEGIN SELECT RAISE(ABORT,'staged inbound requires explicit promotion'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_plan_block_target_insert BEFORE INSERT ON native_inbound_targets
 WHEN EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.inbound_id)
 BEGIN SELECT RAISE(ABORT,'staged inbound requires explicit promotion'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_plan_block_target_update BEFORE UPDATE ON native_inbound_targets
 WHEN EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.inbound_id OR inbound_id=OLD.inbound_id)
 BEGIN SELECT RAISE(ABORT,'staged inbound requires explicit promotion'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_plan_block_client_bind BEFORE INSERT ON native_inbound_clients
 WHEN EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.inbound_id)
 BEGIN SELECT RAISE(ABORT,'staged inbound requires explicit promotion'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_plan_block_client_bind_update BEFORE UPDATE ON native_inbound_clients
 WHEN EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.inbound_id)
 BEGIN SELECT RAISE(ABORT,'staged inbound requires explicit promotion'); END`,
		// An unbound authoring profile cannot affect any delivered configuration.
		// Preserve invalidation for every other native profile update, including
		// enabling, archiving, binding, or any profile that already has a target.
		`DROP TRIGGER IF EXISTS native_dirty_profile_update`,
		`CREATE TRIGGER native_dirty_profile_update AFTER UPDATE ON native_inbounds
 WHEN NOT (OLD.enabled=0 AND NEW.enabled=0 AND OLD.archived=0 AND NEW.archived=0
 AND EXISTS(SELECT 1 FROM native_inbound_plans WHERE inbound_id=NEW.id)
 AND NOT EXISTS(SELECT 1 FROM native_inbound_targets WHERE inbound_id=NEW.id)
 AND NOT EXISTS(SELECT 1 FROM native_inbound_clients WHERE inbound_id=NEW.id))
 BEGIN UPDATE native_config_generation SET value=value+1 WHERE id=1; END`,
	} {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	// Additive authoring storage does not change the opt-in usage schema level.
	return tx.Commit()
}

func planAEAD(master []byte) (cipher.AEAD, error) {
	if len(master) != 32 {
		return nil, ErrUnavailable
	}
	h := hmac.New(sha256.New, master)
	_, _ = h.Write([]byte("DARK-PRO-NATIVE-INBOUND-CREATION-PLAN-v1"))
	return profileAEAD(h.Sum(nil))
}
func sealPlanValue(key []byte, aad string, value any) ([]byte, error) {
	a, err := planAEAD(key)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, raw, []byte(aad)), nil
}
func openPlanValue(key []byte, aad string, encrypted []byte, value any) error {
	a, err := planAEAD(key)
	if err != nil {
		return err
	}
	n := a.NonceSize()
	if len(encrypted) < n+a.Overhead() {
		return ErrUnavailable
	}
	raw, err := a.Open(nil, encrypted[:n], encrypted[n:], []byte(aad))
	if err != nil || json.Unmarshal(raw, value) != nil {
		return ErrUnavailable
	}
	return nil
}
func planAAD(id, revision uint64) string { return fmt.Sprintf("plan:%d:%d", id, revision) }
func planReceiptAAD(event string, id, revision uint64) string {
	return fmt.Sprintf("receipt:%s:%d:%d", event, id, revision)
}

func validPlanInstallation(id string) bool {
	_, err := hex.DecodeString(id)
	return len(id) == 32 && err == nil
}
func canonicalPlanIP(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
		return ""
	}
	return ip.String()
}
func planListenerPort(p uint16) bool { return p >= 1024 && p != 2087 && p != 8443 && p != 24443 }

func normalizeNativeInboundPlan(in NativeInboundPlanInput) (NativeInboundPlanInput, error) {
	in.profileOptionsProvided = in.profileOptionsProvided || in.Profile.Options != nil
	in.Reason = strings.TrimSpace(in.Reason)
	in.Profile.Name = strings.TrimSpace(in.Profile.Name)
	if !eventRE.MatchString(in.EventID) {
		return in, planError("", "event_id", "event_id_invalid", ErrInvalid)
	}
	if in.Profile.ID == 0 || in.Profile.ID > 1<<53-1 || !validName(in.Profile.Name, 128) {
		return in, planError("", "profile", "profile_invalid", ErrInvalid)
	}
	if in.ExpectedPlanRevision >= 1<<53-1 || in.ExpectedInboundRevision >= 1<<53-1 ||
		(in.ExpectedPlanRevision == 0) != (in.ExpectedInboundRevision == 0) {
		return in, planError("", "expected_plan_revision", "revision_invalid", ErrInvalid)
	}
	if !validName(in.Reason, 200) {
		return in, planError("", "reason", "reason_invalid", ErrInvalid)
	}
	if len(in.Nodes) == 0 || len(in.Nodes) > MaxNativeInboundPlanNodes {
		return in, planError("", "nodes", "node_count_invalid", ErrInvalid)
	}
	// Own all slices: caller edits must never change a checked request/receipt.
	in.Profile.ServerNames = append([]string(nil), in.Profile.ServerNames...)
	in.Profile.ShortIDs = append([]string(nil), in.Profile.ShortIDs...)
	options, optionsErr := xrayconfig.NormalizeProfileOptions(in.Profile.Transport, in.Profile.Options)
	if optionsErr != nil {
		return in, planError("", "profile.options", "profile_options_invalid", ErrInvalid)
	}
	in.Profile.Options = options
	in.Nodes = append([]NativeInboundPlanNode(nil), in.Nodes...)
	seen := map[string]bool{}
	for i := range in.Nodes {
		n := &in.Nodes[i]
		if !idRE.MatchString(n.NodeID) || !validPlanInstallation(n.InstallationID) || seen[n.NodeID] {
			return in, planError(n.NodeID, "node_id", "node_identity_invalid", ErrInvalid)
		}
		seen[n.NodeID] = true
		n.ListenIP = canonicalPlanIP(n.ListenIP)
		if n.ListenIP == "" {
			return in, planError(n.NodeID, "listen_ip", "listen_ip_invalid", ErrInvalid)
		}
		var ok bool
		n.DirectHosts, ok = normalizeRouteHosts(n.DirectHosts)
		if !ok {
			return in, planError(n.NodeID, "direct_hosts", "host_invalid", ErrInvalid)
		}
		n.TunnelHosts, ok = normalizeRouteHosts(n.TunnelHosts)
		hostUnion := map[string]bool{}
		for _, host := range n.DirectHosts {
			hostUnion[host] = true
		}
		for _, host := range n.TunnelHosts {
			hostUnion[host] = true
		}
		if !ok || len(hostUnion) > maxRouteHosts {
			return in, planError(n.NodeID, "tunnel_hosts", "host_limit_or_invalid", ErrInvalid)
		}
		if n.DirectPort == 0 && len(n.DirectHosts) != 0 || n.TunnelPort == 0 && len(n.TunnelHosts) != 0 {
			return in, planError(n.NodeID, "hosts", "disabled_path_has_hosts", ErrInvalid)
		}
		for field, port := range map[string]uint16{"direct_port": n.DirectPort, "tunnel_port": n.TunnelPort} {
			if port != 0 && !planListenerPort(port) {
				return in, planError(n.NodeID, field, "listener_port_invalid_or_reserved", ErrInvalid)
			}
		}
		if n.DirectPort == 0 && n.TunnelPort == 0 && !n.SwapEnabled {
			return in, planError(n.NodeID, "paths", "path_required", ErrInvalid)
		}
		if n.SwapEnabled {
			if !idRE.MatchString(n.SwapSourceNodeID) || n.SwapSourceNodeID == n.NodeID || !validPlanInstallation(n.SwapSourceInstallationID) {
				return in, planError(n.NodeID, "swap_source_node_id", "swap_source_invalid", ErrInvalid)
			}
			n.SwapSourceListenIP = canonicalPlanIP(n.SwapSourceListenIP)
			if n.SwapSourceListenIP == "" {
				return in, planError(n.NodeID, "swap_source_listen_ip", "listen_ip_invalid", ErrInvalid)
			}
			if !planListenerPort(n.SwapSourcePort) || !planListenerPort(n.SwapDestinationPort) || n.SwapEntryPort == 0 {
				return in, planError(n.NodeID, "swap_ports", "swap_port_invalid_or_reserved", ErrInvalid)
			}
			n.SwapEntryAddress = strings.TrimSpace(n.SwapEntryAddress)
			if n.SwapEntryAddress != "" {
				n.SwapEntryAddress = canonicalRouteHost(n.SwapEntryAddress)
				if n.SwapEntryAddress == "" {
					return in, planError(n.NodeID, "swap_entry_address", "host_invalid", ErrInvalid)
				}
			}
		} else if n.SwapSourceNodeID != "" || n.SwapSourceInstallationID != "" || n.SwapSourceListenIP != "" ||
			n.SwapSourcePort != 0 || n.SwapEntryAddress != "" || n.SwapEntryPort != 0 || n.SwapDestinationPort != 0 {
			return in, planError(n.NodeID, "swap", "disabled_swap_has_settings", ErrInvalid)
		}
	}
	sort.Slice(in.Nodes, func(i, j int) bool { return in.Nodes[i].NodeID < in.Nodes[j].NodeID })
	if _, err := planListeners(in.Nodes); err != nil {
		return in, err
	}
	if _, err := planEntryMappings(in.Nodes); err != nil {
		return in, err
	}
	return in, nil
}

type planEntryMapping struct{ signature, node string }

func planEntryMappings(nodes []NativeInboundPlanNode) (map[string]planEntryMapping, error) {
	out := map[string]planEntryMapping{}
	for _, n := range nodes {
		if !n.SwapEnabled || n.SwapEntryAddress == "" {
			continue
		}
		entry := fmt.Sprintf("%s:%d", n.SwapEntryAddress, n.SwapEntryPort)
		value := planEntryMapping{signature: fmt.Sprintf("%s:%s:%s:%d", n.SwapSourceNodeID,
			n.SwapSourceInstallationID, n.SwapSourceListenIP, n.SwapSourcePort), node: n.NodeID}
		if prior, exists := out[entry]; exists && prior.signature != value.signature {
			return nil, planError(n.NodeID, "swap_entry_address", "entry_mapping_conflict", ErrConflict)
		}
		out[entry] = value
	}
	return out, nil
}

type planNodeListeners struct {
	installation string
	listenIP     string
	ports        map[uint16]bool
}

// Includes relay-only source nodes, not merely the selected destination rows.
func planListeners(nodes []NativeInboundPlanNode) (map[string]*planNodeListeners, error) {
	out := map[string]*planNodeListeners{}
	add := func(node, installation, ip string, port uint16) error {
		state, exists := out[node]
		if !exists {
			state = &planNodeListeners{installation: installation, listenIP: ip, ports: map[uint16]bool{}}
			out[node] = state
		}
		if state.installation != installation || state.listenIP != ip {
			return planError(node, "listen_ip", "node_binding_conflict", ErrConflict)
		}
		if port == 0 {
			return nil
		}
		if state.ports[port] {
			return planError(node, "ports", "listener_port_collision", ErrConflict)
		}
		state.ports[port] = true
		return nil
	}
	edges := map[string][]string{}
	for _, n := range nodes {
		if err := add(n.NodeID, n.InstallationID, n.ListenIP, n.DirectPort); err != nil {
			return nil, err
		}
		if err := add(n.NodeID, n.InstallationID, n.ListenIP, n.TunnelPort); err != nil {
			return nil, err
		}
		if n.SwapEnabled {
			// Reuse of the destination's ordinary Tunnel termination is explicit.
			if n.SwapDestinationPort != n.TunnelPort {
				if err := add(n.NodeID, n.InstallationID, n.ListenIP, n.SwapDestinationPort); err != nil {
					return nil, err
				}
			}
			if err := add(n.SwapSourceNodeID, n.SwapSourceInstallationID, n.SwapSourceListenIP, n.SwapSourcePort); err != nil {
				return nil, err
			}
			edges[n.SwapSourceNodeID] = append(edges[n.SwapSourceNodeID], n.NodeID)
		}
	}
	color := map[string]int{}
	var visit func(string) bool
	visit = func(id string) bool {
		if color[id] == 1 {
			return false
		}
		if color[id] == 2 {
			return true
		}
		color[id] = 1
		for _, next := range edges[id] {
			if !visit(next) {
				return false
			}
		}
		color[id] = 2
		return true
	}
	for node := range out {
		if !visit(node) {
			return nil, planError(node, "swap_source_node_id", "route_cycle", ErrConflict)
		}
	}
	return out, nil
}

func nativePlanStatus(nodes []NativeInboundPlanNode) string {
	for _, n := range nodes {
		if n.DirectPort != 0 && len(n.DirectHosts) == 0 || n.TunnelPort != 0 && len(n.TunnelHosts) == 0 || n.SwapEnabled && n.SwapEntryAddress == "" {
			return "WAITING_HOST"
		}
	}
	return "SAVED_DISABLED"
}

func plannedProfile(in InboundInput, privateKey string) xrayconfig.Profile {
	return xrayconfig.Profile{InboundID: in.ID, Transport: in.Transport, GRPCService: in.GRPCService,
		RealityTarget: in.RealityTarget, ServerNames: in.ServerNames, ShortIDs: in.ShortIDs,
		Options: in.Options, RealityPrivateKey: privateKey, Users: []xrayconfig.User{}}
}
func validatePlannedProfile(p xrayconfig.Profile) error {
	_, err := xrayconfig.RenderAssigned("profile-validation", "00000000000000000000000000000000", "127.0.0.1", []xrayconfig.Assignment{{Profile: p, DirectPort: 10443}})
	if err != nil {
		return planError("", "profile", "profile_invalid", ErrInvalid)
	}
	return nil
}

func sealPlannedProfile(key []byte, p xrayconfig.Profile) ([]byte, error) {
	return sealInboundProfile(key, p)
}

type planReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func readNativeInboundPlan(ctx context.Context, q inboundReader, key []byte, id uint64) (NativeInboundPlan, error) {
	var out NativeInboundPlan
	var revision, inboundRevision uint64
	var enc []byte
	var status, event string
	var created, updated int64
	err := q.QueryRowContext(ctx, `SELECT revision,inbound_revision,status,last_event_id,payload_enc,created_unix,updated_unix FROM native_inbound_plans WHERE inbound_id=?`, id).
		Scan(&revision, &inboundRevision, &status, &event, &enc, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if err = openPlanValue(key, planAAD(id, revision), enc, &out); err != nil {
		return out, err
	}
	if out.Profile.ID != id || out.PlanRevision != revision || out.InboundRevision != inboundRevision || out.Profile.Revision != inboundRevision ||
		out.Status != status || out.LastEventID != event || out.CreatedUnix != created || out.UpdatedUnix != updated ||
		out.Profile.Enabled || out.Profile.Archived || out.Profile.BoundClients != 0 || out.RuntimeApply || !out.Editable {
		return NativeInboundPlan{}, ErrUnavailable
	}
	return out, nil
}

func (s *Store) ListNativeInboundPlans(ctx context.Context) ([]NativeInboundPlanSummary, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT inbound_id,revision,inbound_revision,status FROM native_inbound_plans ORDER BY inbound_id LIMIT 257`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]NativeInboundPlanSummary, 0)
	for rows.Next() {
		var p NativeInboundPlanSummary
		if err = rows.Scan(&p.InboundID, &p.PlanRevision, &p.InboundRevision, &p.Status); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if len(out) > 256 {
		return nil, ErrInvalid
	}
	return out, rows.Err()
}

func (s *Store) NativeInboundPlan(ctx context.Context, key []byte, id uint64) (NativeInboundPlan, error) {
	if id == 0 || id > 1<<53-1 {
		return NativeInboundPlan{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NativeInboundPlan{}, err
	}
	defer tx.Rollback()
	p, err := readNativeInboundPlan(ctx, tx, key, id)
	if err != nil {
		return p, err
	}
	if _, _, err = editablePlanProfile(ctx, tx, key, p); err != nil {
		if !errors.Is(err, ErrConflict) {
			return p, err
		}
		p.Editable = false
		p.EditBlocker = "profile_changed_or_materialized"
	}
	return p, nil
}

func editablePlanProfile(ctx context.Context, tx *sql.Tx, key []byte, plan NativeInboundPlan) (NativeInbound, xrayconfig.Profile, error) {
	in, p, err := readNativeInbound(ctx, tx, key, plan.Profile.ID)
	if err != nil {
		return in, p, err
	}
	var targets int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_inbound_targets WHERE inbound_id=?`, in.ID).Scan(&targets); err != nil {
		return in, p, err
	}
	if in.Enabled || in.Archived || in.BoundClients != 0 || targets != 0 || in.Revision != plan.InboundRevision ||
		!reflect.DeepEqual(in.InboundInput, plan.Profile.InboundInput) || in.PublicKey != plan.Profile.PublicKey {
		return in, p, planError("", "profile", "profile_changed_or_materialized", ErrConflict)
	}
	return in, p, nil
}

// Generic native operations cannot bypass the complete staged setup. Logical
// archive/disable and removal of a binding remain available for revocation.
func rejectNativePlanMaterialization(ctx context.Context, q inboundReader, id uint64) error {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_inbound_plans WHERE inbound_id=?`, id).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return planError("", "profile", "staged_plan_requires_promotion", ErrConflict)
	}
	return nil
}

func verifyPlanCollisions(ctx context.Context, tx *sql.Tx, key []byte, in NativeInboundPlanInput) error {
	proposed, err := planListeners(in.Nodes)
	if err != nil {
		return err
	}
	entries, err := planEntryMappings(in.Nodes)
	if err != nil {
		return err
	}
	check := func(node, installation, ip string, ports ...uint16) error {
		p := proposed[node]
		if p == nil {
			return nil
		}
		if p.installation != installation || p.listenIP != ip {
			return planError(node, "listen_ip", "existing_node_binding_conflict", ErrConflict)
		}
		for _, port := range ports {
			if port != 0 && p.ports[port] {
				return planError(node, "ports", "existing_listener_port_collision", ErrConflict)
			}
		}
		return nil
	}
	// Reservations include disabled, nonarchived profiles. A staged creation
	// cannot take a port merely because its current owner is paused.
	rows, err := tx.QueryContext(ctx, `SELECT t.node_id,t.installation_id,t.listen_ip,t.direct_port,t.swap_port
 FROM native_inbound_targets t JOIN native_inbounds i ON i.id=t.inbound_id WHERE i.archived=0`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var node, installation, ip string
		var direct, swap uint16
		if err = rows.Scan(&node, &installation, &ip, &direct, &swap); err != nil {
			rows.Close()
			return err
		}
		if err = check(node, installation, ip, direct, swap); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT d.node_id,d.installation_id,d.listen_ip,d.direct_port,d.tunnel_port,d.swap_node_port
 FROM native_route_drafts d JOIN native_inbounds i ON i.id=d.inbound_id WHERE i.archived=0`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var node, installation, ip string
		var direct, tunnel, swap uint16
		if err = rows.Scan(&node, &installation, &ip, &direct, &tunnel, &swap); err != nil {
			rows.Close()
			return err
		}
		if err = check(node, installation, ip, direct, tunnel, swap); err != nil {
			rows.Close()
			return err
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, `SELECT p.inbound_id FROM native_inbound_plans p JOIN native_inbounds i ON i.id=p.inbound_id
 WHERE p.inbound_id<>? AND i.archived=0 ORDER BY p.inbound_id LIMIT 257`, in.Profile.ID)
	if err != nil {
		return err
	}
	ids := make([]uint64, 0)
	for rows.Next() {
		var id uint64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(ids) > 256 {
		return ErrUnavailable
	}
	for _, id := range ids {
		other, e := readNativeInboundPlan(ctx, tx, key, id)
		if e != nil {
			return e
		}
		otherEntries, e := planEntryMappings(other.Nodes)
		if e != nil {
			return ErrUnavailable
		}
		for entry, mapping := range entries {
			if otherMapping, exists := otherEntries[entry]; exists && mapping.signature != otherMapping.signature {
				return planError(mapping.node, "swap_entry_address", "entry_mapping_conflict", ErrConflict)
			}
		}
		listeners, e := planListeners(other.Nodes)
		if e != nil {
			return ErrUnavailable
		}
		for node, state := range listeners {
			ports := make([]uint16, 0, len(state.ports))
			for port := range state.ports {
				ports = append(ports, port)
			}
			if e = check(node, state.installation, state.listenIP, ports...); e != nil {
				return e
			}
		}
	}
	return nil
}

func compileNativeInboundPlan(p xrayconfig.Profile, nodes []NativeInboundPlanNode) (xrayconfig.CreationTopology, error) {
	destinations := make([]xrayconfig.CreationNode, 0, len(nodes))
	swaps := make([]xrayconfig.CreationSwap, 0)
	for _, n := range nodes {
		destinations = append(destinations, xrayconfig.CreationNode{NodeID: n.NodeID, InstallationID: n.InstallationID,
			ListenIP: n.ListenIP, DirectPort: n.DirectPort, TunnelPort: n.TunnelPort})
		if n.SwapEnabled {
			swaps = append(swaps, xrayconfig.CreationSwap{Name: "swap-" + n.NodeID, SourceNodeID: n.SwapSourceNodeID,
				SourceInstallationID: n.SwapSourceInstallationID, SourceListenIP: n.SwapSourceListenIP,
				SourcePort: n.SwapSourcePort, DestinationNodeID: n.NodeID, DestinationPort: n.SwapDestinationPort})
		}
	}
	topology, err := xrayconfig.RenderCreationTopology(p, destinations, swaps)
	if err != nil {
		return topology, planError("", "nodes", "topology_invalid", ErrInvalid)
	}
	return topology, nil
}

func validateNativeInboundPlanTx(ctx context.Context, tx *sql.Tx, key []byte, in NativeInboundPlanInput) (NativeInboundPlan, xrayconfig.Profile, xrayconfig.CreationTopology, error) {
	var plan NativeInboundPlan
	var private xrayconfig.Profile
	var topology xrayconfig.CreationTopology
	if _, err := planAEAD(key); err != nil {
		return plan, private, topology, err
	}
	now := time.Now().Unix()
	plan = NativeInboundPlan{Nodes: in.Nodes, PlanRevision: in.ExpectedPlanRevision + 1, LastEventID: in.EventID,
		Reason: in.Reason, Status: nativePlanStatus(in.Nodes), CreatedUnix: now, UpdatedUnix: now, Editable: true}
	if in.ExpectedPlanRevision == 0 {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_inbounds WHERE id=?`, in.Profile.ID).Scan(&count); err != nil {
			return plan, private, topology, err
		}
		if count != 0 {
			return plan, private, topology, planError("", "profile.id", "inbound_id_exists", ErrConflict)
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM native_inbounds`).Scan(&count); err != nil {
			return plan, private, topology, err
		}
		if count >= 256 {
			return plan, private, topology, planError("", "profile.id", "inbound_limit", ErrConflict)
		}
		plan.Profile = NativeInbound{InboundInput: in.Profile, Revision: 1}
		// A new preview creates neither a private key nor an identity. This key
		// validates shape only; its hashes are explicitly preview-only.
		private = plannedProfile(in.Profile, base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("\x01", 32))))
	} else {
		old, err := readNativeInboundPlan(ctx, tx, key, in.Profile.ID)
		if err != nil {
			return plan, private, topology, err
		}
		if old.PlanRevision != in.ExpectedPlanRevision || old.InboundRevision != in.ExpectedInboundRevision {
			return plan, private, topology, planError("", "expected_plan_revision", "plan_revision_conflict", ErrConflict)
		}
		existing, original, err := editablePlanProfile(ctx, tx, key, old)
		if err != nil {
			return plan, private, topology, err
		}
		// An older open browser does not know about Options. Require explicit
		// options (including an empty object for clearing) before replacing a
		// profile that already has extended settings.
		if existing.Options != nil && !in.profileOptionsProvided {
			return plan, private, topology, planError("", "profile.options", "profile_options_required", ErrConflict)
		}
		plan.CreatedUnix = old.CreatedUnix
		plan.Profile = existing
		if !reflect.DeepEqual(existing.InboundInput, in.Profile) {
			plan.Profile.Revision++
		}
		plan.Profile.InboundInput = in.Profile
		private = plannedProfile(in.Profile, original.RealityPrivateKey)
	}
	plan.InboundRevision = plan.Profile.Revision
	if err := validatePlannedProfile(private); err != nil {
		return plan, private, topology, err
	}
	if err := verifyPlanCollisions(ctx, tx, key, in); err != nil {
		return plan, private, topology, err
	}
	topology, err := compileNativeInboundPlan(private, in.Nodes)
	return plan, private, topology, err
}

func (s *Store) PreviewNativeInboundPlan(ctx context.Context, key []byte, input NativeInboundPlanInput) (NativeInboundPlanPreview, error) {
	var out NativeInboundPlanPreview
	in, err := normalizeNativeInboundPlan(input)
	if err != nil {
		return out, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	plan, _, topology, err := validateNativeInboundPlanTx(ctx, tx, key, in)
	if err != nil {
		return out, err
	}
	scope := "saved_profile"
	if in.ExpectedPlanRevision == 0 {
		scope = "preview_only"
	}
	return NativeInboundPlanPreview{Valid: true, Plan: plan, TopologyValidated: true,
		TopologyValidationOnly: true, TopologyKeyScope: scope, Topology: topology}, nil
}

func readNativePlanReplay(ctx context.Context, tx *sql.Tx, key []byte, in NativeInboundPlanInput, kind, hash string) (NativeInboundPlanReceipt, bool, error) {
	var receipt NativeInboundPlanReceipt
	var priorHash, priorKind string
	var id, revision uint64
	var enc []byte
	var created int64
	err := tx.QueryRowContext(ctx, `SELECT fingerprint,kind,inbound_id,revision,receipt_enc,created_unix FROM native_inbound_plan_events WHERE event_id=?`, in.EventID).
		Scan(&priorHash, &priorKind, &id, &revision, &enc, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return receipt, false, nil
	}
	if err != nil {
		return receipt, false, err
	}
	if priorHash != hash || priorKind != kind || id != in.Profile.ID {
		return receipt, false, ErrConflict
	}
	if err = openPlanValue(key, planReceiptAAD(in.EventID, id, revision), enc, &receipt); err != nil {
		return receipt, false, err
	}
	if receipt.EventID != in.EventID || receipt.Plan.Profile.ID != id || receipt.Plan.PlanRevision != revision ||
		receipt.Plan.UpdatedUnix != created || receipt.Replayed || receipt.RuntimeChanged || receipt.ClientsDeleted ||
		receipt.CreditChanged || receipt.UsageReset || receipt.SubscriptionPublished || receipt.Plan.RuntimeApply || receipt.Plan.Profile.Enabled {
		return NativeInboundPlanReceipt{}, false, ErrUnavailable
	}
	receipt.Replayed = true
	return receipt, true, nil
}

// ReplayNativeInboundPlan is receipt-only. It permits an authenticated owner
// to recover the immutable result after a lost response even if live node
// pairing has since changed. It never validates or applies a new operation.
func (s *Store) ReplayNativeInboundPlan(ctx context.Context, key []byte, input NativeInboundPlanInput) (NativeInboundPlanReceipt, bool, error) {
	in, err := normalizeNativeInboundPlan(input)
	if err != nil {
		return NativeInboundPlanReceipt{}, false, err
	}
	kind := "create"
	if in.ExpectedPlanRevision > 0 {
		kind = "update"
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return NativeInboundPlanReceipt{}, false, err
	}
	defer tx.Rollback()
	return readNativePlanReplay(ctx, tx, key, in, kind, fingerprint("native_inbound_plan_"+kind, in))
}

// SaveNativeInboundPlan commits a new disabled profile, every selected path,
// and its immutable exact-response receipt in one SQLite transaction. A retry
// returns that receipt even if a newer plan revision has since been saved.
func (s *Store) SaveNativeInboundPlan(ctx context.Context, key []byte, input NativeInboundPlanInput) (NativeInboundPlanReceipt, error) {
	var out NativeInboundPlanReceipt
	in, err := normalizeNativeInboundPlan(input)
	if err != nil {
		return out, err
	}
	kind := "create"
	if in.ExpectedPlanRevision > 0 {
		kind = "update"
	}
	hash := fingerprint("native_inbound_plan_"+kind, in)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if replay, found, e := readNativePlanReplay(ctx, tx, key, in, kind, hash); e != nil || found {
		return replay, e
	}
	plan, private, _, err := validateNativeInboundPlanTx(ctx, tx, key, in)
	if err != nil {
		return out, err
	}
	if kind == "create" {
		priv, e := ecdh.X25519().GenerateKey(rand.Reader)
		if e != nil {
			return out, e
		}
		private.RealityPrivateKey = base64.RawURLEncoding.EncodeToString(priv.Bytes())
		plan.Profile.PublicKey = base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes())
		if _, e = compileNativeInboundPlan(private, in.Nodes); e != nil {
			return out, e
		}
		enc, e := sealPlannedProfile(key, private)
		if e != nil {
			return out, e
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO native_inbounds(id,name,profile_enc,public_key,created_unix,enabled,archived,revision)
 VALUES(?,?,?,?,?,0,0,1)`, in.Profile.ID, in.Profile.Name, enc, plan.Profile.PublicKey, plan.CreatedUnix); err != nil {
			return out, err
		}
	} else if plan.InboundRevision != in.ExpectedInboundRevision {
		enc, e := sealPlannedProfile(key, private)
		if e != nil {
			return out, e
		}
		result, e := tx.ExecContext(ctx, `UPDATE native_inbounds SET name=?,profile_enc=?,revision=?
 WHERE id=? AND revision=? AND enabled=0 AND archived=0`, in.Profile.Name, enc, plan.InboundRevision, in.Profile.ID, in.ExpectedInboundRevision)
		if e != nil {
			return out, e
		}
		count, e := result.RowsAffected()
		if e != nil {
			return out, e
		}
		if count != 1 {
			return out, ErrConflict
		}
	}
	enc, err := sealPlanValue(key, planAAD(in.Profile.ID, plan.PlanRevision), plan)
	if err != nil {
		return out, err
	}
	if kind == "create" {
		_, err = tx.ExecContext(ctx, `INSERT INTO native_inbound_plans(inbound_id,revision,inbound_revision,status,last_event_id,payload_enc,created_unix,updated_unix)
 VALUES(?,?,?,?,?,?,?,?)`, in.Profile.ID, plan.PlanRevision, plan.InboundRevision, plan.Status, in.EventID, enc, plan.CreatedUnix, plan.UpdatedUnix)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE native_inbound_plans SET revision=?,inbound_revision=?,status=?,last_event_id=?,payload_enc=?,updated_unix=?
 WHERE inbound_id=? AND revision=? AND inbound_revision=?`, plan.PlanRevision, plan.InboundRevision, plan.Status, in.EventID, enc, plan.UpdatedUnix,
			in.Profile.ID, in.ExpectedPlanRevision, in.ExpectedInboundRevision)
		if err == nil {
			var count int64
			count, err = result.RowsAffected()
			if err == nil && count != 1 {
				err = ErrConflict
			}
		}
	}
	if err != nil {
		return out, err
	}
	out = NativeInboundPlanReceipt{Plan: plan, EventID: in.EventID}
	sealedReceipt, err := sealPlanValue(key, planReceiptAAD(in.EventID, in.Profile.ID, plan.PlanRevision), out)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO native_inbound_plan_events(event_id,fingerprint,kind,inbound_id,revision,receipt_enc,created_unix)
 VALUES(?,?,?,?,?,?,?)`, in.EventID, hash, kind, in.Profile.ID, plan.PlanRevision, sealedReceipt, plan.UpdatedUnix); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return NativeInboundPlanReceipt{}, err
	}
	return out, nil
}
