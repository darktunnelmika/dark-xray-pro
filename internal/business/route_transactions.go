package business

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

var routeNodeID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
var routeID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var routeSHA = regexp.MustCompile(`^[a-f0-9]{64}$`)

const routeMax = 1<<53 - 1

type RouteDraftRef struct {
	InboundID       uint64 `json:"inbound_id"`
	InboundRevision uint64 `json:"inbound_revision"`
	DraftRevision   uint64 `json:"draft_revision"`
}
type RouteCommandRequest struct {
	ID              string              `json:"id"`
	Action          string              `json:"action"`
	StageID         string              `json:"stage_id"`
	RuntimeInstance string              `json:"runtime_instance"`
	Expected        mx.RuntimeCondition `json:"expected"`
	Drafts          []RouteDraftRef     `json:"drafts"`
}
type RouteCommandView struct {
	ID             string                 `json:"id"`
	NodeID         string                 `json:"node_id"`
	InstallationID string                 `json:"installation_id"`
	Serial         uint64                 `json:"serial"`
	Action         string                 `json:"action"`
	StageID        string                 `json:"stage_id"`
	State          string                 `json:"state"`
	Created        int64                  `json:"created_unix_nano"`
	Receipt        *mx.TransactionReceipt `json:"receipt,omitempty"`
}
type RouteChannelView struct {
	NodeID           string               `json:"node_id"`
	InstallationID   string               `json:"installation_id"`
	BinarySHA        string               `json:"binary_sha"`
	ReportedAt       int64                `json:"reported_at_unix_nano"`
	Status           mx.TransactionStatus `json:"status"`
	ObservationFresh bool                 `json:"observation_fresh"`
	Commands         []RouteCommandView   `json:"commands"`
}
type routePayload struct {
	Request      RouteCommandRequest   `json:"request"`
	DraftState   string                `json:"draft_state"`
	Generation   uint64                `json:"generation"`
	Command      mx.TransactionCommand `json:"command"`
	Candidate    xrayconfig.Candidate  `json:"candidate"`
	ConfigBase64 string                `json:"config_base64"`
}
type routeRow struct {
	view        RouteCommandView
	payload     routePayload
	fingerprint string
}

