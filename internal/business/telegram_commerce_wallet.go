package business

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// appendTelegramWalletTx records retail store credit, never representative GB
// or user-month credit. The ledger is the balance authority; no cached wallet
// balance is independently updated.
func appendTelegramWalletTx(ctx context.Context, tx *sql.Tx, eventID, storeID, customerID, bucket, currency string, delta int64, reason string) error {
	return appendTelegramWalletEntryTx(ctx, tx, eventID, storeID, customerID, bucket, currency, delta, reason, false)
}

// A verified promotion reversal may leave bonus debt when previously earned
// credit was spent. New earnings offset that debt; paid credit is untouched.
func appendTelegramBonusReversalTx(ctx context.Context, tx *sql.Tx, eventID, storeID, customerID string, stars int64, reason string) error {
	if stars <= 0 {
		return ErrInvalid
	}
	return appendTelegramWalletEntryTx(ctx, tx, eventID, storeID, customerID, "bonus", "XTR", -stars, reason, true)
}

func appendTelegramWalletEntryTx(ctx context.Context, tx *sql.Tx, eventID, storeID, customerID, bucket, currency string, delta int64, reason string, allowBonusDebt bool) error {
	if !eventRE.MatchString(eventID) || !idRE.MatchString(storeID) || !idRE.MatchString(customerID) || (bucket != "paid" && bucket != "bonus") || currency != "XTR" || delta == 0 || delta < -1_000_000_000 || delta > 1_000_000_000 || !validName(reason, 200) || allowBonusDebt && (bucket != "bonus" || delta >= 0) {
		return ErrInvalid
	}
	intent := struct {
		Store, Customer, Bucket, Currency, Reason string
		Delta                                     int64
		Reversal                                  bool
	}{storeID, customerID, bucket, currency, reason, delta, allowBonusDebt}
	hash := fingerprint("telegram-wallet", intent)
	var prior string
	err := tx.QueryRowContext(ctx, "SELECT fingerprint FROM telegram_commerce_wallet_ledger WHERE event_id=?", eventID).Scan(&prior)
	if err == nil {
		if prior != hash {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	var count int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM telegram_commerce_customers WHERE store_id=? AND customer_id=?", storeID, customerID).Scan(&count) != nil || count != 1 {
		return ErrTelegramDenied
	}
	var balance int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(delta),0) FROM telegram_commerce_wallet_ledger WHERE store_id=? AND customer_id=? AND bucket=? AND currency=?", storeID, customerID, bucket, currency).Scan(&balance); err != nil {
		return ErrUnavailable
	}
	const limit int64 = 1_000_000_000_000_000
	ok := balance >= -limit && balance <= limit &&
		!(delta > 0 && balance > limit-delta) && !(delta < 0 && balance < -limit-delta)
	if !ok {
		return ErrUnavailable
	}
	next := balance + delta
	// A positive earning may reduce an existing bonus debt without erasing it.
	if !ok || next < 0 && (!allowBonusDebt && (bucket != "bonus" || delta < 0)) {
		return ErrInsufficient
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_wallet_ledger(event_id,fingerprint,store_id,customer_id,bucket,currency,delta,reason,created_unix) VALUES(?,?,?,?,?,?,?,?,?)", eventID, hash, storeID, customerID, bucket, currency, delta, reason, time.Now().UTC().Unix())
	return err
}

func (s *Store) TelegramWallet(ctx context.Context, a TelegramActor) (TelegramWallet, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramWallet{}, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramWallet{}, err
	}
	out := TelegramWallet{Currency: "XTR"}
	err = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(CASE WHEN bucket='paid' THEN delta ELSE 0 END),0),COALESCE(SUM(CASE WHEN bucket='bonus' THEN delta ELSE 0 END),0) FROM telegram_commerce_wallet_ledger WHERE store_id=? AND customer_id=? AND currency='XTR'", access.Store.ID, access.CustomerID).Scan(&out.PaidStars, &out.BonusStars)
	if err != nil || out.PaidStars < 0 {
		return TelegramWallet{}, ErrUnavailable
	}
	if out.BonusStars > 0 {
		out.AvailableBonusStars = out.BonusStars
	}
	return out, nil
}
