package api

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

const testToken = "this-is-a-lab-only-random-looking-token-1234567890"

func TestHeartbeatAuthAndVisibleFleet(t *testing.T) {
    server, err := NewServer(telemetry.NewStore(0, nil), testToken)
    if err != nil { t.Fatal(err) }
    handler := server.Handler()
    sample := telemetry.Sample{
        NodeID: "de-01", Name: "Lab node", BootID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        Sequence: 1, Interface: "eth0", CPUCores: 4, CPUPercent: 20,
        RAMTotal: 1024, RAMUsed: 200,
    }
    payload, err := json.Marshal(sample)
    if err != nil { t.Fatal(err) }
    call := func(path, method, token string) *httptest.ResponseRecorder {
        t.Helper()
        req := httptest.NewRequest(method, path, bytes.NewReader(payload))
        if token != "" { req.Header.Set("Authorization", "Bearer "+token) }
        w := httptest.NewRecorder()
        handler.ServeHTTP(w, req)
        return w
    }
    if res := call("/api/v1/agent/heartbeat", "POST", ""); res.Code != 401 {
        t.Fatalf("expected unauthorized, got %d", res.Code)
    }
    if res := call("/api/v1/agent/heartbeat", "POST", testToken); res.Code != 204 {
        t.Fatalf("expected accepted, got %d: %s", res.Code, res.Body.String())
    }
    if res := call("/api/v1/agent/heartbeat", "POST", testToken); res.Code != 409 {
        t.Fatalf("expected sequence conflict, got %d", res.Code)
    }
    res := call("/api/v1/fleet", "GET", "")
    if res.Code != 200 { t.Fatal(res.Code) }
    var fleet telemetry.Fleet
    if err := json.Unmarshal(res.Body.Bytes(), &fleet); err != nil { t.Fatal(err) }
    if fleet.Total != 1 || fleet.Online != 1 || fleet.MetricScope != "physical_interface_not_billable" {
        t.Fatalf("bad fleet %+v", fleet)
    }
}

func TestHubRequiresSecret(t *testing.T) {
    if _, err := NewServer(telemetry.NewStore(0, nil), "abc"); err == nil {
        t.Fatal("must refuse short token")
    }
}
