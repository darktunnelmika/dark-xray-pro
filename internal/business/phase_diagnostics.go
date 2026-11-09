package business

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"modernc.org/sqlite"
)

// Diagnostics are deliberately separate from the accounting journal. Only the
// fixed vocabulary below and numeric metadata may cross the reporting boundary.
type DiagnosticOperation uint8

const (
	DiagnosticOperationSessionVerify DiagnosticOperation = iota + 1
	DiagnosticOperationSessionLogin
	DiagnosticOperationMetadata
	DiagnosticOperationRuntime
	DiagnosticOperationUsage
	DiagnosticOperationObserve
	diagnosticOperationCount
)

type DiagnosticSource uint8

const (
	DiagnosticSourceAPI DiagnosticSource = iota + 1
	DiagnosticSourceStore
	DiagnosticSourceHubLocal
	DiagnosticSourceInternal
	diagnosticSourceCount
)

type DiagnosticPhase uint8

const (
	DiagnosticPhaseEntry DiagnosticPhase = iota + 1
	DiagnosticPhaseAuth
	DiagnosticPhaseDecode
	DiagnosticPhaseRegistryAuth
	DiagnosticPhaseEpochProvenance
	DiagnosticPhaseCredentialRead
	DiagnosticPhasePasswordCheck
	DiagnosticPhaseSessionRead
	DiagnosticPhasePermissionsRead
	DiagnosticPhaseBeginTx
	DiagnosticPhaseSchema
	DiagnosticPhaseWork
	DiagnosticPhaseCommit
	DiagnosticPhaseRollback
	DiagnosticPhaseClientScope
	DiagnosticPhaseProjection
	DiagnosticPhaseResponse
	diagnosticPhaseCount
)

var diagnosticOperationNames = [...]string{"", "session_verify", "session_login", "metadata", "runtime", "usage", "observe"}
var diagnosticSourceNames = [...]string{"", "api", "store", "hub_local", "internal"}
var diagnosticPhaseNames = [...]string{"", "entry", "auth", "decode", "registry_auth", "epoch_provenance", "credential_read", "password_check", "session_read", "permissions_read", "begin_tx", "schema", "work", "commit", "rollback", "client_scope", "projection", "response"}

type diagnosticKind uint8

const (
	diagnosticPhaseBegin diagnosticKind = iota
	diagnosticPhaseEnd
	diagnosticCurrentPhase
	diagnosticScopeExit
)

var diagnosticKindNames = [...]string{"phase_begin", "phase_end", "current", "scope_exit"}

type diagnosticResult uint8

const (
	diagnosticNotReported diagnosticResult = iota
	diagnosticNoErrorObserved
	diagnosticSQLite
	diagnosticContextDeadline
	diagnosticContextCanceled
	diagnosticNoRows
	diagnosticTxDone
	diagnosticBadConn
	diagnosticOtherError
)

var diagnosticResultNames = [...]string{"not_reported", "no_error_observed", "sqlite", "context_deadline", "context_canceled", "no_rows", "tx_done", "driver_bad_conn", "other"}

type diagnosticContext uint8

const (
	diagnosticContextUnknown diagnosticContext = iota
	diagnosticContextNone
	diagnosticContextIsCanceled
	diagnosticContextIsDeadline
)

var diagnosticContextNames = [...]string{"unknown", "none", "canceled", "deadline"}

const (
	diagnosticQueueCapacity   = 256
	diagnosticSlowCapacity = 64
	diagnosticSlowThreshold = time.Second
	diagnosticActiveCapacity  = 64
	diagnosticPacketLimit     = 512
	diagnosticBatchLimit      = 16
	diagnosticTick            = 250 * time.Millisecond
	diagnosticSummaryInterval = time.Second
	diagnosticStopLimit       = 250 * time.Millisecond
)

// Every queued record is a value. In particular, it retains no error, request,
// context, SQL, address, credential, application identifier or arbitrary string.
type diagnosticRecord struct {
	kind                          diagnosticKind
	span, parent                  uint64
	operation                     DiagnosticOperation
	source                        DiagnosticSource
	phase                         DiagnosticPhase
	at, duration                  int64
	result                        diagnosticResult
	context                       diagnosticContext
	sqlitePresent                 bool
	sqlitePrimary, sqliteExtended int32
}

// Published slot values are immutable. Queue loss cannot manufacture a current
// holder: phase completion and scope exit update the slot independently of I/O.
type diagnosticCurrent struct {
	span, parent uint64
	operation    DiagnosticOperation
	source       DiagnosticSource
	phase        DiagnosticPhase
	entered      time.Time
}

