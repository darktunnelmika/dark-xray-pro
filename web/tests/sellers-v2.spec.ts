import { expect, test, type Page } from "@playwright/test";

const csrf = "c".repeat(64);
type Write = { path: string; csrf: string; body: Record<string, unknown> };
async function fixture(page: Page, options: { owner?: boolean; enabled?: boolean; granted?: boolean; crossTenant?: boolean; loseQuotaReply?: boolean; denyFirstQuotaRetry?: boolean; empty?: boolean } = {}) {
  const user = { id: options.owner ? "owner" : "rep-a", username: "seller-qa", role: options.owner ? "owner" : "representative", representative_id: options.owner ? "owner" : "rep-a", permissions: { manage_sellers: options.granted ?? true } };
  const writes: Write[] = [];
  let permission = options.granted ?? true, permissionRevision = 0, loseReply = options.loseQuotaReply ?? false;
  let denyRetry = options.denyFirstQuotaRetry ?? false;
  const sellers = options.empty ? [] : [{ id: "seller-a", representative_id: "rep-a", name: "Exact Balance Seller", telegram_user_id: "123456789", enabled: true, reserved_bytes: "9007199254740993", reserved_units: "20", revision: 0 }];
  const receipts = new Map<string, Record<string, unknown>>();
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user, csrf_token: csrf, expires_unix: 1900000000 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { as_of: new Date().toISOString(), total_nodes: 0, online_nodes: 0, offline_nodes: 0, nodes: [], metric_scope: "default_route_interface_not_billable", physical_rates_complete: false } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname;
    const rep = { id: "rep-a", display_name: "QA Representative", kind: "representative", enabled: true, volume_credit_bytes: 10737418240, unlimited_credit_units: 100, client_count: 0, archived_count: 0, created_unix: 1 };
    if (path === "/api/v1/workspace/me") return route.fulfill({ json: { user, balance: { ...rep, id: user.representative_id }, data_plane: "not_activated" } });
    if (path === "/api/v1/workspace/representatives") return route.fulfill({ json: { representatives: [rep], total: 1, limit: Number(url.searchParams.get("limit")), offset: Number(url.searchParams.get("offset")), q: url.searchParams.get("q") ?? "", state: url.searchParams.get("state") ?? "", sort: url.searchParams.get("sort") ?? "", has_more: false } });
    if (!path.startsWith("/api/v1/workspace/representatives/rep-a/seller")) return route.fulfill({ status: 404, json: { error: "not_found" } });
    if (options.enabled === false) return route.fulfill({ status: 404, json: { error: "not_found" } });
    if (req.method() === "POST") {
      const body = req.postDataJSON() as Record<string, unknown>;
      writes.push({ path, csrf: req.headers()["x-csrf-token"] ?? "", body });
      if (req.headers()["x-csrf-token"] !== csrf || body.representative_id !== "rep-a") return route.fulfill({ status: 403, json: { error: "denied" } });
      if (path.endsWith("/seller-permission")) {
        if (!options.owner || body.expected_revision !== permissionRevision) return route.fulfill({ status: 409, json: { error: "conflict" } });
        permission = body.manage_sellers as boolean; permissionRevision++;
        return route.fulfill({ json: { representative_id: "rep-a", manage_sellers: permission, revision: permissionRevision, replayed: false, sessions_revoked: true } });
      }
      if (!options.owner && !permission) return route.fulfill({ status: 403, json: { error: "denied" } });
      if (path.endsWith("/quota")) {
        const event = String(body.event_id), existing = receipts.get(event);
        if (existing && denyRetry) { denyRetry = false; return route.fulfill({ status: 403, json: { error: "permission_revoked" } }); }
        if (existing) return route.fulfill({ json: { ...existing, replayed: true } });
        const seller = sellers.find(s => s.id === body.seller_id)!;
        const sign = body.action === "allocate" ? 1n : -1n;
        seller.reserved_bytes = (BigInt(seller.reserved_bytes) + sign * BigInt(String(body.bytes))).toString();
        seller.reserved_units = (BigInt(seller.reserved_units) + sign * BigInt(String(body.units))).toString();
        const receipt = { event_id: event, representative_id: "rep-a", seller_id: seller.id, action: body.action, representative_free_bytes: "10737418239", representative_free_units: "100", seller_reserved_bytes: seller.reserved_bytes, seller_reserved_units: seller.reserved_units, replayed: false };
        receipts.set(event, receipt);
        if (loseReply) { loseReply = false; return route.abort("failed"); }
        return route.fulfill({ json: receipt });
      }
      if (path.endsWith("/state")) {
        const seller = sellers.find(s => s.id === body.seller_id)!;
        if (seller.revision !== body.expected_revision) return route.fulfill({ status: 409, json: { error: "conflict" } });
        seller.enabled = body.enabled as boolean; seller.revision++;
        return route.fulfill({ json: { seller, replayed: false } });
      }
      if (path.endsWith("/sellers")) {
        const seller = { id: String(body.seller_id), representative_id: "rep-a", name: String(body.name), telegram_user_id: String(body.telegram_user_id), enabled: true, reserved_bytes: "0", reserved_units: "0", revision: 0 };
        sellers.push(seller); return route.fulfill({ status: 201, json: seller });
      }
    }
    if (path.endsWith("/seller-permission")) return route.fulfill({ json: { representative_id: "rep-a", manage_sellers: permission, revision: permissionRevision } });
    if (path.endsWith("/sellers")) return route.fulfill({ json: { representative_id: "rep-a", sellers: options.crossTenant ? sellers.map(s => ({ ...s, representative_id: "other-rep" })) : sellers, limit: 25, offset: Number(url.searchParams.get("offset")) } });
    return route.fulfill({ status: 404, json: { error: "not_found" } });
  });
  return { writes, sellers };
}
async function openOwner(page: Page) {
  await page.goto("/representatives");
  await page.getByRole("button", { name: "Manage account" }).click();
  await page.getByRole("button", { name: "Subordinate sellers", exact: true }).click();
}

