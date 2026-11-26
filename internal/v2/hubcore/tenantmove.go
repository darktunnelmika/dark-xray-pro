package hubcore

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
)

// Moving many clients at once between the owner, a reseller and a connected
// shop — all of a seller's clients with one call if no names are given.
// Credentials, subscription links, usage and history stay with the clients;
// a move into or out of a shop renames them into or out of the shop's name
// space (usage the nodes still report under the old name is carried over,
// as for any rename). Credit is not moved: adjust it explicitly if needed.

// MoveSide is one end of a move: the owner, a reseller or a tenant.
type MoveSide struct {
	Kind string `json:"kind"` // owner | rep | tenant
	ID   string `json:"id"`   // the reseller's or the tenant's id
}

func (m MoveSide) repID() string {
	switch m.Kind {
	case "rep":
		return m.ID
	case "tenant":
		return tenantRep(m.ID)
	}
	return ""
}

func (m MoveSide) String() string {
	if m.Kind == "owner" {
		return "owner"
	}
	return m.Kind + " " + m.ID
}

// MoveRequest picks clients of From (by their names there, or all of them)
// and moves them To.
type MoveRequest struct {
	From   MoveSide `json:"from"`
	To     MoveSide `json:"to"`
	Names  []string `json:"names"` // empty = every client of From
	DryRun bool     `json:"dry_run"`
}

// MoveReport tells what was (or would be) moved.
type MoveReport struct {
	DryRun  bool              `json:"dry_run"`
	Moved   int               `json:"moved"`
	Renamed map[string]string `json:"renamed,omitempty"` // old name → new name, where they differ beyond the shop prefix
	Skipped []string          `json:"skipped"`
	Notes   []string          `json:"notes"`
}

func (s *Store) checkSide(ctx context.Context, m MoveSide) error {
	var x int
	switch m.Kind {
	case "owner":
		return nil
	case "rep":
		if s.ro.QueryRowContext(ctx, `SELECT 1 FROM reps WHERE id=?`, m.ID).Scan(&x) != nil {
			return fmt.Errorf("%w: reseller %q does not exist", ErrInvalid, m.ID)
		}
		return nil
	case "tenant":
		if s.ro.QueryRowContext(ctx, `SELECT 1 FROM tenants WHERE id=?`, m.ID).Scan(&x) != nil {
			return fmt.Errorf("%w: connected shop %q does not exist", ErrInvalid, m.ID)
		}
		return nil
	}
	return fmt.Errorf("%w: kind is owner, rep or tenant", ErrInvalid)
}

