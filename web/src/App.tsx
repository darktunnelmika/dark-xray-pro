import { useEffect, useRef, useState } from "react";
import {
  ArchiveRestore, ArrowLeftRight, Bot, CheckCircle2, ChevronDown, ChevronRight, DatabaseBackup,
  CircleHelp, CreditCard, Globe2, HardDrive, LayoutDashboard,
  Menu, Moon, Network, Server, Settings2, ShieldCheck, Users, FolderOpen, UserRound, LogOut,
  Sun, Waypoints, ShoppingBag, X,
} from "lucide-react";
import { getFleet, type FleetSnapshot, type NodeSnapshot } from "./api";
import InboundPlanner from "./InboundPlanner";
import NodeManager from "./NodeManager";
import NodeHistory from "./NodeHistory";
import DashboardOverview from "./DashboardOverview";
import BusinessWorkspace from "./BusinessWorkspace";
import ModuleStatus from "./ModuleStatus";
import { useSession } from "./session";
import { allowed, useNavigation, type Page } from "./navigation";
import LoginPage from "./LoginPage";

type Theme = "light" | "dark" | "system";
type Language = "en" | "fa";

function stored<T extends string>(key: string, fallback: T, allowed: readonly T[]): T {
  try {
    const value = localStorage.getItem(key);
    return allowed.find((item) => item === value) ?? fallback;
  } catch {
    return fallback;
  }
}

