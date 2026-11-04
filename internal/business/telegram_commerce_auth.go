package business

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

var telegramManagementPermissions = []string{
	"broadcast_draft", "broadcast_send", "create_clients", "finance",
	"manage_admins", "manage_products", "manage_store", "renew_clients", "support",
}

const telegramStoreColumns = "store_id,bot_id,representative_id,name,manager_user_id,enabled,revision"

func telegramNonce() (string, error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func validTelegramActor(a TelegramActor) bool {
	return a.BotID > 0 && a.UserID > 0 && a.ChatID == a.UserID
}

func scanTelegramStore(row interface{ Scan(...any) error }) (TelegramStore, error) {
	var out TelegramStore
	err := row.Scan(&out.ID, &out.BotID, &out.RepresentativeID, &out.Name, &out.ManagerUserID, &out.Enabled, &out.Revision)
	return out, err
}

func telegramStoreByBotTx(ctx context.Context, tx *sql.Tx, botID int64) (TelegramStore, error) {
	out, err := scanTelegramStore(tx.QueryRowContext(ctx, "SELECT "+telegramStoreColumns+" FROM telegram_commerce_stores WHERE bot_id=?", botID))
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrTelegramDenied
	}
	return out, err
}

func telegramPermissions(input []string, admin bool) ([]string, error) {
	if len(input) > len(telegramManagementPermissions) {
		return nil, ErrInvalid
	}
	out := append([]string{}, input...)
	sort.Strings(out)
	for i, p := range out {
		found := false
		for _, known := range telegramManagementPermissions {
			if p == known {
				found = true
				break
			}
		}
		if !found || i > 0 && out[i-1] == p || admin && (p == "manage_admins" || p == "manage_store") {
			return nil, ErrInvalid
		}
	}
	return out, nil
}

func telegramHasPermission(list []string, p string) bool {
	for _, v := range list {
		if v == p {
			return true
		}
	}
	return false
}

// authorizeTelegramTx re-reads current bot/store identity, parent suspension,
// membership and parent grants within the SAME transaction as protected work.
func authorizeTelegramTx(ctx context.Context, tx *sql.Tx, a TelegramActor, required string) (TelegramAccess, error) {
	var out TelegramAccess
	if !validTelegramActor(a) {
		return out, ErrTelegramDenied
	}
	if err := telegramCommerceReady(ctx, tx); err != nil {
		return out, err
	}
	store, err := telegramStoreByBotTx(ctx, tx, a.BotID)
	if err != nil {
		return out, err
	}
	if !store.Enabled {
		return out, ErrTelegramDenied
	}
	rep, err := readRep(ctx, tx, store.RepresentativeID)
	if err != nil || !rep.Enabled {
		return out, ErrTelegramDenied
	}
	control, err := readRepresentativeControl(ctx, tx, rep.ID)
	if err != nil || control.LoginSuspended {
		return out, ErrTelegramDenied
	}
	out = TelegramAccess{Store: store, UserID: a.UserID, Role: "customer", Permissions: []string{}}
	err = tx.QueryRowContext(ctx, "SELECT customer_id FROM telegram_commerce_customers WHERE store_id=? AND telegram_user_id=? AND private_chat_id=?", store.ID, a.UserID, a.ChatID).Scan(&out.CustomerID)
	if errors.Is(err, sql.ErrNoRows) {
		return TelegramAccess{}, ErrTelegramDenied
	}
	if err != nil {
		return TelegramAccess{}, ErrUnavailable
	}
	var role, raw string
	var enabled bool
	err = tx.QueryRowContext(ctx, "SELECT role,permissions_json,enabled FROM telegram_commerce_members WHERE store_id=? AND telegram_user_id=?", store.ID, a.UserID).Scan(&role, &raw, &enabled)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return TelegramAccess{}, ErrUnavailable
	}
	if err == nil && enabled {
		var permissions []string
		if json.Unmarshal([]byte(raw), &permissions) != nil {
			return TelegramAccess{}, ErrUnavailable
		}
		if role == "owner" || role == "representative" {
			if store.ManagerUserID != a.UserID || role != rep.Kind || role == "owner" && rep.ID != "owner" {
				return TelegramAccess{}, ErrTelegramDenied
			}
			permissions = append([]string{}, telegramManagementPermissions...)
		} else if role != "admin" {
			return TelegramAccess{}, ErrTelegramDenied
		}
		permissions, err = telegramPermissions(permissions, role == "admin")
		if err != nil {
			return TelegramAccess{}, ErrUnavailable
		}
		// Delegation never exceeds the representative's current native grants.
		if rep.Kind == "representative" {
			filtered := make([]string, 0, len(permissions))
			policy, policyErr := catalogPolicyTx(ctx, tx, rep.ID)
			if policyErr != nil {
				return TelegramAccess{}, ErrUnavailable
			}
			for _, p := range permissions {
				if p == "create_clients" && !control.Permissions.CreateClients || p == "renew_clients" && !control.Permissions.RenewClients || p == "manage_products" && !policy.ManageProducts || p == "finance" && !control.Permissions.ViewFinance {
					continue
				}
				filtered = append(filtered, p)
			}
			permissions = filtered
		}
		out.Role, out.Permissions = role, permissions
	}
	if required != "" && !telegramHasPermission(out.Permissions, required) {
		return TelegramAccess{}, ErrTelegramDenied
	}
	return out, nil
}

