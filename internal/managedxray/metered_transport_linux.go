//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Metered polls use a separate MAC domain; neither v1 nor a missing usage
// condition can be used to acquire a metered channel.
func SignMeteredTransactionPoll(key []byte, p TransactionPoll) TransactionPoll {
	p.Signature = ""
	p.Signature = transactionMAC(key, "DARK-PRO-METERED-ROUTE-POLL-v2", p)
	return p
}
func VerifyMeteredTransactionPoll(key []byte, p TransactionPoll) bool {
	if p.Status.Usage == nil || !validMeteredCondition(*p.Status.Usage) {
		return false
	}
	want := SignMeteredTransactionPoll(key, p)
	structural := p
	structural.Status.Usage = nil
	structural.Status.Observed.Usage = nil
	structural = SignTransactionPoll(key, structural)
	return VerifyTransactionPoll(key, structural) && transactionHash.MatchString(p.Signature) && hmac.Equal([]byte(want.Signature), []byte(p.Signature))
}

// No private config, client IDs or caller-chosen billing map crosses this
// request. The HUB resolves the persisted command and its encrypted candidate.
type MeteredAuthorityRequest struct {
	Command TransactionCommand `json:"command"`
	Birth   *BirthReport       `json:"birth,omitempty"`
}
type MeteredRemoteAuthority struct {
	client                              *http.Client
	endpoint, token, node, installation string
}

func NewMeteredRemoteAuthority(client *http.Client, endpoint, token, node, installation string) (*MeteredRemoteAuthority, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || client == nil || len(token) < 32 || !transactionNode.MatchString(node) || !transactionID.MatchString(installation) {
		return nil, ErrTransaction
	}
	secure := *client
	secure.CheckRedirect = func(*http.Request, []*http.Request) error { return ErrTransaction }
	return &MeteredRemoteAuthority{&secure, endpoint, token, node, installation}, nil
}
func (a *MeteredRemoteAuthority) request(ctx context.Context, c TransactionCommand, v xrayconfig.Candidate, b *uc.ProcessToken) (uc.Epoch, error) {
	var epoch uc.Epoch
	if a == nil || ctx == nil || c.Version != 2 || c.NodeID != a.node || c.InstallationID != a.installation || !validTransactionSignature([]byte(a.token), c) || TransactionPayloadSHA(v) != c.PayloadSHA {
		return epoch, ErrTransaction
	}
	in := MeteredAuthorityRequest{Command: c}
	path := "check"
	if b != nil {
		proof, e := SignBirthReport(a.token, c.Lease, *b)
		if e != nil {
			return epoch, e
		}
		in.Birth = &proof
		path = "register"
	}
	raw, e := json.Marshal(in)
	if e != nil {
		return epoch, ErrTransaction
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, a.endpoint+"/api/v1/agent/metered-route-transactions/"+path, bytes.NewReader(raw))
	if e != nil {
		return epoch, ErrTransaction
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Content-Type", "application/json")
	res, e := a.client.Do(req)
	if e != nil {
		return epoch, ErrUsageSettlement
	}
	defer res.Body.Close()
	if res.TLS == nil || res.TLS.Version < tls.VersionTLS13 || len(res.TLS.VerifiedChains) == 0 {
		return epoch, ErrTransaction
	}
	if b == nil {
		if res.StatusCode != 204 {
			return epoch, ErrTransaction
		}
		return epoch, nil
	}
	if res.StatusCode != 201 {
		return epoch, ErrUsageSettlement
	}
	d := json.NewDecoder(io.LimitReader(res.Body, 65536))
	d.DisallowUnknownFields()
	var extra any
	if d.Decode(&epoch) != nil || !errors.Is(d.Decode(&extra), io.EOF) {
		return uc.Epoch{}, ErrTransaction
	}
	if epoch.NodeID != a.node || epoch.InstallationID != a.installation || epoch.ConfigSHA != v.SHA256 || epoch.KernelBootID != b.BootID || epoch.PID != b.PID || epoch.ProcessStartTicks != b.StartTicks {
		return uc.Epoch{}, ErrTransaction
	}
	if _, _, e = uc.CanonicalEpoch(epoch); e != nil {
		return uc.Epoch{}, e
	}
	return epoch, nil
}
func (a *MeteredRemoteAuthority) Check(ctx context.Context, c TransactionCommand, v xrayconfig.Candidate) error {
	_, e := a.request(ctx, c, v, nil)
	return e
}
func (a *MeteredRemoteAuthority) Register(ctx context.Context, c TransactionCommand, v xrayconfig.Candidate, b uc.ProcessToken) (uc.Epoch, error) {
	return a.request(ctx, c, v, &b)
}

// SyncMeteredTransaction never creates commands or renews leases. All activity
// is explicitly queued by the owner; committed receipts are replayed exactly.
func (t *TransactionRuntime) SyncMeteredTransaction(ctx context.Context, client *http.Client, endpoint string) error {
	if t.metered == nil {
		return ErrTransaction
	}
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || client == nil {
		return ErrTransaction
	}
	secure := *client
	secure.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("route transaction redirect refused") }
	post := func(path string, body any) (*http.Response, error) {
		raw, e := json.Marshal(body)
		if e != nil {
			return nil, ErrTransaction
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(raw))
		if e != nil {
			return nil, ErrTransaction
		}
		req.Header.Set("Authorization", "Bearer "+string(t.options.LeaseKey))
		req.Header.Set("Content-Type", "application/json")
		res, e := secure.Do(req)
		if e != nil {
			return nil, errors.New("route transaction transport unavailable")
		}
		if res.TLS == nil || res.TLS.Version < tls.VersionTLS13 || len(res.TLS.VerifiedChains) == 0 {
			res.Body.Close()
			return nil, ErrTransaction
		}
		return res, nil
	}
	p := SignMeteredTransactionPoll(t.options.LeaseKey, TransactionPoll{NodeID: t.options.NodeID, InstallationID: t.options.InstallationID, BinarySHA: t.options.BinarySHA, At: time.Now().UnixNano(), Status: t.Status()})
	res, e := post("/api/v1/agent/metered-route-transactions/poll", p)
	if e != nil {
		return e
	}
	if res.StatusCode == 204 {
		res.Body.Close()
		return nil
	}
	if res.StatusCode != 200 {
		res.Body.Close()
		return ErrTransaction
	}
	var d TransactionDelivery
	dec := json.NewDecoder(io.LimitReader(res.Body, 2*xrayconfig.MaxCandidateBytes+65536))
	dec.DisallowUnknownFields()
	e = dec.Decode(&d)
	var trailing any
	if e == nil && !errors.Is(dec.Decode(&trailing), io.EOF) {
		e = ErrTransaction
	}
	res.Body.Close()
	if e != nil {
		return ErrTransaction
	}
	raw, e := base64.StdEncoding.Strict().DecodeString(d.ConfigBase64)
	if e != nil || len(raw) > xrayconfig.MaxCandidateBytes {
		return ErrTransaction
	}
	d.Candidate.Config = raw
	ack, operationErr := t.ExecuteMetered(ctx, d.Command, d.Candidate)
	if ack.CommandID == "" {
		return operationErr
	}
	if !VerifyMeteredTransactionReceipt(t.options.LeaseKey, d.Command, ack) {
		return ErrTransaction
	}
	res, e = post("/api/v1/agent/metered-route-transactions/receipt", ack)
	if e != nil {
		return e
	}
	res.Body.Close()
	if res.StatusCode != 204 {
		return errors.New("route transaction receipt not committed")
	}
	return operationErr
}
