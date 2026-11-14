package hubcore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// TLS certificates are stored once on the HUB and referenced from inbound
// templates as {"dark_cert":"<id>"} inside tlsSettings.certificates. The
// renderer swaps the reference for the PEM lines, so renewing a certificate
// is one upload and every inbound and node picks it up on its next sync.

const certSchema = `
CREATE TABLE IF NOT EXISTS certs (
  id TEXT PRIMARY KEY,
  cert_pem TEXT NOT NULL,
  key_pem TEXT NOT NULL,
  domains TEXT NOT NULL,
  not_after INTEGER NOT NULL,
  updated_unix INTEGER NOT NULL
);
`

// CertView is a certificate without its private key.
type CertView struct {
	ID       string   `json:"id"`
	Domains  []string `json:"domains"`
	NotAfter int64    `json:"not_after"`
	DaysLeft int64    `json:"days_left"`
	UsedBy   []string `json:"used_by"`
}

func (s *Store) ensureCertSchema() error {
	_, err := s.db.Exec(certSchema)
	return err
}

// PutCert validates the pair (key matches, leaf parses, not expired) and
// stores it.
func (s *Store) PutCert(ctx context.Context, id, certPEM, keyPEM string) error {
	if !idRE.MatchString(id) {
		return ErrInvalid
	}
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return fmt.Errorf("%w: certificate and key do not form a valid pair: %v", ErrInvalid, err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("%w: certificate: %v", ErrInvalid, err)
	}
	if leaf.NotAfter.Before(s.now()) {
		return fmt.Errorf("%w: certificate expired on %s", ErrInvalid, leaf.NotAfter.UTC().Format("2006-01-02"))
	}
	domains := leaf.DNSNames
	if len(domains) == 0 && leaf.Subject.CommonName != "" {
		domains = []string{leaf.Subject.CommonName}
	}
	for _, ip := range leaf.IPAddresses {
		domains = append(domains, ip.String())
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO certs(id,cert_pem,key_pem,domains,not_after,updated_unix) VALUES(?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET cert_pem=excluded.cert_pem, key_pem=excluded.key_pem, domains=excluded.domains,
not_after=excluded.not_after, updated_unix=excluded.updated_unix`,
		id, strings.TrimSpace(certPEM)+"\n", strings.TrimSpace(keyPEM)+"\n", strings.Join(domains, ","), leaf.NotAfter.Unix(), s.now().Unix())
	return err
}

func certRef(id string) string { return `"dark_cert":"` + id + `"` }

// Certs lists certificates with expiry and the inbounds that use them.
func (s *Store) Certs(ctx context.Context) ([]CertView, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,domains,not_after FROM certs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	out := []CertView{}
	for rows.Next() {
		var c CertView
		var d string
		if err := rows.Scan(&c.ID, &d, &c.NotAfter); err != nil {
			rows.Close()
			return nil, err
		}
		c.Domains = splitTags(d)
		c.DaysLeft = (c.NotAfter - s.now().Unix()) / 86400
		out = append(out, c)
	}
	rows.Close()
	for i := range out {
		out[i].UsedBy, err = s.certUsers(ctx, out[i].ID)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) certUsers(ctx context.Context, id string) ([]string, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT tag FROM inbounds WHERE instr(replace(template,' ',''), ?) > 0 ORDER BY tag`, certRef(id))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tags := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// DeleteCert refuses while an inbound still references the certificate.
func (s *Store) DeleteCert(ctx context.Context, id string) error {
	users, err := s.certUsers(ctx, id)
	if err != nil {
		return err
	}
	if len(users) > 0 {
		return fmt.Errorf("%w: still used by %s", ErrInvalid, strings.Join(users, ", "))
	}
	r, err := s.db.ExecContext(ctx, `DELETE FROM certs WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func pemLines(p string) []string {
	return strings.Split(strings.TrimRight(p, "\n"), "\n")
}

// resolveCerts replaces {"dark_cert":"id"} entries in an inbound template
// with inline certificate and key lines.
func (s *Store) resolveCerts(ctx context.Context, t map[string]any) error {
	st, _ := t["streamSettings"].(map[string]any)
	tl, _ := st["tlsSettings"].(map[string]any)
	list, _ := tl["certificates"].([]any)
	for i, item := range list {
		m, _ := item.(map[string]any)
		id, _ := m["dark_cert"].(string)
		if id == "" {
			continue
		}
		var cp, kp string
		err := s.ro.QueryRowContext(ctx, `SELECT cert_pem,key_pem FROM certs WHERE id=?`, id).Scan(&cp, &kp)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: certificate %q is missing", ErrInvalid, id)
		}
		if err != nil {
			return err
		}
		list[i] = map[string]any{"certificate": pemLines(cp), "key": pemLines(kp)}
	}
	return nil
}
