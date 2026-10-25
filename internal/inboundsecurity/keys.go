package inboundsecurity

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

func (s *Service) GenerateX25519() (KeyPair, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, failure("key_generation_failed")
	}
	return KeyPair{Kind: "x25519", PrivateKey: base64.RawURLEncoding.EncodeToString(key.Bytes()), PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes())}, nil
}

// The expected formats are pinned to XTLS/Xray-core v26.9.9, commit
// 52a412d9e2f5c2a5142b1b4e2ab3771dacb8b120. Unknown formats fail closed.
func (s *Service) GenerateMLDSA65(ctx context.Context) (MLDSA65Keys, error) {
	if s.core == nil {
		return MLDSA65Keys{}, failure("pinned_xray_core_required")
	}
	output, err := s.core.run(ctx, "mldsa65")
	if err != nil {
		return MLDSA65Keys{}, err
	}
	fields, ok := parseLabelledOutput(output, "Seed:", "Verify:")
	if !ok || !canonicalBase64(fields[0], base64.RawURLEncoding, 32) || !canonicalBase64(fields[1], base64.RawURLEncoding, 1952) {
		return MLDSA65Keys{}, failure("invalid_core_key_output")
	}
	return MLDSA65Keys{Kind: "mldsa65", Seed: fields[0], Verify: fields[1]}, nil
}

func (s *Service) GenerateECH(ctx context.Context, serverName string) (ECHKeys, error) {
	if !validDNSName(serverName) {
		return ECHKeys{}, failure("invalid_ech_server_name")
	}
	if s.core == nil {
		return ECHKeys{}, failure("pinned_xray_core_required")
	}
	serverName = strings.ToLower(serverName)
	output, err := s.core.run(ctx, "tls", "ech", "--serverName", serverName)
	if err != nil {
		return ECHKeys{}, err
	}
	fields, ok := parseLabelledOutput(output, "ECH config list:", "ECH server keys:")
	if !ok || !validateECH(fields[0], fields[1], serverName) {
		return ECHKeys{}, failure("invalid_core_key_output")
	}
	return ECHKeys{Kind: "ech", ServerName: serverName, Config: fields[0], PrivateKey: fields[1]}, nil
}

func canonicalBase64(value string, encoding *base64.Encoding, length int) bool {
	data, err := encoding.Strict().DecodeString(value)
	return err == nil && len(data) == length && encoding.EncodeToString(data) == value
}

func parseLabelledOutput(output []byte, first, second string) ([]string, bool) {
	text := strings.TrimSpace(string(output))
	if !strings.HasPrefix(text, first) {
		return nil, false
	}
	text = strings.TrimSpace(strings.TrimPrefix(text, first))
	parts := strings.Split(text, second)
	if len(parts) != 2 {
		return nil, false
	}
	values := []string{strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])}
	for _, value := range values {
		if value == "" || strings.ContainsAny(value, "\t\r\n ") {
			return nil, false
		}
	}
	return values, true
}

