package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

const inboundAuthoringBase = "/api/v1/workspace/native-inbound-plans"

func authoringAPIFixture(t *testing.T, protocol string) (*Server, business.NativeInboundPlanInput, *http.Cookie, string) {
	t.Helper()
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	in.EventID = "authoring-http-create-0001"
	in.Nodes = []business.NativeInboundPlanNode{{NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "198.51.100.20",
		DirectPort: 22501, DirectHosts: []string{"edge.example.org"}}}
	spec := &inboundspec.Spec{Version: 1, Protocol: protocol, Network: "ws", Security: "none", Settings: map[string]any{},
		Stream: map[string]any{"wsSettings": map[string]any{"path": "/authoring", "headers": map[string]any{"Authorization": "fixture-write-only-header"}}},
		Client: map[string]any{"security": "auto"}}
	if protocol == "vless" {
		spec.Network, spec.Security = "raw", "reality"
		spec.Settings = map[string]any{"decryption": "none"}
		spec.Stream = map[string]any{}
		spec.Client = map[string]any{"fp": "chrome"}
		spec.Reality = map[string]any{"target": "www.example.org:443", "serverNames": []any{"www.example.org"}, "shortIds": []any{"ab12"}}
	}
	// These legacy mirrors deliberately disagree. The typed specification is
	// authoritative and must normalize them before persistence or feedback.
	in.Profile = business.InboundInput{ID: 501, Name: "HTTP authoring fixture", Transport: "grpc", Security: "tls",
		GRPCService: "stale-service", RealityTarget: "stale.example:443", ServerNames: []string{"stale.example"}, ShortIDs: []string{"ffff"}, Spec: spec}
	return s, in, cookie, csrf
}

func cloneAuthoringAPIInput(t *testing.T, input business.NativeInboundPlanInput) business.NativeInboundPlanInput {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var result business.NativeInboundPlanInput
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func requireAuthoringPublicResponse(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for _, value := range []string{"-----BEGIN", "fixture-write-only-header", `"privateKey":`, `"mldsa65Seed":`, `"echServerKeys":`, `"certificate":`, `"key":`, `"config":`, `"manifest":`} {
		if strings.Contains(response.Body.String(), value) {
			t.Fatal("authoring response exposed private material", response.Code, value)
		}
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authoring response is cacheable")
	}
}

func TestInboundAuthoringAPICapabilityV4AndProtocolCatalog(t *testing.T) {
	s, _, cookie, _ := authoringAPIFixture(t, "vmess")
	response := browserRequest(t, s.Handler(), "GET", inboundAuthoringBase+"/capabilities", nil, cookie, "", "")
	var caps struct {
		Version                int                        `json:"version"`
		Available              bool                       `json:"available"`
		AuthoringSpec          bool                       `json:"authoring_spec"`
		ExplicitHubDestination bool                       `json:"explicit_hub_destination"`
		RuntimeApply           bool                       `json:"runtime_apply"`
		Protocols              []string                   `json:"protocols"`
		Catalog                []inboundspec.ProtocolInfo `json:"protocol_catalog"`
	}
	want := []string{"vless", "vmess", "trojan", "shadowsocks", "wireguard", "hysteria", "http", "mixed", "tunnel", "tun", "mtproto", "amneziawg", "tuic", "masque"}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &caps) != nil || caps.Version != 4 || !caps.Available || !caps.AuthoringSpec ||
		!caps.ExplicitHubDestination || caps.RuntimeApply || !reflect.DeepEqual(caps.Protocols, want) || len(caps.Catalog) != len(want) {
		t.Fatal("capability contract omitted authoring protocols or overstated activation", response.Code)
	}
	for index, item := range caps.Catalog {
		if item.Protocol != want[index] || len(item.Networks) == 0 || len(item.Securities) == 0 {
			t.Fatal("protocol catalog lost its per-protocol network/security contract", item.Protocol)
		}
		external := item.Protocol == "mtproto" || item.Protocol == "amneziawg" || item.Protocol == "tuic"
		if external && item.Engine != item.Protocol || !external && item.Engine != "xray" {
			t.Fatal("protocol catalog claims the wrong execution engine", item.Protocol)
		}
	}
	requireAuthoringPublicResponse(t, response)
}

