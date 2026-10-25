package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func TestInboundHubIsExplicitStableDestination(t *testing.T) {
	s, in, cookie, csrf := creationPlanAPIFixture(t)
	response := browserRequest(t, s.Handler(), "GET", "/api/v1/workspace/native-inbounds", nil, cookie, "", "")
	var payload struct {
		Nodes []inboundDestination `json:"nodes"`
	}
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.Nodes) != 3 {
		t.Fatal("hub absent from explicit destinations", response.Code)
	}
	hub := payload.Nodes[0]
	if hub.NodeID != localHubNodeID || hub.Kind != "hub" || len(hub.InstallationID) != 32 || hub.InstallationID != s.localHubInstallation() {
		t.Fatal("unstable hub identity")
	}
	state := browserRequest(t, s.Handler(), "GET", "/api/v1/workspace/native-inbounds-state", nil, cookie, "", "")
	var actual struct {
		Nodes []inboundDestination `json:"nodes"`
	}
	if state.Code != 200 || json.Unmarshal(state.Body.Bytes(), &actual) != nil || len(actual.Nodes) != 3 || actual.Nodes[0].NodeID != hub.NodeID || actual.Nodes[0].InstallationID != hub.InstallationID {
		t.Fatal("state polling omitted explicit hub")
	}
	for _, node := range s.registry.List() {
		if node.NodeID == localHubNodeID {
			t.Fatal("hub became paired remote node")
		}
	}
	for _, destination := range []inboundDestination{payload.Nodes[1], hub} {
		candidate := in
		candidate.Nodes = []business.NativeInboundPlanNode{{NodeID: destination.NodeID, InstallationID: destination.InstallationID, ListenIP: "198.51.100.20", DirectPort: 22443, DirectHosts: []string{"edge.example.org"}}}
		response = browserRequest(t, s.Handler(), "POST", "/api/v1/workspace/native-inbound-plans/preview", candidate, cookie, csrf, "https://"+browserHost)
		var preview business.NativeInboundPlanPreview
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &preview) != nil || len(preview.Topology.Nodes) != 1 || preview.Topology.Nodes[0].NodeID != destination.NodeID {
			t.Fatal("implicit hub or wrong destination", response.Code, response.Body.String())
		}
	}
	in.Nodes = []business.NativeInboundPlanNode{{NodeID: hub.NodeID, InstallationID: "ffffffffffffffffffffffffffffffff", ListenIP: "198.51.100.20", DirectPort: 22443}}
	response = browserRequest(t, s.Handler(), "POST", "/api/v1/workspace/native-inbound-plans/preview", in, cookie, csrf, "https://"+browserHost)
	if response.Code != http.StatusConflict {
		t.Fatal("forged hub installation accepted", response.Code)
	}
}
