import { Plus, Trash2 } from "lucide-react";
import InboundSpecFields from "./InboundSpecFields";
import { readSpecPath, socketFields, tlsFields, writeSpecPath, type AuthoringSpec, type SpecField, type SpecObject } from "./inboundAuthoringSpec";

type Props = { language: "fa" | "en"; spec: AuthoringSpec; secretPaths: string[]; disabled: boolean; invalidFields: string[]; onChange: (value: AuthoringSpec) => void; onInvalid: (path: string, invalid: boolean) => void };
const field = (path: string, label: string, kind: SpecField["kind"] = "text", extra: Partial<SpecField> = {}): SpecField => ({ path, label, kind, ...extra });
const number = (path: string, label: string, min = 0, max = Number.MAX_SAFE_INTEGER) => field(path, label, "number", { min, max });
const choice = (path: string, label: string, options: string[]) => field(path, label, "select", { options });
const rows = (path: string, label: string, fields: SpecField[]) => field(path, label, "rows", { fields });
const packet = (path: string, label: string) => field(path, label, "packet");
function transformFields(depth: number): SpecField[] {
  const args: SpecField[] = [choice("type", "Encoding", ["array", "str", "hex", "base64"]), packet("bytes", "Bytes"), number("u64", "Integer"), field("reuse", "Reuse capture"), field("metadata", "Metadata")];
  if (depth < 3) args.push(...transformFields(depth + 1).map(child => ({ ...child, path: `transform.${child.path}` })));
  return [choice("op", "Transform operation", ["concat", "slice", "xor16", "xor32", "be16", "be32", "le16", "le32", "le64", "pad", "truncate", "add", "sub", "and", "or", "shl", "shr"]), rows("args", "Transform arguments", args)];
}
function customFields(tcp: boolean): SpecField[] {
  return [number("rand", "Random byte count", 0, 65536), field("randRange", "Random byte range"), field("capture", "Capture name"), choice("type", "Encoding", ["array", "str", "hex", "base64"]), field("reuse", "Reuse capture"), packet("packet", "Packet bytes / text"), ...(tcp ? [field("delay", "Delay (ms)")] : []), ...transformFields(0).map(child => ({ ...child, path: `transform.${child.path}` }))];
}
function maskFields(type: string, tcp: boolean): SpecField[] {
  switch (type) {
    case "fragment": return [field("packets", "Packets", "text", { placeholder: "tlshello" }), field("lengths", "Fragment-length ranges", "lines", { placeholder: "100-200" }), field("delays", "Delay ranges", "lines", { placeholder: "10-20" }), field("maxSplit", "Maximum split range")];
    case "sudoku": return [field("password", "Password", "text", { secret: true }), field("ascii", "ASCII mode"), field("customTable", "Custom table"), field("customTables", "Custom tables", "lines"), number("paddingMin", "Minimum padding"), number("paddingMax", "Maximum padding")];
    case "xmc": return [field("hostname", "Hostname"), field("password", "Password", "text", { secret: true, required: true }), rows("profiles", "Minecraft profiles", [field("username", "Username", "text", { required: true }), field("uuid", "UUID", "text", { required: true }), field("texturesValue", "Textures value", "text", { required: true }), field("texturesSignature", "Textures signature", "text", { required: true })])];
    case "salamander": return [field("password", "Password", "text", { secret: true, required: true }), field("packetSize", "Packet-size range", "text", { placeholder: "512-1024" })];
    case "mkcp-legacy": return [choice("header", "Legacy header", ["dns", "dtls", "srtp", "utp", "wechat", "wireguard"]), field("value", "Header value")];
    case "xdns": return [rows("domains", "DNS domains", [field("names", "Domain names", "lines"), number("lenLimit", "Name length limit", 0, 255), number("labelLimit", "Label length limit", 0, 63), field("types", "DNS record types", "numbers"), number("edns0", "EDNS0 size", 0, 65535)]), rows("resolvers", "Resolvers", [field("addrs", "Resolver addresses", "lines")]), number("extraPoll", "Extra polling")];
    case "xicmp": return [field("dgram", "Datagram socket", "boolean"), field("ips", "IP addresses", "lines")];
    case "realm": return [field("url", "Realm URL", "text", { required: true, secret: true, placeholder: "realm://token@example.org/id" }), field("stunServers", "STUN servers", "lines", { required: true, placeholder: "stun.example.org:3478" }), choice("ipMode", "IP mode", ["dual", "v4", "v6"]), field("portMapping.enabled", "Enable port mapping", "boolean"), number("portMapping.timeout", "Port mapping timeout", 0, 2147483647), number("portMapping.lifetime", "Port mapping lifetime", 0, 2147483647), ...tlsFields.filter(item => item.path !== "tls.certificates").map(item => ({ ...item, path: item.path.replace(/^tls\./, "tlsConfig.") })), field("tlsConfig.fingerprint", "Fingerprint"), field("tlsConfig.echConfigList", "ECH config"), field("tlsConfig.pinnedPeerCertSha256", "Pinned certificate SHA-256"), field("tlsConfig.verifyPeerCertByName", "Certificate name")];
    case "noise": return [field("reset", "Reset range"), rows("noise", "Noise packets", [field("rand", "Random bytes"), field("randRange", "Random byte range"), choice("type", "Encoding", ["array", "str", "hex", "base64", "exp"]), packet("packet", "Packet bytes / text"), field("delay", "Delay (ms)")])];
    case "header-custom": return tcp ? [] : [choice("mode", "Mode", ["prefix", "standalone"]), rows("client", "Client packets", customFields(false)), rows("server", "Server packets", customFields(false))];
    default: return [];
  }
}
const quicFields: SpecField[] = [choice("congestion", "Congestion control", ["reno", "bbr", "brutal", "force-brutal"]), choice("bbrProfile", "BBR profile", ["conservative", "standard", "aggressive"]), field("debug", "Debug", "boolean"), field("brutalUp", "Brutal uplink", "text", { placeholder: "100mbps" }), field("brutalDown", "Brutal downlink", "text", { placeholder: "100mbps" }), field("brutalDisableLossCompensation", "Disable loss compensation", "boolean"), number("initStreamReceiveWindow", "Initial stream receive window"), number("maxStreamReceiveWindow", "Maximum stream receive window"), number("initConnectionReceiveWindow", "Initial connection receive window"), number("maxConnectionReceiveWindow", "Maximum connection receive window"), number("maxIdleTimeout", "Maximum idle timeout (seconds)", 4, 120), number("keepAlivePeriod", "Keepalive period (seconds)", 2, 60), field("disablePathMTUDiscovery", "Disable path MTU discovery", "boolean"), field("disableChromeParrot", "Disable Chrome parrot", "boolean"), field("disableGSO", "Disable GSO", "boolean"), number("maxIncomingStreams", "Maximum incoming streams", 8, 2147483647), field("disableStatelessReset", "Disable stateless reset", "boolean"), field("udpHop.ports", "Client UDP hopping ports", "text", { placeholder: "20000-21000,22000" }), field("udpHop.interval", "Client hopping interval (seconds)", "text", { placeholder: "10-30" })];

