package business

import (
	"errors"
	"strings"
	"testing"
)

func nativeApplyEvidenceFixture(t *testing.T) (DarkRestoreNativeApplyEvidenceV1, DarkRestoreNativeApplyPinsV1) {
	t.Helper()
	e := DarkRestoreNativeApplyEvidenceV1{
		Scope: DarkRestoreNativeApplyEvidenceScopeV1, ClientID: "m5-native-apply-client",
		TransitionSHA: strings.Repeat("a", 64), PreparedContentSHA: strings.Repeat("b", 64),
		PreparedAuthoritySHA: strings.Repeat("c", 64), InboundID: 801, PlanRevision: 7,
		PreparedInboundRevision: 11, PromotionEventID: "m5-native-apply-promotion-0001",
		PromotionReceiptSHA: strings.Repeat("d", 64), PromotedInboundRevision: 12,
		InitialApplySHA: strings.Repeat("e", 64), ApplyCommandID: strings.Repeat("1", 32),
		StageID: strings.Repeat("2", 32), HealthRoundID: strings.Repeat("3", 32),
		HealthReceiptSHA: strings.Repeat("f", 64), PreparedUnixNano: 100,
		RepresentativeID: "rep-native-apply", ImportReceiptSHA: strings.Repeat("4", 64),
		NativeBaselineSHA: strings.Repeat("5", 64), CurrentFiniteEntitlement: 1000,
		SettledHistoricalBytes: 100, NativePostMigrationBytes: 300, RemainingBudgetBytes: 600,
		PromotedUnixNano: 200, AppliedUnixNano: 300, HealthAcceptedUnixNano: 400,
	}
	p := DarkRestoreNativeApplyPinsV1{
		ClientID: e.ClientID, TransitionSHA: e.TransitionSHA, PreparedContentSHA: e.PreparedContentSHA,
		PreparedAuthoritySHA: e.PreparedAuthoritySHA, InboundID: e.InboundID,
		PlanRevision: e.PlanRevision, PreparedInboundRevision: e.PreparedInboundRevision,
		PromotionEventID: e.PromotionEventID, PromotionReceiptSHA: e.PromotionReceiptSHA,
		InitialApplySHA: e.InitialApplySHA, ApplyCommandID: e.ApplyCommandID, StageID: e.StageID,
		HealthRoundID: e.HealthRoundID, HealthReceiptSHA: e.HealthReceiptSHA, RepresentativeID: e.RepresentativeID,
		ImportReceiptSHA: e.ImportReceiptSHA, NativeBaselineSHA: e.NativeBaselineSHA,
		CurrentFiniteEntitlement: e.CurrentFiniteEntitlement, SettledHistoricalBytes: e.SettledHistoricalBytes,
		NativePostMigrationBytes: e.NativePostMigrationBytes, RemainingBudgetBytes: e.RemainingBudgetBytes,
	}
	var err error
	p.DocumentSHA, err = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", e)
	if err != nil {
		t.Fatal(err)
	}
	return e, p
}

func TestDarkRestoreNativeApplyEvidenceContractV1(t *testing.T) {
	e, p := nativeApplyEvidenceFixture(t)
	got, err := ValidateDarkRestoreNativeApplyEvidenceV1(e, p, 500)
	if err != nil || !got.NativeApplyRecorded || !got.HealthAccepted || got.AuthorizesMutation ||
		got.Scope != DarkRestoreNativeApplyEvidenceScopeV1 || got.EvidenceSHA != p.DocumentSHA {
		t.Fatal(got, err)
	}

	tests := []struct {
		name string
		edit func(*DarkRestoreNativeApplyEvidenceV1, *DarkRestoreNativeApplyPinsV1)
		now  int64
	}{
		{"foreign transition", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.TransitionSHA = strings.Repeat("0", 64)
		}, 500},
		{"foreign content", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.PreparedContentSHA = strings.Repeat("0", 64)
		}, 500},
		{"foreign authority", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.PreparedAuthoritySHA = strings.Repeat("0", 64)
		}, 500},
		{"wrong inbound", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) { e.InboundID++ }, 500},
		{"wrong plan revision", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) { e.PlanRevision++ }, 500},
		{"promotion skipped revision", func(e *DarkRestoreNativeApplyEvidenceV1, p *DarkRestoreNativeApplyPinsV1) {
			e.PromotedInboundRevision++
			p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", *e)
		}, 500},
		{"foreign promotion", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.PromotionReceiptSHA = strings.Repeat("0", 64)
		}, 500},
		{"foreign initial apply", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.InitialApplySHA = strings.Repeat("0", 64)
		}, 500},
		{"foreign apply command", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.ApplyCommandID = strings.Repeat("4", 32)
		}, 500},
		{"foreign stage", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.StageID = strings.Repeat("4", 32)
		}, 500},
		{"foreign health round", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.HealthRoundID = strings.Repeat("4", 32)
		}, 500},
		{"foreign health receipt", func(e *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {
			e.HealthReceiptSHA = strings.Repeat("0", 64)
		}, 500},
		{"repinned foreign payer", func(e *DarkRestoreNativeApplyEvidenceV1, p *DarkRestoreNativeApplyPinsV1) {
			e.RepresentativeID = "rep-native-apply-foreign"
			p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", *e)
		}, 500},
		{"repinned entitlement math", func(e *DarkRestoreNativeApplyEvidenceV1, p *DarkRestoreNativeApplyPinsV1) {
			e.RemainingBudgetBytes++
			p.RemainingBudgetBytes++
			p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", *e)
		}, 500},
		{"promotion before prepare", func(e *DarkRestoreNativeApplyEvidenceV1, p *DarkRestoreNativeApplyPinsV1) {
			e.PromotedUnixNano = e.PreparedUnixNano
			p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", *e)
		}, 500},
		{"apply before promotion", func(e *DarkRestoreNativeApplyEvidenceV1, p *DarkRestoreNativeApplyPinsV1) {
			e.AppliedUnixNano = e.PromotedUnixNano
			p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", *e)
		}, 500},
		{"health before apply", func(e *DarkRestoreNativeApplyEvidenceV1, p *DarkRestoreNativeApplyPinsV1) {
			e.HealthAcceptedUnixNano = e.AppliedUnixNano
			p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", *e)
		}, 500},
		{"future health", func(_ *DarkRestoreNativeApplyEvidenceV1, _ *DarkRestoreNativeApplyPinsV1) {}, 399},
		{"repinned document only", func(e *DarkRestoreNativeApplyEvidenceV1, p *DarkRestoreNativeApplyPinsV1) {
			e.ClientID = "m5-native-apply-foreign"
			p.DocumentSHA, _ = cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", *e)
		}, 500},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bad, pins := nativeApplyEvidenceFixture(t)
			tc.edit(&bad, &pins)
			if got, err := ValidateDarkRestoreNativeApplyEvidenceV1(bad, pins, tc.now); !errors.Is(err, ErrDarkRestoreCutover) || got != (DarkRestoreNativeApplyValidationV1{}) {
				t.Fatal("invalid apply evidence accepted", got, err)
			}
		})
	}
}
