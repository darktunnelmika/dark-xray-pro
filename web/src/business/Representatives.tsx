import { useEffect, useRef, useState, type FormEvent } from "react";
import { ArrowRight, CreditCard, KeyRound, Plus, ShieldCheck, UserCog, Users, X } from "lucide-react";
import { mutation } from "../session";
import type { BusinessModel } from "./useBusinessWorkspace";
import RepresentativeDirectory from "./RepresentativeDirectory";
import RepresentativeControlsV9 from "./RepresentativeControlsV9";
import SellersV2 from "./SellersV2";
import "./RepresentativesV2.css";

// The existing business Ledger remains the only source of credit. The V2
// workspace deliberately reuses its authenticated, CSRF-protected mutations.
const copy = {
  fa: {
    title:"نمایندگان", subtitle:"مدیریت یکپارچه حساب، دسترسی و اعتبار نمایندگان",
    add:"ایجاد نماینده", close:"بستن", back:"بازگشت به فهرست",
    profile:"پروفایل نماینده", access:"حساب و مجوزها", credentials:"اطلاعات ورود",
    credit:"اعتبار", sellers:"فروشندگان زیرمجموعه",
    sellerNote:"سهمیه فروشندگان پس از اتصال تراکنشی به دفتر اعتبار اصلی فعال می‌شود. تا آن زمان هیچ اعتبار آزمایشی قابل استفاده نیست.",
    createTitle:"ایجاد نماینده جدید", createHelp:"فقط حساب نمایندگی ساخته می‌شود؛ ورود و اعتبار از داخل پروفایل آن تنظیم می‌شوند.",
    id:"شناسه نماینده", name:"نام نماینده", idHint:"حروف انگلیسی، عدد، خط تیره و زیرخط (حداکثر ۶۴ نویسه)",
    create:"ثبت نماینده", creating:"در حال ثبت...", created:"نماینده ایجاد شد. اکنون ورود و اعتبار را از پروفایل تنظیم کنید.",
    invalid:"شناسه یا نام نماینده معتبر نیست.", createFailed:"ساخت نماینده تأیید نشد؛ پیش از تلاش مجدد فهرست را بررسی کنید.",
    loginHelp:"ثبت ورود اولیه نماینده؛ تغییر رمز حساب موجود باید از مسیر مستقل و امن خودش انجام شود.",
    setLogin:"ثبت مشخصات ورود", loginSuccess:"مشخصات ورود ذخیره شد.",
    loginFailed:"ثبت ورود ناموفق یا نامشخص بود؛ قبل از ارسال دوباره حساب را بررسی کنید.",
    creditHelp:"اعتبار فقط از Ledger اصلی پنل تغییر می‌کند؛ برای هر تغییر، دلیل حسابرسی لازم است.",
    creditWarning:"اعداد منفی اعتبار را کاهش می‌دهند. ثبت تراکنش مجدد با همان رویداد نباید اعتبار را دوبار تغییر دهد.",
  },
  en: {
    title:"Representatives", subtitle:"Unified account, access and credit management",
    add:"Create representative", close:"Close", back:"Back to directory",
    profile:"Representative profile", access:"Account & permissions", credentials:"Login credentials",
    credit:"Credit", sellers:"Subordinate sellers",
    sellerNote:"Seller quotas require atomic integration with the existing ledger. No experimental wallet or credit can be spent here.",
    createTitle:"Create representative", createHelp:"This creates the representative identity only. Set login and credit inside the profile.",
    id:"Representative ID", name:"Display name", idHint:"ASCII letters, numbers, dash and underscore, up to 64 characters",
    create:"Create account", creating:"Saving...", created:"Representative created. Configure login and credit in the profile.",
    invalid:"Enter a valid representative ID and name.", createFailed:"Account creation could not be confirmed. Check the directory before retrying.",
    loginHelp:"Create the initial representative login. Changing existing credentials requires a separately reviewed secure flow.",
    setLogin:"Save login", loginSuccess:"Login saved.",
    loginFailed:"Login creation is unconfirmed; inspect the existing account before retrying.",
    creditHelp:"All credit changes use the existing authoritative PRO ledger and audited operations.",
    creditWarning:"Negative numbers withdraw credit. Retrying one event must never apply the credit change twice.",
  },
} as const;

