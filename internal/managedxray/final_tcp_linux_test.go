//go:build linux

package managedxray

import (
	"encoding/json"
	"strings"
	"testing"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestFinalTCPScopeRejectsDetachedOutboundAndAmbiguousPolicy(t *testing.T) {
	base := `{"log":{},"stats":{},"api":{},"policy":{"levels":{"0":{"statsUserUplink":true,"statsUserDownlink":true}},"system":{"statsInboundUplink":true,"statsInboundDownlink":true}},"inbounds":[{"listen":"127.0.0.1","port":12345,"tag":"one","protocol":"vless","settings":{},"streamSettings":{"network":"tcp","security":"reality","realitySettings":{}}}],"outbounds":[{"tag":"direct","protocol":"freedom"}]}`
	if !finalTCPScope([]byte(base)) {
		t.Fatal("native direct scope rejected")
	}
	for _, mode := range []string{"mux", "outbound-transport", "proxy-chain", "redirect", "fragment", "noise", "extra-level", "online", "grpc", "extra-grpc", "sniffing", "duplicate", "case-duplicate"} {
		t.Run(mode, func(t *testing.T) {
			var doc map[string]any
			if json.Unmarshal([]byte(base), &doc) != nil {
				t.Fatal("fixture")
			}
			out := doc["outbounds"].([]any)[0].(map[string]any)
			in := doc["inbounds"].([]any)[0].(map[string]any)
			levels := doc["policy"].(map[string]any)["levels"].(map[string]any)
			switch mode {
			case "mux":
				out["mux"] = map[string]any{"enabled": true}
			case "outbound-transport":
				out["streamSettings"] = map[string]any{"network": "grpc"}
			case "proxy-chain":
				out["proxySettings"] = map[string]any{"tag": "another"}
			case "redirect":
				out["settings"] = map[string]any{"redirect": "127.0.0.1:1"}
			case "fragment":
				out["settings"] = map[string]any{"fragment": map[string]any{"packets": "tlshello"}}
			case "noise":
				out["settings"] = map[string]any{"noises": []any{map[string]any{}}}
			case "extra-level":
				levels["1"] = levels["0"]
			case "online":
				levels["0"].(map[string]any)["statsUserOnline"] = true
			case "grpc":
				in["streamSettings"].(map[string]any)["network"] = "grpc"
			case "extra-grpc":
				in["streamSettings"].(map[string]any)["grpcSettings"] = map[string]any{"serviceName": "hidden"}
			case "sniffing":
				in["sniffing"] = map[string]any{"enabled": true}
			}
			raw, _ := json.Marshal(doc)
			if mode == "duplicate" {
				raw = []byte(strings.Replace(base, `"outbounds":`, `"outbounds":[],"outbounds":`, 1))
			}
			if mode == "case-duplicate" {
				raw = []byte(strings.Replace(base, `"outbounds":`, `"Outbounds":[],"outbounds":`, 1))
			}
			if finalTCPScope(raw) {
				t.Fatal("unreviewed counter path admitted")
			}
		})
	}
}

func TestFinalTCPJournalRejectsLegacyOrIncompleteProof(t *testing.T) {
	epoch := strings.Repeat("a", 32)
	receipt := uc.Receipt{EpochID: epoch, Sequence: 3, SHA: strings.Repeat("b", 64)}
	j := journal{Version: 5, Usage: &observedUsage{Epoch: uc.Epoch{ID: epoch}}, Admission: &usageAdmission{FinalCounterProtocol: uc.FinalTCPProtocol, TrafficBarrier: pretrafficActivationPossible, DrainNonce: strings.Repeat("c", 64), FinalReceipt: &receipt}}
	if !validFinalTCPJournal(j) || !validPretrafficJournal(j) {
		t.Fatal("valid pending final receipt rejected")
	}
	for _, mode := range []string{"legacy", "wrong-protocol", "missing-nonce", "uppercase-nonce", "pretraffic", "foreign-epoch", "bad-receipt", "wrong-seal"} {
		t.Run(mode, func(t *testing.T) {
			bad := j
			a := *j.Admission
			bad.Admission = &a
			switch mode {
			case "legacy":
				bad.Version = 4
			case "wrong-protocol":
				a.FinalCounterProtocol = "unknown"
			case "missing-nonce":
				a.DrainNonce = ""
			case "uppercase-nonce":
				a.DrainNonce = strings.Repeat("C", 64)
			case "pretraffic":
				a.TrafficBarrier = pretrafficNeverInstalled
			case "foreign-epoch":
				r := receipt
				r.EpochID = strings.Repeat("d", 32)
				a.FinalReceipt = &r
			case "bad-receipt":
				r := receipt
				r.Sequence = 0
				a.FinalReceipt = &r
			case "wrong-seal":
				r := receipt
				r.Sequence++
				a.Settlement = &r
			}
			if validFinalTCPJournal(bad) && validPretrafficJournal(bad) {
				t.Fatal("invalid settlement proof accepted")
			}
		})
	}
}
