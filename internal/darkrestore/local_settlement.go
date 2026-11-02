//go:build linux

package darkrestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

var ErrLocalSettlement = errors.New("unproven local source settlement")

// The source supervisor provisions this identifier before admitting traffic.
// Changing even one byte of the pre-epoch snapshot gives a different binding.
// This namespace is deliberately distinct from destination client IDs.
func LocalSourceID(snapshotSHA, sourceID string) string {
	sum := sha256.Sum256([]byte("darkrestore-local-source-epoch-v1\x00" + snapshotSHA + "\x00" + sourceID))
	return "drs-" + hex.EncodeToString(sum[:12])
}

type LocalSourceSettlement struct {
	Scope        string     `json:"scope"`
	SourceID     string     `json:"source_id"`
	SourceSHA    string     `json:"source_sha256"`
	AuthoritySHA string     `json:"authority_sha256"`
	JournalSHA   string     `json:"journal_sha256"`
	Terminal     uc.Receipt `json:"terminal_receipt"`
	PriorBytes   int64      `json:"pre_epoch_bytes,string"`
	Up           int64      `json:"epoch_uplink_bytes,string"`
	Down         int64      `json:"epoch_downlink_bytes,string"`
	Historical   int64      `json:"settled_historical_bytes,string"`
	Remaining    int64      `json:"finite_quota_remaining_bytes,string"`
}

func checkPrivateCopy(path, pin string) error {
	safe, err := safeSource(path)
	if err != nil {
		return err
	}
	for _, p := range []string{filepath.Dir(safe), safe} {
		st, e := os.Lstat(p)
		if e != nil || st.Mode().Perm()&0077 != 0 {
			return ErrUnsafeSource
		}
	}
	sum, _, err := hashSource(safe)
	if err != nil || sum != pin {
		return ErrUnsafeSource
	}
	return nil
}

