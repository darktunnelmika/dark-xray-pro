//go:build linux

package darkrestore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func signedCoverageFixture(t *testing.T, approval HandoffCoverageApprovalV1) ([]byte, string, []byte, string, VerifiedHandoffCoverageApprovalV1) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	message, err := coverageApprovalMessageV1(approval)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(SignedHandoffCoverageApprovalV1{
		Version: 1, Approval: approval, Signature: ed25519.Sign(private, message),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	documentSum := sha256.Sum256(raw)
	publicSum := sha256.Sum256(public)
	documentPin := hex.EncodeToString(documentSum[:])
	publicPin := hex.EncodeToString(publicSum[:])
	verified, err := VerifySignedHandoffCoverageApprovalV1(raw, documentPin, public, publicPin)
	if err != nil {
		t.Fatal(err)
	}
	return raw, documentPin, public, publicPin, verified
}

func completeCoverageFixture(t *testing.T) (LocalHandoffInventoryV1, string, []VerifiedLocalHandoff, HandoffCoverageApprovalV1, VerifiedHandoffCoverageApprovalV1) {
	t.Helper()
	first, a := inventorySignedFixture(t, "1")
	second, b := inventorySignedFixture(t, "2")
	inventory := LocalHandoffInventoryV1{
		Version: 1, SourceSHA: strings.Repeat("a", 64), SourceID: "synthetic-customer",
		Sources: []LocalHandoffSourcePinV1{b, a},
	}
	pin, err := DigestLocalHandoffInventoryV1(inventory)
	if err != nil {
		t.Fatal(err)
	}
	approval := HandoffCoverageApprovalV1{
		DestinationStateSHA: strings.Repeat("4", 64), NativeBaselineSHA: strings.Repeat("1", 64),
		Version: 1, InventorySHA: pin, SourceSHA: inventory.SourceSHA,
		SourceID: inventory.SourceID, Scope: HandoffCoverageScopeV1,
		Statement: HandoffCoverageStatementV1, DeclaredSources: 2,
		EpochIDs:                  []string{a.EpochID, b.EpochID},
		InventoryCompleteAsserted: true, DeclaredSourcesRetired: true,
		ApprovedUnixNano: 1000_000_000_000,
		DestinationSHA:   strings.Repeat("c", 64), ImportReceiptSHA: strings.Repeat("b", 64),
		BillingAttributionSHA: strings.Repeat("f", 64), BillingAttribution: "disjoint_source_epoch_deltas",
	}
	_, _, _, _, verified := signedCoverageFixture(t, approval)
	return inventory, pin, []VerifiedLocalHandoff{second, first}, approval, verified
}

func TestSignedHandoffCoverageApprovalFailsClosed(t *testing.T) {
	inventory, pin, handoffs, approval, _ := completeCoverageFixture(t)
	raw, documentPin, public, publicPin, verified := signedCoverageFixture(t, approval)
	if _, _, _, err := verified.Evidence(); err != nil {
		t.Fatal(err)
	}
	if _, err := CheckLocalHandoffCoverageV1(inventory, pin, handoffs); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		raw       []byte
		docPin    string
		public    []byte
		publicPin string
	}{
		{name: "wrong document pin", raw: raw, docPin: strings.Repeat("e", 64), public: public, publicPin: publicPin},
		{name: "wrong authority pin", raw: raw, docPin: documentPin, public: public, publicPin: strings.Repeat("e", 64)},
		{name: "wrong public key", raw: raw, docPin: documentPin, public: append([]byte(nil), public...), publicPin: publicPin},
		{name: "missing newline", raw: raw[:len(raw)-1], docPin: documentPin, public: public, publicPin: publicPin},
		{name: "noncanonical whitespace", raw: append([]byte(" "), raw...), docPin: documentPin, public: public, publicPin: publicPin},
	}
	tests[2].public[0] ^= 1
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := VerifySignedHandoffCoverageApprovalV1(test.raw, test.docPin, test.public, test.publicPin); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("unsafe approval accepted", err)
			}
		})
	}
	for _, mutate := range []struct {
		name string
		fn   func(*HandoffCoverageApprovalV1)
	}{
		{"missing destination", func(a *HandoffCoverageApprovalV1) { a.DestinationSHA = "" }},
		{"missing import receipt", func(a *HandoffCoverageApprovalV1) { a.ImportReceiptSHA = "" }},
		{"unknown attribution", func(a *HandoffCoverageApprovalV1) { a.BillingAttribution = "unknown" }},
		{"missing attribution evidence", func(a *HandoffCoverageApprovalV1) { a.BillingAttributionSHA = "" }},
		{"invalid retirement bundle", func(a *HandoffCoverageApprovalV1) { a.RetirementBundleSHA = "not-a-digest" }},
		{"not complete", func(a *HandoffCoverageApprovalV1) { a.InventoryCompleteAsserted = false }},
		{"not retired", func(a *HandoffCoverageApprovalV1) { a.DeclaredSourcesRetired = false }},
		{"wrong scope", func(a *HandoffCoverageApprovalV1) { a.Scope = "declared_sources_only" }},
		{"wrong statement", func(a *HandoffCoverageApprovalV1) { a.Statement = "looks_complete" }},
		{"count mismatch", func(a *HandoffCoverageApprovalV1) { a.DeclaredSources++ }},
		{"duplicate epoch", func(a *HandoffCoverageApprovalV1) { a.EpochIDs[1] = a.EpochIDs[0] }},
		{"unordered epochs", func(a *HandoffCoverageApprovalV1) { a.EpochIDs[0], a.EpochIDs[1] = a.EpochIDs[1], a.EpochIDs[0] }},
	} {
		t.Run(mutate.name, func(t *testing.T) {
			changed := approval
			changed.EpochIDs = append([]string(nil), approval.EpochIDs...)
			mutate.fn(&changed)
			public, private, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			message, _ := coverageApprovalMessageV1(changed)
			signed := SignedHandoffCoverageApprovalV1{Version: 1, Approval: changed, Signature: ed25519.Sign(private, message)}
			encoded, _ := json.Marshal(signed)
			encoded = append(encoded, '\n')
			d := sha256.Sum256(encoded)
			k := sha256.Sum256(public)
			if _, err = VerifySignedHandoffCoverageApprovalV1(encoded, hex.EncodeToString(d[:]), public, hex.EncodeToString(k[:])); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("ambiguous completeness assertion accepted", err)
			}
		})
	}
}

