package inboundexec

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type ReconciliationUsage struct {
	PrincipalID string `json:"principal_id"`
	InboundID   uint64 `json:"inbound_id"`
	ClientID    string `json:"client_id"`
	Role        string `json:"role"`
	Upload      uint64 `json:"upload,string"`
	Download    uint64 `json:"download,string"`
	Held        bool   `json:"held"`
}

type ReconciliationState struct {
	EpochID            string                `json:"epoch_id"`
	NodeID             string                `json:"node_id"`
	InstallationID     string                `json:"installation_id"`
	Generation         uint64                `json:"generation"`
	Revision           uint64                `json:"revision"`
	State              string                `json:"state"`
	ConfigSHA          string                `json:"config_sha"`
	ManifestSHA        string                `json:"manifest_sha"`
	ProcessIdentitySHA string                `json:"process_identity_sha"`
	KnownUsage         []ReconciliationUsage `json:"known_usage"`
	AffectedClientIDs  []string              `json:"affected_client_ids"`
	AffectedInboundIDs []uint64              `json:"affected_inbound_ids"`
	HasMeasuredUsage   bool                  `json:"has_measured_usage"`
	UnknownUsagePolicy string                `json:"unknown_usage_policy"`
	AllowedActions     []string              `json:"allowed_actions"`
	Blockers           []Blocker             `json:"blockers"`
}

type ReconciliationInput struct {
	EventID          string `json:"event_id"`
	EpochID          string `json:"epoch_id"`
	ExpectedRevision uint64 `json:"expected_revision"`
	Action           string `json:"action"`
	Reason           string `json:"reason"`
	ReviewSHA        string `json:"review_sha,omitempty"`
}

type ReconciliationPreview struct {
	Valid     bool                `json:"valid"`
	ReviewSHA string              `json:"review_sha"`
	State     ReconciliationState `json:"state"`
	Blockers  []Blocker           `json:"blockers"`
}

type ReconciliationReceipt struct {
	EventID            string                `json:"event_id"`
	EpochID            string                `json:"epoch_id"`
	InboundID          uint64                `json:"inbound_id"`
	NodeID             string                `json:"node_id"`
	InstallationID     string                `json:"installation_id"`
	Generation         uint64                `json:"generation"`
	ActorID            string                `json:"actor_id"`
	Reason             string                `json:"reason"`
	Revision           uint64                `json:"revision"`
	State              string                `json:"state"`
	UnknownUsagePolicy string                `json:"unknown_usage_policy"`
	KnownUsage         []ReconciliationUsage `json:"known_usage"`
	AffectedClientIDs  []string              `json:"affected_client_ids"`
	AffectedInboundIDs []uint64              `json:"affected_inbound_ids"`
	HasMeasuredUsage   bool                  `json:"has_measured_usage"`
	CreatedUnixNano    int64                 `json:"created_unix_nano"`
	Replayed           bool                  `json:"replayed"`
}

// EpochClearance is private Agent authority to clear exactly one reconciled
// uncertain journal. It cannot start a process, waive another epoch, or replace
// its own delivery lease. Known pending reports are acknowledged before use.
type EpochClearance struct {
	Version          int    `json:"version"`
	EventID          string `json:"event_id"`
	EpochID          string `json:"epoch_id"`
	NodeID           string `json:"node_id"`
	InstallationID   string `json:"installation_id"`
	Generation       uint64 `json:"generation"`
	EpochSHA         string `json:"epoch_sha"`
	KnownSequence    uint64 `json:"known_sequence"`
	KnownCountersSHA string `json:"known_counters_sha"`
	Policy           string `json:"policy"`
	CreatedUnixNano  int64  `json:"created_unix_nano"`
	Signature        string `json:"signature"`
}

func SealClearance(key []byte, c EpochClearance) (EpochClearance, error) {
	if len(key) < 32 || c.Version != Version || c.EventID == "" || c.EpochID == "" || c.NodeID == "" || len(c.InstallationID) != 32 || c.Generation == 0 || !validIdentitySHA(c.EpochSHA) || !validIdentitySHA(c.KnownCountersSHA) || c.Policy != "owner_loss" || c.CreatedUnixNano <= 0 {
		return EpochClearance{}, ErrInvalid
	}
	c.Signature = ""
	raw, err := json.Marshal(c)
	if err != nil {
		return EpochClearance{}, err
	}
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("DARK-PRO-MANAGED-EPOCH-CLEARANCE-v1\x00"))
	_, _ = h.Write(raw)
	c.Signature = hex.EncodeToString(h.Sum(nil))
	return c, nil
}

func VerifyClearance(key []byte, c EpochClearance) error {
	expected, err := SealClearance(key, c)
	if err != nil || !hmac.Equal([]byte(c.Signature), []byte(expected.Signature)) {
		return ErrInvalid
	}
	return nil
}
