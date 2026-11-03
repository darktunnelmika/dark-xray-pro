package business

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type DarkRestoreCutoverEvidenceReceiptV1 struct {
	ClientID                 string                                `json:"client_id"`
	OriginalEventID          string                                `json:"original_import_event_id"`
	ActorUserID              string                                `json:"actor_user_id"`
	SourceSHA256             string                                `json:"source_sha256"`
	SourceID                 string                                `json:"source_id"`
	Inventory                darkrestore.LocalHandoffInventoryV1   `json:"inventory"`
	InventorySHA256          string                                `json:"inventory_sha256"`
	CoverageApproval         darkrestore.HandoffCoverageApprovalV1 `json:"coverage_approval"`
	CoverageApprovalSHA256   string                                `json:"coverage_approval_sha256"`
	CoverageAuthoritySHA256  string                                `json:"coverage_authority_public_key_sha256"`
	DestinationSHA           string                                `json:"destination_sha256"`
	DestinationStateSHA      string                                `json:"destination_state_sha256"`
	ImportReceiptSHA         string                                `json:"import_receipt_sha256"`
	NativeBaselineSHA        string                                `json:"native_baseline_sha256"`
	BillingAttributionSHA    string                                `json:"billing_attribution_sha256"`
	TransitionSHA            string                                `json:"transition_sha256"`
	NativeDocument           []byte                                `json:"native_document,omitempty"`
	NativePublicKey          []byte                                `json:"native_public_key,omitempty"`
	NativePins               darkrestore.NativeBaselinePinsV1      `json:"native_pins,omitzero"`
	SettledHistoricalBytes   int64                                 `json:"settled_historical_bytes"`
	CurrentFiniteEntitlement int64                                 `json:"current_finite_entitlement_bytes"`
	NativeBaselineUplink     int64                                 `json:"native_baseline_uplink"`
	NativeBaselineDownlink   int64                                 `json:"native_baseline_downlink"`
	RemainingBudgetBytes     int64                                 `json:"remaining_budget_bytes"`
	TransitionOpenedUnixNano int64                                 `json:"transition_opened_unix_nano,string"`
	CreatedUnix              int64                                 `json:"created_unix"`
	Replayed                 bool                                  `json:"replayed"`
}

type DarkRestoreCutoverReceiptV1 struct {
	Prepared                     DarkRestorePreparedBindingV1 `json:"prepared,omitzero"`
	ClientID                     string                       `json:"client_id"`
	ActorUserID                  string                       `json:"actor_user_id"`
	EvidenceInventorySHA256      string                       `json:"evidence_inventory_sha256"`
	EvidenceApprovalSHA256       string                       `json:"evidence_approval_sha256"`
	SettledHistoricalBytes       int64                        `json:"settled_historical_bytes"`
	NativePostMigrationBytes     int64                        `json:"native_post_migration_bytes"`
	CurrentFiniteEntitlement     int64                        `json:"current_finite_entitlement_bytes"`
	RemainingBudgetBytes         int64                        `json:"remaining_budget_bytes"`
	SubscriptionRevision         uint64                       `json:"subscription_revision"`
	SubscriptionDeliveryScope    string                       `json:"subscription_delivery_scope"`
	SubscriptionSnapshotID       string                       `json:"subscription_snapshot_id"`
	SubscriptionEventID          string                       `json:"subscription_event_id"`
	SubscriptionSnapshotRevision uint64                       `json:"subscription_snapshot_revision"`
	SubscriptionDeliveredNano    int64                        `json:"subscription_delivered_unix_nano,string"`
	TransitionOpenedUnixNano     int64                        `json:"transition_opened_unix_nano,string"`
	TransitionSHA                string                       `json:"transition_sha256"`
	CreatedUnix                  int64                        `json:"created_unix"`
	Replayed                     bool                         `json:"replayed"`
}

func cutoverRemaining(entitlement, settled, up, down int64) (int64, error) {
	projection, err := darkrestore.ProjectFiniteCutoverBudgetV1(
		entitlement, &settled,
		&darkrestore.NativeCutoverCountersV1{Uplink: up, Downlink: down},
	)
	if err != nil || projection.RemainingBytes == nil || *projection.RemainingBytes <= 0 {
		return 0, ErrDarkRestoreCutover
	}
	return *projection.RemainingBytes, nil
}

func localAttachmentCoveredByInventory(attachment *DarkRestoreLocalAttachmentReceiptV1, inventory darkrestore.LocalHandoffInventoryV1) bool {
	if attachment == nil || attachment.HandoffSHA == "" || attachment.SignerPublicSHA == "" ||
		attachment.Settlement.SourceSHA != inventory.SourceSHA ||
		attachment.Settlement.SourceID != inventory.SourceID {
		return false
	}
	for _, pin := range inventory.Sources {
		if pin.EpochID == attachment.Settlement.Terminal.EpochID &&
			pin.Sequence == attachment.Settlement.Terminal.Sequence &&
			pin.ReceiptSHA == attachment.Settlement.Terminal.SHA &&
			pin.AuthoritySHA == attachment.Settlement.AuthoritySHA &&
			pin.JournalSHA == attachment.Settlement.JournalSHA &&
			pin.HandoffSHA == attachment.HandoffSHA &&
			pin.SignerPublicSHA == attachment.SignerPublicSHA {
			return true
		}
	}
	return false
}

