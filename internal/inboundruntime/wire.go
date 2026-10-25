package inboundruntime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

// The installed release supplies this capability set. The HUB compares hashes
// with its approved release before issuing any execution lease.
type Capabilities struct {
	Version  int                           `json:"version"`
	Binaries map[string]string             `json:"binaries"`
	Host     *inboundexec.HostCapabilities `json:"host,omitempty"`
}
type PollInput struct {
	NodeID            string       `json:"node_id"`
	InstallationID    string       `json:"installation_id"`
	Capabilities      Capabilities `json:"capabilities"`
	UncertainEpochID  string       `json:"uncertain_epoch_id,omitempty"`
	ReconciledEpochID string       `json:"reconciled_epoch_id,omitempty"`
}
type Lease struct {
	Version           int    `json:"version"`
	NodeID            string `json:"node_id"`
	InstallationID    string `json:"installation_id"`
	Generation        uint64 `json:"generation"`
	ConfigSHA         string `json:"config_sha"`
	ManifestSHA       string `json:"manifest_sha"`
	Issued            int64  `json:"issued_unix_nano"`
	Expires           int64  `json:"expires_unix_nano"`
	Signature         string `json:"signature"`
	ResumeEpochID     string `json:"resume_epoch_id,omitempty"`
	ResumeEpochSHA    string `json:"resume_epoch_sha,omitempty"`
	ResumeOperationID string `json:"resume_operation_id,omitempty"`
}
type Delivery struct {
	Bundle    inboundexec.Bundle          `json:"bundle"`
	Lease     Lease                       `json:"lease"`
	Clearance *inboundexec.EpochClearance `json:"clearance,omitempty"`
}
type EpochReceipt struct {
	EpochID         string `json:"epoch_id"`
	ProcessIdentity string `json:"process_identity"`
	Registered      bool   `json:"registered"`
}

type Exchange interface {
	Poll(context.Context, PollInput) (*Delivery, error)
	Register(context.Context, inboundexec.EpochInput) (EpochReceipt, error)
	Usage(context.Context, inboundexec.UsageReport) (inboundexec.UsageReceipt, error)
	Observe(context.Context, inboundexec.Observation) error
}

func SignLease(key []byte, l Lease) (Lease, error) {
	if len(key) < 32 || l.Version != 1 || l.NodeID == "" || len(l.InstallationID) != 32 || l.Generation == 0 || len(l.ConfigSHA) != 64 || len(l.ManifestSHA) != 64 || l.Issued <= 0 || l.Expires <= l.Issued || l.Expires-l.Issued > int64(30*time.Second) {
		return Lease{}, errors.New("invalid_inbound_lease")
	}
	if l.ResumeEpochID != "" || l.ResumeEpochSHA != "" || l.ResumeOperationID != "" {
		if l.ResumeEpochID == "" || len(l.ResumeEpochSHA) != 64 || l.ResumeOperationID == "" {
			return Lease{}, errors.New("invalid_inbound_resume_lease")
		}
	}
	l.Signature = ""
	b, e := json.Marshal(l)
	if e != nil {
		return Lease{}, e
	}
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("DARK-PRO-MANAGED-INBOUND-LEASE-v1\x00"))
	_, _ = h.Write(b)
	l.Signature = hex.EncodeToString(h.Sum(nil))
	return l, nil
}

func VerifyDelivery(key []byte, d Delivery, node, installation string, now time.Time) error {
	if d.Clearance != nil {
		c := d.Clearance
		if inboundexec.VerifyClearance(key, *c) != nil || c.NodeID != node || c.InstallationID != installation {
			return errors.New("inbound_clearance_scope_mismatch")
		}
		if d.Bundle.Generation == 0 && d.Lease == (Lease{}) && inboundexec.Digest(d.Bundle) == inboundexec.Digest(inboundexec.Bundle{}) {
			// This packet grants only permission to clear one reconciled
			// uncertain journal. It contains no process execution authority.
			return nil
		}
		if c.Generation > d.Bundle.Generation {
			return errors.New("inbound_clearance_generation_mismatch")
		}
	}
	l := d.Lease
	signature := l.Signature
	expected, e := SignLease(key, l)
	if e != nil || !hmac.Equal([]byte(signature), []byte(expected.Signature)) {
		return errors.New("inbound_lease_signature_mismatch")
	}
	if l.NodeID != node || l.InstallationID != installation || d.Bundle.NodeID != node || d.Bundle.InstallationID != installation || l.Generation != d.Bundle.Generation || l.ConfigSHA != d.Bundle.ConfigSHA || l.ManifestSHA != d.Bundle.ManifestSHA || l.Expires <= now.UnixNano() || l.Issued > now.Add(2*time.Second).UnixNano() {
		return errors.New("inbound_lease_scope_mismatch")
	}
	return inboundexec.VerifyBundle(key, d.Bundle)
}

func ReportSHA(r inboundexec.UsageReport) string {
	raw, _ := json.Marshal(r)
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func hashBytes(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func FinalProofSHA(identity, nonce string, counters []inboundexec.Counter) string {
	raw, _ := json.Marshal(struct {
		Process  string                `json:"process"`
		Nonce    string                `json:"nonce"`
		Counters []inboundexec.Counter `json:"counters"`
	}{identity, nonce, counters})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
