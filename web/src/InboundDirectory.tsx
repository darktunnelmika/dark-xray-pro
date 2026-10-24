import { useEffect, useMemo, useState } from "react";
import { ArrowUpRight, Copy, ListFilter, Plus, Search } from "lucide-react";
import type { Inbound, InboundOverview } from "./inboundTypes";
import { creationNodeName, type CreationCapabilities } from "./InboundCreationModel";
import { navigateURL } from "./navigation";
import { transportLabel } from "./inboundNetworkOptions";

const words = {
  en: {
    add: "Add Inbound", filters: "Filters", search: "Search Inbounds", placeholder: "Name or ID…", status: "Status", transport: "Transport", node: "Node",
    all: "All", enabled: "Enabled", disabled: "Disabled", archived: "Archived", promoted: "Prepared for LAB", planned: "Saved paused setup", waitingHost: "Waiting for Host", plannedNodes: "Planned Nodes", assignedNodes: "Assigned Nodes", unknownPlanNodes: "Open the saved setup for its Nodes", name: "Inbound", nodes: "Nodes", clients: "Clients", actions: "Actions",
    open: "Open", copy: "Copy settings", previous: "Previous", next: "Next", results: "results", page: "Page", of: "of", empty: "No Inbounds match these filters.",
    none: "No Inbounds yet. Add an Inbound and configure its Nodes and paths.", unattached: "No Node assigned", clear: "Clear filters", swap: "Select an Inbound to manage its saved Dark Swap route drafts.",
  },
  fa: {
    add: "افزودن اینباند", filters: "فیلترها", search: "جست‌وجوی اینباند", placeholder: "نام یا شناسه…", status: "وضعیت", transport: "ترنسپورت", node: "نود",
    all: "همه", enabled: "فعال", disabled: "غیرفعال", archived: "آرشیوشده", promoted: "آماده‌شده برای LAB", planned: "طرح ذخیره‌شدهٔ غیرفعال", waitingHost: "در انتظار هاست", plannedNodes: "نودهای طرح", assignedNodes: "نودهای اختصاص‌یافته", unknownPlanNodes: "نودها را در تنظیمات ذخیره‌شده ببین", name: "اینباند", nodes: "نودها", clients: "کلاینت‌ها", actions: "عملیات",
    open: "جزئیات", copy: "کپی تنظیمات", previous: "قبلی", next: "بعدی", results: "نتیجه", page: "صفحه", of: "از", empty: "اینباندی با این فیلترها پیدا نشد.",
    none: "هنوز اینباندی نیست. یک اینباند بساز و نودها و مسیرهایش را مشخص کن.", unattached: "بدون نود مقصد", clear: "پاک‌کردن فیلترها", swap: "برای مدیریت پیش‌نویس مسیر دارک سواپ، اینباند را انتخاب کن.",
  },
} as const;

