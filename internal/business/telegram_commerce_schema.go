package business

import (
	"context"
	"database/sql"
	"strings"
)

// This family lives in the native business database. It adds shop identity,
// retail payments and receipt provenance, not another representative wallet.
// Credentials and bot tokens are deliberately absent.
var telegramCommerceDDL = []string{
	"CREATE TABLE telegram_commerce_meta(id INTEGER PRIMARY KEY CHECK(id=1),version INTEGER NOT NULL CHECK(version=1))",
	"CREATE TABLE telegram_commerce_stores(store_id TEXT PRIMARY KEY,bot_id INTEGER NOT NULL UNIQUE CHECK(bot_id>0),representative_id TEXT NOT NULL REFERENCES representatives(id),name TEXT NOT NULL,manager_user_id INTEGER NOT NULL CHECK(manager_user_id>0),enabled INTEGER NOT NULL CHECK(enabled IN(0,1)),revision INTEGER NOT NULL CHECK(revision>=1),created_unix INTEGER NOT NULL)",
	"CREATE TABLE telegram_commerce_customers(customer_id TEXT PRIMARY KEY,store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),telegram_user_id INTEGER NOT NULL CHECK(telegram_user_id>0),private_chat_id INTEGER NOT NULL CHECK(private_chat_id=telegram_user_id),created_unix INTEGER NOT NULL,UNIQUE(store_id,telegram_user_id),UNIQUE(store_id,customer_id))",
	"CREATE TABLE telegram_commerce_members(store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),telegram_user_id INTEGER NOT NULL CHECK(telegram_user_id>0),role TEXT NOT NULL CHECK(role IN('owner','representative','admin')),permissions_json TEXT NOT NULL,enabled INTEGER NOT NULL CHECK(enabled IN(0,1)),revision INTEGER NOT NULL CHECK(revision>=1),PRIMARY KEY(store_id,telegram_user_id))",
	"CREATE TABLE telegram_commerce_prices(store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),product_id TEXT NOT NULL REFERENCES catalog_products(id),amount_stars INTEGER NOT NULL CHECK(amount_stars>0 AND amount_stars<=1000000000),revision INTEGER NOT NULL CHECK(revision>=1),PRIMARY KEY(store_id,product_id))",
	"CREATE TABLE telegram_commerce_trial_policy(store_id TEXT PRIMARY KEY REFERENCES telegram_commerce_stores(store_id),enabled INTEGER NOT NULL CHECK(enabled IN(0,1)),volume_bytes INTEGER NOT NULL CHECK(volume_bytes>0 AND volume_bytes<=9000000000000000),revision INTEGER NOT NULL CHECK(revision>=1))",
	"CREATE TABLE telegram_commerce_trials(store_id TEXT NOT NULL,customer_id TEXT NOT NULL,client_id TEXT NOT NULL UNIQUE REFERENCES clients(id),event_id TEXT NOT NULL UNIQUE,receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL,PRIMARY KEY(store_id,customer_id),FOREIGN KEY(store_id,customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id))",
	"CREATE TABLE telegram_commerce_events(event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),actor_user_id INTEGER NOT NULL CHECK(actor_user_id>=0),action TEXT NOT NULL,receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)",
	"CREATE TABLE telegram_commerce_invites(invite_id TEXT PRIMARY KEY,store_id TEXT NOT NULL REFERENCES telegram_commerce_stores(store_id),invited_user_id INTEGER NOT NULL CHECK(invited_user_id>0),permissions_json TEXT NOT NULL,created_by INTEGER NOT NULL CHECK(created_by>0),expires_unix INTEGER NOT NULL,accepted_unix INTEGER NOT NULL DEFAULT 0,revoked INTEGER NOT NULL DEFAULT 0 CHECK(revoked IN(0,1)))",
	"CREATE TABLE telegram_commerce_orders(order_id TEXT PRIMARY KEY,event_id TEXT NOT NULL UNIQUE,store_id TEXT NOT NULL,customer_id TEXT NOT NULL,telegram_user_id INTEGER NOT NULL CHECK(telegram_user_id>0),product_id TEXT NOT NULL REFERENCES catalog_products(id),client_id TEXT NOT NULL,action TEXT NOT NULL CHECK(action IN('create','renew')),currency TEXT NOT NULL CHECK(currency='XTR'),amount_stars INTEGER NOT NULL CHECK(amount_stars>0 AND amount_stars<=1000000000),status TEXT NOT NULL CHECK(status IN('quoted','paid_pending_fulfillment','paid_pending_provisioning','refunded')),product_json BLOB NOT NULL,intent_json BLOB NOT NULL,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),expires_unix INTEGER NOT NULL,created_unix INTEGER NOT NULL,FOREIGN KEY(store_id,customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id))",
	"CREATE TABLE telegram_commerce_payments(bot_id INTEGER NOT NULL CHECK(bot_id>0),charge_id TEXT NOT NULL,order_id TEXT NOT NULL UNIQUE REFERENCES telegram_commerce_orders(order_id),customer_id TEXT NOT NULL REFERENCES telegram_commerce_customers(customer_id),currency TEXT NOT NULL CHECK(currency='XTR'),amount_stars INTEGER NOT NULL CHECK(amount_stars>0),created_unix INTEGER NOT NULL,PRIMARY KEY(bot_id,charge_id))",
	"CREATE TABLE telegram_commerce_order_receipts(order_id TEXT PRIMARY KEY REFERENCES telegram_commerce_orders(order_id),receipt_json BLOB NOT NULL,created_unix INTEGER NOT NULL)",
	"CREATE TABLE telegram_commerce_customer_clients(store_id TEXT NOT NULL,customer_id TEXT NOT NULL,client_id TEXT PRIMARY KEY REFERENCES clients(id),created_unix INTEGER NOT NULL,FOREIGN KEY(store_id,customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id))",
	"CREATE TABLE telegram_commerce_wallet_ledger(event_id TEXT PRIMARY KEY,fingerprint TEXT NOT NULL CHECK(length(fingerprint)=64),store_id TEXT NOT NULL,customer_id TEXT NOT NULL,bucket TEXT NOT NULL CHECK(bucket IN('paid','bonus')),currency TEXT NOT NULL CHECK(currency='XTR'),delta INTEGER NOT NULL CHECK(typeof(delta)='integer' AND delta BETWEEN -1000000000 AND 1000000000),reason TEXT NOT NULL,created_unix INTEGER NOT NULL,FOREIGN KEY(store_id,customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id))",
	"CREATE TABLE telegram_commerce_payment_refunds(order_id TEXT PRIMARY KEY REFERENCES telegram_commerce_orders(order_id),bot_id INTEGER NOT NULL,charge_id TEXT NOT NULL,created_unix INTEGER NOT NULL,FOREIGN KEY(bot_id,charge_id) REFERENCES telegram_commerce_payments(bot_id,charge_id))",
	"CREATE TABLE telegram_commerce_order_deliveries(order_id TEXT PRIMARY KEY REFERENCES telegram_commerce_orders(order_id),store_id TEXT NOT NULL,customer_id TEXT NOT NULL,client_id TEXT NOT NULL REFERENCES clients(id),content_sha256 TEXT NOT NULL CHECK(length(content_sha256)=64),client_terms_sha256 TEXT NOT NULL CHECK(length(client_terms_sha256)=64),telegram_message_id INTEGER NOT NULL CHECK(telegram_message_id>0),generation INTEGER NOT NULL CHECK(generation>0),valid_until_unix_nano INTEGER NOT NULL,delivered_unix INTEGER NOT NULL,FOREIGN KEY(store_id,customer_id) REFERENCES telegram_commerce_customers(store_id,customer_id))",
	"CREATE INDEX telegram_commerce_customer_orders ON telegram_commerce_orders(store_id,customer_id,created_unix,order_id)",
	"CREATE INDEX telegram_commerce_customer_services ON telegram_commerce_customer_clients(store_id,customer_id,created_unix,client_id)",
	"CREATE INDEX telegram_commerce_wallet_customer ON telegram_commerce_wallet_ledger(store_id,customer_id,bucket,created_unix)",
	"CREATE TRIGGER telegram_commerce_orders_immutable_terms BEFORE UPDATE OF order_id,event_id,store_id,customer_id,telegram_user_id,product_id,client_id,action,currency,amount_stars,product_json,intent_json,fingerprint,expires_unix,created_unix ON telegram_commerce_orders BEGIN SELECT RAISE(ABORT,'telegram order terms immutable'); END",
}

