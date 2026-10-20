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
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// TransactionPoll is a fresh signed observation, separate from historical ACKs.
type TransactionPoll struct {
	NodeID         string            `json:"node_id"`
	InstallationID string            `json:"installation_id"`
	BinarySHA      string            `json:"binary_sha"`
	At             int64             `json:"at_unix_nano"`
	Status         TransactionStatus `json:"status"`
	Signature      string            `json:"signature"`
}

func SignTransactionPoll(key []byte, p TransactionPoll) TransactionPoll {
	p.Signature = ""
	p.Signature = transactionMAC(key, "DARK-PRO-ROUTE-POLL-v1", p)
	return p
}
func VerifyTransactionPoll(key []byte, p TransactionPoll) bool {
	if p.Status.Usage != nil || p.Status.Observed.Usage != nil {
		return false
	}
	want := SignTransactionPoll(key, p)
	return len(key) >= 32 && transactionNode.MatchString(p.NodeID) && transactionID.MatchString(p.InstallationID) && transactionHash.MatchString(p.BinarySHA) && transactionID.MatchString(p.Status.Instance) && p.Status.Serial <= maxTransactionSerial && p.At > 0 && (p.Status.CurrentStageID == "" || transactionID.MatchString(p.Status.CurrentStageID)) && (p.Status.PreviousStageID == "" || transactionID.MatchString(p.Status.PreviousStageID)) && (p.Status.StagedID == "" || transactionID.MatchString(p.Status.StagedID)) && (p.Status.InterruptedCommandSHA == "" || transactionHash.MatchString(p.Status.InterruptedCommandSHA)) && transactionHash.MatchString(p.Signature) && hmac.Equal([]byte(want.Signature), []byte(p.Signature))
}

// Only the authenticated TLS Agent endpoint may carry these private bytes.
type TransactionDelivery struct {
	Command      TransactionCommand   `json:"command"`
	Candidate    xrayconfig.Candidate `json:"candidate"`
	ConfigBase64 string               `json:"config_base64"`
}

// SyncTransaction delivers at most one queued action and commits its exact
// signed receipt. Lost responses replay the same command/receipt on next poll;
// polling never authorizes Apply, recovery or lease renewal by itself.
func (t *TransactionRuntime) SyncTransaction(ctx context.Context, client *http.Client, endpoint string) error {
	if t.metered != nil {
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
	p := SignTransactionPoll(t.options.LeaseKey, TransactionPoll{NodeID: t.options.NodeID, InstallationID: t.options.InstallationID, BinarySHA: t.options.BinarySHA, At: time.Now().UnixNano(), Status: t.Status()})
	res, e := post("/api/v1/agent/route-transactions/poll", p)
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
	ack, operationErr := t.Execute(ctx, d.Command, d.Candidate)
	if ack.CommandID == "" {
		return operationErr
	}
	if !VerifyTransactionReceipt(t.options.LeaseKey, d.Command, ack) {
		return ErrTransaction
	}
	res, e = post("/api/v1/agent/route-transactions/receipt", ack)
	if e != nil {
		return e
	}
	res.Body.Close()
	if res.StatusCode != 204 {
		return errors.New("route transaction receipt not committed")
	}
	return operationErr
}
