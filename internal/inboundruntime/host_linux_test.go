//go:build linux

package inboundruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestHostCapabilitiesDistinguishParentAndChildNetworkAuthority(t *testing.T) {
	for _, tc := range []struct {
		name, status           string
		root, effective, child bool
	}{
		{"unprivileged", "CapEff:\t0\nCapAmb:\t0\nCapBnd:\tffffffff\n", false, false, false},
		{"parent only", "CapEff:\t1000\nCapAmb:\t0\nCapBnd:\t1000\n", false, true, false},
		{"ambient", "CapEff:\t1000\nCapAmb:\t1000\nCapBnd:\t1000\n", false, true, true},
		{"root bounded", "CapEff:\t1000\nCapAmb:\t0\nCapBnd:\t1000\n", true, true, true},
		{"root without bounding", "CapEff:\t1000\nCapAmb:\t0\nCapBnd:\t0\n", true, true, false},
		{"malformed", "CapEff:\tinvalid\nCapAmb:\t1000\nCapBnd:\t1000\n", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			effective, child := processNetworkCapabilities([]byte(tc.status), tc.root)
			if effective != tc.effective || child != tc.child {
				t.Fatalf("authority inference: %v %v", effective, child)
			}
		})
	}
}

func TestTUNDNSMarkerPrecedesAnyCustomerConfigCommand(t *testing.T) {
	for _, marker := range []string{"upstream engine", tunDNSActivationMarker + "-untrusted-suffix", tunDNSActivationMarker} {
		t.Run(marker, func(t *testing.T) {
			dir := t.TempDir()
			path, touched := filepath.Join(dir, "engine"), filepath.Join(dir, "config-was-read")
			quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
			raw := []byte("#!/bin/sh\nif [ \"$1\" = version ]; then printf '%s\\n' " + quote(marker) + "; exit 0; fi\nprintf touched > " + quote(touched) + "\n")
			if err := os.WriteFile(path, raw, 0700); err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(raw)
			binary, err := OpenBinary(path, hex.EncodeToString(digest[:]))
			if err != nil {
				t.Fatal(err)
			}
			defer binary.Close()
			bundle := inboundexec.Bundle{NativeConfig: []byte(`{"inbounds":[{"protocol":"tun","settings":{"autoSystemDnsToGateway":true}}]}`)}
			err = guardTUNDNSActivation(context.Background(), binary, bundle)
			if (err == nil) != (marker == tunDNSActivationMarker) {
				t.Fatal("non-exact feature marker accepted", err)
			}
			if _, err := os.Stat(touched); !os.IsNotExist(err) {
				t.Fatal("customer config command ran before feature proof", err)
			}
			if marker != tunDNSActivationMarker {
				if err := validateGroup(context.Background(), binary, nil, bundle); err == nil {
					t.Fatal("old engine reached config validation")
				}
				if _, err := os.Stat(touched); !os.IsNotExist(err) {
					t.Fatal("old engine parsed customer config before rejection", err)
				}
			}
		})
	}
}
