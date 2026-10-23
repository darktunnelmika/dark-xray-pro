import { useCallback, useEffect, useState } from "react";
export type Page = "overview" | "clients" | "groups" | "representatives" | "marketplace" | "finance" | "nodes" | "inbounds" | "dark-swap" | "dark-restore" | "telegram" | "backups" | "settings" | "account";
export type Role = "owner" | "representative";
const paths: Record<Page, string> = { overview: "/dashboard", clients: "/clients", groups: "/groups", representatives: "/representatives", marketplace: "/marketplace", finance: "/finance", nodes: "/nodes", inbounds: "/inbounds", "dark-swap": "/dark-swap", "dark-restore": "/dark-restore", telegram: "/telegram", backups: "/backups", settings: "/settings", account: "/account" };
export function allowed(page: Page, role: Role): boolean { return role === "owner" || ["clients", "groups", "marketplace", "finance", "telegram", "settings", "account"].includes(page); }
function pageAtPath(path: string): Page | undefined {
  if (path === "/business") return "clients";
  return (Object.keys(paths) as Page[]).find(page => paths[page] === path);
}
function fromPath(path: string, role: Role): Page {
  const page = pageAtPath(path);
  return page && allowed(page, role) ? page : role === "owner" ? "overview" : "clients";
}

// Only local application destinations may be carried through login or written
// by a workspace. Inbound detail queries survive; external/unknown routes do not.
function applicationURL(value: string): { page: Page; url: URL } | null {
  if (!value.startsWith("/") || value.startsWith("//")) return null;
  try {
    const url = new URL(value, location.origin);
    const page = pageAtPath(url.pathname);
    if (url.origin !== location.origin || !page) return null;
    url.pathname = paths[page];
    url.hash = "";
    return { page, url };
  } catch { return null; }
}

export type NavigationGuard = (destination: URL) => boolean;
const navigationGuards = new Set<NavigationGuard>();
let rememberedURL: string | null = null;
let rememberedState: unknown = null;
const currentURL = () => location.pathname + location.search;
function rememberCurrentURL() {
  rememberedURL = currentURL();
  rememberedState = history.state;
}
function permitsNavigation(destination: URL): boolean {
  for (const guard of navigationGuards) {
    try { if (!guard(destination)) return false; }
    catch { return false; }
  }
  return true;
}

export function registerNavigationGuard(guard: NavigationGuard): () => void {
  if (rememberedURL === null) rememberCurrentURL();
  navigationGuards.add(guard);
  return () => { navigationGuards.delete(guard); };
}

// This writes browser history only. Callers update their React page/detail state
// after a true result; no synthetic popstate event is emitted.
export function navigateURL(value: string, replace = false): boolean {
  const destination = applicationURL(value);
  if (!destination) return false;
  const url = destination.url.pathname + destination.url.search;
  if (url === currentURL()) { rememberCurrentURL(); return true; }
  if (!permitsNavigation(destination.url)) return false;
  history[replace ? "replaceState" : "pushState"](history.state, "", url);
  rememberCurrentURL();
  return true;
}

function guardHistoryNavigation(event: PopStateEvent) {
  const destination = new URL(location.href);
  if (rememberedURL !== null && currentURL() !== rememberedURL && !permitsNavigation(destination)) {
    // popstate is delivered after the browser moves. Restore the last accepted
    // URL before any page/detail listener can apply the rejected destination.
    history.pushState(rememberedState, "", rememberedURL);
    event.stopImmediatePropagation();
    return;
  }
  rememberCurrentURL();
}

export function useNavigation(role: Role | undefined) {
  const [page, updatePage] = useState<Page>(() => fromPath(location.pathname, role ?? "owner"));
  useEffect(() => {
    rememberCurrentURL();
    window.addEventListener("popstate", guardHistoryNavigation, true);
    return () => window.removeEventListener("popstate", guardHistoryNavigation, true);
  }, []);
  useEffect(() => {
    if (!role) {
      if (location.pathname !== "/login") {
        const destination = applicationURL(currentURL());
        const next = destination ? destination.url.pathname + destination.url.search : paths.overview;
        history.replaceState(history.state, "", "/login?next=" + encodeURIComponent(next));
        rememberCurrentURL();
      }
      return;
    }
    const sync = () => {
      const restoringLogin = location.pathname === "/login";
      const requested = restoringLogin
        ? new URLSearchParams(location.search).get("next") ?? "" : currentURL();
      const destination = applicationURL(requested);
      const permitted = destination && allowed(destination.page, role) ? destination : null;
      const selected = permitted ? permitted.page : fromPath("", role);
      const url = permitted ? permitted.url.pathname + permitted.url.search : paths[selected];
      updatePage(selected);
      if (currentURL() !== url) history.replaceState(history.state, "", url);
      rememberCurrentURL();
      // A detail workspace can mount during session restoration before this
      // effect replaces /login. Let its existing history listener read the now
      // canonical query without a timeout or an additional history entry.
      if (restoringLogin) window.dispatchEvent(new PopStateEvent("popstate", { state: history.state }));
    };
    sync(); window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, [role]);
  const setPage = useCallback((next: Page) => {
    if (!role || !allowed(next, role)) return;
    // Selecting the current workspace keeps its detail query. Cross-workspace
    // navigation still enters the destination's list and runs unsaved guards.
    const destination = location.pathname === paths[next] ? currentURL() : paths[next];
    if (!navigateURL(destination)) return;
    updatePage(next);
  }, [role]);
  return { page, setPage };
}
