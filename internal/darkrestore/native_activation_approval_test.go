package darkrestore

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

const nativeActivationApprovalNow int64 = 1_000_000_000_000

func nativeActivationApprovalFixtureV1(t *testing.T, change func(*NativeActivationApprovalV1)) ([]byte, []byte, NativeActivationApprovalPinsV1) {
	t.Helper()
	digest := func(v byte) string { return strings.Repeat(string([]byte{v}), 64) }
	v := NativeActivationApprovalV1{
		Version: 1, Scope: "darkrestore_native_activation_operator_approval_v1",
		ApprovalID: "approval-synthetic-0001", OperatorID: "operator-synthetic", AuthoritySHA: digest('1'),
		ApprovedUnixNano: nativeActivationApprovalNow - 1_000, ExpiresUnixNano: nativeActivationApprovalNow + 1_000,
		ActivationEvidenceSHA: digest('2'), DestinationSHA: digest('3'), DestinationStateSHA: digest('4'),
		InventorySHA: digest('5'), CoverageApprovalSHA: digest('6'), BillingAttributionSHA: digest('7'),
		ImportReceiptSHA: digest('8'), NativeBaselineSHA: digest('9'), PreparedAuthoritySHA: digest('a'),
		TransitionSHA: digest('b'), NativeApplyEvidenceSHA: digest('c'), RepresentativeID: "representative-synthetic",
		CurrentFiniteEntitlement: 1_000, SettledHistoricalBytes: 700, NativePostMigrationBytes: 299, RemainingBudgetBytes: 1,
		InboundID: 17, InitialApplySHA: digest('d'), HealthRoundID: "health-round-synthetic",
		DeliveryRevision: 11, SnapshotID: "snapshot-synthetic", SnapshotEventID: "snapshot-event-synthetic-0001",
		SnapshotRevision: 9, ContentSHA: digest('e'), DeliveredUnixNano: nativeActivationApprovalNow - 2_000,
		SubscriptionExpiresUnixNano: nativeActivationApprovalNow + 10_000,
		InventoryComplete:           true, DeclaredSourcesRetired: true, LegacyEntitlementResolved: true,
	}
	if change != nil {
		change(&v)
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	message, err := nativeActivationApprovalMessageV1(v)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(SignedNativeActivationApprovalV1{Version: 1, Approval: v, Signature: ed25519.Sign(private, message)})
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	document, key := sha256.Sum256(raw), sha256.Sum256(public)
	return raw, public, NativeActivationApprovalPinsV1{
		DocumentSHA: hex.EncodeToString(document[:]), PublicKeySHA: hex.EncodeToString(key[:]),
		AuthoritySHA: v.AuthoritySHA, ActivationEvidenceSHA: v.ActivationEvidenceSHA,
		DestinationSHA: v.DestinationSHA, InventorySHA: v.InventorySHA,
	}
}

func TestNativeActivationApprovalSignedEvidenceIsReadOnlyAndCopied(t *testing.T) {
	raw, public, pins := nativeActivationApprovalFixtureV1(t, nil)
	verified, err := VerifySignedNativeActivationApprovalV1(raw, public, pins, nativeActivationApprovalNow)
	if err != nil {
		t.Fatal(err)
	}
	approval, body, key, actualPins, authorizesMutation, err := verified.Evidence()
	if err != nil || approval.RemainingBudgetBytes != 1 || actualPins != pins || authorizesMutation {
		t.Fatal("approval evidence changed authority or budget", err, authorizesMutation)
	}
	body[0] ^= 1
	key[0] ^= 1
	raw[0] ^= 1
	public[0] ^= 1
	if _, body2, key2, _, mutation2, err := verified.Evidence(); err != nil || body2[0] != '{' || len(key2) != ed25519.PublicKeySize || mutation2 {
		t.Fatal("verified approval aliases caller or authorizes mutation", err)
	}
	if _, _, _, _, _, err := (VerifiedNativeActivationApprovalV1{}).Evidence(); !errors.Is(err, ErrHandoffInventory) {
		t.Fatal("unverified approval accepted", err)
	}
}

func TestNativeActivationApprovalRejectsIndependentPinMismatch(t *testing.T) {
	raw, public, pins := nativeActivationApprovalFixtureV1(t, nil)
	for _, name := range []string{"document", "key", "authority", "activation", "destination", "inventory"} {
		t.Run(name, func(t *testing.T) {
			p := pins
			wrong := strings.Repeat("f", 64)
			switch name {
			case "document":
				p.DocumentSHA = wrong
			case "key":
				p.PublicKeySHA = wrong
			case "authority":
				p.AuthoritySHA = wrong
			case "activation":
				p.ActivationEvidenceSHA = wrong
			case "destination":
				p.DestinationSHA = wrong
			case "inventory":
				p.InventorySHA = wrong
			}
			if _, err := VerifySignedNativeActivationApprovalV1(raw, public, p, nativeActivationApprovalNow); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("foreign independent pin accepted", err)
			}
		})
	}
}

