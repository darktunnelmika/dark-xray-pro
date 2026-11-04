//go:build linux

package business_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/darktunnelmika/dark-xray-pro/internal/api"
	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	mx "github.com/darktunnelmika/dark-xray-pro/internal/managedxray"
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func TestDarkRestoreApplyReviewThroughOwnerHTTP(t *testing.T) {
	ctx := context.Background()
	store, id, key, pins, node, password, roundID := business.ApplyAcceptanceHTTPFixture(t)
	defer store.Close()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	reg, err := noderegistry.Open(filepath.Join(dir, "nodes.json"), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := reg.Issue(node.NodeID, "Synthetic prepared promotion")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reg.Claim(ticket.PairCode, node.NodeID, node.InstallationID, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	srv, err := api.NewServer(telemetry.NewStore(time.Minute, time.Now), reg, strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, setup := range []func() error{
		func() error { return srv.EnableBusinessLab(store) },
		func() error { return srv.EnableNativeInbounds(key) },
		func() error { return srv.EnableClientOwnershipV1Lab(ctx) },
		func() error {
			return srv.StageDarkRestoreLocalHandoff(ctx, pins.Path, pins.SHA, pins.PublicPath, pins.PublicSHA)
		},
		func() error { return srv.EnableMeteredRouteNodesForPlanPromotion([]business.MeteredRouteNode{node}) },
		func() error {
			return srv.EnableInboundInitialHealth(mx.ProtocolHealthEndpoint{Address: "11.0.0.1:443", ServerName: "verifier.example.com", CertificateSHA: strings.Repeat("a", 64)})
		},
	} {
		if err := setup(); err != nil {
			t.Fatal(err)
		}
	}
	h := srv.BrowserHandler()
	var cookie *http.Cookie
	var csrf string
	request := func(method, path string, body any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "https://pro.example"+path, bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", "https://pro.example")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w := request("POST", "/api/v1/session/login", map[string]string{"username": "ownership-owner", "password": password})
	var login struct {
		CSRF string `json:"csrf_token"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &login) != nil || len(w.Result().Cookies()) != 1 {
		t.Fatal("login", w.Code, w.Body.String())
	}
	cookie = w.Result().Cookies()[0]
	csrf = login.CSRF

	base := "/api/v1/workspace/dark-restore/handoff/apply-acceptance"
	input := map[string]any{"handoff_sha256": pins.SHA, "event_id": "http-apply-acceptance-0001", "inbound_id": 802, "round_id": roundID, "review_sha256": "", "confirmed": false}
	if w = request("GET", base, nil); w.Code != 404 {
		t.Fatal("missing receipt", w.Code, w.Body.String())
	}
	w = request("POST", base+"/preview", input)
	var preview business.DarkRestoreNativeApplyReviewV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || preview.AuthorizesMutation || preview.Evidence.ClientID != id {
		t.Fatal("review", w.Code, w.Body.String())
	}
	if w = request("GET", base, nil); w.Code != 404 {
		t.Fatal("preview mutated receipt", w.Code)
	}
	input["confirmed"] = true
	input["review_sha256"] = strings.Repeat("f", 64)
	if w = request("POST", base, input); w.Code != 409 {
		t.Fatal("incorrect review", w.Code, w.Body.String())
	}
	input["review_sha256"] = preview.ReviewSHA
	w = request("POST", base, input)
	var receipt business.DarkRestoreNativeApplyAcceptanceReceiptV1
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || !receipt.NativeApplyRecorded || receipt.SubscriptionActivated || receipt.SubscriptionDelivered || receipt.AuthorizesFurtherMutation {
		t.Fatal("record", w.Code, w.Body.String())
	}
	w = request("POST", base, input)
	var replay business.DarkRestoreNativeApplyAcceptanceReceiptV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || !replay.Replayed {
		t.Fatal("replay", w.Code, w.Body.String())
	}
	replay.Replayed = false
	if !reflect.DeepEqual(receipt, replay) {
		t.Fatal("changed receipt")
	}
	w = request("GET", base, nil)
	var got business.DarkRestoreNativeApplyAcceptanceReceiptV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil || !reflect.DeepEqual(receipt, got) {
		t.Fatal("recovery", w.Code, w.Body.String())
	}
	input["client_id"] = "foreign"
	if w = request("POST", base, input); w.Code != 422 {
		t.Fatal("caller chose client", w.Code)
	}
	delete(input, "client_id")
	savedCSRF := csrf
	csrf = ""
	if w = request("POST", base, input); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	csrf = savedCSRF
	if w = request("GET", base+"?client_id=foreign", nil); w.Code != 422 {
		t.Fatal("query scope bypass", w.Code)
	}
	cookie = nil
	csrf = ""
	w = request("POST", "/api/v1/session/login", map[string]string{"username": "rep-source", "password": password})
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &login) != nil {
		t.Fatal("rep login", w.Code)
	}
	cookie = w.Result().Cookies()[0]
	csrf = login.CSRF
	if w = request("GET", base, nil); w.Code != 403 {
		t.Fatal("rep read acceptance", w.Code)
	}
	if w = request("POST", base, input); w.Code != 403 {
		t.Fatal("rep recorded acceptance", w.Code)
	}
}
