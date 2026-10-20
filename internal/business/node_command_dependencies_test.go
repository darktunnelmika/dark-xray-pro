package business

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
)

func readDependencyHistory(t *testing.T, s *Store, key []byte, node string) NodeDependencies {
	t.Helper()
	tx, err := s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	out := NodeDependencies{Counts: map[string]*int64{}, UnknownReasons: map[string]string{}}
	inspectCommandDependencies(context.Background(), tx, key, node, &out)
	return out
}

func TestNodeHistoryMapsSurviveStageApplyAndLegacyReplay(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "recorded", true: "legacy"}[legacy], func(t *testing.T) {
			s, _, p := routeQueueFixture(t)
			ctx := context.Background()
			v, err := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, routeRequest(p, 1, "stage", ""))
			if err != nil {
				t.Fatal(err)
			}
			tx, err := s.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			row, err := readRouteCommand(ctx, tx, routeTestKey, v.ID)
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if !reflect.DeepEqual(row.payload.DependencyNodes, []string{p.NodeID}) {
				t.Fatal("new stage omitted map")
			}
			if legacy {
				row.payload.DependencyNodes = nil
				enc, err := sealRouteCommand(routeTestKey, row.view, row.payload)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := tx.Exec(`UPDATE native_route_commands SET payload_enc=? WHERE id=?`, enc, v.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			d := routePoll(t, s, &p, true)
			routeStageAck(t, s, &p, d)
			apply, err := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, routeRequest(p, 2, "apply", v.ID))
			if err != nil {
				t.Fatal(err)
			}
			tx, err = s.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			ap, err := readRouteCommand(ctx, tx, routeTestKey, apply.ID)
			tx.Rollback()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(ap.payload.DependencyNodes, row.payload.DependencyNodes) {
				t.Fatal("apply rewrote stage map")
			}
			out := readDependencyHistory(t, s, routeTestKey, p.NodeID)
			dependencyCount(t, out, "historical_stage_references", 1)
			if legacy {
				dependencyCount(t, out, "unmapped_historical_commands", 2)
				if out.Counts["historical_upstream_command_references"] != nil || out.UnknownReasons["historical_upstream_command_references"] != "historical_node_map_missing" {
					t.Fatal("guessed legacy map")
				}
			} else {
				dependencyCount(t, out, "unmapped_historical_commands", 0)
				dependencyCount(t, out, "historical_upstream_command_references", 0)
			}
		})
	}
}

func TestNodeHistoryRejectsBadMapsCorruptionAndScanLimit(t *testing.T) {
	for _, mode := range []string{"upstream", "missing-owner", "duplicate", "ciphertext", "limit", "missing-stage"} {
		t.Run(mode, func(t *testing.T) {
			s, _, p := routeQueueFixture(t)
			ctx := context.Background()
			v, err := s.QueueRouteCommand(ctx, routeTestKey, p.NodeID, routeRequest(p, 1, "stage", ""))
			if err != nil {
				t.Fatal(err)
			}
			tx, err := s.db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			row, err := readRouteCommand(ctx, tx, routeTestKey, v.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "upstream":
				row.payload.DependencyNodes = []string{p.NodeID, "upstream"}
			case "missing-owner":
				row.payload.DependencyNodes = []string{"upstream"}
			case "duplicate":
				row.payload.DependencyNodes = []string{p.NodeID, p.NodeID}
			case "missing-stage":
				row.view.StageID = routeTestID(999)
				row.payload.Command.StageID = row.view.StageID
			}
			enc, err := sealRouteCommand(routeTestKey, row.view, row.payload)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "ciphertext" {
				enc = []byte{0}
			}
			if _, err := tx.Exec(`UPDATE native_route_commands SET payload_enc=? WHERE id=?`, enc, v.ID); err != nil {
				t.Fatal(err)
			}
			if mode == "limit" {
				_, err = tx.Exec(`WITH RECURSIVE seq(n) AS (SELECT 2 UNION ALL SELECT n+1 FROM seq WHERE n<513) INSERT INTO native_route_commands(id,node_id,installation_id,serial,action,stage_id,fingerprint,created,state,payload_enc) SELECT printf('%032x',n),'route-qa','11111111111111111111111111111111',n,'stage',printf('%032x',n),'fp',1,'acknowledged',x'00' FROM seq`)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			out := readDependencyHistory(t, s, routeTestKey, "upstream")
			if mode == "upstream" {
				dependencyCount(t, out, "historical_upstream_command_references", 1)
				return
			}
			reason := "command_history_unverifiable"
			if mode == "limit" {
				reason = "command_history_limit"
			}
			if out.Counts["historical_upstream_command_references"] != nil || out.UnknownReasons["historical_upstream_command_references"] != reason {
				t.Fatal("invalid history trusted", out)
			}
		})
	}
}

func TestNodeHistoryMeteredStageCapturesMap(t *testing.T) {
	s, _, key, _, p := meteredQueueFixture(t)
	meteredQueuedApply(t, s, key, &p)
	out := readDependencyHistory(t, s, key, p.NodeID)
	dependencyCount(t, out, "historical_stage_references", 1)
	dependencyCount(t, out, "unmapped_historical_commands", 0)
	dependencyCount(t, out, "historical_upstream_command_references", 0)
	unknown := readDependencyHistory(t, s, nil, p.NodeID)
	if unknown.UnknownReasons["historical_stage_references"] != "inbound_key_unavailable" {
		t.Fatal("missing key trusted")
	}
}
