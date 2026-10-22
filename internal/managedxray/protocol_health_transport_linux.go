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

// SyncMeteredHealth is separate from lease/command execution. It persists an
// attempt BEFORE network I/O, then its exact receipt BEFORE posting to the HUB.
// A crash between these saves is unknown, never permission to rerun the probe.
func (t *TransactionRuntime) SyncMeteredHealth(ctx context.Context, client *http.Client, endpoint string) error {
	if !t.healthSync.TryLock() {
		return ErrProtocolHealth
	}
	defer t.healthSync.Unlock()
	u, err := url.Parse(endpoint)
	if t.metered == nil || client == nil || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return ErrProtocolHealth
	}
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
		rejected := res.StatusCode == 409 && time.Now().UnixNano() >= a.Request.Expires
		if res.StatusCode != 204 && !rejected {
			return ErrProtocolHealth
		}
		t.mu.Lock()
		defer t.mu.Unlock()
		s := t.state
		if s.Health == nil || s.Health.Request != a.Request {
			return ErrProtocolHealth
		}
		copy := *s.Health
		copy.Acknowledged = !rejected
		copy.RejectedAfterExpiry = rejected
		s.Health = &copy
		if err := t.save(s); err != nil {
			return err
		}
		if rejected {
			return ErrProtocolHealth
		}
		return nil
	}
	t.mu.Lock()
	saved := t.state.Health
	t.mu.Unlock()
	if saved != nil && saved.Receipt != nil && !saved.Acknowledged && !saved.RejectedAfterExpiry {
		return send(saved)
	}
	p := SignMeteredTransactionPoll(t.options.LeaseKey, TransactionPoll{NodeID: t.options.NodeID, InstallationID: t.options.InstallationID, BinarySHA: t.options.BinarySHA, At: time.Now().UnixNano(), Status: t.Status()})
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
	if err != nil || !verifyProtocolHealthRequest(t.options.LeaseKey, q, time.Now().UnixNano()) || q.NodeID != t.options.NodeID || q.InstallationID != t.options.InstallationID || q.EndpointSHA != d.Endpoint.SHA() || !d.Endpoint.Valid() {
		return ErrProtocolHealth
	}
	if saved != nil && saved.Request.ID == q.ID {
		if saved.Request != q || saved.Receipt == nil {
			return ErrProtocolHealth
		}
		return send(saved)
	}
	// Do not overwrite an unresolved attempt while it could still be delivered.
	if saved != nil && !saved.Acknowledged && saved.Request.Expires > time.Now().UnixNano() {
		return ErrProtocolHealth
	}
	t.mu.Lock()
	s := t.state
	s.Health = &protocolHealthAttempt{Request: q}
	err = t.save(s)
	t.mu.Unlock()
	if err != nil {
		return err
	}
	proof, err := t.ProbeMeteredProtocol(ctx, q, d.Endpoint)
	if err != nil {
		return err
	}
	a := &protocolHealthAttempt{Request: q, Receipt: &proof}
	t.mu.Lock()
	s = t.state
	s.Health = a
	err = t.save(s)
	t.mu.Unlock()
	if err != nil {
		return err
	}
	return send(a)
}
