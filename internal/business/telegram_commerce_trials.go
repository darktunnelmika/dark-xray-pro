package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func telegramTrialPolicyTx(ctx context.Context, tx *sql.Tx, storeID string) (TelegramTrialPolicy, error) {
	out := TelegramTrialPolicy{StoreID: storeID, DurationHours: 24, StartsAt: "issuance"}
	err := tx.QueryRowContext(ctx, "SELECT enabled,volume_bytes,revision FROM telegram_commerce_trial_policy WHERE store_id=?", storeID).Scan(&out.Enabled, &out.VolumeBytes, &out.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	return out, err
}

func (s *Store) GetTelegramTrialPolicy(ctx context.Context, a TelegramActor) (TelegramTrialPolicy, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramTrialPolicy{}, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramTrialPolicy{}, err
	}
	return telegramTrialPolicyTx(ctx, tx, access.Store.ID)
}

func (s *Store) SetTelegramTrialPolicy(ctx context.Context, a TelegramActor, in TelegramTrialPolicyChange) (TelegramTrialPolicy, error) {
	var out TelegramTrialPolicy
	if !eventRE.MatchString(in.EventID) || in.ExpectedRevision < 0 || in.VolumeBytes <= 0 || in.VolumeBytes > MaxCatalogVolume {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "manage_store")
	if err != nil {
		return out, err
	}
	hash := fingerprint("telegram-trial-policy", struct {
		Actor   TelegramActor
		Request TelegramTrialPolicyChange
	}{a, in})
	if replay, e := telegramEventReplayTx(ctx, tx, in.EventID, hash, "trial_policy", &out); e != nil || replay {
		return out, e
	}
	prior, err := telegramTrialPolicyTx(ctx, tx, access.Store.ID)
	if err != nil {
		return out, err
	}
	if prior.Revision != in.ExpectedRevision {
		return out, ErrConflict
	}
	out = TelegramTrialPolicy{StoreID: access.Store.ID, Enabled: in.Enabled, VolumeBytes: in.VolumeBytes, Revision: prior.Revision + 1, DurationHours: 24, StartsAt: "issuance"}
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_trial_policy(store_id,enabled,volume_bytes,revision) VALUES(?,?,?,?) ON CONFLICT(store_id) DO UPDATE SET enabled=excluded.enabled,volume_bytes=excluded.volume_bytes,revision=excluded.revision", out.StoreID, out.Enabled, out.VolumeBytes, out.Revision); err != nil {
		return TelegramTrialPolicy{}, err
	}
	if err = appendTelegramEventTx(ctx, tx, in.EventID, hash, access.Store.ID, a.UserID, "trial_policy", out); err != nil {
		return TelegramTrialPolicy{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramTrialPolicy{}, err
	}
	return out, nil
}

// ClaimTelegramTrial grants at most one native volume identity per store and
// customer, with native credit debit in the same transaction. Its 24 hours start
// at issuance; first-use timing is deliberately not claimed.
func (s *Store) ClaimTelegramTrial(ctx context.Context, a TelegramActor, eventID string) (TelegramService, error) {
	var out TelegramService
	if !eventRE.MatchString(eventID) {
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
	if err = telegramSalesAllowedTx(ctx, tx, access, "create"); err != nil {
		return out, err
	}
	policy, err := telegramTrialPolicyTx(ctx, tx, access.Store.ID)
	if err != nil {
		return out, err
	}
	if !policy.Enabled {
		return out, ErrTelegramDenied
	}
	var existing, priorEvent string
	err = tx.QueryRowContext(ctx, "SELECT client_id,event_id FROM telegram_commerce_trials WHERE store_id=? AND customer_id=?", access.Store.ID, access.CustomerID).Scan(&existing, &priorEvent)
	if err == nil {
		if err = telegramCustomerOwnsClientTx(ctx, tx, access, existing); err != nil {
			return out, err
		}
		client, e := readClient(ctx, tx, existing)
		if e != nil {
			return out, e
		}
		state := client.DeploymentState
		if client.Archived {
			state = "archived"
		} else if client.ExpiresUnix <= time.Now().UTC().Unix() {
			state = "expired"
		}
		return TelegramService{Client: client, State: state}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	nonce, err := telegramNonce()
	if err != nil {
		return out, err
	}
	client, err := s.createClientTx(ctx, tx, NewClient{EventID: eventID, ClientID: "tg-trial-" + nonce[:32], RepresentativeID: access.Store.RepresentativeID, Name: "Trial-" + nonce[:6], DurationDays: 1, IPCount: 1, VolumeBytes: policy.VolumeBytes}, access.Store.RepresentativeID)
	if err != nil {
		return out, err
	}
	out = TelegramService{Client: client, State: "pending_node_apply"}
	raw, err := json.Marshal(out)
	if err != nil {
		return TelegramService{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_trials(store_id,customer_id,client_id,event_id,receipt_json,created_unix) VALUES(?,?,?,?,?,?)", access.Store.ID, access.CustomerID, client.ID, eventID, raw, client.CreatedUnix); err != nil {
		return TelegramService{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_customer_clients(store_id,customer_id,client_id,created_unix) VALUES(?,?,?,?)", access.Store.ID, access.CustomerID, client.ID, client.CreatedUnix); err != nil {
		return TelegramService{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramService{}, err
	}
	return out, nil
}
