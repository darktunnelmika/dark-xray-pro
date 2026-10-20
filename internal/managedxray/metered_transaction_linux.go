//go:build linux

package managedxray

import (
	"context"
	"crypto/hmac"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

// MeteredCondition binds an owner-reviewed command to one exact durable epoch
// and admission intent. Samples do not change it; draining, final proof and
// settlement do. EpochSHA includes the complete trusted billing bindings.
type MeteredCondition struct {
	EpochID      string     `json:"epoch_id,omitempty"`
	EpochSHA     string     `json:"epoch_sha,omitempty"`
	AdmissionSHA string     `json:"admission_sha,omitempty"`
	Settlement   uc.Receipt `json:"settlement"`
}

type MeteredEvidence struct {
	After           MeteredCondition `json:"after"`
	PriorSettlement uc.Receipt       `json:"prior_settlement"`
}

// MeteredTransactionAuthority is an INTERNAL trusted HUB adapter, not browser
// input. Check must revalidate the immutable candidate against current source,
// topology, bindings and eligibility. Register must atomically recheck these
// and register the actual birth with the HUB's authoritative billing mapping.
// Neither callback may call back into this executor. No shipping HTTP/Agent
// adapter implements this interface yet; the empty-route channel stays v1.
type MeteredTransactionAuthority interface {
	Check(context.Context, TransactionCommand, xrayconfig.Candidate) error
	Register(context.Context, TransactionCommand, xrayconfig.Candidate, uc.ProcessToken) (uc.Epoch, error)
}

type MeteredTransactionOptions struct {
	Authority     MeteredTransactionAuthority
	Sender        *uc.Sender
	Interval      time.Duration
	MaxSampleGap  time.Duration
	MaxPendingAge time.Duration
}

// OpenMeteredTransactions requires an independent private directory, loopback
// listeners and the explicit custom-core gate. v2 journal authentication cannot
// be downgraded to the unmetered executor. It never adopts an ordinary runtime.
func OpenMeteredTransactions(o Options, m MeteredTransactionOptions) (*TransactionRuntime, error) {
	if !o.ExperimentalFinalTCP || m.Authority == nil || m.Sender == nil {
		return nil, ErrTransaction
	}
	t, err := openTransactions(o, 2)
	if err != nil {
		return nil, err
	}
	t.metered = &m
	return t, nil
}

func validMeteredCondition(u MeteredCondition) bool {
	if u.AdmissionSHA != "" && !transactionHash.MatchString(u.AdmissionSHA) {
		return false
	}
	if u.EpochID == "" {
		return u.EpochSHA == "" && u.Settlement == (uc.Receipt{})
	}
	return transactionID.MatchString(u.EpochID) && transactionHash.MatchString(u.EpochSHA) && u.AdmissionSHA != "" &&
		(u.Settlement == (uc.Receipt{}) || (uc.ValidReceipt(u.Settlement) && u.Settlement.EpochID == u.EpochID))
}

// Caller owns t.mu; take the runtime snapshot and epoch under one lock.
func (t *TransactionRuntime) meteredSnapshot() (Snapshot, *MeteredCondition) {
	r := t.runtime
	r.mu.Lock()
	defer r.mu.Unlock()
	u := &MeteredCondition{}
	if r.state.Admission != nil {
		u.AdmissionSHA = hashJSON(r.state.Admission)
		if r.state.Admission.Settlement != nil {
			u.Settlement = *r.state.Admission.Settlement
		}
	}
	if r.state.Usage != nil {
		u.EpochID, u.EpochSHA = r.state.Usage.Epoch.ID, hashJSON(r.state.Usage.Epoch)
	}
	return r.snapshot(), u
}

func (t *TransactionRuntime) meteredReceipt(c TransactionCommand, outcome string, prior uc.Receipt) TransactionReceipt {
	s, u := t.meteredSnapshot()
	r := t.receipt(c, outcome, s)
	r.Version = 2
	r.Metered = &MeteredEvidence{After: *u, PriorSettlement: prior}
	return signTransactionReceipt(t.options.LeaseKey, r)
}

// VerifyMeteredTransactionReceipt verifies historical execution, including the
// exact prior seal on replacement. It does not claim current liveness or a
// subscription commit. v1 verifiers intentionally refuse this separate proof.
func VerifyMeteredTransactionReceipt(key []byte, c TransactionCommand, r TransactionReceipt) bool {
	if c.Version != 2 || !validTransactionSignature(key, c) || r.Version != 2 || r.Metered == nil ||
		r.CommandID != c.ID || r.CommandSHA != hashJSON(c) || r.Serial != c.Serial || r.Action != c.Action || r.StageID != c.StageID ||
		r.NodeID != c.NodeID || r.InstallationID != c.InstallationID || r.RuntimeInstance != c.RuntimeInstance || r.ObservedAt <= 0 ||
		r.Observed.DataPlaneVerified || r.Observed.SubscriptionPublished || !validMeteredCondition(r.Metered.After) || !transactionHash.MatchString(r.Signature) {
		return false
	}
	e := r.Metered
	if e.PriorSettlement != (uc.Receipt{}) && (!uc.ValidReceipt(e.PriorSettlement) || e.PriorSettlement.EpochID != c.Usage.EpochID) {
		return false
	}
	switch r.Outcome {
	case "staged":
		if c.Action != "stage" || Condition(r.Observed) != c.Expected || e.After != *c.Usage || e.PriorSettlement != (uc.Receipt{}) {
			return false
		}
	case "applied", "rolled_back", "renewed":
		want := map[string]string{"applied": "apply", "rolled_back": "rollback", "renewed": "renew"}[r.Outcome]
		u := r.Observed.Usage
		if c.Action != want || !r.Observed.ProcessReady || r.Observed.NoListeners || r.Observed.ActiveSequence != c.RuntimeSequence ||
			r.Observed.Highwater != c.RuntimeSequence || r.Observed.SavedSequence != c.RuntimeSequence || r.Observed.SavedSHA != c.Lease.ConfigSHA ||
			r.Observed.ActiveSHA != c.Lease.ConfigSHA || r.Observed.LeaseExpires != c.Lease.Expires ||
			u == nil || !u.Required || !u.Running || u.Failure != "" || u.Settled || !u.SettlementRequired || u.Receipts == 0 ||
			e.After.EpochID == "" || e.After.Settlement != (uc.Receipt{}) {
			return false
		}
		if c.Action == "renew" {
			if e.After != *c.Usage || e.PriorSettlement != (uc.Receipt{}) {
				return false
			}
		} else if e.After.EpochID == c.Usage.EpochID || (c.Usage.EpochID != "" && e.PriorSettlement.EpochID != c.Usage.EpochID) ||
			(c.Usage.Settlement != (uc.Receipt{}) && e.PriorSettlement != c.Usage.Settlement) {
			return false
		}
	case "failed_stopped":
		if c.Action == "stage" || r.Observed.ProcessReady {
			return false
		}
	default:
		return false
	}
	want := signTransactionReceipt(key, r)
	return hmac.Equal([]byte(want.Signature), []byte(r.Signature))
}

// ReplayMeteredUsage only reconciles durable usage evidence. It never resumes
// an interrupted transaction, opens a listener, renews a lease or invents zero.
func (t *TransactionRuntime) ReplayMeteredUsage(ctx context.Context) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed || t.metered == nil {
		return 0, ErrClosed
	}
	return t.runtime.ReplayObservedUsage(ctx, t.metered.Sender)
}

