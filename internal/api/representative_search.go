package api

import (
	"net/http"
	"strconv"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

// Strict owner directory query: one value per documented key and bounded
// offsets. This remains on the existing authenticated browser-only route.
func representativeSearchParams(r *http.Request) (business.RepresentativeSearch, error) {
	q := r.URL.Query()
	opt := business.RepresentativeSearch{Limit: 50}
	for key, vals := range q {
		if len(vals) != 1 {
			return opt, business.ErrInvalid
		}
		switch key {
		case "limit", "offset", "q", "state", "sort":
		default:
			return opt, business.ErrInvalid
		}
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			return opt, business.ErrInvalid
		}
		opt.Limit = n
	}
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 1_000_000 {
			return opt, business.ErrInvalid
		}
		opt.Offset = n
	}
	opt.Query = q.Get("q")
	opt.State = q.Get("state")
	opt.Sort = q.Get("sort")
	return opt, nil
}
