package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func tlsAPICredentials(t *testing.T) *business.TLSCredentials {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "API TLS fixture"},
		DNSNames: []string{"www.example.org"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, leaf, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return &business.TLSCredentials{CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))}
}

func tlsPlanAPIFixture(t *testing.T) (*Server, business.NativeInboundPlanInput, *http.Cookie, string) {
	t.Helper()
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	in.Profile.Security, in.Profile.Transport = "tls", "ws"
	in.Profile.RealityTarget, in.Profile.GRPCService = "", ""
	in.Profile.ShortIDs = []string{}
	in.Profile.Options = &xrayconfig.ProfileOptions{WebSocket: &xrayconfig.WebSocketOptions{Path: "/socket", Host: "edge.example.org"}}
	in.TLSCredentials = tlsAPICredentials(t)
	return s, in, cookie, csrf
}

func requireTLSAPISecretFree(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for _, forbidden := range []string{"-----BEGIN", "certificate_pem", "private_key_pem", "tls_credentials", "reality_private_key", `"config":`} {
		if strings.Contains(response.Body.String(), forbidden) {
			t.Fatal("TLS HTTP response exposed credential/configuration field", forbidden, response.Code)
		}
	}
}

func TestCreationTLSAPIWriteOnlyPreviewReadbackRetentionRenewalAndReplay(t *testing.T) {
	s, in, cookie, csrf := tlsPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbound-plans"
	request := func(method, path string, body any, want int) *httptest.ResponseRecorder {
		t.Helper()
		w := browserRequest(t, s.Handler(), method, path, body, cookie, csrf, "https://"+browserHost)
		requireTLSAPISecretFree(t, w)
		if w.Code != want {
			t.Fatalf("TLS HTTP %s %s: status %d, expected %d", method, path, w.Code, want)
		}
		return w
	}
	capability := request("GET", base+"/capabilities", nil, http.StatusOK)
	var caps struct {
		Version                    int      `json:"version"`
		Securities                 []string `json:"securities"`
		Transports                 []string `json:"transports"`
		ProfileTLS                 bool     `json:"profile_tls"`
		ProfileTLSScope            string   `json:"profile_tls_scope"`
		ProfileWebSocket           bool     `json:"profile_websocket"`
		ProfileHTTPUpgrade         bool     `json:"profile_http_upgrade"`
		ProfileNetworkOptionsScope string   `json:"profile_network_options_scope"`
		RuntimeApply               bool     `json:"runtime_apply"`
	}
	if err := json.Unmarshal(capability.Body.Bytes(), &caps); err != nil || caps.Version != 4 || !caps.ProfileTLS || caps.ProfileTLSScope != "creation_plan" || !caps.ProfileWebSocket || !caps.ProfileHTTPUpgrade || caps.ProfileNetworkOptionsScope != "creation_plan" || caps.RuntimeApply || !reflect.DeepEqual(caps.Securities, []string{"none", "reality", "tls"}) || !reflect.DeepEqual(caps.Transports, []string{"tcp", "kcp", "ws", "grpc", "httpupgrade", "xhttp", "xdrive"}) {
		t.Fatal("TLS capability did not advertise the authoring-only contract", err)
	}
	previewHTTP := request("POST", base+"/preview", in, http.StatusOK)
	var preview business.NativeInboundPlanPreview
	if err := json.Unmarshal(previewHTTP.Body.Bytes(), &preview); err != nil || !preview.Valid || !preview.TopologyValidated || preview.Plan.Profile.TLS == nil || preview.Plan.Profile.PublicKey != "" || preview.Plan.Profile.Enabled || len(preview.Endpoints) == 0 {
		t.Fatal("TLS preview did not return paused public certificate metadata", err)
	}
	for _, endpoint := range preview.Endpoints {
		if endpoint.Security != "tls" || endpoint.TLSSHA256 != preview.Plan.Profile.TLS.SHA256 || endpoint.TLSPublicKeySHA256 != preview.Plan.Profile.TLS.PublicKeySHA256 || endpoint.SpiderX != "" || endpoint.WebSocket == nil {
			t.Fatal("TLS endpoint preview is not bound to the public profile certificate")
		}
	}
	decodeReceipt := func(w *httptest.ResponseRecorder) business.NativeInboundPlanReceipt {
		t.Helper()
		var receipt business.NativeInboundPlanReceipt
		if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil || receipt.RuntimeChanged || receipt.Plan.Profile.Enabled || receipt.Plan.Profile.TLS == nil || receipt.Plan.Profile.PublicKey != "" {
			t.Fatal("invalid TLS HTTP receipt", err)
		}
		return receipt
	}
	created := decodeReceipt(request("POST", base, in, http.StatusCreated))
	if !reflect.DeepEqual(created.Plan.Profile.TLS, preview.Plan.Profile.TLS) || created.Plan.InboundRevision != 1 {
		t.Fatal("TLS save changed the verified preview certificate")
	}
	retained := in
	retained.EventID = "tls-api-retained-edit-001"
	retained.ExpectedInboundRevision, retained.ExpectedPlanRevision = 1, 1
	retained.TLSCredentials = nil
	retained.Profile.Transport = "httpupgrade"
	retained.Profile.Options = &xrayconfig.ProfileOptions{WebSocket: &xrayconfig.WebSocketOptions{}, HTTPUpgrade: &xrayconfig.HTTPUpgradeOptions{Path: "/upgrade", Host: "upgrade.example.org"}}
	edited := decodeReceipt(request("PUT", base+"/501", retained, http.StatusOK))
	if edited.Plan.InboundRevision != 2 || edited.Plan.Profile.Options.HTTPUpgrade == nil || !reflect.DeepEqual(edited.Plan.Profile.TLS, created.Plan.Profile.TLS) {
		t.Fatal("omitted credentials did not retain the certificate across an HTTP transport edit")
	}
	renewal := retained
	renewal.EventID = "tls-api-explicit-renewal-001"
	renewal.ExpectedInboundRevision, renewal.ExpectedPlanRevision = 2, 2
	renewal.TLSCredentials = tlsAPICredentials(t)
	renewed := decodeReceipt(request("PUT", base+"/501", renewal, http.StatusOK))
	if renewed.Plan.InboundRevision != 3 || renewed.Plan.PlanRevision != 3 || renewed.Plan.Profile.TLS.SHA256 == created.Plan.Profile.TLS.SHA256 {
		t.Fatal("TLS renewal did not change the certificate and revision")
	}
	for _, historical := range []struct {
		method, path string
		input        business.NativeInboundPlanInput
		plan         business.NativeInboundPlan
	}{{"POST", base, in, created.Plan}, {"PUT", base + "/501", retained, edited.Plan}, {"PUT", base + "/501", renewal, renewed.Plan}} {
		replay := decodeReceipt(request(historical.method, historical.path, historical.input, http.StatusOK))
		if !replay.Replayed || !reflect.DeepEqual(replay.Plan, historical.plan) {
			t.Fatal("TLS HTTP retry returned a different historical certificate or plan")
		}
	}
	var reopened struct {
		Plan business.NativeInboundPlan `json:"plan"`
	}
	read := request("GET", base+"/501", nil, http.StatusOK)
	if err := json.Unmarshal(read.Body.Bytes(), &reopened); err != nil || !reflect.DeepEqual(reopened.Plan, renewed.Plan) {
		t.Fatal("TLS plan readback did not retain the latest public certificate", err)
	}
	var directory struct {
		Inbounds []business.NativeInbound `json:"inbounds"`
	}
	list := request("GET", "/api/v1/workspace/native-inbounds", nil, http.StatusOK)
	if err := json.Unmarshal(list.Body.Bytes(), &directory); err != nil || len(directory.Inbounds) != 1 || !reflect.DeepEqual(directory.Inbounds[0].TLS, renewed.Plan.Profile.TLS) || directory.Inbounds[0].Security != "tls" {
		t.Fatal("TLS directory lost public certificate metadata", err)
	}
}

