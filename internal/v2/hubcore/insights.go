package hubcore

import "context"

// Insights feeds the live dashboard: traffic per day across all clients,
// today's heaviest users, and the latest enforcement events from nodes.
type Insights struct {
	Daily  []ClientDaily `json:"daily"`
	Top    []TopUser     `json:"top"`
	Events []Event       `json:"events"`
}

type TopUser struct {
	Email string `json:"email"`
	Bytes int64  `json:"bytes"`
}

type Event struct {
	Unix   int64  `json:"unix"`
	NodeID string `json:"node_id"`
	Email  string `json:"email"`
	Kind   string `json:"kind"`
}

func (s *Store) Insights(ctx context.Context, days int) (Insights, error) {
	if days <= 0 || days > 90 {
		days = 14
	}
	var in Insights
	today := s.now().UTC()
	from := today.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	rows, err := s.ro.QueryContext(ctx, `SELECT day,SUM(up),SUM(down) FROM usage_daily WHERE day>=? GROUP BY day`, from)
	if err != nil {
		return in, err
	}
	got := map[string]ClientDaily{}
	for rows.Next() {
		var d ClientDaily
		if err := rows.Scan(&d.Day, &d.Up, &d.Down); err != nil {
			rows.Close()
			return in, err
		}
		got[d.Day] = d
	}
	rows.Close()
	for i := days - 1; i >= 0; i-- {
		day := today.AddDate(0, 0, -i).Format("2006-01-02")
		d := got[day]
		d.Day = day
		in.Daily = append(in.Daily, d)
	}
	rows, err = s.ro.QueryContext(ctx, `SELECT email,SUM(up+down) b FROM usage_daily WHERE day=? GROUP BY email ORDER BY b DESC LIMIT 8`, today.Format("2006-01-02"))
	if err != nil {
		return in, err
	}
	in.Top = []TopUser{}
	for rows.Next() {
		var u TopUser
		if err := rows.Scan(&u.Email, &u.Bytes); err != nil {
			rows.Close()
			return in, err
		}
		in.Top = append(in.Top, u)
	}
	rows.Close()
	rows, err = s.ro.QueryContext(ctx, `SELECT unix,node_id,email,kind FROM events ORDER BY id DESC LIMIT 15`)
	if err != nil {
		return in, err
	}
	defer rows.Close()
	in.Events = []Event{}
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Unix, &e.NodeID, &e.Email, &e.Kind); err != nil {
			return in, err
		}
		in.Events = append(in.Events, e)
	}
	return in, rows.Err()
}
