package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// The historical method prefix is retained for the supervisor contract.
// Eligibility is actual expiry OR manual archive, never a plain disable.
type VolumeExpiryRefundRequest struct {
	ClientID                   string `json:"client_id"`
	ExpectedExpiresUnix        int64  `json:"expected_expires_unix"`
	ExpectedChargedVolumeBytes int64  `json:"expected_charged_volume_bytes,string"`
}

type VolumeExpiryRefundProof struct {
	Kind           string `json:"kind"`
	EpochID        string `json:"epoch_id"`
	NodeID         string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	FinalSequence  uint64 `json:"final_sequence,string"`
	FinalSHA       string `json:"final_sha"`
}

type VolumeExpiryRefundStatus struct {
	Request          VolumeExpiryRefundRequest `json:"request"`
	RepresentativeID string                    `json:"representative_id"`
	Generation       string                    `json:"generation"`
	Trigger          string                    `json:"trigger,omitempty"` // expired | archived
	State            string                    `json:"state"`
	Reason           string                    `json:"reason,omitempty"`
	VolumeBytes      int64                     `json:"volume_bytes,string"`
	FundedBytes      *int64                    `json:"funded_bytes,string,omitempty"`
	UsedBytes        *int64                    `json:"used_bytes,string,omitempty"`
	RefundBytes      *int64                    `json:"refund_bytes,string,omitempty"`
}

type VolumeExpiryRefundReceipt struct {
	Generation           string                    `json:"generation"`
	ClientID             string                    `json:"client_id"`
	RepresentativeID     string                    `json:"representative_id"`
	Trigger              string                    `json:"trigger"`
	ExpiresUnix          int64                     `json:"expires_unix"`
	ChargedVolumeBytes   int64                     `json:"charged_volume_bytes,string"`
	FundedBytes          int64                     `json:"funded_bytes,string"`
	PreviouslyRefunded   int64                     `json:"previously_refunded_bytes,string"`
	UsedBytes            int64                     `json:"used_bytes,string"`
	RefundedBytes        int64                     `json:"refunded_bytes,string"`
	VolumeBefore         int64                     `json:"volume_before_bytes,string"`
	VolumeAfter          int64                     `json:"volume_after_bytes,string"`
	RepresentativeBefore int64                     `json:"representative_before_bytes,string"`
	RepresentativeAfter  int64                     `json:"representative_after_bytes,string"`
	Proofs               []VolumeExpiryRefundProof `json:"proofs"`
	CreatedUnix          int64                     `json:"created_unix"`
}

type VolumeExpiryRefundResult struct {
	Status   VolumeExpiryRefundStatus   `json:"status"`
	Receipt  *VolumeExpiryRefundReceipt `json:"receipt,omitempty"`
	Applied  bool                       `json:"applied"`
	Replayed bool                       `json:"replayed"`
}

type VolumeExpiryRefundSweep struct {
	Examined     int                        `json:"examined"`
	Applied      int                        `json:"applied"`
	NextClientID string                     `json:"next_client_id"`
	Results      []VolumeExpiryRefundResult `json:"results"`
}

var volumeRefundBaseSchema = []string{
	`CREATE TABLE volume_refund_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))`,
	`CREATE TABLE volume_refund_receipts(
 generation TEXT PRIMARY KEY,client_id TEXT NOT NULL REFERENCES clients(id),
 representative_id TEXT NOT NULL REFERENCES representatives(id),client_uuid TEXT NOT NULL,
 expires_unix INTEGER NOT NULL,charged_volume_bytes INTEGER NOT NULL CHECK(charged_volume_bytes>=0),
 trigger_kind TEXT NOT NULL CHECK(trigger_kind IN('expired','archived')),
 refunded_bytes INTEGER NOT NULL CHECK(typeof(refunded_bytes)='integer' AND refunded_bytes>=0),
 receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL,
 UNIQUE(client_id,expires_unix,charged_volume_bytes))`,
	`CREATE INDEX volume_refund_representative_history ON volume_refund_receipts(representative_id,created_unix,generation)`,
	`CREATE TRIGGER volume_refund_receipts_no_update BEFORE UPDATE ON volume_refund_receipts BEGIN SELECT RAISE(ABORT,'volume refund receipts immutable'); END`,
	`CREATE TRIGGER volume_refund_receipts_no_delete BEFORE DELETE ON volume_refund_receipts BEGIN SELECT RAISE(ABORT,'volume refund receipts immutable'); END`,
	`CREATE TRIGGER volume_refund_meta_no_update BEFORE UPDATE ON volume_refund_meta BEGIN SELECT RAISE(ABORT,'volume refund policy immutable'); END`,
	`CREATE TRIGGER volume_refund_meta_no_delete BEFORE DELETE ON volume_refund_meta BEGIN SELECT RAISE(ABORT,'volume refund policy immutable'); END`,
}