const translations = {
  en: {
    overview: "Overview", nodes: "Nodes", inbounds: "Inbounds", settings: "Settings",
    infrastructure: "Infrastructure", management: "Customer & sales", recovery: "Backup & recovery", workspaceGroup: "Account & settings",
    "dark-swap": "Dark Swap", "dark-restore": "Dark Restore", telegram: "Telegram", backups: "Backups",
    "dark-swapTitle": "Dark Swap", "dark-swapLead": "Plan and review saved connection routes between Nodes.",
    "dark-restoreTitle": "Dark Restore", "dark-restoreLead": "Current scope and remaining work for a separate restore workspace.",
    telegramTitle: "Telegram & MiniApp", telegramLead: "Current scope of bot connection, customer service and representative sales.",
    backupsTitle: "Backups", backupsLead: "Availability of product backup and recovery for the owner.",
    clients: "Clients", groups: "Groups", representatives: "Representatives", marketplace: "Marketplace", finance: "Finance", account: "My account",
    clientsTitle: "Clients", clientsLead: "Create and manage customers in one place.", groupsTitle: "Groups", groupsLead: "Organize customers without changing their financial history.",
    marketplaceTitle: "Plan marketplace", marketplaceLead: "Owner-defined offers, no live purchases until approved.", representativesTitle: "Representatives", representativesLead: "Accounts, permissions and representative credit.", financeTitle: "Finance", financeLead: "Account balances and immutable transaction history.", accountTitle: "My account", accountLead: "Your profile and password settings.",
    overviewTitle: "Infrastructure, at a glance.", overviewLead: "Monitor your HUB, connected Nodes and network activity in one view.",
    nodesTitle: "Node fleet", nodesLead: "Live agent heartbeat, CPU, memory and physical network rates.",
    inboundsTitle: "Inbounds & routes", inboundsLead: "Manage Inbounds, their Nodes and connection routes.",
    businessTitle: "Business & accounts", businessLead: "Private owner and representative credit, clients and immutable financial history.",
    settingsTitle: "Appearance & lab", settingsLead: "A calm interface, built for long sessions.",
    liveFleet: "Live fleet", emptyTitle: "No Nodes connected yet",
    emptyLead: "Start the isolated Go lab agent to see actual host measurements here. No fabricated sample traffic.",
    errorTitle: "Live data unavailable", errorLead: "The last verified snapshot remains visible. Check the local HUB service.",
    online: "Online", offline: "Offline", total: "Total Nodes",
    download: "Fleet interface RX", upload: "Fleet interface TX",
    fleetMonitor: "Node monitor", updated: "Last received",
    memory: "RAM", iface: "Interface", cpu: "CPU", cores: "cores",
    disk: "Root disk", load: "Load (1m / 5m / 15m)", uptime: "Host uptime", tcp: "Host TCP sockets",
    units: "Default-route interface rate, not customer-billable traffic.",
    inboundsEmpty: "No Inbounds engine connected",
    inboundsNote: "Direct, Tunnel and Dark Swap will be configured per node within each Inbound. No legacy tunnel settings will be imported automatically.",
    plan: "Next build milestone", swap: "Dark Swap: tunnel port + node port + tunnel address",
    replacement: "Safe Node Replacement: pair, validate, choose fresh routes, activate",
    colors: "Color mode", system: "System", light: "Light", dark: "Dark",
    language: "Language", boundary: "Lab boundary", boundaryText: "Localhost-only server. No old Hub, Node Agent, customer database or Telegram credentials are connected.",
    developing: "Isolated development build", collecting: "Collecting live samples…",
    retry: "Retry is automatic", selected: "Select an item to view more details",
    health: "Fleet health", notLive: "Live connection interrupted", statsDelayed: "Stats delayed",
    pro: "DARK XRAY PRO", role: "Control center", soon: "Planned feature",
    inspectNode: "Inspect", nodeDetails: "Node details", nodeSelected: "Select a Node to inspect its status.",
    lastHeartbeat: "Last heartbeat", metricFreshness: "Metrics", metricLive: "Current", metricDelayed: "Delayed / unavailable",
    nodeAddress: "Interface", activationBoundary: "Read-only LAB view. Pairing and live Xray Apply/Rollback are not exposed through this browser.",
    pairingBoundary: "New LAB Node pairing is owner-only on localhost. No legacy Nodes or customer data are connected.",
    skipToContent: "Skip to content", navigationTitle: "Navigation",
  },
  fa: {
    overview: "داشبورد", nodes: "نودها", inbounds: "اینباندها", settings: "تنظیمات",
    infrastructure: "زیرساخت", management: "مشتری و فروش", recovery: "پشتیبان و بازیابی", workspaceGroup: "حساب و تنظیمات",
    "dark-swap": "دارک سواپ", "dark-restore": "دارک ریستور", telegram: "تلگرام", backups: "پشتیبان‌ها",
    "dark-swapTitle": "دارک سواپ", "dark-swapLead": "طراحی و بررسی مسیرهای ذخیره‌شدهٔ اتصال میان نودها.",
    "dark-restoreTitle": "دارک ریستور", "dark-restoreLead": "وضعیت فعلی و بخش‌های باقی‌ماندهٔ فضای مستقل ریستور.",
    telegramTitle: "تلگرام و مینی‌اپ", telegramLead: "وضعیت اتصال ربات، خدمات مشتری و فروش نمایندگان.",
    backupsTitle: "پشتیبان‌ها", backupsLead: "وضعیت پشتیبان‌گیری و بازیابی محصول با دسترسی مالک.",
    clients: "کلاینت‌ها", groups: "گروه‌ها", representatives: "نمایندگان", marketplace: "فروشگاه پلن‌ها", finance: "مالی", account: "حساب من",
    clientsTitle: "مدیریت کلاینت‌ها", clientsLead: "ساخت، تمدید و مدیریت مشتری در یک بخش مشخص.", groupsTitle: "مدیریت گروه‌ها", groupsLead: "دسته‌بندی مشتری‌ها بدون تغییر سوابق مالی.",
    marketplaceTitle: "فروشگاه پلن‌ها", marketplaceLead: "مدیریت محصولات نمایندگان؛ بدون فروش و کسر اعتبار تا تأیید عملیاتی.", representativesTitle: "مدیریت نمایندگان", representativesLead: "حساب‌ها، دسترسی و اعتبار نمایندگان.", financeTitle: "حسابداری", financeLead: "موجودی حساب و تاریخچهٔ ثبت‌شدهٔ تراکنش‌ها.", accountTitle: "حساب من", accountLead: "مشخصات حساب و تغییر رمز عبور.",
    overviewTitle: "نمای کامل زیرساخت، در یک نگاه", overviewLead: "وضعیت هاب، نودها و ترافیک شبکه را یک‌جا دنبال کن.",
    nodesTitle: "مدیریت نودها", nodesLead: "وضعیت زنده Agent، پردازنده، حافظه و سرعت شبکه.",
    inboundsTitle: "اینباندها و مسیرها", inboundsLead: "اینباندها، نودهای مقصد و مسیرهای اتصال را مدیریت کن.",
    businessTitle: "نمایندگان، کلاینت‌ها و اعتبار", businessLead: "ورود امن و مدیریت اعتبار و سوابق مالی فقط با اطلاعات واقعی.",
    settingsTitle: "ظاهر و محیط آزمایش", settingsLead: "طراحی خلوت برای استفاده طولانی‌مدت.",
    liveFleet: "وضعیت ناوگان", emptyTitle: "هنوز نودی متصل نشده",
    emptyLead: "Agent آزمایشی Go را اجرا کن تا آمار واقعی سرور نمایش داده شود. عدد ساختگی نداریم.",
    errorTitle: "داده زنده در دسترس نیست", errorLead: "آخرین اطلاعات معتبر حفظ شده است. سرویس HUB محلی را بررسی کن.",
    online: "آنلاین", offline: "آفلاین", total: "کل نودها",
    download: "دریافت شبکه نودها", upload: "ارسال شبکه نودها",
    fleetMonitor: "مانیتور نودها", updated: "آخرین دریافت",
    memory: "رم", iface: "اینترفیس", cpu: "پردازنده", cores: "هسته",
    disk: "دیسک ریشه", load: "لود (۱ / ۵ / ۱۵ دقیقه)", uptime: "زمان روشن‌بودن سرور", tcp: "سوکت‌های TCP سرور",
    units: "سرعت اینترفیس مسیر پیش‌فرض، نه مصرف قابل کسر از سهمیه کاربران.",
    inboundsEmpty: "هنوز موتور اینباند متصل نیست",
    inboundsNote: "Direct، Tunnel و Dark Swap برای هر نود داخل اینباند تنظیم می‌شوند. تونل قدیمی خودکار وارد نمی‌شود.",
    plan: "مرحله بعدی توسعه", swap: "دارک سواپ: پورت تونل + پورت نود + آدرس تونل",
    replacement: "جایگزینی ایمن نود: اتصال، بررسی، انتخاب مسیر و فعال‌سازی",
    colors: "حالت پوسته", system: "سیستم", light: "روز", dark: "شب",
    language: "زبان", boundary: "محدوده آزمایشی", boundaryText: "فقط روی localhost. هیچ HUB یا نود قدیمی، دیتابیس مشتری یا اطلاعات ربات متصل نیست.",
    developing: "نسخه آزمایشی مستقل", collecting: "در حال جمع‌آوری نمونه واقعی…",
    retry: "تلاش مجدد خودکار است", selected: "برای دیدن جزئیات یک مورد انتخاب کن",
    health: "سلامت ناوگان", notLive: "ارتباط زنده قطع شده", statsDelayed: "تأخیر در آمار",
    pro: "DARK XRAY PRO", role: "مرکز کنترل", soon: "قابلیت در دست ساخت",
    inspectNode: "جزئیات", nodeDetails: "جزئیات نود", nodeSelected: "برای بررسی وضعیت، یک نود انتخاب کن.",
    lastHeartbeat: "آخرین ضربان", metricFreshness: "وضعیت آمار", metricLive: "به‌روز", metricDelayed: "با تأخیر / ناموجود",
    nodeAddress: "اینترفیس", activationBoundary: "نمای آزمایشی فقط‌خواندنی است. اتصال نود و Apply/Rollback زنده Xray از این صفحه فعال نیست.",
    pairingBoundary: "اتصال نود جدید فقط با دسترسی مالک روی localhost انجام می‌شود. نودها و کاربران قدیمی متصل نیستند.",
    skipToContent: "رفتن به محتوای صفحه", navigationTitle: "منوی صفحه‌ها",
  },
} as const;

