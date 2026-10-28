package configstage

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

func TestOfflineBackupStageAuthenticatesRejectedHistoryAndDependencies(t *testing.T) {
	s, key, path := initStore(t)
	op, candidate := fixture(t)
	meta, err := s.Create(op, candidate, testBinarySHA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Report(proofOf(meta, false)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	visited := 0
	check := func(clear []byte) error {
		visited++
		if !bytes.Equal(clear, candidate.Config) {
			return errors.New("payload differs")
		}
		return nil
	}
	if err = ValidateOfflineBackup(raw, key, check); err != nil || visited != 1 {
		t.Fatal("rejected history skipped", err, visited)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("validation changed encrypted store")
	}
	if ValidateOfflineBackup(raw, bytes.Repeat([]byte{42}, 32), check) == nil {
		t.Fatal("wrong key accepted")
	}
	if ValidateOfflineBackup(raw, key, func([]byte) error { return errors.New("external dependency") }) == nil {
		t.Fatal("historical dependency ignored")
	}
	if ValidateOfflineBackup(bytes.Replace(raw, []byte(`"version":1`), []byte(`"version":1,"version":1`), 1), key, check) == nil {
		t.Fatal("ambiguous envelope accepted")
	}
}
