import { useEffect, useMemo, useState } from "react";
import { ArrowUpRight, Copy, ListFilter, Plus, Search } from "lucide-react";
import type { Inbound, InboundOverview } from "./inboundTypes";
import { navigateURL } from "./navigation";

const words = {
  en: {
    add: "Add Inbound", filters: "Filters", search: "Search Inbounds", placeholder: "Name or ID…", status: "Status", transport: "Transport", node: "Node",
    all: "All", enabled: "Enabled", disabled: "Disabled", archived: "Archived", name: "Inbound", nodes: "Nodes", clients: "Clients", actions: "Actions",
    open: "Open", copy: "Copy settings", previous: "Previous", next: "Next", results: "results", page: "Page", of: "of", empty: "No Inbounds match these filters.",
    none: "No Inbounds yet. Add an Inbound, then assign its Nodes and routes.", unattached: "No Node assigned", clear: "Clear filters", swap: "Select an Inbound to manage its saved Dark Swap route drafts.",
  },
  fa: {
    add: "افزودن اینباند", filters: "فیلترها", search: "جست‌وجوی اینباند", placeholder: "نام یا شناسه…", status: "وضعیت", transport: "ترنسپورت", node: "نود",
    all: "همه", enabled: "فعال", disabled: "غیرفعال", archived: "آرشیوشده", name: "اینباند", nodes: "نودها", clients: "کلاینت‌ها", actions: "عملیات",
    open: "جزئیات", copy: "کپی تنظیمات", previous: "قبلی", next: "بعدی", results: "نتیجه", page: "صفحه", of: "از", empty: "اینباندی با این فیلترها پیدا نشد.",
    none: "هنوز اینباندی نیست. یک اینباند بساز و سپس نود و مسیرهایش را مشخص کن.", unattached: "بدون نود مقصد", clear: "پاک‌کردن فیلترها", swap: "برای مدیریت پیش‌نویس مسیر دارک سواپ، اینباند را انتخاب کن.",
  },
} as const;

export default function InboundDirectory({ data, language, blocked, swapWorkspace, onOpen, onCreate }: {
  data: InboundOverview; language: "fa" | "en"; blocked: boolean; swapWorkspace: boolean;
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
    const state = item.archived ? "archived" : item.enabled ? "enabled" : "disabled";
    return (item.name + " " + item.id).toLocaleLowerCase().includes(query.trim().toLocaleLowerCase()) &&
      (status === "all" || status === state) && (transport === "all" || transport === item.transport) &&
      (node === "all" || data.targets.some(target => target.inbound_id === item.id && target.node_id === node));
  }).sort((a, b) => b.id - a.id), [data.inbounds, data.targets, query, status, transport, node]);
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
      <label>{t.status}<select aria-label={t.status} value={status} onChange={event => { setStatus(event.target.value); setPage(1); }}><option value="all">{t.all}</option><option value="enabled">{t.enabled}</option><option value="disabled">{t.disabled}</option><option value="archived">{t.archived}</option></select></label>
      <label>{t.transport}<select aria-label={t.transport} value={transport} onChange={event => { setTransport(event.target.value); setPage(1); }}><option value="all">{t.all}</option><option value="tcp">TCP / REALITY</option><option value="grpc">gRPC / REALITY</option></select></label>
      <label>{t.node}<select aria-label={t.node} value={node} onChange={event => { setNode(event.target.value); setPage(1); }}><option value="all">{t.all}</option>{data.nodes.map(item => <option key={item.node_id} value={item.node_id}>{item.name}</option>)}</select></label>
      {(query || status !== "all" || transport !== "all" || node !== "all") && <button type="button" className="native-text-action" onClick={reset}>{t.clear}</button>}
    </div><span className="inbound-result-count" role="status">{filtered.length.toLocaleString(language === "fa" ? "fa-IR" : "en-US")} {t.results}</span>
    </div>
    <div className="inbound-table-wrap"><table className="inbound-table">
      <thead><tr><th scope="col">{t.name}</th><th scope="col">{t.transport}</th><th scope="col">{t.nodes}</th><th scope="col">{t.clients}</th><th scope="col">{t.status}</th><th scope="col">{t.actions}</th></tr></thead>
      <tbody>{filtered.slice((current - 1) * size, current * size).map(item => {
        const attached = data.targets.filter(target => target.inbound_id === item.id);
        const state = item.archived ? "archived" : item.enabled ? "enabled" : "disabled";
        return <tr key={item.id}>
          <th scope="row" data-label={t.name}><button type="button" className="inbound-name" aria-label={item.name} onClick={() => onOpen(item)}><strong>{item.name}</strong><bdi dir="ltr">#{item.id}</bdi></button></th>
          <td data-label={t.transport}><bdi dir="ltr">{item.transport === "grpc" ? "gRPC" : "TCP"} / REALITY</bdi></td>
          <td data-label={t.nodes}><span className="inbound-node-names">{attached.length ? attached.map(target => data.nodes.find(n => n.node_id === target.node_id)?.name ?? target.node_id).join(" · ") : t.unattached}</span></td>
          <td data-label={t.clients}>{item.bound_clients.toLocaleString(language === "fa" ? "fa-IR" : "en-US")}</td>
          <td data-label={t.status}><span className={"inbound-state inbound-state-" + state}>{t[state]}</span></td>
          <td data-label={t.actions}><div className="inbound-row-actions"><button type="button" onClick={() => onOpen(item)} aria-label={t.open + " " + item.name}><ArrowUpRight size={16}/><span>{t.open}</span></button><button type="button" disabled={blocked} onClick={() => onCreate(item)} aria-label={t.copy + " " + item.name}><Copy size={16}/></button></div></td>
        </tr>;
      })}</tbody>
    </table></div>
    {!filtered.length && <div className="inbound-directory-empty">{data.inbounds.length ? t.empty : t.none}</div>}
    <nav className="inbound-pagination" aria-label="Inbound pagination"><span>{t.page} {current.toLocaleString(language === "fa" ? "fa-IR" : "en-US")} {t.of} {pages.toLocaleString(language === "fa" ? "fa-IR" : "en-US")}</span><div><button type="button" disabled={current <= 1} onClick={() => setPage(current - 1)}>{t.previous}</button><button type="button" disabled={current >= pages} onClick={() => setPage(current + 1)}>{t.next}</button></div></nav>
  </section>;
}
