import type { Inbound } from "./inboundTypes";

export type SpecValue = string | number | boolean | null | SpecObject | SpecValue[];
export type SpecObject = { [key: string]: SpecValue };
export type AuthoringSpec = {
  version: 1; protocol: string; network: string; security: "none" | "tls" | "reality";
  settings: SpecObject; stream: SpecObject; tls: SpecObject; reality: SpecObject; client: SpecObject;
};
export type SpecField = {
  path: string; label: string; fa?: string;
  kind?: "text" | "number" | "numbers" | "boolean" | "select" | "lines" | "pem" | "pemText" | "packet" | "pairs" | "rows" | "json";
  options?: string[]; fields?: SpecField[]; required?: boolean; secret?: boolean; wide?: boolean;
  min?: number; max?: number; placeholder?: string; hint?: string; faHint?: string;
  disabled?: boolean;
  initial?: SpecValue; generate?: "password" | "hex";
};
export type SpecGroup = { title: string; fa: string; fields: SpecField[]; advanced?: boolean };
export const inboundProtocols = ["vmess", "vless", "trojan", "shadowsocks", "wireguard", "hysteria", "http", "mixed", "tunnel", "tun", "mtproto", "amneziawg", "tuic", "masque"] as const;
export const inboundTransports = ["tcp", "kcp", "ws", "grpc", "httpupgrade", "xhttp", "xdrive"] as const;
export const protocolNames: Record<string, string> = { vmess: "VMess", vless: "VLESS", trojan: "Trojan", shadowsocks: "Shadowsocks", wireguard: "WireGuard", hysteria: "Hysteria 2", http: "HTTP", mixed: "Mixed (SOCKS / HTTP)", tunnel: "Dokodemo-door / Tunnel", tun: "TUN", mtproto: "MTProto", amneziawg: "AmneziaWG", tuic: "TUIC", masque: "MASQUE" };
export const networkNames: Record<string, string> = { tcp: "TCP (RAW)", kcp: "mKCP", ws: "WebSocket", grpc: "gRPC", httpupgrade: "HTTPUpgrade", xhttp: "XHTTP", xdrive: "XDrive", hysteria: "Hysteria", masque: "MASQUE", tuic: "TUIC", wireguard: "WireGuard", amneziawg: "AmneziaWG", tun: "TUN", mtproto: "MTProto" };
export const generalTransportProtocols = ["vmess", "vless", "trojan", "shadowsocks"];
export const specFingerprints = ["chrome", "firefox", "safari", "ios", "android", "edge", "360", "qq", "random", "randomized"];
const f = (path: string, label: string, extra: Partial<SpecField> = {}): SpecField => ({ path, label, ...extra });
const num = (path: string, label: string, extra: Partial<SpecField> = {}) => f(path, label, { kind: "number", min: 0, ...extra });
const bool = (path: string, label: string) => f(path, label, { kind: "boolean" });
const list = (path: string, label: string, extra: Partial<SpecField> = {}) => f(path, label, { kind: "lines", ...extra });
const select = (path: string, label: string, options: string[], extra: Partial<SpecField> = {}) => f(path, label, { kind: "select", options, ...extra });
const secret = (path: string, label: string, extra: Partial<SpecField> = {}) => f(path, label, { secret: true, ...extra });
const fallbackFields = [f("name", "SNI"), f("alpn", "ALPN"), f("path", "Path"), f("dest", "Destination", { required: true, placeholder: "127.0.0.1:8080" }), select("xver", "PROXY protocol", ["0", "1", "2"], { kind: "number", max: 2 })];
const fallback = f("settings.fallbacks", "Fallbacks", { fa: "مقصدهای Fallback", kind: "rows", fields: fallbackFields, wide: true });
export const protocolFields: Record<string, SpecField[]> = {
  vmess: [],
  vless: [secret("settings.decryption", "Decryption", { initial: "none", placeholder: "none" }), secret("settings.testseed", "Test seed (4 integers)", { kind: "numbers" }), fallback],
  trojan: [fallback],
  shadowsocks: [select("settings.method", "Cipher", ["2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305", "aes-128-gcm", "aes-256-gcm", "chacha20-poly1305", "xchacha20-poly1305", "none"], { required: true }), secret("settings.password", "Server password", { fa: "رمز سرور", generate: "password" }), select("settings.network", "Allowed traffic", ["tcp,udp", "tcp", "udp"]), f("settings.ivCheck", "IV check", { kind: "boolean", disabled: true, hint: "This option is not implemented by the selected core.", faHint: "این گزینه در هستهٔ فعلی پیاده‌سازی نشده است." })],
  wireguard: [secret("settings.secretKey", "Private key", { fa: "کلید خصوصی" }), num("settings.mtu", "MTU", { min: 576, max: 65535 }), list("settings.dns", "DNS servers"), bool("settings.noKernelTun", "Userspace TUN"), f("settings.subnetIp", "Subnet IP", { placeholder: "10.66.0.0" }), num("settings.subnetCidr", "Subnet prefix", { min: 1, max: 30 }), f("settings.reserved", "Reserved bytes (3)", { kind: "numbers" })],
  hysteria: [num("settings.version", "Hysteria version", { min: 2, max: 2, initial: 2 })],
  http: [bool("settings.allowTransparent", "Transparent HTTP")],
  mixed: [select("settings.auth", "Authentication", ["password", "noauth"]), bool("settings.udp", "UDP"), f("settings.ip", "UDP bind IP", { placeholder: "127.0.0.1" })],
  tunnel: [f("settings.rewriteAddress", "Destination address", { fa: "آدرس مقصد" }), num("settings.rewritePort", "Destination port", { min: 1, max: 65535 }), select("settings.allowedNetwork", "Allowed traffic", ["tcp,udp", "tcp", "udp"]), bool("settings.followRedirect", "Follow redirect"), f("settings.portMap", "Port mappings", { kind: "pairs", wide: true, hint: "One source-port = destination-host:port mapping per line.", faHint: "در هر خط پورت مبدأ = هاست:پورت مقصد وارد کن." })],
  tun: [f("settings.name", "Interface name", { placeholder: "xray0" }), f("settings.desc", "Description"), num("settings.mtu", "MTU", { max: 65535 }), list("settings.gateway", "Gateway CIDRs"), list("settings.dns", "DNS servers"), num("settings.userLevel", "User level"), list("settings.autoSystemRoutingTable", "System route CIDRs"), f("settings.autoOutboundsInterface", "Outbound interface"), bool("settings.autoSystemDnsToGateway", "System DNS to gateway"), list("settings.autoSystemWfpBlockLeak", "WFP leak protection", { placeholder: "dns\nmisconfigtun" })],
  mtproto: [f("settings.fakeTlsDomain", "Fake TLS domain", { placeholder: "www.cloudflare.com" }), secret("settings.secret", "Secret"), bool("settings.proxyProtocolListener", "PROXY protocol listener"), select("settings.preferIp", "Preferred IP", ["prefer-ipv6", "prefer-ipv4", "only-ipv6", "only-ipv4"]), bool("settings.debug", "Debug"), f("settings.domainFronting.ip", "Domain-fronting IP"), num("settings.domainFronting.port", "Domain-fronting port", { min: 1, max: 65535 }), bool("settings.domainFronting.proxyProtocol", "Domain-fronting PROXY protocol"), num("settings.throttleMaxConnections", "Maximum connections"), num("settings.pendingHandshakesMaxPerIp", "Pending handshakes per IP"), bool("settings.routeThroughXray", "Route through Xray"), f("settings.outboundTag", "Outbound tag"), f("settings.publicIpv4", "Public IPv4"), f("settings.publicIpv6", "Public IPv6")],
  amneziawg: [secret("settings.server.privateKey", "Private key"), f("settings.server.publicKey", "Public key"), f("settings.server.subnetIp", "Subnet IP"), num("settings.server.subnetCidr", "Subnet prefix", { max: 128 }), num("settings.server.mtu", "MTU", { min: 576, max: 9000 }), f("settings.server.primaryDns", "Primary DNS"), f("settings.server.secondaryDns", "Secondary DNS"), f("settings.server.externalInterface", "External interface"), bool("settings.server.ipv6Enabled", "IPv6"), f("settings.server.ipv6Subnet", "IPv6 subnet"), f("settings.server.ipv6ExternalInterface", "IPv6 external interface"), ...["jc", "jmin", "jmax", "s1", "s2", "s3", "s4"].map(key => num(`settings.server.${key}`, key.toUpperCase())), ...["h1", "h2", "h3", "h4"].map(key => f(`settings.server.${key}`, key.toUpperCase())), ...["i1", "i2", "i3", "i4", "i5"].map(key => f(`settings.server.${key}`, key.toUpperCase())), secret("settings.server.headerProtectionKey", "Header protection key"), ...["contentPaddingAddition", "rekeyAfterTime", "rekeyTimeout", "rejectAfterTime", "keepaliveTimeout", "maxHandshakeAttempts"].map(key => f(`settings.server.${key}`, key.replace(/[A-Z]/g, letter => ` ${letter.toLowerCase()}`))), bool("settings.server.randomTrailers", "Random trailers"), bool("settings.server.disableCookies", "Disable cookies")],
  tuic: [secret("settings.server.certificate", "Certificate file path", { placeholder: "/etc/ssl/certs/server.pem" }), secret("settings.server.private_key", "Private key file path", { placeholder: "/etc/ssl/private/server.key" }), select("settings.server.congestion_control", "Congestion control", ["bbr", "cubic", "new_reno"]), list("settings.server.alpn", "ALPN"), select("settings.server.udp_relay_mode", "UDP relay mode", ["native", "quic"]), bool("settings.server.zero_rtt_handshake", "0-RTT handshake"), select("settings.server.log_level", "Log level", ["warn", "info", "error", "debug"]), num("settings.server.max_idle_time", "Maximum idle time (seconds)", { min: 1, max: 86400 }), num("settings.server.authentication_timeout", "Authentication timeout (seconds)", { min: 1, max: 600 }), num("settings.server.max_udp_relay_packet_size", "Maximum UDP packet size"), f("settings.server.sni", "SNI")],
  masque: [list("settings.address", "Tunnel addresses", { placeholder: "10.14.0.1/24\nfd14::1/64" }), num("settings.mtu", "MTU", { min: 1280, max: 65535 })],
};

