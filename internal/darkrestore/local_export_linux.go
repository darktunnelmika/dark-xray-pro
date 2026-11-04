//go:build linux

package darkrestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// LocalInputs are independently approved, offline source-host checkpoints.
type LocalInputs struct {
	Source, SourceSHA, Authority, AuthoritySHA string
	ID, Node, Installation                     string
	AsOf                                       time.Time
}

type LocalImportCopy struct {
	Settlement LocalSourceSettlement `json:"source_settlement"`
	SHA        string                `json:"import_sha256"`
}

// ExportLocalImportCopy publishes a new one-identity M2 input after validating
// both sources. Existing files are never replaced. Publishing the same evidence
// twice must yield the same bytes or return a conflict.
func ExportLocalImportCopy(ctx context.Context, in LocalInputs, seal managedxray.LocalTCPSeal, out string) (LocalImportCopy, error) {
	var zero LocalImportCopy
	prior, err := InspectLocalSettlement(ctx, in, seal)
	if err != nil {
		return zero, err
	}
	parent := filepath.Dir(out)
	canonical, err := filepath.EvalSymlinks(parent)
	if err != nil || !filepath.IsAbs(out) || filepath.Clean(out) != out || canonical != parent || out == in.Source || out == in.Authority {
		return zero, ErrUnsafeSource
	}
	info, err := os.Stat(parent)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return zero, ErrUnsafeSource
	}
	// Build privately on the same filesystem as the final path. Keep SQLite's
	// possible rollback files inside this directory through failure or retry.
	dir, err := os.MkdirTemp(parent, ".local-import-")
	if err != nil {
		return zero, err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "staging.sqlite")
	if err = copyPinnedSQLite(in.Source, in.SourceSHA, path); err != nil {
		return zero, err
	}
	if err = appendLocalEpoch(ctx, path, prior); err != nil {
		return zero, err
	}
	digest, _, err := hashSource(path)
	if err != nil {
		return zero, err
	}
	review, err := BuildReview(ctx, path, digest, in.AsOf)
	total := strconv.FormatInt(prior.Historical, 10)
	if err != nil || review.Status != "ready_for_review" || len(review.Clients) != 1 || len(review.Groups) != 1 || review.Clients[0].SourceID != in.ID || review.Clients[0].HistoricalBytes != total || review.Groups[0].HistoricalBytes != total || review.Summary.TotalConsumedBytes != total {
		return zero, ErrLocalSettlement
	}
	if _, err = ReadEventHistory(ctx, path, review); err != nil {
		return zero, err
	}
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	err = f.Sync()
	f.Close()
	if err != nil {
		return zero, err
	}
	if err = os.Link(path, out); err != nil {
		if !errors.Is(err, os.ErrExist) || checkPrivateCopy(out, digest) != nil {
			return zero, ErrUnsafeSource
		}
	}
	if err = os.Remove(path); err != nil {
		return zero, err
	}
	d, err := os.Open(parent)
	if err != nil {
		return zero, err
	}
	err = d.Sync()
	d.Close()
	if err != nil {
		return zero, err
	}
	return LocalImportCopy{Settlement: prior, SHA: digest}, nil
}

func copyPinnedSQLite(src, sha, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	g, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(g, h), io.LimitReader(f, (8<<30)+1))
	closeErr := g.Close()
	if err != nil || closeErr != nil || n > 8<<30 || hex.EncodeToString(h.Sum(nil)) != sha {
		return ErrUnsafeSource
	}
	return nil
}
func appendLocalEpoch(ctx context.Context, path string, proof LocalSourceSettlement) error {
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=rw&_pragma=trusted_schema(0)&_pragma=foreign_keys(1)&_pragma=journal_mode(DELETE)&_pragma=synchronous(FULL)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tables, triggers, clients, groups int
	err = tx.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM pragma_table_list WHERE schema='main' AND name='restore_usage' AND type='table'),
 (SELECT count(*) FROM sqlite_schema WHERE type='trigger' AND tbl_name='restore_usage' COLLATE NOCASE),
 (SELECT count(*) FROM restore_subscriptions),(SELECT count(*) FROM restore_groups)`).Scan(&tables, &triggers, &clients, &groups)
	if err != nil || tables != 1 || triggers != 0 || clients != 1 || groups != 1 {
		return ErrLocalSettlement
	}
	// The closed epoch is a separate historical scope. Updating core_clients
	// would also fire the legacy local-meter trigger and could count bytes twice.
	if _, err = tx.ExecContext(ctx, "INSERT OR ABORT INTO restore_usage(restore_id,scope,up,down) VALUES(?,?,?,?)", proof.SourceID, "m4-local-epoch:"+proof.Terminal.EpochID, proof.Up, proof.Down); err != nil {
		return ErrLocalSettlement
	}
	if _, err = tx.ExecContext(ctx, "CREATE TABLE darkpro_local_epoch_provenance_v1(id INTEGER PRIMARY KEY CHECK(id=1),receipt BLOB NOT NULL)"); err != nil {
		return ErrLocalSettlement
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO darkpro_local_epoch_provenance_v1 VALUES(1,?)", raw); err != nil {
		return err
	}
	return tx.Commit()
}
