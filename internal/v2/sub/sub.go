// Package sub renders subscription content for v2 clients: share links,
// the Base64 list most clients import, and a sing-box outbound set.
package sub

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Endpoint is one public way to reach an inbound: the node's own address
// (Direct) or an entry server in front of it (Tunnel).
type Endpoint struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	// Optional per-host overrides of what the client sends.
	SNI           string   `json:"sni,omitempty"`
	Host          string   `json:"host,omitempty"`
	Path          string   `json:"path,omitempty"` // ws/httpupgrade/xhttp path or gRPC service name
	ALPN          string   `json:"alpn,omitempty"` // comma-separated
	Fingerprint   string   `json:"fingerprint,omitempty"`
	AllowInsecure bool     `json:"allow_insecure,omitempty"`
	Exclude       []string `json:"exclude,omitempty"`  // formats that skip this host: links | singbox | clash
	Fragment      bool     `json:"fragment,omitempty"` // Xray JSON: fragment the TLS hello for this address
	Mux           int      `json:"mux,omitempty"`      // Xray JSON: mux concurrency (0 = off)
}

func (ep Endpoint) excluded(format string) bool {
	for _, f := range ep.Exclude {
		if f == format {
			return true
		}
	}
	return false
}

// forEndpoint applies an endpoint's overrides to a copy of the template.
func forEndpoint(t template, m Meta, ep Endpoint) (template, Meta) {
	if ep.SNI != "" {
		if t.Stream.Security == "reality" {
			t.Stream.Reality.ServerNames = []string{ep.SNI}
		} else {
			t.Stream.TLS.ServerName = ep.SNI
		}
	}
	if ep.Host != "" {
		t.Stream.WS.Host, t.Stream.HTTPUpgrade.Host, t.Stream.XHTTP.Host = ep.Host, ep.Host, ep.Host
	}
	if ep.Path != "" {
		t.Stream.WS.Path, t.Stream.HTTPUpgrade.Path, t.Stream.XHTTP.Path = ep.Path, ep.Path, ep.Path
		t.Stream.GRPC.ServiceName = ep.Path
	}
	if ep.ALPN != "" {
		t.Stream.TLS.ALPN = strings.Split(ep.ALPN, ",")
	}
	if ep.Fingerprint != "" {
		m.Fingerprint = ep.Fingerprint
	}
	t.Stream.insecure = ep.AllowInsecure || m.Insecure
	return t, m
}

// Meta holds what a client needs but the server config does not carry.
type Meta struct {
	Endpoints   []Endpoint `json:"endpoints"`
	PublicKey   string     `json:"public_key,omitempty"`  // REALITY
	ShortID     string     `json:"short_id,omitempty"`    // REALITY
	Fingerprint string     `json:"fingerprint,omitempty"` // uTLS
	SpiderX     string     `json:"spider_x,omitempty"`
	Remark      string     `json:"remark,omitempty"`
	Insecure    bool       `json:"insecure,omitempty"`   // self-signed certificate: clients skip CA verification
	PinSHA256   string     `json:"pin_sha256,omitempty"` // and pin this certificate fingerprint where they can
	MTU         int        `json:"mtu,omitempty"`        // WireGuard
	// VLESSEncryption is what clients send when the inbound uses Xray's
	// VLESS encryption (settings.decryption on the server); empty = none.
	VLESSEncryption string `json:"vless_encryption,omitempty"`
	// Plain inbounds (SOCKS, HTTP, port forwarding) shown on the inbound card.
	ProxyUser string `json:"proxy_user,omitempty"`
	ProxyPass string `json:"proxy_pass,omitempty"`
	Target    string `json:"target,omitempty"`
	// MTProto proxy secret (shared, shown on the inbound card).
	MTProtoSecret string `json:"mtproto_secret,omitempty"`
	// Per client, filled when a subscription is built (never stored):
	WGPrivateKey string `json:"-"`
	WGAddress    string `json:"-"` // e.g. 10.66.0.2/32
}

// Inbound is one inbound template with its metadata.
type Inbound struct {
	Tag      string
	Template []byte
	Meta     Meta
}

// Account identifies the client inside every protocol.
type Account struct {
	Email  string
	UUID   string
	Secret string
	// IranDirect adds "Iranian sites and IPs go direct" to app profiles
	// (sing-box, Clash): those sites often refuse foreign addresses.
	IranDirect bool
	// RemarkTemplate names each config, e.g. "{remark} · {path} · {left}".
	// Variables: {remark} {tag} {path} {email} {used} {left} {total} {days} {proto}.
	RemarkTemplate string
	// Vars holds the client's live values for the template.
	Vars map[string]string
	// InfoLines adds non-working entries at the top of plain link lists
	// that show remaining volume and days in the app's server list.
	InfoLines []string
	// Fragment, if set, adds fragmentation (and noise) to Xray JSON configs.
	Fragment *Fragment
}

