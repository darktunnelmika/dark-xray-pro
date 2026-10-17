import { useEffect, useState, type FormEvent } from "react";
import { Check, ClipboardCopy, KeyRound, Link2, RefreshCw, ShieldCheck, ShieldPlus, XCircle } from "lucide-react";
import { api, mutation, ApiError, useSession, type Language } from "./session";
import "./NodeManager.css";

type Registration = {
  node_id: string;
  name: string;
  paired_at: string;
  status: "online" | "offline";
  metrics_current: boolean;
  last_seen: string | null;
};
type Roster = { nodes: Registration[]; mode: "new_pro_lab_only" };
type Ticket = { node_id: string; name: string; pair_code: string; expires_at: string };

const words = {
  en: {
    title: "Add a new PRO Node", lead: "Only freshly installed PRO Agents. No existing DARK XRAY Nodes are imported or modified.",
    nodeId: "New Node ID", nodeName: "Display name", generate: "Generate one-time Pair Code",
    ticketTitle: "Pair Code · shown only once", expires: "Expires", copy: "Copy Pair Code",
    copied: "Copied", cancel: "Cancel unused code", failed: "Operation was not accepted",
    conflict: "This Node ID is already paired or has an unexpired invitation. Cancel the invitation or use another ID.",
    invalid: "Enter a valid new Node ID and name.", unavailable: "Node registry is unavailable. Nothing was changed.",
    steps: "On a fresh test server, install the PRO Agent, verify the HUB TLS certificate and its pin, then enter this code securely. Keep the Agent identity file. This does not install an Agent or enable customer Xray by itself.",
    boundary: "PRO LAB only · no old Nodes · no deletion · no Xray or subscription activation",
    roster: "Registered NEW PRO Nodes", empty: "No freshly paired PRO Nodes yet.",
    online: "Online", offline: "Offline", delayed: "Metrics delayed", lastSeen: "Last heartbeat",
    refreshing: "Checking registrations", retry: "Refresh list", registryIssue: "Cannot load registered Nodes.",
    codeLost: "After refresh or leaving this page the code cannot be viewed again. Cancel a pending invitation by its ID, or wait for expiry.",
    codeCancelled: "Pending Pair Code revoked. No Node or client was changed.",
    cancelOther: "Cancel invitation by ID", cancelId: "Unclaimed Node ID", cancelSubmit: "Revoke unused invitation",
    cancelledNotFound: "No unclaimed invitation exists for that Node ID.",
  },
  fa: {
    title: "افزودن نود جدید PRO", lead: "فقط Agent جدید و مستقل PRO؛ هیچ نود یا هویت پنل قدیمی منتقل یا تغییر داده نمی‌شود.",
    nodeId: "شناسه نود جدید", nodeName: "نام نمایشی", generate: "ساخت کد اتصال یک‌بارمصرف",
    ticketTitle: "کد اتصال · فقط همین بار نمایش داده می‌شود", expires: "انقضا", copy: "کپی کد اتصال",
    copied: "کپی شد", cancel: "لغو کد استفاده‌نشده", failed: "درخواست پذیرفته نشد",
    conflict: "این شناسه قبلاً متصل شده یا کد فعال دارد. کد قبلی را لغو کن یا شناسه تازه بزن.",
    invalid: "شناسه و نام نود جدید را به‌درستی وارد کن.", unavailable: "دفتر نودها در دسترس نیست؛ تغییری اعمال نشد.",
    steps: "روی یک سرور آزمایشی تازه، Agent مخصوص PRO را نصب کن؛ گواهی TLS و اثرانگشت HUB را تأیید کن، سپس این کد را محرمانه وارد کن. فایل هویت Agent باید حفظ شود. این فرم به‌تنهایی Agent یا Xray مشتریان را نصب و فعال نمی‌کند.",
    boundary: "فقط PRO آزمایشی · بدون دست‌زدن به نودهای قدیمی · بدون حذف · بدون فعال‌سازی Xray یا اشتراک",
    roster: "نودهای تازهٔ ثبت‌شده در PRO", empty: "هنوز نود تازه‌ای Pair نشده است.",
    online: "آنلاین", offline: "آفلاین", delayed: "آمار با تأخیر", lastSeen: "آخرین Heartbeat",
    refreshing: "در حال دریافت لیست نودها", retry: "بازخوانی", registryIssue: "فهرست نودها قابل دریافت نیست.",
    codeLost: "بعد از رفرش یا خروج از این بخش، کد دوباره قابل نمایش نیست؛ می‌توانی دعوت در انتظار را با شناسه لغو کنی یا تا انقضا صبر کنی.",
    codeCancelled: "کد در انتظار باطل شد؛ هیچ نود یا کلاینتی تغییر نکرد.",
    cancelOther: "لغو کد قبلی با شناسه", cancelId: "شناسهٔ دعوت استفاده‌نشده", cancelSubmit: "باطل‌کردن دعوت",
    cancelledNotFound: "برای این شناسه کد استفاده‌نشده‌ای وجود ندارد.",
  },
} as const;

