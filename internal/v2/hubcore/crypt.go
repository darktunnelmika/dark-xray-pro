package hubcore

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/scrypt"
)

// Encrypted backups: "DKB1" | salt(16) | nonce(12) | AES-256-GCM(gzip snapshot),
// key = scrypt(password, salt, N=2^15, r=8, p=1). The password is the owner's;
// without it the file is useless, so a backup sent to Telegram is safe to keep.

var backupMagic = []byte("DKB1")

// ErrBadPassword means an encrypted backup could not be opened.
var ErrBadPassword = errors.New("wrong backup password or damaged file")

func backupKey(pass string, salt []byte) ([]byte, error) {
	return scrypt.Key([]byte(pass), salt, 1<<15, 8, 1, 32)
}

// EncryptBackup seals a backup with a password.
func EncryptBackup(plain []byte, pass string) ([]byte, error) {
	if len(pass) < 8 {
		return nil, fmt.Errorf("%w: backup password needs 8+ characters", ErrInvalid)
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key, err := backupKey(pass, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	out := append(append(append([]byte{}, backupMagic...), salt...), nonce...)
	return gcm.Seal(out, nonce, plain, backupMagic), nil
}

// IsEncryptedBackup reports whether data starts like an encrypted backup.
func IsEncryptedBackup(data []byte) bool { return bytes.HasPrefix(data, backupMagic) }

// DecryptBackup opens an encrypted backup.
func DecryptBackup(data []byte, pass string) ([]byte, error) {
	if !IsEncryptedBackup(data) || len(data) < 4+16+12+16 {
		return nil, ErrBadPassword
	}
	salt, nonce, body := data[4:20], data[20:32], data[32:]
	key, err := backupKey(pass, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, nonce, body, backupMagic)
	if err != nil {
		return nil, ErrBadPassword
	}
	return plain, nil
}

// EncryptedBackup returns a fresh encrypted snapshot.
func (s *Store) EncryptedBackup(ctx context.Context, pass string) ([]byte, error) {
	var buf bytes.Buffer
	if err := s.WriteBackup(ctx, s.tmpDir(), &buf); err != nil {
		return nil, err
	}
	return EncryptBackup(buf.Bytes(), pass)
}

// DecryptFile writes the plain gzip backup next to an encrypted one and
// returns its path (used by the restore command).
func DecryptFile(path, pass string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	plain, err := DecryptBackup(data, pass)
	if err != nil {
		return "", err
	}
	out := path + ".sqlite.gz"
	return out, os.WriteFile(out, plain, 0o600)
}
