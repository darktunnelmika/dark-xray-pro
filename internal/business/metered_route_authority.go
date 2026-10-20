package business

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
	"strconv"
	"time"
)

func validMeteredObservation(o mx.Snapshot) bool {
	switch o.Phase {
	case "usage_active_draining", "usage_active_final_saved", "usage_active_settled",
		"usage_pretraffic_settling", "usage_pretraffic_settled", "usage_preparing_api_only",
		"usage_prepare_failed", "usage_prepare_expired", "usage_prepare_birth_unverified",
		"usage_prepared_no_customer_listeners", "usage_waiting_zero_receipt",
		"usage_activating_signed_inbounds", "usage_admission_failed_stopped",
		"usage_outbox_failed_stopped", "metered_transaction_failed_stopped":
		o.Phase = "awaiting_fresh_lease"
	}
	return validObservation(o)
}

func meteredRouteUsers(ctx context.Context, tx *sql.Tx, inbound uint64) ([]xrayconfig.User, error) {
	rows, e := tx.QueryContext(ctx, `SELECT c.id,c.uuid FROM native_inbound_clients b JOIN clients c ON c.id=b.client_id JOIN representatives r ON r.id=c.representative_id JOIN pro_usage_eligibility u ON u.client_id=c.id WHERE b.inbound_id=? AND c.archived=0 AND c.deployment_state!='disabled' AND c.expires_unix>? AND r.enabled=1 AND u.eligible=1 ORDER BY c.id LIMIT 20001`, inbound, time.Now().Unix())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []xrayconfig.User{}
	for rows.Next() {
		var id, uuid string
		if e = rows.Scan(&id, &uuid); e != nil {
			return nil, e
		}
		email, e := uc.CounterEmail(id)
		if e != nil {
			return nil, e
		}
		out = append(out, xrayconfig.User{UUID: uuid, Email: email})
	}
	if len(out) > 20000 {
		return nil, ErrInvalid
	}
	return out, rows.Err()
}
func meteredRouteRefs(ctx context.Context, tx *sql.Tx, node, install string) ([]RouteDraftRef, error) {
	rows, e := tx.QueryContext(ctx, `SELECT i.id,i.revision,d.revision FROM native_inbound_targets t JOIN native_inbounds i ON i.id=t.inbound_id JOIN native_route_drafts d ON d.inbound_id=i.id AND d.node_id=t.node_id WHERE t.node_id=? AND t.installation_id=? AND i.enabled=1 AND i.archived=0 ORDER BY i.id`, node, install)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []RouteDraftRef
	for rows.Next() {
		var r RouteDraftRef
		if e = rows.Scan(&r.InboundID, &r.InboundRevision, &r.DraftRevision); e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func meteredRouteExpiry(ctx context.Context, tx *sql.Tx, node, install string) (int64, error) {
	var expiry sql.NullInt64
	e := tx.QueryRowContext(ctx, `SELECT MIN(c.expires_unix) FROM native_inbound_targets t JOIN native_inbounds i ON i.id=t.inbound_id JOIN native_inbound_clients b ON b.inbound_id=i.id JOIN clients c ON c.id=b.client_id JOIN representatives r ON r.id=c.representative_id JOIN pro_usage_eligibility u ON u.client_id=c.id WHERE t.node_id=? AND t.installation_id=? AND i.enabled=1 AND i.archived=0 AND c.archived=0 AND c.deployment_state!='disabled' AND c.expires_unix>? AND r.enabled=1 AND u.eligible=1`, node, install, time.Now().Unix()).Scan(&expiry)
	if e != nil {
		return 0, e
	}
	if !expiry.Valid {
		return 0, ErrConflict
	}
	return expiry.Int64 * int64(time.Second), nil
}
func meteredHash(v any) string {
	raw, _ := json.Marshal(v)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func meteredEpoch(ctx context.Context, tx *sql.Tx, node, install string, u mx.MeteredCondition) (uc.Epoch, error) {
	var e uc.Epoch
	var raw []byte
	if u.EpochID == "" {
		return e, ErrConflict
	}
	if err := tx.QueryRowContext(ctx, "SELECT epoch_json FROM pro_usage_epochs WHERE epoch_id=? AND node_id=? AND installation_id=?", u.EpochID, node, install).Scan(&raw); err != nil {
		return e, err
	}
	if json.Unmarshal(raw, &e) != nil || meteredHash(e) != u.EpochSHA {
		return uc.Epoch{}, ErrConflict
	}
	return e, nil
}
func meteredSeal(ctx context.Context, tx *sql.Tx, node, install string, r uc.Receipt) error {
	if !uc.ValidReceipt(r) {
		return ErrConflict
	}
	var state string
	var raw []byte
	var sha sql.NullString
	var final sql.NullBool
	e := tx.QueryRowContext(ctx, `SELECT e.state,e.cursor_json,r.sha256,r.quiesced FROM pro_usage_epochs e LEFT JOIN pro_usage_receipts r ON r.epoch_id=e.epoch_id AND r.sequence=? WHERE e.epoch_id=? AND e.node_id=? AND e.installation_id=?`, strconv.FormatUint(r.Sequence, 10), r.EpochID, node, install).Scan(&state, &raw, &sha, &final)
	if e != nil {
		return e
	}
	var c uc.Cursor
	if json.Unmarshal(raw, &c) != nil || state != "sealed" || !sha.Valid || sha.String != r.SHA || !final.Valid || !final.Bool || !c.Quiesced || c.EpochID != r.EpochID || c.Sequence != r.Sequence || c.ReportSHA != r.SHA {
		return ErrConflict
	}
	return nil
}

// Every authority check resolves the exact immutable delivered command. The
// Agent cannot invent a command, change its payload, or register an ordinary
// delivery using this channel. SQL snapshot reads and epoch registration share
// one write transaction, including the latest eligibility and topology.
func (s *Store) MeteredRouteAuthority(ctx context.Context, key, token []byte, in mx.MeteredAuthorityRequest) (uc.Epoch, error) {
	none := uc.Epoch{}
	if s.usage == nil {
		return none, ErrUnavailable
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return none, e
	}
	defer tx.Rollback()
	row, e := readMeteredRouteCommand(ctx, tx, key, in.Command.ID)
	if e != nil {
		return none, e
	}
	c := row.payload.Command
	signed, e := mx.SignTransactionCommand(token, in.Command)
	if e != nil || signed.Signature != in.Command.Signature || meteredHash(c) != meteredHash(in.Command) || c.Version != 2 || row.view.State != "delivered" {
		return none, ErrConflict
	}
	now := time.Now().UnixNano()
	if c.Issued > now+int64(time.Second) || c.Expires <= now {
		return none, ErrConflict
	}
	if c.Action != "stage" && (!mx.ValidLeaseSignature(c.Lease, string(token)) || c.Lease.Expires <= now || c.Lease.Expires-c.Lease.Issued > int64(30*time.Second)) {
		return none, ErrConflict
	}
	if e = meteredRouteStillCurrent(ctx, tx, key, row); e != nil {
		return none, e
	}
	if c.Usage.EpochID != "" {
		if _, e = meteredEpoch(ctx, tx, c.NodeID, c.InstallationID, *c.Usage); e != nil {
			return none, e
		}
		if c.Usage.Settlement != (uc.Receipt{}) {
			if e = meteredSeal(ctx, tx, c.NodeID, c.InstallationID, c.Usage.Settlement); e != nil {
				return none, e
			}
		}
	} else {
		var n int
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_epochs WHERE node_id=? AND installation_id=?", c.NodeID, c.InstallationID).Scan(&n); e != nil {
			return none, e
		}
		// A retried registration is checked against its command-bound birth below.
		if n != 0 && in.Birth == nil {
			return none, ErrConflict
		}
	}
	if in.Birth == nil {
		return none, tx.Commit()
	}
	if c.Action != "apply" && c.Action != "rollback" {
		return none, ErrConflict
	}
	b := in.Birth
	if !mx.ValidBirthReport(string(token), *b) || b.Lease != c.Lease {
		return none, ErrConflict
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("DARK-PRO-METERED-COMMAND-BIRTH-v2\x00" + c.ID + "\x00" + meteredHash(*b)))
	birth := UsageBirth{EpochID: hex.EncodeToString(mac.Sum(nil)[:16]), KernelBootID: b.KernelBootID, PID: b.PID, StartTicks: b.ProcessStartTicks}
	var prior []byte
	e = tx.QueryRowContext(ctx, "SELECT epoch_json FROM native_metered_route_births WHERE command_id=?", c.ID).Scan(&prior)
	if e == nil {
		var old uc.Epoch
		if json.Unmarshal(prior, &old) != nil || old.ID != birth.EpochID || old.KernelBootID != birth.KernelBootID || old.PID != birth.PID || old.ProcessStartTicks != birth.StartTicks {
			return none, ErrConflict
		}
		return old, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return none, e
	}
	if c.Usage.EpochID != "" {
		// The ledger, not the Agent's claim, must already have an exact final seal.
		var raw []byte
		if e = tx.QueryRowContext(ctx, "SELECT cursor_json FROM pro_usage_epochs WHERE epoch_id=?", c.Usage.EpochID).Scan(&raw); e != nil {
			return none, e
		}
		var cursor uc.Cursor
		if json.Unmarshal(raw, &cursor) != nil {
			return none, ErrConflict
		}
		if e = meteredSeal(ctx, tx, c.NodeID, c.InstallationID, uc.Receipt{EpochID: cursor.EpochID, Sequence: cursor.Sequence, SHA: cursor.ReportSHA}); e != nil {
			return none, e
		}
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(row.payload.ConfigBase64)
	if e != nil {
		return none, e
	}
	candidate := row.payload.Candidate
	candidate.Config = raw
	cfg := NodeConfig{NodeID: c.NodeID, InstallationID: c.InstallationID, ConfigSHA: candidate.SHA256, Candidate: candidate}
	epoch, e := s.registerMeteredRouteEpoch(ctx, tx, cfg, birth)
	if e != nil {
		return none, e
	}
	raw, e = json.Marshal(epoch)
	if e != nil {
		return none, e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO native_metered_route_births(command_id,epoch_json) VALUES(?,?)", c.ID, raw); e != nil {
		return none, e
	}
	if e = tx.Commit(); e != nil {
		return none, e
	}
	return epoch, nil
}
func (s *Store) registerMeteredRouteEpoch(ctx context.Context, tx *sql.Tx, c NodeConfig, birth UsageBirth) (uc.Epoch, error) {
	var err error
	var config struct {
		Inbounds []struct {
			Protocol string `json:"protocol"`
			Settings struct {
				Clients []struct {
					UUID  string `json:"id"`
					Email string `json:"email"`
				} `json:"clients"`
			} `json:"settings"`
		} `json:"inbounds"`
	}
	if json.Unmarshal(c.Candidate.Config, &config) != nil {
		return uc.Epoch{}, ErrInvalid
	}
	epoch := uc.Epoch{NodeID: c.NodeID, InstallationID: c.InstallationID, ID: birth.EpochID, ConfigSHA: c.ConfigSHA, KernelBootID: birth.KernelBootID, PID: birth.PID, ProcessStartTicks: birth.StartTicks}
	seen := map[string]string{}
	for _, in := range config.Inbounds {
		if in.Protocol != "vless" {
			return uc.Epoch{}, ErrInvalid
		}
		for _, u := range in.Settings.Clients {
			var id string
			if err = tx.QueryRowContext(ctx, "SELECT id FROM clients WHERE uuid=?", u.UUID).Scan(&id); err != nil {
				return uc.Epoch{}, ErrConflict
			}
			email, e := uc.CounterEmail(id)
			if e != nil || email != u.Email {
				return uc.Epoch{}, ErrConflict
			}
			if prior, exists := seen[id]; exists {
				if prior != u.Email {
					return uc.Epoch{}, ErrConflict
				}
				continue
			}
			seen[id] = u.Email
			// A native VLESS receiver TERMINATES customer traffic, including Swap
			// destinations. This does not label a separate transit server billable.
			epoch.Bindings = append(epoch.Bindings, uc.Binding{ClientID: id, Email: email, Billable: true})
		}
	}
	canonical, _, err := uc.CanonicalEpoch(epoch)
	if err != nil {
		return uc.Epoch{}, err
	}
	if err = s.usage.RegisterEpochTx(ctx, tx, canonical); err != nil {
		return uc.Epoch{}, err
	}

	return canonical, nil
}

// Signed Agent evidence is necessary, but not sufficient: the HUB confirms
// epoch identity, initial receipt and prior final seal from its own ledger.
func meteredReceiptLedger(ctx context.Context, tx *sql.Tx, row meteredRouteRow, ack mx.TransactionReceipt) error {
	c := row.payload.Command
	if ack.Metered == nil {
		return ErrInvalid
	}
	proof := ack.Metered
	if proof.PriorSettlement != (uc.Receipt{}) {
		if e := meteredSeal(ctx, tx, c.NodeID, c.InstallationID, proof.PriorSettlement); e != nil {
			return e
		}
	}
	if proof.After.EpochID != "" {
		epoch, e := meteredEpoch(ctx, tx, c.NodeID, c.InstallationID, proof.After)
		if e != nil {
			return e
		}
		if proof.After.Settlement != (uc.Receipt{}) {
			if e = meteredSeal(ctx, tx, c.NodeID, c.InstallationID, proof.After.Settlement); e != nil {
				return e
			}
		}
		if ack.Outcome == "applied" || ack.Outcome == "rolled_back" {
			var raw []byte
			if e = tx.QueryRowContext(ctx, "SELECT epoch_json FROM native_metered_route_births WHERE command_id=?", c.ID).Scan(&raw); e != nil {
				return e
			}
			var registered uc.Epoch
			if json.Unmarshal(raw, &registered) != nil || meteredHash(registered) != meteredHash(epoch) {
				return ErrConflict
			}
		}
		if ack.Outcome == "applied" || ack.Outcome == "rolled_back" || ack.Outcome == "renewed" {
			var n int
			if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_receipts WHERE epoch_id=? AND sequence='1' AND quiesced=0", epoch.ID).Scan(&n); e != nil {
				return e
			}
			if n != 1 {
				return ErrConflict
			}
		}
	}
	return nil
}
