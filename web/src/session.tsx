import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";

export type AppUser = { id: string; username: string; role: "owner" | "representative"; representative_id: string };
export type Session = { authenticated: true; user: AppUser; csrf_token: string; expires_unix: number };
export type Language = "en" | "fa";
const revokedEvent = "dark-pro-session-revoked";
const changedKey = "dark-pro-session-change"; // Non-secret event only; never store credentials or tokens.

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message); }
}
export async function api<T>(path: string, options: RequestInit = {}): Promise<T> {
  if (!path.startsWith("/api/v1/") || path.startsWith("//")) throw new ApiError(0, "invalid_api_path");
  const controller = new AbortController();
  const abort = () => controller.abort();
  options.signal?.addEventListener("abort", abort, { once: true });
  if (options.signal?.aborted) abort();
  const timer = window.setTimeout(abort, 8000);
  try {
    const response = await fetch(path, { ...options, cache: "no-store", credentials: "same-origin", signal: controller.signal,
      headers: { Accept: "application/json", ...(options.headers ?? {}) } });
    if (!response.ok) {
      if (response.status === 401 && !path.startsWith("/api/v1/session/")) window.dispatchEvent(new Event(revokedEvent));
      let code = "HTTP " + response.status;
      try { const body = await response.json(); if (typeof body?.error === "string" && /^[a-z_]{3,80}$/.test(body.error)) code = body.error; } catch { /* no raw server output */ }
      throw new ApiError(response.status, code);
    }
    return await response.json() as T;
  } finally {
    window.clearTimeout(timer);
    options.signal?.removeEventListener("abort", abort);
  }
}
export function mutation<T>(path: string, body: unknown, csrf: string): Promise<T> {
  return api<T>(path, { method: "POST", headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf }, body: JSON.stringify(body) });
}
function readSession(value: unknown): Session | null {
  if (!value || typeof value !== "object") throw new Error("invalid_session");
  const s = value as Omit<Partial<Session>, "authenticated"> & { authenticated?: boolean };
  if (s.authenticated === false) return null;
  if (s.authenticated !== true || !s.user || !["owner", "representative"].includes(s.user.role) ||
      typeof s.user.id !== "string" || !s.user.id || typeof s.user.username !== "string" ||
      typeof s.user.representative_id !== "string" || typeof s.csrf_token !== "string" || !/^[a-f0-9]{64}$/.test(s.csrf_token) ||
      !Number.isSafeInteger(s.expires_unix) || s.expires_unix! <= Date.now() / 1000) throw new Error("invalid_session");
  return s as Session;
}
type Auth = {
  session: Session | null; phase: "checking" | "signedout" | "signedin" | "unavailable";
  signIn: (username: string, password: string) => Promise<void>;
  signOut: () => Promise<void>; expire: () => void; refresh: () => Promise<void>;
};
const SessionContext = createContext<Auth | null>(null);
export function useSession(): Auth {
  const context = useContext(SessionContext);
  if (!context) throw new Error("SessionProvider is required");
  return context;
}
export function SessionProvider({ children }: { children: ReactNode }) {
  const [session, setSession] = useState<Session | null>(null);
  const [phase, setPhase] = useState<Auth["phase"]>("checking");
  const sequence = useRef(0);
  const expire = useCallback(() => { sequence.current++; setSession(null); setPhase("signedout"); }, []);
  const refresh = useCallback(async () => {
    const generation = ++sequence.current;
    try {
      const next = readSession(await api<unknown>("/api/v1/session/status"));
      if (generation !== sequence.current) return;
      setSession(next); setPhase(next ? "signedin" : "signedout");
    } catch {
      if (generation !== sequence.current) return;
      setSession(null); setPhase("unavailable");
    }
  }, []);
  useEffect(() => {
    void refresh();
    const changed = (event: StorageEvent) => { if (event.key === changedKey) { expire(); void refresh(); } };
    window.addEventListener(revokedEvent, expire); window.addEventListener("storage", changed);
    return () => { sequence.current++; window.removeEventListener(revokedEvent, expire); window.removeEventListener("storage", changed); };
  }, [expire, refresh]);
  useEffect(() => {
    if (!session) return;
    const timer = window.setTimeout(expire, Math.min(2_147_483_647, Math.max(0, session.expires_unix * 1000 - Date.now())));
    return () => window.clearTimeout(timer);
  }, [session, expire]);
  const broadcast = () => { try { localStorage.setItem(changedKey, crypto.randomUUID()); } catch { /* private browser */ } };
  const signIn = async (username: string, password: string) => {
    const generation = ++sequence.current;
    const next = readSession(await api<unknown>("/api/v1/session/login", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ username, password }) }));
    if (!next || generation !== sequence.current) throw new Error("session_changed");
    setSession(next); setPhase("signedin"); broadcast();
  };
  const signOut = async () => {
    if (!session) { expire(); return; }
    // Do not claim logout if revocation could not be committed by the server.
    await mutation("/api/v1/session/logout", {}, session.csrf_token);
    expire(); broadcast();
  };
  return <SessionContext.Provider value={{ session, phase, signIn, signOut, expire, refresh }}>{children}</SessionContext.Provider>;
}
