//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

type ProtocolHealthDelivery struct {
	Request  ProtocolHealthRequest  `json:"request"`
	Endpoint ProtocolHealthEndpoint `json:"endpoint"`
}
type ProtocolHealthSubmission struct {
	ChallengeID string                `json:"challenge_id"`
	Receipt     ProtocolHealthReceipt `json:"receipt"`
}
type protocolHealthAttempt struct {
	Request             ProtocolHealthRequest  `json:"request"`
	Receipt             *ProtocolHealthReceipt `json:"receipt,omitempty"`
	Acknowledged        bool                   `json:"acknowledged"`
	RejectedAfterExpiry bool                   `json:"rejected_after_expiry,omitempty"`
}

const (
	// Two maximum-size inbound sets fit without evicting any live nonce. If a
	// caller produces more work before expiry, refuse it instead of forgetting
	// an attempt that could still be replayed.
	maxProtocolHealthAttempts = 512
	maxProtocolHealthSyncWork = 512
	maxProtocolHealthSyncTime = 12 * time.Second
)

func protocolHealthAttemptCount(s transactionState) int {
	n := len(s.HealthHistory)
	if s.Health != nil {
		n++
	}
	return n
}

func protocolHealthAttempts(s transactionState) []*protocolHealthAttempt {
	attempts := make([]*protocolHealthAttempt, 0, protocolHealthAttemptCount(s))
	for i := range s.HealthHistory {
		attempts = append(attempts, &s.HealthHistory[i])
	}
	if s.Health != nil {
		attempts = append(attempts, s.Health)
	}
	return attempts
}

// Retire only expired unknown attempts or terminal receipts. An unacknowledged
// successful proof is an outbox item, even after expiry, and must survive until
// the authenticated HUB accepts it or explicitly refuses it after expiry.
// The barrier and retained history are saved together with the next intent;
// a later wall-clock rollback cannot make a pruned request executable again.
func retireProtocolHealthAttempts(s transactionState, now int64) transactionState {
	retire := func(a *protocolHealthAttempt) bool {
		if a.Request.Expires > now || (a.Receipt != nil && !a.Acknowledged && !a.RejectedAfterExpiry) {
			return false
		}
		if a.Request.Expires > s.HealthRetiredThrough {
			s.HealthRetiredThrough = a.Request.Expires
		}
		return true
	}
	history := make([]protocolHealthAttempt, 0, len(s.HealthHistory))
	for i := range s.HealthHistory {
		if !retire(&s.HealthHistory[i]) {
			history = append(history, s.HealthHistory[i])
		}
	}
	s.HealthHistory = history
	if s.Health != nil && retire(s.Health) {
		s.Health = nil
	}
	return s
}

func replaceProtocolHealthAttempt(s transactionState, a protocolHealthAttempt) (transactionState, error) {
	if s.Health != nil && s.Health.Request.ID == a.Request.ID {
		if s.Health.Request != a.Request {
			return s, ErrProtocolHealth
		}
		s.Health = &a
		return s, nil
	}
	for i := range s.HealthHistory {
		if s.HealthHistory[i].Request.ID != a.Request.ID {
			continue
		}
		if s.HealthHistory[i].Request != a.Request {
			return s, ErrProtocolHealth
		}
		// Never alter the live state's backing slice before the atomic save.
		s.HealthHistory = append([]protocolHealthAttempt(nil), s.HealthHistory...)
		s.HealthHistory[i] = a
		return s, nil
	}
	return s, ErrProtocolHealth
}

// SyncMeteredHealth is separate from lease/command execution. It persists an
// attempt BEFORE network I/O, then its exact receipt BEFORE posting to the HUB.
// A crash between these saves is unknown, never permission to rerun the probe.
// Accepted work is drained immediately, with an independent count and time
// bound, so the worker's idle polling interval is not imposed on every inbound.
func (t *TransactionRuntime) SyncMeteredHealth(ctx context.Context, client *http.Client, endpoint string) error {
	return t.syncMeteredHealth(ctx, client, endpoint, t.ProbeMeteredProtocol, time.Now)
}

