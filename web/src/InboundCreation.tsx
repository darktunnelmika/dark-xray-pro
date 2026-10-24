import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { ArrowRightLeft, CheckCircle2, ChevronLeft, ChevronRight, ClipboardCheck, Globe2, KeyRound, Link2, LoaderCircle, Plus, RefreshCw, Server, ShieldCheck, SlidersHorizontal } from "lucide-react";
import { ApiError } from "./session";
import { creationAPI, CreationApiError } from "./InboundCreationApi";
import WorkspaceDialog from "./WorkspaceDialog";
import type { Inbound, InboundNode, InboundTarget } from "./inboundTypes";
import InboundCreationNodes, { creationIssueMessage } from "./InboundCreationNodes";
import InboundProfileAdvanced from "./InboundProfileAdvanced";
import InboundTrafficOptions from "./InboundTrafficOptions";
import { canonicalProfileOptions, profileOptionSection, profileOptionsPayload } from "./inboundProfileOptions";
import { canonicalCreationIP, creationHosts, creationIssues, creationNodeName, creationPayload, creationShortIDs, creationWaitingHosts, emptyCreation, freshCreationShortID, selectedCreationNodes, stateFromCreationPlan,
  type CreationCapabilities, type CreationIssue, type CreationPayload, type CreationPlan, type CreationPlanNode, type CreationPlanResponse, type CreationPreview, type CreationReceipt, type CreationState } from "./InboundCreationModel";
import "./InboundCreation.css";

type Language = "en" | "fa";
type Section = "profile" | "network" | "security" | "nodes" | "swap" | "hosts" | "advanced" | "review";
const sections: Section[] = ["profile", "network", "security", "nodes", "swap", "hosts", "advanced", "review"];
const words = {
  en: {
    create: "Create Inbound", edit: "Edit Inbound setup", lead: "Profile, destination Nodes and every connection path in one setup.",
    profile: "Profile", network: "Network", security: "Security", nodes: "Nodes & routes", swapTab: "Dark Swap", hostsTab: "Hosts", advanced: "Advanced", review: "Review", sections: "Inbound creation sections", close: "Close", cancel: "Cancel", next: "Continue", back: "Back",
    profileTitle: "Connection profile", profileHelp: "Choose a supported preset and enter its REALITY settings.", name: "Inbound name", preset: "Connection preset",
    target: "REALITY target (host:port)", serverNames: "Server names (comma-separated)", service: "gRPC service", serviceHelp: "1–64 letters, numbers, underscores or hyphens.",
    shortIDs: "Short IDs", generate: "Generate Short ID", shortHelp: "Separate IDs with commas. Each uses 2–16 lowercase hexadecimal characters and an even length.",
    generated: "A new Inbound identity and private REALITY key are generated automatically when you save.", preserved: "The existing Inbound identity and private REALITY key are preserved.",
    note: "Change note", notePlaceholder: "Describe this setup", check: "Check complete setup", save: "Save paused setup", retrySave: "Retry the same save",
    checkFirst: "Check the complete setup before saving.", checked: "Configuration checked. Saving keeps the Inbound paused.",
    paused: "Saved configuration awaits separate activation. This action does not start listeners or publish subscriptions.",
    reviewTitle: "Review the complete setup", profileSummary: "Profile", nodeSummary: "Destination Nodes", hostSummary: "Unique Hosts", direct: "Direct", tunnel: "Tunnel", swap: "Dark Swap",
    waiting: "Waiting for Host", noHosts: "No Host yet", noNodes: "Select destination Nodes to complete this setup.",
    unsaved: "Discard the unsaved changes to this Inbound setup?", error: "The server could not validate this setup. Review the fields and try again.",
    uncertain: "The save result could not be confirmed. Retry to resolve the same operation; your complete setup is retained.",
    conflict: "This setup or a referenced Node changed. Reload the saved setup and review it before saving again.", createConflict: "A referenced Node or port assignment changed. Review the affected settings and check the complete setup again.", reload: "Reload saved setup", reloadConfirm: "Reload the saved setup and discard your current edits?",
    loading: "Working…", previewOnly: "Configuration preview", source: "Source", destination: "Destination", missing: "Not set", copy: "The public profile is copied. A new identity and key will be created; Node assignments are selected here.",
    readOnly: "This saved setup is read-only because its profile was changed, assigned or activated outside the setup editor.", staleReferences: "A referenced Node installation changed. Re-select the affected Nodes before checking this setup.",
  },
  fa: {
    create: "ساخت اینباند", edit: "ویرایش تنظیمات اینباند", lead: "پروفایل، نودهای مقصد و تمام مسیرهای اتصال را همین‌جا تنظیم کن.",
    profile: "پروفایل", network: "شبکه", security: "امنیت", nodes: "نودها و مسیرها", swapTab: "دارک سواپ", hostsTab: "هاست‌ها", advanced: "پیشرفته", review: "بازبینی", sections: "بخش‌های ساخت اینباند", close: "بستن", cancel: "انصراف", next: "ادامه", back: "قبلی",
    profileTitle: "پروفایل اتصال", profileHelp: "نوع اتصال پشتیبانی‌شده را انتخاب کن و تنظیمات REALITY را وارد کن.", name: "نام اینباند", preset: "نوع اتصال",
    target: "مقصد REALITY (هاست:پورت)", serverNames: "نام‌های سرور (با کاما)", service: "سرویس gRPC", serviceHelp: "۱ تا ۶۴ حرف انگلیسی، عدد، زیرخط یا خط تیره.",
    shortIDs: "شناسه‌های کوتاه (Short IDs)", generate: "ساخت Short ID", shortHelp: "شناسه‌ها را با کاما جدا کن؛ هر شناسه ۲ تا ۱۶ رقم هگز کوچک با طول زوج دارد.",
    generated: "شناسهٔ اینباند و کلید خصوصی REALITY تازه هنگام ذخیره به‌صورت خودکار ساخته می‌شوند.", preserved: "شناسهٔ اینباند و کلید خصوصی REALITY فعلی حفظ می‌شوند.",
    note: "یادداشت تغییر", notePlaceholder: "این تنظیمات را توضیح بده", check: "بررسی کامل تنظیمات", save: "ذخیرهٔ تنظیمات غیرفعال", retrySave: "تلاش دوباره برای همین ذخیره",
    checkFirst: "پیش از ذخیره، تنظیمات کامل را بررسی کن.", checked: "تنظیمات بررسی شد؛ اینباند پس از ذخیره غیرفعال می‌ماند.",
    paused: "تنظیمات ذخیره‌شده منتظر فعال‌سازی جداگانه می‌ماند؛ این کار پورت یا اشتراک را فعال نمی‌کند.",
    reviewTitle: "بازبینی تنظیمات کامل", profileSummary: "پروفایل", nodeSummary: "نود مقصد", hostSummary: "هاست یکتا", direct: "مستقیم", tunnel: "تونل", swap: "دارک‌سواپ",
    waiting: "در انتظار هاست", noHosts: "بدون هاست", noNodes: "برای تکمیل تنظیمات، نودهای مقصد را انتخاب کن.",
    unsaved: "تغییرات ذخیره‌نشدهٔ تنظیمات این اینباند کنار گذاشته شود؟", error: "سرور نتوانست تنظیمات را تأیید کند؛ فیلدها را بررسی و دوباره تلاش کن.",
    uncertain: "نتیجهٔ ذخیره تأیید نشد؛ برای مشخص‌شدن نتیجه، همان عملیات را دوباره بفرست. تمام تنظیمات حفظ شده است.",
    conflict: "این تنظیمات یا یکی از نودهای وابسته تغییر کرده؛ نسخهٔ ذخیره‌شده را دوباره بارگذاری و بررسی کن.", createConflict: "یکی از نودهای وابسته یا اختصاص پورت‌ها تغییر کرده؛ تنظیمات مربوط را بازبینی و دوباره بررسی کامل کن.", reload: "بارگذاری تنظیمات ذخیره‌شده", reloadConfirm: "نسخهٔ ذخیره‌شده بارگذاری و ویرایش‌های فعلی کنار گذاشته شود؟",
    loading: "در حال انجام…", previewOnly: "پیش‌نمایش تنظیمات", source: "مبدأ", destination: "مقصد", missing: "تنظیم نشده", copy: "پروفایل عمومی کپی شده؛ شناسه و کلید تازه ساخته می‌شود و نودهای مقصد را همین‌جا انتخاب می‌کنی.",
    readOnly: "این تنظیمات فقط خواندنی است؛ پروفایل خارج از این ویرایشگر تغییر کرده، به نودی اختصاص یافته یا فعال شده است.", staleReferences: "نصب یکی از نودهای وابسته تغییر کرده؛ پیش از بررسی، نودهای مربوط را دوباره انتخاب کن.",
  },
};