type stream struct {
	insecure bool
	Network  string `json:"network"`
	Security string `json:"security"`
	Reality  struct {
		ServerNames []string `json:"serverNames"`
		ShortIDs    []string `json:"shortIds"`
	} `json:"realitySettings"`
	TLS struct {
		ServerName string   `json:"serverName"`
		ALPN       []string `json:"alpn"`
	} `json:"tlsSettings"`
	GRPC struct {
		ServiceName string `json:"serviceName"`
		MultiMode   bool   `json:"multiMode"`
	} `json:"grpcSettings"`
	WS struct {
		Path string `json:"path"`
		Host string `json:"host"`
	} `json:"wsSettings"`
	HTTPUpgrade struct {
		Path string `json:"path"`
		Host string `json:"host"`
	} `json:"httpupgradeSettings"`
	XHTTP struct {
		Path string `json:"path"`
		Host string `json:"host"`
		Mode string `json:"mode"`
	} `json:"xhttpSettings"`
	KCP struct {
		Header struct {
			Type string `json:"type"`
		} `json:"header"`
		Seed string `json:"seed"`
	} `json:"kcpSettings"`
	TCP struct {
		Header struct {
			Type    string `json:"type"`
			Request struct {
				Path    []string            `json:"path"`
				Headers map[string][]string `json:"headers"`
			} `json:"request"`
		} `json:"header"`
	} `json:"tcpSettings"`
}

// tcpHTTP returns the path and Host of a TCP inbound with HTTP obfuscation.
func (s stream) tcpHTTP() (path, host string, ok bool) {
	if s.Network != "tcp" || s.TCP.Header.Type != "http" {
		return "", "", false
	}
	path = "/"
	if len(s.TCP.Header.Request.Path) > 0 {
		path = s.TCP.Header.Request.Path[0]
	}
	for k, v := range s.TCP.Header.Request.Headers {
		if strings.EqualFold(k, "host") && len(v) > 0 {
			host = strings.Join(v, ",")
		}
	}
	return path, host, true
}

type template struct {
	Protocol string `json:"protocol"`
	Settings struct {
		Flow              string `json:"flow"`
		Method            string `json:"method"`             // shadowsocks
		CongestionControl string `json:"congestion_control"` // tuic
		Password          string `json:"password"`           // shadowsocks 2022 server key
	} `json:"settings"`
	Stream stream `json:"streamSettings"`
}

func parse(raw []byte) (template, error) {
	var t template
	if err := json.Unmarshal(raw, &t); err != nil {
		return t, err
	}
	if t.Stream.Network == "" {
		t.Stream.Network = "tcp"
	}
	if t.Stream.Security == "" {
		t.Stream.Security = "none"
	}
	return t, nil
}

func first(v []string) string {
	if len(v) > 0 {
		return v[0]
	}
	return ""
}

