import { ArrowRight, ArrowRightLeft, ChevronDown, Globe2, Link2, Plus, Server } from "lucide-react";
import type { InboundNode, InboundTarget } from "./inboundTypes";
import { creationHosts, creationNodeName, creationPort, creationWaitingHosts, newCreationNode, type CreationIssue, type CreationNode, type CreationState } from "./InboundCreationModel";

type Language = "en" | "fa";
const messages: Record<Language, Record<string, string>> = {
  en: {
    name_required: "Enter an Inbound name.", grpc_service_invalid: "Use 1–64 letters, numbers, underscores or hyphens for the gRPC service.",
    target_invalid: "Enter a REALITY target with a valid port.", server_names_required: "Enter at least one server name.",
    short_ids_invalid: "Use 1–16 unique Short IDs, each 2–16 lowercase hexadecimal characters with an even length.",
    node_required: "Select at least one destination Node.", pairing_changed: "This Node’s installation changed. Remove and select it again.",
    listen_ip_required: "Enter this destination Node’s listening IP.", path_required: "Enable at least one connection path.",
    port_invalid: "Listener ports must be whole numbers from 1024 to 65535.", entry_port_invalid: "The external entry port must be from 1 to 65535.",
    host_invalid: "Use a host name or IPv4 address without a scheme, path or port.", host_duplicate: "Remove duplicate addresses from this path.",
    host_limit: "Use at most 64 unique addresses for each destination Node.", source_required: "Choose a different, currently paired source Node.",
    port_reserved: "Ports 2087, 8443 and 24443 are reserved.", port_collision: "Direct and Tunnel need separate ports; Swap may reuse this Node’s Tunnel port.",
    source_port_collision: "The source relay port conflicts with another listener in this plan.", route_cycle: "These Swap links form a loop. Change the source Node.",
    entry_mapping_conflict: "This public entry address and port already map to a different source relay. Use a distinct entry mapping.",
    reason_required: "Enter a change note of up to 200 characters.",
    source_listen_ip_required: "Enter the IP assigned to the source relay Node.", source_listen_ip_conflict: "The source relay IP must match this Node’s listening IP in the plan.",
    listen_ip_invalid: "Use a valid, assigned listening IP; wildcard, multicast and link-local IPs are not supported.",
    listener_port_invalid_or_reserved: "Use a listener port from 1024 to 65535, excluding 2087, 8443 and 24443.", swap_port_invalid_or_reserved: "Check the relay and destination listener ports and the external entry port.",
    existing_listener_port_collision: "This port already belongs to another Inbound or saved setup on the Node.", listener_port_collision: "Two listeners in this setup use the same port on the Node.",
    node_binding_conflict: "Use the same installation and listening IP for every role of this Node.", existing_node_binding_conflict: "This Node already has a different assigned listening IP or installation.",
    host_limit_or_invalid: "Check the Host format and the limit of 64 unique addresses per Node.", profile_invalid: "Check the profile name, REALITY target, server names and Short IDs.",
    profile_changed_or_materialized: "The saved profile changed or was assigned outside this editor; reload it to review.", plan_revision_conflict: "A newer version of this setup was saved. Reload it before continuing.",
    node_count_invalid: "Select between 1 and 64 destination Nodes.", node_identity_invalid: "Re-select the affected Node to use its current installation.",
    swap_source_invalid: "Choose a currently paired source Node different from the destination.", topology_invalid: "The complete connection topology could not be compiled. Review the listening IPs and ports.",
    inbound_id_exists: "This Inbound identity already exists. Reload to check the saved setup.", inbound_limit: "The server’s Inbound limit has been reached.", reason_invalid: "Enter a change note of up to 200 characters.",
  },
  fa: {
    name_required: "نام اینباند را وارد کن.", grpc_service_invalid: "برای سرویس gRPC از ۱ تا ۶۴ حرف انگلیسی، عدد، زیرخط یا خط تیره استفاده کن.",
    target_invalid: "مقصد REALITY را همراه یک پورت معتبر وارد کن.", server_names_required: "حداقل یک نام سرور وارد کن.",
    short_ids_invalid: "۱ تا ۱۶ شناسهٔ یکتا وارد کن؛ هر Short ID باید ۲ تا ۱۶ رقم هگز کوچک با طول زوج داشته باشد.",
    node_required: "حداقل یک نود مقصد را انتخاب کن.", pairing_changed: "نصب این نود تغییر کرده؛ نود را حذف و دوباره انتخاب کن.",
    listen_ip_required: "IP شنود این نود مقصد را وارد کن.", path_required: "حداقل یک مسیر اتصال را فعال کن.",
    port_invalid: "پورت شنود باید عدد صحیحی از ۱۰۲۴ تا ۶۵۵۳۵ باشد.", entry_port_invalid: "پورت ورودی بیرونی باید از ۱ تا ۶۵۵۳۵ باشد.",
    host_invalid: "نام هاست یا IPv4 را بدون پروتکل، مسیر و پورت وارد کن.", host_duplicate: "آدرس‌های تکراری این مسیر را حذف کن.",
    host_limit: "برای هر نود مقصد حداکثر ۶۴ آدرس یکتا وارد کن.", source_required: "یک نود مبدأ متصل و متفاوت از مقصد انتخاب کن.",
    port_reserved: "پورت‌های ۲۰۸۷، ۸۴۴۳ و ۲۴۴۴۳ رزرو هستند.", port_collision: "پورت مستقیم و تونل باید جدا باشند؛ دارک‌سواپ می‌تواند پورت تونل همین نود را استفاده کند.",
    source_port_collision: "پورت رلهٔ مبدأ با یک شنوندهٔ دیگر در این تنظیمات تداخل دارد.", route_cycle: "اتصال‌های دارک‌سواپ حلقه ساخته‌اند؛ نود مبدأ را تغییر بده.",
    entry_mapping_conflict: "این آدرس و پورت ورودی بیرونی به رلهٔ مبدأ دیگری متصل است؛ یک نگاشت ورودی جدا انتخاب کن.",
    reason_required: "یادداشت تغییر را با حداکثر ۲۰۰ نویسه وارد کن.",
    source_listen_ip_required: "IP اختصاص‌یافته به نود رلهٔ مبدأ را وارد کن.", source_listen_ip_conflict: "IP رلهٔ مبدأ باید با IP شنود همین نود در تنظیمات یکسان باشد.",
    listen_ip_invalid: "یک IP شنود معتبر و اختصاص‌یافته وارد کن؛ آدرس عمومی شنود، چندپخشی و لینک‌لوکال پشتیبانی نمی‌شود.",
    listener_port_invalid_or_reserved: "پورت شنود باید از ۱۰۲۴ تا ۶۵۵۳۵ و غیر از ۲۰۸۷، ۸۴۴۳ و ۲۴۴۴۳ باشد.", swap_port_invalid_or_reserved: "پورت‌های شنود مبدأ و مقصد و پورت ورودی بیرونی را بررسی کن.",
    existing_listener_port_collision: "این پورت روی نود به اینباند یا تنظیمات ذخیره‌شدهٔ دیگری تعلق دارد.", listener_port_collision: "دو شنونده در این تنظیمات از یک پورت روی همین نود استفاده می‌کنند.",
    node_binding_conflict: "برای تمام نقش‌های این نود، نصب و IP شنود یکسان انتخاب کن.", existing_node_binding_conflict: "این نود از قبل IP شنود یا نصب متفاوتی دارد.",
    host_limit_or_invalid: "قالب آدرس‌ها و سقف ۶۴ هاست یکتا برای هر نود را بررسی کن.", profile_invalid: "نام پروفایل، مقصد REALITY، نام‌های سرور و شناسه‌های کوتاه را بررسی کن.",
    profile_changed_or_materialized: "پروفایل ذخیره‌شده خارج از این ویرایشگر تغییر کرده یا اختصاص یافته؛ نسخهٔ تازه را بارگذاری کن.", plan_revision_conflict: "نسخهٔ تازه‌تری از این تنظیمات ذخیره شده؛ پیش از ادامه دوباره بارگذاری کن.",
    node_count_invalid: "بین ۱ تا ۶۴ نود مقصد انتخاب کن.", node_identity_invalid: "برای استفاده از نصب فعلی، نود مربوط را دوباره انتخاب کن.",
    swap_source_invalid: "یک نود مبدأ متصل و متفاوت از مقصد انتخاب کن.", topology_invalid: "ساخت پیکربندی کامل اتصال ممکن نشد؛ IPهای شنود و پورت‌ها را بررسی کن.",
    inbound_id_exists: "این شناسهٔ اینباند از قبل وجود دارد؛ تنظیمات ذخیره‌شده را بارگذاری و بررسی کن.", inbound_limit: "تعداد اینباندها به سقف مجاز سرور رسیده است.", reason_invalid: "یادداشت تغییر را با حداکثر ۲۰۰ نویسه وارد کن.",
  },
};
export function creationIssueMessage(issue: CreationIssue, language: Language): string { return messages[language][issue.code] ?? (language === "fa" ? "تنظیمات این بخش را بررسی کن." : "Review this section’s settings."); }

