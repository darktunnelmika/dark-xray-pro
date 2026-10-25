//go:build linux

package inboundruntime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

var errLostReceipt = errors.New("synthetic lost receipt after durable commit")

type fixtureProcess struct {
	values, finalValues                  []ix.Counter
	stops, activations, finals, renewals int
	identity                             string
	dead                                 bool
}

func (p *fixtureProcess) Stop()                { p.stops++ }
func (p *fixtureProcess) Renew(time.Time) bool { p.renewals++; return true }
func (p *fixtureProcess) Identity() string     { return p.identity }
func (p *fixtureProcess) Alive() bool          { return !p.dead }
func (p *fixtureProcess) BinarySHAs() map[string]string {
	return map[string]string{"xray": strings.Repeat("b", 64)}
}
func (p *fixtureProcess) Activate(context.Context, string) error { p.activations++; return nil }
func (p *fixtureProcess) Health(context.Context) error           { return nil }
func (p *fixtureProcess) Counters(context.Context) ([]ix.Counter, error) {
	return append([]ix.Counter(nil), p.values...), nil
}
func (p *fixtureProcess) Final(context.Context, string) ([]ix.Counter, error) {
	p.finals++
	values := p.finalValues
	if values == nil {
		values = p.values
	}
	return append([]ix.Counter(nil), values...), nil
}

type fixtureExchange struct {
	delivery                              *Delivery
	registerCalls, usageCalls, polls      int
	loseRegister, loseOrdinary, loseFinal bool
	badReceipt                            bool
	receipts                              map[uint64]ix.UsageReport
	observations                          []ix.Observation
	pollInputs                            []PollInput
}

func (x *fixtureExchange) Poll(_ context.Context, in PollInput) (*Delivery, error) {
	x.polls++
	x.pollInputs = append(x.pollInputs, in)
	return x.delivery, nil
}
func (x *fixtureExchange) Register(_ context.Context, in ix.EpochInput) (EpochReceipt, error) {
	x.registerCalls++
	if x.loseRegister {
		x.loseRegister = false
		return EpochReceipt{}, errLostReceipt
	}
	return EpochReceipt{EpochID: in.EpochID, ProcessIdentity: in.ProcessIdentity, Registered: true}, nil
}
func (x *fixtureExchange) Usage(_ context.Context, in ix.UsageReport) (ix.UsageReceipt, error) {
	x.usageCalls++
	if x.receipts == nil {
		x.receipts = map[uint64]ix.UsageReport{}
	}
	previous, replay := x.receipts[in.Sequence]
	if replay && ReportSHA(previous) != ReportSHA(in) {
		return ix.UsageReceipt{}, ErrProof
	}
	x.receipts[in.Sequence] = in
	if !in.Final && x.loseOrdinary {
		x.loseOrdinary = false
		return ix.UsageReceipt{}, errLostReceipt
	}
	if in.Final && x.loseFinal {
		x.loseFinal = false
		return ix.UsageReceipt{}, errLostReceipt
	}
	r := ix.UsageReceipt{EpochID: in.EpochID, Sequence: in.Sequence, SHA256: ReportSHA(in), Final: in.Final, Replayed: replay}
	if x.badReceipt {
		r.SHA256 = strings.Repeat("0", 64)
	}
	return r, nil
}
func (x *fixtureExchange) Observe(_ context.Context, in ix.Observation) error {
	x.observations = append(x.observations, in)
	return nil
}

