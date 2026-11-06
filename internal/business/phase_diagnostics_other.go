//go:build !linux

package business

// Other platforms retain bounded counters and an explicitly unavailable sink.
// The Linux-only operational exporter is not replaced with a blocking logger.
type phaseDiagnosticJournal struct{}

func openPhaseDiagnosticJournal() *phaseDiagnosticJournal { return nil }
func (*phaseDiagnosticJournal) send([]byte) bool { return false }
func (*phaseDiagnosticJournal) close() {}
