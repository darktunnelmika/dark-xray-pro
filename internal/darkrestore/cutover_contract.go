package darkrestore

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

const (
	HandoffCoverageScopeV1     = "all_known_legacy_sources_for_imported_identity"
	HandoffCoverageStatementV1 = "authority_asserts_inventory_complete_and_all_declared_sources_retired"
	handoffCoverageDomainV1    = "DARK-PRO-DARKRESTORE-HANDOFF-COVERAGE-v1\x00"
)

type HandoffCoverageApprovalV1 struct {
	DestinationStateSHA       string   `json:"destination_state_sha256"`
	NativeBaselineSHA         string   `json:"native_baseline_sha256"`
	Version                   int      `json:"version"`
	InventorySHA              string   `json:"inventory_sha256"`
	SourceSHA                 string   `json:"source_sha256"`
	SourceID                  string   `json:"source_id"`
	Scope                     string   `json:"scope"`
	Statement                 string   `json:"statement"`
	DeclaredSources           int      `json:"declared_sources"`
	EpochIDs                  []string `json:"epoch_ids"`
	InventoryCompleteAsserted bool     `json:"inventory_complete_asserted"`
	DeclaredSourcesRetired    bool     `json:"declared_sources_retired"`
	ApprovedUnixNano          int64    `json:"approved_unix_nano,string"`
	DestinationSHA            string   `json:"destination_sha256"`
	ImportReceiptSHA          string   `json:"import_receipt_sha256"`
	BillingAttributionSHA     string   `json:"billing_attribution_sha256"`
	BillingAttribution        string   `json:"billing_attribution"`
}

type SignedHandoffCoverageApprovalV1 struct {
	Version   int                       `json:"version"`
	Approval  HandoffCoverageApprovalV1 `json:"approval"`
	Signature []byte                    `json:"signature"`
}

type VerifiedHandoffCoverageApprovalV1 struct {
	approval     HandoffCoverageApprovalV1
	documentSHA  string
	publicKeySHA string
	valid        bool
}

func coverageApprovalMessageV1(in HandoffCoverageApprovalV1) ([]byte, error) {
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, ErrHandoffInventory
	}
	return append([]byte(handoffCoverageDomainV1), raw...), nil
}

func canonicalCoverageApprovalV1(in HandoffCoverageApprovalV1) (HandoffCoverageApprovalV1, error) {
	if in.Version != 1 || !inventoryDigestValid(in.InventorySHA) ||
		!inventoryDigestValid(in.SourceSHA) || in.SourceID == "" ||
		in.Scope != HandoffCoverageScopeV1 ||
		in.Statement != HandoffCoverageStatementV1 ||
		!in.InventoryCompleteAsserted || !in.DeclaredSourcesRetired ||
		in.DeclaredSources < 1 || in.DeclaredSources > 256 ||
		len(in.EpochIDs) != in.DeclaredSources || in.ApprovedUnixNano <= 0 ||
		!inventoryDigestValid(in.DestinationSHA) || !inventoryDigestValid(in.ImportReceiptSHA) ||
		!inventoryDigestValid(in.DestinationStateSHA) || !inventoryDigestValid(in.NativeBaselineSHA) ||
		!inventoryDigestValid(in.BillingAttributionSHA) || in.BillingAttribution != "disjoint_source_epoch_deltas" {
		return HandoffCoverageApprovalV1{}, ErrHandoffInventory
	}
	in.EpochIDs = append([]string(nil), in.EpochIDs...)
	sort.Strings(in.EpochIDs)
	for i, epoch := range in.EpochIDs {
		if !validInventoryEpochID(epoch) || (i > 0 && in.EpochIDs[i-1] == epoch) {
			return HandoffCoverageApprovalV1{}, ErrHandoffInventory
		}
	}
	return in, nil
}

func validInventoryEpochID(epoch string) bool {
	return uc.ValidReceipt(uc.Receipt{EpochID: epoch, Sequence: 1, SHA: "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"})
}

