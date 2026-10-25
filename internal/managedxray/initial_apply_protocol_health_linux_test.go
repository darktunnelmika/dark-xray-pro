//go:build linux

package managedxray

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func initialApplyHealthTestRequest(t *testing.T) ([]byte, ProtocolHealthRequest) {
	t.Helper()
	key, q := protocolHealthTestRequest(t)
	q.Version, q.HandoverID, q.InitialApplySHA = InitialApplyProtocolHealthVersion, "", strings.Repeat("a", 64)
	q, err := SignProtocolHealthRequest(key, q)
	if err != nil {
		t.Fatal(err)
	}
	return key, q
}

func TestProtocolHealthLegacyWireAndMACGolden(t *testing.T) {
	// Fixed hashes cover the complete signed JSON bytes from before v4. They
	// also protect nested field ordering and the historical receipt domains.
	for _, golden := range []struct {
		version                int
		requestMAC, requestSHA string
		receiptMAC, receiptSHA string
	}{
		{2, "b735b2fbf34556821660078f3fdb66e58fe2c901a5a303b20dc1f73577a3c770", "74eec9fda9ebe74bb2063ec67e6c565cb54de6b24ec2202b62137dc5afc40fd7", "56075381476cff25efdb4cd49acb3458c7b26ebda124823e7bee9017bc54ed6f", "6b79a85a5c96a005ce558292395b81283be0f29a66d7ab2ef0cb06cc700ed390"},
		{3, "ae434a93e47be0a882af5332c9a8f9b8224cb66adbf3c5942aceba43d329b4f5", "2b558347e82fe3fad9f574c6e6ff18db09faf7cecd3b90e10034337e4c9b8992", "a21b9f99e552234c2572e10bc4db48a7e04ee08bdb0e824e22f8bf0faded82a7", "2b6040a44e85b84f335c6ff2b4f9202f10b32e7fe980464f79f481b5e8490306"},
	} {
		t.Run(fmt.Sprintf("v%d", golden.version), func(t *testing.T) {
			key, q := protocolHealthTestRequest(t)
			q.Version, q.Issued, q.Expires = golden.version, 1700000000000000000, 1700000006000000000
			if q.Version == ConsumerProtocolHealthVersion {
				q.ConsumerManifestSHA, q.ConsumerAddress, q.ConsumerPort = strings.Repeat("9", 64), "11.0.0.2", 39443
			}
			q, err := SignProtocolHealthRequest(key, q)
			if err != nil || q.Signature != golden.requestMAC || hashJSON(q) != golden.requestSHA {
				t.Fatal("legacy request bytes or MAC changed", err)
			}
			raw, err := json.Marshal(q)
			if err != nil || bytes.Contains(raw, []byte("initial_apply_sha")) {
				t.Fatal("legacy request acquired initial Apply scope", err)
			}
			p := ProtocolHealthReceipt{Version: q.Version, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: q.Issued + 1}
			p.Signature = transactionMAC(key, protocolHealthMACDomain("RECEIPT", q.Version), p)
			if p.Signature != golden.receiptMAC || hashJSON(p) != golden.receiptSHA || !VerifyProtocolHealthReceipt(key, q, p, time.Unix(0, p.Completed)) {
				t.Fatal("legacy receipt bytes, MAC or historical verification changed")
			}
		})
	}
}

