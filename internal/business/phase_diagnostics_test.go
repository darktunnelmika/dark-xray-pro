package business

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"modernc.org/sqlite"
)

func diagnosticTestStore() *Store {
	return &Store{phaseDiagnostics: newPhaseDiagnostics(nil)}
}

func diagnosticTestRecord() diagnosticRecord {
	return diagnosticRecord{kind: diagnosticPhaseBegin, span: 1, operation: DiagnosticOperationUsage, source: DiagnosticSourceStore, phase: DiagnosticPhaseWork, at: 1, result: diagnosticNotReported, context: diagnosticContextNone}
}

func TestBusinessPhaseDiagnosticsDisabled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, store := range []*Store{nil, new(Store)} {
		got, span := store.BeginDiagnostic(ctx, DiagnosticOperationRuntime, DiagnosticSourceAPI)
		if got != ctx || span != nil {
			t.Fatal("disabled diagnostics changed the context or allocated a span")
		}
		span.Enter(DiagnosticPhaseAuth)
		span.End(context.Canceled)
		store.StartPhaseDiagnostics()(ctx)
		if !errors.Is(store.EnablePhaseDiagnostics(), ErrUnavailable) {
			t.Fatal("missing store was accepted")
		}
	}
	// A zero-value DB is sufficient here: enabling must not touch its driver or
	// pool, and this test never starts the exporter or performs database work.
	store := &Store{db: &sql.DB{}}
	if store.EnablePhaseDiagnostics() != nil {
		t.Fatal("startup enable failed")
	}
	first := store.phaseDiagnostics
	if store.EnablePhaseDiagnostics() != nil || store.phaseDiagnostics != first || first.started.Load() {
		t.Fatal("enable was not idempotent and inert")
	}
}

func TestBusinessPhaseDiagnosticsNestedCurrentAndCancellation(t *testing.T) {
	s := diagnosticTestStore()
	base, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ctx, parent := s.BeginDiagnostic(base, DiagnosticOperationMetadata, DiagnosticSourceAPI)
	parent.Enter(DiagnosticPhaseAuth)
	childContext, child := s.BeginDiagnostic(ctx, DiagnosticOperationSessionVerify, DiagnosticSourceStore)
	child.Enter(DiagnosticPhaseSessionRead)
	if child.parent != parent.sequence || childContext.Done() != base.Done() {
		t.Fatal("numeric parent or cancellation was not preserved")
	}
	a, aok := childContext.Deadline()
	b, bok := base.Deadline()
	if aok != bok || !a.Equal(b) {
		t.Fatal("deadline changed")
	}
	current := s.phaseDiagnostics.currentRecords(time.Now())
	if len(current) != 2 {
		t.Fatal("current phases missing")
	}
	for _, record := range current {
		if record.context != diagnosticContextUnknown || record.result != diagnosticNotReported || record.duration < 0 {
			t.Fatal("current snapshot invented a result or context state")
		}
	}
	cancel()
	child.finishPhase(context.Canceled)
	child.End(context.Canceled)
	parent.Enter(DiagnosticPhaseResponse)
	parent.End(nil)
	if len(s.phaseDiagnostics.currentRecords(time.Now())) != 0 || s.phaseDiagnostics.snapshot().active != 0 {
		t.Fatal("finished spans remained current")
	}
	seenCanceled, seenParentExit := false, false
	for len(s.phaseDiagnostics.queue) > 0 {
		record := <-s.phaseDiagnostics.queue
		if record.span == child.sequence && record.kind == diagnosticPhaseEnd && record.phase == DiagnosticPhaseSessionRead {
			seenCanceled = record.result == diagnosticContextCanceled && record.context == diagnosticContextIsCanceled && !record.sqlitePresent
		}
		if record.span == parent.sequence && record.kind == diagnosticScopeExit {
			seenParentExit = record.result == diagnosticNoErrorObserved && record.context == diagnosticContextIsCanceled
		}
		if diagnosticRecordPacket(record) == nil {
			t.Fatal("valid recorded event was not serializable")
		}
	}
	if !seenCanceled || !seenParentExit {
		t.Fatal("exit/error distinction missing")
	}
}