func ensureRouteTransactions(db *sql.DB) error {
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, q := range []string{
		`CREATE TABLE IF NOT EXISTS native_route_channels(node_id TEXT PRIMARY KEY,installation_id TEXT NOT NULL,binary_sha TEXT NOT NULL,serial INTEGER NOT NULL DEFAULT 0,observed_at INTEGER NOT NULL DEFAULT 0,observation TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS native_route_commands(id TEXT PRIMARY KEY,node_id TEXT NOT NULL REFERENCES native_route_channels(node_id),installation_id TEXT NOT NULL,serial INTEGER NOT NULL,action TEXT NOT NULL,stage_id TEXT NOT NULL,fingerprint TEXT NOT NULL,created INTEGER NOT NULL,state TEXT NOT NULL CHECK(state IN('queued','delivered','acknowledged','failed','acknowledged_stale','abandoned_unknown')),payload_enc BLOB NOT NULL,receipt TEXT NOT NULL DEFAULT '',UNIQUE(node_id,installation_id,serial))`,
		`CREATE UNIQUE INDEX IF NOT EXISTS native_route_one_pending ON native_route_commands(node_id) WHERE state IN('queued','delivered')`,
		`CREATE TRIGGER IF NOT EXISTS native_route_no_delete BEFORE DELETE ON native_route_commands BEGIN SELECT RAISE(ABORT,'route command audit immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_receipt_immutable BEFORE UPDATE OF receipt ON native_route_commands WHEN OLD.receipt!='' AND NEW.receipt!=OLD.receipt BEGIN SELECT RAISE(ABORT,'route receipt immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_delivery_immutable BEFORE UPDATE OF payload_enc ON native_route_commands WHEN OLD.state!='queued' BEGIN SELECT RAISE(ABORT,'delivered route command immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_claim_immutable BEFORE UPDATE OF node_id,installation_id,binary_sha ON native_route_channels BEGIN SELECT RAISE(ABORT,'route claim immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_claim_no_delete BEFORE DELETE ON native_route_channels BEGIN SELECT RAISE(ABORT,'route claim cannot be dropped'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_identity_immutable BEFORE UPDATE OF id,node_id,installation_id,serial,action,stage_id,fingerprint,created ON native_route_commands BEGIN SELECT RAISE(ABORT,'route command identity immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_block_native_insert BEFORE INSERT ON native_runtime_delivery WHEN EXISTS(SELECT 1 FROM native_route_channels WHERE node_id=NEW.node_id) BEGIN SELECT RAISE(ABORT,'route executor owns node'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_block_native_update BEFORE UPDATE ON native_runtime_delivery WHEN EXISTS(SELECT 1 FROM native_route_channels WHERE node_id=NEW.node_id) BEGIN SELECT RAISE(ABORT,'route executor owns node'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_block_customer_binding BEFORE INSERT ON native_inbound_clients WHEN EXISTS(SELECT 1 FROM native_inbound_targets t JOIN native_route_channels c ON c.node_id=t.node_id WHERE t.inbound_id=NEW.inbound_id) BEGIN SELECT RAISE(ABORT,'route LAB forbids customer bindings'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_block_bound_target BEFORE INSERT ON native_inbound_targets WHEN EXISTS(SELECT 1 FROM native_route_channels WHERE node_id=NEW.node_id) AND (EXISTS(SELECT 1 FROM native_inbound_clients WHERE inbound_id=NEW.inbound_id) OR NEW.listen_ip NOT IN('127.0.0.1','::1')) BEGIN SELECT RAISE(ABORT,'route LAB target rejected'); END`,
		`CREATE TRIGGER IF NOT EXISTS native_route_block_bound_target_update BEFORE UPDATE ON native_inbound_targets WHEN EXISTS(SELECT 1 FROM native_route_channels WHERE node_id=NEW.node_id) AND (EXISTS(SELECT 1 FROM native_inbound_clients WHERE inbound_id=NEW.inbound_id) OR NEW.listen_ip NOT IN('127.0.0.1','::1')) BEGIN SELECT RAISE(ABORT,'route LAB target rejected'); END`,
	} {
		if _, e = tx.Exec(q); e != nil {
			return e
		}
	}
	return tx.Commit()
}
func (s *Store) RouteChannelOwned(ctx context.Context, node string) (bool, error) {
	var n int
	e := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM native_route_channels WHERE node_id=?", node).Scan(&n)
	return n != 0, e
}

