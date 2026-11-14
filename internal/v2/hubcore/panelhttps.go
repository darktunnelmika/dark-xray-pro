package hubcore

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The panel can serve itself over HTTPS on a port of its own, with a real
// certificate for the owner's domain: any stored certificate (uploaded or
// issued by the built-in ACME client) whose names cover the requested
// host is used, and renewals are picked up without a restart. Without a
// matching certificate the HUB's own self-signed one answers.

type PanelHTTPS struct {
	Port   int    `json:"port"`   // 0 = off
	Domain string `json:"domain"` // shown as the panel address; optional
}

type PanelHTTPSStatus struct {
	PanelHTTPS
	Running  int    `json:"running"`   // port the running HUB listens on (0 = none)
	CertID   string `json:"cert_id"`   // certificate that covers Domain
	DaysLeft int64  `json:"days_left"` // of that certificate
	URL      string `json:"url"`
}

var panelRunning int

// SetPanelRunning records the port main actually started.
func SetPanelRunning(port int) { panelRunning = port }

func (s *Store) PanelHTTPSSettings(ctx context.Context) PanelHTTPS {
	var p PanelHTTPS
	_ = json.Unmarshal([]byte(s.Setting(ctx, "panel_https")), &p)
	return p
}

func (s *Store) PanelHTTPSStatus(ctx context.Context) PanelHTTPSStatus {
	p := s.PanelHTTPSSettings(ctx)
	st := PanelHTTPSStatus{PanelHTTPS: p, Running: panelRunning}
	if p.Domain != "" {
		if id, na, ok := s.certFor(ctx, p.Domain); ok {
			st.CertID, st.DaysLeft = id, (na-s.now().Unix())/86400
		}
		if p.Port > 0 {
			st.URL = "https://" + p.Domain
			if p.Port != 443 {
				st.URL += ":" + strconv.Itoa(p.Port)
			}
			st.URL += "/"
		}
	}
	return st
}

func (s *Store) PutPanelHTTPS(ctx context.Context, p PanelHTTPS) error {
	p.Domain = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(p.Domain, ".")))
	if p.Port < 0 || p.Port > 65535 || reservedPort(p.Port) {
		return fmt.Errorf("%w: pick a port between 1 and 65535 (not 41000-41999), or 0 to turn it off", ErrInvalid)
	}
	if p.Domain != "" && (net.ParseIP(p.Domain) != nil || !strings.Contains(p.Domain, ".") || strings.ContainsAny(p.Domain, " /:*")) {
		return fmt.Errorf("%w: enter a domain name such as panel.example.com", ErrInvalid)
	}
	b, _ := json.Marshal(p)
	return s.SetSetting(ctx, "panel_https", string(b))
}

// certFor finds the stored certificate with the latest expiry covering name.
func (s *Store) certFor(ctx context.Context, name string) (id string, notAfter int64, ok bool) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,domains,not_after FROM certs WHERE not_after>? ORDER BY not_after DESC`, s.now().Unix())
	if err != nil {
		return "", 0, false
	}
	defer rows.Close()
	for rows.Next() {
		var d string
		if rows.Scan(&id, &d, &notAfter) != nil {
			continue
		}
		for _, dn := range splitTags(d) {
			if nameMatches(dn, name) {
				return id, notAfter, true
			}
		}
	}
	return "", 0, false
}

func nameMatches(pattern, name string) bool {
	pattern, name = strings.ToLower(pattern), strings.ToLower(name)
	if pattern == name {
		return true
	}
	if rest, ok := strings.CutPrefix(pattern, "*."); ok {
		i := strings.IndexByte(name, '.')
		return i > 0 && name[i+1:] == rest
	}
	return false
}

type certCacheEntry struct {
	key  string // id + updated
	cert *tls.Certificate
}

// PanelCertificate returns a GetCertificate callback for the panel listener.
func (s *Store) PanelCertificate(fallback tls.Certificate) func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	var mu sync.Mutex
	cache := map[string]certCacheEntry{}
	return func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		name := hello.ServerName
		if name == "" {
			return &fallback, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		id, _, ok := s.certFor(ctx, name)
		if !ok {
			return &fallback, nil
		}
		var certPEM, keyPEM string
		var updated int64
		if s.ro.QueryRowContext(ctx, `SELECT cert_pem,key_pem,updated_unix FROM certs WHERE id=?`, id).Scan(&certPEM, &keyPEM, &updated) != nil {
			return &fallback, nil
		}
		key := id + "@" + strconv.FormatInt(updated, 10)
		mu.Lock()
		defer mu.Unlock()
		if e, ok := cache[id]; ok && e.key == key {
			return e.cert, nil
		}
		c, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
		if err != nil {
			return &fallback, nil
		}
		cache[id] = certCacheEntry{key: key, cert: &c}
		return &c, nil
	}
}
