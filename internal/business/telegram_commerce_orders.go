package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type telegramCheckoutIntent struct {
	Actor              TelegramActor
	Request            TelegramCheckoutRequest
	StoreID            string
	CustomerID         string
	Product            TelegramProduct
	ClientID           string
	ExpectedClientHash string
}

func telegramProductTx(ctx context.Context, tx *sql.Tx, store TelegramStore, id string) (TelegramProduct, error) {
	var out TelegramProduct
	product, err := readCatalog(ctx, tx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	if product.RepresentativeID != store.RepresentativeID || product.Status != "published" {
		return out, ErrTelegramDenied
	}
	out.Product = product
	err = tx.QueryRowContext(ctx, "SELECT amount_stars,revision FROM telegram_commerce_prices WHERE store_id=? AND product_id=?", store.ID, id).Scan(&out.PriceStars, &out.PriceRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return TelegramProduct{}, ErrNotFound
	}
	return out, err
}

func (s *Store) SetTelegramProductPrice(ctx context.Context, a TelegramActor, in TelegramProductPrice) (TelegramProduct, error) {
	var out TelegramProduct
	if !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.ProductID) || in.PriceStars <= 0 || in.PriceStars > 1_000_000_000 || in.ExpectedRevision < 0 {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "manage_products")
	if err != nil {
		return out, err
	}
	hash := fingerprint("telegram-product-price", struct {
		Actor   TelegramActor
		Request TelegramProductPrice
	}{a, in})
	if replay, e := telegramEventReplayTx(ctx, tx, in.EventID, hash, "product_price", &out); e != nil || replay {
		return out, e
	}
	product, err := readCatalog(ctx, tx, in.ProductID)
	if err != nil {
		return out, err
	}
	if product.RepresentativeID != access.Store.RepresentativeID || product.Status == "archived" {
		return out, ErrTelegramDenied
	}
	if access.Store.RepresentativeID != "owner" {
		policy, e := catalogPolicyTx(ctx, tx, access.Store.RepresentativeID)
		if e != nil {
			return out, e
		}
		if !policyPermitsProduct(policy, product.CatalogSpec, product.Status == "published") {
			return out, ErrTelegramDenied
		}
	}
	var revision int64
	err = tx.QueryRowContext(ctx, "SELECT revision FROM telegram_commerce_prices WHERE store_id=? AND product_id=?", access.Store.ID, in.ProductID).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if revision != in.ExpectedRevision {
		return out, ErrConflict
	}
	out = TelegramProduct{Product: product, PriceStars: in.PriceStars, PriceRevision: revision + 1}
	_, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_prices(store_id,product_id,amount_stars,revision) VALUES(?,?,?,?) ON CONFLICT(store_id,product_id) DO UPDATE SET amount_stars=excluded.amount_stars,revision=excluded.revision", access.Store.ID, in.ProductID, in.PriceStars, out.PriceRevision)
	if err != nil {
		return TelegramProduct{}, err
	}
	if err = appendTelegramEventTx(ctx, tx, in.EventID, hash, access.Store.ID, a.UserID, "product_price", out); err != nil {
		return TelegramProduct{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramProduct{}, err
	}
	return out, nil
}

func (s *Store) ListTelegramProducts(ctx context.Context, a TelegramActor) ([]TelegramProduct, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT p.product_id FROM telegram_commerce_prices p JOIN catalog_products c ON c.id=p.product_id WHERE p.store_id=? AND c.representative_id=? AND c.status='published' ORDER BY c.created_unix DESC,c.id LIMIT 201", access.Store.ID, access.Store.RepresentativeID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 200 {
		return nil, ErrUnavailable
	}
	out := make([]TelegramProduct, 0, len(ids))
	for _, id := range ids {
		p, e := telegramProductTx(ctx, tx, access.Store, id)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, nil
}

// ListTelegramManagedProducts includes published native products without a
// Stars price so an authorized manager can configure a new shop. Those entries
// are never returned by the customer storefront or accepted at checkout.
func (s *Store) ListTelegramManagedProducts(ctx context.Context, a TelegramActor) ([]TelegramProduct, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "manage_products")
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT id FROM catalog_products WHERE representative_id=? AND status='published' ORDER BY created_unix DESC,id LIMIT 201", access.Store.RepresentativeID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) != nil {
			rows.Close()
			return nil, ErrUnavailable
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 200 {
		return nil, ErrUnavailable
	}
	out := make([]TelegramProduct, 0, len(ids))
	for _, id := range ids {
		p, err := readCatalog(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		item := TelegramProduct{Product: p}
		err = tx.QueryRowContext(ctx, "SELECT amount_stars,revision FROM telegram_commerce_prices WHERE store_id=? AND product_id=?", access.Store.ID, id).Scan(&item.PriceStars, &item.PriceRevision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Store) ListTelegramServices(ctx context.Context, a TelegramActor) ([]TelegramService, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT b.client_id FROM telegram_commerce_customer_clients b JOIN clients c ON c.id=b.client_id WHERE b.store_id=? AND b.customer_id=? AND c.representative_id=? ORDER BY b.created_unix DESC,b.client_id LIMIT 201", access.Store.ID, access.CustomerID, access.Store.RepresentativeID)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) > 200 {
		return nil, ErrUnavailable
	}
	out := make([]TelegramService, 0, len(ids))
	for _, id := range ids {
		client, e := readClient(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		state := client.DeploymentState
		if client.Archived {
			state = "archived"
		} else if client.ExpiresUnix <= time.Now().UTC().Unix() {
			state = "expired"
		}
		out = append(out, TelegramService{Client: client, State: state})
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	for i := range out {
		usage, e := s.ClientUsage(ctx, out[i].Client.ID)
		if e != nil {
			continue
		}
		if usage.RepresentativeID != access.Store.RepresentativeID {
			return nil, ErrTelegramDenied
		}
		out[i].Usage = usage
		out[i].UsageAvailable = usage.Known
		if usage.Known && usage.Exhausted {
			out[i].State = "quota_exhausted"
		}
	}
	return out, nil
}

func telegramSalesAllowedTx(ctx context.Context, tx *sql.Tx, access TelegramAccess, action string) error {
	control, err := readRepresentativeControl(ctx, tx, access.Store.RepresentativeID)
	if err != nil {
		return err
	}
	if !control.Representative.Enabled || control.LoginSuspended {
		return ErrTelegramDenied
	}
	if control.Representative.Kind == "owner" {
		return nil
	}
	if action == "create" && !control.Permissions.CreateClients || action == "renew" && !control.Permissions.RenewClients {
		return ErrTelegramDenied
	}
	return nil
}

func (s *Store) PrepareTelegramCheckout(ctx context.Context, a TelegramActor, in TelegramCheckoutRequest) (TelegramOrder, error) {
	var out TelegramOrder
	if !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.ProductID) || in.ClientID != "" && !idRE.MatchString(in.ClientID) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return out, err
	}
	action := "create"
	if in.ClientID != "" {
		action = "renew"
	}
	if err = telegramSalesAllowedTx(ctx, tx, access, action); err != nil {
		return out, err
	}
	var priorID string
	err = tx.QueryRowContext(ctx, "SELECT order_id FROM telegram_commerce_orders WHERE event_id=?", in.EventID).Scan(&priorID)
	if err == nil {
		prior, intent, e := readTelegramOrderTx(ctx, tx, priorID)
		if e != nil {
			return out, e
		}
		if intent.Actor != a || intent.Request != in || prior.StoreID != access.Store.ID || prior.CustomerID != access.CustomerID {
			return out, ErrConflict
		}
		prior.Replayed = true
		return prior, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	product, err := telegramProductTx(ctx, tx, access.Store, in.ProductID)
	if err != nil {
		return out, err
	}
	id, err := telegramNonce()
	if err != nil {
		return out, err
	}
	clientID := "tg-" + id[:32]
	intent := telegramCheckoutIntent{Actor: a, Request: in, StoreID: access.Store.ID, CustomerID: access.CustomerID, Product: product, ClientID: clientID}
	if action == "renew" {
		if err = telegramCustomerOwnsClientTx(ctx, tx, access, in.ClientID); err != nil {
			return out, err
		}
		client, e := readClient(ctx, tx, in.ClientID)
		if e != nil {
			return out, e
		}
		if client.Archived || client.PlanKind != product.Product.PlanKind || client.IPCount != product.Product.IPCount {
			return out, ErrConflict
		}
		if _, e = calculateBulkExtension(client, product.Product.DurationDays, product.Product.VolumeBytes, time.Now().UTC().Unix()); e != nil {
			return out, e
		}
		clientID = in.ClientID
		intent.ClientID = clientID
		intent.ExpectedClientHash = fingerprint("telegram-renewal-client", client)
	}
	now := time.Now().UTC().Unix()
	out = TelegramOrder{ID: id, StoreID: access.Store.ID, CustomerID: access.CustomerID, BotID: a.BotID, UserID: a.UserID, ProductID: in.ProductID, ClientID: clientID, Action: action, Currency: "XTR", AmountStars: product.PriceStars, Status: "quoted", Product: product.Product, ExpiresUnix: now + 900, CreatedUnix: now}
	terms, _ := json.Marshal(out.Product)
	raw, _ := json.Marshal(intent)
	_, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_orders(order_id,event_id,store_id,customer_id,telegram_user_id,product_id,client_id,action,currency,amount_stars,status,product_json,intent_json,fingerprint,expires_unix,created_unix) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", id, in.EventID, out.StoreID, out.CustomerID, a.UserID, out.ProductID, clientID, action, "XTR", out.AmountStars, "quoted", terms, raw, fingerprint("telegram-checkout", intent), out.ExpiresUnix, now)
	if err != nil {
		return TelegramOrder{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramOrder{}, err
	}
	return out, nil
}

func readTelegramOrderTx(ctx context.Context, tx *sql.Tx, id string) (TelegramOrder, telegramCheckoutIntent, error) {
	var out TelegramOrder
	var intent telegramCheckoutIntent
	if len(id) != 48 {
		return out, intent, ErrInvalid
	}
	var terms, raw []byte
	var hash, event, representative string
	err := tx.QueryRowContext(ctx, "SELECT o.order_id,o.store_id,o.customer_id,s.bot_id,o.telegram_user_id,o.product_id,o.client_id,o.action,o.currency,o.amount_stars,o.status,o.product_json,o.intent_json,o.fingerprint,o.event_id,o.expires_unix,o.created_unix,s.representative_id FROM telegram_commerce_orders o JOIN telegram_commerce_stores s ON s.store_id=o.store_id WHERE o.order_id=?", id).Scan(&out.ID, &out.StoreID, &out.CustomerID, &out.BotID, &out.UserID, &out.ProductID, &out.ClientID, &out.Action, &out.Currency, &out.AmountStars, &out.Status, &terms, &raw, &hash, &event, &out.ExpiresUnix, &out.CreatedUnix, &representative)
	if errors.Is(err, sql.ErrNoRows) {
		return out, intent, ErrNotFound
	}
	if err != nil {
		return out, intent, err
	}
	if json.Unmarshal(terms, &out.Product) != nil || json.Unmarshal(raw, &intent) != nil || hash != fingerprint("telegram-checkout", intent) || intent.Request.EventID != event || intent.Actor.BotID != out.BotID || intent.Actor.UserID != out.UserID || intent.Actor.ChatID != out.UserID || intent.StoreID != out.StoreID || intent.CustomerID != out.CustomerID || intent.Product.Product != out.Product || intent.Product.PriceStars != out.AmountStars || out.Currency != "XTR" || out.Product.ID != out.ProductID || out.Product.RepresentativeID != representative || intent.ClientID != out.ClientID {
		return TelegramOrder{}, telegramCheckoutIntent{}, ErrUnavailable
	}
	action := "create"
	if intent.Request.ClientID != "" {
		action = "renew"
	}
	if action != out.Action || action == "renew" && intent.Request.ClientID != out.ClientID {
		return TelegramOrder{}, telegramCheckoutIntent{}, ErrUnavailable
	}
	return out, intent, nil
}

// ListTelegramOrders provides recovery for captured payments whose native
// fulfillment is still pending. Even managers see only their own customer
// orders here; these reads do not imply payment, activation or delivery.
func (s *Store) ListTelegramOrders(ctx context.Context, a TelegramActor, limit, offset int) ([]TelegramOrder, error) {
	if limit < 1 || limit > 200 || offset < 0 || offset > 1_000_000 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT order_id FROM telegram_commerce_orders WHERE store_id=? AND customer_id=? AND telegram_user_id=? ORDER BY created_unix DESC,order_id DESC LIMIT ? OFFSET ?", access.Store.ID, access.CustomerID, a.UserID, limit, offset)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, limit)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := make([]TelegramOrder, 0, len(ids))
	for _, id := range ids {
		order, e := readTelegramCustomerOrderTx(ctx, tx, access, id)
		if e != nil {
			return nil, e
		}
		out = append(out, order)
	}
	return out, nil
}

// TelegramOrderByID rechecks the current bot and customer binding. Possession
// of an opaque invoice payload never grants access to another customer's order.
func (s *Store) TelegramOrderByID(ctx context.Context, a TelegramActor, id string) (TelegramOrder, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramOrder{}, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramOrder{}, err
	}
	return readTelegramCustomerOrderTx(ctx, tx, access, id)
}

func readTelegramCustomerOrderTx(ctx context.Context, tx *sql.Tx, access TelegramAccess, id string) (TelegramOrder, error) {
	order, _, err := readTelegramOrderTx(ctx, tx, id)
	if err != nil {
		return TelegramOrder{}, err
	}
	if order.StoreID != access.Store.ID || order.BotID != access.Store.BotID || order.CustomerID != access.CustomerID || order.UserID != access.UserID {
		return TelegramOrder{}, ErrNotFound
	}
	if err = validateTelegramOrderDataTx(ctx, tx, id); err != nil {
		return TelegramOrder{}, err
	}
	return order, nil
}

func (s *Store) CheckTelegramPreCheckout(ctx context.Context, a TelegramActor, payload, currency string, amount int64) (TelegramOrder, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramOrder{}, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramOrder{}, err
	}
	order, intent, err := readTelegramOrderTx(ctx, tx, payload)
	if err != nil {
		return TelegramOrder{}, err
	}
	if order.StoreID != access.Store.ID || order.CustomerID != access.CustomerID || order.UserID != a.UserID || currency != "XTR" || amount != order.AmountStars || order.Status != "quoted" || order.ExpiresUnix <= time.Now().UTC().Unix() {
		return TelegramOrder{}, ErrTelegramPayment
	}
	if err = telegramSalesAllowedTx(ctx, tx, access, order.Action); err != nil {
		return TelegramOrder{}, err
	}
	if _, err = telegramProductTx(ctx, tx, access.Store, order.ProductID); err != nil {
		return TelegramOrder{}, err
	}
	rep, err := readRep(ctx, tx, access.Store.RepresentativeID)
	if err != nil {
		return TelegramOrder{}, err
	}
	bytes, units := order.Product.VolumeBytes, int64(0)
	if order.Product.PlanKind == "unlimited" {
		bytes = 0
		units = (order.Product.DurationDays*order.Product.IPCount + 29) / 30
	}
	if order.Action == "renew" {
		if err = telegramCustomerOwnsClientTx(ctx, tx, access, order.ClientID); err != nil {
			return TelegramOrder{}, err
		}
		client, e := readClient(ctx, tx, order.ClientID)
		if e != nil {
			return TelegramOrder{}, e
		}
		if fingerprint("telegram-renewal-client", client) != intent.ExpectedClientHash {
			return TelegramOrder{}, ErrConflict
		}
		change, e := calculateBulkExtension(client, order.Product.DurationDays, order.Product.VolumeBytes, time.Now().UTC().Unix())
		if e != nil {
			return TelegramOrder{}, e
		}
		bytes, units = change.VolumeCost, change.UnitsCost
	}
	if rep.Kind == "representative" && (rep.VolumeCreditBytes < bytes || rep.UnlimitedCreditUnits < units) {
		return TelegramOrder{}, ErrInsufficient
	}
	return order, nil
}

// RecordTelegramPayment captures a verified successful_payment independently of
// fulfillment. A late event for a now-disabled shop must remain reconciliable.
// Identity, amount and currency must still match the immutable invoice exactly.
func (s *Store) RecordTelegramPayment(ctx context.Context, a TelegramActor, in TelegramPayment) (TelegramPaymentReceipt, error) {
	var out TelegramPaymentReceipt
	if !validTelegramActor(a) || in.Currency != "XTR" || in.TotalAmount <= 0 || !validName(in.TelegramChargeID, 256) || strings.TrimSpace(in.TelegramChargeID) != in.TelegramChargeID {
		return out, ErrTelegramPayment
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return out, err
	}
	order, _, err := readTelegramOrderTx(ctx, tx, in.Payload)
	if err != nil {
		return out, err
	}
	if order.BotID != a.BotID || order.UserID != a.UserID || order.Currency != in.Currency || order.AmountStars != in.TotalAmount {
		return out, ErrTelegramPayment
	}
	var existingOrder, customer, currency string
	var amount, created int64
	err = tx.QueryRowContext(ctx, "SELECT order_id,customer_id,currency,amount_stars,created_unix FROM telegram_commerce_payments WHERE bot_id=? AND charge_id=?", a.BotID, in.TelegramChargeID).Scan(&existingOrder, &customer, &currency, &amount, &created)
	if err == nil {
		if existingOrder != order.ID || customer != order.CustomerID || currency != in.Currency || amount != in.TotalAmount {
			return out, ErrTelegramPayment
		}
		return TelegramPaymentReceipt{Order: order, TelegramChargeID: in.TelegramChargeID, CreatedUnix: created, Replayed: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	var paid int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM telegram_commerce_payments WHERE order_id=?", order.ID).Scan(&paid); err != nil {
		return out, err
	}
	if paid != 0 || order.Status != "quoted" {
		return out, ErrTelegramPayment
	}
	now := time.Now().UTC().Unix()
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_payments(bot_id,charge_id,order_id,customer_id,currency,amount_stars,created_unix) VALUES(?,?,?,?,?,?,?)", a.BotID, in.TelegramChargeID, order.ID, order.CustomerID, in.Currency, in.TotalAmount, now); err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE telegram_commerce_orders SET status='paid_pending_fulfillment' WHERE order_id=? AND status='quoted'", order.ID); err != nil {
		return out, err
	}
	order.Status = "paid_pending_fulfillment"
	out = TelegramPaymentReceipt{Order: order, TelegramChargeID: in.TelegramChargeID, CreatedUnix: now}
	if err = tx.Commit(); err != nil {
		return TelegramPaymentReceipt{}, err
	}
	return out, nil
}

// FulfillTelegramOrder shares the native create/renew transaction and records
// its immutable receipt in that transaction. It does not activate a runtime or
// call a subscription or Telegram transport.
func (s *Store) FulfillTelegramOrder(ctx context.Context, a TelegramActor, payload string) (TelegramOrderReceipt, error) {
	var out TelegramOrderReceipt
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return out, err
	}
	order, intent, err := readTelegramOrderTx(ctx, tx, payload)
	if err != nil {
		return out, err
	}
	if order.StoreID != access.Store.ID {
		return out, ErrTelegramDenied
	}
	if order.CustomerID != access.CustomerID && !telegramHasPermission(access.Permissions, "finance") {
		return out, ErrTelegramDenied
	}
	if err = telegramSalesAllowedTx(ctx, tx, access, order.Action); err != nil {
		return out, err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT receipt_json FROM telegram_commerce_order_receipts WHERE order_id=?", order.ID).Scan(&raw)
	if err == nil {
		if err = validateTelegramOrderDataTx(ctx, tx, order.ID); err != nil {
			return TelegramOrderReceipt{}, err
		}
		if json.Unmarshal(raw, &out) != nil || out.Order.ID != order.ID || out.Client.ID != order.ClientID || out.Client.RepresentativeID != access.Store.RepresentativeID {
			return TelegramOrderReceipt{}, ErrUnavailable
		}
		if order.Status == "refunded" {
			return TelegramOrderReceipt{}, ErrConflict
		}
		out.Replayed = true
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if order.Status != "paid_pending_fulfillment" {
		return out, ErrTelegramPayment
	}
	var amount int64
	var currency, customer string
	var bot int64
	err = tx.QueryRowContext(ctx, "SELECT bot_id,customer_id,currency,amount_stars FROM telegram_commerce_payments WHERE order_id=?", order.ID).Scan(&bot, &customer, &currency, &amount)
	if err != nil || bot != order.BotID || customer != order.CustomerID || currency != order.Currency || amount != order.AmountStars {
		return out, ErrTelegramPayment
	}
	if _, err = telegramProductTx(ctx, tx, access.Store, order.ProductID); err != nil {
		return out, err
	}
	event := "tg-order-" + order.ID
	var client Client
	if order.Action == "create" {
		name := []rune(order.Product.Name)
		if len(name) > 100 {
			name = name[:100]
		}
		client, err = s.createClientTx(ctx, tx, NewClient{EventID: event, ClientID: order.ClientID, RepresentativeID: access.Store.RepresentativeID, Name: string(name) + "-" + order.ID[:6], DurationDays: order.Product.DurationDays, IPCount: order.Product.IPCount, VolumeBytes: order.Product.VolumeBytes, Unlimited: order.Product.PlanKind == "unlimited"}, access.Store.RepresentativeID)
		if err != nil {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_customer_clients(store_id,customer_id,client_id,created_unix) VALUES(?,?,?,?)", order.StoreID, order.CustomerID, client.ID, time.Now().UTC().Unix()); err != nil {
			return out, err
		}
	} else {
		ownerAccess := access
		ownerAccess.CustomerID = order.CustomerID
		if err = telegramCustomerOwnsClientTx(ctx, tx, ownerAccess, order.ClientID); err != nil {
			return out, err
		}
		current, e := readClient(ctx, tx, order.ClientID)
		if e != nil {
			return out, e
		}
		if fingerprint("telegram-renewal-client", current) != intent.ExpectedClientHash {
			return out, ErrConflict
		}
		client, err = s.extendClientTx(ctx, tx, ExtendClient{EventID: event, ClientID: order.ClientID, AdditionalDays: order.Product.DurationDays, AdditionalVolumeBytes: order.Product.VolumeBytes, Reason: "Telegram paid renewal"}, access.Store.RepresentativeID)
		if err != nil {
			return out, err
		}
	}
	order.Status = "paid_pending_provisioning"
	out = TelegramOrderReceipt{Order: order, Client: client, CreatedUnix: time.Now().UTC().Unix()}
	raw, err = json.Marshal(out)
	if err != nil {
		return TelegramOrderReceipt{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_order_receipts(order_id,receipt_json,created_unix) VALUES(?,?,?)", order.ID, raw, out.CreatedUnix); err != nil {
		return TelegramOrderReceipt{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE telegram_commerce_orders SET status='paid_pending_provisioning' WHERE order_id=? AND status='paid_pending_fulfillment'", order.ID); err != nil {
		return TelegramOrderReceipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramOrderReceipt{}, err
	}
	return out, nil
}

// RecordTelegramRefund accepts only an authenticated refunded_payment update
// normalized by the Telegram transport. It records the monetary fact even if
// the shop is now suspended. It never invents a runtime stop or quota refund.
func (s *Store) RecordTelegramRefund(ctx context.Context, a TelegramActor, in TelegramPayment) (TelegramOrder, error) {
	var out TelegramOrder
	if !validTelegramActor(a) || in.Currency != "XTR" || in.TotalAmount <= 0 || !validName(in.TelegramChargeID, 256) {
		return out, ErrTelegramPayment
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return out, err
	}
	out, _, err = readTelegramOrderTx(ctx, tx, in.Payload)
	if err != nil {
		return TelegramOrder{}, err
	}
	if out.BotID != a.BotID || out.UserID != a.UserID || out.Currency != in.Currency || out.AmountStars != in.TotalAmount {
		return TelegramOrder{}, ErrTelegramPayment
	}
	var order, customer, currency string
	var amount int64
	err = tx.QueryRowContext(ctx, "SELECT order_id,customer_id,currency,amount_stars FROM telegram_commerce_payments WHERE bot_id=? AND charge_id=?", a.BotID, in.TelegramChargeID).Scan(&order, &customer, &currency, &amount)
	if errors.Is(err, sql.ErrNoRows) {
		// A verified refund may be processed while capture of its earlier
		// successful_payment is retrying. Keep that exact invoice retryable.
		// A different charge for an already-paid order is a real mismatch.
		var paidOrder string
		err = tx.QueryRowContext(ctx, "SELECT order_id FROM telegram_commerce_payments WHERE order_id=?", out.ID).Scan(&paidOrder)
		if errors.Is(err, sql.ErrNoRows) {
			return TelegramOrder{}, ErrUnavailable
		}
		if err != nil {
			return TelegramOrder{}, err
		}
		return TelegramOrder{}, ErrTelegramPayment
	}
	if err != nil {
		return TelegramOrder{}, err
	}
	if order != out.ID || customer != out.CustomerID || currency != in.Currency || amount != in.TotalAmount {
		return TelegramOrder{}, ErrTelegramPayment
	}
	var refundBot int64
	var refundCharge string
	err = tx.QueryRowContext(ctx, "SELECT bot_id,charge_id FROM telegram_commerce_payment_refunds WHERE order_id=?", out.ID).Scan(&refundBot, &refundCharge)
	if err == nil {
		if refundBot != a.BotID || refundCharge != in.TelegramChargeID || out.Status != "refunded" {
			return TelegramOrder{}, ErrUnavailable
		}
		out.Replayed = true
		return out, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return TelegramOrder{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_payment_refunds(order_id,bot_id,charge_id,created_unix) VALUES(?,?,?,?)", out.ID, a.BotID, in.TelegramChargeID, time.Now().UTC().Unix()); err != nil {
		return TelegramOrder{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE telegram_commerce_orders SET status='refunded' WHERE order_id=?", out.ID); err != nil {
		return TelegramOrder{}, err
	}
	if err = reconcileTelegramReferralRefundTx(ctx, tx, out.ID); err != nil {
		return TelegramOrder{}, err
	}
	out.Status = "refunded"
	if err = tx.Commit(); err != nil {
		return TelegramOrder{}, err
	}
	return out, nil
}
