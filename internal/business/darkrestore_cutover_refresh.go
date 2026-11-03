package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"time"
)

const DarkRestoreCutoverRefreshScopeV1 = "darkrestore_cutover_state_refresh_v1"

type cutoverRefreshQuery interface {
	sellerQueryV2
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

type DarkRestoreCutoverRefreshInputV1 struct {
	EventID          string                       `json:"event_id"`
	CauseEventID     string                       `json:"cause_event_id"`
	Reason           string                       `json:"reason"`
	PreviousSnapshot DarkRestoreCutoverSnapshotV1 `json:"previous_snapshot"`
}

type DarkRestoreCutoverRefreshReceiptV1 struct {
	Scope                    string                       `json:"scope"`
	ClientID                 string                       `json:"client_id"`
	ActorUserID              string                       `json:"actor_user_id"`
	EventID                  string                       `json:"event_id"`
	CauseEventID             string                       `json:"cause_event_id"`
	CauseKind                string                       `json:"cause_kind"`
	Reason                   string                       `json:"reason"`
	Revision                 uint64                       `json:"revision"`
	TransitionSHA            string                       `json:"transition_sha256"`
	SettledHistoricalBytes   int64                        `json:"settled_historical_bytes"`
	PreviousSnapshot         DarkRestoreCutoverSnapshotV1 `json:"previous_snapshot"`
	CurrentSnapshot          DarkRestoreCutoverSnapshotV1 `json:"current_snapshot"`
	CurrentFiniteEntitlement int64                        `json:"current_finite_entitlement_bytes"`
	RemainingBudgetBytes     int64                        `json:"remaining_budget_bytes"`
	CreatedUnix              int64                        `json:"created_unix"`
	AuthorizesMutation       bool                         `json:"authorizes_mutation"`
	Replayed                 bool                         `json:"replayed"`
}

func canonicalCutoverRefreshSnapshot(snapshot DarkRestoreCutoverSnapshotV1) (DarkRestoreCutoverSnapshotV1, error) {
	if snapshot.AuthorizesMutation {
		return snapshot, ErrDarkRestoreCutover
	}
	stateSHA, baselineSHA := snapshot.StateSHA, snapshot.NativeBaselineSHA
	snapshot.StateSHA, snapshot.NativeBaselineSHA = "", ""
	canonical, err := hashDarkRestoreCutoverSnapshotV1(snapshot)
	if err != nil || canonical.StateSHA != stateSHA || canonical.NativeBaselineSHA != baselineSHA {
		return snapshot, ErrDarkRestoreCutover
	}
	return canonical, nil
}

func sameCutoverRefreshStableState(a, b DarkRestoreCutoverSnapshotV1) bool {
	return a.SourceSHA == b.SourceSHA && a.SourceID == b.SourceID &&
		a.ClientID == b.ClientID && a.ClientUUID == b.ClientUUID &&
		a.ClientCreatedUnix == b.ClientCreatedUnix &&
		a.ImportEventID == b.ImportEventID && a.ImportReceiptSHA == b.ImportReceiptSHA &&
		a.ImportedHistoricalBytes == b.ImportedHistoricalBytes &&
		a.PlanKind == "volume" && b.PlanKind == "volume" &&
		a.DeploymentState == "disabled" && b.DeploymentState == "disabled" &&
		!a.Archived && !b.Archived &&
		a.NativeAccountPresent && b.NativeAccountPresent &&
		a.NativeBaselineComplete && b.NativeBaselineComplete &&
		a.NativeBaselineSHA == b.NativeBaselineSHA &&
		reflect.DeepEqual(a.NativeUplink, b.NativeUplink) &&
		reflect.DeepEqual(a.NativeDownlink, b.NativeDownlink) &&
		reflect.DeepEqual(a.NativeHolds, b.NativeHolds) &&
		a.NativeUplink != nil && a.NativeDownlink != nil && a.NativeHolds != nil &&
		*a.NativeHolds == 0 && *b.NativeHolds == 0
}

func verifyCutoverRefreshCause(ctx context.Context, q sellerQueryV2, client, event string, previous, current DarkRestoreCutoverSnapshotV1, opened int64) (string, int64, error) {
	var representative, kind string
	var days, volume, created int64
	err := q.QueryRowContext(ctx, `SELECT representative_id,kind,added_days,added_volume_bytes,created_unix FROM client_operations WHERE event_id=? AND client_id=?`, event, client).
		Scan(&representative, &kind, &days, &volume, &created)
	if err == nil {
		if kind != "extend" || created < opened/1_000_000_000 ||
			representative != previous.RepresentativeID || current.RepresentativeID != previous.RepresentativeID ||
			days < 0 || volume < 0 || days == 0 && volume == 0 ||
			previous.CurrentEntitlement > MaxVolumeCredit-volume ||
			current.CurrentEntitlement != previous.CurrentEntitlement+volume {
			return "", 0, ErrDarkRestoreCutover
		}
		expires := previous.ExpiresUnix
		if expires < created && days > 0 {
			expires = created
		}
		if days > MaxDays || expires > int64(^uint64(0)>>1)-days*86400 || current.ExpiresUnix != expires+days*86400 {
			return "", 0, ErrDarkRestoreCutover
		}
		return "client_extension", created, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", 0, err
	}

	var oldRepresentative, newRepresentative string
	var receiptRaw []byte
	err = q.QueryRowContext(ctx, `SELECT e.previous_representative_id,e.representative_id,r.receipt_json,r.created_unix
 FROM client_ownership_entries_v1 e
 JOIN client_ownership_receipts_v1 r ON r.event_id=e.event_id
 WHERE e.event_id=? AND e.client_id=?`, event, client).
		Scan(&oldRepresentative, &newRepresentative, &receiptRaw, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, ErrDarkRestoreCutover
	}
	if err != nil || created < opened/1_000_000_000 ||
		oldRepresentative != previous.RepresentativeID || newRepresentative != current.RepresentativeID ||
		previous.CurrentEntitlement != current.CurrentEntitlement ||
		previous.ExpiresUnix != current.ExpiresUnix {
		return "", 0, ErrDarkRestoreCutover
	}
	var receipt ClientOwnershipReceipt
	if json.Unmarshal(receiptRaw, &receipt) != nil || receipt.Replayed ||
		receipt.EventID != event || receipt.BalanceChanged || receipt.UsageReset || receipt.CredentialsChanged {
		return "", 0, ErrDarkRestoreCutover
	}
	found := false
	for _, item := range receipt.Clients {
		if item.ClientID == client &&
			item.PreviousRepresentativeID == oldRepresentative &&
			item.RepresentativeID == newRepresentative &&
			item.UsagePresent && item.UsageComplete &&
			item.Holds == 0 &&
			previous.NativeUplink != nil && previous.NativeDownlink != nil &&
			item.Uplink == *previous.NativeUplink && item.Downlink == *previous.NativeDownlink {
			found = true
		}
	}
	if !found {
		return "", 0, ErrDarkRestoreCutover
	}
	return "ownership_assignment", created, nil
}

func decodeCutoverRefresh(raw []byte, out *DarkRestoreCutoverRefreshReceiptV1) error {
	if len(raw) == 0 || len(raw) > 96<<10 || json.Unmarshal(raw, out) != nil || out.Replayed ||
		out.Scope != DarkRestoreCutoverRefreshScopeV1 || out.AuthorizesMutation ||
		!idRE.MatchString(out.ClientID) || out.ActorUserID == "" ||
		!eventRE.MatchString(out.EventID) || !eventRE.MatchString(out.CauseEventID) ||
		out.EventID == out.CauseEventID || !validName(out.Reason, 200) ||
		(out.CauseKind != "client_extension" && out.CauseKind != "ownership_assignment") ||
		out.Revision == 0 || out.Revision >= routeMax || !routeSHA.MatchString(out.TransitionSHA) ||
		out.SettledHistoricalBytes < 0 || out.CurrentFiniteEntitlement <= 0 ||
		out.RemainingBudgetBytes <= 0 || out.CreatedUnix <= 0 {
		return ErrUnavailable
	}
	previous, err := canonicalCutoverRefreshSnapshot(out.PreviousSnapshot)
	if err != nil {
		return ErrUnavailable
	}
	current, err := canonicalCutoverRefreshSnapshot(out.CurrentSnapshot)
	if err != nil || !sameCutoverRefreshStableState(previous, current) ||
		previous.StateSHA == current.StateSHA ||
		current.CurrentEntitlement != out.CurrentFiniteEntitlement {
		return ErrUnavailable
	}
	canonical, err := json.Marshal(out)
	if err != nil || !reflect.DeepEqual(canonical, raw) {
		return ErrUnavailable
	}
	return nil
}

func readCutoverRefreshChain(ctx context.Context, q cutoverRefreshQuery, evidence *DarkRestoreCutoverEvidenceReceiptV1) ([]DarkRestoreCutoverRefreshReceiptV1, error) {
	if evidence == nil {
		return nil, ErrDarkRestoreCutover
	}
	rows, err := q.QueryContext(ctx, `SELECT revision,event_id,cause_event_id,previous_state_sha,current_state_sha,receipt_json,created_unix
 FROM darkrestore_cutover_refresh_receipts_v1 WHERE client_id=? ORDER BY revision`, evidence.ClientID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DarkRestoreCutoverRefreshReceiptV1{}
	expectedSHA := evidence.DestinationStateSHA
	for rows.Next() {
		var revision uint64
		var event, cause, previousSHA, currentSHA string
		var raw []byte
		var created int64
		if err = rows.Scan(&revision, &event, &cause, &previousSHA, &currentSHA, &raw, &created); err != nil {
			return nil, ErrUnavailable
		}
		var receipt DarkRestoreCutoverRefreshReceiptV1
		if decodeCutoverRefresh(raw, &receipt) != nil ||
			receipt.ClientID != evidence.ClientID || receipt.TransitionSHA != evidence.TransitionSHA ||
			receipt.SettledHistoricalBytes != evidence.SettledHistoricalBytes ||
			receipt.Revision != uint64(len(out)+1) || receipt.Revision != revision ||
			receipt.EventID != event || receipt.CauseEventID != cause ||
			receipt.PreviousSnapshot.StateSHA != previousSHA ||
			receipt.CurrentSnapshot.StateSHA != currentSHA ||
			receipt.PreviousSnapshot.StateSHA != expectedSHA || receipt.CreatedUnix != created {
			return nil, ErrUnavailable
		}
		kind, _, causeErr := verifyCutoverRefreshCause(ctx, q, evidence.ClientID, receipt.CauseEventID, receipt.PreviousSnapshot, receipt.CurrentSnapshot, evidence.TransitionOpenedUnixNano)
		remaining, budgetErr := cutoverRemaining(receipt.CurrentSnapshot.CurrentEntitlement, evidence.SettledHistoricalBytes, *receipt.CurrentSnapshot.NativeUplink, *receipt.CurrentSnapshot.NativeDownlink)
		if causeErr != nil || kind != receipt.CauseKind || budgetErr != nil || remaining != receipt.RemainingBudgetBytes {
			return nil, ErrUnavailable
		}
		out = append(out, receipt)
		expectedSHA = currentSHA
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func effectiveCutoverStateSHA(ctx context.Context, q cutoverRefreshQuery, evidence *DarkRestoreCutoverEvidenceReceiptV1) (string, error) {
	chain, err := readCutoverRefreshChain(ctx, q, evidence)
	if err != nil {
		return "", err
	}
	if len(chain) == 0 {
		return evidence.DestinationStateSHA, nil
	}
	return chain[len(chain)-1].CurrentSnapshot.StateSHA, nil
}

func cutoverRefreshEventAvailable(ctx context.Context, tx *sql.Tx, event string) error {
	if err := executionEventAvailable(ctx, tx, event); err != nil {
		return err
	}
	if err := rejectDarkRestoreEvent(ctx, tx, event); err != nil {
		return err
	}
	for _, query := range []string{
		`SELECT COUNT(*) FROM client_operations WHERE event_id=?`,
		`SELECT COUNT(*) FROM client_ownership_receipts_v1 WHERE event_id=?`,
		`SELECT COUNT(*) FROM darkrestore_cutover_refresh_receipts_v1 WHERE event_id=?`,
		`SELECT COUNT(*) FROM darkrestore_cutover_receipts_v1 WHERE json_extract(receipt_json,'$.subscription_event_id')=?`,
	} {
		var n int
		if err := tx.QueryRowContext(ctx, query, event).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return ErrConflict
		}
	}
	return nil
}

// RefreshDarkRestoreCutoverV1 records one already-committed, independently
// audited renewal or ownership assignment. It never edits the client, original
// evidence, historical reservation, balances, runtime or subscription. A final
// delivery receipt closes this V1 refresh path; later changes remain fail-closed
// until a separately versioned delivery-chain contract exists.
func (s *Store) RefreshDarkRestoreCutoverV1(ctx context.Context, token, sourceSHA, sourceID string, in DarkRestoreCutoverRefreshInputV1) (DarkRestoreCutoverRefreshReceiptV1, error) {
	zero := DarkRestoreCutoverRefreshReceiptV1{Scope: DarkRestoreCutoverRefreshScopeV1}
	if !eventRE.MatchString(in.EventID) || !eventRE.MatchString(in.CauseEventID) ||
		in.EventID == in.CauseEventID || !validName(in.Reason, 200) {
		return zero, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	var client string
	if err = tx.QueryRowContext(ctx, `SELECT client_id FROM darkrestore_import_entries_v1 WHERE source_sha256=? AND source_id=?`, sourceSHA, sourceID).Scan(&client); errors.Is(err, sql.ErrNoRows) {
		return zero, ErrNotFound
	} else if err != nil {
		return zero, err
	}
	evidence, err := readCutoverEvidence(ctx, tx, client)
	if err != nil || evidence == nil {
		return zero, ErrDarkRestoreCutover
	}
	fp := fingerprint("darkrestore-cutover-refresh-v1", struct {
		Actor, SourceSHA, SourceID string
		Input                      DarkRestoreCutoverRefreshInputV1
	}{actor, sourceSHA, sourceID, in})
	var priorRaw []byte
	var priorFP string
	err = tx.QueryRowContext(ctx, `SELECT fingerprint,receipt_json FROM darkrestore_cutover_refresh_receipts_v1 WHERE event_id=?`, in.EventID).Scan(&priorFP, &priorRaw)
	if err == nil {
		var prior DarkRestoreCutoverRefreshReceiptV1
		if priorFP != fp || decodeCutoverRefresh(priorRaw, &prior) != nil {
			return zero, ErrConflict
		}
		prior.Replayed = true
		if err = tx.Commit(); err != nil {
			return zero, err
		}
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	if final, readErr := readCutoverReceipt(ctx, tx, client); readErr != nil || final != nil {
		if readErr != nil {
			return zero, readErr
		}
		return zero, ErrConflict
	}
	if err = cutoverRefreshEventAvailable(ctx, tx, in.EventID); err != nil {
		return zero, err
	}
	previous, err := canonicalCutoverRefreshSnapshot(in.PreviousSnapshot)
	if err != nil {
		return zero, err
	}
	chain, err := readCutoverRefreshChain(ctx, tx, evidence)
	if err != nil {
		return zero, err
	}
	expectedSHA := evidence.DestinationStateSHA
	if len(chain) > 0 {
		expectedSHA = chain[len(chain)-1].CurrentSnapshot.StateSHA
	}
	if previous.StateSHA != expectedSHA {
		return zero, ErrConflict
	}
	current, err := readDarkRestoreCutoverSnapshotV1(ctx, tx, sourceSHA, sourceID)
	if err != nil || !sameCutoverRefreshStableState(previous, current) || current.StateSHA == previous.StateSHA {
		return zero, ErrDarkRestoreCutover
	}
	causeKind, _, err := verifyCutoverRefreshCause(ctx, tx, client, in.CauseEventID, previous, current, evidence.TransitionOpenedUnixNano)
	if err != nil {
		return zero, err
	}
	remaining, err := cutoverRemaining(current.CurrentEntitlement, evidence.SettledHistoricalBytes, *current.NativeUplink, *current.NativeDownlink)
	if err != nil {
		return zero, err
	}
	now := time.Now().Unix()
	out := DarkRestoreCutoverRefreshReceiptV1{
		Scope: DarkRestoreCutoverRefreshScopeV1, ClientID: client, ActorUserID: actor,
		EventID: in.EventID, CauseEventID: in.CauseEventID, CauseKind: causeKind, Reason: in.Reason,
		Revision: uint64(len(chain) + 1), TransitionSHA: evidence.TransitionSHA,
		SettledHistoricalBytes: evidence.SettledHistoricalBytes,
		PreviousSnapshot:       previous, CurrentSnapshot: current,
		CurrentFiniteEntitlement: current.CurrentEntitlement, RemainingBudgetBytes: remaining,
		CreatedUnix: now,
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO darkrestore_cutover_refresh_receipts_v1(client_id,revision,event_id,cause_event_id,fingerprint,previous_state_sha,current_state_sha,receipt_json,created_unix) VALUES(?,?,?,?,?,?,?,?,?)`,
		client, out.Revision, out.EventID, out.CauseEventID, fp, previous.StateSHA, current.StateSHA, raw, now); err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}