// ExecuteMetered is a distinct v2 interpreter. Stage remains offline. Apply
// and Rollback durably record intent, seal the previous epoch, recheck current
// authority, prepare a new API-only birth and await its durable zero receipt.
// A failed transition never automatically restores an earlier user's access.
func (t *TransactionRuntime) ExecuteMetered(ctx context.Context, c TransactionCommand, candidate xrayconfig.Candidate) (TransactionReceipt, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return TransactionReceipt{}, ErrClosed
	}
	if t.metered == nil || c.Version != 2 || ctx == nil || ctx.Err() != nil || !validTransactionSignature(t.options.LeaseKey, c) ||
		c.NodeID != t.options.NodeID || c.InstallationID != t.options.InstallationID || c.BinarySHA != t.options.BinarySHA {
		return TransactionReceipt{}, ErrTransaction
	}
	// Freeze pointer fields too: callers cannot change a signed command during
	// a network callback. Candidate bytes are frozen before any such callback.
	u := *c.Usage
	c.Usage = &u
	if t.state.Pending == nil && t.state.LastCommand != nil && hashJSON(*t.state.LastCommand) == hashJSON(c) {
		if c.Action == "stage" && TransactionPayloadSHA(candidate) != c.PayloadSHA {
			return TransactionReceipt{}, ErrTransaction
		}
		ack := cloneMeteredReceipt(*t.state.LastReceipt)
		if ack.Outcome == "failed_stopped" {
			return ack, ErrRuntime
		}
		return ack, nil
	}
	now := time.Now().UnixNano()
	if c.RuntimeInstance != t.instance || c.Serial <= t.state.Serial || c.Issued > now+int64(time.Second) || c.Expires <= now {
		return TransactionReceipt{}, ErrTransaction
	}
	if t.state.Pending != nil {
		if c.Action != "stage" || c.InterruptedCommandSHA != hashJSON(*t.state.Pending) {
			return TransactionReceipt{}, ErrTransactionInterrupted
		}
	} else if c.InterruptedCommandSHA != "" {
		return TransactionReceipt{}, ErrTransaction
	}
	before, usage := t.meteredSnapshot()
	if Condition(before) != c.Expected || *usage != *c.Usage {
		return TransactionReceipt{}, ErrTransaction
	}
	opCtx, cancel := context.WithDeadline(ctx, time.Unix(0, c.Expires))
	defer cancel()
	if c.Action == "stage" {
		return t.stageMetered(opCtx, c, candidate)
	}
	if TransactionPayloadSHA(candidate) != TransactionPayloadSHA(xrayconfig.Candidate{}) {
		return TransactionReceipt{}, ErrTransaction
	}
	var target *transactionStage
	switch c.Action {
	case "apply":
		target = t.state.Staged
		if target == nil || target.Expires <= now || target.Base != c.Expected || target.UsageBase == nil || *target.UsageBase != *c.Usage || target.Revision.Sequence != c.RuntimeSequence {
			return TransactionReceipt{}, ErrTransaction
		}
		var stop context.CancelFunc
		opCtx, stop = context.WithDeadline(opCtx, time.Unix(0, target.Expires))
		defer stop()
	case "rollback":
		target = t.state.Previous
		if !before.ProcessReady && t.state.Current != nil && c.StageID == t.state.Current.ID {
			target = t.state.Current
		}
	case "renew":
		target = t.state.Current
	}
	if target == nil || target.ID != c.StageID || target.PayloadSHA != c.PayloadSHA || target.Revision.SHA != c.Lease.ConfigSHA || target.Revision.Manifest != c.Lease.ManifestSHA ||
		(c.Action != "renew" && c.RuntimeSequence <= before.Highwater) ||
		(c.Action == "renew" && (!before.ProcessReady || before.ActiveSHA != target.Revision.SHA || before.ActiveSequence != c.RuntimeSequence)) {
		return TransactionReceipt{}, ErrTransaction
	}
	v := *target.Revision
	v.Sequence = c.RuntimeSequence
	if !t.validMeteredLease(c.Lease, &v) {
		return TransactionReceipt{}, ErrLease
	}
	if err := t.checkMeteredAuthority(opCtx, c, v); err != nil {
		return TransactionReceipt{}, err
	}
	checked, checkedUsage := t.meteredSnapshot()
	if opCtx.Err() != nil || Condition(checked) != c.Expected || *checkedUsage != *c.Usage || !t.validMeteredLease(c.Lease, &v) {
		return TransactionReceipt{}, ErrStale
	}
	next := t.state
	next.Serial, next.Pending = c.Serial, &c
	if err := t.save(next); err != nil {
		return TransactionReceipt{}, err
	}
	if t.afterIntent != nil {
		t.afterIntent()
	}
	var err error
	var prior uc.Receipt
	if c.Action == "renew" {
		_, err = t.runtime.Refresh(c.Lease)
	} else {
		prior, err = t.replaceMetered(opCtx, c, v)
	}
	if err != nil {
		// Even malformed registration output must close the API-only child.
		// The durable admission/usage intent remains a sticky recovery fence.
		t.runtime.mu.Lock()
		t.runtime.failAdmissionLocked("metered_transaction_failed_stopped")
		t.runtime.mu.Unlock()
	}
	if t.afterChange != nil {
		t.afterChange()
	}
	outcome := map[string]string{"apply": "applied", "rollback": "rolled_back", "renew": "renewed"}[c.Action]
	if err != nil {
		outcome = "failed_stopped"
	}
	ack := t.meteredReceipt(c, outcome, prior)
	if !VerifyMeteredTransactionReceipt(t.options.LeaseKey, c, ack) {
		t.runtime.Close()
		t.closed = true
		return TransactionReceipt{}, ErrTransactionInterrupted
	}
	next = t.state
	if err == nil && c.Action != "renew" {
		current := *target
		current.Revision = &v
		if target != next.Current {
			next.Previous = next.Current
		}
		next.Current, next.Staged = &current, nil
	}
	next.Pending, next.LastCommand, next.LastReceipt = nil, &c, &ack
	if e := t.save(next); e != nil {
		return TransactionReceipt{}, e
	}
	return cloneMeteredReceipt(ack), err
}