// Claim is durable and cannot switch an ordinary/metered Node into an unmetered
// executor. Only a never-delivered loopback Node with zero bindings qualifies.
func (s *Store) ClaimRouteChannel(ctx context.Context, node, install, binary string) error {
	if !routeNodeID.MatchString(node) || !routeID.MatchString(install) || !routeSHA.MatchString(binary) {
		return ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var oldInstall, oldBinary string
	e = tx.QueryRowContext(ctx, "SELECT installation_id,binary_sha FROM native_route_channels WHERE node_id=?", node).Scan(&oldInstall, &oldBinary)
	if e == nil {
		if oldInstall != install || oldBinary != binary {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	var n int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM native_runtime_delivery WHERE node_id=?", node).Scan(&n); e != nil {
		return e
	}
	if n != 0 {
		return ErrConflict
	}
	if s.usage != nil {
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=?", node).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return ErrConflict
		}
	}
	if e = routeEmptyNode(ctx, tx, node, install); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO native_route_channels(node_id,installation_id,binary_sha) VALUES(?,?,?)", node, install, binary)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func routeEmptyNode(ctx context.Context, tx *sql.Tx, node, install string) error {
	var total, bad int
	e := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN t.installation_id!=? OR t.listen_ip NOT IN('127.0.0.1','::1') OR EXISTS(SELECT 1 FROM native_inbound_clients c WHERE c.inbound_id=t.inbound_id) THEN 1 ELSE 0 END),0) FROM native_inbound_targets t WHERE t.node_id=?`, install, node).Scan(&total, &bad)
	if e != nil {
		return e
	}
	if total == 0 || total > 256 || bad != 0 {
		return ErrConflict
	}
	return nil
}
func routeCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrInvalid
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte("DARK-PRO-HUB-ROUTE-QUEUE-v1"))
	b, e := aes.NewCipher(h.Sum(nil))
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(b)
}
func routeAAD(v RouteCommandView) []byte {
	return []byte(fmt.Sprintf("route-command:%s:%s:%s:%d", v.ID, v.NodeID, v.InstallationID, v.Serial))
}
func sealRouteCommand(key []byte, v RouteCommandView, p routePayload) ([]byte, error) {
	a, e := routeCipher(key)
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(p)
	if e != nil || len(raw) > 2*xrayconfig.MaxCandidateBytes+65536 {
		return nil, ErrInvalid
	}
	n := make([]byte, a.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return nil, e
	}
	return a.Seal(n, n, raw, routeAAD(v)), nil
}
func readRouteCommand(ctx context.Context, tx *sql.Tx, key []byte, id string) (routeRow, error) {
	var r routeRow
	var encrypted []byte
	var receipt string
	v := &r.view
	e := tx.QueryRowContext(ctx, `SELECT id,node_id,installation_id,serial,action,stage_id,fingerprint,created,state,payload_enc,receipt FROM native_route_commands WHERE id=?`, id).Scan(&v.ID, &v.NodeID, &v.InstallationID, &v.Serial, &v.Action, &v.StageID, &r.fingerprint, &v.Created, &v.State, &encrypted, &receipt)
	if errors.Is(e, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	a, e := routeCipher(key)
	if e != nil {
		return r, e
	}
	n := a.NonceSize()
	if len(encrypted) < n+a.Overhead() {
		return r, ErrUnavailable
	}
	raw, e := a.Open(nil, encrypted[:n], encrypted[n:], routeAAD(*v))
	if e != nil || json.Unmarshal(raw, &r.payload) != nil {
		return r, ErrUnavailable
	}
	c := r.payload.Command
	if c.ID != v.ID || c.NodeID != v.NodeID || c.InstallationID != v.InstallationID || c.Serial != v.Serial || c.Action != v.Action || c.StageID != v.StageID {
		return r, ErrUnavailable
	}
	if receipt != "" {
		var ack mx.TransactionReceipt
		if json.Unmarshal([]byte(receipt), &ack) != nil || ack.CommandID != v.ID {
			return r, ErrUnavailable
		}
		v.Receipt = &ack
	}
	return r, nil
}
func routeObservation(ctx context.Context, tx *sql.Tx, node string) (RouteChannelView, uint64, error) {
	var out RouteChannelView
	var serial uint64
	var raw string
	out.NodeID = node
	e := tx.QueryRowContext(ctx, "SELECT installation_id,binary_sha,serial,observed_at,observation FROM native_route_channels WHERE node_id=?", node).Scan(&out.InstallationID, &out.BinarySHA, &serial, &out.ReportedAt, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return out, 0, ErrNotFound
	}
	if e != nil {
		return out, 0, e
	}
	if raw != "" && json.Unmarshal([]byte(raw), &out.Status) != nil {
		return out, 0, ErrUnavailable
	}
	now := time.Now().UnixNano()
	out.ObservationFresh = out.ReportedAt > 0 && out.ReportedAt <= now+int64(time.Second) && now-out.ReportedAt < int64(12*time.Second)
	return out, serial, nil
}

// All enabled targets must be reviewed together; private profiles are rendered
// from the same SQL snapshot. No browser-supplied Xray bytes are accepted.
func routeCandidate(ctx context.Context, tx *sql.Tx, key []byte, node, install string, refs []RouteDraftRef) (xrayconfig.Candidate, error) {
	if e := routeEmptyNode(ctx, tx, node, install); e != nil {
		return xrayconfig.Candidate{}, e
	}
	wanted := map[uint64]RouteDraftRef{}
	for _, r := range refs {
		if r.InboundID == 0 || r.DraftRevision == 0 || r.InboundRevision == 0 || wanted[r.InboundID].InboundID != 0 {
			return xrayconfig.Candidate{}, ErrInvalid
		}
		wanted[r.InboundID] = r
	}
	type src struct {
		id  uint64
		rev uint64
		enc []byte
		ip  string
	}
	var sources []src
	rows, e := tx.QueryContext(ctx, `SELECT i.id,i.revision,i.profile_enc,t.listen_ip FROM native_inbound_targets t JOIN native_inbounds i ON i.id=t.inbound_id WHERE t.node_id=? AND t.installation_id=? AND i.enabled=1 AND i.archived=0 ORDER BY i.id`, node, install)
	if e != nil {
		return xrayconfig.Candidate{}, e
	}
	for rows.Next() {
		var x src
		if e = rows.Scan(&x.id, &x.rev, &x.enc, &x.ip); e != nil {
			rows.Close()
			return xrayconfig.Candidate{}, e
		}
		sources = append(sources, x)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return xrayconfig.Candidate{}, e
	}
	if len(sources) == 0 || len(sources) != len(wanted) {
		return xrayconfig.Candidate{}, ErrConflict
	}
	var assignments []xrayconfig.Assignment
	listen := ""
	for _, x := range sources {
		ref, ok := wanted[x.id]
		if !ok || ref.InboundRevision != x.rev {
			return xrayconfig.Candidate{}, ErrConflict
		}
		d, e := readNativeRouteDraft(ctx, tx, key, x.id, node)
		if e != nil {
			return xrayconfig.Candidate{}, e
		}
		if d.DraftRevision != ref.DraftRevision || d.ExpectedInboundRevision != ref.InboundRevision || d.InstallationID != install || d.SwapEnabled {
			return xrayconfig.Candidate{}, ErrConflict
		}
		in := d.NativeRouteDraftInput
		in.ExpectedDraftRevision = d.DraftRevision
		if _, _, _, _, e = verifyDraft(ctx, tx, key, in); e != nil {
			return xrayconfig.Candidate{}, e
		}
		ip := net.ParseIP(x.ip)
		if ip == nil || !ip.IsLoopback() || listen != "" && listen != x.ip {
			return xrayconfig.Candidate{}, ErrConflict
		}
		listen = x.ip
		p, e := openProfile(key, x.enc, x.id)
		if e != nil {
			return xrayconfig.Candidate{}, e
		}
		p.Users = []xrayconfig.User{}
		assignments = append(assignments, xrayconfig.Assignment{Profile: p, DirectPort: d.DirectPort, SwapPort: d.TunnelPort})
	}
	c, e := xrayconfig.RenderAssigned(node, install, listen, assignments)
	if e != nil {
		return c, ErrInvalid
	}
	return c, nil
}
func routeGeneration(ctx context.Context, tx *sql.Tx) (uint64, error) {
	var g uint64
	e := tx.QueryRowContext(ctx, "SELECT value FROM native_config_generation WHERE id=1").Scan(&g)
	return g, e
}
func routeStillCurrent(ctx context.Context, tx *sql.Tx, key []byte, row routeRow) error {
	draftState, e := routeDraftState(ctx, tx, row.view.NodeID)
	if e != nil {
		return e
	}
	if draftState != row.payload.DraftState {
		return ErrConflict
	}
	g, e := routeGeneration(ctx, tx)
	if e != nil {
		return e
	}
	if g != row.payload.Generation {
		return ErrConflict
	}
	if e = routeEmptyNode(ctx, tx, row.view.NodeID, row.view.InstallationID); e != nil {
		return e
	}
	if row.view.Action == "rollback" {
		return nil
	}
	c, e := routeCandidate(ctx, tx, key, row.view.NodeID, row.view.InstallationID, row.payload.Request.Drafts)
	if e != nil {
		return e
	}
	if mx.TransactionPayloadSHA(c) != row.payload.Command.PayloadSHA {
		return ErrConflict
	}
	return nil
}

func (s *Store) QueueRouteCommand(ctx context.Context, key []byte, node string, in RouteCommandRequest) (RouteCommandView, error) {
	empty := RouteCommandView{}
	if !routeID.MatchString(in.ID) || !routeID.MatchString(in.RuntimeInstance) || len(in.Drafts) > 256 {
		return empty, ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return empty, e
	}
	defer tx.Rollback()
	prior, e := readRouteCommand(ctx, tx, key, in.ID)
	fp := fingerprint("route-command-request-v1", struct {
		Node    string
		Request RouteCommandRequest
	}{node, in})
	if e == nil {
		if prior.fingerprint != fp {
			return empty, ErrConflict
		}
		return prior.view, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return empty, e
	}
	channel, serial, e := routeObservation(ctx, tx, node)
	if e != nil {
		return empty, e
	}
	if !channel.ObservationFresh || channel.Status.Instance != in.RuntimeInstance || mx.Condition(channel.Status.Observed) != in.Expected {
		return empty, ErrConflict
	}
	if serial < channel.Status.Serial {
		serial = channel.Status.Serial
	}
	if serial >= routeMax || in.Expected.Highwater >= routeMax {
		return empty, ErrConflict
	}
	var pending, count int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM native_route_commands WHERE node_id=? AND state IN('queued','delivered')", node).Scan(&pending); e != nil {
		return empty, e
	}
	if pending != 0 {
		return empty, ErrConflict
	}
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM native_route_commands WHERE node_id=?", node).Scan(&count); e != nil {
		return empty, e
	}
	if count >= 10000 {
		return empty, ErrConflict
	}
	generation, e := routeGeneration(ctx, tx)
	if e != nil {
		return empty, e
	}
	now := time.Now().UnixNano()
	cmd := mx.TransactionCommand{Version: 1, RuntimeInstance: in.RuntimeInstance, ID: in.ID, Serial: serial + 1, Action: in.Action, NodeID: node, InstallationID: channel.InstallationID, BinarySHA: channel.BinarySHA, Expected: in.Expected, Issued: now, Expires: now + int64(2*time.Minute)}
	draftState, e := routeDraftState(ctx, tx, node)
	if e != nil {
		return empty, e
	}
	payload := routePayload{DraftState: draftState, Request: in, Generation: generation, Command: cmd}
	var candidate xrayconfig.Candidate
	switch in.Action {
	case "stage":
		if in.StageID != "" {
			return empty, ErrInvalid
		}
		candidate, e = routeCandidate(ctx, tx, key, node, channel.InstallationID, in.Drafts)
		if e != nil {
			return empty, e
		}
		cmd.StageID = cmd.ID
		cmd.RuntimeSequence = in.Expected.Highwater + 1
		cmd.InterruptedCommandSHA = channel.Status.InterruptedCommandSHA
	case "apply", "rollback", "renew":
		if len(in.Drafts) != 0 || !routeID.MatchString(in.StageID) || channel.Status.InterruptedCommandSHA != "" {
			return empty, ErrConflict
		}
		stage, e := readRouteCommand(ctx, tx, key, in.StageID)
		if e != nil {
			return empty, e
		}
		if stage.view.NodeID != node || stage.view.InstallationID != channel.InstallationID || stage.view.Action != "stage" || stage.view.State != "acknowledged" || stage.view.Receipt == nil || stage.view.Receipt.Outcome != "staged" {
			return empty, ErrConflict
		}
		raw, e := base64.StdEncoding.Strict().DecodeString(stage.payload.ConfigBase64)
		if e != nil {
			return empty, ErrUnavailable
		}
		candidate = stage.payload.Candidate
		candidate.Config = raw
		payload.Request.Drafts = stage.payload.Request.Drafts
		cmd.StageID = in.StageID
		cmd.RuntimeSequence = in.Expected.Highwater + 1
		if in.Action == "apply" {
			if channel.Status.StagedID != in.StageID || cmd.Expected != stage.payload.Command.Expected || stage.payload.Command.Expires <= now {
				return empty, ErrConflict
			}
			cmd.RuntimeSequence = stage.payload.Command.RuntimeSequence
			cmd.Expires = stage.payload.Command.Expires
		}
		if in.Action == "renew" {
			if channel.Status.CurrentStageID != in.StageID || !channel.Status.Observed.ProcessReady || channel.Status.Observed.ActiveSHA != candidate.SHA256 {
				return empty, ErrConflict
			}
			cmd.RuntimeSequence = channel.Status.Observed.ActiveSequence
		}
		if in.Action == "rollback" {
			o := channel.Status.Observed
			if in.StageID != channel.Status.PreviousStageID && (o.ProcessReady || in.StageID != channel.Status.CurrentStageID || candidate.SHA256 != o.SavedSHA) {
				return empty, ErrConflict
			}
		}
	default:
		return empty, ErrInvalid
	}
	cmd.PayloadSHA = mx.TransactionPayloadSHA(candidate)
	payload.Command = cmd
	payload.Candidate = candidate
	payload.ConfigBase64 = base64.StdEncoding.EncodeToString(candidate.Config)
	view := RouteCommandView{ID: cmd.ID, NodeID: node, InstallationID: cmd.InstallationID, Serial: cmd.Serial, Action: cmd.Action, StageID: cmd.StageID, State: "queued", Created: now}
	row := routeRow{view: view, payload: payload}
	if e = routeStillCurrent(ctx, tx, key, row); e != nil {
		return empty, e
	}
	enc, e := sealRouteCommand(key, view, payload)
	if e != nil {
		return empty, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO native_route_commands(id,node_id,installation_id,serial,action,stage_id,fingerprint,created,state,payload_enc) VALUES(?,?,?,?,?,?,?,?,?,?)`, view.ID, node, view.InstallationID, view.Serial, view.Action, view.StageID, fp, now, view.State, enc)
	if e != nil {
		return empty, ErrConflict
	}
	if _, e = tx.ExecContext(ctx, "UPDATE native_route_channels SET serial=? WHERE node_id=?", cmd.Serial, node); e != nil {
		return empty, e
	}
	if e = tx.Commit(); e != nil {
		return empty, e
	}
	return view, nil
}

