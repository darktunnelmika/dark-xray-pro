package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/agentidentity"
	"github.com/darktunnelmika/dark-xray-pro/internal/agenttransport"
	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundsecurity"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

type securityFixtureResolver struct{}

func (securityFixtureResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return []net.IPAddr{{IP: net.ParseIP("8.8.4.4")}}, nil
}

type securityTargetConnection struct{ net.Conn }

func (securityTargetConnection) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("8.8.4.4"), Port: 443}
}

func nodeSecurityTLSFixture(t *testing.T, node string) (*inboundsecurity.Service, *atomic.Int32) {
	t.Helper()
	now := time.Now()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	certificate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "target.agent.test"},
		DNSNames: []string{"target.agent.test"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(parsed)
	var dials atomic.Int32
	service, err := inboundsecurity.New(inboundsecurity.Config{LocalNodeID: node, RootCAs: pool, Resolver: securityFixtureResolver{}, ProbeTimeout: 2 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != "8.8.4.4:443" {
				return nil, errors.New("unrecognized fixture address")
			}
			dials.Add(1)
			client, server := net.Pipe()
			go func() {
				defer server.Close()
				secure := tls.Server(server, &tls.Config{MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
					NextProtos: []string{"h2"}, CurvePreferences: []tls.CurveID{tls.X25519},
					Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
				_ = secure.HandshakeContext(ctx)
			}()
			return securityTargetConnection{client}, nil
		}})
	if err != nil {
		t.Fatal(err)
	}
	return service, &dials
}

func TestInboundSecuritySelectedAgentRealTLSAndNoHubFallback(t *testing.T) {
	registry, err := noderegistry.Open(filepath.Join(t.TempDir(), "hub", "registry.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	identity := agentidentity.Identity{NodeID: "security-node", InstallationID: strings.Repeat("a", 32), AgentToken: strings.Repeat("b", 64), Paired: true}
	invite, err := registry.Issue(identity.NodeID, "Selected security Node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Claim(invite.PairCode, identity.NodeID, identity.InstallationID, identity.AgentToken); err != nil {
		t.Fatal(err)
	}
	hub, err := api.NewServer(telemetry.NewStore(15*time.Second, time.Now), registry, "fixture-owner-token-at-least-32-characters")
	if err != nil {
		t.Fatal(err)
	}
	var hubDials atomic.Int32
	hubConfig := inboundsecurity.Config{NodeProbe: hub.ProbeSelectedNode, DialContext: func(context.Context, string, string) (net.Conn, error) {
		hubDials.Add(1)
		return nil, errors.New("HUB must never dial selected Node target")
	}}
	if err := hub.EnableInboundSecurity(hubConfig); err != nil {
		t.Fatal(err)
	}
	hubTools, err := inboundsecurity.New(hubConfig)
	if err != nil {
		t.Fatal(err)
	}
	// The Hub/Agent channel is real local TLS, using the same pinned transport
	// constructor as the production Node agent. The target TLS is net.Pipe.
	remote := httptest.NewTLSServer(hub.AgentHandler())
	defer remote.Close()
	pin := sha256.Sum256(remote.Certificate().Raw)
	client, endpoint, err := agenttransport.New(agenttransport.Config{Endpoint: remote.URL, PinSHA256: hex.EncodeToString(pin[:]),
		CABundle: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: remote.Certificate().Raw})})
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	nodeTools, nodeDials := nodeSecurityTLSFixture(t, identity.NodeID)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		runInboundSecurityWorkerWithService(ctx, client, endpoint, identity, nodeTools)
	}()
	result, err := hubTools.Probe(ctx, inboundsecurity.ProbeRequest{Scope: "node", NodeID: identity.NodeID, Target: "target.agent.test:443", ServerName: "target.agent.test"})
	if err != nil || !result.OK || !result.Suitable || result.Scope != "node" || result.NodeID != identity.NodeID || result.IP != "8.8.4.4" ||
		!result.TLS13 || !result.H2 || !result.X25519 || !result.Certificate.Valid || nodeDials.Load() != 1 || hubDials.Load() != 0 {
		t.Fatalf("selected Agent did not produce its own real TLS proof: result=%+v error=%v node_dials=%d hub_dials=%d", result, err, nodeDials.Load(), hubDials.Load())
	}
	cancel()
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("Node security worker did not stop with context")
	}
	if _, err := hubTools.Probe(context.Background(), inboundsecurity.ProbeRequest{Scope: "node", NodeID: "unknown-node", Target: "target.agent.test:443"}); err == nil || hubDials.Load() != 0 {
		t.Fatal("unknown selected Node fell back to HUB")
	}
}

