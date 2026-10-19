package business

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"math"
	"sort"
	"strconv"
	"time"
)

// BulkExtensionIntent never publishes a subscription or touches live Xray.
type BulkExtensionIntent struct {
	ClientIDs             []string `json:"client_ids"`
	AdditionalDays        int64    `json:"additional_days"`
	AdditionalVolumeBytes int64    `json:"additional_volume_bytes"`
	Reason                string   `json:"reason"`
}
type BulkExtensionCommit struct {
	BulkExtensionIntent
	EventID   string `json:"event_id"`
	QuoteHash string `json:"quote_hash"`
	QuoteUnix int64  `json:"quote_unix"`
}
type BulkExtensionClientQuote struct {
	ClientID             string `json:"client_id"`
	RepresentativeID     string `json:"representative_id"`
	PlanKind             string `json:"plan_kind"`
	ExpiresUnix          int64  `json:"expires_unix"`
	EstimatedExpiresUnix int64  `json:"estimated_expires_unix"`
	VolumeCostBytes      string `json:"volume_cost_bytes"`
	UnlimitedCostUnits   string `json:"unlimited_cost_units"`
}
type BulkExtensionRepQuote struct {
	RepresentativeID     string `json:"representative_id"`
	OwnerExempt          bool   `json:"owner_exempt"`
	AvailableVolumeBytes string `json:"available_volume_bytes"`
	AvailableUnits       string `json:"available_unlimited_units"`
	VolumeDebitBytes     string `json:"volume_debit_bytes"`
	UnlimitedDebitUnits  string `json:"unlimited_debit_units"`
	Sufficient           bool   `json:"sufficient"`
}
type BulkExtensionQuote struct {
	QuoteHash       string                     `json:"quote_hash"`
	QuoteUnix       int64                      `json:"quote_unix"`
	Count           int                        `json:"count"`
	Clients         []BulkExtensionClientQuote `json:"clients"`
	Representatives []BulkExtensionRepQuote    `json:"representatives"`
	Sufficient      bool                       `json:"sufficient"`
	AppliedToXray   bool                       `json:"applied_to_xray"`
}
type BulkExtensionResult struct {
	Count                       int  `json:"count"`
	Replayed                    bool `json:"replayed"`
	AppliedToXray               bool `json:"applied_to_xray"`
	CustomerTrafficChanged      bool `json:"customer_traffic_changed"`
	ClientOrUsageHistoryDeleted bool `json:"client_or_usage_history_deleted"`
}
type bulkExtensionSnapshot struct {
	Intent BulkExtensionIntent
	AtUnix int64
	Rows   []Client
	Reps   []Representative
}
type calculatedExtension struct {
	Client     Client
	VolumeCost int64
	UnitsCost  int64
}

func sortedExtension(input BulkExtensionIntent, scope string) (BulkExtensionIntent, error) {
	if !validName(input.Reason, 200) || input.AdditionalDays < 0 || input.AdditionalDays > MaxDays ||
		input.AdditionalVolumeBytes < 0 || input.AdditionalVolumeBytes > MaxVolumeCredit ||
		(input.AdditionalDays == 0 && input.AdditionalVolumeBytes == 0) ||
		len(input.ClientIDs) < 1 || len(input.ClientIDs) > 50 || (scope != "" && !idRE.MatchString(scope)) {
		return BulkExtensionIntent{}, ErrInvalid
	}
	input.ClientIDs = append([]string(nil), input.ClientIDs...)
	sort.Strings(input.ClientIDs)
	for i, id := range input.ClientIDs {
		if !idRE.MatchString(id) || (i > 0 && id == input.ClientIDs[i-1]) {
			return BulkExtensionIntent{}, ErrInvalid
		}
	}
	return input, nil
}

