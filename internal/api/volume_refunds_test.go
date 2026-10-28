package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
)

func volumeRefundServer(t *testing.T) (*Server, business.Client) {
	t.Helper()
	s := enabledOwner(t)
	ctx := context.Background()
	if err := s.business.EnableUsageLedger(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableNativeInbounds(bytes.Repeat([]byte{7}, 32)); err != nil {
		t.Fatal(err)
	}
	if err := s.EnableVolumeRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	for _, rep := range []string{"refund-rep", "refund-other"} {
		if _, err := s.business.CreateRepresentative(ctx, rep, rep); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.ProvisionRepresentativeLogin(ctx, rep, rep, repPassword); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.business.AdjustCredits(ctx, business.CreditChange{EventID: "refund-api-initial-credit-01", RepresentativeID: "refund-rep", VolumeDeltaBytes: 1000, UnlimitedDeltaUnits: 10, Reason: "Synthetic volume refund acceptance"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.business.CreateClient(ctx, business.NewClient{EventID: "refund-api-new-volume-0001", ClientID: "refund-volume", RepresentativeID: "refund-rep", Name: "Unused volume", DurationDays: 30, VolumeBytes: 100})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.business.CreateClient(ctx, business.NewClient{EventID: "refund-api-new-unlimited-01", ClientID: "refund-unlimited", RepresentativeID: "refund-rep", Name: "Unchanged unlimited", DurationDays: 30, IPCount: 2, Unlimited: true}); err != nil {
		t.Fatal(err)
	}
	return s, c
}

func TestVolumeRefundAutomaticArchiveAndTokenIsolation(t *testing.T) {
	s, client := volumeRefundServer(t)
	ctx := context.Background()
	rep, csrf := browserLogin(t, s, "refund-rep", repPassword)
	h := s.BrowserHandler()
	state := func(id, action string) {
		t.Helper()
		w := browserRequest(t, h, "POST", "/api/v1/workspace/clients/"+id+"/state", business.SetClientState{EventID: "refund-api-" + action + "-" + id, ClientID: id, Action: action, Reason: "Representative manual action"}, rep, csrf, "https://"+browserHost)
		if w.Code != 200 {
			t.Fatal("client state", w.Code, w.Body.String())
		}
	}
	state(client.ID, "disable")
	if batch, err := s.business.SweepExpiredVolumeRefunds(ctx, s.inboundKey, "", 50); err != nil || batch.Applied != 0 {
		t.Fatal("disable refunded volume", batch, err)
	}
	state(client.ID, "archive")
	state("refund-unlimited", "archive")
	w := browserRequest(t, h, "GET", "/api/v1/workspace/clients/"+client.ID+"/volume-refund", nil, rep, "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"ready"`) {
		t.Fatal("archive finality status", w.Code, w.Body.String())
	}
	before, err := s.business.Representative(ctx, "refund-rep")
	if err != nil || before.VolumeCreditBytes != 900 || before.UnlimitedCreditUnits != 8 {
		t.Fatal("read credited money or altered tokens", before, err)
	}
	worker, cancel := context.WithCancel(ctx)
	go s.runVolumeRefunds(worker, 5*time.Millisecond, time.Second)
	t.Cleanup(func() {
		cancel()
		done, stop := context.WithTimeout(ctx, time.Second)
		defer stop()
		s.WaitVolumeRefunds(done)
	})
	deadline := time.After(2 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		after, err := s.business.Representative(ctx, "refund-rep")
		if err != nil {
			t.Fatal(err)
		}
		if after.VolumeCreditBytes == 1000 {
			if after.UnlimitedCreditUnits != 8 {
				t.Fatal("unlimited token accounting changed", after)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("automatic worker did not return unused volume")
		case <-tick.C:
		}
	}
	c, err := s.business.Client(ctx, client.ID)
	if err != nil || c.UUID != client.UUID || !c.Archived || c.VolumeBytes != 0 || c.ChargeVolumeBytes != 100 {
		t.Fatal("refund lost identity or revived capacity", c, err)
	}
	history, err := s.business.VolumeRefundHistory(ctx, "refund-rep", 25, 0)
	if err != nil || len(history) != 1 || history[0].RefundedBytes != 100 || history[0].Trigger != "archived" {
		t.Fatal("immutable refund history", history, err)
	}
	if _, err := s.business.SweepExpiredVolumeRefunds(ctx, s.inboundKey, "", 50); err != nil {
		t.Fatal(err)
	}
	after, _ := s.business.Representative(ctx, "refund-rep")
	if after.VolumeCreditBytes != 1000 || after.UnlimitedCreditUnits != 8 {
		t.Fatal("repeated sweep changed credit", after)
	}
	// Spend the complete balance, including returned bytes, through the real
	// representative creation API. A display-only credit cannot pass this.
	created := browserRequest(t, h, "POST", "/api/v1/workspace/clients", business.NewClient{
		EventID: "refund-api-reuse-returned-01", ClientID: "refund-reused-credit", RepresentativeID: "refund-rep",
		Name: "Created from returned credit", DurationDays: 30, VolumeBytes: 1000,
	}, rep, csrf, "https://"+browserHost)
	if created.Code != 201 {
		t.Fatal("representative could not reuse returned volume", created.Code, created.Body.String())
	}
	after, err = s.business.Representative(ctx, "refund-rep")
	if err != nil || after.VolumeCreditBytes != 0 || after.UnlimitedCreditUnits != 8 {
		t.Fatal("reused returned credit violated conservation", after, err)
	}
}

func TestVolumeRefundBrowserGateScopeAndAgentIsolation(t *testing.T) {
	closed := enabledOwner(t)
	owner, _ := browserLogin(t, closed, ownerUsername, ownerPassword)
	if w := browserRequest(t, closed.Handler(), "GET", "/api/v1/workspace/volume-refunds", nil, owner, "", ""); w.Code != 404 {
		t.Fatal("refund API exposed before migration", w.Code)
	}
	s, client := volumeRefundServer(t)
	rep, _ := browserLogin(t, s, "refund-rep", repPassword)
	other, _ := browserLogin(t, s, "refund-other", repPassword)
	h := s.BrowserHandler()
	path := "/api/v1/workspace/clients/" + client.ID + "/volume-refund"
	for _, item := range []struct {
		path   string
		cookie *http.Cookie
		want   int
	}{
		{path, nil, 401}, {path, other, 403}, {path, rep, 200},
		{path + "?force=true", rep, 400}, {"/api/v1/workspace/volume-refunds?representative_id=refund-rep", other, 400},
		{"/api/v1/workspace/volume-refunds?limit=25&offset=0", other, 200},
		{"/api/v1/workspace/volume-refunds?limit=25&limit=1", rep, 400},
	} {
		w := browserRequest(t, h, "GET", item.path, nil, item.cookie, "", "")
		if w.Code != item.want {
			t.Fatal("refund access", item.path, w.Code, w.Body.String())
		}
		if item.want == 200 && item.cookie == other {
			var page struct {
				RepresentativeID string                            `json:"representative_id"`
				Refunds          []business.VolumeRefundHistoryRow `json:"refunds"`
			}
			if json.Unmarshal(w.Body.Bytes(), &page) != nil || page.RepresentativeID != "refund-other" || len(page.Refunds) != 0 {
				t.Fatal("financial history crossed scope", page)
			}
		}
	}
	for _, path := range []string{path, "/api/v1/workspace/volume-refunds"} {
		req := httptest.NewRequest("GET", "https://"+browserHost+path, nil)
		req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		req.AddCookie(rep)
		w := httptest.NewRecorder()
		s.AgentHandler().ServeHTTP(w, req)
		if w.Code != 404 {
			t.Fatal("Agent listener exposed financial return", w.Code)
		}
		permission, reviewed := requiredRepresentativePermission(req)
		if !reviewed || permission != "view_finance" {
			t.Fatal("refund bypasses finance permission", permission, reviewed)
		}
	}
	rights := business.DefaultRepresentativePermissions()
	rights.ViewFinance = false
	if _, err := s.business.UpdateRepresentativeControl(context.Background(), business.RepresentativeControlChange{
		EventID: "refund-api-revoke-finance-01", RepresentativeID: "refund-rep", ExpectedRevision: 0,
		DisplayName: "refund-rep", Permissions: rights, Reason: "Financial report isolation",
	}); err != nil {
		t.Fatal(err)
	}
	rep, _ = browserLogin(t, s, "refund-rep", repPassword)
	for _, path := range []string{path, "/api/v1/workspace/volume-refunds"} {
		w := browserRequest(t, h, "GET", path, nil, rep, "", "")
		if w.Code != 403 {
			t.Fatal("disabled financial permission ignored", path, w.Code, w.Body.String())
		}
	}
}
