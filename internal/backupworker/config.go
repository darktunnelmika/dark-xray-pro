// Package backupworker coordinates one fixed, isolated LAB service. It is not a
// general command runner, migration service, or restored-service activator.
package backupworker

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"filippo.io/age"
	"github.com/darktunnelmika/dark-xray-pro/internal/backupcontrol"
)

const (
	ConfigPath        = "/etc/dark-xray-pro-backup-worker/config.json"
	StateDirectory    = "/var/lib/dark-xray-pro-backup-worker"
	ArchiveDirectory  = "/var/backups/dark-xray-pro-native"
	UnitName          = "dark-xray-pro-lab.service"
	HubStateDirectory = "/var/lib/dark-xray-pro-lab"
	ReleaseDirectory  = "/opt/dark-xray-pro-lab/releases"
	LABIPv4           = "80.253.255.181"
)

var (
	ErrUnsafe             = errors.New("backup_worker_unsafe_state")
	ErrPreflight          = errors.New("backup_preflight_refused")
	ErrStop               = errors.New("hub_stop_unconfirmed")
	ErrResume             = errors.New("hub_resume_unconfirmed")
	ErrCreate             = errors.New("backup_creation_failed")
	ErrUnknownPublication = errors.New("backup_publication_unknown")
	ErrReceipt            = errors.New("backup_receipt_invalid")
	idPattern             = regexp.MustCompile(`^[a-f0-9]{32}$`)
	hex40                 = regexp.MustCompile(`^[a-f0-9]{40}$`)
	hex64                 = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

// Only the root-owned configuration selects executable identities and limits.
// No field accepts a service name, command, arbitrary destination or private key.
type Config struct {
	Version              int    `json:"version"`
	HubBinary            string `json:"hub_binary"`
	HubSHA256            string `json:"hub_sha256"`
	HubRevision          string `json:"hub_revision"`
	BackupBinary         string `json:"backup_binary"`
	BackupSHA256         string `json:"backup_sha256"`
	BackupRevision       string `json:"backup_revision"`
	PublicRecipient      string `json:"public_recipient"`
	MaxPayloadBytes      int64  `json:"max_payload_bytes"`
	BackupTimeoutSeconds int    `json:"backup_timeout_seconds"`
	StopTimeoutSeconds   int    `json:"stop_timeout_seconds"`
	ResumeTimeoutSeconds int    `json:"resume_timeout_seconds"`
}

func (c Config) ciphertextLimit() int64 {
	return min(c.MaxPayloadBytes+c.MaxPayloadBytes/16+16<<20, backupcontrol.MaxArchiveBytes)
}

func (c Config) Validate() error {
	if c.Version != 1 || !hex40.MatchString(c.HubRevision) || !hex40.MatchString(c.BackupRevision) ||
		!hex64.MatchString(c.HubSHA256) || !hex64.MatchString(c.BackupSHA256) {
		return ErrUnsafe
	}
	for _, b := range []struct{ path, name string }{{c.HubBinary, "dark-pro-hub"}, {c.BackupBinary, "dark-pro-backup"}} {
		if !filepath.IsAbs(b.path) || filepath.Clean(b.path) != b.path || filepath.Base(b.path) != b.name ||
			filepath.Dir(filepath.Dir(b.path)) != ReleaseDirectory || !hex40.MatchString(filepath.Base(filepath.Dir(b.path))) {
			return ErrUnsafe
		}
	}
	r, e := age.ParseX25519Recipient(c.PublicRecipient)
	if e != nil || r.String() != c.PublicRecipient {
		return ErrUnsafe
	}
	if c.MaxPayloadBytes < 1<<20 || c.MaxPayloadBytes > 64<<30 ||
		c.BackupTimeoutSeconds < 10 || c.BackupTimeoutSeconds > 3600 ||
		c.StopTimeoutSeconds < 5 || c.StopTimeoutSeconds > 120 ||
		c.ResumeTimeoutSeconds < 5 || c.ResumeTimeoutSeconds > 180 {
		return ErrUnsafe
	}
	return nil
}

func LoadConfig() (Config, error) {
	var c Config
	if trustedAncestors(ConfigPath) != nil {
		return c, ErrUnsafe
	}
	f, e := openChecked(ConfigPath, 0, true, 64<<10)
	if e != nil {
		return c, ErrUnsafe
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 64<<10))
	if e != nil || strictJSON(raw, &c) != nil {
		return c, ErrUnsafe
	}
	return c, c.Validate()
}

func validateBinary(path, hash, revision string) error {
	if trustedAncestors(path) != nil {
		return ErrUnsafe
	}
	f, e := openChecked(path, 0, false, 256<<20)
	if e != nil {
		return ErrUnsafe
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || st.Mode().Perm()&0111 == 0 {
		return ErrUnsafe
	}
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil || hex.EncodeToString(h.Sum(nil)) != hash {
		return ErrUnsafe
	}
	info, e := buildinfo.Read(f)
	if e != nil {
		return ErrUnsafe
	}
	var got, modified string
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			got = s.Value
		}
		if s.Key == "vcs.modified" {
			modified = s.Value
		}
	}
	current, e := os.Lstat(path)
	if e != nil || !os.SameFile(st, current) || got != revision || modified != "false" {
		return ErrUnsafe
	}
	return nil
}

func strictJSON(raw []byte, out any) error {
	// encoding/json normally accepts duplicate object keys and keeps the last
	// value. Receipts and privileged configuration must have one interpretation.
	tokens := json.NewDecoder(bytes.NewReader(raw))
	tokens.UseNumber()
	if jsonValue(tokens, 0) != nil {
		return ErrUnsafe
	}
	if _, e := tokens.Token(); e != io.EOF {
		return ErrUnsafe
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(out) != nil || d.Decode(&extra) != io.EOF {
		return ErrUnsafe
	}
	return nil
}

func jsonValue(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrUnsafe
	}
	t, e := d.Token()
	if e != nil || t == nil {
		return ErrUnsafe
	}
	delim, container := t.(json.Delim)
	if !container {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, e := d.Token()
			k, ok := key.(string)
			if e != nil || !ok || seen[k] {
				return ErrUnsafe
			}
			seen[k] = true
			if jsonValue(d, depth+1) != nil {
				return ErrUnsafe
			}
		}
	case '[':
		for d.More() {
			if jsonValue(d, depth+1) != nil {
				return ErrUnsafe
			}
		}
	default:
		return ErrUnsafe
	}
	end, e := d.Token()
	if e != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return ErrUnsafe
	}
	return nil
}

func seconds(n int) time.Duration { return time.Duration(n) * time.Second }
