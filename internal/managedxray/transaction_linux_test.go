//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func transactionFixture(t *testing.T) (*TransactionRuntime, Options) {
	t.Helper()
	o := testOptions(t)
	r, err := OpenTransactions(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r, o
}

func transactionCommand(t *testing.T, r *TransactionRuntime, o Options, action string, serial, revision uint64, stage string, c xrayconfig.Candidate) TransactionCommand {
	t.Helper()
	now := time.Now()
	cmd := TransactionCommand{Version: 1, ID: fmt.Sprintf("%032x", serial), Serial: serial, Action: action, StageID: stage,
		NodeID: o.NodeID, InstallationID: o.InstallationID, RuntimeSequence: revision, PayloadSHA: TransactionPayloadSHA(c),
		BinarySHA: o.BinarySHA, Expected: Condition(r.Status().Observed), Issued: now.UnixNano(), Expires: now.Add(time.Minute).UnixNano()}
	if action == "stage" {
		cmd.StageID = cmd.ID
	} else {
		cmd.Lease = grant(t, o, revision, c, 40*time.Second)
	}
	signed, err := SignTransactionCommand(o.LeaseKey, cmd)
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func executeTransaction(t *testing.T, r *TransactionRuntime, o Options, cmd TransactionCommand, c xrayconfig.Candidate, outcome string) TransactionReceipt {
	t.Helper()
	ack, err := r.Execute(context.Background(), cmd, c)
	if err != nil || ack.Outcome != outcome || !VerifyTransactionReceipt(o.LeaseKey, cmd, ack) {
		t.Fatalf("transaction %s: outcome=%s err=%v", cmd.Action, ack.Outcome, err)
	}
	return ack
}

func TestRouteTransactionsStageApplySignedAckReplayAndRollback(t *testing.T) {
	r, o := transactionFixture(t)
	port := freePort(t)
	c1, c2 := testCandidate(t, port, "one"), testCandidate(t, port, "two")
	stage := transactionCommand(t, r, o, "stage", 1, 1, "", c1)
	ack := executeTransaction(t, r, o, stage, c1, "staged")
	if ack.Observed.ProcessReady {
		t.Fatal("Stage activated a process")
	}
	assertPort(t, port, false)
	raw, err := os.ReadFile(filepath.Join(o.Directory, "transactions.enc"))
	if err != nil || bytes.Contains(raw, []byte("privateKey")) || bytes.Contains(raw, []byte("af469f46")) {
		t.Fatal("private staged config leaked", err)
	}
	apply := transactionCommand(t, r, o, "apply", 2, 1, stage.ID, c1)
	ack = executeTransaction(t, r, o, apply, xrayconfig.Candidate{}, "applied")
	assertPort(t, port, true)
	pid := r.runtime.process.cmd.Process.Pid
	if replay := executeTransaction(t, r, o, apply, xrayconfig.Candidate{}, "applied"); replay != ack || r.runtime.process.cmd.Process.Pid != pid {
		t.Fatal("lost receipt replay changed the process or receipt")
	}
	forged := ack
	forged.Observed.ActiveSHA = strings.Repeat("a", 64)
	if VerifyTransactionReceipt(o.LeaseKey, apply, forged) {
		t.Fatal("unsigned observed result accepted")
	}
	forged = ack
	forged.CommandSHA = strings.Repeat("b", 64)
	if VerifyTransactionReceipt(o.LeaseKey, apply, forged) {
		t.Fatal("cross-command receipt accepted")
	}
	stage2 := transactionCommand(t, r, o, "stage", 3, 2, "", c2)
	executeTransaction(t, r, o, stage2, c2, "staged")
	if r.runtime.process.cmd.Process.Pid != pid {
		t.Fatal("Stage restarted active process")
	}
	apply2 := transactionCommand(t, r, o, "apply", 4, 2, stage2.ID, c2)
	executeTransaction(t, r, o, apply2, xrayconfig.Candidate{}, "applied")
	if r.runtime.process.cmd.Process.Pid == pid {
		t.Fatal("Apply did not replace owned process")
	}
	rollback := transactionCommand(t, r, o, "rollback", 5, 3, stage.ID, c1)
	executeTransaction(t, r, o, rollback, xrayconfig.Candidate{}, "rolled_back")
	if s := r.Status().Observed; s.ActiveSHA != c1.SHA256 || s.ActiveSequence != 3 {
		t.Fatal("rollback did not restore previous candidate")
	}
	assertPort(t, port, true)
	renew := transactionCommand(t, r, o, "renew", 6, 3, stage.ID, c1)
	pid = r.runtime.process.cmd.Process.Pid
	executeTransaction(t, r, o, renew, xrayconfig.Candidate{}, "renewed")
	if r.runtime.process.cmd.Process.Pid != pid {
		t.Fatal("renewal restarted process")
	}
}

func TestRouteTransactionsRejectWrongScopeAlteredPayloadStaleBaseAndMetering(t *testing.T) {
	r, o := transactionFixture(t)
	c := testCandidate(t, freePort(t), "one")
	cmd := transactionCommand(t, r, o, "stage", 1, 1, "", c)
	for _, mutate := range []func(*TransactionCommand){
		func(v *TransactionCommand) { v.NodeID = "foreign" },
		func(v *TransactionCommand) { v.InstallationID = strings.Repeat("e", 32) },
		func(v *TransactionCommand) { v.BinarySHA = strings.Repeat("a", 64) },
		func(v *TransactionCommand) { v.Expected.Highwater = 100 },
		func(v *TransactionCommand) {
			v.Issued = time.Now().Add(-time.Minute).UnixNano()
			v.Expires = time.Now().Add(-time.Second).UnixNano()
		},
	} {
		bad := cmd
		mutate(&bad)
		bad, _ = SignTransactionCommand(o.LeaseKey, bad)
		if _, err := r.Execute(context.Background(), bad, c); err == nil {
			t.Fatal("invalid scoped command accepted")
		}
	}
	bad := c
	bad.Listeners = append([]xrayconfig.Listener(nil), c.Listeners...)
	bad.Listeners[0].InboundID++
	if _, err := r.Execute(context.Background(), cmd, bad); err == nil {
		t.Fatal("unsigned listener metadata accepted")
	}
	bad = c
	bad.Config = append([]byte(nil), c.Config...)
	bad.Config[0] = '!'
	if _, err := r.Execute(context.Background(), cmd, bad); err == nil {
		t.Fatal("altered private bytes accepted")
	}
	if r.Status().Serial != 0 || r.Status().Observed.ProcessReady {
		t.Fatal("rejected Stage mutated runtime")
	}
	metered, err := xrayconfig.WithUsageAPI(c, "127.0.0.1:19777")
	if err != nil {
		t.Fatal(err)
	}
	meteredCommand := transactionCommand(t, r, o, "stage", 1, 1, "", metered)
	if _, err = r.Execute(context.Background(), meteredCommand, metered); err == nil {
		t.Fatal("metering admission bypass accepted")
	}
	executeTransaction(t, r, o, cmd, c, "staged")
	apply := transactionCommand(t, r, o, "apply", 2, 1, cmd.ID, c)
	apply.Expected.SavedSHA = strings.Repeat("a", 64)
	apply, _ = SignTransactionCommand(o.LeaseKey, apply)
	if _, err = r.Execute(context.Background(), apply, xrayconfig.Candidate{}); err == nil {
		t.Fatal("stale review base accepted")
	}
	assertPort(t, c.Listeners[0].NodePort, false)
}

func TestRouteTransactionsRestartKeepsStageAndHistoricalReceiptNeverStarts(t *testing.T) {
	r, o := transactionFixture(t)
	c := testCandidate(t, freePort(t), "one")
	stage := transactionCommand(t, r, o, "stage", 1, 1, "", c)
	executeTransaction(t, r, o, stage, c, "staged")
	r.Close()
	r2, err := OpenTransactions(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r2.Close)
	if r2.Status().StagedID != stage.ID || r2.Status().Observed.ProcessReady {
		t.Fatal("restart lost stage or started listener")
	}
	apply := transactionCommand(t, r2, o, "apply", 2, 1, stage.ID, c)
	ack := executeTransaction(t, r2, o, apply, xrayconfig.Candidate{}, "applied")
	r2.Close()
	r3, err := OpenTransactions(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r3.Close)
	replayed := executeTransaction(t, r3, o, apply, xrayconfig.Candidate{}, "applied")
	if replayed != ack || replayed.RuntimeInstance == r3.Status().Instance || r3.Status().Observed.ProcessReady {
		t.Fatal("historical receipt was treated as fresh activation")
	}
	assertPort(t, c.Listeners[0].NodePort, false)
}

func TestRouteTransactionsFailedApplyRetainsGoodAndReceiptWriteFailureFences(t *testing.T) {
	r, o := transactionFixture(t)
	port := freePort(t)
	good, bad := testCandidate(t, port, "one"), testCandidate(t, port, "reject")
	s1 := transactionCommand(t, r, o, "stage", 1, 1, "", good)
	executeTransaction(t, r, o, s1, good, "staged")
	a1 := transactionCommand(t, r, o, "apply", 2, 1, s1.ID, good)
	executeTransaction(t, r, o, a1, xrayconfig.Candidate{}, "applied")
	s2 := transactionCommand(t, r, o, "stage", 3, 2, "", bad)
	executeTransaction(t, r, o, s2, bad, "staged")
	a2 := transactionCommand(t, r, o, "apply", 4, 2, s2.ID, bad)
	ack, err := r.Execute(context.Background(), a2, xrayconfig.Candidate{})
	if err == nil || ack.Outcome != "failed_previous_retained" || !VerifyTransactionReceipt(o.LeaseKey, a2, ack) || r.Status().Observed.ActiveSHA != good.SHA256 {
		t.Fatal("failed Apply lost previous good or returned false success", err)
	}
	if replay, replayErr := r.Execute(context.Background(), a2, xrayconfig.Candidate{}); replayErr == nil || replay != ack {
		t.Fatal("replaying a failed transaction changed its outcome")
	}
	assertPort(t, port, true)
	other := testCandidate(t, port, "two")
	s3 := transactionCommand(t, r, o, "stage", 5, 3, "", other)
	executeTransaction(t, r, o, s3, other, "staged")
	a3 := transactionCommand(t, r, o, "apply", 6, 3, s3.ID, other)
	r.afterChange = func() { r.journal.dir = filepath.Join(o.Directory, "missing") }
	ack, err = r.Execute(context.Background(), a3, xrayconfig.Candidate{})
	if err == nil || ack.Signature != "" || r.Status().Observed.ProcessReady {
		t.Fatal("unpersisted receipt left process serving")
	}
	assertPort(t, port, false)
}

func TestRouteTransactionCrashHelper(t *testing.T) {
	encoded := os.Getenv("DARK_PRO_TRANSACTION_CRASH_HELPER")
	if encoded == "" {
		return
	}
	var args struct {
		Options Options
		Port    uint16
		Marker  string
	}
	raw, _ := base64.StdEncoding.DecodeString(encoded)
	if json.Unmarshal(raw, &args) != nil {
		os.Exit(30)
	}
	r, err := OpenTransactions(args.Options)
	if err != nil {
		os.Exit(31)
	}
	defer r.Close()
	c := testCandidate(t, args.Port, "one")
	s := transactionCommand(t, r, args.Options, "stage", 1, 1, "", c)
	executeTransaction(t, r, args.Options, s, c, "staged")
	r.afterChange = func() {
		if os.WriteFile(args.Marker, []byte("changed-before-receipt"), 0600) != nil {
			os.Exit(32)
		}
		time.Sleep(30 * time.Second)
	}
	a := transactionCommand(t, r, args.Options, "apply", 2, 1, s.ID, c)
	_, _ = r.Execute(context.Background(), a, xrayconfig.Candidate{})
}

func TestRouteTransactionsCrashAfterApplyNeedsExplicitRestaging(t *testing.T) {
	o := testOptions(t)
	port := freePort(t)
	marker := filepath.Join(t.TempDir(), "crash-marker")
	raw, _ := json.Marshal(struct {
		Options Options
		Port    uint16
		Marker  string
	}{o, port, marker})
	child := exec.Command(os.Args[0], "-test.run=^TestRouteTransactionCrashHelper$", "-test.timeout=40s")
	child.Env = append(os.Environ(), "DARK_PRO_TRANSACTION_CRASH_HELPER="+base64.StdEncoding.EncodeToString(raw))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill() })
	waitFor(t, func() bool { _, err := os.Stat(marker); return err == nil })
	assertPort(t, port, true)
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = child.Wait()
	r, err := OpenTransactions(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	s := r.Status()
	if s.InterruptedCommandSHA == "" || s.Observed.ProcessReady {
		t.Fatal("interrupted operation appeared successful")
	}
	c := testCandidate(t, port, "one")
	apply := transactionCommand(t, r, o, "apply", 3, 2, fmt.Sprintf("%032x", 1), c)
	if _, err = r.Execute(context.Background(), apply, xrayconfig.Candidate{}); !errors.Is(err, ErrTransactionInterrupted) {
		t.Fatal("ambiguous operation restarted without review", err)
	}
	stage := transactionCommand(t, r, o, "stage", 3, 2, "", c)
	stage.InterruptedCommandSHA = s.InterruptedCommandSHA
	stage, _ = SignTransactionCommand(o.LeaseKey, stage)
	executeTransaction(t, r, o, stage, c, "staged")
	apply = transactionCommand(t, r, o, "apply", 4, 2, stage.ID, c)
	executeTransaction(t, r, o, apply, xrayconfig.Candidate{}, "applied")
	assertPort(t, port, true)
}

