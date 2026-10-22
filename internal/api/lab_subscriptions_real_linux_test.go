//go:build linux

package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

// This runs inside the existing two-inbound real health-round acceptance case.
// The consumer never receives a fixture-built client config: its independent
// URI parser must turn the actual token HTTP response into working core input.
func testLabSubscriptionRealDelivery(t *testing.T, ctx context.Context, f handoverHealthFixture, binary string) {
	t.Helper()
	audit, err := f.Store.MeteredHandoverAudit(ctx, f.Key, f.Plan.ID)
	if err != nil || audit.CanPublish || len(audit.HealthRounds) != 1 || !audit.HealthRounds[0].Current || len(f.Inbounds) != 2 {
		t.Fatal("LAB delivery requires the complete current two-inbound health round", err)
	}
	round := audit.HealthRounds[0]
	before := f.Runtime.Status()
	beforeClients := make(map[string]business.Client, len(f.Customers))
	for _, c := range f.Customers {
		beforeClients[c.ID], err = f.Store.Client(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	type tokenMetadata struct {
		ClientID   string `json:"client_id"`
		Generation uint64 `json:"generation"`
		State      string `json:"state"`
	}
	type deliverySnapshot struct {
		ID             string `json:"id"`
		EventID        string `json:"event_id"`
		ClientID       string `json:"client_id"`
		HandoverID     string `json:"handover_id"`
		RoundID        string `json:"round_id"`
		NodeID         string `json:"node_id"`
		InstallationID string `json:"installation_id"`
		Revision       uint64 `json:"revision"`
		Created        int64  `json:"created_unix_nano"`
		Expires        int64  `json:"expires_unix_nano"`
		RouteCount     int    `json:"route_count"`
		Current        bool   `json:"current"`
	}
	base := "/api/v1/workspace/clients/" + f.CustomerID + "/subscription"
	post := func(path string, body any, want int, out any) {
		t.Helper()
		w := browserRequest(t, f.Server.BrowserHandler(), http.MethodPost, path, body, f.Cookie, f.CSRF, "https://"+browserHost)
		if w.Code != want || json.Unmarshal(w.Body.Bytes(), out) != nil {
			t.Fatal("owner LAB subscription response", w.Code, "want", want)
		}
	}
	mutate := func(action string, n int, generation uint64, state string) tokenMetadata {
		t.Helper()
		var reply struct {
			tokenMetadata
			Token string `json:"token"`
			Path  string `json:"path"`
		}
		post(base+"/"+action, map[string]any{"event_id": handoverTestID(n), "expected_generation": generation}, http.StatusOK, &reply)
		if reply.ClientID != f.CustomerID || reply.Generation != generation+1 || reply.State != state || reply.Token != "" || reply.Path != "" {
			t.Fatal("token mutation did not return secret-free generation-bound metadata")
		}
		return reply.tokenMetadata
	}
	reveal := func(generation uint64) string {
		t.Helper()
		var reply struct {
			ClientID   string `json:"client_id"`
			Generation uint64 `json:"generation"`
			Token      string `json:"token"`
			Path       string `json:"path"`
		}
		post(base+"/reveal", map[string]any{"expected_generation": generation}, http.StatusOK, &reply)
		secret, err := base64.RawURLEncoding.Strict().DecodeString(reply.Token)
		if err != nil || len(secret) != 32 || reply.ClientID != f.CustomerID || reply.Generation != generation || reply.Path != "/api/v1/subscriptions/"+reply.Token {
			t.Fatal("explicit token reveal lost the exact client generation or relative endpoint")
		}
		return reply.Path
	}
	public := httptest.NewServer(f.Server.BrowserHandler())
	defer public.Close()
	deliveryExpires := round.Expires
	get := func(path string, want int) []byte {
		t.Helper()
		fetchCtx, cancelFetch := context.WithTimeout(ctx, 3*time.Second)
		defer cancelFetch()
		if want == http.StatusOK {
			var cancelExpiry context.CancelFunc
			fetchCtx, cancelExpiry = context.WithDeadline(fetchCtx, time.Unix(0, deliveryExpires))
			defer cancelExpiry()
		}
		for {
			r, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, public.URL+path, nil)
			if err != nil {
				t.Fatal("consumer request construction failed")
			}
			res, err := public.Client().Do(r)
			if err != nil {
				t.Fatal("consumer HTTP transport or bounded delivery deadline failed")
			}
			raw, err := io.ReadAll(io.LimitReader(res.Body, (256<<10)+1))
			res.Body.Close()
			if err != nil || len(raw) > 256<<10 || !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
				t.Fatal("consumer HTTP response or cache boundary", res.StatusCode, "want", want)
			}
			if res.StatusCode != http.StatusOK && (bytes.Contains(raw, []byte("vless://")) || bytes.Contains(raw, []byte(f.CustomerCredential))) {
				t.Fatal("unavailable subscription leaked consumer credentials")
			}
			if res.StatusCode == http.StatusServiceUnavailable && (want == http.StatusOK || want == http.StatusNotFound) {
				if res.Header.Get("Retry-After") != "1" {
					t.Fatal("busy consumer response omitted the documented retry hint")
				}
				// Short fixture polling tolerates only the documented TryLock
				// contention response. Neither this request's bounded budget nor
				// the original health/snapshot deadline is refreshed by a retry.
				select {
				case <-fetchCtx.Done():
					t.Fatal("consumer remained busy past its bounded delivery deadline")
				case <-time.After(10 * time.Millisecond):
				}
				continue
			}
			if res.StatusCode != want {
				t.Fatal("consumer HTTP status", res.StatusCode, "want", want)
			}
			if want == http.StatusOK {
				if fetchCtx.Err() != nil || time.Now().UnixNano() >= deliveryExpires {
					t.Fatal("consumer content arrived after the original delivery deadline")
				}
				if !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") {
					t.Fatal("subscription did not return its documented text representation")
				}
			}
			return raw
		}
	}
	issued := mutate("issue", 351, 0, "active")
	path := reveal(issued.Generation)
	if reveal(issued.Generation) != path {
		t.Fatal("explicit reveal changed a stable subscription token")
	}
	get(path, http.StatusNotFound) // A token cannot publish the mutable desired profile.
	var snapshot deliverySnapshot
	body := map[string]any{"event_id": handoverTestID(352), "client_id": f.CustomerID, "round_id": round.ID, "expected_revision": 0}
	snapshotPath := handoverBase + "/" + f.Plan.ID + "/lab-subscription-snapshot"
	post(snapshotPath, body, http.StatusCreated, &snapshot)
	if snapshot.ID == "" || snapshot.EventID != handoverTestID(352) || snapshot.ClientID != f.CustomerID || snapshot.HandoverID != f.Plan.ID || snapshot.RoundID != round.ID || snapshot.NodeID != f.Plan.TargetNode || snapshot.InstallationID != f.Plan.TargetInstallation || snapshot.Revision != 1 || snapshot.Created < round.Created || snapshot.Expires <= snapshot.Created || snapshot.Expires > round.Expires || snapshot.Expires > before.Observed.LeaseExpires || snapshot.RouteCount != 2 || !snapshot.Current {
		t.Fatal("LAB snapshot lost its exact current round, candidate scope or bounded deadline")
	}
	deliveryExpires = snapshot.Expires
	var replay deliverySnapshot
	post(snapshotPath, body, http.StatusOK, &replay)
	if replay != snapshot {
		t.Fatal("snapshot retry changed content identity, revision or original deadline")
	}
	raw := get(path+"?format=raw", http.StatusOK)
	for _, query := range []string{"", "?format=base64"} {
		encoded := get(path+query, http.StatusOK)
		decoded, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(encoded)))
		if err != nil || !bytes.Equal(decoded, raw) {
			t.Fatal("default and explicit base64 did not contain the exact raw subscription")
		}
	}
	get(path+"?format=unsupported", http.StatusUnprocessableEntity)
	for _, other := range f.Customers {
		if other.ID != f.CustomerID && bytes.Contains(raw, []byte(other.UUID)) {
			t.Fatal("subscription exposed another customer's credential")
		}
	}
	for _, inbound := range f.Inbounds {
		if bytes.Contains(raw, []byte(inbound.Credential)) || bytes.Contains(raw, []byte(inbound.SystemProbeID)) {
			t.Fatal("subscription exposed a system probe credential or accounting identity")
		}
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != len(f.Inbounds) {
		t.Fatal("consumer subscription omitted or duplicated destination inbounds")
	}
	ports := make(map[uint16]bool, len(f.Inbounds))
	for _, inbound := range f.Inbounds {
		ports[inbound.Port] = true
	}
	deliveryCtx, finishDelivery := context.WithDeadline(ctx, time.Unix(0, snapshot.Expires))
	defer finishDelivery()
	for _, line := range lines {
		u, err := url.Parse(line)
		if err != nil || u.Scheme != "vless" || u.User == nil || u.User.Username() != f.CustomerCredential || u.Hostname() != "127.0.0.1" || u.Path != "" || u.Fragment == "" {
			t.Fatal("consumer URI did not describe the exact synthetic customer's loopback route")
		}
		if _, password := u.User.Password(); password {
			t.Fatal("consumer URI included an unexpected credential")
		}
		port, err := strconv.ParseUint(u.Port(), 10, 16)
		if err != nil || !ports[uint16(port)] {
			t.Fatal("consumer URI changed or repeated an applied inbound port")
		}
		delete(ports, uint16(port))
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q) != 8 || q.Get("encryption") != "none" || q.Get("flow") != "xtls-rprx-vision" || q.Get("type") != "tcp" || q.Get("security") != "reality" || q.Get("fp") != "chrome" || q.Get("sni") != "one.example.org" || q.Get("sid") != "aa11" {
			t.Fatal("consumer URI changed the applied Reality/Vision transport")
		}
		key, err := base64.RawURLEncoding.Strict().DecodeString(q.Get("pbk"))
		if err != nil || len(key) != 32 {
			t.Fatal("consumer URI omitted its Reality public key")
		}
		for _, values := range q {
			if len(values) != 1 {
				t.Fatal("consumer URI contains ambiguous repeated parameters")
			}
		}
		func() {
			client, stop := labSubscriptionURIClient(t, deliveryCtx, binary, u, f.Origin)
			defer stop()
			payload := bytes.Repeat([]byte("synthetic subscription echo\n"), 1024)
			request, err := http.NewRequestWithContext(deliveryCtx, http.MethodPost, f.Origin.URL+"/dark-pro/lab-subscription-echo", bytes.NewReader(payload))
			if err != nil {
				t.Fatal("synthetic echo request construction failed")
			}
			request.Header.Set("Content-Type", "application/octet-stream")
			response, err := client.Do(request)
			if err != nil {
				t.Fatal("consumer URI could not carry real pinned-core customer traffic")
			}
			defer response.Body.Close()
			echo, err := io.ReadAll(io.LimitReader(response.Body, int64(len(payload)+1)))
			if err != nil || response.StatusCode != http.StatusOK || !bytes.Equal(echo, payload) {
				t.Fatal("real subscription connection did not preserve the exact echo payload")
			}
		}()
	}
	if len(ports) != 0 {
		t.Fatal("an applied destination listener was not exercised by consumer output")
	}
	revoked := mutate("revoke", 353, issued.Generation, "revoked")
	get(path, http.StatusNotFound)
	rotated := mutate("rotate", 354, revoked.Generation, "active")
	newPath := reveal(rotated.Generation)
	if newPath == path || !bytes.Equal(get(newPath+"?format=raw", http.StatusOK), raw) {
		t.Fatal("rotation did not replace the token while preserving the exact current snapshot")
	}
	get(path, http.StatusNotFound)
	// Preserve the original deadline. A fresh token and real customer traffic
	// cannot renew health evidence or lease authority.
	remaining := time.Until(time.Unix(0, snapshot.Expires)) + 10*time.Millisecond
	if remaining > 0 {
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			t.Fatal("fixture deadline preceded subscription expiry")
		case <-timer.C:
		}
	}
	get(newPath, http.StatusNotFound)
	after := f.Runtime.Status()
	view, err := f.Store.MeteredHandover(ctx, f.Plan.ID)
	if err != nil || view.CanPublish || view.Phase != "target_applied_health_required" || after.Observed.SubscriptionPublished || after.Observed.DataPlaneVerified || after.Instance != before.Instance || after.Serial != before.Serial || after.CurrentStageID != before.CurrentStageID || mx.Condition(after.Observed) != mx.Condition(before.Observed) || after.Observed.LeaseExpires != before.Observed.LeaseExpires || after.Usage == nil || before.Usage == nil || *after.Usage != *before.Usage {
		t.Fatal("subscription delivery changed runtime, epoch, lease or production publication authority", err)
	}
	for id, beforeClient := range beforeClients {
		afterClient, err := f.Store.Client(ctx, id)
		if err != nil || afterClient != beforeClient {
			t.Fatal("subscription delivery changed customer UUID, plan, expiry or financial metadata", err)
		}
	}
	t.Log("LAB_SUBSCRIPTION_REAL_PASS: token HTTP raw/base64, both applied Reality/Vision ports, exact customer echo, revoke and original expiry; production publication remains disabled")
	// The surrounding fixture drains the same real runtime and compares each
	// customer's final ledger totals to its exact final core counters.
}

