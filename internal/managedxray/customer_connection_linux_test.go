//go:build linux

package managedxray

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"strings"
	"testing"
	"time"
)

func TestCustomerConnectionChallengeAndReceiptBoundaries(t *testing.T) {
	pub, private, _ := ed25519.GenerateKey(nil)
	key := bytes.Repeat([]byte{7}, 64)
	now := time.Now().UnixNano()
	h := strings.Repeat("a", 64)
	q := CustomerConnectionChallenge{Version: 1, ID: strings.Repeat("a", 32), PlanSHA: h, ClientID: "customer-1", EpochID: strings.Repeat("b", 32), ConfigSHA: h, CoreSHA: h, CredentialSHA: h, ListenerSHA: h, EndpointSHA: h, NodeID: "node-1", InstallationID: strings.Repeat("c", 32), Process: uc.ProcessToken{PID: 123, StartTicks: 456, BootID: "boot"}, InboundID: 1, Port: 10240, Signer: pub, Issued: now, Expires: now + int64(8*time.Second), Consent: true}
	q.State = CustomerConnectionState{Expected: RuntimeCondition{Highwater: 1, SavedSequence: 1, SavedSHA: h, ActiveSequence: 1, ActiveSHA: h, ProcessReady: true}, Usage: MeteredCondition{EpochID: q.EpochID, EpochSHA: h, AdmissionSHA: h}, LeaseExpires: q.Expires}
	q, e := SignCustomerConnectionChallenge(key, q)
	if e != nil || !VerifyCustomerConnectionChallenge(key, q, now) {
		t.Fatal(e)
	}
	if VerifyCustomerConnectionChallenge(key, q, q.Expires) || VerifyCustomerConnectionChallenge(key, q, q.Issued-1) || VerifyCustomerConnectionChallenge(bytes.Repeat([]byte{8}, 64), q, now) {
		t.Fatal("expiry or foreign key accepted")
	}
	edits := []func(*CustomerConnectionChallenge){
		func(x *CustomerConnectionChallenge) { x.Consent = false }, func(x *CustomerConnectionChallenge) { x.ClientID = "other" }, func(x *CustomerConnectionChallenge) { x.Process.PID++ }, func(x *CustomerConnectionChallenge) { x.Port++ }, func(x *CustomerConnectionChallenge) { x.EndpointSHA = strings.Repeat("c", 64) }, func(x *CustomerConnectionChallenge) { x.State.Usage.AdmissionSHA = strings.Repeat("d", 64) }, func(x *CustomerConnectionChallenge) { x.Expires++ },
	}
	r := CustomerConnectionReceipt{Challenge: q, Completed: now + 1, ResponseSHA: protocolHealthResponseSHA(q.ID)}
	r.Signature = ed25519.Sign(private, r.signingBytes())
	if !r.Verify(q) {
		t.Fatal("receipt rejected")
	}
	for n, edit := range edits {
		bad := q
		edit(&bad)
		if VerifyCustomerConnectionChallenge(key, bad, now) || r.Verify(bad) {
			t.Fatal("unbound mutation", n)
		}
	}
	bad := r
	bad.ResponseSHA = strings.Repeat("f", 64)
	bad.Signature = ed25519.Sign(private, bad.signingBytes())
	if bad.Verify(q) {
		t.Fatal("foreign response accepted")
	}
	bad = r
	bad.Completed = q.Expires
	bad.Signature = ed25519.Sign(private, bad.signingBytes())
	if bad.Verify(q) {
		t.Fatal("expired response accepted")
	}
	// A caller cannot mutate a returned saved receipt and thereby change durable
	// replay behavior. A pending attempt remains unknown and produces no proof.
	runtime := &Runtime{state: journal{CustomerConnection: &customerConnectionAttempt{Challenge: q, Receipt: &r}}}
	saved := runtime.SavedCustomerConnectionReceipt()
	saved.Signature[0] ^= 1
	saved.Challenge.Signer[0] ^= 1
	if !runtime.SavedCustomerConnectionReceipt().Verify(q) {
		t.Fatal("saved receipt alias")
	}
	runtime.state.CustomerConnection.Receipt = nil
	if runtime.SavedCustomerConnectionReceipt() != nil {
		t.Fatal("pending attempt invented proof")
	}
	raw, _ := json.Marshal(q)
	if bytes.Contains(raw, private) {
		t.Fatal("private signer serialized")
	}
}