type ProfileTab = "access" | "credentials" | "credit" | "sellers";
const repID = /^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$/;

export default function Representatives({ model }: { model: BusinessModel }) {
  const { session, language, actionBusy, setActionBusy, setNotice } = model;
  const t = copy[language];
  const [activeID, setActiveID] = useState("");
  const [tab, setTab] = useState<ProfileTab>("access");
  const [showCreate, setShowCreate] = useState(false);
  const [newID, setNewID] = useState("");
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [createError, setCreateError] = useState("");
  const [loginName, setLoginName] = useState("");
  const [loginPassword, setLoginPassword] = useState("");
  const [loginBusy, setLoginBusy] = useState(false);
  const [loginError, setLoginError] = useState("");
  const focusRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!showCreate) return;
    focusRef.current?.focus();
    const esc = (event: KeyboardEvent) => {
      if (event.key === "Escape" && !creating) setShowCreate(false);
    };
    window.addEventListener("keydown", esc);
    return () => window.removeEventListener("keydown", esc);
  }, [showCreate, creating]);

  function manage(id: string) {
    if (!repID.test(id)) return;
    setActiveID(id);
    setTab("access");
    model.setCreditRep(id);
    model.setLoginRep(id);
    setLoginError("");
    setLoginName("");
    setLoginPassword("");
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const id = newID.trim();
    const name = newName.trim();
    if (!repID.test(id) || id === "owner" || name.length < 1 || name.length > 128) {
      setCreateError(t.invalid);
      return;
    }
    if (creating || actionBusy) return;
    setCreating(true);
    setActionBusy(true);
    setCreateError("");
    try {
      const saved = await mutation<{ id:string; display_name:string; kind:string }>(
        "/api/v1/workspace/representatives",
        { id, name }, session.csrf_token,
      );
      if (saved.id !== id || saved.kind !== "representative" || saved.display_name !== name)
        throw new Error("unverified_account_creation");
      setShowCreate(false);
      setNewID("");
      setNewName("");
      setNotice(t.created);
      manage(id);
      model.reload();
    } catch {
      setCreateError(t.createFailed);
    } finally {
      setCreating(false);
      setActionBusy(false);
    }
  }

  async function setLogin(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!activeID || !repID.test(activeID) || !loginName.trim() || loginPassword.length < 12 ||
        actionBusy || loginBusy) return;
    setLoginBusy(true);
    setActionBusy(true);
    setLoginError("");
    try {
      await mutation(
        "/api/v1/workspace/representatives/" + encodeURIComponent(activeID) + "/login",
        { username: loginName.trim(), password: loginPassword },
        session.csrf_token,
      );
      setLoginPassword("");
      setLoginName("");
      setNotice(t.loginSuccess);
      model.reload();
    } catch {
      setLoginError(t.loginFailed);
    } finally {
      setLoginBusy(false);
      setActionBusy(false);
    }
  }

  if (session.user.role !== "owner") return null;

  return <section className="rep-v2-workspace" aria-label="Representatives V2 workspace">
    <header className="rep-v2-header">
      <div>
        <h2><Users size={21}/>{t.title}</h2>
        <p>{t.subtitle}</p>
      </div>
      <button type="button" className="rep-v2-primary" disabled={actionBusy}
        onClick={() => { setCreateError(""); setShowCreate(true); }}>
        <Plus size={17}/>{t.add}
      </button>
    </header>

    {!activeID ?
      <RepresentativeDirectory model={model} onManage={manage}/> :
      <section className="rep-v2-profile" aria-label={t.profile}>
        <header className="rep-v2-profile-head">
          <div><span>{t.profile}</span><strong dir="ltr">{activeID}</strong></div>
          <button type="button" onClick={() => setActiveID("")} disabled={actionBusy || loginBusy}>
            <ArrowRight size={16}/>{t.back}
          </button>
        </header>
        <nav className="rep-v2-tabs" aria-label={t.profile}>
          {([
            ["access", t.access, UserCog],
            ["credentials", t.credentials, KeyRound],
            ["credit", t.credit, CreditCard],
            ["sellers", t.sellers, Users],
          ] as const).map(([key, label, Icon]) =>
            <button type="button" key={key} className={tab === key ? "selected" : ""}
              aria-current={tab === key ? "page" : undefined} onClick={() => setTab(key)}>
              <Icon size={15}/>{label}
            </button>)}
        </nav>
        {tab === "access" && <RepresentativeControlsV9 model={model} representativeID={activeID}
          onClose={() => setActiveID("")}/>}
        {tab === "credentials" &&
          <form className="business-panel rep-v2-detail-form" onSubmit={e => { void setLogin(e); }}>
            <h3><KeyRound size={18}/>{t.credentials}</h3><p>{t.loginHelp}</p>
            <label>{model.t.repUsername}
              <input autoComplete="off" value={loginName} maxLength={64} required
                disabled={actionBusy || loginBusy} onChange={e=>setLoginName(e.target.value)}/>
            </label>
            <label>{model.t.repPassword}
              <input autoComplete="new-password" type="password" value={loginPassword}
                minLength={12} required disabled={actionBusy || loginBusy}
                onChange={e=>setLoginPassword(e.target.value)}/>
            </label>
            <button type="submit" className="rep-v2-primary"
              disabled={actionBusy || loginBusy || !loginName.trim() || loginPassword.length < 12}>
              <ShieldCheck size={16}/>{t.setLogin}
            </button>
            {loginError && <p role="alert" className="rep-v2-error">{loginError}</p>}
          </form>}
        {tab === "credit" &&
          <form className="business-panel rep-v2-detail-form" onSubmit={e => { void model.adjustCredit(e); }}>
            <h3><CreditCard size={18}/>{t.credit}</h3><p>{t.creditHelp}</p>
            <label>{model.t.addVolume}
              <input type="number" step="any" value={model.creditVolume} required
                disabled={actionBusy} onChange={e=>model.setCreditVolume(e.target.value)}/>
            </label>
            <label>{model.t.addUnits}
              <input type="number" step="1" value={model.creditUnits} required
                disabled={actionBusy} onChange={e=>model.setCreditUnits(e.target.value)}/>
            </label>
            <label>{model.t.reason}
              <input value={model.creditReason} maxLength={200} required
                disabled={actionBusy} onChange={e=>model.setCreditReason(e.target.value)}/>
            </label>
            <p className="rep-v2-help">{t.creditWarning}</p>
            <button type="submit" className="rep-v2-primary" disabled={actionBusy || model.creditRep!==activeID || !model.creditReason.trim()}>
              <CreditCard size={16}/>{model.t.applyCredit}
            </button>
          </form>}
        {tab === "sellers" && <SellersV2 key={activeID} model={model} representativeID={activeID}/>}
      </section>}

    {showCreate && <div className="rep-v2-backdrop">
      <div className="business-panel rep-v2-dialog" role="dialog" aria-modal="true" aria-labelledby="rep-v2-create-title">
        <header>
          <div><h3 id="rep-v2-create-title"><Plus size={18}/>{t.createTitle}</h3><p>{t.createHelp}</p></div>
          <button type="button" aria-label={t.close} onClick={()=>setShowCreate(false)} disabled={creating}><X size={18}/></button>
        </header>
        <form onSubmit={e=>{void create(e);}}>
          <label>{t.id}
            <input ref={focusRef} value={newID} autoComplete="off" maxLength={64}
              pattern="[A-Za-z0-9][A-Za-z0-9_.-]{0,63}" required disabled={creating}
              onChange={e=>setNewID(e.target.value)}/>
            <small>{t.idHint}</small>
          </label>
          <label>{t.name}
            <input value={newName} maxLength={128} required disabled={creating}
              onChange={e=>setNewName(e.target.value)}/>
          </label>
          {createError && <p className="rep-v2-error" role="alert">{createError}</p>}
          <footer>
            <button type="button" disabled={creating} onClick={()=>setShowCreate(false)}>{t.close}</button>
            <button className="rep-v2-primary" type="submit" disabled={creating || actionBusy}>
              <Plus size={16}/>{creating ? t.creating : t.create}
            </button>
          </footer>
        </form>
      </div>
    </div>}
  </section>;
}
