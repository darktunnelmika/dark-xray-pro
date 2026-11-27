package hubcore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The external API: what other programs (a partner's website or bot, a
// reseller's own bot) call with a key, as opposed to the panel's own API
// behind a browser session. The partner API (partner.go, dp_ keys) and the
// reseller API (resapi.go, dr_ keys) share everything here:
//
//   - keys carry scopes, an optional list of allowed addresses, an optional
//     expiry and a request rate (requests per minute, a token bucket);
//   - an address that keeps sending wrong keys is slowed down (429);
//   - every answer carries X-Request-ID, errors are
//     {"detail": "...", "code": "...", "request_id": "..."};
//   - a change sent with an Idempotency-Key header runs once: the same key
//     and the same request within 24 hours gets the first answer again
//     (Idempotent-Replayed: true), so a retry after a timeout never makes a
//     second panel or charges credit twice;
//   - lists take limit and offset;
//   - each key's owner (a partner key, a reseller) has an event list it can
//     read, and can have a webhook: events are written to an outbox as they
//     happen and delivered in order, in batches, signed with HMAC-SHA256,
//     retried with a growing wait (apiwebhook.go).

const extAPISchema = `
CREATE TABLE IF NOT EXISTS api_idempotency (
  key_id TEXT NOT NULL,
  idem_key TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  status INTEGER NOT NULL DEFAULT 0,
  content_type TEXT NOT NULL DEFAULT '',
  body BLOB NOT NULL DEFAULT '',
  created_unix INTEGER NOT NULL,
  PRIMARY KEY(key_id, idem_key)
);
`

func (s *Store) ensureExtAPISchema() error {
	if _, err := s.db.Exec(extAPISchema); err != nil {
		return err
	}
	return s.ensureAPIWebhookSchema()
}

// Error codes of the external API, stable for programs to switch on.
const (
	codeUnauthorized   = "unauthorized"
	codeKeyExpired     = "key_expired"
	codeIPNotAllowed   = "ip_not_allowed"
	codeScopeMissing   = "scope_missing"
	codeRateLimited    = "rate_limited"
	codeInvalidJSON    = "invalid_json"
	codeValidation     = "validation_failed"
	codeNotFound       = "not_found"
	codeCredit         = "insufficient_credit"
	codeCreditLimit    = "credit_limit"
	codePolicy         = "policy_denied"
	codeDisabled       = "account_disabled"
	codeConflict       = "conflict"
	codeIdemReused     = "idempotency_key_reused"
	codeIdemInProgress = "idempotency_in_progress"
	codeInternal       = "internal_error"
)

// apiCaller is the key behind an external API request.
type apiCaller struct {
	Kind    string // partner | reseller
	KeyID   string
	Name    string
	RepID   string // reseller keys: whose clients
	Scopes  map[string]bool
	IPs     []netip.Prefix // empty: any address
	Rate    int            // requests per minute; 0 = the default
	Expires int64          // 0 = never
}

func (c apiCaller) can(scope string) bool { return c.Scopes[scope] }

func (c apiCaller) ipAllowed(ip string) bool {
	if len(c.IPs) == 0 {
		return true
	}
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	a = a.Unmap()
	for _, p := range c.IPs {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// Request rates.
const (
	defaultKeyRate = 120  // requests a minute when a key sets none
	maxKeyRate     = 6000 // the most a key can be given
	badKeyRate     = 20   // wrong keys a minute from one address before 429
)

// errKey* are why a key was refused.
var (
	errKeyInvalid = errors.New("invalid or disabled key")
	errKeyExpired = errors.New("this key has expired")
	errAccountOff = errors.New("this account is turned off")
)

// ---- key settings shared by partner and reseller keys ----

// normScopes keeps known names (scopes, event types), sorted and without
// repeats; an unknown one is an error. Letter case does not matter.
func normScopes(in []string, known []string) ([]string, error) {
	return normNames(in, known, true)
}

// normNames is normScopes for names whose letter case counts (ids the
// owner typed, such as package ids) when lower is false.
func normNames(in []string, known []string, lower bool) ([]string, error) {
	ok := map[string]bool{}
	for _, k := range known {
		ok[k] = true
	}
	seen := map[string]bool{}
	out := []string{}
	for _, sc := range in {
		sc = strings.TrimSpace(sc)
		if lower {
			sc = strings.ToLower(sc)
		}
		if sc == "" || seen[sc] {
			continue
		}
		if !ok[sc] {
			return nil, fmt.Errorf("%w: unknown name %q (known: %s)", ErrInvalid, sc, strings.Join(known, ", "))
		}
		seen[sc] = true
		out = append(out, sc)
	}
	sort.Strings(out)
	return out, nil
}

func scopeSet(csv string) map[string]bool {
	m := map[string]bool{}
	for _, sc := range splitTags(csv) {
		m[sc] = true
	}
	return m
}

func scopeList(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, on := range m {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// normIPs checks a list of addresses and networks (1.2.3.4, 10.0.0.0/8,
// 2001:db8::/32) and returns it cleaned up.
func normIPs(in []string) ([]string, error) {
	if len(in) > 50 {
		return nil, fmt.Errorf("%w: at most 50 allowed addresses", ErrInvalid)
	}
	out := []string{}
	seen := map[string]bool{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		p, err := parsePrefix(v)
		if err != nil {
			return nil, fmt.Errorf("%w: %q is not an IP address or network", ErrInvalid, v)
		}
		if t := p.String(); !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out, nil
}

func parsePrefix(v string) (netip.Prefix, error) {
	if strings.Contains(v, "/") {
		p, err := netip.ParsePrefix(v)
		if err != nil {
			return netip.Prefix{}, err
		}
		return p.Masked(), nil
	}
	a, err := netip.ParseAddr(v)
	if err != nil {
		return netip.Prefix{}, err
	}
	a = a.Unmap()
	return netip.PrefixFrom(a, a.BitLen()), nil
}

func prefixes(csv string) []netip.Prefix {
	var out []netip.Prefix
	for _, v := range splitTags(csv) {
		if p, err := parsePrefix(v); err == nil {
			out = append(out, p)
		}
	}
	return out
}

func checkKeyRate(rate int) error {
	if rate < 0 || rate > maxKeyRate {
		return fmt.Errorf("%w: rate_per_min is 0 (default %d) to %d", ErrInvalid, defaultKeyRate, maxKeyRate)
	}
	return nil
}

// bearerKey is the key in Authorization: Bearer <key> or X-API-Key.
func bearerKey(r *http.Request) string {
	if tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		return strings.TrimSpace(tok)
	}
	return strings.TrimSpace(r.Header.Get("X-API-Key"))
}

// splitKey splits <prefix><id>_<secret>.
func splitKey(tok, prefix string) (id, secret string, ok bool) {
	rest, ok := strings.CutPrefix(tok, prefix)
	if !ok {
		return "", "", false
	}
	id, secret, ok = strings.Cut(rest, "_")
	return id, secret, ok && id != "" && secret != ""
}

// ---- rate limits ----

type tokenBucket struct {
	tokens float64
	last   time.Time
}

// rateLimiter keeps one token bucket per name: perMin tokens a minute, at
// most perMin at once.
type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	calls   int
}

func (l *rateLimiter) bucket(name string, perMin int, now time.Time) *tokenBucket {
	if l.buckets == nil {
		l.buckets = map[string]*tokenBucket{}
	}
	if l.calls++; l.calls%1000 == 0 {
		for k, b := range l.buckets { // forget idle buckets
			if now.Sub(b.last) > 15*time.Minute {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[name]
	if b == nil {
		b = &tokenBucket{tokens: float64(perMin), last: now}
		l.buckets[name] = b
	}
	if el := now.Sub(b.last).Seconds(); el > 0 {
		b.tokens = math.Min(float64(perMin), b.tokens+el*float64(perMin)/60)
		b.last = now
	}
	return b
}

// take spends a token. It returns whether one was there, what is left and,
// when refused, how long until the next one.
func (l *rateLimiter) take(name string, perMin int, now time.Time) (bool, int, time.Duration) {
	if perMin <= 0 {
		return true, 0, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.bucket(name, perMin, now)
	if b.tokens < 1 {
		wait := time.Duration((1 - b.tokens) * 60 / float64(perMin) * float64(time.Second))
		return false, 0, wait
	}
	b.tokens--
	return true, int(b.tokens), 0
}

// ---- answers ----

var requestIDRE = regexp.MustCompile(`^[A-Za-z0-9._-]{8,64}$`)

// requestID takes the caller's X-Request-ID when it looks like one, or
// makes one, and puts it on the answer.
func requestID(w http.ResponseWriter, r *http.Request) string {
	id := r.Header.Get("X-Request-ID")
	if !requestIDRE.MatchString(id) {
		id = randomHex(8)
	}
	w.Header().Set("X-Request-ID", id)
	return id
}

// apiErr answers an error: {"detail", "code", "request_id"}. detail stays
// for programs written against the first partner API.
func apiErr(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"detail": detail, "code": code, "request_id": w.Header().Get("X-Request-ID")})
}

// apiJSON answers a value.
func apiJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errDetail is a store error's message without the sentinel's own words
// ("invalid: the id may …" → "the id may …").
func errDetail(err error) string {
	msg := err.Error()
	for _, sentinel := range []error{ErrInvalid, ErrPolicy, ErrNotFound, ErrCredit, ErrAuth, errPartnerFunds} {
		if p := sentinel.Error() + ": "; strings.HasPrefix(msg, p) {
			return strings.TrimPrefix(msg, p)
		}
	}
	return msg
}

// apiFail answers a store error.
func apiFail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		apiErr(w, http.StatusNotFound, codeNotFound, "not found")
	case errors.Is(err, ErrCredit):
		apiErr(w, http.StatusPaymentRequired, codeCredit, "not enough credit")
	case errors.Is(err, errPartnerFunds):
		apiErr(w, http.StatusPaymentRequired, codeFunds, errDetail(err))
	case errors.Is(err, ErrPolicy):
		apiErr(w, http.StatusForbidden, codePolicy, errDetail(err))
	case errors.Is(err, ErrAuth):
		apiErr(w, http.StatusForbidden, codeDisabled, "this account is turned off")
	case errors.Is(err, ErrInvalid):
		d := errDetail(err)
		if d == ErrInvalid.Error() {
			d = "the request is not valid"
		}
		apiErr(w, http.StatusUnprocessableEntity, codeValidation, d)
	default:
		log.Printf("external api: %v", err)
		apiErr(w, http.StatusInternalServerError, codeInternal, "internal error")
	}
}

// readJSON reads a request body into v. An empty body leaves v as it is.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return true
		}
		apiErr(w, http.StatusBadRequest, codeInvalidJSON, "the body is not valid JSON: "+err.Error())
		return false
	}
	return true
}

// listPage is a list request's limit and offset.
type listPage struct{ Limit, Offset int }

func readPage(r *http.Request, def, max int) listPage {
	q := r.URL.Query()
	p := listPage{Limit: def}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		p.Limit = min(n, max)
	}
	if n, err := strconv.Atoi(q.Get("offset")); err == nil && n > 0 {
		p.Offset = n
	}
	return p
}

