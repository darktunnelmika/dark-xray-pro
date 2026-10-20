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
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

// This acceptance adds the real HUB queue, TLS authority and ledger to the
// pinned core. The independent H4/H5 suite continues proving active-stream
// late counters; this fixture deliberately sends no customer traffic.
func TestRealPinnedMeteredRouteTransport(t *testing.T) {
	if os.Getenv("DARK_PRO_TEST_FINAL_TCP") != "1" {
		t.Skip("explicit final TCP core required")
	}
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if !filepath.IsAbs(binary) || len(pin) != 64 || os.Geteuid() == 0 {
		t.Fatal("nonroot pinned final TCP core required")
	}
	for _, mode := range []string{"replace-rollback", "lost-ack-before", "lost-ack-after", "revoked-after-final", "lost-registration", "lost-seal"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
			defer cancel()
			portA, portB := liveUsagePort(t), liveUsagePort(t)
			for portA == portB {
				portB = liveUsagePort(t)
			}
			s, key := meteredRouteAPIFixture(t, pin, portA)
			var loseBefore, loseAfter, loseRegister, loseSeal, revoke atomic.Bool
			var registrations atomic.Int64
			handler := s.AgentHandler()
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/agent/metered-route-transactions/register" {
					registrations.Add(1)
				}
				path := r.URL.Path
				if path == "/api/v1/agent/metered-route-transactions/receipt" && loseBefore.CompareAndSwap(true, false) {
					w.WriteHeader(503)
					return
				}
				discard := path == "/api/v1/agent/metered-route-transactions/receipt" && loseAfter.CompareAndSwap(true, false) ||
					path == "/api/v1/agent/metered-route-transactions/register" && loseRegister.CompareAndSwap(true, false) ||
					path == "/api/v1/agent/native-usage/settled" && loseSeal.CompareAndSwap(true, false)
				if discard {
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, r)
					if rec.Code >= 400 {
						t.Errorf("underlying lost response HTTP %d: %s", rec.Code, rec.Body.String())
					}
					w.WriteHeader(503)
					return
				}
				handler.ServeHTTP(w, r)
				if path == "/api/v1/agent/native-usage/settled" && revoke.CompareAndSwap(true, false) {
					if _, e := s.business.SetClientState(context.Background(), business.SetClientState{EventID: "metered-remote-revoke-after-final", ClientID: "metered-route-qa", Action: "disable", Reason: "Disposable post-settlement revocation"}); e != nil {
						t.Error(e)
					}
				}
			}))
			srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			srv.StartTLS()
			defer srv.Close()
			cert := srv.Certificate()
			sum := sha256.Sum256(cert.Raw)
			transportCfg := agenttransport.Config{Endpoint: srv.URL, PinSHA256: hex.EncodeToString(sum[:]), CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})}
			client, endpoint, e := agenttransport.New(transportCfg)
			if e != nil {
				t.Fatal(e)
			}
			sender, e := uc.NewSender(uc.SenderOptions{Endpoint: endpoint, NodeID: "route-qa", InstallationID: installID, Token: nodeToken, PinSHA256: transportCfg.PinSHA256, Client: client})
			if e != nil {
				t.Fatal(e)
			}
			defer sender.Close()
			authority, e := mx.NewMeteredRemoteAuthority(client, endpoint, nodeToken, "route-qa", installID)
			if e != nil {
				t.Fatal(e)
			}
			options := mx.Options{Directory: filepath.Join(t.TempDir(), "metered-runtime"), StateKey: bytes.Repeat([]byte{0x62}, 32), LeaseKey: []byte(nodeToken), NodeID: "route-qa", InstallationID: installID, ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin, ExperimentalFinalTCP: true}
			runtime, e := mx.OpenMeteredTransactions(options, mx.MeteredTransactionOptions{Authority: authority, Sender: sender, Interval: 100 * time.Millisecond, MaxSampleGap: 8 * time.Second, MaxPendingAge: 8 * time.Second})
			if e != nil {
				t.Fatal(e)
			}
			defer func() { runtime.Close() }()
			syncNow := func() {
				t.Helper()
				if e := runtime.SyncMeteredTransaction(ctx, client, endpoint); e != nil {
					t.Fatal("metered Agent sync", e)
				}
			}
			transportCfg.PinSHA256 = strings.Repeat("f", 64)
			wrong, _, e := agenttransport.New(transportCfg)
			if e != nil {
				t.Fatal(e)
			}
			if e = runtime.SyncMeteredTransaction(ctx, wrong, endpoint); e == nil {
				t.Fatal("wrong TLS pin")
			}
			if e = runtime.SyncTransaction(ctx, client, endpoint); e == nil {
				t.Fatal("v1 transport on metered runtime")
			}
			cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
			base := "/api/v1/workspace/native-nodes/route-qa/metered-route-transactions"
			enqueue := func(n int, action, stage string, revision uint64) business.MeteredRouteCommandView {
				t.Helper()
				syncNow()
				in := meteredRouteOwnerRequest(runtime.Status(), n, action, stage, revision)
				w := browserRequest(t, s.Handler(), "POST", base, in, cookie, csrf, "https://"+browserHost)
				if w.Code != 202 {
					t.Fatalf("owner %s HTTP %d: %s", action, w.Code, w.Body.String())
				}
				var out business.MeteredRouteCommandView
				if json.Unmarshal(w.Body.Bytes(), &out) != nil {
					t.Fatal("owner command response")
				}
				return out
			}
			receipt := func(id, outcome string) *mx.TransactionReceipt {
				t.Helper()
				v, e := s.business.MeteredRouteChannel(ctx, key, "route-qa")
				if e != nil {
					t.Fatal(e)
				}
				for _, c := range v.Commands {
					if c.ID == id {
						if c.Receipt == nil || c.Receipt.Outcome != outcome {
							t.Fatalf("receipt %s: %+v", outcome, c)
						}
						return c.Receipt
					}
				}
				t.Fatal("missing command")
				return nil
			}
			closed := func(port uint16) {
				t.Helper()
				c, e := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), 100*time.Millisecond)
				if e == nil {
					c.Close()
					t.Fatal("listener remained open", port)
				}
			}
			stage1 := enqueue(1, "stage", "", 1)
			syncNow()
			receipt(stage1.ID, "staged")
			closed(portA)
			apply1 := enqueue(2, "apply", stage1.ID, 0)
			loseBefore.Store(mode == "lost-ack-before")
			loseAfter.Store(mode == "lost-ack-after")
			loseRegister.Store(mode == "lost-registration")
			e = runtime.SyncMeteredTransaction(ctx, client, endpoint)
			if mode == "lost-registration" {
				if e == nil || runtime.Status().Observed.ProcessReady {
					t.Fatal("ambiguous birth admitted traffic", e)
				}
				receipt(apply1.ID, "failed_stopped")
				closed(portA)
				if registrations.Load() != 1 {
					t.Fatal("unexpected register retry")
				}
				return
			}
			if mode == "lost-ack-before" || mode == "lost-ack-after" {
				if e == nil {
					t.Fatal("lost response reported success")
				}
				birth := runtime.Status().Usage.EpochID
				syncNow()
				if registrations.Load() != 1 || runtime.Status().Usage.EpochID != birth {
					t.Fatal("ACK retry restarted or registered twice")
				}
			} else if e != nil {
				t.Fatal("initial apply", e)
			}
			ack1 := receipt(apply1.ID, "applied")
			epoch1 := ack1.Metered.After.EpochID
			if epoch1 == "" || !runtime.Status().Observed.ProcessReady {
				t.Fatal("missing first admitted epoch")
			}
			meteredRouteAPIDraft(t, s, key, 1, portB)
			stage2 := enqueue(3, "stage", "", 2)
			syncNow()
			receipt(stage2.ID, "staged")
			if runtime.Status().Usage.EpochID != epoch1 || !runtime.Status().Observed.ProcessReady {
				t.Fatal("Stage disturbed existing epoch")
			}
			apply2 := enqueue(4, "apply", stage2.ID, 0)
			revoke.Store(mode == "revoked-after-final")
			loseSeal.Store(mode == "lost-seal")
			e = runtime.SyncMeteredTransaction(ctx, client, endpoint)
			if mode == "revoked-after-final" || mode == "lost-seal" {
				if e == nil || runtime.Status().Observed.ProcessReady {
					t.Fatal("failed settlement/recheck admitted next birth", e)
				}
				receipt(apply2.ID, "failed_stopped")
				closed(portA)
				closed(portB)
				if registrations.Load() != 1 {
					t.Fatal("new birth registered before seal and recheck")
				}
				if mode == "lost-seal" {
					if _, e = runtime.ReplayMeteredUsage(ctx); e != nil {
						t.Fatal("exact seal replay", e)
					}
					if runtime.Status().Observed.ProcessReady {
						t.Fatal("usage replay resumed process")
					}
				}
				return
			}
			if e != nil {
				t.Fatal("replacement", e)
			}
			ack2 := receipt(apply2.ID, "applied")
			if ack2.Metered.PriorSettlement.EpochID != epoch1 || ack2.Metered.After.EpochID == epoch1 {
				t.Fatal("missing prior seal/new epoch")
			}
			closed(portA)
			// Restoring the old route requires freshly restored owner drafts and a
			// new command. It never revives old eligibility, lease or process birth.
			meteredRouteAPIDraft(t, s, key, 2, portA)
			rollback := enqueue(5, "rollback", stage1.ID, 0)
			syncNow()
			back := receipt(rollback.ID, "rolled_back")
			if back.Metered.After.EpochID == epoch1 || back.Metered.After.EpochID == ack2.Metered.After.EpochID || back.Metered.PriorSettlement.EpochID != ack2.Metered.After.EpochID {
				t.Fatal("rollback reused epoch")
			}
			closed(portB)
			renew := enqueue(6, "renew", stage1.ID, 0)
			syncNow()
			renewed := receipt(renew.ID, "renewed")
			if renewed.Metered.After.EpochID != back.Metered.After.EpochID {
				t.Fatal("renew created another epoch")
			}
			if registrations.Load() != 3 {
				t.Fatal("wrong number of supervised births", registrations.Load())
			}
			usage, e := s.business.ClientUsage(ctx, "metered-route-qa")
			if e != nil || usage.Total != 0 || !usage.Known || !usage.Eligible {
				t.Fatal("idle counter/eligibility", e)
			}
			runtime.Close()
			closed(portA)
		})
	}
}
