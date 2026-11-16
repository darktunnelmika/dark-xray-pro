package hubcore

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ShopCategory is a group of products in a seller's shop: its order in the
// bot and an optional emoji. Products refer to it by name.
type ShopCategory struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
	Old   string `json:"old,omitempty"` // on save: the name it had (a rename)
	Count int    `json:"count"`         // published products in it (read only)
}

func catKey(seller string) string { return "shop_cats:" + seller }

// ShopCategories lists a seller's categories in their order: the saved list
// first, then any category a product names that the list does not have yet.
func (s *Store) ShopCategories(ctx context.Context, seller string) []ShopCategory {
	var list []ShopCategory
	_ = json.Unmarshal([]byte(s.Setting(ctx, catKey(seller))), &list)
	at := map[string]int{}
	for i := range list {
		at[list[i].Name] = i
	}
	rows, err := s.ro.QueryContext(ctx, `SELECT category,SUM(status='published') FROM products WHERE seller=? AND category<>'' AND status<>'archived' GROUP BY category ORDER BY MIN(sort),MIN(price)`, seller)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name string
			var n int
			if rows.Scan(&name, &n) != nil {
				continue
			}
			if i, ok := at[name]; ok {
				list[i].Count = n
			} else {
				at[name] = len(list)
				list = append(list, ShopCategory{Name: name, Count: n})
			}
		}
	}
	if list == nil {
		list = []ShopCategory{}
	}
	return list
}

// PutShopCategories saves the order and emojis. A category with Old set is
// renamed on its products too; a category that is no longer listed leaves
// its products uncategorised.
func (s *Store) PutShopCategories(ctx context.Context, by Principal, seller string, list []ShopCategory) error {
	if by.Role == "rep" {
		seller = by.RepID
	} else if by.Role != "owner" {
		return ErrAuth
	}
	if len(list) > 40 {
		return fmt.Errorf("%w: at most 40 categories", ErrInvalid)
	}
	seen := map[string]bool{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	clean := make([]ShopCategory, 0, len(list))
	for _, c := range list {
		c.Name, c.Emoji, c.Old = strings.TrimSpace(c.Name), strings.TrimSpace(c.Emoji), strings.TrimSpace(c.Old)
		if c.Name == "" || len(c.Name) > 40 || utf8.RuneCountInString(c.Emoji) > 4 || seen[c.Name] {
			return fmt.Errorf("%w: category names must be unique and short", ErrInvalid)
		}
		seen[c.Name] = true
		if c.Old != "" && c.Old != c.Name {
			if _, err := tx.ExecContext(ctx, `UPDATE products SET category=? WHERE seller=? AND category=?`, c.Name, seller, c.Old); err != nil {
				return err
			}
		}
		clean = append(clean, ShopCategory{Name: c.Name, Emoji: c.Emoji})
	}
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT category FROM products WHERE seller=? AND category<>''`, seller)
	if err != nil {
		return err
	}
	var gone []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil && !seen[name] {
			gone = append(gone, name)
		}
	}
	rows.Close()
	for _, name := range gone {
		if _, err := tx.ExecContext(ctx, `UPDATE products SET category='' WHERE seller=? AND category=?`, seller, name); err != nil {
			return err
		}
	}
	raw, _ := json.Marshal(clean)
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, catKey(seller), string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}