export const transportGroups: Record<string, SpecGroup[]> = {
  tcp: [{ title: "TCP settings", fa: "تنظیمات TCP", fields: [bool("stream.tcpSettings.acceptProxyProtocol", "Accept PROXY protocol"), select("stream.tcpSettings.header.type", "Header camouflage", ["none", "http"])] }, { title: "HTTP camouflage", fa: "پوشش HTTP", advanced: true, fields: [f("stream.tcpSettings.header.request.version", "HTTP version"), f("stream.tcpSettings.header.request.method", "Method", { placeholder: "GET" }), list("stream.tcpSettings.header.request.path", "Request paths"), f("stream.tcpSettings.header.request.headers", "Request headers", { kind: "pairs" }), f("stream.tcpSettings.header.response.version", "Response version"), f("stream.tcpSettings.header.response.status", "Response status"), f("stream.tcpSettings.header.response.reason", "Response reason"), f("stream.tcpSettings.header.response.headers", "Response headers", { kind: "pairs" })] }],
  kcp: [{ title: "mKCP settings", fa: "تنظیمات mKCP", fields: [num("stream.kcpSettings.mtu", "MTU", { min: 576, max: 1460 }), num("stream.kcpSettings.tti", "TTI (ms)", { min: 10, max: 100 }), num("stream.kcpSettings.uplinkCapacity", "Uplink capacity (MB/s)"), num("stream.kcpSettings.downlinkCapacity", "Downlink capacity (MB/s)"), num("stream.kcpSettings.cwndMultiplier", "Congestion-window multiplier", { min: 1 }), num("stream.kcpSettings.maxSendingWindow", "Maximum sending window", { min: 576 })] }],
  ws: [{ title: "WebSocket settings", fa: "تنظیمات WebSocket", fields: [f("stream.wsSettings.path", "Path", { placeholder: "/" }), f("stream.wsSettings.host", "Host"), f("stream.wsSettings.headers", "Headers", { kind: "pairs", wide: true }), bool("stream.wsSettings.acceptProxyProtocol", "Accept PROXY protocol"), num("stream.wsSettings.heartbeatPeriod", "Heartbeat period (seconds)")] }],
  grpc: [{ title: "gRPC settings", fa: "تنظیمات gRPC", fields: [f("stream.grpcSettings.serviceName", "Service name"), f("stream.grpcSettings.authority", "Authority"), bool("stream.grpcSettings.multiMode", "MultiMode")] }, { title: "gRPC timeouts and buffers", fa: "مهلت‌ها و بافر gRPC", advanced: true, fields: [num("stream.grpcSettings.idle_timeout", "Idle timeout (seconds)"), num("stream.grpcSettings.health_check_timeout", "Health-check timeout (seconds)"), bool("stream.grpcSettings.permit_without_stream", "Permit without stream"), num("stream.grpcSettings.initial_windows_size", "Initial window size"), f("stream.grpcSettings.user_agent", "User agent")] }],
  httpupgrade: [{ title: "HTTPUpgrade settings", fa: "تنظیمات HTTPUpgrade", fields: [f("stream.httpupgradeSettings.path", "Path", { placeholder: "/" }), f("stream.httpupgradeSettings.host", "Host"), f("stream.httpupgradeSettings.headers", "Headers", { kind: "pairs", wide: true }), bool("stream.httpupgradeSettings.acceptProxyProtocol", "Accept PROXY protocol")] }],
  xhttp: [], xdrive: [],
};
function httpGroups(kind: "xhttp" | "xdrive"): SpecGroup[] {
  const prefix = `stream.${kind}Settings`;
  return [{ title: `${networkNames[kind]} settings`, fa: `تنظیمات ${networkNames[kind]}`, fields: [f(`${prefix}.path`, "Path", { placeholder: "/" }), f(`${prefix}.host`, "Host"), select(`${prefix}.mode`, "Mode", ["auto", "packet-up", "stream-up", "stream-one"]), f(`${prefix}.headers`, "Headers", { kind: "pairs" })] },
    { title: "Padding and streaming", fa: "Padding و جریان داده", advanced: true, fields: [f(`${prefix}.xPaddingBytes`, "Padding bytes", { placeholder: "100-1000" }), bool(`${prefix}.noGRPCHeader`, "Omit gRPC header"), bool(`${prefix}.noSSEHeader`, "Omit SSE header"), bool(`${prefix}.xPaddingObfsMode`, "Padding obfuscation mode"), f(`${prefix}.xPaddingKey`, "Padding key"), f(`${prefix}.xPaddingHeader`, "Padding header"), select(`${prefix}.xPaddingPlacement`, "Padding placement", ["queryInHeader", "header", "cookie", "query"]), select(`${prefix}.xPaddingMethod`, "Padding method", ["repeat-x", "tokenish"]), num(`${prefix}.scMaxBufferedPosts`, "Maximum buffered posts"), f(`${prefix}.scMaxEachPostBytes`, "Maximum bytes per post"), f(`${prefix}.scMinPostsIntervalMs`, "Minimum post interval (ms)"), f(`${prefix}.scStreamUpServerSecs`, "Stream-up server duration (seconds)")] },
    { title: "XMUX", fa: "تنظیمات XMUX", advanced: true, fields: ["maxConcurrency", "maxConnections", "cMaxReuseTimes", "hMaxRequestTimes", "hMaxReusableSecs", "hKeepAlivePeriod"].map(key => key === "hKeepAlivePeriod" ? num(`${prefix}.xmux.${key}`, key) : f(`${prefix}.xmux.${key}`, key)) },
    { title: "Download connection", fa: "اتصال دانلود", advanced: true, fields: [f(`${prefix}.downloadSettings.address`, "Address"), num(`${prefix}.downloadSettings.port`, "Port", { min: 1, max: 65535 }), select(`${prefix}.downloadSettings.network`, "Transport", [...inboundTransports]), select(`${prefix}.downloadSettings.security`, "Security", ["none", "tls", "reality"]), f(`${prefix}.downloadSettings.xhttpSettings.path`, "Path"), f(`${prefix}.downloadSettings.xhttpSettings.host`, "Host"), f(`${prefix}.downloadSettings.tlsSettings.serverName`, "SNI")] }];
}
transportGroups.xhttp = httpGroups("xhttp").filter(group => group.title !== "Download connection");
transportGroups.xhttp.push({ title: "Request layout", fa: "ساختار درخواست", advanced: true, fields: [select("stream.xhttpSettings.uplinkHTTPMethod", "Uplink HTTP method", ["POST", "PUT", "GET"]), select("stream.xhttpSettings.sessionIDPlacement", "Session ID placement", ["path", "header", "cookie", "query"]), f("stream.xhttpSettings.sessionIDKey", "Session ID key"), f("stream.xhttpSettings.sessionIDTable", "Session ID alphabet"), f("stream.xhttpSettings.sessionIDLength", "Session ID length"), select("stream.xhttpSettings.seqPlacement", "Sequence placement", ["path", "header", "cookie", "query"]), f("stream.xhttpSettings.seqKey", "Sequence key"), select("stream.xhttpSettings.uplinkDataPlacement", "Uplink data placement", ["auto", "body", "header", "cookie"]), f("stream.xhttpSettings.uplinkDataKey", "Uplink data key"), f("stream.xhttpSettings.uplinkChunkSize", "Uplink chunk size"), num("stream.xhttpSettings.serverMaxHeaderBytes", "Maximum server header bytes")] });
transportGroups.xdrive = [
  { title: "XDrive storage", fa: "ذخیره‌سازی XDrive", fields: [select("stream.xdriveSettings.service", "Storage service", ["Google Drive", "local", "template"], { required: true }), f("stream.xdriveSettings.remoteFolder", "Remote folder", { required: true }), secret("stream.xdriveSettings.secrets", "Storage credentials", { kind: "lines", wide: true, hint: "Google Drive: client ID, client secret and refresh token, one per line.", faHint: "Google Drive: شناسهٔ کلاینت، رمز کلاینت و Refresh Token؛ هرکدام در یک خط." }), f("stream.address", "Front address"), num("stream.port", "Front port", { min: 1, max: 65535 })] },
  { title: "XDrive timing and buffers", fa: "زمان‌بندی و بافر XDrive", advanced: true, fields: [num("stream.xdriveSettings.segmentBytes", "Segment bytes", { max: 16 * 1024 * 1024 }), num("stream.xdriveSettings.flushIntervalMs", "Flush interval (ms)"), num("stream.xdriveSettings.pollIntervalMs", "Poll interval (ms)"), num("stream.xdriveSettings.maxPollIntervalMs", "Maximum poll interval (ms)"), num("stream.xdriveSettings.sessionTtlSeconds", "Session lifetime (seconds)"), num("stream.xdriveSettings.concurrency", "Concurrency", { max: 64 }), num("stream.xdriveSettings.eagerWindowMs", "Eager window (ms)"), num("stream.xdriveSettings.holeTimeoutMs", "Hole timeout (ms)")] },
  { title: "Storage template authentication", fa: "احراز هویت قالب ذخیره‌سازی", advanced: true, fields: [bool("stream.xdriveSettings.template.flatten", "Flatten object names"), num("stream.xdriveSettings.template.concurrency", "Template concurrency", { max: 64 }), select("stream.xdriveSettings.template.auth.type", "Authentication type", ["none", "basic", "static", "oauth2"]), secret("stream.xdriveSettings.template.auth.header", "Authentication headers", { kind: "pairs" }), secret("stream.xdriveSettings.template.auth.username", "Username"), secret("stream.xdriveSettings.template.auth.password", "Password"), f("stream.xdriveSettings.template.auth.tokenUrl", "Token URL"), secret("stream.xdriveSettings.template.auth.form", "Token form", { kind: "pairs" }), f("stream.xdriveSettings.template.auth.tokenPath", "Token JSON path"), f("stream.xdriveSettings.template.auth.expiryPath", "Expiry JSON path"), f("stream.xdriveSettings.template.retry.status", "Retry HTTP statuses", { kind: "numbers" }), f("stream.xdriveSettings.template.retry.rateReason", "Rate-limit reason")] },
  ...["put", "get", "delete", "list"].map(operation => ({ title: `Storage template · ${operation.toUpperCase()}`, fa: `قالب ذخیره‌سازی · ${operation.toUpperCase()}`, advanced: true, fields: [select(`stream.xdriveSettings.template.${operation}.method`, "HTTP method", ["GET", "PUT", "POST", "PATCH", "DELETE", "HEAD"]), f(`stream.xdriveSettings.template.${operation}.url`, "URL"), f(`stream.xdriveSettings.template.${operation}.headers`, "Headers", { kind: "pairs" }), f(`stream.xdriveSettings.template.${operation}.body`, "Body"), f(`stream.xdriveSettings.template.${operation}.namesRegex`, "Object-name pattern")] })),
];
transportGroups.hysteria = [{ title: "Hysteria transport", fa: "انتقال Hysteria", fields: [num("stream.hysteriaSettings.udpIdleTimeout", "UDP idle timeout (seconds)", { min: 2, max: 600 }), select("stream.hysteriaSettings.masquerade.type", "Masquerade type", ["proxy", "file", "string"]), f("stream.hysteriaSettings.masquerade.dir", "Directory"), secret("stream.hysteriaSettings.masquerade.url", "Proxy URL"), bool("stream.hysteriaSettings.masquerade.rewriteHost", "Rewrite Host"), bool("stream.hysteriaSettings.masquerade.xForwarded", "X-Forwarded headers"), bool("stream.hysteriaSettings.masquerade.insecure", "Skip upstream certificate verification"), f("stream.hysteriaSettings.masquerade.content", "Response content"), f("stream.hysteriaSettings.masquerade.headers", "Response headers", { kind: "pairs" }), num("stream.hysteriaSettings.masquerade.statusCode", "Response status", { max: 599 })] }];
transportGroups.masque = [{ title: "MASQUE transport", fa: "انتقال MASQUE", fields: [f("stream.masqueSettings.path", "Path", { placeholder: "/.well-known/masque/ip/*/*/" })] }];
export const socketFields: SpecField[] = [num("stream.sockopt.mark", "Mark"), bool("stream.sockopt.tcpFastOpen", "TCP Fast Open"), select("stream.sockopt.tproxy", "Transparent proxy", ["off", "redirect", "tproxy"]), select("stream.sockopt.domainStrategy", "Domain strategy", ["AsIs", "UseIP", "UseIPv4", "UseIPv6", "UseIPv4v6", "UseIPv6v4", "ForceIP", "ForceIPv4", "ForceIPv6"]), f("stream.sockopt.dialerProxy", "Dialer proxy tag"), f("stream.sockopt.interface", "Interface"), bool("stream.sockopt.acceptProxyProtocol", "Accept PROXY protocol"), num("stream.sockopt.tcpKeepAliveIdle", "Keepalive idle (seconds)"), num("stream.sockopt.tcpKeepAliveInterval", "Keepalive interval (seconds)"), num("stream.sockopt.tcpUserTimeout", "TCP user timeout (ms)"), f("stream.sockopt.tcpCongestion", "TCP congestion algorithm"), num("stream.sockopt.tcpWindowClamp", "TCP window clamp"), num("stream.sockopt.tcpMaxSeg", "TCP maximum segment"), bool("stream.sockopt.tcpMptcp", "Multipath TCP"), bool("stream.sockopt.v6only", "IPv6 only")];
socketFields.push(bool("stream.sockopt.penetrate", "Penetrate"), list("stream.sockopt.trustedXForwardedFor", "Trusted X-Forwarded-For"), select("stream.sockopt.addressPortStrategy", "Address / port discovery", ["none", "SrvPortOnly", "SrvAddressOnly", "SrvPortAndAddress", "TxtPortOnly", "TxtAddressOnly", "TxtPortAndAddress"]), bool("stream.sockopt.happyEyeballs.prioritizeIPv6", "Happy Eyeballs: prefer IPv6"), num("stream.sockopt.happyEyeballs.tryDelayMs", "Connection-attempt delay (ms)"), num("stream.sockopt.happyEyeballs.interleave", "Address-family interleave", { min: 1, max: 65535 }), num("stream.sockopt.happyEyeballs.maxConcurrentTry", "Concurrent connection attempts", { max: 65535 }), f("stream.sockopt.customSockopt", "Custom socket options", { kind: "rows", fields: [select("system", "System", ["linux", "windows", "darwin"]), select("network", "Network", ["tcp", "udp"]), select("type", "Value type", ["int", "str"]), f("level", "Level", { required: true }), f("opt", "Option", { required: true }), f("value", "Value")] }));
export const muxFields: SpecField[] = [bool("client.mux.enabled", "Enable MUX"), num("client.mux.concurrency", "Concurrency", { min: -1, max: 1024 }), num("client.mux.xudpConcurrency", "XUDP concurrency", { min: -1, max: 1024 }), select("client.mux.xudpProxyUDP443", "UDP 443 handling", ["reject", "allow", "skip"])];
export const realityFields: SpecField[] = [f("reality.target", "Target (host:port)", { fa: "هدف REALITY (هاست:پورت)", required: true, placeholder: "example.org:443" }), list("reality.serverNames", "Server names / SNI", { fa: "نام‌های سرور / SNI", required: true }), list("reality.shortIds", "Short IDs", { fa: "شناسه‌های کوتاه", required: true }), secret("reality.privateKey", "Private key", { fa: "کلید خصوصی", hint: "Leave blank to generate a new key on save, or retain the saved key when editing.", faHint: "خالی: تولید خودکار در ساخت جدید یا حفظ کلید ذخیره‌شده هنگام ویرایش." })];
export const realityAdvancedFields: SpecField[] = [num("reality.xver", "PROXY version (Xver)", { max: 2 }), bool("reality.show", "Show"), num("reality.maxTimediff", "Maximum time difference (ms)"), f("reality.minClientVer", "Minimum client version"), f("reality.maxClientVer", "Maximum client version"), secret("reality.mldsa65Seed", "ML-DSA-65 seed"), secret("reality.masterKeyLog", "REALITY master-key log path"), ...["Upload", "Download"].flatMap(direction => [num(`reality.limitFallback${direction}.afterBytes`, `Fallback ${direction.toLowerCase()} — after bytes`), num(`reality.limitFallback${direction}.bytesPerSec`, `Fallback ${direction.toLowerCase()} — bytes/second`), num(`reality.limitFallback${direction}.burstBytesPerSec`, `Fallback ${direction.toLowerCase()} — burst bytes/second`)])];
export const certificateFields: SpecField[] = [secret("certificate", "Certificate chain (PEM)", { kind: "pem", wide: true }), secret("key", "Private key (PEM)", { kind: "pem", wide: true }), secret("certificateFile", "Certificate file path", { placeholder: "/etc/ssl/certs/server.pem" }), secret("keyFile", "Private key file path", { placeholder: "/etc/ssl/private/server.key" }), select("usage", "Usage", ["encipherment", "verify", "issue"]), bool("oneTimeLoading", "Load once"), bool("buildChain", "Build certificate chain"), num("ocspStapling", "OCSP stapling interval (seconds)")];
export const tlsFields: SpecField[] = [f("tls.serverName", "Server name / SNI"), list("tls.alpn", "ALPN", { placeholder: "h2\nhttp/1.1" }), select("tls.minVersion", "Minimum TLS version", ["1.0", "1.1", "1.2", "1.3"]), select("tls.maxVersion", "Maximum TLS version", ["1.0", "1.1", "1.2", "1.3"]), bool("tls.rejectUnknownSni", "Reject unknown SNI"), f("tls.certificates", "Certificates", { fa: "گواهی‌ها", kind: "rows", fields: certificateFields, wide: true })];
export const tlsAdvancedFields: SpecField[] = [f("tls.cipherSuites", "Cipher suites"), bool("tls.enableSessionResumption", "Session resumption"), bool("tls.disableSystemRoot", "Disable system roots"), secret("tls.echServerKeys", "ECH server keys", { wide: true }), list("tls.curvePreferences", "Curve preferences"), secret("tls.masterKeyLog", "TLS master-key log path"), bool("tls.useSystemCA", "Use system CA")];
export const clientFields: SpecField[] = [select("client.fingerprint", "Fingerprint", specFingerprints), f("client.serverName", "Client SNI"), f("client.spiderX", "SpiderX", { placeholder: "/" }), f("client.publicKey", "REALITY public key"), f("client.shortId", "Selected short ID"), f("client.mldsa65Verify", "ML-DSA-65 verify"), select("client.flow", "Flow", ["xtls-rprx-vision", "xtls-rprx-vision-udp443"]), f("client.encryption", "Encryption"), f("client.echConfigList", "ECH config list", { wide: true }), list("client.pinnedPeerCertSha256", "Pinned certificate SHA-256", { placeholder: "SHA-256 hex (64 characters)" }), f("client.verifyPeerCertByName", "Verify peer certificate name")];

