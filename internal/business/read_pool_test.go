package business

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestBusinessReadPoolRejectsWritesAfterConnectionReplacement(t *testing.T) {
	s, _ := opened(t)
	if s.db.Stats().MaxOpenConnections != 1 || s.readOnly.Stats().MaxOpenConnections != 2 {
		t.Fatal("unbounded pool or extra writer")
	}
	var syncMode int
	if err := s.db.QueryRow("PRAGMA synchronous").Scan(&syncMode); err != nil || syncMode != 2 {
		t.Fatal("writer durability changed", syncMode, err)
	}
	s.readOnly.SetMaxIdleConns(0)
	for i := 0; i < 3; i++ {
		conn, err := s.readOnly.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var queryOnly int
		if err = conn.QueryRowContext(context.Background(), "PRAGMA query_only").Scan(&queryOnly); err != nil || queryOnly != 1 {
			t.Fatal("replacement lost query_only", err)
		}
		if _, err = conn.ExecContext(context.Background(), "UPDATE representatives SET display_name='unsafe' WHERE id='owner'"); err == nil {
			t.Fatal("reader wrote data")
		}
		// Even accidentally clearing query_only cannot defeat the read-only URI.
		if _, err = conn.ExecContext(context.Background(), "PRAGMA query_only=OFF"); err != nil {
			t.Fatal(err)
		}
		if _, err = conn.ExecContext(context.Background(), "UPDATE representatives SET display_name='unsafe' WHERE id='owner'"); err == nil {
			t.Fatal("mode=ro did not reject write")
		}
		conn.Close()
	}
	owner, err := s.Representative(context.Background(), "owner")
	if err != nil || owner.DisplayName != "Primary owner" {
		t.Fatal("failed reader write changed data", owner, err)
	}
}

func TestBusinessReadPoolBusyReadersCancelWithoutWriterFallback(t *testing.T) {
	s, session := authAvailabilityStore(t)
	var held []*sql.Conn
	for i := 0; i < 2; i++ {
		c, err := s.readOnly.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
		defer c.Close()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	result, err := s.VerifySession(ctx, session.Token)
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnauthenticated) || result.Token != "" {
		t.Fatal("saturated readers fabricated auth or fell back", result, err)
	}
	var one int
	if err = s.db.QueryRow("SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatal("reader exhaustion consumed writer", err)
	}
	for _, c := range held {
		c.Close()
	}
	result, err = s.VerifySession(context.Background(), session.Token)
	if err != nil || result.User.ID != "owner" {
		t.Fatal("canceled read changed session", err)
	}
}

func TestBusinessReadPoolSessionRevocationAndPermissionsStayCommitted(t *testing.T) {
	s, _ := authAvailabilityStore(t)
	ctx := context.Background()
	if _, err := s.CreateRepresentative(ctx, "read-pool-rep", "Reader rep"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProvisionRepresentativeLogin(ctx, "read-pool-rep", "read-pool-rep", authAvailabilityPassword); err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, "read-pool-rep", authAvailabilityPassword)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.db.Exec(`INSERT INTO representative_controls(representative_id,revision,login_suspended,view_clients,create_clients,renew_clients,edit_clients,manage_clients,manage_groups,view_finance,updated_unix)
 VALUES('read-pool-rep',1,0,1,1,1,1,1,1,1,0)`)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE representative_controls SET view_clients=0 WHERE representative_id='read-pool-rep'"); err != nil {
		t.Fatal(err)
	}
	readctx, cancel := context.WithTimeout(ctx, time.Second)
	before, err := s.VerifySession(readctx, session.Token)
	cancel()
	if err != nil || !before.User.Permissions.ViewClients {
		t.Fatal("reader waited on or exposed uncommitted permission", err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	after, err := s.VerifySession(ctx, session.Token)
	if err != nil || after.User.Permissions.ViewClients {
		t.Fatal("reader cached permission after commit", err)
	}
	tx, err = s.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM auth_sessions WHERE token_sha256=?", digestSession(session.Token)); err != nil {
		t.Fatal(err)
	}
	readctx, cancel = context.WithTimeout(ctx, time.Second)
	before, err = s.VerifySession(readctx, session.Token)
	cancel()
	if err != nil || before.User.ID != session.User.ID {
		t.Fatal("uncommitted logout invalidated session", err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VerifySession(ctx, session.Token); err != nil {
		t.Fatal("rolled back logout invalidated session", err)
	}
	if err = s.Logout(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VerifySession(ctx, session.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("committed logout not enforced", err)
	}
}

func TestBusinessReadPoolNodeProjectionSurvivesHeldWriter(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	ctx := context.Background()
	if _, err := f.s.BindInboundExecutionClients(ctx, f.key, "owner", 501, ix.BindingsInput{EventID: "read-pool-bind-00001", ExpectedRevision: 0, ClientIDs: []string{f.client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Read-only Node projection contention regression"}); err != nil {
		t.Fatal(err)
	}
	f.command(t, "read-pool-activate-00001", "activate")
	b := f.bundle(t)
	epoch := f.birth(t, b, "read-pool-epoch-00001")
	f.ready(t, b, epoch)
	before, err := f.s.ListInboundExecutionNodeChannels(ctx, f.key)
	if err != nil || len(before) != 1 {
		t.Fatal(before, err)
	}
	writer, err := f.s.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	readctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	after, err := f.s.ListInboundExecutionNodeChannels(readctx, f.key)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("busy writer blocked or refreshed Node evidence", before, after, err)
	}
	// Wrong decryption authority must still fail even while the writer is held.
	key := append([]byte(nil), f.key...)
	key[0] ^= 1
	if _, err = f.s.ListInboundExecutionNodeChannels(readctx, key); err == nil {
		t.Fatal("reader accepted unauthenticated observation")
	}
}

func TestBusinessReadPoolCloseAndReopenHasNoOldSessionSnapshot(t *testing.T) {
	s, path := opened(t)
	ctx := context.Background()
	if _, err := s.BootstrapOwner(ctx, authAvailabilityUser, authAvailabilityPassword); err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, authAvailabilityUser, authAvailabilityPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.VerifySession(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.VerifySession(ctx, session.Token); err == nil || errors.Is(err, ErrUnauthenticated) {
		t.Fatal("closed reader fabricated authentication result", err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.Logout(ctx, session.Token); err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.VerifySession(ctx, session.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("reopened reader retained old session", err)
	}
}