type securityRoundTripper func(*http.Request) (*http.Response, error)

func (f securityRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestInboundSecurityWorkerRevalidatesPublicTargetAndRejectsOtherScope(t *testing.T) {
	identity := agentidentity.Identity{NodeID: "security-node", InstallationID: strings.Repeat("a", 32), AgentToken: strings.Repeat("b", 64), Paired: true}
	service, dials := nodeSecurityTLSFixture(t, identity.NodeID)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var results atomic.Int32
	client := &http.Client{Transport: securityRoundTripper(func(request *http.Request) (*http.Response, error) {
		if request.Header.Get("Authorization") != "Bearer "+identity.AgentToken || request.Header.Get("Content-Type") != "application/json" {
			t.Error("worker omitted installation authentication")
		}
		if strings.HasSuffix(request.URL.Path, "/result") {
			var body struct {
				NodeID         string                      `json:"node_id"`
				InstallationID string                      `json:"installation_id"`
				Result         inboundsecurity.ProbeResult `json:"result"`
			}
			if json.NewDecoder(request.Body).Decode(&body) != nil || body.NodeID != identity.NodeID || body.InstallationID != identity.InstallationID || body.Result.Scope != "node" || body.Result.NodeID != identity.NodeID || body.Result.Error != "node_probe_failed" || body.Result.OK {
				t.Error("private target rejection was represented as successful")
			}
			results.Add(1)
			cancel()
			return &http.Response{StatusCode: http.StatusNoContent, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		}
		task := inboundSecurityTask{ID: strings.Repeat("c", 32), Request: inboundsecurity.ProbeRequest{Scope: "node", NodeID: identity.NodeID, Target: "127.0.0.1:443"}, Expires: time.Now().Add(10 * time.Second).Unix()}
		raw, _ := json.Marshal(task)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
	})}
	runInboundSecurityWorkerWithService(ctx, client, "https://pinned-hub.example", identity, service)
	if results.Load() != 1 || dials.Load() != 0 {
		t.Fatal("Node trusted HUB job without rechecking its target")
	}
	for _, kind := range []string{"hub_scope", "different_node", "expired", "future", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			var unexpectedResult atomic.Int32
			client := &http.Client{Transport: securityRoundTripper(func(request *http.Request) (*http.Response, error) {
				if strings.HasSuffix(request.URL.Path, "/result") {
					unexpectedResult.Add(1)
				}
				task := inboundSecurityTask{ID: strings.Repeat("c", 32), Request: inboundsecurity.ProbeRequest{Scope: "node", NodeID: identity.NodeID, Target: "target.agent.test:443"}, Expires: time.Now().Add(10 * time.Second).Unix()}
				switch kind {
				case "hub_scope":
					task.Request.Scope = "hub"
				case "different_node":
					task.Request.NodeID = "other-node"
				case "expired":
					task.Expires = time.Now().Add(-time.Second).Unix()
				case "future":
					task.Expires = time.Now().Add(time.Minute).Unix()
				}
				raw, _ := json.Marshal(task)
				if kind == "duplicate" {
					raw = []byte(strings.Replace(string(raw), `"scope":"node"`, `"scope":"hub","scope":"node"`, 1))
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw))), Header: make(http.Header)}, nil
			})}
			runInboundSecurityWorkerWithService(ctx, client, "https://pinned-hub.example", identity, service)
			if unexpectedResult.Load() != 0 || dials.Load() != 0 {
				t.Fatal("invalid scope, lifetime, or duplicate job started work")
			}
		})
	}
}
