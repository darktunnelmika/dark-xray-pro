package hubcore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// The panel as a Telegram Mini App. A reseller (or the owner) opens
// /tgapp/ from a button in its bot; the page sends Telegram's signed
// initData, the HUB checks it with the main bot's token or a reseller bot's
// (the matching bot tells whose bot it is) and signs the chat in to the
// panel it manages: no password, the same panel and rights as on the web.
//
// Who may: in the main bot the panel's Telegram owners (the owner's panel)
// and resellers managed there; in a reseller's bot that reseller (and its
// sub-sellers). Only a reseller's own chats and its "manager" bot admins
// get in; "sales" and "support" admins keep using the bot. A login with
// two-step on is not signed in this way (it must use its password and
// code).

const tgPanelMaxAge = time.Hour

// ErrTgPanel explains why a Telegram chat cannot open the panel.
var ErrTgPanel = errors.New("this Telegram account does not manage a panel here")

// TgPanelLogin checks initData and returns a new session token and the
// login it belongs to.
func (s *Store) TgPanelLogin(ctx context.Context, initData string) (string, string, error) {
	u, seller, err := s.verifyMiniUser(ctx, initData)
	if err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrAuth, err)
	}
	// verifyMiniUser accepts a day-old signature (the store); signing in
	// to a panel wants a fresh one.
	if _, err := VerifyInitData(initData, s.tokenFor(ctx, seller), s.now(), tgPanelMaxAge); err != nil {
		return "", "", fmt.Errorf("%w: open the panel again from the bot", ErrAuth)
	}
	user := ""
	if seller == "" {
		for _, o := range s.TelegramOwners(ctx) {
			if o == u.ID {
				_ = s.ro.QueryRowContext(ctx, `SELECT username FROM admins WHERE role='owner' ORDER BY created_unix,username LIMIT 1`).Scan(&user)
				break
			}
		}
	}
	if user == "" {
		// A reseller's bot also manages its sub-sellers.
		rep, role := s.TgRepRoleIn(ctx, u.ID, "", s.inBotScope(ctx, seller))
		if rep == "" || (role != "owner" && role != "manager") {
			return "", "", ErrTgPanel
		}
		if user, err = s.repPanelLogin(ctx, rep); err != nil {
			return "", "", err
		}
	}
	if user == "" {
		return "", "", ErrTgPanel
	}
	var secret string
	_ = s.ro.QueryRowContext(ctx, `SELECT totp_secret FROM admins WHERE username=?`, user).Scan(&secret)
	if secret != "" {
		return "", "", fmt.Errorf("%w: two-step login is on for this panel; sign in with the password and code", ErrAuth)
	}
	tok, err := s.newSession(ctx, user)
	return tok, user, err
}

// tokenFor is the bot token of a seller's bot ("" = the main bot).
func (s *Store) tokenFor(ctx context.Context, seller string) string {
	if seller == "" {
		return tgToken
	}
	for _, b := range s.RepBotsAll(ctx) {
		if b.Rep == seller {
			return b.Token
		}
	}
	return ""
}

// repPanelLogin is the sign-in name the reseller's panel sessions use: its
// first login, or a new one for Telegram only (with a random password
// nobody knows) when it has none.
func (s *Store) repPanelLogin(ctx context.Context, rep string) (string, error) {
	var user string
	err := s.ro.QueryRowContext(ctx, `SELECT username FROM admins WHERE role='rep' AND rep_id=? ORDER BY created_unix,username LIMIT 1`, rep).Scan(&user)
	if err == nil && user != "" {
		return user, nil
	}
	user = "tg-" + rep
	if len(user) > 64 {
		user = user[:64]
	}
	if err := s.SetRepLogin(ctx, user, randomHex(24), rep); err != nil {
		return "", err
	}
	return user, nil
}

// newSession opens a panel session for a login.
func (s *Store) newSession(ctx context.Context, user string) (string, error) {
	tok := randomHex(32)
	now := s.now()
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_unix<?`, now.Unix()); err != nil {
		return "", err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions(token_sha256,username,expires_unix) VALUES(?,?,?)`,
		hashToken(tok), user, now.Add(sessionTTL).Unix())
	return tok, err
}

const tgPanelHTML = `<!doctype html><html lang="fa" dir="rtl"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>…</title>
<link rel="stylesheet" href="/tgapp/s.css"><script src="https://telegram.org/js/telegram-web-app.js"></script></head>
<body><main><div class="spin"></div><p id="m">در حال ورود به پنل…</p></main><script src="/tgapp/a.js"></script></body></html>`

const tgPanelCSS = `body{margin:0;font:15px/1.7 system-ui,-apple-system,"Segoe UI",Tahoma,sans-serif;background:var(--tg-theme-bg-color,#0b0f17);color:var(--tg-theme-text-color,#e6edf3)}
main{min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:24px;text-align:center}
.spin{width:36px;height:36px;border-radius:50%;border:3px solid #2de2e633;border-top-color:#2de2e6;animation:r 1s linear infinite}
@keyframes r{to{transform:rotate(360deg)}}.err .spin{display:none}.err p{color:#ff7b72}`

const tgPanelJS = `(function(){
var tg=window.Telegram&&window.Telegram.WebApp;try{tg.ready();tg.expand();}catch(e){}
var init=(tg&&tg.initData)||new URLSearchParams(location.hash.slice(1)).get("tgWebAppData")||"";
var m=document.getElementById("m");
function fail(t){document.body.className="err";m.textContent=t;}
if(!init){fail("این صفحه را از دکمهٔ «پنل مدیریت» داخل ربات باز کنید.");return;}
fetch("/tgapp/login",{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({init_data:init})})
.then(function(r){return r.ok?r.json():r.text().then(function(t){throw new Error(t);});})
.then(function(j){location.replace(j.go||"/");})
.catch(function(e){fail(e.message||"ورود ممکن نشد.");});
})();`

func registerTgPanel(mux *http.ServeMux, s *Store) {
	static := func(ctype, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ctype)
			w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self' https://telegram.org; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "no-cache")
			_, _ = w.Write([]byte(body))
		}
	}
	mux.HandleFunc("GET /tgapp/{$}", static("text/html; charset=utf-8", tgPanelHTML))
	mux.HandleFunc("GET /tgapp/a.js", static("text/javascript; charset=utf-8", tgPanelJS))
	mux.HandleFunc("GET /tgapp/s.css", static("text/css; charset=utf-8", tgPanelCSS))
	mux.HandleFunc("POST /tgapp/login", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			InitData string `json:"init_data"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&in); err != nil || in.InitData == "" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		tok, user, err := s.TgPanelLogin(r.Context(), in.InitData)
		if err != nil {
			msg := "ورود ممکن نشد: این حساب تلگرام پنلی را اینجا مدیریت نمی‌کند."
			if strings.Contains(err.Error(), "two-step") {
				msg = "ورود دومرحله‌ای این پنل روشن است؛ با رمز و کد وارد شوید."
			} else if strings.Contains(err.Error(), "again") {
				msg = "این صفحه قدیمی شده؛ دوباره از دکمهٔ «پنل مدیریت» در ربات باز کنید."
			}
			http.Error(w, msg, http.StatusForbidden)
			return
		}
		setSessionCookie(w, r, tok, int(sessionTTL.Seconds()))
		if secret := s.panelPath(r.Context()); secret != "" {
			setGate(w, r, secret)
		}
		s.Audit(r.Context(), Principal{User: user}, r, http.StatusOK)
		reply(w, map[string]string{"go": "/"}, nil)
	})
}