func TestRouteTransactionsRejectCorruptStateAndPublicListener(t *testing.T) {
	ordinaryOptions := testOptions(t)
	ordinary := mustOpen(t, ordinaryOptions)
	if _, err := OpenTransactions(ordinaryOptions); !errors.Is(err, ErrPrivate) {
		t.Fatal("ordinary native directory reused before its first apply", err)
	}
	if ordinary.Snapshot().Phase != "awaiting_fresh_lease" {
		t.Fatal("ordinary supervisor was changed")
	}
	r, o := transactionFixture(t)
	c := testCandidate(t, freePort(t), "one")
	s := transactionCommand(t, r, o, "stage", 1, 1, "", c)
	executeTransaction(t, r, o, s, c, "staged")
	r.Close()
	path := filepath.Join(o.Directory, "transactions.enc")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 1
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenTransactions(o); !errors.Is(err, ErrCorrupt) {
		t.Fatal("tampered staged candidate accepted", err)
	}
	o.ListenIP = "192.0.2.10"
	if _, err = OpenTransactions(o); !errors.Is(err, ErrTransaction) {
		t.Fatal("public runtime admitted by loopback gate", err)
	}
}

func TestRouteTransactionsConcurrentStageConsumesOnlyOneSerial(t *testing.T) {
	r, o := transactionFixture(t)
	c1, c2 := testCandidate(t, freePort(t), "one"), testCandidate(t, freePort(t), "two")
	one, two := transactionCommand(t, r, o, "stage", 1, 1, "", c1), transactionCommand(t, r, o, "stage", 1, 1, "", c2)
	results := make(chan error, 2)
	go func() { _, err := r.Execute(context.Background(), one, c1); results <- err }()
	go func() { _, err := r.Execute(context.Background(), two, c2); results <- err }()
	a, b := <-results, <-results
	if (a == nil) == (b == nil) {
		t.Fatal("conflicting same-serial commands were not serialized", a, b)
	}
	if r.Status().Serial != 1 {
		t.Fatal("unexpected serial")
	}
	assertPort(t, c1.Listeners[0].NodePort, false)
	assertPort(t, c2.Listeners[0].NodePort, false)
}

