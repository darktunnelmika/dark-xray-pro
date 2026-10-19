package business

// Owner-reviewed inbound route drafts; no runtime Apply.
import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"
)

const maxRouteHosts = 64

var routeHostLabel = regexp.MustCompile("^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")

type NativeRouteDraftInput struct {
	EventID                     string   `json:"event_id"`
	InboundID                   uint64   `json:"inbound_id"`
	NodeID                      string   `json:"node_id"`
	InstallationID              string   `json:"installation_id"`
	ExpectedInboundRevision     uint64   `json:"expected_inbound_revision"`
	ExpectedDraftRevision       uint64   `json:"expected_draft_revision"`
	DirectPort                  uint16   `json:"direct_port"`
	DirectHosts                 []string `json:"direct_hosts"`
	TunnelPort                  uint16   `json:"tunnel_port"`
	TunnelHosts                 []string `json:"tunnel_hosts"`
	SwapEnabled                 bool     `json:"swap_enabled"`
	SwapUpstreamNodeID          string   `json:"swap_upstream_node_id"`
	SwapTunnelHost              string   `json:"swap_tunnel_host"`
	SwapTunnelPort              uint16   `json:"swap_tunnel_port"`
	SwapNodePort                uint16   `json:"swap_node_port"`
	ConfirmExistingRouteDisable bool     `json:"confirm_existing_route_disable"`
	Reason                      string   `json:"reason"`
}
type NativeRouteDraft struct {
	NativeRouteDraftInput
	DraftRevision         uint64 `json:"draft_revision"`
	ListenIP              string `json:"listen_ip"`
	NativeDirectPort      uint16 `json:"native_direct_port"`
	NativeSwapPort        uint16 `json:"native_swap_port"`
	BillingSourceNodeID   string `json:"billing_source_node_id"`
	CreatedUnix           int64  `json:"created_unix"`
	UpdatedUnix           int64  `json:"updated_unix"`
	ApplyAllowed          bool   `json:"apply_allowed"`
	AgentAckVerified      bool   `json:"agent_ack_verified"`
	DataPlaneVerified     bool   `json:"data_plane_verified"`
	BillingVerified       bool   `json:"billing_verified"`
	SubscriptionPublished bool   `json:"subscription_published"`
}
type NativeRouteDraftReceipt struct {
	Draft          NativeRouteDraft `json:"draft"`
	Replayed       bool             `json:"replayed"`
	ClientsDeleted bool             `json:"clients_deleted"`
	CreditChanged  bool             `json:"credit_changed"`
	UsageReset     bool             `json:"usage_reset"`
	RuntimeChanged bool             `json:"runtime_changed"`
}

