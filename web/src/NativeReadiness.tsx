import {
  Activity, ArrowUpRight, CheckCircle2, CircleAlert, Database,
  Network, RadioTower, Server, ShieldCheck, ShieldX,
} from "lucide-react";
import "./NativeReadiness.css";

type Language = "fa" | "en";

// The shape is created by the owner-authorized HUB endpoint from already
// persisted native state + cached real Agent telemetry. It is a diagnostic
// read, never a signed Xray delivery, billing grant or launch permission.
export type NativePreflight = {
  scope: "new_pro_lab_only";
  paired_nodes: number;
  online_nodes: number;
  fresh_nodes: number;
  configured_inbounds: number;
  configured_targets: number;
  client_bindings: number;
  usage_ledger_enabled: boolean;
  remote_metered_admission: "trusted_remote_epoch_registration_pending";
  subscription_published: false;
  customer_data_plane_verified: false;
  deployment_ready: false;
  blocking_reason: "remote_metered_admission_not_wired";
};

const words = {
  en: {
    eyebrow: "DEPLOYMENT READINESS  /  CONTROLLED STAGING",
    title: "Native Xray · release gates",
    lead: "Real HUB state, not a simulated Node deployment. Validate prerequisites before accepting a customer listener.",
    blocked: "Customer activation blocked", blockedNote: "The trusted remote process-birth + initial usage receipt handshake is not connected to the production Agent yet. The HUB intentionally refuses metered runtime delivery (503).",
    paired: "Paired Nodes", online: "Online Agents", fresh: "Fresh telemetry", inbound: "Enabled Inbounds",
    targets: "Node targets", bindings: "Client bindings",
    step1: "Agent transport & identity", step1Note: "New paired PRO Agent sending a fresh authenticated heartbeat and host metrics",
    step2: "Native configuration", step2Note: "Saved real Inbound profile, explicit Node target and non-overlapping Direct/Swap ports",
    step3: "Accounting journal", step3Note: "Private PRO usage ledger must be enabled; incomplete imported history must remain ineligible",
    step4: "First-use receipt barrier", step4Note: "Trusted process birth, signed meter epoch, durable zero report and HUB receipt before opening any customer port",
    step5: "End-to-end customer proof", step5Note: "Actual owned listener, encrypted handshake, attributed bytes, quota/revocation and rollback on this Node",
    done: "Verified prerequisite", pending: "Pending", blockedStep: "Requires implementation",
    note: "Monitoring ONLINE is not proof of Xray listener readiness, subscription delivery or customer accounting.",
    viewNodes: "Inspect live Nodes",
    scope: "NEW PRO only · old DARK XRAY subscriptions never imported automatically",
  },
  fa: {
    eyebrow: "DEPLOYMENT READINESS  /  آماده‌سازی کنترل‌شده",
    title: "آمادگی استقرار Native Xray",
    lead: "وضعیت واقعی HUB، نه استقرار نمایشی؛ قبل از پذیرش اتصال مشتری همهٔ پیش‌شرط‌ها بررسی می‌شوند.",
    blocked: "فعال‌سازی مشتری مسدود است", blockedNote: "مسیر تأیید هویت فرایند واقعی نود و رسید اولیهٔ مصرف هنوز به Agent عملیاتی متصل نیست. HUB عمداً درخواست اجرای Xray همراه حسابداری را با 503 رد می‌کند.",
    paired: "نودهای جفت‌شده", online: "Agentهای آنلاین", fresh: "آمار تازه", inbound: "اینباندهای فعال",
    targets: "مقصدهای نود", bindings: "اتصال کلاینت",
    step1: "هویت و ارتباط Agent", step1Note: "Agent جدید PRO با احراز هویت و گزارش منابع تازه",
    step2: "پیکربندی واقعی Inbound", step2Note: "اینباند ذخیره‌شده، مقصد مشخص و پورت‌های جداگانهٔ Direct/Swap",
    step3: "دفتر حسابداری", step3Note: "ثبت مصرف خصوصی PRO باید فعال باشد؛ تاریخچهٔ واردشده با مقدار نامعلوم اجازهٔ اتصال ندارد",
    step4: "سد تأیید اولین مصرف", step4Note: "هویت پردازش Xray، دورهٔ حسابداری امضاشده و رسید ثبت‌شدهٔ صفر اولیه قبل از بازشدن هر پورت مشتری",
    step5: "تأیید واقعی اتصال و حسابداری", step5Note: "پورت واقعی، Handshake، مصرف ثبت‌شده، سهمیه، قطع دسترسی و Rollback روی همین نود",
    done: "پیش‌شرط تأیید شد", pending: "در انتظار", blockedStep: "نیازمند تکمیل",
    note: "Online بودن مانیتور نود، اثبات فعال‌بودن Xray یا تحویل اشتراک و صحت حسابداری نیست.",
    viewNodes: "مشاهده نودهای زنده",
    scope: "فقط PRO جدید · اشتراک‌های DARK XRAY قدیمی خودکار منتقل نمی‌شوند",
  },
} as const;

