package hubcore

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Bulk work and the change feed of the connected shop API.
//
// A bulk call takes up to 500 items and runs them in one transaction, each
// item in its own savepoint: by default a failed item is reported and the
// others go ahead; with "atomic": true one failure undoes the whole call.
//
// The change feed returns every client whose state or usage changed after a
// cursor, oldest change first, plus the clients that were deleted (or moved
// out of the shop). Keep the returned cursor and ask again with it; "more"
// says whether another page is ready right now.

const tenantBulkMax = 500

// TenantBulkResult is one item's outcome.
type TenantBulkResult struct {
	Name           string `json:"name"`
	OK             bool   `json:"ok"`
	Error          string `json:"error,omitempty"`
	Code           string `json:"code,omitempty"`
	UUID           string `json:"uuid,omitempty"`
	TrojanPassword string `json:"trojan_password,omitempty"`
	SubToken       string `json:"sub_token,omitempty"`
	SubURL         string `json:"subscription_url,omitempty"`
}

func bulkCode(err error) (string, string) {
	msg := err.Error()
	for _, p := range []string{"invalid: ", "conflict: ", "not allowed by policy: "} {
		msg = strings.TrimPrefix(msg, p)
	}
	switch {
	case errors.Is(err, ErrNotFound):
		return "not_found", "client not found"
	case errors.Is(err, ErrConflict):
		return "conflict", msg
	case errors.Is(err, ErrPolicy):
		return "policy", msg
	case errors.Is(err, ErrInvalid):
		return "invalid", msg
	}
	return "unavailable", "temporarily unavailable"
}

// tenantBulk runs one function per item inside one transaction.
func (s *Store) tenantBulk(ctx context.Context, tenant string, names []string, atomic bool,
	do func(tx *sql.Tx, t Tenant, i int) error) ([]TenantBulkResult, bool, error) {
	if len(names) == 0 || len(names) > tenantBulkMax {
		return nil, false, fmt.Errorf("%w: send 1 to %d items", ErrInvalid, tenantBulkMax)
	}
	t, err := s.tenantReady(ctx, tenant)
	if err != nil {
		return nil, false, err
	}
	t.Inbounds = s.TenantInbounds(ctx, t) // once for the whole call
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, err
	}
	defer tx.Rollback()
	out := make([]TenantBulkResult, len(names))
	failed := false
	for i, n := range names {
		out[i].Name = n
		if _, err := tx.ExecContext(ctx, `SAVEPOINT item`); err != nil {
			return nil, false, err
		}
		if err := do(tx, t, i); err != nil {
			if _, e := tx.ExecContext(ctx, `ROLLBACK TO item`); e != nil {
				return nil, false, e
			}
			out[i].Code, out[i].Error = bulkCode(err)
			if out[i].Code == "unavailable" {
				return nil, false, err
			}
			failed = true
		} else {
			out[i].OK = true
		}
		if _, err := tx.ExecContext(ctx, `RELEASE item`); err != nil {
			return nil, false, err
		}
		if failed && atomic {
			for j := i + 1; j < len(names); j++ {
				out[j] = TenantBulkResult{Name: names[j], Code: "skipped", Error: "not tried: an earlier item failed and the call is atomic"}
			}
			return out, false, nil
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// TenantBulkCreate makes many clients.
func (s *Store) TenantBulkCreate(ctx context.Context, tenant string, items []TenantClientIn, atomic bool) ([]TenantBulkResult, bool, error) {
	names := make([]string, len(items))
	for i := range items {
		items[i].Name = strings.TrimSpace(items[i].Name)
		names[i] = items[i].Name
	}
	var creds [][3]string
	creds = make([][3]string, len(items))
	res, committed, err := s.tenantBulk(ctx, tenant, names, atomic, func(tx *sql.Tx, t Tenant, i int) error {
		if err := s.tenantCreateTx(ctx, tx, t, items[i]); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT uuid,secret,sub_token FROM clients WHERE email=?`, tenantEmail(t.ID, items[i].Name)).
			Scan(&creds[i][0], &creds[i][1], &creds[i][2])
	})
	if committed {
		for i := range res {
			if res[i].OK {
				res[i].UUID, res[i].TrojanPassword, res[i].SubToken = creds[i][0], creds[i][1], creds[i][2]
			}
		}
	}
	return res, committed, err
}

// TenantBulkUpdate changes many clients (the same fields as one change).
func (s *Store) TenantBulkUpdate(ctx context.Context, tenant string, items []TenantClientIn, atomic bool) ([]TenantBulkResult, bool, error) {
	names := make([]string, len(items))
	for i := range items {
		names[i] = items[i].Name
	}
	return s.tenantBulk(ctx, tenant, names, atomic, func(tx *sql.Tx, t Tenant, i int) error {
		return s.tenantUpdateTx(ctx, tx, t, items[i].Name, items[i])
	})
}

// TenantBulkRenewItem is one renewal.
type TenantBulkRenewItem struct {
	Name string `json:"name"`
	TenantRenewIn
}

// TenantBulkRenew renews many clients.
func (s *Store) TenantBulkRenew(ctx context.Context, tenant string, items []TenantBulkRenewItem, atomic bool) ([]TenantBulkResult, bool, error) {
	names := make([]string, len(items))
	for i := range items {
		names[i] = items[i].Name
	}
	return s.tenantBulk(ctx, tenant, names, atomic, func(tx *sql.Tx, t Tenant, i int) error {
		return s.tenantRenewTx(ctx, tx, t, items[i].Name, items[i].TenantRenewIn)
	})
}

// TenantBulkDelete removes many clients.
func (s *Store) TenantBulkDelete(ctx context.Context, tenant string, names []string, atomic bool) ([]TenantBulkResult, bool, error) {
	return s.tenantBulk(ctx, tenant, names, atomic, func(tx *sql.Tx, t Tenant, i int) error {
		email := tenantEmail(t.ID, names[i])
		var x int
		if !tenantNameRE.MatchString(names[i]) || tx.QueryRowContext(ctx, `SELECT 1 FROM clients WHERE email=? AND rep_id=?`, email, tenantRep(t.ID)).Scan(&x) != nil {
			return ErrNotFound
		}
		if err := keepRepUsage(ctx, tx, email); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM clients WHERE email=?`, email); err != nil {
			return err
		}
		return forgetClient(ctx, tx, email)
	})
}

// --- the change feed ---

// TenantChange is one changed (or deleted) client.
type TenantChange struct {
	Name            string `json:"name"`
	Rev             int64  `json:"rev"`
	Deleted         bool   `json:"deleted,omitempty"`
	ExternalRef     string `json:"external_ref,omitempty"`
	Status          string `json:"status,omitempty"`
	Enabled         bool   `json:"enabled"`
	VolumeBytes     int64  `json:"volume_bytes"`
	UsedBytes       int64  `json:"used_bytes"`
	UpBytes         int64  `json:"up_bytes"`
	DownBytes       int64  `json:"down_bytes"`
	RemainingBytes  int64  `json:"remaining_bytes"`
	ExpiresUnix     int64  `json:"expires_unix"`
	EffectiveExpiry int64  `json:"effective_expiry_unix"`
	FirstSeen       int64  `json:"first_seen_unix"`
	LastSeen        int64  `json:"last_seen_unix"`
}

// feedCursor is an opaque "after this change" mark: a revision and a name.
func encodeCursor(rev int64, email string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(rev, 10) + ":" + email))
}

