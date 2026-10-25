package inboundspec

import (
	"fmt"
	"strconv"
	"strings"
)

var sockoptFields = map[string]rule{
	"mark": integer(0, 2147483647), "tcpFastOpen": oneof(flag(), integer(0, 2147483647)), "tproxy": enum("off", "redirect", "tproxy"), "acceptProxyProtocol": flag(),
	"domainStrategy":       enum("AsIs", "UseIP", "UseIPv6v4", "UseIPv6", "UseIPv4v6", "UseIPv4", "ForceIP", "ForceIPv6v4", "ForceIPv6", "ForceIPv4v6", "ForceIPv4"),
	"tcpKeepAliveInterval": integer(0, 2147483647), "tcpKeepAliveIdle": integer(0, 2147483647), "tcpCongestion": enum("", "bbr", "cubic", "reno"), "tcpcongestion": enum("", "bbr", "cubic", "reno"),
	"tcpWindowClamp": integer(0, 2147483647), "tcpMaxSeg": integer(0, 65535), "penetrate": flag(), "tcpUserTimeout": integer(0, 2147483647), "v6only": flag(), "V6Only": flag(), "interface": shortText(), "tcpMptcp": flag(), "dialerProxy": shortText(),
	"trustedXForwardedFor": stringList(), "addressPortStrategy": enum("none", "SrvPortOnly", "SrvAddressOnly", "SrvPortAndAddress", "TxtPortOnly", "TxtAddressOnly", "TxtPortAndAddress"),
	"happyEyeballs": object(map[string]rule{"prioritizeIPv6": flag(), "tryDelayMs": integer(0, 2147483647), "interleave": integer(1, 65535), "maxConcurrentTry": integer(0, 65535)}),
	"customSockopt": array(object(map[string]rule{"system": enum("linux", "windows", "darwin"), "network": enum("", "tcp", "udp"), "type": enum("int", "str"), "level": shortText(), "opt": shortText(), "value": oneof(shortText(), integer(-2147483648, 2147483647))}), 128),
}
var requestFields = map[string]rule{"version": enum("1.0", "1.1"), "method": shortText(), "path": stringList(), "headers": dictionary(stringList())}
var responseFields = map[string]rule{"version": enum("1.0", "1.1"), "status": shortText(), "reason": shortText(), "headers": dictionary(stringList())}
var xmuxFields = map[string]rule{"maxConcurrency": boundedRange(), "maxConnections": boundedRange(), "cMaxReuseTimes": boundedRange(), "hMaxRequestTimes": boundedRange(), "hMaxReusableSecs": boundedRange(), "hKeepAlivePeriod": integer(0, 2147483647)}
var xhttpFields = map[string]rule{
	"host": shortText(), "path": shortText(), "mode": enum("auto", "packet-up", "stream-up", "stream-one"), "headers": dictionary(shortText()),
	"xPaddingBytes": boundedRange(), "xPaddingObfsMode": flag(), "xPaddingKey": shortText(), "xPaddingHeader": shortText(), "xPaddingPlacement": enum("", "queryInHeader", "header", "cookie", "query"), "xPaddingMethod": enum("", "repeat-x", "tokenish"),
	"uplinkHTTPMethod": enum("", "POST", "PUT", "GET"), "sessionIDPlacement": enum("", "path", "header", "cookie", "query"), "sessionIDKey": shortText(), "sessionIDTable": shortText(), "sessionIDLength": boundedRange(),
	"seqPlacement": enum("", "path", "header", "cookie", "query"), "seqKey": shortText(), "uplinkDataPlacement": enum("", "auto", "body", "header", "cookie"), "uplinkDataKey": shortText(), "uplinkChunkSize": boundedRange(),
	"noGRPCHeader": flag(), "noSSEHeader": flag(), "scMaxEachPostBytes": boundedRange(), "scMinPostsIntervalMs": boundedRange(), "scMaxBufferedPosts": integer(0, 2147483647), "scStreamUpServerSecs": boundedRange(), "serverMaxHeaderBytes": integer(0, 2147483647), "xmux": object(xmuxFields),
}
var storageOp = object(map[string]rule{"method": enum("GET", "PUT", "POST", "PATCH", "DELETE", "HEAD"), "url": textRule(), "headers": dictionary(shortText()), "body": textRule(), "namesRegex": textRule()})
var storageTemplate = object(map[string]rule{
	"flatten": flag(), "concurrency": integer(0, 64), "auth": object(map[string]rule{"type": enum("", "none", "basic", "static", "oauth2"), "header": dictionary(shortText()), "username": shortText(), "password": textRule(), "tokenUrl": textRule(), "form": dictionary(textRule()), "tokenPath": shortText(), "expiryPath": shortText()}),
	"put": storageOp, "get": storageOp, "delete": storageOp, "list": storageOp, "retry": object(map[string]rule{"status": array(integer(100, 599), 64), "rateReason": shortText()}),
})
var transportSchemas = map[string]map[string]rule{
	"tcp":         {"acceptProxyProtocol": flag(), "header": object(map[string]rule{"type": enum("none", "http"), "request": object(requestFields), "response": object(responseFields)})},
	"kcp":         {"mtu": integer(576, 1460), "tti": integer(10, 100), "uplinkCapacity": integer(0, 4294967295), "downlinkCapacity": integer(0, 4294967295), "cwndMultiplier": integer(1, 4294967295), "maxSendingWindow": integer(576, 4294967295)},
	"ws":          {"acceptProxyProtocol": flag(), "path": shortText(), "host": shortText(), "headers": dictionary(shortText()), "heartbeatPeriod": integer(0, 4294967295)},
	"grpc":        {"serviceName": shortText(), "authority": shortText(), "multiMode": flag(), "idle_timeout": integer(0, 2147483647), "health_check_timeout": integer(0, 2147483647), "permit_without_stream": flag(), "initial_windows_size": integer(0, 2147483647), "user_agent": shortText()},
	"httpupgrade": {"acceptProxyProtocol": flag(), "path": shortText(), "host": shortText(), "headers": dictionary(shortText())},
	"xhttp":       xhttpFields,
	"xdrive":      {"service": enum("Google Drive", "local", "template"), "remoteFolder": textRule(), "secrets": array(textRule(), 32), "segmentBytes": integer(0, 16<<20), "flushIntervalMs": integer(0, 4294967295), "pollIntervalMs": integer(0, 4294967295), "maxPollIntervalMs": integer(0, 4294967295), "sessionTtlSeconds": integer(0, 4294967295), "concurrency": integer(0, 64), "eagerWindowMs": integer(0, 4294967295), "holeTimeoutMs": integer(0, 4294967295), "template": storageTemplate},
	"hysteria":    {"version": integer(2, 2), "udpIdleTimeout": integer(2, 600), "masquerade": object(map[string]rule{"type": enum("", "proxy", "file", "string"), "dir": shortText(), "url": shortText(), "rewriteHost": flag(), "xForwarded": flag(), "insecure": flag(), "content": textRule(), "headers": dictionary(shortText()), "statusCode": integer(0, 599)})},
	"masque":      {"path": shortText()},
}

