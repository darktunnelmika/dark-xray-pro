//go:build linux

package managedxray

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/xrayconfig"
)

func TestConsumerEndpointPolicyRejectsUnapprovedAddressFormsAndAmbiguousJSON(t *testing.T) {
	for _, address := range []string{"", "consumer.example.org", "011.0.0.2", "11.0.0.2:14443", "11.0.0.2/32", "11.0.0.2 ", "::ffff:11.0.0.2", "[::1]", "0.0.0.1", "10.0.0.1", "100.64.0.1", "100.127.255.255", "127.0.0.2", "169.254.169.254", "172.16.0.1", "192.0.0.9", "192.0.2.1", "192.88.99.2", "192.168.0.1", "198.18.0.1", "198.19.255.255", "198.51.100.1", "203.0.113.1", "224.0.0.1", "240.0.0.1", "255.255.255.254", "255.255.255.255"} {
		if ValidateConsumerProtocolEndpoint(address, 14443) {
			t.Fatal("unsafe or noncanonical consumer address accepted", address)
		}
	}
	if ValidateConsumerProtocolEndpoint("11.0.0.2", 443) || !ValidateConsumerProtocolEndpoint("11.0.0.2", 1024) || !ValidateConsumerProtocolEndpoint("11.0.0.2", 65535) {
		t.Fatal("consumer port boundary changed")
	}
	a := ConsumerEndpointApproval{NodeID: "node-a", InstallationID: strings.Repeat("a", 32), InboundID: 42, Address: "11.0.0.2", Port: 14443}
	if ValidateConsumerEndpointApprovals([]ConsumerEndpointApproval{a}) != nil {
		t.Fatal("explicit tuple refused")
	}
	b := a
	b.InboundID++
	if ValidateConsumerEndpointApprovals([]ConsumerEndpointApproval{a, b}) == nil {
		t.Fatal("same endpoint authorized under two different inbounds")
	}
	b = a
	b.Port++
	if ValidateConsumerEndpointApprovals([]ConsumerEndpointApproval{a, b}) != nil {
		t.Fatal("two explicit alternatives for an inbound refused")
	}
	for _, mutate := range []func(*ConsumerEndpointApproval){
		func(a *ConsumerEndpointApproval) { a.NodeID = "../node" },
		func(a *ConsumerEndpointApproval) { a.InstallationID = "unknown" },
		func(a *ConsumerEndpointApproval) { a.InboundID = 0 },
		func(a *ConsumerEndpointApproval) { a.InboundID = 1 << 53 },
		func(a *ConsumerEndpointApproval) { a.Address = "127.0.0.1" },
	} {
		bad := a
		mutate(&bad)
		if ValidateConsumerEndpointApprovals([]ConsumerEndpointApproval{bad}) == nil {
			t.Fatal("unbound consumer authority accepted")
		}
	}
	many := make([]ConsumerEndpointApproval, MaxConsumerEndpointApprovals+1)
	for i := range many {
		many[i] = a
		many[i].Port += uint16(i)
	}
	if ValidateConsumerEndpointApprovals(many[:MaxConsumerEndpointApprovals]) != nil || ValidateConsumerEndpointApprovals(many) == nil {
		t.Fatal("startup approval bound changed")
	}
	raw, _ := json.Marshal([]ConsumerEndpointApproval{a})
	if parsed, err := ParseConsumerEndpointApprovalsJSON(string(raw)); err != nil || len(parsed) != 1 || parsed[0] != a {
		t.Fatal("valid strict startup JSON refused", err)
	}
	for _, invalid := range []string{"", "null", "[]", "{}", string(raw) + "{}", strings.Repeat(" ", (64<<10)+1), strings.Replace(string(raw), `"port":14443`, `"port":14443,"Port":14444`, 1), strings.Replace(string(raw), `"port":14443`, `"port":14443,"unknown":1`, 1), strings.Replace(string(raw), `"port":14443`, `"port":65536`, 1)} {
		if _, err := ParseConsumerEndpointApprovalsJSON(invalid); err == nil {
			t.Fatal("ambiguous or unbounded startup policy accepted")
		}
	}
}

func consumerHealthTestRequest(t *testing.T) ([]byte, ProtocolHealthRequest) {
	t.Helper()
	key, q := protocolHealthTestRequest(t)
	q.Version = ConsumerProtocolHealthVersion
	q.ConsumerManifestSHA, q.ConsumerAddress, q.ConsumerPort = strings.Repeat("9", 64), "11.0.0.2", 39443
	var err error
	q, err = SignProtocolHealthRequest(key, q)
	if err != nil {
		t.Fatal(err)
	}
	return key, q
}