func TestInboundAuthoringAPICreatePreviewReopenAndRedactedEdit(t *testing.T) {
	for _, protocol := range []string{"vmess", "vless"} {
		t.Run(protocol, func(t *testing.T) {
			s, in, cookie, csrf := authoringAPIFixture(t, protocol)
			call := func(method, path string, body any, status int) *httptest.ResponseRecorder {
				t.Helper()
				response := browserRequest(t, s.Handler(), method, path, body, cookie, csrf, "https://"+browserHost)
				requireAuthoringPublicResponse(t, response)
				if response.Code != status {
					t.Fatalf("authoring request %s %s returned %d, expected %d: %s", method, path, response.Code, status, response.Body.String())
				}
				return response
			}
			previewResponse := call("POST", inboundAuthoringBase+"/preview", in, http.StatusOK)
			var preview business.NativeInboundPlanPreview
			if json.Unmarshal(previewResponse.Body.Bytes(), &preview) != nil || !preview.Valid || !preview.TopologyValidated || !preview.TopologyValidationOnly ||
				preview.RuntimeChanged || preview.Plan.Profile.Spec == nil || preview.Plan.Profile.Spec.Protocol != protocol || len(preview.Topology.Nodes) != 1 || preview.Topology.Nodes[0].NodeID != "de" {
				t.Fatal("preview lost selected protocol or added an implicit destination")
			}
			call("GET", inboundAuthoringBase+"/501", nil, http.StatusNotFound)
			createdResponse := call("POST", inboundAuthoringBase, in, http.StatusCreated)
			var created business.NativeInboundPlanReceipt
			if json.Unmarshal(createdResponse.Body.Bytes(), &created) != nil || created.Plan.Profile.Spec == nil || created.Plan.Profile.Spec.Protocol != protocol ||
				created.Plan.Profile.Enabled || created.RuntimeChanged || created.SubscriptionPublished || created.Plan.PlanRevision != 1 || created.Plan.InboundRevision != 1 {
				t.Fatal("create receipt did not retain a paused typed specification")
			}
			profile := created.Plan.Profile
			if profile.Transport != profile.Spec.Network || profile.Security != profile.Spec.Security || profile.GRPCService != "" || profile.Options != nil || len(profile.SpecSecretPaths) == 0 {
				t.Fatal("canonical mirrors or write-only presence metadata were lost")
			}
			if protocol == "vless" {
				if profile.Transport != "tcp" || profile.Security != "reality" || profile.RealityTarget != "www.example.org:443" ||
					!reflect.DeepEqual(profile.ServerNames, []string{"www.example.org"}) || !reflect.DeepEqual(profile.ShortIDs, []string{"ab12"}) ||
					profile.PublicKey == "" || profile.PublicKey != profile.Spec.Client["publicKey"] || profile.PublicKey == preview.Plan.Profile.PublicKey || profile.Spec.Client["fingerprint"] != "chrome" {
					t.Fatal("REALITY public identity or canonical fields are inconsistent")
				}
				if _, leaked := profile.Spec.Reality["privateKey"]; leaked {
					t.Fatal("saved REALITY private key was returned")
				}
			} else if profile.PublicKey != "" || profile.Security != "none" || profile.RealityTarget != "" || len(profile.ServerNames) != 0 || len(profile.ShortIDs) != 0 {
				t.Fatal("VMess inherited stale REALITY mirrors")
			}
			var loaded struct {
				Plan              business.NativeInboundPlan `json:"plan"`
				ReferencesCurrent bool                       `json:"references_current"`
			}
			if json.Unmarshal(call("GET", inboundAuthoringBase+"/501", nil, http.StatusOK).Body.Bytes(), &loaded) != nil || !loaded.ReferencesCurrent || !loaded.Plan.Editable || !reflect.DeepEqual(loaded.Plan.Profile, created.Plan.Profile) {
				t.Fatal("saved typed profile did not reopen identically")
			}
			edit := cloneAuthoringAPIInput(t, in)
			edit.EventID = "authoring-http-redacted-edit-0001"
			edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = 1, 1
			edit.Profile = loaded.Plan.Profile.InboundInput
			edit.Profile.Name = "Renamed through redacted HTTP form"
			if protocol == "vmess" {
				edit.Profile.Spec.Stream["wsSettings"].(map[string]any)["path"] = "/edited"
			}
			var updated business.NativeInboundPlanReceipt
			if json.Unmarshal(call("PUT", inboundAuthoringBase+"/501", edit, http.StatusOK).Body.Bytes(), &updated) != nil || updated.Plan.PlanRevision != 2 || updated.Plan.InboundRevision != 2 || updated.Plan.Profile.PublicKey != created.Plan.Profile.PublicKey {
				t.Fatal("redacted edit rotated keys or lost revisions")
			}
			// Inspect only the private in-process candidate to prove preserved
			// headers. HTTP previews intentionally exclude these bytes.
			check := cloneAuthoringAPIInput(t, edit)
			check.EventID = "authoring-http-private-check-0001"
			check.ExpectedPlanRevision, check.ExpectedInboundRevision = 2, 2
			check.Profile = updated.Plan.Profile.InboundInput
			privatePreview, err := s.business.PreviewNativeInboundPlan(context.Background(), s.inboundKey, check)
			if err != nil || len(privatePreview.Topology.Nodes) != 1 {
				t.Fatal("saved private profile cannot compile after redacted edit", err)
			}
			if protocol == "vmess" && !bytes.Contains(privatePreview.Topology.Nodes[0].Candidate.Config, []byte("fixture-write-only-header")) {
				t.Fatal("redacted HTTP edit dropped the private transport header")
			}
			var replay business.NativeInboundPlanReceipt
			if json.Unmarshal(call("POST", inboundAuthoringBase, in, http.StatusOK).Body.Bytes(), &replay) != nil || !replay.Replayed || !reflect.DeepEqual(replay.Plan, created.Plan) {
				t.Fatal("create retry did not return the original receipt")
			}
			legacy := cloneAuthoringAPIInput(t, check)
			legacy.EventID = "authoring-http-legacy-edit-0001"
			legacy.Profile.Spec = nil
			legacy.Profile.Transport, legacy.Profile.Security = "tcp", "reality"
			legacy.Profile.RealityTarget = "www.example.org:443"
			legacy.Profile.ServerNames, legacy.Profile.ShortIDs = []string{"www.example.org"}, []string{"ab12"}
			call("PUT", inboundAuthoringBase+"/501", legacy, http.StatusConflict)
		})
	}
}

