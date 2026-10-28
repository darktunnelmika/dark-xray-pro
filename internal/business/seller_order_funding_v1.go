package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

type sellerOrderRecordV1 struct {
	fingerprint string
	intent      sellerOrderIntentV1
	receipt     SellerOrderReceiptV1
}

const sellerOrderColumnsV1 = `event_id,fingerprint,actor_user_id,representative_id,seller_id,client_id,action,
 volume_debit_bytes,unlimited_debit_units,reserved_bytes_before,reserved_bytes_after,
 reserved_units_before,reserved_units_after,request_json,receipt_json,created_unix`

func readSellerOrderRecordV1(ctx context.Context, tx *sql.Tx, id string) (sellerOrderRecordV1, error) {
	return scanSellerOrderRecordV1(tx.QueryRowContext(ctx, `SELECT `+sellerOrderColumnsV1+` FROM seller_order_receipts_v1 WHERE event_id=?`, id))
}

// Canonical JSON round trips reject extra fields, duplicate keys, altered
// fingerprints, mismatched typed facts and a fabricated replay flag.
func scanSellerOrderRecordV1(row interface{ Scan(...any) error }) (sellerOrderRecordV1, error) {
	var out sellerOrderRecordV1
	var facts SellerOrderReceiptV1
	var clientID string
	var request, raw []byte
	err := row.Scan(&facts.EventID, &out.fingerprint, &facts.ActorUserID, &facts.RepresentativeID, &facts.SellerID, &clientID, &facts.Action,
		&facts.VolumeDebitBytes, &facts.UnlimitedDebitUnits, &facts.ReservedBytesBefore, &facts.ReservedBytesAfter,
		&facts.ReservedUnitsBefore, &facts.ReservedUnitsAfter, &request, &raw, &facts.CreatedUnix)
	if err != nil {
		return out, err
	}
	if len(request) > 16384 || len(raw) > 32768 || json.Unmarshal(request, &out.intent) != nil || json.Unmarshal(raw, &out.receipt) != nil {
		return out, ErrUnavailable
	}
	canonical, err := json.Marshal(out.intent)
	if err != nil || !bytes.Equal(canonical, request) {
		return out, ErrUnavailable
	}
	facts.Client = out.receipt.Client
	facts.PreviousClient = out.receipt.PreviousClient
	canonical, err = json.Marshal(facts)
	if err != nil || !bytes.Equal(canonical, raw) || facts.Client.ID != clientID {
		return out, ErrUnavailable
	}
	k, err := out.intent.identity()
	if err != nil {
		return out, ErrUnavailable
	}
	if k.event != facts.EventID || k.representative != facts.RepresentativeID || k.seller != facts.SellerID || k.client != clientID || k.action != facts.Action ||
		facts.Client.RepresentativeID != facts.RepresentativeID || facts.CreatedUnix <= 0 ||
		out.fingerprint != sellerOrderFingerprintV1(facts.ActorUserID, out.intent) {
		return out, ErrUnavailable
	}
	for _, pair := range [][2]int64{{facts.ReservedBytesBefore, MaxVolumeCredit}, {facts.ReservedBytesAfter, MaxVolumeCredit},
		{facts.VolumeDebitBytes, MaxVolumeCredit}, {facts.ReservedUnitsBefore, MaxUnlimitedCredit}, {facts.ReservedUnitsAfter, MaxUnlimitedCredit}, {facts.UnlimitedDebitUnits, MaxUnlimitedCredit}} {
		if pair[0] < 0 || pair[0] > pair[1] {
			return out, ErrUnavailable
		}
	}
	if facts.ReservedBytesBefore-facts.VolumeDebitBytes != facts.ReservedBytesAfter || facts.ReservedUnitsBefore-facts.UnlimitedDebitUnits != facts.ReservedUnitsAfter {
		return out, ErrUnavailable
	}
	if c := out.intent.Create; c != nil {
		plan := "volume"
		units := int64(0)
		if c.Unlimited {
			plan = "unlimited"
			units = (c.DurationDays*c.IPCount + 29) / 30
		}
		want := SellerOrderClientV1{ID: c.ClientID, RepresentativeID: c.RepresentativeID, Name: c.Name, UUID: facts.Client.UUID,
			PlanKind: plan, IPCount: c.IPCount, VolumeBytes: c.VolumeBytes, DurationDays: c.DurationDays,
			ChargedVolumeBytes: c.VolumeBytes, ChargedUnlimitedUnits: units, CreatedUnix: facts.CreatedUnix,
			ExpiresUnix: facts.CreatedUnix + c.DurationDays*86400, DeploymentState: "pending_node_apply"}
		if facts.PreviousClient != nil || facts.Client != want || facts.VolumeDebitBytes != c.VolumeBytes || facts.UnlimitedDebitUnits != units {
			return out, ErrUnavailable
		}
	} else {
		if facts.PreviousClient == nil {
			return out, ErrUnavailable
		}
		before := facts.PreviousClient.client()
		if before.ID != clientID || before.RepresentativeID != facts.RepresentativeID {
			return out, ErrUnavailable
		}
		change, err := calculateBulkExtension(before, out.intent.Extend.AdditionalDays, out.intent.Extend.AdditionalVolumeBytes, facts.CreatedUnix)
		if err != nil || sellerOrderClientSnapshotV1(change.Client) != facts.Client || change.VolumeCost != facts.VolumeDebitBytes || change.UnitsCost != facts.UnlimitedDebitUnits {
			return out, ErrUnavailable
		}
	}
	return out, nil
}