test("owner grants the separate seller permission with revision and CSRF", async ({ page }) => {
  const { writes } = await fixture(page, { owner: true, granted: false });
  await openOwner(page);
  await page.getByLabel("Allow seller management").check();
  await page.getByLabel("Seller permission reason").fill("Approved seller management");
  await page.getByRole("button", { name: "Save seller permission" }).click();
  await expect(page.getByText("Change recorded.", { exact: true })).toBeVisible();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({ csrf, body: { representative_id: "rep-a", expected_revision: 0, manage_sellers: true } });
  expect(writes[0].path).toMatch(/\/seller-permission$/);
});

test("representative creates an own seller without an owner permission control", async ({ page }) => {
  const { writes } = await fixture(page, { empty: true });
  await page.goto("/account");
  await expect(page.getByLabel("Allow seller management")).toHaveCount(0);
  await page.getByRole("button", { name: "Add seller", exact: true }).click();
  await page.getByLabel("Seller ID", { exact: true }).fill("seller-new");
  await page.getByLabel("Seller name", { exact: true }).fill("فروشنده جدید");
  await page.getByLabel("Telegram user ID", { exact: true }).fill("999123456");
  await page.getByRole("button", { name: "Create seller", exact: true }).click();
  await expect(page.getByRole("button", { name: /فروشنده جدید/ })).toBeVisible();
  expect(writes[0]).toMatchObject({ csrf, body: { representative_id: "rep-a", seller_id: "seller-new", telegram_user_id: 999123456 } });
});

test("lost quota reply retains its exact event across revoked retry and reload", async ({ page }) => {
  const { writes, sellers } = await fixture(page, { loseQuotaReply: true, denyFirstQuotaRetry: true });
  await page.goto("/account");
  await page.getByRole("button", { name: /Exact Balance Seller/ }).click();
  const details = page.getByRole("region", { name: "Seller details" });
  await expect(details.getByText("9,007,199,254,740,993", { exact: true })).toBeVisible();
  await page.getByLabel("Quota volume unit").selectOption("bytes");
  await page.getByLabel("Quota volume", { exact: true }).fill("1");
  await page.getByLabel("Quota transfer reason").fill("Exact one byte allocation");
  await page.getByRole("button", { name: "Record transfer" }).click();
  await expect(page.getByRole("button", { name: "Retry same request" })).toBeVisible();
  await expect(page.getByLabel("Quota volume", { exact: true })).toBeDisabled();
  await page.reload();
  await expect(page.getByRole("button", { name: "Retry same request" })).toBeVisible();
  await page.getByRole("button", { name: "Retry same request" }).click();
  await expect.poll(() => writes.length).toBe(2);
  await expect(page.getByRole("button", { name: "Retry same request" })).toBeEnabled();
  await expect(page.getByLabel("Quota volume", { exact: true })).toBeDisabled();
  await page.reload();
  await page.getByRole("button", { name: "Retry same request" }).click();
  await expect(page.getByText("Change recorded.", { exact: true })).toBeVisible();
  await expect(details.getByText("9,007,199,254,740,994", { exact: true })).toBeVisible();
  expect(writes).toHaveLength(3);
  expect(writes[0].body).toEqual(writes[1].body);
  expect(writes[0].body).toEqual(writes[2].body);
  expect(writes[0].body).toMatchObject({ bytes: "1", units: "0", action: "allocate" });
  expect(sellers[0].reserved_bytes).toBe("9007199254740994");
});

