package hubcore

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// OutboundFromLink turns a share link (vless://, vmess://, trojan://, ss://,
// socks://, http://) into an Xray outbound, so a chained upstream server can
// be added by pasting its link instead of writing JSON.
func OutboundFromLink(link, tag string) (RouteOutbound, error) {
	link = strings.TrimSpace(link)
	o := RouteOutbound{Tag: tag, Kind: "custom"}
	var cfg map[string]any
	var err error
	scheme, _, _ := strings.Cut(link, "://")
	switch strings.ToLower(scheme) {
	case "vmess":
		cfg, o.Remark, err = vmessOut(link)
	case "vless", "trojan":
		cfg, o.Remark, err = uriOut(link)
	case "ss":
		cfg, o.Remark, err = ssOut(link)
	case "socks", "socks5", "http", "https":
		cfg, o.Remark, err = proxyOut(link)
	default:
		return o, fmt.Errorf("%w: unsupported link (vless, vmess, trojan, ss, socks, http)", ErrInvalid)
	}
	if err != nil {
		return o, err
	}
	if o.Tag == "" {
		o.Tag = outTag(o.Remark)
	}
	if err := validTag(o.Tag); err != nil {
		return o, err
	}
	cfg["tag"] = o.Tag
	o.Config, _ = json.Marshal(cfg)
	return o, nil
}

func validTag(t string) error {
	if !outTagRE.MatchString(t) || t == "direct" || t == "block" || t == apiTag {
		return fmt.Errorf("%w: pick another outbound tag", ErrInvalid)
	}
	return nil
}

func outTag(remark string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(remark) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
		if b.Len() >= 24 {
			break
		}
	}
	if b.Len() == 0 {
		return "chain"
	}
	return "out-" + b.String()
}

func hostPort(u *url.URL) (string, int, error) {
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if host == "" || err != nil || port < 1 || port > 65535 {
		return "", 0, fmt.Errorf("%w: the link has no valid address and port", ErrInvalid)
	}
	return host, port, nil
}

func uriOut(link string) (map[string]any, string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, "", err
	}
	q := u.Query()
	id := u.User.Username()
	if id == "" {
		return nil, "", fmt.Errorf("%w: the link has no user id/password", ErrInvalid)
	}
	var settings map[string]any
	proto := strings.ToLower(u.Scheme)
	if proto == "vless" {
		user := map[string]any{"id": id, "encryption": or(q.Get("encryption"), "none")}
		if f := q.Get("flow"); f != "" {
			user["flow"] = f
		}
		settings = map[string]any{"vnext": []any{map[string]any{"address": host, "port": port, "users": []any{user}}}}
	} else {
		settings = map[string]any{"servers": []any{map[string]any{"address": host, "port": port, "password": id}}}
	}
	get := func(k string) string { return q.Get(k) }
	return map[string]any{"protocol": proto, "settings": settings, "streamSettings": stream(get, host)}, u.Fragment, nil
}

