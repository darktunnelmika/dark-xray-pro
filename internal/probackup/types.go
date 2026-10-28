// Package probackup implements the bounded, encrypted DARK PRO backup container.
// It does not collect live state, manage keys, or activate restored services.
package probackup

import (
	"context"
	"errors"
	"io"
	"strings"
)

const Version = 1

// QuarantineName is reserved for Restore. Application startup must refuse a
// restored tree containing this marker until a separate recovery procedure.
const QuarantineName = "restore-quarantine.json"

type Role string

const (
	RoleBusinessDB         Role = "business"
	RoleRegistry           Role = "registry"
	RoleReplacements       Role = "replacements"
	RoleConfigStage        Role = "config_stage"
	RoleLabRevisions       Role = "lab_revisions"
	RoleNodeHistoryDB      Role = "node_history"
	RoleOwnerReportDB      Role = "owner_report_db"
	RoleOwnerReportConfig  Role = "owner_report_config"
	RoleOwnerReportToken   Role = "owner_report_token"
	RoleOwnerReportRuntime Role = "owner_report_runtime"
	RoleInboundRuntime     Role = "inbound_runtime"
	RoleRuntimeConfig      Role = "runtime_config"
	RoleAgentTLSCert       Role = "agent_tls_cert"
	RoleAgentTLSKey        Role = "agent_tls_key"
	RoleInventory          Role = "inventory"
)

// This ordering is part of container version 1, not a Go declaration ordinal.
var wireRoles = [...]Role{"", RoleBusinessDB, RoleRegistry, RoleReplacements,
	RoleConfigStage, RoleLabRevisions, RoleNodeHistoryDB, RoleOwnerReportDB,
	RoleOwnerReportConfig, RoleOwnerReportToken, RoleOwnerReportRuntime,
	RoleInboundRuntime, RoleRuntimeConfig, RoleAgentTLSCert, RoleAgentTLSKey, RoleInventory}

type FileSource struct {
	Name string
	Role Role
	Size int64
	// Open must return an immutable, finite snapshot, not a changing live file.
	// The codec closes it, verifies exactly Size bytes, and never seeks.
	Open func(context.Context) (io.ReadCloser, error)
}

type FileEntry struct {
	Name   string `json:"name"`
	Role   Role   `json:"role"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Metadata struct {
	BackupID              string         `json:"backup_id"`
	CreatedUnix           int64          `json:"created_unix"`
	SourceRevision        string         `json:"source_revision"`
	BusinessSchemaVersion int64          `json:"business_schema_version"`
	FeatureVersions       map[string]int `json:"feature_versions,omitempty"`
}

type Manifest struct {
	Version  int         `json:"version"`
	Metadata Metadata    `json:"metadata"`
	Files    []FileEntry `json:"files"`
}

// Limits apply to bytes actually processed, not only declared file sizes.
// Plaintext counts the entire uncompressed container including its framing.
// A zero field selects its default; negative or excessive values are rejected.
type Limits struct {
	MaxCiphertextBytes int64
	MaxPlaintextBytes  int64
	MaxFileBytes       int64
	MaxFiles           int
	MaxManifestBytes   int
}

func DefaultLimits() Limits {
	return Limits{MaxCiphertextBytes: 66 << 30, MaxPlaintextBytes: 64 << 30,
		MaxFileBytes: 64 << 30, MaxFiles: 4096, MaxManifestBytes: 1 << 20}
}

var (
	ErrOptions              = errors.New("backup_options_invalid")
	ErrRecipient            = errors.New("backup_recipient_invalid")
	ErrIdentity             = errors.New("backup_identity_invalid")
	ErrSource               = errors.New("backup_snapshot_source_failed")
	ErrIO                   = errors.New("backup_io_failed")
	ErrFormat               = errors.New("backup_container_invalid")
	ErrIntegrity            = errors.New("backup_authentication_or_integrity_failed")
	ErrLimit                = errors.New("backup_limit_exceeded")
	ErrDestination          = errors.New("backup_destination_must_be_absent_and_private")
	ErrValidation           = errors.New("backup_restore_validation_failed")
	ErrPublicationUncertain = errors.New("backup_publication_durability_unconfirmed")
	ErrUnsupported          = errors.New("backup_atomic_restore_unsupported")
)

func (l Limits) resolve() (Limits, error) {
	d := DefaultLimits()
	if l.MaxCiphertextBytes == 0 {
		l.MaxCiphertextBytes = d.MaxCiphertextBytes
	}
	if l.MaxPlaintextBytes == 0 {
		l.MaxPlaintextBytes = d.MaxPlaintextBytes
	}
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = d.MaxFileBytes
	}
	if l.MaxFiles == 0 {
		l.MaxFiles = d.MaxFiles
	}
	if l.MaxManifestBytes == 0 {
		l.MaxManifestBytes = d.MaxManifestBytes
	}
	if l.MaxCiphertextBytes < 1 || l.MaxCiphertextBytes > 2<<40 ||
		l.MaxPlaintextBytes < 1 || l.MaxPlaintextBytes > 1<<40 ||
		l.MaxFileBytes < 1 || l.MaxFileBytes > 1<<40 ||
		l.MaxFiles < 1 || l.MaxFiles > 65536 || l.MaxManifestBytes < 1 || l.MaxManifestBytes > 16<<20 {
		return Limits{}, ErrOptions
	}
	return l, nil
}

func roleByte(role Role) (byte, bool) {
	for i := 1; i < len(wireRoles); i++ {
		if wireRoles[i] == role {
			return byte(i), true
		}
	}
	return 0, false
}

func safePart(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func safeName(name string) bool {
	if len(name) == 0 || len(name) > 240 || name == QuarantineName || strings.HasPrefix(name, QuarantineName+"/") {
		return false
	}
	parts := strings.Split(name, "/")
	if len(parts) > 8 {
		return false
	}
	for _, p := range parts {
		if !safePart(p) {
			return false
		}
	}
	return true
}

func addOrderedName(name, previous string, seen map[string]bool) bool {
	if !safeName(name) || name <= previous || seen[name] {
		return false
	}
	for at := strings.LastIndexByte(name, '/'); at >= 0; at = strings.LastIndexByte(name[:at], '/') {
		if seen[name[:at]] {
			return false
		}
	}
	seen[name] = true
	return true
}

func hexString(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for _, c := range []byte(s) {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func validMetadata(m Metadata) bool {
	if len(m.BackupID) > 128 || !safePart(m.BackupID) || m.CreatedUnix <= 0 ||
		!hexString(m.SourceRevision, 40) && !hexString(m.SourceRevision, 64) ||
		m.BusinessSchemaVersion < 0 || len(m.FeatureVersions) > 64 {
		return false
	}
	for k, v := range m.FeatureVersions {
		if len(k) > 64 || !safePart(k) || v < 0 {
			return false
		}
	}
	return true
}

func cloneManifest(m Manifest) Manifest {
	m.Files = append([]FileEntry(nil), m.Files...)
	if m.Metadata.FeatureVersions != nil {
		features := make(map[string]int, len(m.Metadata.FeatureVersions))
		for k, v := range m.Metadata.FeatureVersions {
			features[k] = v
		}
		m.Metadata.FeatureVersions = features
	}
	return m
}
