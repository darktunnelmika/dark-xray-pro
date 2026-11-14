package hubcore

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Two-step login with an authenticator app (RFC 6238: SHA-1, 6 digits,
// 30-second steps, one step of clock drift either way). A code is accepted
// once: the last used step is stored, so a code seen over a shoulder or
// in a proxy log cannot be replayed.

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func (s *Store) ensureTOTPSchema() error {
	for _, c := range []string{"totp_pending", "totp_last"} {
		def := "TEXT NOT NULL DEFAULT ''"
		if c == "totp_last" {
			def = "INTEGER NOT NULL DEFAULT 0"
		}
		if err := s.addColumn("admins", c, def); err != nil {
			return err
		}
	}
	return nil
}

func totpAt(secret string, step int64) (string, error) {
	key, err := b32.DecodeString(strings.ToUpper(secret))
	if err != nil {
		return "", err
	}
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, key)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000), nil
}

// totpMatch returns the step the code belongs to, or 0.
func totpMatch(secret, code string, now time.Time) int64 {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if len(code) != 6 {
		return 0
	}
	cur := now.Unix() / 30
	for _, st := range []int64{cur, cur - 1, cur + 1} {
		if want, err := totpAt(secret, st); err == nil && hmac.Equal([]byte(want), []byte(code)) {
			return st
		}
	}
	return 0
}

// TOTPSetup starts enrolment: a fresh secret is kept pending until a code
// from the app confirms it. Returns the otpauth:// URI for the QR code.
func (s *Store) TOTPSetup(ctx context.Context, user string) (secret, uri string, err error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	secret = b32.EncodeToString(raw)
	res, err := s.db.ExecContext(ctx, `UPDATE admins SET totp_pending=? WHERE username=?`, secret, user)
	if err != nil {
		return "", "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", "", ErrNotFound
	}
	label := url.PathEscape("Dark Xray Pro:" + user)
	uri = "otpauth://totp/" + label + "?secret=" + secret + "&issuer=" + url.QueryEscape("Dark Xray Pro") + "&digits=6&period=30"
	return secret, uri, nil
}

// TOTPEnable confirms the pending secret with a code from the app.
func (s *Store) TOTPEnable(ctx context.Context, user, code string) error {
	var pending string
	if err := s.ro.QueryRowContext(ctx, `SELECT totp_pending FROM admins WHERE username=?`, user).Scan(&pending); err != nil || pending == "" {
		return fmt.Errorf("%w: start the setup first", ErrInvalid)
	}
	st := totpMatch(pending, code, s.now())
	if st == 0 {
		return fmt.Errorf("%w: the code does not match — check the phone's clock and try the next code", ErrInvalid)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE admins SET totp_secret=totp_pending, totp_pending='', totp_last=? WHERE username=?`, st, user)
	return err
}

// TOTPDisable turns two-step login off after checking the password and a code.
func (s *Store) TOTPDisable(ctx context.Context, user, pass, code string) error {
	secret, last, err := s.verifyPassword(ctx, user, pass)
	if err != nil {
		return err
	}
	if secret == "" {
		return nil
	}
	if err := s.checkTOTP(ctx, user, secret, last, code); err != nil {
		return err
	}
	return s.ClearTOTP(ctx, user)
}

// ClearTOTP removes two-step login (owner reset or server-side recovery).
func (s *Store) ClearTOTP(ctx context.Context, user string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE admins SET totp_secret='', totp_pending='', totp_last=0 WHERE username=?`, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// checkTOTP verifies the second step for a login that has it on.
func (s *Store) checkTOTP(ctx context.Context, user, secret string, last int64, code string) error {
	if secret == "" {
		return nil
	}
	if strings.TrimSpace(code) == "" {
		return ErrTOTPRequired
	}
	st := totpMatch(secret, code, s.now())
	if st == 0 || st <= last {
		return ErrAuth
	}
	// Claim the step atomically so a concurrent replay loses.
	res, err := s.db.ExecContext(ctx, `UPDATE admins SET totp_last=? WHERE username=? AND totp_last<?`, st, user, st)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrAuth
	}
	return nil
}
