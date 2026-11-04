package business

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

type DarkRestoreQuarantineRegistration struct {
	Scope     string
	PlanSHA   string
	CoreSHA   string
	Signature []byte
}

func (r DarkRestoreQuarantineRegistration) signingBytes() []byte {
	return []byte("DARK-PRO-NODE-QUARANTINE-REGISTRATION-v1\x00" + r.PlanSHA + "\x00" + r.CoreSHA)
}

func (r DarkRestoreQuarantineRegistration) Verify(pub ed25519.PublicKey, planSHA, coreSHA string) bool {
	return r.Scope == "offline_syntax_only_v1" && routeSHA.MatchString(planSHA) && routeSHA.MatchString(coreSHA) && r.PlanSHA == planSHA && r.CoreSHA == coreSHA && len(pub) == ed25519.PublicKeySize && ed25519.Verify(pub, r.signingBytes(), r.Signature)
}

// The directory and signing key belong to the local node operator. The file
// contains only a signed digest. Its existence cannot open a listener.
func (p DarkRestoreQuarantinePlan) RegisterOffline(ctx context.Context, node, installation, planSHA, binary, coreSHA, dir string, key ed25519.PrivateKey) (DarkRestoreQuarantineRegistration, error) {
	var zero DarkRestoreQuarantineRegistration
	info, err := os.Lstat(dir)
	if err != nil || !filepath.IsAbs(dir) || !info.IsDir() || info.Mode().Perm()&0077 != 0 || len(key) != ed25519.PrivateKeySize {
		return zero, ErrInvalid
	}
	proof, err := p.ValidateOffline(ctx, node, installation, planSHA, binary, coreSHA)
	if err != nil {
		return zero, err
	}
	if !proof.XraySyntaxValidated || proof.CanActivate {
		return zero, ErrDarkRestoreCutover
	}
	r := DarkRestoreQuarantineRegistration{Scope: "offline_syntax_only_v1", PlanSHA: planSHA, CoreSHA: coreSHA}
	r.Signature = ed25519.Sign(key, r.signingBytes())
	if !r.Verify(key.Public().(ed25519.PublicKey), planSHA, coreSHA) {
		return zero, ErrInvalid
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return zero, err
	}
	tmp, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return zero, err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(raw)
	if err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err != nil {
		return zero, err
	}
	if closeErr != nil {
		return zero, closeErr
	}
	path := filepath.Join(dir, planSHA+".json")
	if err = os.Link(tmp.Name(), path); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return zero, err
		}
		st, err := os.Lstat(path)
		if err != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() != int64(len(raw)) {
			return zero, ErrConflict
		}
		prior, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(prior, raw) {
			return zero, ErrConflict
		}
	}
	directory, err := os.Open(dir)
	if err != nil {
		return zero, err
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return zero, err
	}
	return r, nil
}