func TestLocalHandoffCutoverBudgetAndTransition(t *testing.T) {
	inventory, pin, handoffs, _, approval := completeCoverageFixture(t)
	base := LocalCutoverInputV1{DestinationStateSHA: strings.Repeat("4", 64),
		DestinationSHA: strings.Repeat("c", 64), ImportReceiptSHA: strings.Repeat("b", 64),
		BillingAttributionSHA: strings.Repeat("f", 64), NativeBaselineSHA: strings.Repeat("1", 64),
		ExpectedNativeBaselineSHA: strings.Repeat("1", 64), TransitionSHA: strings.Repeat("2", 64),
		DeliveryTransitionSHA: strings.Repeat("2", 64), NowUnixNano: 2000_000_000_000,
		PlanKind: "volume", CurrentFiniteEntitlement: 200,
		ImportedHistorical: 10, NativeUplink: 3, NativeDownlink: 4,
		NativeBaselineComplete: true, ClientExpiresUnix: 3000, NowUnix: 2000,
		TransitionOpenedUnixNano: 1000_000_000_100,
		SubscriptionRevision:     7, SubscriptionDeliveredNano: 1000_000_000_101,
	}
	got, err := EvaluateLocalHandoffCutoverV1(inventory, pin, handoffs, approval, base)
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthorizesMutation || got.SettledHistoricalBytes != 110 || got.NativePostMigrationBytes != 7 ||
		got.RemainingBudgetBytes != 83 || !got.SubscriptionIssueAllowed ||
		!got.SubscriptionDelivered || !got.ActivationAllowed ||
		len(got.SubscriptionIssueBlockers) != 0 || len(got.ActivationBlockers) != 0 {
		t.Fatalf("unexpected positive decision: %+v", got)
	}

	preDelivery := base
	preDelivery.SubscriptionRevision = 0
	preDelivery.SubscriptionDeliveredNano = 0
	got, err = EvaluateLocalHandoffCutoverV1(inventory, pin, handoffs, approval, preDelivery)
	if err != nil || !got.SubscriptionIssueAllowed || got.SubscriptionDelivered || got.ActivationAllowed ||
		!reflect.DeepEqual(got.ActivationBlockers, []string{"subscription_not_delivered_after_transition"}) {
		t.Fatalf("pre-delivery transition was not isolated: %+v %v", got, err)
	}

	for _, test := range []struct {
		name    string
		change  func(*LocalCutoverInputV1)
		blocker string
	}{
		{"legacy unlimited", func(v *LocalCutoverInputV1) { v.PlanKind = "unlimited" }, "finite_entitlement_required"},
		{"baseline expected snapshot mismatch", func(v *LocalCutoverInputV1) { v.ExpectedNativeBaselineSHA = strings.Repeat("3", 64) }, "native_baseline_incomplete"},
		{"future transition", func(v *LocalCutoverInputV1) { v.TransitionOpenedUnixNano = v.NowUnixNano + 1 }, "transition_not_opened_after_approval"},
		{"simultaneous approval", func(v *LocalCutoverInputV1) { v.TransitionOpenedUnixNano = 1000_000_000_000 }, "transition_not_opened_after_approval"},
		{"clock mismatch", func(v *LocalCutoverInputV1) { v.NowUnixNano++; v.NowUnix++ }, "transition_not_opened_after_approval"},
		{"future delivery", func(v *LocalCutoverInputV1) { v.SubscriptionDeliveredNano = v.NowUnixNano + 1 }, "subscription_not_delivered_after_transition"},
		{"foreign delivery transition", func(v *LocalCutoverInputV1) { v.DeliveryTransitionSHA = strings.Repeat("3", 64) }, "subscription_not_delivered_after_transition"},
		{"unknown baseline", func(v *LocalCutoverInputV1) { v.NativeBaselineComplete = false }, "native_baseline_incomplete"},
		{"usage hold", func(v *LocalCutoverInputV1) { v.NativeHolds = 1 }, "native_usage_holds"},
		{"archived", func(v *LocalCutoverInputV1) { v.ClientArchived = true }, "client_archived"},
		{"expired", func(v *LocalCutoverInputV1) { v.ClientExpiresUnix = v.NowUnix }, "client_expired"},
		{"approval ordering", func(v *LocalCutoverInputV1) { v.TransitionOpenedUnixNano = 999_999_999_999 }, "transition_not_opened_after_approval"},
		{"budget exhausted", func(v *LocalCutoverInputV1) { v.CurrentFiniteEntitlement = 117 }, "finite_budget_exhausted"},
		{"delivery simultaneous", func(v *LocalCutoverInputV1) { v.SubscriptionDeliveredNano = v.TransitionOpenedUnixNano }, "subscription_not_delivered_after_transition"},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.change(&changed)
			decision, err := EvaluateLocalHandoffCutoverV1(inventory, pin, handoffs, approval, changed)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, blocker := range append(append([]string{}, decision.SubscriptionIssueBlockers...), decision.ActivationBlockers...) {
				if blocker == test.blocker {
					found = true
				}
			}
			if !found || decision.ActivationAllowed ||
				test.blocker != "subscription_not_delivered_after_transition" && decision.SubscriptionIssueAllowed {
				t.Fatalf("fail-closed blocker missing: %+v", decision)
			}
		})
	}
}

