import NativeInbounds from "./NativeInbounds";
import { useState } from "react";
import { AlertTriangle, ArrowRightLeft, CheckCircle2, LockKeyhole, Server, Waypoints } from "lucide-react";
import type { NodeSnapshot } from "./api";

type Language = "en" | "fa";

const labels = {
  en: {
    title: "Inbound deployment draft", lead: "Build a Direct + Dark Swap plan for one NEW PRO Node. Nothing here is saved or applied.",
    target: "Target Node", selectNode: "Choose a fresh PRO Node", inbound: "Inbound ID",
    direct: "Direct", directPort: "Direct Node port", swap: "Dark Swap",
    tunnelPort: "Destination tunnel port", nodePort: "Node port", tunnelAddress: "Tunnel host or IPv4",
    preview: "Read-only route preview", noNodes: "No independently paired PRO Nodes are available yet.",
    noNode: "Select a paired new Node to preview the route.", missingInbound: "Enter a positive numeric Inbound ID.",
    missingRoute: "Enable at least one route.", badDirect: "Direct Node port must be 1–65535.",
    badSwapTunnel: "Tunnel port must be 1–65535.", badSwapNode: "Dark Swap Node port must be 1–65535.",
    badSwapAddress: "Use a valid IPv4 address or DNS hostname without scheme, spaces or a port.",
    shared: "Shared Node port: Direct/Swap traffic cannot be reliably split for accounting.",
    distinct: "Distinct Node ports: paths can be tagged separately; billing is NOT verified.",
    pending: "Activation blocked", next: "Requires new Agent verification, route probes, REALITY handshake, live rollback and owner approval.",
    unsafe: "PREVIEW ONLY · not persisted · no Xray changes · no subscription changes.",
    directPath: "Direct", swapPath: "Dark Swap",
  },
  fa: {
    title: "پیش‌نویس اینباند", lead: "مسیر Direct و Dark Swap را فقط برای یک نود جدید PRO طراحی کن. هیچ تنظیمی ذخیره یا اجرا نمی‌شود.",
    target: "نود مقصد", selectNode: "انتخاب نود جدید PRO", inbound: "شناسه اینباند",
    direct: "مستقیم (Direct)", directPort: "پورت نود مستقیم", swap: "دارک سواپ",
    tunnelPort: "پورت تانل مقصد", nodePort: "پورت نود", tunnelAddress: "هاست یا IP تانل",
    preview: "پیش‌نمایش فقط‌خواندنی", noNodes: "هنوز نود مستقلی به PRO متصل نشده است.",
    noNode: "برای پیش‌نمایش، یک نود جدید متصل انتخاب کن.", missingInbound: "شناسه عددی معتبر و مثبت اینباند را وارد کن.",
    missingRoute: "حداقل یکی از مسیرها را فعال کن.", badDirect: "پورت مستقیم باید بین ۱ تا ۶۵۵۳۵ باشد.",
    badSwapTunnel: "پورت تانل باید بین ۱ تا ۶۵۵۳۵ باشد.", badSwapNode: "پورت دارک سواپ باید بین ۱ تا ۶۵۵۳۵ باشد.",
    badSwapAddress: "آدرس IPv4 یا دامنه معتبر، بدون پروتکل، فاصله و پورت وارد کن.",
    shared: "پورت مشترک: مصرف Direct و Swap جداگانه قابل انتساب قطعی نیست.",
    distinct: "پورت‌های مجزا: امکان برچسب‌گذاری مسیرها وجود دارد؛ محاسبه مصرف هنوز تأیید نشده.",
    pending: "فعال‌سازی مسدود", next: "نیازمند تأیید Agent جدید، تست مسیر، اتصال REALITY، Rollback واقعی و تأیید مالک.",
    unsafe: "فقط پیش‌نمایش · بدون ذخیره · بدون تغییر Xray · بدون تغییر اشتراک.",
    directPath: "مستقیم", swapPath: "دارک سواپ",
  },
} as const;