func TestConsumerProtocolHealthBindsManifestIngressAndProtocolVersion(t *testing.T) {
	key, q := consumerHealthTestRequest(t)
	p := ProtocolHealthReceipt{Version: q.Version, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: q.Issued + 1}
	p.Signature = transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v3", p)
	if !VerifyProtocolHealthReceipt(key, q, p, time.Now()) {
		t.Fatal("valid v3 receipt refused")
	}
	for _, mutate := range []func(*ProtocolHealthRequest){
		func(q *ProtocolHealthRequest) { q.ConsumerManifestSHA = strings.Repeat("8", 64) },
		func(q *ProtocolHealthRequest) { q.ConsumerAddress = "11.0.0.3" },
		func(q *ProtocolHealthRequest) { q.ConsumerPort++ },
		func(q *ProtocolHealthRequest) { q.Port++ },
		func(q *ProtocolHealthRequest) { q.InboundID++ },
		func(q *ProtocolHealthRequest) {
			q.Version = ProtocolHealthVersion
			q.ConsumerManifestSHA = ""
			q.ConsumerAddress = ""
			q.ConsumerPort = 0
		},
	} {
		bad := q
		mutate(&bad)
		bad, err := SignProtocolHealthRequest(key, bad)
		if err != nil || VerifyProtocolHealthReceipt(key, bad, p, time.Now()) {
			t.Fatal("receipt moved to another manifest, ingress, origin or protocol", err)
		}
	}
	for _, mutate := range []func(*ProtocolHealthRequest){
		func(q *ProtocolHealthRequest) { q.Version = ProtocolHealthVersion },
		func(q *ProtocolHealthRequest) { q.ConsumerManifestSHA = "" },
		func(q *ProtocolHealthRequest) { q.ConsumerManifestSHA = strings.Repeat("0", 64) },
		func(q *ProtocolHealthRequest) { q.ConsumerAddress = "" },
		func(q *ProtocolHealthRequest) { q.ConsumerPort = 0 },
		func(q *ProtocolHealthRequest) { q.ConsumerAddress = "127.0.0.1" },
	} {
		bad := q
		mutate(&bad)
		if _, err := SignProtocolHealthRequest(key, bad); err == nil {
			t.Fatal("partial/downgraded consumer request signed")
		}
	}
	badRequest := q
	badRequest.Signature = ""
	badRequest.Signature = transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-REQUEST-v2", badRequest)
	if VerifyProtocolHealthReceipt(key, badRequest, p, time.Now()) {
		t.Fatal("legacy request MAC authorized consumer execution")
	}
	bad := p
	bad.Signature = ""
	bad.Signature = transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2", bad)
	if VerifyProtocolHealthReceipt(key, q, bad, time.Now()) || VerifyProtocolHealthReceipt(key, q, p, time.Unix(0, q.Expires)) {
		t.Fatal("legacy receipt MAC or expiration accepted")
	}
	_, legacy := protocolHealthTestRequest(t)
	legacyRaw, _ := json.Marshal(legacy)
	if bytes.Contains(legacyRaw, []byte("consumer_")) {
		t.Fatal("legacy JSON changed when consumer route is absent")
	}
	signed := legacy.Signature
	legacy.Signature = ""
	if transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-REQUEST-v2", legacy) != signed {
		t.Fatal("legacy request MAC domain changed")
	}
}

