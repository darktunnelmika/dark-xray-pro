package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestCreationEndpointsPersistPublicPortsIPv6AndClientOverrides(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	in.Profile.Options = &xrayconfig.ProfileOptions{Client: &xrayconfig.ClientOptions{Fingerprint: "firefox", SpiderX: "/profile"}}
	in.Nodes[0].DirectHosts = []string{"2001:db8::10", "edge.example.org"}
	in.Nodes[0].DirectEndpoints = []NativeInboundEndpoint{
		{Address: "2001:0DB8:0:0:0:0:0:10", Port: 443, Label: "ورودی عمومی", Enabled: true, SNI: "www.example.org", Fingerprint: "safari", SpiderX: "/endpoint"},
		{Address: "edge.example.org", Port: 0, Enabled: true},
		{Address: "off.example.org", Port: 80, Label: "Retained disabled endpoint", Enabled: false},
	}
	in.Nodes[1].SwapEntryAddress = "2001:db8::20"
	before := planGeneration(t, s)
	preview, err := s.PreviewNativeInboundPlan(context.Background(), key, in)
	if err != nil {
		t.Fatal(err)
	}
	var public, inherited, disabled *NativeInboundEndpointPreview
	for i := range preview.Endpoints {
		e := &preview.Endpoints[i]
		if e.NodeID != "nl" || e.Path != "direct" {
			continue
		}
		switch e.Address {
		case "2001:db8::10":
			public = e
		case "edge.example.org":
			inherited = e
		case "off.example.org":
			disabled = e
		}
	}
	if public == nil || public.Port != 443 || public.ListenPort != 21185 || public.Fingerprint != "safari" || public.SpiderX != "/endpoint" || public.Label != "ورودی عمومی" {
		t.Fatal("public endpoint was not resolved independently", public)
	}
	if inherited == nil || inherited.Port != 21185 || inherited.Fingerprint != "firefox" || inherited.SpiderX != "/profile" {
		t.Fatal("profile defaults were not inherited", inherited)
	}
	if disabled == nil || disabled.Enabled {
		t.Fatal("disabled endpoint was lost or enabled")
	}
	raw, _ := json.Marshal(preview.Endpoints)
	for _, forbidden := range []string{"private", "uuid", "vless://"} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatal("preview leaked credential material", forbidden)
		}
	}
	created, err := s.SaveNativeInboundPlan(context.Background(), key, in)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := s.NativeInboundPlan(context.Background(), key, in.Profile.ID)
	if err != nil || !reflect.DeepEqual(loaded, created.Plan) {
		t.Fatal("endpoint metadata did not survive readback", err)
	}
	replay, err := s.SaveNativeInboundPlan(context.Background(), key, in)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(replay.Plan, created.Plan) {
		t.Fatal("endpoint replay changed result", err)
	}
	if planGeneration(t, s) != before || planCount(t, s, "native_inbound_targets") != 0 || planCount(t, s, "native_inbound_clients") != 0 {
		t.Fatal("endpoint authoring changed runtime")
	}
}

