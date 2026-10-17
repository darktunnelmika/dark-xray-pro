package api

import (
	"bytes"
	"context"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestNativeOwnerCannotSaveIdenticalDirectAndSwapPorts(t *testing.T) {
	s := enabledOwner(t)
	if e := s.EnableNativeInbounds(bytes.Repeat([]byte{12}, 32)); e != nil {
		t.Fatal(e)
	}
	ticket, e := s.registry.Issue("port-qa-node", "Independent PRO QA")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.registry.Claim(ticket.PairCode, "port-qa-node", installID, nodeToken); e != nil {
		t.Fatal(e)
	}
	cookie, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	in := business.InboundInput{ID: 9191, Name: "Port safety", Transport: "tcp", RealityTarget: "www.example.org:443", ServerNames: []string{"www.example.org"}, ShortIDs: []string{"aa11"}}
	make := browserRequest(t, s.Handler(), "POST", "/api/v1/workspace/native-inbounds", in, cookie, csrf, "https://"+browserHost)
	if make.Code != 201 {
		t.Fatalf("test inbound create failed HTTP %d", make.Code)
	}
	target := business.InboundTarget{InboundID: 9191, NodeID: "port-qa-node", InstallationID: installID, ListenIP: "127.0.0.1", DirectPort: 19600, SwapPort: 19600}
	w := browserRequest(t, s.Handler(), "POST", "/api/v1/workspace/native-inbounds/9191/target", target, cookie, csrf, "https://"+browserHost)
	if w.Code != 422 {
		t.Fatalf("server must reject same Direct and Swap port; HTTP %d: %s", w.Code, w.Body.String())
	}
	targets, err := s.business.ListInboundTargets(context.Background())
	if err != nil || len(targets) != 0 {
		t.Fatalf("invalid target unexpectedly persisted: %+v %v", targets, err)
	}
	target.SwapPort = 19601
	allowed := browserRequest(t, s.Handler(), "POST", "/api/v1/workspace/native-inbounds/9191/target", target, cookie, csrf, "https://"+browserHost)
	if allowed.Code != 200 {
		t.Fatalf("distinct Direct and Swap ports should still work: %d", allowed.Code)
	}
}
