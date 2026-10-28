package labrevision

import (
	"bytes"
	"encoding/json"
)

// ValidateOfflineBackup verifies the authenticated selection journal and every
// historical encrypted version without selecting a version or opening a Store.
func ValidateOfflineBackup(raw, key []byte, checkPayload func([]byte) error) error {
	if len(key) != 32 || len(raw) == 0 || len(raw) > maxStoredBytes || checkPayload == nil {
		return ErrUnsafe
	}
	var current disk
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&current) != nil {
		return ErrUnsafe
	}
	canonical, err := json.Marshal(current)
	if err != nil || !bytes.Equal(raw, canonical) || checkState(current, key) != nil {
		return ErrUnsafe
	}
	aead, err := aeadFor(key)
	if err != nil {
		return ErrUnsafe
	}
	for _, entry := range current.Versions {
		clear, err := aead.Open(nil, entry.Nonce, entry.Ciphertext, versionAAD(entry.Version))
		if err != nil || checkPayload(clear) != nil {
			return ErrUnsafe
		}
	}
	return nil
}