// VerifySignedHandoffCoverageApprovalV1 checks an independently pinned document
// and independently pinned Ed25519 authority. Cryptographic validity proves who
// made the assertion, not that undeclared legacy sources do not exist.
func VerifySignedHandoffCoverageApprovalV1(raw []byte, documentPin string, publicKey []byte, publicKeyPin string) (VerifiedHandoffCoverageApprovalV1, error) {
	var zero VerifiedHandoffCoverageApprovalV1
	if len(raw) == 0 || len(raw) > 16<<10 || !bytes.HasSuffix(raw, []byte("\n")) ||
		!inventoryDigestValid(documentPin) || !inventoryDigestValid(publicKeyPin) ||
		len(publicKey) != ed25519.PublicKeySize {
		return zero, ErrHandoffInventory
	}
	documentSum := sha256.Sum256(raw)
	publicSum := sha256.Sum256(publicKey)
	if hex.EncodeToString(documentSum[:]) != documentPin ||
		hex.EncodeToString(publicSum[:]) != publicKeyPin {
		return zero, ErrHandoffInventory
	}
	var signed SignedHandoffCoverageApprovalV1
	if json.Unmarshal(raw, &signed) != nil || signed.Version != 1 ||
		len(signed.Signature) != ed25519.SignatureSize {
		return zero, ErrHandoffInventory
	}
	canonicalApproval, err := canonicalCoverageApprovalV1(signed.Approval)
	if err != nil || !equalStrings(canonicalApproval.EpochIDs, signed.Approval.EpochIDs) {
		return zero, ErrHandoffInventory
	}
	signed.Approval = canonicalApproval
	canonical, err := json.Marshal(signed)
	if err != nil || !bytes.Equal(raw, append(canonical, '\n')) {
		return zero, ErrHandoffInventory
	}
	message, err := coverageApprovalMessageV1(canonicalApproval)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), message, signed.Signature) {
		return zero, ErrHandoffInventory
	}
	return VerifiedHandoffCoverageApprovalV1{
		approval: canonicalApproval, documentSHA: documentPin,
		publicKeySHA: publicKeyPin, valid: true,
	}, nil
}

func (v VerifiedHandoffCoverageApprovalV1) Evidence() (HandoffCoverageApprovalV1, string, string, error) {
	if !v.valid {
		return HandoffCoverageApprovalV1{}, "", "", ErrHandoffInventory
	}
	a := v.approval
	a.EpochIDs = append([]string(nil), a.EpochIDs...)
	return a, v.documentSHA, v.publicKeySHA, nil
}

// LocalCutoverInputV1 is a model input, not a browser/API authorization payload.
// Destination/receipt/attribution pins must be independently loaded from trusted
// destination state. Baseline counters must come from one authoritative snapshot.
type LocalCutoverInputV1 struct {
	DestinationStateSHA       string
	DestinationSHA            string
	ImportReceiptSHA          string
	BillingAttributionSHA     string
	NativeBaselineSHA         string
	ExpectedNativeBaselineSHA string
	TransitionSHA             string
	DeliveryTransitionSHA     string
	NowUnixNano               int64
	PlanKind                  string
	CurrentFiniteEntitlement  int64
	ImportedHistorical        int64
	NativeUplink              int64
	NativeDownlink            int64
	NativeBaselineComplete    bool
	NativeHolds               int64
	ClientArchived            bool
	ClientExpiresUnix         int64
	NowUnix                   int64
	TransitionOpenedUnixNano  int64
	SubscriptionRevision      uint64
	SubscriptionDeliveredNano int64
}

type LocalCutoverDecisionV1 struct {
	// This pure model cannot authorize mutations; a Store transaction is required.
	AuthorizesMutation         bool     `json:"authorizes_mutation"`
	InventorySHA               string   `json:"inventory_sha256"`
	CoverageApprovalSHA        string   `json:"coverage_approval_sha256"`
	CoverageAuthorityPublicSHA string   `json:"coverage_authority_public_key_sha256"`
	DeclaredSources            int      `json:"declared_sources"`
	SettledHistoricalBytes     int64    `json:"settled_historical_bytes,string"`
	NativePostMigrationBytes   int64    `json:"native_post_migration_bytes,string"`
	RemainingBudgetBytes       int64    `json:"remaining_budget_bytes,string"`
	SubscriptionIssueAllowed   bool     `json:"subscription_issue_allowed"`
	SubscriptionDelivered      bool     `json:"subscription_delivered"`
	ActivationAllowed          bool     `json:"activation_allowed"`
	SubscriptionIssueBlockers  []string `json:"subscription_issue_blockers"`
	ActivationBlockers         []string `json:"activation_blockers"`
}

