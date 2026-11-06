import { useState, type FormEvent } from "react";
import { Globe2, LockKeyhole, Moon, Network, Sun } from "lucide-react";
import { ApiError, useSession, type Language } from "./session";

export default function LoginPage({ language, setLanguage, dark, toggleTheme }: {
  language: Language; setLanguage: (language: Language) => void; dark: boolean; toggleTheme: () => void;
}) {
  const auth = useSession();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<"credentials" | "unavailable" | "rate_limited" | null>(null);
  const fa = language === "fa";
  async function submit(event: FormEvent) {
    event.preventDefault(); if (busy) return;
    setBusy(true); setError(null);
    try { await auth.signIn(username, password); setPassword(""); }
    catch (failure) {
      setError(failure instanceof ApiError && failure.status === 401 ? "credentials" :
        failure instanceof ApiError && failure.status === 429 ? "rate_limited" : "unavailable");
    }
    finally { setBusy(false); }
  }
  return <main className="login-page">
    <header className="login-topbar">
      <div className="login-brand"><Network size={23}/><strong>DARK XRAY <span>PRO</span></strong></div>
      <div className="login-tools">
        <button type="button" className="icon-button" aria-label="Switch language" onClick={() => setLanguage(fa ? "en" : "fa")}><Globe2 size={19}/><span>{fa ? "FA" : "EN"}</span></button>
        <button type="button" className="icon-button" aria-label="Toggle dark and light mode" onClick={toggleTheme}>{dark ? <Sun size={20}/> : <Moon size={20}/>}</button>
      </div>
    </header>
    <section className="login-card" aria-labelledby="login-title">
      <div className="login-symbol"><LockKeyhole size={28}/></div>
      <p className="login-eyebrow">DARK XRAY PRO</p>
      <h1 id="login-title">{fa ? "ورود به پنل" : "Sign in"}</h1>
      <p className="login-description">{fa ? "با حساب مالک یا نماینده وارد شو؛ دسترسی‌ها پس از ورود مشخص می‌شوند." : "Use your owner or representative account. Your permissions follow your account."}</p>
      {auth.phase === "checking" ? <p role="status">{fa ? "در حال بررسی نشست…" : "Checking your session…"}</p> : auth.phase === "unavailable" ?
        <div className="login-error" role="alert"><p>{fa ? "سرویس ورود در دسترس نیست؛ اطلاعات حساب یا دسترسی فرضی نمایش داده نمی‌شود." : "Sign-in service unavailable. No account or permissions are assumed."}</p>
          <button type="button" onClick={() => { void auth.refresh(); }}>{fa ? "تلاش دوباره" : "Retry"}</button></div> :
        <form onSubmit={(event) => { void submit(event); }}>
          <label>{fa ? "نام کاربری" : "Username"}<input autoComplete="username" autoFocus value={username} onChange={e => setUsername(e.target.value)} required minLength={3} maxLength={128} dir="ltr"/></label>
          <label>{fa ? "رمز عبور" : "Password"}<input type="password" autoComplete="current-password" value={password} onChange={e => setPassword(e.target.value)} required minLength={12} maxLength={256} dir="ltr"/></label>
          {error && <p role="alert" className="login-error">{error === "credentials"
            ? (fa ? "نام کاربری یا رمز عبور نادرست است." : "The username or password is incorrect.")
            : error === "rate_limited"
              ? (fa ? "تعداد تلاش‌های ورود زیاد است؛ کمی صبر کن و دوباره تلاش کن." : "Too many sign-in attempts. Wait before trying again.")
              : (fa ? "سرویس ورود موقتاً در دسترس نیست؛ کمی بعد دوباره تلاش کن." : "The sign-in service is temporarily unavailable. Try again shortly.")}</p>}
          <button className="login-submit" disabled={busy} type="submit">{busy ? (fa ? "در حال ورود…" : "Signing in…") : (fa ? "ورود" : "Sign in")}</button>
        </form>}
      <div className="login-boundary">{fa ? "نسخهٔ بررسی PRO · بدون انتقال اطلاعات یا فعال‌سازی سرویس مشتری" : "PRO review build · no migration or customer service activation"}</div>
    </section>
    <footer className="login-footer">DARK XRAY PRO</footer>
  </main>;
}