func decodeCursor(c string) (int64, string, error) {
	if c == "" {
		return 0, "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return 0, "", fmt.Errorf("%w: bad cursor", ErrInvalid)
	}
	r, e, ok := strings.Cut(string(b), ":")
	rev, err := strconv.ParseInt(r, 10, 64)
	if !ok || err != nil || rev < 0 {
		return 0, "", fmt.Errorf("%w: bad cursor", ErrInvalid)
	}
	return rev, e, nil
}

// TenantChanges returns up to limit changes after cursor, the next cursor and
// whether more are ready.
func (s *Store) TenantChanges(ctx context.Context, tenant, cursor string, limit int) ([]TenantChange, string, bool, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rev, after, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", false, err
	}
	rep := tenantRep(tenant)
	const r = `MAX(c.rev, COALESCE(u.rev,0))`
	rows, err := s.ro.QueryContext(ctx, `SELECT c.email, `+r+`, c.external_ref, c.enabled, c.volume_bytes, c.expires_unix, c.duration_seconds,
COALESCE(u.up,0), COALESCE(u.down,0), COALESCE(u.first_seen_unix,0), COALESCE(u.last_seen_unix,0)
FROM clients c LEFT JOIN client_usage u ON u.email=c.email
WHERE c.rep_id=? AND (`+r+`>? OR (`+r+`=? AND c.email>?)) ORDER BY 2, c.email LIMIT ?`, rep, rev, rev, after, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	type item struct {
		email string
		ch    TenantChange
	}
	var items []item
	now := s.now().Unix()
	for rows.Next() {
		var it item
		var c ClientView
		if err := rows.Scan(&it.email, &it.ch.Rev, &it.ch.ExternalRef, &c.Enabled, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds,
			&c.Usage.Up, &c.Usage.Down, &c.Usage.FirstSeen, &c.Usage.LastSeen); err != nil {
			rows.Close()
			return nil, "", false, err
		}
		c.EffectiveUntil = EffectiveExpiry(c.Client, c.Usage)
		c.Remaining = Remaining(c.Client, c.Usage)
		ch := &it.ch
		ch.Status, ch.Enabled, ch.VolumeBytes = pgStatus(c, now), c.Enabled, c.VolumeBytes
		ch.UpBytes, ch.DownBytes, ch.UsedBytes = c.Usage.Up, c.Usage.Down, c.Usage.Up+c.Usage.Down
		ch.RemainingBytes, ch.ExpiresUnix, ch.EffectiveExpiry = c.Remaining, c.ExpiresUnix, c.EffectiveUntil
		ch.FirstSeen, ch.LastSeen = c.Usage.FirstSeen, c.Usage.LastSeen
		if ch.VolumeBytes < 0 {
			ch.VolumeBytes, ch.RemainingBytes = -1, -1
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, "", false, err
	}
	gone, err := s.ro.QueryContext(ctx, `SELECT name, rev FROM tenant_tombstones WHERE tenant_id=? AND (rev>? OR (rev=? AND name>?)) ORDER BY rev, name LIMIT ?`,
		tenant, rev, rev, after, limit+1)
	if err != nil {
		return nil, "", false, err
	}
	for gone.Next() {
		var it item
		if err := gone.Scan(&it.email, &it.ch.Rev); err != nil {
			gone.Close()
			return nil, "", false, err
		}
		it.ch.Deleted = true
		items = append(items, it)
	}
	gone.Close()
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ch.Rev != items[j].ch.Rev {
			return items[i].ch.Rev < items[j].ch.Rev
		}
		return items[i].email < items[j].email
	})
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	out := make([]TenantChange, 0, len(items))
	next := cursor
	for _, it := range items {
		it.ch.Name = tenantName(tenant, it.email)
		out = append(out, it.ch)
		next = encodeCursor(it.ch.Rev, it.email)
	}
	if next == "" {
		next = encodeCursor(0, "") // nothing changed yet: start from the beginning next time
	}
	return out, next, more, nil
}

