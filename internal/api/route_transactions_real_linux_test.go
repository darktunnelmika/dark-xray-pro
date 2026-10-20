//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
)

func TestRealPinnedRouteQueueAgentLostACKAndRollback(t *testing.T) {
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if binary == "" && pin == "" {
		t.Skip("explicit pinned stock Xray required")
	}
	if binary == "" || len(pin) != 64 || os.Geteuid() == 0 {
		t.Fatal("pinned stock core and unprivileged execution required")
	}
	freePort := func() uint16 {
		l, e := net.Listen("tcp4", "127.0.0.1:0")
		if e != nil {
			t.Fatal(e)
		}
		p := uint16(l.Addr().(*net.TCPAddr).Port)
		l.Close()
		return p
	}
	portA, portB := freePort(), freePort()
	for portB == portA {
		portB = freePort()
	}
	s, key := routeAPIFixture(t, pin, portA)
	var loseBefore, loseAfter atomic.Bool
	handler := s.AgentHandler()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/route-transactions/receipt" {
			if loseBefore.CompareAndSwap(true, false) {
				w.WriteHeader(503)
				return
			}
			if loseAfter.CompareAndSwap(true, false) {
				record := httptest.NewRecorder()
				handler.ServeHTTP(record, r)
				if record.Code != 204 {
					t.Errorf("receipt failed before simulated lost response: %d %s", record.Code, record.Body.String())
				}
				w.WriteHeader(503)
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()
	cert := srv.Certificate()
	digest := sha256.Sum256(cert.Raw)
	cfg := agenttransport.Config{Endpoint: srv.URL, PinSHA256: hex.EncodeToString(digest[:]), CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})}
	client, endpoint, e := agenttransport.New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	options := mx.Options{Directory: filepath.Join(t.TempDir(), "route-runtime"), StateKey: bytes.Repeat([]byte{0x63}, 32), LeaseKey: []byte(nodeToken), NodeID: "route-qa", InstallationID: installID, ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin}
	runtime, e := mx.OpenTransactions(options)
	if e != nil {
		t.Fatal(e)
	}
	defer runtime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cfg.PinSHA256 = strings.Repeat("f", 64)
	wrong, _, e := agenttransport.New(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = runtime.SyncTransaction(ctx, wrong, endpoint); e == nil {
		t.Fatal("wrong certificate pin accepted")
	}
	syncNow := func() {
		t.Helper()
		if e := runtime.SyncTransaction(ctx, client, endpoint); e != nil {
			t.Fatal("Agent sync", e)
		}
	}
	syncNow()
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	base := "/api/v1/workspace/native-nodes/route-qa/route-transactions"
	enqueue := func(id int, action, stage string, revision uint64) business.RouteCommandView {
		t.Helper()
		syncNow()
		in := routeOwnerRequest(runtime.Status(), id, action, stage, revision)
		w := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost)
		if w.Code != 202 {
			t.Fatalf("owner %s HTTP %d: %s", action, w.Code, w.Body.String())
		}
		var v business.RouteCommandView
		if json.Unmarshal(w.Body.Bytes(), &v) != nil {
			t.Fatal("invalid queue view")
		}
		return v
	}
	assertPort := func(p uint16, want bool) {
		t.Helper()
		conn, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(p))), time.Second)
		if conn != nil {
			conn.Close()
		}
		if (e == nil) != want {
			t.Fatalf("real listener %d ready=%v want=%v", p, e == nil, want)
		}
	}
	stageA := enqueue(1, "stage", "", 1)
	syncNow()
	assertPort(portA, false)
	applyA := enqueue(2, "apply", stageA.ID, 0)
	loseBefore.Store(true)
	if e = runtime.SyncTransaction(ctx, client, endpoint); e == nil {
		t.Fatal("uncommitted receipt reported success")
	}
	before := runtime.Status().Observed
	assertPort(portA, true)
	v, e := s.business.RouteChannel(ctx, key, "route-qa")
	if e != nil || v.Commands[0].ID != applyA.ID || v.Commands[0].State != "delivered" {
		t.Fatal("HUB advanced without receipt", e)
	}
	syncNow()
	after := runtime.Status().Observed
	if after.ActiveSequence != before.ActiveSequence || after.LeaseExpires != before.LeaseExpires {
		t.Fatal("lost ACK replay restarted or renewed process")
	}
	v, e = s.business.RouteChannel(ctx, key, "route-qa")
	if e != nil || v.Commands[0].State != "acknowledged" || v.Commands[0].Receipt == nil {
		t.Fatal("exact ACK not durably recorded", e)
	}
	// A second whole-Node draft swaps the real loopback listener. No customer
	// identity or usage account is provisioned by this test or by this queue.
	routeAPIDraft(t, s, key, 1, portB)
	stageB := enqueue(3, "stage", "", 2)
	syncNow()
	assertPort(portA, true)
	assertPort(portB, false)
	enqueue(4, "apply", stageB.ID, 0)
	loseAfter.Store(true)
	if e = runtime.SyncTransaction(ctx, client, endpoint); e == nil {
		t.Fatal("lost HTTP commit response reported success")
	}
	before = runtime.Status().Observed
	syncNow()
	after = runtime.Status().Observed
	if before.LeaseExpires != after.LeaseExpires || before.ActiveSequence != after.ActiveSequence {
		t.Fatal("committed ACK retry changed process")
	}
	assertPort(portA, false)
	assertPort(portB, true)
	enqueue(5, "rollback", stageA.ID, 0)
	syncNow()
	assertPort(portA, true)
	assertPort(portB, false)
	v, e = s.business.RouteChannel(ctx, key, "route-qa")
	if e != nil || v.Commands[0].State != "acknowledged" || v.Commands[0].Receipt.Outcome != "rolled_back" || v.Commands[0].Receipt.Observed.ActiveSequence != 3 {
		t.Fatal("fresh revision rollback missing", e)
	}
	runtime.Close()
	assertPort(portA, false)
	restarted, e := mx.OpenTransactions(options)
	if e != nil {
		t.Fatal(e)
	}
	defer restarted.Close()
	if e = restarted.SyncTransaction(ctx, client, endpoint); e != nil {
		t.Fatal(e)
	}
	if restarted.Status().Observed.ProcessReady {
		t.Fatal("polling old ACK after restart activated listener")
	}
	assertPort(portA, false)
	v, e = s.business.RouteChannel(ctx, key, "route-qa")
	if e != nil || v.Status.Observed.ProcessReady || !v.ObservationFresh {
		t.Fatal("HUB invented post-restart liveness", e)
	}
}
