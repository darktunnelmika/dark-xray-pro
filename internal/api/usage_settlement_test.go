//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

func TestUsageSettlementRegistryScopePublicDenialAndExactReceipt(t *testing.T) {
	s, c, _, epoch := readyUsageServer(t)
	ctx := context.Background()
	box, err := uc.Open(uc.Options{Directory: filepath.Join(t.TempDir(), "box"), Key: bytes.Repeat([]byte{3}, 32), Epoch: epoch, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	defer box.Close()
	r, err := box.Observe([]uc.Usage{{ClientID: c.ID}}, false)
	if err != nil {
		t.Fatal(err)
	}
	p := usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}
	ack, err := s.business.UsageLedger().Accept(ctx, p, r)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/v1/agent/native-usage/settled?node_id=" + epoch.NodeID + "&installation_id=" + epoch.InstallationID
	call := func(h http.Handler, token string, version uint16, raw []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", path, bytes.NewReader(raw))
		req.TLS = &tls.ConnectionState{Version: version}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	body, _ := json.Marshal(ack)
	for _, token := range []string{"", ownerToken, otherToken} {
		if w := call(s.AgentHandler(), token, tls.VersionTLS13, body); w.Code != 401 {
			t.Fatal("foreign caller", w.Code)
		}
	}
	if w := call(s.Handler(), nodeToken, tls.VersionTLS13, body); w.Code != 404 {
		t.Fatal("public seal endpoint", w.Code)
	}
	if w := call(s.AgentHandler(), nodeToken, tls.VersionTLS12, body); w.Code != 426 {
		t.Fatal("old TLS", w.Code)
	}
	if w := call(s.AgentHandler(), nodeToken, tls.VersionTLS13, body); w.Code != 409 {
		t.Fatal("open epoch sealed by query", w.Code)
	}
	for _, raw := range []string{`{"epoch_id":"bad"}`, `{"sealed":true}`, `{"epoch_id":"x","epoch_id":"y"}`, strings.Repeat(" ", 1025)} {
		if w := call(s.AgentHandler(), nodeToken, tls.VersionTLS13, []byte(raw)); w.Code != 400 && w.Code != 413 {
			t.Fatal("invalid settlement accepted", w.Code)
		}
	}
	final, err := box.Observe(r.Users, true)
	if err != nil {
		t.Fatal(err)
	}
	ack, err = s.business.UsageLedger().Accept(ctx, p, final)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(ack)
	for i := 0; i < 2; i++ {
		w := call(s.AgentHandler(), nodeToken, tls.VersionTLS13, body)
		var proof uc.SettlementReceipt
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &proof) != nil || !proof.Sealed || proof.Receipt != ack {
			t.Fatal("exact seal missing", w.Code, w.Body.String())
		}
	}
}