func TestBusinessPhaseDiagnosticsDroppedEndCannotLeaveCurrent(t *testing.T) {
	s := diagnosticTestStore()
	d := s.phaseDiagnostics
	for n := 0; n < diagnosticQueueCapacity; n++ {
		d.record(diagnosticTestRecord())
	}
	_, span := s.BeginDiagnostic(context.Background(), DiagnosticOperationUsage, DiagnosticSourceHubLocal)
	span.Enter(DiagnosticPhaseCommit)
	current := d.currentRecords(time.Now())
	if len(current) != 1 || current[0].phase != DiagnosticPhaseCommit || current[0].source != DiagnosticSourceHubLocal {
		t.Fatal("dropped start hid the live commit or mislabeled local work")
	}
	span.End(nil)
	span.End(nil)
	if len(d.currentRecords(time.Now())) != 0 || d.snapshot().active != 0 {
		t.Fatal("dropped end manufactured a current holder")
	}
	stats := d.snapshot()
	if stats.pending != diagnosticQueueCapacity || stats.queueFull != 5 || stats.attempted != diagnosticQueueCapacity+5 || stats.enqueued != diagnosticQueueCapacity {
		t.Fatal("queue overflow accounting changed")
	}
}

func TestBusinessPhaseDiagnosticsCapacityMasksParent(t *testing.T) {
	s := diagnosticTestStore()
	ctx, parent := s.BeginDiagnostic(context.Background(), DiagnosticOperationMetadata, DiagnosticSourceAPI)
	parent.Enter(DiagnosticPhaseAuth)
	spans := []*DiagnosticSpan{parent}
	for len(spans) < diagnosticActiveCapacity {
		_, span := s.BeginDiagnostic(context.Background(), DiagnosticOperationUsage, DiagnosticSourceInternal)
		if span == nil {
			t.Fatal("active capacity smaller than declared")
		}
		spans = append(spans, span)
	}
	untracked, child := s.BeginDiagnostic(ctx, DiagnosticOperationSessionVerify, DiagnosticSourceStore)
	if child != nil || diagnosticSpanFromContext(untracked) != nil || untracked.Done() != ctx.Done() {
		t.Fatal("capacity loss did not mask the parent diagnostic span")
	}
	diagnosticEnter(untracked, DiagnosticPhaseBeginTx)
	diagnosticFinishPhase(untracked, context.Canceled)
	if parent.phase != DiagnosticPhaseAuth || s.phaseDiagnostics.snapshot().untrackedSpans != 1 {
		t.Fatal("untracked child relabeled its parent or was not counted")
	}
	for _, span := range spans {
		span.End(nil)
	}
	if s.phaseDiagnostics.snapshot().active != 0 {
		t.Fatal("active slots leaked")
	}
	_, replacement := s.BeginDiagnostic(context.Background(), DiagnosticOperationObserve, DiagnosticSourceInternal)
	if replacement == nil || replacement.sequence == parent.sequence {
		t.Fatal("released slot was not safely reusable")
	}
	replacement.End(nil)
}

type diagnosticHostileError struct{ cause error }

func (diagnosticHostileError) Error() string   { panic("Error must not be called by diagnostics") }
func (e diagnosticHostileError) Unwrap() error { return e.cause }

type diagnosticPretendSQLiteError struct{}

func (diagnosticPretendSQLiteError) Error() string { panic("Error must not be called by diagnostics") }
func (diagnosticPretendSQLiteError) Code() int     { return 9 }

