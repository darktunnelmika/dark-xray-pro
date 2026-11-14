package hubcore

import (
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// WireGuard runs as an Xray inbound: each client is a peer whose email
// carries the usage counters, so volume, expiry and enforcement work as
// for every other protocol. A client's key pair is derived from its
// secret (nothing extra to store or leak), and its tunnel address is
// allocated once per inbound and kept.

const wgKey = "dark_wg" // template key: {"subnet": "10.66.0.0/16"} (HUB-only)

const wgSchema = `
CREATE TABLE IF NOT EXISTS wg_ips (
  tag TEXT NOT NULL,
  email TEXT NOT NULL,
  ip TEXT NOT NULL,
  PRIMARY KEY(tag, email),
  UNIQUE(tag, ip)
);
`

func (s *Store) ensureWGSchema() error {
	_, err := s.db.Exec(wgSchema)
	return err
}

// WGKeys returns a client's WireGuard private and public key (base64).
func WGKeys(secret string) (private, public string) {
	seed := sha256.Sum256([]byte("dark-wg:" + secret))
	seed[0] &= 248
	seed[31] = (seed[31] & 127) | 64
	k, err := ecdh.X25519().NewPrivateKey(seed[:])
	if err != nil {
		return "", ""
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes())
}

// WGServerKeys makes the inbound's key pair.
func WGServerKeys() (private, public string, err error) {
	priv, _, err := RealityKeys()
	if err != nil {
		return "", "", err
	}
	raw, err := base64.RawURLEncoding.DecodeString(priv)
	if err != nil {
		return "", "", err
	}
	k, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(k.Bytes()), base64.StdEncoding.EncodeToString(k.PublicKey().Bytes()), nil
}

// wgIP returns the client's address in the inbound's subnet, allocating
// the lowest free one (.1 is the server) on first use.
func (s *Store) wgIP(ctx context.Context, tag, email, subnet string) (string, error) {
	var ip string
	err := s.db.QueryRowContext(ctx, `SELECT ip FROM wg_ips WHERE tag=? AND email=?`, tag, email).Scan(&ip)
	if err == nil {
		return ip, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	_, n, err := net.ParseCIDR(subnet)
	if err != nil || n.IP.To4() == nil {
		return "", fmt.Errorf("%w: WireGuard subnet %q is not an IPv4 network", ErrInvalid, subnet)
	}
	ones, _ := n.Mask.Size()
	if ones > 28 {
		return "", fmt.Errorf("%w: WireGuard subnet %s is too small", ErrInvalid, subnet)
	}
	used := map[string]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT ip FROM wg_ips WHERE tag=?`, tag)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var x string
		if rows.Scan(&x) == nil {
			used[x] = true
		}
	}
	rows.Close()
	base := binary.BigEndian.Uint32(n.IP.To4())
	size := uint32(1) << (32 - ones)
	for off := uint32(2); off < size-1; off++ {
		b := make(net.IP, 4)
		binary.BigEndian.PutUint32(b, base+off)
		cand := b.String()
		if used[cand] {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT INTO wg_ips(tag,email,ip) VALUES(?,?,?)`, tag, email, cand); err != nil {
			return "", err
		}
		return cand, nil
	}
	return "", fmt.Errorf("%w: WireGuard subnet %s is full", ErrInvalid, subnet)
}

func wgSubnet(t map[string]any) string {
	if m, ok := t[wgKey].(map[string]any); ok {
		if sn, _ := m["subnet"].(string); sn != "" {
			return sn
		}
	}
	return "10.66.0.0/16"
}

// wgPeers builds the inbound's peer list.
func (s *Store) wgPeers(ctx context.Context, tag, subnet string, list []Client) ([]any, error) {
	peers := make([]any, 0, len(list))
	for _, c := range list {
		ip, err := s.wgIP(ctx, tag, c.Email, subnet)
		if err != nil {
			return nil, err
		}
		_, pub := WGKeys(c.Secret)
		peers = append(peers, map[string]any{"publicKey": pub, "allowedIPs": []string{ip + "/32"}, "email": c.Email})
	}
	return peers, nil
}