var volumeRefundSchema = append(append([]string{}, volumeRefundBaseSchema...), volumeRefundTrackingSchema...)

// EnableVolumeExpiryRefunds is explicit startup/migration wiring. It neither
// initializes unknown usage nor credits any account; the supervisor sweep is
// separate. The net volume cap survives rollback to existing renewal code.
func (s *Store) EnableVolumeExpiryRefunds(ctx context.Context) error {
	if s.usage == nil {
		return ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'volume_refund_*'`).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		// A partial or modified policy is not repaired into financial authority.
		// The complete original refund policy may explicitly gain prospective
		// exposure tracking; no historical identity receives a fresh marker.
		if count == len(volumeRefundBaseSchema) {
			if err = verifyVolumeRefundSchema(ctx, tx, volumeRefundBaseSchema); err != nil {
				return err
			}
			for _, q := range volumeRefundTrackingSchema {
				if _, err = tx.ExecContext(ctx, q); err != nil {
					return err
				}
			}
		}
		if err = volumeRefundConfigured(ctx, tx); err != nil {
			return err
		}
		return tx.Commit()
	}
	for _, q := range volumeRefundSchema {
		if _, err = tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO volume_refund_meta(id,version) VALUES(1,1)`); err != nil {
		return err
	}
	if err = volumeRefundConfigured(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}

func volumeRefundConfigured(ctx context.Context, q sellerQueryV2) error {
	return verifyVolumeRefundSchema(ctx, q, volumeRefundSchema)
}

func verifyVolumeRefundSchema(ctx context.Context, q sellerQueryV2, schema []string) error {
	var count int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'volume_refund_*'`).Scan(&count); err != nil || count != len(schema) {
		return ErrUnavailable
	}
	var version int
	if err := q.QueryRowContext(ctx, `SELECT version FROM volume_refund_meta WHERE id=1`).Scan(&version); err != nil || version != 1 {
		return ErrUnavailable
	}
	for _, ddl := range schema {
		name := strings.Fields(ddl)[2]
		if i := strings.IndexByte(name, '('); i >= 0 {
			name = name[:i]
		}
		var actual string
		if err := q.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE name=?`, name).Scan(&actual); err != nil || strings.Join(strings.Fields(actual), " ") != strings.Join(strings.Fields(ddl), " ") {
			return ErrUnavailable
		}
	}
	return nil
}

func volumeRefundGeneration(c Client) string {
	// Trigger is deliberately absent: archiving and later expiry must never
	// refund the same purchased entitlement twice.
	return fingerprint("representative-unused-volume-v1", struct {
		ClientID, UUID, RepresentativeID string
		ExpiresUnix, ChargedVolumeBytes  int64
	}{c.ID, c.UUID, c.RepresentativeID, c.ExpiresUnix, c.ChargeVolumeBytes})
}

func refundRequestFor(c Client) VolumeExpiryRefundRequest {
	return VolumeExpiryRefundRequest{c.ID, c.ExpiresUnix, c.ChargeVolumeBytes}
}

