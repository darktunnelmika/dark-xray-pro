//go:build linux

package inboundruntime

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// OfflineJournal is private recovery metadata, not a browser response. Bundle
// configuration may contain credentials. Callers must hold the existing
// lifetime writer lock and independently prove every returned epoch stopped.
type OfflineJournal struct {
	Phase   string
	Epochs  []inboundexec.EpochInput
	Bundles []inboundexec.Bundle
}

// ValidateOfflineJournal authenticates the existing envelope and reuses the
// exact runtime schema validator without creating files, opening a Worker,
// changing phase, acquiring a lease or contacting any node.
func ValidateOfflineJournal(path string, key []byte, node, installation string) (OfflineJournal, error) {
	var result OfflineJournal
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(key) < 32 || node == "" || len(installation) != 32 {
		return result, ErrRuntime
	}
	for p := path; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e != nil || st.Mode()&os.ModeSymlink != 0 {
			return result, ErrRuntime
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	fd, e := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return result, ErrRuntime
	}
	f := os.NewFile(uintptr(fd), "offline-runtime-journal")
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 64<<20 {
		return result, ErrRuntime
	}
	identity, ok := st.Sys().(*syscall.Stat_t)
	if !ok || identity.Uid != uint32(os.Geteuid()) || identity.Nlink != 1 {
		return result, ErrRuntime
	}
	raw, e := io.ReadAll(io.LimitReader(f, 64<<20+1))
	if e != nil || len(raw) > 64<<20 {
		return result, ErrRuntime
	}
	digest := sha256.Sum256(append([]byte("DARK-PRO-MANAGED-INBOUND-JOURNAL-v1\x00"), key...))
	block, e := aes.NewCipher(digest[:])
	if e != nil {
		return result, ErrRuntime
	}
	aead, e := cipher.NewGCM(block)
	if e != nil || len(raw) < aead.NonceSize()+aead.Overhead() {
		return result, ErrRuntime
	}
	clear, e := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte("managed-inbound-v1\x00"+node+"\x00"+installation))
	if e != nil {
		return result, ErrRuntime
	}
	d := json.NewDecoder(bytes.NewReader(clear))
	d.DisallowUnknownFields()
	var state journal
	var extra any
	if d.Decode(&state) != nil || d.Decode(&extra) != io.EOF || state.Version != 1 || state.NodeID != node || state.InstallationID != installation {
		return result, ErrRuntime
	}
	canonical, e := json.Marshal(state)
	if e != nil || !bytes.Equal(canonical, clear) || validateJournal(key, state) != nil {
		return result, ErrRuntime
	}
	result.Phase = state.Phase
	if state.Epoch != nil {
		result.Epochs = append(result.Epochs, *state.Epoch)
	}
	if state.Bundle != nil {
		result.Bundles = append(result.Bundles, *state.Bundle)
	}
	if state.Resolution != nil {
		result.Epochs = append(result.Epochs, state.Resolution.Epoch)
		result.Bundles = append(result.Bundles, state.Resolution.Bundle)
	}
	return result, nil
}
