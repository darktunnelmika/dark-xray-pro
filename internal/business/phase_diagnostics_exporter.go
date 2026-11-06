package business

import (
	"database/sql"
	"fmt"
	"time"
)

// A snapshot is eventually consistent while producers run. Its counters are
// cumulative; closed means admission has ended, not that output was durable or
// every previously running request has exited. Pending and active remain visible.
type diagnosticSnapshot struct {
	attempted, enqueued, sent uint64
	queueFull, transportDropped, closedDropped, formatDropped uint64
	shutdownDropped, untrackedSpans, invalid, auxSendFailed, stopTimeout uint64
	pending, active int
	producers int64
	closed bool
}

func (d *phaseDiagnostics) snapshot() diagnosticSnapshot {
	s := diagnosticSnapshot{
		attempted: d.counters.attempted.Load(), enqueued: d.counters.enqueued.Load(), sent: d.counters.sent.Load(),
		queueFull: d.counters.queueFull.Load(), transportDropped: d.counters.transportDropped.Load(), closedDropped: d.counters.closedDropped.Load(), formatDropped: d.counters.formatDropped.Load(),
		shutdownDropped: d.counters.shutdownDropped.Load(), untrackedSpans: d.counters.untrackedSpans.Load(), invalid: d.counters.invalid.Load(), auxSendFailed: d.counters.auxSendFailed.Load(), stopTimeout: d.counters.stopTimeout.Load(),
		pending: len(d.queue), producers: d.counters.producers.Load(), closed: d.closed.Load(),
	}
	for i := range d.active {
		if d.active[i].Load() != nil {
			s.active++
		}
	}
	return s
}

func diagnosticCollectorPacket(s diagnosticSnapshot, now time.Time) []byte {
	packet := fmt.Appendf(nil, "PRIORITY=5\nSYSLOG_IDENTIFIER=dark-pro-business-phase\nMESSAGE=v=1 kind=collector at_ns=%d attempt=%d enq=%d sent=%d full=%d transport=%d late_drop=%d format=%d discard=%d untracked=%d invalid=%d aux_fail=%d stopwait=%d pending=%d active=%d prod=%d closed=%t\n",
		now.UnixNano(), s.attempted, s.enqueued, s.sent, s.queueFull, s.transportDropped, s.closedDropped, s.formatDropped, s.shutdownDropped, s.untrackedSpans, s.invalid, s.auxSendFailed, s.stopTimeout, s.pending, s.active, s.producers, s.closed)
	if len(packet) > diagnosticPacketLimit {
		return nil
	}
	return packet
}

func diagnosticPoolPacket(stats sql.DBStats, sampled time.Time, duration time.Duration) []byte {
	packet := fmt.Appendf(nil, "PRIORITY=5\nSYSLOG_IDENTIFIER=dark-pro-business-phase\nMESSAGE=v=1 kind=db_pool at_ns=%d sample_ns=%d max_open=%d open=%d in_use=%d idle=%d wait_count=%d wait_ns=%d scope=process_global\n",
		sampled.UnixNano(), int64(duration), stats.MaxOpenConnections, stats.OpenConnections, stats.InUse, stats.Idle, stats.WaitCount, int64(stats.WaitDuration))
	if len(packet) > diagnosticPacketLimit {
		return nil
	}
	return packet
}

func (d *phaseDiagnostics) flushEvents(journal *phaseDiagnosticJournal) {
	for n := 0; n < diagnosticBatchLimit && !d.closed.Load(); n++ {
		select {
		case record := <-d.queue:
			packet := diagnosticRecordPacket(record)
			if packet == nil {
				d.counters.formatDropped.Add(1)
			} else if journal.send(packet) {
				d.counters.sent.Add(1)
			} else {
				d.counters.transportDropped.Add(1)
			}
		default:
			return
		}
	}
}

func (d *phaseDiagnostics) currentRecords(now time.Time) []diagnosticRecord {
	records := make([]diagnosticRecord, 0, diagnosticActiveCapacity)
	for i := range d.active {
		current := d.active[i].Load()
		if current == nil || current.phase == 0 || now.Before(current.entered) {
			continue
		}
		record := diagnosticRecord{kind: diagnosticCurrentPhase, span: current.span, parent: current.parent, operation: current.operation, source: current.source, phase: current.phase, at: now.UnixNano(), duration: int64(now.Sub(current.entered)), result: diagnosticNotReported, context: diagnosticContextUnknown}
		if d.active[i].Load() == current {
			// The record describes this observation, not the later send time.
			records = append(records, record)
		}
	}
	return records
}

func (d *phaseDiagnostics) sendAux(journal *phaseDiagnosticJournal, packet []byte) {
	if !journal.send(packet) {
		d.counters.auxSendFailed.Add(1)
	}
}

// Stop discards a bounded amount of queued output; it never drains it to a sink.
// A producer already inside record may finish after this pass. Such a late
// queued record remains counted by pending; no final completeness is asserted.
func (d *phaseDiagnostics) discardQueued() {
	for n := 0; n < diagnosticQueueCapacity; n++ {
		select {
		case <-d.queue:
			d.counters.shutdownDropped.Add(1)
		default:
			return
		}
	}
}

// The concrete journal is private and nonblocking. Tests can exercise the same
// socket implementation against an owned Unix socket without a writer callback.
func (d *phaseDiagnostics) runExporter(journal *phaseDiagnosticJournal) {
	defer func() { journal.close() }()
	tick := time.NewTicker(diagnosticTick)
	defer tick.Stop()
	now := time.Now()
	nextEvents := now
	nextSummary := now.Add(diagnosticSummaryInterval)
	for {
		if d.closed.Load() {
			d.discardQueued()
			d.sendAux(journal, diagnosticCollectorPacket(d.snapshot(), time.Now()))
			return
		}
		select {
		case <-d.stop:
			// Re-enter the cancellation branch before any further stats or sends.
			continue
		case <-tick.C:
		}
		if d.closed.Load() {
			continue
		}
		now = time.Now()
		if !now.Before(nextEvents) {
			d.flushEvents(journal)
			// Completion-based scheduling prevents a delayed ticker catch-up burst.
			nextEvents = time.Now().Add(diagnosticTick)
		}
		if now.Before(nextSummary) || d.closed.Load() {
			continue
		}
		if journal == nil {
			journal = openPhaseDiagnosticJournal()
		}
		for _, record := range d.currentRecords(time.Now()) {
			if d.closed.Load() {
				break
			}
			d.sendAux(journal, diagnosticRecordPacket(record))
		}
		if !d.closed.Load() && d.db != nil {
			started := time.Now()
			stats := d.db.Stats() // memory-only pool snapshot; never a request-path call
			finished := time.Now()
			if !d.closed.Load() {
				d.sendAux(journal, diagnosticPoolPacket(stats, finished, finished.Sub(started)))
			}
		}
		if !d.closed.Load() {
			d.sendAux(journal, diagnosticCollectorPacket(d.snapshot(), time.Now()))
		}
		nextSummary = time.Now().Add(diagnosticSummaryInterval)
	}
}
