package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func TestOnlyAgentRoutesReachIsolatedTLSListener(t *testing.T) {
	server, _, _, _ := newLab(t)
	admin := server.Handler()
	ticket := request(t, admin, "POST", "/api/v1/admin/nodes/invites", ownerToken,
		map[string]string{"node_id": "new-lab-01", "name": "New independent Node"})
	if ticket.Code != http.StatusCreated {
		t.Fatalf("owner invite failed: %d %s", ticket.Code, ticket.Body.String())
	}
	var invite noderegistry.PairTicket
	if err := json.Unmarshal(ticket.Body.Bytes(), &invite); err != nil {t.Fatal(err)}

	rawHTTP := httptest.NewRequest(http.MethodPost, "/api/v1/agent/pulse", bytes.NewReader(nil))
	withoutTLS := httptest.NewRecorder()
	server.AgentHandler().ServeHTTP(withoutTLS, rawHTTP)
	if withoutTLS.Code != http.StatusUpgradeRequired {
		t.Fatalf("plaintext public handler accepted: %d", withoutTLS.Code)
	}

	tlsServer := httptest.NewUnstartedServer(server.AgentHandler())
	tlsServer.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	tlsServer.StartTLS()
	defer tlsServer.Close()
	sum := sha256.Sum256(tlsServer.Certificate().Raw)
	pin := hex.EncodeToString(sum[:])
	ca := pem.EncodeToMemory(&pem.Block{Type:"CERTIFICATE",Bytes:tlsServer.Certificate().Raw})
	client, endpoint, err := agenttransport.New(agenttransport.Config{
		Endpoint:tlsServer.URL,PinSHA256:pin,CABundle:ca,
	})
	if err != nil { t.Fatal(err) }
	defer client.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := agenttransport.Probe(ctx, client, endpoint); err != nil {t.Fatal(err)}

	for _, action := range []struct{method,path string}{
		{"GET", "/api/v1/admin/nodes"},
		{"POST", "/api/v1/admin/nodes/invites"},
		{"GET", "/api/v1/fleet"},
		{"GET", "/"},
		{"POST", "/api/v1/agent/health"},
		{"POST", "/api/v1/admin/nodes/invites"},
	} {
		req, err := http.NewRequestWithContext(ctx, action.method, endpoint+action.path, nil)
		if err != nil {t.Fatal(err)}
		req.Header.Set("Authorization", "Bearer "+ownerToken)
		res, err := client.Do(req)
		if err != nil {t.Fatal(err)}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("TLS agent surface leaked %s %s with HTTP %d", action.method, action.path, res.StatusCode)
		}
	}
	identity := map[string]string{
		"pair_code": invite.PairCode, "node_id": "new-lab-01",
		"installation_id": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"agent_token": nodeToken,
	}
	post := func(path, token string, payload any) int {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {t.Fatal(err)}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+path, bytes.NewReader(raw))
		if err != nil {t.Fatal(err)}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {req.Header.Set("Authorization","Bearer "+token)}
		res,err:=client.Do(req)
		if err != nil {t.Fatal(err)}
		defer res.Body.Close()
		return res.StatusCode
	}
	if got := post("/api/v1/agent/enroll","",identity); got != http.StatusOK {
		t.Fatalf("legitimate HTTPS pairing failed: %d", got)
	}
	pulse := telemetry.Pulse{
		NodeID:"new-lab-01",InstallationID:identity["installation_id"],
		BootID:"cccccccccccccccccccccccccccccccc",Sequence:1,
	}
	if got := post("/api/v1/agent/pulse",ownerToken,pulse); got != http.StatusUnauthorized {
		t.Fatalf("owner token impersonated Agent on WAN TLS: %d",got)
	}
	if got := post("/api/v1/agent/pulse",nodeToken,pulse); got != http.StatusNoContent {
		t.Fatalf("valid Agent pulse failed: %d",got)
	}
	if got := post("/api/v1/agent/pulse",nodeToken,pulse); got != http.StatusConflict {
		t.Fatalf("replayed Pulse accepted: %d",got)
	}
	snapshot := server.snapshot()
	if snapshot.Total != 1 || snapshot.Online != 1 || snapshot.Nodes[0].Name != "New independent Node" {
		t.Fatalf("fleet state corrupt after remote HTTPS pairing: %+v",snapshot)
	}
}