func normalizeSockopt(m map[string]any, path string) error {
	if err := checkObject(m, sockoptFields, path); err != nil {
		return err
	}
	for a, b := range map[string]string{"tcpcongestion": "tcpCongestion", "V6Only": "v6only"} {
		if v, ok := m[a]; ok {
			if _, dup := m[b]; dup {
				return invalid(path+"."+b, "duplicate_alias")
			}
			m[b] = v
			delete(m, a)
		}
	}
	for _, v := range list(m["customSockopt"]) {
		x := v.(map[string]any)
		for _, k := range []string{"level", "opt"} {
			if str(x, k) == "" {
				return invalid(path+".customSockopt."+k, "required")
			}
		}
		if n, ok := number(x["value"]); ok {
			x["value"] = strconv.FormatInt(n, 10)
		}
	}
	return nil
}

func validateHeaders(m map[string]any, path string) error {
	for k, v := range m {
		if strings.ContainsAny(k, "\r\n: ") || k == "" {
			return invalid(path, "header_name_invalid")
		}
		switch x := v.(type) {
		case string:
			if strings.ContainsAny(x, "\r\n") {
				return invalid(path, "header_value_invalid")
			}
		case []any:
			for _, z := range x {
				if strings.ContainsAny(z.(string), "\r\n") {
					return invalid(path, "header_value_invalid")
				}
			}
		}
	}
	return nil
}

