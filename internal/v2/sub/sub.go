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
	Exclude       []string `json:"exclude,omitempty"` // formats that skip this host: links | singbox | clash
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
	t.Stream.insecure = ep.AllowInsecure
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
}

type template struct {
	Protocol string `json:"protocol"`
	Settings struct {
		Flow     string `json:"flow"`
		Method   string `json:"method"`   // shadowsocks
		Password string `json:"password"` // shadowsocks 2022 server key
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
	case "grpc":
		q.Set("serviceName", s.GRPC.ServiceName)
		q.Set("mode", "gun")
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

func remark(in Inbound, ep Endpoint) string {
	r := or(in.Meta.Remark, in.Tag)
	if ep.Name != "" {
		r += " | " + ep.Name
	}
	return r
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

// ssPassword is the client-side password of a 2022 multi-user inbound.
func ssPassword(t template, secret string) string {
	return t.Settings.Password + ":" + SS2022UserKey(secret, t.Settings.Method)
}

// Links returns one share link per inbound endpoint, in a stable order.
func Links(a Account, ins []Inbound) ([]string, error) {
	sorted := append([]Inbound(nil), ins...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Tag < sorted[j].Tag })
	var out []string
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
			frag := url.PathEscape(remark(in, ep))
			switch t.Protocol {
			case "vless":
				q := params(t, in.Meta)
				q.Set("encryption", "none")
				if t.Settings.Flow != "" && t.Stream.Network == "tcp" {
					q.Set("flow", t.Settings.Flow)
				}
				out = append(out, "vless://"+a.UUID+"@"+host+"?"+q.Encode()+"#"+frag)
			case "shadowsocks":
				if SS2022KeySize(t.Settings.Method) == 0 {
					continue
				}
				// SIP022: the 2022 userinfo is percent-encoded, not base64.
				ui := url.QueryEscape(t.Settings.Method) + ":" + url.QueryEscape(ssPassword(t, a.Secret))
				out = append(out, "ss://"+ui+"@"+host+"#"+frag)
			case "trojan":
				out = append(out, "trojan://"+url.PathEscape(a.Secret)+"@"+host+"?"+params(t, in.Meta).Encode()+"#"+frag)
			case "vmess":
				s := t.Stream
				v := map[string]string{"v": "2", "ps": remark(in, ep), "add": ep.Address, "port": strconv.Itoa(ep.Port),
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