func TestNativeActivationApprovalRejectsUnknownEvidenceAndBadBudget(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*NativeActivationApprovalV1)
	}{
		{"inventory not complete", func(v *NativeActivationApprovalV1) { v.InventoryComplete = false }},
		{"sources not retired", func(v *NativeActivationApprovalV1) { v.DeclaredSourcesRetired = false }},
		{"legacy entitlement unknown", func(v *NativeActivationApprovalV1) { v.LegacyEntitlementResolved = false }},
		{"zero finite entitlement", func(v *NativeActivationApprovalV1) { v.CurrentFiniteEntitlement = 0 }},
		{"history exceeds entitlement", func(v *NativeActivationApprovalV1) { v.SettledHistoricalBytes = 1_001 }},
		{"native exceeds remaining", func(v *NativeActivationApprovalV1) { v.NativePostMigrationBytes = 301 }},
		{"remaining mismatch", func(v *NativeActivationApprovalV1) { v.RemainingBudgetBytes = 2 }},
		{"missing attribution", func(v *NativeActivationApprovalV1) { v.BillingAttributionSHA = "" }},
		{"approval before delivery", func(v *NativeActivationApprovalV1) { v.ApprovedUnixNano = v.DeliveredUnixNano - 1 }},
		{"subscription expired", func(v *NativeActivationApprovalV1) { v.SubscriptionExpiresUnixNano = nativeActivationApprovalNow }},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, public, pins := nativeActivationApprovalFixtureV1(t, test.change)
			if _, err := VerifySignedNativeActivationApprovalV1(raw, public, pins, nativeActivationApprovalNow); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("unknown or contradictory approval accepted", err)
			}
		})
	}
}

func TestNativeActivationApprovalRejectsClockAndLeaseViolations(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*NativeActivationApprovalV1)
		now    int64
	}{
		{"not yet approved", nil, nativeActivationApprovalNow - 2_000},
		{"approval expired", nil, nativeActivationApprovalNow + 2_000},
		{"lease too long", func(v *NativeActivationApprovalV1) {
			v.ExpiresUnixNano = v.ApprovedUnixNano + maxNativeActivationApprovalLeaseNano + 1
		}, nativeActivationApprovalNow},
		{"reversed lease", func(v *NativeActivationApprovalV1) { v.ExpiresUnixNano = v.ApprovedUnixNano }, nativeActivationApprovalNow},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, public, pins := nativeActivationApprovalFixtureV1(t, test.change)
			if _, err := VerifySignedNativeActivationApprovalV1(raw, public, pins, test.now); !errors.Is(err, ErrHandoffInventory) {
				t.Fatal("invalid clock or lease accepted", err)
			}
		})
	}
}

func TestNativeActivationApprovalRejectsRepinnedTampering(t *testing.T) {
	raw, public, pins := nativeActivationApprovalFixtureV1(t, nil)
	var signed SignedNativeActivationApprovalV1
	if json.Unmarshal(raw, &signed) != nil {
		t.Fatal("fixture decode")
	}
	signed.Approval.NativePostMigrationBytes++
	tampered, _ := json.Marshal(signed)
	tampered = append(tampered, '\n')
	unknown := append([]byte("{\"unknown\":1,"), raw[1:]...)
	for _, body := range [][]byte{tampered, unknown, append([]byte(" "), raw...), raw[:len(raw)-1]} {
		digest := sha256.Sum256(body)
		p := pins
		p.DocumentSHA = hex.EncodeToString(digest[:])
		if _, err := VerifySignedNativeActivationApprovalV1(body, public, p, nativeActivationApprovalNow); !errors.Is(err, ErrHandoffInventory) {
			t.Fatal("invalid re-pinned approval accepted", err)
		}
	}
}
