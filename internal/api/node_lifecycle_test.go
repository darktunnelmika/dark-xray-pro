package api

import (
	"context"
	"encoding/json"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"net/http"
	"strings"
	"testing"
)

func TestOwnerNodeLifecycleFencesAndDetachesWithoutAgent(t *testing.T) {
	s, plan, cookie, csrf := creationPlanAPIFixture(t)
	ctx := context.Background()
	if err := s.business.EnableInboundExecution(ctx); err != nil {
		t.Fatal(err)
	}
	s.inboundExecution = &inboundExecutionService{}
	h := s.BrowserHandler()
	origin := "https://" + browserHost
	w := browserRequest(t, h, "POST", "/api/v1/workspace/native-inbound-plans", plan, cookie, csrf, origin)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	base := "/api/v1/workspace/nodes/de/lifecycle"
	in := noderegistry.LifecycleInput{EventID: strings.Repeat("c", 32), InstallationID: strings.Repeat("b", 32), ExpectedRevision: 1, Action: "disable"}
	for _, tc := range []struct {
		cookie       *http.Cookie
		csrf, origin string
		want         int
	}{{nil, "", "", 401}, {cookie, "", origin, 403}, {cookie, csrf, "https://evil.example", 403}} {
		if w = browserRequest(t, h, "POST", base, in, tc.cookie, tc.csrf, tc.origin); w.Code != tc.want {
			t.Fatal(w.Code, tc.want)
		}
	}
	for i := 0; i < 2; i++ {
		w = browserRequest(t, h, "POST", base, in, cookie, csrf, origin)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	var rec noderegistry.LifecycleReceipt
	if json.Unmarshal(w.Body.Bytes(), &rec) != nil || !rec.Completed || rec.State.Status != "disabled" {
		t.Fatal(w.Body.String())
	}
	p, err := s.business.NativeInboundPlan(ctx, s.inboundKey, plan.Profile.ID)
	if err != nil || len(p.Nodes) != 0 {
		t.Fatal(p, err)
	}
	caps := browserRequest(t, h, "GET", "/api/v1/workspace/native-inbound-plans/capabilities", nil, cookie, "", "")
	if caps.Code != 200 || !strings.Contains(caps.Body.String(), `"node_ids":[]`) {
		t.Fatal("last-node detachment broke global capabilities", caps.Code, caps.Body.String())
	}
	if s.registry.DeploymentAllowed("de", in.InstallationID) {
		t.Fatal("disabled node admitted")
	}
	if err = s.nativePlanReferences(plan.Nodes); err == nil {
		t.Fatal("disabled node could be reattached")
	}
	in = noderegistry.LifecycleInput{EventID: strings.Repeat("d", 32), InstallationID: strings.Repeat("b", 32), ExpectedRevision: 2, Action: "enable"}
	if w = browserRequest(t, h, "POST", base, in, cookie, csrf, origin); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	p, err = s.business.NativeInboundPlan(ctx, s.inboundKey, plan.Profile.ID)
	if err != nil || len(p.Nodes) != 0 {
		t.Fatal("enable restored old routes", p, err)
	}
	in = noderegistry.LifecycleInput{EventID: strings.Repeat("e", 32), InstallationID: strings.Repeat("b", 32), ExpectedRevision: 3, Action: "remove"}
	if w = browserRequest(t, h, "POST", base, in, cookie, csrf, origin); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = browserRequest(t, h, "GET", "/api/v1/workspace/nodes", nil, cookie, "", "")
	if strings.Contains(w.Body.String(), `"node_id":"de"`) {
		t.Fatal("removed node still listed", w.Body.String())
	}
	// Accounting authentication is retained, while deployment is fenced.
	if _, ok := s.registry.Authenticate("de", strings.Repeat("b", 32), strings.Repeat("3", 64)); !ok {
		t.Fatal("settlement identity lost")
	}
}