func TestCreationEndpointMetadataRequiresExplicitClear(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	in.Nodes[0].DirectEndpoints = []NativeInboundEndpoint{{Address: in.Nodes[0].DirectHosts[0], Port: 443, Enabled: true, Label: "Public endpoint"}}
	created, err := s.SaveNativeInboundPlan(context.Background(), key, in)
	if err != nil {
		t.Fatal(err)
	}
	edit := copiedPlan(in)
	edit.EventID = "endpoint-metadata-stale-001"
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = 1, 1
	edit.Nodes[0].DirectEndpoints = nil
	if _, err = s.SaveNativeInboundPlan(context.Background(), key, edit); !errors.Is(err, ErrConflict) {
		t.Fatal("older editor erased endpoint metadata", err)
	}
	edit.Nodes[0].DirectEndpoints = []NativeInboundEndpoint{}
	edit.EventID = "endpoint-metadata-clear-001"
	cleared, err := s.SaveNativeInboundPlan(context.Background(), key, edit)
	if err != nil || cleared.Plan.PlanRevision != 2 || cleared.Plan.InboundRevision != 1 || cleared.Plan.Profile.PublicKey != created.Plan.Profile.PublicKey {
		t.Fatal("explicit endpoint clear failed or changed identity", err)
	}
	for _, n := range cleared.Plan.Nodes {
		if n.NodeID == "nl" && (len(n.DirectEndpoints) != 0 || !reflect.DeepEqual(n.DirectHosts, in.Nodes[0].DirectHosts)) {
			t.Fatal("simple Host mirror was not retained")
		}
	}
	// Clearing the current value never rewrites the immutable earlier receipt.
	// Rollback readers must therefore inspect event envelopes as well as the
	// latest profile and plan, even when the current plan is legacy-compatible.
	var current, receipt []byte
	if err = s.db.QueryRow(`SELECT payload_enc FROM native_inbound_plans WHERE inbound_id=?`, in.Profile.ID).Scan(&current); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRow(`SELECT receipt_enc FROM native_inbound_plan_events WHERE event_id=?`, in.EventID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(current, []byte("DXPPLN2\x00")) || !bytes.HasPrefix(receipt, []byte("DXPPLN2\x00")) {
		t.Fatal("clear changed the immutable receipt's version boundary")
	}
	replayed, err := s.SaveNativeInboundPlan(context.Background(), key, in)
	if err != nil || !replayed.Replayed || !reflect.DeepEqual(replayed.Plan, created.Plan) {
		t.Fatal("clearing current metadata lost the original exact replay", err)
	}
}

func TestCreationEndpointValidationKeepsExactFieldAndRollsBack(t *testing.T) {
	cases := []struct {
		name, field, code string
		mutate            func(*NativeInboundPlanInput)
	}{
		{"mirror", "direct_endpoints", "endpoint_hosts_mismatch", func(in *NativeInboundPlanInput) { in.Nodes[0].DirectHosts = []string{"different.example.org"} }},
		{"sni", "direct_endpoints.0.sni", "endpoint_sni_invalid", func(in *NativeInboundPlanInput) { in.Nodes[0].DirectEndpoints[0].SNI = "wrong.example.org" }},
		{"fingerprint", "direct_endpoints.0.fingerprint", "client_fingerprint_invalid", func(in *NativeInboundPlanInput) { in.Nodes[0].DirectEndpoints[0].Fingerprint = "unsafe" }},
		{"spider", "direct_endpoints.0.spider_x", "client_spider_x_invalid", func(in *NativeInboundPlanInput) { in.Nodes[0].DirectEndpoints[0].SpiderX = "//external.example.org" }},
		{"label", "direct_endpoints.0.label", "endpoint_label_invalid", func(in *NativeInboundPlanInput) { in.Nodes[0].DirectEndpoints[0].Label = strings.Repeat("ف", 129) }},
		{"duplicate-effective", "direct_endpoints.1.address", "endpoint_duplicate", func(in *NativeInboundPlanInput) {
			e := in.Nodes[0].DirectEndpoints[0]
			e.Port = in.Nodes[0].DirectPort
			e.SNI = in.Profile.ServerNames[0]
			e.Fingerprint = "chrome"
			e.SpiderX = "/"
			e.Enabled = false
			in.Nodes[0].DirectEndpoints = append(in.Nodes[0].DirectEndpoints, e)
		}},
		{"disabled-path", "direct_endpoints", "disabled_path_has_endpoints", func(in *NativeInboundPlanInput) { in.Nodes[0].DirectPort = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, key, in := creationPlanFixture(t)
			in.Nodes[0].DirectEndpoints = []NativeInboundEndpoint{{Address: in.Nodes[0].DirectHosts[0], Enabled: true}}
			tc.mutate(&in)
			_, err := s.SaveNativeInboundPlan(context.Background(), key, in)
			var detail *NativeInboundPlanError
			if !errors.As(err, &detail) || detail.NodeID != "nl" || detail.Field != tc.field || detail.Code != tc.code {
				t.Fatalf("wrong bounded endpoint error: %v %#v", err, detail)
			}
			if planCount(t, s, "native_inbounds") != 0 || planCount(t, s, "native_inbound_plan_events") != 0 {
				t.Fatal("invalid endpoint partially persisted")
			}
		})
	}
}

func TestCreationDisabledEndpointsWaitForHostAndDifferentPublicPortsAreDistinct(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	in.Nodes[0].DirectHosts = []string{}
	in.Nodes[0].DirectEndpoints = []NativeInboundEndpoint{{Address: "edge.example.org", Enabled: false, Port: 443}, {Address: "edge.example.org", Enabled: false, Port: 2053}}
	preview, err := s.PreviewNativeInboundPlan(context.Background(), key, in)
	if err != nil || preview.Plan.Status != "WAITING_HOST" {
		t.Fatal("disabled endpoints became ready or same-host ports collided", err)
	}
	in.Nodes[0].DirectHosts = []string{"edge.example.org"}
	in.Nodes[0].DirectEndpoints[0].Enabled = true
	in.Nodes[0].DirectEndpoints[1].Enabled = true
	preview, err = s.PreviewNativeInboundPlan(context.Background(), key, in)
	if err != nil || preview.Plan.Status != "SAVED_DISABLED" {
		t.Fatal("distinct public ports failed", err)
	}
}

func TestCreationEndpointEnvelopeRejectsRollbackAndAuthenticatedDowngrade(t *testing.T) {
	s, key, in := creationPlanFixture(t)
	in.Nodes[0].DirectEndpoints = []NativeInboundEndpoint{{Address: in.Nodes[0].DirectHosts[0], Enabled: true, Port: 443}}
	created, err := s.SaveNativeInboundPlan(context.Background(), key, in)
	if err != nil {
		t.Fatal(err)
	}
	var enc []byte
	if err = s.db.QueryRow(`SELECT payload_enc FROM native_inbound_plans WHERE inbound_id=?`, in.Profile.ID).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(enc, []byte("DXPPLN2\x00")) {
		t.Fatal("endpoint plan used legacy envelope")
	}
	a, err := planAEAD(key)
	if err != nil {
		t.Fatal(err)
	}
	n := a.NonceSize()
	if _, err = a.Open(nil, enc[:n], enc[n:], []byte(planAAD(in.Profile.ID, 1))); err == nil {
		t.Fatal("old binary could read and drop endpoints")
	}
	raw, _ := json.Marshal(created.Plan)
	nonce := bytes.Repeat([]byte{9}, n)
	legacy := a.Seal(nonce, nonce, raw, []byte(planAAD(in.Profile.ID, 1)))
	var out NativeInboundPlan
	if err = openPlanValue(key, planAAD(in.Profile.ID, 1), legacy, &out); !errors.Is(err, ErrUnavailable) {
		t.Fatal("v1 authenticated payload smuggled new sections", err)
	}
	if err = openPlanValue(key, planAAD(in.Profile.ID, 2), enc, &out); !errors.Is(err, ErrUnavailable) {
		t.Fatal("revision rebinding accepted", err)
	}
	legacyPlan := created.Plan
	legacyPlan.Nodes = append([]NativeInboundPlanNode(nil), legacyPlan.Nodes...)
	for i := range legacyPlan.Nodes {
		legacyPlan.Nodes[i].DirectEndpoints = nil
		legacyPlan.Nodes[i].TunnelEndpoints = nil
	}
	raw, _ = json.Marshal(legacyPlan)
	nonce = append([]byte("DXPPLN2\x00"), 0x11, 0x22, 0x33, 0x44)
	legacy = a.Seal(nonce, nonce, raw, []byte(planAAD(in.Profile.ID, 1)))
	out = NativeInboundPlan{}
	if err = openPlanValue(key, planAAD(in.Profile.ID, 1), legacy, &out); err != nil || !reflect.DeepEqual(out, legacyPlan) {
		t.Fatal("legacy random nonce mistaken for a new plan envelope", err)
	}
}

func TestAuthoringEndpointReviewPreservesTransportAndClientDefaultsAfterReopen(t *testing.T) {
	for _, network := range []string{"grpc", "xhttp", "ws", "httpupgrade"} {
		t.Run(network, func(t *testing.T) {
			s, key, in := authoringPlanFixture(t)
			in.Nodes = in.Nodes[:1]
			in.Nodes[0].TunnelPort, in.Nodes[0].TunnelHosts = 0, nil
			in.Nodes[0].DirectHosts = []string{"edge.example.org", "other.example.org"}
			in.Nodes[0].DirectEndpoints = []NativeInboundEndpoint{
				{Address: "edge.example.org", Enabled: true, Port: 443},
				{Address: "other.example.org", Enabled: true, Port: 8443, Fingerprint: "safari"},
			}
			spec := in.Profile.Spec
			spec.Network = network
			spec.Client = map[string]any{"fingerprint": "firefox", "spiderX": "/profile"}
			settings := map[string]any{"path": "/saved-path", "host": "transport.example.org", "headers": map[string]any{"Authorization": "synthetic-private-header"}}
			if network == "grpc" {
				settings = map[string]any{"serviceName": "saved-service", "authority": "transport.example.org", "multiMode": true}
			}
			if network == "xhttp" {
				settings["mode"], settings["xPaddingBytes"] = "stream-up", "100-200"
			}
			if network == "ws" || network == "httpupgrade" {
				cert := businessTLSCredentials(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
				spec.Security, spec.Reality = "tls", nil
				spec.TLS = map[string]any{"serverName": "www.example.org", "certificates": []any{map[string]any{"certificate": strings.Split(strings.TrimSpace(cert.CertificatePEM), "\n"), "key": strings.Split(strings.TrimSpace(cert.PrivateKeyPEM), "\n")}}}
				delete(spec.Client, "spiderX")
			}
			spec.Stream = map[string]any{network + "Settings": settings}
			created, err := s.SaveNativeInboundPlan(context.Background(), key, in)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := s.NativeInboundPlan(context.Background(), key, in.Profile.ID)
			if err != nil || loaded.Profile.Options != nil {
				t.Fatal("spec plan lost authoring identity", err)
			}
			edit := copiedPlan(in)
			edit.Profile = loaded.Profile.InboundInput
			edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = created.Plan.PlanRevision, created.Plan.InboundRevision
			edit.EventID = "authoring-endpoint-review-" + network
			preview, err := s.PreviewNativeInboundPlan(context.Background(), key, edit)
			if err != nil || len(preview.Endpoints) != 2 {
				t.Fatal("reopened endpoint review failed", err)
			}
			inherited, overridden := preview.Endpoints[0], preview.Endpoints[1]
			spider := "/profile"
			if spec.Security == "tls" {
				spider = ""
			}
			if inherited.Transport != network || inherited.Port != 443 || inherited.ServerName != "www.example.org" || inherited.Fingerprint != "firefox" || inherited.SpiderX != spider || overridden.Fingerprint != "safari" || overridden.Port != 8443 {
				t.Fatal("review lost saved spec or per-endpoint client settings", inherited, overridden)
			}
			switch network {
			case "grpc":
				if inherited.GRPCService != "saved-service" || inherited.GRPCAuthority != "transport.example.org" || !inherited.GRPCMultiMode {
					t.Fatal("gRPC review lost saved settings", inherited)
				}
			case "xhttp":
				if inherited.XHTTP == nil || *inherited.XHTTP != (xrayconfig.XHTTPOptions{Path: "/saved-path", Host: "transport.example.org", Mode: "stream-up", XPaddingBytes: "100-200"}) {
					t.Fatal("XHTTP review lost saved settings", inherited)
				}
			case "ws":
				if inherited.WebSocket == nil || inherited.WebSocket.Path != "/saved-path" || inherited.WebSocket.Host != "transport.example.org" {
					t.Fatal("WebSocket review lost saved settings", inherited)
				}
			case "httpupgrade":
				if inherited.HTTPUpgrade == nil || inherited.HTTPUpgrade.Path != "/saved-path" || inherited.HTTPUpgrade.Host != "transport.example.org" {
					t.Fatal("HTTPUpgrade review lost saved settings", inherited)
				}
			}
			raw, err := json.Marshal(preview.Endpoints)
			if err != nil || bytes.Contains(raw, []byte("synthetic-private-header")) || bytes.Contains(raw, []byte("-----BEGIN")) || bytes.Contains(raw, []byte("privateKey")) {
				t.Fatal("endpoint review exposed private authoring material", err)
			}
		})
	}
}

func TestAuthoringEndpointDefaultsMatchExportSecurity(t *testing.T) {
	for _, security := range []string{"reality", "tls", "none"} {
		t.Run(security, func(t *testing.T) {
			s, key, in := authoringPlanFixture(t)
			in.Nodes = in.Nodes[:1]
			in.Nodes[0].TunnelPort, in.Nodes[0].TunnelHosts = 0, nil
			in.Nodes[0].DirectHosts = []string{"www.example.org"}
			spec := in.Profile.Spec
			spec.Security = security
			if security != "reality" {
				spec.Reality = nil
			}
			if security == "tls" {
				cert := businessTLSCredentials(t, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
				spec.TLS = map[string]any{"certificates": []any{map[string]any{"certificate": strings.Split(strings.TrimSpace(cert.CertificatePEM), "\n"), "key": strings.Split(strings.TrimSpace(cert.PrivateKeyPEM), "\n")}}}
			}
			result, err := s.SaveNativeInboundPlan(context.Background(), key, in)
			if err != nil {
				t.Fatal(err)
			}
			var ciphertext []byte
			if err = s.db.QueryRow(`SELECT profile_enc FROM native_inbounds WHERE id=?`, in.Profile.ID).Scan(&ciphertext); err != nil {
				t.Fatal(err)
			}
			private, err := openInboundProfile(key, ciphertext, in.Profile.ID)
			if err != nil {
				t.Fatal(err)
			}
			client, err := inboundspec.ExportClient(*private.Spec, "json", inboundspec.User{UUID: "8d5f5d49-923b-45c9-b53f-0c455dd516b4", Email: "synthetic@example.org"}, inboundspec.Endpoint{Address: "www.example.org", Port: 443})
			if err != nil {
				t.Fatal(err)
			}
			var generic map[string]any
			if json.Unmarshal(client, &generic) != nil {
				t.Fatal("invalid client export")
			}
			outbound := generic["outbounds"].([]any)[0].(map[string]any)
			stream := outbound["streamSettings"].(map[string]any)
			settings, _ := stream[security+"Settings"].(map[string]any)
			endpoint := previewPlanEndpoints(result.Plan)[0]
			if endpoint.ServerName != specString(settings, "serverName") || endpoint.Fingerprint != specString(settings, "fingerprint") || endpoint.SpiderX != specString(settings, "spiderX") {
				t.Fatal("review advertises different defaults from the actual client export", endpoint)
			}
		})
	}
}
