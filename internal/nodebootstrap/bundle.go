// Package nodebootstrap exports only the exact release-matched NEW PRO Node
// Agent binary and its reviewed installer; it never includes any identities,
// Pair Codes, financial records, tunnels or credentials.
package nodebootstrap

import (
	"archive/zip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var ErrUnsafe = errors.New("unsafe or unavailable matching PRO Agent binary")

const MaxAgentBytes int64 = 128 << 20

//go:embed install-new-pro-node.sh
var files embed.FS

type Bundle struct {
	path   string
	digest string
	size   int64
}

func Installer() ([]byte, error) {
	return files.ReadFile("install-new-pro-node.sh")
}

func checkedFile(path string) (*os.File, os.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Base(path) != "dark-pro-agent" || filepath.Clean(path) != path {
		return nil, nil, ErrUnsafe
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() == 0 ||
		info.Size() > MaxAgentBytes {
		return nil, nil, ErrUnsafe
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() != info.Size() {
		f.Close()
		return nil, nil, ErrUnsafe
	}
	return f, info, nil
}

// Open locks package identity to the binary reviewed and installed together
// with the running HUB release. It does not accept caller-provided HTTP paths.
func Open(path string) (*Bundle, error) {
	f, info, err := checkedFile(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, f); err != nil {
		return nil, err
	}
	if _, err := Installer(); err != nil {
		return nil, err
	}
	return &Bundle{path: path, digest: hex.EncodeToString(sum.Sum(nil)), size: info.Size()}, nil
}

func (b *Bundle) AgentSHA256() string {
	if b == nil {
		return ""
	}
	return b.digest
}

func add(w *zip.Writer, name string, data io.Reader, mode os.FileMode) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	file, err := w.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(file, data)
	return err
}

// Download is invoked only after authenticated owner browser authorization.
// Never call it from an Agent HTTP handler or from unauthenticated static paths.
func (b *Bundle) Download(w http.ResponseWriter) error {
	if b == nil {
		return ErrUnsafe
	}
	f, info, err := checkedFile(b.path)
	if err != nil {
		return err
	}
	defer f.Close()
	if info.Size() != b.size {
		return ErrUnsafe
	}
	actual := sha256.New()
	if _, err = io.Copy(actual, f); err != nil {
		return err
	}
	if hex.EncodeToString(actual.Sum(nil)) != b.digest {
		return ErrUnsafe
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	script, err := Installer()
	if err != nil {
		return err
	}
	scriptSHA := sha256.Sum256(script)
	manifest := fmt.Sprintf("%s  dark-pro-agent\n%x  install-new-pro-node.sh\n", b.digest, scriptSHA)

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", `attachment; filename="dark-pro-new-node-installer.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	archive := zip.NewWriter(w)
	if err = add(archive, "dark-pro-agent", f, 0755); err != nil {
		return err
	}
	if err = add(archive, "install-new-pro-node.sh", strings.NewReader(string(script)), 0755); err != nil {
		return err
	}
	if err = add(archive, "SHA256SUMS", strings.NewReader(manifest), 0600); err != nil {
		return err
	}
	return archive.Close()
}