func TestRouteTransactionsExpiredLeaseNeverRevivedByReceiptReplay(t *testing.T) {
	r, o := transactionFixture(t)
	c := testCandidate(t, freePort(t), "one")
	s := transactionCommand(t, r, o, "stage", 1, 1, "", c)
	executeTransaction(t, r, o, s, c, "staged")
	a := transactionCommand(t, r, o, "apply", 2, 1, s.ID, c)
	a.Lease = grant(t, o, 1, c, time.Second)
	a, _ = SignTransactionCommand(o.LeaseKey, a)
	ack := executeTransaction(t, r, o, a, xrayconfig.Candidate{}, "applied")
	waitFor(t, func() bool { return r.Status().Observed.Phase == "lease_expired_stopped" })
	if replay := executeTransaction(t, r, o, a, xrayconfig.Candidate{}, "applied"); replay != ack {
		t.Fatal("receipt replay changed historical evidence")
	}
	assertPort(t, c.Listeners[0].NodePort, false)
	rollback := transactionCommand(t, r, o, "rollback", 3, 2, s.ID, c)
	executeTransaction(t, r, o, rollback, xrayconfig.Candidate{}, "rolled_back")
	assertPort(t, c.Listeners[0].NodePort, true)
}

func TestRouteTransactionsRestrictiveFailureRequiresFreshRollbackAuthority(t *testing.T) {
	r, o := transactionFixture(t)
	port := freePort(t)
	good := testCandidate(t, port, "one")
	s := transactionCommand(t, r, o, "stage", 1, 1, "", good)
	executeTransaction(t, r, o, s, good, "staged")
	a := transactionCommand(t, r, o, "apply", 2, 1, s.ID, good)
	executeTransaction(t, r, o, a, xrayconfig.Candidate{}, "applied")
	bad := testCandidate(t, port, "reject")
	var doc map[string]any
	if json.Unmarshal(bad.Config, &doc) != nil {
		t.Fatal("bad fixture")
	}
	doc["inbounds"].([]any)[0].(map[string]any)["settings"].(map[string]any)["clients"] = []any{}
	bad.Config, _ = json.Marshal(doc)
	digest := sha256.Sum256(bad.Config)
	bad.SHA256 = hex.EncodeToString(digest[:])
	digest = sha256.Sum256([]byte("restrictive-fixture:" + bad.SHA256))
	bad.ManifestSHA256 = hex.EncodeToString(digest[:])
	s2 := transactionCommand(t, r, o, "stage", 3, 2, "", bad)
	executeTransaction(t, r, o, s2, bad, "staged")
	a2 := transactionCommand(t, r, o, "apply", 4, 2, s2.ID, bad)
	ack, err := r.Execute(context.Background(), a2, xrayconfig.Candidate{})
	if err == nil || ack.Outcome != "failed_stopped" || !VerifyTransactionReceipt(o.LeaseKey, a2, ack) {
		t.Fatal("restrictive failure restored removed authority", err)
	}
	assertPort(t, port, false)
	rollback := transactionCommand(t, r, o, "rollback", 5, 3, s.ID, good)
	executeTransaction(t, r, o, rollback, xrayconfig.Candidate{}, "rolled_back")
	assertPort(t, port, true)
}

