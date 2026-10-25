//go:build linux

package inboundruntime

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

type journal struct {
	Version        int                      `json:"version"`
	NodeID         string                   `json:"node_id"`
	InstallationID string                   `json:"installation_id"`
	Generation     uint64                   `json:"generation"`
	Bundle         *inboundexec.Bundle      `json:"bundle,omitempty"`
	Epoch          *inboundexec.EpochInput  `json:"epoch,omitempty"`
	Phase          string                   `json:"phase"`
	Sequence       uint64                   `json:"sequence"`
	Pending        *inboundexec.UsageReport `json:"pending,omitempty"`
	LastCounters   []inboundexec.Counter    `json:"last_counters"`
	ActivateNonce  string                   `json:"activate_nonce,omitempty"`
	DrainNonce     string                   `json:"drain_nonce,omitempty"`
	FinalAccepted  bool                     `json:"final_accepted"`
	Resolution     *journalResolution       `json:"resolution,omitempty"`
	PublicAliases  *publicAliasJournal      `json:"public_aliases,omitempty"`
}

type journalResolution struct {
	Epoch     inboundexec.EpochInput     `json:"epoch"`
	Clearance inboundexec.EpochClearance `json:"clearance"`
	Bundle    inboundexec.Bundle         `json:"bundle"`
}

type privateJournal struct {
	dir  string
	lock *os.File
	aead cipher.AEAD
	aad  []byte
}

func openJournal(dir string, key []byte, node, installation string) (*privateJournal, journal, error) {
	empty := journal{Version: 1, NodeID: node, InstallationID: installation, Phase: "idle"}
	if !filepath.IsAbs(dir) || len(key) < 32 || node == "" || len(installation) != 32 {
		return nil, empty, errors.New("invalid_inbound_runtime_directory")
	}
	// Reject symlink ancestors before creating any private state below them.
	// The final directory being private is insufficient if a configurable
	// ancestor silently redirects the journal to another installation.
	for ancestor := filepath.Clean(dir); ; ancestor = filepath.Dir(ancestor) {
		st, err := os.Lstat(ancestor)
		if err == nil && st.Mode()&os.ModeSymlink != 0 || err != nil && !os.IsNotExist(err) {
			return nil, empty, errors.New("inbound_runtime_directory_symlink")
		}
		if ancestor == filepath.Dir(ancestor) {
			break
		}
	}
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, empty, e
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0 {
		return nil, empty, errors.New("inbound_runtime_directory_must_be_private")
	}
	fd, e := syscall.Open(filepath.Join(dir, "writer.lock"), syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if e != nil {
		return nil, empty, e
	}
	lock := os.NewFile(uintptr(fd), "writer.lock")
	if e = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		_ = lock.Close()
		return nil, empty, errors.New("inbound_runtime_already_owned")
	}
	material := append([]byte("DARK-PRO-MANAGED-INBOUND-JOURNAL-v1\x00"), key...)
	digest := sha256.Sum256(material)
	block, e := aes.NewCipher(digest[:])
	if e != nil {
		_ = lock.Close()
		return nil, empty, e
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		_ = lock.Close()
		return nil, empty, e
	}
	j := &privateJournal{dir: dir, lock: lock, aead: aead, aad: []byte("managed-inbound-v1\x00" + node + "\x00" + installation)}
	file, e := os.OpenFile(filepath.Join(dir, "state.enc"), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if os.IsNotExist(e) {
		if e = j.save(empty); e != nil {
			j.close()
			return nil, empty, e
		}
		return j, empty, nil
	}
	if e != nil {
		j.close()
		return nil, empty, e
	}
	defer file.Close()
	st, e = file.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 {
		j.close()
		return nil, empty, errors.New("inbound_journal_not_private")
	}
	raw, e := io.ReadAll(io.LimitReader(file, 64<<20+1))
	if e != nil || len(raw) > 64<<20 || len(raw) < aead.NonceSize()+aead.Overhead() {
		j.close()
		return nil, empty, errors.New("inbound_journal_corrupt")
	}
	clear, e := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], j.aad)
	if e != nil {
		j.close()
		return nil, empty, errors.New("inbound_journal_authentication_failed")
	}
	var state journal
	d := json.NewDecoder(bytes.NewReader(clear))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&state) != nil || d.Decode(&extra) != io.EOF || state.Version != 1 || state.NodeID != node || state.InstallationID != installation {
		j.close()
		return nil, empty, errors.New("inbound_journal_identity_mismatch")
	}
	if err := validateJournal(key, state); err != nil {
		j.close()
		return nil, empty, err
	}
	return j, state, nil
}