func TestInboundAuthoringAPIInvalidNestedInputIsAtomic(t *testing.T) {
	s, original, cookie, csrf := authoringAPIFixture(t, "vmess")
	for _, item := range []struct {
		name string
		edit func(*business.NativeInboundPlanInput)
	}{
		{"wrong_security", func(in *business.NativeInboundPlanInput) { in.Profile.Spec.Security = "reality" }},
		{"unknown_nested", func(in *business.NativeInboundPlanInput) {
			in.Profile.Spec.Stream["wsSettings"].(map[string]any)["unknownOption"] = "private-invalid-marker"
		}},
		{"managed_users", func(in *business.NativeInboundPlanInput) {
			if in.Profile.Spec.Settings == nil {
				in.Profile.Spec.Settings = map[string]any{}
			}
			in.Profile.Spec.Settings["clients"] = []any{map[string]any{"id": "private-invalid-marker"}}
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			input := cloneAuthoringAPIInput(t, original)
			item.edit(&input)
			for _, path := range []string{inboundAuthoringBase + "/preview", inboundAuthoringBase} {
				response := browserRequest(t, s.Handler(), "POST", path, input, cookie, csrf, "https://"+browserHost)
				if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "private-invalid-marker") {
					t.Fatal("invalid nested specification was accepted or reflected", response.Code)
				}
			}
			if response := browserRequest(t, s.Handler(), "GET", inboundAuthoringBase+"/501", nil, cookie, "", ""); response.Code != http.StatusNotFound {
				t.Fatal("failed authoring request persisted a partial plan", response.Code)
			}
		})
	}
	// The same event/id remain usable after validation failure: no reservation,
	// partial profile, or event receipt may have escaped a failed transaction.
	created := browserRequest(t, s.Handler(), "POST", inboundAuthoringBase, original, cookie, csrf, "https://"+browserHost)
	if created.Code != http.StatusCreated {
		t.Fatal("failed authoring request consumed an ID or event", created.Code)
	}
	var receipt business.NativeInboundPlanReceipt
	if json.Unmarshal(created.Body.Bytes(), &receipt) != nil {
		t.Fatal("invalid create receipt")
	}
	edit := cloneAuthoringAPIInput(t, original)
	edit.EventID = "authoring-http-invalid-edit-0001"
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = 1, 1
	edit.Profile = receipt.Plan.Profile.InboundInput
	edit.Profile.Spec.Stream["wsSettings"].(map[string]any)["unknownOption"] = true
	if response := browserRequest(t, s.Handler(), "PUT", inboundAuthoringBase+"/501", edit, cookie, csrf, "https://"+browserHost); response.Code != http.StatusUnprocessableEntity {
		t.Fatal("unknown nested edit accepted", response.Code)
	}
	response := browserRequest(t, s.Handler(), "GET", inboundAuthoringBase+"/501", nil, cookie, "", "")
	var reopened struct {
		Plan business.NativeInboundPlan `json:"plan"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &reopened) != nil || reopened.Plan.PlanRevision != 1 || reopened.Plan.InboundRevision != 1 {
		t.Fatal("failed edit changed current revisions", response.Code)
	}
}

func TestInboundAuthoringAPITLSCertificateRowsRetainSecretsAcrossReorderAndDelete(t *testing.T) {
	s, in, cookie, csrf := authoringAPIFixture(t, "vless")
	credentials := []*business.TLSCredentials{tlsAPICredentials(t), tlsAPICredentials(t), tlsAPICredentials(t)}
	ids := []string{strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32)}
	rows := make([]any, len(credentials))
	for index, material := range credentials {
		rows[index] = map[string]any{"id": ids[index], "certificate": strings.Split(strings.TrimSpace(material.CertificatePEM), "\n"),
			"key": strings.Split(strings.TrimSpace(material.PrivateKeyPEM), "\n"), "usage": "encipherment"}
	}
	in.Profile.Spec = &inboundspec.Spec{Version: 1, Protocol: "vless", Network: "tcp", Security: "tls", Settings: map[string]any{"decryption": "none"},
		TLS: map[string]any{"serverName": "www.example.org", "certificates": rows}, Client: map[string]any{"serverName": "www.example.org"}}
	call := func(method, path string, body any, status int) *httptest.ResponseRecorder {
		t.Helper()
		response := browserRequest(t, s.Handler(), method, path, body, cookie, csrf, "https://"+browserHost)
		requireAuthoringPublicResponse(t, response)
		if response.Code != status {
			t.Fatalf("TLS row operation returned %d, expected %d: %s", response.Code, status, response.Body.String())
		}
		return response
	}
	var saved business.NativeInboundPlanReceipt
	if json.Unmarshal(call("POST", inboundAuthoringBase, in, http.StatusCreated).Body.Bytes(), &saved) != nil {
		t.Fatal("invalid TLS create receipt")
	}
	publicRows := saved.Plan.Profile.Spec.TLS["certificates"].([]any)
	if len(publicRows) != 3 || publicRows[0].(map[string]any)["id"] != ids[0] || len(saved.Plan.Profile.SpecSecretPaths) < 6 {
		t.Fatal("public certificate list lost stable IDs or secret-presence metadata")
	}
	assertPrivateOrder := func(plan business.NativeInboundPlan, order []int) {
		t.Helper()
		check := cloneAuthoringAPIInput(t, in)
		check.EventID = "authoring-http-cert-private-check"
		check.ExpectedPlanRevision, check.ExpectedInboundRevision = plan.PlanRevision, plan.InboundRevision
		check.Profile = plan.Profile.InboundInput
		preview, err := s.business.PreviewNativeInboundPlan(context.Background(), s.inboundKey, check)
		if err != nil || len(preview.Topology.Nodes) != 1 {
			t.Fatal("redacted TLS profile cannot compile", err)
		}
		var config struct {
			Inbounds []struct {
				Stream struct {
					TLS struct {
						Certificates []map[string]any `json:"certificates"`
					} `json:"tlsSettings"`
				} `json:"streamSettings"`
			} `json:"inbounds"`
		}
		if json.Unmarshal(preview.Topology.Nodes[0].Candidate.Config, &config) != nil || len(config.Inbounds) != 1 || len(config.Inbounds[0].Stream.TLS.Certificates) != len(order) {
			t.Fatal("TLS engine candidate lost certificates")
		}
		for index, expected := range order {
			certificate := config.Inbounds[0].Stream.TLS.Certificates[index]
			if _, leaked := certificate["id"]; leaked {
				t.Fatal("authoring row IDs leaked into engine config")
			}
			join := func(value any) string {
				var lines []string
				for _, line := range value.([]any) {
					lines = append(lines, line.(string))
				}
				return strings.Join(lines, "\n")
			}
			if join(certificate["certificate"]) != strings.TrimSpace(credentials[expected].CertificatePEM) || join(certificate["key"]) != strings.TrimSpace(credentials[expected].PrivateKeyPEM) {
				t.Fatal("TLS key followed the former array index instead of its certificate row")
			}
		}
	}
	assertPrivateOrder(saved.Plan, []int{0, 1, 2})
	edit := cloneAuthoringAPIInput(t, in)
	edit.EventID = "authoring-http-cert-reorder-0001"
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = 1, 1
	edit.Profile = saved.Plan.Profile.InboundInput
	edit.Profile.Spec.TLS["certificates"] = []any{publicRows[2], publicRows[0], publicRows[1]}
	if json.Unmarshal(call("PUT", inboundAuthoringBase+"/501", edit, http.StatusOK).Body.Bytes(), &saved) != nil {
		t.Fatal("invalid TLS reorder receipt")
	}
	assertPrivateOrder(saved.Plan, []int{2, 0, 1})
	publicRows = saved.Plan.Profile.Spec.TLS["certificates"].([]any)
	edit.EventID = "authoring-http-cert-delete-0001"
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = saved.Plan.PlanRevision, saved.Plan.InboundRevision
	edit.Profile = saved.Plan.Profile.InboundInput
	edit.Profile.Spec.TLS["certificates"] = []any{publicRows[0], publicRows[2]}
	if json.Unmarshal(call("PUT", inboundAuthoringBase+"/501", edit, http.StatusOK).Body.Bytes(), &saved) != nil {
		t.Fatal("invalid TLS deletion receipt")
	}
	assertPrivateOrder(saved.Plan, []int{2, 1})
	var loaded struct {
		Plan business.NativeInboundPlan `json:"plan"`
	}
	if json.Unmarshal(call("GET", inboundAuthoringBase+"/501", nil, http.StatusOK).Body.Bytes(), &loaded) != nil || len(loaded.Plan.Profile.Spec.TLS["certificates"].([]any)) != 2 {
		t.Fatal("deleted certificate returned on reopen")
	}
	edit.EventID = "authoring-http-cert-clear-key-0001"
	edit.ExpectedPlanRevision, edit.ExpectedInboundRevision = saved.Plan.PlanRevision, saved.Plan.InboundRevision
	edit.Profile = loaded.Plan.Profile.InboundInput
	edit.Profile.Spec.TLS["certificates"].([]any)[0].(map[string]any)["key"] = []any{}
	call("PUT", inboundAuthoringBase+"/501", edit, http.StatusUnprocessableEntity)
	if json.Unmarshal(call("GET", inboundAuthoringBase+"/501", nil, http.StatusOK).Body.Bytes(), &loaded) != nil || loaded.Plan.PlanRevision != saved.Plan.PlanRevision {
		t.Fatal("failed explicit key clearing changed the plan")
	}
}