func TestConsumerProtocolHealthClientDialsApprovedIngressAndRetainsOriginBinding(t *testing.T) {
	_, q := consumerHealthTestRequest(t)
	q.InboundID = 1
	uuid := "af469f46-e18a-45f4-bf57-c7f649b10db5"
	email, err := uc.CounterEmail(q.SystemProbeID)
	if err != nil {
		t.Fatal(err)
	}
	credential := sha256.Sum256([]byte(uuid))
	q.CredentialSHA = hex.EncodeToString(credential[:])
	profile := xrayconfig.Profile{InboundID: q.InboundID, Transport: "tcp", RealityTarget: "www.example.org:443", ServerNames: []string{"Case.example.org"}, ShortIDs: []string{"aa11"}, RealityPrivateKey: base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x12}, 32)), Users: []xrayconfig.User{{UUID: uuid, Email: email}}}
	candidate, err := xrayconfig.RenderAssigned(q.NodeID, q.InstallationID, "127.0.0.1", []xrayconfig.Assignment{{Profile: profile, DirectPort: q.Port}})
	if err != nil {
		t.Fatal(err)
	}
	endpoint := ProtocolHealthEndpoint{Address: "11.0.0.1:19443", ServerName: "verifier.example.org", CertificateSHA: strings.Repeat("a", 64)}
	q.EndpointSHA = endpoint.SHA()
	readClient := func(q ProtocolHealthRequest) (string, uint16, string) {
		t.Helper()
		client, err := protocolHealthClient(candidate, q, 19543, endpoint)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Outbounds []struct {
				Tag      string `json:"tag"`
				Settings struct {
					Vnext []struct {
						Address string `json:"address"`
						Port    uint16 `json:"port"`
					} `json:"vnext"`
				} `json:"settings"`
			} `json:"outbounds"`
		}
		if json.Unmarshal(client.Config, &doc) != nil || len(doc.Outbounds) != 2 || doc.Outbounds[0].Tag != "deny" || len(doc.Outbounds[1].Settings.Vnext) != 1 || !bytes.Contains(client.Config, []byte(`"serverName":"Case.example.org"`)) || !bytes.Contains(client.Config, []byte(`"ip":["11.0.0.1/32"]`)) {
			t.Fatal("probe client lost exact Reality identity or restricted verifier routing")
		}
		v := doc.Outbounds[1].Settings.Vnext[0]
		return v.Address, v.Port, client.SHA256
	}
	address, port, digest := readClient(q)
	if address != q.ConsumerAddress || port != q.ConsumerPort || ValidateProtocolHealthSupport(candidate, q, endpoint) != nil {
		t.Fatal("consumer proof shortcut its approved ingress")
	}
	changed := q
	changed.ConsumerPort++
	_, changedPort, changedDigest := readClient(changed)
	if changedPort == port || changedDigest == digest {
		t.Fatal("ingress change did not change actual client bytes")
	}
	changed = q
	changed.Port++
	if _, err := protocolHealthClient(candidate, changed, 19543, endpoint); err == nil {
		t.Fatal("consumer ingress bypassed origin candidate identity")
	}
	changed = q
	changed.ConsumerAddress = ""
	if _, err := protocolHealthClient(candidate, changed, 19543, endpoint); err == nil {
		t.Fatal("missing ingress fell back to loopback")
	}
	legacy := q
	legacy.Version, legacy.ConsumerManifestSHA, legacy.ConsumerAddress, legacy.ConsumerPort = ProtocolHealthVersion, "", "", 0
	address, port, _ = readClient(legacy)
	if address != "127.0.0.1" || port != q.Port {
		t.Fatal("legacy local proof route changed")
	}
}

func TestConsumerProtocolAuthorityIsExactAndCopiedAtConstruction(t *testing.T) {
	_, q := consumerHealthTestRequest(t)
	a := ConsumerEndpointApproval{NodeID: q.NodeID, InstallationID: q.InstallationID, InboundID: q.InboundID, Address: q.ConsumerAddress, Port: q.ConsumerPort}
	r := &TransactionRuntime{options: Options{NodeID: q.NodeID, InstallationID: q.InstallationID}, metered: &MeteredTransactionOptions{}}
	if r.consumerProtocolApproved(q) {
		t.Fatal("empty Agent startup policy authorized consumer proof")
	}
	r.metered.ConsumerEndpointApprovals = []ConsumerEndpointApproval{a}
	if !r.consumerProtocolApproved(q) {
		t.Fatal("exact Agent startup authority refused")
	}
	for _, mutate := range []func(*ProtocolHealthRequest){
		func(q *ProtocolHealthRequest) { q.NodeID = "other" },
		func(q *ProtocolHealthRequest) { q.InstallationID = strings.Repeat("8", 32) },
		func(q *ProtocolHealthRequest) { q.InboundID++ },
		func(q *ProtocolHealthRequest) { q.ConsumerAddress = "11.0.0.3" },
		func(q *ProtocolHealthRequest) { q.ConsumerPort++ },
	} {
		bad := q
		mutate(&bad)
		if r.consumerProtocolApproved(bad) {
			t.Fatal("consumer startup approval crossed identity or tuple")
		}
	}
	t.Run("constructor-copies-policy", func(t *testing.T) {
		o := testOptions(t)
		o.ExperimentalFinalTCP = true
		a.NodeID, a.InstallationID = o.NodeID, o.InstallationID
		policy := []ConsumerEndpointApproval{a}
		m := MeteredTransactionOptions{Authority: &rejectMeteredRegistration{}, Sender: &uc.Sender{}, ConsumerEndpointApprovals: policy}
		runtime, err := OpenMeteredTransactions(o, m)
		if err != nil {
			t.Fatal(err)
		}
		defer runtime.Close()
		policy[0].Port++
		q.NodeID, q.InstallationID = o.NodeID, o.InstallationID
		if !runtime.consumerProtocolApproved(q) {
			t.Fatal("caller mutated startup authority after construction")
		}
		q.ConsumerPort++
		if runtime.consumerProtocolApproved(q) {
			t.Fatal("mutated caller slice granted new ingress authority")
		}
	})
}
