//go:build linux

package managedxray

import (
	"bytes"
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func protocolHealthTestRequest(t *testing.T) ([]byte, ProtocolHealthRequest) {
	t.Helper()
	key := bytes.Repeat([]byte{0x73}, 32)
	now := time.Now()
	q, err := SignProtocolHealthRequest(key, ProtocolHealthRequest{Version: ProtocolHealthVersion, SystemProbeID: "system-probe-test", ID: strings.Repeat("a", 32), HandoverID: strings.Repeat("b", 32), NodeID: "target", InstallationID: strings.Repeat("c", 32), RuntimeInstance: strings.Repeat("d", 32), StageID: strings.Repeat("e", 32), ApplyCommandID: strings.Repeat("f", 32), InboundID: 7612, Port: 27612, CredentialSHA: strings.Repeat("a", 64), EndpointSHA: strings.Repeat("b", 64), Expected: RuntimeCondition{ProcessReady: true, ActiveSequence: 3, ActiveSHA: strings.Repeat("c", 64)}, Usage: MeteredCondition{EpochID: strings.Repeat("d", 32), EpochSHA: strings.Repeat("e", 64), AdmissionSHA: strings.Repeat("f", 64)}, Issued: now.UnixNano(), Expires: now.Add(6 * time.Second).UnixNano()})
	if err != nil {
		t.Fatal(err)
	}
	return key, q
}

func TestProtocolHealthReceiptBindsRequestChallengeAndExpiry(t *testing.T) {
	key, q := protocolHealthTestRequest(t)
	p := ProtocolHealthReceipt{Version: ProtocolHealthVersion, RequestSHA: hashJSON(q), ResponseSHA: protocolHealthResponseSHA(q.ID), Completed: time.Now().UnixNano()}
	p.Signature = transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2", p)
	if !VerifyProtocolHealthReceipt(key, q, p, time.Now()) {
		t.Fatal("valid evidence rejected")
	}
	for _, mutate := range []func(*ProtocolHealthRequest){
		func(q *ProtocolHealthRequest) { q.SystemProbeID = "other-system-probe" },
		func(q *ProtocolHealthRequest) { q.ID = strings.Repeat("0", 32) },
		func(q *ProtocolHealthRequest) { q.HandoverID = strings.Repeat("0", 32) },
		func(q *ProtocolHealthRequest) { q.NodeID = "different" },
		func(q *ProtocolHealthRequest) { q.InstallationID = strings.Repeat("0", 32) },
		func(q *ProtocolHealthRequest) { q.RuntimeInstance = strings.Repeat("0", 32) },
		func(q *ProtocolHealthRequest) { q.StageID = strings.Repeat("0", 32) },
		func(q *ProtocolHealthRequest) { q.ApplyCommandID = strings.Repeat("0", 32) },
		func(q *ProtocolHealthRequest) { q.CredentialSHA = strings.Repeat("0", 64) },
		func(q *ProtocolHealthRequest) { q.EndpointSHA = strings.Repeat("0", 64) },
		func(q *ProtocolHealthRequest) { q.Usage.EpochID = strings.Repeat("0", 32) },
		func(q *ProtocolHealthRequest) { q.Expected.ActiveSequence++ },
		func(q *ProtocolHealthRequest) { q.Port++ },
		func(q *ProtocolHealthRequest) { q.InboundID++ },
	} {
		bad := q
		mutate(&bad)
		bad, _ = SignProtocolHealthRequest(key, bad)
		if VerifyProtocolHealthReceipt(key, bad, p, time.Now()) {
			t.Fatal("proof transplanted")
		}
	}
	if VerifyProtocolHealthReceipt(bytes.Repeat([]byte{0x74}, 32), q, p, time.Now()) || VerifyProtocolHealthReceipt(key, q, p, time.Unix(0, q.Expires)) || VerifyProtocolHealthReceipt(key, q, p, time.Unix(0, q.Issued-1)) {
		t.Fatal("wrong key/expired/future evidence")
	}
	for _, mutate := range []func(*ProtocolHealthReceipt){
		func(p *ProtocolHealthReceipt) { p.Completed = q.Expires },
		func(p *ProtocolHealthReceipt) { p.Completed = q.Issued - 1 },
		func(p *ProtocolHealthReceipt) { p.ResponseSHA = strings.Repeat("0", 64) },
		func(p *ProtocolHealthReceipt) { p.Version = 1 },
	} {
		bad := p
		mutate(&bad)
		bad.Signature = ""
		bad.Signature = transactionMAC(key, "DARK-PRO-PROTOCOL-HEALTH-RECEIPT-v2", bad)
		if VerifyProtocolHealthReceipt(key, q, bad, time.Now()) {
			t.Fatal("invalid signed evidence accepted")
		}
	}
	bad := p
	bad.Signature = ""
	bad.Signature = transactionMAC(key, "DARK-PRO-ROUTE-RECEIPT-v1", bad)
	if VerifyProtocolHealthReceipt(key, q, bad, time.Now()) {
		t.Fatal("execution ACK MAC domain accepted")
	}
}

func TestProtocolHealthEndpointRejectsUnapprovedAddressAndTLSBypass(t *testing.T) {
	e := ProtocolHealthEndpoint{Address: "11.0.0.1:443", ServerName: "verifier.example.org", CertificateSHA: strings.Repeat("a", 64)}
	config, err := e.tlsConfig()
	if err != nil || config.InsecureSkipVerify || config.MinVersion != 0x304 || config.VerifyConnection == nil {
		t.Fatal("ordinary TLS verification disabled", err)
	}
	for _, address := range []string{"127.0.0.1:443", "10.0.0.1:443", "169.254.169.254:80", "0.0.0.0:443", "224.0.0.1:443", "[::1]:443", "verifier.example.org:443", "11.0.0.1:0", "11.0.0.1:65536", "011.0.0.1:443"} {
		bad := e
		bad.Address = address
		if _, err := bad.tlsConfig(); err == nil {
			t.Fatal("unsafe address", address)
		}
	}
	for _, name := range []string{"", "127.0.0.1", "evil.example.org/path", "evil.example.org\nHost: other", "user@evil.example.org"} {
		bad := e
		bad.ServerName = name
		if _, err := bad.tlsConfig(); err == nil {
			t.Fatal("invalid TLS identity", name)
		}
	}
	bad := e
	bad.CABundle = []byte("not a CA")
	if _, err := bad.tlsConfig(); err == nil {
		t.Fatal("invalid explicit CA fell back to default trust")
	}
	bad = e
	bad.CertificateSHA = ""
	if _, err := bad.tlsConfig(); err == nil {
		t.Fatal("missing pin accepted")
	}
	bad = e
	bad.ServerName = "other.example.org"
	if bad.SHA() == e.SHA() {
		t.Fatal("endpoint identity not bound")
	}
}

func TestProtocolHealthRejectsOverlongOrUnboundRequests(t *testing.T) {
	key, q := protocolHealthTestRequest(t)
	for _, mutate := range []func(*ProtocolHealthRequest){
		func(q *ProtocolHealthRequest) { q.Version = 1 },
		func(q *ProtocolHealthRequest) { q.SystemProbeID = "" },
		func(q *ProtocolHealthRequest) { q.Expires = q.Issued + int64(9*time.Second) },
		func(q *ProtocolHealthRequest) { q.Expires = q.Issued },
		func(q *ProtocolHealthRequest) { q.Issued = 0 },
		func(q *ProtocolHealthRequest) { q.Expected.ProcessReady = false },
		func(q *ProtocolHealthRequest) { q.Usage.EpochID = "" },
		func(q *ProtocolHealthRequest) { q.CredentialSHA = "" },
		func(q *ProtocolHealthRequest) { q.Port = 443 },
	} {
		bad := q
		mutate(&bad)
		if _, err := SignProtocolHealthRequest(key, bad); err == nil {
			t.Fatal("invalid challenge signed")
		}
	}
	if _, err := SignProtocolHealthRequest([]byte("short"), q); err == nil {
		t.Fatal("short key signed")
	}
	unsigned := q
	unsigned.Signature = ""
	if verifyProtocolHealthRequest(key, unsigned, time.Now().UnixNano()) {
		t.Fatal("unsigned challenge accepted")
	}
}

func TestProtocolHealthSOCKSRejectsMalformedRepliesAndBoundsCancellation(t *testing.T) {
	for _, reply := range [][]byte{{5, 1, 0, 1, 0, 0, 0, 0, 0, 0}, {4, 0, 0, 1, 0, 0, 0, 0, 0, 0}, {5, 0, 1, 1, 0, 0, 0, 0, 0, 0}, {5, 0, 0, 9}, {5, 0, 0, 1, 0}, {5, 0, 0, 1, 0, 0, 0, 0, 0, 0}} {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		done := make(chan struct{})
		go func() {
			defer close(done)
			c, err := listener.Accept()
			if err != nil {
				return
			}
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(time.Second))
			hello := make([]byte, 3)
			if _, err = io.ReadFull(c, hello); err != nil {
				return
			}
			_, _ = c.Write([]byte{5, 0})
			request := make([]byte, 10)
			if _, err = io.ReadFull(c, request); err != nil {
				return
			}
			for _, b := range reply {
				_, _ = c.Write([]byte{b})
			}
		}()
		c, err := protocolHealthSOCKS(ctx, listener.Addr().String(), "11.0.0.1:443")
		good := bytes.Equal(reply, []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
		if (err == nil) != good {
			t.Fatal("malformed SOCKS reply contract", reply, err)
		}
		if c != nil {
			c.Close()
		}
		cancel()
		listener.Close()
		<-done
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	stopAccept := context.AfterFunc(ctx, func() { listener.Close() })
	defer stopAccept()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := listener.Accept()
		if err == nil {
			defer c.Close()
			<-ctx.Done()
		}
	}()
	if c, err := protocolHealthSOCKS(ctx, listener.Addr().String(), "11.0.0.1:443"); err == nil || c != nil {
		t.Fatal("silent SOCKS peer escaped deadline")
	}
	<-done
}