func validateECH(config64, keys64, serverName string) bool {
	list, err := base64.StdEncoding.Strict().DecodeString(config64)
	if err != nil || base64.StdEncoding.EncodeToString(list) != config64 || len(list) < 2 {
		return false
	}
	keys, err := base64.StdEncoding.Strict().DecodeString(keys64)
	if err != nil || base64.StdEncoding.EncodeToString(keys) != keys64 || len(keys) < 36 || binary.BigEndian.Uint16(keys[:2]) != 32 {
		return false
	}
	config := list[2:]
	if int(binary.BigEndian.Uint16(list[:2])) != len(config) || int(binary.BigEndian.Uint16(keys[34:36])) != len(keys)-36 || !bytes.Equal(keys[36:], config) {
		return false
	}
	// One ECHConfig with the X25519 KEM is emitted by the pinned command.
	if len(config) < 47 || binary.BigEndian.Uint16(config[:2]) != 0xfe0d || int(binary.BigEndian.Uint16(config[2:4])) != len(config)-4 ||
		binary.BigEndian.Uint16(config[5:7]) != 0x0020 || binary.BigEndian.Uint16(config[7:9]) != 32 {
		return false
	}
	private, err := ecdh.X25519().NewPrivateKey(keys[2:34])
	if err != nil || subtle.ConstantTimeCompare(private.PublicKey().Bytes(), config[9:41]) != 1 {
		return false
	}
	suiteBytes := int(binary.BigEndian.Uint16(config[41:43]))
	if suiteBytes < 4 || suiteBytes%4 != 0 || len(config) < 47+suiteBytes {
		return false
	}
	for offset := 43; offset < 43+suiteBytes; offset += 4 {
		kdf, aead := binary.BigEndian.Uint16(config[offset:offset+2]), binary.BigEndian.Uint16(config[offset+2:offset+4])
		if kdf < 1 || kdf > 3 || aead < 1 || aead > 3 {
			return false
		}
	}
	nameLengthOffset := 44 + suiteBytes // skip maximum_name_length
	nameLength := int(config[nameLengthOffset])
	nameStart, extensionsOffset := nameLengthOffset+1, nameLengthOffset+1+nameLength
	if len(config) < extensionsOffset+2 || string(config[nameStart:extensionsOffset]) != serverName {
		return false
	}
	return int(binary.BigEndian.Uint16(config[extensionsOffset:extensionsOffset+2])) == len(config)-extensionsOffset-2
}

type pinnedCore struct {
	path string
	hash [32]byte
	mu   sync.Mutex
}

func newPinnedCore(path, expected string) (*pinnedCore, error) {
	decoded, err := hex.DecodeString(expected)
	if !filepath.IsAbs(path) || len(expected) != 64 || expected != strings.ToLower(expected) || err != nil || len(decoded) != 32 {
		return nil, failure("invalid_core_pin")
	}
	core := &pinnedCore{path: path}
	copy(core.hash[:], decoded)
	file, err := core.openVerified()
	if err != nil {
		return nil, err
	}
	_ = file.Close()
	return core, nil
}

func (c *pinnedCore) openVerified() (*os.File, error) {
	info, err := os.Lstat(c.path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 || info.Mode().Perm()&0022 != 0 || info.Size() < 1 || info.Size() > 512<<20 {
		return nil, failure("pinned_core_unavailable")
	}
	file, err := os.Open(c.path)
	if err != nil {
		return nil, failure("pinned_core_unavailable")
	}
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		file.Close()
		return nil, failure("core_pin_mismatch")
	}
	hash := sha256.New()
	_, err = io.Copy(hash, io.LimitReader(file, (512<<20)+1))
	if err != nil || subtle.ConstantTimeCompare(hash.Sum(nil), c.hash[:]) != 1 {
		file.Close()
		return nil, failure("core_pin_mismatch")
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, failure("pinned_core_unavailable")
	}
	return file, nil
}

type limitedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (b *limitedOutput) Write(data []byte) (int, error) {
	if len(data) > b.limit-b.buffer.Len() {
		return 0, failure("core_output_too_large")
	}
	return b.buffer.Write(data)
}

func (c *pinnedCore) run(ctx context.Context, arguments ...string) ([]byte, error) {
	// Key generation is cheap and serialized; avoid unbounded child processes.
	if !c.mu.TryLock() {
		return nil, failure("key_generation_busy")
	}
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return nil, failure("key_generation_cancelled")
	}
	file, err := c.openVerified()
	if err != nil {
		return nil, err
	}
	defer file.Close()
	path := c.path
	if runtime.GOOS == "linux" {
		// Execute the already hashed open file, not a replaceable path lookup.
		path = "/proc/self/fd/3"
	}
	command := exec.CommandContext(ctx, path, arguments...)
	if runtime.GOOS == "linux" {
		command.ExtraFiles = []*os.File{file}
	}
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C"}
	command.WaitDelay = time.Second
	stdout, stderr := &limitedOutput{limit: 32 << 10}, &limitedOutput{limit: 4 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, failure("key_generation_cancelled")
		}
		return nil, failure("core_key_generation_failed")
	}
	return append([]byte(nil), stdout.buffer.Bytes()...), nil
}