func fixtureDelivery(t *testing.T, key []byte, b ix.Bundle) *Delivery {
	t.Helper()
	sealed, err := ix.SealBundle(key, b)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	l, err := SignLease(key, Lease{Version: 1, NodeID: b.NodeID, InstallationID: b.InstallationID, Generation: b.Generation, ConfigSHA: sealed.ConfigSHA, ManifestSHA: sealed.ManifestSHA, Issued: now.UnixNano(), Expires: now.Add(30 * time.Second).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	return &Delivery{Bundle: sealed, Lease: l}
}

func fixtureWorker(t *testing.T, phase string) (*Worker, *fixtureProcess, *fixtureExchange) {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "pinned-engine")
	contents := []byte("synthetic pinned binary, never executed by state-machine tests\n")
	if err := os.WriteFile(binary, contents, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Directory: filepath.Join(dir, "journal"), NodeID: "runtime-node", InstallationID: strings.Repeat("a", 32), Key: []byte(strings.Repeat("k", 32)), Native: BinaryConfig{Path: binary, SHA256: hashBytes(contents)}}
	w, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	b := ix.Bundle{Version: 1, NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, Generation: 1, OperationID: "fixture-operation-0001", NativeConfig: []byte(`{"inbounds":[]}`), Revisions: []ix.RevisionRef{{InboundID: 501, Revision: 1, PlanRevision: 1, InboundRevision: 1}}, Listeners: []ix.Listener{{InboundID: 501, Tag: "customer-listener", Engine: "xray", ResourceKind: "socket", Listen: "127.0.0.1", Port: 20001, Network: "tcp"}}, Principals: []ix.Principal{{ID: "customer-0001", Engine: "xray", ClientID: "retail-client", Role: "customer", Counter: "user>>>counter-0001>>>traffic", Billable: true}}}
	delivery := fixtureDelivery(t, cfg.Key, b)
	b = delivery.Bundle
	member, _ := json.Marshal(ix.ProcessMember{BootID: "00000000-0000-0000-0000-000000000001", PID: 1, StartTicks: 1, BinarySHA: strings.Repeat("b", 64), ConfigSHA: strings.Repeat("c", 64)})
	identity, _ := json.Marshal(map[string]string{"xray": string(member)})
	p := &fixtureProcess{identity: string(identity), values: []ix.Counter{{PrincipalID: "customer-0001"}}}
	w.state = journal{Version: 1, NodeID: cfg.NodeID, InstallationID: cfg.InstallationID, Generation: 1, Bundle: &b, Epoch: &ix.EpochInput{EpochID: "fixture-epoch-0001", NodeID: b.NodeID, InstallationID: b.InstallationID, Generation: 1, ConfigSHA: b.ConfigSHA, ManifestSHA: b.ManifestSHA, ProcessIdentity: p.identity, BinarySHAs: p.BinarySHAs()}, Phase: phase, ActivateNonce: strings.Repeat("a", 64)}
	if phase != "prepared" && phase != "registered" {
		w.state.Sequence = 1
		w.state.LastCounters = append([]ix.Counter(nil), p.values...)
	}
	w.process = p
	w.deadline = time.Now().Add(30 * time.Second)
	if err = w.save(); err != nil {
		t.Fatal(err)
	}
	x := &fixtureExchange{delivery: delivery, receipts: map[uint64]ix.UsageReport{}}
	return w, p, x
}

func stoppedDelivery(t *testing.T, w *Worker) *Delivery {
	t.Helper()
	b := cloneBundle(*w.state.Bundle)
	b.Generation++
	b.OperationID = "fixture-stop-operation-0002"
	b.NativeConfig = nil
	b.Listeners = []ix.Listener{}
	b.Principals = []ix.Principal{}
	b.Components = []ix.Component{}
	return fixtureDelivery(t, w.cfg.Key, b)
}

func TestWorkerLostRegistrationReceiptReplaysBeforeAdmission(t *testing.T) {
	w, p, x := fixtureWorker(t, "prepared")
	x.loseRegister = true
	if err := w.Step(context.Background(), x); !errors.Is(err, errLostReceipt) {
		t.Fatal(err)
	}
	if p.activations != 0 || w.state.Phase != "prepared" || x.usageCalls != 0 {
		t.Fatal("ambiguous registration admitted traffic")
	}
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if x.registerCalls != 2 || p.activations != 1 || x.polls != 1 || w.state.Phase != "active" {
		t.Fatal("registration did not replay before fresh admission authority")
	}
}

func TestWorkerLostZeroReceiptThenWithdrawalNeverActivates(t *testing.T) {
	w, p, x := fixtureWorker(t, "prepared")
	x.loseOrdinary = true
	if err := w.Step(context.Background(), x); !errors.Is(err, errLostReceipt) {
		t.Fatal(err)
	}
	if w.state.Pending == nil || p.activations != 0 || w.state.Pending.Sequence != 1 {
		t.Fatal("zero outbox was not retained")
	}
	x.delivery = stoppedDelivery(t, w)
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if p.activations != 0 || p.finals != 1 || len(x.receipts) != 2 || !x.receipts[2].Final || w.state.Epoch != nil {
		t.Fatal("withdrawn closed generation was not sealed")
	}
	if x.receipts[1].Final || x.receipts[2].Counters[0].Upload != 0 {
		t.Fatal("zero/final accounting confused")
	}
}

func TestWorkerLostFinalReceiptReopensExactOutbox(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	p.values[0].Upload, p.values[0].Download = 123, 456
	x.delivery = stoppedDelivery(t, w)
	x.loseFinal = true
	if err := w.Step(context.Background(), x); !errors.Is(err, errLostReceipt) {
		t.Fatal(err)
	}
	if w.state.Pending == nil || !w.state.Pending.Final || w.state.Pending.Sequence != 3 {
		t.Fatal("missing durable final outbox")
	}
	sha := ReportSHA(*w.state.Pending)
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if len(x.receipts) != 2 || ReportSHA(x.receipts[3]) != sha || x.receipts[3].Counters[0].Upload != 123 || reopened.state.Epoch != nil || reopened.state.Generation != 2 {
		t.Fatal("final response loss changed or duplicated financial report")
	}
}

func TestWorkerSealedSampleSurvivesEarlierReceiptLossAndRestart(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	w.state.Sequence = 2
	w.state.LastCounters = []ix.Counter{{PrincipalID: "customer-0001", Upload: 20, Download: 30}}
	r := w.makeReport(w.state.LastCounters, false)
	w.state.Pending = &r
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	p.finalValues = []ix.Counter{{PrincipalID: "customer-0001", Upload: 25, Download: 45}}
	w.deadline = time.Now().Add(8 * time.Second)
	x.loseOrdinary = true
	x.delivery = stoppedDelivery(t, w)
	if err := w.Step(context.Background(), x); !errors.Is(err, errLostReceipt) {
		t.Fatal(err)
	}
	if w.state.Phase != "sealed_waiting_receipt" || w.state.Pending == nil || w.state.Pending.Final || w.state.LastCounters[0].Download != 45 {
		t.Fatal("final sample overwrote an ambiguous ordinary report")
	}
	cfg := w.cfg
	w.Close()
	reopened, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err = reopened.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if len(x.receipts) != 2 || !x.receipts[3].Final || x.receipts[3].Counters[0].Upload != 25 || x.receipts[2].Counters[0].Upload != 20 || reopened.state.Phase != "idle" {
		t.Fatal("sealed cumulative sample lost or replay changed bytes")
	}
}

func TestWorkerJournalFailureFencesUndurableReportAndOwnedProcess(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	p.values[0].Upload = 19
	oldDir := w.journal.dir
	badPath := filepath.Join(t.TempDir(), "ordinary-file")
	if err := os.WriteFile(badPath, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	w.journal.dir = badPath
	if err := w.Step(context.Background(), x); !errors.Is(err, ErrPersistence) {
		t.Fatal(err)
	}
	if x.usageCalls != 0 || x.polls != 0 || p.stops != 1 || !w.persistFailed {
		t.Fatal("non-durable report escaped or traffic survived journal failure")
	}
	w.journal.dir = oldDir
	if err := w.Step(context.Background(), x); !errors.Is(err, ErrPersistence) {
		t.Fatal(err)
	}
	if x.usageCalls != 0 || x.polls != 0 || len(x.observations) != 1 || x.observations[0].AccountingState != "uncertain" {
		t.Fatal("restoring path silently resumed speculative state")
	}
}

func TestWorkerWrongReceiptCannotAdvancePendingCursor(t *testing.T) {
	w, p, x := fixtureWorker(t, "prepared")
	x.badReceipt = true
	if err := w.Step(context.Background(), x); !errors.Is(err, ErrProof) {
		t.Fatal(err)
	}
	if w.state.Pending == nil || w.state.Sequence != 1 || p.activations != 0 {
		t.Fatal("foreign receipt opened admission")
	}
	x.badReceipt = false
	if err := w.Step(context.Background(), x); err != nil {
		t.Fatal(err)
	}
	if p.activations != 1 || len(x.receipts) != 1 {
		t.Fatal("exact receipt replay failed")
	}
}

func TestWorkerLostIncarnationHoldsBeforeReplayingOrdinaryOutbox(t *testing.T) {
	w, p, x := fixtureWorker(t, "active")
	w.state.Sequence = 2
	w.state.LastCounters = []ix.Counter{{PrincipalID: "customer-0001", Upload: 21, Download: 34}}
	r := w.makeReport(w.state.LastCounters, false)
	w.state.Pending = &r
	if err := w.save(); err != nil {
		t.Fatal(err)
	}
	p.dead = true
	x.badReceipt = true
	if err := w.Step(context.Background(), x); !errors.Is(err, ErrProof) {
		t.Fatal(err)
	}
	if w.state.Phase != "uncertain" || w.process != nil || p.stops != 1 || len(x.observations) != 1 || x.observations[0].AccountingState != "uncertain" || x.polls != 0 {
		t.Fatal("broken outbox delayed uncertainty or permitted a replacement")
	}
	x.badReceipt = false
	if err := w.Step(context.Background(), x); !errors.Is(err, ErrUncertain) {
		t.Fatal(err)
	}
	if w.state.Pending != nil || len(x.receipts) != 1 || x.receipts[2].Counters[0].Upload != 21 || p.activations != 0 {
		t.Fatal("known cumulative bytes were lost or unknown tail was forgiven")
	}
}

func TestWorkerLostPreparedOrDrainingIncarnationBecomesUncertain(t *testing.T) {
	for _, phase := range []string{"prepared", "registered", "zero_pending", "activation_intent", "draining"} {
		t.Run(phase, func(t *testing.T) {
			w, p, x := fixtureWorker(t, phase)
			p.dead = true
			if err := w.Step(context.Background(), x); !errors.Is(err, ErrUncertain) {
				t.Fatal(err)
			}
			if w.state.Phase != "uncertain" || w.process != nil || len(x.observations) != 1 || x.registerCalls != 0 || x.polls != 1 {
				t.Fatal("lost non-active incarnation retried without a hold")
			}
		})
	}
}

func TestNativeStopKillsOwnedHandleWhenBirthIdentityUnavailable(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &Native{cmd: cmd, done: make(chan struct{}), dir: t.TempDir()}
	go func() { _ = cmd.Wait(); close(p.done) }()
	finished := make(chan struct{})
	go func() { p.Stop(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("unmeasured owned birth remained alive")
	}
}

func TestJournalRejectsAuthenticatedButInconsistentRecoveryState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*journal)
	}{
		{"generation", func(j *journal) { j.Generation++ }},
		{"foreign_process", func(j *journal) { j.Pending.ProcessIdentity = "foreign-process" }},
		{"cursor", func(j *journal) { j.Pending.Sequence++ }},
		{"regressed_final_sample", func(j *journal) { j.LastCounters[0].Upload = 0 }},
		{"unknown_phase", func(j *journal) { j.Phase = "ready-ish" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, _, _ := fixtureWorker(t, "active")
			w.state.Sequence = 2
			w.state.LastCounters = []ix.Counter{{PrincipalID: "customer-0001", Upload: 10}}
			r := w.makeReport(append([]ix.Counter(nil), w.state.LastCounters...), false)
			w.state.Pending = &r
			tc.change(&w.state)
			if err := w.journal.save(w.state); err != nil {
				t.Fatal(err)
			}
			cfg := w.cfg
			w.Close()
			bad, err := Open(cfg)
			if err == nil {
				bad.Close()
				t.Fatal("inconsistent encrypted journal recovered")
			}
		})
	}
}

