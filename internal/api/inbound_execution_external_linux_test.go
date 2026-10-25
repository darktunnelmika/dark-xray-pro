//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

// Worker-level tests prove actual encrypted user traffic. This complementary
// test proves that the *real authored* profile, generated identities and
// manifests cross the owner/Agent/group boundary without handcrafted adapters.
func TestManagedInboundRealExternalCompilerAgentLifecycle(t *testing.T) {
	core := managedTestCore(t)
	for _, protocol := range []string{"tuic", "amneziawg", "mtproto"} {
		t.Run(protocol, func(t *testing.T) {
			path := os.Getenv("DARK_PRO_TEST_ENGINE_" + strings.ToUpper(protocol))
			if path == "" {
				t.Skip("set the separately pinned external acceptance executable")
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(raw)
			external := map[string]inboundruntime.BinaryConfig{protocol: {Path: path, SHA256: hex.EncodeToString(hash[:])}}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			s, plan, cookie, csrf := creationPlanAPIFixture(t)
			dir, err := os.MkdirTemp("", "dxp-external-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			if err = s.business.EnableUsageLedger(ctx); err != nil {
				t.Fatal(err)
			}
			if err = s.EnableInboundExecution(InboundExecutionConfig{Directory: filepath.Join(dir, "hub"), Native: core, External: external}); err != nil {
				t.Fatal(err)
			}
			defer s.inboundExecution.local.Close()
			client, err := s.business.CreateClient(ctx, business.NewClient{EventID: "external-client-create-" + protocol, ClientID: "external-client", RepresentativeID: "owner", Name: "External compiler fixture", DurationDays: 30, VolumeBytes: 8 << 20})
			if err != nil {
				t.Fatal(err)
			}
			spec := &inboundspec.Spec{Version: 1, Protocol: protocol, Network: protocol, Security: "none", Settings: map[string]any{}}
			switch protocol {
			case "tuic":
				cert := tlsAPICredentials(t)
				spec.Security = "tls"
				spec.TLS = map[string]any{"serverName": "www.example.org", "alpn": []string{"h3"}, "certificates": []any{map[string]any{"certificate": strings.Split(strings.TrimSpace(cert.CertificatePEM), "\n"), "key": strings.Split(strings.TrimSpace(cert.PrivateKeyPEM), "\n")}}}
			case "amneziawg":
				spec.Settings["server"] = map[string]any{"privateKey": base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{42}, 32)), "subnetIp": "10.88.0.0", "subnetCidr": 24}
			case "mtproto":
				spec.Settings = map[string]any{"secret": "owned-integration-fixture-seed", "fakeTlsDomain": "example.test", "routeThroughXray": true, "outboundTag": "direct"}
			}
			plan.EventID = "external-create-plan-" + protocol
			plan.Profile = business.InboundInput{ID: 501, Name: "External " + protocol, Spec: spec}
			plan.Nodes = []business.NativeInboundPlanNode{{NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "127.0.0.1", DirectPort: managedTestPort(t), DirectHosts: []string{"127.0.0.1"}, TunnelHosts: []string{}}}
			request := func(method, path string, input, output any, status int) {
				t.Helper()
				w := browserRequest(t, s.Handler(), method, path, input, cookie, csrf, "https://"+browserHost)
				if w.Code != status {
					t.Fatalf("%s %s: HTTP%d %s", method, path, w.Code, w.Body.String())
				}
				if output != nil && json.Unmarshal(w.Body.Bytes(), output) != nil {
					t.Fatal("invalid API response")
				}
			}
			request("POST", "/api/v1/workspace/native-inbound-plans", plan, nil, 201)
			base := "/api/v1/workspace/native-inbounds/501/execution"
			command := func(action string) {
				t.Helper()
				var state ix.State
				request("GET", base, nil, &state, 200)
				input := ix.CommandInput{EventID: "external-" + action + "-" + protocol, Action: action, ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: []string{client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Real standalone compiler acceptance"}
				var preview ix.Preview
				request("POST", base+"/preview", input, &preview, 200)
				if !preview.Valid {
					t.Fatal("external review blocked", preview.Blockers)
				}
				input.ReviewSHA = preview.ReviewSHA
				request("POST", base+"/commands", input, nil, 202)
			}
			command("activate")
			agent := httptest.NewUnstartedServer(s.AgentHandler())
			agent.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			agent.StartTLS()
			defer agent.Close()
			worker, err := inboundruntime.Open(inboundruntime.Config{Directory: filepath.Join(dir, "node"), NodeID: "de", InstallationID: strings.Repeat("b", 32), Key: []byte(strings.Repeat("3", 64)), Native: core, External: external})
			if err != nil {
				t.Fatal(err)
			}
			defer worker.Close()
			exchange, err := inboundruntime.NewHTTPExchange(agent.Client(), agent.URL, strings.Repeat("3", 64), "de", strings.Repeat("b", 32))
			if err != nil {
				t.Fatal(err)
			}
			if err = worker.Step(ctx, exchange); err != nil {
				t.Fatal("compiled external activation", err)
			}
			var state ix.State
			request("GET", base, nil, &state, 200)
			if state.ObservedState != "active" || len(state.Targets) != 1 || !state.Targets[0].ProcessReady || state.Targets[0].HealthState != "engine_ready" {
				t.Fatal("external group lacks fresh readiness", state.ObservedState, state.Targets)
			}
			command("stop")
			if err = worker.Step(ctx, exchange); err != nil {
				t.Fatal("compiled external final", err)
			}
			if err = worker.Step(ctx, exchange); err != nil {
				t.Fatal("compiled external stop", err)
			}
			request("GET", base, nil, &state, 200)
			if state.ObservedState != "stopped" {
				t.Fatal("external final not reflected", state.ObservedState, state.Targets)
			}
		})
	}
}