const nav: { id: Page; Icon: typeof LayoutDashboard }[] = [
  { id: "overview", Icon: LayoutDashboard },
  { id: "nodes", Icon: Server },
  { id: "inbounds", Icon: Waypoints },
  { id: "dark-swap", Icon: ArrowLeftRight },
  { id: "clients", Icon: Users },
  { id: "groups", Icon: FolderOpen },
  { id: "representatives", Icon: UserRound },
  { id: "marketplace", Icon: ShoppingBag },
  { id: "finance", Icon: CreditCard },
  { id: "telegram", Icon: Bot },
  { id: "backups", Icon: DatabaseBackup },
  { id: "dark-restore", Icon: ArchiveRestore },
  { id: "account", Icon: UserRound },
  { id: "settings", Icon: Settings2 },
];

type NavGroup = "infrastructure" | "management" | "recovery" | "workspaceGroup";
const navGroups: { id: NavGroup; pages: Page[] }[] = [
  { id: "infrastructure", pages: ["overview", "nodes", "inbounds", "dark-swap"] },
  { id: "management", pages: ["clients", "groups", "representatives", "marketplace", "finance", "telegram"] },
  { id: "recovery", pages: ["backups", "dark-restore"] },
  { id: "workspaceGroup", pages: ["account", "settings"] },
];