// MoveClients moves clients between the owner, resellers and tenants.
func (s *Store) MoveClients(ctx context.Context, req MoveRequest) (MoveReport, error) {
	rep := MoveReport{DryRun: req.DryRun, Renamed: map[string]string{}, Skipped: []string{}, Notes: []string{}}
	if err := s.checkSide(ctx, req.From); err != nil {
		return rep, err
	}
	if err := s.checkSide(ctx, req.To); err != nil {
		return rep, err
	}
	if req.From == req.To {
		return rep, fmt.Errorf("%w: the clients are already there", ErrInvalid)
	}
	fromRep, toRep := req.From.repID(), req.To.repID()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return rep, err
	}
	defer tx.Rollback()

	// The clients, by their stored names.
	var emails []string
	if len(req.Names) == 0 {
		rows, err := tx.QueryContext(ctx, `SELECT email FROM clients WHERE rep_id=? ORDER BY email`, fromRep)
		if err != nil {
			return rep, err
		}
		for rows.Next() {
			var e string
			if err := rows.Scan(&e); err != nil {
				rows.Close()
				return rep, err
			}
			emails = append(emails, e)
		}
		rows.Close()
	} else {
		seen := map[string]bool{}
		for _, n := range req.Names {
			e := n
			if req.From.Kind == "tenant" {
				e = tenantEmail(req.From.ID, n)
			}
			if seen[e] {
				continue
			}
			seen[e] = true
			var x int
			if tx.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=? AND rep_id=?`, e, fromRep).Scan(&x) != nil {
				rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: not a client of %s", n, req.From))
				continue
			}
			emails = append(emails, e)
		}
	}

	// New names: into a shop's name space, out of it, or unchanged.
	type pair struct{ old, new string }
	var pairs []pair
	var keep []string // moved without a rename
	taken := map[string]bool{}
	for _, e := range emails {
		base := e
		if req.From.Kind == "tenant" {
			base = tenantName(req.From.ID, e)
		}
		n := base
		if req.To.Kind == "tenant" {
			n = tenantSafeName(base)
			if n != base {
				rep.Renamed[base] = n
			}
			n = tenantEmail(req.To.ID, n)
		}
		if n == e {
			keep = append(keep, e)
			continue
		}
		var x int
		low := strings.ToLower(n)
		if taken[low] || tx.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=? COLLATE NOCASE`, n).Scan(&x) == nil {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: the name %s is already taken there", base, strings.TrimPrefix(n, req.To.ID+tenantSep)))
			continue
		}
		if req.To.Kind != "tenant" && reservedName(ctx, tx, n) {
			rep.Skipped = append(rep.Skipped, fmt.Sprintf("%s: the name is in a connected shop's name space", base))
			continue
		}
		taken[low] = true
		pairs = append(pairs, pair{e, n})
	}
	rep.Moved = len(pairs) + len(keep)
	if req.To.Kind == "rep" || req.From.Kind == "rep" {
		rep.Notes = append(rep.Notes, "reseller credit was not changed: adjust it in the reseller's page if needed")
	}
	if req.To.Kind == "tenant" {
		if t, err := s.TenantByID(ctx, req.To.ID); err == nil && len(t.Inbounds) > 0 {
			var n int
			_ = tx.QueryRowContext(ctx, `SELECT COUNT(DISTINCT tag) FROM client_inbounds WHERE email IN (SELECT value FROM json_each(?)) AND tag NOT IN (SELECT value FROM json_each(?))`,
				jsonList(emails), jsonList(t.Inbounds)).Scan(&n)
			if n > 0 {
				rep.Notes = append(rep.Notes, fmt.Sprintf("%d inbounds of the moved clients are not in the shop's list; the clients keep them, but the shop cannot give them to others", n))
			}
		}
	}
	if req.DryRun || rep.Moved == 0 {
		return rep, nil
	}

	// Rename in bulk: one pass over each table that names clients.
	if len(pairs) > 0 {
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys=ON`); err != nil {
			return rep, err
		}
		if _, err := tx.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS move_map(old TEXT PRIMARY KEY, new TEXT NOT NULL)`); err != nil {
			return rep, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM temp.move_map`); err != nil {
			return rep, err
		}
		st, err := tx.PrepareContext(ctx, `INSERT INTO temp.move_map(old,new) VALUES(?,?)`)
		if err != nil {
			return rep, err
		}
		for _, p := range pairs {
			if _, err := st.ExecContext(ctx, p.old, p.new); err != nil {
				st.Close()
				return rep, err
			}
		}
		st.Close()
		if err := renameByMap(ctx, tx, s.now().Unix()); err != nil {
			return rep, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET rep_id=? WHERE email IN (SELECT new FROM temp.move_map)`, toRep); err != nil {
			return rep, err
		}
		if req.From.Kind == "tenant" || req.To.Kind == "tenant" {
			// The shop's own references stay with the shop.
			if _, err := tx.ExecContext(ctx, `UPDATE clients SET external_ref='' WHERE email IN (SELECT new FROM temp.move_map)`); err != nil {
				return rep, err
			}
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM temp.move_map`); err != nil {
			return rep, err
		}
	}
	for _, e := range keep {
		if _, err := tx.ExecContext(ctx, `UPDATE clients SET rep_id=? WHERE email=?`, toRep, e); err != nil {
			return rep, err
		}
	}
	if req.To.Kind == "tenant" {
		// A shop's customers are not the panel bots' to reach.
		if _, err := tx.ExecContext(ctx, `DELETE FROM tg_links WHERE email IN (SELECT email FROM clients WHERE rep_id=?)`, toRep); err != nil {
			return rep, err
		}
	}
	return rep, tx.Commit()
}

func jsonList(v []string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, x := range v {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(fmt.Sprintf("%q", x))
	}
	b.WriteByte(']')
	return b.String()
}

// renameByMap renames every client in temp.move_map (old → new) and moves
// everything stored under the old names, one statement per table — the bulk
// form of RenameClient.
func renameByMap(ctx context.Context, tx *sql.Tx, now int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT m.name, p.name FROM sqlite_master m JOIN pragma_table_info(m.name) p
WHERE m.type='table' AND p.name IN ('email','client_email') ORDER BY m.name`)
	if err != nil {
		return err
	}
	var cols [][2]string
	for rows.Next() {
		var t, c string
		if err := rows.Scan(&t, &c); err != nil {
			rows.Close()
			return err
		}
		cols = append(cols, [2]string{t, c})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE clients SET email=(SELECT new FROM temp.move_map WHERE old=clients.email) WHERE email IN (SELECT old FROM temp.move_map)`); err != nil {
		return err
	}
	for _, tc := range cols {
		if tc[0] == "clients" || tc[0] == "credit_ledger" {
			continue
		}
		q := `UPDATE OR REPLACE "` + tc[0] + `" SET "` + tc[1] + `"=(SELECT new FROM temp.move_map WHERE old="` + tc[0] + `"."` + tc[1] + `") WHERE "` + tc[1] + `" IN (SELECT old FROM temp.move_map)`
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("rename in %s: %w", tc[0], err)
		}
	}
	for _, q := range []string{
		`UPDATE OR REPLACE settings SET key='remind:'||(SELECT new FROM temp.move_map WHERE 'remind:'||old=settings.key) WHERE key IN (SELECT 'remind:'||old FROM temp.move_map)`,
		`DELETE FROM client_renames WHERE old IN (SELECT new FROM temp.move_map)`,
		`UPDATE client_renames SET new=(SELECT new FROM temp.move_map WHERE old=client_renames.new) WHERE new IN (SELECT old FROM temp.move_map)`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT OR REPLACE INTO client_renames(old,new,unix) SELECT old,new,? FROM temp.move_map`, now)
	return err
}

func tenantMoveRoutes(s *Store, mux *docMux, guard func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("POST /api/v2/admin/clients/move", guard(func(w http.ResponseWriter, r *http.Request) {
		var in MoveRequest
		if !decode(w, r, &in) {
			return
		}
		rep, err := s.MoveClients(r.Context(), in)
		reply(w, rep, err)
	}))
}