function TCPPacketGroups(props: Props & { path: string }) {
  return <>{["clients", "servers", "errors"].map(direction => {
    const path = `${props.path}.${direction}`, value = readSpecPath(props.spec, path), groups = Array.isArray(value) ? value : [];
    return <section className="spec-repeater" key={path}><header><h5>{direction}</h5><button type="button" disabled={props.disabled} onClick={() => props.onChange(writeSpecPath(props.spec, path, [...groups, []]))}><Plus size={13}/>{props.language === "fa" ? "افزودن گروه بسته" : "Add packet group"}</button></header>{groups.map((group, index) => {
      const packets = Array.isArray(group) ? group : [];
      return <details className="spec-row" key={index} open><summary>{direction} {index + 1}</summary><div className="spec-row-body">{packets.map((_, item) => <div className="spec-row-body" key={item}><InboundSpecFields {...props} prefix={`${path}.${index}.${item}.`} fields={customFields(true)}/><button type="button" disabled={props.disabled} onClick={() => props.onChange(writeSpecPath(props.spec, `${path}.${index}`, packets.filter((_, at) => at !== item)))}><Trash2 size={13}/>{props.language === "fa" ? "حذف بسته" : "Remove packet"}</button></div>)}<div className="spec-actions"><button type="button" disabled={props.disabled} onClick={() => props.onChange(writeSpecPath(props.spec, `${path}.${index}`, [...packets, {}]))}><Plus size={13}/>{props.language === "fa" ? "افزودن بسته" : "Add packet"}</button><button type="button" disabled={props.disabled} onClick={() => props.onChange(writeSpecPath(props.spec, path, groups.filter((_, at) => at !== index)))}>{props.language === "fa" ? "حذف گروه" : "Remove group"}</button></div></div></details>;
    })}</section>;
  })}</>;
}