func decodeCutoverEvidence(raw []byte, out *DarkRestoreCutoverEvidenceReceiptV1) error {
	if len(raw) == 0 || len(raw) > 128<<10 || json.Unmarshal(raw, out) != nil || out.Replayed ||
		out.ClientID == "" || out.OriginalEventID == "" || out.ActorUserID == "" ||
		out.SourceSHA256 == "" || out.SourceID == "" ||
		out.InventorySHA256 == "" || out.CoverageApprovalSHA256 == "" ||
		out.CoverageAuthoritySHA256 == "" || !routeSHA.MatchString(out.DestinationSHA) ||
		!routeSHA.MatchString(out.DestinationStateSHA) || !routeSHA.MatchString(out.ImportReceiptSHA) ||
		!routeSHA.MatchString(out.NativeBaselineSHA) || !routeSHA.MatchString(out.BillingAttributionSHA) ||
		!routeSHA.MatchString(out.TransitionSHA) || out.SettledHistoricalBytes < 0 ||
		out.CurrentFiniteEntitlement <= 0 ||
		out.NativeBaselineUplink < 0 || out.NativeBaselineDownlink < 0 ||
		out.RemainingBudgetBytes <= 0 || out.TransitionOpenedUnixNano <= 0 ||
		out.CreatedUnix <= 0 || out.TransitionOpenedUnixNano/1_000_000_000 != out.CreatedUnix {
		return ErrUnavailable
	}
	if len(out.NativeDocument) > 0 {
		native, nativeErr := darkrestore.VerifySignedNativeBaselineV1(out.NativeDocument, out.NativePublicKey, out.NativePins)
		if nativeErr != nil || out.NativePins.DestinationSHA != out.DestinationSHA {
			return ErrUnavailable
		}
		nativeValue, _, _, _, nativeErr := native.Evidence()
		if nativeErr != nil || nativeValue.SourceSHA != out.SourceSHA256 || nativeValue.SourceID != out.SourceID ||
			nativeValue.ClientID != out.ClientID || nativeValue.ImportReceiptSHA != out.ImportReceiptSHA ||
			nativeValue.Uplink != out.NativeBaselineUplink || nativeValue.Downlink != out.NativeBaselineDownlink ||
			out.CoverageApproval.ApprovedUnixNano < nativeValue.MeasuredThroughNano ||
			nativeValue.FenceUntilNano <= out.TransitionOpenedUnixNano || nativeValue.MeasuredThroughNano > out.TransitionOpenedUnixNano ||
			nativeValue.FenceUntilNano-nativeValue.MeasuredThroughNano > int64(5*time.Minute) {
			return ErrUnavailable
		}
	} else if len(out.NativePublicKey) > 0 || out.NativePins != (darkrestore.NativeBaselinePinsV1{}) {
		return ErrUnavailable
	}

	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, raw) {
		return ErrUnavailable
	}
	digest, err := darkrestore.DigestLocalHandoffInventoryV1(out.Inventory)
	if err != nil || digest != out.InventorySHA256 ||
		out.Inventory.SourceSHA != out.SourceSHA256 || out.Inventory.SourceID != out.SourceID ||
		out.CoverageApproval.Version != 1 ||
		out.CoverageApproval.InventorySHA != out.InventorySHA256 ||
		out.CoverageApproval.SourceSHA != out.SourceSHA256 ||
		out.CoverageApproval.SourceID != out.SourceID ||
		out.CoverageApproval.DestinationSHA != out.DestinationSHA ||
		out.CoverageApproval.DestinationStateSHA != out.DestinationStateSHA ||
		out.CoverageApproval.ImportReceiptSHA != out.ImportReceiptSHA ||
		out.CoverageApproval.NativeBaselineSHA != out.NativeBaselineSHA ||
		out.CoverageApproval.BillingAttributionSHA != out.BillingAttributionSHA ||
		out.CoverageApproval.BillingAttribution != "disjoint_source_epoch_deltas" ||
		out.CoverageApproval.ApprovedUnixNano >= out.TransitionOpenedUnixNano ||
		out.CoverageApproval.Scope != darkrestore.HandoffCoverageScopeV1 ||
		out.CoverageApproval.Statement != darkrestore.HandoffCoverageStatementV1 ||
		!out.CoverageApproval.InventoryCompleteAsserted ||
		!out.CoverageApproval.DeclaredSourcesRetired ||
		out.CoverageApproval.DeclaredSources != len(out.Inventory.Sources) ||
		len(out.CoverageApproval.EpochIDs) != len(out.Inventory.Sources) {
		return ErrUnavailable
	}
	for i, source := range out.Inventory.Sources {
		if out.CoverageApproval.EpochIDs[i] != source.EpochID {
			return ErrUnavailable
		}
	}
	transitionSHA, err := darkRestoreTransitionSHA(
		out.ClientID, out.InventorySHA256, out.CoverageApprovalSHA256,
		out.DestinationStateSHA, out.NativeBaselineSHA, out.NativePins.DocumentSHA, out.TransitionOpenedUnixNano,
	)
	if err != nil || transitionSHA != out.TransitionSHA {
		return ErrUnavailable
	}
	remaining, err := cutoverRemaining(
		out.CurrentFiniteEntitlement, out.SettledHistoricalBytes,
		out.NativeBaselineUplink, out.NativeBaselineDownlink,
	)
	if err != nil || remaining != out.RemainingBudgetBytes {
		return ErrUnavailable
	}
	return nil
}

