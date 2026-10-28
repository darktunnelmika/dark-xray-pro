package configstage

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
)

// ValidateOfflineBackup authenticates every retained stage, including rejected
// stages, and passes private plaintext only to the supplied dependency check.
// It does not open a Store or perform any filesystem operation.
func ValidateOfflineBackup(raw, key []byte, checkPayload func([]byte) error) error {
	if len(key) != 32 || len(raw) == 0 || len(raw) > maxFileBytes || checkPayload == nil {
		return ErrUnsafe
	}
	var current disk
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&current) != nil {
		return ErrUnsafe
	}
	canonical, err := json.Marshal(current)
	if err != nil || !bytes.Equal(raw, canonical) || current.Version != 1 || current.Stages == nil || len(current.Stages) > maxEntries {
		return ErrUnsafe
	}
	aead, err := makeAEAD(key)
	if err != nil {
		return ErrUnsafe
	}
	for id, entry := range current.Stages {
		m := entry.Meta
		if !hex32.MatchString(id) || id != m.OperationID || !validMeta(m) || len(entry.Nonce) != aead.NonceSize() || len(entry.Ciphertext) < aead.Overhead() || (m.Status == Pending && entry.Proof != nil) || (m.Status != Pending && entry.Proof == nil) {
			return ErrUnsafe
		}
		clear, err := aead.Open(nil, entry.Nonce, entry.Ciphertext, aad(m))
		if err != nil {
			return ErrUnsafe
		}
		digest := sha256.Sum256(clear)
		if subtle.ConstantTimeCompare(digest[:], mustHex(m.ConfigSHA256)) != 1 {
			return ErrUnsafe
		}
		if entry.Proof != nil && (!matches(m, *entry.Proof) || (m.Status == Verified && !entry.Proof.SyntaxValid) || (m.Status == Rejected && entry.Proof.SyntaxValid) || m.CanaryAttempted != entry.Proof.CanaryAttempted || m.CanaryLoopbackBound != entry.Proof.CanaryLoopbackBound) {
			return ErrUnsafe
		}
		if checkPayload(clear) != nil {
			return ErrUnsafe
		}
	}
	return nil
}
