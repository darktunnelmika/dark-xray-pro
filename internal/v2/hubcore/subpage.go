package hubcore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
)

// The customer page: opening the subscription link in a browser shows
// usage, time left, a QR code and one-tap buttons for common apps,
// instead of raw links. Apps keep getting their formats as before.

// wantsPage: a browser asked, not a VPN app.
func wantsPage(r *http.Request) bool {
	q := r.URL.Query()
	if q.Has("raw") || q.Get("format") != "" {
		return false
	}
	ua := strings.ToLower(r.UserAgent())
	if !strings.HasPrefix(ua, "mozilla/") {
		return false
	}
	for _, app := range []string{"v2ray", "hiddify", "happ", "streisand", "clash", "mihomo", "sing-box", "sfa", "sfi", "karing", "v2box", "nekobox", "foxray", "shadowrocket"} {
		if strings.Contains(ua, app) {
			return false
		}
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

type pageApp struct {
	Name, Platform, Link string
	Rec                  bool // suits the visitor's device
}

type pageData struct {
	Lang, Dir, Title, Email, Status, StatusClass string
	Used, Total, Left, ExpiresText, DaysText     string
	DaysUnit                                     string
	Percent                                      int
	SubURL                                       string
	Links                                        []pageLink
	Apps                                         []pageApp
	Announce, Support                            string
	Nonce                                        string
	Ring                                         int // stroke offset of the usage ring (0–100)
	OtherLang, OtherLangName                     string
	BotLink                                      string // t.me deep link that ties this service to the visitor's Telegram
	T                                            map[string]string
	Accent                                       template.CSS // checked #rrggbb
	Logo                                         template.URL // checked data:image/… URL
}

type pageLink struct {
	Name, URL string
	I         int
}

var pageText = map[string]map[string]string{
	"fa": {"bot": "اتصال به ربات تلگرام", "botsub": "هشدار پایان حجم و زمان، تمدید و پشتیبانی", "qr": "QR", "rec": "پیشنهادی برای دستگاه شما", "other": "برنامه‌های دیگر", "usage": "مصرف", "hide": "بستن", "used": "مصرف‌شده", "left": "باقی‌مانده", "expires": "انقضا", "days": "روز مانده", "dayunit": "روز", "noexp": "بدون انقضا", "notstarted": "از اولین اتصال شروع می‌شود",
		"active": "فعال", "ended": "تمام‌شده", "disabled": "غیرفعال", "sub": "لینک اشتراک", "copy": "کپی", "copied": "کپی شد", "add": "افزودن به برنامه",
		"configs": "کانفیگ‌ها", "support": "پشتیبانی", "scan": "این کد را با برنامهٔ VPN اسکن کنید", "of": "از", "unlimited": "نامحدود"},
	"en": {"bot": "Connect the Telegram bot", "botsub": "Alerts before it runs out, renewal and support", "qr": "QR", "rec": "Suggested for your device", "other": "Other apps", "usage": "Usage", "hide": "Close", "used": "Used", "left": "Remaining", "expires": "Expires", "days": "days left", "dayunit": "days", "noexp": "No expiry", "notstarted": "Starts at first connection",
		"active": "Active", "ended": "Ended", "disabled": "Disabled", "sub": "Subscription link", "copy": "Copy", "copied": "Copied", "add": "Add to an app",
		"configs": "Configs", "support": "Support", "scan": "Scan this code with your VPN app", "of": "of", "unlimited": "Unlimited"},
}

func (s *Store) subPageData(ctx context.Context, email, subURL, lang string) (pageData, error) {
	var c Client
	var u Usage
	err := s.ro.QueryRowContext(ctx, `SELECT c.email,c.enabled,c.volume_bytes,c.expires_unix,c.duration_seconds,COALESCE(cu.up,0),COALESCE(cu.down,0),COALESCE(cu.first_seen_unix,0)
FROM clients c LEFT JOIN client_usage cu ON cu.email=c.email WHERE c.email=?`, email).
		Scan(&c.Email, &c.Enabled, &c.VolumeBytes, &c.ExpiresUnix, &c.DurationSeconds, &u.Up, &u.Down, &u.FirstSeen)
	if err != nil {
		return pageData{}, ErrNotFound
	}
	T := pageText[lang]
	set, _ := s.brandFor(ctx, email)
	d := pageData{Lang: lang, Dir: "rtl", Email: c.Email, Title: or2(set.Title, c.Email), SubURL: subURL, Announce: set.Announce, Support: set.SupportURL, T: T}
	if accentRE.MatchString(set.Accent) {
		d.Accent = template.CSS(set.Accent)
	}
	if validLogo(set.Logo) {
		d.Logo = template.URL(set.Logo)
	}
	if lang == "en" {
		d.Dir = "ltr"
	}
	used := u.Up + u.Down
	d.Used = gbText(used)
	left := Remaining(c, u)
	d.Left = gbText(left)
	d.Total = T["unlimited"]
	if c.VolumeBytes > 0 {
		d.Total = gbText(c.VolumeBytes)
		d.Percent = int(100 * float64(used) / float64(c.VolumeBytes))
		if d.Percent > 100 {
			d.Percent = 100
		}
	}
	now := s.now().Unix()
	exp := EffectiveExpiry(c, u)
	switch {
	case exp > 0:
		days := (exp - now + 86399) / 86400
		if days < 0 {
			days = 0
		}
		d.DaysText, d.DaysUnit = fmt.Sprint(days), T["days"]
		d.ExpiresText = time.Unix(exp, 0).UTC().Format("2006-01-02")
	case c.DurationSeconds > 0:
		d.DaysText, d.DaysUnit = fmt.Sprint(c.DurationSeconds/86400), T["dayunit"]
		d.ExpiresText = T["notstarted"]
	default:
		d.DaysText, d.ExpiresText = "∞", T["noexp"]
	}
	d.Status, d.StatusClass = T["active"], "good"
	if !c.Enabled {
		d.Status, d.StatusClass = T["disabled"], "bad"
	} else if left == 0 || (exp > 0 && exp <= now) {
		d.Status, d.StatusClass = T["ended"], "bad"
	}
	links, _, err := s.Subscription(ctx, email)
	if err == nil {
		for _, l := range links {
			if strings.Contains(l, "00000000-0000-0000-0000-000000000000@127.0.0.1") {
				continue
			}
			name := l
			if i := strings.LastIndex(l, "#"); i >= 0 {
				if n, err := url.PathUnescape(l[i+1:]); err == nil {
					name = n
				}
			} else if strings.HasPrefix(l, "vmess://") {
				name = "VMess"
			}
			d.Links = append(d.Links, pageLink{Name: name, URL: l, I: len(d.Links)})
		}
	}
	enc := url.QueryEscape(subURL)
	// With fragment on, apps that import Xray JSON get that format.
	xenc, xurl := enc, subURL
	if set.Fragment {
		xurl = subURL + "?format=xray"
		xenc = url.QueryEscape(xurl)
	}
	name := url.QueryEscape(d.Title)
	d.Apps = []pageApp{
		{"v2rayNG", "Android", "v2rayng://install-sub?url=" + xenc + "&name=" + name, false},
		{"Hiddify", "Android · iOS · Desktop", "hiddify://import/" + subURL + "#" + url.PathEscape(d.Title), false},
		{"Happ", "Android · iOS", "happ://add/" + xurl, false},
		{"Streisand", "iOS", "streisand://import/" + subURL + "#" + url.PathEscape(d.Title), false},
		{"V2Box", "iOS", "v2box://install-sub?url=" + enc + "&name=" + name, false},
		{"sing-box", "Android · iOS", "sing-box://import-remote-profile?url=" + url.QueryEscape(subURL+"?format=singbox") + "#" + url.PathEscape(d.Title), false},
		{"Clash Meta", "Android · Desktop", "clash://install-config?url=" + url.QueryEscape(subURL+"?format=clash") + "&name=" + name, false},
		{"Karing", "Android · iOS · Desktop", "karing://install-config?url=" + enc + "&name=" + name, false},
	}
	d.Ring = 100 - d.Percent
	if c.VolumeBytes <= 0 {
		d.Ring = 0
	}
	var seller string
	_ = s.ro.QueryRowContext(ctx, `SELECT rep_id FROM clients WHERE email=?`, email).Scan(&seller)
	if bot := s.BotUsernameFor(ctx, seller); bot != "" && botNameRE.MatchString(bot) {
		if i := strings.LastIndex(subURL, "/sub/"); i >= 0 {
			if tok := subURL[i+5:]; tgStartRE.MatchString(tok) {
				d.BotLink = "https://t.me/" + bot + "?start=" + tok
			}
		}
	}
	d.OtherLang, d.OtherLangName = "en", "English"
	if lang == "en" {
		d.OtherLang, d.OtherLangName = "fa", "فارسی"
	}
	return d, nil
}

// suggestApps puts the apps for the visitor's system first.
func suggestApps(apps []pageApp, ua string) []pageApp {
	ua = strings.ToLower(ua)
	os := ""
	switch {
	case strings.Contains(ua, "android"):
		os = "Android"
	case strings.Contains(ua, "iphone"), strings.Contains(ua, "ipad"):
		os = "iOS"
	case strings.Contains(ua, "windows"), strings.Contains(ua, "macintosh"), strings.Contains(ua, "linux"):
		os = "Desktop"
	}
	if os == "" {
		return apps
	}
	var rec, rest []pageApp
	for _, a := range apps {
		if strings.Contains(a.Platform, os) && len(rec) < 3 {
			a.Rec = true
			rec = append(rec, a)
		} else {
			rest = append(rest, a)
		}
	}
	return append(rec, rest...)
}

// configQR is the QR of one config on the page (?qr=1&cfg=N).
func (s *Store) configQR(ctx context.Context, w http.ResponseWriter, email string, n int) {
	d, err := s.subPageData(ctx, email, "", "fa")
	if err != nil || n < 0 || n >= len(d.Links) {
		http.NotFound(w, nil)
		return
	}
	serveSubQR(w, d.Links[n].URL)
}

func or2(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func pageLang(r *http.Request) string {
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "en") || r.URL.Query().Get("lang") == "en" {
		return "en"
	}
	return "fa"
}

func (s *Store) serveSubPage(w http.ResponseWriter, r *http.Request, email, subURL string) {
	d, err := s.subPageData(r.Context(), email, subURL, pageLang(r))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	nb := make([]byte, 16)
	_, _ = rand.Read(nb)
	d.Nonce = base64.StdEncoding.EncodeToString(nb)
	d.Apps = suggestApps(d.Apps, r.UserAgent())
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'unsafe-inline'; script-src 'nonce-"+d.Nonce+"'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_ = subPageTmpl.Execute(w, d)
}

func serveSubQR(w http.ResponseWriter, subURL string) {
	png, err := qrcode.Encode(subURL, qrcode.Medium, 320)
	if err != nil {
		http.Error(w, "qr", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(png)
}

var subPageTmpl = template.Must(template.New("sub").Funcs(template.FuncMap{"safeURL": func(s string) template.URL { return template.URL(s) }}).Parse(`<!DOCTYPE html>
<html lang="{{.Lang}}" dir="{{.Dir}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="dark"><meta name="robots" content="noindex"><title>{{.Title}}</title>
<style>
:root{--bg:#060912;--glass:rgb(148 163 255/.06);--line:rgb(160 175 255/.13);--text:#e9ecff;--muted:#8d95c4;--cyan:#2de2e6;--violet:#9b5cff;--good:#3ef0a1;--bad:#ff5f7e;--warn:#ffc65c}
{{if .Accent}}:root{--cyan:{{.Accent}};--violet:{{.Accent}}}{{end}}
.logo{width:44px;height:44px;border-radius:12px;object-fit:contain;flex:none}
*{box-sizing:border-box}html,body{overflow-x:hidden}body{margin:0;min-height:100vh;background:radial-gradient(1200px 600px at 80% -10%,rgb(155 92 255/.18),transparent 60%),radial-gradient(900px 500px at -10% 10%,rgb(45 226 230/.12),transparent 60%),var(--bg);
color:var(--text);font:15px/1.6 Vazirmatn,"Segoe UI",Tahoma,system-ui,sans-serif;padding:20px 14px 40px}
.wrap{max-width:560px;margin:0 auto;display:grid;gap:14px;grid-template-columns:minmax(0,1fr)}.wrap>*{min-width:0}
.card{background:var(--glass);border:1px solid var(--line);border-radius:20px;padding:18px;backdrop-filter:blur(16px);box-shadow:inset 0 1px 0 rgb(45 226 230/.15)}
h1{margin:0;font-size:22px}.row{display:flex;gap:10px;align-items:center;flex-wrap:wrap}.sp{flex:1}
.badge{font-size:12px;padding:3px 12px;border-radius:99px;border:1px solid currentColor}.good{color:var(--good)}.bad{color:var(--bad)}
.muted{color:var(--muted);font-size:13px}.big{font-size:26px;font-weight:700;text-shadow:0 0 18px rgb(45 226 230/.35);white-space:nowrap}.big small{font-size:13px;font-weight:500;color:var(--muted);text-shadow:none}
.grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:10px}.stat{background:rgb(255 255 255/.03);border:1px solid var(--line);border-radius:14px;padding:12px}
.bar{height:8px;border-radius:99px;background:rgb(255 255 255/.06);overflow:hidden;margin-top:10px}.bar i{display:block;height:100%;border-radius:inherit;background:linear-gradient(90deg,var(--violet),var(--cyan));box-shadow:0 0 12px var(--cyan)}
.qr{display:block;margin:6px auto;width:220px;height:220px;border-radius:14px;background:#fff;padding:8px}
code{display:block;direction:ltr;text-align:left;word-break:break-all;font-size:12px;background:rgb(0 0 0/.3);border-radius:10px;padding:10px}
button,.btn{appearance:none;border:1px solid var(--line);background:rgb(255 255 255/.05);color:var(--text);border-radius:12px;padding:9px 14px;font:inherit;cursor:pointer;text-decoration:none;display:inline-flex;gap:6px;align-items:center}
.btn.app{justify-content:space-between;width:100%}.apps{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);gap:8px}
.primary{background:linear-gradient(90deg,rgb(155 92 255/.35),rgb(45 226 230/.35));border-color:rgb(45 226 230/.5)}
.cfg{display:flex;gap:8px;align-items:center;padding:8px 0;border-bottom:1px solid var(--line)}.cfg:last-child{border:0}.cfg span{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.ann{border-color:rgb(255 198 92/.4);color:var(--warn);white-space:pre-wrap}
@media(max-width:420px){.apps{grid-template-columns:1fr}}
.hero{display:grid;grid-template-columns:auto minmax(0,1fr);gap:18px;align-items:center}
.ring{position:relative;width:118px;height:118px}.ring svg{width:100%;height:100%;transform:rotate(-90deg)}
.ring .tr{stroke:rgb(255 255 255/.07)}.ring .fg{stroke:url(#g);stroke-linecap:round;filter:drop-shadow(0 0 6px var(--cyan))}
.ring .c{position:absolute;inset:0;display:grid;place-content:center;text-align:center;line-height:1.2}.ring .c b{font-size:20px}.ring .c span{font-size:11px;color:var(--muted)}
.kv{display:grid;gap:6px}.kv div{display:flex;justify-content:space-between;gap:10px;border-bottom:1px dashed var(--line);padding-bottom:6px}.kv div:last-child{border:0}
.kv b{white-space:nowrap}.lang{color:var(--muted);font-size:13px;text-decoration:none}
.rec{border-color:rgb(45 226 230/.55);box-shadow:0 0 18px -8px var(--cyan)}.lbl{font-size:12px;color:var(--muted);margin:12px 0 6px}
.cqr{display:none;margin:8px auto 4px;width:200px;height:200px;border-radius:12px;background:#fff;padding:6px}.cfg.open .cqr{display:block}.cfg{flex-wrap:wrap}
@media(max-width:400px){.hero{grid-template-columns:1fr;justify-items:center}.kv{width:100%}}
</style></head><body><div class="wrap">
<div class="row"><a class="lang" href="?lang={{.OtherLang}}">🌐 {{.OtherLangName}}</a></div>
<div class="card"><div class="row">{{if .Logo}}<img class="logo" src="{{.Logo}}" alt="">{{end}}<h1>{{.Title}}</h1><span class="sp"></span><span class="badge {{.StatusClass}}">{{.Status}}</span></div>
{{if ne .Title .Email}}<div class="muted">{{.Email}}</div>{{end}}</div>
{{if .Announce}}<div class="card ann">{{.Announce}}</div>{{end}}
<div class="card hero">
<div class="ring"><svg viewBox="0 0 36 36"><defs><linearGradient id="g"><stop offset="0" stop-color="var(--violet)"/><stop offset="1" stop-color="var(--cyan)"/></linearGradient></defs>
<circle class="tr" cx="18" cy="18" r="15.9" fill="none" stroke-width="3"/><circle class="fg" cx="18" cy="18" r="15.9" fill="none" stroke-width="3" pathLength="100" stroke-dasharray="100" stroke-dashoffset="{{.Ring}}"/></svg>
<div class="c"><b><bdi>{{.Left}}</bdi></b><span>{{index .T "left"}}</span></div></div>
<div class="kv">
<div><span class="muted">{{index .T "used"}}</span><b><bdi>{{.Used}}</bdi> <small class="muted">{{index .T "of"}} <bdi>{{.Total}}</bdi></small></b></div>
<div><span class="muted">{{index .T "expires"}}</span><b><bdi>{{.DaysText}}</bdi> <small class="muted">{{.DaysUnit}}</small></b></div>
<div><span class="muted"></span><span class="muted"><bdi>{{.ExpiresText}}</bdi></span></div>
</div></div>
{{if .BotLink}}<a class="btn app rec" href="{{safeURL .BotLink}}"><span>🤖 {{index .T "bot"}}</span><span class="muted">{{index .T "botsub"}}</span></a>{{end}}
<div class="card"><b>{{index .T "add"}}</b>
{{$rec := false}}{{range .Apps}}{{if .Rec}}{{$rec = true}}{{end}}{{end}}
{{if $rec}}<div class="lbl">{{index .T "rec"}}</div><div class="apps">{{range .Apps}}{{if .Rec}}<a class="btn app rec" href="{{safeURL .Link}}"><span>{{.Name}}</span><span class="muted">{{.Platform}}</span></a>{{end}}{{end}}</div><div class="lbl">{{index .T "other"}}</div>{{end}}
<div class="apps"{{if not $rec}} style="margin-top:10px"{{end}}>{{range .Apps}}{{if not .Rec}}<a class="btn app" href="{{safeURL .Link}}"><span>{{.Name}}</span><span class="muted">{{.Platform}}</span></a>{{end}}{{end}}</div></div>
<div class="card"><div class="row"><b>{{index .T "sub"}}</b><span class="sp"></span><button class="primary" data-copy="{{.SubURL}}">{{index .T "copy"}}</button></div>
<img class="qr" src="?qr=1" alt="QR"><div class="muted" style="text-align:center">{{index .T "scan"}}</div><code>{{.SubURL}}</code></div>
{{if .Links}}<div class="card"><b>{{index .T "configs"}}</b>{{range .Links}}<div class="cfg"><span dir="ltr">{{.Name}}</span><button data-qr>{{index $.T "qr"}}</button><button data-copy="{{.URL}}">{{index $.T "copy"}}</button><img class="cqr" alt="QR" loading="lazy" data-src="?qr=1&amp;cfg={{.I}}"></div>{{end}}</div>{{end}}
{{if .Support}}<a class="btn" href="{{safeURL .Support}}">💬 {{index .T "support"}}</a>{{end}}
</div><script nonce="{{.Nonce}}">
document.addEventListener("click",function(e){var q=e.target.closest("[data-qr]");if(q){var r=q.parentNode,im=r.querySelector(".cqr");if(!im.src)im.src=im.getAttribute("data-src");r.classList.toggle("open");return;}
var b=e.target.closest("[data-copy]");if(!b)return;var v=b.getAttribute("data-copy"),t=b.textContent;
(navigator.clipboard?navigator.clipboard.writeText(v):Promise.reject()).catch(function(){var a=document.createElement("textarea");a.value=v;document.body.appendChild(a);a.select();document.execCommand("copy");a.remove();})
.finally(function(){b.textContent={{index .T "copied"}};setTimeout(function(){b.textContent=t},1500)});});
</script></body></html>`))

var accentRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var (
	botNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{3,31}$`)
	tgStartRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`) // Telegram's limit for a start parameter
)

// validLogo accepts a small inline image (the page's CSP only allows data:
// images, so no third party ever sees a customer open their page).
func validLogo(v string) bool {
	if v == "" || len(v) > 200000 {
		return false
	}
	for _, p := range []string{"data:image/png;base64,", "data:image/jpeg;base64,", "data:image/webp;base64,", "data:image/svg+xml;base64,"} {
		if strings.HasPrefix(v, p) {
			_, err := base64.StdEncoding.DecodeString(v[len(p):])
			return err == nil
		}
	}
	return false
}
