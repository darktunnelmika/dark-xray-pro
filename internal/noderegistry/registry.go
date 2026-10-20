// Package noderegistry durably binds NEW lab Node IDs to unique installations.
// This registry never imports legacy HUB credentials, lease state or tunnels.
package noderegistry

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"
)

var (
	ErrInvalid     = errors.New("invalid node enrollment")
	ErrExists      = errors.New("node already paired")
	ErrPending     = errors.New("node already has an unexpired pairing invite")
	ErrInvite      = errors.New("pairing code expired, unknown, or already consumed")
	ErrTooMany     = errors.New("too many pending pairings")
	ErrUnsafeState = errors.New("unsafe registry path or permissions")
	idPattern      = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)
	hex32Pattern   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	hex64Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

const maxRegistryBytes = 4 * 1024 * 1024
const maxPending = 128
const pairingTTL = 10 * time.Minute

type NodeInfo struct {
	NodeID         string    `json:"node_id"`
	Name           string    `json:"name"`
	InstallationID string    `json:"installation_id"`
	PairedAt       time.Time `json:"paired_at"`
}

// PairTicket is a one-time secret: never retain or display it in a list endpoint.
type PairTicket struct {
	NodeID    string    `json:"node_id"`
	Name      string    `json:"name"`
	PairCode  string    `json:"pair_code"`
	ExpiresAt time.Time `json:"expires_at"`
}