func ensureNativeRouteDrafts(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS native_route_drafts (
   inbound_id INTEGER NOT NULL REFERENCES native_inbounds(id),
   node_id TEXT NOT NULL,installation_id TEXT NOT NULL,
   revision INTEGER NOT NULL CHECK(revision>=1),
   inbound_revision INTEGER NOT NULL CHECK(inbound_revision>=1),
   listen_ip TEXT NOT NULL,
   original_direct_port INTEGER NOT NULL,original_swap_port INTEGER NOT NULL,
   direct_port INTEGER NOT NULL,tunnel_port INTEGER NOT NULL,swap_node_port INTEGER NOT NULL,
   last_event_id TEXT NOT NULL UNIQUE,payload_enc BLOB NOT NULL,
   created_unix INTEGER NOT NULL,updated_unix INTEGER NOT NULL,
   PRIMARY KEY(inbound_id,node_id))`,
		`CREATE INDEX IF NOT EXISTS native_route_drafts_by_node ON native_route_drafts(node_id,installation_id,inbound_id)`,
		`CREATE TABLE IF NOT EXISTS native_route_draft_events (
   event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL,
   inbound_id INTEGER NOT NULL,node_id TEXT NOT NULL,
   revision INTEGER NOT NULL,created_unix INTEGER NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS native_route_draft_events_scope ON native_route_draft_events(inbound_id,node_id,revision DESC)`,
		`CREATE TRIGGER IF NOT EXISTS native_route_draft_events_append_only_update
   BEFORE UPDATE ON native_route_draft_events BEGIN SELECT RAISE(ABORT,'route draft audit immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_draft_events_append_only_delete
   BEFORE DELETE ON native_route_draft_events BEGIN SELECT RAISE(ABORT,'route draft audit immutable'); END`,
	} {
		if _, err = tx.Exec(stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func canonicalRouteHost(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if h == "" || len(h) > 253 || strings.ContainsAny(h, "/:@%?#\\ \t\r\n") {
		return ""
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			return ""
		}
		return ip.String()
	}
	parts := strings.Split(h, ".")
	if len(parts) < 2 {
		return ""
	}
	for _, part := range parts {
		if !routeHostLabel.MatchString(part) {
			return ""
		}
	}
	if len(parts[len(parts)-1]) < 2 {
		return ""
	}
	return h
}
func normalizeRouteHosts(hosts []string) ([]string, bool) {
	if len(hosts) > maxRouteHosts {
		return nil, false
	}
	out := make([]string, 0, len(hosts))
	seen := map[string]bool{}
	for _, raw := range hosts {
		host := canonicalRouteHost(raw)
		if host == "" || seen[host] {
			return nil, false
		}
		seen[host] = true
		out = append(out, host)
	}
	return out, true
}
func validDraftPort(p uint16) bool { return p == 0 || p >= 1024 }
func checkDraftInput(in NativeRouteDraftInput) (NativeRouteDraftInput, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !eventRE.MatchString(in.EventID) || in.InboundID == 0 || in.InboundID > (1<<53-1) ||
		!idRE.MatchString(in.NodeID) || len(in.InstallationID) != 32 ||
		in.ExpectedInboundRevision == 0 || in.ExpectedDraftRevision > (1<<53-1) ||
		!validName(in.Reason, 200) || !validDraftPort(in.DirectPort) ||
		!validDraftPort(in.TunnelPort) || !validDraftPort(in.SwapNodePort) ||
		!validDraftPort(in.SwapTunnelPort) {
		return in, ErrInvalid
	}
	if _, err := hex.DecodeString(in.InstallationID); err != nil {
		return in, ErrInvalid
	}
	var ok bool
	in.DirectHosts, ok = normalizeRouteHosts(in.DirectHosts)
	if !ok {
		return in, ErrInvalid
	}
	in.TunnelHosts, ok = normalizeRouteHosts(in.TunnelHosts)
	if !ok || len(in.DirectHosts)+len(in.TunnelHosts) > maxRouteHosts {
		return in, ErrInvalid
	}
	if in.DirectPort == 0 && len(in.DirectHosts) != 0 {
		return in, ErrInvalid
	}
	if in.TunnelPort == 0 && len(in.TunnelHosts) != 0 {
		return in, ErrInvalid
	}
	ports := []uint16{in.DirectPort, in.TunnelPort}
	if in.SwapEnabled {
		if in.SwapNodePort == 0 || in.SwapTunnelPort == 0 ||
			in.SwapUpstreamNodeID == in.NodeID || !idRE.MatchString(in.SwapUpstreamNodeID) {
			return in, ErrInvalid
		}
		in.SwapTunnelHost = canonicalRouteHost(in.SwapTunnelHost)
		if in.SwapTunnelHost == "" {
			return in, ErrInvalid
		}
		ports = append(ports, in.SwapNodePort)
	} else if in.SwapTunnelHost != "" || in.SwapUpstreamNodeID != "" ||
		in.SwapTunnelPort != 0 || in.SwapNodePort != 0 {
		return in, ErrInvalid
	}
	if in.DirectPort == 0 && in.TunnelPort == 0 && !in.SwapEnabled {
		return in, ErrInvalid
	}
	seen := map[uint16]bool{}
	for _, p := range ports {
		if p == 0 {
			continue
		}
		if p == 2087 || p == 8443 || p == 24443 || seen[p] {
			return in, ErrConflict
		}
		seen[p] = true
	}
	return in, nil
}
func routeDraftKey(master []byte) ([]byte, error) {
	if len(master) != 32 {
		return nil, ErrUnavailable
	}
	m := hmac.New(sha256.New, master)
	_, _ = m.Write([]byte("DARK-PRO-NATIVE-INBOUND-ROUTE-DRAFT-v1"))
	return m.Sum(nil), nil
}
func sealRouteDraft(master []byte, id uint64, node string, rev uint64, data []byte) ([]byte, error) {
	key, err := routeDraftKey(master)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	aad := []byte(fmt.Sprintf("route-draft:%d:%s:%d", id, node, rev))
	return aead.Seal(nonce, nonce, data, aad), nil
}
func openRouteDraft(master []byte, id uint64, node string, rev uint64, enc []byte) (NativeRouteDraft, error) {
	var out NativeRouteDraft
	key, err := routeDraftKey(master)
	if err != nil {
		return out, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return out, ErrUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return out, ErrUnavailable
	}
	n := aead.NonceSize()
	if len(enc) < n+aead.Overhead() {
		return out, ErrUnavailable
	}
	aad := []byte(fmt.Sprintf("route-draft:%d:%s:%d", id, node, rev))
	plain, err := aead.Open(nil, enc[:n], enc[n:], aad)
	if err != nil || json.Unmarshal(plain, &out) != nil ||
		out.InboundID != id || out.NodeID != node || out.DraftRevision != rev {
		return NativeRouteDraft{}, ErrUnavailable
	}
	return out, nil
}
func readNativeRouteDraft(ctx context.Context, tx *sql.Tx, master []byte, id uint64, node string) (NativeRouteDraft, error) {
	var enc []byte
	var rev uint64
	var install, listen string
	var inboundRev uint64
	var direct, swap, savedDirect, savedTunnel, savedSwapNode uint16
	var lastEvent string
	err := tx.QueryRowContext(ctx, `SELECT revision,installation_id,inbound_revision,listen_ip,
  original_direct_port,original_swap_port,direct_port,tunnel_port,swap_node_port,last_event_id,payload_enc
  FROM native_route_drafts WHERE inbound_id=? AND node_id=?`, id, node).Scan(
		&rev, &install, &inboundRev, &listen, &direct, &swap, &savedDirect, &savedTunnel, &savedSwapNode, &lastEvent, &enc)
	if err != nil {
		return NativeRouteDraft{}, err
	}
	out, err := openRouteDraft(master, id, node, rev, enc)
	if err != nil {
		return NativeRouteDraft{}, err
	}
	if out.InstallationID != install || out.ExpectedInboundRevision != inboundRev ||
		out.ListenIP != listen || out.NativeDirectPort != direct || out.NativeSwapPort != swap ||
		out.DirectPort != savedDirect || out.TunnelPort != savedTunnel ||
		(out.SwapEnabled && out.SwapNodePort != savedSwapNode) ||
		(!out.SwapEnabled && savedSwapNode != 0) ||
		out.EventID != lastEvent {
		return NativeRouteDraft{}, ErrUnavailable
	}
	return out, nil
}
func (s *Store) NativeRouteDraft(ctx context.Context, master []byte, id uint64, node string) (NativeRouteDraft, error) {
	if id == 0 || id > (1<<53-1) || !idRE.MatchString(node) {
		return NativeRouteDraft{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return NativeRouteDraft{}, err
	}
	defer tx.Rollback()
	draft, err := readNativeRouteDraft(ctx, tx, master, id, node)
	if errors.Is(err, sql.ErrNoRows) {
		return NativeRouteDraft{}, ErrNotFound
	}
	if err != nil {
		return NativeRouteDraft{}, err
	}
	if err = tx.Commit(); err != nil {
		return NativeRouteDraft{}, err
	}
	return draft, nil
}
func nativeDraftPorts(in NativeRouteDraftInput) []uint16 {
	ports := []uint16{in.DirectPort, in.TunnelPort}
	if in.SwapEnabled {
		ports = append(ports, in.SwapNodePort)
	}
	return ports
}

// verifyDraft uses only the business DB's current Native Inbound bindings,
// without writing any runtime desired state. A registered Agent identity
// is independently checked by the owner API, never taken from browser JSON.
func verifyDraft(ctx context.Context, tx *sql.Tx, key []byte, in NativeRouteDraftInput) (NativeRouteDraftPreview, string, uint16, uint16, error) {
	out := NativeRouteDraftPreview{NodeID: in.NodeID, ChangedNodeOnly: true}
	var inboundRev uint64
	var enabled, archived int
	err := tx.QueryRowContext(ctx, `SELECT revision,enabled,archived
  FROM native_inbounds WHERE id=?`, in.InboundID).Scan(&inboundRev, &enabled, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return out, "", 0, 0, ErrNotFound
	}
	if err != nil {
		return out, "", 0, 0, err
	}
	if enabled != 1 || archived != 0 || inboundRev != in.ExpectedInboundRevision {
		return out, "", 0, 0, ErrConflict
	}
	var installation, listen string
	var direct, swap uint16
	err = tx.QueryRowContext(ctx, `SELECT installation_id,listen_ip,direct_port,swap_port
  FROM native_inbound_targets WHERE inbound_id=? AND node_id=?`, in.InboundID, in.NodeID).
		Scan(&installation, &listen, &direct, &swap)
	if errors.Is(err, sql.ErrNoRows) {
		return out, "", 0, 0, ErrNotFound
	}
	if err != nil {
		return out, "", 0, 0, err
	}
	if installation != in.InstallationID {
		return out, "", 0, 0, ErrConflict
	}
	ip := net.ParseIP(listen)
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return out, "", 0, 0, ErrConflict
	}
	if (direct != 0 && in.DirectPort == 0 || swap != 0 && in.TunnelPort == 0 &&
		(!in.SwapEnabled || in.SwapNodePort != swap)) && !in.ConfirmExistingRouteDisable {
		return out, "", 0, 0, ErrConflict
	}
	if in.SwapEnabled {
		var upstreamInstall string
		var upstreamDirect, upstreamSwap uint16
		err = tx.QueryRowContext(ctx, `SELECT installation_id,direct_port,swap_port
   FROM native_inbound_targets WHERE inbound_id=? AND node_id=?`,
			in.InboundID, in.SwapUpstreamNodeID).Scan(&upstreamInstall, &upstreamDirect, &upstreamSwap)
		if err != nil || upstreamInstall == "" || in.SwapTunnelPort != upstreamSwap {
			// This is a topology preflight, NOT proof that the upstream is a
			// provisioned tunnel. Actual Agent/Xray validation is a later gate.
			return out, "", 0, 0, ErrConflict
		}
	}
	if err := verifySwapDraftGraph(ctx, tx, key, in); err != nil {
		return out, "", 0, 0, err
	}
	proposed := map[uint16]bool{}
	for _, p := range nativeDraftPorts(in) {
		if p != 0 {
			proposed[p] = true
		}
	}
	// Never reserve an active port owned by another Native Inbound on
	// the same installation. Historical/replaced installations are distinct.
	rows, err := tx.QueryContext(ctx, `SELECT t.direct_port,t.swap_port
  FROM native_inbound_targets t JOIN native_inbounds i ON i.id=t.inbound_id
  WHERE t.node_id=? AND t.installation_id=? AND t.inbound_id!=? AND i.archived=0`,
		in.NodeID, in.InstallationID, in.InboundID)
	if err != nil {
		return out, "", 0, 0, err
	}
	for rows.Next() {
		var a, b uint16
		if err = rows.Scan(&a, &b); err != nil {
			rows.Close()
			return out, "", 0, 0, err
		}
		if proposed[a] && a != 0 || proposed[b] && b != 0 {
			rows.Close()
			return out, "", 0, 0, ErrConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, "", 0, 0, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT direct_port,tunnel_port,swap_node_port
  FROM native_route_drafts WHERE node_id=? AND installation_id=? AND inbound_id!=?`,
		in.NodeID, in.InstallationID, in.InboundID)
	if err != nil {
		return out, "", 0, 0, err
	}
	for rows.Next() {
		var a, b, c uint16
		if err = rows.Scan(&a, &b, &c); err != nil {
			rows.Close()
			return out, "", 0, 0, err
		}
		if proposed[a] && a != 0 || proposed[b] && b != 0 || proposed[c] && c != 0 {
			rows.Close()
			return out, "", 0, 0, ErrConflict
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, "", 0, 0, err
	}
	var current uint64
	err = tx.QueryRowContext(ctx, `SELECT revision FROM native_route_drafts
  WHERE inbound_id=? AND node_id=?`, in.InboundID, in.NodeID).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, "", 0, 0, err
	}
	if current != in.ExpectedDraftRevision {
		return out, "", 0, 0, ErrConflict
	}
	out.Valid = true
	out.CurrentDraftRevision = current
	out.NativeInboundRevision = inboundRev
	out.NativeDirectPort = direct
	out.NativeSwapPort = swap
	out.Reason = "owner_preview_only; no runtime configuration or billing changed"
	return out, ip.String(), direct, swap, nil
}
func (s *Store) PreviewNativeRouteDraft(ctx context.Context, key []byte, in NativeRouteDraftInput) (NativeRouteDraftPreview, error) {
	if len(key) != 32 {
		return NativeRouteDraftPreview{}, ErrInvalid
	}
	clean, err := checkDraftInput(in)
	if err != nil {
		return NativeRouteDraftPreview{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return NativeRouteDraftPreview{}, err
	}
	defer tx.Rollback()
	out, _, _, _, err := verifyDraft(ctx, tx, key, clean)
	if err != nil {
		return NativeRouteDraftPreview{}, err
	}
	if err = tx.Commit(); err != nil {
		return NativeRouteDraftPreview{}, err
	}
	return out, nil
}

// SaveNativeRouteDraft atomically stores ONLY a proposed route. It does not
// mutate native_inbound_targets or send anything to an Agent/Xray.
func (s *Store) SaveNativeRouteDraft(ctx context.Context, master []byte, input NativeRouteDraftInput) (NativeRouteDraftReceipt, error) {
	var empty NativeRouteDraftReceipt
	in, err := checkDraftInput(input)
	if err != nil {
		return empty, err
	}
	if len(master) != 32 {
		return empty, ErrInvalid
	}
	hash := fingerprint("native-route-draft-v1", in)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var knownHash string
	var knownInbound, knownRevision uint64
	var knownNode string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,inbound_id,node_id,revision
  FROM native_route_draft_events WHERE event_id=?`, in.EventID).Scan(
		&knownHash, &knownInbound, &knownNode, &knownRevision)
	if err == nil {
		if knownHash != hash || knownInbound != in.InboundID || knownNode != in.NodeID {
			return empty, ErrConflict
		}
		existing, e := readNativeRouteDraft(ctx, tx, master, in.InboundID, in.NodeID)
		if e != nil || existing.DraftRevision != knownRevision || existing.EventID != in.EventID {
			return empty, ErrConflict
		}
		if e = tx.Commit(); e != nil {
			return empty, e
		}
		return NativeRouteDraftReceipt{Draft: existing, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return empty, err
	}
	// Route event IDs also cannot collide with client/credit/representative,
	// product, lifecycle or other audited operations.
	for _, table := range []string{"events", "client_operations", "client_profile_edits",
		"representative_control_events", "catalog_events", "catalog_policy_events",
		"native_inbound_operations"} {
		var found int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE event_id=?", in.EventID).Scan(&found); err != nil {
			return empty, err
		}
		if found != 0 {
			return empty, ErrConflict
		}
	}
	preview, listen, nativeDirect, nativeSwap, err := verifyDraft(ctx, tx, master, in)
	if err != nil || !preview.Valid {
		return empty, err
	}
	now := time.Now().UTC().Unix()
	version := preview.CurrentDraftRevision + 1
	if version > (1<<53 - 1) {
		return empty, ErrConflict
	}
	created := now
	if preview.CurrentDraftRevision != 0 {
		if err = tx.QueryRowContext(ctx, `SELECT created_unix FROM native_route_drafts
   WHERE inbound_id=? AND node_id=?`, in.InboundID, in.NodeID).Scan(&created); err != nil {
			return empty, err
		}
	}
	// The destination Node is the ONLY intended billable source for Swap.
	// This is a draft declaration, not independently verified metering.
	draft := NativeRouteDraft{
		NativeRouteDraftInput: in, DraftRevision: version, ListenIP: listen,
		NativeDirectPort: nativeDirect, NativeSwapPort: nativeSwap,
		BillingSourceNodeID: in.NodeID, CreatedUnix: created, UpdatedUnix: now,
	}
	plain, err := json.Marshal(draft)
	if err != nil {
		return empty, err
	}
	enc, err := sealRouteDraft(master, in.InboundID, in.NodeID, version, plain)
	if err != nil {
		return empty, err
	}
	if preview.CurrentDraftRevision == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO native_route_drafts(
    inbound_id,node_id,installation_id,revision,inbound_revision,listen_ip,
    original_direct_port,original_swap_port,direct_port,tunnel_port,swap_node_port,
    last_event_id,payload_enc,created_unix,updated_unix)
    VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			in.InboundID, in.NodeID, in.InstallationID, version, in.ExpectedInboundRevision, listen,
			nativeDirect, nativeSwap, in.DirectPort, in.TunnelPort,
			func() uint16 {
				if in.SwapEnabled {
					return in.SwapNodePort
				}
				return 0
			}(),
			in.EventID, enc, created, now)
	} else {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE native_route_drafts SET
    installation_id=?,revision=?,inbound_revision=?,listen_ip=?,
    original_direct_port=?,original_swap_port=?,direct_port=?,tunnel_port=?,
    swap_node_port=?,last_event_id=?,payload_enc=?,updated_unix=?
    WHERE inbound_id=? AND node_id=? AND revision=?`,
			in.InstallationID, version, in.ExpectedInboundRevision, listen, nativeDirect, nativeSwap,
			in.DirectPort, in.TunnelPort, func() uint16 {
				if in.SwapEnabled {
					return in.SwapNodePort
				}
				return 0
			}(),
			in.EventID, enc, now, in.InboundID, in.NodeID, preview.CurrentDraftRevision)
		if err == nil {
			var affected int64
			affected, err = result.RowsAffected()
			if err == nil && affected != 1 {
				return empty, ErrConflict
			}
		}
	}
	if err != nil {
		return empty, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO native_route_draft_events(
   event_id,fingerprint,inbound_id,node_id,revision,created_unix)
   VALUES(?,?,?,?,?,?)`, in.EventID, hash, in.InboundID, in.NodeID, version, now)
	if err != nil {
		return empty, err
	}
	if err = tx.Commit(); err != nil {
		return empty, err
	}
	return NativeRouteDraftReceipt{Draft: draft}, nil
}

type NativeRouteDraftPreview struct {
	Valid                 bool   `json:"valid"`
	Reason                string `json:"reason"`
	ChangedNodeOnly       bool   `json:"changed_node_only"`
	ApplyAllowed          bool   `json:"apply_allowed"`
	AgentAckVerified      bool   `json:"agent_ack_verified"`
	DataPlaneVerified     bool   `json:"data_plane_verified"`
	BillingVerified       bool   `json:"billing_verified"`
	SubscriptionPublished bool   `json:"subscription_published"`
	CurrentDraftRevision  uint64 `json:"current_draft_revision"`
	NativeInboundRevision uint64 `json:"native_inbound_revision"`
	NativeDirectPort      uint16 `json:"native_direct_port"`
	NativeSwapPort        uint16 `json:"native_swap_port"`
	NodeID                string `json:"node_id"`
}
