//go:build linux

package inboundruntime

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func fixtureTLSMaterial(t *testing.T) ([]byte, []byte) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "native-assets.example"}, DNSNames: []string{"native-assets.example"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, public, private)
	if err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key})
}

func TestNativeTLSFilesFreezeExactMaterialWithoutCopyingArbitraryPaths(t *testing.T) {
	dir := t.TempDir()
	certificate, key := fixtureTLSMaterial(t)
	certificatePath, keyPath := filepath.Join(dir, "certificate.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certificatePath, certificate, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(dir, "acme-current.pem")
	if err := os.Symlink(certificatePath, symlink); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"inbounds": []any{map[string]any{"tag": "customer", "streamSettings": map[string]any{"tlsSettings": map[string]any{"certificates": []any{map[string]any{"certificateFile": symlink, "keyFile": keyPath, "usage": "encipherment", "oneTimeLoading": false}}}, "wsSettings": map[string]any{"headers": map[string]any{"certificateFile": "/unknown/host/path"}}}, "settings": map[string]any{"dir": "/storage/left/untouched"}}}, "outbounds": []any{}}
	raw, _ := json.Marshal(cfg)
	frozen, err := nativeConfig(raw, "/tmp/private-native-stats.sock")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(frozen, []byte(certificatePath)) || bytes.Contains(frozen, []byte(symlink)) || bytes.Contains(frozen, []byte(keyPath)) || !bytes.Contains(frozen, []byte("BEGIN PRIVATE KEY")) || !bytes.Contains(frozen, []byte("/unknown/host/path")) || !bytes.Contains(frozen, []byte("/storage/left/untouched")) {
		t.Fatal("typed file freezing crossed an asset boundary")
	}
	nextCertificate, nextKey := fixtureTLSMaterial(t)
	if err = os.WriteFile(certificatePath, nextCertificate, 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, nextKey, 0600); err != nil {
		t.Fatal(err)
	}
	next, err := nativeConfig(raw, "/tmp/private-native-stats.sock")
	if err != nil || hashBytes(next) == hashBytes(frozen) {
		t.Fatal("file rotation did not produce a new process configuration identity", err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(frozen, &decoded); err != nil {
		t.Fatal(err)
	}
	in := decoded["inbounds"].([]any)[0].(map[string]any)
	row := in["streamSettings"].(map[string]any)["tlsSettings"].(map[string]any)["certificates"].([]any)[0].(map[string]any)
	if row["oneTimeLoading"] != true {
		t.Fatal("runtime could reload unbound certificate bytes")
	}
	lines := row["key"].([]any)
	var text []string
	for _, line := range lines {
		text = append(text, line.(string))
	}
	if strings.TrimSpace(strings.Join(text, "\n")) != strings.TrimSpace(string(key)) {
		t.Fatal("earlier frozen configuration changed with source path")
	}
}

func TestNativeTLSAssetsRejectUnboundedAndNonRegularSources(t *testing.T) {
	dir := t.TempDir()
	oversized := filepath.Join(dir, "oversized.pem")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte{'x'}, maxTLSAssetBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readTLSAsset(oversized); err == nil {
		t.Fatal("oversized material accepted")
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := readTLSAsset(fifo); finished <- err }()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("FIFO accepted")
		}
	case <-time.After(time.Second):
		t.Fatal("asset open blocked on FIFO")
	}
	if _, err := readTLSAsset(dir); err == nil {
		t.Fatal("directory material accepted")
	}
	if _, err := readTLSAsset("relative.pem"); err == nil {
		t.Fatal("relative material accepted")
	}
	certificate, key := fixtureTLSMaterial(t)
	_, foreignKey := fixtureTLSMaterial(t)
	cp, kp := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(cp, certificate, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kp, foreignKey, 0600); err != nil {
		t.Fatal(err)
	}
	if freezeTLSCertificate(map[string]any{"certificateFile": cp, "keyFile": kp}) == nil {
		t.Fatal("mismatched key accepted")
	}
	if err := os.WriteFile(kp, key, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cp, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := readTLSAsset(cp); err == nil {
		t.Fatal("untrusted writable material accepted")
	}
	if err := freezeTLSCertificate(map[string]any{"certificateFile": "", "keyFile": "", "certificate": strings.Split(string(certificate), "\n"), "key": strings.Split(string(key), "\n")}); err != nil {
		t.Fatal("valid inline material with empty file fields rejected", err)
	}
}

func TestNativeKeyLogRequiresPrivateOwnedRegularFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keys.log")
	if err := prepareKeyLog(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("key log not private", err)
	}
	link := filepath.Join(dir, "key-log-link")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if prepareKeyLog(link) == nil {
		t.Fatal("key-log symlink accepted")
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if prepareKeyLog(path) == nil {
		t.Fatal("public key-log file accepted")
	}
	missing := filepath.Join(dir, "not-created", "keys.log")
	if prepareKeyLog(missing) == nil {
		t.Fatal("unknown parent path created")
	}
	if _, err = os.Stat(filepath.Dir(missing)); !os.IsNotExist(err) {
		t.Fatal("key log created an undeclared parent")
	}
}