export default function InboundFinalMask(props: Props) {
  const { language, spec, disabled, onChange } = props, fa = language === "fa";
  const noStream = ["mtproto", "amneziawg", "tuic", "tun", "http", "mixed", "tunnel"].includes(spec.protocol);
  const allowedDirection = (direction: string) => !noStream && (direction === "tcp" ? !["kcp", "wireguard", "hysteria", "masque"].includes(spec.network) : spec.network === "kcp" || ["hysteria", "wireguard"].includes(spec.protocol));
  return <details className="spec-disclosure"><summary>FinalMask</summary><div className="spec-group-body">
    {(["tcp", "udp"] as const).map(direction => {
      const path = `stream.finalmask.${direction}`, value = readSpecPath(spec, path), masks = Array.isArray(value) ? value : [], allowed = allowedDirection(direction);
      const types = direction === "tcp" ? ["fragment", "header-custom", "sudoku", "xmc"] : ["salamander", "mkcp-legacy", "xdns", "xicmp", "realm", "header-custom", "noise", "sudoku"];
      const maskAllowed = (kind: string) => spec.protocol === "hysteria" ? kind === "salamander" : kind !== "salamander" || spec.protocol === "wireguard";
      return <section className="spec-repeater" key={direction}><header><h5>{direction.toUpperCase()} <bdi>{masks.length}</bdi></h5><button type="button" disabled={disabled || !allowed || masks.length >= 64} onClick={() => onChange(writeSpecPath(spec, path, [...masks, { type: types.find(maskAllowed)!, settings: {} }]))}><Plus size={14}/>{fa ? "افزودن ماسک" : "Add mask"}</button></header>{!allowed && <p className="creation-help">{fa ? "این نوع ماسک با پروتکل یا انتقال فعلی سازگار نیست." : "This mask direction is incompatible with the selected protocol or transport."}</p>}
        {masks.map((value, index) => {
          const mask = value as SpecObject, type = String(mask.type ?? ""), base = `${path}.${index}`, prefix = `${base}.settings.`;
          return <details key={index} className="spec-row" open><summary>{type || direction.toUpperCase()} {index + 1}</summary><div className="spec-row-body"><label className="creation-field"><span>{fa ? "نوع ماسک" : "Mask type"}</span><select aria-label={`${direction.toUpperCase()} mask ${index + 1}`} data-creation-field={`spec.${base}.type`} disabled={disabled} value={type} onChange={event => onChange(writeSpecPath(spec, base, { type: event.target.value, settings: {} }))}>{types.map(kind => <option key={kind} value={kind} disabled={!maskAllowed(kind)}>{kind}</option>)}</select></label><InboundSpecFields {...props} prefix={prefix} fields={maskFields(type, direction === "tcp")}/>{type === "header-custom" && direction === "tcp" && <TCPPacketGroups {...props} path={`${base}.settings`}/>}<button type="button" className="spec-remove" disabled={disabled} onClick={() => onChange(writeSpecPath(spec, path, masks.filter((_, at) => at !== index)))}><Trash2 size={14}/>{fa ? "حذف ماسک" : "Remove mask"}</button></div></details>;
        })}
      </section>;
    })}
    <details className="spec-disclosure"><summary>QUIC</summary><div className="spec-group-body">{spec.network === "xhttp" || spec.protocol === "hysteria" ? <><InboundSpecFields {...props} prefix="stream.finalmask.quicParams." fields={quicFields}/><p className="creation-help">{fa ? "Port Hopping در خروجی اتصال کلاینت قرار می‌گیرد." : "Port hopping is included in client connection settings."}</p></> : <p className="creation-help">{fa ? "تنظیمات QUIC برای Hysteria و XHTTP در دسترس است." : "QUIC settings are available for Hysteria and XHTTP."}</p>}</div></details>
  </div></details>;
}

export function ECHSocketFields(props: Props) {
  return <InboundSpecFields {...props} fields={socketFields.map(field => ({ ...field, path: field.path.replace(/^stream\.sockopt\./, "tls.echSockopt.") }))}/>;
}

export function sniffingFields(): SpecField[] {
  return [field("settings.sniffing.enabled", "Enable Sniffing", "boolean"), field("settings.sniffing.destOverride", "Destination protocols", "lines", { placeholder: "http\ntls\nquic\nfakedns" }), field("settings.sniffing.metadataOnly", "Metadata only", "boolean"), field("settings.sniffing.routeOnly", "Routing only", "boolean"), field("settings.sniffing.domainsExcluded", "Excluded domains", "lines"), field("settings.sniffing.ipsExcluded", "Excluded IPs / CIDRs", "lines")];
}
