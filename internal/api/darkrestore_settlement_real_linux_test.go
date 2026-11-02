//go:build linux

package api

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/darkrestore"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	fixture "github.com/darktunnelmika/dark-xray-pro/testdata/darkrestore"
)

func TestRealPinnedDarkRestoreLocalSettlement(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" {
		t.Skip("explicit experimental barrier core required")
	}
	testRealPinnedPretrafficUsageAdmission(t, []string{"active-restore-cutover"})
}

type localSettlementFixture struct {
	dir, source, sha, id, uuid, dbPath string
	at                                 time.Time
}

func settlementHash(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	v := sha256.Sum256(raw)
	return hex.EncodeToString(v[:])
}
func newLocalSettlementFixture(t *testing.T) (*Server, *localSettlementFixture) {
	t.Helper()
	f := &localSettlementFixture{dir: t.TempDir(), at: time.Now(), uuid: "01234567-89ab-4cde-8fab-0123456789ab"}
	if err := os.Chmod(f.dir, 0700); err != nil {
		t.Fatal(err)
	}
	f.source = filepath.Join(f.dir, "legacy.sqlite")
	db, err := sql.Open("sqlite", f.source)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(fixture.SQL + "UPDATE restore_subscriptions SET legacy_total=134217728"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(f.source, 0600); err != nil {
		t.Fatal(err)
	}
	f.sha = settlementHash(t, f.source)
	f.id = darkrestore.LocalSourceID(f.sha, "synthetic-source")
	f.dbPath = filepath.Join(f.dir, "authority-live.sqlite")
	s := businessLabAt(t, f.dbPath)
	if _, err = s.business.BootstrapOwner(context.Background(), ownerUsername, ownerPassword); err != nil {
		t.Fatal(err)
	}
	return s, f
}
func (f *localSettlementFixture) provisionSourceUUID(t *testing.T, client *business.Client) {
	t.Helper()
	// The independent source replica is provisioned before constructing or serving its first config.
	db, err := sql.Open("sqlite", f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	result, err := db.Exec("UPDATE clients SET uuid=? WHERE id=? AND uuid=?", f.uuid, f.id, client.UUID)
	if err != nil {
		t.Fatal(err)
	}
	if n, e := result.RowsAffected(); e != nil || n != 1 {
		t.Fatal("cannot bind source test identity")
	}
	client.UUID = f.uuid
}
func (f *localSettlementFixture) check(t *testing.T, ctx context.Context, o managedxray.Options, epoch uc.Epoch, final uc.Report) {
	t.Helper()
	journal := filepath.Join(o.Directory, "runtime.enc")
	jSHA := settlementHash(t, journal)
	key := filepath.Join(f.dir, "source.key")
	if err := os.WriteFile(key, o.StateKey, 0600); err != nil {
		t.Fatal(err)
	}
	seal, err := managedxray.ReadLocalTCPSeal(journal, key, jSHA, epoch.NodeID, epoch.InstallationID, o.ListenIP)
	if err != nil {
		t.Fatal("runtime exit proof", err)
	}
	db, err := sql.Open("sqlite", f.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	authority := filepath.Join(f.dir, "authority.sqlite")
	if _, err = db.Exec("VACUUM INTO ?", authority); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(authority, 0600); err != nil {
		t.Fatal(err)
	}
	pin := settlementHash(t, authority)
	inspect := func(seal managedxray.LocalTCPSeal, sha, node string) (darkrestore.LocalSourceSettlement, error) {
		return darkrestore.InspectLocalSettlement(ctx, f.source, sha, authority, pin, "synthetic-source", node, epoch.InstallationID, f.at, seal)
	}
	result, err := inspect(seal, f.sha, epoch.NodeID)
	if err != nil {
		t.Fatal("private source reconciliation", err)
	}
	u := final.Users[0]
	if result.PriorBytes != 310 || result.Up != u.Uplink || result.Down != u.Downlink || result.Historical != 310+u.Uplink+u.Downlink || result.Remaining != 134217728-result.Historical || result.Terminal.SHA != final.SHA {
		t.Fatal("finite quota or reported history mismatch")
	}
	replay, err := inspect(seal, f.sha, epoch.NodeID)
	if err != nil || replay != result {
		t.Fatal("read-only replay changed result", err)
	}
	if _, err = inspect(managedxray.LocalTCPSeal{}, f.sha, epoch.NodeID); err == nil {
		t.Fatal("ledger without source exit proof accepted")
	}
	if _, err = inspect(seal, f.sha, "wrong-node"); err == nil {
		t.Fatal("wrong node accepted")
	}
	if _, err = inspect(seal, pin, epoch.NodeID); err == nil {
		t.Fatal("different snapshot accepted")
	}
	if settlementHash(t, f.source) != f.sha || settlementHash(t, authority) != pin {
		t.Fatal("offline input changed")
	}
	// Exercise the packaged command with the same independently pinned copies.
	if cli := os.Getenv("DARK_PRO_TEST_SETTLEMENT_CLI"); cli != "" {
		args := []string{"-source", f.source, "-source-sha256", f.sha, "-authority", authority, "-authority-sha256", pin, "-source-id", "synthetic-source", "-node", epoch.NodeID, "-installation", epoch.InstallationID, "-as-of-unix", strconv.FormatInt(f.at.Unix(), 10), "-journal", journal, "-journal-sha256", jSHA, "-state-key-file", key, "-source-listen-ip", o.ListenIP}
		raw, err := exec.CommandContext(ctx, cli, args...).Output()
		var viaCLI darkrestore.LocalSourceSettlement
		if err != nil || json.Unmarshal(raw, &viaCLI) != nil || viaCLI != result {
			t.Fatal("packaged CLI mismatch", err)
		}
		badKey := filepath.Join(f.dir, "wrong.key")
		if err = os.WriteFile(badKey, make([]byte, 32), 0600); err != nil {
			t.Fatal(err)
		}
		for i, flag := range args {
			if flag == "-state-key-file" {
				args[i+1] = badKey
				break
			}
		}
		if raw, err = exec.CommandContext(ctx, cli, args...).Output(); err == nil || len(raw) != 0 {
			t.Fatal("incorrect journal key produced a receipt")
		}
	}
	for _, entry := range []struct{ name, sql string }{
		{"unknown-tail", "UPDATE pro_usage_epochs SET state='uncertain'"},
		{"held", "UPDATE pro_usage_accounts SET holds=1"},
		{"quota-account-drift", "UPDATE pro_usage_accounts SET uplink=uplink+1"},
		{"different-uuid", "UPDATE clients SET uuid='22345678-1234-4234-8234-123456789abc'"},
		{"missing-terminal", "DROP TRIGGER pro_usage_receipts_no_delete; DELETE FROM pro_usage_receipts WHERE quiesced=1"},
		{"missing-initial", "DROP TRIGGER pro_usage_receipts_no_delete; DELETE FROM pro_usage_receipts WHERE sequence='1'"},
	} {
		copyPath := filepath.Join(f.dir, entry.name+".sqlite")
		if _, err = db.Exec("VACUUM INTO ?", copyPath); err != nil {
			t.Fatal(err)
		}
		if err = os.Chmod(copyPath, 0600); err != nil {
			t.Fatal(err)
		}
		broken, err := sql.Open("sqlite", copyPath)
		if err != nil {
			t.Fatal(err)
		}
		_, err = broken.Exec(entry.sql)
		e := broken.Close()
		if err != nil || e != nil {
			t.Fatal("mutation fixture failed", entry.name, err, e)
		}
		_, err = darkrestore.InspectLocalSettlement(ctx, f.source, f.sha, copyPath, settlementHash(t, copyPath), "synthetic-source", epoch.NodeID, epoch.InstallationID, f.at, seal)
		if err == nil {
			t.Fatal("contradictory source proof accepted", entry.name)
		}
	}
	t.Logf("DARKRESTORE_LOCAL_SETTLEMENT_PASS prior=%d up=%d down=%d historical=%d remaining=%d", result.PriorBytes, result.Up, result.Down, result.Historical, result.Remaining)
}