type diagnosticCounters struct {
	attempted        atomic.Uint64
	enqueued         atomic.Uint64
	sent             atomic.Uint64
	queueFull        atomic.Uint64
	transportDropped atomic.Uint64
	closedDropped    atomic.Uint64
	formatDropped    atomic.Uint64
	shutdownDropped  atomic.Uint64
	untrackedSpans   atomic.Uint64
	invalid          atomic.Uint64
	auxSendFailed    atomic.Uint64
	stopTimeout      atomic.Uint64
	producers        atomic.Int64
}

type phaseDiagnostics struct {
	db       *sql.DB
	queue    chan diagnosticRecord
	slowQueue chan diagnosticRecord
	active   [diagnosticActiveCapacity]atomic.Pointer[diagnosticCurrent]
	sequence atomic.Uint64
	closed   atomic.Bool
	started  atomic.Bool
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
	counters diagnosticCounters
}

// EnablePhaseDiagnostics is startup-only. It does not open a connection, start
// a goroutine, read configuration, change SQL, or alter database settings.
func (s *Store) EnablePhaseDiagnostics() error {
	if s == nil || s.db == nil {
		return ErrUnavailable
	}
	if s.phaseDiagnostics == nil {
		s.phaseDiagnostics = newPhaseDiagnostics(s.db)
	}
	return nil
}

func newPhaseDiagnostics(db *sql.DB) *phaseDiagnostics {
	return &phaseDiagnostics{db: db, queue: make(chan diagnosticRecord, diagnosticQueueCapacity), slowQueue: make(chan diagnosticRecord, diagnosticSlowCapacity), stop: make(chan struct{}), done: make(chan struct{})}
}

// StartPhaseDiagnostics starts at most one exporter. The returned stop function
// is idempotent, waits only for cancellation (never output delivery), and is
// bounded by both the supplied context and a small diagnostic-only stop limit.
func (s *Store) StartPhaseDiagnostics() func(context.Context) {
	if s == nil || s.phaseDiagnostics == nil {
		return func(context.Context) {}
	}
	d := s.phaseDiagnostics
	d.startExporter(nil)
	return d.stopExporter
}

// A supplied concrete journal transfers ownership only on the first start.
// Production supplies nil, so the goroutine opens only the fixed system sink.
func (d *phaseDiagnostics) startExporter(journal *phaseDiagnosticJournal) bool {
	if !d.started.CompareAndSwap(false, true) {
		return false
	}
	go func() {
		defer close(d.done)
		if journal == nil {
			journal = openPhaseDiagnosticJournal()
		}
		d.runExporter(journal)
	}()
	return true
}

func (d *phaseDiagnostics) stopExporter(ctx context.Context) {
	d.stopOnce.Do(func() {
		d.closed.Store(true)
		close(d.stop)
	})
	if ctx == nil {
		select {
		case <-d.done:
		default:
			d.counters.stopTimeout.Add(1)
		}
		return
	}
	timer := time.NewTimer(diagnosticStopLimit)
	defer timer.Stop()
	select {
	case <-d.done:
	case <-ctx.Done():
		d.counters.stopTimeout.Add(1)
	case <-timer.C:
		d.counters.stopTimeout.Add(1)
	}
}

type diagnosticContextKey struct{}

// DiagnosticSpan belongs to one synchronous call. It must not be concurrently
// mutated by separate goroutines. Nested calls receive independent spans; only
// their immutable numeric parent sequence crosses the context boundary.
type DiagnosticSpan struct {
	collector             *phaseDiagnostics
	ctx                   context.Context
	sequence, parent      uint64
	operation             DiagnosticOperation
	source                DiagnosticSource
	slot                  int
	current               *diagnosticCurrent
	phase                 DiagnosticPhase
	phaseStarted, started time.Time
	ended                 bool
}

func diagnosticSpanFromContext(ctx context.Context) *DiagnosticSpan {
	if ctx == nil {
		return nil
	}
	span, _ := ctx.Value(diagnosticContextKey{}).(*DiagnosticSpan)
	return span
}