func TestRouteTransactionsAutoRollbackKeepsActualLastGood(t *testing.T) {
	r, o := transactionFixture(t)
	port := freePort(t)
	c1, c2, c3 := testCandidate(t, port, "one"), testCandidate(t, port, "two"), testCandidate(t, port, "three")
	s1 := transactionCommand(t, r, o, "stage", 1, 1, "", c1)
	executeTransaction(t, r, o, s1, c1, "staged")
	a1 := transactionCommand(t, r, o, "apply", 2, 1, s1.ID, c1)
	executeTransaction(t, r, o, a1, xrayconfig.Candidate{}, "applied")
	s2 := transactionCommand(t, r, o, "stage", 3, 2, "", c2)
	executeTransaction(t, r, o, s2, c2, "staged")
	a2 := transactionCommand(t, r, o, "apply", 4, 2, s2.ID, c2)
	executeTransaction(t, r, o, a2, xrayconfig.Candidate{}, "applied")
	r.runtime.mu.Lock()
	child := r.runtime.process
	r.runtime.mu.Unlock()
	child.stop()
	waitFor(t, func() bool { return r.Status().Observed.ActiveSHA == c1.SHA256 })
	s3 := transactionCommand(t, r, o, "stage", 5, 3, "", c3)
	executeTransaction(t, r, o, s3, c3, "staged")
	a3 := transactionCommand(t, r, o, "apply", 6, 3, s3.ID, c3)
	executeTransaction(t, r, o, a3, xrayconfig.Candidate{}, "applied")
	rollback := transactionCommand(t, r, o, "rollback", 7, 4, s1.ID, c1)
	executeTransaction(t, r, o, rollback, xrayconfig.Candidate{}, "rolled_back")
	if r.Status().Observed.ActiveSHA != c1.SHA256 {
		t.Fatal("rollback restored the crashed revision")
	}
}