func or(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

// stream builds streamSettings from share-link style keys.
func stream(get func(string) string, host string) map[string]any {
	nw := or(get("type"), "tcp")
	if nw == "h2" {
		nw = "http"
	}
	ss := map[string]any{"network": nw}
	path, hh := get("path"), get("host")
	switch nw {
	case "ws":
		ss["wsSettings"] = map[string]any{"path": or(path, "/"), "headers": map[string]any{"Host": hh}}
	case "httpupgrade":
		ss["httpupgradeSettings"] = map[string]any{"path": or(path, "/"), "host": hh}
	case "xhttp", "splithttp":
		ss["network"] = "xhttp"
		x := map[string]any{"path": or(path, "/"), "host": hh, "mode": or(get("mode"), "auto")}
		if e := get("extra"); e != "" {
			var extra map[string]any
			if json.Unmarshal([]byte(e), &extra) == nil {
				x["extra"] = extra
			}
		}
		ss["xhttpSettings"] = x
	case "grpc":
		ss["grpcSettings"] = map[string]any{"serviceName": or(get("serviceName"), path), "multiMode": get("mode") == "multi"}
	case "tcp", "raw":
		if get("headerType") == "http" {
			ss["tcpSettings"] = map[string]any{"header": map[string]any{"type": "http", "request": map[string]any{"path": []string{or(path, "/")}, "headers": map[string]any{"Host": strings.Split(hh, ",")}}}}
		}
	}
	sni := or(get("sni"), or(get("peer"), hh))
	if sni == "" && net.ParseIP(host) == nil {
		sni = host
	}
	switch strings.ToLower(get("security")) {
	case "tls":
		t := map[string]any{"serverName": sni, "fingerprint": or(get("fp"), "chrome")}
		if a := get("alpn"); a != "" {
			t["alpn"] = strings.Split(a, ",")
		}
		if get("allowInsecure") == "1" || get("insecure") == "1" {
			t["allowInsecure"] = true
		}
		ss["security"], ss["tlsSettings"] = "tls", t
	case "reality":
		ss["security"], ss["realitySettings"] = "reality", map[string]any{"serverName": sni, "fingerprint": or(get("fp"), "chrome"), "publicKey": get("pbk"), "shortId": get("sid"), "spiderX": get("spx")}
	}
	return ss
}

func b64(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, fmt.Errorf("%w: the link is not valid base64", ErrInvalid)
}

func vmessOut(link string) (map[string]any, string, error) {
	raw, err := b64(link[len("vmess://"):])
	if err != nil {
		return nil, "", err
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, "", fmt.Errorf("%w: vmess link is not JSON", ErrInvalid)
	}
	str := func(k string) string {
		switch x := v[k].(type) {
		case string:
			return x
		case float64:
			return strconv.Itoa(int(x))
		}
		return ""
	}
	port, _ := strconv.Atoi(str("port"))
	if str("add") == "" || port < 1 || port > 65535 || str("id") == "" {
		return nil, "", fmt.Errorf("%w: vmess link misses address, port or id", ErrInvalid)
	}
	keys := map[string]string{"type": str("net"), "path": str("path"), "host": str("host"), "sni": str("sni"), "fp": str("fp"), "alpn": str("alpn"), "security": str("tls"), "headerType": str("type")}
	if keys["type"] == "grpc" {
		keys["serviceName"] = str("path")
		keys["mode"] = str("type")
	}
	get := func(k string) string { return keys[k] }
	user := map[string]any{"id": str("id"), "security": or(str("scy"), "auto")}
	cfg := map[string]any{"protocol": "vmess", "settings": map[string]any{"vnext": []any{map[string]any{"address": str("add"), "port": port, "users": []any{user}}}}, "streamSettings": stream(get, str("add"))}
	return cfg, str("ps"), nil
}

func ssOut(link string) (map[string]any, string, error) {
	rest := link[len("ss://"):]
	rest, frag, _ := strings.Cut(rest, "#")
	remark, _ := url.PathUnescape(frag)
	rest, _, _ = strings.Cut(rest, "?")
	var userinfo, hp string
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		userinfo, hp = rest[:at], rest[at+1:]
		if u, err := url.PathUnescape(userinfo); err == nil {
			userinfo = u
		}
		if !strings.Contains(userinfo, ":") {
			b, err := b64(userinfo)
			if err != nil {
				return nil, "", err
			}
			userinfo = string(b)
		}
	} else {
		b, err := b64(rest)
		if err != nil {
			return nil, "", err
		}
		at := strings.LastIndex(string(b), "@")
		if at < 0 {
			return nil, "", fmt.Errorf("%w: bad ss link", ErrInvalid)
		}
		userinfo, hp = string(b[:at]), string(b[at+1:])
	}
	method, pass, ok := strings.Cut(userinfo, ":")
	host, ps, err := net.SplitHostPort(hp)
	port, _ := strconv.Atoi(ps)
	if !ok || err != nil || port < 1 || port > 65535 {
		return nil, "", fmt.Errorf("%w: bad ss link", ErrInvalid)
	}
	cfg := map[string]any{"protocol": "shadowsocks", "settings": map[string]any{"servers": []any{map[string]any{"address": host, "port": port, "method": method, "password": pass}}}}
	return cfg, remark, nil
}

func proxyOut(link string) (map[string]any, string, error) {
	u, err := url.Parse(link)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	host, port, err := hostPort(u)
	if err != nil {
		return nil, "", err
	}
	server := map[string]any{"address": host, "port": port}
	if u.User != nil {
		pw, _ := u.User.Password()
		server["users"] = []any{map[string]any{"user": u.User.Username(), "pass": pw}}
	}
	proto := "socks"
	if strings.HasPrefix(strings.ToLower(u.Scheme), "http") {
		proto = "http"
	}
	cfg := map[string]any{"protocol": proto, "settings": map[string]any{"servers": []any{server}}}
	if strings.ToLower(u.Scheme) == "https" {
		cfg["streamSettings"] = map[string]any{"security": "tls", "tlsSettings": map[string]any{"serverName": host}}
	}
	return cfg, u.Fragment, nil
}
