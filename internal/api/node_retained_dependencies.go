package api

import (
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/configstage"
)

// Each store is independently locked. This never claims cross-store atomicity,
// process state, or a retirement permission, and returns no private candidates.
func (s *Server) retainedNodeDependencies(node, install string, out *business.NodeDependencies) {
	stageFields := []string{"saved_config_stages", "pending_config_stages", "foreign_config_stages"}
	revisionFields := []string{"saved_lab_versions", "selected_lab_versions", "previous_lab_versions", "lab_selection_events", "foreign_lab_versions"}
	unknown := func(fields []string, reason string) {
		for _, f := range fields {
			out.Counts[f] = nil
			out.UnknownReasons[f] = reason
		}
	}
	if s.stages == nil {
		unknown(stageFields, "config_stage_store_disabled")
	} else {
		var total, pending, foreign int64
		for _, m := range s.stages.List() {
			if m.NodeID == node {
				total++
				if m.Status == configstage.Pending {
					pending++
				}
				if m.InstallationID != install {
					foreign++
				}
			}
		}
		out.Counts[stageFields[0]] = &total
		out.Counts[stageFields[1]] = &pending
		out.Counts[stageFields[2]] = &foreign
	}
	if s.labRevisions == nil {
		unknown(revisionFields, "lab_revision_store_disabled")
	} else {
		snapshot := s.labRevisions.Snapshot()
		ids := make(map[string]bool)
		var total, selected, previous, events, foreign int64
		for _, v := range snapshot.Versions {
			if v.NodeID == node {
				total++
				ids[v.ID] = true
				if v.InstallationID != install {
					foreign++
				}
			}
		}
		if ids[snapshot.SelectedVersion] {
			selected = 1
		}
		if ids[snapshot.PreviousVersion] {
			previous = 1
		}
		for _, e := range snapshot.Events {
			if (e.Action == "selected_for_lab_rehearsal_not_applied" || e.Action == "lab_selection_rolled_back_no_process_change") && (ids[e.SelectedVersion] || ids[e.PreviousVersion]) {
				events++
			}
		}
		for i, n := range []int64{total, selected, previous, events, foreign} {
			value := n
			out.Counts[revisionFields[i]] = &value
		}
	}
}
