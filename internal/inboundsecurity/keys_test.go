package inboundsecurity

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pinnedScript(t *testing.T, content string) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fixture-core")
	raw := []byte("#!/bin/sh\n" + content)
	if err := os.WriteFile(path, raw, 0700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return Config{CorePath: path, CoreSHA256: hex.EncodeToString(hash[:])}
}

func echFixture(t *testing.T, serverName string) (string, string) {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// TLS ECHConfig and Xray's private-key/config container, with one
	// X25519/HKDF-SHA256/AES128GCM suite. No external executable is involved.
	content := []byte{0, 0, 0x20, 0, 32}
	content = append(content, key.PublicKey().Bytes()...)
	content = append(content, 0, 4, 0, 1, 0, 1, 0, byte(len(serverName)))
	content = append(content, []byte(serverName)...)
	content = append(content, 0, 0)
	config := []byte{0xfe, 0x0d, 0, 0}
	binary.BigEndian.PutUint16(config[2:4], uint16(len(content)))
	config = append(config, content...)
	list := []byte{0, 0}
	binary.BigEndian.PutUint16(list, uint16(len(config)))
	list = append(list, config...)
	keys := []byte{0, 32}
	keys = append(keys, key.Bytes()...)
	keys = append(keys, list...)
	return base64.StdEncoding.EncodeToString(list), base64.StdEncoding.EncodeToString(keys)
}

func TestPinnedCoreOutputsAndNoInheritedSecrets(t *testing.T) {
	seed := base64.RawURLEncoding.EncodeToString(make([]byte, 32))
	verify := base64.RawURLEncoding.EncodeToString(make([]byte, 1952))
	config64, keys64 := echFixture(t, "public.example")
	t.Setenv("INBOUND_SECURITY_TEST_SECRET", "do-not-forward")
	config := pinnedScript(t, "test -z \"$INBOUND_SECURITY_TEST_SECRET\" || exit 19\n"+
		"case \"$1\" in\n"+
		"mldsa65) printf 'Seed: %s\\nVerify: %s\\n' '"+seed+"' '"+verify+"';;\n"+
		"tls) test \"$2\" = ech && test \"$3\" = --serverName && test \"$4\" = public.example || exit 20\n"+
		"printf 'ECH config list:\\n%s\\nECH server keys:\\n%s\\n' '"+config64+"' '"+keys64+"';;\n"+
		"*) exit 21;;\nesac\n")
	service := serviceForTest(t, config)
	if !service.Capabilities().Keys["mldsa65"] || !service.Capabilities().Keys["ech"] {
		t.Fatal("verified core tools not enabled")
	}
	MLDSA, err := service.GenerateMLDSA65(context.Background())
	if err != nil || MLDSA.Kind != "mldsa65" || MLDSA.Seed != seed || MLDSA.Verify != verify {
		t.Fatal("pinned MLDSA command or format validation failed", err)
	}
	ECH, err := service.GenerateECH(context.Background(), "public.example")
	if err != nil || ECH.Kind != "ech" || ECH.ServerName != "public.example" || ECH.Config != config64 || ECH.PrivateKey != keys64 {
		t.Fatal("pinned ECH command or consistency validation failed", err)
	}
	if _, err := service.GenerateECH(context.Background(), "public.example;echo unsafe"); errorCode(err) != "invalid_ech_server_name" {
		t.Fatal("unsafe ECH argument accepted")
	}
}

func TestCorePinAndBoundedErrorHandling(t *testing.T) {
	service := serviceForTest(t, Config{})
	if _, err := service.GenerateMLDSA65(context.Background()); errorCode(err) != "pinned_xray_core_required" {
		t.Fatal("core command ran without a pin")
	}
	config := pinnedScript(t, "printf 'not a key\\n'\n")
	service = serviceForTest(t, config)
	if _, err := service.GenerateMLDSA65(context.Background()); errorCode(err) != "invalid_core_key_output" {
		t.Fatal("unrecognized output accepted")
	}
	if err := os.WriteFile(config.CorePath, []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GenerateMLDSA65(context.Background()); errorCode(err) != "core_pin_mismatch" {
		t.Fatal("changed executable ran")
	}
	config.CoreSHA256 = strings.Repeat("0", 64)
	if _, err := New(config); errorCode(err) != "core_pin_mismatch" {
		t.Fatal("incorrect startup pin accepted")
	}
	config = pinnedScript(t, "printf 'private secret diagnostics' >&2\nexit 1\n")
	if _, err := serviceForTest(t, config).GenerateMLDSA65(context.Background()); err == nil || strings.Contains(err.Error(), "secret") || errorCode(err) != "core_key_generation_failed" {
		t.Fatal("core failure leaked child output")
	}
	config = pinnedScript(t, "printf '%040000d' 0\n")
	if _, err := serviceForTest(t, config).GenerateMLDSA65(context.Background()); errorCode(err) != "core_key_generation_failed" {
		t.Fatal("oversized output accepted")
	}
	config = pinnedScript(t, "exit 99\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := serviceForTest(t, config).GenerateMLDSA65(ctx); errorCode(err) != "key_generation_cancelled" {
		t.Fatal("cancelled key generation ran")
	}
	link := filepath.Join(t.TempDir(), "symlink-core")
	if err := os.Symlink(config.CorePath, link); err != nil {
		t.Fatal(err)
	}
	config.CorePath = link
	if _, err := New(config); errorCode(err) != "pinned_core_unavailable" {
		t.Fatal("symlink executable accepted")
	}
}

func TestECHRejectsKeyConfigAndNameMismatch(t *testing.T) {
	config64, keys64 := echFixture(t, "public.example")
	if !validateECH(config64, keys64, "public.example") || validateECH(config64, keys64, "wrong.example") {
		t.Fatal("ECH name validation failed")
	}
	keys, _ := base64.StdEncoding.DecodeString(keys64)
	keys[8] ^= 0x80
	if validateECH(config64, base64.StdEncoding.EncodeToString(keys), "public.example") {
		t.Fatal("mismatched ECH private/public key accepted")
	}
	list, _ := base64.StdEncoding.DecodeString(config64)
	list[1] ^= 1
	if validateECH(base64.StdEncoding.EncodeToString(list), keys64, "public.example") {
		t.Fatal("inconsistent ECH vector length accepted")
	}
	if _, ok := parseLabelledOutput([]byte("Seed: A\nVerify: B\nVerify: C"), "Seed:", "Verify:"); ok {
		t.Fatal("duplicate output labels accepted")
	}
}

func TestPinnedXraySecurityKeyCommands(t *testing.T) {
	path, hash := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if path == "" && hash == "" {
		t.Skip("pinned Xray binary and SHA256 not supplied")
	}
	if path == "" || hash == "" {
		t.Fatal("both pinned Xray test variables are required")
	}
	service := serviceForTest(t, Config{CorePath: path, CoreSHA256: hash})
	MLDSA, err := service.GenerateMLDSA65(context.Background())
	if err != nil || !canonicalBase64(MLDSA.Seed, base64.RawURLEncoding, 32) || !canonicalBase64(MLDSA.Verify, base64.RawURLEncoding, 1952) {
		t.Fatal("actual pinned Xray MLDSA65 generation failed", err)
	}
	ECH, err := service.GenerateECH(context.Background(), "public.example")
	if err != nil || !validateECH(ECH.Config, ECH.PrivateKey, "public.example") {
		t.Fatal("actual pinned Xray ECH generation failed", err)
	}
}
