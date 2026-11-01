package darkrestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixtureSHA(t *testing.T, path string) string {
	t.Helper()
	data, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func TestPrivateReviewRealHistoryAndNoAutomaticImport(t *testing.T) {
	path, _ := sourceFixture(t, 517)
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("ALTER TABLE core_clients ADD COLUMN body TEXT"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE core_clients SET body='{"id":"01234567-89ab-4cde-8fab-0123456789ab"}' WHERE email='super_private_core_email_0001'`); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	before := fixtureSHA(t, path)
	r, e := BuildReview(context.Background(), path, before, time.Unix(1780000000, 0))
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "ready_for_review" || r.ImportEnabled || len(r.Clients) != 517 || len(r.Groups) != 1 || r.SourceSHA256 != before || r.SourceSizeBytes < 16 {
		t.Fatal("review/source inaccurate", r.Status, e, len(r.Clients))
	}
	if r.Groups[0].HistoricalBytes != "165440" || r.Summary.TotalConsumedBytes != r.Groups[0].HistoricalBytes {
		t.Fatal("historical totals were not preserved", r.Groups[0].HistoricalBytes, r.Summary.TotalConsumedBytes)
	}
	if r.Clients[1].UUID != "01234567-89ab-4cde-8fab-0123456789ab" || len(r.Clients[0].ReviewReasons) == 0 {
		t.Fatal("core UUID proof incorrectly inferred")
	}
	if r.Clients[0].Status != "archive_history_only" || r.Clients[29].Status != "skip_already_promoted" {
		t.Fatal("deleted/promoted must never become new active service")
	}
	if r.Clients[0].QuotaKind != "legacy_unlimited" || r.Clients[1].QuotaKind != "volume" {
		t.Fatal("legacy plan type was guessed")
	}
	raw, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"private-legacy.example", "super_private_token_", "core_email_", "/secret/", "token="} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private URL/token/email leaked")
		}
	}
	if _, e = ReadPrivateReview(raw); e != nil {
		t.Fatal(e)
	}
	if fixtureSHA(t, path) != before {
		t.Fatal("inspection changed old snapshot")
	}
	if files, _ := filepath.Glob(path + "-*"); len(files) > 0 {
		t.Fatal("WAL/SHM written", files)
	}
}
func TestPrivateReviewBadCommitAndUnsafeInputs(t *testing.T) {
	path, _ := sourceFixture(t, 2)
	sha := fixtureSHA(t, path)
	if _, e := BuildReview(context.Background(), path, strings.Repeat("f", 64), time.Unix(1780000000, 0)); !errors.Is(e, ErrUnsafeSource) {
		t.Fatal("mismatched snapshot accepted", e)
	}
	if _, e := BuildReview(context.Background(), path, sha, time.Unix(1780000000, 0)); e != nil {
		t.Fatal(e)
	}
	if _, e := BuildReview(context.Background(), path, sha, time.Unix(-1, 0)); !errors.Is(e, ErrUnsafeSource) {
		t.Fatal("invalid timestamp accepted", e)
	}
	invalid := []byte(`{"version":1,"status":"complete","mode":"offline_operator_review_only","source_sha256":"` + sha + `","source_size_bytes":100}`)
	if _, e := ReadPrivateReview(invalid); !errors.Is(e, ErrInspection) {
		t.Fatal("client-provided success was accepted")
	}
}
func TestPrivateReviewUnknownTimeAndDuplicateUUIDAreFlagged(t *testing.T) {
	path, _ := sourceFixture(t, 3)
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("ALTER TABLE core_clients ADD COLUMN body TEXT"); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE core_clients SET body='{"id":"01234567-89ab-4cde-8fab-0123456789ab"}'`); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`UPDATE restore_subscriptions SET legacy_expire=0 WHERE id='super_private_restore_client_0001'`); e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	r, e := BuildReview(context.Background(), path, fixtureSHA(t, path), time.Unix(1780000000, 0))
	if e != nil {
		t.Fatal(e)
	}
	reasons := strings.Join(r.Clients[1].ReviewReasons, ",")
	if !strings.Contains(reasons, "no_finite_expiry") || !strings.Contains(reasons, "duplicate_core_uuid") {
		t.Fatal("unsafe mapping not flagged", reasons)
	}
	if r.ImportEnabled {
		t.Fatal("review enabled import")
	}
}