function rate(bps: number | undefined): string {
  if (bps === undefined || !Number.isFinite(bps)) return "—";
  return (bps / 1_000_000).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " Mbps";
}
function ram(bytes: number): string {
  if (!Number.isFinite(bytes)) return "—";
  return (bytes / (1024 ** 3)).toFixed(1) + " GB";
}
function dateTime(value: string | null, lang: Language): string {
  if (!value) return "—";
  const date = new Date(value);
  return Number.isFinite(date.getTime())
    ? date.toLocaleTimeString(lang === "fa" ? "fa-IR" : "en-GB", { hour: "2-digit", minute: "2-digit", second: "2-digit" })
    : "—";
}
function Meter({ value }: { value: number | null }) {
  const valid = value !== null && Number.isFinite(value);
  const n = valid ? Math.min(100, Math.max(0, value)) : 0;
  return <div className={"meter" + (valid ? "" : " meter-unknown")} aria-label={valid ? n.toFixed(0) + "%" : "Unknown"}>
    {valid && <span style={{ width: n + "%" }} />}
  </div>;
}
function Trend({ values }: { values: number[] }) {
  if (values.length < 2) return <span className="trend-empty" aria-label="Awaiting multiple readings">—</span>;
  const max = Math.max(...values, 1);
  const points = values.map((n, i) => {
    const x = (i / Math.max(1, values.length - 1)) * 112;
    const y = 32 - Math.min(1, n / max) * 27;
    return x.toFixed(1) + "," + y.toFixed(1);
  }).join(" ");
  return <svg className="trend" viewBox="0 0 112 36" preserveAspectRatio="none" aria-hidden="true"><polyline points={points} /></svg>;
}

function NodeRow({ node, history, lang, selected, onSelect }: {
  node: NodeSnapshot; history: number[]; lang: Language; selected?: boolean; onSelect?: (id: string) => void;
}) {
  const t = translations[lang];
  const good = node.status === "online";
  const current = good && node.metrics_current;
  const ramPercent = current && node.ram_total_bytes > 0 ? node.ram_used_bytes / node.ram_total_bytes * 100 : null;
  return (
    <article className={"node-row " + (good ? "" : "node-offline") + (selected ? " node-selected" : "")}>
      <div className="node-identity">
        <span className="node-glyph"><Server size={19} strokeWidth={1.7} /></span>
        <div>
          <div className="node-name">{node.name}</div>
          <div className="node-meta">{node.node_id} <span className="dot-separator">·</span> {t.updated} {dateTime(node.last_seen, lang)}{good && !current && <span className="stats-delayed"> · {t.statsDelayed}</span>}</div>
          {onSelect && <button type="button" className="node-inspect-button" aria-label={t.inspectNode + " " + node.name}
            aria-pressed={selected ?? false} onClick={() => onSelect(node.node_id)}>{t.inspectNode} <ChevronRight size={13}/></button>}
        </div>
      </div>
      <div className="node-resource">
        <div className="resource-top"><span>{t.cpu} <small>({node.cpu_cores > 0 ? node.cpu_cores : "—"} {t.cores})</small></span><strong>{current ? node.cpu_percent.toFixed(1) + "%" : "—"}</strong></div>
        <Meter value={current ? node.cpu_percent : null} />
      </div>
      <div className="node-resource">
        <div className="resource-top"><span>{t.memory}</span><strong>{current ? ram(node.ram_used_bytes) : "—"} / {node.ram_total_bytes ? ram(node.ram_total_bytes) : "—"}</strong></div>
        <Meter value={ramPercent} />
      </div>
      <div className="node-network">
        <div className="network-lines"><span>↓ {current ? rate(node.rx_bits_per_second ?? undefined) : "—"}</span><span>↑ {current ? rate(node.tx_bits_per_second ?? undefined) : "—"}</span></div>
        <Trend values={history} />
      </div>
      <div className={"status-tag " + (good ? "status-online" : "status-offline")}><i />{good ? t.online : t.offline}</div>
    </article>
  );
}

