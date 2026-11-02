//go:build linux

package managedxray

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// LocalTCPSeal is produced only by the private-journal verifier on the source
// host. An ordinary ledger ACK cannot substitute for the runtime's final seal.
type LocalTCPSeal struct {
	receipt                      uc.Receipt
	bindingSHA, uuid, journalSHA string
}

func (s LocalTCPSeal) Matches(e uc.Epoch, uuid string, terminal uc.Receipt) bool {
	_, binding, err := uc.CanonicalEpoch(e)
	return err == nil && uuid != "" && s.uuid == uuid && s.bindingSHA == binding && s.receipt == terminal && uc.ValidReceipt(terminal)
}
func (s LocalTCPSeal) JournalSHA() string { return s.journalSHA }

// ReadLocalTCPSeal does not open a Runtime, lock a service, or start a process.
// The key is read from a private raw 32-byte file and never passed on argv.
func ReadLocalTCPSeal(path, keyPath, pin, node, installation, listen string) (LocalTCPSeal, error) {
	var zero LocalTCPSeal
	for _, p := range []string{path, keyPath} {
		if !filepath.IsAbs(p) || p != filepath.Clean(p) || privateAncestors(filepath.Dir(p)) != nil {
			return zero, ErrPrivate
		}
	}
	key, err := privateRead(keyPath, 32)
	if err != nil || len(key) != 32 {
		return zero, ErrPrivate
	}
	defer clear(key)
	sealed, err := privateRead(path, 24<<20)
	if err != nil {
		return zero, err
	}
	h := sha256.Sum256(sealed)
	if hex.EncodeToString(h[:]) != pin {
		return zero, ErrCorrupt
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return zero, ErrCorrupt
	}
	a, err := cipher.NewGCM(block)
	if err != nil || len(sealed) < a.NonceSize()+a.Overhead() {
		return zero, ErrCorrupt
	}
	aad := []byte("DARK-PRO-RUNTIME-v1\x00" + node + "\x00" + installation + "\x00" + listen)
	d := disk{aead: a, aad: aad}
	j, err := d.decode(sealed)
	if err != nil || j.Version != 5 || j.Phase != "usage_active_settled" || j.Usage == nil || j.Admission.Settlement == nil || j.Admission.FinalReceipt == nil || *j.Admission.Settlement != *j.Admission.FinalReceipt {
		return zero, ErrUsageSettlement
	}
	e := j.Usage.Epoch
	_, binding, err := uc.CanonicalEpoch(e)
	if err != nil || e.ConfigSHA != j.Good.SHA || e.NodeID != node || e.InstallationID != installation || len(e.Bindings) != 1 {
		return zero, ErrUsageSettlement
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil || string(bytes.TrimSpace(boot)) != e.KernelBootID || uc.VerifyEpochStopped(e) != nil {
		return zero, ErrUsageSettlement
	}
	// The authenticated config must have actually served this credential.
	var cfg struct {
		Inbounds []struct {
			Settings struct{ Clients []struct{ ID, Email string } }
		}
	}
	if json.Unmarshal(j.Good.Raw, &cfg) != nil || len(cfg.Inbounds) != 1 || len(cfg.Inbounds[0].Settings.Clients) != 1 {
		return zero, ErrUsageSettlement
	}
	c := cfg.Inbounds[0].Settings.Clients[0]
	if c.ID == "" || c.Email != e.Bindings[0].Email {
		return zero, ErrUsageSettlement
	}
	return LocalTCPSeal{receipt: *j.Admission.FinalReceipt, bindingSHA: binding, uuid: c.ID, journalSHA: pin}, nil
}
