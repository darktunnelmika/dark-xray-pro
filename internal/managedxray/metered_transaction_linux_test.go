//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type rejectMeteredRegistration struct{ checks int }

func (a *rejectMeteredRegistration) Check(context.Context, TransactionCommand, xrayconfig.Candidate) error {
	a.checks++
	return nil
}
func (*rejectMeteredRegistration) Register(context.Context, TransactionCommand, xrayconfig.Candidate, uc.ProcessToken) (uc.Epoch, error) {
	return uc.Epoch{}, ErrUsageSettlement
}

func TestMeteredTransactionProtocolExactEpochSealAndVersionSeparation(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	now := time.Now()
	c := TransactionCommand{Version: 2, RuntimeInstance: strings.Repeat("1", 32), ID: strings.Repeat("2", 32), StageID: strings.Repeat("3", 32),
		NodeID: "test-node", InstallationID: testInstall, Serial: 3, RuntimeSequence: 2, Action: "apply", PayloadSHA: strings.Repeat("a", 64), BinarySHA: strings.Repeat("b", 64),
		Issued: now.UnixNano(), Expires: now.Add(time.Minute).UnixNano(), Usage: &MeteredCondition{EpochID: strings.Repeat("4", 32), EpochSHA: strings.Repeat("c", 64), AdmissionSHA: strings.Repeat("d", 64)}}
	c.Lease, _ = SignLease(key, Lease{NodeID: c.NodeID, InstallationID: c.InstallationID, Sequence: 2, ConfigSHA: strings.Repeat("e", 64), ManifestSHA: strings.Repeat("f", 64), Issued: c.Issued, Expires: now.Add(30 * time.Second).UnixNano()})
	c, err := SignTransactionCommand(key, c)
	if err != nil {
		t.Fatal(err)
	}
	r := TransactionReceipt{Version: 2, CommandID: c.ID, CommandSHA: hashJSON(c), Serial: c.Serial, Action: c.Action, StageID: c.StageID, NodeID: c.NodeID,
		InstallationID: c.InstallationID, RuntimeInstance: c.RuntimeInstance, Outcome: "applied", ObservedAt: now.UnixNano(),
		Observed: Snapshot{ProcessReady: true, Highwater: 2, SavedSequence: 2, SavedSHA: c.Lease.ConfigSHA, ActiveSequence: 2, ActiveSHA: c.Lease.ConfigSHA, LeaseExpires: c.Lease.Expires, Usage: &UsageObservationStatus{Required: true, Running: true, SettlementRequired: true, Receipts: 1}},
		Metered:  &MeteredEvidence{After: MeteredCondition{EpochID: strings.Repeat("5", 32), EpochSHA: strings.Repeat("6", 64), AdmissionSHA: strings.Repeat("7", 64)}, PriorSettlement: uc.Receipt{EpochID: c.Usage.EpochID, Sequence: 9, SHA: strings.Repeat("8", 64)}}}
	r = signTransactionReceipt(key, r)
	if !VerifyMeteredTransactionReceipt(key, c, r) || VerifyTransactionReceipt(key, c, r) {
		t.Fatal("separate exact metered proof rejected or v1 accepted")
	}
	poll := SignTransactionPoll(key, TransactionPoll{NodeID: c.NodeID, InstallationID: c.InstallationID, BinarySHA: c.BinarySHA, At: now.UnixNano(), Status: TransactionStatus{Instance: c.RuntimeInstance, Usage: c.Usage}})
	if VerifyTransactionPoll(key, poll) {
		t.Fatal("metered observation entered the exclusive empty-route channel")
	}
	for _, mode := range []string{"missing-seal", "foreign-seal", "reused-epoch", "missing-epoch", "missing-zero-receipt", "observer-stopped", "observer-failed", "observer-settled", "old-instance", "subscription", "missing-proof", "unsigned-proof"} {
		t.Run(mode, func(t *testing.T) {
			bad := cloneMeteredReceipt(r)
			switch mode {
			case "missing-seal":
				bad.Metered.PriorSettlement = uc.Receipt{}
			case "foreign-seal":
				bad.Metered.PriorSettlement.EpochID = bad.Metered.After.EpochID
			case "reused-epoch":
				bad.Metered.After.EpochID = c.Usage.EpochID
			case "missing-epoch":
				bad.Metered.After = MeteredCondition{}
			case "missing-zero-receipt":
				bad.Observed.Usage.Receipts = 0
			case "observer-stopped":
				bad.Observed.Usage.Running = false
			case "observer-failed":
				bad.Observed.Usage.Failure = "unknown tail"
			case "observer-settled":
				bad.Observed.Usage.Settled = true
			case "old-instance":
				bad.RuntimeInstance = strings.Repeat("9", 32)
			case "subscription":
				bad.Observed.SubscriptionPublished = true
			case "missing-proof":
				bad.Metered = nil
			case "unsigned-proof":
				bad.Metered.PriorSettlement.Sequence++
			}
			if mode != "unsigned-proof" {
				bad = signTransactionReceipt(key, bad)
			}
			if VerifyMeteredTransactionReceipt(key, c, bad) {
				t.Fatal("invalid metered receipt accepted")
			}
		})
	}
	for _, mode := range []string{"v1-usage", "v2-missing-usage", "v2-missing-instance", "bad-epoch", "missing-epoch-hash", "missing-admission", "foreign-settlement"} {
		t.Run(mode, func(t *testing.T) {
			bad := c
			u := *c.Usage
			bad.Usage = &u
			switch mode {
			case "v1-usage":
				bad.Version = 1
			case "v2-missing-usage":
				bad.Usage = nil
			case "v2-missing-instance":
				bad.RuntimeInstance = ""
			case "bad-epoch":
				u.EpochID = "wrong"
			case "missing-epoch-hash":
				u.EpochSHA = ""
			case "missing-admission":
				u.AdmissionSHA = ""
			case "foreign-settlement":
				u.Settlement = r.Metered.PriorSettlement
				u.Settlement.EpochID = strings.Repeat("f", 32)
			}
			if _, e := SignTransactionCommand(key, bad); e == nil {
				t.Fatal("ambiguous command signed")
			}
		})
	}
}