// The explicit probe and clock arguments allow isolated transport/crash tests;
// production always uses the actual restricted probe and wall clock above.
func (t *TransactionRuntime) syncMeteredHealth(ctx context.Context, client *http.Client, endpoint string, probe func(context.Context, ProtocolHealthRequest, ProtocolHealthEndpoint) (ProtocolHealthReceipt, error), clock func() time.Time) error {
	if !t.healthSync.TryLock() {
		return ErrProtocolHealth
	}
	defer t.healthSync.Unlock()
	u, err := url.Parse(endpoint)
	if ctx == nil || ctx.Err() != nil || t.metered == nil || client == nil || probe == nil || clock == nil || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return ErrProtocolHealth
	}
	ctx, cancel := context.WithTimeout(ctx, maxProtocolHealthSyncTime)
	defer cancel()
	secure := *client
	secure.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrProtocolHealth }
	post := func(path string, v any) (*http.Response, error) {
		raw, e := json.Marshal(v)
		if e != nil {
			return nil, ErrProtocolHealth
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/v1/agent/metered-health/"+path, bytes.NewReader(raw))
		if e != nil {
			return nil, ErrProtocolHealth
		}
		req.Header.Set("Authorization", "Bearer "+string(t.options.LeaseKey))
		req.Header.Set("Content-Type", "application/json")
		res, e := secure.Do(req)
		if e != nil {
			return nil, ErrProtocolHealth
		}
		if res.TLS == nil || res.TLS.Version < tls.VersionTLS13 || len(res.TLS.VerifiedChains) == 0 {
			res.Body.Close()
			return nil, ErrProtocolHealth
		}
		return res, nil
	}
	// A saved successful proof can be replayed historically even after stop or
	// restart. Never turn a pending/unknown attempt into a second execution.
	send := func(a *protocolHealthAttempt) error {
		res, e := post("receipt", ProtocolHealthSubmission{a.Request.ID, *a.Receipt})
		if e != nil {
			return e
		}
		res.Body.Close()
		rejected := res.StatusCode == 409 && clock().UnixNano() >= a.Request.Expires
		if res.StatusCode != 204 && !rejected {
			return ErrProtocolHealth
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		s := t.state
		copy := *a
		copy.Acknowledged = copy.Acknowledged || !rejected
		copy.RejectedAfterExpiry = rejected && !copy.Acknowledged
		s, e = replaceProtocolHealthAttempt(s, copy)
		if e != nil {
			return e
		}
		if err := t.save(s); err != nil {
			return err
		}
		if rejected {
			return ErrProtocolHealth
		}
		return nil
	}
	for work := 0; work < maxProtocolHealthSyncWork; work++ {
		if ctx.Err() != nil {
			return ErrProtocolHealth
		}
		t.mu.Lock()
		var pending *protocolHealthAttempt
		for _, a := range protocolHealthAttempts(t.state) {
			if a.Receipt != nil && !a.Acknowledged && !a.RejectedAfterExpiry {
				pending = a
				break
			}
		}
		t.mu.Unlock()
		if pending != nil {
			if err = send(pending); err != nil {
				return err
			}
			continue
		}
		p := SignMeteredTransactionPoll(t.options.LeaseKey, TransactionPoll{NodeID: t.options.NodeID, InstallationID: t.options.InstallationID, BinarySHA: t.options.BinarySHA, At: clock().UnixNano(), Status: t.Status()})
		res, err := post("poll", p)
		if err != nil {
			return err
		}
		if res.StatusCode == 204 || res.StatusCode == 404 {
			res.Body.Close()
			return nil
		}
		if res.StatusCode != 200 {
			res.Body.Close()
			return ErrProtocolHealth
		}
		var d ProtocolHealthDelivery
		dec := json.NewDecoder(io.LimitReader(res.Body, 128<<10))
		dec.DisallowUnknownFields()
		err = dec.Decode(&d)
		var extra any
		if err == nil && !errors.Is(dec.Decode(&extra), io.EOF) {
			err = ErrProtocolHealth
		}
		res.Body.Close()
		q := d.Request
		if err != nil || !verifyProtocolHealthRequest(t.options.LeaseKey, q, clock().UnixNano()) || q.NodeID != t.options.NodeID || q.InstallationID != t.options.InstallationID || q.EndpointSHA != d.Endpoint.SHA() || !d.Endpoint.Valid() {
			return ErrProtocolHealth
		}
		t.mu.Lock()
		var saved *protocolHealthAttempt
		for _, a := range protocolHealthAttempts(t.state) {
			if a.Request.ID == q.ID {
				saved = a
				break
			}
		}
		t.mu.Unlock()
		if saved != nil {
			if saved.Request != q || saved.Receipt == nil {
				return ErrProtocolHealth
			}
			if err = send(saved); err != nil {
				return err
			}
			continue
		}
		if err = t.beginProtocolHealthAttempt(q, clock().UnixNano()); err != nil {
			return err
		}
		proof, err := probe(ctx, q, d.Endpoint)
		if err != nil {
			return err
		}
		if !VerifyProtocolHealthReceipt(t.options.LeaseKey, q, proof, time.Unix(0, proof.Completed)) {
			return ErrProtocolHealth
		}
		a := &protocolHealthAttempt{Request: q, Receipt: &proof}
		t.mu.Lock()
		s, saveErr := replaceProtocolHealthAttempt(t.state, *a)
		if saveErr == nil {
			saveErr = t.save(s)
		}
		t.mu.Unlock()
		if saveErr != nil {
			return saveErr
		}
		if err = send(a); err != nil {
			return err
		}
	}
	return nil
}

func (t *TransactionRuntime) beginProtocolHealthAttempt(q ProtocolHealthRequest, now int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := retireProtocolHealthAttempts(t.state, now)
	if q.Expires <= s.HealthRetiredThrough || protocolHealthAttemptCount(s) >= maxProtocolHealthAttempts {
		return ErrProtocolHealth
	}
	for _, a := range protocolHealthAttempts(s) {
		// Unknown or unacknowledged work cannot be displaced while executable.
		if a.Request.ID == q.ID || (!a.Acknowledged && !a.RejectedAfterExpiry && (a.Receipt != nil || a.Request.Expires > now)) {
			return ErrProtocolHealth
		}
	}
	if s.Health != nil {
		s.HealthHistory = append(s.HealthHistory, *s.Health)
	}
	s.Health = &protocolHealthAttempt{Request: q}
	s.Version = meteredTransactionJournalVersion
	return t.save(s)
}