func TestLocalHandoffCutoverRejectsCoverageMismatch(t *testing.T) {
	inventory, pin, handoffs, approval, verified := completeCoverageFixture(t)
	input := LocalCutoverInputV1{DestinationStateSHA: strings.Repeat("4", 64),
		DestinationSHA: strings.Repeat("c", 64), ImportReceiptSHA: strings.Repeat("b", 64),
		BillingAttributionSHA: strings.Repeat("f", 64), NativeBaselineSHA: strings.Repeat("1", 64),
		ExpectedNativeBaselineSHA: strings.Repeat("1", 64), TransitionSHA: strings.Repeat("2", 64),
		DeliveryTransitionSHA: strings.Repeat("2", 64), NowUnixNano: 2000_000_000_000,
		PlanKind: "volume", CurrentFiniteEntitlement: 200, ImportedHistorical: 10,
		NativeBaselineComplete: true, ClientExpiresUnix: 3000, NowUnix: 2000,
		TransitionOpenedUnixNano: 1000_000_000_100,
	}
	for _, test := range []struct {
		name string
		run  func() error
	}{
		{"changed inventory pin", func() error {
			_, err := EvaluateLocalHandoffCutoverV1(inventory, strings.Repeat("e", 64), handoffs, verified, input)
			return err
		}},
		{"missing receipt", func() error {
			_, err := EvaluateLocalHandoffCutoverV1(inventory, pin, handoffs[:1], verified, input)
			return err
		}},
		{"unverified receipt", func() error {
			_, err := EvaluateLocalHandoffCutoverV1(inventory, pin, []VerifiedLocalHandoff{{}, handoffs[1]}, verified, input)
			return err
		}},
		{"approval inventory mismatch", func() error {
			changed := approval
			changed.InventorySHA = strings.Repeat("e", 64)
			_, _, _, _, bad := signedCoverageFixture(t, changed)
			_, err := EvaluateLocalHandoffCutoverV1(inventory, pin, handoffs, bad, input)
			return err
		}},
		{"approval source mismatch", func() error {
			changed := approval
			changed.SourceID = "different-customer"
			_, _, _, _, bad := signedCoverageFixture(t, changed)
			_, err := EvaluateLocalHandoffCutoverV1(inventory, pin, handoffs, bad, input)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.run(); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("coverage mismatch accepted", err)
			}
		})
	}
}

