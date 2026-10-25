import { useState, type ReactNode } from "react";
import { Copy, Pencil } from "lucide-react";
import type { InboundNode } from "./inboundTypes";
import { creationNodeName, creationPathHosts, creationShortIDs, creationWaitingHosts, selectedCreationNodes, type CreationNode, type CreationState } from "./InboundCreationModel";
import { profileOptionsDraft } from "./inboundProfileOptions";
import { trafficLines } from "./inboundTrafficOptions";
import { effectiveEndpoint } from "./inboundEndpointOptions";
import type { CreationEndpointPreview } from "./inboundEndpointPreview";
import { transportLabel } from "./inboundNetworkOptions";
import type { TLSMetadata } from "./inboundTLS";
import { TLSCertificateSummary } from "./InboundTLSCredentials";

const words = {
  en: {
    connection: "Connection", routes: "Dark Swap · Nodes & paths", advanced: "Advanced settings", editConnection: "Edit connection", editRoutes: "Edit Dark Swap", editAdvanced: "Edit advanced settings",
    name: "Inbound name", protocol: "Protocol", transport: "Transport & security", service: "gRPC service", target: "REALITY target", serverNames: "Server names", shortIDs: "Short IDs",
    authority: "gRPC Authority", multiMode: "gRPC MultiMode", xver: "REALITY Xver", time: "Maximum time difference (ms)", minVersion: "Minimum client version", maxVersion: "Maximum client version", show: "REALITY Show",
    on: "Enabled", off: "Disabled", none: "None", default: "Default", nodes: "Destination Nodes", hosts: "Unique Hosts", listen: "Destination listening IP", port: "Listener port", addresses: "Hosts",
    direct: "Direct", tunnel: "Tunnel", swap: "Dark Swap", waiting: "Waiting for Host", noHosts: "No Host yet", noNodes: "No destination Nodes selected.",
    source: "Source Node", sourceIP: "Source relay listening IP", sourcePort: "Source relay port", entry: "External entry address", entryPort: "External entry port", destinationPort: "Destination exit port", reuse: "Reuses this Node’s Tunnel port",
    draft: "Retained draft", draftOff: "Disabled · draft values are not saved", sniffing: "Sniffing", retained: "Disabled · settings retained", overrides: "Destination overrides", routeOnly: "Route Only", metadataOnly: "Metadata Only", domains: "Excluded domains", ips: "Excluded IPs or CIDRs",
    noSniffing: "No custom Sniffing settings.", fallbacks: "TCP fallbacks", noFallbacks: "No fallbacks configured.", destination: "Destination", path: "Path", proxy: "PROXY protocol", fallback: "Fallback", tcp: "These retained fallbacks require TCP before this setup can be saved.",
    xhttpPath: "XHTTP Path", xhttpHost: "XHTTP Host", xhttpMode: "XHTTP Mode", xhttpPadding: "XHTTP Padding", fingerprint: "Client Fingerprint", spider: "Client SpiderX", publicKey: "REALITY public key", copyKey: "Copy public key", copied: "Copied", copyFailed: "Copy unavailable",
    endpoint: "Host", publicPort: "Public port", sni: "SNI", label: "Display name", endpointOff: "Disabled · settings saved", inherit: "Inherited", checkedEndpoints: "Checked customer endpoints", checkedHint: "Connection settings confirmed by the server for this paused setup.",
  },
  fa: {
    connection: "اتصال", routes: "دارک‌سواپ · نودها و مسیرها", advanced: "تنظیمات پیشرفته", editConnection: "ویرایش اتصال", editRoutes: "ویرایش دارک‌سواپ", editAdvanced: "ویرایش تنظیمات پیشرفته",
    name: "نام اینباند", protocol: "پروتکل", transport: "انتقال و امنیت", service: "سرویس gRPC", target: "مقصد REALITY", serverNames: "نام‌های سرور", shortIDs: "شناسه‌های کوتاه",
    authority: "Authority در gRPC", multiMode: "MultiMode در gRPC", xver: "Xver در REALITY", time: "حداکثر اختلاف زمان (میلی‌ثانیه)", minVersion: "حداقل نسخهٔ کلاینت", maxVersion: "حداکثر نسخهٔ کلاینت", show: "Show در REALITY",
    on: "فعال", off: "خاموش", none: "هیچ‌کدام", default: "پیش‌فرض", nodes: "نود مقصد", hosts: "هاست یکتا", listen: "IP شنود نود مقصد", port: "پورت شنود", addresses: "هاست‌ها",
    direct: "مستقیم", tunnel: "تونل", swap: "دارک‌سواپ", waiting: "در انتظار هاست", noHosts: "بدون هاست", noNodes: "هیچ نود مقصدی انتخاب نشده.",
    source: "نود مبدأ", sourceIP: "IP شنود رلهٔ مبدأ", sourcePort: "پورت رلهٔ مبدأ", entry: "آدرس ورودی بیرونی", entryPort: "پورت ورودی بیرونی", destinationPort: "پورت خروجی مقصد", reuse: "استفاده از پورت تونل همین نود",
    draft: "مقادیر پیش‌نویس", draftOff: "خاموش · مقادیر پیش‌نویس ذخیره نمی‌شوند", sniffing: "Sniffing", retained: "خاموش · تنظیمات حفظ شده", overrides: "پروتکل‌های تشخیص مقصد", routeOnly: "Route Only", metadataOnly: "Metadata Only", domains: "دامنه‌های مستثنا", ips: "IPها یا CIDRهای مستثنا",
    noSniffing: "تنظیمات سفارشی Sniffing وجود ندارد.", fallbacks: "Fallbackهای TCP", noFallbacks: "هیچ Fallbackی تنظیم نشده.", destination: "مقصد", path: "مسیر", proxy: "پروتکل PROXY", fallback: "Fallback", tcp: "برای ذخیرهٔ این Fallbackهای حفظ‌شده، انتقال باید TCP باشد.",
    xhttpPath: "Path در XHTTP", xhttpHost: "Host در XHTTP", xhttpMode: "Mode در XHTTP", xhttpPadding: "Padding در XHTTP", fingerprint: "Fingerprint کلاینت", spider: "SpiderX کلاینت", publicKey: "کلید عمومی REALITY", copyKey: "کپی کلید عمومی", copied: "کپی شد", copyFailed: "کپی در دسترس نیست",
    endpoint: "Host", publicPort: "پورت عمومی", sni: "SNI", label: "نام نمایشی", endpointOff: "خاموش · تنظیمات ذخیره می‌شود", inherit: "ارث‌بری", checkedEndpoints: "نقاط اتصال مشتریِ بررسی‌شده", checkedHint: "تنظیمات اتصال این طرح خاموش به تأیید سرور رسیده است.",
  },
};

