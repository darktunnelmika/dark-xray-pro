package business

import (
	"context"
	"database/sql"
	"errors"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

type ClientServiceLabel struct {
	ID   uint64 `json:"id"`
	Name string `json:"name"`
}
type ClientRepresentativeLabel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}
type ClientDirectoryInfo struct {
	Representative *ClientRepresentativeLabel `json:"representative,omitempty"`
	Scope          string                     `json:"scope"`
	Inbounds       []ClientServiceLabel       `json:"inbounds"`
	Usage          *usageledger.Account       `json:"usage,omitempty"`
}

// Only clients already selected by the scoped page query are enriched. Counters,
// ownership and saved membership share its transaction; no per-row HTTP fanout.
func (s *Store) enrichClientDirectory(ctx context.Context, tx *sql.Tx, key []byte, clients []Client) error {
	if len(clients) == 0 {
		return nil
	}
	page := map[string]*Client{}
	representatives := map[string]*ClientRepresentativeLabel{}
	for i := range clients {
		c := &clients[i]
		c.Directory = &ClientDirectoryInfo{Scope: "saved_membership_and_committed_usage", Inbounds: []ClientServiceLabel{}}
		label, found := representatives[c.RepresentativeID]
		if !found {
			label = &ClientRepresentativeLabel{ID: c.RepresentativeID}
			if err := tx.QueryRowContext(ctx, "SELECT display_name FROM representatives WHERE id=?", c.RepresentativeID).Scan(&label.DisplayName); err != nil {
				return err
			}
			representatives[c.RepresentativeID] = label
		}
		c.Directory.Representative = label
		page[c.ID] = c
		if s.usage != nil {
			a, e := s.usage.AccountTx(ctx, tx, c.ID)
			if errors.Is(e, usageledger.ErrMissing) {
				a = usageledger.Account{ClientID: c.ID, RepresentativeID: c.RepresentativeID, Plan: c.PlanKind, Limit: c.VolumeBytes}
				e = nil
			}
			if e != nil {
				return e
			}
			if a.ClientID != c.ID || a.RepresentativeID != c.RepresentativeID {
				return ErrUnavailable
			}
			c.Directory.Usage = &a
		}
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,name FROM native_inbounds ORDER BY id LIMIT 257")
	if e != nil {
		return e
	}
	labels := []ClientServiceLabel{}
	for rows.Next() {
		var label ClientServiceLabel
		if e = rows.Scan(&label.ID, &label.Name); e != nil {
			rows.Close()
			return e
		}
		labels = append(labels, label)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	if len(labels) > 256 {
		return ErrUnavailable
	}
	for _, label := range labels {
		ids, e := clientInboundIDs(ctx, tx, key, label.ID)
		if e != nil {
			return e
		}
		for _, id := range ids {
			if c := page[id]; c != nil {
				c.Directory.Inbounds = append(c.Directory.Inbounds, label)
			}
		}
	}
	return nil
}
