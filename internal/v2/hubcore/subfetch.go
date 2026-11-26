package hubcore

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// When each client last fetched its subscription, and a short cache of the
// subscription body.
//
// Thousands of apps refresh at once when a shop moves its customers here
// (or simply every few hours). Writing one row per fetch would queue them
// all behind the single SQLite writer, so fetches are noted in memory and
// written together every 30 seconds (RunLegacy). The body of a subscription
// is kept for subCacheTTL: a refresh storm builds each client's configs once,
// not once per request. A changed client is at most that much behind.

const subFetchSchema = `
CREATE TABLE IF NOT EXISTS sub_fetch (
  email TEXT PRIMARY KEY,
  unix INTEGER NOT NULL,
  ua TEXT NOT NULL DEFAULT ''
);`

func (s *Store) ensureSubFetchSchema() error {
	_, err := s.db.Exec(subFetchSchema)
	return err
}

type subFetchNote struct {
	unix int64
	ua   string
}

var subFetches = struct {
	sync.Mutex
	m map[*Store]map[string]subFetchNote
}{m: map[*Store]map[string]subFetchNote{}}

// noteSubFetch records a fetch in memory.
func (s *Store) noteSubFetch(email, ua string) {
	if len(ua) > 120 {
		ua = ua[:120]
	}
	subFetches.Lock()
	m := subFetches.m[s]
	if m == nil {
		m = map[string]subFetchNote{}
		subFetches.m[s] = m
	}
	m[email] = subFetchNote{unix: s.now().Unix(), ua: ua}
	subFetches.Unlock()
}

// flushSubFetches writes the noted fetches in one transaction.
func (s *Store) flushSubFetches(ctx context.Context) error {
	subFetches.Lock()
	m := subFetches.m[s]
	delete(subFetches.m, s)
	subFetches.Unlock()
	if len(m) == 0 {
		return nil
	}
	put := func() error {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		st, err := tx.PrepareContext(ctx, `INSERT INTO sub_fetch(email,unix,ua) VALUES(?,?,?)
ON CONFLICT(email) DO UPDATE SET unix=MAX(unix,excluded.unix), ua=excluded.ua`)
		if err != nil {
			return err
		}
		defer st.Close()
		for e, n := range m {
			if _, err := st.ExecContext(ctx, e, n.unix, n.ua); err != nil {
				return err
			}
		}
		return tx.Commit()
	}
	if err := put(); err != nil {
		// Back into memory for the next round (newer notes win).
		subFetches.Lock()
		cur := subFetches.m[s]
		if cur == nil {
			cur = map[string]subFetchNote{}
			subFetches.m[s] = cur
		}
		for e, n := range m {
			if c, ok := cur[e]; !ok || c.unix < n.unix {
				cur[e] = n
			}
		}
		subFetches.Unlock()
		return err
	}
	return nil
}

// pruneSubFetches forgets deleted clients' fetches.
func (s *Store) pruneSubFetches(ctx context.Context) {
	_, _ = s.db.ExecContext(ctx, `DELETE FROM sub_fetch WHERE email NOT IN (SELECT email FROM clients)`)
}

// subFetchedAt is when a client last fetched (pending notes included).
func (s *Store) subFetchedAt(ctx context.Context, emails []string) map[string]int64 {
	out := map[string]int64{}
	if len(emails) == 0 {
		return out
	}
	for i := 0; i < len(emails); i += 500 {
		part := emails[i:min(i+500, len(emails))]
		args := make([]any, len(part))
		q := `SELECT email,unix FROM sub_fetch WHERE email IN (`
		for j, e := range part {
			args[j] = e
			if j > 0 {
				q += ","
			}
			q += "?"
		}
		rows, err := s.ro.QueryContext(ctx, q+")", args...)
		if err != nil {
			continue
		}
		for rows.Next() {
			var e string
			var u int64
			if rows.Scan(&e, &u) == nil {
				out[e] = u
			}
		}
		rows.Close()
	}
	subFetches.Lock()
	for _, e := range emails {
		if n, ok := subFetches.m[s][e]; ok && n.unix > out[e] {
			out[e] = n.unix
		}
	}
	subFetches.Unlock()
	return out
}

// ---- the body cache ----

const subCacheTTL = 30 * time.Second

type subCacheEntry struct {
	body        []byte
	ctype, info string
	at          time.Time
}