func init() {
	for _, table := range []string{"events", "payments", "order_receipts", "customer_clients", "wallet_ledger", "payment_refunds", "order_deliveries", "trials"} {
		for _, operation := range []string{"UPDATE", "DELETE"} {
			telegramCommerceDDL = append(telegramCommerceDDL, "CREATE TRIGGER telegram_commerce_"+table+"_no_"+strings.ToLower(operation)+" BEFORE "+operation+" ON telegram_commerce_"+table+" BEGIN SELECT RAISE(ABORT,'telegram commerce history immutable'); END")
		}
	}
}

// EnableTelegramCommerce is explicit opt-in. Opening an old database never
// creates these tables or starts a receiver. Usage must be configured first.
func (s *Store) EnableTelegramCommerce(ctx context.Context) error {
	if ctx == nil || s == nil || s.db == nil || s.usage == nil {
		return ErrUnavailable
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	present, err := telegramCommerceSchemaState(ctx, tx)
	if err != nil {
		return err
	}
	if present {
		return tx.Commit()
	}
	for _, statement := range telegramCommerceDDL {
		if _, err = tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_meta(id,version) VALUES(1,1)"); err != nil {
		return err
	}
	return tx.Commit()
}

func telegramCommerceSchemaState(ctx context.Context, q sellerQueryV2) (bool, error) {
	var count int
	if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE name GLOB 'telegram_commerce_*'").Scan(&count); err != nil {
		return false, ErrUnavailable
	}
	if count == 0 {
		return false, nil
	}
	if count != len(telegramCommerceDDL) {
		return false, ErrUnavailable
	}
	var version int
	if q.QueryRowContext(ctx, "SELECT version FROM telegram_commerce_meta WHERE id=1").Scan(&version) != nil || version != 1 {
		return false, ErrUnavailable
	}
	for _, statement := range telegramCommerceDDL {
		parts := strings.Fields(statement)
		name := strings.SplitN(parts[2], "(", 2)[0]
		var actual string
		if q.QueryRowContext(ctx, "SELECT sql FROM sqlite_master WHERE name=?", name).Scan(&actual) != nil ||
			strings.Join(strings.Fields(actual), " ") != strings.Join(parts, " ") {
			return false, ErrUnavailable
		}
	}
	return true, nil
}

func telegramCommerceReady(ctx context.Context, q sellerQueryV2) error {
	present, err := telegramCommerceSchemaState(ctx, q)
	if err != nil || !present {
		return ErrUnavailable
	}
	return nil
}

// VerifyTelegramCommerceSchema lets maintenance validate this optional family
// without enabling a receiver or modifying an existing snapshot.
func (s *Store) VerifyTelegramCommerceSchema(ctx context.Context) error {
	return telegramCommerceReady(ctx, s.db)
}

var _ sellerQueryV2 = (*sql.DB)(nil)