// Do not let a caller mutate the retained receipt through nested pointers.
func cloneMeteredReceipt(r TransactionReceipt) TransactionReceipt {
	if r.Metered != nil {
		m := *r.Metered
		r.Metered = &m
	}
	if r.Observed.Usage != nil {
		u := *r.Observed.Usage
		r.Observed.Usage = &u
	}
	return r
}

func (t *TransactionRuntime) validMeteredLease(l Lease, v *savedRevision) bool {
	r := t.runtime
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.closed && !r.fenced && r.validLease(l, v) && l.Issued > r.state.LastLeaseIssued
}

func (t *TransactionRuntime) checkMeteredAuthority(ctx context.Context, c TransactionCommand, v savedRevision) error {
	// Pass a private copy; an adapter must not alter the journal's candidate.
	frozen := freeze(Revision{Sequence: v.Sequence, Candidate: v.candidate()})
	u := *c.Usage
	c.Usage = &u
	return t.metered.Authority.Check(ctx, c, frozen.candidate())
}

func (t *TransactionRuntime) stageMetered(ctx context.Context, c TransactionCommand, candidate xrayconfig.Candidate) (TransactionReceipt, error) {
	v := freeze(Revision{Sequence: c.RuntimeSequence, Candidate: candidate})
	frozen := v.candidate()
	if candidate.CanActivate || !candidate.RequiresXrayTest || !candidate.RequiresDataPlaneProof || !candidate.RequiresSubscriptionCommit ||
		TransactionPayloadSHA(candidate) != c.PayloadSHA || TransactionPayloadSHA(frozen) != c.PayloadSHA || c.RuntimeSequence <= c.Expected.Highwater || !finalTCPScope(frozen.Config) {
		return TransactionReceipt{}, ErrTransaction
	}
	if _, _, _, err := xrayconfig.UsageBootstrap(frozen); err != nil {
		return TransactionReceipt{}, err
	}
	if err := validateLayout(frozen, t.options.ListenIP, t.runtime.reserved); err != nil {
		return TransactionReceipt{}, err
	}
	if _, err := xrayconfig.ValidateOffline(ctx, frozen, t.options.Binary, t.options.BinarySHA); err != nil {
		return TransactionReceipt{}, err
	}
	if err := t.checkMeteredAuthority(ctx, c, *v); err != nil {
		return TransactionReceipt{}, err
	}
	s, u := t.meteredSnapshot()
	if ctx.Err() != nil || time.Now().UnixNano() >= c.Expires || Condition(s) != c.Expected || *u != *c.Usage {
		return TransactionReceipt{}, ErrStale
	}
	next := t.state
	next.Serial, next.Pending = c.Serial, nil
	next.Staged = &transactionStage{ID: c.ID, PayloadSHA: c.PayloadSHA, Expires: c.Expires, Base: c.Expected, UsageBase: u, Revision: v}
	ack := t.receipt(c, "staged", s)
	ack.Version = 2
	ack.Metered = &MeteredEvidence{After: *u}
	ack = signTransactionReceipt(t.options.LeaseKey, ack)
	next.LastCommand, next.LastReceipt = &c, &ack
	if err := t.save(next); err != nil {
		return TransactionReceipt{}, err
	}
	return cloneMeteredReceipt(ack), nil
}

