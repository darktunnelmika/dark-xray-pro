package business

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"
)

// ClientProfileEditIntent changes one PRO-native client. Plan kind and identity
// remain fixed; volume and expiry can grow, never silently reset.
type ClientProfileEditIntent struct {
	ClientID              string `json:"client_id"`
	Name                  string `json:"name"`
	PlanKind              string `json:"plan_kind"`
	IPCount               int64  `json:"ip_count"`
	AdditionalDays        int64  `json:"additional_days"`
	AdditionalVolumeBytes int64  `json:"additional_volume_bytes"`
	Reason                string `json:"reason"`
}
type ClientProfileEditCommit struct {
	ClientProfileEditIntent
	EventID   string `json:"event_id"`
	QuoteHash string `json:"quote_hash"`
	QuoteUnix int64  `json:"quote_unix"`
}
type ClientProfileEditQuote struct {
	QuoteHash                  string `json:"quote_hash"`
	QuoteUnix                  int64  `json:"quote_unix"`
	ClientID                   string `json:"client_id"`
	RepresentativeID           string `json:"representative_id"`
	PlanKind                   string `json:"plan_kind"`
	PreviousName               string `json:"previous_name"`
	NewName                    string `json:"new_name"`
	PreviousIPCount            int64  `json:"previous_ip_count"`
	NewIPCount                 int64  `json:"new_ip_count"`
	PreviousExpiresUnix        int64  `json:"previous_expires_unix"`
	NewExpiresUnix             int64  `json:"new_expires_unix"`
	AddedVolumeBytes           string `json:"added_volume_bytes"`
	VolumeCreditDebitBytes     string `json:"volume_credit_debit_bytes"`
	UnlimitedCreditDebitUnits  string `json:"unlimited_credit_debit_units"`
	AvailableVolumeCreditBytes string `json:"available_volume_credit_bytes"`
	AvailableUnlimitedUnits    string `json:"available_unlimited_units"`
	OwnerExempt                bool   `json:"owner_exempt"`
	Sufficient                 bool   `json:"sufficient"`
	AppliedToXray              bool   `json:"applied_to_xray"`
	CreditRefunded             bool   `json:"credit_refunded"`
}
type ClientProfileEditReceipt struct {
	Client                      Client `json:"client"`
	Replayed                    bool   `json:"replayed"`
	AppliedToXray               bool   `json:"applied_to_xray"`
	CreditRefunded              bool   `json:"credit_refunded"`
	CustomerTrafficChanged      bool   `json:"customer_traffic_changed"`
	ClientOrUsageHistoryDeleted bool   `json:"client_or_usage_history_deleted"`
}
type ClientProfileEditAudit struct {
	EventID                   string `json:"event_id"`
	ClientID                  string `json:"client_id"`
	RepresentativeID          string `json:"representative_id"`
	PreviousName              string `json:"previous_name"`
	NewName                   string `json:"new_name"`
	PlanKind                  string `json:"plan_kind"`
	PreviousIPCount           int64  `json:"previous_ip_count"`
	NewIPCount                int64  `json:"new_ip_count"`
	AddedDays                 int64  `json:"added_days"`
	AddedVolumeBytes          string `json:"added_volume_bytes"`
	VolumeCreditDebitBytes    string `json:"volume_credit_debit_bytes"`
	UnlimitedCreditDebitUnits string `json:"unlimited_credit_debit_units"`
	PreviousExpiresUnix       int64  `json:"previous_expires_unix"`
	NewExpiresUnix            int64  `json:"new_expires_unix"`
	Reason                    string `json:"reason"`
	CreatedUnix               int64  `json:"created_unix"`
}