export default function InboundDirectory({ data, plans, language, blocked, swapWorkspace, onOpen, onCreate }: {
  data: InboundOverview; language: "fa" | "en"; blocked: boolean; swapWorkspace: boolean;
  plans?: CreationCapabilities["plans"];
  onOpen: (inbound: Inbound) => void; onCreate: (source?: Inbound) => void;
}) {
  const t = words[language];
  const [query, setQuery] = useState(() => new URLSearchParams(location.search).get("q") ?? "");
  const [status, setStatus] = useState(() => new URLSearchParams(location.search).get("state") ?? "all");
  const [transport, setTransport] = useState(() => new URLSearchParams(location.search).get("transport") ?? "all");
  const [node, setNode] = useState(() => new URLSearchParams(location.search).get("node") ?? "all");
  const [page, setPage] = useState(() => Math.max(1, Math.floor(Number(new URLSearchParams(location.search).get("page")) || 1)));
  const [filtersOpen, setFiltersOpen] = useState(false);
  const activeFilters = [status, transport, node].filter(value => value !== "all").length;
  useEffect(() => { if (activeFilters) setFiltersOpen(true); }, [status, transport, node]);
  const size = 20;
  const filtered = useMemo(() => data.inbounds.filter(item => {
    const plan = plans?.find(plan => plan.inbound_id === item.id);
    const state = item.archived ? "archived" : plan?.promotion ? "promoted" : item.enabled ? "enabled" : "disabled";
    const pausedPlan = !!plan && !plan.promotion && !item.archived && !item.enabled;
    return (item.name + " " + item.id).toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()) &&
      (status === "all" || status === state || status === "planned" && pausedPlan || status === "waiting_host" && pausedPlan && plan?.status === "WAITING_HOST") && (transport === "all" || transport === item.transport) &&
      (node === "all" || data.targets.some(target => target.inbound_id === item.id && target.node_id === node) || !!plan?.node_ids?.includes(node));
  }).sort((a, b) => b.id - a.id), [data.inbounds, data.targets, plans, query, status, transport, node]);
  const pages = Math.max(1, Math.ceil(filtered.length / size));
  const current = Math.min(page, pages);
  useEffect(() => { if (page !== current) setPage(current); }, [page, current]);
  useEffect(() => {
    const params = new URLSearchParams(location.search);
    for (const [key, value, empty] of [["q", query, ""], ["state", status, "all"], ["transport", transport, "all"], ["node", node, "all"], ["page", String(current), "1"]]) {
      if (value === empty) params.delete(key); else params.set(key, value);
    }
    const suffix = params.toString();
    navigateURL(location.pathname + (suffix ? "?" + suffix : ""), true);
  }, [query, status, transport, node, current]);
  useEffect(() => {
    const sync = () => {
      const params = new URLSearchParams(location.search);
      setQuery(params.get("q") ?? ""); setStatus(params.get("state") ?? "all");
      setTransport(params.get("transport") ?? "all"); setNode(params.get("node") ?? "all");
      setPage(Math.max(1, Math.floor(Number(params.get("page")) || 1)));
    };
    window.addEventListener("popstate", sync); return () => window.removeEventListener("popstate", sync);
  }, []);
  const reset = () => { setQuery(""); setStatus("all"); setTransport("all"); setNode("all"); setPage(1); };
  return <section className="inbound-directory" aria-label="Inbound directory">
    <div className="inbound-directory-toolbar">
      <label className="inbound-directory-search"><span>{t.search}</span><div><Search size={17}/><input value={query} placeholder={t.placeholder} onChange={event => { setQuery(event.target.value); setPage(1); }}/></div></label>
      <button type="button" className="native-primary" disabled={blocked} onClick={() => onCreate()}><Plus size={17}/>{t.add}</button>
      <button type="button" className="inbound-filter-toggle" aria-expanded={filtersOpen} aria-controls="inbound-directory-filters" onClick={() => setFiltersOpen(value => !value)}><ListFilter size={17}/>{t.filters}{activeFilters > 0 && <span>({activeFilters.toLocaleString(language === "fa" ? "fa-IR" : "en-US")})</span>}</button>
    </div>
    {swapWorkspace && <p className="inbound-directory-help">{t.swap}</p>}
    <div className="inbound-filter-bar"><div className="inbound-directory-filters" id="inbound-directory-filters" data-expanded={filtersOpen}>
      <label>{t.status}<select aria-label={t.status} value={status} onChange={event => { setStatus(event.target.value); setPage(1); }}><option value="all">{t.all}</option><option value="enabled">{t.enabled}</option><option value="disabled">{t.disabled}</option><option value="planned">{t.planned}</option><option value="promoted">{t.promoted}</option><option value="waiting_host">{t.waitingHost}</option><option value="archived">{t.archived}</option></select></label>
      <label>{t.transport}<select aria-label={t.transport} value={transport} onChange={event => { setTransport(event.target.value); setPage(1); }}><option value="all">{t.all}</option>{["tcp", "grpc", "xhttp", "ws", "httpupgrade"].map(transport => <option key={transport} value={transport}>{transportLabel(transport)}</option>)}</select></label>
      <label>{t.node}<select aria-label={t.node} value={node} onChange={event => { setNode(event.target.value); setPage(1); }}><option value="all">{t.all}</option>{data.nodes.map(item => <option key={item.node_id} value={item.node_id}>{creationNodeName(data.nodes, item.node_id)}</option>)}</select></label>
      {(query || status !== "all" || transport !== "all" || node !== "all") && <button type="button" className="native-text-action" onClick={reset}>{t.clear}</button>}
    </div><span className="inbound-result-count" role="status">{filtered.length.toLocaleString(language === "fa" ? "fa-IR" : "en-US")} {t.results}</span>
    </div>
    <div className="inbound-table-wrap"><table className="inbound-table">
      <thead><tr><th scope="col">{t.name}</th><th scope="col">{t.transport}</th><th scope="col">{t.nodes}</th><th scope="col">{t.clients}</th><th scope="col">{t.status}</th><th scope="col">{t.actions}</th></tr></thead>
      <tbody>{filtered.slice((current - 1) * size, current * size).map(item => {
        const attached = data.targets.filter(target => target.inbound_id === item.id);
        const plan = plans?.find(plan => plan.inbound_id === item.id);
        const state = item.archived ? "archived" : plan?.promotion ? "promoted" : item.enabled ? "enabled" : "disabled";
        const pausedPlan = !!plan && !plan.promotion && state === "disabled";
        return <tr key={item.id}>
          <th scope="row" data-label={t.name}><button type="button" className="inbound-name" aria-label={item.name} onClick={() => onOpen(item)}><strong>{item.name}</strong><bdi dir="ltr">#{item.id}</bdi></button></th>
          <td data-label={t.transport}><bdi dir="ltr">{transportLabel(item.transport)} / {item.security === "tls" ? "TLS" : "REALITY"}</bdi></td>
          <td data-label={t.nodes}>{plan && <div className="inbound-node-names" data-planned-nodes><strong>{t.plannedNodes}: </strong>{plan.node_ids ? plan.node_ids.map(id => creationNodeName(data.nodes, id)).join(" · ") : t.unknownPlanNodes}</div>}
            {(!plan || attached.length > 0) && <div className="inbound-node-names" data-assigned-nodes>{plan && <strong>{t.assignedNodes}: </strong>}{attached.length ? attached.map(target => creationNodeName(data.nodes, target.node_id)).join(" · ") : t.unattached}</div>}</td>
          <td data-label={t.clients}>{item.bound_clients.toLocaleString(language === "fa" ? "fa-IR" : "en-US")}</td>
          <td data-label={t.status}><span className={"inbound-state inbound-state-" + state} data-plan-state={pausedPlan || undefined}>{pausedPlan ? plan.status === "WAITING_HOST" ? t.waitingHost : t.planned : t[state]}</span></td>
          <td data-label={t.actions}><div className="inbound-row-actions"><button type="button" onClick={() => onOpen(item)} aria-label={t.open + " " + item.name}><ArrowUpRight size={16}/><span>{t.open}</span></button><button type="button" disabled={blocked} onClick={() => onCreate(item)} aria-label={t.copy + " " + item.name}><Copy size={16}/></button></div></td>
        </tr>;
      })}</tbody>
    </table></div>
    {!filtered.length && <div className="inbound-directory-empty">{data.inbounds.length ? t.empty : t.none}</div>}
    <nav className="inbound-pagination" aria-label="Inbound pagination"><span>{t.page} {current.toLocaleString(language === "fa" ? "fa-IR" : "en-US")} {t.of} {pages.toLocaleString(language === "fa" ? "fa-IR" : "en-US")}</span><div><button type="button" disabled={current <= 1} onClick={() => setPage(current - 1)}>{t.previous}</button><button type="button" disabled={current >= pages} onClick={() => setPage(current + 1)}>{t.next}</button></div></nav>
  </section>;
}