func (t *TransactionRuntime) replaceMetered(ctx context.Context, c TransactionCommand, v savedRevision) (uc.Receipt, error) {
	var prior uc.Receipt
	r := t.runtime
	if c.Usage.EpochID != "" {
		r.mu.Lock()
		unopened := r.provenUnopenedLocked()
		r.mu.Unlock()
		var err error
		if unopened {
			_, err = r.SettleUnopenedUsage(ctx, t.metered.Sender)
		} else {
			_, err = r.SettleActiveTCP(ctx, t.metered.Sender)
		}
		if err != nil {
			return prior, err
		}
		_, u := t.meteredSnapshot()
		prior = u.Settlement
		if !uc.ValidReceipt(prior) || prior.EpochID != c.Usage.EpochID {
			return uc.Receipt{}, ErrUsageSettlement
		}
	}
	// Accounting may have exhausted quota while draining. Recheck AFTER the
	// exact final seal and BEFORE preparing a new birth, including Rollback.
	if err := t.checkMeteredAuthority(ctx, c, v); err != nil {
		return prior, err
	}
	birth, err := r.PrepareUsageAdmission(ctx, Revision{Sequence: v.Sequence, Candidate: v.candidate()}, c.Lease)
	if err != nil {
		return prior, err
	}
	frozen := freeze(Revision{Sequence: v.Sequence, Candidate: v.candidate()})
	u := *c.Usage
	registration := c
	registration.Usage = &u
	epoch, err := t.metered.Authority.Register(ctx, registration, frozen.candidate(), birth)
	if err != nil {
		return prior, err
	}
	_, _, endpoint, err := xrayconfig.UsageBootstrap(v.candidate())
	if err != nil {
		return prior, err
	}
	_, err = r.ActivateUsageAdmission(ctx, UsageObservation{Epoch: epoch, StatsEndpoint: endpoint, Sender: t.metered.Sender,
		Interval: t.metered.Interval, MaxSampleGap: t.metered.MaxSampleGap, MaxPendingAge: t.metered.MaxPendingAge})
	return prior, err
}
