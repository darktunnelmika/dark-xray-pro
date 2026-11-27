package hubcore

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRepTrialLifecycle(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	const mainTok = "111:main-bot-token-main-bot-token-000"
	SetTelegramToken(mainTok)
	defer SetTelegramToken("")
	if err := s.PutRepTrialPolicy(ctx, RepTrial{On: true, GB: 5, Days: 3, MaxClients: 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepTrialPolicy(ctx, RepTrial{On: true, GB: 5, Days: 99}); err == nil {
		t.Fatal("99 days")
	}
	// From the Mini App: offered, claimed once.
	h := SubHandler(s)
	call := func(chat, method, path string) (int, map[string]any) {
		t.Helper()
		req := httptest.NewRequest(method, "https://hub.example"+path, bytes.NewReader(nil))
		req.Header.Set("X-Init-Data", signInitData(mainTok, `{"id":`+chat+`,"first_name":"Tara","username":"tara"}`, *now))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}
	if _, r := call("800", "GET", "/app/api/panels"); r["trial"] == nil || r["on"] != true {
		t.Fatalf("offer %v", r)
	}
	code, r := call("800", "POST", "/app/api/paneltrial")
	if code != 200 || r["password"] == "" {
		t.Fatalf("claim %d %v", code, r)
	}
	rep := r["username"].(string)
	if code, _ := call("800", "POST", "/app/api/paneltrial"); code == 200 {
		t.Fatal("twice")
	}
	if _, r := call("800", "GET", "/app/api/panels"); r["trial"] != nil {
		t.Fatal("offered again")
	}
	if pol, _ := s.RepPolicyOf(ctx, rep); pol.MaxClients != 2 {
		t.Fatalf("cap %+v", pol)
	}
	if err := s.RepPutClient(ctx, "t", rep, Client{Email: "t1", Enabled: true, VolumeBytes: 1 << 30, ExpiresUnix: now.Unix() + 30*86400, Inbounds: []string{"de-vless"}}, "volume"); err != nil {
		t.Fatal(err)
	}
	// A day before the end: reminded once.
	if rm, fr := s.RepTrialsDue(ctx); len(rm)+len(fr) != 0 {
		t.Fatal("due too early")
	}
	*now = now.Add(50 * time.Hour)
	if rm, _ := s.RepTrialsDue(ctx); len(rm) != 1 || rm[0] != rep {
		t.Fatalf("remind %v", rm)
	}
	s.RepTrialReminded(ctx, rep)
	if rm, _ := s.RepTrialsDue(ctx); len(rm) != 0 {
		t.Fatal("reminded twice")
	}
	// The end: frozen.
	*now = now.Add(23 * time.Hour)
	_, fr := s.RepTrialsDue(ctx)
	if len(fr) != 1 {
		t.Fatalf("freeze %v", fr)
	}
	if err := s.FreezeRepTrial(ctx, rep); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.ClientByEmail(ctx, "t1"); c.Enabled {
		t.Fatal("client still on")
	}
	if pol, _ := s.RepPolicyOf(ctx, rep); !pol.DenyCreate || !pol.DenyRenew || !pol.DenyEdit {
		t.Fatalf("not locked %+v", pol)
	}
	if st := s.RepTrialStateOf(ctx, rep); !st.Frozen || s.RepTrialStats(ctx)["frozen"] != 1 {
		t.Fatalf("state %+v", st)
	}
	// A paid top-up makes it a normal panel and brings the users back.
	if err := s.PutCard(ctx, CardInfo{Number: "6037991234567890", Holder: "Owner"}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRepSale(ctx, RepSale{Enabled: true, Packages: []RepPackage{{ID: "p1", Name: "10", GB: 10, Price: 100000}}}); err != nil {
		t.Fatal(err)
	}
	o, err := s.CreateRepOrder(ctx, "", 800, "Tara", rep, "", "p1")
	if err != nil || o.Kind != "topup" {
		t.Fatalf("order %+v %v", o, err)
	}
	if _, err := s.AttachRepReceipt(ctx, o.ID, 800, "photo-1"); err != nil {
		t.Fatal(err)
	}
	res, err := s.ApproveRepOrder(ctx, "owner", "", o.ID)
	if err != nil || !res.Converted {
		t.Fatalf("approve %+v %v", res, err)
	}
	if c, _ := s.ClientByEmail(ctx, "t1"); !c.Enabled {
		t.Fatal("client not back")
	}
	if pol, _ := s.RepPolicyOf(ctx, rep); pol.DenyCreate || pol.MaxClients != 0 {
		t.Fatalf("not unlocked %+v", pol)
	}
	if s.IsTrialPanel(ctx, rep) || s.RepTrialStateOf(ctx, rep).Trial {
		t.Fatal("still a trial")
	}
	if rm, fr := s.RepTrialsDue(ctx); len(rm)+len(fr) != 0 {
		t.Fatal("due after convert")
	}
}