func (s *Store) PollRouteCommand(ctx context.Context, key, token []byte, p mx.TransactionPoll) (mx.TransactionDelivery, bool, error) {
	none := mx.TransactionDelivery{}
	now := time.Now().UnixNano()
	if !mx.VerifyTransactionPoll(token, p) || p.At > now+int64(time.Second) || now-p.At > int64(12*time.Second) || !validObservation(p.Status.Observed) || p.Status.Observed.Usage != nil {
		return none, false, ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return none, false, e
	}
	defer tx.Rollback()
	ch, _, e := routeObservation(ctx, tx, p.NodeID)
	if e != nil {
		return none, false, e
	}
	if ch.InstallationID != p.InstallationID || ch.BinarySHA != p.BinarySHA || p.At <= ch.ReportedAt {
		return none, false, ErrConflict
	}
	raw, _ := json.Marshal(p.Status)
	if _, e = tx.ExecContext(ctx, "UPDATE native_route_channels SET observed_at=?,observation=? WHERE node_id=?", p.At, string(raw), p.NodeID); e != nil {
		return none, false, e
	}
	var id string
	e = tx.QueryRowContext(ctx, "SELECT id FROM native_route_commands WHERE node_id=? AND state IN('queued','delivered')", p.NodeID).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return none, false, tx.Commit()
	}
	if e != nil {
		return none, false, e
	}
	row, e := readRouteCommand(ctx, tx, key, id)
	if e != nil {
		return none, false, e
	}
	// Persist the fresh observation even when stale work is refused, so the
	// owner can abandon an ambiguous command and explicitly restage/recover.
	refuse := func() (mx.TransactionDelivery, bool, error) {
		if e := tx.Commit(); e != nil {
			return none, false, e
		}
		return none, false, ErrConflict
	}
	if e = routeStillCurrent(ctx, tx, key, row); e != nil {
		return refuse()
	}
	c := row.payload.Command
	if row.view.State == "queued" {
		if row.payload.Request.RuntimeInstance != p.Status.Instance || mx.Condition(p.Status.Observed) != c.Expected || p.Status.Serial >= c.Serial || c.Expires <= now {
			return refuse()
		}
		if c.Action != "stage" {
			expiry := now + int64(30*time.Second)
			if expiry > c.Expires {
				expiry = c.Expires
			}
			c.Lease, e = mx.SignLease(token, mx.Lease{NodeID: c.NodeID, InstallationID: c.InstallationID, Sequence: c.RuntimeSequence, ConfigSHA: row.payload.Candidate.SHA256, ManifestSHA: row.payload.Candidate.ManifestSHA256, Issued: now, Expires: expiry})
			if e != nil {
				return none, false, ErrConflict
			}
		}
		c, e = mx.SignTransactionCommand(token, c)
		if e != nil {
			return none, false, ErrInvalid
		}
		row.payload.Command = c
		enc, e := sealRouteCommand(key, row.view, row.payload)
		if e != nil {
			return none, false, e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE native_route_commands SET state='delivered',payload_enc=? WHERE id=? AND state='queued'", enc, id); e != nil {
			return none, false, e
		}
	}
	out := mx.TransactionDelivery{Command: c}
	if c.Action == "stage" {
		out.Candidate = row.payload.Candidate
		out.ConfigBase64 = row.payload.ConfigBase64
	}
	if e = tx.Commit(); e != nil {
		return none, false, e
	}
	return out, true, nil
}