func TestRouteTransactionsBoundInstanceRejectsNewExecutionButReplaysHistory(t *testing.T) {
	o := testOptions(t)
	r, e := OpenTransactions(o)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	candidate := testCandidate(t, freePort(t), "one")
	cmd := transactionCommand(t, r, o, "stage", 1, 1, "", candidate)
	cmd.RuntimeInstance = strings.Repeat("f", 32)
	cmd, _ = SignTransactionCommand(o.LeaseKey, cmd)
	if _, e = r.Execute(context.Background(), cmd, candidate); e == nil {
		t.Fatal("command for another executor instance accepted")
	}
	cmd.RuntimeInstance = r.Status().Instance
	cmd, _ = SignTransactionCommand(o.LeaseKey, cmd)
	ack, e := r.Execute(context.Background(), cmd, candidate)
	if e != nil {
		t.Fatal(e)
	}
	r.Close()
	restarted, e := OpenTransactions(o)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	replay, e := restarted.Execute(context.Background(), cmd, candidate)
	if e != nil || replay.Signature != ack.Signature || restarted.Status().Observed.ProcessReady {
		t.Fatal("historical receipt unavailable or replay activated process", e)
	}
	next := transactionCommand(t, restarted, o, "stage", 2, 1, "", candidate)
	next.RuntimeInstance = cmd.RuntimeInstance
	next, _ = SignTransactionCommand(o.LeaseKey, next)
	if _, e = restarted.Execute(context.Background(), next, candidate); e == nil {
		t.Fatal("fresh execution used pre-restart instance authority")
	}
	next.RuntimeInstance = restarted.Status().Instance
	next, _ = SignTransactionCommand(o.LeaseKey, next)
	if _, e = restarted.Execute(context.Background(), next, candidate); e != nil {
		t.Fatal(e)
	}
}
