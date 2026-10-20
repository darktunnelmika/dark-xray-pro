//go:build linux

package managedxray

import (
	"encoding/base64"
	"encoding/json"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
	"strings"
	"testing"
	"time"
)

func TestMeteredTransportPreservesAbsentPayloadAcrossJSON(t *testing.T) {
	for _, action := range []string{"apply", "rollback", "renew"} {
		d := TransactionDelivery{Command: TransactionCommand{Action: action}}
		raw, e := json.Marshal(d)
		if e != nil {
			t.Fatal(e)
		}
		var wire TransactionDelivery
		if e = json.Unmarshal(raw, &wire); e != nil {
			t.Fatal(e)
		}
		c, e := meteredDeliveryCandidate(wire)
		if e != nil || c.Config != nil || TransactionPayloadSHA(c) != TransactionPayloadSHA(xrayconfig.Candidate{}) {
			t.Fatal("absent payload changed", action, e)
		}
		wire.ConfigBase64 = base64.StdEncoding.EncodeToString([]byte("{}"))
		if _, e = meteredDeliveryCandidate(wire); e == nil {
			t.Fatal("non-Stage bytes accepted")
		}
		wire.ConfigBase64 = ""
		wire.Candidate.RequiresXrayTest = true
		if _, e = meteredDeliveryCandidate(wire); e == nil {
			t.Fatal("non-Stage metadata accepted")
		}
	}
	if _, e := meteredDeliveryCandidate(TransactionDelivery{Command: TransactionCommand{Action: "stage"}}); e == nil {
		t.Fatal("empty Stage")
	}
	d := TransactionDelivery{Command: TransactionCommand{Action: "stage"}, ConfigBase64: base64.StdEncoding.EncodeToString([]byte("{}"))}
	if c, e := meteredDeliveryCandidate(d); e != nil || string(c.Config) != "{}" {
		t.Fatal("Stage bytes changed", e)
	}
}

func TestMeteredTransportPollCannotDowngradeOrOmitUsage(t *testing.T) {
	key := []byte(strings.Repeat("a", 32))
	p := TransactionPoll{NodeID: "metered-qa", InstallationID: strings.Repeat("b", 32), BinarySHA: strings.Repeat("c", 64), At: time.Now().UnixNano(), Status: TransactionStatus{Instance: strings.Repeat("d", 32), Usage: &MeteredCondition{}}}
	signed := SignMeteredTransactionPoll(key, p)
	if !VerifyMeteredTransactionPoll(key, signed) || VerifyTransactionPoll(key, signed) {
		t.Fatal("poll domain separation")
	}
	old := SignTransactionPoll(key, p)
	if VerifyMeteredTransactionPoll(key, old) {
		t.Fatal("v1 signature")
	}
	p.Status.Usage = nil
	p = SignMeteredTransactionPoll(key, p)
	if VerifyMeteredTransactionPoll(key, p) {
		t.Fatal("missing usage accepted")
	}
	signed.Status.Serial++
	if VerifyMeteredTransactionPoll(key, signed) {
		t.Fatal("mutated signed poll")
	}
}