func readCutoverEvidence(ctx context.Context, q sellerQueryV2, id string) (*DarkRestoreCutoverEvidenceReceiptV1, error) {
	present, err := darkRestoreCutoverSchemaState(ctx, q)
	if err != nil || !present {
		return nil, err
	}
	var inventorySHA, approvalSHA, authoritySHA, settled string
	var opened, created int64
	var raw []byte
	err = q.QueryRowContext(ctx, `SELECT inventory_sha256,coverage_approval_sha256,coverage_authority_sha256,settled_historical_bytes,transition_opened_unix_nano,receipt_json,created_unix FROM darkrestore_cutover_evidence_v1 WHERE client_id=?`, id).
		Scan(&inventorySHA, &approvalSHA, &authoritySHA, &settled, &opened, &raw, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out DarkRestoreCutoverEvidenceReceiptV1
	if decodeCutoverEvidence(raw, &out) != nil || out.ClientID != id ||
		out.InventorySHA256 != inventorySHA || out.CoverageApprovalSHA256 != approvalSHA ||
		out.CoverageAuthoritySHA256 != authoritySHA ||
		strconv.FormatInt(out.SettledHistoricalBytes, 10) != settled ||
		out.TransitionOpenedUnixNano != opened || out.CreatedUnix != created {
		return nil, ErrUnavailable
	}
	if len(out.NativeDocument) == 0 {
		return &out, nil
	}
	var uuid string
	var clientCreated int64
	if err = q.QueryRowContext(ctx, `SELECT uuid,created_unix FROM clients WHERE id=?`, id).Scan(&uuid, &clientCreated); err != nil || clientCreated <= 0 || clientCreated > 9223372036854775807/1_000_000_000 {
		return nil, ErrUnavailable
	}
	verified, e := darkrestore.VerifySignedNativeBaselineV1(out.NativeDocument, out.NativePublicKey, out.NativePins)
	if e != nil {
		return nil, ErrUnavailable
	}
	native, _, _, _, e := verified.Evidence()
	if e != nil || native.ClientUUID != uuid || native.CoverageFromNano > clientCreated*1_000_000_000 || native.MeasuredThroughNano < clientCreated*1_000_000_000 {
		return nil, ErrUnavailable
	}
	holds := int64(0)
	before, e := hashDarkRestoreCutoverSnapshotV1(DarkRestoreCutoverSnapshotV1{ClientID: id, ClientUUID: uuid, NativeAccountPresent: true, NativeUplink: &native.Uplink, NativeDownlink: &native.Downlink, NativeHolds: &holds})
	if e != nil || before.NativeBaselineSHA != native.BeforeNativeSHA {
		return nil, ErrUnavailable
	}
	before.NativeBaselineComplete = true
	after, e := hashDarkRestoreCutoverSnapshotV1(before)
	if e != nil || after.NativeBaselineSHA != out.NativeBaselineSHA {
		return nil, ErrUnavailable
	}
	return &out, nil
}

func decodeCutoverReceipt(raw []byte, out *DarkRestoreCutoverReceiptV1) error {
	if len(raw) == 0 || len(raw) > 32<<10 || json.Unmarshal(raw, out) != nil || out.Replayed ||
		out.ClientID == "" || out.ActorUserID == "" ||
		out.EvidenceInventorySHA256 == "" || out.EvidenceApprovalSHA256 == "" ||
		out.SettledHistoricalBytes < 0 || out.NativePostMigrationBytes < 0 ||
		out.CurrentFiniteEntitlement <= 0 || out.RemainingBudgetBytes <= 0 ||
		out.SubscriptionRevision == 0 || (out.SubscriptionDeliveryScope != HandoverSubscriptionScope && out.SubscriptionDeliveryScope != DarkRestorePreparedDeliveryScopeV1) ||
		!routeID.MatchString(out.SubscriptionSnapshotID) || !eventRE.MatchString(out.SubscriptionEventID) ||
		out.SubscriptionSnapshotRevision == 0 || out.SubscriptionSnapshotRevision >= routeMax ||
		out.SubscriptionDeliveredNano <= out.TransitionOpenedUnixNano ||
		out.TransitionOpenedUnixNano <= 0 || !routeSHA.MatchString(out.TransitionSHA) || out.CreatedUnix <= 0 {
		return ErrUnavailable
	}
	if out.SubscriptionDeliveryScope == DarkRestorePreparedDeliveryScopeV1 {
		if !validPreparedBinding(out.Prepared) || out.SubscriptionSnapshotRevision != out.Prepared.PlanRevision || out.SubscriptionSnapshotID != out.Prepared.ContentSHA[:32] {
			return ErrUnavailable
		}
	} else if out.Prepared != (DarkRestorePreparedBindingV1{}) {
		return ErrUnavailable
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(canonical, raw) {
		return ErrUnavailable
	}
	remaining, budgetErr := cutoverRemaining(
		out.CurrentFiniteEntitlement, out.SettledHistoricalBytes,
		out.NativePostMigrationBytes, 0,
	)
	if budgetErr != nil || remaining != out.RemainingBudgetBytes ||
		out.CreatedUnix < out.SubscriptionDeliveredNano/1_000_000_000 {
		return ErrUnavailable
	}
	return nil
}

func readCutoverReceipt(ctx context.Context, q sellerQueryV2, id string) (*DarkRestoreCutoverReceiptV1, error) {
	present, err := darkRestoreCutoverSchemaState(ctx, q)
	if err != nil || !present {
		return nil, err
	}
	var revision uint64
	var delivered, created int64
	var remaining string
	var raw []byte
	err = q.QueryRowContext(ctx, `SELECT subscription_revision,subscription_delivered_unix_nano,remaining_budget_bytes,receipt_json,created_unix FROM darkrestore_cutover_receipts_v1 WHERE client_id=?`, id).
		Scan(&revision, &delivered, &remaining, &raw, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out DarkRestoreCutoverReceiptV1
	if decodeCutoverReceipt(raw, &out) != nil || out.ClientID != id ||
		out.SubscriptionRevision != revision || out.SubscriptionDeliveredNano != delivered ||
		strconv.FormatInt(out.RemainingBudgetBytes, 10) != remaining || out.CreatedUnix != created {
		return nil, ErrUnavailable
	}
	return &out, nil
}

// CompleteDarkRestoreZeroBaselineV1 cannot establish lifetime coverage from
// recorded zero counters or absent activity. Retained only as a closed legacy API.
func (s *Store) CompleteDarkRestoreZeroBaselineV1(ctx context.Context, token, sourceSHA, sourceID string) (DarkRestoreCutoverSnapshotV1, error) {
	return DarkRestoreCutoverSnapshotV1{}, ErrDarkRestoreCutover
}

func darkRestoreTransitionSHA(clientID, inventorySHA, approvalSHA, stateSHA, nativeSHA, nativeDocumentSHA string, opened int64) (string, error) {
	if nativeDocumentSHA == "" {
		return cutoverSnapshotDigest("DARK-PRO-CUTOVER-TRANSITION-v1", struct {
			ClientID, InventorySHA, ApprovalSHA, StateSHA, NativeSHA string
			OpenedUnixNano                                           int64
		}{clientID, inventorySHA, approvalSHA, stateSHA, nativeSHA, opened})
	}

	return cutoverSnapshotDigest("DARK-PRO-CUTOVER-TRANSITION-v1", struct {
		ClientID, InventorySHA, ApprovalSHA, StateSHA, NativeSHA, NativeDocumentSHA string
		OpenedUnixNano                                                              int64
	}{clientID, inventorySHA, approvalSHA, stateSHA, nativeSHA, nativeDocumentSHA, opened})
}

// OpenDarkRestoreCutoverV1 records an immutable evidence boundary from a
// transactionally re-read destination snapshot. destinationSHA and
// billingAttributionSHA are local operator pins, never browser-selected values.
func (s *Store) OpenDarkRestoreCutoverV1(ctx context.Context, token string, inventory darkrestore.LocalHandoffInventoryV1, inventoryPin string, handoffs []darkrestore.VerifiedLocalHandoff, approval darkrestore.VerifiedHandoffCoverageApprovalV1, destinationSHA, billingAttributionSHA string, native darkrestore.VerifiedNativeBaselineV1, nativePins darkrestore.NativeBaselinePinsV1) (DarkRestoreCutoverEvidenceReceiptV1, error) {
	var zero DarkRestoreCutoverEvidenceReceiptV1
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	inventory.Sources = append([]darkrestore.LocalHandoffSourcePinV1(nil), inventory.Sources...)
	sort.Slice(inventory.Sources, func(i, j int) bool { return inventory.Sources[i].EpochID < inventory.Sources[j].EpochID })
	present, err := darkRestoreCutoverSchemaState(ctx, tx)
	if err != nil || !present || !routeSHA.MatchString(destinationSHA) || !routeSHA.MatchString(billingAttributionSHA) {
		return zero, ErrUnavailable
	}
	if _, e := darkrestore.CheckLocalHandoffCoverageV1(inventory, inventoryPin, handoffs); e != nil {
		return zero, ErrDarkRestoreCutover
	}
	snapshot, err := readDarkRestoreCutoverSnapshotV1(ctx, tx, inventory.SourceSHA, inventory.SourceID)
	if err != nil {
		return zero, err
	}
	id, originalEvent := snapshot.ClientID, snapshot.ImportEventID
	nativeValue, nativeRaw, nativePublic, verifiedPins, nativeErr := native.Evidence()
	if nativeErr != nil || verifiedPins != nativePins || nativePins.DestinationSHA != destinationSHA {
		return zero, ErrDarkRestoreCutover
	}
	if saved, readErr := readCutoverEvidence(ctx, tx, id); readErr != nil {
		return zero, readErr
	} else if saved != nil {
		approvalValue, approvalSHA, authoritySHA, evidenceErr := approval.Evidence()
		if evidenceErr != nil || saved.ActorUserID != actor || saved.NativePins != nativePins ||
			!bytes.Equal(saved.NativeDocument, nativeRaw) || !bytes.Equal(saved.NativePublicKey, nativePublic) ||
			saved.InventorySHA256 != inventoryPin ||
			saved.CoverageApprovalSHA256 != approvalSHA ||
			saved.CoverageAuthoritySHA256 != authoritySHA ||
			saved.DestinationSHA != destinationSHA ||
			saved.ImportReceiptSHA != snapshot.ImportReceiptSHA ||
			saved.BillingAttributionSHA != billingAttributionSHA ||
			!reflect.DeepEqual(saved.CoverageApproval, approvalValue) {
			return zero, ErrConflict
		}
		saved.Replayed = true
		return *saved, tx.Commit()
	}
	beforeSnapshot := snapshot
	now := time.Now()
	snapshot, err = projectAttestedNativeBaselineV1(snapshot, native, nativePins, now.UnixNano())
	if err != nil {
		return zero, err
	}
	if !snapshot.NativeAccountPresent || !snapshot.NativeBaselineComplete ||
		snapshot.NativeUplink == nil || snapshot.NativeDownlink == nil || snapshot.NativeHolds == nil ||
		snapshot.DeploymentState != "disabled" || snapshot.Archived {
		return zero, ErrDarkRestoreCutover
	}
	importedHistorical, err := strconv.ParseInt(snapshot.ImportedHistoricalBytes, 10, 64)
	if err != nil || importedHistorical < 0 {
		return zero, ErrUnavailable
	}
	attachment, err := readDarkRestoreLocalAttachment(ctx, tx, id)
	if err != nil || !localAttachmentCoveredByInventory(attachment, inventory) {
		return zero, ErrDarkRestoreCutover
	}
	approvalValue, approvalSHA, authoritySHA, err := approval.Evidence()
	if err != nil || approvalValue.ApprovedUnixNano < nativeValue.MeasuredThroughNano {
		return zero, ErrDarkRestoreCutover
	}
	opened := now.UnixNano()
	transitionSHA, err := darkRestoreTransitionSHA(id, inventoryPin, approvalSHA, snapshot.StateSHA, snapshot.NativeBaselineSHA, nativePins.DocumentSHA, opened)
	if err != nil {
		return zero, err
	}
	decision, err := darkrestore.EvaluateLocalHandoffCutoverV1(inventory, inventoryPin, handoffs, approval, darkrestore.LocalCutoverInputV1{
		DestinationStateSHA: snapshot.StateSHA, DestinationSHA: destinationSHA,
		ImportReceiptSHA: snapshot.ImportReceiptSHA, BillingAttributionSHA: billingAttributionSHA,
		NativeBaselineSHA: snapshot.NativeBaselineSHA, ExpectedNativeBaselineSHA: snapshot.NativeBaselineSHA,
		TransitionSHA: transitionSHA, NowUnixNano: opened,
		PlanKind: snapshot.PlanKind, CurrentFiniteEntitlement: snapshot.CurrentEntitlement,
		ImportedHistorical: importedHistorical, NativeUplink: *snapshot.NativeUplink,
		NativeDownlink: *snapshot.NativeDownlink, NativeBaselineComplete: snapshot.NativeBaselineComplete,
		NativeHolds: *snapshot.NativeHolds, ClientArchived: snapshot.Archived,
		ClientExpiresUnix: snapshot.ExpiresUnix, NowUnix: now.Unix(),
		TransitionOpenedUnixNano: opened,
	})
	if err != nil || !decision.SubscriptionIssueAllowed || decision.ActivationAllowed || decision.AuthorizesMutation {
		return zero, ErrDarkRestoreCutover
	}
	out := DarkRestoreCutoverEvidenceReceiptV1{
		ClientID: id, OriginalEventID: originalEvent, ActorUserID: actor,
		SourceSHA256: inventory.SourceSHA, SourceID: inventory.SourceID,
		Inventory: inventory, InventorySHA256: inventoryPin,
		CoverageApproval: approvalValue, CoverageApprovalSHA256: approvalSHA,
		CoverageAuthoritySHA256: authoritySHA,
		DestinationSHA:          destinationSHA, DestinationStateSHA: snapshot.StateSHA,
		ImportReceiptSHA: snapshot.ImportReceiptSHA, NativeBaselineSHA: snapshot.NativeBaselineSHA,
		BillingAttributionSHA: billingAttributionSHA, TransitionSHA: transitionSHA,
		NativeDocument: nativeRaw, NativePublicKey: nativePublic, NativePins: nativePins,
		SettledHistoricalBytes:   decision.SettledHistoricalBytes,
		CurrentFiniteEntitlement: snapshot.CurrentEntitlement,
		NativeBaselineUplink:     *snapshot.NativeUplink, NativeBaselineDownlink: *snapshot.NativeDownlink,
		RemainingBudgetBytes:     decision.RemainingBudgetBytes,
		TransitionOpenedUnixNano: opened, CreatedUnix: now.Unix(),
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO darkrestore_cutover_evidence_v1(client_id,inventory_sha256,coverage_approval_sha256,coverage_authority_sha256,settled_historical_bytes,transition_opened_unix_nano,receipt_json,created_unix) VALUES(?,?,?,?,?,?,?,?)`,
		id, inventoryPin, approvalSHA, authoritySHA, strconv.FormatInt(out.SettledHistoricalBytes, 10), opened, raw, out.CreatedUnix); err != nil {
		return zero, err
	}
	bindingSHA, e := cutoverHistoricalBudgetSHA(out)
	if e != nil {
		return zero, e
	}
	if s.usage == nil {
		return zero, ErrUnavailable
	}
	if e = s.usage.ReserveHistoricalBudgetTx(ctx, tx, usageledger.HistoricalBudget{ClientID: id, HistoricalBytes: out.SettledHistoricalBytes, ReceiptSHA: bindingSHA}); e != nil {
		return zero, e
	}
	result, e := tx.ExecContext(ctx, `UPDATE pro_usage_accounts SET baseline_complete=1 WHERE client_id=? AND baseline_complete=0 AND uplink=? AND downlink=? AND holds=0`, id, *beforeSnapshot.NativeUplink, *beforeSnapshot.NativeDownlink)
	if e != nil {
		return zero, e
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		return zero, ErrConflict
	}
	if nativeValue.FenceUntilNano <= time.Now().UnixNano() {
		return zero, ErrDarkRestoreCutover
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}

// FinalizeDarkRestoreCutoverV1 seals the delivery side of the transition. The
// subscription must have an authoritative revision strictly newer than the
// immutable evidence boundary.
func (s *Store) FinalizeDarkRestoreCutoverV1(ctx context.Context, token, clientID string) (DarkRestoreCutoverReceiptV1, error) {
	var zero DarkRestoreCutoverReceiptV1
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	actor, err := ownershipOwner(ctx, tx, token)
	if err != nil {
		return zero, err
	}
	evidence, err := readCutoverEvidence(ctx, tx, clientID)
	if err != nil || evidence == nil {
		return zero, ErrDarkRestoreCutover
	}
	if saved, readErr := readCutoverReceipt(ctx, tx, clientID); readErr != nil {
		return zero, readErr
	} else if saved != nil {
		if saved.ActorUserID != actor || saved.EvidenceInventorySHA256 != evidence.InventorySHA256 ||
			saved.EvidenceApprovalSHA256 != evidence.CoverageApprovalSHA256 ||
			saved.TransitionSHA != evidence.TransitionSHA {
			return zero, ErrConflict
		}
		saved.Replayed = true
		return *saved, tx.Commit()
	}
	snapshot, err := readDarkRestoreCutoverSnapshotV1(ctx, tx, evidence.SourceSHA256, evidence.SourceID)
	if err != nil {
		return zero, err
	}
	if snapshot.ClientID != clientID ||
		snapshot.StateSHA != evidence.DestinationStateSHA ||
		snapshot.NativeBaselineSHA != evidence.NativeBaselineSHA ||
		snapshot.ImportReceiptSHA != evidence.ImportReceiptSHA ||
		!snapshot.NativeAccountPresent || !snapshot.NativeBaselineComplete ||
		snapshot.NativeUplink == nil || snapshot.NativeDownlink == nil || snapshot.NativeHolds == nil ||
		snapshot.PlanKind != "volume" || snapshot.DeploymentState != "disabled" ||
		snapshot.Archived || snapshot.ExpiresUnix <= time.Now().Unix() || *snapshot.NativeHolds != 0 {
		return zero, ErrDarkRestoreCutover
	}
	remaining, err := cutoverRemaining(
		snapshot.CurrentEntitlement, evidence.SettledHistoricalBytes,
		*snapshot.NativeUplink, *snapshot.NativeDownlink,
	)
	if err != nil {
		return zero, err
	}
	delivery, err := readSubscriptionDelivery(ctx, tx, clientID)
	if err != nil || !delivery.Enabled || delivery.Revision == 0 ||
		delivery.Scope != HandoverSubscriptionScope ||
		delivery.Created <= evidence.TransitionOpenedUnixNano || delivery.Created > time.Now().UnixNano() {
		return zero, ErrDarkRestoreCutover
	}
	now := time.Now()
	out := DarkRestoreCutoverReceiptV1{
		ClientID: clientID, ActorUserID: actor,
		EvidenceInventorySHA256:      evidence.InventorySHA256,
		EvidenceApprovalSHA256:       evidence.CoverageApprovalSHA256,
		SettledHistoricalBytes:       evidence.SettledHistoricalBytes,
		NativePostMigrationBytes:     *snapshot.NativeUplink + *snapshot.NativeDownlink,
		CurrentFiniteEntitlement:     snapshot.CurrentEntitlement,
		RemainingBudgetBytes:         remaining,
		SubscriptionRevision:         delivery.Revision,
		SubscriptionDeliveryScope:    delivery.Scope,
		SubscriptionSnapshotID:       delivery.SnapshotID,
		SubscriptionEventID:          delivery.EventID,
		SubscriptionSnapshotRevision: delivery.SnapshotRevision,
		SubscriptionDeliveredNano:    delivery.Created,
		TransitionOpenedUnixNano:     evidence.TransitionOpenedUnixNano,
		TransitionSHA:                evidence.TransitionSHA,
		CreatedUnix:                  now.Unix(),
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO darkrestore_cutover_receipts_v1(client_id,subscription_revision,subscription_delivered_unix_nano,remaining_budget_bytes,receipt_json,created_unix) VALUES(?,?,?,?,?,?)`,
		clientID, out.SubscriptionRevision, out.SubscriptionDeliveredNano,
		strconv.FormatInt(out.RemainingBudgetBytes, 10), raw, out.CreatedUnix); err != nil {
		return zero, err
	}
	if err = tx.Commit(); err != nil {
		return zero, err
	}
	return out, nil
}

func cutoverSnapshotMatchesEvidence(ctx context.Context, q cutoverRefreshQuery, evidence *DarkRestoreCutoverEvidenceReceiptV1) (DarkRestoreCutoverSnapshotV1, int64, error) {
	var zero DarkRestoreCutoverSnapshotV1
	if evidence == nil || len(evidence.NativeDocument) == 0 {
		return zero, 0, ErrDarkRestoreCutover
	}
	if err := verifyCutoverHistoricalBudget(ctx, q, evidence); err != nil {
		return zero, 0, err
	}

	snapshot, err := readDarkRestoreCutoverSnapshotV1(ctx, q, evidence.SourceSHA256, evidence.SourceID)
	if err != nil {
		return zero, 0, err
	}
	effectiveStateSHA, err := effectiveCutoverStateSHA(ctx, q, evidence)
	if err != nil {
		return zero, 0, err
	}
	if snapshot.ClientID != evidence.ClientID ||
		snapshot.StateSHA != effectiveStateSHA ||
		snapshot.NativeBaselineSHA != evidence.NativeBaselineSHA ||
		snapshot.ImportReceiptSHA != evidence.ImportReceiptSHA ||
		!snapshot.NativeAccountPresent || !snapshot.NativeBaselineComplete ||
		snapshot.NativeUplink == nil || snapshot.NativeDownlink == nil || snapshot.NativeHolds == nil ||
		snapshot.PlanKind != "volume" || snapshot.DeploymentState != "disabled" ||
		snapshot.Archived || snapshot.ExpiresUnix <= time.Now().Unix() || *snapshot.NativeHolds != 0 {
		return zero, 0, ErrDarkRestoreCutover
	}
	remaining, err := cutoverRemaining(
		snapshot.CurrentEntitlement, evidence.SettledHistoricalBytes,
		*snapshot.NativeUplink, *snapshot.NativeDownlink,
	)
	if err != nil {
		return zero, 0, ErrDarkRestoreCutover
	}
	return snapshot, remaining, nil
}

func requireDarkRestoreSubscriptionTransition(ctx context.Context, tx *sql.Tx, id string) error {
	present, err := darkRestoreSchemaState(ctx, tx)
	if err != nil || !present {
		return err
	}
	var imported bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM darkrestore_import_entries_v1 WHERE client_id=?)`, id).Scan(&imported); err != nil {
		return ErrUnavailable
	}
	if !imported {
		return nil
	}
	evidence, err := readCutoverEvidence(ctx, tx, id)
	if err != nil || evidence == nil {
		return ErrDarkRestoreCutover
	}
	_, _, err = cutoverSnapshotMatchesEvidence(ctx, tx, evidence)
	return err
}

func requireDarkRestoreFinalCutover(ctx context.Context, tx *sql.Tx, id string) error {
	receipt, err := readCutoverReceipt(ctx, tx, id)
	if err != nil || receipt == nil {
		return ErrDarkRestoreCutover
	}
	evidence, err := readCutoverEvidence(ctx, tx, id)
	if err != nil || evidence == nil ||
		receipt.EvidenceInventorySHA256 != evidence.InventorySHA256 ||
		receipt.EvidenceApprovalSHA256 != evidence.CoverageApprovalSHA256 ||
		receipt.SettledHistoricalBytes != evidence.SettledHistoricalBytes ||
		receipt.TransitionOpenedUnixNano != evidence.TransitionOpenedUnixNano ||
		receipt.TransitionSHA != evidence.TransitionSHA {
		return ErrDarkRestoreCutover
	}
	snapshot, remaining, err := cutoverSnapshotMatchesEvidence(ctx, tx, evidence)
	if err != nil ||
		receipt.CurrentFiniteEntitlement != snapshot.CurrentEntitlement ||
		receipt.NativePostMigrationBytes != *snapshot.NativeUplink+*snapshot.NativeDownlink ||
		receipt.RemainingBudgetBytes != remaining {
		return ErrDarkRestoreCutover
	}
	if receipt.SubscriptionDeliveryScope == DarkRestorePreparedDeliveryScopeV1 {
		if err = verifyPreparedPlanReceipt(ctx, tx, receipt); err != nil {
			return err
		}
		// Preparation is a real, immutable delivery boundary, but it neither
		// promotes the plan/client binding nor proves a native apply. Keep the
		// logical enable path closed until a later receipt binds that operation.
		return ErrDarkRestoreCutover
	}

	delivery, err := readSubscriptionDelivery(ctx, tx, id)
	if err != nil || !delivery.Enabled ||
		delivery.Revision != receipt.SubscriptionRevision ||
		delivery.Scope != receipt.SubscriptionDeliveryScope ||
		delivery.SnapshotID != receipt.SubscriptionSnapshotID ||
		delivery.EventID != receipt.SubscriptionEventID ||
		delivery.SnapshotRevision != receipt.SubscriptionSnapshotRevision ||
		delivery.Created != receipt.SubscriptionDeliveredNano ||
		receipt.SubscriptionDeliveredNano <= evidence.TransitionOpenedUnixNano || receipt.SubscriptionDeliveredNano > time.Now().UnixNano() {
		return ErrDarkRestoreCutover
	}
	return nil
}

func validateDarkRestoreCutovers(ctx context.Context, tx *sql.Tx) error {
	if err := validateDarkRestorePreparedPromotions(ctx, tx); err != nil {
		return err
	}
	if err := validateDarkRestoreConnections(ctx, tx); err != nil {
		return err
	}
	if err := validateDarkRestoreSystemProbeRounds(ctx, tx); err != nil {
		return err
	}
	if err := validateDarkRestoreListeners(ctx, tx); err != nil {
		return err
	}
	if err := validateDarkRestoreServingHealth(ctx, tx); err != nil {
		return err
	}
	if err := validateDarkRestoreAdmissions(ctx, tx); err != nil {
		return err
	}
	if err := validateDarkRestoreBirths(ctx, tx); err != nil {
		return err
	}
	present, err := darkRestoreCutoverSchemaState(ctx, tx)
	if err != nil || !present {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT client_id FROM darkrestore_cutover_evidence_v1 ORDER BY client_id`)
	if err != nil {
		return ErrUnavailable
	}
	var evidenceIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return ErrUnavailable
		}
		evidenceIDs = append(evidenceIDs, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return ErrUnavailable
	}
	rows.Close()
	for _, id := range evidenceIDs {
		evidence, e := readCutoverEvidence(ctx, tx, id)
		if e != nil || evidence == nil {
			return ErrUnavailable
		}
		var event, sourceSHA, sourceID, deployment string
		var baseline int
		if e = tx.QueryRowContext(ctx, `SELECT e.event_id,e.source_sha256,e.source_id,a.baseline_complete,c.deployment_state FROM darkrestore_import_entries_v1 e JOIN pro_usage_accounts a ON a.client_id=e.client_id JOIN clients c ON c.id=e.client_id WHERE e.client_id=?`, id).
			Scan(&event, &sourceSHA, &sourceID, &baseline, &deployment); e != nil ||
			event != evidence.OriginalEventID || sourceSHA != evidence.SourceSHA256 ||
			sourceID != evidence.SourceID || baseline != 1 {
			return ErrUnavailable
		}
		attachment, e := readDarkRestoreLocalAttachment(ctx, tx, id)
		if e != nil || !localAttachmentCoveredByInventory(attachment, evidence.Inventory) {
			return ErrUnavailable
		}
		if _, e = readCutoverRefreshChain(ctx, tx, evidence); e != nil {
			return ErrUnavailable
		}
		enabled, budgetErr := usageledger.HistoricalBudgetsEnabled(ctx, tx)
		if budgetErr != nil {
			return ErrUnavailable
		}
		if enabled && len(evidence.NativeDocument) > 0 {
			if verifyCutoverHistoricalBudget(ctx, tx, evidence) != nil {
				return ErrUnavailable
			}
		}
		admitted, admissionErr := hasDarkRestoreNativeAdmission(ctx, tx, id)
		if admissionErr != nil {
			return ErrUnavailable
		}
		if deployment == "disabled" && len(evidence.NativeDocument) > 0 && !admitted {
			if _, _, e = cutoverSnapshotMatchesEvidence(ctx, tx, evidence); e != nil {
				return ErrUnavailable
			}
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT client_id FROM darkrestore_cutover_receipts_v1 ORDER BY client_id`)
	if err != nil {
		return ErrUnavailable
	}
	var finalIDs []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return ErrUnavailable
		}
		finalIDs = append(finalIDs, id)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return ErrUnavailable
	}
	rows.Close()
	for _, id := range finalIDs {
		final, e := readCutoverReceipt(ctx, tx, id)
		evidence, ee := readCutoverEvidence(ctx, tx, id)
		if e != nil || ee != nil || final == nil || evidence == nil ||
			final.EvidenceInventorySHA256 != evidence.InventorySHA256 ||
			final.EvidenceApprovalSHA256 != evidence.CoverageApprovalSHA256 ||
			final.SettledHistoricalBytes != evidence.SettledHistoricalBytes ||
			final.TransitionOpenedUnixNano != evidence.TransitionOpenedUnixNano ||
			final.TransitionSHA != evidence.TransitionSHA {
			return ErrUnavailable
		}
	}
	var invalid int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM darkrestore_import_entries_v1 e JOIN clients c ON c.id=e.client_id LEFT JOIN darkrestore_cutover_receipts_v1 r ON r.client_id=e.client_id WHERE c.deployment_state!='disabled' AND r.client_id IS NULL`).Scan(&invalid); err != nil || invalid != 0 {
		return ErrUnavailable
	}
	return nil
}

func cutoverHistoricalBudgetSHA(evidence DarkRestoreCutoverEvidenceReceiptV1) (string, error) {
	evidence.Replayed = false
	return cutoverSnapshotDigest("DARK-PRO-CUTOVER-HISTORICAL-BUDGET-v1", evidence)
}
func verifyCutoverHistoricalBudget(ctx context.Context, q sellerQueryV2, evidence *DarkRestoreCutoverEvidenceReceiptV1) error {
	if evidence == nil {
		return ErrDarkRestoreCutover
	}
	reservation, err := usageledger.ReadHistoricalBudget(ctx, q, evidence.ClientID)
	if err != nil {
		return ErrDarkRestoreCutover
	}
	binding, err := cutoverHistoricalBudgetSHA(*evidence)
	if err != nil || reservation.HistoricalBytes != evidence.SettledHistoricalBytes || reservation.ReceiptSHA != binding {
		return ErrDarkRestoreCutover
	}
	return nil
}
