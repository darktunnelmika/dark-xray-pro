import type { AppUser as User, Language } from "../session";
export type Representative = { id: string; display_name: string; kind: string; enabled: boolean; volume_credit_bytes: number; unlimited_credit_units: number };
export type Client = { id: string; representative_id: string; name: string; plan_kind: string; ip_count: number; volume_bytes: number; duration_days: number; deployment_state: string; expires_unix: number; archived: boolean };
export type ClientOperation = { event_id: string; client_id: string; representative_id: string; kind: string; reason: string; added_days: number; added_volume_bytes: number; volume_credit_debit: number; unlimited_credit_debit: number; created_unix: number };
export type ClientGroup = { id: string; representative_id: string; name: string; color: string; member_count: number; created_unix: number; updated_unix: number };
export type LedgerEvent = { event_id: string; kind: string; representative_id: string; object_id: string; reason: string; volume_delta_bytes: number; unlimited_delta_units: number };
export type Workspace = { user: User; balance: Representative; data_plane: "not_activated" };

export type Section = "clients" | "groups" | "representatives" | "marketplace" | "finance" | "account";
export function uid(): string { return "pro-" + crypto.randomUUID().slice(0, 18); }
export const words = {
  en: {
    loading: "Checking the private business service…",
    disabled: "Business storage is not enabled on this LAB HUB yet.",
    disabledNote: "The private owner setup, database and authenticated API must be enabled first. No fabricated accounts or credits are shown.",
    unavailable: "Business data unavailable or unverified. Client management is blocked until the next successful refresh.",
    login: "Sign in", logout: "Sign out", username: "Username", password: "Password",
    rotate: "Change password", oldPassword: "Current password", newPassword: "New password (12+ characters)", change: "Change and sign out",
    loginHelp: "Owner and representative credentials are separate from Node pairing and the temporary HTTPS preview password.",
    account: "Account", role: "Role", volume: "Volume credit", unlimited: "Unlimited units",
    reps: "Representatives", clients: "Clients", history: "Financial audit",
    groups: "Client groups", newGroup: "Create group", groupId: "Group ID", groupName: "Group name",
    groupColor: "Color (optional #RRGGBB)", groupCount: "members",
    groupAssign: "Move clients to group", clientIDs: "Client IDs (comma-separated, max 500)",
    groupEdit: "Rename / edit group", groupSave: "Save group", groupDelete: "Delete group",
    groupRemove: "Remove member from group", groupSingleClient: "Client ID to detach",
    confirmGroup: "Delete this group? All clients and financial history will remain intact.",
    groupWarning: "Deleting a group never deletes clients, consumed traffic or credit history.",
    manageClient: "Manage", clientLifecycle: "Renew and manage clients", renew: "Extend / add volume",
    extraDays: "Additional days", extraVolume: "Additional volume (GB)",
    archive: "Archive", disable: "Disable", enable: "Enable", stateAction: "Client action",
    applyState: "Record state change", archiveConfirm: "Archive client? UUID, history, spent credit and group memberships remain preserved.",
    clientOps: "Client operation history", archived: "Archived", chooseClient: "Choose an existing client",
    safeLifecycle: "Enable only returns to pending Node Apply; no Xray service is activated.",
    newClientReason: "Operation reason",
    createRep: "Create representative", repId: "Representative ID", displayName: "Display name",
    provision: "Set representative login", setLogin: "Create login", repUsername: "Representative username", repPassword: "New password (12+ characters)",
    grant: "Adjust representative credit", reason: "Reason", addVolume: "Volume delta (GB, +/-)", addUnits: "Unlimited-unit delta (+/-)",
    applyCredit: "Record credit transaction", createClient: "Create client record", clientId: "Client ID",
    clientName: "Client name", duration: "Duration (days)", slots: "IP/User slots (0 = unlimited for volume)",
    plan: "Plan type", volumePlan: "Volume", unlimitedPlan: "Unlimited",
    volumeGB: "Plan volume (GB)", saveClient: "Create pending client",
    pending: "Client records are pending Xray Node Apply; no active subscription is issued.",
    refresh: "Refresh", previous: "Previous", next: "Next", empty: "No records in this view.",
    accountScope: "Scoped to your representative account.", ownerScope: "Owner-only operations",
    failed: "Request failed", saved: "Saved in private PRO database",
    unsafe: "LAB ONLY · no old HUB or Telegram migration · no live Xray deployment",
    missing: "Complete the required fields with valid values.", loginError: "Sign-in rejected or temporarily unavailable.",
  },
  fa: {
    loading: "در حال بررسی سرویس خصوصی حسابداری…",
    disabled: "دیتابیس مالی در HUB آزمایشی هنوز فعال نشده است.",
    disabledNote: "ابتدا باید حساب مالک، دیتابیس خصوصی و API امن فعال شوند. اطلاعات و اعتبار ساختگی نمایش نمی‌دهیم.",
    unavailable: "اطلاعات کسب‌وکار در دسترس یا معتبر نیست؛ مدیریت کلاینت تا دریافت اطلاعات معتبر غیرفعال است.",
    login: "ورود", logout: "خروج", username: "نام کاربری", password: "رمز عبور",
    rotate: "تغییر رمز عبور", oldPassword: "رمز فعلی", newPassword: "رمز جدید (حداقل ۱۲ کاراکتر)", change: "تغییر رمز و خروج",
    loginHelp: "ورود مالک و نماینده مستقل از Pair Code نودها و رمز موقت HTTPS است.",
    account: "حساب", role: "نقش", volume: "اعتبار حجمی", unlimited: "اعتبار نامحدود",
    reps: "نمایندگان", clients: "کلاینت‌ها", history: "تاریخچه مالی",
    groups: "گروه‌های کلاینت‌ها", newGroup: "ساخت گروه", groupId: "شناسه گروه", groupName: "نام گروه",
    groupColor: "رنگ (اختیاری #RRGGBB)", groupCount: "عضو",
    groupAssign: "انتقال گروهی کلاینت‌ها", clientIDs: "شناسه کلاینت‌ها (با کاما، حداکثر ۵۰۰)",
    groupEdit: "ویرایش گروه", groupSave: "ذخیره گروه", groupDelete: "حذف گروه",
    groupRemove: "حذف عضو از گروه", groupSingleClient: "شناسه کلاینت برای جداسازی",
    confirmGroup: "این گروه حذف شود؟ کلاینت‌ها و سوابق مالی محفوظ می‌مانند.",
    groupWarning: "حذف گروه هرگز کلاینت‌ها، ترافیک مصرف‌شده و اعتبار تاریخی را پاک نمی‌کند.",
    manageClient: "مدیریت", clientLifecycle: "تمدید و مدیریت کلاینت‌ها", renew: "تمدید / افزایش حجم",
    extraDays: "روز اضافه", extraVolume: "حجم اضافه (گیگ)",
    archive: "آرشیو", disable: "غیرفعال", enable: "فعال‌سازی رکورد", stateAction: "عملیات کلاینت",
    applyState: "ثبت تغییر وضعیت", archiveConfirm: "کلاینت آرشیو شود؟ UUID، تاریخچه، اعتبار مصرف‌شده و عضویت گروه‌ها محفوظ می‌مانند.",
    clientOps: "تاریخچه عملیات کلاینت", archived: "آرشیوشده", chooseClient: "انتخاب کلاینت",
    safeLifecycle: "فعال‌سازی فقط رکورد را در انتظار Node Apply قرار می‌دهد؛ Xray فعال نمی‌شود.",
    newClientReason: "دلیل عملیات",
    createRep: "ساخت نماینده", repId: "شناسه نماینده", displayName: "نام نمایشی",
    provision: "ساخت ورود نماینده", setLogin: "ثبت ورود", repUsername: "نام کاربری نماینده", repPassword: "رمز جدید (حداقل ۱۲ کاراکتر)",
    grant: "تغییر اعتبار نماینده", reason: "دلیل تغییر", addVolume: "تغییر اعتبار حجمی (گیگ، +/-)", addUnits: "تغییر اعتبار نامحدود (+/-)",
    applyCredit: "ثبت تراکنش اعتبار", createClient: "ساخت رکورد کلاینت", clientId: "شناسه کلاینت",
    clientName: "نام کلاینت", duration: "مدت (روز)", slots: "تعداد IP/کاربر (صفر = نامحدود برای حجمی)",
    plan: "نوع اشتراک", volumePlan: "حجمی", unlimitedPlan: "نامحدود",
    volumeGB: "حجم پلن (گیگ)", saveClient: "ساخت کلاینت در انتظار اعمال",
    pending: "کلاینت‌ها تا قبل از Apply واقعی Xray فعال نمی‌شوند و لینک اشتراک صادر نمی‌شود.",
    refresh: "به‌روزرسانی", previous: "قبلی", next: "بعدی", empty: "هنوز رکوردی برای این بخش وجود ندارد.",
    accountScope: "فقط اطلاعات نمایندگی خودت قابل‌دسترسی است.", ownerScope: "عملیات مخصوص مالک",
    failed: "عملیات ناموفق بود", saved: "در دیتابیس خصوصی PRO ثبت شد",
    unsafe: "فقط LAB · بدون مهاجرت HUB قدیمی یا ربات‌ها · بدون اعمال زنده Xray",
    missing: "فیلدهای ضروری را با مقدار معتبر تکمیل کن.", loginError: "ورود پذیرفته نشد یا سرویس در دسترس نیست.",
  },
};

export function gb(bytes: number): string {
  return Number.isFinite(bytes) ? (bytes / (1024 ** 3)).toLocaleString("en-US", { maximumFractionDigits: 2 }) + " GB" : "—";
}
export function dateTime(sec: number, lang: Language): string {
  return Number.isFinite(sec) ? new Date(sec * 1000).toLocaleDateString(lang === "fa" ? "fa-IR" : "en-GB") : "—";
}
export function bytesFromGB(value: string, allowNegative = false): number | null {
  const number = Number(value);
  if (!value.trim() || !Number.isFinite(number) || (!allowNegative && number <= 0)) return null;
  const bytes = Math.round(number * (1024 ** 3));
  return Number.isSafeInteger(bytes) && Math.abs(bytes) <= 1_000_000_000_000_000_000 ? bytes : null;
}