type invitation struct {
	NodeID    string    `json:"node_id"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}

type binding struct {
	Info      NodeInfo `json:"info"`
	TokenHash string   `json:"token_hash"`
}

type receipt struct {
	NodeID         string `json:"node_id"`
	InstallationID string `json:"installation_id"`
	TokenHash      string `json:"token_hash"`
}

type diskState struct {
	Version  int                    `json:"version"`
	Invites  map[string]invitation  `json:"invites"`
	Bindings map[string]binding     `json:"bindings"`
	Receipts map[string]receipt     `json:"receipts"`
	Renames  map[string]NameReceipt `json:"renames,omitempty"`
}

type Registry struct {
	mu    sync.RWMutex
	path  string
	now   func() time.Time
	state diskState
}

func empty() diskState {
	return diskState{
		Version:  1,
		Invites:  map[string]invitation{},
		Bindings: map[string]binding{},
		Receipts: map[string]receipt{},
		Renames:  map[string]NameReceipt{},
	}
}

func validName(s string) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > 80 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func tokenDigest(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func clone(s diskState) diskState {
	d := empty()
	for k, v := range s.Invites {
		d.Invites[k] = v
	}
	for k, v := range s.Bindings {
		d.Bindings[k] = v
	}
	for k, v := range s.Receipts {
		d.Receipts[k] = v
	}
	for k, v := range s.Renames {
		d.Renames[k] = v
	}
	return d
}

// Open enforces a private directory and a 0600 on-disk registry. It does NOT
// repair permissions silently or generate Node credentials on behalf of agents.
func Open(path string, clock func() time.Time) (*Registry, error) {
	if path == "" {
		return nil, ErrUnsafeState
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	parent := filepath.Dir(abs)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrUnsafeState
	}
	if clock == nil {
		clock = time.Now
	}
	r := &Registry{path: abs, now: clock, state: empty()}
	fileInfo, err := os.Lstat(abs)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm()&0077 != 0 || fileInfo.Size() > maxRegistryBytes {
		return nil, ErrUnsafeState
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var state diskState
	if err := decoder.Decode(&state); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, ErrUnsafeState
	}
	if state.Version != 1 || state.Invites == nil || state.Bindings == nil || state.Receipts == nil ||
		len(state.Invites) > maxPending || len(state.Bindings) > 10000 || len(state.Receipts) > 10000 {
		return nil, ErrUnsafeState
	}
	for id, b := range state.Bindings {
		if !idPattern.MatchString(id) || b.Info.NodeID != id || !hex32Pattern.MatchString(b.Info.InstallationID) ||
			!validName(b.Info.Name) || !hex64Pattern.MatchString(b.TokenHash) {
			return nil, ErrUnsafeState
		}
	}
	if err := validateRenames(state); err != nil {
		return nil, err
	}
	r.state = state
	return r, nil
}

// saveLocked commits to disk BEFORE exposing a new identity or issuing a code.
// No plaintext pairing code or agent token is written to the HUB filesystem.
func (r *Registry) saveLocked(next diskState) error {
	encoded, err := json.Marshal(next)
	if err != nil {
		return err
	}
	if len(encoded) > maxRegistryBytes {
		return ErrTooMany
	}
	dir := filepath.Dir(r.path)
	fd, err := os.CreateTemp(dir, ".node-registry-")
	if err != nil {
		return err
	}
	tmp := fd.Name()
	defer os.Remove(tmp)
	if err := fd.Chmod(0600); err != nil {
		fd.Close()
		return err
	}
	if _, err := fd.Write(encoded); err != nil {
		fd.Close()
		return err
	}
	if err := fd.Sync(); err != nil {
		fd.Close()
		return err
	}
	if err := fd.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, r.path); err != nil {
		return err
	}
	// The rename has committed, even if directory sync subsequently fails.
	// Keep memory consistent with the visible file so a retry cannot overwrite
	// a committed receipt from a stale in-memory state.
	r.state = next
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("registry persisted but directory sync failed: %w", err)
	}
	return nil
}

// Issue reserves a NEW logical Node ID, without creating an active Node or
// inheriting any previous tunnel, route, lease, inbound or legacy credentials.
func (r *Registry) Issue(id, name string) (PairTicket, error) {
	if !idPattern.MatchString(id) || !validName(name) {
		return PairTicket{}, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now().UTC()
	if _, exists := r.state.Bindings[id]; exists {
		return PairTicket{}, ErrExists
	}
	next := clone(r.state)
	for digest, invite := range next.Invites {
		if !now.Before(invite.ExpiresAt) {
			delete(next.Invites, digest)
			continue
		}
		if invite.NodeID == id {
			return PairTicket{}, ErrPending
		}
	}
	if len(next.Invites) >= maxPending {
		return PairTicket{}, ErrTooMany
	}
	code, err := randomCode()
	if err != nil {
		return PairTicket{}, err
	}
	expiry := now.Add(pairingTTL)
	next.Invites[tokenDigest(code)] = invitation{NodeID: id, Name: name, ExpiresAt: expiry}
	if err := r.saveLocked(next); err != nil {
		return PairTicket{}, err
	}
	return PairTicket{NodeID: id, Name: name, PairCode: code, ExpiresAt: expiry}, nil
}

// Claim binds the self-generated agent token to exactly one installation.
// Repeating the identical claim is idempotent after a lost response.
// Reusing the code with ANY different token or installation is refused.
func (r *Registry) Claim(code, id, installationID, agentToken string) (NodeInfo, error) {
	if !hex64Pattern.MatchString(code) || !idPattern.MatchString(id) ||
		!hex32Pattern.MatchString(installationID) || !hex64Pattern.MatchString(agentToken) {
		return NodeInfo{}, ErrInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key, digest := tokenDigest(code), tokenDigest(agentToken)
	if receipt, ok := r.state.Receipts[key]; ok {
		if receipt.NodeID == id && receipt.InstallationID == installationID && receipt.TokenHash == digest {
			binding, active := r.state.Bindings[id]
			if active && binding.Info.InstallationID == installationID && binding.TokenHash == digest {
				return binding.Info, nil
			}
		}
		return NodeInfo{}, ErrInvite
	}
	ticket, ok := r.state.Invites[key]
	if !ok || ticket.NodeID != id || !r.now().UTC().Before(ticket.ExpiresAt) {
		return NodeInfo{}, ErrInvite
	}
	if _, exists := r.state.Bindings[id]; exists {
		return NodeInfo{}, ErrExists
	}
	for _, b := range r.state.Bindings {
		if b.Info.InstallationID == installationID {
			return NodeInfo{}, ErrExists
		}
	}
	next := clone(r.state)
	delete(next.Invites, key)
	info := NodeInfo{NodeID: id, Name: ticket.Name, InstallationID: installationID, PairedAt: r.now().UTC()}
	next.Bindings[id] = binding{Info: info, TokenHash: digest}
	next.Receipts[key] = receipt{NodeID: id, InstallationID: installationID, TokenHash: digest}
	if err := r.saveLocked(next); err != nil {
		return NodeInfo{}, err
	}
	return info, nil
}

// Authenticate uses a pinned installation and constant-time SHA comparison.
// No shared owner secret can impersonate a paired Node.
func (r *Registry) Authenticate(id, installationID, token string) (NodeInfo, bool) {
	if !idPattern.MatchString(id) || !hex32Pattern.MatchString(installationID) || !hex64Pattern.MatchString(token) {
		return NodeInfo{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.state.Bindings[id]
	if !ok || b.Info.InstallationID != installationID {
		return NodeInfo{}, false
	}
	expected, err := hex.DecodeString(b.TokenHash)
	if err != nil {
		return NodeInfo{}, false
	}
	got := sha256.Sum256([]byte(token))
	if subtle.ConstantTimeCompare(expected, got[:]) != 1 {
		return NodeInfo{}, false
	}
	return b.Info, true
}

func (r *Registry) List() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]NodeInfo, 0, len(r.state.Bindings))
	for _, b := range r.state.Bindings {
		result = append(result, b.Info)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].NodeID < result[j].NodeID })
	return result
}