export default function App() {
  const auth = useSession();
  const { page, setPage } = useNavigation(auth.session?.user.role);
  const activeNavGroup = navGroups.find(group => group.pages.includes(page))!.id;
  const [expandedNavGroups, setExpandedNavGroups] = useState<NavGroup[]>([]);
  const [logoutError, setLogoutError] = useState(false);
  const [language, setLanguage] = useState<Language>(() => stored("dark-pro-lang", "en", ["en", "fa"]));
  const [theme, setTheme] = useState<Theme>(() => stored("dark-pro-theme", "system", ["system", "light", "dark"]));
  const [systemDark, setSystemDark] = useState(() => window.matchMedia("(prefers-color-scheme: dark)").matches);
  const [mobileMenu, setMobileMenu] = useState(false);
  const [compactNavigation, setCompactNavigation] = useState(() => window.matchMedia("(max-width: 760px)").matches);
  const sidebarRef = useRef<HTMLElement>(null);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const [fleet, setFleet] = useState<FleetSnapshot | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [history, setHistory] = useState<Record<string, number[]>>({});
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(() => {
    try { return sessionStorage.getItem("dark-pro-selected-node"); } catch { return null; }
  });
  const t = translations[language];
  const infrastructureVisible = page === "overview" || page === "nodes" || page === "inbounds" || page === "dark-swap";
  const mobileOpen = Boolean(auth.session) && compactNavigation && mobileMenu;

  useEffect(() => {
    setExpandedNavGroups([]);
  }, [page, auth.session?.user.id]);

  useEffect(() => {
    const query = window.matchMedia("(max-width: 760px)");
    const onResize = () => {
      setCompactNavigation(query.matches);
      if (!query.matches) setMobileMenu(false);
    };
    query.addEventListener("change", onResize);
    return () => query.removeEventListener("change", onResize);
  }, []);

  useEffect(() => {
    if (!mobileOpen) return;
    const sidebar = sidebarRef.current;
    if (!sidebar) return;
    const originalOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const focusable = () => Array.from(sidebar.querySelectorAll<HTMLElement>(
      'button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])',
    )).filter(element => element.getClientRects().length > 0);
    focusable()[0]?.focus({ preventScroll: true });
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") { event.preventDefault(); setMobileMenu(false); return; }
      if (event.key !== "Tab") return;
      const items = focusable();
      const first = items[0], last = items.at(-1);
      if (!first || !last) { event.preventDefault(); return; }
      if (event.shiftKey && (document.activeElement === first || !sidebar.contains(document.activeElement))) {
        event.preventDefault(); last.focus();
      } else if (!event.shiftKey && (document.activeElement === last || !sidebar.contains(document.activeElement))) {
        event.preventDefault(); first.focus();
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.body.style.overflow = originalOverflow;
      document.removeEventListener("keydown", onKeyDown);
      const opener = menuButtonRef.current;
      if (opener?.isConnected && opener.getClientRects().length > 0) opener.focus({ preventScroll: true });
      else sidebarRef.current?.querySelector<HTMLElement>('[aria-current="page"]')?.focus({ preventScroll: true });
    };
  }, [mobileOpen]);

  useEffect(() => {
    setMobileMenu(false);
    if (auth.session?.user.role === "owner") return;
    setFleet(null); setProblem(null); setHistory({});
  }, [auth.session?.user.role, auth.session?.user.id]);

  useEffect(() => {
    try {
      if (selectedNodeId) sessionStorage.setItem("dark-pro-selected-node", selectedNodeId);
      else sessionStorage.removeItem("dark-pro-selected-node");
    } catch { /* private mode */ }
  }, [selectedNodeId]);

  // Route persistence is per-tab: refresh never unexpectedly returns to Overview.
  useEffect(() => {
    try { sessionStorage.setItem("dark-pro-page", page); } catch { /* storage disabled */ }
  }, [page]);

  useEffect(() => {
    const m = window.matchMedia("(prefers-color-scheme: dark)");
    const onChange = (event: MediaQueryListEvent) => setSystemDark(event.matches);
    m.addEventListener("change", onChange);
    return () => m.removeEventListener("change", onChange);
  }, []);
  useEffect(() => {
    const active = theme === "system" ? (systemDark ? "dark" : "light") : theme;
    document.documentElement.dataset.theme = active;
    try { localStorage.setItem("dark-pro-theme", theme); } catch { /* private mode */ }
  }, [theme, systemDark]);
  useEffect(() => {
    document.documentElement.lang = language;
    document.documentElement.dir = language === "fa" ? "rtl" : "ltr";
    try { localStorage.setItem("dark-pro-lang", language); } catch { /* private mode */ }
  }, [language]);
  // Read-only polling runs only in infrastructure sections. Hidden tabs pause
  // requests entirely; visibility/online restores them immediately. No
  // billing/representative endpoints are called by the dashboard.
  useEffect(() => {
    if (!auth.session || auth.session.user.role !== "owner" || !infrastructureVisible) return;
    let active = true;
    let timer: number | undefined;
    let request: AbortController | undefined;
    async function poll() {
      if (!active || document.hidden || request) return;
      const current = new AbortController();
      request = current;
      const deadline = window.setTimeout(() => current.abort(), 4000);
      try {
        const fresh = await getFleet(current.signal);
        if (!active || current.signal.aborted) return;
        setFleet(fresh);
        setProblem(null);
        setHistory(before => {
          const after: Record<string, number[]> = {};
          for (const node of fresh.nodes) {
            const past = before[node.node_id] ?? [];
            if (node.status === "online" && node.metrics_current &&
                node.rx_bits_per_second != null && node.tx_bits_per_second != null) {
              const reading = node.rx_bits_per_second + node.tx_bits_per_second;
              after[node.node_id] = Number.isFinite(reading) && reading >= 0 ? [...past, reading].slice(-20) : past;
            } else if (past.length > 0) {
              after[node.node_id] = past;
            }
          }
          return after;
        });
      } catch (error) {
        if (active && !current.signal.aborted) setProblem(error instanceof Error ? error.message : "Unknown network error");
        else if (active && !document.hidden) setProblem("fleet_request_timeout");
      } finally {
        window.clearTimeout(deadline);
        request = undefined;
        if (active) timer = window.setTimeout(() => { void poll(); }, 5000);
      }
    }
    const resume = () => {
      if (!document.hidden && active && !request) {
        window.clearTimeout(timer);
        void poll();
      }
    };
    void poll();
    document.addEventListener("visibilitychange", resume);
    window.addEventListener("online", resume);
    return () => {
      active = false;
      window.clearTimeout(timer);
      request?.abort();
      document.removeEventListener("visibilitychange", resume);
      window.removeEventListener("online", resume);
    };
  }, [auth.session?.user.id, auth.session?.user.role, infrastructureVisible]);

  const ready = fleet !== null;
  const selectedNode = fleet?.nodes.find((node) => node.node_id === selectedNodeId) ?? null;


  const darkMode = theme === "dark" || (theme === "system" && systemDark);
  const toggleTheme = () => setTheme(darkMode ? "light" : "dark");
  if (!auth.session) return <LoginPage language={language} setLanguage={setLanguage} dark={darkMode} toggleTheme={toggleTheme} />;
  if (!allowed(page, auth.session.user.role)) return <main role="status">{language === "fa" ? "در حال بازکردن فضای مجاز حساب…" : "Opening your workspace…"}</main>;
  async function logout() { try { setLogoutError(false); await auth.signOut(); } catch { setLogoutError(true); } }
  return (
    <div className="app-frame">
      <a className="skip-link" href="#pro-content" inert={mobileOpen}>{t.skipToContent}</a>
      <aside ref={sidebarRef} id="pro-sidebar" className={"sidebar " + (mobileOpen ? "sidebar-open" : "")}
        inert={compactNavigation && !mobileOpen} aria-hidden={compactNavigation && !mobileOpen ? true : undefined}
        role={compactNavigation ? "dialog" : undefined} aria-modal={mobileOpen || undefined}
        aria-label={compactNavigation ? t.navigationTitle : undefined}>
        <div className="brand">
          <div className="brand-mark"><Network size={22} strokeWidth={1.8} /></div>
          <div><strong>DARK XRAY <span>PRO</span></strong><small>{t.role}</small></div>
          <button className="mobile-close icon-button" aria-label={language === "fa" ? "بستن منو" : "Close menu"} onClick={() => setMobileMenu(false)}><X size={20} /></button>
        </div>
        <nav className="sidebar-nav" aria-label="Main navigation">
          {navGroups.map(group => {
            const entries = nav.filter(item => group.pages.includes(item.id) && allowed(item.id, auth.session!.user.role));
            if (entries.length === 0) return null;
            const current = group.id === activeNavGroup;
            const expanded = current || expandedNavGroups.includes(group.id);
            return <div className={"nav-group" + (current ? " nav-group-current" : "")} key={group.id}>
              <button type="button" className="sidebar-caption nav-group-toggle"
                aria-expanded={expanded} aria-controls={"pro-nav-" + group.id} disabled={current}
                onClick={() => setExpandedNavGroups(open => open.includes(group.id)
                  ? open.filter(id => id !== group.id) : [...open, group.id])}>
                <span>{t[group.id]}</span><ChevronDown size={14} aria-hidden="true"/>
              </button>
              <div className="nav-group-items" id={"pro-nav-" + group.id} hidden={!expanded}>
              {entries.map(({ id, Icon }) => <button type="button" key={id}
                className={"nav-item " + (page === id ? "selected" : "")}
                aria-current={page === id ? "page" : undefined}
                onClick={() => { setPage(id); setMobileMenu(false); }}>
                <Icon size={18} strokeWidth={1.8}/><span>{t[id]}</span>
                {page === id && <ChevronRight className="nav-chevron" size={14}/>}
              </button>)}
              </div>
            </div>;
          })}
        </nav>
        <div className="sidebar-bottom">
          <div className="sidebar-safety"><ShieldCheck size={17} /><span>{t.developing}</span></div>
        </div>
      </aside>

      <div className="main-area" inert={mobileOpen}>
        <header className="topbar">
          <div className="topbar-left">
            <button ref={menuButtonRef} className="mobile-menu icon-button" aria-label={language === "fa" ? "بازکردن منو" : "Open menu"} aria-controls="pro-sidebar" aria-expanded={mobileOpen} onClick={() => setMobileMenu(true)}><Menu size={21} /></button>
            <span className="breadcrumb">{t[activeNavGroup]}</span><ChevronRight size={15} className="muted-icon" />
            <strong>{t[page]}</strong>
          </div>
          <div className="topbar-right">
            <button type="button" className="account-shortcut" aria-label={t.account + ": " + auth.session.user.username} onClick={() => setPage("account")}><UserRound size={17}/><span>{auth.session.user.username}</span></button>
            <button type="button" className="icon-button" aria-label={language === "fa" ? "خروج" : "Sign out"} onClick={() => { void logout(); }}><LogOut size={18}/></button>
            <button className="topbar-lang" onClick={() => setLanguage(language === "en" ? "fa" : "en")} aria-label="Switch language"><Globe2 size={17} />{language.toUpperCase()}</button>
            <button className="icon-button theme-shortcut" aria-label="Toggle dark and light mode" onClick={() => setTheme(theme === "dark" || (theme === "system" && systemDark) ? "light" : "dark")}>{theme === "dark" || (theme === "system" && systemDark) ? <Sun size={20} /> : <Moon size={20} />}</button>
          </div>
        </header>

        <main id="pro-content" className="workspace" tabIndex={-1}>
          {logoutError && <p role="alert">{language === "fa" ? "خروج تأیید نشد؛ دوباره تلاش کن." : "Logout was not confirmed. Please retry."}</p>}
          <section className="page-heading">
            <h1>{t[page + "Title" as keyof typeof t]}</h1>
            <p>{t[page + "Lead" as keyof typeof t]}</p>
          </section>

          {infrastructureVisible && problem && <div className="error-banner" role="alert"><CircleHelp size={20} /><div><strong>{t.errorTitle}</strong><p>{t.errorLead} <span className="technical">{problem}</span></p></div></div>}

          {page === "overview" && (
            <>
              <DashboardOverview fleet={fleet} history={history} language={language}
                stale={problem !== null}
                onInspectNode={id => { setSelectedNodeId(id); setPage("nodes"); }}/>
            </>
          )}

          {page === "nodes" && (
            <>
              <div className="section-head section-head-top">
                <div><h2>{t.liveFleet}</h2><p>{t.selected}</p></div>
                <span className="count-pill">{ready ? fleet.total_nodes : "—"} {t.total}</span>
              </div>
              <NodeTable fleet={fleet} history={history} lang={language}
                selectedNodeId={selectedNodeId} onSelect={setSelectedNodeId} />
              {selectedNode ? <>
                <NodeInspector node={selectedNode} lang={language} />
                <NodeHistory nodeID={selectedNode.node_id} language={language}/>
              </> : (
                <div className="node-manager-note" role="status">
                  <ShieldCheck size={19}/>
                  <div><strong>{ready && fleet.nodes.length === 0 ? t.emptyTitle : t.nodeSelected}</strong>
                    <p>{ready && fleet.nodes.length === 0 ? t.pairingBoundary : t.activationBoundary}</p></div>
                </div>
              )}
              <NodeManager language={language} />
              <div className="notice-line"><ShieldCheck size={18} />{t.boundaryText}</div>
            </>
          )}

          {page === "inbounds" && <InboundPlanner nodes={fleet?.nodes ?? []} language={language} />}
          {page === "dark-swap" && <InboundPlanner nodes={fleet?.nodes ?? []} language={language} workspace="dark-swap" />}
          {(page === "dark-restore" || page === "telegram" || page === "backups") &&
            <ModuleStatus page={page} language={language} onNavigate={setPage}/>}
          {(["clients", "groups", "representatives", "marketplace", "finance", "account"] as Page[]).includes(page) && <BusinessWorkspace key={auth.session.user.id + ":" + page} language={language} section={page as "clients" | "groups" | "representatives" | "marketplace" | "finance" | "account"} />}

          {page === "settings" && (
            <div className="settings-list">
              <section className="settings-row"><div><h2><Sun size={19} /> {t.colors}</h2><p>{t.settingsLead}</p></div>
                <div className="segment theme-picker" role="group" aria-label="Color mode">
                  {(["light", "dark", "system"] as Theme[]).map((mode) =>
                    <button className={theme === mode ? "active" : ""} key={mode} aria-pressed={theme === mode} onClick={() => setTheme(mode)}>
                      <span className={"theme-preview theme-preview-" + mode} aria-hidden="true"><i/><span><b/><b/></span></span>{t[mode]}
                    </button>)}
                </div>
              </section>
              <section className="settings-row"><div><h2><Globe2 size={19}/>{t.language}</h2><p>English / فارسی</p></div>
                <div className="segment" role="group" aria-label="Language">
                  <button className={language === "en" ? "active" : ""} aria-pressed={language === "en"} onClick={() => setLanguage("en")}>English</button>
                  <button className={language === "fa" ? "active" : ""} aria-pressed={language === "fa"} onClick={() => setLanguage("fa")}>فارسی</button>
                </div>
              </section>
              <section className="settings-row"><div><h2><ShieldCheck size={19}/>{t.boundary}</h2><p>{t.boundaryText}</p></div><span className="stable-chip"><CheckCircle2 size={16} />Local only</span></section>
            </div>
          )}
        </main>
        <footer className="footer"><span><ShieldCheck size={14} />{t.developing}</span><span>DARK XRAY PRO</span></footer>
      </div>
      {mobileOpen && <button className="backdrop" tabIndex={-1} aria-label="Close navigation" onClick={() => setMobileMenu(false)} />}
    </div>
  );
}

