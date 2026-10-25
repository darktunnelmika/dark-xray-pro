//go:build linux

package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	ix "github.com/darktunnelmika/dark-xray-pro/internal/inboundexec"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundruntime"
	"github.com/darktunnelmika/dark-xray-pro/internal/inboundspec"
)

func managedTestCore(t *testing.T) inboundruntime.BinaryConfig {
	t.Helper()
	path := os.Getenv("DARK_PRO_TEST_INBOUND_CORE")
	if path == "" {
		t.Skip("set DARK_PRO_TEST_INBOUND_CORE to the separately pinned v2 test engine")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	hash := sha256.Sum256(raw)
	return inboundruntime.BinaryConfig{Path: path, SHA256: hex.EncodeToString(hash[:])}
}
func managedTestPort(t *testing.T) uint16 {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}
func managedEcho(t *testing.T) (string, func()) {
	t.Helper()
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); _, _ = io.Copy(c, c) }()
		}
	}()
	return l.Addr().String(), func() { _ = l.Close(); <-done }
}
func managedVLESS(port uint16, uuid, target string, payload []byte) error {
	c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), time.Second)
	if e != nil {
		return e
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	id, e := hex.DecodeString(strings.ReplaceAll(uuid, "-", ""))
	if e != nil {
		return e
	}
	host, p, _ := net.SplitHostPort(target)
	n, _ := strconv.ParseUint(p, 10, 16)
	header := append([]byte{0}, id...)
	header = append(header, 0, 1, 0, 0, 1)
	binary.BigEndian.PutUint16(header[len(header)-3:len(header)-1], uint16(n))
	header = append(header, net.ParseIP(host).To4()...)
	if _, e = c.Write(append(header, payload...)); e != nil {
		return e
	}
	response := make([]byte, 2)
	if _, e = io.ReadFull(c, response); e != nil {
		return e
	}
	if response[0] != 0 || response[1] != 0 {
		return io.ErrUnexpectedEOF
	}
	got := make([]byte, len(payload))
	if _, e = io.ReadFull(c, got); e != nil {
		return e
	}
	if !bytes.Equal(payload, got) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func managedHTTP(port uint16, user, password, target string, payload []byte) error {
	c, e := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), time.Second)
	if e != nil {
		return e
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if _, e = fmt.Fprintf(c, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", target, target, base64.StdEncoding.EncodeToString([]byte(user+":"+password))); e != nil {
		return e
	}
	reader := bufio.NewReader(c)
	line, e := reader.ReadString('\n')
	if e != nil {
		return e
	}
	if !strings.Contains(line, " 200 ") {
		return fmt.Errorf("CONNECT rejected")
	}
	for {
		line, e = reader.ReadString('\n')
		if e != nil {
			return e
		}
		if line == "\r\n" {
			break
		}
	}
	if _, e = c.Write(payload); e != nil {
		return e
	}
	got := make([]byte, len(payload))
	if _, e = io.ReadFull(reader, got); e != nil {
		return e
	}
	if !bytes.Equal(got, payload) {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// This test crosses the real owner session/CSRF boundary, reviewed plan, SQLite
// accounting, TLS Agent API, durable worker outbox, gated Xray and TCP payload.
func TestManagedInboundRealOwnerToAgentAccountingSiblingsAndStop(t *testing.T) {
	core := managedTestCore(t)
	s, template, cookie, csrf := creationPlanAPIFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	if e := s.business.EnableUsageLedger(ctx); e != nil {
		t.Fatal(e)
	}
	if e := s.EnableInboundExecution(InboundExecutionConfig{Directory: filepath.Join(t.TempDir(), "hub-execution"), Native: core}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(s.inboundExecution.local.Close)
	client, e := s.business.CreateClient(ctx, business.NewClient{EventID: "managed-real-client-create-0001", ClientID: "managed-real-client", RepresentativeID: "owner", Name: "Managed real traffic", DurationDays: 30, VolumeBytes: 8 << 20})
	if e != nil {
		t.Fatal(e)
	}
	request := func(method, path string, input, output any, want int) {
		t.Helper()
		w := browserRequest(t, s.Handler(), method, path, input, cookie, csrf, "https://"+browserHost)
		if w.Code != want {
			t.Fatalf("%s %s: HTTP%d: %s", method, path, w.Code, w.Body.String())
		}
		if output != nil && json.Unmarshal(w.Body.Bytes(), output) != nil {
			t.Fatal("invalid owner response")
		}
	}
	create := func(id uint64, port uint16) {
		t.Helper()
		in := template
		in.EventID = "managed-real-create-" + strconv.FormatUint(id, 10)
		in.Profile = business.InboundInput{ID: id, Name: "Managed " + strconv.FormatUint(id, 10), Spec: &inboundspec.Spec{Version: 1, Protocol: "http", Network: "tcp", Security: "none"}}
		in.Nodes = []business.NativeInboundPlanNode{{NodeID: "de", InstallationID: strings.Repeat("b", 32), ListenIP: "127.0.0.1", DirectPort: port, DirectHosts: []string{"127.0.0.1"}, TunnelHosts: []string{}}}
		request("POST", "/api/v1/workspace/native-inbound-plans", in, nil, 201)
	}
	command := func(id uint64, action, event string) {
		t.Helper()
		base := "/api/v1/workspace/native-inbounds/" + strconv.FormatUint(id, 10) + "/execution"
		var state ix.State
		request("GET", base, nil, &state, 200)
		input := ix.CommandInput{EventID: event, Action: action, ExpectedRevision: state.Revision, ExpectedPlanRevision: state.PlanRevision, ExpectedInboundRevision: state.InboundRevision, ClientIDs: []string{client.ID}, AccountingMode: ix.AccountingPerClient, Reason: "Real reviewed integration operation"}
		var preview ix.Preview
		request("POST", base+"/preview", input, &preview, 200)
		if !preview.Valid {
			t.Fatalf("preview refused: %#v", preview.Blockers)
		}
		input.ReviewSHA = preview.ReviewSHA
		var result ix.Operation
		request("POST", base+"/commands", input, &result, 202)
		var replay ix.Operation
		request("POST", base+"/commands", input, &replay, 202)
		if !replay.Replayed || result.ID != replay.ID {
			t.Fatal("owner command did not replay exactly")
		}
	}
	port1, port2 := managedTestPort(t), managedTestPort(t)
	create(501, port1)
	command(501, "activate", "managed-real-activate-0001")
	username := func(id uint64) string {
		t.Helper()
		b, ok, e := s.business.InboundExecutionBundle(ctx, s.inboundKey, "de", strings.Repeat("b", 32))
		if e != nil || !ok {
			t.Fatal(e)
		}
		for _, p := range b.Principals {
			if p.InboundID == id && p.ClientID == client.ID {
				return strings.Split(p.Counter, ">>>")[1]
			}
		}
		t.Fatal("missing trusted user counter")
		return ""
	}
	nodes, e := s.business.ListInboundExecutionNodes(ctx)
	if e != nil || len(nodes) != 1 || nodes[0].NodeID != "de" {
		t.Fatal("node-only selection created a HUB execution", nodes, e)
	}
	var loseZero atomic.Bool
	var loseFinal atomic.Bool
	agentHandler := s.AgentHandler()
	agent := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lost := false
		if r.URL.Path == "/api/v1/agent/inbound-execution/usage" {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
			var report ix.UsageReport
			_ = json.Unmarshal(raw, &report)
			lost = !loseZero.Swap(true) || report.Final && loseFinal.Swap(false)
		}
		if lost {
			record := httptest.NewRecorder()
			agentHandler.ServeHTTP(record, r)
			if record.Code == 200 {
				http.Error(w, "lost committed response", 503)
			} else {
				w.WriteHeader(record.Code)
				_, _ = w.Write(record.Body.Bytes())
			}
			return
		}
		agentHandler.ServeHTTP(w, r)
	}))
	agent.TLS = &tls.Config{MinVersion: tls.VersionTLS13}
	agent.StartTLS()
	defer agent.Close()
	nodeConfig := inboundruntime.Config{Directory: filepath.Join(t.TempDir(), "node-execution"), NodeID: "de", InstallationID: strings.Repeat("b", 32), Key: []byte(strings.Repeat("3", 64)), Native: core}
	worker, e := inboundruntime.Open(nodeConfig)
	if e != nil {
		t.Fatal(e)
	}
	defer worker.Close()
	exchange, e := inboundruntime.NewHTTPExchange(agent.Client(), agent.URL, strings.Repeat("3", 64), "de", strings.Repeat("b", 32))
	if e != nil {
		t.Fatal(e)
	}
	target, stopEcho := managedEcho(t)
	defer stopEcho()
	payload := bytes.Repeat([]byte("managed-payload-"), 2048)
	if e = worker.Step(ctx, exchange); e == nil {
		t.Fatal("lost zero receipt must hold admission closed")
	}
	if e = managedHTTP(port1, username(501), client.UUID, target, payload); e == nil {
		t.Fatal("customer admitted before the durable zero receipt was acknowledged")
	}
	if e = worker.Step(ctx, exchange); e != nil {
		t.Fatal("zero receipt replay/admission", e)
	}
	if e = managedHTTP(port1, username(501), client.UUID, target, payload); e != nil {
		t.Fatal("real authenticated HTTP traffic", e)
	}
	if e = worker.Step(ctx, exchange); e != nil {
		t.Fatal("usage checkpoint", e)
	}
	usage, e := s.business.ClientUsage(ctx, client.ID)
	if e != nil || usage.Total != int64(len(payload)*2) {
		t.Fatalf("real exact payload charge=%d want=%d: %v", usage.Total, len(payload)*2, e)
	}
	var state ix.State
	request("GET", "/api/v1/workspace/native-inbounds/501/execution", nil, &state, 200)
	if state.ObservedState != "active" || len(state.Targets) != 1 || !state.Targets[0].ProcessReady || state.Targets[0].HealthState != "engine_ready" || state.ActiveRevision == 0 {
		t.Fatalf("real execution evidence missing: %#v", state)
	}
	var subscription struct {
		ix.SubscriptionView
		Enabled bool `json:"enabled"`
	}
	subBase := "/api/v1/workspace/clients/" + client.ID + "/inbound-subscription"
	request("GET", subBase, nil, &subscription, 200)
	if !subscription.Enabled || !subscription.Available || subscription.RouteCount != 1 || subscription.PathVerified {
		t.Fatal("managed subscription evidence incorrect")
	}
	request("POST", subBase+"/issue", map[string]any{"event_id": "managed-real-subscription-issue", "expected_generation": 0}, nil, 200)
	var secret struct {
		Path string `json:"path"`
	}
	request("POST", subBase+"/reveal", map[string]any{"expected_generation": 1}, &secret, 200)
	public := httptest.NewRecorder()
	s.Handler().ServeHTTP(public, httptest.NewRequest("GET", secret.Path+"?format=uri", nil))
	if public.Code != 200 || !strings.Contains(public.Body.String(), "127.0.0.1") || public.Header().Get("Cache-Control") != "no-store, private" {
		t.Fatal("current subscription unavailable or cacheable", public.Code)
	}
	create(502, port2)
	command(502, "activate", "managed-real-activate-0002")
	if e = worker.Step(ctx, exchange); e != nil {
		t.Fatal("drain for sibling apply", e)
	}
	if e = worker.Step(ctx, exchange); e != nil {
		t.Fatal("start complete sibling config", e)
	}
	for i, port := range []uint16{port1, port2} {
		if e = managedHTTP(port, username(uint64(501+i)), client.UUID, target, payload); e != nil {
			t.Fatal("sibling lost after complete-node apply", port, e)
		}
	}
	if e = worker.Step(ctx, exchange); e != nil {
		t.Fatal(e)
	}
	usage, e = s.business.ClientUsage(ctx, client.ID)
	if e != nil || usage.Total != int64(len(payload)*6) {
		t.Fatal("sibling account duplicated/omitted counters", usage.Total, e)
	}
	command(501, "stop", "managed-real-stop-0001")
	if e = worker.Step(ctx, exchange); e != nil {
		t.Fatal(e)
	}
	if e = worker.Step(ctx, exchange); e != nil {
		t.Fatal(e)
	}
	if e = managedHTTP(port1, "stopped", client.UUID, target, payload); e == nil {
		t.Fatal("stopped listener still admitted customer")
	}
	if e = managedHTTP(port2, username(502), client.UUID, target, payload); e != nil {
		t.Fatal("stopping one inbound stopped its sibling", e)
	}
	command(502, "stop", "managed-real-stop-0002")
	loseFinal.Store(true)
	if e = worker.Step(ctx, exchange); e == nil {
		t.Fatal("lost final receipt unexpectedly acknowledged")
	}
	worker.Close()
	worker, e = inboundruntime.Open(nodeConfig)
	if e != nil {
		t.Fatal("reopen durable final outbox", e)
	}
	defer worker.Close()
	if e = worker.Step(ctx, exchange); e != nil {
		t.Fatal("final receipt replay after supervisor restart", e)
	}
	if e = managedHTTP(port2, "stopped", client.UUID, target, payload); e == nil {
		t.Fatal("last stopped listener survived")
	}
	usage, e = s.business.ClientUsage(ctx, client.ID)
	if e != nil || usage.Total != int64(len(payload)*8) {
		t.Fatal("final usage was not committed exactly", usage.Total, e)
	}
	public = httptest.NewRecorder()
	s.Handler().ServeHTTP(public, httptest.NewRequest("GET", secret.Path+"?format=uri", nil))
	if public.Code != 404 {
		t.Fatal("stopped generations remained publicly published", public.Code)
	}
	for _, path := range []string{"/api/v1/agent/inbound-execution/poll", "/api/v1/agent/inbound-execution/register", "/api/v1/agent/inbound-execution/usage"} {
		w := browserRequest(t, s.Handler(), "POST", path, map[string]string{}, cookie, csrf, "https://"+browserHost)
		if w.Code != 404 {
			t.Fatal("Agent runtime exposed on browser surface", path, w.Code)
		}
	}
}
