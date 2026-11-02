//go:build linux

package darkrestore

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

const localHandoffDomain = "DARK-PRO-DARKRESTORE-LOCAL-HANDOFF-v1\x00"

type SignedLocalHandoffV1 struct {
	Version    int                   `json:"version"`
	Settlement LocalSourceSettlement `json:"source_settlement"`
	Signature  []byte                `json:"signature"`
}
type LocalHandoffFileV1 struct {
	SHA          string                `json:"sha256"`
	PublicKeySHA string                `json:"public_key_sha256"`
	Settlement   LocalSourceSettlement `json:"source_settlement"`
}

func privateHandoffBytes(path string, limit int) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrLocalHandoff
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return nil, ErrLocalHandoff
	}
	for _, p := range []string{filepath.Dir(path), path} {
		st, e := os.Lstat(p)
		if e != nil || st.Mode().Perm()&0077 != 0 {
			return nil, ErrLocalHandoff
		}
		if p == path && (!st.Mode().IsRegular() || st.Size() < 1 || st.Size() > int64(limit)) {
			return nil, ErrLocalHandoff
		}
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrLocalHandoff
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil || len(raw) > limit {
		return nil, ErrLocalHandoff
	}
	return raw, nil
}

// ReadLocalHandoffPublicKey only accepts an operator-pinned, owner-private raw
// Ed25519 public key. A browser request cannot select the trust anchor.
func ReadLocalHandoffPublicKey(path, pin string) ([]byte, error) {
	raw, err := privateHandoffBytes(path, ed25519.PublicKeySize)
	h := sha256.Sum256(raw)
	if err != nil || len(raw) != ed25519.PublicKeySize || hex.EncodeToString(h[:]) != pin {
		return nil, ErrLocalHandoff
	}
	return raw, nil
}
func localHandoffMessage(proof LocalSourceSettlement) ([]byte, error) {
	raw, err := json.Marshal(proof)
	if err != nil {
		return nil, err
	}
	return append([]byte(localHandoffDomain), raw...), nil
}

// WriteSignedLocalHandoff verifies the live source inputs on their original
// host and boot, then emits an immutable private file. The source journal key
// never enters the output. An identical retry verifies instead of overwrites.
func WriteSignedLocalHandoff(ctx context.Context, in LocalInputs, seal managedxray.LocalTCPSeal, keyPath, out string) (LocalHandoffFileV1, error) {
	var zero LocalHandoffFileV1
	proof, err := InspectLocalSettlement(ctx, in, seal)
	if err != nil {
		return zero, err
	}
	key, err := privateHandoffBytes(keyPath, ed25519.PrivateKeySize)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return zero, ErrLocalHandoff
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	public := ed25519.PrivateKey(key).Public().(ed25519.PublicKey)
	if !bytes.Equal(public, ed25519.NewKeyFromSeed(key[:ed25519.SeedSize]).Public().(ed25519.PublicKey)) {
		return zero, ErrLocalHandoff
	}
	publicHash := sha256.Sum256(public)
	msg, err := localHandoffMessage(proof)
	if err != nil {
		return zero, err
	}
	signed := SignedLocalHandoffV1{Version: 1, Settlement: proof, Signature: ed25519.Sign(ed25519.PrivateKey(key), msg)}
	raw, err := json.Marshal(signed)
	if err != nil {
		return zero, err
	}
	raw = append(raw, '\n')
	if !filepath.IsAbs(out) || filepath.Clean(out) != out {
		return zero, ErrLocalHandoff
	}
	dir := filepath.Dir(out)
	canonical, e := filepath.EvalSymlinks(dir)
	info, e2 := os.Stat(dir)
	if e != nil || e2 != nil || canonical != dir || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return zero, ErrLocalHandoff
	}
	temp, e := os.CreateTemp(dir, ".local-handoff-")
	if e != nil {
		return zero, e
	}
	defer os.Remove(temp.Name())
	if _, e = temp.Write(raw); e != nil {
		temp.Close()
		return zero, e
	}
	if e = temp.Sync(); e != nil {
		temp.Close()
		return zero, e
	}
	if e = temp.Close(); e != nil {
		return zero, e
	}
	if e = os.Link(temp.Name(), out); e != nil {
		if !errors.Is(e, os.ErrExist) {
			return zero, e
		}
		existing, x := privateHandoffBytes(out, 16<<10)
		if x != nil || !bytes.Equal(existing, raw) {
			return zero, ErrLocalHandoff
		}
	}
	d, e := os.Open(dir)
	if e != nil {
		return zero, e
	}
	e = d.Sync()
	d.Close()
	if e != nil {
		return zero, e
	}
	sum := sha256.Sum256(raw)
	return LocalHandoffFileV1{SHA: hex.EncodeToString(sum[:]), PublicKeySHA: hex.EncodeToString(publicHash[:]), Settlement: proof}, nil
}

func VerifySignedLocalHandoff(path, pin string, publicKey []byte) (VerifiedLocalHandoff, error) {
	var zero VerifiedLocalHandoff
	if len(publicKey) != ed25519.PublicKeySize || len(pin) != 64 {
		return zero, ErrLocalHandoff
	}
	raw, err := privateHandoffBytes(path, 16<<10)
	if err != nil {
		return zero, err
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != pin || !bytes.HasSuffix(raw, []byte("\n")) {
		return zero, ErrLocalHandoff
	}
	var signed SignedLocalHandoffV1
	if json.Unmarshal(raw, &signed) != nil || signed.Version != 1 || len(signed.Signature) != ed25519.SignatureSize {
		return zero, ErrLocalHandoff
	}
	canonical, _ := json.Marshal(signed)
	if !bytes.Equal(raw, append(canonical, '\n')) {
		return zero, ErrLocalHandoff
	}
	proof := signed.Settlement
	if proof.Scope != "single_local_source_epoch" || proof.SourceID == "" || len(proof.SourceSHA) != 64 ||
		len(proof.AuthoritySHA) != 64 || len(proof.JournalSHA) != 64 || proof.PriorBytes < 0 ||
		proof.Up < 0 || proof.Down < 0 || proof.Historical < proof.PriorBytes {
		return zero, ErrLocalHandoff
	}
	msg, err := localHandoffMessage(proof)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(publicKey), msg, signed.Signature) {
		return zero, ErrLocalHandoff
	}
	publicSum := sha256.Sum256(publicKey)
	return VerifiedLocalHandoff{settlement: proof, digest: pin, publicKeySHA: hex.EncodeToString(publicSum[:]), valid: true}, nil
}
