//go:build linux

package hubstate

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func privateTemp(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	if e := os.Chmod(d, 0700); e != nil {
		t.Fatal(e)
	}
	return d
}
func fixtureOptions(root string) Options {
	return Options{Root: root, Registry: filepath.Join(root, "registry.json"), Replacements: filepath.Join(root, "replacements.json"), BusinessDB: filepath.Join(root, "business.sqlite"), Listen: "127.0.0.1:2087", SourceRevision: strings.Repeat("a", 40), Environment: []string{"DARK_PRO_LAB_MODE=1", "DARK_PRO_ENABLE_BUSINESS_CORE=1", EnableEnv + "=1", "DARK_PRO_LAB_TOKEN=" + strings.Repeat("b", 64)}}
}
func fixture(t *testing.T) (*Lock, Inventory) {
	t.Helper()
	root := privateTemp(t)
	lock, e := Acquire(root)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { lock.Close() })
	o := fixtureOptions(root)
	db, e := business.Open(o.BusinessDB)
	if e != nil {
		t.Fatal(e)
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	inv, e := BuildInventory(o)
	if e != nil {
		t.Fatal(e)
	}
	if e = WriteInventory(lock, inv); e != nil {
		t.Fatal(e)
	}
	inv, e = LoadInventory(lock)
	if e != nil {
		t.Fatal(e)
	}
	return lock, inv
}
func fingerprintFile(t *testing.T, path string) string {
	t.Helper()
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

func TestExclusiveFenceQuarantineAndInvalidation(t *testing.T) {
	root := privateTemp(t)
	first, e := Acquire(root)
	if e != nil {
		t.Fatal(e)
	}
	if second, e := Acquire(root); !errors.Is(e, ErrBusy) {
		if second != nil {
			second.Close()
		}
		t.Fatalf("second owner accepted: %v", e)
	}
	if e = os.WriteFile(filepath.Join(root, InventoryName), []byte("stale private inventory"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = InvalidateInventory(first); e != nil {
		t.Fatal(e)
	}
	if _, e = LoadInventory(first); e == nil {
		t.Fatal("invalidated inventory usable")
	}
	first.Close()
	second, e := Acquire(root)
	if e != nil {
		t.Fatal(e)
	}
	second.Close()
	if e = os.WriteFile(filepath.Join(root, QuarantineName), []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Acquire(root); !errors.Is(e, ErrQuarantined) {
		t.Fatal("quarantine bypassed", e)
	}
	if e = CheckQuarantine(filepath.Join(root, "data/business.sqlite")); !errors.Is(e, ErrQuarantined) {
		t.Fatal("ancestor quarantine bypassed", e)
	}
}
func TestPrivateFenceRejectsLinksAndFIFO(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			root := privateTemp(t)
			target := filepath.Join(root, LockName)
			other := filepath.Join(root, "other")
			if e := os.WriteFile(other, []byte("x"), 0600); e != nil {
				t.Fatal(e)
			}
			var e error
			switch kind {
			case "symlink":
				e = os.Symlink(other, target)
			case "hardlink":
				e = os.Link(other, target)
			case "fifo":
				e = syscall.Mkfifo(target, 0600)
			}
			if e != nil {
				t.Fatal(e)
			}
			if lock, e := Acquire(root); e == nil {
				lock.Close()
				t.Fatal("unsafe lock accepted")
			}
		})
	}
}

func TestQuarantineRejectsAliasIntoRestoredSubdirectory(t *testing.T) {
	root := privateTemp(t)
	if err := os.Mkdir(filepath.Join(root, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, QuarantineName), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(privateTemp(t), "alias")
	if err := os.Symlink(filepath.Join(root, "data"), alias); err != nil {
		t.Fatal(err)
	}
	if err := CheckQuarantine(filepath.Join(alias, "business.sqlite")); err == nil {
		t.Fatal("alias bypassed parent quarantine")
	}
	if err := CheckQuarantine(filepath.Join(privateTemp(t), "missing", "unused.json")); err != nil {
		t.Fatal("missing default rejected", err)
	}
}
func TestInventoryRejectsUnknownPersistenceAndUnpinnedBuild(t *testing.T) {
	for _, alter := range []func(*Options){func(o *Options) { o.Environment = append(o.Environment, "DARK_PRO_ENABLE_UNRECOGNIZED_DB=1") }, func(o *Options) { o.SourceRevision = "" }, func(o *Options) { o.Static = "/tmp/ui" }, func(o *Options) { o.BusinessDB = "/outside/business.sqlite" }} {
		o := fixtureOptions(privateTemp(t))
		alter(&o)
		if _, e := BuildInventory(o); e == nil {
			t.Fatal("unsafe/incomplete inventory accepted")
		}
	}
	inv, e := BuildInventory(fixtureOptions(privateTemp(t)))
	if e != nil {
		t.Fatal(e)
	}
	inv.Resources = append(inv.Resources, inv.Resources[0])
	if ValidateArchiveInventory(inv) == nil {
		t.Fatal("duplicate resource accepted")
	}
}

func TestInventoryAgentTLSCompleteness(t *testing.T) {
	for _, kind := range []string{"complete", "missing-both", "orphan-pair", "optional-key", "disabled-remote-agent"} {
		t.Run(kind, func(t *testing.T) {
			o := fixtureOptions(privateTemp(t))
			o.AgentTLSListen = "127.0.0.1:8443"
			o.AgentTLSCert = filepath.Join(o.Root, "agent-cert.pem")
			o.AgentTLSKey = filepath.Join(o.Root, "agent-key.pem")
			o.Environment = append(o.Environment, "DARK_PRO_ENABLE_REMOTE_AGENT=1")
			inv, err := BuildInventory(o)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "missing-both":
				kept := []Resource{}
				for _, r := range inv.Resources {
					if r.Role != "agent_tls_cert" && r.Role != "agent_tls_key" {
						kept = append(kept, r)
					}
				}
				inv.Resources = kept
			case "orphan-pair":
				inv.Flags["agent-tls-listen"] = ""
			case "optional-key":
				for i := range inv.Resources {
					if inv.Resources[i].Role == "agent_tls_key" {
						inv.Resources[i].Required = false
					}
				}
			case "disabled-remote-agent":
				inv.Environment["DARK_PRO_ENABLE_REMOTE_AGENT"] = "0"
			}
			err = ValidateArchiveInventory(inv)
			if kind == "complete" && err != nil {
				t.Fatal("complete listener rejected", err)
			}
			if kind != "complete" && err == nil {
				t.Fatal("incomplete Agent TLS contract accepted")
			}
		})
	}
}
func TestSnapshotIsStandaloneCompleteAndSourceUnchanged(t *testing.T) {
	lock, inv := fixture(t)
	source := filepath.Join(lock.root, "business.sqlite")
	before := fingerprintFile(t, source)
	dest := privateTemp(t)
	sources, e := Snapshot(context.Background(), lock, inv, dest)
	if e != nil {
		t.Fatal(e)
	}
	if len(sources) != 2 {
		t.Fatalf("empty optional stores should remain absent: %#v", sources)
	}
	if fingerprintFile(t, source) != before {
		t.Fatal("source SQLite changed")
	}
	var got Inventory
	raw, e := os.ReadFile(filepath.Join(dest, "inventory.json"))
	if e != nil || json.Unmarshal(raw, &got) != nil {
		t.Fatal("snapshot inventory missing")
	}
	if len(got.SnapshotFiles) != 1 || got.SnapshotFiles[0].Name != "data/business.sqlite" || ValidateArchiveInventory(got) != nil {
		t.Fatal("snapshot set not exact")
	}
	for _, r := range got.Resources {
		if r.Role == "registry" && r.Present {
			t.Fatal("missing registry invented")
		}
	}
	if _, e = business.ValidateOfflineBackup(context.Background(), filepath.Join(dest, "data/business.sqlite")); e != nil {
		t.Fatal(e)
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, e = os.Lstat(filepath.Join(dest, "data/business.sqlite") + suffix); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("snapshot has SQLite sidecar")
		}
	}
	got.SnapshotFiles = nil
	if ValidateArchiveInventory(got) != nil {
		t.Fatal("live structural validation should not access filesystem")
	}
}

