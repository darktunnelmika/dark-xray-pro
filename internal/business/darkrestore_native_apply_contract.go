package business

const DarkRestoreNativeApplyEvidenceScopeV1 = "darkrestore_native_apply_evidence_v1"

// DarkRestoreNativeApplyEvidenceV1 is a pure, append-only projection of the
// existing prepared delivery, plan-promotion and accepted initial-apply/health
// receipts. It does not create those receipts or authorize a mutation.
type DarkRestoreNativeApplyEvidenceV1 struct {
	Scope                    string `json:"scope"`
	ClientID                 string `json:"client_id"`
	TransitionSHA            string `json:"transition_sha256"`
	PreparedContentSHA       string `json:"prepared_content_sha256"`
	PreparedAuthoritySHA     string `json:"prepared_authority_sha256"`
	InboundID                uint64 `json:"inbound_id"`
	PlanRevision             uint64 `json:"plan_revision"`
	PreparedInboundRevision  uint64 `json:"prepared_inbound_revision"`
	PromotionEventID         string `json:"promotion_event_id"`
	PromotionReceiptSHA      string `json:"promotion_receipt_sha256"`
	PromotedInboundRevision  uint64 `json:"promoted_inbound_revision"`
	InitialApplySHA          string `json:"initial_apply_sha256"`
	ApplyCommandID           string `json:"apply_command_id"`
	StageID                  string `json:"stage_id"`
	HealthRoundID            string `json:"health_round_id"`
	HealthReceiptSHA         string `json:"health_receipt_sha256"`
	RepresentativeID         string `json:"representative_id"`
	ImportReceiptSHA         string `json:"import_receipt_sha256"`
	NativeBaselineSHA        string `json:"native_baseline_sha256"`
	CurrentFiniteEntitlement int64  `json:"current_finite_entitlement,string"`
	SettledHistoricalBytes   int64  `json:"settled_historical_bytes,string"`
	NativePostMigrationBytes int64  `json:"native_post_migration_bytes,string"`
	RemainingBudgetBytes     int64  `json:"remaining_budget_bytes,string"`
	PreparedUnixNano         int64  `json:"prepared_unix_nano"`
	PromotedUnixNano         int64  `json:"promoted_unix_nano"`
	AppliedUnixNano          int64  `json:"applied_unix_nano"`
	HealthAcceptedUnixNano   int64  `json:"health_accepted_unix_nano"`
}

// Independent pins are operator/trusted-adapter inputs. No browser boolean or
// unpinned receipt can make an apply complete.
type DarkRestoreNativeApplyPinsV1 struct {
	ClientID                 string
	TransitionSHA            string
	PreparedContentSHA       string
	PreparedAuthoritySHA     string
	InboundID                uint64
	PlanRevision             uint64
	PreparedInboundRevision  uint64
	PromotionEventID         string
	PromotionReceiptSHA      string
	InitialApplySHA          string
	ApplyCommandID           string
	StageID                  string
	HealthRoundID            string
	HealthReceiptSHA         string
	RepresentativeID         string
	ImportReceiptSHA         string
	NativeBaselineSHA        string
	CurrentFiniteEntitlement int64
	SettledHistoricalBytes   int64
	NativePostMigrationBytes int64
	RemainingBudgetBytes     int64
	DocumentSHA              string
}

type DarkRestoreNativeApplyValidationV1 struct {
	Scope               string `json:"scope"`
	EvidenceSHA         string `json:"evidence_sha256"`
	NativeApplyRecorded bool   `json:"native_apply_recorded"`
	HealthAccepted      bool   `json:"health_accepted"`
	AuthorizesMutation  bool   `json:"authorizes_mutation"`
}

