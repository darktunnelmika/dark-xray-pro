package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

const MaxOwnershipBatch = 500

var ErrOwnershipDenied = errors.New("client_ownership_permission_denied")

type ClientOwnershipIntent struct {
	ClientIDs        []string `json:"client_ids"`
	SourceGroupID    string   `json:"source_group_id"`
	RepresentativeID string   `json:"representative_id"`
	GroupID          string   `json:"group_id"`
	Reason           string   `json:"reason"`
}
type ClientOwnershipCommit struct {
	ClientOwnershipIntent
	EventID   string `json:"event_id"`
	QuoteHash string `json:"quote_hash"`
	QuoteUnix int64  `json:"quote_unix"`
}
type ClientOwnershipItem struct {
	ClientID                 string `json:"client_id"`
	Name                     string `json:"name"`
	PreviousRepresentativeID string `json:"previous_representative_id"`
	RepresentativeID         string `json:"representative_id"`
	PreviousGroupID          string `json:"previous_group_id"`
	GroupID                  string `json:"group_id"`
	Revision                 int64  `json:"revision"`
	UsagePresent             bool   `json:"usage_present"`
	UsageComplete            bool   `json:"usage_complete"`
	Uplink                   int64  `json:"uplink_bytes,string"`
	Downlink                 int64  `json:"downlink_bytes,string"`
	Holds                    int64  `json:"reconciliation_holds"`
}
type ClientOwnershipQuote struct {
	Intent         ClientOwnershipIntent `json:"intent"`
	QuoteHash      string                `json:"quote_hash"`
	QuoteUnix      int64                 `json:"quote_unix"`
	Count          int                   `json:"count"`
	Clients        []ClientOwnershipItem `json:"clients"`
	RefundPolicy   string                `json:"refund_policy"`
	BalanceChanged bool                  `json:"balance_changed"`
	UsageReset     bool                  `json:"usage_reset"`
}
type ClientOwnershipReceipt struct {
	EventID            string                `json:"event_id"`
	ActorUserID        string                `json:"actor_user_id"`
	RepresentativeID   string                `json:"representative_id"`
	GroupID            string                `json:"group_id"`
	Count              int                   `json:"count"`
	Clients            []ClientOwnershipItem `json:"clients"`
	CreatedUnix        int64                 `json:"created_unix"`
	Replayed           bool                  `json:"replayed"`
	BalanceChanged     bool                  `json:"balance_changed"`
	UsageReset         bool                  `json:"usage_reset"`
	CredentialsChanged bool                  `json:"credentials_changed"`
}
type ownershipSnapshot struct {
	Actor         string
	Intent        ClientOwnershipIntent
	At            int64
	Target        string
	TargetEnabled bool
	Group         Group
	SourceGroup   Group
	Clients       []Client
	Groups        []string
	Revisions     []int64
}

