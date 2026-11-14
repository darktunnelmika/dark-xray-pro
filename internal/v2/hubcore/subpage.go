package hubcore

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
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
	T                                            map[string]string
}

type pageLink struct{ Name, URL string }

var pageText = map[string]map[string]string{
	"fa": {"used": "مصرف‌شده", "left": "باقی‌مانده", "expires": "انقضا", "days": "روز مانده", "dayunit": "روز", "noexp": "بدون انقضا", "notstarted": "از اولین اتصال شروع می‌شود",
		"active": "فعال", "ended": "تمام‌شده", "disabled": "غیرفعال", "sub": "لینک اشتراک", "copy": "کپی", "copied": "کپی شد", "add": "افزودن به برنامه",
		"configs": "کانفیگ‌ها", "support": "پشتیبانی", "scan": "این کد را با برنامهٔ VPN اسکن کنید", "of": "از", "unlimited": "نامحدود"},
	"en": {"used": "Used", "left": "Remaining", "expires": "Expires", "days": "days left", "dayunit": "days", "noexp": "No expiry", "notstarted": "Starts at first connection",
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
	set := s.SubSettings(ctx)
	d := pageData{Lang: lang, Dir: "rtl", Email: c.Email, Title: or2(set.Title, c.Email), SubURL: subURL, Announce: set.Announce, Support: set.SupportURL, T: T}
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
			d.Links = append(d.Links, pageLink{Name: name, URL: l})
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
		{"v2rayNG", "Android", "v2rayng://install-sub?url=" + xenc + "&name=" + name},
		{"Hiddify", "Android · iOS · Desktop", "hiddify://import/" + subURL + "#" + url.PathEscape(d.Title)},
		{"Happ", "Android · iOS", "happ://add/" + xurl},
		{"Streisand", "iOS", "streisand://import/" + subURL + "#" + url.PathEscape(d.Title)},
		{"V2Box", "iOS", "v2box://install-sub?url=" + enc + "&name=" + name},
		{"sing-box", "Android · iOS", "sing-box://import-remote-profile?url=" + url.QueryEscape(subURL+"?format=singbox") + "#" + url.PathEscape(d.Title)},
		{"Clash Meta", "Android · Desktop", "clash://install-config?url=" + url.QueryEscape(subURL+"?format=clash") + "&name=" + name},
		{"Karing", "Android · iOS · Desktop", "karing://install-config?url=" + enc + "&name=" + name},
	}
	return d, nil
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
</style></head><body><div class="wrap">
<div class="card"><div class="row"><h1>{{.Title}}</h1><span class="sp"></span><span class="badge {{.StatusClass}}">{{.Status}}</span></div>
{{if ne .Title .Email}}<div class="muted">{{.Email}}</div>{{end}}</div>
{{if .Announce}}<div class="card ann">{{.Announce}}</div>{{end}}
<div class="card"><div class="grid">
<div class="stat"><div class="muted">{{index .T "left"}}</div><div class="big"><bdi>{{.Left}}</bdi></div></div>
<div class="stat"><div class="muted">{{index .T "expires"}}</div><div class="big"><bdi>{{.DaysText}}</bdi> <small>{{.DaysUnit}}</small></div><div class="muted"><bdi>{{.ExpiresText}}</bdi></div></div></div>
<div class="row muted" style="margin-top:12px"><span>{{index .T "used"}}: <bdi>{{.Used}}</bdi></span><span class="sp"></span><span>{{index .T "of"}} <bdi>{{.Total}}</bdi></span></div>
<div class="bar"><i style="width:{{.Percent}}%"></i></div></div>
<div class="card"><div class="row"><b>{{index .T "sub"}}</b><span class="sp"></span><button class="primary" data-copy="{{.SubURL}}">{{index .T "copy"}}</button></div>
<img class="qr" src="?qr=1" alt="QR"><div class="muted" style="text-align:center">{{index .T "scan"}}</div><code>{{.SubURL}}</code></div>
<div class="card"><b>{{index .T "add"}}</b><div class="apps" style="margin-top:10px">{{range .Apps}}<a class="btn app" href="{{safeURL .Link}}"><span>{{.Name}}</span><span class="muted">{{.Platform}}</span></a>{{end}}</div></div>
{{if .Links}}<div class="card"><b>{{index .T "configs"}}</b>{{range .Links}}<div class="cfg"><span dir="ltr">{{.Name}}</span><button data-copy="{{.URL}}">{{index $.T "copy"}}</button></div>{{end}}</div>{{end}}
{{if .Support}}<a class="btn" href="{{safeURL .Support}}">💬 {{index .T "support"}}</a>{{end}}
</div><script nonce="{{.Nonce}}">
document.addEventListener("click",function(e){var b=e.target.closest("[data-copy]");if(!b)return;var v=b.getAttribute("data-copy"),t=b.textContent;
(navigator.clipboard?navigator.clipboard.writeText(v):Promise.reject()).catch(function(){var a=document.createElement("textarea");a.value=v;document.body.appendChild(a);a.select();document.execCommand("copy");a.remove();})
.finally(function(){b.textContent={{index .T "copied"}};setTimeout(function(){b.textContent=t},1500)});});
</script></body></html>`))
