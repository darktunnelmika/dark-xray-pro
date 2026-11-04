//go:build linux

package telegrambot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

type botConfig struct {
	BotID    int64  `json:"expected_bot_id"`
	Token    string `json:"token"`
	Username string `json:"username,omitempty"`
}
type fileConfig struct {
	Version  int         `json:"version"`
	Purpose  string      `json:"purpose"`
	Revision int64       `json:"revision,omitempty"`
	Bots     []botConfig `json:"bots"`
}

func parseConfig(raw []byte) (fileConfig, error) {
	var c fileConfig
	var fields map[string]json.RawMessage
	if len(raw) > 16<<10 || decodeJSON(raw, &fields) != nil || required(fields, "version", &c.Version) != nil || required(fields, "purpose", &c.Purpose) != nil || c.Purpose != ConfigPurpose || required(fields, "bots", &c.Bots) != nil || c.Bots == nil || len(c.Bots) > maxBots {
		return fileConfig{}, ErrConfig
	}
	switch c.Version {
	case 1:
		if len(fields) != 3 || len(c.Bots) == 0 {
			return fileConfig{}, ErrConfig
		}
	case 2:
		if len(fields) != 4 || required(fields, "revision", &c.Revision) != nil || c.Revision < 1 || c.Revision > maxManagedInteger {
			return fileConfig{}, ErrConfig
		}
	default:
		return fileConfig{}, ErrConfig
	}
	var entries []map[string]json.RawMessage
	if json.Unmarshal(fields["bots"], &entries) != nil {
		return fileConfig{}, ErrConfig
	}
	seen := map[int64]bool{}
	for i, b := range c.Bots {
		if b.BotID <= 0 || seen[b.BotID] || !tokenPattern.MatchString(b.Token) {
			return fileConfig{}, ErrConfig
		}
		if c.Version == 1 {
			if len(entries[i]) != 2 {
				return fileConfig{}, ErrConfig
			}
		} else {
			if b.BotID > maxManagedInteger || len(entries[i]) < 2 || len(entries[i]) > 3 {
				return fileConfig{}, ErrConfig
			}
			for key := range entries[i] {
				if key != "expected_bot_id" && key != "token" && key != "username" {
					return fileConfig{}, ErrConfig
				}
			}
			if _, present := entries[i]["username"]; present && (required(entries[i], "username", &c.Bots[i].Username) != nil || !botUsernamePattern.MatchString(c.Bots[i].Username)) {
				return fileConfig{}, ErrConfig
			}
		}
		seen[b.BotID] = true
	}
	return c, nil
}

// CheckLABConfig validates only the fixed private LAB configuration and returns
// its bot count. It does not open a worker, journal or lock, or perform network
// I/O. The caller is responsible for enforcing the LAB mode and service user.
func CheckLABConfig() (int, error) {
	return checkLABConfig(LABConfigPath)
}

func checkLABConfig(path string) (int, error) {
	raw, err := readPrivate(path, 16<<10)
	if err != nil {
		return 0, ErrConfig
	}
	// Best effort: JSON decoding may also allocate strings containing secrets.
	defer clear(raw)
	config, err := parseConfig(raw)
	if err != nil {
		return 0, ErrConfig
	}
	count := len(config.Bots)
	clear(config.Bots)
	return count, nil
}

// Open performs no network I/O. A disabled worker returns before inspecting
// files, credentials or the backend. Tokens never enter business state/journal.
func Open(options Options, backend Backend) (*Worker, error) {
	if options.EnableFlag == "" || options.EnableFlag == "0" {
		return nil, nil
	}
	if options.EnableFlag != "1" || !options.LABMode || options.ConfigPath != LABConfigPath || backend == nil {
		return nil, ErrConfig
	}
	raw, err := readPrivate(options.ConfigPath, 16<<10)
	if err != nil {
		return nil, ErrConfig
	}
	config, err := parseConfig(raw)
	if err != nil {
		return nil, err
	}
	return openWorker(config, LABJournalPath, backend, nil)
}

// transport is injected only by tests. Production always enters through Open.
func openWorker(config fileConfig, path string, backend Backend, transport http.RoundTripper) (*Worker, error) {
	if backend == nil {
		return nil, ErrConfig
	}
	raw, _ := json.Marshal(config)
	if _, err := parseConfig(raw); err != nil {
		return nil, err
	}
	apis := map[int64]*api{}
	ids := []int64{}
	for _, b := range config.Bots {
		a, err := newAPI(b.BotID, b.Token, transport)
		if err != nil {
			for _, a := range apis {
				a.close()
			}
			return nil, err
		}
		apis[b.BotID] = a
		ids = append(ids, b.BotID)
	}
	j, err := openJournal(path, ids)
	if err != nil {
		for _, a := range apis {
			a.close()
		}
		return nil, err
	}
	return &Worker{backend: backend, journal: j, apis: apis, done: make(chan struct{}), statuses: map[int64]BotStatus{}}, nil
}

