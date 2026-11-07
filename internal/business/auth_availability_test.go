package business

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const authAvailabilityUser = "availability-owner"
const authAvailabilityPassword = "availability-owner-password-2026"

func authAvailabilityStore(t *testing.T) (*Store, LoginSession) {
	t.Helper()
	s, _ := opened(t)
	if _, err := s.BootstrapOwner(context.Background(), authAvailabilityUser, authAvailabilityPassword); err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(context.Background(), authAvailabilityUser, authAvailabilityPassword)
	if err != nil {
		t.Fatal(err)
	}
	return s, session
}

func authAvailabilityCalls(s *Store, token string) map[string]func(*testing.T, context.Context) error {
	return map[string]func(*testing.T, context.Context) error{
		"login": func(t *testing.T, ctx context.Context) error {
			session, err := s.Login(ctx, authAvailabilityUser, authAvailabilityPassword)
			if session.Token != "" || session.User.ID != "" {
				t.Fatal("unavailable login returned an authenticated identity")
			}
			return err
		},
		"session": func(t *testing.T, ctx context.Context) error {
			session, err := s.VerifySession(ctx, token)
			if session.Token != "" || session.User.ID != "" {
				t.Fatal("unavailable verification returned an authenticated identity")
			}
			return err
		},
		"password": func(t *testing.T, ctx context.Context) error {
			return s.RotatePassword(ctx, "owner", authAvailabilityPassword, "replacement-availability-password")
		},
	}
}

func TestAuthAvailabilityBusyConnectionPreservesCancellationAndSession(t *testing.T) {
	s, session := authAvailabilityStore(t)
	// Borrow the writer connection. Login and password mutations still wait;
	// existing session verification uses its separate committed read snapshot.
	conn, err := s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for name, call := range authAvailabilityCalls(s, session.Token) {
		if name == "session" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			err := call(t, ctx)
			if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("blocked %s misclassified its deadline: %v", name, err)
			}
		})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	verifiedWhileBusy, verifyErr := s.VerifySession(ctx, session.Token)
	cancel()
	if verifyErr != nil || verifiedWhileBusy.User.ID != "owner" {
		t.Fatalf("writer contention blocked committed session verification: %v", verifyErr)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	verified, err := s.VerifySession(context.Background(), session.Token)
	if err != nil || verified.User.ID != "owner" {
		t.Fatalf("temporary contention lost the existing session: %v", err)
	}
	if _, err := s.Login(context.Background(), authAvailabilityUser, authAvailabilityPassword); err != nil {
		t.Fatalf("temporary contention changed the password: %v", err)
	}
}

func TestAuthAvailabilityClosedStoreDoesNotRejectCredentials(t *testing.T) {
	s, session := authAvailabilityStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for name, call := range authAvailabilityCalls(s, session.Token) {
		t.Run(name, func(t *testing.T) {
			err := call(t, context.Background())
			if err == nil || errors.Is(err, ErrUnauthenticated) {
				t.Fatalf("closed-store %s misclassified its failure: %v", name, err)
			}
		})
	}
}

func TestAuthAvailabilityMissingCredentialsRemainUnauthenticated(t *testing.T) {
	s, _ := authAvailabilityStore(t)
	if _, err := s.Login(context.Background(), "missing-owner", authAvailabilityPassword); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing username did not retain invalid-credential semantics: %v", err)
	}
	if _, err := s.VerifySession(context.Background(), strings.Repeat("f", 64)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("unknown session did not retain invalid-session semantics: %v", err)
	}
	if err := s.RotatePassword(context.Background(), "missing-owner", authAvailabilityPassword, "replacement-availability-password"); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("missing password identity did not retain invalid-credential semantics: %v", err)
	}
}
