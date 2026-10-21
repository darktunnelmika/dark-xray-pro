//go:build linux

package managedxray

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

type transactionStage struct {
	UsageBase  *MeteredCondition `json:"usage_base,omitempty"`
	ID         string            `json:"id"`
	PayloadSHA string            `json:"payload_sha"`
	Expires    int64             `json:"expires"`
	Base       RuntimeCondition  `json:"base"`
	Revision   *savedRevision    `json:"revision"`
}

type transactionState struct {
	Version     int                 `json:"version"`
	Serial      uint64              `json:"serial"`
	Staged      *transactionStage   `json:"staged,omitempty"`
	Current     *transactionStage   `json:"current,omitempty"`
	Previous    *transactionStage   `json:"previous,omitempty"`
	Pending     *TransactionCommand `json:"pending,omitempty"`
	LastCommand *TransactionCommand `json:"last_command,omitempty"`
	LastReceipt *TransactionReceipt `json:"last_receipt,omitempty"`
}

// TransactionRuntime owns a DIFFERENT, private runtime from the ordinary
// native sync worker. It is an internal loopback LAB executor, not permission
// to activate a paired public Node or to bypass an existing usage epoch.
type TransactionRuntime struct {
	metered       *MeteredTransactionOptions
	protocolProbe sync.Mutex
	mu            sync.Mutex
	runtime       *Runtime
	options       Options
	journal       *transactionDisk
	state         transactionState
	instance      string
	closed        bool
	afterIntent   func() // Test-only crash checkpoint.
	afterChange   func() // Test-only crash after process change, before receipt.
}

type TransactionStatus struct {
	Usage                 *MeteredCondition `json:"usage,omitempty"`
	CurrentStageID        string            `json:"current_stage_id,omitempty"`
	PreviousStageID       string            `json:"previous_stage_id,omitempty"`
	Instance              string            `json:"instance"`
	Serial                uint64            `json:"serial"`
	StagedID              string            `json:"staged_id,omitempty"`
	InterruptedCommandSHA string            `json:"interrupted_command_sha,omitempty"`
	Observed              Snapshot          `json:"observed"`
}

func OpenTransactions(o Options) (*TransactionRuntime, error) {
	if o.ExperimentalFinalTCP {
		return nil, ErrTransaction
	}
	return openTransactions(o, 1)
}

func openTransactions(o Options, version int) (*TransactionRuntime, error) {
	// Public/metered rollout is a separate gate. This executor cannot silently
	// take over the existing native runtime directory or a system service.
	ip := net.ParseIP(o.ListenIP)
	if ip == nil || !ip.IsLoopback() || !transactionNode.MatchString(o.NodeID) || !transactionHash.MatchString(o.BinarySHA) {
		return nil, ErrTransaction
	}
	d, err := openTransactionDisk(o)
	if err != nil {
		return nil, err
	}
	d.version = version
	if version == 2 {
		d.aad = append(d.aad, []byte("\x00METERED-FINAL-TCP-v2")...)
	}
	inner := o
	inner.Directory = filepath.Join(o.Directory, "owned-runtime")
	r, err := Open(inner)
	if err != nil {
		return nil, err
	}
	s, err := d.load(o.LeaseKey)
	if err != nil {
		r.Close()
		return nil, err
	}
	instance := make([]byte, 16)
	if _, err = rand.Read(instance); err != nil {
		r.Close()
		return nil, err
	}
	o.StateKey = append([]byte(nil), o.StateKey...)
	o.LeaseKey = append([]byte(nil), o.LeaseKey...)
	return &TransactionRuntime{runtime: r, options: o, journal: d, state: s, instance: hex.EncodeToString(instance)}, nil
}

func (t *TransactionRuntime) Status() TransactionStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := TransactionStatus{Instance: t.instance, Serial: t.state.Serial, Observed: t.runtime.Snapshot()}
	if t.metered != nil {
		out.Observed, out.Usage = t.meteredSnapshot()
	}
	if t.state.Current != nil {
		out.CurrentStageID = t.state.Current.ID
	}
	if t.state.Previous != nil {
		out.PreviousStageID = t.state.Previous.ID
	}
	if t.state.Staged != nil {
		out.StagedID = t.state.Staged.ID
	}
	if t.state.Pending != nil {
		out.InterruptedCommandSHA = hashJSON(*t.state.Pending)
	}
	return out
}

func (t *TransactionRuntime) save(s transactionState) error {
	if err := t.journal.save(s); err != nil {
		// A successful process change without a durable receipt is ambiguous.
		// Close only this executor's owned child and require explicit recovery.
		t.runtime.Close()
		t.closed = true
		return err
	}
	t.state = s
	return nil
}

