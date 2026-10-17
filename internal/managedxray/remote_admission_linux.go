//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

const MeteredStatsEndpoint = "127.0.0.1:29761"

type BirthReport struct {
	Lease             Lease  `json:"lease"`
	KernelBootID      string `json:"kernel_boot_id"`
	PID               int    `json:"pid"`
	ProcessStartTicks uint64 `json:"process_start_ticks,string"`
	Signature         string `json:"signature"`
}

func (b BirthReport) signature(key []byte) string {
	b.Signature = ""
	raw, _ := json.Marshal(b)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("DARK-PRO-REMOTE-PROCESS-BIRTH-v1\x00"))
	mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}
func SignBirthReport(token string, lease Lease, birth uc.ProcessToken) (BirthReport, error) {
	if len(token) < 32 || birth.PID <= 0 || len(birth.BootID) != 36 || birth.StartTicks == 0 || lease.Signature == "" {
		return BirthReport{}, ErrInvalid
	}
	r := BirthReport{Lease: lease, KernelBootID: birth.BootID, PID: birth.PID, ProcessStartTicks: birth.StartTicks}
	r.Signature = r.signature([]byte(token))
	return r, nil
}
func ValidBirthReport(token string, r BirthReport) bool {
	if len(token) < 32 || len(r.Signature) != 64 || len(r.Lease.Signature) != 64 || r.PID <= 0 || r.ProcessStartTicks == 0 || len(r.KernelBootID) != 36 {
		return false
	}
	expected := r.signature([]byte(token))
	return hmac.Equal([]byte(expected), []byte(r.Signature))
}
func ValidLeaseSignature(l Lease, token string) bool {
	return len(token) >= 32 && len(l.Signature) == 64 && hmac.Equal([]byte(l.Signature), []byte(l.signature([]byte(token))))
}