func (s *Store) ResolveTelegramActor(ctx context.Context, a TelegramActor) (TelegramAccess, error) {
	if ctx == nil || !validTelegramActor(a) {
		return TelegramAccess{}, ErrTelegramDenied
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return TelegramAccess{}, err
	}
	defer tx.Rollback()
	return authorizeTelegramTx(ctx, tx, a, "")
}

// TelegramStart registers a customer only after an authenticated private update
// for a configured, enabled bot. The same Telegram ID has separate identities
// and wallets in separate stores.
func (s *Store) TelegramStart(ctx context.Context, a TelegramActor) (TelegramAccess, error) {
	if ctx == nil || !validTelegramActor(a) {
		return TelegramAccess{}, ErrTelegramDenied
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TelegramAccess{}, err
	}
	defer tx.Rollback()
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return TelegramAccess{}, err
	}
	store, err := telegramStoreByBotTx(ctx, tx, a.BotID)
	if err != nil {
		return TelegramAccess{}, err
	}
	if !store.Enabled {
		return TelegramAccess{}, ErrTelegramDenied
	}
	hash := sha256.Sum256([]byte(store.ID + ":" + decimalTelegramUser(a.UserID)))
	customerID := "tg-" + hex.EncodeToString(hash[:16])
	if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO telegram_commerce_customers(customer_id,store_id,telegram_user_id,private_chat_id,created_unix) VALUES(?,?,?,?,?)", customerID, store.ID, a.UserID, a.ChatID, time.Now().UTC().Unix()); err != nil {
		return TelegramAccess{}, err
	}
	access, err := authorizeTelegramTx(ctx, tx, a, "")
	if err != nil {
		return TelegramAccess{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramAccess{}, err
	}
	return access, nil
}

func decimalTelegramUser(user int64) string {
	raw, _ := json.Marshal(user)
	return string(raw)
}

func telegramOwnerBrowserTx(ctx context.Context, tx *sql.Tx, token string) error {
	scope, err := telegramBrowserScopeTx(ctx, tx, token)
	if err != nil {
		return err
	}
	if scope != "" {
		return ErrTelegramDenied
	}
	return nil
}

func telegramBrowserScopeTx(ctx context.Context, tx *sql.Tx, token string) (string, error) {
	if len(token) != 64 {
		return "", ErrUnauthenticated
	}
	var id, role, parent string
	var disabled, enabled int
	var expires int64
	err := tx.QueryRowContext(ctx, "SELECT u.id,u.role,u.representative_id,CASE WHEN u.role='representative' AND c.representative_id IS NOT NULL THEN c.login_suspended ELSE u.disabled END,r.enabled,s.expires_unix FROM auth_sessions s JOIN auth_users u ON u.id=s.user_id JOIN representatives r ON r.id=u.representative_id LEFT JOIN representative_controls c ON c.representative_id=u.representative_id WHERE s.token_sha256=?", digestSession(token)).Scan(&id, &role, &parent, &disabled, &enabled, &expires)
	if err != nil || disabled != 0 || enabled != 1 || expires <= time.Now().UTC().Unix() {
		return "", ErrUnauthenticated
	}
	if id == "owner" && role == "owner" && parent == "owner" {
		return "", nil
	}
	if role != "representative" || parent == "owner" || !idRE.MatchString(parent) {
		return "", ErrUnauthenticated
	}
	return parent, nil
}

func telegramEventReplayTx(ctx context.Context, tx *sql.Tx, event, hash, action string, target any) (bool, error) {
	var prior, kind string
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT fingerprint,action,receipt_json FROM telegram_commerce_events WHERE event_id=?", event).Scan(&prior, &kind, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if prior != hash || kind != action {
		return false, ErrConflict
	}
	if target != nil && json.Unmarshal(raw, target) != nil {
		return false, ErrUnavailable
	}
	return true, nil
}

func appendTelegramEventTx(ctx context.Context, tx *sql.Tx, event, hash, store string, actor int64, action string, receipt any) error {
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_events(event_id,fingerprint,store_id,actor_user_id,action,receipt_json,created_unix) VALUES(?,?,?,?,?,?,?)", event, hash, store, actor, action, raw, time.Now().UTC().Unix())
	return err
}