// InspectLocalSettlement reconciles one pre-traffic legacy snapshot and one
// independently approved, offline, dedicated source accounting database. It
// returns a scope-specific result and never grants destination admission.
func InspectLocalSettlement(ctx context.Context, in LocalInputs, seal managedxray.LocalTCPSeal) (LocalSourceSettlement, error) {
	source, sourceSHA, authority, authoritySHA := in.Source, in.SourceSHA, in.Authority, in.AuthoritySHA
	id, node, installation, at := in.ID, in.Node, in.Installation, in.AsOf
	var zero LocalSourceSettlement
	if ctx == nil || source == authority || id == "" || node == "" || installation == "" || at.Unix() <= 0 || at.After(time.Now()) {
		return zero, ErrLocalSettlement
	}
	if err := checkPrivateCopy(source, sourceSHA); err != nil {
		return zero, err
	}
	if err := checkPrivateCopy(authority, authoritySHA); err != nil {
		return zero, err
	}
	review, err := BuildReview(ctx, source, sourceSHA, at)
	if err != nil || review.Status != "ready_for_review" {
		return zero, ErrLocalSettlement
	}
	var client ReviewClient
	for _, c := range review.Clients {
		if c.SourceID == id {
			client = c
			break
		}
	}
	if client.UUID == "" || client.Status != "requires_manual_review" || len(client.ReviewReasons) != 0 || client.QuotaKind != "volume" {
		return zero, ErrLocalSettlement
	}
	for _, other := range review.Clients {
		if other.SourceID != id && other.UUID == client.UUID {
			return zero, ErrLocalSettlement
		}
	}
	legacy, err := sql.Open("sqlite", sourceURI(source))
	if err != nil {
		return zero, err
	}
	defer legacy.Close()
	var scopes int
	// Node-level mapping or nonlocal observations require independent source seals.
	err = legacy.QueryRowContext(ctx, `SELECT COUNT(*) FROM restore_subscriptions r
 WHERE r.id=? AND r.node_mode='selected' AND r.include_local=1 AND json(r.node_ids)='[]'
 AND json_type(r.inbound_ids)='array' AND json_array_length(r.inbound_ids)=1
 AND json_type(r.inbound_ids,'$[0]')='integer' AND json_extract(r.inbound_ids,'$[0]')>0
 AND NOT EXISTS(SELECT 1 FROM restore_usage WHERE restore_id=r.id AND (scope IS NULL OR scope!='local'))
 AND NOT EXISTS(SELECT 1 FROM restore_group_targets g WHERE g.group_id=r.group_id AND (typeof(g.body)!='text' OR json(g.body)!='{}'))
 AND EXISTS(SELECT 1 FROM restore_usage WHERE restore_id=r.id AND scope='local')`, id).Scan(&scopes)
	if err != nil || scopes != 1 {
		return zero, ErrLocalSettlement
	}
	db, err := sql.Open("sqlite", sourceURI(authority))
	if err != nil {
		return zero, err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	var epochRaw, cursorRaw, seedRaw []byte
	var binding, state, principal, install, storedEpoch, uuid, config, boot, ticks string
	var pid, baseline, holds int
	var up, down int64
	target := LocalSourceID(sourceSHA, id)
	// This first scope supports exactly one process registration. Even an open
	// successor without a first report invalidates a single-epoch result.
	var n int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_epochs").Scan(&n); err != nil || n != 1 {
		return zero, ErrLocalSettlement
	}
	err = tx.QueryRowContext(ctx, `SELECT e.epoch_json,e.cursor_json,e.binding_sha,e.state,e.node_id,e.installation_id,e.epoch_id,e.config_sha,e.kernel_boot_id,e.pid,e.start_ticks,c.uuid,a.uplink,a.downlink,a.baseline_complete,a.holds,s.body_json
 FROM pro_usage_epochs e,clients c JOIN pro_usage_accounts a ON a.client_id=c.id JOIN pro_usage_seeds s ON s.client_id=c.id WHERE c.id=?`, target).Scan(&epochRaw, &cursorRaw, &binding, &state, &principal, &install, &storedEpoch, &config, &boot, &pid, &ticks, &uuid, &up, &down, &baseline, &holds, &seedRaw)
	if err != nil || state != "sealed" || principal != node || install != installation || uuid != client.UUID || baseline != 1 || holds != 0 {
		return zero, ErrLocalSettlement
	}
	// The final process receipt covers its lifetime. Independently require
	// retirement of the source account and expiry of its last signed lease.
	var retired int
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM clients c
	JOIN client_operations op ON op.client_id=c.id AND op.kind='archive'
	JOIN native_runtime_delivery d ON d.node_id=? AND d.installation_id=? AND d.sha=?
	JOIN native_config_generation g ON g.id=1
	WHERE c.id=? AND c.archived=1 AND c.deployment_state='disabled'
	AND op.volume_credit_debit=0 AND op.unlimited_credit_debit=0
	AND d.issued>0 AND d.issued<d.expires AND d.expires<=? AND d.generation<g.value
	AND (SELECT COUNT(*) FROM native_runtime_delivery)=1`, node, installation, config, target, time.Now().UnixNano()).Scan(&retired)
	if err != nil || retired != 1 {
		return zero, ErrLocalSettlement
	}
	var e uc.Epoch
	var cursor uc.Cursor
	if usagewire.Decode(epochRaw, &e) != nil || usagewire.Decode(cursorRaw, &cursor) != nil {
		return zero, ErrLocalSettlement
	}
	_, canonical, err := uc.CanonicalEpoch(e)
	if err != nil || canonical != binding || e.NodeID != node || e.InstallationID != install || e.ID != storedEpoch || e.ConfigSHA != config || e.KernelBootID != boot || e.PID != pid || strconv.FormatUint(e.ProcessStartTicks, 10) != ticks || len(e.Bindings) != 1 {
		return zero, ErrLocalSettlement
	}
	b := e.Bindings[0]
	email, err := uc.CounterEmail(target)
	if err != nil || b.ClientID != target || b.Email != email || !b.Billable || b.Role != "" {
		return zero, ErrLocalSettlement
	}
	var seed usageledger.Seed
	if usagewire.Decode(seedRaw, &seed) != nil || seed.ClientID != target || !seed.Complete || seed.Uplink != 0 || seed.Downlink != 0 {
		return zero, ErrLocalSettlement
	}
	var initial, terminal string
	if err = tx.QueryRowContext(ctx, "SELECT sha256 FROM pro_usage_receipts WHERE epoch_id=? AND sequence='1' AND quiesced=0", e.ID).Scan(&initial); err != nil {
		return zero, ErrLocalSettlement
	}
	r := uc.Report{Version: 1, EpochID: e.ID, BindingSHA: binding, Sequence: 1, Users: []uc.Usage{{ClientID: target}}, SHA: initial}
	if uc.VerifyReport(e, r) != nil || cursor.EpochID != e.ID || cursor.Sequence <= 1 || !cursor.Quiesced {
		return zero, ErrLocalSettlement
	}
	if err = tx.QueryRowContext(ctx, "SELECT sha256 FROM pro_usage_receipts WHERE epoch_id=? AND sequence=? AND quiesced=1", e.ID, strconv.FormatUint(cursor.Sequence, 10)).Scan(&terminal); err != nil || terminal != cursor.ReportSHA {
		return zero, ErrLocalSettlement
	}
	r.Sequence, r.Quiesced, r.Users, r.SHA = cursor.Sequence, true, cursor.Users, terminal
	terminalReceipt := uc.Receipt{EpochID: e.ID, Sequence: r.Sequence, SHA: r.SHA}
	if !seal.Matches(e, client.UUID, terminalReceipt) || uc.VerifyReport(e, r) != nil || up != r.Users[0].Uplink || down != r.Users[0].Downlink {
		return zero, ErrLocalSettlement
	}
	var ledgerUp, ledgerDown int64
	if err = tx.QueryRowContext(ctx, "SELECT SUM(uplink),SUM(downlink) FROM pro_usage_deltas WHERE epoch_id=? AND client_id=?", e.ID, target).Scan(&ledgerUp, &ledgerDown); err != nil || ledgerUp != up || ledgerDown != down {
		return zero, ErrLocalSettlement
	}
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM pro_usage_holds WHERE client_id=? OR epoch_id=?", target, e.ID).Scan(&n); err != nil || n != 0 {
		return zero, ErrLocalSettlement
	}
	prior, err := strconv.ParseInt(client.HistoricalBytes, 10, 64)
	if err != nil || prior < 0 || prior > math.MaxInt64-up-down {
		return zero, ErrLocalSettlement
	}
	quota, err := strconv.ParseInt(client.QuotaBytes, 10, 64)
	if err != nil || quota <= 0 {
		return zero, ErrLocalSettlement
	}
	total := prior + up + down
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	if err = checkPrivateCopy(source, sourceSHA); err != nil {
		return zero, err
	}
	if err = checkPrivateCopy(authority, authoritySHA); err != nil {
		return zero, err
	}
	return LocalSourceSettlement{Scope: "single_local_source_epoch", SourceID: id, SourceSHA: sourceSHA, AuthoritySHA: authoritySHA, Terminal: terminalReceipt, JournalSHA: seal.JournalSHA(), PriorBytes: prior, Up: up, Down: down, Historical: total, Remaining: max(0, quota-total)}, nil
}