func TestBusinessPhaseDiagnosticsSafeErrors(t *testing.T) {
	cases := []struct {
		err  error
		want diagnosticResult
	}{
		{nil, diagnosticNoErrorObserved}, {context.Canceled, diagnosticContextCanceled},
		{context.DeadlineExceeded, diagnosticContextDeadline}, {sql.ErrNoRows, diagnosticNoRows},
		{sql.ErrTxDone, diagnosticTxDone}, {driver.ErrBadConn, diagnosticBadConn},
		{diagnosticHostileError{}, diagnosticOtherError}, {diagnosticPretendSQLiteError{}, diagnosticOtherError},
	}
	for _, test := range cases {
		if classifyDiagnosticError(test.err) != test.want {
			t.Fatal("fixed error classification mismatch")
		}
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal("synthetic database open failed")
	}
	defer db.Close()
	if _, err = db.Exec("CREATE TABLE phase_numeric_error(id INTEGER PRIMARY KEY,n INTEGER UNIQUE)"); err != nil {
		t.Fatal("synthetic schema failed")
	}
	if _, err = db.Exec("INSERT INTO phase_numeric_error(n) VALUES(1)"); err != nil {
		t.Fatal("synthetic seed failed")
	}
	_, err = db.Exec("INSERT INTO phase_numeric_error(n) VALUES(1)")
	var actual *sqlite.Error
	if !errors.As(err, &actual) || actual == nil || actual.Code()&255 != 19 {
		t.Fatal("synthetic constraint did not yield a concrete SQLite code")
	}
	s := diagnosticTestStore()
	ctx, cancel := context.WithCancel(context.Background())
	_, span := s.BeginDiagnostic(ctx, DiagnosticOperationUsage, DiagnosticSourceStore)
	cancel()
	wrapped := diagnosticHostileError{cause: err}
	record := span.record(diagnosticPhaseEnd, time.Now(), time.Second, classifyDiagnosticError(wrapped), wrapped)
	if !record.sqlitePresent || int(record.sqliteExtended) != actual.Code() || int(record.sqlitePrimary) != actual.Code()&255 || record.result != diagnosticSQLite || record.context != diagnosticContextIsCanceled {
		t.Fatal("driver code was inferred, lost or conflated with context cancellation")
	}
	packet := diagnosticRecordPacket(record)
	if packet == nil || strings.Contains(string(packet), "phase_numeric_error") || strings.Contains(string(packet), "INSERT") {
		t.Fatal("SQL metadata leaked or valid fixed packet was lost")
	}
	pretend := diagnosticPretendSQLiteError{}
	record = span.record(diagnosticPhaseEnd, time.Now(), 0, classifyDiagnosticError(pretend), pretend)
	if record.sqlitePresent {
		t.Fatal("duck-typed Code was accepted as SQLite")
	}
	span.End(nil)
}

func TestBusinessPhaseDiagnosticsPacketBounds(t *testing.T) {
	r := diagnosticRecord{kind: diagnosticPhaseEnd, span: math.MaxUint64, parent: math.MaxUint64, operation: DiagnosticOperationSessionVerify, source: DiagnosticSourceInternal, phase: DiagnosticPhasePermissionsRead, at: math.MaxInt64, duration: math.MaxInt64, result: diagnosticContextDeadline, context: diagnosticContextIsDeadline, sqlitePresent: true, sqlitePrimary: 255, sqliteExtended: math.MaxInt32}
	if packet := diagnosticRecordPacket(r); len(packet) == 0 || len(packet) > diagnosticPacketLimit {
		t.Fatal("maximum numeric phase record exceeds its fixed packet bound")
	}
	s := diagnosticSnapshot{attempted: math.MaxUint64, enqueued: math.MaxUint64, sent: math.MaxUint64, queueFull: math.MaxUint64, transportDropped: math.MaxUint64, closedDropped: math.MaxUint64, formatDropped: math.MaxUint64, shutdownDropped: math.MaxUint64, untrackedSpans: math.MaxUint64, invalid: math.MaxUint64, auxSendFailed: math.MaxUint64, stopTimeout: math.MaxUint64, pending: diagnosticQueueCapacity, active: diagnosticActiveCapacity, producers: math.MaxInt64}
	if packet := diagnosticCollectorPacket(s, time.Unix(0, math.MaxInt64)); len(packet) == 0 || len(packet) > diagnosticPacketLimit {
		t.Fatal("maximum collector record exceeds its fixed packet bound")
	}
	maxInt := int(^uint(0) >> 1)
	pool := sql.DBStats{MaxOpenConnections: maxInt, OpenConnections: maxInt, InUse: maxInt, Idle: maxInt, WaitCount: math.MaxInt64, WaitDuration: time.Duration(math.MaxInt64)}
	if packet := diagnosticPoolPacket(pool, time.Unix(0, math.MaxInt64), time.Duration(math.MaxInt64)); len(packet) == 0 || len(packet) > diagnosticPacketLimit {
		t.Fatal("maximum pool record exceeds its fixed packet bound")
	}
	r.operation = diagnosticOperationCount
	if diagnosticRecordPacket(r) != nil {
		t.Fatal("unknown operation was serialized")
	}
	typeOf := reflect.TypeOf(diagnosticRecord{})
	for i := 0; i < typeOf.NumField(); i++ {
		switch typeOf.Field(i).Type.Kind() {
		case reflect.Bool, reflect.Int32, reflect.Int64, reflect.Uint8, reflect.Uint64:
		default:
			t.Fatal("queued record retains a nonnumeric value")
		}
	}
}