func labSubscriptionURIClient(t *testing.T, ctx context.Context, binary string, u *url.URL, origin *httptest.Server) (*http.Client, func()) {
	t.Helper()
	q := u.Query()
	port, err := strconv.ParseUint(u.Port(), 10, 16)
	if err != nil {
		t.Fatal("invalid consumer port")
	}
	socksPort := liveUsagePort(t)
	doc := map[string]any{
		"log":      map[string]string{"loglevel": "none"},
		"inbounds": []any{map[string]any{"listen": "127.0.0.1", "port": socksPort, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}}},
		"outbounds": []any{map[string]any{
			"protocol":       "vless",
			"settings":       map[string]any{"vnext": []any{map[string]any{"address": u.Hostname(), "port": port, "users": []any{map[string]any{"id": u.User.Username(), "encryption": q.Get("encryption"), "flow": q.Get("flow")}}}}},
			"streamSettings": map[string]any{"network": q.Get("type"), "security": q.Get("security"), "realitySettings": map[string]any{"serverName": q.Get("sni"), "fingerprint": q.Get("fp"), "publicKey": q.Get("pbk"), "shortId": q.Get("sid")}},
		}},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "subscription-client.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	childCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(childCtx, binary, "run", "-config", path)
	cmd.Env = []string{"PATH=/usr/bin:/bin"}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal("pinned consumer core did not start")
	}
	var stopped sync.Once
	stop := func() { stopped.Do(func() { cancel(); _ = cmd.Wait() }) }
	t.Cleanup(stop)
	socks := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(socksPort)))
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		conn, err := net.DialTimeout("tcp4", socks, 100*time.Millisecond)
		if err == nil {
			conn.Close()
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("consumer core did not start before the original snapshot deadline")
		case <-tick.C:
		}
	}
	transport := &http.Transport{
		TLSClientConfig:   origin.Client().Transport.(*http.Transport).TLSClientConfig.Clone(),
		DisableKeepAlives: true,
		DialContext: func(c context.Context, _ string, address string) (net.Conn, error) {
			return liveUsageSOCKS(c, socks, address)
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 5 * time.Second}, stop
}