const words = {
  en: {
    title: "Destination Nodes", help: "Select the Nodes that will receive this Inbound, then configure each row.", none: "No paired Nodes are available.",
    selected: "selected", select: "Select destination", configure: "Configure", waiting: "Waiting for Host", configured: "Configured", noPath: "No path selected",
    direct: "Direct", tunnel: "Tunnel", swap: "Dark Swap", listen: "Destination listening IP", listenHint: "Use an IP assigned to this Node.",
    directPort: "Direct port", directHosts: "Direct Hosts", tunnelPort: "Tunnel port", tunnelHosts: "Tunnel Hosts", hostsHint: "One address per line or separated with commas. Up to 64 unique Hosts per Node.",
    emptyHost: "You can save this path without a Host. It will wait for an address.", paths: "Connection paths", remove: "Remove this Node and its changes from the setup?",
    source: "Source Node", choose: "Choose a source Node", relay: "Source relay port", entryHost: "External entry address", entryPort: "External entry port", destinationPort: "Destination exit port",
    sourceIP: "Source relay listening IP",
    swapHint: "The external entry reaches a dedicated relay on the source Node, then the destination port.",
    sourceHint: "Select the paired Node that receives the external tunnel connection.", entryHint: "Enter the address and port of your configured external tunnel.",
    useTunnel: "Use this Node’s Tunnel port", reuse: "The destination uses this Node’s existing Tunnel port.", shadow: "A separate destination listener is configured.",
    relayLabel: "Source relay", entryLabel: "External entry", destinationLabel: "Destination", notSet: "Not set", limit: "Select up to 64 destination Nodes.",
  },
  fa: {
    title: "نودهای مقصد", help: "نودهایی که این اینباند را دریافت می‌کنند انتخاب کن و مسیرهای هر ردیف را تنظیم کن.", none: "نود متصلی در دسترس نیست.",
    selected: "انتخاب‌شده", select: "انتخاب مقصد", configure: "تنظیم", waiting: "در انتظار هاست", configured: "تنظیم‌شده", noPath: "مسیری انتخاب نشده",
    direct: "مستقیم", tunnel: "تونل", swap: "دارک‌سواپ", listen: "IP شنود نود مقصد", listenHint: "یک IP اختصاص‌یافته به همین نود وارد کن.",
    directPort: "پورت مستقیم", directHosts: "هاست‌های مستقیم", tunnelPort: "پورت تونل", tunnelHosts: "هاست‌های تونل", hostsHint: "هر آدرس در یک خط یا با کاما؛ حداکثر ۶۴ هاست یکتا برای هر نود.",
    emptyHost: "می‌توانی این مسیر را بدون هاست ذخیره کنی؛ تا افزودن آدرس در انتظار می‌ماند.", paths: "مسیرهای اتصال", remove: "این نود و تغییراتش از تنظیمات حذف شود؟",
    source: "نود مبدأ", choose: "انتخاب نود مبدأ", relay: "پورت رلهٔ مبدأ", entryHost: "آدرس ورودی بیرونی", entryPort: "پورت ورودی بیرونی", destinationPort: "پورت خروجی مقصد",
    sourceIP: "IP شنود رلهٔ مبدأ",
    swapHint: "ورودی بیرونی به رلهٔ اختصاصی نود مبدأ و سپس به پورت نود مقصد متصل می‌شود.",
    sourceHint: "نود متصلی را انتخاب کن که اتصال تونل بیرونی را دریافت می‌کند.", entryHint: "آدرس و پورت تونل بیرونیِ تنظیم‌شدهٔ خودت را وارد کن.",
    useTunnel: "استفاده از پورت تونل همین نود", reuse: "مقصد از پورت تونل همین نود استفاده می‌کند.", shadow: "یک شنوندهٔ جدا برای مقصد تنظیم می‌شود.",
    relayLabel: "رلهٔ مبدأ", entryLabel: "ورودی بیرونی", destinationLabel: "مقصد", notSet: "تنظیم نشده", limit: "حداکثر ۶۴ نود مقصد انتخاب کن.",
  },
};