func TestMeteredTransactionsStageEncryptedReplayDowngradeAndInterruptedIntent(t *testing.T) {
	o := testOptions(t)
	o.ExperimentalFinalTCP = true
	authority := &rejectMeteredRegistration{}
	// No network operation is reachable in this offline/pending-intent test.
	m := MeteredTransactionOptions{Authority: authority, Sender: &uc.Sender{}}
	r, err := OpenMeteredTransactions(o, m)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { r.Close() }()
	c, err := xrayconfig.WithUsageAPI(testCandidate(t, freePort(t), "one"), "127.0.0.1:19842")
	if err != nil {
		t.Fatal(err)
	}
	command := func(action string, serial uint64, stage string) TransactionCommand {
		cmd := transactionCommand(t, r, o, action, serial, 1, stage, c)
		s := r.Status()
		cmd.Version, cmd.RuntimeInstance, cmd.Usage = 2, s.Instance, s.Usage
		if action == "stage" {
			cmd.InterruptedCommandSHA = s.InterruptedCommandSHA
		}
		cmd, e := SignTransactionCommand(o.LeaseKey, cmd)
		if e != nil {
			t.Fatal(e)
		}
		return cmd
	}
	stage := command("stage", 1, "")
	ack, err := r.ExecuteMetered(context.Background(), stage, c)
	if err != nil || !VerifyMeteredTransactionReceipt(o.LeaseKey, stage, ack) || r.Status().Observed.ProcessReady {
		t.Fatal("offline stage", err)
	}
	if _, e := r.Execute(context.Background(), stage, c); e == nil {
		t.Fatal("metered command entered v1 executor")
	}
	mutated := cloneMeteredReceipt(ack)
	ack.Metered.After.AdmissionSHA = strings.Repeat("f", 64)
	replay, err := r.ExecuteMetered(context.Background(), stage, c)
	if err != nil || !reflect.DeepEqual(replay, mutated) || authority.checks != 1 {
		t.Fatal("replay mutated retained evidence or rechecked historical authority", err)
	}
	raw, err := os.ReadFile(filepath.Join(o.Directory, "transactions.enc"))
	if err != nil || bytes.Contains(raw, []byte("privateKey")) || bytes.Contains(raw, []byte("one.example.org")) {
		t.Fatal("unencrypted staged configuration", err)
	}
	r.Close()
	legacy := o
	legacy.ExperimentalFinalTCP = false
	if bad, e := OpenTransactions(legacy); e == nil {
		bad.Close()
		t.Fatal("v2 state downgraded to empty-route executor")
	}
	r, err = OpenMeteredTransactions(o, m)
	if err != nil {
		t.Fatal(err)
	}
	// The authenticated ACK survives restart but remains bound to its old
	// instance; retrieving it must not activate or revalidate any candidate.
	if replay, e := r.ExecuteMetered(context.Background(), stage, c); e != nil || !reflect.DeepEqual(replay, mutated) || r.Status().Observed.ProcessReady {
		t.Fatal("historical stage replay", e)
	}
	apply := command("apply", 2, stage.ID)
	// A newer issued lease can overtake a staged replacement. Reject its old
	// lease before persisting intent or draining the reviewed epoch.
	r.runtime.mu.Lock()
	r.runtime.state.LastLeaseIssued = apply.Lease.Issued
	r.runtime.mu.Unlock()
	if _, e := r.ExecuteMetered(context.Background(), apply, xrayconfig.Candidate{}); !errors.Is(e, ErrLease) || r.Status().Serial != 1 || r.Status().InterruptedCommandSHA != "" {
		t.Fatal("stale lease consumed transaction intent", e)
	}
	r.runtime.mu.Lock()
	r.runtime.state.LastLeaseIssued = 0
	r.runtime.mu.Unlock()
	r.afterIntent = func() { panic("test interruption after fsync") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("intent checkpoint not reached")
			}
		}()
		_, _ = r.ExecuteMetered(context.Background(), apply, xrayconfig.Candidate{})
	}()
	r.Close()
	r, err = OpenMeteredTransactions(o, m)
	if err != nil || r.Status().InterruptedCommandSHA == "" {
		t.Fatal("durable pending intent lost", err)
	}
	if _, e := r.ExecuteMetered(context.Background(), apply, xrayconfig.Candidate{}); e == nil {
		t.Fatal("interrupted apply automatically retried")
	}
	fresh := command("stage", 3, "")
	bad := fresh
	bad.InterruptedCommandSHA = ""
	bad, _ = SignTransactionCommand(o.LeaseKey, bad)
	if _, e := r.ExecuteMetered(context.Background(), bad, c); !errors.Is(e, ErrTransactionInterrupted) {
		t.Fatal("missing interrupted intent acknowledgement", e)
	}
	if _, e := r.ExecuteMetered(context.Background(), fresh, c); e != nil || r.Status().InterruptedCommandSHA != "" || r.Status().Observed.ProcessReady {
		t.Fatal("explicit offline restaging failed", e)
	}
}