// listOut is a page of a list.
type listOut[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// pageOf cuts one page out of a whole list.
func pageOf[T any](all []T, p listPage) listOut[T] {
	out := listOut[T]{Items: []T{}, Total: len(all), Limit: p.Limit, Offset: p.Offset}
	if p.Offset < len(all) {
		out.Items = all[p.Offset:min(len(all), p.Offset+p.Limit)]
	}
	return out
}

// ---- the request pipeline ----

// routeMux is where routes are registered (an *http.ServeMux; a recorder
// in tests).
type routeMux interface {
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// extAuthFunc finds the key behind a request.
type extAuthFunc func(r *http.Request) (apiCaller, error)

// extHandler is an external API handler.
type extHandler func(w http.ResponseWriter, r *http.Request, c apiCaller)

// ext wraps an external API handler: request id, key (with a slowdown for
// an address that keeps sending wrong ones), address, rate, scope, then (for
// changes) idempotency and the change log. scope "" needs no scope.
//
// The slowdown only ever answers requests whose key is wrong: a right key
// is served even from an address that also sends wrong ones, so partners
// behind one CDN or NAT never lock each other out. Keys are long random
// secrets checked by hash, so guessing gains nothing from the extra tries.
func (s *Store) ext(auth extAuthFunc, scope string, h extHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID(w, r)
		ip := realIP(r)
		now := s.now()
		c, err := auth(r)
		if errors.Is(err, errAccountOff) {
			apiErr(w, http.StatusForbidden, codeDisabled, err.Error())
			return
		}
		if err != nil {
			if ok, _, wait := s.apiRates.take("bad:"+ip, badKeyRate, now); !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
				apiErr(w, http.StatusTooManyRequests, codeRateLimited, "too many wrong keys from this address; wait a minute")
				return
			}
			w.Header().Set("WWW-Authenticate", "Bearer")
			code := codeUnauthorized
			if errors.Is(err, errKeyExpired) {
				code = codeKeyExpired
			}
			apiErr(w, http.StatusUnauthorized, code, err.Error())
			return
		}
		if !c.ipAllowed(ip) {
			apiErr(w, http.StatusForbidden, codeIPNotAllowed, "this key may not be used from "+ip)
			return
		}
		rate := c.Rate
		if rate <= 0 {
			rate = defaultKeyRate
		}
		ok, left, wait := s.apiRates.take(c.Kind+":"+c.KeyID, rate, now)
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rate))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(left))
		if !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			apiErr(w, http.StatusTooManyRequests, codeRateLimited, fmt.Sprintf("more than %d requests a minute; slow down", rate))
			return
		}
		if scope != "" && !c.can(scope) {
			apiErr(w, http.StatusForbidden, codeScopeMissing, "this key does not have the "+scope+" permission")
			return
		}
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			h(w, r, c)
			return
		}
		sw := &statusWriter{ResponseWriter: w}
		s.idempotent(sw, r, c.Kind+":"+c.KeyID, func(w http.ResponseWriter, r *http.Request) { h(w, r, c) })
		p := Principal{User: c.Kind + ":" + c.Name, Role: "owner"}
		if c.Kind == "reseller" {
			p = Principal{User: "key:" + c.RepID + "/" + c.Name, Role: "rep", RepID: c.RepID}
		}
		s.Audit(r.Context(), p, r, sw.status)
	}
}

