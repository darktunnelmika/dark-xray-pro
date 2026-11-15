package nodeagent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The HUB's address can change (a new server, a domain): the HUB sends the
// new address as a task, the agent checks it reaches the same HUB (its
// certificate is signed by the pinned CA for the given name), saves it in
// the state directory, and restarts onto it. The saved address wins over
// the -hub flag, so the systemd unit never needs editing.

// HubAddr is the saved override.
type HubAddr struct {
	URL  string `json:"url"`
	Name string `json:"name,omitempty"` // TLS name; "" = host from URL
}

func hubAddrPath(stateDir string) string { return filepath.Join(stateDir, "hub.json") }

// LoadHubAddr returns the saved address, if any.
func LoadHubAddr(stateDir string) (HubAddr, bool) {
	b, err := os.ReadFile(hubAddrPath(stateDir))
	if err != nil {
		return HubAddr{}, false
	}
	var a HubAddr
	if json.Unmarshal(b, &a) != nil || !strings.HasPrefix(a.URL, "https://") {
		return HubAddr{}, false
	}
	return a, true
}

// checkHub proves addr serves a certificate from the pinned CA.
func checkHub(ctx context.Context, a HubAddr, caFile string) error {
	u, err := url.Parse(a.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("the address must look like https://host:port")
	}
	host := u.Host
	if u.Port() == "" {
		host = net.JoinHostPort(u.Hostname(), "443")
	}
	name := a.Name
	if name == "" {
		name = u.Hostname()
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS13, ServerName: name}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return errors.New("hub CA file has no certificate")
		}
		cfg.RootCAs = pool
	}
	d := &tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: cfg}
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	c, err := d.DialContext(dctx, "tcp", host)
	if err != nil {
		return fmt.Errorf("cannot reach the HUB at %s: %w", a.URL, err)
	}
	return c.Close()
}

// SetHubTask checks and saves a new HUB address, then restarts the agent.
func SetHubTask(stateDir, caFile string) TaskFunc {
	return func(ctx context.Context, args json.RawMessage) (any, error) {
		var a HubAddr
		if err := json.Unmarshal(args, &a); err != nil {
			return nil, err
		}
		a.URL = strings.TrimRight(strings.TrimSpace(a.URL), "/")
		if err := checkHub(ctx, a, caFile); err != nil {
			return nil, err
		}
		b, _ := json.Marshal(a)
		tmp := hubAddrPath(stateDir) + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return nil, err
		}
		if err := os.Rename(tmp, hubAddrPath(stateDir)); err != nil {
			return nil, err
		}
		return Restart{Value: map[string]string{"hub": a.URL}}, nil
	}
}