// Exactly the cumulative user-month delta used by Store.ExtendClient, never
// ceil(addedDays*slots/30) on each separate extension.
func calculateBulkExtension(c Client, addDays, addVolume, now int64) (calculatedExtension, error) {
	var out calculatedExtension
	if c.Archived {
		return out, ErrConflict
	}
	if addDays < 0 || addDays > MaxDays || c.DurationDays > MaxDays-addDays {
		return out, ErrInvalid
	}
	days := c.DurationDays + addDays
	volume, ok := safeAdd(c.VolumeBytes, addVolume, MaxVolumeCredit)
	if !ok {
		return out, ErrInvalid
	}
	var volCost, unitsCost int64
	switch c.PlanKind {
	case "unlimited":
		if addVolume != 0 || c.IPCount <= 0 || c.IPCount > MaxIPLimit ||
			days > (math.MaxInt64-29)/c.IPCount {
			return out, ErrInvalid
		}
		total := (days*c.IPCount + 29) / 30
		if total < c.ChargeUnlimitedUnits {
			return out, ErrConflict
		}
		unitsCost = total - c.ChargeUnlimitedUnits
	case "volume":
		volCost = addVolume
	default:
		return out, ErrConflict
	}
	chargedVolume, ok := safeAdd(c.ChargeVolumeBytes, volCost, MaxVolumeCredit)
	if !ok {
		return out, ErrInvalid
	}
	chargedUnits, ok := safeAdd(c.ChargeUnlimitedUnits, unitsCost, MaxUnlimitedCredit)
	if !ok {
		return out, ErrInvalid
	}
	expiry := c.ExpiresUnix
	if expiry < now && addDays > 0 {
		expiry = now
	}
	if addDays > (math.MaxInt64-expiry)/86400 {
		return out, ErrInvalid
	}
	expiry += addDays * 86400
	c.DurationDays = days
	c.VolumeBytes = volume
	c.ChargeVolumeBytes = chargedVolume
	c.ChargeUnlimitedUnits = chargedUnits
	c.ExpiresUnix = expiry
	return calculatedExtension{Client: c, VolumeCost: volCost, UnitsCost: unitsCost}, nil
}
func extensionQuoteTx(ctx context.Context, tx *sql.Tx, intent BulkExtensionIntent, scope string, at int64) (BulkExtensionQuote, []calculatedExtension, error) {
	q := BulkExtensionQuote{Count: len(intent.ClientIDs), Sufficient: true, Clients: make([]BulkExtensionClientQuote, 0, len(intent.ClientIDs))}
	changes := make([]calculatedExtension, 0, len(intent.ClientIDs))
	reps := make(map[string]Representative)
	totals := make(map[string][2]int64)
	snapshot := bulkExtensionSnapshot{Intent: intent, AtUnix: at}
	for _, id := range intent.ClientIDs {
		c, err := readClient(ctx, tx, id)
		if errors.Is(err, sql.ErrNoRows) {
			return BulkExtensionQuote{}, nil, ErrNotFound
		}
		if err != nil {
			return BulkExtensionQuote{}, nil, err
		}
		if scope != "" && c.RepresentativeID != scope {
			return BulkExtensionQuote{}, nil, ErrConflict
		}
		r, found := reps[c.RepresentativeID]
		if !found {
			r, err = readRep(ctx, tx, c.RepresentativeID)
			if err != nil {
				return BulkExtensionQuote{}, nil, err
			}
			reps[c.RepresentativeID] = r
		}
		if !r.Enabled || (r.Kind != "owner" && r.Kind != "representative") {
			return BulkExtensionQuote{}, nil, ErrConflict
		}
		change, err := calculateBulkExtension(c, intent.AdditionalDays, intent.AdditionalVolumeBytes, at)
		if err != nil {
			return BulkExtensionQuote{}, nil, err
		}
		changes = append(changes, change)
		snapshot.Rows = append(snapshot.Rows, c)
		current := totals[c.RepresentativeID]
		vol, ok := safeAdd(current[0], change.VolumeCost, MaxVolumeCredit)
		if !ok {
			return BulkExtensionQuote{}, nil, ErrInvalid
		}
		units, ok := safeAdd(current[1], change.UnitsCost, MaxUnlimitedCredit)
		if !ok {
			return BulkExtensionQuote{}, nil, ErrInvalid
		}
		totals[c.RepresentativeID] = [2]int64{vol, units}
		q.Clients = append(q.Clients, BulkExtensionClientQuote{
			ClientID: c.ID, RepresentativeID: c.RepresentativeID, PlanKind: c.PlanKind,
			ExpiresUnix: c.ExpiresUnix, EstimatedExpiresUnix: change.Client.ExpiresUnix,
			VolumeCostBytes:    strconv.FormatInt(change.VolumeCost, 10),
			UnlimitedCostUnits: strconv.FormatInt(change.UnitsCost, 10),
		})
	}
	ids := make([]string, 0, len(reps))
	for id := range reps {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	q.Representatives = make([]BulkExtensionRepQuote, 0, len(ids))
	for _, id := range ids {
		r := reps[id]
		snapshot.Reps = append(snapshot.Reps, r)
		cost := totals[id]
		sufficient := r.Kind == "owner" || (r.VolumeCreditBytes >= cost[0] && r.UnlimitedCreditUnits >= cost[1])
		if !sufficient {
			q.Sufficient = false
		}
		q.Representatives = append(q.Representatives, BulkExtensionRepQuote{
			RepresentativeID: id, OwnerExempt: r.Kind == "owner",
			AvailableVolumeBytes: strconv.FormatInt(r.VolumeCreditBytes, 10),
			AvailableUnits:       strconv.FormatInt(r.UnlimitedCreditUnits, 10),
			VolumeDebitBytes:     strconv.FormatInt(cost[0], 10),
			UnlimitedDebitUnits:  strconv.FormatInt(cost[1], 10),
			Sufficient:           sufficient,
		})
	}
	q.QuoteUnix = at
	q.QuoteHash = fingerprint("bulk-extend-snapshot-v7", snapshot)
	return q, changes, nil
}

// Quote is an authenticated read-only SQLite snapshot even though the
// browser invokes it through a CSRF-protected POST with a JSON selection.
func (s *Store) QuoteBulkExtension(ctx context.Context, input BulkExtensionIntent, scope string) (BulkExtensionQuote, error) {
	intent, err := sortedExtension(input, scope)
	if err != nil {
		return BulkExtensionQuote{}, err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return BulkExtensionQuote{}, err
	}
	defer tx.Rollback()
	q, _, err := extensionQuoteTx(ctx, tx, intent, scope, time.Now().UTC().Unix())
	if err != nil {
		return BulkExtensionQuote{}, err
	}
	if err = tx.Commit(); err != nil {
		return BulkExtensionQuote{}, err
	}
	return q, nil
}

// One SQLite transaction, one set fingerprint, deterministic immutable audit
// IDs. Identical retries return before checking a now-stale quote or debiting
// credit. Different intent with reused batch ID is a conflict.
func (s *Store) CommitBulkExtension(ctx context.Context, input BulkExtensionCommit, scope string) (BulkExtensionResult, error) {
	var out BulkExtensionResult
	intent, err := sortedExtension(input.BulkExtensionIntent, scope)
	if err != nil || !eventRE.MatchString(input.EventID) {
		return out, ErrInvalid
	}
	if len(input.QuoteHash) != 64 {
		return out, ErrInvalid
	}
	if _, err = hex.DecodeString(input.QuoteHash); err != nil {
		return out, ErrInvalid
	}
	batchFP := fingerprint("bulk-extend-v7", struct {
		EventID string
		Intent  BulkExtensionIntent
	}{input.EventID, intent})
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	allReplay := false
	for i, id := range intent.ClientIDs {
		ev, fp := bulkExtensionEventID(input.EventID, id, i, batchFP)
		replayed, e := checkClientOp(ctx, tx, ev, fp, "extend")
		if e != nil {
			return out, e
		}
		if i == 0 {
			allReplay = replayed
		} else if allReplay != replayed {
			return out, ErrConflict
		}
		// Rep tenant scope is revalidated inside the same write transaction.
		c, e := readClient(ctx, tx, id)
		if errors.Is(e, sql.ErrNoRows) {
			return out, ErrNotFound
		}
		if e != nil {
			return out, e
		}
		if scope != "" && c.RepresentativeID != scope {
			return out, ErrConflict
		}
	}
	if allReplay {
		if err = tx.Commit(); err != nil {
			return out, err
		}
		return BulkExtensionResult{Count: len(intent.ClientIDs), Replayed: true}, nil
	}
	now := time.Now().UTC().Unix()
	if input.QuoteUnix <= 0 || input.QuoteUnix < now-180 || input.QuoteUnix > now+5 {
		return out, ErrConflict
	}
	q, quotedChanges, err := extensionQuoteTx(ctx, tx, intent, scope, input.QuoteUnix)
	if err != nil {
		return out, err
	}
	if !q.Sufficient {
		return out, ErrInsufficient
	}
	if q.QuoteHash != input.QuoteHash {
		return out, ErrConflict
	}
	for _, rep := range q.Representatives {
		if rep.OwnerExempt {
			continue
		}
		vol, e := strconv.ParseInt(rep.VolumeDebitBytes, 10, 64)
		if e != nil {
			return out, ErrInvalid
		}
		units, e := strconv.ParseInt(rep.UnlimitedDebitUnits, 10, 64)
		if e != nil {
			return out, ErrInvalid
		}
		if _, err = tx.ExecContext(ctx,
			"UPDATE representatives SET volume_credit_bytes=volume_credit_bytes-?,unlimited_credit_units=unlimited_credit_units-? WHERE id=?",
			vol, units, rep.RepresentativeID); err != nil {
			return out, err
		}
	}
	for i, id := range intent.ClientIDs {
		previous, err := readClient(ctx, tx, id)
		if err != nil {
			return out, err
		}
		actual, err := calculateBulkExtension(previous, intent.AdditionalDays, intent.AdditionalVolumeBytes, now)
		if err != nil {
			return out, err
		}
		if actual.VolumeCost != quotedChanges[i].VolumeCost || actual.UnitsCost != quotedChanges[i].UnitsCost {
			return out, ErrConflict
		}
		c := actual.Client
		if _, err = tx.ExecContext(ctx,
			"UPDATE clients SET duration_days=?,volume_bytes=?,charged_volume_bytes=?,charged_unlimited_units=?,expires_unix=? WHERE id=?",
			c.DurationDays, c.VolumeBytes, c.ChargeVolumeBytes, c.ChargeUnlimitedUnits, c.ExpiresUnix, id); err != nil {
			return out, err
		}
		eventID, fp := bulkExtensionEventID(input.EventID, id, i, batchFP)
		if _, err = tx.ExecContext(ctx,
			"INSERT INTO client_operations(event_id,fingerprint,client_id,representative_id,kind,reason,added_days,added_volume_bytes,volume_credit_debit,unlimited_credit_debit,created_unix) VALUES(?,?,?,?,'extend',?,?,?,?,?,?)",
			eventID, fp, id, c.RepresentativeID, intent.Reason, intent.AdditionalDays, intent.AdditionalVolumeBytes, actual.VolumeCost, actual.UnitsCost, now); err != nil {
			return out, err
		}
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	return BulkExtensionResult{Count: len(intent.ClientIDs)}, nil
}
func bulkExtensionEventID(batchID, clientID string, index int, batchFP string) (string, string) {
	if index == 0 {
		return batchID, batchFP
	}
	seed := sha256.Sum256([]byte("bulk-extend-v7\x00" + batchID + "\x00" + clientID))
	eventID := "bulk-extend-" + hex.EncodeToString(seed[:20])
	fp := fingerprint("bulk-extend-child-v7", struct {
		Batch            string
		ClientID         string
		BatchFingerprint string
	}{batchID, clientID, batchFP})
	return eventID, fp
}
