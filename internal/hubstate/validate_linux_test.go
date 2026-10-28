//go:build linux

package hubstate

import (
	"context"
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
	"testing"
	"time"
)

func stagedFixture(t *testing.T) (string, Inventory) {
	t.Helper()
	lock, inv := fixture(t)
	stage := privateTemp(t)
	if _, err := Snapshot(context.Background(), lock, inv, stage); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(stage, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &inv) != nil {
		t.Fatal("snapshot inventory invalid")
	}
	return stage, inv
}

func addStagePayload(t *testing.T, stage string, inv *Inventory, role string, raw []byte, required bool) {
	t.Helper()
	name := resources[role]
	if name == "" {
		t.Fatal("unknown fixture role")
	}
	path := filepath.Join(stage, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	found := false
	for i := range inv.Resources {
		if inv.Resources[i].Role == role {
			inv.Resources[i].Present = true
			found = true
		}
	}
	if !found {
		inv.Resources = append(inv.Resources, Resource{Name: name, Role: role, Path: filepath.Join(inv.Root, "assets", role), Present: true, Required: required})
	}
	inv.SnapshotFiles = append(inv.SnapshotFiles, SnapshotFile{Name: name, Role: role, Size: int64(len(raw))})
}

func TestValidateSnapshotUsesOnlyStagedNamesAndRejectsMissingFiles(t *testing.T) {
	stage, inv := stagedFixture(t)
	oldRoot := inv.Root
	inv.Root = "/nonexistent-private-original-hub"
	for i := range inv.Resources {
		rel, err := filepath.Rel(oldRoot, inv.Resources[i].Path)
		if err != nil {
			t.Fatal(err)
		}
		inv.Resources[i].Path = filepath.Join(inv.Root, rel)
	}
	if err := ValidateSnapshot(context.Background(), stage, inv); err != nil {
		t.Fatal("restore consulted original filesystem", err)
	}
	path := filepath.Join(stage, "data/business.sqlite")
	before := fingerprintFile(t, path)
	if err := ValidateSnapshot(context.Background(), stage, inv); err != nil {
		t.Fatal(err)
	}
	if fingerprintFile(t, path) != before {
		t.Fatal("validation changed staged SQLite")
	}
	if err := os.Rename(path, path+".absent"); err != nil {
		t.Fatal(err)
	}
	if ValidateSnapshot(context.Background(), stage, inv) == nil {
		t.Fatal("missing declared business payload accepted")
	}
}

func TestValidateSnapshotRetainedCryptoAndCanonicalRegistry(t *testing.T) {
	stage, inv := stagedFixture(t)
	addStagePayload(t, stage, &inv, "config_stage", []byte(`{"version":1,"stages":{}}`), false)
	if ValidateSnapshot(context.Background(), stage, inv) == nil {
		t.Fatal("missing retained encryption key accepted")
	}
	inv.Environment["DARK_PRO_STAGE_KEY_HEX"] = strings.Repeat("a", 64)
	if err := ValidateSnapshot(context.Background(), stage, inv); err != nil {
		t.Fatal(err)
	}
	addStagePayload(t, stage, &inv, "registry", []byte(`{"version":1,"invites":{},"bindings":{},"receipts":{}}`), false)
	if err := ValidateSnapshot(context.Background(), stage, inv); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(stage, "data/registry.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"invites":{},"bindings":{},"receipts":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if ValidateSnapshot(context.Background(), stage, inv) == nil {
		t.Fatal("unknown registry version accepted")
	}
}

func certificatePair(t *testing.T) ([]byte, []byte) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "backup-fixture.invalid"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})
}

func TestValidateSnapshotTLSPairAndNoOriginalAssetReads(t *testing.T) {
	stage, inv := stagedFixture(t)
	cert, key := certificatePair(t)
	inv.Environment["DARK_PRO_ENABLE_REMOTE_AGENT"] = "1"
	inv.Flags["agent-tls-listen"] = "127.0.0.1:8443"
	addStagePayload(t, stage, &inv, "agent_tls_cert", cert, true)
	addStagePayload(t, stage, &inv, "agent_tls_key", key, true)
	for i := range inv.Resources {
		if strings.HasPrefix(inv.Resources[i].Role, "agent_tls_") {
			inv.Resources[i].Path = "/nonexistent-original-config/" + inv.Resources[i].Role
		}
	}
	if err := ValidateSnapshot(context.Background(), stage, inv); err != nil {
		t.Fatal("staged TLS pair rejected", err)
	}
	_, otherKey := certificatePair(t)
	if err := os.WriteFile(filepath.Join(stage, "config/agent-key.pem"), otherKey, 0600); err != nil {
		t.Fatal(err)
	}
	if ValidateSnapshot(context.Background(), stage, inv) == nil {
		t.Fatal("mismatched TLS key accepted")
	}
}

func TestOptionalMissingRuntimeOrBotJournalRejectsRetainedArtifacts(t *testing.T) {
	for _, role := range []string{"inbound_runtime", "owner_report_db"} {
		for _, artifact := range []string{"writer.lock", "birth-abandoned", "inbox.sqlite-wal"} {
			t.Run(role+"/"+artifact, func(t *testing.T) {
				dir := privateTemp(t)
				name := "state.enc"
				if role == "owner_report_db" {
					name = "inbox.sqlite"
				}
				inv := Inventory{Resources: []Resource{{Role: role, Path: filepath.Join(dir, name)}}}
				if err := os.WriteFile(filepath.Join(dir, artifact), nil, 0600); err != nil {
					t.Fatal(err)
				}
				var release func()
				var err error
				if role == "inbound_runtime" {
					release, err = holdRuntimeStopped(context.Background(), inv)
				} else {
					release, err = holdBotStopped(inv)
				}
				if err == nil {
					release()
					t.Fatal("missing state treated as empty despite retained evidence")
				}
			})
		}
	}
}
