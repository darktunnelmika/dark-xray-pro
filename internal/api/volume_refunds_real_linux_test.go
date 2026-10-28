//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

// The separately pinned real core owns every byte counter and final receipt.
// All listeners, identities and storage are disposable and loopback-only.
func TestManagedInboundRealVolumeRefundAfterPartialTraffic(t *testing.T) {
	core := managedTestCore(t)
	if os.Geteuid() == 0 {
		t.Fatal("real volume refund acceptance requires an unprivileged test user")
	}
	for _, trigger := range []string{"manual_archive", "expiry"} {
		t.Run(trigger, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			s, _, _, _ := newLab(t)
			database := filepath.Join(t.TempDir(), "private", "business.sqlite")
			store, err := business.Open(database)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			if err = s.EnableBusinessLab(store); err != nil {
				t.Fatal(err)
			}
			if _, err = store.BootstrapOwner(ctx, ownerUsername, ownerPassword); err != nil {
				t.Fatal(err)
			}
			if err = store.EnableUsageLedger(ctx); err != nil {
				t.Fatal(err)
			}
			if err = s.EnableNativeInbounds(bytes.Repeat([]byte{0xc9}, 32)); err != nil {
				t.Fatal(err)
			}
			if err = s.EnableVolumeRefunds(ctx); err != nil {
				t.Fatal(err)
			}
			if err = s.EnableInboundExecution(InboundExecutionConfig{Directory: filepath.Join(t.TempDir(), "hub-execution"), Native: core}); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(s.inboundExecution.local.Close)
			ticket, err := s.registry.Issue("de", "Disposable real refund node")
			if err != nil {
				t.Fatal(err)
			}
			installation, token := strings.Repeat("b", 32), strings.Repeat("3", 64)
			if _, err = s.registry.Claim(ticket.PairCode, "de", installation, token); err != nil {
				t.Fatal(err)
			}
			const repID = "refund-real-rep"
			const purchased int64 = 1 << 20
			if _, err = store.CreateRepresentative(ctx, repID, "Real traffic refund fixture"); err != nil {
				t.Fatal(err)
			}
			if _, err = store.ProvisionRepresentativeLogin(ctx, repID, repID, repPassword); err != nil {
				t.Fatal(err)
			}
			if _, err = store.AdjustCredits(ctx, business.CreditChange{EventID: "refund-real-start-credit-0001", RepresentativeID: repID, VolumeDeltaBytes: purchased, UnlimitedDeltaUnits: 10, Reason: "Disposable real-core refund acceptance"}); err != nil {
				t.Fatal(err)
			}
			owner, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
			rep, repCSRF := browserLogin(t, s, repID, repPassword)
			request := func(cookie *http.Cookie, csrf, method, path string, input, output any, want int) {
				t.Helper()
				w := browserRequest(t, s.Handler(), method, path, input, cookie, csrf, "https://"+browserHost)
				if w.Code != want {
					t.Fatalf("%s %s: HTTP%d: %s", method, path, w.Code, w.Body.String())
				}
				if output != nil && json.Unmarshal(w.Body.Bytes(), output) != nil {
					t.Fatal("invalid authenticated response")
				}
			}
			var client business.Client
			request(rep, repCSRF, "POST", "/api/v1/workspace/clients", business.NewClient{EventID: "refund-real-create-volume-0001", ClientID: "refund-real-volume", RepresentativeID: repID, Name: "Partly used real-core volume", DurationDays: 30, VolumeBytes: purchased}, &client, 201)
			var unlimited business.Client
			request(rep, repCSRF, "POST", "/api/v1/workspace/clients", business.NewClient{EventID: "refund-real-create-token-0001", ClientID: "refund-real-unlimited", RepresentativeID: repID, Name: "Unchanged token control", DurationDays: 30, Unlimited: true, IPCount: 2}, &unlimited, 201)
			balance, err := store.Representative(ctx, repID)
			if err != nil || balance.VolumeCreditBytes != 0 || balance.UnlimitedCreditUnits != 8 {
				t.Fatal("real refund fixture funding is not isolated", balance, err)
			}
			port := managedTestPort(t)
			plan := business.NativeInboundPlanInput{EventID: "refund-real-inbound-create-0001", Reason: "Disposable loopback execution",
				Profile: business.InboundInput{ID: 501, Name: "Real volume refund", Spec: &inboundspec.Spec{Version: 1, Protocol: "http", Network: "tcp", Security: "none"}},
				Nodes:   []business.NativeInboundPlanNode{{NodeID: "de", InstallationID: installation, ListenIP: "127.0.0.1", DirectPort: port, DirectHosts: []string{"127.0.0.1"}, TunnelHosts: []string{}}},
			}
			request(owner, ownerCSRF, "POST", "/api/v1/workspace/native-inbound-plans", plan, nil, 201)
			base := "/api/v1/workspace/native-inbounds/501/execution"
			var state ix.State
			request(owner, ownerCSRF, "GET", base, nil, &state, 200)
			command := ix.CommandInput{EventID: "refund-real-activate-0001", Action: "activate", ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: []string{client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Reviewed disposable real traffic"}
			var preview ix.Preview
			request(owner, ownerCSRF, "POST", base+"/preview", command, &preview, 200)
			if !preview.Valid {
				t.Fatal("real refund activation preview rejected", preview.Blockers)
			}
			command.ReviewSHA = preview.ReviewSHA
			request(owner, ownerCSRF, "POST", base+"/commands", command, nil, 202)
			bundle, ok, err := store.InboundExecutionBundle(ctx, s.inboundKey, "de", installation)
			if err != nil || !ok || len(bundle.Principals) != 1 || bundle.Principals[0].ClientID != client.ID {
				t.Fatal("missing single billable real-core principal", err)
			}
			username := strings.Split(bundle.Principals[0].Counter, ">>>")[1]
			target, stopEcho := managedEcho(t)
			defer stopEcho()
			payload := bytes.Repeat([]byte("refund-checkpoint-"), 1024)
			tail := bytes.Repeat([]byte("unreported-final-tail-"), 1024)
			checkpointBytes := int64(2 * len(payload))
			finalBytes := int64(2 * (len(payload) + len(tail)))
			var injectTail, tailSent, loseFinal atomic.Bool
			var lastCheckpoint atomic.Int64
			var finalReport atomic.Pointer[ix.UsageReport]
			actual := s.AgentHandler()
			agent := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// The worker already sent its ordinary checkpoint before polling.
				// Exercise bytes arriving before it learns the entitlement ended;
				// only the real engine's terminal drain can settle this tail.
				if r.URL.Path == "/api/v1/agent/inbound-execution/poll" && injectTail.Swap(false) {
					if e := managedHTTP(port, username, client.UUID, target, tail); e != nil {
						t.Error("real pre-drain tail failed", e)
						http.Error(w, "test tail failed", 500)
						return
					}
					tailSent.Store(true)
				}
				if r.URL.Path != "/api/v1/agent/inbound-execution/usage" {
					actual.ServeHTTP(w, r)
					return
				}
				raw, e := io.ReadAll(r.Body)
				if e != nil {
					t.Error(e)
					http.Error(w, "test report read failed", 500)
					return
				}
				r.Body = io.NopCloser(bytes.NewReader(raw))
				var report ix.UsageReport
				if json.Unmarshal(raw, &report) != nil {
					t.Error("invalid worker-generated usage report")
					http.Error(w, "test report decode failed", 500)
					return
				}
				record := httptest.NewRecorder()
				actual.ServeHTTP(record, r)
				if record.Code == 200 {
					if report.Final {
						finalReport.Store(&report)
						if loseFinal.Swap(false) {
							http.Error(w, "lost committed final receipt", 503)
							return
						}
					} else if len(report.Counters) == 1 {
						lastCheckpoint.Store(int64(report.Counters[0].Upload + report.Counters[0].Download))
					}
				}
				for k, values := range record.Header() {
					w.Header()[k] = values
				}
				w.WriteHeader(record.Code)
				_, _ = w.Write(record.Body.Bytes())
			}))
			agent.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			agent.StartTLS()
			defer agent.Close()
			config := inboundruntime.Config{Directory: filepath.Join(t.TempDir(), "node-execution"), NodeID: "de", InstallationID: installation, Key: []byte(token), Native: core}
			worker, err := inboundruntime.Open(config)
			if err != nil {
				t.Fatal(err)
			}
			defer worker.Close()
			exchange, err := inboundruntime.NewHTTPExchange(agent.Client(), agent.URL, token, "de", installation)
			if err != nil {
				t.Fatal(err)
			}
			if err = worker.Step(ctx, exchange); err != nil {
				t.Fatal("real birth, zero receipt and admission", err)
			}
			if err = managedHTTP(port, username, client.UUID, target, payload); err != nil {
				t.Fatal("real authenticated partial traffic", err)
			}
			if err = worker.Step(ctx, exchange); err != nil {
				t.Fatal("real traffic checkpoint", err)
			}
			usage, err := store.ClientUsage(ctx, client.ID)
			if err != nil || usage.Total != checkpointBytes || usage.Holds != 0 || !usage.Known {
				t.Fatal("real checkpoint not billed exactly", usage, err)
			}
			archive := func(c business.Client) {
				request(rep, repCSRF, "POST", "/api/v1/workspace/clients/"+c.ID+"/state", business.SetClientState{EventID: "refund-real-archive-" + c.ID, ClientID: c.ID, Action: "archive", Reason: "Representative manual deletion"}, nil, 200)
			}
			if trigger == "manual_archive" {
				archive(client)
			} else {
				// Only this disposable fixture's wall-clock deadline is advanced.
				// Consumption, financial records and final proofs are never seeded
				// or edited: they all pass through the real runtime and APIs.
				clock, e := sql.Open("sqlite", database)
				if e != nil {
					t.Fatal(e)
				}
				result, e := clock.ExecContext(ctx, "UPDATE clients SET expires_unix=? WHERE id=? AND archived=0", time.Now().Unix()-1, client.ID)
				_ = clock.Close()
				if e != nil {
					t.Fatal("disposable expiry deadline", e)
				}
				if count, e := result.RowsAffected(); e != nil || count != 1 {
					t.Fatal("expiry fixture changed an unexpected number of rows", count, e)
				}
			}
			archive(unlimited)
			var pending business.VolumeExpiryRefundStatus
			request(rep, repCSRF, "GET", "/api/v1/workspace/clients/"+client.ID+"/volume-refund", nil, &pending, 200)
			if pending.State != "waiting_final_usage" || pending.Reason != "managed_epoch_unsettled" || pending.RefundBytes != nil {
				t.Fatal("incomplete active usage became refundable", pending)
			}
			batch, err := store.SweepExpiredVolumeRefunds(ctx, s.inboundKey, "", 50)
			if err != nil || batch.Applied != 0 {
				t.Fatal("returned volume before real final settlement", batch, err)
			}
			balance, err = store.Representative(ctx, repID)
			if err != nil || balance.VolumeCreditBytes != 0 || balance.UnlimitedCreditUnits != 8 {
				t.Fatal("pending finality or unlimited deletion changed credit", balance, err)
			}
			injectTail.Store(true)
			loseFinal.Store(true)
			if err = worker.Step(ctx, exchange); err == nil {
				t.Fatal("lost committed final response was acknowledged")
			}
			final := finalReport.Load()
			if !tailSent.Load() || final == nil || !final.Final || len(final.Counters) != 1 || final.Counters[0].PrincipalID != bundle.Principals[0].ID || int64(final.Counters[0].Upload+final.Counters[0].Download) != finalBytes || lastCheckpoint.Load() != checkpointBytes {
				t.Fatal("real final receipt did not include the unreported traffic tail")
			}
			usage, err = store.ClientUsage(ctx, client.ID)
			if err != nil || usage.Total != finalBytes || usage.Holds != 0 {
				t.Fatal("authoritative final tail differs from real core", usage, err)
			}
			refundCtx, stopRefunds := context.WithCancel(ctx)
			go s.RunVolumeRefunds(refundCtx)
			defer func() {
				stopRefunds()
				wait, done := context.WithTimeout(context.Background(), 2*time.Second)
				defer done()
				s.WaitVolumeRefunds(wait)
			}()
			returned := purchased - finalBytes
			deadline := time.Now().Add(4 * time.Second)
			for {
				balance, err = store.Representative(ctx, repID)
				if err != nil {
					t.Fatal(err)
				}
				if balance.VolumeCreditBytes == returned {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("automatic worker did not return exact unused credit", balance)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if balance.UnlimitedCreditUnits != 8 {
				t.Fatal("volume settlement refunded unlimited tokens")
			}
			// Replay the real durable final outbox after supervisor restart,
			// including the case where the HUB already returned unused credit.
			worker.Close()
			worker, err = inboundruntime.Open(config)
			if err != nil {
				t.Fatal("reopen actual final outbox", err)
			}
			defer worker.Close()
			if err = worker.Step(ctx, exchange); err != nil {
				t.Fatal("real final receipt replay", err)
			}
			if _, err = store.SweepExpiredVolumeRefunds(ctx, s.inboundKey, "", 50); err != nil {
				t.Fatal(err)
			}
			replay, err := store.RefundExpiredVolume(ctx, s.inboundKey, pending.Request)
			if err != nil || !replay.Replayed || replay.Applied || replay.Receipt == nil || replay.Receipt.RefundedBytes != returned || len(replay.Receipt.Proofs) != 1 || replay.Receipt.Proofs[0].Kind != "managed" || replay.Receipt.Proofs[0].FinalSHA != ix.Digest(*final) {
				t.Fatal("refund replay lost its exact real-core final proof", replay, err)
			}
			history, err := store.VolumeRefundHistory(ctx, repID, 25, 0)
			wantTrigger := "expired"
			if trigger == "manual_archive" {
				wantTrigger = "archived"
			}
			if err != nil || len(history) != 1 || history[0].ClientID != client.ID || history[0].Trigger != wantTrigger || history[0].RefundedBytes != returned || history[0].UsedBytes != finalBytes {
				t.Fatal("real final replay changed immutable refund history", history, err)
			}
			current, err := store.Client(ctx, client.ID)
			usage, usageErr := store.ClientUsage(ctx, client.ID)
			if err != nil || usageErr != nil || current.UUID != client.UUID || current.VolumeBytes != finalBytes || current.ChargeVolumeBytes != purchased || current.Archived != (trigger == "manual_archive") || usage.Total != finalBytes || usage.Eligible {
				t.Fatal("refund lost identity, usage or exhausted net cap", current, usage, err, usageErr)
			}
			if err = managedHTTP(port, username, client.UUID, target, payload); err == nil {
				t.Fatal("settled old credential still admitted traffic")
			}
			var created business.Client
			request(rep, repCSRF, "POST", "/api/v1/workspace/clients", business.NewClient{EventID: "refund-real-reuse-credit-0001", ClientID: "refund-real-reused", RepresentativeID: repID, Name: "Paid with returned real-traffic credit", DurationDays: 30, VolumeBytes: returned}, &created, 201)
			balance, err = store.Representative(ctx, repID)
			if err != nil || balance.VolumeCreditBytes != 0 || balance.UnlimitedCreditUnits != 8 || created.ChargeVolumeBytes != returned {
				t.Fatal("returned credit was not spendable exactly once", balance, created, err)
			}
			t.Logf("REAL_PARTIAL_TRAFFIC_REFUND_PASS: trigger=%s purchased=%d used=%d returned=%d tokens=8", wantTrigger, purchased, finalBytes, returned)
		})
	}
}
