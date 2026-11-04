package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"testing"
	"time"
)

func TestTelegramConfigCheckIdentityAndRedaction(t *testing.T) {
	const secret = "do-not-print-telegram-config-sentinel"
	for _, tc := range []struct {
		name    string
		args    []string
		lab     string
		uid     int
		gid     int
		account *user.User
		err     error
		count   int
		want    string
		exit    int
		checks  int
	}{
		{name: "argument", args: []string{secret}, want: "invalid_arguments", exit: 2},
		{name: "lab missing", want: "lab_mode_required", exit: 2},
		{name: "lab malformed", lab: secret, want: "lab_mode_required", exit: 2},
		{name: "root", lab: "1", gid: 988, want: "service_identity_required", exit: 2},
		{name: "root group", lab: "1", uid: 999, want: "service_identity_required", exit: 2},
		{name: "missing account", lab: "1", uid: 999, gid: 988, want: "service_identity_required", exit: 2},
		{name: "wrong user", lab: "1", uid: 999, gid: 988, account: &user.User{Username: "other", Uid: "999", Gid: "988"}, want: "service_identity_required", exit: 2},
		{name: "wrong uid", lab: "1", uid: 999, gid: 988, account: &user.User{Username: "darkprolab", Uid: "1000", Gid: "988"}, want: "service_identity_required", exit: 2},
		{name: "wrong primary group", lab: "1", uid: 999, gid: 988, account: &user.User{Username: "darkprolab", Uid: "999", Gid: "999"}, want: "service_identity_required", exit: 2},
		{name: "invalid account id", lab: "1", uid: 999, gid: 988, account: &user.User{Username: "darkprolab", Uid: secret, Gid: "988"}, want: "service_identity_required", exit: 2},
		{name: "config refused", lab: "1", uid: 999, gid: 988, account: &user.User{Username: "darkprolab", Uid: "999", Gid: "988"}, err: errors.New(secret), want: "private_config_refused", exit: 1, checks: 1},
		{name: "initialized empty panel config", lab: "1", uid: 999, gid: 988, account: &user.User{Username: "darkprolab", Uid: "999", Gid: "988"}, want: "private_config_valid", checks: 1},
		{name: "oversized count refused", lab: "1", uid: 999, gid: 988, account: &user.User{Username: "darkprolab", Uid: "999", Gid: "988"}, count: 17, want: "private_config_refused", exit: 1, checks: 1},
		{name: "valid two store config", lab: "1", uid: 999, gid: 988, account: &user.User{Username: "darkprolab", Uid: "999", Gid: "988"}, count: 2, want: "private_config_valid", checks: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checks := 0
			hooks := telegramConfigCheckHooks{
				getenv: func(key string) string {
					if key != "DARK_PRO_LAB_MODE" {
						t.Fatalf("unexpected environment read: %s", key)
					}
					return tc.lab
				},
				euid: func() int { return tc.uid }, egid: func() int { return tc.gid },
				lookupUser: func(name string) (*user.User, error) {
					if name != "darkprolab" {
						t.Fatal("unexpected identity lookup")
					}
					return tc.account, nil
				},
				check: func() (int, error) { checks++; return tc.count, tc.err },
			}
			var output bytes.Buffer
			if exit := runTelegramConfigCheck(tc.args, &output, hooks); exit != tc.exit {
				t.Fatalf("exit %d, expected %d", exit, tc.exit)
			}
			if checks != tc.checks || strings.Contains(output.String(), secret) {
				t.Fatalf("invalid check count or sensitive output: checks=%d", checks)
			}
			var report telegramConfigCheckReport
			if err := json.Unmarshal(output.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Code != tc.want || report.Scope != "private_config_only" || report.Activation != "not_performed" {
				t.Fatalf("unexpected report: %+v", report)
			}
			if len(report.Acceptance) != 6 {
				t.Fatal("missing acceptance limits")
			}
			for key, value := range report.Acceptance {
				if value != "unknown" {
					t.Fatalf("claimed live acceptance: %s", key)
				}
			}
			if tc.exit == 0 {
				if report.Status != "passed" || report.ConfiguredBots == nil || *report.ConfiguredBots != tc.count {
					t.Fatal("missing bounded config result")
				}
			} else if report.Status != "blocked" || report.ConfiguredBots != nil {
				t.Fatal("failed check reported configuration evidence")
			}
		})
	}
}

type telegramConfigBrokenWriter struct{}

func (telegramConfigBrokenWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestTelegramConfigCheckOutputFailure(t *testing.T) {
	if exit := runTelegramConfigCheck([]string{"refused"}, telegramConfigBrokenWriter{}, telegramConfigCheckHooks{}); exit != 2 {
		t.Fatalf("output failure returned %d", exit)
	}
}

// Invoke the actual main entrypoint in a child. The early command and positional
// guard must finish before startup can write any normal HUB state or bind ports.
func TestTelegramConfigCheckEntrypoint(t *testing.T) {
	const marker = "DARK_PRO_TEST_CONFIG_CHECK_CHILD"
	if mode := os.Getenv(marker); mode != "" {
		switch mode {
		case "check":
			os.Args = []string{"dark-pro-hub", "check-telegram-config"}
		case "extra":
			os.Args = []string{"dark-pro-hub", "check-telegram-config", "never-print-this-sentinel"}
		case "typo":
			os.Args = []string{"dark-pro-hub", "check-telegram-config-never-print-this-sentinel"}
		case "misplaced":
			os.Args = []string{"dark-pro-hub", "-listen", "127.0.0.1:0", "check-telegram-config"}
		default:
			os.Exit(91)
		}
		main()
		os.Exit(92)
	}
	for _, tc := range []struct {
		mode string
		want string
		exit int
	}{
		{"check", `"code":"lab_mode_required"`, 2},
		{"extra", `"code":"invalid_arguments"`, 2},
		{"typo", "unexpected positional arguments; no HUB startup performed", 1},
		{"misplaced", "unexpected positional arguments; no HUB startup performed", 1},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			cwd := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTelegramConfigCheckEntrypoint$")
			cmd.Dir = cwd
			cmd.Env = append(os.Environ(), marker+"="+tc.mode, "DARK_PRO_LAB_MODE=", "DARK_PRO_ENABLE_TELEGRAM_BOTS_V1=invalid-before-runtime")
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != tc.exit {
				t.Fatalf("entrypoint did not exit safely: %v", err)
			}
			if !bytes.Contains(out, []byte(tc.want)) || bytes.Contains(out, []byte("never-print-this-sentinel")) {
				t.Fatalf("unexpected redacted entrypoint output: %s", out)
			}
			entries, err := os.ReadDir(cwd)
			if err != nil || len(entries) != 0 {
				t.Fatal("entrypoint created HUB state")
			}
		})
	}
}
