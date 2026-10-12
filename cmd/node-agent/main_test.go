package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/agentidentity"
	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func TestLabDoesNotContactProductionOrRemoteNodes(t *testing.T) {
	accepted := []string{"http://127.0.0.1:2087"}
	for _, raw := range accepted {
		if _, err := checkLabEndpoint(raw); err != nil { t.Fatalf("%s: %v", raw, err) }
	}
	denied := []string{
		"http://old-hub.example:2087", "http://127.0.0.1:2088",
		"https://example.com", "http://localhost:2087",
		"http://127.0.0.1:2087/other", "http://127.0.0.1:2087?node=x",
	}
	for _, raw := range denied {
		if _, err := checkLabEndpoint(raw); err == nil {
			t.Fatalf("unsafe lab endpoint permitted: %s", raw)
		}
	}
}

func TestAgentGeneratedSecretAndHubEndToEndInMemoryHTTP(t *testing.T) {
	base := t.TempDir()
	now := time.Date(2026, 10, 12, 8, 0, 0, 0, time.UTC)
	reg, err := noderegistry.Open(filepath.Join(base, "private-hub", "registry.json"), func() time.Time{return now})
	if err != nil { t.Fatal(err) }
	ticket, err := reg.Issue("lab-01", "Fresh Node")
	if err != nil { t.Fatal(err) }
	identityPath := filepath.Join(base, "private-agent", "identity.json")
	identity, err := agentidentity.LoadOrCreate(identityPath, "lab-01", ticket.PairCode)
	if err != nil { t.Fatal(err) }

	fleet := telemetry.NewStore(15*time.Second,func()time.Time{return now})
	hub,err := api.NewServer(fleet,reg,"this-is-a-long-private-owner-lab-token-1234567890")
	if err != nil { t.Fatal(err) }
	server := httptest.NewServer(hub.Handler())
	defer server.Close()
	client := &http.Client{Timeout: 5*time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := enroll(ctx,client,server.URL,identity); err != nil {t.Fatal(err)}
	// A lost HTTP response can cause the identical request to be delivered twice.
	if err := enroll(ctx,client,server.URL,identity); err != nil {
		t.Fatalf("enrollment is not safely idempotent: %v",err)
	}
	identity,err=agentidentity.MarkPaired(identityPath,identity)
	if err!=nil { t.Fatal(err) }
	if _,err:=agentidentity.SecureRead(identityPath,"lab-01");err!=nil {t.Fatal(err)}
	sample:=telemetry.Sample{
		NodeID:"lab-01",InstallationID:identity.InstallationID,Name:"spoofed-name",
		BootID:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",Sequence:1,
		Interface:"ens3",CPUCores:4,CPUPercent:20,
		RAMTotal:8192,RAMUsed:1024,RXBytes:12345,TXBytes:6789,
	}
	if err:=transmit(ctx,client,server.URL,identity.AgentToken,sample);err!=nil {t.Fatal(err)}
	pulse:=telemetry.Pulse{NodeID:"lab-01",InstallationID:identity.InstallationID,
		BootID:sample.BootID,Sequence:1}
	if err:=sendPulse(ctx,client,server.URL,identity.AgentToken,pulse);err!=nil{
		t.Fatal("independent heartbeat failed:",err)
	}
	if err:=sendPulse(ctx,client,server.URL,identity.AgentToken,pulse);err==nil{
		t.Fatal("duplicate pulse incorrectly renewed online status")
	}
	response,err:=client.Get(server.URL+"/api/v1/fleet")
	if err!=nil {t.Fatal(err)}
	defer response.Body.Close()
	var doc telemetry.Fleet
	if err:=json.NewDecoder(response.Body).Decode(&doc);err!=nil {t.Fatal(err)}
	if doc.Total!=1||doc.Online!=1||doc.Nodes[0].Name!="Fresh Node" {
		t.Fatalf("Agent spoofed display name or Node not visible: %+v",doc)
	}
	// Wrong Node token cannot create a false Online state or a new Node.
	sample.NodeID="unpaired"
	if err:=transmit(ctx,client,server.URL,identity.AgentToken,sample);err==nil {
		t.Fatal("Agent illegally sent heartbeat for an unpaired logical Node")
	}
}
