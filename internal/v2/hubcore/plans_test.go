package hubcore

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/wire"
)

func TestNextPlanTemplatesNodeUsage(t *testing.T) {
	s, now := newStore(t)
	seed(t, s)
	ctx := context.Background()
	if err := s.SetNextPlan(ctx, "alice", &NextPlan{}); err == nil {
		t.Fatal("empty next plan accepted")
	}
	if err := s.SetNextPlan(ctx, "alice", &NextPlan{GB: 2, Days: 30}); err != nil {
		t.Fatal(err)
	}
	if n := s.runNextPlans(ctx); n != 0 {
		t.Fatal("next plan before the current one ended")
	}
	// alice has 1000 bytes; use them up on de-1.
	s.ApplyUsage(ctx, "de-1", wire.UsageBatch{NodeID: "de-1", Seq: 1, Traffic: []wire.UserTraffic{{Email: "alice", Up: 400, Down: 600}}})
	if n := s.runNextPlans(ctx); n != 1 {
		t.Fatal("next plan not applied after the volume ran out")
	}
	list, _ := s.Clients(ctx)
	for _, c := range list {
		if c.Email == "alice" {
			if c.VolumeBytes != 2<<30 || c.ExpiresUnix != now.Unix()+30*86400 || c.NextPlan != nil || c.Usage.Up+c.Usage.Down != 0 {
				t.Fatalf("after next plan %+v", c)
			}
		}
	}
	if n := s.runNextPlans(ctx); n != 0 {
		t.Fatal("applied twice")
	}
	nu, _ := s.ClientNodeUsage(ctx, "alice", 7)
	if len(nu) != 1 || nu[0].NodeID != "de-1" || nu[0].Up+nu[0].Down != 1000 {
		t.Fatalf("node usage %+v (history survives the reset)", nu)
	}
	// Templates.
	if err := s.PutTemplate(ctx, ClientTemplate{ID: "t30", Name: "30 GB", GB: 30, Days: 30, FirstUse: true, Inbounds: []string{"de-vless"}, ResetPeriod: "month"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutTemplate(ctx, ClientTemplate{ID: "bad", Name: "x", ResetPeriod: "year"}); err == nil {
		t.Fatal("bad template accepted")
	}
	if tl, _ := s.Templates(ctx); len(tl) != 1 || tl[0].GB != 30 || !tl[0].FirstUse {
		t.Fatalf("templates %+v", tl)
	}
	if err := s.DeleteTemplate(ctx, "t30"); err != nil {
		t.Fatal(err)
	}
	_ = time.Second
}

func TestDeviceLimit(t *testing.T) {
	s, _ := newStore(t)
	seed(t, s)
	ctx := context.Background()
	list, _ := s.Clients(ctx)
	var tok string
	for _, c := range list {
		if c.Email == "bob" {
			tok = c.SubToken
		}
	}
	h := SubHandler(s)
	fetch := func(hwid string) int {
		r := httptest.NewRequest("GET", "/sub/"+tok, nil)
		if hwid != "" {
			r.Header.Set("X-Hwid", hwid)
			r.Header.Set("X-Device-Os", "Android")
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if c := fetch("dev-a"); c != http.StatusOK {
		t.Fatalf("no limit: %d", c)
	}
	if err := s.SetHWIDLimit(ctx, "bob", 2); err != nil {
		t.Fatal(err)
	}
	if fetch("dev-b") != 200 || fetch("dev-a") != 200 {
		t.Fatal("within the limit refused")
	}
	if c := fetch("dev-c"); c != http.StatusForbidden {
		t.Fatalf("third device: %d", c)
	}
	if c := fetch(""); c != 200 {
		t.Fatalf("no header, not strict: %d", c)
	}
	s.SetSetting(ctx, "hwid_strict", "1")
	if c := fetch(""); c != http.StatusForbidden {
		t.Fatalf("no header, strict: %d", c)
	}
	if d, _ := s.Devices(ctx, "bob"); len(d) != 2 || d[0].OS != "Android" {
		t.Fatalf("devices %+v", d)
	}
	s.DeleteDevice(ctx, "bob", "dev-b")
	if c := fetch("dev-c"); c != 200 {
		t.Fatalf("after freeing a slot: %d", c)
	}
}
