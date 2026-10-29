// Package backupimport stores owner-supplied ciphertext in a separate recovery
// inbox. It never decrypts an archive, reads a recovery key, opens a database,
// stops a service, or promotes a file to the native backup history.
package backupimport

import (
	"errors"
	"io"
)

const (
	Directory              = "/var/lib/dark-xray-pro-backup-worker/uploads"
	MaxUploadBytes   int64 = 66 << 30
	MaxInboxBytes    int64 = 132 << 30
	MinFreeBytes     int64 = 256 << 20
	MaxEntries             = 4096
	MaxActiveEntries       = 32
)

var (
	ErrInvalid     = errors.New("invalid_backup_upload")
	ErrConflict    = errors.New("backup_upload_conflict")
	ErrBusy        = errors.New("backup_upload_busy")
	ErrCapacity    = errors.New("backup_upload_capacity")
	ErrNotFound    = errors.New("backup_upload_not_found")
	ErrUnsafe      = errors.New("unsafe_backup_upload_state")
	ErrUnavailable = errors.New("backup_upload_unavailable")
)

type Upload struct {
	ID            string `json:"id"`
	EventID       string `json:"event_id"`
	ActorID       string `json:"actor_id"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	State         string `json:"state"`
	CreatedUnix   int64  `json:"created_unix"`
	UpdatedUnix   int64  `json:"updated_unix"`
	ReceivedBytes int64  `json:"received_bytes"`
	Verification  string `json:"verification"`
	Downloadable  bool   `json:"downloadable"`
	ErrorCode     string `json:"error_code"`
	Attempts      int    `json:"attempts"`
}

type CreateRequest struct {
	EventID   string `json:"event_id"`
	ActorID   string `json:"actor_id"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type Response struct {
	Upload   Upload `json:"upload"`
	Replayed bool   `json:"replayed"`
}

type Status struct {
	Enabled          bool     `json:"enabled"`
	MaxUploadBytes   int64    `json:"max_upload_bytes"`
	MaxInboxBytes    int64    `json:"max_inbox_bytes"`
	ReservedBytes    int64    `json:"reserved_bytes"`
	MaxEntries       int      `json:"max_entries"`
	MaxActiveEntries int      `json:"max_active_entries"`
	Uploads          []Upload `json:"uploads"`
}

type ReadSeekCloser interface {
	io.Reader
	io.Seeker
	io.Closer
}
