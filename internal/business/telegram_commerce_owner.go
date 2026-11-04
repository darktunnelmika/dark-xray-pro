package business

import (
	"context"
	"database/sql"
)

func authorizeTelegramOwnerTx(ctx context.Context, tx *sql.Tx, a TelegramActor) (TelegramAccess, error) {
	access, err := authorizeTelegramTx(ctx, tx, a, "manage_store")
	if err != nil {
		return TelegramAccess{}, err
	}
	if access.Role != "owner" || access.Store.RepresentativeID != "owner" {
		return TelegramAccess{}, ErrTelegramDenied
	}
	return access, nil
}

func (s *Store) ListTelegramRepresentatives(ctx context.Context, a TelegramActor, limit, offset int) ([]Representative, error) {
	if limit < 1 || limit > 200 || offset < 0 || offset > 1_000_000 {
		return nil, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = authorizeTelegramOwnerTx(ctx, tx, a); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT "+repColumns+" FROM representatives WHERE kind='representative' ORDER BY created_unix DESC,id LIMIT ? OFFSET ?", limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Representative{}
	for rows.Next() {
		r, e := repScan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) CreateTelegramRepresentative(ctx context.Context, a TelegramActor, in TelegramRepresentativeCreate) (Representative, error) {
	var out Representative
	if !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.ID) || in.ID == "owner" || !validName(in.Name, 128) {
		return out, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramOwnerTx(ctx, tx, a)
	if err != nil {
		return out, err
	}
	hash := fingerprint("telegram-owner-representative", struct {
		Actor   TelegramActor
		Request TelegramRepresentativeCreate
	}{a, in})
	if replay, e := telegramEventReplayTx(ctx, tx, in.EventID, hash, "owner_create_representative", &out); e != nil || replay {
		return out, e
	}
	out, err = s.createRepresentativeTx(ctx, tx, in.ID, in.Name)
	if err != nil {
		return Representative{}, err
	}
	if err = appendTelegramEventTx(ctx, tx, in.EventID, hash, access.Store.ID, a.UserID, "owner_create_representative", out); err != nil {
		return Representative{}, err
	}
	if err = tx.Commit(); err != nil {
		return Representative{}, err
	}
	return out, nil
}

func (s *Store) AdjustTelegramRepresentativeCredits(ctx context.Context, a TelegramActor, in CreditChange) (Representative, error) {
	var out Representative
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramOwnerTx(ctx, tx, a)
	if err != nil {
		return out, err
	}
	hash := fingerprint("telegram-owner-credit", struct {
		Actor   TelegramActor
		Request CreditChange
	}{a, in})
	if replay, e := telegramEventReplayTx(ctx, tx, in.EventID, hash, "owner_adjust_credits", &out); e != nil || replay {
		return out, e
	}
	out, err = s.adjustCreditsTx(ctx, tx, in)
	if err != nil {
		return Representative{}, err
	}
	if err = appendTelegramEventTx(ctx, tx, in.EventID, hash, access.Store.ID, a.UserID, "owner_adjust_credits", out); err != nil {
		return Representative{}, err
	}
	if err = tx.Commit(); err != nil {
		return Representative{}, err
	}
	return out, nil
}
