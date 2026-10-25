package api

import (
	"encoding/json"
	"net/http"
	"testing"

	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
)

func TestManagedInboundOwnerBoundaryAndDisabledState(t *testing.T) {
	s, _, cookie, csrf := creationPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbounds/501/execution"
	w := browserRequest(t, s.Handler(), "GET", base, nil, cookie, "", "")
	var state ix.State
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &state) != nil || state.Enabled || state.AccountingMode != ix.AccountingPerClient || state.ServerTimeMillis <= 0 || state.PublicationState != "withheld" || state.AllowedAccountingModes == nil || state.ClientIDs == nil || state.Targets == nil {
		t.Fatal("disabled managed state does not satisfy public UI contract", w.Code)
	}
	for _, path := range []string{base + "/preview", base + "/commands", base + "/clients", base + "/reconciliations/preview", base + "/reconciliations"} {
		for _, tc := range []struct {
			name, token string
			cookie      *http.Cookie
			want        int
		}{{"anonymous", "", nil, 401}, {"missing CSRF", "", cookie, 403}, {"wrong CSRF", "wrong", cookie, 403}, {"disabled", csrf, cookie, 503}} {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				w := browserRequest(t, s.Handler(), "POST", path, map[string]any{}, tc.cookie, tc.token, "https://"+browserHost)
				if w.Code != tc.want {
					t.Fatal("managed mutation boundary", w.Code, tc.want)
				}
			})
		}
	}
	for _, path := range []string{base, base + "/operations/unknown-event-0001", base + "/reconciliations/unknown-event-0001"} {
		w := browserRequest(t, s.Handler(), "GET", path, nil, nil, "", "")
		if w.Code != 401 {
			t.Fatal("managed read exposed without owner session", path, w.Code)
		}
	}
}
