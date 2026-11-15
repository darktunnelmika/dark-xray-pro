package hubcore

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"github.com/darktunnelmika/dark-xray-pro/internal/v2/sub"
)

// StreamSpec builds an Xray inbound from three independent choices —
// protocol, transport and security — instead of a fixed list of presets.
// Only combinations Xray accepts are allowed (see checkStream).
type StreamSpec struct {
	Protocol    string   `json:"protocol"`     // vless | vmess | trojan
	Network     string   `json:"network"`      // tcp | ws | grpc | httpupgrade | xhttp
	Security    string   `json:"security"`     // none | tls | reality
	Vision      bool     `json:"vision"`       // xtls-rprx-vision flow (VLESS + TCP + TLS/REALITY)
	Path        string   `json:"path"`         // ws/httpupgrade/xhttp path, or the gRPC service name
	Host        string   `json:"host"`         // ws/httpupgrade/xhttp Host header (CDN)
	GRPCMulti   bool     `json:"grpc_multi"`   // gRPC multiMode
	XHTTPMode   string   `json:"xhttp_mode"`   // auto | packet-up | stream-up | stream-one
	Fingerprint string   `json:"fingerprint"`  // uTLS fingerprint handed to clients
	ALPN        string   `json:"alpn"`         // TLS: e.g. "h2,http/1.1"
	ServerNames []string `json:"server_names"` // REALITY: extra names besides the SNI
	Dest        string   `json:"dest"`         // REALITY: target, default <sni>:443
}

var (
	streamProtocols  = []string{"vless", "vmess", "trojan"}
	streamNetworks   = []string{"tcp", "ws", "grpc", "httpupgrade", "xhttp"}
	streamSecurities = []string{"none", "tls", "reality"}
	xhttpModes       = []string{"", "auto", "packet-up", "stream-up", "stream-one"}
	fingerprints     = []string{"", "chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"}
)

// checkStream reports why a combination cannot work, or nil.
func checkStream(s StreamSpec) error {
	bad := func(f string, a ...any) error { return fmt.Errorf("%w: "+f, append([]any{ErrInvalid}, a...)...) }
	switch {
	case !slices.Contains(streamProtocols, s.Protocol):
		return bad("protocol must be vless, vmess or trojan")
	case !slices.Contains(streamNetworks, s.Network):
		return bad("transport must be tcp, ws, grpc, httpupgrade or xhttp")
	case !slices.Contains(streamSecurities, s.Security):
		return bad("security must be none, tls or reality")
	case s.Security == "reality" && s.Protocol == "vmess":
		return bad("REALITY works with VLESS and Trojan, not VMess")
	case s.Security == "reality" && !slices.Contains([]string{"tcp", "grpc", "xhttp"}, s.Network):
		return bad("REALITY works only over tcp, grpc or xhttp")
	case s.Vision && (s.Protocol != "vless" || s.Network != "tcp" || s.Security == "none"):
		return bad("Vision needs VLESS over tcp with TLS or REALITY")
	case !slices.Contains(xhttpModes, s.XHTTPMode):
		return bad("unknown xhttp mode %q", s.XHTTPMode)
	case !slices.Contains(fingerprints, s.Fingerprint):
		return bad("unknown fingerprint %q", s.Fingerprint)
	case strings.ContainsAny(s.Path+s.Host, " \\t\"'<>"):
		return bad("path and host cannot contain spaces or quotes")
	}
	return nil
}

// buildStream fills t (the inbound template) and the client metadata.
func buildStream(p PresetRequest, t map[string]any, m *sub.Meta) error {
	s := *p.Stream
	if err := checkStream(s); err != nil {
		return err
	}
	t["protocol"] = s.Protocol
	if s.Vision {
		t["settings"] = map[string]any{"flow": "xtls-rprx-vision"}
	}
	path := strings.TrimSpace(s.Path)
	if path == "" {
		path = randomHex(6)
		if s.Network != "grpc" {
			path = "/" + path
		}
	} else if s.Network != "grpc" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	st := map[string]any{"network": s.Network, "security": s.Security}
	switch s.Network {
	case "ws":
		ws := map[string]any{"path": path}
		if s.Host != "" {
			ws["host"] = s.Host
		}
		st["wsSettings"] = ws
	case "httpupgrade":
		st["httpupgradeSettings"] = map[string]any{"path": path, "host": s.Host}
	case "grpc":
		st["grpcSettings"] = map[string]any{"serviceName": strings.TrimPrefix(path, "/"), "multiMode": s.GRPCMulti}
	case "xhttp":
		x := map[string]any{"path": path, "mode": s.XHTTPMode}
		if s.XHTTPMode == "" {
			x["mode"] = "auto"
		}
		if s.Host != "" {
			x["host"] = s.Host
		}
		st["xhttpSettings"] = x
	}
	if s.Fingerprint != "" {
		m.Fingerprint = s.Fingerprint
	}
	switch s.Security {
	case "reality":
		if p.SNI == "" {
			return fmt.Errorf("%w: REALITY needs a target name (sni)", ErrInvalid)
		}
		priv, pub, err := RealityKeys()
		if err != nil {
			return err
		}
		sidb := make([]byte, 4)
		if _, err := rand.Read(sidb); err != nil {
			return err
		}
		sid := hex.EncodeToString(sidb)
		names := []string{p.SNI}
		for _, n := range s.ServerNames {
			if n = strings.TrimSpace(n); n != "" && !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
		dest := strings.TrimSpace(s.Dest)
		if dest == "" {
			dest = p.SNI + ":443"
		}
		m.PublicKey, m.ShortID = pub, sid
		st["realitySettings"] = map[string]any{"dest": dest, "serverNames": names, "privateKey": priv, "shortIds": []string{sid}}
	case "tls":
		if p.Cert == "" || p.SNI == "" {
			return fmt.Errorf("%w: TLS needs a certificate and its domain (sni)", ErrInvalid)
		}
		alpn := []string{"h2", "http/1.1"}
		if s.Network == "ws" || s.Network == "httpupgrade" {
			alpn = []string{"http/1.1"}
		}
		if a := strings.TrimSpace(s.ALPN); a != "" {
			alpn = strings.Split(strings.ReplaceAll(a, " ", ""), ",")
		}
		st["tlsSettings"] = map[string]any{"serverName": p.SNI, "alpn": alpn, "certificates": []any{map[string]any{"dark_cert": p.Cert}}}
		m.Insecure, m.PinSHA256 = p.selfSigned, p.selfPin
	}
	t["streamSettings"] = st
	return nil
}