// pruneTombstones forgets deletions older than 30 days: a shop that has not
// read its feed for that long reads its whole list again.
func (s *Store) pruneTombstones(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM tenant_tombstones WHERE unix < ?`, s.now().Unix()-30*86400)
}

func tenantBulkRoutes(s *Store, mux *docMux, auth func(func(http.ResponseWriter, *http.Request, string)) http.HandlerFunc, base func(*http.Request, string) string) {
	reply := func(w http.ResponseWriter, r *http.Request, tenant string, res []TenantBulkResult, committed bool, err error) {
		if err != nil {
			tenantFail(w, err)
			return
		}
		ok := 0
		for i := range res {
			if res[i].OK {
				ok++
				if res[i].SubToken != "" {
					res[i].SubURL = subURL(base(r, tenant), r, res[i].SubToken)
				}
			}
		}
		code := http.StatusOK
		if !committed {
			code = http.StatusUnprocessableEntity
		}
		tenantJSON(w, code, map[string]any{"committed": committed, "ok": ok, "failed": len(res) - ok, "results": res})
	}
	const bodyMax = 8 << 20
	mux.HandleFunc("POST "+tenantAPI+"/bulk/create", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in struct {
			Clients []TenantClientIn `json:"clients"`
			Atomic  bool             `json:"atomic"`
		}
		if !tenantDecode(w, r, bodyMax, &in) {
			return
		}
		res, committed, err := s.TenantBulkCreate(r.Context(), tenant, in.Clients, in.Atomic)
		reply(w, r, tenant, res, committed, err)
	}))
	mux.HandleFunc("POST "+tenantAPI+"/bulk/update", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in struct {
			Clients []TenantClientIn `json:"clients"`
			Atomic  bool             `json:"atomic"`
		}
		if !tenantDecode(w, r, bodyMax, &in) {
			return
		}
		res, committed, err := s.TenantBulkUpdate(r.Context(), tenant, in.Clients, in.Atomic)
		reply(w, r, tenant, res, committed, err)
	}))
	mux.HandleFunc("POST "+tenantAPI+"/bulk/renew", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in struct {
			Clients []TenantBulkRenewItem `json:"clients"`
			Atomic  bool                  `json:"atomic"`
		}
		if !tenantDecode(w, r, bodyMax, &in) {
			return
		}
		res, committed, err := s.TenantBulkRenew(r.Context(), tenant, in.Clients, in.Atomic)
		reply(w, r, tenant, res, committed, err)
	}))
	mux.HandleFunc("POST "+tenantAPI+"/bulk/delete", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		var in struct {
			Names  []string `json:"names"`
			Atomic bool     `json:"atomic"`
		}
		if !tenantDecode(w, r, bodyMax, &in) {
			return
		}
		res, committed, err := s.TenantBulkDelete(r.Context(), tenant, in.Names, in.Atomic)
		reply(w, r, tenant, res, committed, err)
	}))
	mux.HandleFunc("GET "+tenantAPI+"/changes", auth(func(w http.ResponseWriter, r *http.Request, tenant string) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, next, more, err := s.TenantChanges(r.Context(), tenant, r.URL.Query().Get("cursor"), limit)
		if err != nil {
			tenantFail(w, err)
			return
		}
		tenantJSON(w, http.StatusOK, map[string]any{"changes": list, "cursor": next, "more": more})
	}))
}
