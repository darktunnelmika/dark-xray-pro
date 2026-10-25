//go:build linux

package inboundruntime

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var errAsset = errors.New("inbound_local_tls_asset_unavailable")

const maxTLSAssetBytes = 1 << 20

// readTLSAsset reads only an explicitly typed certificate/key path on this
// target. ACME symlinks are allowed; the opened regular-file bytes are frozen
// into the private process config and are never reloaded under a new identity.
func readTLSAsset(path string) ([]byte, error) {
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return nil, errAsset
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errAsset
	}
	f := os.NewFile(uintptr(fd), "tls-asset")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > maxTLSAssetBytes {
		return nil, errAsset
	}
	data, err := io.ReadAll(io.LimitReader(f, maxTLSAssetBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxTLSAssetBytes {
		return nil, errAsset
	}
	return data, nil
}

func certificatePEM(data []byte) bool {
	found := false
	for len(strings.TrimSpace(string(data))) > 0 {
		block, rest := pem.Decode(data)
		if block == nil || block.Type != "CERTIFICATE" {
			return false
		}
		if _, err := x509.ParseCertificate(block.Bytes); err != nil {
			return false
		}
		found, data = true, rest
	}
	return found
}

func freezeTLSCertificate(c map[string]any) error {
	certificatePath, certificateFile := c["certificateFile"]
	keyPath, keyFile := c["keyFile"]
	if certificatePath == "" {
		delete(c, "certificateFile")
		certificateFile = false
	}
	if keyPath == "" {
		delete(c, "keyFile")
		keyFile = false
	}
	if !certificateFile && !keyFile {
		return nil
	}
	path, ok := certificatePath.(string)
	if !ok || path == "" {
		return errAsset
	}
	certificate, err := readTLSAsset(path)
	if err != nil || !certificatePEM(certificate) {
		return errAsset
	}
	var key []byte
	usage, _ := c["usage"].(string)
	if keyFile {
		path, ok = keyPath.(string)
		if !ok || path == "" {
			return errAsset
		}
		key, err = readTLSAsset(path)
		if err != nil {
			return errAsset
		}
		if _, err = tls.X509KeyPair(certificate, key); err != nil {
			return errAsset
		}
	} else if usage != "verify" {
		return errAsset
	}
	c["certificate"] = strings.Split(strings.TrimSpace(string(certificate)), "\n")
	if len(key) > 0 {
		c["key"] = strings.Split(strings.TrimSpace(string(key)), "\n")
	}
	delete(c, "certificateFile")
	delete(c, "keyFile")
	c["oneTimeLoading"] = true
	return nil
}

// prepareKeyLog is an explicit write asset, kept separate from read-only
// certificate material and XDrive storage. Never follow a key-log symlink or
// create parent paths; the engine may only append to an owned private file.
func prepareKeyLog(path string) error {
	if path == "" || path == "none" {
		return nil
	}
	if !filepath.IsAbs(path) || strings.ContainsAny(path, "\x00\r\n") {
		return errAsset
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() || parent.Mode().Perm()&0022 != 0 && parent.Mode()&os.ModeSticky == 0 {
		return errAsset
	}
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_APPEND|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return errAsset
	}
	f := os.NewFile(uintptr(fd), "tls-key-log")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return errAsset
	}
	identity, ok := info.Sys().(*syscall.Stat_t)
	if !ok || identity.Uid != uint32(os.Geteuid()) {
		return errAsset
	}
	return nil
}

func freezeNativeTLSAssets(cfg map[string]any) error {
	// Only the engine's typed streamSettings.tlsSettings location is read.
	// Arbitrary headers, user dictionaries and storage paths are not assets.
	remaining := 64 << 20
	for _, field := range []string{"inbounds", "outbounds"} {
		entries, _ := cfg[field].([]any)
		for _, entry := range entries {
			inbound, _ := entry.(map[string]any)
			stream, _ := inbound["streamSettings"].(map[string]any)
			settings, _ := stream["tlsSettings"].(map[string]any)
			certificates, _ := settings["certificates"].([]any)
			for _, entry := range certificates {
				certificate, ok := entry.(map[string]any)
				if !ok || freezeTLSCertificate(certificate) != nil {
					return errAsset
				}
				for _, field := range []string{"certificate", "key"} {
					switch lines := certificate[field].(type) {
					case []string:
						for _, line := range lines {
							remaining -= len(line) + 1
						}
					case []any:
						for _, raw := range lines {
							if line, ok := raw.(string); ok {
								remaining -= len(line) + 1
							}
						}
					}
				}
				if remaining < 0 {
					return errAsset
				}
			}
			if raw, exists := settings["masterKeyLog"]; exists {
				path, ok := raw.(string)
				if !ok || prepareKeyLog(path) != nil {
					return errAsset
				}
			}
		}
	}
	return nil
}
