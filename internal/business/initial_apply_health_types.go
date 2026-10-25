package business

const InitialApplyHealthScope = "lab_initial_apply"
const HandoverSubscriptionScope = "lab_single_handover"

type InitialApplyHealthBlocker struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

type InitialApplyHealthPreparationInput struct {
	EventID          string `json:"event_id"`
	PromotionEventID string `json:"promotion_event_id"`
}

type InitialApplyHealthPreparation struct {
	EventID          string `json:"event_id"`
	PromotionEventID string `json:"promotion_event_id"`
	InboundID        uint64 `json:"inbound_id"`
	NodeID           string `json:"node_id"`
	InstallationID   string `json:"installation_id"`
	Prepared         bool   `json:"prepared"`
	CreatedUnixNano  int64  `json:"created_unix_nano"`
	Replayed         bool   `json:"replayed"`
}

type InitialApplyHealthState struct {
	Scope            string                         `json:"scope"`
	InboundID        uint64                         `json:"inbound_id"`
	PromotionEventID string                         `json:"promotion_event_id"`
	NodeID           string                         `json:"node_id"`
	InstallationID   string                         `json:"installation_id"`
	InboundRevision  uint64                         `json:"inbound_revision"`
	DraftRevision    uint64                         `json:"draft_revision"`
	DirectPort       uint16                         `json:"direct_port"`
	Prepared         bool                           `json:"prepared"`
	CanPrepare       bool                           `json:"can_prepare"`
	Eligible         bool                           `json:"eligible"`
	Blockers         []InitialApplyHealthBlocker    `json:"blockers"`
	Preparation      *InitialApplyHealthPreparation `json:"preparation,omitempty"`
	InitialApplySHA  string                         `json:"initial_apply_sha,omitempty"`
	ApplyCommandID   string                         `json:"apply_command_id,omitempty"`
	StageID          string                         `json:"stage_id,omitempty"`
	EpochID          string                         `json:"epoch_id,omitempty"`
	ClientIDs        []string                       `json:"client_ids"`
	LatestRound      *InitialApplyHealthRound       `json:"latest_round,omitempty"`
	ServerTimeMillis int64                          `json:"server_time_unix_milli"`
}

type InitialApplyHealthRoundInput struct {
	ID              string `json:"id"`
	InitialApplySHA string `json:"initial_apply_sha"`
}

// The fixed manifest contains exactly the one promoted loopback listener.
// Current means the complete proof is live now; an accepted historical member
// never renews this round's original deadline.
type InitialApplyHealthRound struct {
	ID               string                     `json:"id"`
	Scope            string                     `json:"scope"`
	InboundID        uint64                     `json:"inbound_id"`
	PromotionEventID string                     `json:"promotion_event_id"`
	InitialApplySHA  string                     `json:"initial_apply_sha"`
	NodeID           string                     `json:"node_id"`
	InstallationID   string                     `json:"installation_id"`
	ApplyCommandID   string                     `json:"apply_command_id"`
	StageID          string                     `json:"stage_id"`
	EpochID          string                     `json:"epoch_id"`
	EndpointSHA      string                     `json:"endpoint_sha"`
	ManifestSHA      string                     `json:"manifest_sha"`
	Created          int64                      `json:"created_unix_nano"`
	Expires          int64                      `json:"expires_unix_nano"`
	Members          []MeteredHealthRoundMember `json:"members"`
	State            string                     `json:"state"`
	Current          bool                       `json:"current"`
	Replayed         bool                       `json:"replayed"`
}

type InitialApplyHealthChallengeTarget struct {
	InboundID       uint64
	NodeID          string
	InstallationID  string
	InitialApplySHA string
}

type InitialApplyHealthEvidence struct {
	ChallengeID     string `json:"challenge_id"`
	RoundID         string `json:"round_id"`
	InboundID       uint64 `json:"inbound_id"`
	InitialApplySHA string `json:"initial_apply_sha"`
	RequestSHA      string `json:"request_sha"`
	ReceiptSHA      string `json:"receipt_sha"`
	Accepted        int64  `json:"accepted_unix_nano"`
	Replayed        bool   `json:"replayed"`
}

type InitialApplySubscriptionMaterialization struct {
	EventID                  string `json:"event_id"`
	ClientID                 string `json:"client_id"`
	RoundID                  string `json:"round_id"`
	InitialApplySHA          string `json:"initial_apply_sha"`
	ExpectedRevision         uint64 `json:"expected_revision"`
	ExpectedDeliveryRevision uint64 `json:"expected_delivery_revision"`
}
