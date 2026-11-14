package nodeagent

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

// Hub is what the agent needs from the HUB. It is an interface so tests and
// a HUB outage can be simulated without a network.
type Hub interface {
	Sync(ctx context.Context, req wire.SyncRequest) (wire.SyncResponse, error)
	DeliverUsage(ctx context.Context, b wire.UsageBatch) (wire.UsageAck, error)
}

// HTTPHub is the HTTPS client. The HUB certificate is pinned with a private
// CA file; the per-Node token is sent as a bearer credential.
type HTTPHub struct {
	BaseURL string
	NodeID  string
	Token   string
	Client  *http.Client
}

// NewHTTPHub builds a client that trusts only caFile (if given).
func NewHTTPHub(baseURL, nodeID, token, caFile, serverName string) (*HTTPHub, error) {
	if !strings.HasPrefix(baseURL, "https://") {
		return nil, errors.New("hub URL must use https")
	}
	tc := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: serverName}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("hub CA file has no certificate")
		}
		tc.RootCAs = pool
	}
	tr := &http.Transport{TLSClientConfig: tc, ForceAttemptHTTP2: true, IdleConnTimeout: 90 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	return &HTTPHub{BaseURL: strings.TrimRight(baseURL, "/"), NodeID: nodeID, Token: token, Client: &http.Client{Transport: tr, Timeout: 20 * time.Second}}, nil
}

func (h *HTTPHub) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set(wire.HeaderNodeID, h.NodeID)
	resp, err := h.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		msg := string(data)
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return fmt.Errorf("hub %s: %s: %s", path, resp.Status, msg)
	}
	return json.Unmarshal(data, out)
}

func (h *HTTPHub) Sync(ctx context.Context, req wire.SyncRequest) (wire.SyncResponse, error) {
	var out wire.SyncResponse
	err := h.post(ctx, wire.PathSync, req, &out)
	return out, err
}

func (h *HTTPHub) DeliverUsage(ctx context.Context, b wire.UsageBatch) (wire.UsageAck, error) {
	var out wire.UsageAck
	err := h.post(ctx, wire.PathUsage, b, &out)
	return out, err
}