// A subprocess deliberately exits without closing SQLite, leaving a genuine
// committed WAL in this disposable fixture. No product/source Store is opened
// by Snapshot, and no live HUB exists during the test.
func TestCrashWALWriterHelper(t *testing.T) {
	if os.Getenv("HUBSTATE_TEST_CRASH_WAL") != "1" {
		return
	}
	db, e := sql.Open("sqlite", os.Getenv("HUBSTATE_TEST_DB"))
	if e != nil {
		os.Exit(3)
	}
	for _, q := range []string{"PRAGMA journal_mode=WAL", "PRAGMA wal_autocheckpoint=0", "INSERT INTO representatives(id,display_name,kind,enabled,created_unix) VALUES('wal-fixture','WAL fixture','representative',1,1)"} {
		if _, e = db.Exec(q); e != nil {
			os.Exit(4)
		}
	}
	os.Exit(0)
}
func TestCommittedCrashWALIncludedWithoutOriginalWrites(t *testing.T) {
	lock, inv := fixture(t)
	path := filepath.Join(lock.root, "business.sqlite")
	cmd := exec.Command(os.Args[0], "-test.run=^TestCrashWALWriterHelper$")
	cmd.Env = append(os.Environ(), "HUBSTATE_TEST_CRASH_WAL=1", "HUBSTATE_TEST_DB="+path)
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("fixture writer failed %v %s", e, output)
	}
	wal, e := os.Stat(path + "-wal")
	if e != nil || wal.Size() == 0 {
		t.Fatal("fixture lacks real committed WAL")
	}
	beforeDB, beforeWAL := fingerprintFile(t, path), fingerprintFile(t, path+"-wal")
	dest := privateTemp(t)
	if _, e = Snapshot(context.Background(), lock, inv, dest); e != nil {
		t.Fatal(e)
	}
	if fingerprintFile(t, path) != beforeDB || fingerprintFile(t, path+"-wal") != beforeWAL {
		t.Fatal("original WAL/database mutated")
	}
	v, e := business.ValidateOfflineBackup(context.Background(), filepath.Join(dest, "data/business.sqlite"))
	if e != nil || v.TableCounts["representatives"] != 2 {
		t.Fatal("committed WAL row omitted", e, v.TableCounts["representatives"])
	}
}

