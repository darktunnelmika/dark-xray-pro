//go:build linux

package managedxray

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const maxTransactionBytes = 24 << 20

type transactionDisk struct {
	version int
	dir     string
	aead    cipher.AEAD
	aad     []byte
}

func openTransactionDisk(o Options) (*transactionDisk, error) {
	if os.Geteuid() == 0 || !filepath.IsAbs(o.Directory) || filepath.Clean(o.Directory) != o.Directory || len(o.StateKey) != 32 {
		return nil, ErrPrivate
	}
	if err := privateAncestors(o.Directory); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(o.Directory, 0700); err != nil {
		return nil, err
	}
	if err := privateAncestors(o.Directory); err != nil {
		return nil, err
	}
	info, err := os.Lstat(o.Directory)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return nil, ErrPrivate
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Geteuid() {
		return nil, ErrPrivate
	}
	// A caller must choose a new executor root, not an ordinary native runtime
	// with usage that could otherwise be hidden by creating a child directory.
	for _, name := range []string{"runtime.enc", "runtime.lock", "usage"} {
		if _, err = os.Lstat(filepath.Join(o.Directory, name)); !errors.Is(err, os.ErrNotExist) {
			return nil, ErrPrivate
		}
	}
	m := hmac.New(sha256.New, o.StateKey)
	m.Write([]byte("DARK-PRO-ROUTE-TRANSACTION-KEY-v1"))
	b, err := aes.NewCipher(m.Sum(nil))
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(b)
	if err != nil {
		return nil, err
	}
	return &transactionDisk{version: 1, dir: o.Directory, aead: aead, aad: []byte("DARK-PRO-ROUTE-TRANSACTION-v1\x00" + o.NodeID + "\x00" + o.InstallationID + "\x00" + o.ListenIP + "\x00" + o.BinarySHA)}, nil
}

func (d *transactionDisk) load(key []byte) (transactionState, error) {
	empty := transactionState{Version: d.version}
	raw, err := privateRead(filepath.Join(d.dir, "transactions.enc"), maxTransactionBytes)
	if errors.Is(err, os.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	n := d.aead.NonceSize()
	if len(raw) < n+d.aead.Overhead() {
		return empty, ErrCorrupt
	}
	plain, err := d.aead.Open(nil, raw[:n], raw[n:], d.aad)
	if err != nil {
		return empty, ErrCorrupt
	}
	var s transactionState
	dec := json.NewDecoder(bytes.NewReader(plain))
	dec.DisallowUnknownFields()
	if dec.Decode(&s) != nil {
		return empty, ErrCorrupt
	}
	var trailing any
	if !errors.Is(dec.Decode(&trailing), io.EOF) || s.Version != d.version || s.Serial > maxTransactionSerial {
		return empty, ErrCorrupt
	}
	for _, stage := range []*transactionStage{s.Staged, s.Current, s.Previous} {
		if stage == nil {
			continue
		}
		if (d.version == 1 && stage.UsageBase != nil) || (d.version == 2 && (stage.UsageBase == nil || !validMeteredCondition(*stage.UsageBase))) {
			return empty, ErrCorrupt
		}
		if !transactionID.MatchString(stage.ID) || stage.Revision == nil || stage.Revision.Sequence == 0 ||
			stage.Revision.Sequence > maxTransactionSerial || stage.Expires <= 0 ||
			!transactionHash.MatchString(stage.PayloadSHA) || TransactionPayloadSHA(stage.Revision.candidate()) != stage.PayloadSHA {
			return empty, ErrCorrupt
		}
	}
	if (s.LastCommand == nil) != (s.LastReceipt == nil) {
		return empty, ErrCorrupt
	}
	if a := s.Health; a != nil {
		if d.version != 2 || !verifyProtocolHealthRequest(key, a.Request, a.Request.Issued) || ((a.Acknowledged || a.RejectedAfterExpiry) && a.Receipt == nil) || (a.Acknowledged && a.RejectedAfterExpiry) {
			return empty, ErrCorrupt
		}
		if a.Receipt != nil && !VerifyProtocolHealthReceipt(key, a.Request, *a.Receipt, time.Unix(0, a.Receipt.Completed)) {
			return empty, ErrCorrupt
		}
	}
	verify := VerifyTransactionReceipt
	if d.version == 2 {
		verify = VerifyMeteredTransactionReceipt
	}
	if s.LastCommand != nil && (s.LastCommand.Version != d.version || s.LastCommand.Serial > s.Serial || !verify(key, *s.LastCommand, *s.LastReceipt)) {
		return empty, ErrCorrupt
	}
	if s.Pending != nil {
		if s.Pending.Version != d.version || !validTransactionSignature(key, *s.Pending) || s.Pending.Serial != s.Serial || s.Pending.Action == "stage" {
			return empty, ErrCorrupt
		}
	} else if s.Serial != 0 && (s.LastCommand == nil || s.LastCommand.Serial != s.Serial) {
		return empty, ErrCorrupt
	}
	return s, nil
}

func (d *transactionDisk) save(s transactionState) error {
	raw, err := json.Marshal(s)
	if err != nil || len(raw) > maxTransactionBytes-64 {
		return ErrInvalid
	}
	nonce := make([]byte, d.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	raw = d.aead.Seal(nonce, nonce, raw, d.aad)
	f, err := os.CreateTemp(d.dir, ".transaction-")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(raw)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(d.dir, "transactions.enc")); err != nil {
		return err
	}
	parent, err := os.Open(d.dir)
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
