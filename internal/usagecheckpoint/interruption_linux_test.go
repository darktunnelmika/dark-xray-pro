//go:build linux

package usagecheckpoint

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestInterruptionSenderRequiresPinScopeAndExactReceipt(t *testing.T) {
	for _, mode := range []string{"valid", "wrong-epoch", "false-hold", "duplicate", "redirect", "wrong-pin", "wrong-installation"} {
		t.Run(mode, func(t *testing.T) {
			epoch := testEpoch(t)
			var calls atomic.Int64
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != "POST" || r.URL.Path != "/api/v1/agent/native-usage/interrupted" {
					t.Error("wrong interruption route")
				}
				switch mode {
				case "redirect":
					w.Header().Set("Location", "https://invalid.example")
					w.WriteHeader(302)
				case "duplicate":
					w.Write([]byte(`{"epoch_id":"x","epoch_id":"y","reconciliation_required":true}`))
				default:
					receipt := InterruptionReceipt{EpochID: epoch.ID, ReconciliationRequired: mode != "false-hold"}
					if mode == "wrong-epoch" {
						receipt.EpochID = strings.Repeat("f", 32)
					}
					json.NewEncoder(w).Encode(receipt)
				}
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
			err = sender.NotifyInterrupted(context.Background(), epoch)
			if (err == nil) != (mode == "valid") {
				t.Fatal("invalid interruption receipt result", err)
			}
			if (mode == "wrong-pin" || mode == "wrong-installation") && calls.Load() != 0 {
				t.Fatal("credentials sent before scope/pin verification")
			}
			if mode == "wrong-installation" && !errors.Is(err, ErrEpoch) {
				t.Fatal("scope mismatch not identified", err)
			}
		})
	}
}
