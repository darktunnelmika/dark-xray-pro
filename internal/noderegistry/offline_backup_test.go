package noderegistry

import (
	"bytes"
	"os"
	"testing"
	"time"
)

func TestOfflineBackupRegistryPreservesPendingAndPairedIdentities(t *testing.T) {
	now := time.Now().UTC()
	reg, path := openTest(t, &now)
	pair, err := reg.Issue("paired", "Paired")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reg.Claim(pair.PairCode, "paired", testInstall, testToken); err != nil {
		t.Fatal(err)
	}
	if _, err = reg.Issue("pending", "Pending"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = ValidateOfflineBackup(raw); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("offline validation changed registry")
	}
	duplicate := bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1)
	if ValidateOfflineBackup(duplicate) == nil || ValidateOfflineBackup(append(raw, []byte(`{}`)...)) == nil {
		t.Fatal("ambiguous registry accepted")
	}
	broken := bytes.Replace(raw, []byte(testInstall), []byte(otherInstall), 1)
	if ValidateOfflineBackup(broken) == nil {
		t.Fatal("receipt/binding mismatch accepted")
	}
}
