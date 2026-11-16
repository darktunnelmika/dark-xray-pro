package hubcore

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// signInitData builds initData the way Telegram does.
func signInitData(token string, user string, at time.Time) string {
	v := url.Values{"user": {user}, "auth_date": {strconv.FormatInt(at.Unix(), 10)}, "query_id": {"AAH"}}
	keys := []string{}
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := []string{}
	for _, k := range keys {
		lines = append(lines, k+"="+v.Get(k))
	}
	km := hmac.New(sha256.New, []byte("WebAppData"))
	km.Write([]byte(token))
	m := hmac.New(sha256.New, km.Sum(nil))
	m.Write([]byte(strings.Join(lines, "\n")))
	v.Set("hash", hex.EncodeToString(m.Sum(nil)))
	return v.Encode()
}

func TestMiniApp(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	const tok = "123:secret-bot-token"
	SetTelegramToken(tok)
	defer SetTelegramToken("")
	user := `{"id":777,"first_name":"Ali","username":"ali"}`
	good := signInitData(tok, user, *now)
	if u, err := VerifyInitData(good, tok, *now, time.Hour); err != nil || u.ID != 777 {
		t.Fatal(u, err)
	}
	if _, err := VerifyInitData(good, "other:token", *now, time.Hour); err == nil {
		t.Fatal("wrong bot token accepted")
	}
	if _, err := VerifyInitData(strings.Replace(good, "Ali", "Bob", 1), tok, *now, time.Hour); err == nil {
		t.Fatal("tampered data accepted")
	}
	if _, err := VerifyInitData(good, tok, now.Add(2*time.Hour), time.Hour); err == nil {
		t.Fatal("old data accepted")
	}
	owner := Principal{User: "boss", Role: "owner"}
	if err := s.PutProduct(ctx, owner, Product{ID: "m30", Name: "ماهانه ۳۰ گیگ", Plan: "volume", GB: 30, Days: 30, Price: 150000, Status: "published", Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	// The panel handler (with its UI at /) must also route the Mini App.
	if h := AdminHandlerWith(s, "tok", "", http.NotFoundHandler()); h == nil {
		t.Fatal("handler")
	}
	srv := httptest.NewServer(SubHandler(s))
	defer srv.Close()
	call := func(method, path, body, data string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if data != "" {
			req.Header.Set("X-Init-Data", data)
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, string(b)
	}
	if code, b := call("GET", "/app/", "", ""); code != 200 || !strings.Contains(b, "app.js") {
		t.Fatalf("page %d", code)
	}
	if code, _ := call("GET", "/app/api/me", "", ""); code != 401 {
		t.Fatal("no init data must be refused")
	}
	if code, _ := call("GET", "/app/api/me", "", good); code != 503 {
		t.Fatal("store must be closed until the owner turns it on")
	}
	if err := s.PutSubSettings(ctx, SubSettings{MiniApp: true}); err != nil {
		t.Fatal(err)
	}
	code, b := call("GET", "/app/api/me", "", good)
	var me struct {
		User     TgUser
		Services []MiniAppService
		Products []Product
	}
	if code != 200 || json.Unmarshal([]byte(b), &me) != nil || me.User.ID != 777 || len(me.Services) != 0 || len(me.Products) != 1 || me.Products[0].Inbounds != nil {
		t.Fatalf("%d %s", code, b)
	}
	code, b = call("POST", "/app/api/order", `{"product":"m30"}`, good)
	if code != 200 || !strings.Contains(b, `"id":`) {
		t.Fatalf("%d %s", code, b)
	}
	if _, b2 := call("POST", "/app/api/order", `{"product":"m30"}`, good); !strings.Contains(b2, `"existing":true`) {
		t.Fatal("a double tap must not create a second order", b2)
	}
	var o struct{ ID int64 }
	_ = json.Unmarshal([]byte(b), &o)
	if _, err := s.FulfillOrder(ctx, owner, o.ID); err != nil {
		t.Fatal(err)
	}
	code, b = call("GET", "/app/api/me", "", good)
	if code != 200 || json.Unmarshal([]byte(b), &me) != nil || len(me.Services) != 1 || !strings.Contains(me.Services[0].SubURL, "/sub/") || me.Services[0].Total != 30<<30 {
		t.Fatalf("after delivery: %s", b)
	}
	// Renewing someone else's service is refused; another user sees nothing.
	other := signInitData(tok, `{"id":888,"first_name":"Eve"}`, *now)
	if code, _ := call("POST", "/app/api/order", `{"product":"m30","renew":"`+me.Services[0].Email+`"}`, other); code != 403 {
		t.Fatal("renewing another user's service")
	}
	if code, _ := call("POST", "/app/api/cancel/"+strconv.FormatInt(o.ID, 10), "", other); code != 404 {
		t.Fatal("cancelling another user's order")
	}
	if code, b := call("POST", "/app/api/link", `{"link":"https://x/sub/nope"}`, other); code != 404 {
		t.Fatal(code, b)
	}
}

// A reseller's bot opens the same Mini App; it shows that reseller's store.
func TestMiniAppReseller(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	seed(t, s)
	SetTelegramToken("123:main-bot-token-xxxxxxxxxxxxxxxxxxxxxxxx")
	defer SetTelegramToken("")
	_ = s.PutRep(ctx, Rep{ID: "ali", Name: "Ali", Enabled: true})
	const repTok = "555555:AArepbottokenrepbottokenrepbottoken12"
	if err := s.PutRepBot(ctx, "ali", RepBot{Token: repTok, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_ = s.PutCardFor(ctx, "ali", CardInfo{Number: "6037991122334455", Holder: "Ali", Bank: "Melli"})
	owner := Principal{User: "boss", Role: "owner"}
	_ = s.PutProduct(ctx, owner, Product{ID: "own", Name: "owner plan", Plan: "volume", GB: 10, Days: 30, Price: 100000, Status: "published", Inbounds: []string{"de-vless"}})
	if err := s.PutProduct(ctx, owner, Product{ID: "alip", Seller: "ali", Name: "ali plan", Plan: "volume", GB: 10, Days: 30, Price: 90000, Status: "published", Inbounds: []string{"de-vless"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutSubSettings(ctx, SubSettings{MiniApp: true}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(SubHandler(s))
	defer srv.Close()
	data := signInitData(repTok, `{"id":888,"first_name":"Sara"}`, *now)
	get := func(method, path, body string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("X-Init-Data", data)
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		return r.StatusCode, string(b)
	}
	code, b := get("GET", "/app/api/me", "")
	var me struct {
		Products []Product
		Card     *CardInfo
		Stars    bool
	}
	if code != 200 || json.Unmarshal([]byte(b), &me) != nil {
		t.Fatal(code, b)
	}
	if len(me.Products) != 1 || me.Products[0].ID != "alip" || me.Card == nil || me.Card.Holder != "Ali" || me.Stars {
		t.Fatalf("the reseller's store only: %s", b)
	}
	if code, _ := get("POST", "/app/api/order", `{"product":"own"}`); code != 403 {
		t.Fatal("the owner's plan is not sold in a reseller's store", code)
	}
	if code, b := get("POST", "/app/api/order", `{"product":"alip"}`); code != 200 {
		t.Fatal(code, b)
	}
	off := false
	_ = s.PutRepBot(ctx, "ali", RepBot{Enabled: true, MiniApp: &off})
	if code, _ := get("GET", "/app/api/me", ""); code != 503 {
		t.Fatal("a reseller can turn its store off", code)
	}
}
