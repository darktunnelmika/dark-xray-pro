//go:build linux

package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	"github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
)

func TestRealPinnedRemoteAgentMeteredAdmissionFromBirthToZeroReceipt(t *testing.T) {
	binary, pin := os.Getenv("DARK_PRO_TEST_XRAY_BINARY"), os.Getenv("DARK_PRO_TEST_XRAY_SHA256")
	if binary == "" && pin == "" {
		t.Skip("pinned stock Xray required")
	}
	if binary == "" || len(pin) != 64 || os.Geteuid() == 0 {
		t.Fatal("pinned real stock Xray, unprivileged test required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	s := enabledOwner(t)
	key := bytes.Repeat([]byte{9}, 32)
	if err := s.EnableNativeInbounds(key); err != nil {
		t.Fatal(err)
	}
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	inv, err := s.registry.Issue("remote-qa", "Ephemeral remote test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.registry.Claim(inv.PairCode, "remote-qa", installID, nodeToken); err != nil {
		t.Fatal(err)
	}
	if err = s.EnableRemoteMeteredNode("remote-qa"); err != nil {
		t.Fatal(err)
	}
	customer, err := s.business.CreateClient(ctx, business.NewClient{
		ClientID: "remote-qa-client", EventID: "remote-client-create-001",
		RepresentativeID: "owner", Name: "Disposable", DurationDays: 30, VolumeBytes: 1 << 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.business.CreateInbound(ctx, key, business.InboundInput{ID: 665,
		Name: "Real isolated", Transport: "tcp", RealityTarget: "www.example.org:443",
		ServerNames: []string{"www.example.org"}, ShortIDs: []string{"aabb"}}); err != nil {
		t.Fatal(err)
	}
	if err = s.business.BindInboundClients(ctx, 665, []string{customer.ID}, true); err != nil {
		t.Fatal(err)
	}
	socket, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint16(socket.Addr().(*net.TCPAddr).Port)
	socket.Close()
	if err = s.business.PutInboundTarget(ctx, business.InboundTarget{InboundID: 665,
		NodeID: "remote-qa", InstallationID: installID, ListenIP: "127.0.0.1", DirectPort: port}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(s.AgentHandler())
	srv.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	srv.StartTLS()
	defer srv.Close()
	cert := srv.Certificate()
	pinned := sha256.Sum256(cert.Raw)
	client, endpoint, err := agenttransport.New(agenttransport.Config{
		Endpoint: srv.URL, PinSHA256: hex.EncodeToString(pinned[:]),
		CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw}),
	})
	if err != nil {
		t.Fatal(err)
	}
	sender, err := uc.NewSender(uc.SenderOptions{Endpoint: endpoint, NodeID: "remote-qa",
		InstallationID: installID, Token: nodeToken, PinSHA256: hex.EncodeToString(pinned[:]), Client: client})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.Close()
	r, err := managedxray.Open(managedxray.Options{
		Directory: filepath.Join(t.TempDir(), "native-runtime"),
		StateKey:  bytes.Repeat([]byte{6}, 32),
		LeaseKey:  []byte(nodeToken), NodeID: "remote-qa", InstallationID: installID,
		ListenIP: "127.0.0.1", Binary: binary, BinarySHA: pin,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// Baseline may be received only after API-only birth and there must be no
	// customer listener before this trusted zero-receipt sequence completes.
	first, err := r.MeteredRemoteSync(ctx, client, endpoint, sender)
	if err != nil {
		t.Fatalf("remote registered Xray birth/zero receipt admission failed: %v phase=%s", err, first.Phase)
	}
	if !first.ProcessReady || first.Usage == nil || !first.Usage.Running || first.Usage.Receipts < 1 ||
		first.DataPlaneVerified || first.SubscriptionPublished {
		t.Fatalf("readiness receipt or scoped observer false: %+v", first)
	}
	connection, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), time.Second)
	if err != nil {
		t.Fatal("real isolated Xray listener never opened AFTER zero receipt", err)
	}
	connection.Close()
	account, err := s.business.ClientUsage(ctx, customer.ID)
	if err != nil || !account.Known || account.Total != 0 {
		t.Fatalf("zero baseline created false billable usage: %+v %v", account, err)
	}
	// Fresh short signed lease on SAME process must not start a second
	// process, duplicate the first epoch or reset the ledger.
	repeated, err := r.MeteredRemoteSync(ctx, client, endpoint, sender)
	if err != nil || !repeated.ProcessReady || repeated.ActiveSequence != first.ActiveSequence {
		t.Fatalf("trusted lease renewal failed / replaced Xray: %+v %v", repeated, err)
	}
	n, err := s.business.ClientUsage(ctx, customer.ID)
	if err != nil || n.Total != 0 {
		t.Fatal("lease renewal fabricated consumption", err)
	}
	r.Close()
	if snapshot := r.Snapshot(); snapshot.ProcessReady {
		t.Fatal("closed runtime still claims customer listener")
	}
	wait := time.Now().Add(4 * time.Second)
	for time.Now().Before(wait) {
		c, e := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
		if e != nil {
			break
		}
		c.Close()
		time.Sleep(40 * time.Millisecond)
	}
	c, err := net.DialTimeout("tcp4", fmt.Sprintf("127.0.0.1:%d", port), 100*time.Millisecond)
	if err == nil {
		c.Close()
		t.Fatal("runtime close did not remove isolated customer listener")
	}
}
