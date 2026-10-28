package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/darktunnelmika/dark-xray-pro/internal/business"
	uc "github.com/darktunnelmika/dark-xray-pro/internal/usagecheckpoint"
	"github.com/darktunnelmika/dark-xray-pro/internal/usageledger"
)

const sellerOrdersAPIRepV1 = "orderRepA"

type sellerOrdersAPIFixtureV1 struct {
	s                             *Server
	h                             http.Handler
	owner, rep, other             *http.Cookie
	ownerCSRF, repCSRF, otherCSRF string
}

func sellerOrdersAPIPathV1(rep, seller string) string {
	return "/api/v1/workspace/representatives/" + rep + "/sellers/" + seller + "/clients"
}

func sellerOrdersAPICreateV1(id string, volume int64) business.SellerClientCreateV1 {
	return business.SellerClientCreateV1{EventID: "seller-order-api-create-" + id, RepresentativeID: sellerOrdersAPIRepV1,
		SellerID: "sellerAlice", ClientID: id, Name: "Synthetic " + id, DurationDays: 30, IPCount: 2,
		VolumeBytes: volume, Reason: "Synthetic browser seller order"}
}

func sellerOrdersAPIExtendV1(id string, volume int64) business.SellerClientExtendV1 {
	return business.SellerClientExtendV1{EventID: "seller-order-api-extend-" + id, RepresentativeID: sellerOrdersAPIRepV1,
		SellerID: "sellerAlice", ClientID: id, AdditionalDays: 15, AdditionalVolumeBytes: volume,
		Reason: "Synthetic browser seller renewal"}
}