func validateJournal(key []byte, s journal) error {
	bad := errors.New("inbound_journal_state_mismatch")
	if validatePublicAliasJournal(s) != nil {
		return bad
	}
	switch s.Phase {
	case "idle", "preparing", "prepared", "registered", "zero_pending", "activation_intent", "active", "draining", "sealed_waiting_receipt", "final_pending", "final_accepted", "uncertain":
	default:
		return bad
	}
	if s.Bundle == nil {
		if s.Generation != 0 || s.Epoch != nil || s.Pending != nil || s.Phase != "idle" || s.Sequence != 0 || s.FinalAccepted || len(s.LastCounters) != 0 || s.ActivateNonce != "" || s.DrainNonce != "" || s.Resolution != nil {
			return bad
		}
		return nil
	}
	b := s.Bundle
	if inboundexec.VerifyBundle(key, *b) != nil || b.NodeID != s.NodeID || b.InstallationID != s.InstallationID || b.Generation != s.Generation {
		return bad
	}
	if r := s.Resolution; r != nil {
		c := r.Clearance
		if inboundexec.VerifyClearance(key, c) != nil || c.NodeID != s.NodeID || c.InstallationID != s.InstallationID || r.Epoch.NodeID != s.NodeID || r.Epoch.InstallationID != s.InstallationID || c.Generation != r.Epoch.Generation || c.EpochID != r.Epoch.EpochID || c.EpochSHA != inboundexec.Digest(r.Epoch) || inboundexec.VerifyBundle(key, r.Bundle) != nil || inboundexec.ValidateEpochIdentity(r.Bundle, r.Epoch) != nil || s.Epoch != nil || s.Phase != "idle" {
			return bad
		}
	}
	if s.Epoch == nil {
		if s.Pending != nil || s.Phase != "idle" && s.Phase != "preparing" {
			return bad
		}
		return nil
	}
	e := s.Epoch
	if inboundexec.ValidateEpochIdentity(*b, *e) != nil {
		return bad
	}
	if e.EpochID == "" || e.NodeID != b.NodeID || e.InstallationID != b.InstallationID || e.Generation != b.Generation || e.ConfigSHA != b.ConfigSHA || e.ManifestSHA != b.ManifestSHA || e.ProcessIdentity == "" || len(e.BinarySHAs) == 0 || !validNonce(s.ActivateNonce) || s.Phase == "idle" || s.Phase == "preparing" {
		return bad
	}
	validCounters := func(values []inboundexec.Counter) bool {
		if len(values) != len(b.Principals) {
			return false
		}
		for i, c := range values {
			if c.PrincipalID != b.Principals[i].ID || i > 0 && values[i-1].PrincipalID >= c.PrincipalID {
				return false
			}
		}
		return true
	}
	if len(s.LastCounters) != 0 && !validCounters(s.LastCounters) || s.Sequence > 0 && !validCounters(s.LastCounters) || s.DrainNonce != "" && !validNonce(s.DrainNonce) {
		return bad
	}
	if s.FinalAccepted != (s.Phase == "final_accepted") {
		return bad
	}
	if r := s.Pending; r != nil {
		if r.EpochID != e.EpochID || r.NodeID != e.NodeID || r.InstallationID != e.InstallationID || r.Generation != e.Generation || r.ProcessIdentity != e.ProcessIdentity || r.Sequence == 0 || r.Sequence != s.Sequence || !validCounters(r.Counters) || s.FinalAccepted {
			return bad
		}
		if r.InitialEpoch != nil {
			if !r.Final || r.Sequence != 1 || inboundexec.Digest(*r.InitialEpoch) != inboundexec.Digest(*e) {
				return bad
			}
			for _, c := range r.Counters {
				if c.Upload != 0 || c.Download != 0 {
					return bad
				}
			}
		}
		if r.Final {
			if r.FinalNonce != s.DrainNonce || !validNonce(r.FinalNonce) || r.FinalProofSHA != FinalProofSHA(r.ProcessIdentity, r.FinalNonce, r.Counters) {
				return bad
			}
		} else if r.FinalNonce != "" || r.FinalProofSHA != "" {
			return bad
		}
		for i, c := range r.Counters {
			if s.LastCounters[i].Upload < c.Upload || s.LastCounters[i].Download < c.Download {
				return bad
			}
		}
	}
	if s.Phase == "sealed_waiting_receipt" && (s.DrainNonce == "" || !validCounters(s.LastCounters)) {
		return bad
	}
	return nil
}
func (j *privateJournal) save(state journal) error {
	raw, e := json.Marshal(state)
	if e != nil || len(raw) > 64<<20 {
		return errors.New("inbound_journal_too_large")
	}
	nonce := make([]byte, j.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return e
	}
	sealed := j.aead.Seal(nonce, nonce, raw, j.aad)
	f, e := os.CreateTemp(j.dir, "state-tmp-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	if _, e = f.Write(sealed); e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(j.dir, "state.enc")); e != nil {
		return e
	}
	d, e := os.Open(j.dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}
func (j *privateJournal) close() {
	if j != nil && j.lock != nil {
		_ = syscall.Flock(int(j.lock.Fd()), syscall.LOCK_UN)
		_ = j.lock.Close()
	}
}