func addCutoverBytes(a, b int64) (int64, bool) {
	if a < 0 || b < 0 || a > math.MaxInt64-b {
		return 0, false
	}
	return a + b, true
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// EvaluateLocalHandoffCutoverV1 is a pure fail-closed M5 transition model.
// It has no Store authority and never removes the existing quarantine.
// A signed, independently pinned attribution assertion is required before
// adding epoch deltas: inventory coverage alone does not establish disjointness. The baseline is
// the complete destination usage from imported-client creation through the
// opening of this transition. Remaining budget is current finite entitlement,
// minus the imported historical baseline, every declared retired-source epoch,
// and native post-migration usage. Later paid renewals increase only the current
// entitlement; no history or payer ledger is rewritten.
func EvaluateLocalHandoffCutoverV1(inventory LocalHandoffInventoryV1, inventoryPin string, handoffs []VerifiedLocalHandoff, approval VerifiedHandoffCoverageApprovalV1, in LocalCutoverInputV1) (LocalCutoverDecisionV1, error) {
	out := LocalCutoverDecisionV1{
		SubscriptionIssueBlockers: []string{},
		ActivationBlockers:        []string{},
	}
	coverage, err := CheckLocalHandoffCoverageV1(inventory, inventoryPin, handoffs)
	if err != nil {
		return out, err
	}
	assertion, approvalSHA, authoritySHA, err := approval.Evidence()
	if err != nil || assertion.InventorySHA != coverage.InventorySHA ||
		assertion.SourceSHA != inventory.SourceSHA || assertion.SourceID != inventory.SourceID ||
		assertion.DeclaredSources != coverage.MatchedSources ||
		!equalStrings(assertion.EpochIDs, coverage.EpochIDs) ||
		assertion.DestinationSHA != in.DestinationSHA ||
		assertion.DestinationStateSHA != in.DestinationStateSHA ||
		assertion.NativeBaselineSHA != in.NativeBaselineSHA ||
		assertion.ImportReceiptSHA != in.ImportReceiptSHA ||
		assertion.BillingAttributionSHA != in.BillingAttributionSHA {
		return out, ErrHandoffInventory
	}
	out.InventorySHA = coverage.InventorySHA
	out.CoverageApprovalSHA = approvalSHA
	out.CoverageAuthorityPublicSHA = authoritySHA
	out.DeclaredSources = coverage.MatchedSources

	settled := in.ImportedHistorical
	if settled < 0 {
		return LocalCutoverDecisionV1{}, ErrHandoffInventory
	}
	for _, handoff := range handoffs {
		proof, e := handoff.Settlement()
		if e != nil || proof.PriorBytes != in.ImportedHistorical {
			return LocalCutoverDecisionV1{}, ErrHandoffInventory
		}
		historical, ok := addCutoverBytes(proof.PriorBytes, proof.Up)
		if !ok {
			return LocalCutoverDecisionV1{}, ErrHandoffInventory
		}
		historical, ok = addCutoverBytes(historical, proof.Down)
		if !ok || historical != proof.Historical {
			return LocalCutoverDecisionV1{}, ErrHandoffInventory
		}
		settled, ok = addCutoverBytes(settled, proof.Up)
		if !ok {
			return LocalCutoverDecisionV1{}, ErrHandoffInventory
		}
		settled, ok = addCutoverBytes(settled, proof.Down)
		if !ok {
			return LocalCutoverDecisionV1{}, ErrHandoffInventory
		}
	}
	native, ok := addCutoverBytes(in.NativeUplink, in.NativeDownlink)
	if !ok {
		return LocalCutoverDecisionV1{}, ErrHandoffInventory
	}
	out.SettledHistoricalBytes = settled
	out.NativePostMigrationBytes = native

	if in.PlanKind != "volume" {
		out.SubscriptionIssueBlockers = append(out.SubscriptionIssueBlockers, "finite_entitlement_required")
	}
	if !in.NativeBaselineComplete || !inventoryDigestValid(in.NativeBaselineSHA) ||
		in.NativeBaselineSHA != in.ExpectedNativeBaselineSHA {
		out.SubscriptionIssueBlockers = append(out.SubscriptionIssueBlockers, "native_baseline_incomplete")
	}
	if in.NativeHolds != 0 {
		out.SubscriptionIssueBlockers = append(out.SubscriptionIssueBlockers, "native_usage_holds")
	}
	if in.ClientArchived {
		out.SubscriptionIssueBlockers = append(out.SubscriptionIssueBlockers, "client_archived")
	}
	if in.NowUnix <= 0 || in.ClientExpiresUnix <= in.NowUnix {
		out.SubscriptionIssueBlockers = append(out.SubscriptionIssueBlockers, "client_expired")
	}
	if !inventoryDigestValid(in.TransitionSHA) || in.NowUnixNano <= 0 ||
		in.NowUnixNano/1_000_000_000 != in.NowUnix ||
		assertion.ApprovedUnixNano >= in.TransitionOpenedUnixNano ||
		in.TransitionOpenedUnixNano > in.NowUnixNano {
		out.SubscriptionIssueBlockers = append(out.SubscriptionIssueBlockers, "transition_not_opened_after_approval")
	}
	total, ok := addCutoverBytes(settled, native)
	if !ok || in.CurrentFiniteEntitlement <= 0 || total >= in.CurrentFiniteEntitlement {
		out.SubscriptionIssueBlockers = append(out.SubscriptionIssueBlockers, "finite_budget_exhausted")
	} else {
		out.RemainingBudgetBytes = in.CurrentFiniteEntitlement - total
	}
	out.SubscriptionIssueAllowed = len(out.SubscriptionIssueBlockers) == 0

	out.SubscriptionDelivered = in.SubscriptionRevision > 0 &&
		in.SubscriptionDeliveredNano > in.TransitionOpenedUnixNano &&
		in.SubscriptionDeliveredNano <= in.NowUnixNano &&
		inventoryDigestValid(in.TransitionSHA) && in.DeliveryTransitionSHA == in.TransitionSHA
	out.ActivationBlockers = append(out.ActivationBlockers, out.SubscriptionIssueBlockers...)
	if !out.SubscriptionDelivered {
		out.ActivationBlockers = append(out.ActivationBlockers, "subscription_not_delivered_after_transition")
	}
	out.ActivationAllowed = len(out.ActivationBlockers) == 0
	return out, nil
}
