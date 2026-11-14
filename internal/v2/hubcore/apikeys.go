package hubcore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// API keys let scripts and other panels use the admin API without the
// owner's master token. A key carries a staff level (admin or viewer), so
// the same guards and the same audit log apply; it is shown once and only
// its SHA-256 is kept. Format: dk_<id>_<secret>.

const apiKeySchema = `
CREATE TABLE IF NOT EXISTS api_keys (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  level TEXT NOT NULL CHECK(level IN ('admin','viewer')),
  secret_sha256 TEXT NOT NULL,
  created_unix INTEGER NOT NULL,
  created_by TEXT NOT NULL,
  last_used INTEGER NOT NULL DEFAULT 0
);
`

func (s *Store) ensureAPIKeySchema() error {
	_, err := s.db.Exec(apiKeySchema)
	return err
}

type APIKey struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Level       string `json:"level"`
	CreatedUnix int64  `json:"created_unix"`
	CreatedBy   string `json:"created_by"`
	LastUsed    int64  `json:"last_used"`
}

// CreateAPIKey returns the new key; it cannot be read again.
func (s *Store) CreateAPIKey(ctx context.Context, name, level, by string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 || (level != "admin" && level != "viewer") {
		return "", fmt.Errorf("%w: a name and a level (admin or viewer) are required", ErrInvalid)
	}
	id, secret := randomHex(4), randomHex(24)
	_, err := s.db.ExecContext(ctx, `INSERT INTO api_keys(id,name,level,secret_sha256,created_unix,created_by) VALUES(?,?,?,?,?,?)`,
		id, name, level, hashToken(secret), s.now().Unix(), by)
	if err != nil {
		return "", err
	}
	return "dk_" + id + "_" + secret, nil
}

func (s *Store) APIKeys(ctx context.Context) ([]APIKey, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT id,name,level,created_unix,created_by,last_used FROM api_keys ORDER BY created_unix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		if err := rows.Scan(&k.ID, &k.Name, &k.Level, &k.CreatedUnix, &k.CreatedBy, &k.LastUsed); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAPIKey(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// apiKeyPrincipal resolves a dk_ bearer key.
func (s *Store) apiKeyPrincipal(ctx context.Context, tok string) (Principal, bool) {
	rest, ok := strings.CutPrefix(tok, "dk_")
	if !ok {
		return Principal{}, false
	}
	id, secret, ok := strings.Cut(rest, "_")
	if !ok || id == "" || secret == "" {
		return Principal{}, false
	}
	var name, level, sum string
	err := s.ro.QueryRowContext(ctx, `SELECT name,level,secret_sha256 FROM api_keys WHERE id=?`, id).Scan(&name, &level, &sum)
	if errors.Is(err, sql.ErrNoRows) || err != nil || !constantEqual(sum, hashToken(secret)) {
		return Principal{}, false
	}
	now := s.now().Unix()
	// Touch at most once a minute to keep reads cheap.
	_, _ = s.db.ExecContext(ctx, `UPDATE api_keys SET last_used=? WHERE id=? AND last_used<?`, now, id, now-60)
	return Principal{User: "key:" + name, Role: "owner", Level: level}, true
}
