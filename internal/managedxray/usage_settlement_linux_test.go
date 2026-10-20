//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestUsageSettlementRuntimeProofRecoveryAndPersistenceFence(t *testing.T) {
	for _, mode := range []string{"never-installed", "legacy-version", "activation-possible", "nonzero-counter", "false-seal", "lost-receipt", "journal-failure"} {
		t.Run(mode, func(t *testing.T) {
			o := testOptions(t)
			r := mustOpen(t, o)
			c, err := xrayconfig.WithUsageAPI(testCandidate(t, 19841, "one"), "127.0.0.1:19842")
			if err != nil {
				t.Fatal(err)
			}
			boot, _, endpoint, err := xrayconfig.UsageBootstrap(c)
			if err != nil {
				t.Fatal(err)
			}
			epoch := uc.Epoch{NodeID: o.NodeID, InstallationID: o.InstallationID, ID: strings.Repeat("c", 32), ConfigSHA: c.SHA256, KernelBootID: "00000000-0000-0000-0000-000000000001", PID: 123, ProcessStartTicks: 456, Bindings: []uc.Binding{{ClientID: "one", Email: "one@pro.invalid", Billable: true}}}
			j := journal{Version: 4, Highwater: 1, Good: freeze(Revision{1, c}), Usage: &observedUsage{Epoch: epoch, StatsEndpoint: endpoint}, Admission: &usageAdmission{StatsEndpoint: endpoint, BootstrapSHA: boot.SHA256, TrafficBarrier: pretrafficNeverInstalled}}
			if mode == "legacy-version" {
				j.Version = 3
				j.Admission.TrafficBarrier = ""
			}
			if mode == "activation-possible" {
				j.Admission.TrafficBarrier = pretrafficActivationPossible
			}
			if err = r.persist(j); err != nil {
				t.Fatal(err)
			}
			r.Close()
			r, err = Open(o)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if mode == "nonzero-counter" {
				box, e := uc.Open(uc.Options{Directory: usageDirectory(o.Directory, epoch.ID), Key: usageKey(o.StateKey, epoch.ID), Epoch: epoch, Create: true})
				if e != nil {
					t.Fatal(e)
				}
				_, e = box.Observe([]uc.Usage{{ClientID: "one", Uplink: 1}}, false)
				box.Close()
				if e != nil {
					t.Fatal(e)
				}
			}
			var calls atomic.Int64
			var lost atomic.Bool
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				calls.Add(1)
				if strings.HasSuffix(req.URL.Path, "/settled") {
					var ack uc.Receipt
					if json.NewDecoder(req.Body).Decode(&ack) != nil {
						t.Error("bad seal request")
					}
					if mode == "journal-failure" {
						if e := os.Chmod(o.Directory, 0500); e != nil {
							t.Error(e)
						}
					}
					json.NewEncoder(w).Encode(uc.SettlementReceipt{Receipt: ack, Sealed: mode != "false-seal"})
					return
				}
				var report uc.Report
				if json.NewDecoder(req.Body).Decode(&report) != nil || !report.Quiesced {
					t.Error("not final")
				}
				if mode == "lost-receipt" && !lost.Swap(true) {
					w.WriteHeader(503)
					return
				}
				json.NewEncoder(w).Encode(uc.Receipt{EpochID: report.EpochID, Sequence: report.Sequence, SHA: report.SHA})
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			server.StartTLS()
			defer server.Close()
			defer os.Chmod(o.Directory, 0700)
			digest := sha256.Sum256(server.Certificate().Raw)
			sender, err := uc.NewSender(uc.SenderOptions{Endpoint: server.URL, NodeID: o.NodeID, InstallationID: o.InstallationID, Token: string(bytes.Repeat([]byte{'t'}, 64)), PinSHA256: hex.EncodeToString(digest[:]), Client: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()
			_, err = r.SettleUnopenedUsage(context.Background(), sender)
			if mode == "lost-receipt" {
				if err == nil || r.UsageStatus().Settled {
					t.Fatal("lost receipt lifted fence")
				}
				r.Close()
				r, err = Open(o)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				_, err = r.ReplayObservedUsage(context.Background(), sender)
			}
			success := mode == "never-installed" || mode == "lost-receipt"
			if (err == nil) != success || r.UsageStatus().Settled != success || r.UsageStatus().SettlementRequired == success {
				t.Fatal("incorrect settlement", r.UsageStatus(), err)
			}
			if mode == "legacy-version" || mode == "activation-possible" {
				if !errors.Is(err, ErrUsageSettlement) || calls.Load() != 0 {
					t.Fatal("unproven birth contacted HUB", err)
				}
			}
			if mode == "nonzero-counter" && (!errors.Is(err, uc.ErrConflict) || calls.Load() != 0) {
				t.Fatal("nonzero tail fabricated")
			}
			if mode == "journal-failure" && !r.fenced {
				t.Fatal("undurable seal enabled reuse")
			}
			if success {
				r.Close()
				r, err = Open(o)
				if err != nil {
					t.Fatal(err)
				}
				defer r.Close()
				if !r.UsageStatus().Settled {
					t.Fatal("settlement lost on reopen")
				}
			}
		})
	}
}