function readableTime(iso: string | null, language: Language): string {
  if (!iso) return "—";
  const parsed = new Date(iso);
  return Number.isFinite(parsed.getTime()) ? parsed.toLocaleString(language === "fa" ? "fa-IR" : "en-GB") : "—";
}

export default function NodeManager({ language }: { language: Language }) {
  const { session } = useSession();
  const t = words[language];
  const [roster, setRoster] = useState<Registration[] | null>(null);
  const [rosterFailed, setRosterFailed] = useState(false);
  const [nodeId, setNodeId] = useState("");
  const [name, setName] = useState("");
  const [cancelId, setCancelId] = useState("");
  const [ticket, setTicket] = useState<Ticket | null>(null);
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState(false);
  const [clock, setClock] = useState(Date.now());

  const load = async (signal?: AbortSignal) => {
    const result = await api<Roster>("/api/v1/workspace/nodes", { signal });
    if (result.mode !== "new_pro_lab_only" || !Array.isArray(result.nodes)) throw new Error("invalid_node_roster");
    setRoster(result.nodes);
    setRosterFailed(false);
  };
  useEffect(() => {
    const controller = new AbortController();
    const fetchRoster = () => { if (!document.hidden) void load(controller.signal).catch(() => { if (!controller.signal.aborted) setRosterFailed(true); }); };
    fetchRoster();
    const timer = window.setInterval(fetchRoster, 10000);
    return () => { controller.abort();window.clearInterval(timer); };
    // The backend list has an independent owner authorization check.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session?.user.id]);

  useEffect(() => {
    const timer = window.setInterval(() => setClock(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, []);
  const remaining = ticket ? Math.max(0, Date.parse(ticket.expires_at) - clock) : 0;
  const liveTicket = ticket && remaining > 0 ? ticket : null;

  function messageFor(error: unknown): string {
    if (error instanceof ApiError) {
      if (error.status === 409) return t.conflict;
      if (error.status === 422 || error.status === 400) return t.invalid;
      if (error.status === 404) return t.cancelledNotFound;
    }
    return t.unavailable;
  }
  async function issue(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (!session || busy) return;
    setBusy(true);setNotice("");setCopied(false);
    try {
      const result = await mutation<Ticket>("/api/v1/workspace/nodes/invites", {
        node_id: nodeId.trim(), name: name.trim(),
      }, session.csrf_token);
      if (result.node_id !== nodeId.trim() || !/^[a-f0-9]{64}$/.test(result.pair_code) ||
          !(Date.parse(result.expires_at) > Date.now())) throw new Error("unexpected_pairing_response");
      setTicket(result);
      setCancelId(result.node_id);
    } catch (err) { setTicket(null); setNotice(messageFor(err)); }
    finally { setBusy(false); }
  }
  async function revoke(id: string) {
    if (!session || busy || !id) return;
    setBusy(true);setNotice("");setCopied(false);
    try {
      await mutation("/api/v1/workspace/nodes/invites/" + encodeURIComponent(id) + "/cancel", {}, session.csrf_token);
      setTicket(null);setNotice(t.codeCancelled);
    } catch(err) { setNotice(messageFor(err)); }
    finally { setBusy(false); }
  }
  async function copyCode() {
    if (!liveTicket) return;
    try { await navigator.clipboard.writeText(liveTicket.pair_code); setCopied(true); }
    catch { setCopied(false); }
  }

  return (
    <section className="pro-node-manager" aria-label={t.title}>
      <header className="pro-node-manager-title">
        <span className="pro-node-manager-icon"><ShieldPlus size={22}/></span>
        <div><h2>{t.title}</h2><p>{t.lead}</p></div>
      </header>
      <form className="pro-node-manager-form" onSubmit={e => { void issue(e); }}>
        <label>{t.nodeId}
          <input value={nodeId} onChange={e => setNodeId(e.target.value)} required maxLength={64}
            pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,63}" placeholder="new-pro-node-01" autoComplete="off"/>
        </label>
        <label>{t.nodeName}
          <input value={name} onChange={e => setName(e.target.value)} required maxLength={80}
            placeholder={language === "fa" ? "مثلاً: نود آزمایشی آلمان" : "e.g. New QA Germany"} autoComplete="off"/>
        </label>
        <button type="submit" disabled={busy}><KeyRound size={16}/>{t.generate}</button>
      </form>
      {liveTicket && (
        <div className="pro-node-ticket" role="region" aria-label={t.ticketTitle}>
          <div className="pro-node-ticket-top"><strong>{t.ticketTitle}</strong><span>{t.expires}: {readableTime(liveTicket.expires_at,language)} ({Math.ceil(remaining / 1000)}s)</span></div>
          <code className="pro-node-ticket-code">{liveTicket.pair_code}</code>
          <div className="pro-node-ticket-actions">
            <button type="button" disabled={busy} onClick={() => { void copyCode(); }}>{copied ? <Check size={15}/> : <ClipboardCopy size={15}/>} {copied ? t.copied : t.copy}</button>
            <button type="button" disabled={busy} onClick={() => { void revoke(liveTicket.node_id); }}><XCircle size={15}/>{t.cancel}</button>
          </div>
        </div>
      )}
      {notice && <p className="pro-node-notice" role="status">{notice}</p>}
      <div className="pro-node-instructions">
        <ShieldCheck size={18}/><p>{t.steps}</p>
      </div>
      <div className="pro-node-cancel">
        <strong>{t.cancelOther}</strong>
        <label>{t.cancelId}
          <input value={cancelId} onChange={e=>setCancelId(e.target.value)} maxLength={64}
            pattern="[A-Za-z0-9][A-Za-z0-9_-]{0,63}" placeholder="new-pro-node-01" autoComplete="off"/>
        </label>
        <button type="button" disabled={busy || !/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(cancelId)}
          onClick={() => { void revoke(cancelId); }}>{t.cancelSubmit}</button>
        <small>{t.codeLost}</small>
      </div>
      <div className="pro-node-roster">
        <div className="pro-node-roster-top">
          <h3><Link2 size={18}/>{t.roster}</h3>
          <button type="button" onClick={() => { void load().catch(() => setRosterFailed(true)); }}><RefreshCw size={15}/>{t.retry}</button>
        </div>
        {rosterFailed && <p role="alert">{t.registryIssue}</p>}
        {!roster && !rosterFailed && <p>{t.refreshing}</p>}
        {roster?.length === 0 && <p>{t.empty}</p>}
        {roster?.map(node => (
          <div className="pro-node-roster-row" key={node.node_id}>
            <div><strong>{node.name}</strong><small>{node.node_id} · {t.lastSeen}: {readableTime(node.last_seen, language)}</small></div>
            <span className={"pro-node-roster-status " + (node.status === "online" ? "is-online" : "is-offline")}>
              {node.status === "online" ? t.online : t.offline}{node.status === "online" && !node.metrics_current ? " · "+t.delayed : ""}
            </span>
          </div>
        ))}
      </div>
      <p className="pro-node-boundary"><ShieldCheck size={15}/>{t.boundary}</p>
    </section>
  );
}