function Value({ label, value, field, option }: { label: string; value: ReactNode; field?: string; option?: string }) {
  return <div data-review-field={field} data-review-option={option}><dt>{label}</dt><dd><bdi>{value || "—"}</bdi></dd></div>;
}

export default function InboundCreationReview({ language, state, nodes, disabled, onEdit, publicKey, checkedEndpoints, tlsMetadata }: {
  language: "en" | "fa"; state: CreationState; nodes: InboundNode[]; disabled: boolean; onEdit: (section: "connection" | "swap" | "advanced") => void;
  publicKey?: string; checkedEndpoints?: CreationEndpointPreview[]; tlsMetadata?: TLSMetadata;
}) {
  const t = words[language], profile = state.profile, options = profile.options ?? profileOptionsDraft(), rows = selectedCreationNodes(state);
  const tls = profile.security === "tls";
  const domains = trafficLines(options.sniffing.domainsExcluded), ips = trafficLines(options.sniffing.ipsExcluded);
  const sniffingConfigured = (options.sniffingPresent || options.sniffing.enabled) && (options.sniffing.enabled || options.sniffing.destOverride.length > 0 || options.sniffing.routeOnly || options.sniffing.metadataOnly || domains.length > 0 || ips.length > 0);
  const sniffingRetained = sniffingConfigured && !options.sniffing.enabled;
  const hostCount = new Set(rows.flatMap(row => [...(row.direct.enabled ? creationPathHosts(row.direct) : []), ...(row.tunnel.enabled ? creationPathHosts(row.tunnel) : [])])).size;
  const defaults = { serverNames: profile.serverNames.split(/[\s,]+/).filter(Boolean), fingerprint: options.clientFingerprint, spiderX: options.clientSpiderX, security: profile.security };
  const [copyState, setCopyState] = useState("");
  const proxy = (value: string) => value === "0" ? `${t.off} (0)` : value === "1" ? "PROXY v1 (1)" : value === "2" ? "PROXY v2 (2)" : value || "—";
  const nondefaultNumber = (value: string) => value.trim() !== "" && Number(value) !== 0;
  const edit = (section: "connection" | "swap" | "advanced", label: string) => <button type="button" disabled={disabled} onClick={() => onEdit(section)}><Pencil size={14}/>{label}</button>;
  function pathValues(row: CreationNode, path: "direct" | "tunnel" | "swap") {
    return <><dl className="creation-review-values">{path === "swap" ? <>
      <Value label={t.source} value={row.swap.sourceNodeID ? creationNodeName(nodes, row.swap.sourceNodeID) : "—"}/>
      <Value label={t.sourceIP} value={row.swap.sourceListenIP}/><Value label={t.sourcePort} value={row.swap.sourcePort}/>
      <Value label={t.entry} value={row.swap.entryHost || t.noHosts}/><Value label={t.entryPort} value={row.swap.entryPort}/>
      <Value label={t.destinationPort} value={row.swap.destinationPort}/>
    </> : <><Value label={t.port} value={row[path].port}/>{row[path].endpoints === undefined && <Value label={t.addresses} value={creationPathHosts(row[path]).join("\n") || t.noHosts}/>}</>}</dl>
      {path !== "swap" && row[path].endpoints?.map((endpoint, index) => {
        const effective = effectiveEndpoint(endpoint, row[path].port, defaults);
        return <div className="creation-review-endpoint" key={endpoint.key} data-review-endpoint={index} data-endpoint-enabled={endpoint.enabled}>
          <h6>{endpoint.label || `${t.endpoint} ${index + 1}`}<span>{endpoint.enabled ? t.on : t.endpointOff}</span></h6>
          <dl className="creation-review-values"><Value label={t.addresses} value={effective.address}/><Value label={t.publicPort} value={effective.port || "—"}/><Value label={t.sni} value={effective.sni}/><Value label={t.fingerprint} value={effective.fingerprint}/>{!tls && <Value label={t.spider} value={effective.spiderX}/>}</dl>
        </div>;
      })}
    </>;
  }
  return <div className="creation-review-sections">
    {!state.spec && <section className="creation-review-card" data-review-section="connection">
      <header><h4>{t.connection}</h4>{edit("connection", t.editConnection)}</header>
      <dl className="creation-review-values">
        <Value label={t.name} value={profile.name} field="name"/><Value label={t.protocol} value="VLESS" field="protocol"/>
        <Value label={t.transport} value={`${transportLabel(profile.transport)} / ${tls ? "TLS" : "REALITY"}${profile.transport === "tcp" ? " · Vision" : ""}`} field="transport"/>
        {profile.transport === "grpc" && <Value label={t.service} value={profile.grpcService} field="grpcService"/>}
        {profile.transport === "xhttp" && <><Value label={t.xhttpPath} value={options.xhttpPath || "/"} option="xhttpPath"/><Value label={t.xhttpHost} value={options.xhttpHost || t.default} option="xhttpHost"/><Value label={t.xhttpMode} value={options.xhttpMode || "auto"} option="xhttpMode"/><Value label={t.xhttpPadding} value={options.xhttpPadding || "100-1000"} option="xhttpPadding"/></>}
        {tls && (profile.transport === "ws" || profile.transport === "httpupgrade") && <><Value label={`${transportLabel(profile.transport)} Path`} value={(profile.transport === "ws" ? options.websocketPath : options.httpUpgradePath) || "/"}/><Value label={`${transportLabel(profile.transport)} Host`} value={(profile.transport === "ws" ? options.websocketHost : options.httpUpgradeHost) || t.default}/></>}
        {!tls && <Value label={t.target} value={profile.realityTarget} field="realityTarget"/>}
        <Value label={t.serverNames} value={profile.serverNames.split(/[\s,]+/).filter(Boolean).join("\n")} field="serverNames"/>
        {!tls && <Value label={t.shortIDs} value={creationShortIDs(state.shortIDs).join(", ")} field="shortIDs"/>}
        {profile.transport === "grpc" && options.grpcAuthority && <Value label={t.authority} value={options.grpcAuthority} option="grpcAuthority"/>}
        {profile.transport === "grpc" && options.grpcMultiMode && <Value label={t.multiMode} value={t.on} option="grpcMultiMode"/>}
        {!tls && <>{nondefaultNumber(options.realityXver) && <Value label={t.xver} value={proxy(options.realityXver)} option="realityXver"/>}
          {nondefaultNumber(options.realityMaxTimeDiff) && <Value label={t.time} value={options.realityMaxTimeDiff} option="realityMaxTimeDiff"/>}
          {options.realityMinClientVer && <Value label={t.minVersion} value={options.realityMinClientVer} option="realityMinClientVer"/>}
          {options.realityMaxClientVer && <Value label={t.maxVersion} value={options.realityMaxClientVer} option="realityMaxClientVer"/>}
          {options.realityShow && <Value label={t.show} value={t.on} option="realityShow"/>}</>}
        {(options.clientPresent || options.clientFingerprint || options.clientSpiderX) && <><Value label={t.fingerprint} value={options.clientFingerprint || "chrome"} option="clientFingerprint"/>{!tls && <Value label={t.spider} value={options.clientSpiderX || "/"} option="clientSpiderX"/>}</>}
        {publicKey && <Value label={t.publicKey} value={publicKey} field="publicKey"/>}
      </dl>
      {tls && (tlsMetadata ? <TLSCertificateSummary metadata={tlsMetadata} language={language}/> : <p className="creation-help">{language === "fa" ? "پس از بررسی کامل، مشخصات عمومی گواهی اینجا نمایش داده می‌شود." : "Check the complete setup to review the certificate’s public metadata."}</p>)}
      {publicKey && <div className="creation-review-key"><button type="button" disabled={disabled} onClick={() => { if (!navigator.clipboard) { setCopyState(t.copyFailed); return; } void navigator.clipboard.writeText(publicKey).then(() => setCopyState(t.copied), () => setCopyState(t.copyFailed)); }}><Copy size={14}/>{t.copyKey}</button><span role="status">{copyState}</span></div>}
    </section>}
    <section className="creation-review-card" data-review-section="routes">
      <header><h4>{t.routes}</h4>{edit("swap", t.editRoutes)}</header>
      <p className="creation-review-meta">{t.nodes}: <bdi>{rows.length}</bdi> · {t.hosts}: <bdi>{hostCount}</bdi></p>
      {!rows.length && <p className="creation-help">{t.noNodes}</p>}
      <div className="creation-review-nodes">{rows.map(row => <article key={row.nodeID} data-review-node={row.nodeID}>
        <header><strong>{creationNodeName(nodes, row.nodeID)}</strong><bdi>{row.nodeID}</bdi>{creationWaitingHosts(row) && <span className="creation-waiting">{t.waiting}</span>}</header>
        <dl className="creation-review-values"><Value label={t.listen} value={row.listenIP}/></dl>
        {(["direct", "tunnel", "swap"] as const).map(path => {
          const enabled = row[path].enabled;
          const retained = !enabled && (path === "swap" ? Object.entries(row.swap).some(([key, value]) => key !== "enabled" && key !== "sourceInstallationID" && value !== "") : row[path].port !== "" || row[path].hosts !== "" || !!row[path].endpoints?.length);
          return <div key={path} className="creation-review-path" data-review-path={path} data-path-enabled={enabled} data-path-retained={retained}>
            <h5>{t[path]}<span>{enabled ? t.on : retained ? t.draftOff : t.off}</span></h5>
            {enabled ? pathValues(row, path) : retained && <details className="creation-review-disclosure"><summary>{t.draft}</summary>{pathValues(row, path)}</details>}
            {enabled && path === "swap" && row.tunnel.enabled && row.swap.destinationPort !== "" && row.swap.destinationPort === row.tunnel.port && <p className="creation-help">{t.reuse}</p>}
          </div>;
        })}
      </article>)}</div>
      {checkedEndpoints && <details className="creation-review-disclosure" data-checked-endpoints><summary>{t.checkedEndpoints} <bdi>{checkedEndpoints.length}</bdi></summary><div className="creation-checked-endpoints"><p className="creation-help">{t.checkedHint}</p>{checkedEndpoints.map((endpoint, index) => <div className="creation-review-endpoint" key={index} data-checked-endpoint={index}><h6>{endpoint.label}<span>{endpoint.enabled ? t.on : t.endpointOff}</span></h6><dl className="creation-review-values"><Value label={t.nodes} value={creationNodeName(nodes, endpoint.node_id)}/><Value label={t.path} value={t[endpoint.path]}/><Value label={t.addresses} value={endpoint.address}/><Value label={t.publicPort} value={endpoint.port}/><Value label={t.port} value={endpoint.listen_port}/><Value label={t.sni} value={endpoint.server_name}/><Value label={t.transport} value={`${transportLabel(endpoint.transport)} / ${endpoint.security === "tls" ? "TLS" : "REALITY"}`}/><Value label={t.fingerprint} value={endpoint.fingerprint}/>{endpoint.security !== "tls" && <Value label={t.spider} value={endpoint.spider_x}/>}{endpoint.grpc_service && <Value label={t.service} value={endpoint.grpc_service}/ >}{endpoint.grpc_authority && <Value label={t.authority} value={endpoint.grpc_authority}/ >}{endpoint.grpc_multi_mode && <Value label={t.multiMode} value={t.on}/ >}{endpoint.transport === "xhttp" && <><Value label={t.xhttpPath} value={endpoint.xhttp?.path || "/"}/><Value label={t.xhttpHost} value={endpoint.xhttp?.host || t.default}/><Value label={t.xhttpMode} value={endpoint.xhttp?.mode || "auto"}/><Value label={t.xhttpPadding} value={endpoint.xhttp?.x_padding_bytes || "100-1000"}/></>}{(endpoint.transport === "ws" || endpoint.transport === "httpupgrade") && <><Value label={`${transportLabel(endpoint.transport)} Path`} value={(endpoint.transport === "ws" ? endpoint.websocket?.path : endpoint.http_upgrade?.path) || "/"}/><Value label={`${transportLabel(endpoint.transport)} Host`} value={(endpoint.transport === "ws" ? endpoint.websocket?.host : endpoint.http_upgrade?.host) || t.default}/></>}</dl></div>)}</div></details>}
    </section>
    {!state.spec && <section className="creation-review-card" data-review-section="advanced">
      <header><h4>{t.advanced}</h4>{edit("advanced", t.editAdvanced)}</header>
      <div className="creation-review-option" data-review-option="sniffing" data-sniffing-enabled={options.sniffing.enabled} data-sniffing-retained={sniffingRetained}>
        <h5>{t.sniffing}<span>{options.sniffing.enabled ? t.on : sniffingRetained ? t.retained : t.off}</span></h5>
        {sniffingConfigured ? <>
          <dl className="creation-review-values"><Value label={t.overrides} value={options.sniffing.destOverride.map(protocol => protocol === "fakedns" ? "FakeDNS" : protocol.toUpperCase()).join(", ") || t.none}/><Value label={t.routeOnly} value={options.sniffing.routeOnly ? t.on : t.off}/><Value label={t.metadataOnly} value={options.sniffing.metadataOnly ? t.on : t.off}/></dl>
          {([[t.domains, domains], [t.ips, ips]] as Array<[string, string[]]>).map(([label, rules]) => <details key={label} className="creation-review-disclosure"><summary>{label} <bdi>{rules.length}</bdi></summary><p dir="ltr">{rules.join("\n") || t.none}</p></details>)}
        </> : <p className="creation-help">{t.noSniffing}</p>}
      </div>
      {!tls && <div className="creation-review-option" data-review-option="fallbacks">
        <h5>{t.fallbacks}<bdi>{options.fallbacks.length}</bdi></h5>
        {!options.fallbacks.length && <p className="creation-help">{t.noFallbacks}</p>}
        {options.fallbacks.length > 0 && profile.transport !== "tcp" && <p className="creation-help creation-waiting">{t.tcp}</p>}
        {options.fallbacks.map((row, index) => <div key={row.key} className="creation-review-fallback" data-review-fallback={index}><h6><bdi dir="ltr">{t.fallback} {index + 1}</bdi></h6><dl className="creation-review-values"><Value label={t.destination} value={row.dest}/><Value label={t.path} value={row.path || t.default}/><Value label={t.proxy} value={proxy(row.xver)}/>{row.alpn && <Value label="ALPN" value={row.alpn}/>}</dl></div>)}
      </div>}
    </section>}
  </div>;
}
