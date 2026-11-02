//go:build linux

package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
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
func (f *localSettlementFixture) check(t *testing.T, ctx context.Context, server *Server, cfg business.NodeConfig, o managedxray.Options, epoch uc.Epoch, final uc.Report) {
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
	// Archive only this disposable source identity through the installed API contract.
	cookie, csrf := browserLogin(t, server, ownerUsername, ownerPassword)
	lifecycle := func(action, event string) int {
		return browserRequest(t, server.Handler(), "POST", "/api/v1/workspace/clients/"+f.id+"/state",
			business.SetClientState{ClientID: f.id, Action: action, EventID: event, Reason: "Local migration source retirement"}, cookie, csrf, "https://"+browserHost).Code
	}
	if lifecycle("archive", "retire-synthetic-restore-source") != 200 || lifecycle("archive", "retire-synthetic-restore-source") != 200 || lifecycle("enable", "reject-synthetic-source-reenable") != 409 {
		t.Fatal("local source retirement or replay failed")
	}
	var expires int64
	if err = db.QueryRow("SELECT expires FROM native_runtime_delivery WHERE node_id=? AND installation_id=?", epoch.NodeID, epoch.InstallationID).Scan(&expires); err != nil {
		t.Fatal(err)
	}
	if expires <= time.Now().UnixNano() {
		t.Fatal("fixture lease expired before testing stale registration")
	}
	_, err = server.business.RegisterNativeUsageEpoch(ctx, cfg, business.UsageBirth{EpochID: strings.Repeat("f", 32), KernelBootID: epoch.KernelBootID, PID: epoch.PID, StartTicks: epoch.ProcessStartTicks + 1})
	if !errors.Is(err, business.ErrConflict) {
		t.Fatal("retired source accepted prior delivery", err)
	}
	authority := filepath.Join(f.dir, "authority.sqlite")
	if _, err = db.Exec("VACUUM INTO ?", authority); err != nil {
		t.Fatal(err)
	}
	if err = os.Chmod(authority, 0600); err != nil {
		t.Fatal(err)
	}
	pin := settlementHash(t, authority)
	inputs := darkrestore.LocalInputs{Source: f.source, SourceSHA: f.sha, Authority: authority, AuthoritySHA: pin, ID: "synthetic-source", Node: epoch.NodeID, Installation: epoch.InstallationID, AsOf: f.at}
	inspect := func(seal managedxray.LocalTCPSeal, sha, node string) (darkrestore.LocalSourceSettlement, error) {
		v := inputs
		v.SourceSHA = sha
		v.Node = node
		return darkrestore.InspectLocalSettlement(ctx, v, seal)
	}
	if _, err = inspect(seal, f.sha, epoch.NodeID); err == nil {
		t.Fatal("unexpired source lease accepted")
	}
	select {
	case <-time.After(time.Until(time.Unix(0, expires)) + time.Millisecond):
	case <-ctx.Done():
		t.Fatal(ctx.Err())
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
	exportPath := filepath.Join(f.dir, "sealed-import.sqlite")
	exported, err := darkrestore.ExportLocalImportCopy(ctx, inputs, seal, exportPath)
	if err != nil || exported.Settlement != result {
		t.Fatal("settled export", err)
	}
	same, err := darkrestore.ExportLocalImportCopy(ctx, inputs, seal, exportPath)
	if err != nil || same != exported {
		t.Fatal("export replay", err)
	}
	if settlementHash(t, exportPath) != exported.SHA || settlementHash(t, f.source) != f.sha {
		t.Fatal("export digest mismatch")
	}
	foreign := filepath.Join(f.dir, "unrelated-output")
	if err = os.WriteFile(foreign, []byte("existing file"), 0600); err != nil {
		t.Fatal(err)
	}
	before := settlementHash(t, foreign)
	if _, err = darkrestore.ExportLocalImportCopy(ctx, inputs, seal, foreign); err == nil || settlementHash(t, foreign) != before {
		t.Fatal("unrelated output replaced")
	}
	f.importAtDestination(t, ctx, exportPath, exported)
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privatePath, publicPath := filepath.Join(f.dir, "handoff.key"), filepath.Join(f.dir, "handoff.pub")
	if err = os.WriteFile(privatePath, private, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(publicPath, public, 0600); err != nil {
		t.Fatal(err)
	}
	publicSum := settlementHash(t, publicPath)
	handoffPath := filepath.Join(f.dir, "signed-handoff.json")
	signed, err := darkrestore.WriteSignedLocalHandoff(ctx, inputs, seal, privatePath, handoffPath)
	if err != nil || signed.Settlement != result || signed.PublicKeySHA != publicSum || settlementHash(t, handoffPath) != signed.SHA {
		t.Fatal("source signed handoff", err)
	}
	signedAgain, err := darkrestore.WriteSignedLocalHandoff(ctx, inputs, seal, privatePath, handoffPath)
	if err != nil || signedAgain != signed {
		t.Fatal("signed handoff retry changed bytes", err)
	}
	if _, err = darkrestore.VerifySignedLocalHandoff(handoffPath, signed.SHA, make([]byte, 32)); err == nil {
		t.Fatal("wrong public key accepted")
	}
	if _, err = darkrestore.VerifySignedLocalHandoff(handoffPath, strings.Repeat("f", 64), public); err == nil {
		t.Fatal("wrong file digest accepted")
	}
	f.attachToEarlierImport(t, ctx, inputs, seal, result, exportPath, exported.SHA, handoffPath, signed.SHA, publicPath, publicSum)
	// Exercise the packaged command with the same independently pinned copies.
	if cli := os.Getenv("DARK_PRO_TEST_SETTLEMENT_CLI"); cli != "" {
		args := []string{"-source", f.source, "-source-sha256", f.sha, "-authority", authority, "-authority-sha256", pin, "-source-id", "synthetic-source", "-node", epoch.NodeID, "-installation", epoch.InstallationID, "-as-of-unix", strconv.FormatInt(f.at.Unix(), 10), "-journal", journal, "-journal-sha256", jSHA, "-state-key-file", key, "-source-listen-ip", o.ListenIP}
		raw, err := exec.CommandContext(ctx, cli, args...).Output()
		var viaCLI darkrestore.LocalSourceSettlement
		if err != nil || json.Unmarshal(raw, &viaCLI) != nil || viaCLI != result {
			t.Fatal("packaged CLI mismatch", err)
		}
		handoffArgs := append(append([]string(nil), args...), "-export-copy", exportPath)
		raw, err = exec.CommandContext(ctx, cli, handoffArgs...).Output()
		var handoff darkrestore.LocalImportCopy
		if err != nil || json.Unmarshal(raw, &handoff) != nil || handoff != exported {
			t.Fatal("CLI export replay failed", err)
		}
		signedArgs := append(append([]string(nil), args...), "-signed-handoff-output", handoffPath, "-handoff-signing-key-file", privatePath)
		raw, err = exec.CommandContext(ctx, cli, signedArgs...).Output()
		var viaSigned darkrestore.LocalHandoffFileV1
		if err != nil || json.Unmarshal(raw, &viaSigned) != nil || viaSigned != signed {
			t.Fatal("CLI signed handoff replay failed", err)
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
		{"unretired-account", "UPDATE clients SET archived=0"},
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
		v := inputs
		v.Authority = copyPath
		v.AuthoritySHA = settlementHash(t, copyPath)
		_, err = darkrestore.InspectLocalSettlement(ctx, v, seal)
		if err == nil {
			t.Fatal("contradictory source proof accepted", entry.name)
		}
	}
	t.Logf("DARKRESTORE_LOCAL_SETTLEMENT_PASS prior=%d up=%d down=%d historical=%d remaining=%d", result.PriorBytes, result.Up, result.Down, result.Historical, result.Remaining)
}

func (f *localSettlementFixture) importAtDestination(t *testing.T, ctx context.Context, path string, handoff darkrestore.LocalImportCopy) {
	t.Helper()
	dst := newOwnershipAPI(t)
	before, err := dst.s.business.Representative(ctx, "transfer-target")
	if err != nil {
		t.Fatal(err)
	}
	if err := dst.s.EnableNativeInbounds(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err := dst.s.StageDarkRestoreImport(ctx, path, handoff.SHA, f.at.Unix()); err != nil {
		t.Fatal("stage settled import copy", err)
	}
	dst.h = dst.s.BrowserHandler()
	req := restoreQuoteRequest{ReviewSHA: dst.s.darkRestoreStage.digest, Mappings: []business.DarkRestoreGroupMappingV1{{SourceGroupID: "synthetic-group", RepresentativeID: "transfer-target"}}}
	origin := "https://" + browserHost
	w := browserRequest(t, dst.h, "POST", restoreBase+"/quote", req, dst.owner, dst.ownerCSRF, origin)
	var q business.DarkRestoreImportQuoteV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil || q.Importable != 1 || q.Blocked != 0 {
		t.Fatal("destination quote", w.Code)
	}
	commit := restoreCommitRequest{restoreQuoteRequest: req, QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix, EventID: "restore-final-local-epoch-0001"}
	for i := 0; i < 2; i++ {
		w = browserRequest(t, dst.h, "POST", restoreBase+"/commit", commit, dst.owner, dst.ownerCSRF, origin)
		var receipt business.DarkRestoreImportReceiptV1
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.Count != 1 || receipt.Replayed != (i == 1) || receipt.CreditChanged {
			t.Fatal("destination receipt/replay", w.Code)
		}
	}
	clients, err := dst.s.business.ListClients(ctx, "transfer-target", 10, 0)
	if err != nil || len(clients) != 1 || clients[0].UUID != f.uuid || clients[0].ExpiresUnix != 1893456000 || clients[0].DeploymentState != "disabled" {
		t.Fatal("destination identity or quarantine", err)
	}
	projection, err := dst.s.business.DarkRestoreUsageV1(ctx, clients[0].ID)
	total := strconv.FormatInt(handoff.Settlement.Historical, 10)
	if err != nil || projection.HistoricalBytes != total || projection.TotalBytes != total || projection.PostMigrationBytes != "0" || projection.CutoverVerified {
		t.Fatal("destination double-counted, lost history or released hold", err)
	}
	after, err := dst.s.business.Representative(ctx, "transfer-target")
	if err != nil || before.VolumeCreditBytes != after.VolumeCreditBytes || before.UnlimitedCreditUnits != after.UnlimitedCreditUnits {
		t.Fatal("destination charged on import")
	}
	w = browserRequest(t, dst.h, "GET", "/api/v1/workspace/clients/"+clients[0].ID+"/usage", nil, dst.target, "", origin)
	var visible struct {
		Restore *business.DarkRestoreUsageV1 `json:"dark_restore"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &visible) != nil || visible.Restore == nil || *visible.Restore != projection {
		t.Fatal("assigned representative cannot see exact migration totals", w.Code)
	}
	t.Log("DARKRESTORE_SETTLED_IMPORT_PASS one receipt after replay; same UUID and expiry; closed source bytes historical; zero post-migration usage")
}

func (f *localSettlementFixture) attachToEarlierImport(t *testing.T, ctx context.Context, inputs darkrestore.LocalInputs, seal managedxray.LocalTCPSeal, proof darkrestore.LocalSourceSettlement, exportPath, exportSHA, handoffPath, handoffSHA, publicPath, publicSHA string) {
	t.Helper()
	destinationPath := filepath.Join(t.TempDir(), "private", "destination.sqlite")
	server := businessLabAt(t, destinationPath)
	if _, err := server.business.BootstrapOwner(ctx, ownerUsername, ownerPassword); err != nil {
		t.Fatal(err)
	}
	dst := newOwnershipAPIWithServer(t, server)
	if err := dst.s.EnableNativeInbounds(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err := dst.s.StageDarkRestoreImport(ctx, f.source, f.sha, f.at.Unix()); err != nil {
		t.Fatal("stage original pre-epoch copy", err)
	}
	dst.h = dst.s.BrowserHandler()
	origin := "https://" + browserHost
	request := restoreQuoteRequest{ReviewSHA: dst.s.darkRestoreStage.digest, Mappings: []business.DarkRestoreGroupMappingV1{{SourceGroupID: "synthetic-group", RepresentativeID: "transfer-target"}}}
	w := browserRequest(t, dst.h, "POST", restoreBase+"/quote", request, dst.owner, dst.ownerCSRF, origin)
	var q business.DarkRestoreImportQuoteV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &q) != nil || q.Importable != 1 {
		t.Fatal("first original quote", w.Code)
	}
	commit := restoreCommitRequest{restoreQuoteRequest: request, QuoteHash: q.QuoteHash, QuoteUnix: q.QuoteUnix, EventID: "restore-pre-epoch-0001"}
	w = browserRequest(t, dst.h, "POST", restoreBase+"/commit", commit, dst.owner, dst.ownerCSRF, origin)
	var original business.DarkRestoreImportReceiptV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &original) != nil || original.Count != 1 {
		t.Fatal("original import", w.Code)
	}
	clients, err := dst.s.business.ListClients(ctx, "transfer-target", 10, 0)
	if err != nil || len(clients) != 1 || clients[0].UUID != f.uuid {
		t.Fatal("first UUID", err)
	}
	id := clients[0].ID
	initial, err := dst.s.business.DarkRestoreUsageV1(ctx, id)
	if err != nil || initial.HistoricalBytes != "310" || initial.SourceEpochSettled || initial.CutoverVerified {
		t.Fatal("pre-epoch view", initial, err)
	}
	// Only the public key and signed result cross the source/destination boundary.
	// The source journal key and the signing private key remain on the source.
	handoffDir := filepath.Join(t.TempDir(), "private")
	if err := os.MkdirAll(handoffDir, 0700); err != nil {
		t.Fatal(err)
	}
	copyPrivate := func(from, to string) {
		t.Helper()
		raw, err := os.ReadFile(from)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(to, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	stagedHandoff, stagedPublic := filepath.Join(handoffDir, "handoff.json"), filepath.Join(handoffDir, "public.key")
	copyPrivate(handoffPath, stagedHandoff)
	copyPrivate(publicPath, stagedPublic)
	if err := dst.s.StageDarkRestoreLocalHandoff(ctx, stagedHandoff, handoffSHA, stagedPublic, strings.Repeat("f", 64)); err == nil {
		t.Fatal("wrong public key pin accepted")
	}
	if err := dst.s.StageDarkRestoreLocalHandoff(ctx, stagedHandoff, strings.Repeat("f", 64), stagedPublic, publicSHA); err == nil {
		t.Fatal("wrong signed result pin accepted")
	}
	var tampered darkrestore.SignedLocalHandoffV1
	originalBytes, err := os.ReadFile(stagedHandoff)
	if err != nil || json.Unmarshal(originalBytes, &tampered) != nil {
		t.Fatal("read signed handoff", err)
	}
	tampered.Signature[0] ^= 1
	badBytes, err := json.Marshal(tampered)
	if err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(handoffDir, "tampered.json")
	if err = os.WriteFile(badPath, append(badBytes, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err = dst.s.StageDarkRestoreLocalHandoff(ctx, badPath, settlementHash(t, badPath), stagedPublic, publicSHA); err == nil {
		t.Fatal("tampered signature with matching file hash accepted")
	}
	if err := dst.s.StageDarkRestoreLocalHandoff(ctx, stagedHandoff, handoffSHA, stagedPublic, publicSHA); err != nil {
		t.Fatal("stage signed cross-host result", err)
	}
	dst.h = dst.s.BrowserHandler()
	owner, err := dst.s.business.Login(ctx, ownerUsername, ownerPassword)
	if err != nil {
		t.Fatal(err)
	}
	balance, err := dst.s.business.Representative(ctx, "transfer-target")
	if err != nil {
		t.Fatal(err)
	}
	ref := map[string]string{"handoff_sha256": handoffSHA}
	for _, bad := range []struct {
		cookie *http.Cookie
		csrf   string
	}{{dst.target, dst.targetCSRF}, {dst.owner, ""}} {
		if w := browserRequest(t, dst.h, "POST", restoreBase+"/handoff/attach", ref, bad.cookie, bad.csrf, origin); w.Code != 403 {
			t.Fatal("representative or missing CSRF attached source", w.Code)
		}
	}
	if w := browserRequest(t, dst.h, "POST", restoreBase+"/handoff/attach", map[string]string{"handoff_sha256": handoffSHA, "path": f.source}, dst.owner, dst.ownerCSRF, origin); w.Code != 422 {
		t.Fatal("browser-supplied source path accepted", w.Code)
	}
	if w := browserRequest(t, dst.h, "POST", restoreBase+"/handoff/attach", map[string]string{"handoff_sha256": strings.Repeat("f", 64)}, dst.owner, dst.ownerCSRF, origin); w.Code != 422 {
		t.Fatal("wrong signed result reference accepted", w.Code)
	}
	if err := os.WriteFile(stagedHandoff, append(badBytes, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if w := browserRequest(t, dst.h, "POST", restoreBase+"/handoff/attach", ref, dst.owner, dst.ownerCSRF, origin); w.Code != 503 {
		t.Fatal("modified signed file after startup accepted", w.Code)
	}
	if w := browserRequest(t, dst.h, "GET", restoreBase+"/handoff", nil, dst.owner, "", origin); w.Code != 503 {
		t.Fatal("modified signed file remained visible", w.Code)
	}
	if err := os.WriteFile(stagedHandoff, originalBytes, 0600); err != nil {
		t.Fatal(err)
	}
	w = browserRequest(t, dst.h, "POST", restoreBase+"/handoff/attach", ref, dst.owner, dst.ownerCSRF, origin)
	var attached business.DarkRestoreLocalAttachmentReceiptV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &attached) != nil || attached.ClientID != id || attached.OriginalEventID != original.EventID || attached.Replayed || attached.Settlement != proof || attached.HandoffSHA != handoffSHA || attached.SignerPublicSHA != publicSHA {
		t.Fatal("attach signed source", w.Code, w.Body.String())
	}
	w = browserRequest(t, dst.h, "POST", restoreBase+"/handoff/attach", ref, dst.owner, dst.ownerCSRF, origin)
	var replay business.DarkRestoreLocalAttachmentReceiptV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || !replay.Replayed || replay.Settlement != proof || replay.HandoffSHA != handoffSHA || replay.SignerPublicSHA != publicSHA {
		t.Fatal("signed attach replay", w.Code, w.Body.String())
	}
	if w := browserRequest(t, dst.h, "GET", restoreBase+"/handoff", nil, dst.target, "", origin); w.Code != 403 {
		t.Fatal("representative read private handoff", w.Code)
	}
	if w := browserRequest(t, dst.h, "GET", restoreBase+"/handoff", nil, dst.owner, "", origin); w.Code != 200 || !strings.Contains(w.Body.String(), handoffSHA) {
		t.Fatal("owner handoff view", w.Code)
	}
	projection, err := dst.s.business.DarkRestoreUsageV1(ctx, id)
	want := strconv.FormatInt(proof.Historical, 10)
	if err != nil || projection.HistoricalBytes != want || projection.TotalBytes != want || projection.PostMigrationBytes != "0" || !projection.SourceEpochSettled || projection.CutoverVerified {
		t.Fatal("late epoch omitted, double counted, or activated", projection, err)
	}
	histories, err := dst.s.business.GroupHistory(ctx, original.Groups[0].TargetGroupID)
	if err != nil || len(histories) != 1 || histories[0].SnapshotBytes != "310" ||
		histories[0].ClosedEpochBytes != strconv.FormatInt(proof.Historical-proof.PriorBytes, 10) || histories[0].HistoricalBytes != want {
		t.Fatal("source group final epoch history inaccurate", histories, err)
	}
	after, err := dst.s.business.Representative(ctx, "transfer-target")
	if err != nil || after.VolumeCreditBytes != balance.VolumeCreditBytes || after.UnlimitedCreditUnits != balance.UnlimitedCreditUnits {
		t.Fatal("attachment charged credit", err)
	}
	if _, err = dst.s.business.ExtendClientScoped(ctx, business.ExtendClient{EventID: "restore-after-settlement-renew-0001", ClientID: id, AdditionalVolumeBytes: 61, Reason: "Current representative renews settled imported identity"}, "transfer-target"); err != nil {
		t.Fatal("later renewal rejected", err)
	}
	charged, err := dst.s.business.Representative(ctx, "transfer-target")
	if err != nil || charged.VolumeCreditBytes != balance.VolumeCreditBytes-61 {
		t.Fatal("later renewal did not debit current representative", err)
	}
	recovered, err := dst.s.business.DarkRestoreImportReceiptV1(ctx, owner.Token, original.EventID)
	if err != nil || !reflect.DeepEqual(recovered, original) {
		t.Fatal("original immutable receipt changed", err)
	}
	clients, err = dst.s.business.ListClients(ctx, "transfer-target", 10, 0)
	if err != nil || len(clients) != 1 || clients[0].ID != id || clients[0].UUID != f.uuid || clients[0].DeploymentState != "disabled" {
		t.Fatal("attachment changed identity or quarantine", err)
	}
	exported, err := darkrestore.BuildReview(ctx, exportPath, exportSHA, f.at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = dst.s.business.QuoteDarkRestoreImportV1(ctx, owner.Token, business.DarkRestoreImportIntentV1{Review: exported, Mappings: request.Mappings}); !errors.Is(err, business.ErrConflict) {
		t.Fatal("settled export produced duplicate identity", err)
	}
	if err = dst.s.business.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = business.ValidateOfflineBackup(ctx, destinationPath); err != nil {
		t.Fatal("backup rejected attached epoch", err)
	}
	reopened, err := business.Open(destinationPath)
	if err != nil {
		t.Fatal("restart rejected attached epoch", err)
	}
	defer reopened.Close()
	// Restart with no source snapshot staged: the signed result can still be
	// recovered and replayed through the owner API without a source journal key.
	restarted, _, _, _ := newLab(t)
	if err := restarted.EnableBusinessLab(reopened); err != nil {
		t.Fatal(err)
	}
	if err := reopened.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.EnableClientOwnershipV1Lab(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.EnableNativeInbounds(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	if err := restarted.StageDarkRestoreLocalHandoff(ctx, stagedHandoff, handoffSHA, stagedPublic, publicSHA); err != nil {
		t.Fatal("restart handoff stage", err)
	}
	if restarted.darkRestoreStage != nil {
		t.Fatal("source snapshot incorrectly restaged")
	}
	restartedCookie, restartedCSRF := browserLogin(t, restarted, ownerUsername, ownerPassword)
	w = browserRequest(t, restarted.BrowserHandler(), "POST", restoreBase+"/handoff/attach", ref, restartedCookie, restartedCSRF, origin)
	var replayed business.DarkRestoreLocalAttachmentReceiptV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replayed) != nil || !replayed.Replayed || replayed.ClientID != id || replayed.HandoffSHA != handoffSHA || replayed.SignerPublicSHA != publicSHA {
		t.Fatal("restart duplicated attachment", w.Code, w.Body.String())
	}
	again, err := reopened.DarkRestoreUsageV1(ctx, id)
	if err != nil || again != projection {
		t.Fatal("history changed across restart", err)
	}
	payer, err := reopened.Representative(ctx, "transfer-target")
	if err != nil || payer.VolumeCreditBytes != charged.VolumeCreditBytes {
		t.Fatal("renewal payer changed across restart", err)
	}
	t.Log("DARKRESTORE_ATTACH_EARLIER_PASS original receipt stable; one UUID; late closed traffic historical; zero import debit; later renewal charged current representative; hold, backup and restart preserved")
	t.Log("DARKRESTORE_SIGNED_HANDOFF_PASS cross-host public-key pin and signature; owner CSRF; path rejection; replay and offline backup")
}
