import { useCallback, useEffect, useState } from "react";
export type Page = "overview" | "clients" | "groups" | "representatives" | "marketplace" | "finance" | "nodes" | "inbounds" | "settings" | "account";
export type Role = "owner" | "representative";
const paths: Record<Page, string> = { overview: "/dashboard", clients: "/clients", groups: "/groups", representatives: "/representatives", marketplace: "/marketplace", finance: "/finance", nodes: "/nodes", inbounds: "/inbounds", settings: "/settings", account: "/account" };
export function allowed(page: Page, role: Role): boolean { return role === "owner" || ["clients", "groups", "marketplace", "finance", "settings", "account"].includes(page); }
function fromPath(path: string, role: Role): Page {
  if (path === "/business") return "clients";
  const page = (Object.keys(paths) as Page[]).find(p => paths[p] === path);
  return page && allowed(page, role) ? page : role === "owner" ? "overview" : "clients";
}
export function useNavigation(role: Role | undefined) {
  const [page, updatePage] = useState<Page>(() => fromPath(location.pathname, role ?? "owner"));
  useEffect(() => {
    if (!role) {
      if (location.pathname !== "/login") {
        const next = paths[fromPath(location.pathname, "owner")];
        history.replaceState(null, "", "/login?next=" + encodeURIComponent(next));
      }
      return;
    }
    const sync = () => {
      const path = location.pathname === "/login" ? new URLSearchParams(location.search).get("next") ?? "" : location.pathname;
      const selected = fromPath(path, role);
      updatePage(selected);
      if (location.pathname !== paths[selected]) history.replaceState(null, "", paths[selected]);
    };
    sync(); window.addEventListener("popstate", sync);
    return () => window.removeEventListener("popstate", sync);
  }, [role]);
  const setPage = useCallback((next: Page) => {
    if (!role || !allowed(next, role)) return;
    if (location.pathname !== paths[next]) history.pushState(null, "", paths[next]);
    updatePage(next);
  }, [role]);
  return { page, setPage };
}