// Fully backward compatible with V7: add a private immutable table without
// bumping PRAGMA user_version. Rolling back to V7 reads exactly the same
// original client/credit/usage data; no account schema rewrite is performed.
func ensureClientProfileEdits(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS client_profile_edits (
    event_id TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    client_id TEXT NOT NULL REFERENCES clients(id),
    representative_id TEXT NOT NULL REFERENCES representatives(id),
    previous_name TEXT NOT NULL, new_name TEXT NOT NULL,
    plan_kind TEXT NOT NULL CHECK(plan_kind IN ('volume','unlimited')),
    previous_ip_count INTEGER NOT NULL CHECK(previous_ip_count BETWEEN 0 AND 1000),
    new_ip_count INTEGER NOT NULL CHECK(new_ip_count BETWEEN 0 AND 1000),
    added_days INTEGER NOT NULL CHECK(added_days BETWEEN 0 AND 3650),
    added_volume_bytes INTEGER NOT NULL CHECK(added_volume_bytes>=0),
    volume_credit_debit_bytes INTEGER NOT NULL CHECK(volume_credit_debit_bytes>=0),
    unlimited_credit_debit_units INTEGER NOT NULL CHECK(unlimited_credit_debit_units>=0),
    previous_expires_unix INTEGER NOT NULL, new_expires_unix INTEGER NOT NULL,
    reason TEXT NOT NULL, created_unix INTEGER NOT NULL
  )`,
		`CREATE INDEX IF NOT EXISTS client_profile_edits_client_time ON client_profile_edits(client_id,created_unix DESC,event_id)`,
		`CREATE TRIGGER IF NOT EXISTS client_profile_edits_immutable_update BEFORE UPDATE ON client_profile_edits BEGIN SELECT RAISE(ABORT,'client profile audits are append-only'); END`,
		`CREATE TRIGGER IF NOT EXISTS client_profile_edits_immutable_delete BEFORE DELETE ON client_profile_edits BEGIN SELECT RAISE(ABORT,'client profile audits are append-only'); END`,
	}
	for _, q := range statements {
		if _, err = tx.Exec(q); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func validateClientProfileEdit(input ClientProfileEditIntent) (ClientProfileEditIntent, error) {
	input.Name = strings.TrimSpace(input.Name)
	input.Reason = strings.TrimSpace(input.Reason)
	if !idRE.MatchString(input.ClientID) || !validName(input.Name, 128) ||
		!validName(input.Reason, 200) ||
		input.AdditionalDays < 0 || input.AdditionalDays > MaxDays ||
		input.AdditionalVolumeBytes < 0 || input.AdditionalVolumeBytes > MaxVolumeCredit ||
		input.IPCount < 0 || input.IPCount > MaxIPLimit ||
		(input.PlanKind != "volume" && input.PlanKind != "unlimited") {
		return ClientProfileEditIntent{}, ErrInvalid
	}
	if input.PlanKind == "unlimited" && (input.IPCount == 0 || input.AdditionalVolumeBytes != 0) {
		return ClientProfileEditIntent{}, ErrInvalid
	}
	return input, nil
}
func computeClientProfileEdit(c Client, rep Representative, input ClientProfileEditIntent, now int64) (Client, ClientProfileEditQuote, error) {
	var quote ClientProfileEditQuote
	if c.Archived || !rep.Enabled || !((rep.Kind == "owner") || (rep.Kind == "representative")) ||
		c.PlanKind != input.PlanKind {
		return Client{}, quote, ErrConflict
	}
	// A decrease in *paid* unlimited seats is a separate policy and refund
	// problem. Reject it to preserve exact user-month accounting on V7 rollback.
	if c.PlanKind == "unlimited" && input.IPCount < c.IPCount {
		return Client{}, quote, ErrConflict
	}
	// Existing lifecycle implementation handles elapsed time and cumulative
	// quota. Reuse its core volume/time algorithm, then price NEW user seats.
	base, err := calculateBulkExtension(c, input.AdditionalDays, input.AdditionalVolumeBytes, now)
	if err != nil {
		return Client{}, quote, err
	}
	next := base.Client
	next.Name = input.Name
	next.IPCount = input.IPCount
	volumeCost, unitCost := base.VolumeCost, base.UnitsCost
	if c.PlanKind == "unlimited" {
		days := next.DurationDays
		if days > (math.MaxInt64-29)/input.IPCount {
			return Client{}, quote, ErrInvalid
		}
		target := (days*input.IPCount + 29) / 30
		if target < c.ChargeUnlimitedUnits {
			return Client{}, quote, ErrConflict
		}
		unitCost = target - c.ChargeUnlimitedUnits
		if _, ok := safeAdd(c.ChargeUnlimitedUnits, unitCost, MaxUnlimitedCredit); !ok {
			return Client{}, quote, ErrInvalid
		}
		next.ChargeUnlimitedUnits = c.ChargeUnlimitedUnits + unitCost
	}
	if next.Name == c.Name && next.IPCount == c.IPCount &&
		input.AdditionalDays == 0 && input.AdditionalVolumeBytes == 0 {
		return Client{}, quote, ErrInvalid
	}
	// Downward volume / expiry edits are never implicit: no refund, no
	// unbounded loss of access, no usage reset or on-the-wire mutation.
	sufficient := rep.Kind == "owner" || (rep.VolumeCreditBytes >= volumeCost && rep.UnlimitedCreditUnits >= unitCost)
	quote = ClientProfileEditQuote{
		ClientID: c.ID, RepresentativeID: c.RepresentativeID, PlanKind: c.PlanKind,
		PreviousName: c.Name, NewName: next.Name,
		PreviousIPCount: c.IPCount, NewIPCount: next.IPCount,
		PreviousExpiresUnix: c.ExpiresUnix, NewExpiresUnix: next.ExpiresUnix,
		AddedVolumeBytes:           strconv.FormatInt(input.AdditionalVolumeBytes, 10),
		VolumeCreditDebitBytes:     strconv.FormatInt(volumeCost, 10),
		UnlimitedCreditDebitUnits:  strconv.FormatInt(unitCost, 10),
		AvailableVolumeCreditBytes: strconv.FormatInt(rep.VolumeCreditBytes, 10),
		AvailableUnlimitedUnits:    strconv.FormatInt(rep.UnlimitedCreditUnits, 10),
		OwnerExempt:                rep.Kind == "owner", Sufficient: sufficient,
	}
	return next, quote, nil
}
func quotedClientProfileEdit(ctx context.Context, tx *sql.Tx, intent ClientProfileEditIntent, scope string, now int64) (ClientProfileEditQuote, Client, error) {
	c, err := readClient(ctx, tx, intent.ClientID)
	if errors.Is(err, sql.ErrNoRows) {
		return ClientProfileEditQuote{}, Client{}, ErrNotFound
	}
	if err != nil {
		return ClientProfileEditQuote{}, Client{}, err
	}
	if scope != "" && c.RepresentativeID != scope {
		return ClientProfileEditQuote{}, Client{}, ErrConflict
	}
	rep, err := readRep(ctx, tx, c.RepresentativeID)
	if err != nil {
		return ClientProfileEditQuote{}, Client{}, err
	}
	next, quote, err := computeClientProfileEdit(c, rep, intent, now)
	if err != nil {
		return ClientProfileEditQuote{}, Client{}, err
	}
	quote.QuoteUnix = now
	quote.QuoteHash = fingerprint("client-profile-edit-v8-quote", struct {
		Intent         ClientProfileEditIntent
		Current        Client
		Representative Representative
		AtUnix         int64
	}{intent, c, rep, now})
	return quote, next, nil
}
func (s *Store) QuoteClientProfileEdit(ctx context.Context, input ClientProfileEditIntent, scope string) (ClientProfileEditQuote, error) {
	intent, err := validateClientProfileEdit(input)
	if err != nil || (scope != "" && !idRE.MatchString(scope)) {
		return ClientProfileEditQuote{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ClientProfileEditQuote{}, err
	}
	defer tx.Rollback()
	quote, _, err := quotedClientProfileEdit(ctx, tx, intent, scope, time.Now().UTC().Unix())
	if err != nil {
		return ClientProfileEditQuote{}, err
	}
	if err = tx.Commit(); err != nil {
		return ClientProfileEditQuote{}, err
	}
	return quote, nil
}
func (s *Store) CommitClientProfileEdit(ctx context.Context, input ClientProfileEditCommit, scope string) (ClientProfileEditReceipt, error) {
	var out ClientProfileEditReceipt
	intent, err := validateClientProfileEdit(input.ClientProfileEditIntent)
	if err != nil || !eventRE.MatchString(input.EventID) || (scope != "" && !idRE.MatchString(scope)) ||
		len(input.QuoteHash) != 64 {
		return out, ErrInvalid
	}
	if _, err = hex.DecodeString(input.QuoteHash); err != nil {
		return out, ErrInvalid
	}
	fp := fingerprint("client-profile-edit-v8-commit", struct {
		EventID string
		Intent  ClientProfileEditIntent
	}{input.EventID, intent})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var priorFP string
	existing := tx.QueryRowContext(ctx, `SELECT fingerprint FROM client_profile_edits WHERE event_id=?`, input.EventID).Scan(&priorFP)
	switch {
	case existing == nil:
		if priorFP != fp {
			return out, ErrConflict
		}
		c, e := readClient(ctx, tx, intent.ClientID)
		if e != nil {
			return out, e
		}
		if scope != "" && c.RepresentativeID != scope {
			return out, ErrConflict
		}
		if err = tx.Commit(); err != nil {
			return out, err
		}
		return ClientProfileEditReceipt{Client: c, Replayed: true}, nil
	case !errors.Is(existing, sql.ErrNoRows):
		return out, existing
	}
	// Shared event ID namespace: old credit and lifecycle requests cannot be
	// reused for a profile edit (or vice versa).
	for _, table := range []string{"events", "client_operations", "representative_control_events"} {
		var existingID string
		e := tx.QueryRowContext(ctx, "SELECT event_id FROM "+table+" WHERE event_id=?", input.EventID).Scan(&existingID)
		if e == nil {
			return out, ErrConflict
		}
		if !errors.Is(e, sql.ErrNoRows) {
			return out, e
		}
	}
	now := time.Now().UTC().Unix()
	if input.QuoteUnix <= 0 || input.QuoteUnix < now-180 || input.QuoteUnix > now+5 {
		return out, ErrConflict
	}
	quote, next, err := quotedClientProfileEdit(ctx, tx, intent, scope, input.QuoteUnix)
	if err != nil {
		return out, err
	}
	if quote.QuoteHash != input.QuoteHash {
		return out, ErrConflict
	}
	if !quote.Sufficient {
		return out, ErrInsufficient
	}
	// Recompute expiry anchored to actual commit time for expired users.
	original, err := readClient(ctx, tx, intent.ClientID)
	if err != nil {
		return out, err
	}
	rep, err := readRep(ctx, tx, original.RepresentativeID)
	if err != nil {
		return out, err
	}
	actual, actualQuote, err := computeClientProfileEdit(original, rep, intent, now)
	if err != nil {
		return out, err
	}
	if actualQuote.VolumeCreditDebitBytes != quote.VolumeCreditDebitBytes ||
		actualQuote.UnlimitedCreditDebitUnits != quote.UnlimitedCreditDebitUnits {
		return out, ErrConflict
	}
	volumeCost, _ := strconv.ParseInt(quote.VolumeCreditDebitBytes, 10, 64)
	unitsCost, _ := strconv.ParseInt(quote.UnlimitedCreditDebitUnits, 10, 64)
	if rep.Kind == "representative" {
		if _, err = tx.ExecContext(ctx,
			`UPDATE representatives SET volume_credit_bytes=volume_credit_bytes-?,unlimited_credit_units=unlimited_credit_units-? WHERE id=?`,
			volumeCost, unitsCost, rep.ID); err != nil {
			return out, err
		}
	}
	if _, err = tx.ExecContext(ctx,
		`UPDATE clients SET display_name=?,ip_count=?,duration_days=?,volume_bytes=?,charged_volume_bytes=?,charged_unlimited_units=?,expires_unix=? WHERE id=?`,
		actual.Name, actual.IPCount, actual.DurationDays, actual.VolumeBytes,
		actual.ChargeVolumeBytes, actual.ChargeUnlimitedUnits, actual.ExpiresUnix, actual.ID); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO client_profile_edits(
  event_id,fingerprint,client_id,representative_id,previous_name,new_name,plan_kind,
  previous_ip_count,new_ip_count,added_days,added_volume_bytes,
  volume_credit_debit_bytes,unlimited_credit_debit_units,
  previous_expires_unix,new_expires_unix,reason,created_unix
  )VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		input.EventID, fp, original.ID, original.RepresentativeID, original.Name, actual.Name, original.PlanKind,
		original.IPCount, actual.IPCount, intent.AdditionalDays, intent.AdditionalVolumeBytes,
		volumeCost, unitsCost, original.ExpiresUnix, actual.ExpiresUnix, intent.Reason, now); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	_ = next // Quote snapshot was checked; only actual commit-time expiry is saved.
	return ClientProfileEditReceipt{Client: actual}, nil
}
func (s *Store) ClientProfileEditHistory(ctx context.Context, id, scope string, limit, offset int) ([]ClientProfileEditAudit, error) {
	if !idRE.MatchString(id) || (scope != "" && !idRE.MatchString(scope)) ||
		limit < 1 || limit > 50 || offset < 0 || offset > 1_000_000 {
		return nil, ErrInvalid
	}
	c, err := s.Client(ctx, id)
	if err != nil {
		return nil, err
	}
	if scope != "" && c.RepresentativeID != scope {
		return nil, ErrConflict
	}
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,client_id,representative_id,
  previous_name,new_name,plan_kind,previous_ip_count,new_ip_count,added_days,
  added_volume_bytes,volume_credit_debit_bytes,unlimited_credit_debit_units,
  previous_expires_unix,new_expires_unix,reason,created_unix
  FROM client_profile_edits WHERE client_id=? ORDER BY created_unix DESC,event_id ASC LIMIT ? OFFSET ?`, id, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ClientProfileEditAudit, 0)
	for rows.Next() {
		var entry ClientProfileEditAudit
		var addedVol, volumeCost, unitCost int64
		if err = rows.Scan(&entry.EventID, &entry.ClientID, &entry.RepresentativeID,
			&entry.PreviousName, &entry.NewName, &entry.PlanKind, &entry.PreviousIPCount, &entry.NewIPCount,
			&entry.AddedDays, &addedVol, &volumeCost, &unitCost,
			&entry.PreviousExpiresUnix, &entry.NewExpiresUnix, &entry.Reason, &entry.CreatedUnix); err != nil {
			return nil, err
		}
		entry.AddedVolumeBytes = strconv.FormatInt(addedVol, 10)
		entry.VolumeCreditDebitBytes = strconv.FormatInt(volumeCost, 10)
		entry.UnlimitedCreditDebitUnits = strconv.FormatInt(unitCost, 10)
		out = append(out, entry)
	}
	return out, rows.Err()
}