func TestBusinessPhaseDiagnosticsMissingSinkAndShutdownAccounting(t *testing.T) {
	d := newPhaseDiagnostics(nil)
	for n := 0; n < diagnosticQueueCapacity; n++ {
		d.record(diagnosticTestRecord())
	}
	d.flushEvents(nil)
	if d.snapshot().transportDropped != diagnosticBatchLimit || len(d.queue) != diagnosticQueueCapacity-diagnosticBatchLimit {
		t.Fatal("missing transport was not an explicitly counted bounded batch")
	}
	d.closed.Store(true)
	d.discardQueued()
	d.record(diagnosticTestRecord())
	s := d.snapshot()
	if s.shutdownDropped != diagnosticQueueCapacity-diagnosticBatchLimit || s.closedDropped != 1 || s.pending != 0 || s.attempted != s.enqueued+s.queueFull+s.closedDropped {
		t.Fatal("shutdown loss accounting is inconsistent after producers finished")
	}
}

func TestBusinessPhaseDiagnosticsBeginWaitCancellation(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal("synthetic database open failed")
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	reserved, err := db.Conn(ctx)
	if err != nil {
		t.Fatal("synthetic connection reservation failed")
	}
	defer reserved.Close()
	// The synthetic pool stands in for the committed reader pool here.
	s := &Store{db: db, readOnly: db}
	if s.EnablePhaseDiagnostics() != nil {
		t.Fatal("enable failed")
	}
	request, cancelRequest := context.WithCancel(ctx)
	defer cancelRequest()
	result := make(chan error, 1)
	go func() {
		traced, span := s.BeginDiagnostic(request, DiagnosticOperationRuntime, DiagnosticSourceStore)
		tx, err := s.diagnosticBeginTx(traced, &sql.TxOptions{ReadOnly: true})
		if tx != nil {
			s.diagnosticRollback(traced, tx)
		}
		span.End(err)
		result <- err
	}()
	seen := false
	for !seen {
		select {
		case record := <-s.phaseDiagnostics.queue:
			seen = record.kind == diagnosticPhaseBegin && record.phase == DiagnosticPhaseBeginTx
		case <-ctx.Done():
			t.Fatal("begin phase was not visible while acquisition was blocked")
		}
	}
	current := s.phaseDiagnostics.currentRecords(time.Now())
	if len(current) != 1 || current[0].phase != DiagnosticPhaseBeginTx {
		t.Fatal("blocked begin was not current")
	}
	cancelRequest()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatal("original cancellation result changed")
		}
	case <-ctx.Done():
		t.Fatal("canceled begin did not return")
	}
	if err = reserved.Close(); err != nil {
		t.Fatal("reserved connection release failed")
	}
	var one int
	if err = db.QueryRowContext(ctx, "SELECT 1").Scan(&one); err != nil || one != 1 || db.Stats().InUse != 0 {
		t.Fatal("canceled acquisition leaked the connection or prevented recovery")
	}
}
