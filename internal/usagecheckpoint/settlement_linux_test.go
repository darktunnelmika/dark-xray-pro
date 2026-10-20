//go:build linux

package usagecheckpoint

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestUsageSettlementSenderRequiresExactSealAndPinnedScope(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-hash", "wrong-sequence", "wrong-epoch", "unsealed", "ordinary-receipt", "duplicate-field", "wrong-pin", "wrong-installation", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			epoch := Epoch{ID: strings.Repeat("b", 32), NodeID: "node-one", InstallationID: strings.Repeat("a", 32), ConfigSHA: strings.Repeat("c", 64), KernelBootID: "00000000-0000-0000-0000-000000000001", PID: 123, ProcessStartTicks: 456, Bindings: []Binding{{ClientID: "customer", Email: "customer@pro.invalid", Billable: true}}}
			receipt := Receipt{EpochID: epoch.ID, Sequence: 2, SHA: strings.Repeat("c", 64)}
			var calls atomic.Int64
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/api/v1/agent/native-usage/settled" || r.Method != "POST" {
					t.Error("wrong route")
				}
				var body Receipt
				if json.NewDecoder(r.Body).Decode(&body) != nil || body != receipt {
					t.Error("wrong requested cursor")
				}
				proof := SettlementReceipt{Receipt: receipt, Sealed: true}
				switch mode {
				case "wrong-hash":
					proof.Receipt.SHA = strings.Repeat("f", 64)
				case "wrong-sequence":
					proof.Receipt.Sequence++
				case "wrong-epoch":
					proof.Receipt.EpochID = strings.Repeat("f", 32)
				case "unsealed":
					proof.Sealed = false
				case "ordinary-receipt":
					json.NewEncoder(w).Encode(receipt)
					return
				case "duplicate-field":
					w.Write([]byte(`{"sealed":true,"sealed":false}`))
					return
				case "redirect":
					w.Header().Set("Location", "https://invalid.example")
					w.WriteHeader(302)
					return
				}
				json.NewEncoder(w).Encode(proof)
			}))
			server.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
			server.StartTLS()
			defer server.Close()
			digest := sha256.Sum256(server.Certificate().Raw)
			pin := hex.EncodeToString(digest[:])
			if mode == "wrong-pin" {
				pin = strings.Repeat("a", 64)
			}
			sender, err := NewSender(SenderOptions{Endpoint: server.URL, NodeID: epoch.NodeID, InstallationID: epoch.InstallationID, Token: strings.Repeat("u", 64), PinSHA256: pin, Client: server.Client()})
			if err != nil {
				t.Fatal(err)
			}
			defer sender.Close()
			if mode == "wrong-installation" {
				epoch.InstallationID = strings.Repeat("f", 32)
			}
			err = sender.ConfirmSettlement(context.Background(), epoch, receipt)
			if (err == nil) != (mode == "valid") {
				t.Fatal("invalid settlement accepted", err)
			}
			if (mode == "wrong-pin" || mode == "wrong-installation") && calls.Load() != 0 {
				t.Fatal("credentials sent before verification")
			}
		})
	}
}
