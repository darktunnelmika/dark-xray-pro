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
	"github.com/darktunnelmika/dark-xray-pro/internal/noderegistry"
	"github.com/darktunnelmika/dark-xray-pro/internal/telemetry"
)

func TestPreparedPromotionThroughOwnerHTTP(t *testing.T) {
	ctx := context.Background()
	store, id, key, pins, node, password := business.PreparedPromotionHTTPFixture(t)
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
	base := "/api/v1/workspace/dark-restore/handoff/promotion"
	in := map[string]any{"handoff_sha256": pins.SHA, "event_id": "http-prepared-transition-0001", "promotion_event_id": "http-prepared-native-0001", "inbound_id": 802, "plan_revision": 1, "inbound_revision": 1, "reason": "Owner reviewed exact synthetic transfer"}
	before, _ := store.Client(ctx, id)
	balances, _ := store.ListRepresentatives(ctx, 100, 0)
	if w = request("GET", base, nil); w.Code != 404 {
		t.Fatal("missing receipt", w.Code, w.Body.String())
	}
	w = request("POST", base+"/preview", in)
	var preview business.DarkRestorePreparedPromotionPreviewV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &preview) != nil || !preview.Eligible || preview.ClientID != id || preview.AuthorizesMutation {
		t.Fatal("preview", w.Code, w.Body.String())
	}
	unchanged, _ := store.Client(ctx, id)
	if !reflect.DeepEqual(before, unchanged) {
		t.Fatal("preview changed client")
	}
	in["confirmed"] = true
	in["review_sha256"] = strings.Repeat("f", 64)
	in["promotion_review_sha256"] = preview.Promotion.ReviewSHA256
	if w = request("POST", base, in); w.Code != 409 {
		t.Fatal("wrong review", w.Code, w.Body.String())
	}
	in["review_sha256"] = preview.ReviewSHA256
	w = request("POST", base, in)
	var receipt business.DarkRestorePreparedPromotionReceiptV1
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &receipt) != nil || !receipt.NativePromotionRecorded || receipt.NativeApplyRecorded || receipt.SubscriptionActivated || receipt.Replayed {
		t.Fatal("commit", w.Code, w.Body.String())
	}
	w = request("POST", base, in)
	var replay business.DarkRestorePreparedPromotionReceiptV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || !replay.Replayed {
		t.Fatal("retry", w.Code, w.Body.String())
	}
	replay.Replayed = false
	if !reflect.DeepEqual(receipt, replay) {
		t.Fatal("replay changed receipt")
	}
	w = request("GET", base, nil)
	var recovered business.DarkRestorePreparedPromotionReceiptV1
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &recovered) != nil || !reflect.DeepEqual(receipt, recovered) {
		t.Fatal("recover", w.Code, w.Body.String())
	}
	after, _ := store.Client(ctx, id)
	if after.DeploymentState != "pending_node_apply" || after.UUID != before.UUID || after.ExpiresUnix != before.ExpiresUnix {
		t.Fatal("unexpected client transition")
	}
	afterBalances, _ := store.ListRepresentatives(ctx, 100, 0)
	if !reflect.DeepEqual(balances, afterBalances) {
		t.Fatal("changed balances")
	}
	channel, err := store.MeteredRouteChannel(ctx, key, node.NodeID)
	if err != nil || len(channel.Commands) != 0 || channel.Status.Observed.ProcessReady {
		t.Fatal("promotion started execution", err)
	}
}