// ---- idempotency ----

const (
	idemTTL   = 24 * time.Hour
	idemStale = 2 * time.Minute // a claim this old with no answer is from a crashed request
)

var idemKeyRE = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// captureWriter passes an answer through and keeps a copy of it.
type captureWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (w *captureWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *captureWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.body.Len() < 1<<20 {
		w.body.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

// idempotent runs h once per (key, Idempotency-Key). Without the header it
// just runs h. Only successful answers are kept: a refused request can be
// sent again, fixed, with the same Idempotency-Key.
func (s *Store) idempotent(w http.ResponseWriter, r *http.Request, keyID string, h http.HandlerFunc) {
	ik := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if ik == "" {
		h(w, r)
		return
	}
	if !idemKeyRE.MatchString(ik) {
		apiErr(w, http.StatusBadRequest, codeValidation, "Idempotency-Key: 1 to 128 letters, digits, dot, dash, underscore or colon")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		apiErr(w, http.StatusRequestEntityTooLarge, codeValidation, "the body is too large")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	sum := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"\n"), body...))
	fp := hex.EncodeToString(sum[:])
	ctx := r.Context()
	now := s.now().Unix()

	claim := func() bool {
		res, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO api_idempotency(key_id,idem_key,fingerprint,created_unix) VALUES(?,?,?,?)`, keyID, ik, fp, now)
		if err != nil {
			return false
		}
		n, _ := res.RowsAffected()
		return n == 1
	}
	if !claim() {
		var gotFP, ctype string
		var status int
		var created int64
		var stored []byte
		err := s.db.QueryRowContext(ctx, `SELECT fingerprint,status,content_type,body,created_unix FROM api_idempotency WHERE key_id=? AND idem_key=?`, keyID, ik).
			Scan(&gotFP, &status, &ctype, &stored, &created)
		stale := err == nil && (created < now-int64(idemTTL.Seconds()) || (status == 0 && created < now-int64(idemStale.Seconds())))
		switch {
		case errors.Is(err, sql.ErrNoRows) || stale:
			_, _ = s.db.ExecContext(ctx, `DELETE FROM api_idempotency WHERE key_id=? AND idem_key=? AND created_unix=?`, keyID, ik, created)
			if !claim() {
				apiErr(w, http.StatusConflict, codeIdemInProgress, "a request with this Idempotency-Key is still running")
				return
			}
		case err != nil:
			apiFail(w, err)
			return
		case gotFP != fp:
			apiErr(w, http.StatusUnprocessableEntity, codeIdemReused, "this Idempotency-Key was used for a different request")
			return
		case status == 0:
			w.Header().Set("Retry-After", "2")
			apiErr(w, http.StatusConflict, codeIdemInProgress, "a request with this Idempotency-Key is still running")
			return
		default:
			if ctype != "" {
				w.Header().Set("Content-Type", ctype)
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(status)
			_, _ = w.Write(stored)
			return
		}
	}
	cw := &captureWriter{ResponseWriter: w}
	done := false
	defer func() {
		// Keep a successful answer; free the key otherwise (also on a panic).
		bg := context.WithoutCancel(ctx)
		if done && cw.status >= 200 && cw.status < 300 && cw.body.Len() < 1<<20 {
			_, _ = s.db.ExecContext(bg, `UPDATE api_idempotency SET status=?, content_type=?, body=? WHERE key_id=? AND idem_key=?`,
				cw.status, cw.Header().Get("Content-Type"), cw.body.Bytes(), keyID, ik)
			return
		}
		_, _ = s.db.ExecContext(bg, `DELETE FROM api_idempotency WHERE key_id=? AND idem_key=? AND status=0`, keyID, ik)
	}()
	h(cw, r)
	done = true
}

// pruneExtAPI forgets old idempotency answers and old events.
func (s *Store) pruneExtAPI(ctx context.Context) error {
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM api_idempotency WHERE created_unix<?`, now.Add(-idemTTL).Unix()); err != nil && !noTable(err) {
		return err
	}
	return s.pruneAPIWebhooks(ctx)
}
