package labrevision

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestOfflineBackupRevisionChecksUnselectedHistoryWithoutSelection(t *testing.T) {
	s, key, path := newStore(t)
	m1, c1 := revisionCandidate(t, strings.Repeat("a", 32), 18569)
	v1, err := s.Prepare(m1, c1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Select(v1.ID, 2); err != nil {
		t.Fatal(err)
	}
	m2, c2 := revisionCandidate(t, strings.Repeat("b", 32), 18570)
	if _, err = s.Prepare(m2, c2); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	visited := 0
	check := func(clear []byte) error {
		visited++
		if !bytes.Equal(clear, c1) && !bytes.Equal(clear, c2) {
			return errors.New("wrong plaintext")
		}
		return nil
	}
	if err = ValidateOfflineBackup(raw, key, check); err != nil || visited != 2 {
		t.Fatal("unselected history omitted", err, visited)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) || s.Snapshot().SelectedVersion != v1.ID {
		t.Fatal("validation changed selection")
	}
	if ValidateOfflineBackup(raw, bytes.Repeat([]byte{42}, 32), check) == nil {
		t.Fatal("wrong key accepted")
	}
	if ValidateOfflineBackup(raw, key, func(clear []byte) error {
		if bytes.Equal(clear, c2) {
			return errors.New("external dependency")
		}
		return nil
	}) == nil {
		t.Fatal("unselected dependency ignored")
	}
	if ValidateOfflineBackup(bytes.Replace(raw, []byte(`"generation":4`), []byte(`"generation":5`), 1), key, check) == nil {
		t.Fatal("forged selection history accepted")
	}
}
