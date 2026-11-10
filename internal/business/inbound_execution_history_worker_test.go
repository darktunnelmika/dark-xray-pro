package business

import (
	"context"
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

func TestExecutionHistoryWorkerCloseWhileWriterBusy(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	worker, err := openExecutionHistoryMaintainer(f.path, f.s.db)
	if err != nil {
		t.Fatal(err)
	}
	defer worker.close()
	holder, err := f.s.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	// Keep the accounting connection reserved across the scheduled attempt.
	time.Sleep(1050 * time.Millisecond)
	done := make(chan error, 1)
	go func() { done <- worker.close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		holder.Close()
		<-done
		t.Fatal("shutdown waited for an occupied accounting connection")
	}
	holder.Close()
	if err = f.s.db.Ping(); err != nil {
		t.Fatal("maintenance closed the accounting pool", err)
	}
}

func TestExecutionHistoryWorkerRejectsUnsafeWriterPool(t *testing.T) {
	f := newExecutionFixture(t)
	defer f.s.Close()
	if worker, err := openExecutionHistoryMaintainer(f.path, nil); err == nil {
		worker.close()
		t.Fatal("missing writer reservation accepted")
	}
	f.s.db.SetMaxOpenConns(2)
	defer f.s.db.SetMaxOpenConns(1)
	if worker, err := openExecutionHistoryMaintainer(f.path, f.s.db); err == nil {
		worker.close()
		t.Fatal("reservation cannot exclude a second accounting connection")
	}
}