// BeginDiagnostic preserves cancellation and deadlines. It never blocks for an
// active slot; capacity exhaustion produces an explicit coverage-loss counter.
func (s *Store) BeginDiagnostic(ctx context.Context, operation DiagnosticOperation, source DiagnosticSource) (context.Context, *DiagnosticSpan) {
	if s == nil || s.phaseDiagnostics == nil || ctx == nil {
		return ctx, nil
	}
	d := s.phaseDiagnostics
	if operation == 0 || operation >= diagnosticOperationCount || source == 0 || source >= diagnosticSourceCount {
		d.counters.invalid.Add(1)
		return context.WithValue(ctx, diagnosticContextKey{}, (*DiagnosticSpan)(nil)), nil
	}
	if d.closed.Load() {
		d.counters.untrackedSpans.Add(1)
		return context.WithValue(ctx, diagnosticContextKey{}, (*DiagnosticSpan)(nil)), nil
	}
	sequence := d.sequence.Add(1)
	if sequence == 0 {
		d.counters.untrackedSpans.Add(1)
		return context.WithValue(ctx, diagnosticContextKey{}, (*DiagnosticSpan)(nil)), nil
	}
	parent := uint64(0)
	if prior := diagnosticSpanFromContext(ctx); prior != nil && prior.collector == d {
		parent = prior.sequence
	}
	now := time.Now()
	current := &diagnosticCurrent{span: sequence, parent: parent, operation: operation, source: source, phase: DiagnosticPhaseEntry, entered: now}
	slot := -1
	for i := range d.active {
		if d.active[i].CompareAndSwap(nil, current) {
			slot = i
			break
		}
	}
	if slot < 0 {
		d.counters.untrackedSpans.Add(1)
		// Do not let a dropped child scope accidentally mutate its parent's
		// current phase through the transaction context helpers.
		return context.WithValue(ctx, diagnosticContextKey{}, (*DiagnosticSpan)(nil)), nil
	}
	if d.closed.Load() {
		d.active[slot].CompareAndSwap(current, nil)
		d.counters.untrackedSpans.Add(1)
		return context.WithValue(ctx, diagnosticContextKey{}, (*DiagnosticSpan)(nil)), nil
	}
	span := &DiagnosticSpan{collector: d, ctx: ctx, sequence: sequence, parent: parent, operation: operation, source: source, slot: slot, current: current, phase: DiagnosticPhaseEntry, phaseStarted: now, started: now}
	d.record(span.record(diagnosticPhaseBegin, now, 0, diagnosticNotReported, nil))
	return context.WithValue(ctx, diagnosticContextKey{}, span), span
}

func diagnosticEnter(ctx context.Context, phase DiagnosticPhase) {
	diagnosticSpanFromContext(ctx).Enter(phase)
}

func diagnosticFinishPhase(ctx context.Context, err error) {
	diagnosticSpanFromContext(ctx).finishPhase(err)
}

// Enter ends the prior phase without inventing its result. Concrete primitive
// wrappers call finishPhase with their actual error before any business mapping.
func (s *DiagnosticSpan) Enter(phase DiagnosticPhase) {
	if s == nil || s.ended {
		return
	}
	if phase == 0 || phase >= diagnosticPhaseCount {
		s.collector.counters.invalid.Add(1)
		return
	}
	now := time.Now()
	s.finishAt(now, diagnosticNotReported, nil)
	s.phase, s.phaseStarted = phase, now
	s.publishCurrent(now)
	s.collector.record(s.record(diagnosticPhaseBegin, now, 0, diagnosticNotReported, nil))
}

func (s *DiagnosticSpan) publishCurrent(now time.Time) {
	current := &diagnosticCurrent{span: s.sequence, parent: s.parent, operation: s.operation, source: s.source, phase: s.phase, entered: now}
	s.collector.active[s.slot].Store(current)
	s.current = current
}

func (s *DiagnosticSpan) finishPhase(err error) {
	if s == nil || s.ended {
		return
	}
	s.finishAt(time.Now(), classifyDiagnosticError(err), err)
}

func (s *DiagnosticSpan) finishAt(now time.Time, result diagnosticResult, err error) {
	if s.phase == 0 {
		return
	}
	record := s.record(diagnosticPhaseEnd, now, now.Sub(s.phaseStarted), result, err)
	s.phase = 0
	s.publishCurrent(now)
	s.collector.record(record)
}

// End records a scope exit, not an ACK or a successful response. In particular,
// nil means only no_error_observed; deferred exits do not prove normal return.
func (s *DiagnosticSpan) End(err error) {
	if s == nil || s.ended {
		return
	}
	now := time.Now()
	s.finishAt(now, classifyDiagnosticError(err), err)
	s.ended = true
	s.collector.active[s.slot].CompareAndSwap(s.current, nil)
	s.collector.record(s.record(diagnosticScopeExit, now, now.Sub(s.started), classifyDiagnosticError(err), err))
}

func (s *DiagnosticSpan) record(kind diagnosticKind, now time.Time, duration time.Duration, result diagnosticResult, err error) diagnosticRecord {
	r := diagnosticRecord{kind: kind, span: s.sequence, parent: s.parent, operation: s.operation, source: s.source, phase: s.phase, at: now.UnixNano(), duration: int64(duration), result: result, context: diagnosticContextState(s.ctx)}
	if kind == diagnosticScopeExit {
		r.phase = 0
	}
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) && sqliteErr != nil {
		code := sqliteErr.Code()
		if code >= 0 && uint64(code) <= uint64(^uint32(0)>>1) {
			r.sqlitePresent = true
			r.sqliteExtended = int32(code)
			r.sqlitePrimary = int32(code & 255)
		}
	}
	return r
}