func TestCoverageApprovalEvidenceIsImmutable(t *testing.T) {
	_, _, _, original, verified := completeCoverageFixture(t)
	first, _, _, err := verified.Evidence()
	if err != nil {
		t.Fatal(err)
	}
	first.EpochIDs[0] = "mutated"
	second, _, _, err := verified.Evidence()
	if err != nil || !reflect.DeepEqual(second.EpochIDs, original.EpochIDs) {
		t.Fatal("verified assertion was mutable", err)
	}
}

func TestCutoverRejectsDestinationReceiptAndAttributionReplay(t *testing.T) {
	inventory, pin, handoffs, _, approval := completeCoverageFixture(t)
	base := LocalCutoverInputV1{DestinationStateSHA: strings.Repeat("4", 64), NativeBaselineSHA: strings.Repeat("1", 64), DestinationSHA: strings.Repeat("c", 64), ImportReceiptSHA: strings.Repeat("b", 64), BillingAttributionSHA: strings.Repeat("f", 64)}
	for _, name := range []string{"destination", "import receipt", "attribution", "destination state", "native snapshot"} {
		t.Run(name, func(t *testing.T) {
			in := base
			switch name {
			case "destination state":
				in.DestinationStateSHA = strings.Repeat("e", 64)
			case "native snapshot":
				in.NativeBaselineSHA = strings.Repeat("e", 64)
			case "destination":
				in.DestinationSHA = strings.Repeat("e", 64)
			case "import receipt":
				in.ImportReceiptSHA = strings.Repeat("e", 64)
			case "attribution":
				in.BillingAttributionSHA = strings.Repeat("e", 64)
			}
			if _, err := EvaluateLocalHandoffCutoverV1(inventory, pin, handoffs, approval, in); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("foreign context accepted", err)
			}
		})
	}
}

func TestCoverageSignatureAndCanonicalEnvelopeCannotBeRepinned(t *testing.T) {
	_, _, _, approval, _ := completeCoverageFixture(t)
	raw, _, public, keyPin, _ := signedCoverageFixture(t, approval)
	var envelope SignedHandoffCoverageApprovalV1
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope.Approval.DestinationSHA = strings.Repeat("e", 64)
	tampered, _ := json.Marshal(envelope)
	tampered = append(tampered, '\n')
	unknown := append([]byte(`{"untrusted_extra":true,`), raw[1:]...)
	for _, body := range [][]byte{tampered, unknown} {
		sum := sha256.Sum256(body)
		if _, err := VerifySignedHandoffCoverageApprovalV1(body, hex.EncodeToString(sum[:]), public, keyPin); !errors.Is(err, ErrHandoffInventory) {
			t.Fatal("re-pinned invalid envelope accepted", err)
		}
	}
}