func ValidateDarkRestoreNativeApplyEvidenceV1(e DarkRestoreNativeApplyEvidenceV1, p DarkRestoreNativeApplyPinsV1, nowUnixNano int64) (DarkRestoreNativeApplyValidationV1, error) {
	var out DarkRestoreNativeApplyValidationV1
	if e.Scope != DarkRestoreNativeApplyEvidenceScopeV1 || !idRE.MatchString(e.ClientID) ||
		!routeSHA.MatchString(e.TransitionSHA) || !routeSHA.MatchString(e.PreparedContentSHA) ||
		!routeSHA.MatchString(e.PreparedAuthoritySHA) || e.InboundID == 0 || e.InboundID >= routeMax ||
		e.PlanRevision == 0 || e.PlanRevision >= routeMax || e.PreparedInboundRevision == 0 ||
		e.PreparedInboundRevision >= routeMax-1 || e.PromotedInboundRevision != e.PreparedInboundRevision+1 ||
		!eventRE.MatchString(e.PromotionEventID) || !routeSHA.MatchString(e.PromotionReceiptSHA) ||
		!routeSHA.MatchString(e.InitialApplySHA) || !routeID.MatchString(e.ApplyCommandID) ||
		!routeID.MatchString(e.StageID) || !routeID.MatchString(e.HealthRoundID) ||
		!routeSHA.MatchString(e.HealthReceiptSHA) || !idRE.MatchString(e.RepresentativeID) ||
		!routeSHA.MatchString(e.ImportReceiptSHA) || !routeSHA.MatchString(e.NativeBaselineSHA) ||
		e.CurrentFiniteEntitlement <= 0 || e.SettledHistoricalBytes < 0 || e.NativePostMigrationBytes < 0 ||
		e.RemainingBudgetBytes < 0 || e.SettledHistoricalBytes > e.CurrentFiniteEntitlement ||
		e.NativePostMigrationBytes > e.CurrentFiniteEntitlement-e.SettledHistoricalBytes ||
		e.RemainingBudgetBytes != e.CurrentFiniteEntitlement-e.SettledHistoricalBytes-e.NativePostMigrationBytes ||
		e.PreparedUnixNano <= 0 ||
		e.PromotedUnixNano <= e.PreparedUnixNano || e.AppliedUnixNano <= e.PromotedUnixNano ||
		e.HealthAcceptedUnixNano <= e.AppliedUnixNano || nowUnixNano < e.HealthAcceptedUnixNano {
		return out, ErrDarkRestoreCutover
	}
	if p.ClientID != e.ClientID || p.TransitionSHA != e.TransitionSHA ||
		p.PreparedContentSHA != e.PreparedContentSHA || p.PreparedAuthoritySHA != e.PreparedAuthoritySHA ||
		p.InboundID != e.InboundID || p.PlanRevision != e.PlanRevision ||
		p.PreparedInboundRevision != e.PreparedInboundRevision || p.PromotionEventID != e.PromotionEventID ||
		p.PromotionReceiptSHA != e.PromotionReceiptSHA || p.InitialApplySHA != e.InitialApplySHA ||
		p.ApplyCommandID != e.ApplyCommandID || p.StageID != e.StageID ||
		p.HealthRoundID != e.HealthRoundID || p.HealthReceiptSHA != e.HealthReceiptSHA ||
		p.RepresentativeID != e.RepresentativeID || p.ImportReceiptSHA != e.ImportReceiptSHA ||
		p.NativeBaselineSHA != e.NativeBaselineSHA || p.CurrentFiniteEntitlement != e.CurrentFiniteEntitlement ||
		p.SettledHistoricalBytes != e.SettledHistoricalBytes || p.NativePostMigrationBytes != e.NativePostMigrationBytes ||
		p.RemainingBudgetBytes != e.RemainingBudgetBytes ||
		!routeSHA.MatchString(p.DocumentSHA) {
		return out, ErrDarkRestoreCutover
	}
	sha, err := cutoverSnapshotDigest("DARK-PRO-NATIVE-APPLY-EVIDENCE-v1", e)
	if err != nil || sha != p.DocumentSHA {
		return out, ErrDarkRestoreCutover
	}
	return DarkRestoreNativeApplyValidationV1{
		Scope: DarkRestoreNativeApplyEvidenceScopeV1, EvidenceSHA: sha,
		NativeApplyRecorded: true, HealthAccepted: true, AuthorizesMutation: false,
	}, nil
}
