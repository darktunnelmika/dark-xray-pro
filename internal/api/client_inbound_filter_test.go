package api

import (
	"net/http/httptest"
	"testing"
)

func TestClientInboundFilterStrictQuery(t *testing.T) {
	for _, query := range []string{"inbound_id=", "inbound_id=0", "inbound_id=-1", "inbound_id=01", "inbound_id=9007199254740992", "inbound_id=1&inbound_id=2"} {
		if _, e := clientSearchParams(httptest.NewRequest("GET", "/api/v1/workspace/clients?"+query, nil)); e == nil {
			t.Fatalf("accepted %s", query)
		}
	}
	q, e := clientSearchParams(httptest.NewRequest("GET", "/api/v1/workspace/clients?inbound_id=501", nil))
	if e != nil || q.InboundID != 501 {
		t.Fatalf("%+v %v", q, e)
	}
}
