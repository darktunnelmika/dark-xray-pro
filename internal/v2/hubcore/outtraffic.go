package hubcore

import "context"

const outboundTrafficSchema = `
CREATE TABLE IF NOT EXISTS outbound_traffic (
  tag TEXT NOT NULL,
  node_id TEXT NOT NULL,
  day TEXT NOT NULL,
  up INTEGER NOT NULL DEFAULT 0,
  down INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(tag, node_id, day)
);
`

func (s *Store) ensureOutboundTrafficSchema() error {
	_, err := s.db.Exec(outboundTrafficSchema)
	return err
}

// OutboundTraffic is how much went out through one outbound (direct,
// block, WARP, a chain…), today and in the last N days, per node.
type OutboundTraffic struct {
	Tag    string           `json:"tag"`
	Today  int64            `json:"today"`
	Total  int64            `json:"total"`
	ByNode map[string]int64 `json:"by_node"`
}

func (s *Store) OutboundTraffic(ctx context.Context, days int) ([]OutboundTraffic, error) {
	if days <= 0 || days > 400 {
		days = 30
	}
	today := s.now().UTC().Format("2006-01-02")
	since := s.now().UTC().AddDate(0, 0, -days+1).Format("2006-01-02")
	rows, err := s.ro.QueryContext(ctx, `SELECT tag,node_id,SUM(up+down),SUM(CASE WHEN day=? THEN up+down ELSE 0 END)
FROM outbound_traffic WHERE day>=? GROUP BY tag,node_id ORDER BY tag`, today, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	idx := map[string]int{}
	out := []OutboundTraffic{}
	for rows.Next() {
		var tag, node string
		var total, td int64
		if err := rows.Scan(&tag, &node, &total, &td); err != nil {
			return nil, err
		}
		i, ok := idx[tag]
		if !ok {
			i = len(out)
			idx[tag] = i
			out = append(out, OutboundTraffic{Tag: tag, ByNode: map[string]int64{}})
		}
		out[i].Total += total
		out[i].Today += td
		out[i].ByNode[node] += total
	}
	return out, rows.Err()
}
