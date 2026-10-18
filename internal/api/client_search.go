package api

import (
	"net/http"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// Strict list parsing: unlike the legacy paging helper, this is the only
// browser endpoint allowed to accept cross-page search, filter and sort.
func clientSearchParams(r *http.Request) (business.ClientSearch, error) {
	q := r.URL.Query()
	opt := business.ClientSearch{Limit: 50}
	for key, vals := range q {
		if len(vals) != 1 {
			return opt, business.ErrInvalid
		}
		switch key {
		case "limit", "offset", "representative_id", "q", "state", "plan", "sort":
		default:
			return opt, business.ErrInvalid
		}
	}
	if v := q.Get("limit"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 1 || n > 200 {
			return opt, business.ErrInvalid
		}
		opt.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, e := strconv.Atoi(v)
		if e != nil || n < 0 || n > 1_000_000 {
			return opt, business.ErrInvalid
		}
		opt.Offset = n
	}
	opt.RepresentativeID = q.Get("representative_id")
	opt.Query = q.Get("q")
	opt.State = q.Get("state")
	opt.Plan = q.Get("plan")
	opt.Sort = q.Get("sort")
	return opt, nil
}
