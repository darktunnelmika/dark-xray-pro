package business

import (
	"testing"
	"time"
)

func TestExecutionHistoryWorkerOptInAndClose(t *testing.T) {
	f := newExecutionFixture(t)
	path := f.path
	if f.s.historyMaintenance != nil {
		t.Fatal("maintenance enabled without opt in")
	}
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DARK_PRO_USAGE_HISTORY_MAINTENANCE", "1")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.historyMaintenance == nil {
		s.Close()
		t.Fatal("explicit opt in ignored")
	}
	start := time.Now()
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("worker shutdown delayed")
	}
	if err = s.historyMaintenance.close(); err != nil {
		t.Fatal("repeated worker close", err)
	}
}