// params builds the shared query string of vless/trojan links.
func params(t template, m Meta) url.Values {
	q := url.Values{}
	s := t.Stream
	q.Set("type", s.Network)
	q.Set("security", s.Security)
	switch s.Security {
	case "reality":
		q.Set("sni", first(s.Reality.ServerNames))
		q.Set("pbk", m.PublicKey)
		sid := m.ShortID
		if sid == "" {
			sid = first(s.Reality.ShortIDs)
		}
		q.Set("sid", sid)
		q.Set("fp", or(m.Fingerprint, "chrome"))
		if m.SpiderX != "" {
			q.Set("spx", m.SpiderX)
		}
	case "tls":
		q.Set("sni", s.TLS.ServerName)
		q.Set("fp", or(m.Fingerprint, "chrome"))
		if s.insecure {
			q.Set("allowInsecure", "1")
		}
		if len(s.TLS.ALPN) > 0 {
			q.Set("alpn", strings.Join(s.TLS.ALPN, ","))
		}
	}
	switch s.Network {
	case "tcp":
		if p, h, ok := s.tcpHTTP(); ok {
			q.Set("headerType", "http")
			q.Set("path", p)
			if h != "" {
				q.Set("host", h)
			}
		}
	case "kcp":
		q.Set("headerType", or(s.KCP.Header.Type, "none"))
		if s.KCP.Seed != "" {
			q.Set("seed", s.KCP.Seed)
		}
	case "grpc":
		q.Set("serviceName", s.GRPC.ServiceName)
		if s.GRPC.MultiMode {
			q.Set("mode", "multi")
		} else {
			q.Set("mode", "gun")
		}
	case "ws":
		q.Set("path", or(s.WS.Path, "/"))
		if s.WS.Host != "" {
			q.Set("host", s.WS.Host)
		}
	case "httpupgrade":
		q.Set("path", or(s.HTTPUpgrade.Path, "/"))
		if s.HTTPUpgrade.Host != "" {
			q.Set("host", s.HTTPUpgrade.Host)
		}
	case "xhttp", "splithttp":
		q.Set("path", or(s.XHTTP.Path, "/"))
		if s.XHTTP.Host != "" {
			q.Set("host", s.XHTTP.Host)
		}
		if s.XHTTP.Mode != "" {
			q.Set("mode", s.XHTTP.Mode)
		}
	}
	return q
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func remark(a Account, in Inbound, ep Endpoint) string {
	base := or(in.Meta.Remark, in.Tag)
	if a.RemarkTemplate == "" {
		if ep.Name != "" {
			return base + " | " + ep.Name
		}
		return base
	}
	var proto string
	var t template
	if json.Unmarshal(in.Template, &t) == nil {
		proto = t.Protocol
	}
	pairs := []string{"{remark}", base, "{tag}", in.Tag, "{path}", ep.Name, "{proto}", proto}
	for k, v := range a.Vars {
		pairs = append(pairs, "{"+k+"}", v)
	}
	out := strings.NewReplacer(pairs...).Replace(a.RemarkTemplate)
	// Collapse separators left by empty variables.
	collapse := strings.NewReplacer(" ·  · ", " · ", " |  | ", " | ", " -  - ", " - ")
	for prev := ""; prev != out; {
		prev, out = out, collapse.Replace(out)
	}
	out = strings.Trim(strings.TrimSpace(out), "·|-")
	if strings.TrimSpace(out) == "" {
		return base
	}
	return strings.TrimSpace(out)
}

// infoLink is a placeholder entry that only carries a label.
func infoLink(label string) string {
	return "vless://00000000-0000-0000-0000-000000000000@127.0.0.1:1?encryption=none&type=tcp#" + url.PathEscape(label)
}

// SS2022KeySize is the PSK length a Shadowsocks 2022 method needs (0 if the
// method is not a 2022 method).
func SS2022KeySize(method string) int {
	switch method {
	case "2022-blake3-aes-128-gcm":
		return 16
	case "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		return 32
	}
	return 0
}

// SS2022UserKey derives a client's Shadowsocks 2022 PSK from its secret, so
// the HUB never stores a second credential and the key is stable across edits.
func SS2022UserKey(secret, method string) string {
	n := SS2022KeySize(method)
	if n == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte("dark-ss2022|" + secret))
	if n == 32 {
		sum2 := sha256.Sum256(append(sum[:], []byte(secret)...))
		return base64.StdEncoding.EncodeToString(append(sum[:16], sum2[:16]...))
	}
	return base64.StdEncoding.EncodeToString(sum[:n])
}

// SSClassic reports a classic (pre-2022) AEAD method: each client has its
// own password (its secret) and there is no server key.
func SSClassic(method string) bool {
	switch method {
	case "aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "chacha20-ietf-poly1305", "xchacha20-poly1305", "xchacha20-ietf-poly1305":
		return true
	}
	return false
}

// SSKnown is any Shadowsocks method the panel can serve.
func SSKnown(method string) bool { return SS2022KeySize(method) > 0 || SSClassic(method) }

// ssPassword is the client-side password of a multi-user inbound.
func ssPassword(t template, secret string) string {
	if SSClassic(t.Settings.Method) {
		return secret
	}
	return t.Settings.Password + ":" + SS2022UserKey(secret, t.Settings.Method)
}

