package business

import (
	"context"
	"database/sql"
)

// TelegramStoreSetup describes saved store prerequisites. It is not evidence of
// a successful Telegram payment, sufficient checkout credit or native delivery.
type TelegramStoreSetup struct {
	AccountEnabled     bool   `json:"account_enabled"`
	ManagerRegistered  bool   `json:"manager_registered"`
	CanCreateClients   bool   `json:"can_create_clients"`
	CanRenewClients    bool   `json:"can_renew_clients"`
	PublishedProducts  int64  `json:"published_products"`
	PricedProducts     int64  `json:"priced_products"`
	CatalogLimit       int    `json:"catalog_limit"`
	TrialEnabled       bool   `json:"trial_enabled"`
	TrialDurationHours int    `json:"trial_duration_hours"`
	TrialStartsAt      string `json:"trial_starts_at"`
}

type TelegramStoreSetupSnapshot struct {
	TelegramStore
	Setup TelegramStoreSetup `json:"setup"`
}

// ListTelegramStoreSetupAs reads metadata and prerequisites in the same
// authenticated snapshot. It does not impersonate the assigned Telegram
// manager, register a customer, enable schema or change any saved setting.
func (s *Store) ListTelegramStoreSetupAs(ctx context.Context, token string) ([]TelegramStoreSetupSnapshot, error) {
	if ctx == nil || s == nil || s.db == nil {
		return nil, ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return nil, err
	}
	scope, err := telegramBrowserScopeTx(ctx, tx, token)
	if err != nil {
		return nil, err
	}

	filter := ""
	args := []any{}
	if scope != "" {
		filter = " WHERE representative_id=?"
		args = append(args, scope)
	}
	// The first CTE keeps the existing 1,000-store browser bound. Catalog counts
	// are grouped once per visible native account, while Stars prices stay
	// store-specific even when several brands share that account. Every joined
	// input is constrained by this authenticated set; there are no per-store or
	// per-product database calls. Existing catalog/price/customer indexes apply.
	query := `WITH visible_stores AS (
 SELECT ` + telegramStoreColumns + ` FROM telegram_commerce_stores` + filter + `
 ORDER BY store_id LIMIT 1001
), visible_accounts AS (
 SELECT DISTINCT representative_id FROM visible_stores
), published_counts AS (
 SELECT c.representative_id,COUNT(*) AS products
 FROM catalog_products c JOIN visible_accounts a ON a.representative_id=c.representative_id
 WHERE c.status='published' GROUP BY c.representative_id
), priced_counts AS (
 SELECT v.store_id,COUNT(*) AS products
 FROM visible_stores v
 JOIN telegram_commerce_prices p ON p.store_id=v.store_id
 JOIN catalog_products c ON c.id=p.product_id AND c.representative_id=v.representative_id AND c.status='published'
 GROUP BY v.store_id
)
SELECT v.store_id,v.bot_id,v.representative_id,v.name,v.manager_user_id,v.enabled,v.revision,
 r.kind,r.enabled,COALESCE(ctrl.login_suspended,0),COALESCE(ctrl.create_clients,1),COALESCE(ctrl.renew_clients,1),
 manager.customer_id IS NOT NULL,COALESCE(products.products,0),COALESCE(prices.products,0),COALESCE(trial.enabled,0)
FROM visible_stores v
LEFT JOIN representatives r ON r.id=v.representative_id
LEFT JOIN representative_controls ctrl ON ctrl.representative_id=v.representative_id
LEFT JOIN telegram_commerce_customers manager ON manager.store_id=v.store_id AND manager.telegram_user_id=v.manager_user_id AND manager.private_chat_id=v.manager_user_id
LEFT JOIN published_counts products ON products.representative_id=v.representative_id
LEFT JOIN priced_counts prices ON prices.store_id=v.store_id
LEFT JOIN telegram_commerce_trial_policy trial ON trial.store_id=v.store_id
ORDER BY v.store_id`
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TelegramStoreSetupSnapshot{}
	for rows.Next() {
		// These constants describe existing commerce behavior: the product
		// readers reject more than 200 entries and trials start at issuance.
		item := TelegramStoreSetupSnapshot{Setup: TelegramStoreSetup{
			CatalogLimit: 200, TrialDurationHours: 24, TrialStartsAt: "issuance",
		}}
		var kind string
		var enabled, suspended, create, renew bool
		if err = rows.Scan(&item.ID, &item.BotID, &item.RepresentativeID, &item.Name, &item.ManagerUserID, &item.Enabled, &item.Revision,
			&kind, &enabled, &suspended, &create, &renew, &item.Setup.ManagerRegistered,
			&item.Setup.PublishedProducts, &item.Setup.PricedProducts, &item.Setup.TrialEnabled); err != nil {
			return nil, err
		}
		if kind != "owner" && kind != "representative" || (kind == "owner") != (item.RepresentativeID == "owner") ||
			item.Setup.PublishedProducts < 0 || item.Setup.PublishedProducts > 1<<53-1 ||
			item.Setup.PricedProducts < 0 || item.Setup.PricedProducts > item.Setup.PublishedProducts {
			return nil, ErrUnavailable
		}
		item.Setup.AccountEnabled = enabled && !suspended
		item.Setup.CanCreateClients = item.Setup.AccountEnabled && (kind == "owner" || create)
		item.Setup.CanRenewClients = item.Setup.AccountEnabled && (kind == "owner" || renew)
		out = append(out, item)
		if len(out) > 1000 {
			return nil, ErrUnavailable
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