function port(value: string): number | null {
  if (!/^[0-9]{1,5}$/.test(value)) return null;
  const parsed = Number(value);
  return parsed >= 1 && parsed <= 65535 ? parsed : null;
}
function inboundId(value: string): number | null {
  if (!/^[0-9]+$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : null;
}
function tunnelHost(value: string): boolean {
  if (!value || value.length > 253 || value.trim() !== value || /[\s/:@\\]/.test(value)) return false;
  if (/^[\d.]+$/.test(value)) {
    const octets = value.split(".");
    return octets.length === 4 && octets.every((part) =>
      /^\d{1,3}$/.test(part) && (part.length === 1 || !part.startsWith("0")) && Number(part) <= 255);
  }
  const parts = value.split(".");
  return parts.length >= 2 && parts.every((part) => /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?$/.test(part));
}

// Legacy LAB preview remains non-persistent. The native owner workflow is
// displayed when its separate private session/backend capability is present.
function DraftInboundPlanner({ nodes, language }: { nodes: NodeSnapshot[]; language: Language }) {
  const t = labels[language];
  const [nodeId, setNodeId] = useState("");
  const [inboundText, setInboundText] = useState("");
  const [enableDirect, setEnableDirect] = useState(true);
  const [directText, setDirectText] = useState("");
  const [enableSwap, setEnableSwap] = useState(false);
  const [tunnelText, setTunnelText] = useState("");
  const [swapPortText, setSwapPortText] = useState("");
  const [hostText, setHostText] = useState("");
  const chosen = nodes.find((node) => node.node_id === nodeId);
  const inbound = inboundId(inboundText);
  const directPort = port(directText);
  const swapPort = port(swapPortText);
  const tunnelPort = port(tunnelText);
  const errors: string[] = [];
  if (!chosen) errors.push(t.noNode);
  if (inbound === null) errors.push(t.missingInbound);
  if (!enableDirect && !enableSwap) errors.push(t.missingRoute);
  if (enableDirect && directPort === null) errors.push(t.badDirect);
  if (enableSwap) {
    if (tunnelPort === null) errors.push(t.badSwapTunnel);
    if (swapPort === null) errors.push(t.badSwapNode);
    if (!tunnelHost(hostText)) errors.push(t.badSwapAddress);
  }
  const ready = errors.length === 0;
  const sharedPort = enableDirect && enableSwap && directPort !== null && directPort === swapPort;
  return (
    <section className="inbound-planner">
      <div className="inbound-planner-heading">
        <span className="inbound-planner-icon"><Waypoints size={22} /></span>
        <div><h2>{t.title}</h2><p>{t.lead}</p></div>
        <span className="inbound-lock"><LockKeyhole size={15} />LAB DRAFT</span>
      </div>
      {nodes.length === 0 && <div className="inbound-empty"><Server size={18}/>{t.noNodes}</div>}
      <div className="inbound-planner-fields">
        <label className="inbound-field">{t.target}
          <select aria-label={t.target} value={nodeId} onChange={(event) => setNodeId(event.target.value)} disabled={nodes.length === 0}>
            <option value="">{t.selectNode}</option>
            {nodes.map((node) => <option key={node.node_id} value={node.node_id}>{node.name} ({node.node_id})</option>)}
          </select>
        </label>
        <label className="inbound-field">{t.inbound}
          <input aria-label={t.inbound} value={inboundText} inputMode="numeric" placeholder="1001" maxLength={20}
            onChange={(event) => setInboundText(event.target.value)} />
        </label>
      </div>
      <div className="inbound-route-grid">
        <fieldset className="inbound-route">
          <legend><label><input type="checkbox" checked={enableDirect} onChange={(event) => setEnableDirect(event.target.checked)} />{t.direct}</label></legend>
          {enableDirect ? <label className="inbound-field">{t.directPort}
            <input aria-label={t.directPort} value={directText} inputMode="numeric" placeholder="443" maxLength={5}
              onChange={(event) => setDirectText(event.target.value)} />
          </label> : <p className="inbound-route-off">—</p>}
        </fieldset>
        <fieldset className="inbound-route">
          <legend><label><input type="checkbox" checked={enableSwap} onChange={(event) => setEnableSwap(event.target.checked)}/>{t.swap}</label></legend>
          {enableSwap ? <div className="inbound-swap-fields">
            <label className="inbound-field">{t.tunnelPort}
              <input aria-label={t.tunnelPort} value={tunnelText} inputMode="numeric" placeholder="6800" maxLength={5}
                onChange={(event) => setTunnelText(event.target.value)} />
            </label>
            <label className="inbound-field">{t.nodePort}
              <input aria-label={t.nodePort} value={swapPortText} inputMode="numeric" placeholder="8443" maxLength={5}
                onChange={(event) => setSwapPortText(event.target.value)} />
            </label>
            <label className="inbound-field">{t.tunnelAddress}
              <input aria-label={t.tunnelAddress} value={hostText} placeholder="node.example.net" maxLength={253}
                onChange={(event) => setHostText(event.target.value)} />
            </label>
          </div> : <p className="inbound-route-off">—</p>}
        </fieldset>
      </div>
      <section className="inbound-preview" role="region" aria-label="Inbound route preview">
        <div className="inbound-preview-heading"><h3><ArrowRightLeft size={18}/>{t.preview}</h3><span>{t.pending}</span></div>
        {ready ? (
          <div className="inbound-preview-rows">
            {enableDirect && <div><CheckCircle2 size={17}/><strong>{t.directPath}</strong><span>Node {chosen?.node_id} · Inbound {inbound} · :{directPort}</span></div>}
            {enableSwap && <div><CheckCircle2 size={17}/><strong>{t.swapPath}</strong><span>{hostText}:{tunnelPort} → Node {chosen?.node_id}:{swapPort} · Inbound {inbound}</span></div>}
            {enableSwap && enableDirect && <p className={sharedPort ? "inbound-warn" : "inbound-muted"}>{sharedPort ? t.shared : t.distinct}</p>}
          </div>
        ) : <ul className="inbound-errors">{errors.map((error) => <li key={error}>{error}</li>)}</ul>}
        <div className="inbound-preview-boundary"><AlertTriangle size={17}/><span><strong>{t.unsafe}</strong> {t.next}</span></div>
      </section>
    </section>
  );
}
export default function InboundPlanner({ nodes, language, workspace }: { nodes: NodeSnapshot[]; language: Language; workspace?: "inbounds" | "dark-swap" }) {
 return <NativeInbounds language={language} workspace={workspace} fallback={<DraftInboundPlanner nodes={nodes} language={language} />} />;
}