func TestInitialApplyProtocolHealthBindsScopeAndExactApplyAuthority(t *testing.T) {
	key, q := initialApplyHealthTestRequest(t)
	p := ProtocolHealthReceipt{Version: q.Version, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: q.Issued + 1}
	p.Signature = transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v4", p)
	now := time.Unix(0, p.Completed)
	if !VerifyProtocolHealthReceipt(key, q, p, now) {
		t.Fatal("valid initial Apply evidence refused")
	}
	for _, test := range []struct {
		name   string
		mutate func(*ProtocolHealthRequest)
	}{
		{"promotion-and-apply", func(q *ProtocolHealthRequest) { q.InitialApplySHA = strings.Repeat("b", 64) }},
		{"runtime", func(q *ProtocolHealthRequest) { q.RuntimeInstance = strings.Repeat("1", 32) }},
		{"stage", func(q *ProtocolHealthRequest) { q.StageID = strings.Repeat("1", 32) }},
		{"apply", func(q *ProtocolHealthRequest) { q.ApplyCommandID = strings.Repeat("1", 32) }},
		{"epoch", func(q *ProtocolHealthRequest) { q.Usage.EpochID = strings.Repeat("1", 32) }},
		{"admission", func(q *ProtocolHealthRequest) { q.Usage.AdmissionSHA = strings.Repeat("1", 64) }},
		{"active-condition", func(q *ProtocolHealthRequest) { q.Expected.ActiveSequence++ }},
		{"probe-credential", func(q *ProtocolHealthRequest) { q.CredentialSHA = strings.Repeat("1", 64) }},
		{"origin", func(q *ProtocolHealthRequest) { q.Port++ }},
		{"handover-version", func(q *ProtocolHealthRequest) {
			q.Version, q.HandoverID, q.InitialApplySHA = ProtocolHealthVersion, strings.Repeat("b", 32), ""
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := q
			test.mutate(&changed)
			changed, err := SignProtocolHealthRequest(key, changed)
			if err != nil || VerifyProtocolHealthReceipt(key, changed, p, now) {
				t.Fatal("receipt transplanted to different authority", err)
			}
		})
	}
	for _, version := range []int{ProtocolHealthVersion, ConsumerProtocolHealthVersion} {
		changed := q
		changed.Signature = ""
		changed.Signature = transactionMAC(key, protocolHealthMACDomain("REQUEST", version), changed)
		if verifyProtocolHealthRequest(key, changed, p.Completed) {
			t.Fatal("legacy MAC authorized initial Apply request")
		}
		proof := p
		proof.Signature = ""
		proof.Signature = transactionMAC(key, protocolHealthMACDomain("RECEIPT", version), proof)
		if VerifyProtocolHealthReceipt(key, q, proof, now) {
			t.Fatal("legacy MAC authorized initial Apply receipt")
		}
	}
	if VerifyProtocolHealthReceipt(key, q, p, time.Unix(0, q.Expires)) || VerifyProtocolHealthReceipt(bytes.Repeat([]byte{0x74}, 32), q, p, now) {
		t.Fatal("expiry or another Agent key accepted")
	}
}

func TestInitialApplyProtocolHealthRejectsMixedOrMissingScope(t *testing.T) {
	key, q := initialApplyHealthTestRequest(t)
	for _, test := range []struct {
		name   string
		mutate func(*ProtocolHealthRequest)
	}{
		{"handover", func(q *ProtocolHealthRequest) { q.HandoverID = strings.Repeat("b", 32) }},
		{"missing-sha", func(q *ProtocolHealthRequest) { q.InitialApplySHA = "" }},
		{"zero-sha", func(q *ProtocolHealthRequest) { q.InitialApplySHA = strings.Repeat("0", 64) }},
		{"uppercase-sha", func(q *ProtocolHealthRequest) { q.InitialApplySHA = strings.Repeat("A", 64) }},
		{"short-sha", func(q *ProtocolHealthRequest) { q.InitialApplySHA = strings.Repeat("a", 63) }},
		{"long-sha", func(q *ProtocolHealthRequest) { q.InitialApplySHA = strings.Repeat("a", 65) }},
		{"nonhex-sha", func(q *ProtocolHealthRequest) { q.InitialApplySHA = strings.Repeat("z", 64) }},
		{"consumer-manifest", func(q *ProtocolHealthRequest) { q.ConsumerManifestSHA = strings.Repeat("a", 64) }},
		{"consumer-address", func(q *ProtocolHealthRequest) { q.ConsumerAddress = "11.0.0.2" }},
		{"consumer-port", func(q *ProtocolHealthRequest) { q.ConsumerPort = 39443 }},
		{"consumer-version", func(q *ProtocolHealthRequest) { q.Version = ConsumerProtocolHealthVersion }},
		{"unknown-version", func(q *ProtocolHealthRequest) { q.Version = 5 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			bad := q
			test.mutate(&bad)
			if _, err := SignProtocolHealthRequest(key, bad); err == nil {
				t.Fatal("mixed or malformed initial Apply request signed")
			}
		})
	}
	for _, legacy := range []func(*testing.T) ([]byte, ProtocolHealthRequest){protocolHealthTestRequest, consumerHealthTestRequest} {
		key, old := legacy(t)
		old.InitialApplySHA = q.InitialApplySHA
		if _, err := SignProtocolHealthRequest(key, old); err == nil {
			t.Fatal("legacy version accepted initial Apply authority")
		}
	}
}

func TestInitialApplyProtocolHealthHasNoConsumerEndpointAuthority(t *testing.T) {
	_, q := initialApplyHealthTestRequest(t)
	r := &TransactionRuntime{metered: &MeteredTransactionOptions{}}
	if !r.consumerProtocolApproved(q) {
		t.Fatal("local initial Apply requires no consumer endpoint approval")
	}
	for _, mutate := range []func(*ProtocolHealthRequest){
		func(q *ProtocolHealthRequest) { q.ConsumerManifestSHA = strings.Repeat("a", 64) },
		func(q *ProtocolHealthRequest) { q.ConsumerAddress = "11.0.0.2" },
		func(q *ProtocolHealthRequest) { q.ConsumerPort = 39443 },
	} {
		bad := q
		mutate(&bad)
		if r.consumerProtocolApproved(bad) {
			t.Fatal("initial Apply gained consumer ingress authority")
		}
	}
}
