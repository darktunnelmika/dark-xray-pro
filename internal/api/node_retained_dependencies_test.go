package api

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestNodeRetainedStagesVersionsAndRollbackHistory(t *testing.T) {
	s, versions, metadata := labRevisionServer(t)
	m, raw, err := s.stages.VerifiedPrivateForLabRevision(metadata.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	v1, err := versions.Prepare(m, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := versions.Select(v1.ID, versions.Snapshot().Generation); err != nil {
		t.Fatal(err)
	}
	m.OperationID = strings.Repeat("e", 32)
	v2, err := versions.Prepare(m, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := versions.Select(v2.ID, versions.Snapshot().Generation); err != nil {
		t.Fatal(err)
	}
	read := func(install string) business.NodeDependencies {
		out := business.NodeDependencies{Counts: map[string]*int64{}, UnknownReasons: map[string]string{}}
		s.retainedNodeDependencies(metadata.NodeID, install, &out)
		return out
	}
	before, _ := json.Marshal(versions.Snapshot())
	stagesBefore, _ := json.Marshal(s.stages.List())
	out := read(metadata.InstallationID)
	for key, want := range map[string]int64{"saved_config_stages": 1, "pending_config_stages": 0, "saved_lab_versions": 2, "selected_lab_versions": 1, "previous_lab_versions": 1, "lab_selection_events": 2, "foreign_lab_versions": 0} {
		if out.Counts[key] == nil || *out.Counts[key] != want {
			t.Fatalf("%s: %v want %d", key, out.Counts[key], want)
		}
	}
	after, _ := json.Marshal(versions.Snapshot())
	stagesAfter, _ := json.Marshal(s.stages.List())
	if !bytes.Equal(before, after) || !bytes.Equal(stagesBefore, stagesAfter) {
		t.Fatal("inspection changed journals")
	}
	foreign := read(strings.Repeat("f", 32))
	if *foreign.Counts["foreign_config_stages"] != 1 || *foreign.Counts["foreign_lab_versions"] != 2 {
		t.Fatal("old installation hidden")
	}
	if _, err := versions.RollbackSelection(versions.Snapshot().Generation); err != nil {
		t.Fatal(err)
	}
	out = read(metadata.InstallationID)
	if *out.Counts["saved_lab_versions"] != 2 || *out.Counts["lab_selection_events"] != 3 || *out.Counts["previous_lab_versions"] != 0 {
		t.Fatal("rollback erased history")
	}
	s.stages = nil
	s.labRevisions = nil
	out = read(metadata.InstallationID)
	if out.Counts["saved_config_stages"] != nil || out.Counts["saved_lab_versions"] != nil || len(out.UnknownReasons) != 8 {
		t.Fatal("disabled journals shown as empty")
	}
}
