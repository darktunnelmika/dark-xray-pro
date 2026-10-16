//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestRegistryScopedUsageInterruptionIsIdempotentAndPrivate(t *testing.T) {
	s, c, _, epoch := readyUsageServer(t)
	path := "/api/v1/agent/native-usage/interrupted?node_id=" + epoch.NodeID + "&installation_id=" + epoch.InstallationID
	body, _ := json.Marshal(uc.Interruption{EpochID: epoch.ID})
	call := func(handler http.Handler, token string, version uint16, raw []byte) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		r.TLS = &tls.ConnectionState{Version: version}
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	for _, token := range []string{"", ownerToken, otherToken} {
		if w := call(s.AgentHandler(), token, tls.VersionTLS13, body); w.Code != 401 {
			t.Fatal("unregistered caller changed usage hold", w.Code)
		}
	}
	if w := call(s.Handler(), nodeToken, tls.VersionTLS13, body); w.Code != 404 {
		t.Fatal("public listener exposes interruption", w.Code)
	}
	if w := call(s.AgentHandler(), nodeToken, tls.VersionTLS12, body); w.Code != 426 {
		t.Fatal("old TLS accepted", w.Code)
	}
	for _, raw := range []string{`{"epoch_id":"bad"}`, `{"epoch_id":"` + epoch.ID + `","client_id":"other"}`, `{"epoch_id":"` + epoch.ID + `","epoch_id":"` + epoch.ID + `"}`} {
		if w := call(s.AgentHandler(), nodeToken, tls.VersionTLS13, []byte(raw)); w.Code != 400 {
			t.Fatal("malformed interruption accepted", w.Code)
		}
	}
	a, e := s.business.ClientUsage(context.Background(), c.ID)
	if e != nil || !a.Known || a.Holds != 0 {
		t.Fatal("invalid notices changed account", a, e)
	}
	for i := 0; i < 2; i++ {
		w := call(s.AgentHandler(), nodeToken, tls.VersionTLS13, body)
		if w.Code != 200 {
			t.Fatal("valid interruption rejected", w.Code, w.Body.String())
		}
		var receipt uc.InterruptionReceipt
		if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || receipt.EpochID != epoch.ID || !receipt.ReconciliationRequired {
			t.Fatal("invalid interruption receipt")
		}
	}
	a, e = s.business.ClientUsage(context.Background(), c.ID)
	if e != nil || a.Known || a.Eligible || a.Holds != 1 || a.Total != 0 {
		t.Fatal("interruption duplicated hold or fabricated bytes", a, e)
	}
	missing, _ := json.Marshal(uc.Interruption{EpochID: strings.Repeat("0", 32)})
	if w := call(s.AgentHandler(), nodeToken, tls.VersionTLS13, missing); w.Code != 404 {
		t.Fatal("unknown epoch created by notice", w.Code)
	}
	kept, e := s.business.Client(context.Background(), c.ID)
	if e != nil || kept.UUID != c.UUID {
		t.Fatal("notice changed customer identity", e)
	}
}