export default function InboundCreationNodes({ language, state, nodes, targets = [], disabled, expanded, onExpanded, onChange, issues = [] }: {
  language: Language; state: CreationState; nodes: InboundNode[]; disabled: boolean; expanded: string;
  targets?: InboundTarget[];
  onExpanded: (id: string) => void; onChange: (value: CreationState) => void; issues?: CreationIssue[];
}) {
  const t = words[language];
  const update = (id: string, value: CreationNode) => onChange({ ...state, nodes: { ...state.nodes, [id]: value } });
  function select(node: InboundNode, checked: boolean) {
    if (!checked && !window.confirm(t.remove)) return;
    const existing = state.nodes[node.node_id];
    onChange({ ...state, selected: checked ? [...state.selected, node.node_id] : state.selected.filter(id => id !== node.node_id),
      nodes: { ...state.nodes, [node.node_id]: existing?.installationID === node.installation_id ? existing : newCreationNode(node) } });
    if (checked) onExpanded(node.node_id);
    else if (expanded === node.node_id) onExpanded(state.selected.find(id => id !== node.node_id) ?? "");
  }
  const visibleNodes = [...nodes, ...state.selected.filter(id => !nodes.some(node => node.node_id === id)).map(id => ({ node_id: id, name: id, installation_id: state.nodes[id].installationID }))];
  return <section className="creation-nodes" aria-label={t.title}>
    <header className="creation-section-heading"><div><h3>{t.title}</h3><p>{t.help}</p></div><span className="creation-count"><bdi>{state.selected.length}</bdi> {t.selected}</span></header>
    {!nodes.length && <p className="creation-empty"><Server size={22}/>{t.none}</p>}
    {state.selected.length >= 64 && <p className="creation-help">{t.limit}</p>}
    <div className="creation-node-list">
      {visibleNodes.map(node => {
        const id = node.node_id, selected = state.selected.includes(id), row = state.nodes[id];
        const open = selected && expanded === id;
        const nodeIssues = issues.filter(issue => issue.nodeID === id);
        return <article key={id} className="creation-node" data-selected={selected} data-open={open} data-node-id={id}>
          <div className="creation-node-heading">
            <label className="creation-node-select"><input aria-label={`${t.select}: ${creationNodeName(visibleNodes, id)}`} type="checkbox" checked={selected}
              disabled={disabled || !selected && state.selected.length >= 64} onChange={event => select(node, event.target.checked)}/><Server size={17}/><span>{creationNodeName(visibleNodes, id)}</span></label>
            {selected && row && <><div className="creation-node-paths" aria-label={t.paths}>
              {row.direct.enabled && <span title={t.direct}><Globe2 size={12}/><bdi>{row.direct.port || "—"}</bdi></span>}
              {row.tunnel.enabled && <span title={t.tunnel}><Link2 size={12}/><bdi>{row.tunnel.port || "—"}</bdi></span>}
              {row.swap.enabled && <span title={t.swap}><ArrowRightLeft size={12}/><bdi>{row.swap.destinationPort || "—"}</bdi></span>}
            </div><button type="button" className="creation-expand" disabled={disabled} aria-label={`${t.configure}: ${creationNodeName(visibleNodes, id)}`} aria-expanded={open} onClick={() => onExpanded(open ? "" : id)}><ChevronDown size={17}/></button></>}
          </div>
          {selected && row && <div className="creation-row-state"><span className={creationWaitingHosts(row) ? "creation-waiting" : ""}>{nodeIssues.length ? `${nodeIssues.length} ${language === "fa" ? "مورد برای بررسی" : "items to review"}` : creationWaitingHosts(row) ? t.waiting : row.direct.enabled || row.tunnel.enabled || row.swap.enabled ? t.configured : t.noPath}</span></div>}
          {open && row && <div className="creation-node-body">
            <label className="creation-field"><span>{t.listen}</span><input aria-label={`${t.listen}: ${id}`} data-creation-field="listenIP" dir="ltr" value={row.listenIP} disabled={disabled} onChange={event => update(id, { ...row, listenIP: event.target.value })} placeholder="203.0.113.10" autoComplete="off"/><small>{t.listenHint}</small></label>
            <div className="creation-path-toggles" role="group" aria-label={`${t.paths}: ${id}`}>
              {(["direct", "tunnel", "swap"] as const).map(path => { const Icon = path === "direct" ? Globe2 : path === "tunnel" ? Link2 : ArrowRightLeft; return <label key={path} data-enabled={row[path].enabled}><span><Icon size={15}/>{t[path]}</span><input type="checkbox" aria-label={`${t[path]}: ${id}`} checked={row[path].enabled} disabled={disabled} onChange={event => update(id, { ...row, [path]: { ...row[path], enabled: event.target.checked } })}/></label>; })}
            </div>
            {(["direct", "tunnel"] as const).map(path => row[path].enabled && <fieldset className="creation-path-fields" disabled={disabled} key={path}>
              <legend>{path === "direct" ? <Globe2 size={15}/> : <Link2 size={15}/>} {t[path]}</legend>
              <div className="creation-port-hosts"><label className="creation-field"><span>{t[`${path}Port`]}</span><input aria-label={`${t[`${path}Port`]}: ${id}`} data-creation-field={`${path}Port`} dir="ltr" inputMode="numeric" value={row[path].port} onChange={event => update(id, { ...row, [path]: { ...row[path], port: event.target.value } })} placeholder={path === "direct" ? "10443" : "1185"}/></label>
                <label className="creation-field"><span>{t[`${path}Hosts`]}<small>{creationHosts(row[path].hosts).length}</small></span><textarea aria-label={`${t[`${path}Hosts`]}: ${id}`} data-creation-field={`${path}Hosts`} dir="ltr" rows={2} spellCheck={false} value={row[path].hosts} onChange={event => update(id, { ...row, [path]: { ...row[path], hosts: event.target.value } })} placeholder="example.org"/></label></div>
              {!creationHosts(row[path].hosts).length && <p className="creation-help creation-waiting">{t.emptyHost}</p>}
            </fieldset>)}
            {(row.direct.enabled || row.tunnel.enabled) && <p className="creation-help">{t.hostsHint}</p>}
            {row.swap.enabled && <fieldset className="creation-path-fields creation-swap-fields" disabled={disabled}>
              <legend><ArrowRightLeft size={15}/> {t.swap}</legend><p className="creation-help">{t.swapHint}</p>
              <div className="creation-field-grid"><label className="creation-field"><span>{t.source}</span><select aria-label={`${t.source}: ${id}`} data-creation-field="sourceNode" value={row.swap.sourceNodeID} onChange={event => {
                const source = nodes.find(item => item.node_id === event.target.value);
                const selectedSource = source && state.selected.includes(source.node_id) ? state.nodes[source.node_id] : undefined;
                const savedIPs = new Set(targets.filter(target => target.node_id === source?.node_id && target.installation_id === source?.installation_id).map(target => target.listen_ip));
                const sourceListenIP = selectedSource?.listenIP || (savedIPs.size === 1 ? [...savedIPs][0] : "");
                update(id, { ...row, swap: { ...row.swap, sourceNodeID: source?.node_id ?? "", sourceInstallationID: source?.installation_id ?? "", sourceListenIP } });
              }}><option value="">{t.choose}</option>{nodes.filter(item => item.node_id !== id).map(item => <option key={item.node_id} value={item.node_id}>{creationNodeName(nodes, item.node_id)}</option>)}</select><small>{t.sourceHint}</small></label>
                <label className="creation-field"><span>{t.relay}</span><input aria-label={`${t.relay}: ${id}`} data-creation-field="sourcePort" dir="ltr" inputMode="numeric" value={row.swap.sourcePort} onChange={event => update(id, { ...row, swap: { ...row.swap, sourcePort: event.target.value } })} placeholder="16443"/></label></div>
              <label className="creation-field"><span>{t.sourceIP}</span><input aria-label={`${t.sourceIP}: ${id}`} data-creation-field="sourceListenIP" dir="ltr" value={row.swap.sourceListenIP} onChange={event => update(id, { ...row, swap: { ...row.swap, sourceListenIP: event.target.value } })} placeholder="203.0.113.20" autoComplete="off"/></label>
              <div className="creation-field-grid"><label className="creation-field"><span>{t.entryHost}</span><input aria-label={`${t.entryHost}: ${id}`} data-creation-field="entryHost" dir="ltr" value={row.swap.entryHost} onChange={event => update(id, { ...row, swap: { ...row.swap, entryHost: event.target.value } })} placeholder="entry.example.org" autoComplete="off"/><small>{t.entryHint}</small></label>
                <label className="creation-field"><span>{t.entryPort}</span><input aria-label={`${t.entryPort}: ${id}`} data-creation-field="entryPort" dir="ltr" inputMode="numeric" value={row.swap.entryPort} onChange={event => update(id, { ...row, swap: { ...row.swap, entryPort: event.target.value } })} placeholder="443"/></label></div>
              <div className="creation-field-grid"><label className="creation-field"><span>{t.destinationPort}</span><input aria-label={`${t.destinationPort}: ${id}`} data-creation-field="swapPort" dir="ltr" inputMode="numeric" value={row.swap.destinationPort} onChange={event => update(id, { ...row, swap: { ...row.swap, destinationPort: event.target.value } })} placeholder="18443"/></label>
                <div className="creation-reuse">{row.tunnel.enabled && creationPort(row.tunnel.port) > 0 && <button type="button" onClick={() => update(id, { ...row, swap: { ...row.swap, destinationPort: row.tunnel.port } })}><Plus size={14}/>{t.useTunnel}</button>}<small>{row.tunnel.enabled && creationPort(row.tunnel.port) && row.swap.destinationPort === row.tunnel.port ? t.reuse : t.shadow}</small></div></div>
              <div className="creation-relay-map"><div><span>{t.entryLabel}</span><bdi dir="ltr">{row.swap.entryHost || t.notSet}:{row.swap.entryPort || "—"}</bdi></div><ArrowRight size={14}/><div><span>{t.relayLabel}</span><bdi>{row.swap.sourceNodeID ? creationNodeName(nodes, row.swap.sourceNodeID) : t.notSet} : {row.swap.sourcePort || "—"}</bdi></div><ArrowRight size={14}/><div><span>{t.destinationLabel}</span><bdi>{creationNodeName(nodes, id)} : {row.swap.destinationPort || "—"}</bdi></div></div>
            </fieldset>}
            {nodeIssues.length > 0 && <ul className="creation-issues" role="alert">{nodeIssues.map((issue, index) => <li key={index}>{creationIssueMessage(issue, language)}</li>)}</ul>}
          </div>}
        </article>;
      })}
    </div>
  </section>;
}
