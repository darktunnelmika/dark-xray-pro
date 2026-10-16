import { test, expect, type Page } from "@playwright/test";

async function setup(page: Page, role: "owner" | "representative" = "owner") {
  let signed = false;
  const seen: string[] = [];
  const user = { id: role, username: "shell-user", role, representative_id: role === "owner" ? "owner" : "rep-one" };
  const session = { authenticated: true, user, csrf_token: "a".repeat(64), expires_unix: 1900000000 };
  await page.route("**/api/v1/**", async route => {
    const request = route.request(); const path = new URL(request.url()).pathname;
    seen.push(request.method() + " " + path);
    if (path.endsWith("/session/status")) return route.fulfill({ json: signed ? session : { enabled: true, authenticated: false } });
    if (path.endsWith("/session/login")) { signed = true; return route.fulfill({ json: session }); }
    if (path.endsWith("/session/logout")) { expect(request.headers()["x-csrf-token"]).toBe(session.csrf_token); signed = false; return route.fulfill({ json: { logged_out: true } }); }
    if (!signed) return route.fulfill({ status: 401, json: {} });
    if (path.endsWith("/fleet")) return route.fulfill({ json: { as_of: new Date().toISOString(), nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0, interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } });
    if (path.endsWith("/me")) return route.fulfill({ json: { user, balance: { volume_credit_bytes: 0, unlimited_credit_units: 0 }, data_plane: "not_activated" } });
    const field = path.endsWith("/client-operations") ? "operations" : path.split("/").at(-1)!;
    return route.fulfill({ json: { [field]: [] } });
  });
  return { seen, expire: () => { signed = false; } };
}
async function login(page: Page) {
  await page.getByLabel("Username", { exact: true }).fill("shell-user");
  await page.getByLabel("Password", { exact: true }).fill("only-a-test-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
}

test("one root login protects every page and does not poll private fleet before authentication", async ({ page }) => {
  const f = await setup(page); await page.goto("/clients");
  await expect(page.getByRole("heading", { name: "Sign in", exact: true })).toBeVisible();
  await expect(page.getByRole("navigation")).toHaveCount(0);
  expect(f.seen.every(path => path.includes("/session/"))).toBe(true);
  await login(page); await expect(page).toHaveURL(/\/clients$/);
  await expect(page.locator(".business-client-form")).toBeVisible();
  await expect(page.getByRole("heading", { name: "Create representative" })).toHaveCount(0);
  await expect(page.getByRole("heading", { name: "Create group" })).toHaveCount(0);
  expect(f.seen.some(path => path.endsWith("/events") || path.endsWith("/groups"))).toBe(false);
});

test("shared session survives page changes, browser history and reload without duplicate login", async ({ page }) => {
  const f = await setup(page); await page.goto("/clients"); await login(page);
  await page.getByRole("button", { name: "Groups", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Create group", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Representatives", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Create representative", exact: true })).toBeVisible();
  await page.goBack(); await expect(page).toHaveURL(/\/groups$/);
  await page.reload(); await expect(page.getByRole("heading", { name: "Create group", exact: true })).toBeVisible();
  expect(f.seen.filter(path => path === "POST /api/v1/session/login")).toHaveLength(1);
  const stored = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }));
  expect(stored).not.toContain("only-a-test-password"); expect(stored).not.toContain("a".repeat(64));
});

test("logout is global and back navigation cannot restore private pages", async ({ page }) => {
  await setup(page); await page.goto("/clients"); await login(page);
  await page.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Sign in", exact: true })).toBeVisible();
  await page.goBack(); await expect(page.getByRole("navigation")).toHaveCount(0);
  await expect(page.locator(".business-client-form")).toHaveCount(0);
});

test("representative role cannot load owner pages or poll infrastructure", async ({ page }) => {
  const f = await setup(page, "representative"); await page.goto("/nodes"); await login(page);
  await expect(page).toHaveURL(/\/clients$/);
  for (const label of ["Overview", "Nodes", "Inbounds", "Representatives"]) await expect(page.getByRole("navigation").getByRole("button", { name: label, exact: true })).toHaveCount(0);
  expect(f.seen.some(path => path.endsWith("/fleet") || path.endsWith("/representatives"))).toBe(false);
});

test("service failure is fail-closed, not an anonymous preview", async ({ page }) => {
  await page.route("**/api/v1/session/status", route => route.fulfill({ status: 503, json: {} }));
  await page.goto("/groups"); await expect(page.getByRole("alert")).toContainText("Sign-in service unavailable");
  await expect(page.getByRole("navigation")).toHaveCount(0);
});

test("root login fits 320px Persian and keeps appearance choice", async ({ page }) => {
  await setup(page); await page.setViewportSize({ width: 320, height: 780 }); await page.goto("/");
  await page.getByRole("button", { name: "Switch language" }).click();
  await expect(page.getByRole("heading", { name: "ورود به پنل" })).toBeVisible();
  await page.getByRole("button", { name: "Toggle dark and light mode" }).click();
  await page.reload(); await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
});


test("client pagination survives menu changes and page reload without storing client data", async ({ page }) => {
  await setup(page);
  await page.route("**/api/v1/workspace/clients?*", route => {
    const offset = Number(new URL(route.request().url()).searchParams.get("offset") ?? "0");
    const clients = offset === 0 ? Array.from({ length: 50 }, (_, i) => ({
      id: "synthetic-" + i, name: "Synthetic " + i, representative_id: "owner",
      plan_kind: "volume", ip_count: 0, volume_bytes: 1024,
      duration_days: 30, deployment_state: "pending_node_apply",
      expires_unix: 1900000000, archived: false,
    })) : [];
    return route.fulfill({ json: { clients } });
  });
  await page.goto("/clients");
  await login(page);
  await expect(page.locator(".business-records").first().locator("strong")).toHaveCount(50);
  await page.locator(".business-pagination").first().getByRole("button", { name: "Next" }).click();
  await expect(page.locator(".business-pagination").first().getByRole("button", { name: "Previous" })).toBeEnabled();
  await page.getByRole("button", { name: "Groups", exact: true }).click();
  await page.getByRole("button", { name: "Clients", exact: true }).click();
  await expect(page.locator(".business-pagination").first().getByRole("button", { name: "Previous" })).toBeEnabled();
  await page.reload();
  await expect(page.locator(".business-pagination").first().getByRole("button", { name: "Previous" })).toBeEnabled();
  const offsets = await page.evaluate(() => Object.entries(sessionStorage).filter(([k]) => k.startsWith("dark-pro-offset:")));
  expect(offsets).toEqual([["dark-pro-offset:owner:clients", "50"]]);
});