func (s *Store) ConfigureTelegramStore(ctx context.Context, token string, in TelegramStoreRegistration) (TelegramStore, error) {
	in.Name = strings.TrimSpace(in.Name)
	if !eventRE.MatchString(in.EventID) || !idRE.MatchString(in.ID) || !idRE.MatchString(in.RepresentativeID) || !validName(in.Name, 128) || in.BotID <= 0 || in.ManagerUserID <= 0 || in.ExpectedRevision < 0 {
		return TelegramStore{}, ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TelegramStore{}, err
	}
	defer tx.Rollback()
	if err = telegramCommerceReady(ctx, tx); err != nil {
		return TelegramStore{}, err
	}
	if err = telegramOwnerBrowserTx(ctx, tx, token); err != nil {
		return TelegramStore{}, err
	}
	hash := fingerprint("telegram-owner-browser-store", in)
	var out TelegramStore
	if replay, e := telegramEventReplayTx(ctx, tx, in.EventID, hash, "owner_browser_configure_store", &out); e != nil || replay {
		return out, e
	}
	rep, err := readRep(ctx, tx, in.RepresentativeID)
	if err != nil {
		return out, err
	}
	if rep.Kind != "owner" && rep.Kind != "representative" || in.Enabled && !rep.Enabled {
		return out, ErrTelegramDenied
	}
	prior, err := scanTelegramStore(tx.QueryRowContext(ctx, "SELECT "+telegramStoreColumns+" FROM telegram_commerce_stores WHERE store_id=?", in.ID))
	if errors.Is(err, sql.ErrNoRows) {
		if in.ExpectedRevision != 0 {
			return out, ErrConflict
		}
		out = TelegramStore{ID: in.ID, BotID: in.BotID, RepresentativeID: in.RepresentativeID, Name: in.Name, ManagerUserID: in.ManagerUserID, Enabled: in.Enabled, Revision: 1}
		_, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_stores("+telegramStoreColumns+",created_unix) VALUES(?,?,?,?,?,?,?,?)", out.ID, out.BotID, out.RepresentativeID, out.Name, out.ManagerUserID, out.Enabled, out.Revision, time.Now().UTC().Unix())
	} else if err == nil {
		if prior.Revision != in.ExpectedRevision || prior.BotID != in.BotID || prior.RepresentativeID != in.RepresentativeID {
			return out, ErrConflict
		}
		out = prior
		out.Name = in.Name
		out.ManagerUserID = in.ManagerUserID
		out.Enabled = in.Enabled
		out.Revision++
		_, err = tx.ExecContext(ctx, "UPDATE telegram_commerce_stores SET name=?,manager_user_id=?,enabled=?,revision=? WHERE store_id=?", out.Name, out.ManagerUserID, out.Enabled, out.Revision, out.ID)
	}
	if err != nil {
		return TelegramStore{}, ErrConflict
	}
	if _, err = tx.ExecContext(ctx, "UPDATE telegram_commerce_members SET enabled=0,revision=revision+1 WHERE store_id=? AND role IN('owner','representative') AND telegram_user_id<>?", out.ID, out.ManagerUserID); err != nil {
		return TelegramStore{}, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO telegram_commerce_members(store_id,telegram_user_id,role,permissions_json,enabled,revision) VALUES(?,?,?,'[]',1,1) ON CONFLICT(store_id,telegram_user_id) DO UPDATE SET role=excluded.role,permissions_json='[]',enabled=1,revision=telegram_commerce_members.revision+1", out.ID, out.ManagerUserID, rep.Kind)
	if err != nil {
		return TelegramStore{}, err
	}
	// Actor zero denotes the authenticated singleton browser owner; it is never
	// attributed to the newly assigned Telegram manager.
	if err = appendTelegramEventTx(ctx, tx, in.EventID, hash, out.ID, 0, "owner_browser_configure_store", out); err != nil {
		return TelegramStore{}, err
	}
	if err = tx.Commit(); err != nil {
		return TelegramStore{}, err
	}
	return out, nil
}

func (s *Store) ListTelegramStoresAs(ctx context.Context, token string) ([]TelegramStore, error) {
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
	query := "SELECT " + telegramStoreColumns + " FROM telegram_commerce_stores"
	args := []any{}
	if scope != "" {
		query += " WHERE representative_id=?"
		args = append(args, scope)
	}
	query += " ORDER BY store_id LIMIT 1001"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TelegramStore{}
	for rows.Next() {
		store, e := scanTelegramStore(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, store)
	}
	if len(out) > 1000 {
		return nil, ErrUnavailable
	}
	return out, rows.Err()
}

func telegramCustomerOwnsClientTx(ctx context.Context, tx *sql.Tx, access TelegramAccess, clientID string) error {
	if !idRE.MatchString(clientID) {
		return ErrInvalid
	}
	var n int
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM telegram_commerce_customer_clients b JOIN clients c ON c.id=b.client_id WHERE b.store_id=? AND b.customer_id=? AND b.client_id=? AND c.representative_id=?", access.Store.ID, access.CustomerID, clientID, access.Store.RepresentativeID).Scan(&n)
	if err != nil {
		return ErrUnavailable
	}
	if n != 1 {
		return ErrTelegramDenied
	}
	return nil
}
