//go:build linux

package business

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"time"

	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// A process birth proves only the API-only bootstrap phase. Admission and
// customer traffic remain controlled by the separate metered runtime.
type DarkRestoreQuarantineBirth struct {
	CoreSHA          string `json:"core_sha256,omitempty"`
	PlanSHA          string
	Lease            mx.Lease
	Process          uc.ProcessToken
	ObservedUnixNano int64
	Signature        []byte
}

func (b DarkRestoreQuarantineBirth) signingBytes() []byte {
	b.Signature = nil
	raw, _ := json.Marshal(b)
	return append([]byte("DARK-PRO-QUARANTINE-API-BIRTH-v1\x00"), raw...)
}
func (b DarkRestoreQuarantineBirth) Verify(pub ed25519.PublicKey, pin string, lease mx.Lease, now int64) bool {
	return routeSHA.MatchString(pin) && (b.CoreSHA == "" || routeSHA.MatchString(b.CoreSHA)) && b.PlanSHA == pin && b.Lease == lease &&
		len(pub) == ed25519.PublicKeySize && b.ObservedUnixNano >= lease.Issued && b.ObservedUnixNano < lease.Expires && now >= b.ObservedUnixNano && now < lease.Expires &&
		b.Process.PID > 0 && b.Process.StartTicks > 0 && b.Process.BootID != "" &&
		ed25519.Verify(pub, b.signingBytes(), b.Signature)
}

func (p DarkRestoreQuarantinePlan) PrepareRuntime(ctx context.Context, r *mx.Runtime, pin string, lease mx.Lease, signer ed25519.PrivateKey) (DarkRestoreQuarantineBirth, error) {
	var zero DarkRestoreQuarantineBirth
	sha, err := p.Digest()
	if err != nil || r == nil || sha != pin || lease.NodeID != p.NodeID || lease.InstallationID != p.InstallationID || lease.ConfigSHA != p.ConfigSHA || lease.ManifestSHA != p.ManifestSHA || len(signer) != ed25519.PrivateKeySize {
		return zero, ErrDarkRestoreCutover
	}
	birth, err := r.PrepareUsageAdmission(ctx, mx.Revision{Sequence: lease.Sequence, Candidate: p.candidate}, lease)
	if err != nil {
		return zero, err
	}
	state := r.Snapshot()
	if state.Phase != "usage_prepared_no_customer_listeners" || state.DataPlaneVerified || state.SubscriptionPublished {
		r.Close()
		return zero, ErrDarkRestoreCutover
	}
	receipt := DarkRestoreQuarantineBirth{CoreSHA: r.BinarySHA256(), PlanSHA: pin, Lease: lease, Process: birth, ObservedUnixNano: time.Now().UnixNano()}
	receipt.Signature = ed25519.Sign(signer, receipt.signingBytes())
	if !receipt.Verify(signer.Public().(ed25519.PublicKey), pin, lease, time.Now().UnixNano()) {
		r.Close()
		return zero, ErrDarkRestoreCutover
	}
	return receipt, nil
}