func readVolumeRefundReceipt(ctx context.Context, tx *sql.Tx, req VolumeExpiryRefundRequest) (*VolumeExpiryRefundReceipt, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT receipt_json FROM volume_refund_receipts
 WHERE client_id=? AND expires_unix=? AND charged_volume_bytes=?`, req.ClientID, req.ExpectedExpiresUnix, req.ExpectedChargedVolumeBytes).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out VolumeExpiryRefundReceipt
	if json.Unmarshal(raw, &out) != nil || out.ClientID != req.ClientID || out.ExpiresUnix != req.ExpectedExpiresUnix || out.ChargedVolumeBytes != req.ExpectedChargedVolumeBytes {
		return nil, ErrUnavailable
	}
	return &out, nil
}

func refundableFunding(ctx context.Context, tx *sql.Tx, c Client) (funded, prior int64, proven bool, err error) {
	// Current charge/display fields alone are insufficient proof. Derive paid
	// capacity from immutable debits to this representative for this identity.
	var initialCount int
	var initial int64
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(-volume_delta_bytes),0) FROM events
 WHERE kind='client_created' AND object_id=? AND representative_id=? AND volume_delta_bytes<0
 AND unlimited_delta_units=0`, c.ID, c.RepresentativeID).Scan(&initialCount, &initial)
	if err != nil {
		return 0, 0, false, err
	}
	sellerFunding, sellerIssued, err := sellerClientFundingV1(ctx, tx, c)
	if err != nil {
		return 0, 0, false, err
	}
	if sellerIssued {
		// The seller reserve was funded from this representative. Its sale
		// marker has zero free-credit debit; count only the verified reserve
		// receipt. A second representative-funded creation is ambiguous.
		if initialCount != 0 || sellerFunding.initialBytes <= 0 {
			return 0, 0, false, nil
		}
		initial = sellerFunding.initialBytes
	} else if initialCount != 1 || initial <= 0 {
		return 0, 0, false, nil
	}
	var renewals, edits int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(volume_credit_debit),0) FROM client_operations
 WHERE client_id=? AND representative_id=? AND kind='extend'`, c.ID, c.RepresentativeID).Scan(&renewals); err != nil {
		return 0, 0, false, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(volume_credit_debit_bytes),0) FROM client_profile_edits
 WHERE client_id=? AND representative_id=? AND plan_kind='volume'`, c.ID, c.RepresentativeID).Scan(&edits); err != nil {
		return 0, 0, false, err
	}
	funded, ok := safeAdd(initial, renewals, MaxVolumeCredit)
	if !ok {
		return 0, 0, false, ErrUnavailable
	}
	funded, ok = safeAdd(funded, sellerFunding.renewalBytes, MaxVolumeCredit)
	if !ok {
		return 0, 0, false, ErrUnavailable
	}
	funded, ok = safeAdd(funded, edits, MaxVolumeCredit)
	if !ok {
		return 0, 0, false, ErrUnavailable
	}
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(refunded_bytes),0) FROM volume_refund_receipts
 WHERE client_id=?`, c.ID).Scan(&prior); err != nil {
		return 0, 0, false, err
	}
	// Refuse mixed/imported/unexplained capacity instead of guessing which
	// party paid for consumed bytes. Normal paid top-ups satisfy this identity.
	return funded, prior, funded == c.ChargeVolumeBytes && prior <= funded && funded-prior == c.VolumeBytes, nil
}

type volumeRefundEvaluation struct {
	status  VolumeExpiryRefundStatus
	client  Client
	rep     Representative
	prior   int64
	proofs  []VolumeExpiryRefundProof
	receipt *VolumeExpiryRefundReceipt
}

func (s *Store) evaluateVolumeRefund(ctx context.Context, tx *sql.Tx, key []byte, c Client, now int64) (volumeRefundEvaluation, error) {
	v := volumeRefundEvaluation{client: c, status: VolumeExpiryRefundStatus{
		Request: refundRequestFor(c), RepresentativeID: c.RepresentativeID, Generation: volumeRefundGeneration(c),
		VolumeBytes: c.VolumeBytes, State: "not_due",
	}}
	if c.PlanKind != "volume" {
		v.status.State = "not_volume"
		return v, nil
	}
	rep, err := readRep(ctx, tx, c.RepresentativeID)
	if err != nil {
		return v, err
	}
	v.rep = rep
	if rep.Kind != "representative" || rep.ID == "owner" {
		v.status.State = "owner_exempt"
		return v, nil
	}
	old, err := readVolumeRefundReceipt(ctx, tx, v.status.Request)
	if err != nil {
		return v, err
	}
	if old != nil {
		v.receipt = old
		v.status.State, v.status.Trigger = "already_refunded", old.Trigger
		v.status.UsedBytes, v.status.RefundBytes, v.status.FundedBytes = &old.UsedBytes, &old.RefundedBytes, &old.FundedBytes
		return v, nil
	}
	if c.Archived {
		v.status.Trigger = "archived"
	} else if c.ExpiresUnix <= now {
		v.status.Trigger = "expired"
	} else {
		return v, nil
	}
	funded, prior, proven, err := refundableFunding(ctx, tx, c)
	if err != nil {
		return v, err
	}
	if !proven {
		v.status.State, v.status.Reason = "funding_unproven", "immutable_representative_debits_incomplete"
		return v, nil
	}
	v.prior = prior
	v.status.FundedBytes = &funded
	var up, down, holds int64
	var known bool
	err = tx.QueryRowContext(ctx, `SELECT uplink,downlink,baseline_complete,holds FROM pro_usage_accounts WHERE client_id=?`, c.ID).Scan(&up, &down, &known, &holds)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !known {
		v.status.State, v.status.Reason = "usage_unknown", "authoritative_baseline_missing"
		return v, nil
	}
	if err != nil {
		return v, err
	}
	if up < 0 || down < 0 || up > int64(^uint64(0)>>1)-down {
		return v, ErrUnavailable
	}
	used := up + down
	v.status.UsedBytes = &used
	if holds != 0 {
		v.status.State, v.status.Reason = "waiting_final_usage", "reconciliation_hold"
		return v, nil
	}
	proofs, blocker, err := volumeRefundFinalProofs(ctx, tx, key, c.ID)
	if err != nil {
		return v, err
	}
	if blocker != "" {
		v.status.State, v.status.Reason = "waiting_final_usage", blocker
		return v, nil
	}
	if len(proofs) == 0 {
		proof, reason, err := volumeRefundNeverIssued(ctx, tx, key, c, used)
		if err != nil {
			return v, err
		}
		if reason != "" {
			v.status.State, v.status.Reason = "waiting_final_usage", reason
			return v, nil
		}
		proofs = append(proofs, proof)
	}
	v.proofs = proofs
	refund := int64(0)
	if used < c.VolumeBytes {
		refund = c.VolumeBytes - used
	}
	if refund > funded-prior {
		return v, ErrUnavailable
	}
	v.status.RefundBytes = &refund
	if _, ok := safeAdd(rep.VolumeCreditBytes, refund, MaxVolumeCredit); !ok {
		v.status.State, v.status.Reason = "credit_limit", "representative_credit_capacity"
		return v, nil
	}
	v.status.State = "ready"
	return v, nil
}

func (s *Store) VolumeExpiryRefundStatus(ctx context.Context, key []byte, clientID string) (VolumeExpiryRefundStatus, error) {
	if !idRE.MatchString(clientID) {
		return VolumeExpiryRefundStatus{}, ErrInvalid
	}
	if s.usage == nil {
		return VolumeExpiryRefundStatus{}, ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return VolumeExpiryRefundStatus{}, err
	}
	defer tx.Rollback()
	if err = volumeRefundConfigured(ctx, tx); err != nil {
		return VolumeExpiryRefundStatus{}, err
	}
	c, err := readClient(ctx, tx, clientID)
	if errors.Is(err, sql.ErrNoRows) {
		return VolumeExpiryRefundStatus{}, ErrNotFound
	}
	if err != nil {
		return VolumeExpiryRefundStatus{}, err
	}
	v, err := s.evaluateVolumeRefund(ctx, tx, key, c, time.Now().UTC().Unix())
	return v.status, err
}

func (s *Store) RefundExpiredVolume(ctx context.Context, key []byte, req VolumeExpiryRefundRequest) (VolumeExpiryRefundResult, error) {
	var out VolumeExpiryRefundResult
	if !idRE.MatchString(req.ClientID) || req.ExpectedExpiresUnix <= 0 || req.ExpectedChargedVolumeBytes < 0 {
		return out, ErrInvalid
	}
	if s.usage == nil {
		return out, ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = volumeRefundConfigured(ctx, tx); err != nil {
		return out, err
	}
	// Replaying a completed old generation after renewal returns its original
	// immutable receipt; it cannot recalculate against the renewed capacity.
	old, err := readVolumeRefundReceipt(ctx, tx, req)
	if err != nil {
		return out, err
	}
	if old != nil {
		out.Receipt, out.Replayed = old, true
		out.Status = VolumeExpiryRefundStatus{Request: req, RepresentativeID: old.RepresentativeID, Generation: old.Generation,
			Trigger: old.Trigger, State: "already_refunded", VolumeBytes: old.VolumeAfter,
			FundedBytes: &old.FundedBytes, UsedBytes: &old.UsedBytes, RefundBytes: &old.RefundedBytes}
		return out, nil
	}
	c, err := readClient(ctx, tx, req.ClientID)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if c.ExpiresUnix != req.ExpectedExpiresUnix || c.ChargeVolumeBytes != req.ExpectedChargedVolumeBytes {
		return out, ErrConflict
	}
	v, err := s.evaluateVolumeRefund(ctx, tx, key, c, time.Now().UTC().Unix())
	if err != nil {
		return out, err
	}
	out.Status = v.status
	if v.status.State != "ready" {
		return out, nil
	}
	refund := *v.status.RefundBytes
	after, ok := safeAdd(v.rep.VolumeCreditBytes, refund, MaxVolumeCredit)
	if !ok {
		return out, ErrConflict
	}
	receipt := VolumeExpiryRefundReceipt{Generation: v.status.Generation, ClientID: c.ID, RepresentativeID: c.RepresentativeID,
		Trigger: v.status.Trigger, ExpiresUnix: c.ExpiresUnix, ChargedVolumeBytes: c.ChargeVolumeBytes,
		FundedBytes: *v.status.FundedBytes, PreviouslyRefunded: v.prior, UsedBytes: *v.status.UsedBytes,
		RefundedBytes: refund, VolumeBefore: c.VolumeBytes, VolumeAfter: c.VolumeBytes - refund,
		RepresentativeBefore: v.rep.VolumeCreditBytes, RepresentativeAfter: after, Proofs: v.proofs, CreatedUnix: time.Now().UTC().Unix()}
	// Only volume changes. Existing triggers invalidate runtime generations;
	// cumulative usage, UUID, paid-history counters and all token fields stay.
	if _, err = tx.ExecContext(ctx, `UPDATE representatives SET volume_credit_bytes=? WHERE id=?`, after, c.RepresentativeID); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE clients SET volume_bytes=? WHERE id=?`, receipt.VolumeAfter, c.ID); err != nil {
		return out, err
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO volume_refund_receipts
 (generation,client_id,representative_id,client_uuid,expires_unix,charged_volume_bytes,trigger_kind,refunded_bytes,receipt_json,created_unix)
 VALUES(?,?,?,?,?,?,?,?,?,?)`, receipt.Generation, c.ID, c.RepresentativeID, c.UUID, c.ExpiresUnix, c.ChargeVolumeBytes,
		receipt.Trigger, refund, raw, receipt.CreatedUnix); err != nil {
		return out, err
	}
	if err = tx.Commit(); err != nil {
		return out, err
	}
	out.Receipt, out.Applied = &receipt, true
	out.Status.State, out.Status.VolumeBytes = "refunded", receipt.VolumeAfter
	return out, nil
}

// A stable cursor moves past pending clients. The caller starts at "" again
// when NextClientID is empty, so newly expired earlier IDs are revisited.
func (s *Store) ListVolumeExpiryRefundCandidates(ctx context.Context, afterClientID string, limit int) ([]VolumeExpiryRefundRequest, error) {
	if limit < 1 || limit > 100 || afterClientID != "" && !idRE.MatchString(afterClientID) {
		return nil, ErrInvalid
	}
	if s.usage == nil {
		return nil, ErrUnavailable
	}
	if err := volumeRefundConfigured(ctx, s.db); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id,c.expires_unix,c.charged_volume_bytes FROM clients c
 JOIN representatives r ON r.id=c.representative_id WHERE c.id>? AND c.plan_kind='volume'
 AND r.kind='representative' AND (c.expires_unix<=? OR c.archived=1)
 AND NOT EXISTS(SELECT 1 FROM volume_refund_receipts f WHERE f.client_id=c.id AND f.expires_unix=c.expires_unix
 AND f.charged_volume_bytes=c.charged_volume_bytes) ORDER BY c.id LIMIT ?`, afterClientID, time.Now().UTC().Unix(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VolumeExpiryRefundRequest, 0)
	for rows.Next() {
		var req VolumeExpiryRefundRequest
		if err = rows.Scan(&req.ClientID, &req.ExpectedExpiresUnix, &req.ExpectedChargedVolumeBytes); err != nil {
			return nil, err
		}
		out = append(out, req)
	}
	return out, rows.Err()
}

func (s *Store) SweepExpiredVolumeRefunds(ctx context.Context, key []byte, afterClientID string, limit int) (VolumeExpiryRefundSweep, error) {
	out := VolumeExpiryRefundSweep{NextClientID: afterClientID, Results: []VolumeExpiryRefundResult{}}
	candidates, err := s.ListVolumeExpiryRefundCandidates(ctx, afterClientID, limit)
	if err != nil {
		return out, err
	}
	for _, req := range candidates {
		result, err := s.RefundExpiredVolume(ctx, key, req)
		out.Examined++
		out.NextClientID = req.ClientID
		if errors.Is(err, ErrConflict) {
			result.Status = VolumeExpiryRefundStatus{Request: req, State: "superseded", Reason: "entitlement_changed"}
		} else if err != nil {
			return out, err
		}
		if result.Applied {
			out.Applied++
		}
		out.Results = append(out.Results, result)
	}
	if len(candidates) < limit {
		out.NextClientID = ""
	}
	return out, nil
}
