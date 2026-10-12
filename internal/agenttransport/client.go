// Package agenttransport constructs a pinned and normally verified TLS connection
// from a NEW Node Agent to the dedicated HTTPS Agent listener on a LAB HUB.
//
// The previous localhost-only protocol is retained solely for same-host tests.
// There is intentionally no insecure TLS option or certificate-bypass fallback.
package agenttransport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrInvalidEndpoint = errors.New("invalid lab Agent HUB endpoint")
var ErrMissingPin = errors.New("remote HTTPS Agent requires an explicit SHA256 certificate pin")
var ErrCertificatePin = errors.New("HUB certificate fingerprint does not match pinned identity")

type Config struct {
	Endpoint string
	PinSHA256 string
	CABundle []byte
}

// New returns a no-proxy HTTP client with redirects forbidden. Standard Go
// chain/hostname verification always runs BEFORE the additional leaf pin check.
func New(cfg Config) (*http.Client, string, error) {
	raw := strings.TrimSpace(cfg.Endpoint)
	if raw != cfg.Endpoint || raw == "" {
		return nil, "", ErrInvalidEndpoint
	}
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.Opaque != "" ||
		u.Path != "" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery ||
		u.Fragment != "" || u.RawFragment != "" {
		return nil, "", ErrInvalidEndpoint
	}
	host, port, err := net.SplitHostPort(u.Host)
	if err != nil || host == "" || strings.Contains(host, "%") {
		return nil, "", ErrInvalidEndpoint
	}
	p, err := strconv.Atoi(port)
	if err != nil || p < 1 || p > 65535 {
		return nil, "", ErrInvalidEndpoint
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 15 * time.Second}
	transport := &http.Transport{
		Proxy: nil, DialContext: dialer.DialContext,
		MaxIdleConnsPerHost: 4, IdleConnTimeout: 25 * time.Second,
		TLSHandshakeTimeout: 4 * time.Second,
	}
	switch u.Scheme {
	case "http":
		// Plaintext is permitted ONLY over this exact loopback address;
		// remote HTTP cannot leak Pair Codes or credentials by accident.
		if u.Host != "127.0.0.1:2087" || cfg.PinSHA256 != "" || len(cfg.CABundle) != 0 {
			return nil, "", ErrInvalidEndpoint
		}
	case "https":
		if len(cfg.PinSHA256) != 64 {
			return nil, "", ErrMissingPin
		}
		pin, err := hex.DecodeString(cfg.PinSHA256)
		if err != nil || len(pin) != 32 {
			return nil, "", ErrMissingPin
		}
		var roots *x509.CertPool
		if len(cfg.CABundle) > 0 {
			roots, err = x509.SystemCertPool()
			if err != nil {
				return nil, "", err
			}
			if !roots.AppendCertsFromPEM(cfg.CABundle) {
				return nil, "", errors.New("supplied CA bundle has no valid PEM certificates")
			}
		}
		transport.TLSClientConfig = &tls.Config{
			MinVersion: tls.VersionTLS13,
			RootCAs: roots,
			// InsecureSkipVerify is intentionally NEVER set.
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return ErrCertificatePin
				}
				digest := sha256.Sum256(state.PeerCertificates[0].Raw)
				if subtle.ConstantTimeCompare(pin, digest[:]) != 1 {
					return ErrCertificatePin
				}
				return nil
			},
		}
	default:
		return nil, "", ErrInvalidEndpoint
	}
	client := &http.Client{
		Timeout: 6 * time.Second, Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client, u.String(), nil
}

// Probe performs a TLS handshake and limited Agent-only health request.
// A caller may use it before attempting to submit a one-time Pair Code.
func Probe(ctx context.Context, client *http.Client, endpoint string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/api/v1/agent/health", nil)
	if err != nil { return err }
	res, err := client.Do(req)
	if err != nil { return err }
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("agent-only HUB health rejected with HTTP %d", res.StatusCode)
	}
	return nil
}
