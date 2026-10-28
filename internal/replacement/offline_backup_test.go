package replacement

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestOfflineBackupReplacementPreservesCancelledOperation(t *testing.T) {
	now := time.Now().UTC()
	journal, path := journalForTest(t, &now)
	op, err := journal.Create(example(t), installID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = journal.Cancel(op.ID, op.Revision); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if ValidateOfflineBackup(raw) != nil {
		t.Fatal("valid retained history rejected")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("offline validation changed journal")
	}
	if ValidateOfflineBackup(bytes.Replace(raw, []byte(`"phase":"cancelled"`), []byte(`"phase":"active"`), 1)) == nil {
		t.Fatal("unknown activation phase accepted")
	}
	if ValidateOfflineBackup(bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1)) == nil {
		t.Fatal("duplicate field accepted")
	}
}