func (t *TransactionRuntime) receipt(c TransactionCommand, outcome string, s Snapshot) TransactionReceipt {
	return signTransactionReceipt(t.options.LeaseKey, TransactionReceipt{
		Version: 1, CommandID: c.ID, CommandSHA: hashJSON(c), Serial: c.Serial,
		Action: c.Action, StageID: c.StageID, NodeID: c.NodeID, InstallationID: c.InstallationID,
		RuntimeInstance: t.instance, Outcome: outcome, ObservedAt: time.Now().UnixNano(), Observed: s,
	})
}

// Execute consumes one authenticated, monotonic transaction. Stage performs
// offline validation and fsync, never starts a listener. Apply and Rollback
// act on actual assigned ports through the existing fail-closed supervisor.
// Receipt replay is historical and never renews a lease or restarts a child.
func (t *TransactionRuntime) Execute(ctx context.Context, c TransactionCommand, candidate xrayconfig.Candidate) (TransactionReceipt, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.metered != nil || c.Version != 1 {
		return TransactionReceipt{}, ErrTransaction
	}
	if t.closed {
		return TransactionReceipt{}, ErrClosed
	}
	if ctx == nil || ctx.Err() != nil {
		return TransactionReceipt{}, ErrTransaction
	}
	if !validTransactionSignature(t.options.LeaseKey, c) || c.NodeID != t.options.NodeID ||
		c.InstallationID != t.options.InstallationID || c.BinarySHA != t.options.BinarySHA {
		return TransactionReceipt{}, ErrTransaction
	}
	// Authenticate before replay, but allow retrieving an expired historical
	// receipt after a lost response. No process method runs on this path.
	if t.state.Pending == nil && t.state.LastCommand != nil && hashJSON(*t.state.LastCommand) == hashJSON(c) {
		if c.Action == "stage" && TransactionPayloadSHA(candidate) != c.PayloadSHA {
			return TransactionReceipt{}, ErrTransaction
		}
		if t.state.LastReceipt.Outcome == "failed_stopped" || t.state.LastReceipt.Outcome == "failed_previous_retained" {
			return *t.state.LastReceipt, ErrRuntime
		}
		return *t.state.LastReceipt, nil
	}
	if c.RuntimeInstance != "" && c.RuntimeInstance != t.instance {
		return TransactionReceipt{}, ErrTransaction
	}
	now := time.Now().UnixNano()
	if c.Serial <= t.state.Serial || c.Issued > now+int64(time.Second) || c.Expires <= now {
		return TransactionReceipt{}, ErrTransaction
	}
	if t.state.Pending != nil {
		if c.Action != "stage" || c.InterruptedCommandSHA != hashJSON(*t.state.Pending) {
			return TransactionReceipt{}, ErrTransactionInterrupted
		}
	} else if c.InterruptedCommandSHA != "" {
		return TransactionReceipt{}, ErrTransaction
	}
	before := t.runtime.Snapshot()
	if before.Usage != nil || Condition(before) != c.Expected {
		return TransactionReceipt{}, ErrTransaction
	}
	if c.Action == "stage" {
		return t.stage(ctx, c, candidate)
	}
	if len(candidate.Config) != 0 || candidate.SHA256 != "" || len(candidate.Listeners) != 0 {
		return TransactionReceipt{}, ErrTransaction
	}

	var target *transactionStage
	switch c.Action {
	case "apply":
		target = t.state.Staged
		if target == nil || target.Expires <= now || target.Base != c.Expected || target.Revision.Sequence != c.RuntimeSequence {
			return TransactionReceipt{}, ErrTransaction
		}
	case "rollback":
		target = t.state.Previous
		// After a restrictive failed Apply or lease expiry, restore the last
		// known good ONLY with a fresh explicit rollback command and lease.
		if !before.ProcessReady && t.state.Current != nil && c.StageID == t.state.Current.ID && before.SavedSHA == t.state.Current.Revision.SHA {
			target = t.state.Current
		}
	case "renew":
		target = t.state.Current
	}
	if target == nil || target.ID != c.StageID || target.PayloadSHA != c.PayloadSHA ||
		target.Revision.SHA != c.Lease.ConfigSHA || target.Revision.Manifest != c.Lease.ManifestSHA {
		return TransactionReceipt{}, ErrTransaction
	}
	if c.Action != "renew" && c.RuntimeSequence <= before.Highwater {
		return TransactionReceipt{}, ErrTransaction
	}
	if c.Action == "renew" && (before.ActiveSHA != target.Revision.SHA || before.ActiveSequence != c.RuntimeSequence || !before.ProcessReady) {
		return TransactionReceipt{}, ErrTransaction
	}
	// Check signature/expiry before recording an intent or disturbing a child.
	v := *target.Revision
	v.Sequence = c.RuntimeSequence
	if !t.runtime.validLease(c.Lease, &v) {
		return TransactionReceipt{}, ErrLease
	}
	next := t.state
	next.Serial, next.Pending = c.Serial, &c
	if err := t.save(next); err != nil {
		return TransactionReceipt{}, err
	}
	if t.afterIntent != nil {
		t.afterIntent()
	}
	var observed Snapshot
	var operationErr error
	deadline := c.Expires
	if c.Action == "apply" && target.Expires < deadline {
		deadline = target.Expires
	}
	opCtx, cancel := context.WithDeadline(ctx, time.Unix(0, deadline))
	defer cancel()
	if c.Action == "renew" {
		t.runtime.mu.Lock()
		if Condition(t.runtime.snapshot()) != c.Expected {
			observed, operationErr = t.runtime.snapshot(), ErrStale
		} else {
			observed, operationErr = t.runtime.refreshLocked(c.Lease)
		}
		t.runtime.mu.Unlock()
	} else {
		observed, operationErr = t.runtime.applyConditional(opCtx, Revision{c.RuntimeSequence, v.candidate()}, c.Lease, c.Expected)
	}
	if t.afterChange != nil {
		t.afterChange()
	}
	outcome := map[string]string{"apply": "applied", "rollback": "rolled_back", "renew": "renewed"}[c.Action]
	next = t.state
	if operationErr != nil {
		outcome = "failed_stopped"
		if observed.ProcessReady {
			outcome = "failed_previous_retained"
		}
	} else if c.Action != "renew" {
		current := *target
		current.Revision = &v
		if target != next.Current {
			next.Previous = next.Current
		}
		next.Current, next.Staged = &current, nil
	}
	ack := t.receipt(c, outcome, observed)
	next.Pending, next.LastCommand, next.LastReceipt = nil, &c, &ack
	if err := t.save(next); err != nil {
		return TransactionReceipt{}, err
	}
	return ack, operationErr
}

