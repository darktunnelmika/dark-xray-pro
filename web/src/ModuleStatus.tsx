import { ArchiveRestore, ArrowUpRight, Bot, DatabaseBackup } from "lucide-react";
import type { Page } from "./navigation";
import type { Language } from "./session";
import "./ModuleStatus.css";

type StatusPage = "dark-restore" | "telegram" | "backups";
type ModuleCopy = {
  label: string;
  status: string;
  summary: string;
  present: string;
  remaining: string[];
  links: { page: Page; label: string }[];
};

const copy: Record<Language, Record<StatusPage, ModuleCopy>> = {
  en: {
    "dark-restore": {
      label: "Dark Restore status", status: "Import not available",
      summary: "Dark Restore is planned as a separate workspace for restored services. Import and recovery from this panel are not available yet.",
      present: "The project includes offline archive checks for data relationships and recorded usage. These checks do not import or change customer data.",
      remaining: ["A dedicated restore workspace and reviewed import flow.", "Preserved client identities, subscription links, group ownership and usage history.", "Recovery and rollback after an interrupted import."],
      links: [{ page: "groups", label: "View current groups" }],
    },
    telegram: {
      label: "Telegram status", status: "Bot connection not available",
      summary: "Bot connection and the sales MiniApp are not implemented in this version. This page shows the work still needed for customer and representative flows.",
      present: "Client management, representative credit and the plan catalog are available in the panel. The catalog does not yet support checkout or payment.",
      remaining: ["Bot connection, customer identity and reliable message delivery.", "Shop, payment, renewal, subscription delivery and support in the bot and MiniApp.", "Representative bot admins, controlled alerts and approved customer broadcasts."],
      links: [{ page: "clients", label: "Open Clients" }, { page: "marketplace", label: "Open plan catalog" }],
    },
    backups: {
      label: "Backup status", status: "Panel backup not available",
      summary: "Product backup and restore from this panel are not available yet. This section is reserved for the owner.",
      present: "Client records, credit and history are stored persistently. Existing runtime journals and installation snapshots are not a full product backup.",
      remaining: ["Create, download, upload and inspect a complete encrypted archive.", "Scheduled backups and retention controls.", "Verified recovery that preserves identity, balances and financial history."],
      links: [],
    },
  },
  fa: {
    "dark-restore": {
      label: "وضعیت دارک ریستور", status: "واردسازی هنوز در دسترس نیست",
      summary: "دارک ریستور برای مدیریت مستقل سرویس‌های بازیابی‌شده در نظر گرفته شده است. واردسازی و بازیابی از این پنل هنوز آماده نیست.",
      present: "ابزار بررسی آفلاین آرشیو، ارتباط داده‌ها و مصرف ثبت‌شده در پروژه وجود دارد. این بررسی، دادهٔ مشتری را وارد یا تغییر نمی‌دهد.",
      remaining: ["فضای مستقل ریستور و جریان بررسی پیش از واردسازی.", "حفظ هویت کلاینت، لینک اشتراک، مالکیت گروه و تاریخچهٔ مصرف.", "بازیابی و بازگشت پس از قطع واردسازی."],
      links: [{ page: "groups", label: "مشاهدهٔ گروه‌های فعلی" }],
    },
    telegram: {
      label: "وضعیت تلگرام", status: "اتصال ربات هنوز آماده نیست",
      summary: "اتصال ربات تلگرام و مینی‌اپ فروش هنوز پیاده‌سازی نشده‌اند. این بخش کارهای باقی‌مانده برای مشتری و نماینده را مشخص می‌کند.",
      present: "مدیریت کلاینت، اعتبار نماینده و کاتالوگ پلن‌ها در پنل وجود دارد. خرید و پرداخت در کاتالوگ هنوز فعال نیست.",
      remaining: ["اتصال ربات، شناسایی مشتری و تحویل مطمئن پیام.", "فروشگاه، پرداخت، تمدید، دریافت اشتراک و پشتیبانی در ربات و مینی‌اپ.", "ادمین‌های ربات نماینده، اعلان‌های قابل‌کنترل و پیام همگانی با تأیید."],
      links: [{ page: "clients", label: "مدیریت کلاینت‌ها" }, { page: "marketplace", label: "مشاهدهٔ کاتالوگ پلن‌ها" }],
    },
    backups: {
      label: "وضعیت پشتیبان‌گیری", status: "پشتیبان‌گیری پنل هنوز آماده نیست",
      summary: "پشتیبان‌گیری کامل و بازیابی از این پنل هنوز در دسترس نیست. این بخش فقط برای مالک است.",
      present: "اطلاعات کلاینت، اعتبار و سوابق به‌صورت پایدار ذخیره می‌شوند. ثبت‌های اجرایی و نسخه‌های نصب، جای پشتیبان کامل محصول را نمی‌گیرند.",
      remaining: ["ساخت، دریافت، بارگذاری و بررسی آرشیو کامل و رمزگذاری‌شده.", "پشتیبان‌گیری زمان‌بندی‌شده و تنظیم نگهداری نسخه‌ها.", "بازیابی تأییدشده با حفظ هویت، موجودی و تاریخچهٔ مالی."],
      links: [],
    },
  },
};

const icons = { "dark-restore": ArchiveRestore, telegram: Bot, backups: DatabaseBackup };

export default function ModuleStatus({ page, language, onNavigate }: {
  page: StatusPage;
  language: Language;
  onNavigate: (page: Page) => void;
}) {
  const content = copy[language][page];
  const Icon = icons[page];
  return <section className="module-status" aria-label={content.label}>
    <header className="module-status-header">
      <span className="module-status-icon"><Icon size={21} aria-hidden="true"/></span>
      <span className="module-status-badge">{content.status}</span>
    </header>
    <p className="module-status-summary">{content.summary}</p>
    <div className="module-status-details">
      <section>
        <h2>{language === "fa" ? "وضعیت فعلی" : "Current scope"}</h2>
        <p>{content.present}</p>
      </section>
      <section>
        <h2>{language === "fa" ? "بخش‌های باقی‌مانده" : "Still to complete"}</h2>
        <ul>{content.remaining.map(item => <li key={item}>{item}</li>)}</ul>
      </section>
    </div>
    {content.links.length > 0 && <div className="module-status-links">
      {content.links.map(link => <button type="button" key={link.page} onClick={() => onNavigate(link.page)}>
        <span>{link.label}</span><ArrowUpRight size={16} aria-hidden="true"/>
      </button>)}
    </div>}
  </section>;
}