// MeteredRemoteSync is a separate, explicitly opted-in first-admission
// protocol. Ordinary Sync can NEVER start a metered candidate. All requests
// use the already verified/CA-trusted/leaf-pinned Agent HTTPS transport.
// A missing receipt, crash, unknown process, changed candidate or outage
// leaves the customer listener CLOSED, with an irreversible settlement fence.
func (r *Runtime) MeteredRemoteSync(ctx context.Context, client *http.Client, endpoint string, sender *uc.Sender) (Snapshot, error) {
	if ctx == nil || client == nil || sender == nil {
		return r.Snapshot(), ErrInvalid
	}
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return r.Snapshot(), ErrLease
	}
	secure := *client
	secure.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrLease }
	base := strings.TrimRight(endpoint, "/")
	query := url.Values{"node_id": {r.options.NodeID}, "installation_id": {r.options.InstallationID}}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/v1/agent/native-metered-runtime?"+query.Encode(), nil)
	if e != nil {
		return r.Snapshot(), ErrLease
	}
	req.Header.Set("Authorization", "Bearer "+string(r.options.LeaseKey))
	result, e := secure.Do(req)
	if e != nil {
		return r.Snapshot(), ErrLease
	}
	defer result.Body.Close()
	if result.TLS == nil || result.TLS.Version < tls.VersionTLS13 || len(result.TLS.VerifiedChains) == 0 {
		return r.Snapshot(), ErrLease
	}
	if result.StatusCode == http.StatusNoContent {
		return r.Snapshot(), nil
	}
	if result.StatusCode != http.StatusOK {
		return r.Snapshot(), ErrUsageSettlement
	}
	var delivery Delivery
	dec := json.NewDecoder(io.LimitReader(result.Body, 2*xrayconfig.MaxCandidateBytes+65536))
	dec.DisallowUnknownFields()
	if dec.Decode(&delivery) != nil {
		return r.Snapshot(), ErrInvalid
	}
	var extra any
	if !errors.Is(dec.Decode(&extra), io.EOF) {
		return r.Snapshot(), ErrInvalid
	}
	if delivery.Version != 2 || delivery.NodeID != r.options.NodeID || delivery.InstallationID != r.options.InstallationID || delivery.ListenIP != r.options.ListenIP || delivery.Lease.Sequence != delivery.Sequence || !ValidLeaseSignature(delivery.Lease, string(r.options.LeaseKey)) {
		return r.Snapshot(), ErrLease
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(delivery.ConfigBase64)
	if e != nil || len(raw) > xrayconfig.MaxCandidateBytes {
		return r.Snapshot(), ErrInvalid
	}
	delivery.Candidate.Config = raw
	revision := Revision{Sequence: delivery.Sequence, Candidate: delivery.Candidate}
	_, _, stats, e := xrayconfig.UsageBootstrap(revision.Candidate)
	if e != nil || stats != MeteredStatsEndpoint {
		return r.Snapshot(), ErrInvalid
	}
	snapshot := r.Snapshot()
	if snapshot.ProcessReady && snapshot.ActiveSequence == revision.Sequence && snapshot.ActiveSHA == revision.Candidate.SHA256 {
		result, e := r.Refresh(delivery.Lease)
		if e == nil {
			_ = reportObservation(ctx, &secure, base, string(r.options.LeaseKey), RuntimeReport{Lease: delivery.Lease, Observed: result})
		}
		return result, e
	}
	// Never restart/adopt a previous Xray birth. A new epoch can only be
	// prepared by a fresh private, empty supervisor after explicit settlement.
	if snapshot.Highwater > 0 || snapshot.SavedSequence > 0 || snapshot.Usage != nil {
		return snapshot, ErrUsageSettlement
	}
	birth, e := r.PrepareUsageAdmission(ctx, revision, delivery.Lease)
	if e != nil {
		return r.Snapshot(), e
	}
	proof, e := SignBirthReport(string(r.options.LeaseKey), delivery.Lease, birth)
	if e != nil {
		return r.Snapshot(), e
	}
	body, _ := json.Marshal(proof)
	post, e := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v1/agent/native-metered-register", bytes.NewReader(body))
	if e != nil {
		return r.Snapshot(), e
	}
	post.Header.Set("Authorization", "Bearer "+string(r.options.LeaseKey))
	post.Header.Set("Content-Type", "application/json")
	reply, e := secure.Do(post)
	if e != nil {
		return r.Snapshot(), ErrUsageSettlement
	}
	defer reply.Body.Close()
	if reply.TLS == nil || reply.TLS.Version < tls.VersionTLS13 || len(reply.TLS.VerifiedChains) == 0 || reply.StatusCode != http.StatusCreated {
		return r.Snapshot(), ErrUsageSettlement
	}
	var epoch uc.Epoch
	epochDecoder := json.NewDecoder(io.LimitReader(reply.Body, 64*1024))
	epochDecoder.DisallowUnknownFields()
	if epochDecoder.Decode(&epoch) != nil || !errors.Is(epochDecoder.Decode(&extra), io.EOF) {
		return r.Snapshot(), ErrInvalid
	}
	if epoch.NodeID != r.options.NodeID || epoch.InstallationID != r.options.InstallationID || epoch.ConfigSHA != delivery.Candidate.SHA256 ||
		epoch.KernelBootID != birth.BootID || epoch.PID != birth.PID || epoch.ProcessStartTicks != birth.StartTicks {
		return r.Snapshot(), ErrLease
	}
	if _, _, e = uc.CanonicalEpoch(epoch); e != nil {
		return r.Snapshot(), e
	}
	// The HUB has now registered the authoritative UUID->billable mapping.
	// Runtime must complete and receive the initial durable zero receipt
	// BEFORE API-only Xray gets its real listener.
	next, e := r.ActivateUsageAdmission(ctx, UsageObservation{Epoch: epoch, StatsEndpoint: stats, Sender: sender,
		Interval: 2 * time.Second, MaxSampleGap: 12 * time.Second, MaxPendingAge: 20 * time.Second})
	if e == nil {
		_ = reportObservation(ctx, &secure, base, string(r.options.LeaseKey), RuntimeReport{Lease: delivery.Lease, Observed: next})
	}
	return next, e
}

// The Agent's timeout must be bounded under the signed lease lifetime.
const MeteredSyncTimeout = 25 * time.Second