function count(value: unknown): number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 && value <= 16384
    ? value : 0;
}

export default function NativeReadiness({ preflight, language, stale }: {
  preflight?: NativePreflight; language: Language; stale: boolean;
}) {
  const t = words[language];
  // Missing or malformed preflight never makes the run eligible. No Node
  // readiness or enabled accounting is inferred from old client-side data.
  const valid = preflight?.scope === "new_pro_lab_only" &&
    preflight?.remote_metered_admission === "trusted_remote_epoch_registration_pending" &&
    preflight?.blocking_reason === "remote_metered_admission_not_wired" &&
    preflight?.deployment_ready === false &&
    preflight?.customer_data_plane_verified === false &&
    preflight?.subscription_published === false &&
    typeof preflight.usage_ledger_enabled === "boolean";
  const p = valid && !stale ? preflight : undefined;
  const online = p !== undefined && count(p.online_nodes) > 0 && count(p.fresh_nodes) > 0;
  const nativeProfile = p !== undefined && count(p.configured_inbounds) > 0 && count(p.configured_targets) > 0;
  const ledger = p?.usage_ledger_enabled === true;
  const steps = [
    { label: t.step1, note: t.step1Note, done: online, Icon: RadioTower },
    { label: t.step2, note: t.step2Note, done: nativeProfile, Icon: Server },
    { label: t.step3, note: t.step3Note, done: ledger, Icon: Database },
    { label: t.step4, note: t.step4Note, done: false, Icon: ShieldX, blocked: true },
    { label: t.step5, note: t.step5Note, done: false, Icon: Network, blocked: true },
  ];
  const stats = [
    { label: t.paired, value: p?.paired_nodes, Icon: Server },
    { label: t.online, value: p?.online_nodes, Icon: RadioTower },
    { label: t.fresh, value: p?.fresh_nodes, Icon: Activity },
    { label: t.inbound, value: p?.configured_inbounds, Icon: Network },
    { label: t.targets, value: p?.configured_targets, Icon: CheckCircle2 },
    { label: t.bindings, value: p?.client_bindings, Icon: Database },
  ];

  return <section className="native-preflight" aria-label="Native deployment readiness">
    <header className="native-preflight-header">
      <div>
        <span className="native-preflight-eyebrow"><Activity size={14}/>{t.eyebrow}</span>
        <h3>{t.title}</h3>
        <p>{t.lead}</p>
      </div>
      <span className="native-preflight-blocked" role="status"><CircleAlert size={16}/>{t.blocked}</span>
    </header>
    <div className="native-preflight-stats">
      {stats.map(({label,value,Icon}) =>
        <div key={label}><Icon size={15}/><span>{label}</span>
          <strong>{p && value !== undefined && Number.isSafeInteger(value) && value >= 0 ? value : "—"}</strong>
        </div>)}
    </div>
    <div className="native-preflight-steps">
      {steps.map(({label,note,done,Icon,...rest},index) => <div className={"native-preflight-step" +
        (done ? " is-complete" : " is-pending")} key={label}>
        <span className="native-preflight-step-icon"><Icon size={18}/></span>
        <div><small dir="ltr">0{index+1}</small><h4>{label}</h4><p>{note}</p></div>
        <span className="native-preflight-step-status">{done ? t.done : rest.blocked ? t.blockedStep : t.pending}</span>
      </div>)}
    </div>
    <div className="native-preflight-footer">
      <p><ShieldCheck size={16}/><span><strong>{t.blocked}</strong> · {t.blockedNote}</span></p>
      <div className="native-preflight-footer-actions">
        <a href="/nodes">{t.viewNodes}<ArrowUpRight size={15}/></a>
        <span>{t.scope}</span>
      </div>
      <small>{t.note}</small>
    </div>
  </section>;
}