func TestJournalRejectsSymlinkAncestorBeforeCreatingState(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "redirect")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	j, _, err := openJournal(filepath.Join(link, "private"), []byte(strings.Repeat("k", 32)), "runtime-node", strings.Repeat("a", 32))
	if err == nil {
		j.close()
		t.Fatal("symlink ancestor accepted")
	}
	if _, err = os.Stat(filepath.Join(target, "private")); !os.IsNotExist(err) {
		t.Fatal("wrote through rejected ancestor")
	}
}

func TestExecutionLeaseBindsEveryBundleAssetAndPrincipal(t *testing.T) {
	w, _, x := fixtureWorker(t, "prepared")
	b := cloneBundle(x.delivery.Bundle)
	b.Components = []ix.Component{{Engine: "mtproto", Tag: "mtg-one", Config: []byte(`{"secret":"first"}`), Manifest: []byte(`{"mode":"authoritative"}`), SHA256: strings.Repeat("e", 64)}}
	d := fixtureDelivery(t, w.cfg.Key, b)
	if err := VerifyDelivery(w.cfg.Key, *d, w.cfg.NodeID, w.cfg.InstallationID, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*ix.Bundle)
	}{
		{"native", func(b *ix.Bundle) { b.NativeConfig = []byte(`{"inbounds":[{}]}`) }},
		{"external", func(b *ix.Bundle) { b.Components[0].Config = []byte(`{"secret":"second"}`) }},
		{"manifest", func(b *ix.Bundle) { b.Components[0].Manifest = []byte(`{"mode":"unmetered"}`) }},
		{"listener", func(b *ix.Bundle) { b.Listeners[0].Port++ }},
		{"client", func(b *ix.Bundle) { b.Principals[0].ClientID = "foreign-client" }},
		{"revision", func(b *ix.Bundle) { b.Revisions[0].Revision++ }},
		{"operation", func(b *ix.Bundle) { b.OperationID = "foreign-operation-0001" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := *d
			bad.Bundle = cloneBundle(d.Bundle)
			tc.change(&bad.Bundle)
			if VerifyDelivery(w.cfg.Key, bad, w.cfg.NodeID, w.cfg.InstallationID, time.Now()) == nil {
				t.Fatal("lease accepted asset substitution")
			}
		})
	}
}
