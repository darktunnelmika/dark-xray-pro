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
  await expect(page.getByRole("button", { name: "Create representative", exact: true })).toBeVisible();
  await page.goBack(); await expect(page).toHaveURL(/\/groups$/);
  await page.reload(); await expect(page.getByRole("heading", { name: "Create group", exact: true })).toBeVisible();
  expect(f.seen.filter(path => path === "POST /api/v1/session/login")).toHaveLength(1);
  const stored = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }));
  expect(stored).not.toContain("only-a-test-password"); expect(stored).not.toContain("a".repeat(64));
});

test("bookmarked Inbound details retain their tab and filters through login and reload", async ({ page }) => {
  const f = await setup(page);
  await page.route("**/api/v1/workspace/native-inbounds-state", route => route.fulfill({ json: {
    inbounds: [{ id: 301, name: "Bookmarked Inbound", enabled: true, archived: false, revision: 1,
      transport: "tcp", grpc_service: "", reality_target: "example.org:443", server_names: ["example.org"],
      short_ids: ["aa"], public_key: "PUBLIC_ONLY_TEST_KEY", bound_clients: 0 }],
    nodes: [], targets: [], runtime: [],
  } }));
  const bookmark = "/inbounds?inbound=301&tab=activity&q=Bookmarked&state=enabled";
  await page.goto(bookmark);
  await expect(page.getByRole("heading", { name: "Sign in", exact: true })).toBeVisible();
  await expect(page).toHaveURL(url => url.pathname === "/login" && url.searchParams.get("next") === bookmark);
  await login(page);
  const isBookmark = (url: URL) => url.pathname === "/inbounds" && url.searchParams.get("inbound") === "301" &&
    url.searchParams.get("tab") === "activity" && url.searchParams.get("q") === "Bookmarked" && url.searchParams.get("state") === "enabled";
  await expect(page).toHaveURL(isBookmark);
  await expect(page.getByRole("heading", { name: "Bookmarked Inbound", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Activity", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.reload();
  await expect(page).toHaveURL(isBookmark);
  await expect(page.getByRole("heading", { name: "Bookmarked Inbound", exact: true })).toBeVisible();
  await expect(page.getByRole("tab", { name: "Activity", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.getByRole("button", { name: "Back to Inbounds", exact: true }).click();
  await expect(page.getByLabel("Search Inbounds", { exact: true })).toHaveValue("Bookmarked");
  await expect(page.getByLabel("Status", { exact: true })).toHaveValue("enabled");
  expect(f.seen.filter(path => path === "POST /api/v1/session/login")).toHaveLength(1);
  expect(f.seen.some(path => path.startsWith("POST /api/v1/workspace/"))).toBe(false);
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
  for (const label of ["Overview", "Nodes", "Inbounds", "Dark Swap", "Representatives", "Dark Restore", "Backups"]) await expect(page.getByRole("navigation").getByRole("button", { name: label, exact: true, includeHidden: true })).toHaveCount(0);
  for (const group of ["Infrastructure", "Backup & recovery"]) await expect(page.getByRole("navigation").getByRole("button", { name: group, exact: true, includeHidden: true })).toHaveCount(0);
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

for (const language of ["en", "fa"] as const) {
  test(`mobile navigation keeps keyboard focus inside the open drawer and restores it (${language})`, async ({ page }) => {
    await setup(page);
    await page.setViewportSize({ width: 320, height: 780 });
    await page.goto("/settings");
    await login(page);
    if (language === "fa") await page.getByRole("button", { name: "Switch language" }).click();
    const opener = page.getByRole("button", { name: language === "fa" ? "بازکردن منو" : "Open menu" });
    const sidebar = page.locator("#pro-sidebar");
    await expect(sidebar).toHaveAttribute("inert", "");
    await expect(page.getByRole("navigation")).toHaveCount(0);
    await opener.focus();
    await page.keyboard.press("Tab");
    expect(await sidebar.evaluate(element => element.contains(document.activeElement))).toBe(false);

    await opener.click();
    const dialog = page.getByRole("dialog", { name: language === "fa" ? "منوی صفحه‌ها" : "Navigation" });
    const close = dialog.getByRole("button", { name: language === "fa" ? "بستن منو" : "Close menu" });
    const last = dialog.getByRole("button", { name: language === "fa" ? "تنظیمات" : "Settings", exact: true });
    await expect(dialog).toHaveAttribute("aria-modal", "true");
    await expect(close).toBeFocused();
    await expect(page.locator(".main-area")).toHaveAttribute("inert", "");
    await page.keyboard.press("Shift+Tab");
    await expect(last).toBeFocused();
    await page.keyboard.press("Tab");
    await expect(close).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(opener).toBeFocused();
    await expect(sidebar).toHaveAttribute("inert", "");
    expect(await page.evaluate(() => document.body.style.overflow)).toBe("");

    await opener.click();
    await dialog.getByRole("button", { name: language === "fa" ? "زیرساخت" : "Infrastructure", exact: true }).click();
    await expect(dialog.getByRole("button", { name: language === "fa" ? "اینباندها" : "Inbounds", exact: true })).toBeVisible();
    await expect(dialog.getByRole("button", { name: language === "fa" ? "دارک سواپ" : "Dark Swap", exact: true, includeHidden: true })).toHaveCount(0);
    await dialog.getByRole("button", { name: language === "fa" ? "مشتری و فروش" : "Customer & sales", exact: true }).click();
    await dialog.getByRole("button", { name: language === "fa" ? "گروه‌ها" : "Groups", exact: true }).click();
    await expect(page).toHaveURL(/\/groups$/);
    await expect(opener).toBeFocused();
    await opener.click();
    await page.setViewportSize({ width: 1280, height: 900 });
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await expect(page.getByRole("navigation")).toBeVisible();
    await expect(sidebar).not.toHaveAttribute("inert", "");
    await expect(page.locator(".backdrop")).toHaveCount(0);
    expect(await page.evaluate(() => document.body.style.overflow)).toBe("");
  });
}

test("an expired session closes the mobile drawer and a new sign-in starts with it closed", async ({ page }) => {
  const fixture = await setup(page);
  await page.setViewportSize({ width: 320, height: 780 });
  await page.goto("/settings");
  await login(page);
  await page.getByRole("button", { name: "Open menu" }).click();
  await expect(page.getByRole("dialog", { name: "Navigation" })).toBeVisible();
  fixture.expire();
  await page.evaluate(() => window.dispatchEvent(new StorageEvent("storage", {
    key: "dark-pro-session-change", newValue: "synthetic-session-expiration",
  })));
  await expect(page.getByRole("heading", { name: "Sign in", exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.body.style.overflow)).toBe("");
  await login(page);
  await expect(page.getByRole("button", { name: "Open menu" })).toHaveAttribute("aria-expanded", "false");
  await expect(page.locator("#pro-sidebar")).toHaveAttribute("inert", "");
  await expect(page.locator(".backdrop")).toHaveCount(0);
});

test("navigation disclosures keep the active section visible and follow browser history", async ({ page }) => {
  await setup(page);
  await page.goto("/settings");
  await login(page);
  const nav = page.getByRole("navigation", { name: "Main navigation" });
  const account = nav.getByRole("button", { name: "Account & settings", exact: true });
  const customers = nav.getByRole("button", { name: "Customer & sales", exact: true });
  await expect(account).toHaveAttribute("aria-expanded", "true");
  await expect(account).toBeDisabled();
  for (const label of ["Infrastructure", "Customer & sales", "Backup & recovery"]) {
    await expect(nav.getByRole("button", { name: label, exact: true })).toHaveAttribute("aria-expanded", "false");
  }
  await expect(nav.getByRole("button", { name: "Groups", exact: true })).toHaveCount(0);
  await customers.focus();
  await page.keyboard.press("Enter");
  await expect(customers).toHaveAttribute("aria-expanded", "true");
  const groupID = await customers.getAttribute("aria-controls");
  expect(groupID).toBeTruthy();
  await expect(page.locator("#" + groupID)).toBeVisible();
  await nav.getByRole("button", { name: "Groups", exact: true }).click();
  await expect(page).toHaveURL(/\/groups$/);
  await expect(customers).toHaveAttribute("aria-expanded", "true");
  await expect(customers).toBeDisabled();
  await expect(account).toHaveAttribute("aria-expanded", "false");
  await expect(nav.getByRole("button", { name: "Groups", exact: true })).toHaveAttribute("aria-current", "page");
  await page.goBack();
  await expect(page).toHaveURL(/\/settings$/);
  await expect(account).toHaveAttribute("aria-expanded", "true");
  await expect(customers).toHaveAttribute("aria-expanded", "false");
  await page.goto("/dark-swap");
  await expect(page).toHaveURL(/\/dark-swap$/);
  await expect(nav.getByRole("button", { name: "Infrastructure", exact: true })).toHaveAttribute("aria-expanded", "true");
  await expect(nav.getByRole("button", { name: "Dark Swap", exact: true, includeHidden: true })).toHaveCount(0);
  await nav.getByRole("button", { name: "Inbounds", exact: true }).click();
  await expect(page).toHaveURL(/\/inbounds$/);
  await page.goBack();
  await expect(page).toHaveURL(/\/dark-swap$/);
  await page.reload();
  await expect(nav.getByRole("button", { name: "Inbounds", exact: true })).toBeVisible();
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
  await expect(page.locator(".clients-explorer-row")).toHaveCount(50);
  await page.locator(".clients-explorer-pager").getByRole("button", { name: "Next" }).click();
  await expect(page.locator(".clients-explorer-pager").getByRole("button", { name: "Previous" })).toBeEnabled();
  await page.getByRole("button", { name: "Groups", exact: true }).click();
  await page.getByRole("button", { name: "Clients", exact: true }).click();
  await expect(page.locator(".clients-explorer-pager").getByRole("button", { name: "Previous" })).toBeEnabled();
  await page.reload();
  await expect(page.locator(".clients-explorer-pager").getByRole("button", { name: "Previous" })).toBeEnabled();
  const offsets = await page.evaluate(() => Object.entries(sessionStorage).filter(([k]) => k.startsWith("dark-pro-offset:")));
  expect(offsets).toEqual([["dark-pro-offset:owner:clients", "50"]]);
});
