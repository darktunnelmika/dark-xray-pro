package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

type subscriptionMetadataResponse struct {
	ix.SubscriptionView
	Enabled     bool  `json:"enabled"`
	ViewVersion int   `json:"view_version"`
	ServerTime  int64 `json:"server_time_unix_milli"`
}

func subscriptionViewAPIFixture(t *testing.T) (*Server, *http.Cookie, string, business.Client) {
	t.Helper()
	s, _, cookie, csrf := creationPlanAPIFixture(t)
	ctx := context.Background()
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	client, err := s.business.CreateClient(ctx, business.NewClient{
		EventID: "subscription-view-client-create-0001", ClientID: "subscription-view-client",
		RepresentativeID: "owner", Name: "Subscription metadata contract",
		DurationDays: 30, VolumeBytes: 8 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, cookie, csrf, client
}

// The metadata API fixture uses only temporary SQLite state and synthetic
// authenticated receipts. Real process/traffic acceptance remains covered by
// TestManagedInboundRealOwnerToAgentAccountingSiblingsAndStop.
func applySubscriptionViewFixture(t *testing.T, s *Server, client business.Client) ix.Observation {
	t.Helper()
	ctx := context.Background()
	if err := s.business.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	s.inboundExecution = &inboundExecutionService{}
	plan := business.NativeInboundPlanInput{
		EventID: "subscription-view-plan-create-0001", Reason: "Isolated metadata contract fixture",
		Profile: business.InboundInput{ID: 501, Name: "Subscription contract", Spec: &inboundspec.Spec{
			Version: 1, Protocol: "vless", Network: "tcp", Security: "none", Settings: map[string]any{"decryption": "none"},
		}},
		Nodes: []business.NativeInboundPlanNode{{
			NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20",
			DirectPort: 21185, DirectHosts: []string{"de.example.org"}, TunnelHosts: []string{},
		}},
	}
	if _, err := s.business.SaveNativeInboundPlan(ctx, s.inboundKey, plan); err != nil {
		t.Fatal(err)
	}
	state, err := s.business.InboundExecution(ctx, s.inboundKey, 501)
	if err != nil {
		t.Fatal(err)
	}
	command := ix.CommandInput{
		EventID: "subscription-view-activate-0001", Action: "activate",
		ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision,
		ClientIDs: []string{client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Accept isolated metadata fixture",
	}
	preview, err := s.business.PreviewInboundExecution(ctx, s.inboundKey, "owner", 501, command)
	if err != nil || !preview.Valid {
		t.Fatalf("fixture preview: %v %#v", err, preview.Blockers)
	}
	command.ReviewSHA = preview.ReviewSHA
	if _, err := s.business.QueueInboundExecution(ctx, s.inboundKey, "owner", 501, command); err != nil {
		t.Fatal(err)
	}
	bundle, found, err := s.business.InboundExecutionBundle(ctx, s.inboundKey, "de", strings.Repeat("b", 32))
	if err != nil || !found || len(bundle.NativeConfig) == 0 || len(bundle.Principals) != 1 {
		t.Fatal("fixture bundle unavailable", err)
	}
	binarySHA := strings.Repeat("b", 64)
	member, err := json.Marshal(ix.ProcessMember{
		BootID: "00000000-0000-0000-0000-000000000001", PID: 1, StartTicks: 1,
		BinarySHA: binarySHA, ConfigSHA: ix.BytesSHA(bundle.NativeConfig),
	})
	if err != nil {
		t.Fatal(err)
	}
	group, err := json.Marshal(map[string]string{"xray": string(member)})
	if err != nil {
		t.Fatal(err)
	}
	epoch := ix.EpochInput{
		EpochID: "subscription-view-epoch-0001", NodeID: bundle.NodeID, InstallationID: bundle.InstallationID,
		Generation: bundle.Generation, ConfigSHA: bundle.ConfigSHA, ManifestSHA: bundle.ManifestSHA,
		BinarySHAs: map[string]string{"xray": binarySHA}, ProcessIdentity: string(group),
	}
	if _, err := s.business.RegisterInboundExecutionEpoch(ctx, s.inboundKey, epoch); err != nil {
		t.Fatal(err)
	}
	report := ix.UsageReport{
		EpochID: epoch.EpochID, NodeID: epoch.NodeID, InstallationID: epoch.InstallationID,
		Generation: epoch.Generation, Sequence: 1, ProcessIdentity: epoch.ProcessIdentity,
		Counters: []ix.Counter{{PrincipalID: bundle.Principals[0].ID, Upload: 0, Download: 0}},
	}
	if _, err := s.business.AcceptInboundExecutionUsage(ctx, s.inboundKey, report); err != nil {
		t.Fatal(err)
	}
	observation := ix.Observation{
		NodeID: bundle.NodeID, InstallationID: bundle.InstallationID, Generation: bundle.Generation,
		OperationID: bundle.OperationID, ConfigSHA: bundle.ConfigSHA, ManifestSHA: bundle.ManifestSHA,
		State: "active", EpochID: epoch.EpochID, ObservedUnixNano: time.Now().UnixNano(),
		LeaseExpires: time.Now().Add(25 * time.Second).UnixNano(), ProcessReady: true,
		HealthState: "healthy", AccountingState: "accepted",
	}
	if err := s.business.ObserveInboundExecution(ctx, s.inboundKey, observation); err != nil {
		t.Fatal(err)
	}
	return observation
}

func TestManagedInboundSubscriptionMetadataVersionsAndRetainedDownload(t *testing.T) {
	s, cookie, csrf, client := subscriptionViewAPIFixture(t)
	observation := applySubscriptionViewFixture(t, s, client)
	base := "/api/v1/workspace/clients/" + client.ID + "/inbound-subscription"
	read := func(query string) subscriptionMetadataResponse {
		t.Helper()
		w := browserRequest(t, s.Handler(), "GET", base+query, nil, cookie, "", "")
		var response subscriptionMetadataResponse
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
			t.Fatal("metadata response", w.Code, w.Body.String())
		}
		return response
	}
	legacy, current := read(""), read("?view=2")
	if !legacy.Enabled || legacy.ViewVersion != 1 || !legacy.Available || !legacy.RuntimeReady ||
		legacy.ExpiresUnixNano != legacy.ReadinessExpiresUnixNano ||
		current.ViewVersion != 2 || !current.Available || !current.RuntimeReady || !current.PathVerified ||
		current.ReadinessExpiresUnixNano <= time.Now().UnixNano() ||
		current.ReadinessExpiresUnixNano > time.Now().Add(15*time.Second).UnixNano() ||
		current.ExpiresUnixNano <= current.ReadinessExpiresUnixNano || current.RouteCount != 1 || current.ServerTime <= 0 {
		t.Fatal("fresh metadata does not separate credential and readiness lifetimes")
	}
	w := browserRequest(t, s.Handler(), "POST", base+"/issue",
		map[string]any{"event_id": "subscription-view-issue-0001", "expected_generation": 0}, cookie, csrf, "https://"+browserHost)
	if w.Code != 200 {
		t.Fatal("issue", w.Code)
	}
	w = browserRequest(t, s.Handler(), "POST", base+"/reveal",
		map[string]any{"expected_generation": 1}, cookie, csrf, "https://"+browserHost)
	var secret struct {
		Path string `json:"path"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &secret) != nil || !strings.HasPrefix(secret.Path, "/api/v1/inbound-subscriptions/") {
		t.Fatal("reveal", w.Code)
	}
	download := func() *httptest.ResponseRecorder {
		t.Helper()
		out := httptest.NewRecorder()
		s.Handler().ServeHTTP(out, httptest.NewRequest("GET", secret.Path+"?format=uri", nil))
		return out
	}
	before := download()
	expectedUsage := "upload=0; download=0; total=" + strconv.FormatInt(client.VolumeBytes, 10) + "; expire=" + strconv.FormatInt(client.ExpiresUnix, 10)
	if before.Header().Get("Subscription-Userinfo") != expectedUsage {
		t.Fatal("missing standard entitlement header", before.Header())
	}
	head := httptest.NewRecorder()
	s.Handler().ServeHTTP(head, httptest.NewRequest("HEAD", secret.Path, nil))
	if head.Code != 200 || head.Header().Get("Subscription-Userinfo") != expectedUsage || head.Body.Len() != 0 {
		t.Fatal("HEAD usage refresh", head.Code, head.Header(), head.Body.String())
	}
	exported := browserRequest(t, s.Handler(), "GET", base+"/export", nil, cookie, "", "")
	if exported.Code != 200 || exported.Header().Get("Subscription-Userinfo") != expectedUsage || !bytes.Equal(exported.Body.Bytes(), before.Body.Bytes()) {
		t.Fatal("authenticated export metadata", exported.Code)
	}

	if before.Code != 200 || before.Header().Get("X-Subscription-Runtime-Ready") != "true" ||
		before.Header().Get("X-Subscription-Readiness-Valid-Until") == "0" {
		t.Fatal("fresh download lacks bounded readiness metadata", before.Code)
	}

	// The node reports a current non-ready state without a withdrawal, identity
	// change or uncertain accounting tail. This must not erase its valid URI.
	observation.State, observation.HealthState, observation.ProcessReady = "degraded", "unknown", false
	observation.LeaseExpires, observation.ObservedUnixNano = 0, time.Now().UnixNano()
	if err := s.business.ObserveInboundExecution(context.Background(), s.inboundKey, observation); err != nil {
		t.Fatal(err)
	}
	legacy, current = read(""), read("?view=2")
	if legacy.Available || legacy.RuntimeReady || legacy.PathVerified || legacy.RouteCount != 0 ||
		len(legacy.Formats) != 0 || legacy.ExpiresUnixNano != 0 || legacy.ReadinessExpiresUnixNano != 0 ||
		legacy.State != "active" || legacy.Generation != 1 {
		t.Fatal("legacy open tab could interpret retained credentials as running")
	}
	if !current.Available || current.RuntimeReady || current.PathVerified || current.ReadinessExpiresUnixNano != 0 ||
		current.RouteCount != 1 || len(current.Formats) == 0 || current.ExpiresUnixNano <= time.Now().UnixNano() ||
		current.State != "active" || current.Generation != 1 {
		t.Fatal("view2 did not retain authorized credentials without readiness")
	}
	after := download()
	if after.Code != 200 || !bytes.Equal(after.Body.Bytes(), before.Body.Bytes()) ||
		after.Header().Get("X-Subscription-SHA256") != before.Header().Get("X-Subscription-SHA256") ||
		after.Header().Get("X-Subscription-Valid-Until") != before.Header().Get("X-Subscription-Valid-Until") ||
		after.Header().Get("X-Subscription-Runtime-Ready") != "false" ||
		after.Header().Get("X-Subscription-Readiness-Valid-Until") != "0" ||
		after.Header().Get("Cache-Control") != "no-store, private" ||
		after.Header().Get("Subscription-Userinfo") != expectedUsage {
		t.Fatal("retained download changed credentials, expiry or cache/confidence contract", after.Code)
	}
	if until, err := strconv.ParseInt(after.Header().Get("X-Subscription-Valid-Until"), 10, 64); err != nil || until <= time.Now().UnixMilli() {
		t.Fatal("invalid retained document deadline")
	}
	w = browserRequest(t, s.Handler(), "GET", base+"/export?format=uri", nil, cookie, "", "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), after.Body.Bytes()) {
		t.Fatal("authorized owner export differs from retained token download", w.Code)
	}
	w = browserRequest(t, s.Handler(), "POST", base+"/revoke",
		map[string]any{"event_id": "subscription-view-revoke-0001", "expected_generation": 1}, cookie, csrf, "https://"+browserHost)
	if w.Code != 200 || download().Code != 404 {
		t.Fatal("retention bypassed explicit token revocation", w.Code)
	}
}

func TestManagedInboundSubscriptionMetadataNegotiationAndDisabledFeature(t *testing.T) {
	s, cookie, _, client := subscriptionViewAPIFixture(t)
	base := "/api/v1/workspace/clients/" + client.ID + "/inbound-subscription"
	for _, enabled := range []bool{false, true} {
		if enabled {
			applySubscriptionViewFixture(t, s, client)
		}
		for _, query := range []string{"", "?view=2"} {
			w := browserRequest(t, s.Handler(), "GET", base+query, nil, cookie, "", "")
			var response subscriptionMetadataResponse
			wantVersion := 1
			if query != "" {
				wantVersion = 2
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil ||
				response.Enabled != enabled || response.ClientID != client.ID || response.ViewVersion != wantVersion {
				t.Fatalf("negotiation enabled=%v query=%q status=%d", enabled, query, w.Code)
			}
		}
		for _, query := range []string{"?view=1", "?view=3", "?view=", "?view=2&view=2", "?view=2&view=1", "?view=02", "?view=2&extra=1", "?extra=1", "?view=%zz", "?view=2;extra=1"} {
			w := browserRequest(t, s.Handler(), "GET", base+query, nil, cookie, "", "")
			if w.Code != 400 {
				t.Fatalf("ambiguous metadata query accepted enabled=%v query=%q status=%d", enabled, query, w.Code)
			}
		}
		w := browserRequest(t, s.Handler(), "GET", base+"?view=2", nil, nil, "", "")
		if w.Code != 401 {
			t.Fatal("negotiation exposed metadata without a session", w.Code)
		}
	}
}
