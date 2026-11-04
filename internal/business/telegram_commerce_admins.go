package business

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func (s *Store) InviteTelegramAdmin(ctx context.Context, a TelegramActor, in TelegramAdminInvitationRequest) (TelegramAdminInvitation, error) {
	var out TelegramAdminInvitation
	permissions, err := telegramPermissions(in.Permissions, true)
	if err != nil || len(permissions) == 0 || !eventRE.MatchString(in.EventID) || in.UserID <= 0 || in.UserID == a.UserID {
		return out, ErrInvalid
	}
	in.Permissions = permissions
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "manage_admins")
	if err != nil {
		return out, err
	}
	for _, p := range permissions {
		if !telegramHasPermission(access.Permissions, p) {
			return out, ErrTelegramDenied
		}
	}
	if in.UserID == access.Store.ManagerUserID {
		return out, ErrTelegramDenied
	}
	hash := fingerprint("telegram-invite-admin", struct {
		Actor   TelegramActor
		Request TelegramAdminInvitationRequest
	}{a, in})
	if replay, e := telegramEventReplayTx(ctx, tx, in.EventID, hash, "invite_admin", &out); e != nil || replay {
		return out, e
	}
	id, err := telegramNonce()
	if err != nil {
		return out, err
	}
	raw, _ := json.Marshal(permissions)
	out = TelegramAdminInvitation{ID: id, StoreID: access.Store.ID, UserID: in.UserID, Permissions: permissions, ExpiresUnix: time.Now().UTC().Add(24 * time.Hour).Unix()}
	// The opaque invitation reference is not a credential: only its exact
	// authenticated Telegram addressee may accept it in this exact bot.
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_invites(invite_id,store_id,invited_user_id,permissions_json,created_by,expires_unix) VALUES(?,?,?,?,?,?)", id, access.Store.ID, in.UserID, string(raw), a.UserID, out.ExpiresUnix); err != nil {
		return TelegramAdminInvitation{}, err
	}
	if err = appendTelegramEventTx(ctx, tx, in.EventID, hash, access.Store.ID, a.UserID, "invite_admin", out); err != nil {
		return TelegramAdminInvitation{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramAdminInvitation{}, err
	}
	return out, nil
}

func (s *Store) AcceptTelegramAdmin(ctx context.Context, a TelegramActor, invitationID string) (TelegramAccess, error) {
	if len(invitationID) != 48 {
		return TelegramAccess{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TelegramAccess{}, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramAccess{}, err
	}
	var store, raw string
	var target, creator, expiry, accepted int64
	var revoked bool
	err = tx.QueryRowContext(ctx, "SELECT store_id,invited_user_id,permissions_json,created_by,expires_unix,accepted_unix,revoked FROM telegram_commerce_invites WHERE invite_id=?", invitationID).Scan(&store, &target, &raw, &creator, &expiry, &accepted, &revoked)
	if err != nil || store != access.Store.ID || target != a.UserID || revoked {
		return TelegramAccess{}, ErrTelegramDenied
	}
	if accepted > 0 {
		if access.Role != "admin" {
			return TelegramAccess{}, ErrTelegramDenied
		}
		return access, nil
	}
	if expiry <= time.Now().UTC().Unix() {
		return TelegramAccess{}, ErrTelegramDenied
	}
	creatorAccess, err := authorizeTelegramTx(ctx, tx, TelegramActor{BotID: a.BotID, UserID: creator, ChatID: creator}, "manage_admins")
	if err != nil {
		return TelegramAccess{}, ErrTelegramDenied
	}
	var permissions []string
	if json.Unmarshal([]byte(raw), &permissions) != nil {
		return TelegramAccess{}, ErrUnavailable
	}
	if permissions, err = telegramPermissions(permissions, true); err != nil {
		return TelegramAccess{}, ErrUnavailable
	}
	for _, p := range permissions {
		if !telegramHasPermission(creatorAccess.Permissions, p) {
			return TelegramAccess{}, ErrTelegramDenied
		}
	}
	var existingRole string
	err = tx.QueryRowContext(ctx, "SELECT role FROM telegram_commerce_members WHERE store_id=? AND telegram_user_id=?", store, target).Scan(&existingRole)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TelegramAccess{}, err
	}
	if err == nil && existingRole != "admin" {
		return TelegramAccess{}, ErrTelegramDenied
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_members(store_id,telegram_user_id,role,permissions_json,enabled,revision) VALUES(?,?,'admin',?,1,1) ON CONFLICT(store_id,telegram_user_id) DO UPDATE SET permissions_json=excluded.permissions_json,enabled=1,revision=telegram_commerce_members.revision+1", store, target, raw); err != nil {
		return TelegramAccess{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE telegram_commerce_invites SET accepted_unix=? WHERE invite_id=? AND accepted_unix=0", time.Now().UTC().Unix(), invitationID); err != nil {
		return TelegramAccess{}, err
	}
	access, err = authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramAccess{}, err
	}
	event := "tg-admin-accept-" + invitationID
	hash := fingerprint("telegram-admin-accepted", struct {
		Actor      TelegramActor
		Invitation string
	}{a, invitationID})
	if err = appendTelegramEventTx(ctx, tx, event, hash, store, a.UserID, "accept_admin", access); err != nil {
		return TelegramAccess{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramAccess{}, err
	}
	return access, nil
}

func (s *Store) ListTelegramAdmins(ctx context.Context, a TelegramActor) ([]TelegramAdmin, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "manage_admins")
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT telegram_user_id,role,permissions_json,enabled,revision FROM telegram_commerce_members WHERE store_id=? ORDER BY telegram_user_id LIMIT 201", access.Store.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TelegramAdmin{}
	for rows.Next() {
		var member TelegramAdmin
		var raw string
		if err = rows.Scan(&member.UserID, &member.Role, &raw, &member.Enabled, &member.Revision); err != nil {
			return nil, err
		}
		if json.Unmarshal([]byte(raw), &member.Permissions) != nil {
			return nil, ErrUnavailable
		}
		out = append(out, member)
	}
	if len(out) > 200 {
		return nil, ErrUnavailable
	}
	return out, rows.Err()
}