// Links returns one share link per inbound endpoint, in a stable order.
func Links(a Account, ins []Inbound) ([]string, error) {
	sorted := append([]Inbound(nil), ins...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Tag < sorted[j].Tag })
	var out []string
	if len(ins) > 0 {
		for _, l := range a.InfoLines {
			out = append(out, infoLink(l))
		}
	}
	for _, in := range sorted {
		base, err := parse(in.Template)
		if err != nil {
			return nil, fmt.Errorf("inbound %s: %w", in.Tag, err)
		}
		for _, ep := range in.Meta.Endpoints {
			if ep.excluded("links") {
				continue
			}
			t, meta := forEndpoint(base, in.Meta, ep)
			in := in
			in.Meta = meta
			host := net.JoinHostPort(ep.Address, strconv.Itoa(ep.Port))
			frag := url.PathEscape(remark(a, in, ep))
			switch t.Protocol {
			case "vless":
				q := params(t, in.Meta)
				q.Set("encryption", or(in.Meta.VLESSEncryption, "none"))
				if t.Settings.Flow != "" && t.Stream.Network == "tcp" {
					q.Set("flow", t.Settings.Flow)
				}
				out = append(out, "vless://"+a.UUID+"@"+host+"?"+q.Encode()+"#"+frag)
			case "shadowsocks":
				if !SSKnown(t.Settings.Method) {
					continue
				}
				// SIP022: the 2022 userinfo is percent-encoded, not base64;
				// classic methods use SIP002's base64 userinfo.
				ui := url.QueryEscape(t.Settings.Method) + ":" + url.QueryEscape(ssPassword(t, a.Secret))
				if SSClassic(t.Settings.Method) {
					ui = base64.RawURLEncoding.EncodeToString([]byte(t.Settings.Method + ":" + ssPassword(t, a.Secret)))
				}
				out = append(out, "ss://"+ui+"@"+host+"#"+frag)
			case "hysteria":
				q := url.Values{}
				q.Set("sni", t.Stream.TLS.ServerName)
				q.Set("alpn", "h3")
				if t.Stream.insecure {
					q.Set("insecure", "1")
				}
				if in.Meta.PinSHA256 != "" {
					q.Set("pinSHA256", in.Meta.PinSHA256)
				}
				out = append(out, "hysteria2://"+url.PathEscape(a.Secret)+"@"+host+"/?"+q.Encode()+"#"+frag)
			case "tuic":
				q := url.Values{}
				q.Set("sni", t.Stream.TLS.ServerName)
				q.Set("alpn", "h3")
				q.Set("congestion_control", or(t.Settings.CongestionControl, "bbr"))
				q.Set("udp_relay_mode", "native")
				if t.Stream.insecure {
					q.Set("allow_insecure", "1")
					q.Set("insecure", "1")
				}
				out = append(out, "tuic://"+a.UUID+":"+url.PathEscape(a.Secret)+"@"+host+"?"+q.Encode()+"#"+frag)
			case "wireguard":
				if in.Meta.WGPrivateKey == "" {
					continue
				}
				q := url.Values{}
				q.Set("publickey", in.Meta.PublicKey)
				q.Set("address", in.Meta.WGAddress)
				q.Set("mtu", strconv.Itoa(or0(in.Meta.MTU, 1420)))
				out = append(out, "wireguard://"+url.PathEscape(in.Meta.WGPrivateKey)+"@"+host+"/?"+q.Encode()+"#"+frag)
			case "trojan":
				out = append(out, "trojan://"+url.PathEscape(a.Secret)+"@"+host+"?"+params(t, in.Meta).Encode()+"#"+frag)
			case "vmess":
				s := t.Stream
				v := map[string]string{"v": "2", "ps": remark(a, in, ep), "add": ep.Address, "port": strconv.Itoa(ep.Port),
					"id": a.UUID, "aid": "0", "scy": "auto", "net": s.Network, "type": "none", "tls": ""}
				if s.Security == "tls" {
					v["tls"], v["sni"] = "tls", s.TLS.ServerName
				}
				switch s.Network {
				case "ws":
					v["path"], v["host"] = s.WS.Path, s.WS.Host
				case "httpupgrade":
					v["path"], v["host"] = s.HTTPUpgrade.Path, s.HTTPUpgrade.Host
				case "grpc":
					v["path"] = s.GRPC.ServiceName
				case "kcp":
					v["type"], v["path"] = or(s.KCP.Header.Type, "none"), s.KCP.Seed
				case "tcp":
					if p, h, ok := s.tcpHTTP(); ok {
						v["type"], v["path"], v["host"] = "http", p, h
					}
				}
				b, _ := json.Marshal(v)
				out = append(out, "vmess://"+base64.StdEncoding.EncodeToString(b))
			}
		}
	}
	return out, nil
}

// Base64 is the plain subscription body most clients accept.
func Base64(links []string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Join(links, "\n")))
}

// Userinfo is the Subscription-Userinfo header clients use to show volume
// and expiry: upload, download, total (0 = unlimited) and expire (unix).
func Userinfo(up, down, total, expire int64) string {
	if total < 0 {
		total = 0
	}
	return fmt.Sprintf("upload=%d; download=%d; total=%d; expire=%d", up, down, total, expire)
}

func or0(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}
