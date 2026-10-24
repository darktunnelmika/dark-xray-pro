//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/webui"
)

// This acceptance test uses the compiled browser UI and real authenticated API.
// Certificates and every business record are disposable fixtures; neither a
// production Node nor a public certificate authority is contacted.
func TestRealBrowserInboundTLSCreationPlan(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_CREATION_BROWSER") != "1" {
		t.Skip("explicit real-browser TLS authoring acceptance only")
	}
	webRoot, script := os.Getenv("DARK_PRO_TEST_WEB_DIST"), os.Getenv("DARK_PRO_TEST_TLS_BROWSER_SCRIPT")
	if !filepath.IsAbs(webRoot) || !filepath.IsAbs(script) {
		t.Fatal("absolute compiled UI and TLS browser script paths required")
	}
	for _, path := range []string{filepath.Join(webRoot, "index.html"), script} {
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			t.Fatal("compiled UI and browser script required")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{0xd5}, 32)
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	ticket, err := s.registry.Issue("de", "Germany")
	if err != nil {
		t.Fatal(err)
	}
	installation := strings.Repeat("a", 32)
	if _, err = s.registry.Claim(ticket.PairCode, "de", installation, strings.Repeat("2", 64)); err != nil {
		t.Fatal(err)
	}
	const existingID = uint64(71001)
	const clientID = "tls-unrelated-client"
	if _, err = s.business.CreateClient(ctx, business.NewClient{EventID: "tls-existing-client-0001", ClientID: clientID,
		RepresentativeID: "owner", Name: "Untouched TLS fixture client", DurationDays: 30, VolumeBytes: 1024 * 1024}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.business.CreateInbound(ctx, key, business.InboundInput{ID: existingID, Name: "Untouched REALITY fixture", Transport: "tcp",
		RealityTarget: "existing.example.org:443", ServerNames: []string{"existing.example.org"}, ShortIDs: []string{"77aa"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.business.BindInboundClients(ctx, existingID, []string{clientID}, true); err != nil {
		t.Fatal(err)
	}
	if err = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: existingID, NodeID: "de", InstallationID: installation,
		ListenIP: "198.51.100.10", DirectPort: 20443}); err != nil {
		t.Fatal(err)
	}
	beforeConfig, err := s.business.BuildNodeConfig(ctx, key, "de", installation)
	if err != nil {
		t.Fatal(err)
	}
	beforeTargets, err := s.business.ListInboundTargets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	beforeClients, err := s.business.ListClients(ctx, "", 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := s.business.ListEvents(ctx, "", 200, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, firstSHA := browserTLSFixture(t, 901)
	renewed, renewedSHA := browserTLSFixture(t, 902)
	owner := s.Handler()
	var mu sync.Mutex
	requests := map[string]int{}
	privateResponse := false
	mux := http.NewServeMux()
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Observe actual handler bytes without substituting an application response.
		recorder := &tlsBrowserResponse{ResponseWriter: w}
		owner.ServeHTTP(recorder, r)
		mu.Lock()
		defer mu.Unlock()
		requests[r.Method+" "+r.URL.Path]++
		for _, forbidden := range []string{"-----BEGIN", first["private_key_pem"], renewed["private_key_pem"], "\"tls_credentials\"", "\"private_key_pem\""} {
			if bytes.Contains(recorder.body.Bytes(), []byte(forbidden)) {
				privateResponse = true
			}
		}
	}))
	mux.Handle("/", webui.Directory(webRoot))
	server := httptest.NewServer(mux)
	defer server.Close()
	directory := t.TempDir()
	resultPath := filepath.Join(directory, "tls-result.json")
	controlPath := filepath.Join(directory, "tls-control.json")
	control, err := json.Marshal(map[string]any{"username": ownerUsername, "password": ownerPassword, "result_path": resultPath,
		"first": first, "renewed": renewed, "first_sha256": firstSHA, "renewed_sha256": renewedSHA})
	if err != nil || os.WriteFile(controlPath, control, 0600) != nil {
		t.Fatal("could not prepare private browser fixture")
	}
	command := exec.CommandContext(ctx, "node", script)
	command.Env = append(os.Environ(), "DARK_PRO_QA_UI_URL="+server.URL, "DARK_PRO_QA_CONTROL="+controlPath)
	output, err := command.CombinedOutput()
	if err != nil {
		// Browser diagnostics must never print the disposable PEM or login secret.
		safe := string(output)
		for _, secret := range []string{ownerPassword, first["private_key_pem"], renewed["private_key_pem"]} {
			safe = strings.ReplaceAll(safe, secret, "[fixture secret]")
		}
		t.Fatalf("real TLS browser failed: %v\n%s", err, safe)
	}
	if !bytes.Contains(output, []byte("NATIVE_UI_TLS_CERTIFICATE_LIFECYCLE_VERIFIED\n")) {
		t.Fatal("browser did not verify the certificate lifecycle")
	}
	var proof struct {
		InboundID   uint64 `json:"inbound_id"`
		EventID     string `json:"event_id"`
		OriginalSHA string `json:"original_sha256"`
		RenewedSHA  string `json:"renewed_sha256"`
	}
	raw, err := os.ReadFile(resultPath)
	if err != nil || json.Unmarshal(raw, &proof) != nil || proof.InboundID == 0 || proof.InboundID == existingID ||
		proof.OriginalSHA != firstSHA || proof.RenewedSHA != renewedSHA || len(proof.EventID) < 16 {
		t.Fatal("TLS browser proof absent or incomplete")
	}
	plan, err := s.business.NativeInboundPlan(ctx, key, proof.InboundID)
	if err != nil {
		t.Fatal("TLS plan was not persisted", err)
	}
	if plan.PlanRevision != 3 || plan.InboundRevision != 3 || plan.Profile.Revision != 3 || plan.LastEventID != proof.EventID ||
		plan.Profile.Security != "tls" || plan.Profile.Transport != "httpupgrade" || plan.Profile.TLS == nil || plan.Profile.TLS.SHA256 != renewedSHA ||
		plan.Profile.PublicKey != "" || plan.Profile.RealityTarget != "" || len(plan.Profile.ShortIDs) != 0 ||
		plan.Profile.Enabled || plan.Profile.Archived || plan.Profile.BoundClients != 0 || plan.RuntimeApply || !plan.Editable || plan.Status != "SAVED_DISABLED" {
		t.Fatal("TLS creation, retained-key edit or certificate replacement lost its invariants")
	}
	if plan.Profile.Options == nil || plan.Profile.Options.HTTPUpgrade == nil || plan.Profile.Options.HTTPUpgrade.Path != "/upgrade" || plan.Profile.Options.HTTPUpgrade.Host != "edge.example.org" {
		t.Fatal("TLS transport settings did not survive real UI/API persistence")
	}
	if len(plan.Nodes) != 1 || plan.Nodes[0].NodeID != "de" || plan.Nodes[0].DirectPort != 10443 || plan.Nodes[0].TunnelPort != 1185 ||
		!reflect.DeepEqual(plan.Nodes[0].DirectHosts, []string{"edge.example.org"}) || !reflect.DeepEqual(plan.Nodes[0].TunnelHosts, []string{"tunnel.example.org"}) {
		t.Fatal("TLS edit changed independent Direct/Tunnel settings")
	}
	profiles, err := s.business.ListInbounds(ctx, key)
	if err != nil || len(profiles) != 2 {
		t.Fatal("TLS browser created duplicate or lost existing profiles")
	}
	targets, err := s.business.ListInboundTargets(ctx)
	if err != nil || !reflect.DeepEqual(targets, beforeTargets) {
		t.Fatal("TLS authoring changed operational targets")
	}
	clients, err := s.business.ListClients(ctx, "", 200, 0)
	if err != nil || !reflect.DeepEqual(clients, beforeClients) {
		t.Fatal("TLS authoring changed customer records")
	}
	events, err := s.business.ListEvents(ctx, "", 200, 0)
	if err != nil || !reflect.DeepEqual(events, beforeEvents) {
		t.Fatal("TLS authoring changed financial events")
	}
	afterConfig, err := s.business.BuildNodeConfig(ctx, key, "de", installation)
	if err != nil || afterConfig.Generation != beforeConfig.Generation || afterConfig.Sequence != beforeConfig.Sequence ||
		afterConfig.ConfigSHA != beforeConfig.ConfigSHA || afterConfig.ManifestSHA != beforeConfig.ManifestSHA ||
		afterConfig.UserCount != beforeConfig.UserCount || !bytes.Equal(afterConfig.Candidate.Config, beforeConfig.Candidate.Config) {
		t.Fatal("TLS authoring changed the unrelated runtime candidate")
	}
	mu.Lock()
	defer mu.Unlock()
	if privateResponse {
		t.Fatal("a real API response exposed certificate credentials")
	}
	apiPath := "/api/v1/workspace/native-inbound-plans"
	want := map[string]int{"POST /api/v1/session/login": 1, "POST " + apiPath + "/preview": 4, "POST " + apiPath: 1,
		"PUT " + apiPath + "/" + strconv.FormatUint(proof.InboundID, 10): 2}
	for request, count := range requests {
		if strings.HasPrefix(request, "GET ") || strings.HasPrefix(request, "HEAD ") {
			continue
		}
		if want[request] != count {
			t.Fatal("TLS browser made an unexpected mutation", request, count)
		}
	}
	for request, count := range want {
		if requests[request] != count {
			t.Fatal("TLS browser omitted a real API operation", request, requests[request])
		}
	}
	t.Log("TLS_BROWSER_LIFECYCLE_PASS: real UI/API/SQLite create, transport edit with retained key, mismatch rejection, renewal, and unchanged runtime/customer state")
}

type tlsBrowserResponse struct {
	http.ResponseWriter
	body bytes.Buffer
}

func (w *tlsBrowserResponse) Write(raw []byte) (int, error) {
	_, _ = w.body.Write(raw)
	return w.ResponseWriter.Write(raw)
}

func browserTLSFixture(t *testing.T, serial int64) (map[string]string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: "edge.example.org"},
		DNSNames: []string{"edge.example.org", "tunnel.example.org"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(der)
	return map[string]string{"certificate_pem": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		"private_key_pem": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}))}, hex.EncodeToString(digest[:])
}
