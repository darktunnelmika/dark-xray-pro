package hubcore

import (
	"context"
	"database/sql"
)

const tgSchedSchema = `
CREATE TABLE IF NOT EXISTS tg_scheduled (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  owner_chat INTEGER NOT NULL,
  text TEXT NOT NULL DEFAULT '',
  photo TEXT NOT NULL DEFAULT '',
  btn_text TEXT NOT NULL DEFAULT '',
  btn_url TEXT NOT NULL DEFAULT '',
  audience TEXT NOT NULL,
  at_unix INTEGER NOT NULL,
  repeat TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'pending',
  runs INTEGER NOT NULL DEFAULT 0,
  last_result TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS tg_scheduled_due ON tg_scheduled(status, at_unix);
`

// TgScheduled is a broadcast planned for later; Repeat is "", "daily" or
// "weekly". The audience is resolved when it is sent, not when planned.
type TgScheduled struct {
	ID         int64
	OwnerChat  int64
	Text       string
	Photo      string
	BtnText    string
	BtnURL     string
	Audience   string
	At         int64
	Repeat     string
	Runs       int
	LastResult string
}

func (s *Store) ensureTgSchedSchema() error {
	_, err := s.db.Exec(tgSchedSchema)
	return err
}

func (s *Store) TgSchedule(ctx context.Context, m TgScheduled) (int64, error) {
	if m.At <= 0 || m.Audience == "" || (m.Text == "" && m.Photo == "") {
		return 0, ErrInvalid
	}
	if m.Repeat != "" && m.Repeat != "daily" && m.Repeat != "weekly" {
		return 0, ErrInvalid
	}
	r, err := s.db.ExecContext(ctx, `INSERT INTO tg_scheduled(owner_chat,text,photo,btn_text,btn_url,audience,at_unix,repeat) VALUES(?,?,?,?,?,?,?,?)`,
		m.OwnerChat, m.Text, m.Photo, m.BtnText, m.BtnURL, m.Audience, m.At, m.Repeat)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

const tgSchedCols = `id,owner_chat,text,photo,btn_text,btn_url,audience,at_unix,repeat,runs,last_result`

func scanSched(rows *sql.Rows) ([]TgScheduled, error) {
	defer rows.Close()
	var out []TgScheduled
	for rows.Next() {
		var m TgScheduled
		if err := rows.Scan(&m.ID, &m.OwnerChat, &m.Text, &m.Photo, &m.BtnText, &m.BtnURL, &m.Audience, &m.At, &m.Repeat, &m.Runs, &m.LastResult); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// TgScheduledList returns planned broadcasts, soonest first.
func (s *Store) TgScheduledList(ctx context.Context) ([]TgScheduled, error) {
	rows, err := s.ro.QueryContext(ctx, `SELECT `+tgSchedCols+` FROM tg_scheduled WHERE status='pending' ORDER BY at_unix LIMIT 50`)
	if err != nil {
		return nil, err
	}
	return scanSched(rows)
}

// TgClaimDue marks due broadcasts as sending and returns them, so a
// restart or a second tick never sends one twice.
func (s *Store) TgClaimDue(ctx context.Context) ([]TgScheduled, error) {
	now := s.now().Unix()
	rows, err := s.db.QueryContext(ctx, `UPDATE tg_scheduled SET status='sending' WHERE status='pending' AND at_unix<=? RETURNING `+tgSchedCols, now)
	if err != nil {
		return nil, err
	}
	return scanSched(rows)
}

// TgScheduledDone records a run; repeating broadcasts move to their next
// time in the future, one-off ones are finished.
func (s *Store) TgScheduledDone(ctx context.Context, id int64, result string) error {
	var at int64
	var rep string
	if err := s.db.QueryRowContext(ctx, `SELECT at_unix,repeat FROM tg_scheduled WHERE id=?`, id).Scan(&at, &rep); err != nil {
		return err
	}
	step := map[string]int64{"daily": 86400, "weekly": 7 * 86400}[rep]
	if step == 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE tg_scheduled SET status='sent', runs=runs+1, last_result=? WHERE id=?`, result, id)
		return err
	}
	now := s.now().Unix()
	for at <= now {
		at += step
	}
	_, err := s.db.ExecContext(ctx, `UPDATE tg_scheduled SET status='pending', at_unix=?, runs=runs+1, last_result=? WHERE id=?`, at, result, id)
	return err
}

func (s *Store) TgScheduledCancel(ctx context.Context, id int64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE tg_scheduled SET status='cancelled' WHERE id=? AND status='pending'`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TgFinishInterrupted closes runs cut off by a restart. They are not sent
// again (some recipients already have the message); repeating ones simply
// move on to their next time.
func (s *Store) TgFinishInterrupted(ctx context.Context) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM tg_scheduled WHERE status='sending'`)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		_ = s.TgScheduledDone(ctx, id, "interrupted by a restart")
	}
}