func canonicalOwnership(input ClientOwnershipIntent) (ClientOwnershipIntent, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !idRE.MatchString(input.RepresentativeID) || !validName(input.Reason, 200) || input.GroupID != "" && !idRE.MatchString(input.GroupID) || input.SourceGroupID != "" && !idRE.MatchString(input.SourceGroupID) {
		return input, ErrInvalid
	}
	if (input.SourceGroupID != "") == (len(input.ClientIDs) > 0) || len(input.ClientIDs) > MaxOwnershipBatch {
		return input, ErrInvalid
	}
	input.ClientIDs = append([]string{}, input.ClientIDs...)
	sort.Strings(input.ClientIDs)
	for i, id := range input.ClientIDs {
		if !idRE.MatchString(id) || i > 0 && id == input.ClientIDs[i-1] {
			return input, ErrInvalid
		}
	}
	return input, nil
}
func ownershipOwner(ctx context.Context, tx *sql.Tx, token string) (string, error) {
	if len(token) != 64 {
		return "", ErrUnauthenticated
	}
	var actor, role, rep string
	var expiry int64
	var disabled, enabled int
	e := tx.QueryRowContext(ctx, `SELECT a.id,a.role,a.representative_id,ss.expires_unix,a.disabled,r.enabled FROM auth_sessions ss JOIN auth_users a ON a.id=ss.user_id JOIN representatives r ON r.id=a.representative_id WHERE ss.token_sha256=?`, digestSession(token)).Scan(&actor, &role, &rep, &expiry, &disabled, &enabled)
	if errors.Is(e, sql.ErrNoRows) || e == nil && (expiry <= time.Now().Unix() || disabled != 0 || enabled != 1) {
		return "", ErrUnauthenticated
	}
	if e != nil {
		return "", ErrUnavailable
	}
	if role != "owner" || rep != "owner" {
		return "", ErrOwnershipDenied
	}
	return actor, nil
}
func readOwnershipGroup(ctx context.Context, tx *sql.Tx, id string) (Group, error) {
	var g Group
	if id == "" {
		return g, nil
	}
	e := tx.QueryRowContext(ctx, `SELECT id,representative_id,name,color,created_unix,updated_unix FROM client_groups WHERE id=?`, id).Scan(&g.ID, &g.RepresentativeID, &g.Name, &g.Color, &g.CreatedUnix, &g.UpdatedUnix)
	if errors.Is(e, sql.ErrNoRows) {
		return g, ErrNotFound
	}
	return g, e
}
func ownershipQuoteTx(ctx context.Context, tx *sql.Tx, actor string, intent ClientOwnershipIntent, at int64) (ClientOwnershipQuote, []Client, error) {
	out := ClientOwnershipQuote{Intent: intent, QuoteUnix: at, RefundPolicy: "original_funder_only_mixed_funding_requires_review", Clients: []ClientOwnershipItem{}}
	present, e := ownershipSchemaState(ctx, tx)
	if e != nil || !present {
		return out, nil, ErrUnavailable
	}
	target, e := readRep(ctx, tx, intent.RepresentativeID)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil, ErrNotFound
	}
	if e != nil {
		return out, nil, e
	}
	if !target.Enabled || (target.Kind != "owner" && target.Kind != "representative") {
		return out, nil, ErrConflict
	}
	group, e := readOwnershipGroup(ctx, tx, intent.GroupID)
	if e != nil {
		return out, nil, e
	}
	if group.ID != "" && group.RepresentativeID != target.ID {
		return out, nil, ErrConflict
	}
	source, e := readOwnershipGroup(ctx, tx, intent.SourceGroupID)
	if e != nil {
		return out, nil, e
	}
	ids := intent.ClientIDs
	if source.ID != "" {
		rows, e := tx.QueryContext(ctx, `SELECT client_id FROM client_group_members WHERE group_id=? ORDER BY client_id LIMIT ?`, source.ID, MaxOwnershipBatch+1)
		if e != nil {
			return out, nil, e
		}
		ids = []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return out, nil, e
			}
			ids = append(ids, id)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return out, nil, e
		}
	}
	if len(ids) < 1 || len(ids) > MaxOwnershipBatch {
		return out, nil, ErrInvalid
	}
	snapshot := ownershipSnapshot{Actor: actor, Intent: intent, At: at, Target: target.ID, TargetEnabled: target.Enabled, Group: group, SourceGroup: source}
	for _, id := range ids {
		c, e := readClient(ctx, tx, id)
		if errors.Is(e, sql.ErrNoRows) {
			return out, nil, ErrNotFound
		}
		if e != nil {
			return out, nil, e
		}
		if c.RepresentativeID == target.ID {
			return out, nil, ErrConflict
		}
		prior, e := readRep(ctx, tx, c.RepresentativeID)
		if e != nil {
			return out, nil, e
		}
		// An ownership action must not silently reactivate traffic of a disabled
		// account. Such clients must first be explicitly disabled/archived.
		if !prior.Enabled && !c.Archived && c.DeploymentState != "disabled" {
			return out, nil, ErrConflict
		}
		_, rev, e := ownershipOriginKnown(ctx, tx, c)
		if e != nil {
			return out, nil, e
		}
		if rev >= 9007199254740990 {
			return out, nil, ErrConflict
		}
		item := ClientOwnershipItem{ClientID: id, Name: c.Name, PreviousRepresentativeID: c.RepresentativeID, RepresentativeID: target.ID, GroupID: group.ID, Revision: rev + 1}
		e = tx.QueryRowContext(ctx, `SELECT group_id FROM client_group_members WHERE client_id=?`, id).Scan(&item.PreviousGroupID)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, nil, e
		}
		if source.ID != "" && (item.PreviousGroupID != source.ID || source.RepresentativeID != c.RepresentativeID) {
			return out, nil, ErrConflict
		}
		if item.PreviousGroupID != "" {
			g, e := readOwnershipGroup(ctx, tx, item.PreviousGroupID)
			if e != nil || g.RepresentativeID != c.RepresentativeID {
				return out, nil, ErrConflict
			}
		}
		e = tx.QueryRowContext(ctx, `SELECT uplink,downlink,baseline_complete,holds FROM pro_usage_accounts WHERE client_id=?`, id).Scan(&item.Uplink, &item.Downlink, &item.UsageComplete, &item.Holds)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return out, nil, e
		}
		item.UsagePresent = e == nil
		out.Clients = append(out.Clients, item)
		snapshot.Clients = append(snapshot.Clients, c)
		snapshot.Groups = append(snapshot.Groups, item.PreviousGroupID)
		snapshot.Revisions = append(snapshot.Revisions, rev)
	}
	// Usage can advance while an owner reviews the quote. It is never reset;
	// the receipt captures its exact commit-time baseline, not the preview value.
	out.Count = len(out.Clients)
	out.QuoteHash = fingerprint("client-ownership-quote-v1", snapshot)
	return out, snapshot.Clients, nil
}
func (s *Store) QuoteClientOwnership(ctx context.Context, token string, input ClientOwnershipIntent) (ClientOwnershipQuote, error) {
	intent, e := canonicalOwnership(input)
	if e != nil {
		return ClientOwnershipQuote{}, e
	}
	if s.usage == nil {
		return ClientOwnershipQuote{}, ErrUnavailable
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return ClientOwnershipQuote{}, e
	}
	defer tx.Rollback()
	actor, e := ownershipOwner(ctx, tx, token)
	if e != nil {
		return ClientOwnershipQuote{}, e
	}
	quote, _, e := ownershipQuoteTx(ctx, tx, actor, intent, time.Now().Unix())
	return quote, e
}
func ownershipRequestFingerprint(actor string, req ClientOwnershipCommit) string {
	return fingerprint("client-ownership-commit-v1", struct {
		Actor   string
		Request ClientOwnershipCommit
	}{actor, req})
}
func newOwnershipEvent(ctx context.Context, tx *sql.Tx, event string) error {
	for _, table := range []string{"darkrestore_import_receipts_v1", "events", "client_operations", "client_profile_edits", "representative_control_events", "catalog_events", "catalog_policy_events", "seller_quota_events_v2", "representative_seller_permission_events_v2", "seller_state_events_v2", "seller_order_receipts_v1"} {
		var present, n int
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&present); e != nil {
			return e
		}
		if present == 0 {
			continue
		}
		if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE event_id=?`, event).Scan(&n); e != nil {
			return e
		}
		if n != 0 {
			return ErrConflict
		}
	}
	return nil
}
func (s *Store) CommitClientOwnership(ctx context.Context, token string, input ClientOwnershipCommit) (ClientOwnershipReceipt, error) {
	var out ClientOwnershipReceipt
	intent, e := canonicalOwnership(input.ClientOwnershipIntent)
	if e != nil || !eventRE.MatchString(input.EventID) || len(input.QuoteHash) != 64 {
		return out, ErrInvalid
	}
	if _, e = hex.DecodeString(input.QuoteHash); e != nil {
		return out, ErrInvalid
	}
	input.ClientOwnershipIntent = intent
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	actor, e := ownershipOwner(ctx, tx, token)
	if e != nil {
		return out, e
	}
	present, e := ownershipSchemaState(ctx, tx)
	if e != nil || !present {
		return out, ErrUnavailable
	}
	fp := ownershipRequestFingerprint(actor, input)
	var savedFP string
	var savedRequest, raw []byte
	e = tx.QueryRowContext(ctx, `SELECT fingerprint,request_json,receipt_json FROM client_ownership_receipts_v1 WHERE event_id=?`, input.EventID).Scan(&savedFP, &savedRequest, &raw)
	requestRaw, _ := json.Marshal(input)
	if e == nil {
		if savedFP != fp || !bytes.Equal(savedRequest, requestRaw) {
			return out, ErrConflict
		}
		if len(raw) > 2<<20 || json.Unmarshal(raw, &out) != nil || out.EventID != input.EventID || out.ActorUserID != actor || out.Replayed || out.Count != len(out.Clients) || out.Count < 1 || out.Count > MaxOwnershipBatch {
			return ClientOwnershipReceipt{}, ErrUnavailable
		}
		canonical, _ := json.Marshal(out)
		if !bytes.Equal(canonical, raw) {
			return ClientOwnershipReceipt{}, ErrUnavailable
		}
		out.Replayed = true
		return out, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return out, e
	}
	if e = newOwnershipEvent(ctx, tx, input.EventID); e != nil {
		return out, e
	}
	now := time.Now().Unix()
	if input.QuoteUnix <= 0 || input.QuoteUnix < now-180 || input.QuoteUnix > now+5 {
		return out, ErrConflict
	}
	quote, clients, e := ownershipQuoteTx(ctx, tx, actor, intent, input.QuoteUnix)
	if e != nil {
		return out, e
	}
	if quote.QuoteHash != input.QuoteHash {
		return out, ErrConflict
	}
	out = ClientOwnershipReceipt{EventID: input.EventID, ActorUserID: actor, RepresentativeID: intent.RepresentativeID, GroupID: intent.GroupID, Count: quote.Count, Clients: quote.Clients, CreatedUnix: now}
	raw, _ = json.Marshal(out)
	if _, e = tx.ExecContext(ctx, `INSERT INTO client_ownership_receipts_v1(event_id,fingerprint,actor_user_id,request_json,receipt_json,created_unix) VALUES(?,?,?,?,?,?)`, input.EventID, fp, actor, requestRaw, raw, now); e != nil {
		return ClientOwnershipReceipt{}, e
	}
	for i, item := range out.Clients {
		c := clients[i]
		if item.Revision == 1 {
			if _, e = tx.ExecContext(ctx, `INSERT INTO client_ownership_origins_v1(client_id,client_uuid,representative_id) VALUES(?,?,?)`, c.ID, c.UUID, c.RepresentativeID); e != nil {
				return ClientOwnershipReceipt{}, e
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO client_ownership_entries_v1(client_id,revision,event_id,previous_representative_id,representative_id,previous_group_id,group_id,usage_present,usage_complete,uplink,downlink,holds) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, item.ClientID, item.Revision, out.EventID, item.PreviousRepresentativeID, item.RepresentativeID, item.PreviousGroupID, item.GroupID, item.UsagePresent, item.UsageComplete, item.Uplink, item.Downlink, item.Holds); e != nil {
			return ClientOwnershipReceipt{}, e
		}
		result, e := tx.ExecContext(ctx, `UPDATE clients SET representative_id=? WHERE id=? AND representative_id=?`, item.RepresentativeID, c.ID, c.RepresentativeID)
		if e != nil {
			return ClientOwnershipReceipt{}, e
		}
		if n, e := result.RowsAffected(); e != nil || n != 1 {
			return ClientOwnershipReceipt{}, ErrConflict
		}
		if _, e = tx.ExecContext(ctx, `DELETE FROM client_group_members WHERE client_id=?`, c.ID); e != nil {
			return ClientOwnershipReceipt{}, e
		}
		if item.GroupID != "" {
			if _, e = tx.ExecContext(ctx, `INSERT INTO client_group_members(client_id,group_id,assigned_unix) VALUES(?,?,?)`, c.ID, item.GroupID, now); e != nil {
				return ClientOwnershipReceipt{}, e
			}
		}
	}
	if e = tx.Commit(); e != nil {
		return ClientOwnershipReceipt{}, e
	}
	return out, nil
}

// Bounded owner-only durable lookup allows lost-response reconciliation without
// re-running a transfer, including after membership or ownership changes again.
func (s *Store) ClientOwnershipReceipt(ctx context.Context, token, event string) (ClientOwnershipReceipt, error) {
	var out ClientOwnershipReceipt
	if !eventRE.MatchString(event) {
		return out, ErrInvalid
	}
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if _, e = ownershipOwner(ctx, tx, token); e != nil {
		return out, e
	}
	present, e := ownershipSchemaState(ctx, tx)
	if e != nil || !present {
		return out, ErrUnavailable
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT receipt_json FROM client_ownership_receipts_v1 WHERE event_id=?`, event).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &out) != nil || out.EventID != event || out.Replayed {
		return ClientOwnershipReceipt{}, ErrUnavailable
	}
	canonical, _ := json.Marshal(out)
	if !bytes.Equal(canonical, raw) {
		return ClientOwnershipReceipt{}, ErrUnavailable
	}
	return out, nil
}