func (s *Store) UpdateTelegramAdmin(ctx context.Context, a TelegramActor, in TelegramAdminChange) (TelegramAdmin, error) {
	var out TelegramAdmin
	permissions, err := telegramPermissions(in.Permissions, true)
	if err != nil || !eventRE.MatchString(in.EventID) || in.UserID <= 0 || in.ExpectedRevision < 1 {
		return out, ErrInvalid
	}
	in.Permissions = permissions
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	access, err := authorizeTelegramTx(ctx, tx, a, "manage_admins")
	if err != nil {
		return out, err
	}
	if in.UserID == access.Store.ManagerUserID || in.UserID == a.UserID {
		return out, ErrTelegramDenied
	}
	for _, p := range permissions {
		if !telegramHasPermission(access.Permissions, p) {
			return out, ErrTelegramDenied
		}
	}
	hash := fingerprint("telegram-change-admin", struct {
		Actor   TelegramActor
		Request TelegramAdminChange
	}{a, in})
	if replay, e := telegramEventReplayTx(ctx, tx, in.EventID, hash, "update_admin", &out); e != nil || replay {
		return out, e
	}
	var role string
	var revision int64
	err = tx.QueryRowContext(ctx, "SELECT role,revision FROM telegram_commerce_members WHERE store_id=? AND telegram_user_id=?", access.Store.ID, in.UserID).Scan(&role, &revision)
	if err != nil || role != "admin" {
		return out, ErrTelegramDenied
	}
	if revision != in.ExpectedRevision {
		return out, ErrConflict
	}
	raw, _ := json.Marshal(permissions)
	if _, err = tx.ExecContext(ctx, "UPDATE telegram_commerce_members SET permissions_json=?,enabled=?,revision=revision+1 WHERE store_id=? AND telegram_user_id=? AND role='admin'", string(raw), in.Enabled, access.Store.ID, in.UserID); err != nil {
		return out, err
	}
	// Previously issued invitations cannot silently undo revocation or a newly
	// narrowed role on a later tap.
	if _, err = tx.ExecContext(ctx, "UPDATE telegram_commerce_invites SET revoked=1 WHERE store_id=? AND invited_user_id=? AND accepted_unix=0", access.Store.ID, in.UserID); err != nil {
		return out, err
	}
	out = TelegramAdmin{UserID: in.UserID, Role: "admin", Permissions: permissions, Enabled: in.Enabled, Revision: revision + 1}
	if err = appendTelegramEventTx(ctx, tx, in.EventID, hash, access.Store.ID, a.UserID, "update_admin", out); err != nil {
		return TelegramAdmin{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramAdmin{}, err
	}
	return out, nil
}