function NodeTable({ fleet, history, lang, selectedNodeId, onSelect }: {
  fleet: FleetSnapshot | null; history: Record<string, number[]>; lang: Language;
  selectedNodeId?: string | null; onSelect?: (id: string) => void;
}) {
  const t = translations[lang];
  if (!fleet) return <div className="empty-panel"><span className="spinner" /><strong>{t.collecting}</strong></div>;
  if (fleet.nodes.length === 0) return (
    <div className="empty-panel"><div className="empty-icon"><HardDrive size={27}/></div><h3>{t.emptyTitle}</h3><p>{t.emptyLead}</p></div>
  );
  return <div className="node-list">{fleet.nodes.map((node) => <NodeRow node={node} history={history[node.node_id] ?? []}
    lang={lang} key={node.node_id} selected={selectedNodeId === node.node_id} onSelect={onSelect} />)}</div>;
}

function uptimeDisplay(seconds: number, language: Language): string {
  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  return days > 0 ? (days + (language === "fa" ? " روز · " : "d · ") + hours + "h") : hours + "h";
}
function NodeInspector({ node, lang }: { node: NodeSnapshot; lang: Language }) {
  const t = translations[lang];
  const online = node.status === "online";
  const current = online && node.metrics_current;
  const lastSeen = node.last_seen ? new Date(node.last_seen) : null;
  const lastSeenText = lastSeen && Number.isFinite(lastSeen.getTime())
    ? lastSeen.toLocaleString(lang === "fa" ? "fa-IR" : "en-GB") : "—";
  const fields = [
    { label: t.lastHeartbeat, value: lastSeenText },
    { label: t.metricFreshness, value: current ? t.metricLive : t.metricDelayed },
    { label: t.nodeAddress, value: current && node.interface ? node.interface : "—" },
    { label: t.cpu, value: current ? node.cpu_percent.toFixed(1) + "%" : "—" },
    { label: t.memory, value: current ? ram(node.ram_used_bytes) + " / " + ram(node.ram_total_bytes) : "—" },
    { label: t.disk, value: current && node.disk_used_bytes != null && node.disk_total_bytes != null ? ram(node.disk_used_bytes) + " / " + ram(node.disk_total_bytes) : "—" },
    { label: t.load, value: current && node.load_1 != null && node.load_5 != null && node.load_15 != null ? [node.load_1,node.load_5,node.load_15].map(v=>v.toFixed(2)).join(" / ") : "—" },
    { label: t.uptime, value: current && node.uptime_seconds != null ? uptimeDisplay(node.uptime_seconds,lang) : "—" },
    { label: t.tcp, value: current && node.tcp_inuse != null ? node.tcp_inuse.toLocaleString("en-US") : "—" },
    { label: t.download, value: current ? rate(node.rx_bits_per_second ?? undefined) : "—" },
    { label: t.upload, value: current ? rate(node.tx_bits_per_second ?? undefined) : "—" },
  ];
  return (
    <section className="node-inspector" role="region" aria-label="Node details">
      <div className="node-inspector-heading">
        <div><span className="eyebrow">{t.nodeDetails}</span><h2>{node.name}</h2><p>{node.node_id}</p></div>
        <span className={"status-tag " + (online ? "status-online" : "status-offline")}>
          <i />{online ? t.online : t.offline}
        </span>
      </div>
      <div className="node-inspector-grid">
        {fields.map(({ label, value }) => (
          <div className="node-inspector-field" key={label}><span>{label}</span><strong>{value}</strong></div>
        ))}
      </div>
      <div className="node-inspector-boundary"><ShieldCheck size={16}/><span>{t.activationBoundary} {t.units}</span></div>
    </section>
  );
}