export function readSpecPath(value: unknown, path: string): SpecValue | undefined {
  return path.split(".").reduce<unknown>((at, key) => at && typeof at === "object" ? (at as Record<string, unknown>)[key] : undefined, value) as SpecValue | undefined;
}
export function protocolClientFields(spec: AuthoringSpec): SpecField[] {
  if (spec.protocol === "vmess") return [select("client.security", "VMess client encryption", ["auto", "aes-128-gcm", "chacha20-poly1305", "none", "zero"]), num("client.alterId", "Alter ID", { min: 0, max: 0 })];
  if (["wireguard", "amneziawg"].includes(spec.protocol)) return [secret("client.preSharedKey", "Pre-shared key"), num("client.keepAlive", "Keepalive (seconds)", { max: 65535 }), list("client.allowedIPs", "Allowed IPs", { placeholder: "0.0.0.0/0\n::/0" }), list("client.dns", "Client DNS")];
  if (spec.protocol === "mtproto") return [f("client.adTag", "Advertising tag")];
  if (spec.protocol === "tuic") return [bool("client.allowInsecure", "Allow insecure TUIC certificate")];
  if (spec.protocol === "masque") return [f("client.host", "Client Host override"), f("client.path", "Client path override"), f("client.headers", "Client headers", { kind: "pairs", wide: true })];
  return [];
}
export function writeSpecPath<T extends object>(source: T, path: string, value: SpecValue | undefined): T {
  const copy = structuredClone(source), keys = path.split(".");
  let cursor = copy as Record<string, unknown>;
  for (let i = 0; i < keys.length - 1; i++) {
    const key = keys[i];
    if (!cursor[key] || typeof cursor[key] !== "object") cursor[key] = /^\d+$/.test(keys[i + 1]) ? [] : {};
    cursor = cursor[key] as Record<string, unknown>;
  }
  if (value === undefined) delete cursor[keys.at(-1)!]; else cursor[keys.at(-1)!] = value;
  return copy;
}
export function isSpecSecret(path: string): boolean {
  const name = path.split(".").at(-1)!.replace(/[_-]/g, "").toLowerCase();
  if (path === "stream.xdriveSettings.template" || name === "headers" || name === "header" && path.includes("template.auth")) return true;
  if (name === "url" && (/^stream\.finalmask\.(?:tcp|udp)\.\d+\.settings\.url$/.test(path) || path === "stream.hysteriaSettings.masquerade.url")) return true;
  if (name === "value" && /^stream\.finalmask\.udp\.\d+\.settings\.value$/.test(path)) return true;
  return ["password", "pass", "privatekey", "secretkey", "secret", "secrets", "decryption", "testseed", "mldsa65seed", "presharedkey", "headerprotectionkey", "echserverkeys", "certificate", "certificatefile", "key", "keyfile", "masterkeylog"].includes(name);
}
export function redactSpec(value: AuthoringSpec, extra: string[] = []): AuthoringSpec {
  function visit(item: SpecValue, path: string): SpecValue {
    if (Array.isArray(item)) return item.map((entry, index) => visit(entry, `${path}.${index}`));
    if (item && typeof item === "object") return Object.fromEntries(Object.entries(item).filter(([key]) => !isSpecSecret(path ? `${path}.${key}` : key) && !extra.includes(path ? `${path}.${key}` : key)).map(([key, child]) => [key, visit(child, path ? `${path}.${key}` : key)]));
    return item;
  }
  return visit(value as unknown as SpecObject, "") as unknown as AuthoringSpec;
}
export function specPayload(value: AuthoringSpec, _retained: string[] = []): AuthoringSpec {
  const result = structuredClone(value);
  // Blank write-only controls delete their key. An explicit clear action or
  // JSON empty value must remain present so the server can distinguish it.
  for (const network of [...inboundTransports, "hysteria", "masque"]) if (network !== result.network) delete result.stream[`${network}Settings`];
  if (result.network !== "xdrive") { delete result.stream.address; delete result.stream.port; }
  if (result.security !== "tls") { result.tls = {}; for (const key of ["echConfigList", "pinnedPeerCertSha256", "verifyPeerCertByName", "allowInsecure"]) delete result.client[key]; }
  if (result.security !== "reality") { result.reality = {}; for (const key of ["spiderX", "publicKey", "shortId", "mldsa65Verify"]) delete result.client[key]; }
  if (["mtproto", "amneziawg", "tuic", "tun", "http", "mixed"].includes(result.protocol)) result.stream = {};
  if (Array.isArray(result.client.pinnedPeerCertSha256)) result.client.pinnedPeerCertSha256 = result.client.pinnedPeerCertSha256.map(pin => typeof pin === "string" ? canonicalCertificatePin(pin) : pin);
  return result;
}
export function canonicalCertificatePin(pin: string): string {
  const hex = pin.replace(/:/g, "");
  if (/^[a-fA-F0-9]{64}$/.test(hex)) return hex.toLowerCase();
  try { const bytes = atob(pin); if (bytes.length === 32) return Array.from(bytes, byte => byte.charCodeAt(0).toString(16).padStart(2, "0")).join(""); } catch { /* The field validator will identify malformed input. */ }
  return pin;
}
export function defaultSpec(protocol = "vless"): AuthoringSpec {
  const intrinsic = !generalTransportProtocols.includes(protocol) && !["http", "mixed", "tunnel"].includes(protocol);
  const network = intrinsic ? protocol : "tcp";
  const security = ["hysteria", "tuic", "masque", "trojan"].includes(protocol) ? "tls" : protocol === "vless" ? "reality" : "none";
  return { version: 1, protocol, network, security, settings: protocol === "vless" ? { decryption: "none" } : protocol === "hysteria" ? { version: 2 } : protocol === "shadowsocks" ? { method: "2022-blake3-aes-256-gcm", network: "tcp,udp" } : protocol === "mixed" ? { auth: "password", udp: true } : {}, stream: {}, tls: {}, reality: security === "reality" ? { shortIds: [Array.from(crypto.getRandomValues(new Uint8Array(4)), n => n.toString(16).padStart(2, "0")).join("")] } : {}, client: {} };
}
export function specFromInbound(source?: Inbound): AuthoringSpec {
  if (source?.spec) return canonicalAuthoringSpec(source.spec);
  const spec = defaultSpec(); if (!source) return spec;
  spec.network = source.transport; spec.security = source.security ?? "reality";
  spec.reality = { target: source.reality_target, serverNames: source.server_names, shortIds: source.short_ids };
  const o = source.options;
  if (source.transport === "grpc") spec.stream.grpcSettings = { serviceName: source.grpc_service, authority: o?.grpc_authority ?? "", multiMode: o?.grpc_multi_mode ?? false };
  if (o?.xhttp) spec.stream.xhttpSettings = { path: o.xhttp.path ?? "", host: o.xhttp.host ?? "", mode: o.xhttp.mode ?? "auto", xPaddingBytes: o.xhttp.x_padding_bytes ?? "" };
  if (o?.websocket) spec.stream.wsSettings = { ...o.websocket };
  if (o?.http_upgrade) spec.stream.httpupgradeSettings = { ...o.http_upgrade };
  if (o?.client) spec.client = { fingerprint: o.client.fingerprint ?? "", spiderX: o.client.spider_x ?? "" };
  if (source.public_key) spec.client.publicKey = source.public_key;
  for (const [from, to] of [["reality_xver", "xver"], ["reality_max_time_diff", "maxTimediff"], ["reality_min_client_ver", "minClientVer"], ["reality_max_client_ver", "maxClientVer"], ["reality_show", "show"]] as const) if (o?.[from] !== undefined) spec.reality[to] = o[from]!;
  return spec;
}
export function allowedSpecSecurity(protocol: string, network: string): Array<AuthoringSpec["security"]> {
  if (["hysteria", "tuic", "masque"].includes(protocol)) return ["tls"];
  if (!generalTransportProtocols.includes(protocol)) return ["none"];
  const modes: Array<AuthoringSpec["security"]> = ["none"];
  if (["tcp", "ws", "grpc", "httpupgrade", "xhttp"].includes(network)) modes.push("tls");
  if (["vless", "trojan"].includes(protocol) && ["tcp", "grpc", "xhttp"].includes(network)) modes.push("reality");
  return modes;
}
export function validAuthoringSpec(value: unknown): value is AuthoringSpec {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const spec = value as AuthoringSpec;
  return spec.version === 1 && inboundProtocols.includes(spec.protocol as typeof inboundProtocols[number]) && typeof spec.network === "string" && ["none", "tls", "reality"].includes(spec.security) && ["settings", "stream", "tls", "reality", "client"].every(key => { const item = (spec as unknown as SpecObject)[key]; return item === undefined || item !== null && typeof item === "object" && !Array.isArray(item); });
}
export function canonicalAuthoringSpec(value: unknown): AuthoringSpec {
  if (!validAuthoringSpec(value)) throw new Error("invalid_authoring_spec");
  const copy = structuredClone(value);
  return { ...copy, settings: copy.settings ?? {}, stream: copy.stream ?? {}, tls: copy.tls ?? {}, reality: copy.reality ?? {}, client: copy.client ?? {} };
}
export function assertPublicSpec(value: unknown): void {
  const spec = canonicalAuthoringSpec(value);
  if (JSON.stringify(spec) !== JSON.stringify(redactSpec(spec))) throw new Error("unexpected_private_spec_material");
}
export function specIssues(spec: AuthoringSpec, retained: string[] = []): Array<{ field: string; code: string }> {
  const issues: Array<{ field: string; code: string }> = [];
  const add = (path: string, code = "spec_field_invalid") => issues.push({ field: `spec.${path}`, code });
  if (!validAuthoringSpec(spec)) return [{ field: "spec.protocol", code: "spec_invalid" }];
  if (!allowedSpecSecurity(spec.protocol, spec.network).includes(spec.security)) add("security", "spec_security_incompatible");
  if (generalTransportProtocols.includes(spec.protocol) && !inboundTransports.includes(spec.network as typeof inboundTransports[number])) add("network");
  const fields = [...(protocolFields[spec.protocol] ?? []), ...(transportGroups[spec.network] ?? []).flatMap(group => group.fields), ...(spec.security === "reality" ? [...realityFields, ...realityAdvancedFields] : spec.security === "tls" ? [...tlsFields, ...tlsAdvancedFields] : []), ...clientFields, ...socketFields, ...muxFields];
  function check(field: SpecField, prefix = "") {
    const path = prefix + field.path, value = readSpecPath(spec, path);
    if (field.required && (value === undefined || value === "" || Array.isArray(value) && !value.length) && !retained.includes(path)) add(path, "spec_field_required");
    if (value === undefined || value === "") return;
    if (field.kind === "number" && (typeof value !== "number" || !Number.isFinite(value) || !Number.isInteger(value) || field.min !== undefined && value < field.min || field.max !== undefined && value > field.max)) add(path);
    if (field.kind === "rows" && Array.isArray(value)) value.forEach((_, index) => field.fields?.forEach(child => check(child, `${path}.${index}.`)));
  }
  fields.forEach(field => check(field));
  if (spec.security === "reality") {
    if (typeof spec.reality.target !== "string" || !/^(?:\[[^\]]+\]|[^\s:/]+):\d{1,5}$/.test(spec.reality.target)) add("reality.target");
    if (Array.isArray(spec.reality.shortIds) && spec.reality.shortIds.some(id => typeof id !== "string" || !/^(?:[a-f0-9]{2}){1,8}$/.test(id))) add("reality.shortIds");
  }
  if (["clients", "accounts", "users", "peers"].some(key => Object.hasOwn(spec.settings, key))) add("settings", "spec_clients_managed");
  return issues;
}
export function specPublicMatches(expected: AuthoringSpec, actual: AuthoringSpec, retained: string[] = []): boolean {
  if (!validAuthoringSpec(actual)) return false;
  const clean = redactSpec(expected, retained);
  function contains(wanted: unknown, received: unknown): boolean {
    if (wanted && typeof wanted === "object") {
      if (!received || typeof received !== "object" || Array.isArray(wanted) !== Array.isArray(received)) return false;
      if (Array.isArray(wanted)) return wanted.length === (received as unknown[]).length && wanted.every((item, index) => contains(item, (received as unknown[])[index]));
      return Object.entries(wanted).every(([key, value]) => contains(value, (received as Record<string, unknown>)[key]));
    }
    return wanted === received || (wanted === "" || wanted === false || wanted === 0) && received === undefined;
  }
  return contains(clean, canonicalAuthoringSpec(actual));
}