func TestCreationTLSAPIRejectsAmbiguousCredentialWireInputAndGenericWrites(t *testing.T) {
	s, in, cookie, csrf := tlsPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbound-plans"
	created := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost)
	if created.Code != http.StatusCreated {
		t.Fatal("TLS fixture create failed", created.Code)
	}
	in.EventID = "tls-api-invalid-edit-001"
	in.ExpectedInboundRevision, in.ExpectedPlanRevision = 1, 1
	in.TLSCredentials = nil
	inputMap := func() map[string]any {
		t.Helper()
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err = json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	for _, item := range []struct {
		name string
		edit func(map[string]any)
	}{
		{"null", func(m map[string]any) { m["tls_credentials"] = nil }},
		{"empty", func(m map[string]any) { m["tls_credentials"] = map[string]any{} }},
		{"certificate only", func(m map[string]any) {
			m["tls_credentials"] = map[string]any{"certificate_pem": tlsAPICredentials(t).CertificatePEM}
		}},
		{"private key only", func(m map[string]any) {
			m["tls_credentials"] = map[string]any{"private_key_pem": tlsAPICredentials(t).PrivateKeyPEM}
		}},
		{"unknown credential path", func(m map[string]any) {
			m["tls_credentials"] = map[string]any{"certificate_file": "/private/example.pem"}
		}},
		{"private profile readback", func(m map[string]any) {
			m["profile"].(map[string]any)["tls"] = map[string]any{"private_key_pem": "secret-marker"}
		}},
	} {
		t.Run(item.name, func(t *testing.T) {
			body := inputMap()
			item.edit(body)
			response := browserRequest(t, s.Handler(), "PUT", base+"/501", body, cookie, csrf, "https://"+browserHost)
			if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "secret-marker") || strings.Contains(response.Body.String(), "/private/example.pem") || strings.Contains(response.Body.String(), "-----BEGIN") {
				t.Fatal("ambiguous credentials accepted or reflected", response.Code)
			}
		})
	}
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{
		`,"tls_credentials":null,"TLS_CREDENTIALS":{}}`,
		`,"tls_credentials":{"private_key_pem":"first","PRIVATE_KEY_PEM":"second"}}`,
	} {
		body := json.RawMessage(append(append([]byte(nil), raw[:len(raw)-1]...), []byte(suffix)...))
		response := browserRequest(t, s.Handler(), "PUT", base+"/501", body, cookie, csrf, "https://"+browserHost)
		if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "first") || strings.Contains(response.Body.String(), "second") {
			t.Fatal("duplicate credential JSON was accepted or reflected", response.Code)
		}
	}
	legacy := inputMap()
	delete(legacy["profile"].(map[string]any), "security")
	response := browserRequest(t, s.Handler(), "PUT", base+"/501", legacy, cookie, csrf, "https://"+browserHost)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), `"field":"profile.security"`) || !strings.Contains(response.Body.String(), `"code":"security_immutable"`) {
		t.Fatal("old editor silently changed TLS security", response.Code)
	}
	for _, attempt := range []struct {
		path string
		body any
	}{
		{"/api/v1/workspace/native-inbounds", in.Profile},
		{"/api/v1/workspace/native-inbounds/501/lifecycle", business.InboundMutation{EventID: "tls-api-generic-enable-001", ExpectedRevision: 1, Action: "enable", Reason: "Generic enable must remain closed"}},
		{"/api/v1/workspace/native-inbounds/501/lifecycle", business.InboundMutation{EventID: "tls-api-generic-edit-001", ExpectedRevision: 1, Action: "edit", Profile: &in.Profile, Reason: "Generic edit must remain closed"}},
	} {
		response := browserRequest(t, s.Handler(), "POST", attempt.path, attempt.body, cookie, csrf, "https://"+browserHost)
		requireTLSAPISecretFree(t, response)
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatal("generic route accepted TLS authoring or activation", attempt.path, response.Code)
		}
	}
	read := browserRequest(t, s.Handler(), "GET", base+"/501", nil, cookie, "", "")
	var saved struct {
		Plan business.NativeInboundPlan `json:"plan"`
	}
	if read.Code != http.StatusOK || json.Unmarshal(read.Body.Bytes(), &saved) != nil || saved.Plan.PlanRevision != 1 || saved.Plan.InboundRevision != 1 || saved.Plan.Profile.Enabled {
		t.Fatal("rejected TLS HTTP requests changed the saved plan")
	}
	requireTLSAPISecretFree(t, read)
}

func TestCreationTLSAPICredentialsRemainOwnerAndCSRFBundled(t *testing.T) {
	s, in, cookie, csrf := tlsPlanAPIFixture(t)
	base := "/api/v1/workspace/native-inbound-plans"
	for _, attempt := range []struct {
		path          string
		cookie        *http.Cookie
		token, origin string
		want          int
	}{
		{base + "/preview", nil, csrf, "https://" + browserHost, http.StatusUnauthorized},
		{base, cookie, "", "https://" + browserHost, http.StatusForbidden},
		{base + "/preview", cookie, csrf, "https://foreign.example.org", http.StatusForbidden},
	} {
		response := browserRequest(t, s.Handler(), "POST", attempt.path, in, attempt.cookie, attempt.token, attempt.origin)
		requireTLSAPISecretFree(t, response)
		if response.Code != attempt.want {
			t.Fatal("TLS credential request bypassed browser authority", response.Code)
		}
	}
	response := browserRequest(t, s.AgentHandler(), "POST", base+"/preview", in, cookie, csrf, "https://"+browserHost)
	requireTLSAPISecretFree(t, response)
	if response.Code == http.StatusOK || response.Code == http.StatusCreated {
		t.Fatal("Agent listener accepted TLS authoring credentials")
	}
}