func privateRegular(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Mode().Perm()&0111 != 0 {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid()) && stat.Nlink == 1
}
func privateParent(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrConfig
	}
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrConfig
		}
		if p == filepath.Dir(path) {
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm()&0077 != 0 {
				return ErrConfig
			}
		}
		if p == "/" {
			break
		}
	}
	return nil
}
func openPrivate(path string, create bool) (*os.File, bool, error) {
	if privateParent(path) != nil {
		return nil, false, ErrConfig
	}
	flags := syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_NONBLOCK
	if create {
		flags = syscall.O_RDWR | syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_NONBLOCK
	}
	created := false
	var fd int
	var err error
	if create {
		fd, err = syscall.Open(path, flags|syscall.O_CREAT|syscall.O_EXCL, 0600)
		created = err == nil
		if errors.Is(err, syscall.EEXIST) {
			fd, err = syscall.Open(path, flags, 0)
		}
	} else {
		fd, err = syscall.Open(path, flags, 0)
	}
	if err != nil {
		return nil, false, ErrConfig
	}
	f := os.NewFile(uintptr(fd), "private-telegram-file")
	info, err := f.Stat()
	if err != nil || !privateRegular(info) {
		f.Close()
		return nil, false, ErrConfig
	}
	return f, created, nil
}
func readPrivate(path string, limit int64) ([]byte, error) {
	return readPrivateWith(path, limit, io.ReadAll)
}

// The reader argument permits deterministic tests of changes during a read;
// production always uses io.ReadAll. These checks compare observed snapshots,
// rather than excluding a concurrent privileged or same-user writer.
func readPrivateWith(path string, limit int64, read func(io.Reader) ([]byte, error)) ([]byte, error) {
	f, _, err := openPrivate(path, false)
	if err != nil {
		return nil, ErrConfig
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !privateRegular(before) || before.Size() < 1 || before.Size() > limit {
		return nil, ErrConfig
	}
	data, err := read(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) != before.Size() || int64(len(data)) > limit {
		clear(data)
		return nil, ErrConfig
	}
	after, fdErr := f.Stat()
	current, pathErr := os.Lstat(path)
	if fdErr != nil || pathErr != nil || !samePrivateFileState(before, after) || !samePrivateFileState(after, current) || privateParent(path) != nil {
		clear(data)
		return nil, ErrConfig
	}
	return data, nil
}

func samePrivateFileState(a, b os.FileInfo) bool {
	if !privateRegular(a) || !privateRegular(b) {
		return false
	}
	x, xok := a.Sys().(*syscall.Stat_t)
	y, yok := b.Sys().(*syscall.Stat_t)
	return xok && yok && x.Dev == y.Dev && x.Ino == y.Ino &&
		x.Uid == y.Uid && x.Gid == y.Gid && x.Mode == y.Mode &&
		x.Nlink == y.Nlink && x.Size == y.Size &&
		x.Mtim == y.Mtim && x.Ctim == y.Ctim
}

// ValidateOfflineBackup inspects only a retained private snapshot, never the
// live paths or Telegram. Empty journalPath is valid for a configured bot that
// has not been enabled. Snapshot must be fenced by the journal .lock.
func ValidateOfflineBackup(ctx context.Context, journalPath string, configRaw []byte) error {
	if ctx == nil {
		return ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	config, err := parseConfig(configRaw)
	if err != nil {
		return err
	}
	if journalPath == "" {
		if config.Version == 2 {
			return ErrJournal
		}
		return nil
	}
	if privateParent(journalPath) != nil {
		return ErrJournal
	}
	for _, suffix := range []string{"-journal", "-wal", "-shm"} {
		if _, err := os.Lstat(journalPath + suffix); !errors.Is(err, os.ErrNotExist) {
			return ErrJournal
		}
	}
	f, _, err := openPrivate(journalPath, false)
	if err != nil {
		return ErrJournal
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || before.Size() < 1 || before.Size() > maxJournalBytes {
		return ErrJournal
	}
	u := url.URL{Scheme: "file", Path: journalPath, RawQuery: "mode=ro&immutable=1&_pragma=query_only(1)&_pragma=trusted_schema(0)&_pragma=foreign_keys(1)&_pragma=busy_timeout(1000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return ErrJournal
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ids := make([]int64, 0, len(config.Bots))
	for _, b := range config.Bots {
		ids = append(ids, b.BotID)
	}
	if err = validateJournal(ctx, db, ids); err != nil {
		return err
	}
	if len(config.Bots) == 0 {
		known, err := knownBotIDs(ctx, db)
		if err != nil || len(known) != 0 {
			return ErrJournal
		}
	}
	var integrity string
	if db.QueryRowContext(ctx, "PRAGMA integrity_check(1)").Scan(&integrity) != nil || integrity != "ok" {
		return ErrJournal
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return ErrJournal
	}
	bad := rows.Next()
	err = rows.Err()
	rows.Close()
	if bad || err != nil {
		return ErrJournal
	}
	// No writable open, schema initialization or dispatch recovery occurs here.
	rows, err = db.QueryContext(ctx, "SELECT bot_id,chat_id,kind,state,payload_json,access_json,anonymous FROM bot_outbox")
	if err != nil {
		return ErrJournal
	}
	for rows.Next() {
		var bot, chat int64
		var kind, state string
		var payload, scope []byte
		var anonymous int
		var a Access
		if rows.Scan(&bot, &chat, &kind, &state, &payload, &scope, &anonymous) != nil || decodeJSON(scope, &a) != nil || a.Identity.BotID != bot || a.Identity.UserID != chat || a.Identity.ChatID != chat || (anonymous == 0 && !a.valid()) || (anonymous == 1 && kind != "checkout" && kind != "callback_answer") {
			rows.Close()
			return ErrJournal
		}
		if (state == "pending" || state == "dispatching" || state == "unknown") && (len(payload) == 0 || len(payload) > 64<<10) {
			rows.Close()
			return ErrJournal
		}
		if len(payload) > 0 {
			var value any
			if decodeJSON(payload, &value) != nil {
				rows.Close()
				return ErrJournal
			}
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return ErrJournal
	}
	after, err := os.Lstat(journalPath)
	if err != nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) || strings.Contains(journalPath, "\x00") {
		return ErrJournal
	}
	return ctx.Err()
}