function normalizedProfile(profile: CreationPayload["profile"]): string {
  return JSON.stringify({ id: profile.id, name: profile.name.trim(), transport: profile.transport, grpc_service: profile.grpc_service,
    reality_target: profile.reality_target, server_names: profile.server_names, short_ids: profile.short_ids, options: canonicalProfileOptions(profile.options) });
}
function normalizedNode(node: CreationPlanNode): string {
  return JSON.stringify({ node_id: node.node_id, installation_id: node.installation_id, listen_ip: canonicalCreationIP(node.listen_ip),
    direct_port: node.direct_port, direct_hosts: node.direct_hosts.map(value => value.trim().toLowerCase()),
    tunnel_port: node.tunnel_port, tunnel_hosts: node.tunnel_hosts.map(value => value.trim().toLowerCase()),
    swap_enabled: node.swap_enabled, swap_source_node_id: node.swap_source_node_id, swap_source_installation_id: node.swap_source_installation_id,
    swap_source_listen_ip: canonicalCreationIP(node.swap_source_listen_ip), swap_source_port: node.swap_source_port,
    swap_entry_address: node.swap_entry_address.trim().toLowerCase(), swap_entry_port: node.swap_entry_port, swap_destination_port: node.swap_destination_port });
}
function validPlan(plan: CreationPlan | undefined, body: CreationPayload, expectedInboundRevision: number): plan is CreationPlan {
  try {
    const waiting = body.nodes.some(row => row.direct_port > 0 && !row.direct_hosts.length || row.tunnel_port > 0 && !row.tunnel_hosts.length || row.swap_enabled && !row.swap_entry_address);
    return !!plan && plan.runtime_apply === false && plan.editable === true && plan.profile?.enabled === false && plan.profile.archived === false && plan.profile.bound_clients === 0 &&
      plan.plan_revision === body.expected_plan_revision + 1 && plan.inbound_revision === expectedInboundRevision && plan.profile.revision === expectedInboundRevision &&
      plan.last_event_id === body.event_id && plan.reason === body.reason.trim() && plan.status === (waiting ? "WAITING_HOST" : "SAVED_DISABLED") &&
      normalizedProfile(plan.profile) === normalizedProfile(body.profile) && Array.isArray(plan.nodes) && plan.nodes.length === body.nodes.length &&
      body.nodes.every(node => plan.nodes.some(row => row.node_id === node.node_id && normalizedNode(row) === normalizedNode(node)));
  } catch { return false; }
}

