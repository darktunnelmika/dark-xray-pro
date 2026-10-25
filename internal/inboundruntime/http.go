package inboundruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/usagewire"
)

type HTTPExchange struct {
	client                              *http.Client
	endpoint, token, node, installation string
}

func NewHTTPExchange(client *http.Client, endpoint, token, node, installation string) (*HTTPExchange, error) {
	u, e := url.Parse(endpoint)
	if e != nil || client == nil || len(token) < 32 || node == "" || len(installation) != 32 || u.User != nil || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "https" && (u.Scheme != "http" || u.Host != "127.0.0.1:2087") {
		return nil, errors.New("invalid_inbound_exchange")
	}
	return &HTTPExchange{client: client, endpoint: endpoint, token: token, node: node, installation: installation}, nil
}
func (x *HTTPExchange) call(ctx context.Context, operation string, input, output any) (int, error) {
	raw, e := json.Marshal(input)
	if e != nil {
		return 0, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, x.endpoint+"/api/v1/agent/inbound-execution/"+operation, bytes.NewReader(raw))
	if e != nil {
		return 0, e
	}
	req.Header.Set("Authorization", "Bearer "+x.token)
	req.Header.Set("Content-Type", "application/json")
	res, e := x.client.Do(req)
	if e != nil {
		return 0, e
	}
	defer res.Body.Close()
	if res.StatusCode == 204 {
		return 204, nil
	}
	if res.StatusCode != 200 {
		return res.StatusCode, fmt.Errorf("inbound_hub_http_%d", res.StatusCode)
	}
	if output == nil {
		return res.StatusCode, errors.New("unexpected_inbound_response")
	}
	raw, e = io.ReadAll(io.LimitReader(res.Body, (64<<20)+1))
	if e != nil || len(raw) > 64<<20 {
		return 0, errors.New("inbound_response_too_large")
	}
	// A Bundle may contain many principals/assets. Its opaque configs are base64
	// strings; the wire parser still rejects duplicate top-level identities.
	if usagewire.Decode(raw, output) != nil {
		return 0, errors.New("invalid_inbound_response")
	}
	return res.StatusCode, nil
}
func (x *HTTPExchange) Poll(ctx context.Context, p PollInput) (*Delivery, error) {
	if p.NodeID != x.node || p.InstallationID != x.installation {
		return nil, errors.New("inbound_exchange_identity")
	}
	var d Delivery
	code, e := x.call(ctx, "poll", p, &d)
	if e != nil || code == 204 {
		return nil, e
	}
	return &d, nil
}
func (x *HTTPExchange) Register(ctx context.Context, p inboundexec.EpochInput) (EpochReceipt, error) {
	var r EpochReceipt
	if p.NodeID != x.node || p.InstallationID != x.installation {
		return r, errors.New("inbound_exchange_identity")
	}
	_, e := x.call(ctx, "register", p, &r)
	return r, e
}
func (x *HTTPExchange) Usage(ctx context.Context, p inboundexec.UsageReport) (inboundexec.UsageReceipt, error) {
	var r inboundexec.UsageReceipt
	if p.NodeID != x.node || p.InstallationID != x.installation {
		return r, errors.New("inbound_exchange_identity")
	}
	_, e := x.call(ctx, "usage", p, &r)
	return r, e
}
func (x *HTTPExchange) Observe(ctx context.Context, p inboundexec.Observation) error {
	if p.NodeID != x.node || p.InstallationID != x.installation {
		return errors.New("inbound_exchange_identity")
	}
	_, e := x.call(ctx, "observation", p, nil)
	return e
}

func ParseExternalBinaries(raw string) (map[string]BinaryConfig, error) {
	result := map[string]BinaryConfig{}
	if strings.TrimSpace(raw) == "" {
		return result, nil
	}
	if len(raw) > 16384 || usagewire.Decode([]byte(raw), &result) != nil || len(result) > 3 {
		return nil, errors.New("invalid_inbound_engine_config")
	}
	for engine, c := range result {
		if engine != "mtproto" && engine != "tuic" && engine != "amneziawg" || len(c.SHA256) != 64 || c.Path == "" {
			return nil, errors.New("invalid_inbound_engine_config")
		}
	}
	return result, nil
}
