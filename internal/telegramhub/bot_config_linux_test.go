//go:build linux

package telegramhub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func privateOwnerBotDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOwnerBotLABDisabledDoesNoIO(t *testing.T) {
	// A FIFO would block if opened; disabled returns without even checking the
	// path, reporter or LAB gates. The helper reads no token environment at all.
	dir := privateOwnerBotDir(t)
	path := filepath.Join(dir, "blocked-config")
	if err := unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TELEGRAM_BOT_TOKEN", "DO_NOT_READ_OLD_TOKEN")
	t.Setenv("BOT_TOKEN", "DO_NOT_READ_OLD_TOKEN")
	for _, flag := range []string{"", "0"} {
		worker, err := OpenLABOwnerReportBot(LABOwnerReportBotOptions{EnableFlag: flag, ConfigPath: path}, nil)
		if err != nil || worker != nil {
			t.Fatal("disabled worker inspected configuration", err)
		}
	}
	reader := reportReaderFunc(func(context.Context, string) (business.OwnerRepLedgerReport, error) {
		t.Fatal("configuration read a ledger")
		return business.OwnerRepLedgerReport{}, nil
	})
	for _, options := range []LABOwnerReportBotOptions{
		{EnableFlag: "true", LABMode: true, BusinessEnabled: true, ConfigPath: OwnerReportLABConfigPath},
		{EnableFlag: "1", BusinessEnabled: true, ConfigPath: OwnerReportLABConfigPath},
		{EnableFlag: "1", LABMode: true, ConfigPath: OwnerReportLABConfigPath},
		{EnableFlag: "1", LABMode: true, BusinessEnabled: true, ConfigPath: path},
		{EnableFlag: "1", LABMode: true, BusinessEnabled: true, ConfigPath: "/etc/old-hub/config.json"},
	} {
		if worker, err := OpenLABOwnerReportBot(options, reader); err != ErrBotLABConfig || worker != nil {
			t.Fatal("invalid enable gate accepted", err)
		}
	}
}

func ownerBotFileConfigFixture() map[string]any {
	return map[string]any{"version": 1, "purpose": "isolated-pro-lab-owner-reports", "expected_bot_id": int64(123456),
		"owner_user_id": int64(900001), "owner_chat_id": int64(900001), "bot_username": "DarkLabHubBot",
		"token_file": OwnerReportLABTokenPath, "journal_file": OwnerReportLABInboxPath}
}

func TestOwnerBotLABConfigurationRequiresDedicatedPrivateBindings(t *testing.T) {
	config, err := parseOwnerBotFileConfig([]byte(ownerBotJSON(ownerBotFileConfigFixture())))
	if err != nil || config.binding() != ownerBotBinding() {
		t.Fatal("valid configuration rejected", err)
	}
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"version", 2}, {"purpose", "production"}, {"expected_bot_id", 0}, {"expected_bot_id", "123456"},
		{"owner_user_id", -1}, {"owner_chat_id", -900001}, {"bot_username", "@DarkLabHubBot"},
		{"token_file", "/etc/old-hub/token"}, {"token_file", OwnerReportLABTokenPath + "/../token"},
		{"journal_file", "/var/lib/dark-xray-pro/business.sqlite"}, {"endpoint", "http://localhost"},
		{"token", fakeOwnerBotToken}, {"bot_username", nil},
	} {
		value := ownerBotFileConfigFixture()
		value[tc.key] = tc.value
		if _, err := parseOwnerBotFileConfig([]byte(ownerBotJSON(value))); err != ErrBotLABConfig {
			t.Fatal("unsafe configuration accepted", tc.key, err)
		}
	}
	for _, raw := range []string{
		strings.TrimSuffix(ownerBotJSON(ownerBotFileConfigFixture()), "}") + `,"version":1}`,
		ownerBotJSON(ownerBotFileConfigFixture()) + `{}`,
		strings.Repeat(" ", 4097),
	} {
		if _, err := parseOwnerBotFileConfig([]byte(raw)); err != ErrBotLABConfig {
			t.Fatal("ambiguous configuration accepted")
		}
	}
}

func TestOwnerBotLABPrivateFilePolicy(t *testing.T) {
	dir := privateOwnerBotDir(t)
	path := filepath.Join(dir, "token")
	if err := os.WriteFile(path, []byte(fakeOwnerBotToken+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	data, err := readPrivateBotFile(path, 256)
	if err != nil || string(data) != fakeOwnerBotToken+"\n" {
		t.Fatal("private file rejected", err)
	}
	if _, err := readPrivateBotFile(path, 4); err != ErrBotLABConfig {
		t.Fatal("oversized token accepted")
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateBotFile(path, 256); err != ErrBotLABConfig {
		t.Fatal("readable token accepted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateBotFile(link, 256); err != ErrBotLABConfig {
		t.Fatal("symlink accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateBotFile(path, 256); err != ErrBotLABConfig {
		t.Fatal("hard-linked credential accepted")
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(link, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateBotFile(link, 256); err != ErrBotLABConfig {
		t.Fatal("non-regular file accepted")
	}
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateBotFile(path, 256); !errors.Is(err, ErrBotLABConfig) {
		t.Fatal("public directory accepted")
	}
}
