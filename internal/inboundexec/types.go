// Package inboundexec is the versioned contract for managed inbound execution.
// Authoring plans grant no execution authority. Private bundles are never
// returned by the owner review/status endpoints.
package inboundexec

import (
	"context"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

const Version = 1
const SchemaVersion = 16
const AccountingPerClient = "per_client"
const AccountingOwnerAggregate = "owner_aggregate"

type Blocker struct {
	Code      string `json:"code"`
	Field     string `json:"field,omitempty"`
	NodeID    string `json:"node_id,omitempty"`
	InboundID uint64 `json:"inbound_id,omitempty"`
	ClientID  string `json:"client_id,omitempty"`
}

type CommandInput struct {
	EventID                 string   `json:"event_id"`
	Action                  string   `json:"action"`
	ExpectedRevision        uint64   `json:"expected_revision"`
	ExpectedPlanRevision    uint64   `json:"expected_plan_revision"`
	ExpectedInboundRevision uint64   `json:"expected_inbound_revision"`
	ClientIDs               []string `json:"client_ids"`
	AccountingMode          string   `json:"accounting_mode"`
	Reason                  string   `json:"reason"`
	ReviewSHA               string   `json:"review_sha,omitempty"`
}

// BindingsInput edits desired credentials only. An applied generation retains
// its immutable binding snapshot until the next reviewed apply.
type BindingsInput struct {
	EventID          string   `json:"event_id"`
	ExpectedRevision uint64   `json:"expected_revision"`
	ClientIDs        []string `json:"client_ids"`
	AccountingMode   string   `json:"accounting_mode"`
	Reason           string   `json:"reason"`
}

type State struct {
	Version                int                   `json:"version"`
	Enabled                bool                  `json:"enabled"`
	InboundID              uint64                `json:"inbound_id"`
	Revision               uint64                `json:"revision"`
	PlanRevision           uint64                `json:"plan_revision"`
	InboundRevision        uint64                `json:"inbound_revision"`
	DesiredRevision        uint64                `json:"desired_revision"`
	ActiveRevision         uint64                `json:"active_revision"`
	ActivePlanRevision     uint64                `json:"active_plan_revision"`
	ActiveInboundRevision  uint64                `json:"active_inbound_revision"`
	PendingChanges         bool                  `json:"pending_changes"`
	DesiredState           string                `json:"desired_state"`
	ObservedState          string                `json:"observed_state"`
	AccountingMode         string                `json:"accounting_mode"`
	AllowedAccountingModes []string              `json:"allowed_accounting_modes"`
	ServerTimeMillis       int64                 `json:"server_time_unix_milli"`
	PublicationState       string                `json:"publication_state"`
	ClientIDs              []string              `json:"client_ids"`
	EffectiveClientIDs     []string              `json:"effective_client_ids"`
	Targets                []TargetState         `json:"targets"`
	AllowedActions         []string              `json:"allowed_actions"`
	Blockers               []Blocker             `json:"blockers"`
	LatestOperation        *Operation            `json:"latest_operation,omitempty"`
	Reconciliations        []ReconciliationState `json:"reconciliations"`
}

type TargetState struct {
	NodeID             string     `json:"node_id"`
	InstallationID     string     `json:"installation_id"`
	DesiredGeneration  uint64     `json:"desired_generation"`
	ObservedGeneration uint64     `json:"observed_generation"`
	State              string     `json:"state"`
	ConfigSHA          string     `json:"config_sha,omitempty"`
	ManifestSHA        string     `json:"manifest_sha,omitempty"`
	Listeners          []Listener `json:"listeners"`
	Blockers           []Blocker  `json:"blockers"`
	ReportedAt         int64      `json:"reported_at_unix_nano"`
	LeaseExpires       int64      `json:"lease_expires_unix_nano"`
	ProcessReady       bool       `json:"process_ready"`
	HealthState        string     `json:"health_state"`
	AccountingState    string     `json:"accounting_state"`
}

type Preview struct {
	Valid     bool          `json:"valid"`
	ReviewSHA string        `json:"review_sha"`
	State     State         `json:"state"`
	Action    string        `json:"action"`
	Targets   []TargetState `json:"targets"`
	Blockers  []Blocker     `json:"blockers"`
}

type Operation struct {
	ID              string        `json:"id"`
	EventID         string        `json:"event_id"`
	InboundID       uint64        `json:"inbound_id"`
	Action          string        `json:"action"`
	Revision        uint64        `json:"revision"`
	State           string        `json:"state"`
	CreatedUnixNano int64         `json:"created_unix_nano"`
	UpdatedUnixNano int64         `json:"updated_unix_nano"`
	Targets         []TargetState `json:"targets"`
	Blockers        []Blocker     `json:"blockers"`
	Replayed        bool          `json:"replayed"`
}

// Principal is an immutable counter binding. Aggregate counters have an owner
// principal and are never charged to a guessed retail client.
type Principal struct {
	ID            string `json:"id"`
	Engine        string `json:"engine"`
	ComponentTag  string `json:"component_tag,omitempty"`
	ClientID      string `json:"client_id,omitempty"`
	Role          string `json:"role"`
	Counter       string `json:"counter"`
	CredentialSHA string `json:"credential_sha,omitempty"`
	InboundID     uint64 `json:"inbound_id"`
	Billable      bool   `json:"billable"`
}

type Listener struct {
	InboundID        uint64 `json:"inbound_id"`
	Tag              string `json:"tag"`
	Engine           string `json:"engine"`
	Protocol         string `json:"protocol"`
	Transport        string `json:"transport"`
	Security         string `json:"security"`
	Kind             string `json:"kind"`
	Listen           string `json:"listen"`
	Port             uint16 `json:"port"`
	Network          string `json:"network"`
	ResourceKind     string `json:"resource_kind"`
	AccountingMode   string `json:"accounting_mode"`
	DependencyNodeID string `json:"dependency_node_id,omitempty"`
}

type Component struct {
	Engine   string `json:"engine"`
	Tag      string `json:"tag"`
	Config   []byte `json:"config"`
	Manifest []byte `json:"manifest"`
	SHA256   string `json:"sha256"`
}

type RevisionRef struct {
	InboundID       uint64 `json:"inbound_id"`
	Revision        uint64 `json:"revision"`
	PlanRevision    uint64 `json:"plan_revision"`
	InboundRevision uint64 `json:"inbound_revision"`
}

// Bundle is private, complete desired state for exactly one installation.
// Generation is monotonic for the node, including empty stop bundles.
type Bundle struct {
	Version        int           `json:"version"`
	NodeID         string        `json:"node_id"`
	InstallationID string        `json:"installation_id"`
	Generation     uint64        `json:"generation"`
	OperationID    string        `json:"operation_id"`
	Revisions      []RevisionRef `json:"revisions"`
	NativeConfig   []byte        `json:"native_config"`
	Components     []Component   `json:"components"`
	Listeners      []Listener    `json:"listeners"`
	Principals     []Principal   `json:"principals"`
	ConfigSHA      string        `json:"config_sha"`
	ManifestSHA    string        `json:"manifest_sha"`
	Signature      string        `json:"signature"`
}

type CompileItem struct {
	Ref            RevisionRef
	Profile        xrayconfig.Profile
	Topology       xrayconfig.CreationTopology
	AccountingMode string
	Principals     []Principal
}
type CompileRequest struct {
	NodeID         string
	InstallationID string
	Generation     uint64
	OperationID    string
	Items          []CompileItem
}
type Compiler interface {
	Compile(context.Context, CompileRequest) (Bundle, error)
}

// Observation is accepted only for the exact desired bundle and installation.
// Ready is a process/health statement; financial acceptance has separate
// durable epoch/usage receipts and cannot be implied by this observation.
type Observation struct {
	NodeID           string `json:"node_id"`
	InstallationID   string `json:"installation_id"`
	Generation       uint64 `json:"generation"`
	OperationID      string `json:"operation_id"`
	ConfigSHA        string `json:"config_sha"`
	ManifestSHA      string `json:"manifest_sha"`
	State            string `json:"state"`
	EpochID          string `json:"epoch_id,omitempty"`
	ErrorCode        string `json:"error_code,omitempty"`
	ObservedUnixNano int64  `json:"observed_unix_nano"`
	LeaseExpires     int64  `json:"lease_expires_unix_nano"`
	ProcessReady     bool   `json:"process_ready"`
	HealthState      string `json:"health_state"`
	AccountingState  string `json:"accounting_state"`
	// InitialEpoch identifies a lost, never-acknowledged birth when reporting
	// uncertainty. It records identity only; it is not a zero baseline.
	InitialEpoch *EpochInput `json:"initial_epoch,omitempty"`
}

type EpochInput struct {
	EpochID         string            `json:"epoch_id"`
	NodeID          string            `json:"node_id"`
	InstallationID  string            `json:"installation_id"`
	Generation      uint64            `json:"generation"`
	ConfigSHA       string            `json:"config_sha"`
	ManifestSHA     string            `json:"manifest_sha"`
	ProcessIdentity string            `json:"process_identity"`
	BinarySHAs      map[string]string `json:"binary_shas"`
}
type Counter struct {
	PrincipalID string `json:"principal_id"`
	Upload      uint64 `json:"upload,string"`
	Download    uint64 `json:"download,string"`
}
type UsageReport struct {
	EpochID         string    `json:"epoch_id"`
	NodeID          string    `json:"node_id"`
	InstallationID  string    `json:"installation_id"`
	Generation      uint64    `json:"generation"`
	Sequence        uint64    `json:"sequence"`
	Final           bool      `json:"final"`
	ProcessIdentity string    `json:"process_identity"`
	FinalNonce      string    `json:"final_nonce,omitempty"`
	FinalProofSHA   string    `json:"final_proof_sha,omitempty"`
	Counters        []Counter `json:"counters"`
	// InitialEpoch is allowed only on a genuinely sealed first, zero report.
	// It settles a closed birth whose registration never reached the HUB.
	InitialEpoch *EpochInput `json:"initial_epoch,omitempty"`
}
type UsageReceipt struct {
	EpochID  string `json:"epoch_id"`
	Sequence uint64 `json:"sequence"`
	SHA256   string `json:"sha256"`
	Final    bool   `json:"final"`
	Replayed bool   `json:"replayed"`
}

// SubscriptionView contains no credential or client document. Exports are
// projected from current applied revisions after authorization on every read.
type SubscriptionView struct {
	ClientID        string    `json:"client_id"`
	Generation      uint64    `json:"generation"`
	State           string    `json:"state"`
	Available       bool      `json:"available"`
	Formats         []string  `json:"formats"`
	RouteCount      int       `json:"route_count"`
	ExpiresUnixNano int64     `json:"expires_unix_nano"`
	PathVerified    bool      `json:"path_verified"`
	Blockers        []Blocker `json:"blockers"`
}

type SubscriptionRoute struct {
	InboundID uint64 `json:"inbound_id"`
	Revision  uint64 `json:"revision"`
	NodeID    string `json:"node_id"`
	Path      string `json:"path"`
	Protocol  string `json:"protocol"`
	Name      string `json:"name"`
}

type Subscription struct {
	Version         int                 `json:"version"`
	ClientID        string              `json:"client_id"`
	Format          string              `json:"format"`
	SHA256          string              `json:"sha256"`
	ExpiresUnixNano int64               `json:"expires_unix_nano"`
	PathVerified    bool                `json:"path_verified"`
	Routes          []SubscriptionRoute `json:"routes"`
	Content         []byte              `json:"-"`
	ContentType     string              `json:"content_type"`
}
