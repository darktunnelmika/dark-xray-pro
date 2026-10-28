package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
)

func privateTestDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if err := os.Chmod(d, 0700); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestRecoveryKeyCreationNeverEchoesOrOverwritesPrivateIdentity(t *testing.T) {
	d := privateTestDir(t)
	path := filepath.Join(d, "recovery-key")
	var out, diagnostic bytes.Buffer
	if code := execute(context.Background(), []string{"keygen", "--out", path}, &out, &diagnostic); code != 0 {
		t.Fatalf("keygen code=%d diagnostic=%s", code, diagnostic.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := age.ParseX25519Identity(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatal("invalid generated recovery identity")
	}
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("recovery identity permissions")
	}
	if bytes.Contains(out.Bytes(), bytes.TrimSpace(raw)) || bytes.Contains(diagnostic.Bytes(), bytes.TrimSpace(raw)) {
		t.Fatal("private recovery identity escaped into output")
	}
	var result map[string]string
	if json.Unmarshal(out.Bytes(), &result) != nil || result["recipient"] != identity.Recipient().String() {
		t.Fatal("public recipient result")
	}
	out.Reset()
	diagnostic.Reset()
	if code := execute(context.Background(), []string{"keygen", "--out", path}, &out, &diagnostic); code != 3 {
		t.Fatal("existing recovery key was not refused", code)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) {
		t.Fatal("existing recovery key changed")
	}
}

func TestRecoveryKeyRejectsPublicDirectoriesSymlinksAndHardlinks(t *testing.T) {
	d := privateTestDir(t)
	public := filepath.Join(d, "public")
	if os.Mkdir(public, 0755) != nil {
		t.Fatal("mkdir")
	}
	if err := os.Chmod(public, 0755); err != nil {
		t.Fatal(err)
	}
	if err := publishFile(filepath.Join(public, "key"), func(f *os.File) error { _, err := f.WriteString("secret"); return err }); err == nil {
		t.Fatal("public output directory accepted")
	}
	target := filepath.Join(d, "target")
	if err := os.WriteFile(target, []byte("retain"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(d, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := publishFile(link, func(*os.File) error { t.Fatal("write reached symlink"); return nil }); err == nil {
		t.Fatal("symlink publication accepted")
	}
	if _, err := openRegular(link, true, 4096); err == nil {
		t.Fatal("symlink secret accepted")
	}
	hard := filepath.Join(d, "hardlink")
	if err := os.Link(target, hard); err != nil {
		t.Fatal(err)
	}
	if _, err := openRegular(hard, true, 4096); err == nil {
		t.Fatal("hardlinked secret accepted")
	}
	alias := filepath.Join(d, "alias")
	if err := os.Symlink(d, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := openRegular(filepath.Join(alias, "target"), false, 4096); err == nil {
		t.Fatal("symlink ancestor accepted")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "retain" {
		t.Fatal("protected existing target changed")
	}
}

func TestArchivePublicationIsAtomicAndDoesNotClobberARacingDestination(t *testing.T) {
	d := privateTestDir(t)
	path := filepath.Join(d, "backup.age")
	if err := publishFile(path, func(f *os.File) error {
		if _, err := f.WriteString("partial"); err != nil {
			return err
		}
		return errors.New("injected disk or encryption interruption")
	}); err == nil {
		t.Fatal("injected interruption ignored")
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial archive published")
	}
	entries, err := os.ReadDir(d)
	if err != nil || len(entries) != 0 {
		t.Fatal("failed private temporary file retained")
	}
	if err := publishFile(path, func(f *os.File) error {
		if _, err := f.WriteString("our complete archive"); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("concurrent creator"), 0600)
	}); err == nil {
		t.Fatal("racing output clobbered")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "concurrent creator" {
		t.Fatal("concurrent output changed")
	}
}

func TestCLIRejectsUnboundedLimitsAndNeverEchoesSecretArguments(t *testing.T) {
	for _, value := range []string{"0", "-1", "1099511627777", "9223372036854775807"} {
		if _, err := arguments([]string{"verify", "--in", "/in", "--identity-file", "/key", "--max-bytes", value}); err == nil {
			t.Fatal("invalid maximum accepted", value)
		}
	}
	secret := "AGE-SECRET-KEY-DO-NOT-ECHO"
	var out, diagnostic bytes.Buffer
	if code := execute(context.Background(), []string{"restore", "--identity", secret}, &out, &diagnostic); code != 2 {
		t.Fatal("secret command-line key accepted", code)
	}
	if strings.Contains(out.String()+diagnostic.String(), secret) {
		t.Fatal("unknown argument echoed")
	}
}

func TestPublicationRefusesMovedParentAndCannotFollowItsReplacement(t *testing.T) {
	d := privateTestDir(t)
	parent, moved, replacement := filepath.Join(d, "parent"), filepath.Join(d, "moved"), filepath.Join(d, "replacement")
	if os.Mkdir(parent, 0700) != nil || os.Mkdir(replacement, 0700) != nil {
		t.Fatal("mkdir")
	}
	err := publishFile(filepath.Join(parent, "key"), func(f *os.File) error {
		if _, err := f.WriteString("private identity"); err != nil {
			return err
		}
		if err := os.Rename(parent, moved); err != nil {
			return err
		}
		return os.Symlink(replacement, parent)
	})
	if err == nil {
		t.Fatal("moved destination parent accepted")
	}
	for _, path := range []string{moved, replacement} {
		files, err := os.ReadDir(path)
		if err != nil || len(files) != 0 {
			t.Fatal("temporary or published secret escaped the pinned parent cleanup")
		}
	}
}
