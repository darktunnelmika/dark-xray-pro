// Package backupcontrol owns the durable, non-financial control plane for
// isolated native backup jobs. It never stops services or reads recovery keys.
package backupcontrol

import (
	"context"
	"errors"
	"io"
)

const SocketPath = "/run/dark-xray-pro-backup-worker/control.sock"
const ProtocolBase = "/v1"

// The control surface intentionally uses a tighter bound than the generic
// codec's configurable limits. All publication and download layers share it.
const MaxArchiveBytes int64 = 66 << 30

var (
	ErrInvalid     = errors.New("invalid_backup_control_input")
	ErrConflict    = errors.New("backup_control_conflict")
	ErrNotFound    = errors.New("backup_control_not_found")
	ErrCapacity    = errors.New("backup_control_capacity")
	ErrUnavailable = errors.New("backup_runner_unavailable")
	ErrUnsafe      = errors.New("unsafe_backup_control_state")
	ErrBusy        = errors.New("backup_control_busy")
)

type Schedule struct {
	Enabled   bool   `json:"enabled"`
	Frequency string `json:"frequency"`
	Time      string `json:"time"`
	Timezone  string `json:"timezone"`
	Weekday   int    `json:"weekday"`
}
type Retention struct {
	KeepLast   int `json:"keep_last"`
	MaxAgeDays int `json:"max_age_days"`
}
type Settings struct {
	Revision  uint64    `json:"revision"`
	Schedule  Schedule  `json:"schedule"`
	Retention Retention `json:"retention"`
}
type Capabilities struct {
	Create    bool `json:"create"`
	Download  bool `json:"download"`
	Cancel    bool `json:"cancel"`
	Schedules bool `json:"schedules"`
	Retention bool `json:"retention"`
	Upload    bool `json:"upload"`
	Restore   bool `json:"restore"`
}
type Status struct {
	Enabled              bool         `json:"enabled"`
	RunnerStatus         string       `json:"runner_status"`
	RecipientFingerprint string       `json:"recipient_fingerprint"`
	Capabilities         Capabilities `json:"capabilities"`
	Settings             Settings     `json:"settings"`
	NextRunUnix          int64        `json:"next_run_unix"`
	Jobs                 []Job        `json:"jobs"`
	Archives             []Archive    `json:"archives"`
}
type Job struct {
	ID               string `json:"id"`
	EventID          string `json:"event_id"`
	ActorID          string `json:"actor_id"`
	Action           string `json:"action"`
	Trigger          string `json:"trigger"`
	State            string `json:"state"`
	Phase            string `json:"phase"`
	CreatedUnix      int64  `json:"created_unix"`
	StartedUnix      int64  `json:"started_unix"`
	FinishedUnix     int64  `json:"finished_unix"`
	ArchiveID        string `json:"archive_id"`
	CancelRequested  bool   `json:"cancel_requested"`
	ErrorCode        string `json:"error_code"`
	WarningCode      string `json:"warning_code"`
	DowntimeMS       int64  `json:"downtime_ms"`
	Revision         uint64 `json:"revision"`
	ResumeRequired   bool   `json:"resume_required"`
	PreServiceActive bool   `json:"pre_service_active"`
	HubStoppedUnix   int64  `json:"hub_stopped_unix"`
	HubResumedUnix   int64  `json:"hub_resumed_unix"`
	// Recipient is an age PUBLIC recipient, never a recovery identity.
	Recipient string   `json:"recipient"`
	Policy    Settings `json:"policy"`
}
type Archive struct {
	ID             string `json:"id"`
	JobID          string `json:"job_id"`
	CreatedUnix    int64  `json:"created_unix"`
	SizeBytes      int64  `json:"size_bytes"`
	PayloadBytes   int64  `json:"payload_bytes"`
	SHA256         string `json:"sha256"`
	SourceRevision string `json:"source_revision"`
	SchemaVersion  int64  `json:"schema_version"`
	Verification   string `json:"verification"`
	Protected      bool   `json:"protected"`
	Downloadable   bool   `json:"downloadable"`
}
type CreateRequest struct {
	EventID string `json:"event_id"`
	Action  string `json:"action"`
	ActorID string `json:"actor_id"`
}
type CancelRequest struct {
	EventID string `json:"event_id"`
	ActorID string `json:"actor_id"`
}
type SettingsRequest struct {
	EventID          string    `json:"event_id"`
	ActorID          string    `json:"actor_id"`
	ExpectedRevision uint64    `json:"expected_revision"`
	Schedule         Schedule  `json:"schedule"`
	Retention        Retention `json:"retention"`
}
type JobResponse struct {
	Job      Job  `json:"job"`
	Replayed bool `json:"replayed"`
}
type SettingsResponse struct {
	Settings Settings `json:"settings"`
	Replayed bool     `json:"replayed"`
}

// ArchiveDownload must be closed after the downstream writer is flushed, or
// immediately on abort. Reading EOF does not release its worker-side lease.
type ArchiveDownload struct {
	Archive       Archive
	Body          io.ReadCloser
	ContentLength int64
}
type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}
type HandlerOptions struct {
	RunnerStatus func() string
	OpenArchive  func(context.Context, Archive) (ReadSeekCloser, error)
	Uploads      UploadStore
}

func DefaultSettings() Settings {
	return Settings{Revision: 1, Schedule: Schedule{Frequency: "daily", Time: "03:00", Timezone: "Asia/Tehran", Weekday: 0}, Retention: Retention{KeepLast: 7, MaxAgeDays: 30}}
}
func DisabledStatus() Status {
	return Status{RunnerStatus: "disabled", Settings: DefaultSettings(), Jobs: []Job{}, Archives: []Archive{}}
}
func Terminal(state string) bool {
	return state == "succeeded" || state == "failed" || state == "cancelled"
}

// Store exports (implemented in the companion files):
// OpenStore(directory, publicRecipient string) (*Store, error)
// Status, CreateJob, GetJob, CancelJob, UpdateSettings, UpdateJob,
// RecoverableJobs, PutArchive, EnqueueScheduled, RetentionCandidates,
// PruneArchives, and OpenDownload. All take context.Context first.
// EnqueueScheduled and retention methods take an explicit time.Time.
