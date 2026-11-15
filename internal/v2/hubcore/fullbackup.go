package hubcore

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Full backup: everything a new server needs to become this HUB, so nodes
// reconnect without a reinstall — the database plus the HUB's own files
// (the CA and certificate the nodes pin, the owner and bot tokens, the
// service settings). It holds private keys, so it only exists encrypted
// (.dkb, crypt.go). Inside: gzip(tar) with etc/<file> entries first and
// v2.sqlite last. A database-only backup is gzip(sqlite); readers accept
// both.

var hubFiles struct {
	dir   string
	names []string
}

// SetHubFiles tells the store which HUB files a full backup carries.
func SetHubFiles(dir string, names ...string) {
	hubFiles.dir, hubFiles.names = dir, names
}

// DefaultHubFiles are the files in /etc/dark-hub2 a full backup carries.
var DefaultHubFiles = []string{"ca.pem", "server.pem", "server.key", "admin-token", "tg-token", "env"}

// FullBackup returns an encrypted full backup.
func (s *Store) FullBackup(ctx context.Context, pass string) ([]byte, error) {
	if len(pass) < 8 {
		return nil, fmt.Errorf("%w: backup password needs 8+ characters", ErrInvalid)
	}
	if hubFiles.dir == "" {
		return nil, fmt.Errorf("%w: this HUB was started without its files directory", ErrInvalid)
	}
	f, err := os.CreateTemp(s.tmpDir(), "snapshot-*.sqlite")
	if err != nil {
		return nil, err
	}
	snap := f.Name()
	f.Close()
	os.Remove(snap)
	defer os.Remove(snap)
	if err := s.SnapshotTo(ctx, snap); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name string, data []byte, mode int64) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(data)), ModTime: time.Now()}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	have := 0
	for _, n := range hubFiles.names {
		data, err := os.ReadFile(filepath.Join(hubFiles.dir, n))
		if err != nil {
			continue // e.g. no bot token
		}
		if err := add("etc/"+n, data, 0o600); err != nil {
			return nil, err
		}
		have++
	}
	if have == 0 {
		return nil, fmt.Errorf("%w: none of the HUB files could be read in %s", ErrInvalid, hubFiles.dir)
	}
	db, err := os.ReadFile(snap)
	if err != nil {
		return nil, err
	}
	if err := add("v2.sqlite", db, 0o600); err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return EncryptBackup(buf.Bytes(), pass)
}

// backupParts opens a gzip backup of either kind: the database stream and,
// for a full backup, the HUB files.
func backupParts(r io.Reader) (db io.Reader, files map[string][]byte, err error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, nil, fmt.Errorf("not a Dark backup: %w", err)
	}
	br := bufio.NewReaderSize(gz, 1024)
	head, _ := br.Peek(512)
	if len(head) < 262 || string(head[257:262]) != "ustar" {
		return br, nil, nil // database-only
	}
	files = map[string][]byte{}
	tr := tar.NewReader(br)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil, nil, errors.New("the full backup has no database")
		}
		if err != nil {
			return nil, nil, fmt.Errorf("damaged backup: %w", err)
		}
		if h.Name == "v2.sqlite" {
			return tr, files, nil
		}
		if name, ok := strings.CutPrefix(h.Name, "etc/"); ok && !strings.ContainsAny(name, "/\\") && name != "" {
			data, err := io.ReadAll(io.LimitReader(tr, 1<<20))
			if err != nil {
				return nil, nil, err
			}
			files[name] = data
		}
	}
}

// IsFullBackup reports whether a plain (decrypted) backup file carries the
// HUB files.
func IsFullBackup(path string) bool {
	in, err := os.Open(path)
	if err != nil {
		return false
	}
	defer in.Close()
	_, files, err := backupParts(in)
	return err == nil && files != nil
}

// RestoreFull restores a full backup onto this server: the database into
// dbPath and the HUB files into etcDir (existing ones are kept with a
// .before-restore suffix). With newIP, the old server's address in the
// service settings is replaced, so the HUB listens on this server.
func RestoreFull(backupPath, dbPath, etcDir, newIP string) ([]string, error) {
	in, err := os.Open(backupPath)
	if err != nil {
		return nil, err
	}
	_, files, err := backupParts(in)
	in.Close()
	if err != nil {
		return nil, err
	}
	if files == nil {
		return nil, fmt.Errorf("this is a database-only backup")
	}
	if err := RestoreBackup(backupPath, dbPath); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(etcDir, 0o700); err != nil {
		return nil, err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	var done []string
	for name, data := range files {
		if name == "env" && newIP != "" {
			data = []byte(replaceHubIP(string(data), newIP))
		}
		p := filepath.Join(etcDir, name)
		if old, err := os.ReadFile(p); err == nil && !bytes.Equal(old, data) {
			_ = os.WriteFile(p+".before-restore-"+stamp, old, 0o600)
		}
		if err := os.WriteFile(p, data, 0o600); err != nil {
			return done, err
		}
		done = append(done, name)
	}
	return done, nil
}

// replaceHubIP swaps the old server address (DARK_HUB2_HOSTS) for newIP in
// the listen and subscription settings. The HUB certificate keeps the old
// address; nodes check it by the fixed name "dark-hub2".
func replaceHubIP(env, newIP string) string {
	old := ""
	for _, l := range strings.Split(env, "\n") {
		if v, ok := strings.CutPrefix(l, "DARK_HUB2_HOSTS="); ok {
			old = strings.Split(strings.TrimSpace(v), ",")[0]
		}
	}
	if old == "" || old == newIP {
		return env
	}
	var out []string
	for _, l := range strings.Split(env, "\n") {
		if !strings.HasPrefix(l, "DARK_HUB2_HOSTS=") {
			l = strings.ReplaceAll(l, old, newIP)
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

// HubUnit is the systemd service a restored HUB runs under.
const HubUnit = `[Unit]
Description=DARK v2 HUB core (node API)
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/opt/dark-v2/dark-hub2 serve -db /var/lib/dark-hub2/v2.sqlite -listen ${DARK_HUB2_LISTEN} -tls-dir /etc/dark-hub2 -hosts ${DARK_HUB2_HOSTS} -sub-listen ${DARK_HUB2_SUB_LISTEN} -sub-base ${DARK_HUB2_SUB_BASE} -tg-owners=${DARK_HUB2_TG_OWNERS}
EnvironmentFile=/etc/dark-hub2/env
Restart=always
RestartSec=2
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/dark-hub2 /etc/dark-hub2 /opt/dark-v2
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
`