func (c SellerOrderClientV1) client() Client {
	return Client{ID: c.ID, RepresentativeID: c.RepresentativeID, Name: c.Name, UUID: c.UUID, PlanKind: c.PlanKind, IPCount: c.IPCount,
		VolumeBytes: c.VolumeBytes, DurationDays: c.DurationDays, ChargeVolumeBytes: c.ChargedVolumeBytes, ChargeUnlimitedUnits: c.ChargedUnlimitedUnits,
		ExpiresUnix: c.ExpiresUnix, CreatedUnix: c.CreatedUnix, DeploymentState: c.DeploymentState, Archived: c.Archived}
}

func verifySellerClientBindingV1(ctx context.Context, tx *sql.Tx, c Client, seller string) error {
	var rep, oldSeller, uuid string
	err := tx.QueryRowContext(ctx, `SELECT representative_id,seller_id,client_uuid FROM seller_order_clients_v1 WHERE client_id=?`, c.ID).Scan(&rep, &oldSeller, &uuid)
	if errors.Is(err, sql.ErrNoRows) || err == nil && (rep != c.RepresentativeID || oldSeller != seller) {
		return ErrSellerPermissionDenied
	}
	if err != nil || uuid != c.UUID {
		return ErrUnavailable
	}
	return nil
}

// Validate the separate reserve debit against both the immutable binding and
// the existing journal's exact zero representative-free-credit marker.
func verifiedSellerOrderV1(ctx context.Context, tx *sql.Tx, record sellerOrderRecordV1) (SellerOrderReceiptV1, error) {
	r := record.receipt
	c, err := readClient(ctx, tx, r.Client.ID)
	if err != nil {
		return r, ErrUnavailable
	}
	if c.RepresentativeID != r.RepresentativeID || c.UUID != r.Client.UUID || c.PlanKind != r.Client.PlanKind {
		return r, ErrUnavailable
	}
	if err = verifySellerClientBindingV1(ctx, tx, c, r.SellerID); err != nil {
		return r, ErrUnavailable
	}
	var fp, rep, client, kind, reason string
	var debitBytes, debitUnits, at int64
	k, _ := record.intent.identity()
	if r.Action == "create" {
		var createEvent string
		if err = tx.QueryRowContext(ctx, `SELECT create_event_id FROM seller_order_clients_v1 WHERE client_id=?`, c.ID).Scan(&createEvent); err != nil || createEvent != r.EventID {
			return r, ErrUnavailable
		}
		err = tx.QueryRowContext(ctx, `SELECT fingerprint,representative_id,object_id,kind,reason,volume_delta_bytes,unlimited_delta_units,created_unix
 FROM events WHERE event_id=?`, r.EventID).Scan(&fp, &rep, &client, &kind, &reason, &debitBytes, &debitUnits, &at)
		if kind != "client_created" {
			return r, ErrUnavailable
		}
	} else {
		var days, volume int64
		err = tx.QueryRowContext(ctx, `SELECT fingerprint,representative_id,client_id,kind,reason,volume_credit_debit,unlimited_credit_debit,created_unix,added_days,added_volume_bytes
 FROM client_operations WHERE event_id=?`, r.EventID).Scan(&fp, &rep, &client, &kind, &reason, &debitBytes, &debitUnits, &at, &days, &volume)
		if kind != "extend" || days != record.intent.Extend.AdditionalDays || volume != record.intent.Extend.AdditionalVolumeBytes {
			return r, ErrUnavailable
		}
	}
	if err != nil || fp != record.fingerprint || rep != r.RepresentativeID || client != r.Client.ID || reason != k.reason || debitBytes != 0 || debitUnits != 0 || at != r.CreatedUnix {
		return r, ErrUnavailable
	}
	return r, nil
}