func (t *TransactionRuntime) stage(ctx context.Context, c TransactionCommand, candidate xrayconfig.Candidate) (TransactionReceipt, error) {
	// Take ownership of bytes/sidecars before persisting or returning to caller.
	v := freeze(Revision{c.RuntimeSequence, candidate})
	frozen := v.candidate()
	if candidate.CanActivate || !candidate.RequiresXrayTest || !candidate.RequiresDataPlaneProof || !candidate.RequiresSubscriptionCommit ||
		TransactionPayloadSHA(candidate) != c.PayloadSHA || TransactionPayloadSHA(frozen) != c.PayloadSHA ||
		c.RuntimeSequence <= c.Expected.Highwater || xrayconfig.RequiresUsageAdmission(frozen) {
		return TransactionReceipt{}, ErrTransaction
	}
	if err := validateLayout(frozen, t.options.ListenIP, t.runtime.reserved); err != nil {
		return TransactionReceipt{}, err
	}
	if _, err := xrayconfig.ValidateOffline(ctx, frozen, t.options.Binary, t.options.BinarySHA); err != nil {
		return TransactionReceipt{}, err
	}
	observed := t.runtime.Snapshot()
	if ctx.Err() != nil || time.Now().UnixNano() >= c.Expires || Condition(observed) != c.Expected {
		return TransactionReceipt{}, ErrTransaction
	}
	next := t.state
	next.Serial, next.Pending = c.Serial, nil
	// The supervisor may have automatically returned to its previous good
	// revision after a child exit. Preserve that actual good as rollback base.
	if next.Current != nil && observed.SavedSHA != next.Current.Revision.SHA {
		if next.Previous != nil && observed.SavedSHA == next.Previous.Revision.SHA {
			next.Current, next.Previous = next.Previous, nil
		} else if t.state.Pending == nil {
			return TransactionReceipt{}, ErrTransaction
		}
	}
	next.Staged = &transactionStage{ID: c.ID, PayloadSHA: c.PayloadSHA, Expires: c.Expires, Base: c.Expected, Revision: v}
	ack := t.receipt(c, "staged", observed)
	next.LastCommand, next.LastReceipt = &c, &ack
	if err := t.save(next); err != nil {
		return TransactionReceipt{}, err
	}
	return ack, nil
}

func (t *TransactionRuntime) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.closed {
		t.closed = true
		t.runtime.Close()
	}
}