func sellerOrdersAPIFixture(t *testing.T) sellerOrdersAPIFixtureV1 {
	t.Helper()
	ctx := context.Background()
	s := enabledOwner(t)
	for _, enable := range []func() error{
		func() error { return s.business.EnableUsageLedger(ctx) },
		func() error { return s.EnableSellerQuotaV2Lab(ctx) },
		func() error { return s.EnableNativeInbounds(bytes.Repeat([]byte{7}, 32)) },
		func() error { return s.EnableVolumeRefunds(ctx) },
		func() error { return s.EnableSellerOrdersV1Lab(ctx) },
	} {
		if err := enable(); err != nil {
			t.Fatal(err)
		}
	}
	owner, ownerCSRF := browserLogin(t, s, ownerUsername, ownerPassword)
	for _, id := range []string{sellerOrdersAPIRepV1, "orderRepB"} {
		if _, err := s.business.CreateRepresentative(ctx, id, "Representative "+id); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.ProvisionRepresentativeLogin(ctx, id, strings.ToLower(id), repPassword); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.AdjustCredits(ctx, business.CreditChange{EventID: "seller-api-initial-" + id,
			RepresentativeID: id, VolumeDeltaBytes: 1000, UnlimitedDeltaUnits: 100, Reason: "Synthetic seller reserve"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.business.UpdateSellerPermissionV2(ctx, owner.Value, business.SellerPermissionChangeV2{
			EventID: "seller-api-grant-" + id, RepresentativeID: id, ManageSellers: true, Reason: "Explicit seller administration",
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i, item := range []struct{ rep, seller string }{
		{sellerOrdersAPIRepV1, "sellerAlice"}, {sellerOrdersAPIRepV1, "sellerBob"}, {"orderRepB", "sellerForeign"},
	} {
		if _, err := s.business.CreateSellerV2As(ctx, owner.Value, business.CreateSellerV2{
			EventID: "seller-api-seller-" + item.seller, RepresentativeID: item.rep, SellerID: item.seller,
			Name: item.seller, TelegramUserID: int64(1001 + i),
		}); err != nil {
			t.Fatal(err)
		}
		if item.seller != "sellerBob" {
			if _, err := s.business.TransferSellerQuotaV2As(ctx, owner.Value, business.SellerQuotaTransferV2{
				EventID: "seller-api-reserve-" + item.seller, RepresentativeID: item.rep, SellerID: item.seller,
				Action: "allocate", Bytes: 1000, Units: 100, Reason: "Move all free credit into seller reserve",
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	rep, repCSRF := browserLogin(t, s, strings.ToLower(sellerOrdersAPIRepV1), repPassword)
	other, otherCSRF := browserLogin(t, s, "orderrepb", repPassword)
	return sellerOrdersAPIFixtureV1{s, s.BrowserHandler(), owner, rep, other, ownerCSRF, repCSRF, otherCSRF}
}

func sellerOrdersAPIReceiptV1(t *testing.T, w *httptest.ResponseRecorder, status int) business.SellerOrderReceiptV1 {
	t.Helper()
	if w.Code != status {
		t.Fatalf("seller order returned %d, want %d: %s", w.Code, status, w.Body.String())
	}
	var out business.SellerOrderReceiptV1
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func sellerOrdersAPIBalancesV1(t *testing.T, f sellerOrdersAPIFixtureV1, free, reserve, units int64) {
	t.Helper()
	rep, err := f.s.business.Representative(context.Background(), sellerOrdersAPIRepV1)
	if err != nil {
		t.Fatal(err)
	}
	seller, err := f.s.business.SellerV2(context.Background(), sellerOrdersAPIRepV1, "sellerAlice")
	if err != nil {
		t.Fatal(err)
	}
	if rep.VolumeCreditBytes != free || rep.UnlimitedCreditUnits != 0 || seller.ReservedBytes != reserve || seller.ReservedUnits != units {
		t.Fatalf("seller order violated conservation: representative=%+v seller=%+v", rep, seller)
	}
}

func sellerOrdersAPICapabilityV1(t *testing.T, h http.Handler, cookie *http.Cookie, want bool) {
	t.Helper()
	w := browserRequest(t, h, "GET", "/api/v1/workspace/me", nil, cookie, "", "")
	if w.Code != 200 {
		t.Fatal("workspace capability unavailable", w.Code, w.Body.String())
	}
	var result struct {
		Capabilities map[string]bool `json:"capabilities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if got, present := result.Capabilities["seller_orders_v1"]; !present || got != want {
		t.Fatal("workspace capability does not reflect successful gate", result.Capabilities, want)
	}
}

func TestSellerOrdersAPIGatePrerequisitesAndAgentIsolation(t *testing.T) {
	ctx := context.Background()
	s, _, _, _ := newLab(t)
	if err := s.EnableSellerOrdersV1Lab(ctx); err == nil || s.sellerOrdersV1Enabled {
		t.Fatal("gate admitted missing business store", err)
	}
	s = enabledOwner(t)
	owner, csrf := browserLogin(t, s, ownerUsername, ownerPassword)
	base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
	assertAbsent := func() {
		t.Helper()
		sellerOrdersAPICapabilityV1(t, s.BrowserHandler(), owner, false)
		if w := browserRequest(t, s.BrowserHandler(), "GET", "/api/v1/workspace/me", nil, nil, "", ""); w.Code != 401 {
			t.Fatal("workspace capability bypassed session authentication", w.Code, w.Body.String())
		}
		for _, path := range []string{base, base + "/anyClient/extend"} {
			if w := browserRequest(t, s.BrowserHandler(), "POST", path, map[string]any{}, owner, csrf, "https://"+browserHost); w.Code != 404 {
				t.Fatal("seller order mounted without successful gate", path, w.Code, w.Body.String())
			}
		}
	}
	assertAbsent()
	for _, enable := range []func() error{
		func() error { return nil },
		func() error { return s.business.EnableUsageLedger(ctx) },
		func() error { return s.EnableSellerQuotaV2Lab(ctx) },
		func() error { return s.EnableNativeInbounds(bytes.Repeat([]byte{7}, 32)) },
	} {
		if err := enable(); err != nil {
			t.Fatal(err)
		}
		if err := s.EnableSellerOrdersV1Lab(ctx); err == nil || s.sellerOrdersV1Enabled {
			t.Fatal("gate implicitly enabled a prerequisite", err)
		}
		assertAbsent()
	}
	if err := s.EnableVolumeRefunds(ctx); err != nil {
		t.Fatal(err)
	}
	failed, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.EnableSellerOrdersV1Lab(failed); err == nil || s.sellerOrdersV1Enabled {
		t.Fatal("failed schema initialization exposed routes", err)
	}
	assertAbsent()
	if err := s.EnableSellerOrdersV1Lab(ctx); err != nil {
		t.Fatal(err)
	}
	sellerOrdersAPICapabilityV1(t, s.BrowserHandler(), owner, true)
	if w := browserRequest(t, s.BrowserHandler(), "GET", "/api/v1/workspace/me", nil, nil, "", ""); w.Code != 401 {
		t.Fatal("enabled capability exposed an anonymous workspace", w.Code, w.Body.String())
	}
	if err := s.EnableSellerOrdersV1Lab(ctx); err == nil {
		t.Fatal("repeated API activation accepted")
	}
	for _, route := range []struct{ method, path string }{{"POST", base}, {"POST", base + "/anyClient/extend"}, {"GET", "/api/v1/workspace/me"}} {
		req := httptest.NewRequest(route.method, "https://"+browserHost+route.path, strings.NewReader(`{}`))
		req.TLS = &tls.ConnectionState{Version: tls.VersionTLS13}
		req.AddCookie(owner)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "https://"+browserHost)
		req.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.AgentHandler().ServeHTTP(w, req)
		if w.Code != 404 {
			t.Fatal("TLS Agent routed seller billing or capabilities", route, w.Code, w.Body.String())
		}
	}
	for _, item := range []struct{ method, path, permission string }{
		{"POST", base, "create_clients"}, {"POST", base + "/anyClient/extend", "renew_clients"},
	} {
		req := httptest.NewRequest(item.method, item.path, nil)
		if key, ok := requiredRepresentativePermission(req); !ok || key != item.permission {
			t.Fatal("seller order missing operation grant", item, key, ok)
		}
	}
	for _, item := range []struct{ method, path string }{
		{"GET", base}, {"POST", base + "/extra"}, {"POST", base + "/anyClient/extend/extra"},
		{"POST", base + "/anyClient/state"}, {"PATCH", base + "/anyClient/extend"},
		{"POST", sellerOrdersAPIPathV1("", "sellerAlice")}, {"POST", sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "")},
		{"POST", base + "//extend"},
	} {
		if _, ok := requiredRepresentativePermission(httptest.NewRequest(item.method, item.path, nil)); ok {
			t.Fatal("unreviewed seller route admitted", item)
		}
	}
}

func TestSellerOrdersAPIOwnerRepresentativeExactReceiptsAndReplay(t *testing.T) {
	for _, owner := range []bool{false, true} {
		name := "representative"
		if owner {
			name = "owner"
		}
		t.Run(name, func(t *testing.T) {
			f := sellerOrdersAPIFixture(t)
			cookie, csrf := f.rep, f.repCSRF
			if owner {
				cookie, csrf = f.owner, f.ownerCSRF
			}
			sellerOrdersAPICapabilityV1(t, f.h, cookie, true)
			post := func(path string, input any) *httptest.ResponseRecorder {
				return browserRequest(t, f.h, "POST", path, input, cookie, csrf, "https://"+browserHost)
			}
			base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
			input := sellerOrdersAPICreateV1("browser-created", 100)
			sellerOrdersAPIBalancesV1(t, f, 0, 1000, 100)
			created := sellerOrdersAPIReceiptV1(t, post(base, input), 201)
			if created.Replayed || created.Action != "create" || created.Client.VolumeBytes != 100 || created.VolumeDebitBytes != 100 ||
				created.ReservedBytesBefore != 1000 || created.ReservedBytesAfter != 900 || created.ReservedUnitsAfter != 100 ||
				created.Client.UUID == "" || created.Client.DeploymentState != "pending_node_apply" || created.ActorUserID == "" {
				t.Fatal("incorrect immutable creation receipt", created)
			}
			usage, err := f.s.business.ClientUsage(context.Background(), input.ClientID)
			if err != nil || !usage.Known || usage.Total != 0 || usage.Limit != 100 {
				t.Fatal("order missing exact new-identity usage seed", usage, err)
			}
			extension := sellerOrdersAPIExtendV1(input.ClientID, 50)
			extendPath := base + "/" + input.ClientID + "/extend"
			renewed := sellerOrdersAPIReceiptV1(t, post(extendPath, extension), 200)
			if renewed.Replayed || renewed.Action != "extend" || renewed.Client.UUID != created.Client.UUID || renewed.Client.VolumeBytes != 150 ||
				renewed.PreviousClient == nil || renewed.PreviousClient.VolumeBytes != 100 || renewed.VolumeDebitBytes != 50 ||
				renewed.ReservedBytesBefore != 900 || renewed.ReservedBytesAfter != 850 || renewed.Client.DeploymentState != "pending_node_apply" {
				t.Fatal("incorrect immutable renewal receipt", renewed)
			}
			for _, item := range []struct {
				path   string
				input  any
				status int
				want   business.SellerOrderReceiptV1
			}{{base, input, 201, created}, {extendPath, extension, 200, renewed}} {
				got := sellerOrdersAPIReceiptV1(t, post(item.path, item.input), item.status)
				item.want.Replayed = true
				if !reflect.DeepEqual(got, item.want) {
					t.Fatal("retry returned fresh state or changed historical receipt", got, item.want)
				}
			}
			changed := input
			changed.VolumeBytes++
			if w := post(base, changed); w.Code != 409 {
				t.Fatal("conflicting creation nonce accepted", w.Code, w.Body.String())
			}
			extension.AdditionalDays++
			if w := post(extendPath, extension); w.Code != 409 {
				t.Fatal("conflicting renewal nonce accepted", w.Code, w.Body.String())
			}
			otherCookie, otherCSRF := f.owner, f.ownerCSRF
			if owner {
				otherCookie, otherCSRF = f.rep, f.repCSRF
			}
			if w := browserRequest(t, f.h, "POST", base, input, otherCookie, otherCSRF, "https://"+browserHost); w.Code != 409 {
				t.Fatal("event replay was rebound to a different trusted actor", w.Code, w.Body.String())
			}
			sellerOrdersAPIBalancesV1(t, f, 0, 850, 100)
		})
	}
}

func TestSellerOrdersAPIScopeBindingAndNoCrossSellerRenewal(t *testing.T) {
	f := sellerOrdersAPIFixture(t)
	base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
	created := sellerOrdersAPICreateV1("scoped-client", 100)
	sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", base, created, f.rep, f.repCSRF, "https://"+browserHost), 201)
	extendPath := base + "/" + created.ClientID + "/extend"
	for _, item := range []struct {
		name, path string
		input      any
		cookie     *http.Cookie
		csrf       string
		want       int
	}{
		{"foreign representative", sellerOrdersAPIPathV1("orderRepB", "sellerForeign"), business.SellerClientCreateV1{
			EventID: "seller-api-cross-tenant-0001", RepresentativeID: "orderRepB", SellerID: "sellerForeign", ClientID: "foreign-client",
			Name: "Forbidden foreign identity", DurationDays: 30, VolumeBytes: 100, Reason: "Cross tenant attempt"}, f.rep, f.repCSRF, 403},
		{"other session", base, created, f.other, f.otherCSRF, 403},
		{"representative path mismatch", sellerOrdersAPIPathV1("orderRepB", "sellerAlice"), created, f.owner, f.ownerCSRF, 422},
		{"seller path mismatch", sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerBob"), created, f.owner, f.ownerCSRF, 422},
		{"client path mismatch", base + "/anotherClient/extend", sellerOrdersAPIExtendV1(created.ClientID, 10), f.owner, f.ownerCSRF, 422},
		{"query injection", base + "?representative_id=orderRepB", created, f.rep, f.repCSRF, 422},
		{"empty query", extendPath + "?", sellerOrdersAPIExtendV1(created.ClientID, 10), f.rep, f.repCSRF, 422},
	} {
		t.Run(item.name, func(t *testing.T) {
			w := browserRequest(t, f.h, "POST", item.path, item.input, item.cookie, item.csrf, "https://"+browserHost)
			if w.Code != item.want {
				t.Fatal("path/tenant binding failed", w.Code, w.Body.String())
			}
		})
	}
	for _, item := range []struct{ rep, seller string }{{sellerOrdersAPIRepV1, "sellerBob"}, {"orderRepB", "sellerForeign"}} {
		input := sellerOrdersAPIExtendV1(created.ClientID, 10)
		input.RepresentativeID, input.SellerID = item.rep, item.seller
		path := sellerOrdersAPIPathV1(item.rep, item.seller) + "/" + created.ClientID + "/extend"
		if w := browserRequest(t, f.h, "POST", path, input, f.owner, f.ownerCSRF, "https://"+browserHost); w.Code != 403 {
			t.Fatal("owner rebound existing funding identity", item, w.Code, w.Body.String())
		}
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 900, 100)
}

func TestSellerOrdersAPIBrowserCredentialsOriginAndCSRF(t *testing.T) {
	f := sellerOrdersAPIFixture(t)
	base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
	create := sellerOrdersAPICreateV1("security-client", 100)
	sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", base, create, f.rep, f.repCSRF, "https://"+browserHost), 201)
	for _, item := range []struct {
		path  string
		input any
	}{{base, create}, {base + "/" + create.ClientID + "/extend", sellerOrdersAPIExtendV1(create.ClientID, 10)}} {
		for _, credentials := range []struct {
			cookie       *http.Cookie
			csrf, origin string
			want         int
		}{
			{nil, f.repCSRF, "https://" + browserHost, 401},
			{f.rep, "", "https://" + browserHost, 403},
			{f.rep, f.ownerCSRF, "https://" + browserHost, 403},
			{f.rep, f.repCSRF, "https://foreign.example", 403},
			{f.rep, f.repCSRF, "", 403},
			{f.rep, f.repCSRF, "null", 403},
		} {
			w := browserRequest(t, f.h, "POST", item.path, item.input, credentials.cookie, credentials.csrf, credentials.origin)
			if w.Code != credentials.want {
				t.Fatal("browser security boundary failed", item.path, w.Code, w.Body.String())
			}
		}
		if w := request(t, f.h, "POST", item.path, ownerToken, item.input); w.Code != 401 {
			t.Fatal("owner bearer authenticated a browser order", w.Code, w.Body.String())
		}
		for _, header := range []struct{ key, value string }{{"Sec-Fetch-Site", "cross-site"}, {"Content-Type", "text/plain"}} {
			raw, _ := json.Marshal(item.input)
			req := httptest.NewRequest("POST", "https://"+browserHost+item.path, bytes.NewReader(raw))
			req.AddCookie(f.rep)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "https://"+browserHost)
			req.Header.Set("X-CSRF-Token", f.repCSRF)
			req.Header.Set(header.key, header.value)
			w := httptest.NewRecorder()
			f.h.ServeHTTP(w, req)
			if w.Code != 403 {
				t.Fatal("browser mutation metadata bypass", header, w.Code, w.Body.String())
			}
		}
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 900, 100)
}

func TestSellerOrdersAPICurrentGrantsAndSellerState(t *testing.T) {
	for _, denied := range []string{"manage", "create", "renew", "seller_disabled"} {
		t.Run(denied, func(t *testing.T) {
			f := sellerOrdersAPIFixture(t)
			ctx := context.Background()
			base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
			created := sellerOrdersAPICreateV1("permission-client", 100)
			sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", base, created, f.rep, f.repCSRF, "https://"+browserHost), 201)
			extension := sellerOrdersAPIExtendV1(created.ClientID, 10)
			extendPath := base + "/" + created.ClientID + "/extend"
			sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", extendPath, extension, f.rep, f.repCSRF, "https://"+browserHost), 200)
			switch denied {
			case "manage":
				if _, err := f.s.business.UpdateSellerPermissionV2(ctx, f.owner.Value, business.SellerPermissionChangeV2{
					EventID: "seller-api-revoke-manage-0001", RepresentativeID: sellerOrdersAPIRepV1, ExpectedRevision: 1,
					ManageSellers: false, Reason: "Revoke seller administration",
				}); err != nil {
					t.Fatal(err)
				}
			case "seller_disabled":
				if _, err := f.s.business.UpdateSellerStateV2(ctx, f.owner.Value, business.SellerStateChangeV2{
					EventID: "seller-api-disable-seller-0001", RepresentativeID: sellerOrdersAPIRepV1, SellerID: "sellerAlice",
					Enabled: false, Reason: "Disable target seller",
				}); err != nil {
					t.Fatal(err)
				}
			default:
				rights := business.DefaultRepresentativePermissions()
				rights.CreateClients = denied != "create"
				rights.RenewClients = denied != "renew"
				if _, err := f.s.business.UpdateRepresentativeControl(ctx, business.RepresentativeControlChange{
					EventID: "seller-api-revoke-operation-0001", RepresentativeID: sellerOrdersAPIRepV1,
					DisplayName: "Representative " + sellerOrdersAPIRepV1, Permissions: rights, Reason: "Revoke operation grant",
				}); err != nil {
					t.Fatal(err)
				}
			}
			if denied != "seller_disabled" {
				if w := browserRequest(t, f.h, "POST", base, created, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 401 {
					t.Fatal("permission change left previous session authoritative", w.Code, w.Body.String())
				}
				f.rep, f.repCSRF = browserLogin(t, f.s, strings.ToLower(sellerOrdersAPIRepV1), repPassword)
			}
			if denied != "renew" {
				if w := browserRequest(t, f.h, "POST", base, created, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 403 {
					t.Fatal("current creation permission ignored on replay", denied, w.Code, w.Body.String())
				}
				newClient := sellerOrdersAPICreateV1("forbidden-new-client", 20)
				if w := browserRequest(t, f.h, "POST", base, newClient, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 403 {
					t.Fatal("current creation permission ignored on new order", denied, w.Code, w.Body.String())
				}
			}
			if denied != "create" {
				if w := browserRequest(t, f.h, "POST", extendPath, extension, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 403 {
					t.Fatal("current renewal permission ignored on replay", denied, w.Code, w.Body.String())
				}
				extension.EventID += "-new"
				if w := browserRequest(t, f.h, "POST", extendPath, extension, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 403 {
					t.Fatal("current renewal permission ignored on new order", denied, w.Code, w.Body.String())
				}
			}
			if denied == "seller_disabled" {
				if w := browserRequest(t, f.h, "POST", base, created, f.owner, f.ownerCSRF, "https://"+browserHost); w.Code != 403 {
					t.Fatal("owner bypassed disabled target seller", w.Code, w.Body.String())
				}
			}
			sellerOrdersAPIBalancesV1(t, f, 0, 890, 100)
		})
	}
}

func TestSellerOrdersAPIInsufficientReserveLeavesNoPartialPurchase(t *testing.T) {
	f := sellerOrdersAPIFixture(t)
	base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
	input := sellerOrdersAPICreateV1("too-large-client", 1001)
	if w := browserRequest(t, f.h, "POST", base, input, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 409 || !strings.Contains(w.Body.String(), "insufficient_credit") {
		t.Fatal("insufficient order was not rejected", w.Code, w.Body.String())
	}
	if _, err := f.s.business.Client(context.Background(), input.ClientID); !errors.Is(err, business.ErrNotFound) {
		t.Fatal("failed debit created a client", err)
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 1000, 100)
	// A refused purchase does not consume its event ID.
	input.VolumeBytes = 100
	created := sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", base, input, f.rep, f.repCSRF, "https://"+browserHost), 201)
	extension := sellerOrdersAPIExtendV1(input.ClientID, 901)
	if w := browserRequest(t, f.h, "POST", base+"/"+input.ClientID+"/extend", extension, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 409 {
		t.Fatal("insufficient renewal accepted", w.Code, w.Body.String())
	}
	client, err := f.s.business.Client(context.Background(), input.ClientID)
	if err != nil || client.VolumeBytes != created.Client.VolumeBytes || client.ExpiresUnix != created.Client.ExpiresUnix {
		t.Fatal("failed renewal changed client entitlement", client, err)
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 900, 100)
}

func TestSellerOrdersAPIStrictBoundedJSONAndExactAmounts(t *testing.T) {
	f := sellerOrdersAPIFixture(t)
	base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
	create := sellerOrdersAPICreateV1("strict-client", 100)
	postRaw := func(path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "https://"+browserHost+path, strings.NewReader(body))
		req.AddCookie(f.rep)
		req.Header.Set("Origin", "https://"+browserHost)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-CSRF-Token", f.repCSRF)
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, req)
		return w
	}
	for _, item := range []struct {
		path, amount string
		input        any
	}{{base, "volume_bytes", create}, {base + "/" + create.ClientID + "/extend", "additional_volume_bytes", sellerOrdersAPIExtendV1(create.ClientID, 10)}} {
		raw, err := json.Marshal(item.input)
		if err != nil {
			t.Fatal(err)
		}
		valid := string(raw)
		mutate := func(key string, value any, remove bool) string {
			fields := map[string]any{}
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if remove {
				delete(fields, key)
			} else {
				fields[key] = value
			}
			raw, err := json.Marshal(fields)
			if err != nil {
				t.Fatal(err)
			}
			return string(raw)
		}
		for _, bad := range []string{
			`null`, `[]`, `{}`, valid + `{}`, valid[:len(valid)-1] + `,"seller_id":"sellerAlice"}`,
			strings.Replace(valid, `"seller_id"`, `"Seller_ID"`, 1), mutate("seller_id", nil, false),
			mutate("seller_id", nil, true), mutate("reason", strings.Repeat("x", 5000), false),
			mutate("actor_user_id", "owner", false), mutate("telegram_user_id", 1001, false),
			mutate("applied_to_xray", true, false), mutate(item.amount, 100, false), mutate(item.amount, nil, false),
		} {
			if w := postRaw(item.path, bad); w.Code != 400 {
				t.Fatal("ambiguous, unbounded or forged JSON accepted", item.path, w.Code, w.Body.String())
			}
		}
		for _, bad := range []string{"01", "+1", "-1", "1e2", "1.0", "9223372036854775808"} {
			if w := postRaw(item.path, mutate(item.amount, bad, false)); w.Code != 422 {
				t.Fatal("noncanonical decimal accepted", item.path, bad, w.Code, w.Body.String())
			}
		}
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 1000, 100)
	const exact int64 = 9007199254740993
	ctx := context.Background()
	if _, err := f.s.business.AdjustCredits(ctx, business.CreditChange{EventID: "seller-api-large-credit-0001", RepresentativeID: sellerOrdersAPIRepV1,
		VolumeDeltaBytes: exact, Reason: "Synthetic exact decimal precision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.business.TransferSellerQuotaV2As(ctx, f.owner.Value, business.SellerQuotaTransferV2{EventID: "seller-api-large-reserve-0001",
		RepresentativeID: sellerOrdersAPIRepV1, SellerID: "sellerAlice", Action: "allocate", Bytes: exact, Reason: "Synthetic exact reserve"}); err != nil {
		t.Fatal(err)
	}
	large := sellerOrdersAPICreateV1("large-exact-client", exact)
	w := browserRequest(t, f.h, "POST", base, large, f.rep, f.repCSRF, "https://"+browserHost)
	receipt := sellerOrdersAPIReceiptV1(t, w, 201)
	if receipt.VolumeDebitBytes != exact || receipt.Client.VolumeBytes != exact || !strings.Contains(w.Body.String(), `"volume_debit_bytes":"9007199254740993"`) ||
		!strings.Contains(w.Body.String(), `"volume_bytes":"9007199254740993"`) || !strings.Contains(w.Body.String(), `"reserved_bytes_before":"9007199254741993"`) {
		t.Fatal("browser receipt lost exact credit or used JSON numbers", w.Body.String())
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 1000, 100)
}

func TestSellerOrdersAPIVolumeFinalityRefundAndReturnedCreditReuse(t *testing.T) {
	f := sellerOrdersAPIFixture(t)
	ctx := context.Background()
	base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
	input := sellerOrdersAPICreateV1("refund-order-client", 100)
	created := sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", base, input, f.rep, f.repCSRF, "https://"+browserHost), 201)
	sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", base+"/"+input.ClientID+"/extend", sellerOrdersAPIExtendV1(input.ClientID, 50), f.rep, f.repCSRF, "https://"+browserHost), 200)
	email, err := uc.CounterEmail(input.ClientID)
	if err != nil {
		t.Fatal(err)
	}
	epoch := uc.Epoch{ID: strings.Repeat("a", 32), NodeID: "seller-api-synthetic-node", InstallationID: strings.Repeat("b", 32),
		ConfigSHA: strings.Repeat("c", 64), KernelBootID: "00000000-0000-0000-0000-000000000001", PID: 123, ProcessStartTicks: 456,
		Bindings: []uc.Binding{{ClientID: input.ClientID, Email: email, Billable: true}}}
	if err := f.s.business.UsageLedger().RegisterEpoch(ctx, epoch); err != nil {
		t.Fatal(err)
	}
	box, err := uc.Open(uc.Options{Directory: filepath.Join(t.TempDir(), "synthetic-outbox"), Key: bytes.Repeat([]byte{7}, 32), Epoch: epoch, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = box.Close() })
	observe := func(final bool) {
		t.Helper()
		report, err := box.Observe([]uc.Usage{{ClientID: input.ClientID, Uplink: 15, Downlink: 25}}, final)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.s.business.UsageLedger().Accept(ctx, usageledger.Principal{NodeID: epoch.NodeID, InstallationID: epoch.InstallationID}, report); err != nil {
			t.Fatal(err)
		}
	}
	observe(false)
	archive := business.SetClientState{EventID: "seller-api-archive-refund-0001", ClientID: input.ClientID, Action: "archive", Reason: "Synthetic manual deletion"}
	if w := browserRequest(t, f.h, "POST", "/api/v1/workspace/clients/"+input.ClientID+"/state", archive, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 200 {
		t.Fatal("browser archive failed", w.Code, w.Body.String())
	}
	if batch, err := f.s.business.SweepExpiredVolumeRefunds(ctx, f.s.inboundKey, "", 50); err != nil || batch.Applied != 0 {
		t.Fatal("nonfinal usage returned money", batch, err)
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 850, 100)
	observe(true)
	if batch, err := f.s.business.SweepExpiredVolumeRefunds(ctx, f.s.inboundKey, "", 50); err != nil || batch.Applied != 1 {
		t.Fatal("final seller funding was not reconciled", batch, err)
	}
	sellerOrdersAPIBalancesV1(t, f, 110, 850, 100)
	client, err := f.s.business.Client(ctx, input.ClientID)
	if err != nil || client.UUID != created.Client.UUID || client.VolumeBytes != 40 || client.ChargeVolumeBytes != 150 || !client.Archived {
		t.Fatal("refund changed identity or restored capacity", client, err)
	}
	for i := 0; i < 2; i++ {
		if batch, err := f.s.business.SweepExpiredVolumeRefunds(ctx, f.s.inboundKey, "", 50); err != nil || batch.Applied != 0 {
			t.Fatal("seller-funded refund credited twice", batch, err)
		}
	}
	// Spend the returned free representative balance through the existing
	// browser issuance path. The seller reserve remains unchanged.
	if w := browserRequest(t, f.h, "POST", "/api/v1/workspace/clients", business.NewClient{
		EventID: "seller-api-reuse-refund-0001", ClientID: "returned-credit-client", RepresentativeID: sellerOrdersAPIRepV1,
		Name: "Returned seller credit reused", DurationDays: 30, VolumeBytes: 110,
	}, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 201 {
		t.Fatal("returned seller credit could not be spent", w.Code, w.Body.String())
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 850, 100)
	usage, err := f.s.business.ClientUsage(ctx, input.ClientID)
	if err != nil || usage.Total != 40 || !usage.Known {
		t.Fatal("refund/reuse changed usage history", usage, err)
	}
}

func TestSellerOrdersAPIUnlimitedBillingAndRefundIsolation(t *testing.T) {
	f := sellerOrdersAPIFixture(t)
	ctx := context.Background()
	base := sellerOrdersAPIPathV1(sellerOrdersAPIRepV1, "sellerAlice")
	input := sellerOrdersAPICreateV1("unlimited-order-client", 0)
	input.Unlimited, input.DurationDays, input.IPCount = true, 60, 3
	created := sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", base, input, f.rep, f.repCSRF, "https://"+browserHost), 201)
	if created.UnlimitedDebitUnits != 6 || created.VolumeDebitBytes != 0 || created.ReservedUnitsAfter != 94 {
		t.Fatal("unlimited user-month creation formula changed", created)
	}
	extension := sellerOrdersAPIExtendV1(input.ClientID, 0)
	path := base + "/" + input.ClientID + "/extend"
	for i, expected := range []int64{2, 1} {
		if i == 1 {
			extension.EventID += "-second"
		}
		receipt := sellerOrdersAPIReceiptV1(t, browserRequest(t, f.h, "POST", path, extension, f.rep, f.repCSRF, "https://"+browserHost), 200)
		if receipt.UnlimitedDebitUnits != expected || receipt.VolumeDebitBytes != 0 {
			t.Fatal("cumulative unlimited renewal formula changed", receipt)
		}
	}
	extension.EventID += "-volume"
	extension.AdditionalVolumeBytes = 1
	if w := browserRequest(t, f.h, "POST", path, extension, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 422 {
		t.Fatal("volume topup altered an unlimited plan", w.Code, w.Body.String())
	}
	if w := browserRequest(t, f.h, "POST", "/api/v1/workspace/clients/"+input.ClientID+"/state", business.SetClientState{
		EventID: "seller-api-archive-unlimited-0001", ClientID: input.ClientID, Action: "archive", Reason: "Synthetic unused unlimited archive",
	}, f.rep, f.repCSRF, "https://"+browserHost); w.Code != 200 {
		t.Fatal("unlimited archive failed", w.Code, w.Body.String())
	}
	if batch, err := f.s.business.SweepExpiredVolumeRefunds(ctx, f.s.inboundKey, "", 50); err != nil || batch.Applied != 0 {
		t.Fatal("unused unlimited token refund introduced", batch, err)
	}
	sellerOrdersAPIBalancesV1(t, f, 0, 1000, 91)
}