func validateStream(s *Spec) error {
	fields := map[string]rule{"sockopt": object(sockoptFields), "finalmask": object(map[string]rule{"tcp": array(object(map[string]rule{"type": shortText(), "settings": object(nil)}), 64), "udp": array(object(map[string]rule{"type": shortText(), "settings": object(nil)}), 64), "quicParams": object(quicFields)})}
	// finalmask.settings has a per-mask schema, validated below instead.
	delete(fields, "finalmask")
	if schema, ok := transportSchemas[s.Network]; ok {
		fields[s.Network+"Settings"] = object(schema)
	}
	if s.Network == "xdrive" {
		fields["address"] = shortText()
		fields["port"] = integer(1, 65535)
	}
	copyForCheck := cloneMap(s.Stream)
	delete(copyForCheck, "finalmask")
	if err := checkObject(copyForCheck, fields, "stream"); err != nil {
		return err
	}
	if has([]string{"mtproto", "amneziawg", "tuic", "tun", "http", "mixed"}, s.Protocol) {
		if len(s.Stream) > 0 {
			return invalid("stream", "protocol_has_no_stream_settings")
		}
		return nil
	}
	if sock := obj(s.Stream, "sockopt"); sock != nil {
		if err := normalizeSockopt(sock, "stream.sockopt"); err != nil {
			return err
		}
		if len(texts(sock["trustedXForwardedFor"])) > 0 && !has([]string{"ws", "httpupgrade", "xhttp", "grpc"}, s.Network) {
			return invalid("stream.sockopt.trustedXForwardedFor", "transport_not_supported")
		}
		if s.Network == "kcp" && boolean(sock, "acceptProxyProtocol") {
			return invalid("stream.sockopt.acceptProxyProtocol", "proxy_protocol_requires_tcp")
		}
	}
	if sock := obj(s.TLS, "echSockopt"); sock != nil {
		if err := normalizeSockopt(sock, "tls.echSockopt"); err != nil {
			return err
		}
	}
	if fm, exists := s.Stream["finalmask"]; exists {
		if s.Protocol == "tunnel" {
			return invalid("stream.finalmask", "tunnel_masks_not_supported")
		}
		m, ok := fm.(map[string]any)
		if !ok {
			return invalid("stream.finalmask", "object_required")
		}
		if err := validateFinalMask(m, s.Network, s.Protocol); err != nil {
			return err
		}
	}
	key := s.Network + "Settings"
	t := obj(s.Stream, key)
	if _, ok := transportSchemas[s.Network]; ok && t == nil {
		t = map[string]any{}
		s.Stream[key] = t
	}
	switch s.Network {
	case "tcp":
		if h := obj(t, "header"); h != nil {
			putDefault(h, "type", "none")
			if str(h, "type") == "none" && (len(obj(h, "request")) > 0 || len(obj(h, "response")) > 0) {
				return invalid("stream.tcpSettings.header", "http_header_type_required")
			}
			for _, k := range []string{"request", "response"} {
				if err := validateHeaders(obj(obj(h, k), "headers"), "stream.tcpSettings.header."+k+".headers"); err != nil {
					return err
				}
			}
		}
	case "ws", "httpupgrade", "xhttp":
		putDefault(t, "path", "/")
		if p := str(t, "path"); !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\r\n") {
			return invalid("stream."+key+".path", "absolute_http_path_required")
		}
		if err := validateHeaders(obj(t, "headers"), "stream."+key+".headers"); err != nil {
			return err
		}
	case "kcp":
		putDefault(t, "mtu", 1350)
		putDefault(t, "tti", 20)
		putDefault(t, "cwndMultiplier", 1)
		if n := num(t, "maxSendingWindow"); n > 0 && n < num(t, "mtu") {
			return invalid("stream.kcpSettings.maxSendingWindow", "window_smaller_than_mtu")
		}
	case "xdrive":
		putDefault(t, "service", "Google Drive")
		if str(t, "remoteFolder") == "" {
			return invalid("stream.xdriveSettings.remoteFolder", "folder_required")
		}
		if str(t, "service") == "Google Drive" && len(texts(t["secrets"])) != 3 {
			return invalid("stream.xdriveSettings.secrets", "three_credentials_required")
		}
		if str(t, "service") == "template" && obj(t, "template") == nil {
			return invalid("stream.xdriveSettings.template", "template_required")
		}
	case "hysteria":
		putDefault(t, "version", 2)
		putDefault(t, "udpIdleTimeout", 60)
		if m := obj(t, "masquerade"); m != nil {
			switch str(m, "type") {
			case "proxy":
				if !strings.HasPrefix(str(m, "url"), "https://") && !strings.HasPrefix(str(m, "url"), "http://") {
					return invalid("stream.hysteriaSettings.masquerade.url", "http_url_required")
				}
			case "file":
				if !validFileRef(str(m, "dir")) {
					return invalid("stream.hysteriaSettings.masquerade.dir", "absolute_directory_required")
				}
			}
			if err := validateHeaders(obj(m, "headers"), "stream.hysteriaSettings.masquerade.headers"); err != nil {
				return err
			}
		}
	case "masque":
		putDefault(t, "path", "/.well-known/masque/ip/*/*/")
	}
	if s.Network == "xhttp" {
		putDefault(t, "mode", "auto")
		if str(t, "uplinkHTTPMethod") == "GET" && str(t, "mode") != "packet-up" {
			return invalid("stream.xhttpSettings.uplinkHTTPMethod", "get_requires_packet_up")
		}
		if x := obj(t, "xmux"); x != nil {
			_, a, _ := parseRange(x["maxConcurrency"], 0, 2147483647)
			_, b, _ := parseRange(x["maxConnections"], 0, 2147483647)
			if a > 0 && b > 0 {
				return invalid("stream.xhttpSettings.xmux", "exclusive_pool_strategies")
			}
		}
		if st := str(t, "sessionIDTable"); st != "" {
			if !has([]string{"ALPHABET", "Alphabet", "BASE36", "Base62", "HEX", "alphabet", "base36", "hex", "number"}, st) {
				if len(st) < 2 || len(st) > 128 {
					return invalid("stream.xhttpSettings.sessionIDTable", "session_table_invalid")
				}
				for _, r := range st {
					if r < 33 || r > 126 {
						return invalid("stream.xhttpSettings.sessionIDTable", "ascii_table_required")
					}
				}
			}
		}
	}
	return nil
}

func engineStream(s Spec) map[string]any {
	out := cloneMap(s.Stream)
	out["network"] = s.Network
	out["security"] = s.Security
	if s.Security == "tls" {
		out["tlsSettings"] = cloneMap(s.TLS)
		stripCertificateIDs(obj(out, "tlsSettings"))
	}
	if s.Security == "reality" {
		r := cloneMap(s.Reality)
		if v, ok := r["maxTimediff"]; ok {
			r["maxTimeDiff"] = v
			delete(r, "maxTimediff")
		}
		out["realitySettings"] = r
	}
	if s.Network == "xhttp" {
		x := obj(out, "xhttpSettings")
		for _, k := range []string{"xmux", "scMinPostsIntervalMs", "uplinkChunkSize", "noGRPCHeader"} {
			delete(x, k)
		}
	}
	if fm := obj(out, "finalmask"); fm != nil {
		stripMaskIDs(fm)
		if q := obj(fm, "quicParams"); q != nil {
			delete(q, "udpHop")
		}
	}
	return out
}

func formatPath(base string, index int, field string) string {
	return fmt.Sprintf("%s.%d.%s", base, index, field)
}