type sellerFundingV1 struct{ initialBytes, renewalBytes int64 }

// Optional provenance is independent of runtime/UI flags. An absent schema
// leaves ordinary representative funding unchanged; partial/altered provenance
// never creates a balance. A seller origin is valid exactly once per identity.
func sellerClientFundingV1(ctx context.Context, tx *sql.Tx, c Client) (sellerFundingV1, bool, error) {
	var total sellerFundingV1
	present, err := sellerOrderSchemaStateV1(ctx, tx)
	if err != nil || !present {
		return total, false, err
	}
	var seller, rep, uuid, createEvent string
	err = tx.QueryRowContext(ctx, `SELECT seller_id,representative_id,client_uuid,create_event_id FROM seller_order_clients_v1 WHERE client_id=?`, c.ID).Scan(&seller, &rep, &uuid, &createEvent)
	if errors.Is(err, sql.ErrNoRows) {
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM seller_order_receipts_v1 WHERE client_id=?`, c.ID).Scan(&n); err != nil || n != 0 {
			return total, false, ErrUnavailable
		}
		return total, false, nil
	}
	if err != nil || rep != c.RepresentativeID || uuid != c.UUID {
		return total, false, ErrUnavailable
	}
	creates := 0
	after := ""
	// Close each bounded page before checking its native journal. All pages
	// share the same transaction snapshot, so a long history is never truncated.
	for {
		rows, err := tx.QueryContext(ctx, `SELECT `+sellerOrderColumnsV1+` FROM seller_order_receipts_v1 WHERE client_id=? AND event_id>? ORDER BY event_id LIMIT 64`, c.ID, after)
		if err != nil {
			return total, false, ErrUnavailable
		}
		records := make([]sellerOrderRecordV1, 0, 64)
		for rows.Next() {
			r, e := scanSellerOrderRecordV1(rows)
			if e != nil {
				rows.Close()
				return total, false, ErrUnavailable
			}
			records = append(records, r)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil || closeErr != nil {
			return total, false, ErrUnavailable
		}
		for _, record := range records {
			r, e := verifiedSellerOrderV1(ctx, tx, record)
			if e != nil || r.SellerID != seller {
				return total, false, ErrUnavailable
			}
			if r.Action == "create" {
				creates++
				if r.EventID != createEvent {
					return total, false, ErrUnavailable
				}
				total.initialBytes = r.VolumeDebitBytes
			} else {
				var ok bool
				total.renewalBytes, ok = safeAdd(total.renewalBytes, r.VolumeDebitBytes, MaxVolumeCredit)
				if !ok {
					return total, false, ErrUnavailable
				}
			}
			after = r.EventID
		}
		if len(records) < 64 {
			break
		}
		if ctx.Err() != nil {
			return total, false, ErrUnavailable
		}
	}
	if creates != 1 {
		return total, false, ErrUnavailable
	}
	return total, true, nil
}
