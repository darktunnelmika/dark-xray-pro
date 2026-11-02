package darkrestore

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func TestArchiveRequiresAnIdentityWitnessFromTheSameDeletion(t *testing.T) {
	const uid = "01234567-89ab-4cde-8fab-0123456789ab"
	const schema = `
 ALTER TABLE restore_subscriptions ADD COLUMN updated_at REAL;
 UPDATE restore_subscriptions SET updated_at=deleted_at, deleted_reason='manual',enabled=0,legacy_total=5000,
 core_email='restore_0123456789abcdef0123@dark.restore';
 ALTER TABLE core_clients ADD COLUMN body TEXT;
 DELETE FROM core_clients;
 CREATE TABLE core_client_tombstones(email TEXT PRIMARY KEY,body TEXT,inbounds TEXT,up INTEGER,down INTEGER,deleted_at REAL);
 INSERT INTO core_client_tombstones VALUES('restore_0123456789abcdef0123@dark.restore',
 '{"email":"restore_0123456789abcdef0123@dark.restore","id":"01234567-89ab-4cde-8fab-0123456789ab","totalGB":5000,"expiryTime":1893456000000}','[1]',3,7,1720000000.25);
 INSERT INTO restore_events SELECT id,'restore.archived',deleted_at FROM restore_subscriptions;
 CREATE TABLE live_audit(actor TEXT,owner TEXT,action TEXT,target TEXT,detail TEXT,at REAL);
 INSERT INTO live_audit SELECT 'owner','owner','dark_restore.archive',id,'soft delete; history preserved',1720000000.5 FROM restore_subscriptions;
 `
	for _, tc := range []struct {
		name, change string
		proven       bool
	}{
		{"same archive", "", true},
		{"no audit", "DROP TABLE live_audit", false},
		{"earlier tombstone", "UPDATE core_client_tombstones SET deleted_at=1719999999", false},
		{"replaced tombstone", "UPDATE core_client_tombstones SET deleted_at=1720000001", false},
		{"repeated archive operation", "INSERT INTO live_audit SELECT * FROM live_audit", false},
		{"both live and deleted", "INSERT INTO core_clients SELECT email,body FROM core_client_tombstones", false},
		{"wrong credential owner", `UPDATE core_client_tombstones SET body=replace(body,'@dark.restore','@other.test')`, false},
		{"different route", "UPDATE core_client_tombstones SET inbounds='[2]'", false},
		{"different plan", `UPDATE core_client_tombstones SET body=replace(body,'5000,','6000,')`, false},
		{"ambiguous UUID", `UPDATE core_client_tombstones SET body=replace(body,'"id":','"id":"conflict","id":')`, false},
		{"aggregate archive audit", "UPDATE live_audit SET action='dark_restore.archive_finished',target='1'", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, _ := sourceFixture(t, 1)
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = db.Exec(schema + tc.change); err != nil {
				t.Fatal(err)
			}
			if err = db.Close(); err != nil {
				t.Fatal(err)
			}
			hash := fixtureSHA(t, path)
			review, err := BuildReview(context.Background(), path, hash, time.Unix(1780000000, 0))
			if err != nil || len(review.Clients) != 1 {
				t.Fatal(err, review.Status)
			}
			got := review.Clients[0]
			if (got.UUID == uid) != tc.proven || (got.UUID != "") != tc.proven || got.Status != "archive_history_only" ||
				strings.Contains(strings.Join(got.ReviewReasons, ","), "uuid_not_verified") == tc.proven ||
				got.HistoricalBytes != "320" || fixtureSHA(t, path) != hash {
				t.Fatal("archive evidence mismatch", got.Status, got.ReviewReasons)
			}
		})
	}
}