test("a new financial write is refused when its recovery journal cannot be saved", async ({ page }) => {
  await page.addInitScript(() => {
    const save = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("dark-pro-seller-request-v2:")) throw new DOMException("Storage unavailable", "QuotaExceededError");
      return save.call(this, key, value);
    };
  });
  const { writes } = await fixture(page);
  await page.goto("/account");
  await page.getByRole("button", { name: /Exact Balance Seller/ }).click();
  await page.getByLabel("Quota volume unit").selectOption("bytes");
  await page.getByLabel("Quota volume", { exact: true }).fill("1");
  await page.getByLabel("Quota transfer reason").fill("Must retain recovery identity");
  await page.getByRole("button", { name: "Record transfer" }).click();
  await expect(page.getByText("The browser could not save the recovery receipt; no request was sent. Restore browser storage and retry.")).toBeVisible();
  expect(writes).toHaveLength(0);
  await expect(page.getByLabel("Quota volume", { exact: true })).toBeEnabled();
});

test("disabling a seller preserves its reserve and still permits reclaim", async ({ page }) => {
  const { writes, sellers } = await fixture(page);
  await page.goto("/account");
  await page.getByRole("button", { name: /Exact Balance Seller/ }).click();
  await page.getByLabel("Seller state reason").fill("Temporarily pause sales");
  await page.getByRole("button", { name: "Disable seller", exact: true }).click();
  await expect(page.getByRole("button", { name: "Enable seller", exact: true })).toBeVisible();
  await page.getByLabel("Quota action").selectOption("reclaim");
  await page.getByLabel("Quota volume unit").selectOption("bytes");
  await page.getByLabel("Quota volume", { exact: true }).fill("1");
  await page.getByLabel("Quota transfer reason").fill("Return unused reserve");
  await page.getByRole("button", { name: "Record transfer" }).click();
  await expect(page.getByText("Change recorded.", { exact: true })).toBeVisible();
  expect(writes[0].body).toMatchObject({ enabled: false, expected_revision: 0 });
  expect(sellers[0].enabled).toBe(false);
  expect(sellers[0].reserved_bytes).toBe("9007199254740992");
});

test("missing schema and denied permission are not presented as empty balances", async ({ page }) => {
  await fixture(page, { enabled: false });
  await page.goto("/account");
  await expect(page.getByText("Seller management has not been enabled on this HUB yet.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Add seller" })).toHaveCount(0);
  await page.unrouteAll();
  await fixture(page, { granted: false });
  await page.reload();
  await expect(page.getByText("This account does not have permission to manage sellers.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Add seller" })).toHaveCount(0);
});

test("a cross-tenant seller response blocks the workspace", async ({ page }) => {
  await fixture(page, { crossTenant: true });
  await page.goto("/account");
  await expect(page.getByText("Seller information could not be verified. Refresh before making a change.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Add seller" })).toHaveCount(0);
  await expect(page.getByText("Exact Balance Seller", { exact: true })).toHaveCount(0);
});

test("Persian seller workspace fits a narrow mobile screen", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.addInitScript(() => localStorage.setItem("dark-pro-lang", "fa"));
  await fixture(page);
  await page.goto("/account");
  const region = page.getByRole("region", { name: "Seller management" });
  await expect(page.locator("html")).toHaveAttribute("lang", "fa");
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(region.getByRole("button", { name: /Exact Balance Seller/ })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth);
  expect(overflow).toBe(false);
  await region.getByRole("button", { name: /Exact Balance Seller/ }).click();
  await expect(page.getByRole("region", { name: "Seller details" })).toBeVisible();
  await page.screenshot({ path: test.info().outputPath("sellers-mobile-fa.png"), fullPage: true });
});
