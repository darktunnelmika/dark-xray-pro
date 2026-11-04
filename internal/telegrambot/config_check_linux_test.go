//go:build linux

package telegrambot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func checkConfigFixture(t *testing.T, count int) []byte {
	t.Helper()
	config := fileConfig{Version: 1, Purpose: ConfigPurpose, Bots: []botConfig{}}
	for i := 0; i < count; i++ {
		id := int64(100 + i)
		config.Bots = append(config.Bots, botConfig{BotID: id, Token: fmt.Sprintf("%d:ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789", id)})
	}
	raw, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func writeCheckConfig(t *testing.T, raw []byte) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCheckLABConfigCountsWithoutCreatingState(t *testing.T) {
	for _, count := range []int{1, 2, maxBots} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			raw := checkConfigFixture(t, count)
			path := writeCheckConfig(t, raw)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			got, err := checkLABConfig(path)
			if err != nil || got != count {
				t.Fatalf("count=%d error=%v", got, err)
			}
			after, err := os.Stat(path)
			if err != nil || !samePrivateFileState(before, after) {
				t.Fatalf("configuration metadata changed: %v", err)
			}
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(current, raw) {
				t.Fatalf("configuration contents changed: %v", err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
				t.Fatalf("checker created state: entries=%v error=%v", entries, err)
			}
		})
	}
}

func TestCheckLABConfigRejectsInvalidAndAmbiguousJSON(t *testing.T) {
	valid := checkConfigFixture(t, 1)
	duplicateID := bytes.Replace(checkConfigFixture(t, 2), []byte(`"expected_bot_id":101`), []byte(`"expected_bot_id":100`), 1)
	for name, raw := range map[string][]byte{
		"empty":             nil,
		"malformed":         []byte(`{"token":"` + syntheticToken),
		"wrong_purpose":     bytes.Replace(valid, []byte(ConfigPurpose), []byte("wrong-purpose"), 1),
		"unknown_field":     append([]byte(`{"extra":true,`), valid[1:]...),
		"missing_version":   bytes.Replace(valid, []byte(`"version":1,`), nil, 1),
		"duplicate_field":   append([]byte(`{"version":1,`), valid[1:]...),
		"duplicate_bot_id":  duplicateID,
		"duplicate_bot_key": bytes.Replace(valid, []byte(`"expected_bot_id":100`), []byte(`"expected_bot_id":100,"expected_bot_id":100`), 1),
		"invalid_token":     bytes.Replace(valid, []byte(syntheticToken), []byte("invalid-token"), 1),
		"no_bots":           checkConfigFixture(t, 0),
		"too_many_bots":     checkConfigFixture(t, maxBots+1),
		"oversize":          append(append([]byte(nil), valid...), bytes.Repeat([]byte(" "), (16<<10)+1-len(valid))...),
	} {
		t.Run(name, func(t *testing.T) {
			path := writeCheckConfig(t, raw)
			count, err := checkLABConfig(path)
			if count != 0 || err != ErrConfig {
				t.Fatalf("count=%d error=%v; want zero and redacted ErrConfig", count, err)
			}
		})
	}
	path := writeCheckConfig(t, append(append([]byte(nil), valid...), bytes.Repeat([]byte(" "), (16<<10)-len(valid))...))
	if count, err := checkLABConfig(path); count != 1 || err != nil {
		t.Fatalf("exact size limit: count=%d error=%v", count, err)
	}
}

func TestCheckLABConfigRejectsUnsafeFiles(t *testing.T) {
	for name, prepare := range map[string]func(string) error{
		"group_readable": func(path string) error { return os.Chmod(path, 0640) },
		"executable":     func(path string) error { return os.Chmod(path, 0700) },
		"shared_parent":  func(path string) error { return os.Chmod(filepath.Dir(path), 0750) },
		"hardlink":       func(path string) error { return os.Link(path, path+".other") },
		"missing":        os.Remove,
		"directory": func(path string) error {
			if err := os.Remove(path); err != nil {
				return err
			}
			return os.Mkdir(path, 0700)
		},
		"symlink": func(path string) error {
			if err := os.Rename(path, path+".target"); err != nil {
				return err
			}
			return os.Symlink(path+".target", path)
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeCheckConfig(t, checkConfigFixture(t, 1))
			if err := prepare(path); err != nil {
				t.Fatal(err)
			}
			if count, err := checkLABConfig(path); count != 0 || err != ErrConfig {
				t.Fatalf("count=%d error=%v; want zero and redacted ErrConfig", count, err)
			}
		})
	}
	t.Run("symlink_ancestor", func(t *testing.T) {
		path := writeCheckConfig(t, checkConfigFixture(t, 1))
		link := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(filepath.Dir(path), link); err != nil {
			t.Fatal(err)
		}
		if count, err := checkLABConfig(filepath.Join(link, "config.json")); count != 0 || err != ErrConfig {
			t.Fatalf("count=%d error=%v", count, err)
		}
	})
}

func TestReadPrivateRejectsChangedFiles(t *testing.T) {
	for name, mutate := range map[string]func(string, []byte) error{
		"private_mode_change": func(path string, _ []byte) error { return os.Chmod(path, 0400) },
		"public_mode_change":  func(path string, _ []byte) error { return os.Chmod(path, 0640) },
		"new_hardlink":        func(path string, _ []byte) error { return os.Link(path, path+".other") },
		"parent_mode_change":  func(path string, _ []byte) error { return os.Chmod(filepath.Dir(path), 0750) },
		"truncate":            func(path string, _ []byte) error { return os.Truncate(path, 1) },
		"mtime_change": func(path string, _ []byte) error {
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			changed := info.ModTime().Add(time.Hour)
			return os.Chtimes(path, changed, changed)
		},
		"same_size_replacement": func(path string, raw []byte) error {
			if err := os.WriteFile(path+".new", raw, 0600); err != nil {
				return err
			}
			return os.Rename(path+".new", path)
		},
		"symlink_replacement": func(path string, _ []byte) error {
			if err := os.Rename(path, path+".target"); err != nil {
				return err
			}
			return os.Symlink(path+".target", path)
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeCheckConfig(t, checkConfigFixture(t, 1))
			var observed []byte
			raw, err := readPrivateWith(path, 16<<10, func(r io.Reader) ([]byte, error) {
				data, err := io.ReadAll(r)
				if err != nil {
					t.Fatal(err)
				}
				if err := mutate(path, data); err != nil {
					t.Fatal(err)
				}
				observed = data
				return data, nil
			})
			if raw != nil || err != ErrConfig {
				t.Fatalf("accepted changed file: data length=%d error=%v", len(raw), err)
			}
			if !bytes.Equal(observed, make([]byte, len(observed))) {
				t.Fatal("rejected read buffer was not cleared")
			}
		})
	}
}

func TestReadPrivateRedactsReadFailure(t *testing.T) {
	path := writeCheckConfig(t, checkConfigFixture(t, 1))
	var observed []byte
	raw, err := readPrivateWith(path, 16<<10, func(r io.Reader) ([]byte, error) {
		observed, _ = io.ReadAll(r)
		return observed, errors.New("read failed: " + path + " " + syntheticToken)
	})
	if raw != nil || err != ErrConfig || strings.Contains(err.Error(), syntheticToken) || strings.Contains(err.Error(), path) {
		t.Fatalf("read failure leaked information: data length=%d error=%v", len(raw), err)
	}
	if !bytes.Equal(observed, make([]byte, len(observed))) {
		t.Fatal("failed read buffer was not cleared")
	}
}