func diagnosticContextState(ctx context.Context) diagnosticContext {
	if ctx == nil {
		return diagnosticContextUnknown
	}
	switch ctx.Err() {
	case nil:
		return diagnosticContextNone
	case context.Canceled:
		return diagnosticContextIsCanceled
	case context.DeadlineExceeded:
		return diagnosticContextIsDeadline
	default:
		return diagnosticContextUnknown
	}
}

func classifyDiagnosticError(err error) diagnosticResult {
	if err == nil {
		return diagnosticNoErrorObserved
	}
	var sqliteErr *sqlite.Error
	switch {
	case errors.As(err, &sqliteErr) && sqliteErr != nil:
		return diagnosticSQLite
	case errors.Is(err, context.DeadlineExceeded):
		return diagnosticContextDeadline
	case errors.Is(err, context.Canceled):
		return diagnosticContextCanceled
	case errors.Is(err, sql.ErrNoRows):
		return diagnosticNoRows
	case errors.Is(err, sql.ErrTxDone):
		return diagnosticTxDone
	case errors.Is(err, driver.ErrBadConn):
		return diagnosticBadConn
	default:
		return diagnosticOtherError
	}
}

func (d *phaseDiagnostics) record(r diagnosticRecord) {
	d.counters.producers.Add(1)
	defer d.counters.producers.Add(-1)
	d.counters.attempted.Add(1)
	if d.closed.Load() {
		d.counters.closedDropped.Add(1)
		return
	}
	// Reserve bounded admission for completed slow work. Ordinary phase chatter
	// cannot occupy this queue. No request waits for diagnostics or a sink.
	queue := d.queue
	if (r.kind == diagnosticPhaseEnd || r.kind == diagnosticScopeExit) && r.duration >= int64(diagnosticSlowThreshold) {
		queue = d.slowQueue
	}
	select {
	case queue <- r:
		d.counters.enqueued.Add(1)
	default:
		d.counters.queueFull.Add(1)
	}
}

func (s *Store) diagnosticBeginTx(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
	diagnosticEnter(ctx, DiagnosticPhaseBeginTx)
	var tx *sql.Tx
	var err error
	if opts != nil && opts.ReadOnly {
		// Display snapshots must not queue behind the accounting writer.
		// Lease authority and mutations pass nil and retain the durable path.
		tx, err = s.readSnapshot(ctx)
	} else {
		tx, err = s.db.BeginTx(ctx, opts)
	}
	diagnosticFinishPhase(ctx, err)
	if err == nil {
		diagnosticEnter(ctx, DiagnosticPhaseWork)
	}
	return tx, err
}

func (s *Store) diagnosticCommit(ctx context.Context, tx *sql.Tx) error {
	diagnosticEnter(ctx, DiagnosticPhaseCommit)
	err := tx.Commit()
	diagnosticFinishPhase(ctx, err)
	return err
}

func (s *Store) diagnosticRollback(ctx context.Context, tx *sql.Tx) {
	diagnosticEnter(ctx, DiagnosticPhaseRollback)
	err := tx.Rollback()
	diagnosticFinishPhase(ctx, err)
}

func diagnosticRecordPacket(r diagnosticRecord) []byte {
	if int(r.kind) >= len(diagnosticKindNames) || r.operation == 0 || r.operation >= diagnosticOperationCount || r.source == 0 || r.source >= diagnosticSourceCount || r.phase >= diagnosticPhaseCount || r.phase == 0 && r.kind != diagnosticScopeExit || int(r.result) >= len(diagnosticResultNames) || int(r.context) >= len(diagnosticContextNames) {
		return nil
	}
	phaseName := "none"
	if r.phase != 0 {
		phaseName = diagnosticPhaseNames[r.phase]
	}
	packet := fmt.Appendf(nil, "PRIORITY=5\nSYSLOG_IDENTIFIER=dark-pro-business-phase\nMESSAGE=v=1 kind=%s seq=%d parent=%d op=%s source=%s phase=%s at_ns=%d duration_ns=%d result=%s context=%s",
		diagnosticKindNames[r.kind], r.span, r.parent, diagnosticOperationNames[r.operation], diagnosticSourceNames[r.source], phaseName, r.at, r.duration, diagnosticResultNames[r.result], diagnosticContextNames[r.context])
	if r.sqlitePresent {
		packet = fmt.Appendf(packet, " sqlite_primary=%d sqlite_extended=%d", r.sqlitePrimary, r.sqliteExtended)
	}
	packet = append(packet, '\n')
	if len(packet) > diagnosticPacketLimit {
		return nil
	}
	return packet
}

