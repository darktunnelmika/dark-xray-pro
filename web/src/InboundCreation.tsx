import { useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { ArrowRightLeft, CheckCircle2, ChevronLeft, ChevronRight, ClipboardCheck, KeyRound, Link2, LoaderCircle, Plus, RefreshCw, ShieldCheck, SlidersHorizontal } from "lucide-react";
import { ApiError } from "./session";
import { creationAPI, CreationApiError } from "./InboundCreationApi";
import WorkspaceDialog from "./WorkspaceDialog";
import type { Inbound, InboundNode, InboundTarget } from "./inboundTypes";
import InboundCreationNodes, { creationIssueMessage } from "./InboundCreationNodes";
import InboundProfileAdvanced from "./InboundProfileAdvanced";
import InboundNetworkOptions from "./InboundNetworkOptions";
import InboundTrafficOptions from "./InboundTrafficOptions";
import InboundCreationReview from "./InboundCreationReview";
import { canonicalProfileOptions, profileOptionSection, profileOptionsPayload } from "./inboundProfileOptions";
import { canonicalEndpointAddress, canonicalEndpoints } from "./inboundEndpointOptions";
import { validEndpointPreviews } from "./inboundEndpointPreview";
import { canonicalCreationIP, creationIssues, creationPayload, creationPort, creationShortIDs, emptyCreation, freshCreationShortID, selectedCreationNodes, stateFromCreationPlan,
  type CreationCapabilities, type CreationIssue, type CreationPayload, type CreationPlan, type CreationPlanNode, type CreationPlanResponse, type CreationPreview, type CreationReceipt, type CreationState } from "./InboundCreationModel";
import "./InboundCreation.css";

type Language = "en" | "fa";
type Section = "connection" | "swap" | "advanced" | "review";
type ConnectionGroup = "general" | "network" | "security";
const sections: Section[] = ["connection", "swap", "advanced", "review"];

function issueSection(issue: CreationIssue): Section {
  if (issue.nodeID || issue.field === "nodes") return "swap";
  if (issue.field === "reason") return "review";
  return profileOptionSection(issue.field) === "advanced" ? "advanced" : "connection";
}
function connectionGroup(field: string): ConnectionGroup {
  if (field === "transport" || field === "grpcService" || profileOptionSection(field) === "network") return "network";
  if (["realityTarget", "serverNames", "shortIDs", "profileOptions"].includes(field) || profileOptionSection(field) === "security") return "security";
  return "general";
}
function focusField(issue: CreationIssue, state: CreationState): string {
  if (issue.field === "nodes") return "node";
  if (issue.field === "paths") return "directEnabled";
  const row = issue.nodeID ? state.nodes[issue.nodeID] : undefined;
  if (row && issue.field === "hosts") return row.direct.enabled ? "directHosts" : "tunnelHosts";
  if (row && issue.field === "ports") {
    const pathFields = { direct: "directPort", tunnel: "tunnelPort", swap_source: "sourcePort", swap_destination: "swapPort" };
    if (issue.path) return pathFields[issue.path];
    const ports: Array<[string, number]> = [];
    if (row.direct.enabled) ports.push(["directPort", creationPort(row.direct.port)]);
    if (row.tunnel.enabled) ports.push(["tunnelPort", creationPort(row.tunnel.port)]);
    if (row.swap.enabled && (!row.tunnel.enabled || row.swap.destinationPort !== row.tunnel.port)) ports.push(["swapPort", creationPort(row.swap.destinationPort)]);
    if (issue.port) return ports.find(([, port]) => port === issue.port)?.[0] ?? "node";
    // Older servers did not identify an externally reserved port. Do not
    // highlight an unrelated Direct input as if it were the known conflict.
    if (issue.code === "existing_listener_port_collision") return "node";
    const seen = new Set<number>();
    for (const [field, port] of ports) { if (issue.code === "port_reserved" ? [2087, 8443, 24443].includes(port) : seen.has(port)) return field; seen.add(port); }
    return ports[0]?.[0] ?? "directEnabled";
  }
  return issue.field;
}
const words = {
  en: {
    create: "Create Inbound", edit: "Edit Inbound setup", lead: "Profile, destination Nodes and every connection path in one setup.",
    connection: "Connection", general: "General", network: "Network", security: "Security", swap: "Dark Swap", advanced: "Advanced", review: "Review", sections: "Inbound creation sections", close: "Close", cancel: "Cancel", next: "Continue", back: "Back",
    profileTitle: "Connection settings", profileHelp: "Name the Inbound, choose its transport and configure REALITY.", name: "Inbound name", preset: "Connection preset",
    target: "REALITY target (host:port)", serverNames: "Server names (comma-separated)", service: "gRPC service", serviceHelp: "1–64 letters, numbers, underscores or hyphens.",
    shortIDs: "Short IDs", generate: "Generate Short ID", shortHelp: "Separate IDs with commas. Each uses 2–16 lowercase hexadecimal characters and an even length.",
    generated: "A new Inbound identity and private REALITY key are generated automatically when you save.", preserved: "The existing Inbound identity and private REALITY key are preserved.",
    note: "Change note", notePlaceholder: "Describe this setup", check: "Check complete setup", save: "Save paused setup", retrySave: "Retry the same save",
    checkFirst: "Check the complete setup before saving.", checked: "Configuration checked. Saving keeps the Inbound paused.",
    paused: "Saved configuration awaits separate activation. This action does not start listeners or publish subscriptions.",
    reviewTitle: "Review the complete setup",
    unsaved: "Discard the unsaved changes to this Inbound setup?", error: "The server could not validate this setup. Review the fields and try again.",
    uncertain: "The save result could not be confirmed. Retry to resolve the same operation; your complete setup is retained.",
    conflict: "This setup or a referenced Node changed. Reload the saved setup and review it before saving again.", createConflict: "A referenced Node or port assignment changed. Review the affected settings and check the complete setup again.", reload: "Reload saved setup", reloadConfirm: "Reload the saved setup and discard your current edits?",
    loading: "Working…", previewOnly: "Configuration preview", source: "Source", destination: "Destination", missing: "Not set", copy: "The public profile is copied. A new identity and key will be created; Node assignments are selected here.",
    readOnly: "This saved setup is read-only because its profile was changed, assigned or activated outside the setup editor.", staleReferences: "A referenced Node installation changed. Re-select the affected Nodes before checking this setup.",
  },
  fa: {
    create: "ساخت اینباند", edit: "ویرایش تنظیمات اینباند", lead: "پروفایل، نودهای مقصد و تمام مسیرهای اتصال را همین‌جا تنظیم کن.",
    connection: "اتصال", general: "عمومی", network: "شبکه", security: "امنیت", swap: "دارک‌سواپ", advanced: "پیشرفته", review: "بازبینی", sections: "بخش‌های ساخت اینباند", close: "بستن", cancel: "انصراف", next: "ادامه", back: "قبلی",
    profileTitle: "تنظیمات اتصال", profileHelp: "نام اینباند، نوع انتقال و تنظیمات REALITY را مشخص کن.", name: "نام اینباند", preset: "نوع اتصال",
    target: "مقصد REALITY (هاست:پورت)", serverNames: "نام‌های سرور (با کاما)", service: "سرویس gRPC", serviceHelp: "۱ تا ۶۴ حرف انگلیسی، عدد، زیرخط یا خط تیره.",
    shortIDs: "شناسه‌های کوتاه (Short IDs)", generate: "ساخت Short ID", shortHelp: "شناسه‌ها را با کاما جدا کن؛ هر شناسه ۲ تا ۱۶ رقم هگز کوچک با طول زوج دارد.",
    generated: "شناسهٔ اینباند و کلید خصوصی REALITY تازه هنگام ذخیره به‌صورت خودکار ساخته می‌شوند.", preserved: "شناسهٔ اینباند و کلید خصوصی REALITY فعلی حفظ می‌شوند.",
    note: "یادداشت تغییر", notePlaceholder: "این تنظیمات را توضیح بده", check: "بررسی کامل تنظیمات", save: "ذخیرهٔ تنظیمات غیرفعال", retrySave: "تلاش دوباره برای همین ذخیره",
    checkFirst: "پیش از ذخیره، تنظیمات کامل را بررسی کن.", checked: "تنظیمات بررسی شد؛ اینباند پس از ذخیره غیرفعال می‌ماند.",
    paused: "تنظیمات ذخیره‌شده منتظر فعال‌سازی جداگانه می‌ماند؛ این کار پورت یا اشتراک را فعال نمی‌کند.",
    reviewTitle: "بازبینی تنظیمات کامل",
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
    direct_port: node.direct_port, direct_hosts: node.direct_hosts.map(canonicalEndpointAddress), direct_endpoints: canonicalEndpoints(node.direct_endpoints),
    tunnel_port: node.tunnel_port, tunnel_hosts: node.tunnel_hosts.map(canonicalEndpointAddress), tunnel_endpoints: canonicalEndpoints(node.tunnel_endpoints),
    swap_enabled: node.swap_enabled, swap_source_node_id: node.swap_source_node_id, swap_source_installation_id: node.swap_source_installation_id,
    swap_source_listen_ip: canonicalCreationIP(node.swap_source_listen_ip), swap_source_port: node.swap_source_port,
    swap_entry_address: canonicalEndpointAddress(node.swap_entry_address), swap_entry_port: node.swap_entry_port, swap_destination_port: node.swap_destination_port });
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
  const [section, setSection] = useState<Section>("connection");
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
  const xhttpSupported = capabilities.profile_xhttp === true && capabilities.profile_network_options_scope === "creation_plan";
  const clientOptionsSupported = capabilities.profile_client_options === true && capabilities.profile_network_options_scope === "creation_plan";
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
    const panel = surface.current?.querySelector<HTMLElement>(`#creation-panel-${issueSection(focusRequest)}`);
    const scope = focusRequest.nodeID ? panel?.querySelector<HTMLElement>(`[data-node-id="${CSS.escape(focusRequest.nodeID)}"]`) : panel;
    const control = scope?.querySelector<HTMLElement>(`[data-creation-field="${CSS.escape(focusField(focusRequest, state))}"]`) ?? Array.from(scope?.querySelectorAll<HTMLElement>("input, select, textarea, summary") ?? []).find(element => element.getClientRects().length > 0 && !element.matches(":disabled"));
    for (let ancestor = control?.parentElement; ancestor; ancestor = ancestor.parentElement) if (ancestor instanceof HTMLDetailsElement) ancestor.open = true;
    control?.focus({ preventScroll: true }); control?.scrollIntoView({ block: "nearest" });
  }, [focusRequest]);

  function update(value: CreationState) { setState(value); setPreview(null); setIssues([]); setMessage(""); setConflict(false); }
  function profile(field: Exclude<keyof CreationState["profile"], "options">, value: string) { update({ ...state, profile: { ...state.profile, [field]: value } }); }
  function close() { if (!busy && (!(dirty || uncertain) || window.confirm(t.unsaved))) onClose(); }
  function move(next: Section) { setFocusRequest(null); setSection(next); }
  function editSection(next: "connection" | "swap" | "advanced") { move(next); tabButtons.current[next]?.focus({ preventScroll: true }); }
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
      if (!xhttpSupported && (state.profile.transport === "xhttp" || options?.xhttp !== undefined)) next.push({ field: "transport", code: "profile_xhttp_unavailable" });
      if (!clientOptionsSupported && options?.client !== undefined) next.push({ field: "clientOptions", code: "profile_client_options_unavailable" });
      if (capabilities.endpoint_options !== true) for (const row of selectedCreationNodes(state)) for (const path of ["direct", "tunnel"] as const) {
        if (row[path].endpoints !== undefined || row[path].endpointsPresent) next.push({ nodeID: row.nodeID, field: path + "Endpoints", code: "endpoint_options_unavailable" });
      }
    }
    setIssues(next);
    if (!next.length) return true;
    const first = next[0];
    setSection(issueSection(first));
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
    if (issue.destinationNodeID && state.selected.includes(issue.destinationNodeID)) {
      issue = { ...issue, nodeID: issue.destinationNodeID };
      if (issue.path === "swap_source" && (issue.field === "listenIP" || issue.field === "ports")) issue = { ...issue, field: issue.field === "listenIP" ? "sourceListenIP" : "sourcePort" };
    } else if (issue.nodeID && !state.selected.includes(issue.nodeID)) {
      const destination = selectedCreationNodes(state).find(row => row.swap.enabled && row.swap.sourceNodeID === issue.nodeID && (!issue.port || creationPort(row.swap.sourcePort) === issue.port));
      if (destination) issue = { ...issue, nodeID: destination.nodeID, field: issue.field === "listenIP" || issue.field === "sourceListenIP" ? "sourceListenIP" : "sourcePort" };
    }
    setIssues([issue]);
    if (issue.nodeID) setExpanded(issue.nodeID);
    setSection(issueSection(issue));
    setFocusRequest({ ...issue });
  }
  function needsReload(error: unknown): boolean {
    return error instanceof ApiError && error.status === 409 && (!(error instanceof CreationApiError) || !error.issue || ["plan_revision_conflict", "profile_changed_or_materialized", "inbound_id_exists", "profile_options_required", "profile_sniffing_required", "profile_fallbacks_required", "profile_xhttp_required", "profile_client_options_required", "endpoint_options_required"].includes(error.issue.code));
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
        result.topology_key_scope !== (editing ? "saved_profile" : "preview_only") || !validPlan(result.plan, payload, expectedInboundRevision(payload)) || capabilities.endpoint_options === true && !validEndpointPreviews(result.endpoints, payload)) throw new Error("invalid_setup_preview");
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
  function groupIssues(group: ConnectionGroup) {
    const items = issues.filter(issue => issueSection(issue) === "connection" && connectionGroup(issue.field) === group);
    return items.length > 0 && <ul className="creation-issues" role="alert">{items.map((issue, index) => <li key={index}>{creationIssueMessage(issue, language)}</li>)}</ul>;
  }
  const invalid = (field: string) => issues.some(issue => !issue.nodeID && issue.field === field);
  return <WorkspaceDialog open title={editing ? t.edit : t.create} subtitle={t.lead} variant="inbound-editor" closeLabel={t.close} busy={busy} onClose={close}>
    <div className="inbound-creation" dir={language === "fa" ? "rtl" : "ltr"} ref={surface}>
      <nav className="creation-tabs" role="tablist" aria-label={t.sections}>
        {sections.map(item => { const Icon = item === "connection" ? SlidersHorizontal : item === "swap" ? ArrowRightLeft : item === "advanced" ? KeyRound : ClipboardCheck; return <button key={item} ref={node => { tabButtons.current[item] = node; }} type="button" role="tab" id={`creation-tab-${item}`} aria-controls={`creation-panel-${item}`} aria-selected={section === item} tabIndex={section === item ? 0 : -1} onClick={() => move(item)} onKeyDown={event => tabKey(event, item)}><Icon size={17} aria-hidden="true"/><span>{t[item]}</span></button>; })}
      </nav>
      <div className="creation-scroll">
        {readOnly && <p className="creation-message" role="status">{t.readOnly}</p>}
        {!referencesValid && <p className="creation-message" role="status">{t.staleReferences}</p>}
        <section id="creation-panel-connection" role="tabpanel" aria-labelledby="creation-tab-connection" hidden={section !== "connection"} className="creation-profile">
          <header className="creation-section-heading"><div><h3>{t.profileTitle}</h3><p>{t.profileHelp}</p></div><span className="creation-protocol"><ShieldCheck size={14}/><bdi>VLESS / REALITY</bdi></span></header>
          {source && !editing && <p className="creation-help">{t.copy}</p>}
          <details className="creation-connection-group" data-connection-group="general" open>
            <summary><SlidersHorizontal size={16} aria-hidden="true"/><span>{t.general}</span></summary>
            <div className="creation-connection-fields">
              <label className="creation-field"><span>{t.name}</span><input data-initial-focus data-creation-field="name" aria-label={t.name} aria-invalid={invalid("name")} maxLength={128} value={state.profile.name} disabled={locked} onChange={event => profile("name", event.target.value)} autoComplete="off"/></label>
              {groupIssues("general")}
            </div>
          </details>
          <details className="creation-connection-group" data-connection-group="network" open>
            <summary><Link2 size={16} aria-hidden="true"/><span>{t.network}</span></summary>
            <div className="creation-connection-fields">
              <fieldset className="creation-presets" disabled={locked}><legend>{t.preset}</legend>{["tcp", "grpc", ...(xhttpSupported ? ["xhttp"] : [])].filter(transport => capabilities.transports.includes(transport)).map(transport => { const title = transport === "tcp" ? "TCP / REALITY" : transport === "grpc" ? "gRPC / REALITY" : "XHTTP / REALITY"; return <label key={transport} data-selected={state.profile.transport === transport}><input type="radio" name="creation-transport" data-creation-field={transport === "tcp" ? "transport" : undefined} aria-label={title} checked={state.profile.transport === transport} onChange={() => profile("transport", transport)}/><span><bdi>{title}</bdi><small>{transport === "tcp" ? "Vision" : transport === "grpc" ? "gRPC" : "XHTTP"}</small></span></label>; })}</fieldset>
              {state.profile.transport === "grpc" && <label className="creation-field"><span>{t.service}</span><input aria-label={t.service} data-creation-field="grpcService" aria-invalid={invalid("grpcService")} dir="ltr" maxLength={64} value={state.profile.grpcService} disabled={locked} onChange={event => profile("grpcService", event.target.value)} autoComplete="off"/><small>{t.serviceHelp}</small></label>}
              {state.profile.transport === "grpc" && <InboundProfileAdvanced language={language} section="network" value={state.profile.options} onChange={options => update({ ...state, profile: { ...state.profile, options } })} supported={capabilities.profile_options === true} disabled={locked} invalidFields={issues.map(issue => issue.field)}/>}
              {state.profile.transport === "xhttp" && <InboundNetworkOptions language={language} section="xhttp" value={state.profile.options} onChange={options => update({ ...state, profile: { ...state.profile, options } })} supported={xhttpSupported} disabled={locked} invalidFields={issues.map(issue => issue.field)}/>}
              {groupIssues("network")}
            </div>
          </details>
          <details className="creation-connection-group" data-connection-group="security" open>
            <summary data-creation-field="profileOptions"><ShieldCheck size={16} aria-hidden="true"/><span>{t.security}</span></summary>
            <div className="creation-connection-fields">
              <div className="creation-field-grid"><label className="creation-field"><span>{t.target}</span><input aria-label={t.target} data-creation-field="realityTarget" aria-invalid={invalid("realityTarget")} dir="ltr" value={state.profile.realityTarget} disabled={locked} onChange={event => profile("realityTarget", event.target.value)} placeholder="example.org:443" autoComplete="off"/></label>
                <label className="creation-field"><span>{t.serverNames}</span><input aria-label={t.serverNames} data-creation-field="serverNames" aria-invalid={invalid("serverNames")} dir="ltr" value={state.profile.serverNames} disabled={locked} onChange={event => profile("serverNames", event.target.value)} placeholder="example.org" autoComplete="off"/></label></div>
              <div className="creation-field"><span><label htmlFor={shortIDInputID}>{t.shortIDs}</label><button type="button" aria-label={t.generate} disabled={locked || creationShortIDs(state.shortIDs).length >= 16} onClick={() => update({ ...state, shortIDs: [...creationShortIDs(state.shortIDs), freshCreationShortID()].join(", ") })}><Plus size={13}/>{t.generate}</button></span><input id={shortIDInputID} aria-label={t.shortIDs} data-creation-field="shortIDs" aria-invalid={invalid("shortIDs")} dir="ltr" value={state.shortIDs} disabled={locked} onChange={event => update({ ...state, shortIDs: event.target.value })} autoComplete="off" spellCheck={false}/><small>{t.shortHelp}</small></div>
              <p className="creation-key-note"><KeyRound size={17}/><span>{editing ? t.preserved : t.generated}</span></p>
              <InboundProfileAdvanced language={language} section="security" value={state.profile.options} onChange={options => update({ ...state, profile: { ...state.profile, options } })} supported={capabilities.profile_options === true} disabled={locked} invalidFields={issues.map(issue => issue.field)}/>
              {(clientOptionsSupported || state.profile.options?.clientPresent) && <InboundNetworkOptions language={language} section="client" value={state.profile.options} onChange={options => update({ ...state, profile: { ...state.profile, options } })} supported={clientOptionsSupported} disabled={locked} invalidFields={issues.map(issue => issue.field)}/>}
              {groupIssues("security")}
            </div>
          </details>
        </section>
        <section id="creation-panel-swap" role="tabpanel" aria-labelledby="creation-tab-swap" hidden={section !== "swap"}>
          {section === "swap" && <InboundCreationNodes language={language} state={state} nodes={nodes} targets={targets} disabled={locked} expanded={expanded} onExpanded={setExpanded} onChange={update} issues={issues} endpointOptionsSupported={capabilities.endpoint_options === true} maxEndpointsPerPath={capabilities.max_endpoints_per_path} listenerPortMin={capabilities.listener_port_min} listenerPortMax={capabilities.listener_port_max} reservedListenerPorts={capabilities.reserved_listener_ports}/>}
          {issues.filter(issue => issue.field === "nodes").map((issue, index) => <p key={index} className="creation-issues" role="alert">{creationIssueMessage(issue, language)}</p>)}
        </section>
        <section id="creation-panel-advanced" role="tabpanel" aria-labelledby="creation-tab-advanced" hidden={section !== "advanced"} className="creation-profile">
          <header className="creation-section-heading"><div><h3>{language === "fa" ? "تنظیمات پیشرفتهٔ ترافیک" : "Advanced traffic settings"}</h3><p>{language === "fa" ? "این بخش اختیاری است. Sniffing و Fallback فقط در طرح خاموش ذخیره می‌شوند؛ فعال‌سازی و انتشار اشتراک برای آن‌ها هنوز در دسترس نیست." : "This section is optional. Sniffing and fallbacks are saved only in paused setup plans; activation and subscription publishing remain unavailable."}</p></div></header>
          <InboundTrafficOptions language={language} value={state.profile.options} transport={state.profile.transport} onChange={options => update({ ...state, profile: { ...state.profile, options } })} sniffingSupported={sniffingSupported} fallbacksSupported={fallbacksSupported} disabled={locked} invalidFields={issues.map(issue => issue.field)}/>
          {issues.filter(issue => issueSection(issue) === "advanced").map((issue, index) => <p key={index} className="creation-issues" role="alert">{creationIssueMessage(issue, language)}</p>)}
          <details className="creation-availability" data-unavailable-features><summary>{language === "fa" ? "امکانات در انتظار پشتیبانی" : "Features pending support"}</summary><dl>
            <div><dt>{t.network}</dt><dd>WebSocket · HTTP Upgrade · {!xhttpSupported && "XHTTP · "}mKCP · TCP HTTP camouflage · Cloudflare / transport PROXY</dd></div>
            <div><dt>{t.security}</dt><dd>None · TLS / certificates · {!clientOptionsSupported && "REALITY Fingerprint / SpiderX · "}Target Scan / Ping</dd></div>
            <div><dt>{language === "fa" ? "تنظیمات Host" : "Host settings"}</dt><dd>{capabilities.endpoint_options !== true && "SNI Override · Fingerprint / SpiderX · "}ALPN · Path · FinalMask · Mihomo · {language === "fa" ? "حذف از قالب‌های اشتراک" : "per-format exclusion"}</dd></div>
            <div><dt>{language === "fa" ? "سایر امکانات" : "Other features"}</dt><dd>Advanced JSON · VMess / Trojan / Shadowsocks / SOCKS / HTTP · Local HUB · Dark Swap Apply / ACK / Diagnostics</dd></div>
          </dl></details>
        </section>
        <section id="creation-panel-review" role="tabpanel" aria-labelledby="creation-tab-review" hidden={section !== "review"} className="creation-review">
          <header className="creation-section-heading"><div><h3>{t.reviewTitle}</h3><p>{t.paused}</p></div></header>
          <InboundCreationReview language={language} state={state} nodes={nodes} disabled={busy} onEdit={editSection} publicKey={savedProfile.current?.public_key} checkedEndpoints={preview?.endpoints}/>
          <label className="creation-field"><span>{t.note}</span><input aria-label={t.note} data-creation-field="reason" maxLength={200} value={state.reason} disabled={locked} onChange={event => update({ ...state, reason: event.target.value })} placeholder={t.notePlaceholder}/></label>
          {issues.filter(issue => issue.field === "reason").map((issue, index) => <p key={index} className="creation-issues" role="alert">{creationIssueMessage(issue, language)}</p>)}
          <div className="creation-preview-status" data-checked={!!preview} role="status"><ShieldCheck size={18}/><span>{preview ? t.checked : t.checkFirst}</span></div>
        </section>
        {message && <div className={"creation-message" + (preview && !uncertain ? " creation-message-success" : "")} role={preview && !uncertain ? "status" : "alert"}><p>{message}</p>{conflict && editing && <button type="button" disabled={busy} onClick={() => void reload()}><RefreshCw size={14}/>{t.reload}</button>}</div>}
      </div>
      <footer className="creation-footer"><div className="creation-footer-status"><ShieldCheck size={14}/><span>{t.previewOnly}</span>{busy && <LoaderCircle size={15} className="creation-spinner"/>}</div><div className="creation-footer-actions">
        <button type="button" disabled={busy} onClick={close}>{t.cancel}</button>
        {section !== "connection" && <button type="button" disabled={busy} onClick={() => move(sections[sections.indexOf(section) - 1])}><ChevronLeft size={15}/>{t.back}</button>}
        {section !== "review" ? <button type="button" className="native-primary" disabled={busy} onClick={() => move(sections[sections.indexOf(section) + 1])}>{t.next}<ChevronRight size={15}/></button> : <>
          <button type="button" disabled={busy || blocked || uncertain || readOnly} onClick={() => void check()}><RefreshCw size={15}/>{t.check}</button>
          <button type="button" className="native-primary" disabled={busy || blocked || !preview || readOnly} onClick={() => void save()}><CheckCircle2 size={15}/>{uncertain ? t.retrySave : t.save}</button>
        </>}
      </div></footer>
    </div>
  </WorkspaceDialog>;
}