var subCache = struct {
	sync.Mutex
	m     map[*Store]map[string]subCacheEntry
	swept map[*Store]time.Time
}{m: map[*Store]map[string]subCacheEntry{}, swept: map[*Store]time.Time{}}

func (s *Store) subCacheGet(key string) (subCacheEntry, bool) {
	subCache.Lock()
	defer subCache.Unlock()
	e, ok := subCache.m[s][key]
	if !ok {
		return e, false
	}
	if age := s.now().Sub(e.at); age < 0 || age >= subCacheTTL {
		delete(subCache.m[s], key)
		return e, false
	}
	return e, true
}

func (s *Store) subCachePut(key string, e subCacheEntry) {
	now := s.now()
	e.at = now
	subCache.Lock()
	defer subCache.Unlock()
	m := subCache.m[s]
	if m == nil {
		m = map[string]subCacheEntry{}
		subCache.m[s] = m
	}
	m[key] = e
	if now.Sub(subCache.swept[s]) > time.Minute {
		subCache.swept[s] = now
		for k, v := range m {
			if now.Sub(v.at) >= subCacheTTL {
				delete(m, k)
			}
		}
	}
}

// dropSubCache forgets the cached bodies (tests, or after a big change).
func (s *Store) dropSubCache() {
	subCache.Lock()
	delete(subCache.m, s)
	subCache.Unlock()
}

// tenantSubRev is a connected shop's client's revision (its row and its
// usage); false for every other client.
func (s *Store) tenantSubRev(ctx context.Context, email string) (string, bool) {
	var r1, r2 int64
	if s.ro.QueryRowContext(ctx, `SELECT c.rev, COALESCE(u.rev,0) FROM clients c LEFT JOIN client_usage u ON u.email=c.email
WHERE c.email=? AND substr(c.rep_id,1,1)='~'`, email).Scan(&r1, &r2) != nil {
		return "", false
	}
	return strconv.FormatInt(r1, 10) + "." + strconv.FormatInt(r2, 10), true
}

// SubFetchReport counts a shop's clients that fetched their subscription
// since a time, and lists one page of names of either side.
type SubFetchReport struct {
	Since      int64    `json:"since"`
	Clients    int      `json:"clients"`
	Fetched    int      `json:"fetched"`
	NotFetched int      `json:"not_fetched"`
	List       string   `json:"list,omitempty"` // fetched | not_fetched
	Names      []string `json:"names,omitempty"`
	Next       string   `json:"next,omitempty"`
}

// TenantSubFetches reports on fetches since since (0 = since the shop's
// first domain pointed here, else the last 24 hours).
func (s *Store) TenantSubFetches(ctx context.Context, tenant string, since int64, list, after string, limit int) (SubFetchReport, error) {
	if err := s.flushSubFetches(ctx); err != nil {
		return SubFetchReport{}, err
	}
	if since <= 0 {
		_ = s.ro.QueryRowContext(ctx, `SELECT COALESCE(MIN(dns_unix),0) FROM tenant_sub_domains WHERE tenant_id=? AND dns_unix>0`, tenant).Scan(&since)
		if since <= 0 {
			since = s.now().Unix() - 86400
		}
	}
	rep := tenantRep(tenant)
	out := SubFetchReport{Since: since}
	if err := s.ro.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(COALESCE(f.unix,0)>=?),0) FROM clients c LEFT JOIN sub_fetch f ON f.email=c.email WHERE c.rep_id=?`,
		since, rep).Scan(&out.Clients, &out.Fetched); err != nil {
		return out, err
	}
	out.NotFetched = out.Clients - out.Fetched
	if list != "fetched" && list != "not_fetched" {
		return out, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	cmp := "<"
	if list == "fetched" {
		cmp = ">="
	}
	out.List = list
	rows, err := s.ro.QueryContext(ctx, `SELECT c.email FROM clients c LEFT JOIN sub_fetch f ON f.email=c.email
WHERE c.rep_id=? AND COALESCE(f.unix,0)`+cmp+`? AND c.email>? ORDER BY c.email LIMIT ?`, rep, since, tenantEmail(tenant, after), limit)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	out.Names = []string{}
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return out, err
		}
		out.Names = append(out.Names, tenantName(tenant, e))
	}
	if len(out.Names) == limit {
		out.Next = out.Names[len(out.Names)-1]
	}
	return out, rows.Err()
}