func (s *Store) AcceptRouteReceipt(ctx context.Context, key, token []byte, ack mx.TransactionReceipt) error {
	if !routeID.MatchString(ack.CommandID) {
		return ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	row, e := readRouteCommand(ctx, tx, key, ack.CommandID)
	if e != nil {
		return e
	}
	c := row.payload.Command
	if ack.RuntimeInstance != row.payload.Request.RuntimeInstance || !mx.VerifyTransactionReceipt(token, c, ack) || !validObservation(ack.Observed) || ack.ObservedAt < c.Issued || ack.ObservedAt > time.Now().Add(time.Second).UnixNano() {
		return ErrInvalid
	}
	if row.view.Receipt != nil {
		old, _ := json.Marshal(row.view.Receipt)
		raw, _ := json.Marshal(ack)
		if string(old) != string(raw) {
			return ErrConflict
		}
		return nil
	}
	if row.view.State != "delivered" && row.view.State != "abandoned_unknown" {
		return ErrConflict
	}
	state := "acknowledged"
	if ack.Outcome == "failed_stopped" || ack.Outcome == "failed_previous_retained" {
		state = "failed"
	}
	if routeStillCurrent(ctx, tx, key, row) != nil {
		state = "acknowledged_stale"
	}
	if row.view.State == "abandoned_unknown" {
		state = row.view.State
	}
	raw, _ := json.Marshal(ack)
	_, e = tx.ExecContext(ctx, "UPDATE native_route_commands SET state=?,receipt=? WHERE id=?", state, string(raw), c.ID)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// Abandon never claims non-execution or stops a Node. Short leases still fence
// its process; recovery needs a fresh signed observation and explicit Stage.
func (s *Store) AbandonRouteCommand(ctx context.Context, node, id string) error {
	if !routeID.MatchString(id) {
		return ErrInvalid
	}
	result, e := s.db.ExecContext(ctx, "UPDATE native_route_commands SET state='abandoned_unknown' WHERE id=? AND node_id=? AND state IN('queued','delivered','abandoned_unknown')", id, node)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return ErrConflict
	}
	return nil
}
func (s *Store) RouteChannel(ctx context.Context, key []byte, node string) (RouteChannelView, error) {
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return RouteChannelView{}, e
	}
	defer tx.Rollback()
	out, _, e := routeObservation(ctx, tx, node)
	if e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT id FROM native_route_commands WHERE node_id=? ORDER BY serial DESC LIMIT 50", node)
	if e != nil {
		return out, e
	}
	var ids []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	out.Commands = make([]RouteCommandView, 0, len(ids))
	for _, id := range ids {
		r, e := readRouteCommand(ctx, tx, key, id)
		if e != nil {
			return out, e
		}
		out.Commands = append(out.Commands, r.view)
	}
	return out, tx.Commit()
}

func routeDraftState(ctx context.Context, tx *sql.Tx, node string) (string, error) {
	rows, e := tx.QueryContext(ctx, "SELECT inbound_id,revision,last_event_id FROM native_route_drafts WHERE node_id=? ORDER BY inbound_id", node)
	if e != nil {
		return "", e
	}
	defer rows.Close()
	var refs []struct {
		ID, Revision uint64
		Event        string
	}
	for rows.Next() {
		var r struct {
			ID, Revision uint64
			Event        string
		}
		if e = rows.Scan(&r.ID, &r.Revision, &r.Event); e != nil {
			return "", e
		}
		refs = append(refs, r)
	}
	if e = rows.Err(); e != nil {
		return "", e
	}
	return fingerprint("route-draft-snapshot", refs), nil
}