func TestDisabledRetainedStageRequiresKeyAndSurvives(t *testing.T) {
	lock, inv := fixture(t)
	o := fixtureOptions(lock.root)
	o.Staging = filepath.Join(lock.root, "configs.json")
	if e := os.WriteFile(o.Staging, []byte(`{"version":1,"stages":{}}`), 0600); e != nil {
		t.Fatal(e)
	}
	without, e := BuildInventory(o)
	if e != nil {
		t.Fatal(e)
	}
	if e = WriteInventory(lock, without); e == nil {
		t.Fatal("retained encrypted store missing key accepted")
	}
	o.Environment = append(o.Environment, "DARK_PRO_STAGE_KEY_HEX="+strings.Repeat("c", 64))
	inv, e = BuildInventory(o)
	if e != nil {
		t.Fatal(e)
	}
	if e = WriteInventory(lock, inv); e != nil {
		t.Fatal(e)
	}
	inv, e = LoadInventory(lock)
	if e != nil {
		t.Fatal(e)
	}
	files, e := Snapshot(context.Background(), lock, inv, privateTemp(t))
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range files {
		if f.Role == "config_stage" {
			found = true
		}
	}
	if !found {
		t.Fatal("disabled retained stage silently omitted")
	}
}

func makeIdleRuntime(t *testing.T, dir, native string) {
	t.Helper()
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	key, install, e := runtimeKeys(native)
	if e != nil {
		t.Fatal(e)
	}
	clear := []byte(fmt.Sprintf(`{"version":1,"node_id":"hub-local","installation_id":%q,"generation":0,"phase":"idle","sequence":0,"last_counters":null,"final_accepted":false}`, install))
	digest := sha256.Sum256(append([]byte("DARK-PRO-MANAGED-INBOUND-JOURNAL-v1\x00"), key...))
	block, _ := aes.NewCipher(digest[:])
	aead, _ := cipher.NewGCM(block)
	nonce := bytes.Repeat([]byte{7}, aead.NonceSize())
	raw := aead.Seal(nonce, nonce, clear, []byte("managed-inbound-v1\x00hub-local\x00"+install))
	if e = os.WriteFile(filepath.Join(dir, "state.enc"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "writer.lock"), nil, 0600); e != nil {
		t.Fatal(e)
	}
}
func TestRuntimeFenceRejectsWriterBirthsAndWrongKey(t *testing.T) {
	root := privateTemp(t)
	dir := filepath.Join(root, "runtime")
	key := strings.Repeat("d", 64)
	makeIdleRuntime(t, dir, key)
	inv := Inventory{Environment: map[string]string{"DARK_PRO_NATIVE_INBOUND_KEY_HEX": key}, Resources: []Resource{{Role: "inbound_runtime", Path: filepath.Join(dir, "state.enc"), Required: true}}}
	release, e := holdRuntimeStopped(context.Background(), inv)
	if e != nil {
		t.Fatal(e)
	}
	if second, e := holdRuntimeStopped(context.Background(), inv); !errors.Is(e, ErrBusy) {
		if second != nil {
			second()
		}
		t.Fatal("runtime writer not fenced", e)
	}
	release()
	if e = os.Mkdir(filepath.Join(dir, "birth-unfinished"), 0700); e != nil {
		t.Fatal(e)
	}
	if release, e = holdRuntimeStopped(context.Background(), inv); e == nil {
		release()
		t.Fatal("unexplained birth accepted")
	}
	os.Remove(filepath.Join(dir, "birth-unfinished"))
	inv.Environment["DARK_PRO_NATIVE_INBOUND_KEY_HEX"] = strings.Repeat("e", 64)
	if release, e = holdRuntimeStopped(context.Background(), inv); e == nil {
		release()
		t.Fatal("wrong runtime key accepted")
	}
}
func TestStoppedProofRejectsSelfAndUnreadableEvidence(t *testing.T) {
	boot, e := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("/proc/self/stat")
	if e != nil {
		t.Fatal(e)
	}
	fields := strings.Fields(string(raw[bytes.LastIndexByte(raw, ')')+1:]))
	ticks, e := strconv.ParseUint(fields[19], 10, 64)
	if e != nil {
		t.Fatal(e)
	}
	visiblePID, e := strconv.Atoi(strings.Fields(string(raw))[0])
	if e != nil {
		t.Fatal(e)
	}
	m := inboundexec.ProcessMember{BootID: strings.TrimSpace(string(boot)), PID: visiblePID, StartTicks: ticks}
	if !errors.Is(verifyStoppedMember(m, os.ReadFile), ErrBusy) {
		t.Fatal("live same incarnation accepted")
	}
	unreadable := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "boot_id") {
			return boot, nil
		}
		return nil, os.ErrPermission
	}
	if verifyStoppedMember(m, unreadable) == nil {
		t.Fatal("permission error treated as exit")
	}
	absent := func(path string) ([]byte, error) {
		if strings.HasSuffix(path, "boot_id") {
			return boot, nil
		}
		return nil, os.ErrNotExist
	}
	if verifyStoppedMember(m, absent) != nil {
		t.Fatal("known absent PID rejected")
	}
	m.StartTicks++
	if verifyStoppedMember(m, os.ReadFile) != nil {
		t.Fatal("different incarnation rejected")
	}
}