export default function InboundCreation({ language, nodes, targets, capabilities, csrf, blocked, source, initialPlan, referencesCurrent = true, onClose, onSaved, onDirtyChange, onBusyChange, onUnauthorized }: {
  language: Language; nodes: InboundNode[]; targets: InboundTarget[]; capabilities: CreationCapabilities; csrf: string; blocked: boolean;
  source?: Inbound; initialPlan?: CreationPlan; referencesCurrent?: boolean; onClose: () => void; onSaved: (plan: CreationPlan) => void; onUnauthorized: () => void;
  onDirtyChange: (dirty: boolean) => void; onBusyChange: (busy: boolean) => void;
}) {
  const t = words[language];
  const shortIDInputID = useId();
  const [state, setState] = useState<CreationState>(() => {
    if (initialPlan) return stateFromCreationPlan(initialPlan);
    const value = emptyCreation(source); value.shortIDs = freshCreationShortID(); return value;
  });
  const initial = useRef(JSON.stringify(state));
  const inboundID = useRef(initialPlan?.profile.id ?? (crypto.getRandomValues(new Uint32Array(1))[0] || 1));
  const [revisions, setRevisions] = useState({ plan: initialPlan?.plan_revision ?? 0, inbound: initialPlan?.inbound_revision ?? 0 });
  const savedProfile = useRef(initialPlan?.profile);
  const [readOnly, setReadOnly] = useState(initialPlan?.editable === false);
  const [referencesValid, setReferencesValid] = useState(referencesCurrent);
  const [section, setSection] = useState<Section>("profile");
  const [expanded, setExpanded] = useState(initialPlan?.nodes[0]?.node_id ?? "");
  const [issues, setIssues] = useState<CreationIssue[]>([]);
  const [preview, setPreview] = useState<CreationPreview | null>(null);
  const [busy, setBusy] = useState(false);
  const [uncertain, setUncertain] = useState(false);
  const [conflict, setConflict] = useState(false);
  const [message, setMessage] = useState("");
  const [focusRequest, setFocusRequest] = useState<CreationIssue | null>(null);
  const pending = useRef<{ fingerprint: string; body: CreationPayload } | null>(null);
  const lifetime = useRef<AbortController | null>(null);
  const surface = useRef<HTMLDivElement>(null);
  const tabButtons = useRef<Partial<Record<Section, HTMLButtonElement | null>>>({});
  const dirty = JSON.stringify(state) !== initial.current;
  const editing = revisions.plan > 0;
  const locked = busy || uncertain || readOnly;
  const sniffingSupported = capabilities.profile_sniffing === true && capabilities.profile_traffic_options_scope === "creation_plan";
  const fallbacksSupported = capabilities.profile_fallbacks === true && capabilities.profile_traffic_options_scope === "creation_plan";
  useEffect(() => { const controller = new AbortController(); lifetime.current = controller; return () => controller.abort(); }, []);
  useEffect(() => { onDirtyChange(dirty); }, [dirty, onDirtyChange]);
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);
  useEffect(() => { onBusyChange(busy); return () => onBusyChange(false); }, [busy, onBusyChange]);
  useEffect(() => {
    tabButtons.current[section]?.scrollIntoView({ block: "nearest", inline: "nearest" });
    surface.current?.querySelector(".creation-scroll")?.scrollTo({ top: 0 });
  }, [section]);
  useEffect(() => {
    if (!focusRequest) return;
    const scope = focusRequest.nodeID ? surface.current?.querySelector<HTMLElement>(`[data-node-id="${CSS.escape(focusRequest.nodeID)}"]`) : surface.current;
    const control = scope?.querySelector<HTMLElement>(`[data-creation-field="${CSS.escape(focusRequest.field)}"]`) ?? Array.from(scope?.querySelectorAll<HTMLElement>("input, select, textarea") ?? []).find(element => element.getClientRects().length > 0 && !element.matches(":disabled"));
    for (let ancestor = control?.parentElement; ancestor; ancestor = ancestor.parentElement) if (ancestor instanceof HTMLDetailsElement) ancestor.open = true;
    control?.focus({ preventScroll: true }); control?.scrollIntoView({ block: "nearest" });
  }, [focusRequest]);

  function update(value: CreationState) { setState(value); setPreview(null); setIssues([]); setMessage(""); setConflict(false); }
  function profile(field: Exclude<keyof CreationState["profile"], "options">, value: string) { update({ ...state, profile: { ...state.profile, [field]: value } }); }
  function close() { if (!busy && (!(dirty || uncertain) || window.confirm(t.unsaved))) onClose(); }
  function move(next: Section) { setSection(next); }
  function tabKey(event: KeyboardEvent<HTMLButtonElement>, current: Section) {
    const step = language === "fa" ? -1 : 1, index = sections.indexOf(current);
    const next = event.key === "Home" ? sections[0] : event.key === "End" ? sections[sections.length - 1] : event.key === "ArrowRight" ? sections[(index + step + sections.length) % sections.length] : event.key === "ArrowLeft" ? sections[(index - step + sections.length) % sections.length] : null;
    if (next) { event.preventDefault(); move(next); tabButtons.current[next]?.focus(); }
  }
  function validate(): boolean {
    const next = creationIssues(state, nodes);
    if (!next.length) {
      const options = profileOptionsPayload(state.profile.options, state.profile.transport);
      if (capabilities.profile_options !== true && options) next.push({ field: "profileOptions", code: "profile_options_unavailable" });
      if (!sniffingSupported && options?.sniffing !== undefined) next.push({ field: "sniffing.enabled", code: "profile_sniffing_unavailable" });
      if (!fallbacksSupported && options?.fallbacks !== undefined) next.push({ field: "fallbacks", code: "profile_fallbacks_unavailable" });
    }
    setIssues(next);
    if (!next.length) return true;
    const first = next[0];
    setSection(first.nodeID || first.field === "nodes" ? "nodes" : first.field === "reason" ? "review" : profileOptionSection(first.field) ?? "profile");
    if (first.nodeID) setExpanded(first.nodeID);
    setFocusRequest({ ...first }); return false;
  }
  function body(): CreationPayload {
    const value = creationPayload(state, inboundID.current, revisions.plan, revisions.inbound, "");
    const fingerprint = JSON.stringify(value);
    if (!pending.current || pending.current.fingerprint !== fingerprint) pending.current = { fingerprint, body: { ...value, event_id: crypto.randomUUID() } };
    return pending.current.body;
  }
  function expectedInboundRevision(payload: CreationPayload): number {
    return revisions.inbound === 0 ? 1 : revisions.inbound + (savedProfile.current && normalizedProfile(savedProfile.current) === normalizedProfile(payload.profile) ? 0 : 1);
  }
  function serverIssue(error: unknown) {
    if (!(error instanceof CreationApiError) || !error.issue) return;
    let issue = error.issue;
    if (issue.nodeID && !state.selected.includes(issue.nodeID)) {
      const destination = selectedCreationNodes(state).find(row => row.swap.enabled && row.swap.sourceNodeID === issue.nodeID);
      if (destination) issue = { ...issue, nodeID: destination.nodeID, field: issue.field === "listenIP" ? "sourceListenIP" : "sourcePort" };
    }
    setIssues([issue]);
    if (issue.nodeID) { setExpanded(issue.nodeID); setSection("nodes"); }
    else if (issue.field === "nodes") setSection("nodes");
    else if (issue.field === "reason") setSection("review");
    else if (profileOptionSection(issue.field)) setSection(profileOptionSection(issue.field)!);
    else if (issue.field === "name" || issue.field.startsWith("profile")) setSection("profile");
    setFocusRequest({ ...issue });
  }
  function needsReload(error: unknown): boolean {
    return error instanceof ApiError && error.status === 409 && (!(error instanceof CreationApiError) || !error.issue || ["plan_revision_conflict", "profile_changed_or_materialized", "inbound_id_exists", "profile_options_required", "profile_sniffing_required", "profile_fallbacks_required"].includes(error.issue.code));
  }
  async function check() {
    if (busy || blocked || uncertain || readOnly || !csrf || !validate()) return;
    const controller = lifetime.current; if (!controller || controller.signal.aborted) return;
    setBusy(true); setMessage(""); setConflict(false); setPreview(null);
    const payload = body();
    try {
      const result = await creationAPI<CreationPreview>("/api/v1/workspace/native-inbound-plans/preview", { method: "POST", signal: controller.signal,
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(payload) }, onUnauthorized);
      if (!result || result.valid !== true || result.runtime_changed !== false || result.topology_validated !== true || result.topology_validation_only !== true ||
        result.topology_key_scope !== (editing ? "saved_profile" : "preview_only") || !validPlan(result.plan, payload, expectedInboundRevision(payload))) throw new Error("invalid_setup_preview");
      if (!controller.signal.aborted) { setPreview(result); setSection("review"); setReferencesValid(true); setMessage(""); }
    } catch (error) { if (!controller.signal.aborted) { const changed = needsReload(error); setConflict(changed); setMessage(changed ? editing ? t.conflict : t.createConflict : t.error); serverIssue(error); } }
    finally { if (!controller.signal.aborted) setBusy(false); }
  }
  async function save() {
    if (busy || blocked || readOnly || !csrf || !preview || !pending.current) return;
    const controller = lifetime.current; if (!controller || controller.signal.aborted) return;
    const payload = pending.current.body;
    setBusy(true); setMessage("");
    try {
      const result = await creationAPI<CreationReceipt>("/api/v1/workspace/native-inbound-plans" + (editing ? "/" + inboundID.current : ""), {
        method: editing ? "PUT" : "POST", signal: controller.signal, headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(payload) }, onUnauthorized);
      if (!result || result.event_id !== payload.event_id || !validPlan(result.plan, payload, expectedInboundRevision(payload)) || result.runtime_changed !== false || result.clients_deleted !== false || result.credit_changed !== false || result.usage_reset !== false || result.subscription_published !== false) throw new Error("invalid_setup_receipt");
      if (!controller.signal.aborted) { initial.current = JSON.stringify(state); onDirtyChange(false); onSaved(result.plan); }
    } catch (error) {
      if (!controller.signal.aborted) {
        const changed = needsReload(error);
        const ambiguous = !(error instanceof ApiError) || error.status === 0 || error.status >= 500;
        setConflict(changed); setUncertain(ambiguous); setMessage(changed ? editing ? t.conflict : t.createConflict : ambiguous ? t.uncertain : t.error);
        if (!ambiguous) { setPreview(null); serverIssue(error); }
      }
    } finally { if (!controller.signal.aborted) setBusy(false); }
  }
  async function reload() {
    if (busy || !editing || !window.confirm(t.reloadConfirm)) return;
    const controller = lifetime.current; if (!controller || controller.signal.aborted) return;
    setBusy(true);
    try {
      const result = await creationAPI<CreationPlanResponse>("/api/v1/workspace/native-inbound-plans/" + inboundID.current, { signal: controller.signal }, onUnauthorized);
      if (!result || result.plan?.profile?.id !== inboundID.current || result.plan.runtime_apply !== false || !Array.isArray(result.plan.nodes) || typeof result.plan.editable !== "boolean") throw new Error("invalid_setup_plan");
      if (!controller.signal.aborted) { const plan = result.plan; const next = stateFromCreationPlan(plan); initial.current = JSON.stringify(next); savedProfile.current = plan.profile; setState(next); setRevisions({ plan: plan.plan_revision, inbound: plan.inbound_revision }); setReadOnly(!plan.editable); setReferencesValid(result.references_current); setPreview(null); pending.current = null; setIssues([]); setConflict(false); setUncertain(false); setMessage(""); }
    } catch { if (!controller.signal.aborted) setMessage(t.error); }
    finally { if (!controller.signal.aborted) setBusy(false); }
  }
  const rows = selectedCreationNodes(state);
  const hostCount = rows.reduce((count, row) => count + new Set([...(row.direct.enabled ? creationHosts(row.direct.hosts) : []), ...(row.tunnel.enabled ? creationHosts(row.tunnel.hosts) : [])]).size, 0);
  const profileIssues = issues.filter(issue => !issue.nodeID && !["nodes", "reason"].includes(issue.field) && !profileOptionSection(issue.field));
  return <WorkspaceDialog open title={editing ? t.edit : t.create} subtitle={t.lead} variant="inbound-editor" closeLabel={t.close} busy={busy} onClose={close}>
    <div className="inbound-creation" dir={language === "fa" ? "rtl" : "ltr"} ref={surface}>
      <nav className="creation-tabs" role="tablist" aria-label={t.sections}>
        {sections.map((item, index) => { const Icon = item === "profile" ? SlidersHorizontal : item === "nodes" ? Server : item === "security" ? ShieldCheck : item === "network" ? Link2 : item === "swap" ? ArrowRightLeft : item === "hosts" ? Globe2 : item === "advanced" ? KeyRound : ClipboardCheck; return <button key={item} ref={node => { tabButtons.current[item] = node; }} type="button" role="tab" id={`creation-tab-${item}`} aria-controls={`creation-panel-${item}`} aria-selected={section === item} tabIndex={section === item ? 0 : -1} onClick={() => move(item)} onKeyDown={event => tabKey(event, item)}><Icon size={17} aria-hidden="true"/><span>{item === "swap" ? t.swapTab : item === "hosts" ? t.hostsTab : t[item]}</span><small aria-hidden="true">{index + 1}</small></button>; })}
      </nav>
      <div className="creation-scroll">
        {readOnly && <p className="creation-message" role="status">{t.readOnly}</p>}
        {!referencesValid && <p className="creation-message" role="status">{t.staleReferences}</p>}
        <section id="creation-panel-profile" role="tabpanel" aria-labelledby="creation-tab-profile" hidden={section !== "profile"} className="creation-profile">
          <header className="creation-section-heading"><div><h3>{t.profileTitle}</h3><p>{t.profileHelp}</p></div><span className="creation-protocol"><ShieldCheck size={14}/><bdi>VLESS</bdi></span></header>
          {source && !editing && <p className="creation-help">{t.copy}</p>}
          <label className="creation-field"><span>{t.name}</span><input data-initial-focus data-creation-field="name" aria-label={t.name} maxLength={128} value={state.profile.name} disabled={locked} onChange={event => profile("name", event.target.value)} autoComplete="off"/></label>
          <fieldset className="creation-presets" disabled={locked}><legend>{t.preset}</legend>{["tcp", "grpc"].filter(transport => capabilities.transports.includes(transport)).map(transport => <label key={transport} data-selected={state.profile.transport === transport}><input type="radio" name="creation-transport" aria-label={transport === "tcp" ? "TCP / REALITY" : "gRPC / REALITY"} checked={state.profile.transport === transport} onChange={() => profile("transport", transport)}/><span><bdi>{transport === "tcp" ? "TCP / REALITY" : "gRPC / REALITY"}</bdi><small>{transport === "tcp" ? "Vision" : "gRPC"}</small></span></label>)}</fieldset>
          {state.profile.transport === "grpc" && <label className="creation-field"><span>{t.service}</span><input aria-label={t.service} data-creation-field="grpcService" dir="ltr" maxLength={64} value={state.profile.grpcService} disabled={locked} onChange={event => profile("grpcService", event.target.value)} autoComplete="off"/><small>{t.serviceHelp}</small></label>}
          <div className="creation-field-grid"><label className="creation-field"><span>{t.target}</span><input aria-label={t.target} data-creation-field="realityTarget" dir="ltr" value={state.profile.realityTarget} disabled={locked} onChange={event => profile("realityTarget", event.target.value)} placeholder="example.org:443" autoComplete="off"/></label>
            <label className="creation-field"><span>{t.serverNames}</span><input aria-label={t.serverNames} data-creation-field="serverNames" dir="ltr" value={state.profile.serverNames} disabled={locked} onChange={event => profile("serverNames", event.target.value)} placeholder="example.org" autoComplete="off"/></label></div>
          <div className="creation-field"><span><label htmlFor={shortIDInputID}>{t.shortIDs}</label><button type="button" aria-label={t.generate} disabled={locked || creationShortIDs(state.shortIDs).length >= 16} onClick={() => update({ ...state, shortIDs: [...creationShortIDs(state.shortIDs), freshCreationShortID()].join(", ") })}><Plus size={13}/>{t.generate}</button></span><input id={shortIDInputID} aria-label={t.shortIDs} data-creation-field="shortIDs" dir="ltr" value={state.shortIDs} disabled={locked} onChange={event => update({ ...state, shortIDs: event.target.value })} autoComplete="off" spellCheck={false}/><small>{t.shortHelp}</small></div>
          <p className="creation-key-note"><KeyRound size={17}/><span>{editing ? t.preserved : t.generated}</span></p>
          {profileIssues.length > 0 && <ul className="creation-issues" role="alert">{profileIssues.map((issue, index) => <li key={index}>{creationIssueMessage(issue, language)}</li>)}</ul>}
        </section>
        <section id="creation-panel-network" role="tabpanel" aria-labelledby="creation-tab-network" hidden={section !== "network"} className="creation-profile">
          <header className="creation-section-heading"><div><h3>{language === "fa" ? "شبکه و انتقال" : "Network and transport"}</h3><p>{language === "fa" ? "انتقال‌های قابل اجرا را انتخاب کن؛ موارد ناسازگار تا تکمیل موتور نمایش غیرفعال دارند." : "Choose a supported transport. Additional transports stay unavailable until runtime support is proven."}</p></div></header>
          <fieldset className="creation-presets" disabled={locked}><legend>{t.preset}</legend>{["tcp", "grpc"].filter(transport => capabilities.transports.includes(transport)).map(transport => <label key={transport} data-selected={state.profile.transport === transport}><input type="radio" name="creation-network-transport" aria-label={transport === "tcp" ? "TCP / REALITY — network" : "gRPC / REALITY — network"} checked={state.profile.transport === transport} onChange={() => profile("transport", transport)}/><span><bdi>{transport === "tcp" ? "TCP / REALITY" : "gRPC / REALITY"}</bdi><small>{transport === "tcp" ? "Vision" : "gRPC"}</small></span></label>)}</fieldset>
          {state.profile.transport === "grpc" && <label className="creation-field"><span>{t.service}</span><input aria-label={t.service + " — network"} dir="ltr" maxLength={64} value={state.profile.grpcService} disabled={locked} onChange={event => profile("grpcService", event.target.value)} autoComplete="off"/></label>}
          {state.profile.transport === "grpc" && <InboundProfileAdvanced language={language} section="network" value={state.profile.options} onChange={options => update({ ...state, profile: { ...state.profile, options } })} supported={capabilities.profile_options === true} disabled={locked} invalidFields={issues.map(issue => issue.field)}/>}
          {issues.filter(issue => profileOptionSection(issue.field) === "network").map((issue, index) => <p key={index} className="creation-issues" role="alert">{creationIssueMessage(issue, language)}</p>)}
          <div className="creation-legacy-features"><strong>{language === "fa" ? "انتقال‌های پنل قدیمی (در انتظار پشتیبانی اجرایی)" : "Legacy transports (not yet operational)"}</strong><div className="creation-legacy-grid">{["WebSocket · Host / Path", "HTTP Upgrade · Host / Path", "XHTTP · Mode / Padding", "mKCP · MTU / TTI", "TCP HTTP camouflage", "Cloudflare / PROXY Protocol"].map(name => <span key={name} className="creation-legacy-option" title={language === "fa" ? "هنوز توسط backend پشتیبانی نمی‌شود" : "Not supported by the backend"}>{name}<small>{language === "fa" ? "غیرفعال" : "Unavailable"}</small></span>)}</div></div>
        </section>
        <section id="creation-panel-security" role="tabpanel" aria-labelledby="creation-tab-security" hidden={section !== "security"} className="creation-profile">
          <header className="creation-section-heading"><div><h3>{language === "fa" ? "امنیت REALITY" : "REALITY security"}</h3><p>{language === "fa" ? "فیلدهای موجود در همین فرم ذخیره می‌شوند؛ امنیت‌های TLS و None هنوز به موتور متصل نیستند." : "Supported REALITY fields save with this setup. TLS and None are not connected to the native runtime."}</p></div><span className="creation-protocol"><ShieldCheck size={14}/> REALITY</span></header>
          <div className="creation-field-grid"><label className="creation-field"><span>{t.target}</span><input dir="ltr" aria-label={t.target + " — security"} value={state.profile.realityTarget} disabled={locked} onChange={event => profile("realityTarget", event.target.value)} placeholder="example.org:443"/></label><label className="creation-field"><span>{t.serverNames}</span><input dir="ltr" aria-label={t.serverNames + " — security"} value={state.profile.serverNames} disabled={locked} onChange={event => profile("serverNames", event.target.value)}/></label></div>
          <label className="creation-field"><span>{t.shortIDs}</span><input dir="ltr" aria-label={t.shortIDs + " — security"} value={state.shortIDs} disabled={locked} onChange={event => update({ ...state, shortIDs: event.target.value })}/></label>
          <p className="creation-key-note"><KeyRound size={17}/><span>{editing ? t.preserved : t.generated}</span></p>
          <InboundProfileAdvanced language={language} section="security" value={state.profile.options} onChange={options => update({ ...state, profile: { ...state.profile, options } })} supported={capabilities.profile_options === true} disabled={locked} invalidFields={issues.map(issue => issue.field)}/>
          {issues.filter(issue => profileOptionSection(issue.field) === "security").map((issue, index) => <p key={index} className="creation-issues" role="alert">{creationIssueMessage(issue, language)}</p>)}
          <div className="creation-legacy-features"><strong>{language === "fa" ? "ابزارهای امنیتی باقی‌مانده" : "Security features pending runtime support"}</strong><div className="creation-legacy-grid">{["None", "TLS · ALPN / certificates", "REALITY · Fingerprint / SpiderX", "REALITY · Target Scan / Ping"].map(name => <span key={name} className="creation-legacy-option">{name}<small>{language === "fa" ? "هنوز قابل تنظیم نیست" : "Not available"}</small></span>)}</div></div>
        </section>
        <section id="creation-panel-swap" role="tabpanel" aria-labelledby="creation-tab-swap" hidden={section !== "swap"}>
          <header className="creation-section-heading creation-extra-heading"><div><h3><ArrowRightLeft size={16}/> {t.swapTab}</h3><p>{language === "fa" ? "برای هر نود مقصد، واسط، پورت رله، ورودی ایران و پورت مقصد را همین‌جا تنظیم کن. ذخیره فقط طرح خاموش است و اتصال عملیاتی هنوز فعال نمی‌شود." : "Configure relay Node, relay port, Iranian entry and destination port per destination. Saving preserves an inactive plan; it does not apply live traffic."}</p></div></header>
          {!rows.length && <div className="creation-swap-empty"><p>{t.noNodes}</p><button type="button" disabled={busy} onClick={() => move("nodes")}>{language === "fa" ? "انتخاب نودهای مقصد" : "Select destination Nodes"}<Server size={15}/></button></div>}
          {section === "swap" && rows.length > 0 && <InboundCreationNodes view="swap" language={language} state={state} nodes={nodes} targets={targets} disabled={locked} expanded={expanded} onExpanded={setExpanded} onChange={update} issues={issues}/>}
        </section>
        <section id="creation-panel-hosts" role="tabpanel" aria-labelledby="creation-tab-hosts" hidden={section !== "hosts"} className="creation-profile">
          <header className="creation-section-heading"><div><h3>{language === "fa" ? "هاست‌ها و خروجی اتصال" : "Hosts and public endpoints"}</h3><p>{language === "fa" ? "آدرس‌های Direct و Tunnel با پورت‌های تعریف‌شدهٔ همان نود ذخیره می‌شوند. انتشار لینک نیاز به فعال‌سازی جداگانه دارد." : "Direct and Tunnel addresses are saved with their Node ports. Publishing customer links remains a separate operation."}</p></div></header>
          {rows.map(row => <article key={row.nodeID} className="creation-host-card"><h4>{creationNodeName(nodes, row.nodeID)}</h4>{(["direct", "tunnel"] as const).filter(path => row[path].enabled).map(path => <label key={path} className="creation-field"><span>{path === "direct" ? t.direct : t.tunnel} · {language === "fa" ? "هاست‌ها" : "Hosts"} · {row[path].port || "—"}</span><textarea rows={3} dir="ltr" aria-label={path + " hosts — " + row.nodeID} value={row[path].hosts} disabled={locked} onChange={event => update({ ...state, nodes: { ...state.nodes, [row.nodeID]: { ...row, [path]: { ...row[path], hosts: event.target.value } } } })}/></label>)}{!row.direct.enabled && !row.tunnel.enabled && <p className="creation-help">{language === "fa" ? "برای این نود مسیر Direct یا Tunnel انتخاب نشده." : "No Direct or Tunnel listener selected on this Node."}</p>}</article>)}
          {!rows.length && <p className="creation-empty">{t.noNodes}</p>}
          <div className="creation-legacy-features"><strong>{language === "fa" ? "تنظیمات پیشرفتهٔ Host در انتظار توسعه" : "Advanced Host controls pending"}</strong><p>{language === "fa" ? "SNI Override، ALPN، Fingerprint، Path، FinalMask، Mihomo و حذف از قالب‌های اشتراک هنوز در plan فعلی ذخیره نمی‌شوند." : "SNI overrides, ALPN, fingerprint, path, FinalMask, Mihomo and per-format exclusion are not yet persisted by creation plans."}</p></div>
        </section>
        <section id="creation-panel-advanced" role="tabpanel" aria-labelledby="creation-tab-advanced" hidden={section !== "advanced"} className="creation-profile">
          <header className="creation-section-heading"><div><h3>{language === "fa" ? "تنظیمات پیشرفتهٔ ترافیک" : "Advanced traffic settings"}</h3><p>{language === "fa" ? "Sniffing و Fallback فقط در طرح خاموش ذخیره می‌شوند. فعال‌سازی و انتشار اشتراک برای این تنظیمات هنوز در دسترس نیست." : "Sniffing and fallbacks are saved only in paused setup plans. Activation and subscription publishing for these settings remain unavailable."}</p></div></header>
          <InboundTrafficOptions language={language} value={state.profile.options} transport={state.profile.transport} onChange={options => update({ ...state, profile: { ...state.profile, options } })} sniffingSupported={sniffingSupported} fallbacksSupported={fallbacksSupported} disabled={locked} invalidFields={issues.map(issue => issue.field)}/>
          {issues.filter(issue => profileOptionSection(issue.field) === "advanced").map((issue, index) => <p key={index} className="creation-issues" role="alert">{creationIssueMessage(issue, language)}</p>)}
          <details className="inbound-traffic-disclosure"><summary>{language === "fa" ? "امکانات در انتظار پشتیبانی" : "Features pending support"}</summary><div className="creation-legacy-features"><div className="creation-legacy-grid">{["Advanced JSON · streamSettings", "TLS managed certificates", "VMess / Trojan / Shadowsocks / SOCKS / HTTP", "Local HUB target", "Dark Swap · live Apply / ACK / Diagnostics"].map(name => <span key={name} className="creation-legacy-option">{name}<small>{language === "fa" ? "هنوز در دسترس نیست" : "Not available"}</small></span>)}</div></div></details>
        </section>
        <section id="creation-panel-nodes" role="tabpanel" aria-labelledby="creation-tab-nodes" hidden={section !== "nodes"}>
          {section === "nodes" && <InboundCreationNodes language={language} state={state} nodes={nodes} targets={targets} disabled={locked} expanded={expanded} onExpanded={setExpanded} onChange={update} issues={issues}/>}
          {issues.filter(issue => issue.field === "nodes").map((issue, index) => <p key={index} className="creation-issues" role="alert">{creationIssueMessage(issue, language)}</p>)}
        </section>
        <section id="creation-panel-review" role="tabpanel" aria-labelledby="creation-tab-review" hidden={section !== "review"} className="creation-review">
          <header className="creation-section-heading"><div><h3>{t.reviewTitle}</h3><p>{t.paused}</p></div></header>
          <dl className="creation-review-counts"><div><dt>{t.profileSummary}</dt><dd>{state.profile.name || "—"}<small><bdi>{state.profile.transport.toUpperCase()} / REALITY</bdi></small></dd></div><div><dt>{t.nodeSummary}</dt><dd>{rows.length}</dd></div><div><dt>{t.hostSummary}</dt><dd>{hostCount}</dd></div></dl>
          <div className="creation-review-nodes">{rows.map(row => <article key={row.nodeID}><header><strong>{creationNodeName(nodes, row.nodeID)}</strong><bdi dir="ltr">{row.listenIP || "—"}</bdi>{creationWaitingHosts(row) && <span className="creation-waiting">{t.waiting}</span>}</header>
            {row.direct.enabled && <p><Globe2 size={14}/><span>{t.direct}</span><bdi>{row.direct.port || "—"}</bdi><small>{creationHosts(row.direct.hosts).join(", ") || t.noHosts}</small></p>}
            {row.tunnel.enabled && <p><Link2 size={14}/><span>{t.tunnel}</span><bdi>{row.tunnel.port || "—"}</bdi><small>{creationHosts(row.tunnel.hosts).join(", ") || t.noHosts}</small></p>}
            {row.swap.enabled && <p className="creation-review-swap"><ArrowRightLeft size={14}/><span>{t.swap}</span><small><bdi dir="ltr">{row.swap.entryHost || t.noHosts}:{row.swap.entryPort || "—"} → {row.swap.sourceNodeID || "—"}:{row.swap.sourcePort || "—"} → {row.nodeID}:{row.swap.destinationPort || "—"}</bdi></small></p>}
          </article>)}</div>
          {!rows.length && <p className="creation-empty">{t.noNodes}</p>}
          <label className="creation-field"><span>{t.note}</span><input aria-label={t.note} data-creation-field="reason" maxLength={200} value={state.reason} disabled={locked} onChange={event => update({ ...state, reason: event.target.value })} placeholder={t.notePlaceholder}/></label>
          {issues.filter(issue => issue.field === "reason").map((issue, index) => <p key={index} className="creation-issues" role="alert">{creationIssueMessage(issue, language)}</p>)}
          <div className="creation-preview-status" data-checked={!!preview} role="status"><ShieldCheck size={18}/><span>{preview ? t.checked : t.checkFirst}</span></div>
        </section>
        {message && <div className={"creation-message" + (preview && !uncertain ? " creation-message-success" : "")} role={preview && !uncertain ? "status" : "alert"}><p>{message}</p>{conflict && editing && <button type="button" disabled={busy} onClick={() => void reload()}><RefreshCw size={14}/>{t.reload}</button>}</div>}
      </div>
      <footer className="creation-footer"><div className="creation-footer-status"><ShieldCheck size={14}/><span>{t.previewOnly}</span>{busy && <LoaderCircle size={15} className="creation-spinner"/>}</div><div className="creation-footer-actions">
        <button type="button" disabled={busy} onClick={close}>{t.cancel}</button>
        {section !== "profile" && <button type="button" disabled={busy} onClick={() => move(sections[sections.indexOf(section) - 1])}><ChevronLeft size={15}/>{t.back}</button>}
        {section !== "review" ? <button type="button" className="native-primary" disabled={busy} onClick={() => move(sections[sections.indexOf(section) + 1])}>{t.next}<ChevronRight size={15}/></button> : <>
          <button type="button" disabled={busy || blocked || uncertain || readOnly} onClick={() => void check()}><RefreshCw size={15}/>{t.check}</button>
          <button type="button" className="native-primary" disabled={busy || blocked || !preview || readOnly} onClick={() => void save()}><CheckCircle2 size={15}/>{uncertain ? t.retrySave : t.save}</button>
        </>}
      </div></footer>
    </div>
  </WorkspaceDialog>;
}
