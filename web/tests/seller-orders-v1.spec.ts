import { expect, test, type Page } from "@playwright/test";
import type { SellerOrderClient, SellerOrderReceipt } from "../src/business/sellerOrdersV1";

const csrf = "c".repeat(64), initialBytes = "9007199254741993";
type Write = { path: string; csrf: string; body: Record<string, unknown> };
type Options = { owner?: boolean; capability?: unknown; create?: boolean; renew?: boolean; sellerEnabled?: boolean;
  lose?: "create" | "extend"; denyRetry?: boolean; corrupt?: "actor" | "balance" };

async function fixture(page: Page, options: Options = {}) {
  const user = { id: options.owner ? "owner" : "rep-a", username: "orders-qa", role: options.owner ? "owner" : "representative",
    representative_id: options.owner ? "owner" : "rep-a", permissions: { manage_sellers: true, create_clients: true, renew_clients: true } };
  const currentUser = { ...user, permissions: { ...user.permissions, create_clients: options.create ?? true, renew_clients: options.renew ?? true } };
  const seller = { id: "seller-a", representative_id: "rep-a", name: "Order reserve seller", telegram_user_id: "123456789",
    enabled: options.sellerEnabled ?? true, reserved_bytes: initialBytes, reserved_units: "100", revision: 0 };
  const clients = new Map<string, SellerOrderClient>(), receipts = new Map<string, SellerOrderReceipt>(), writes: Write[] = [];
  let lost = false, denied = false, corrupted = false;
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user, csrf_token: csrf, expires_unix: 1900000000 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { as_of: new Date().toISOString(), total_nodes: 0, online_nodes: 0, offline_nodes: 0, nodes: [], metric_scope: "default_route_interface_not_billable", physical_rates_complete: false } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname;
    const rep = { id: "rep-a", display_name: "Zero free credit representative", kind: "representative", enabled: true,
      volume_credit_bytes: 0, unlimited_credit_units: 0, client_count: clients.size, archived_count: 0, created_unix: 1 };
    if (path === "/api/v1/workspace/me") {
      const capability = "capability" in options ? options.capability : true;
      return route.fulfill({ json: { user: currentUser, balance: { ...rep, id: user.representative_id }, data_plane: "not_activated",
        ...(capability === undefined ? {} : { capabilities: { seller_orders_v1: capability } }) } });
    }
    if (path === "/api/v1/workspace/representatives") return route.fulfill({ json: { representatives: [rep], total: 1,
      limit: Number(url.searchParams.get("limit")), offset: Number(url.searchParams.get("offset")), q: url.searchParams.get("q") ?? "",
      state: url.searchParams.get("state") ?? "", sort: url.searchParams.get("sort") ?? "", has_more: false } });
    if (req.method() === "GET" && path.endsWith("/seller-permission")) return route.fulfill({ json: { representative_id: "rep-a", manage_sellers: true, revision: 1 } });
    if (req.method() === "GET" && path.endsWith("/sellers")) return route.fulfill({ json: { representative_id: "rep-a", sellers: [seller], limit: 25, offset: Number(url.searchParams.get("offset")) } });
    const base = "/api/v1/workspace/representatives/rep-a/sellers/seller-a/clients";
    if (req.method() !== "POST" || !(path === base || path.startsWith(base + "/") && path.endsWith("/extend"))) return route.fulfill({ status: 404, json: { error: "not_found" } });
    const body = req.postDataJSON() as Record<string, unknown>, action = path === base ? "create" : "extend";
    writes.push({ path, csrf: req.headers()["x-csrf-token"] ?? "", body });
    if (req.headers()["x-csrf-token"] !== csrf || body.representative_id !== "rep-a" || body.seller_id !== "seller-a") return route.fulfill({ status: 403, json: { error: "denied" } });
    const existing = receipts.get(String(body.event_id));
    if (existing) {
      if (options.denyRetry && !denied) { denied = true; return route.fulfill({ status: 403, json: { error: "permission_revoked" } }); }
      return route.fulfill({ status: action === "create" ? 201 : 200, json: { ...existing, replayed: true } });
    }
    const now = Math.floor(Date.now() / 1000), id = String(body.client_id);
    let client: SellerOrderClient, previous: SellerOrderClient | undefined, bytes = 0n, units = 0n;
    if (action === "create") {
      bytes = BigInt(String(body.volume_bytes));
      units = body.unlimited ? (BigInt(Number(body.duration_days)) * BigInt(Number(body.ip_count)) + 29n) / 30n : 0n;
      client = { id, representative_id: "rep-a", name: String(body.name), uuid: "11111111-2222-4333-8444-555555555555",
        plan_kind: body.unlimited ? "unlimited" : "volume", ip_count: Number(body.ip_count), volume_bytes: bytes.toString(),
        duration_days: Number(body.duration_days), charged_volume_bytes: bytes.toString(), charged_unlimited_units: units.toString(),
        created_unix: now, expires_unix: now + Number(body.duration_days) * 86400, deployment_state: "pending_node_apply", archived: false };
    } else {
      previous = clients.get(id);
      if (!previous) return route.fulfill({ status: 404, json: { error: "client_missing" } });
      previous = { ...previous };
      const days = previous.duration_days + Number(body.additional_days), added = BigInt(String(body.additional_volume_bytes));
      bytes = previous.plan_kind === "volume" ? added : 0n;
      units = previous.plan_kind === "unlimited" ? (BigInt(days) * BigInt(previous.ip_count) + 29n) / 30n - BigInt(previous.charged_unlimited_units) : 0n;
      client = { ...previous, duration_days: days, volume_bytes: (BigInt(previous.volume_bytes) + added).toString(),
        charged_volume_bytes: (BigInt(previous.charged_volume_bytes) + bytes).toString(), charged_unlimited_units: (BigInt(previous.charged_unlimited_units) + units).toString(),
        expires_unix: (Number(body.additional_days) > 0 ? Math.max(previous.expires_unix, now) : previous.expires_unix) + Number(body.additional_days) * 86400 };
    }
    if (BigInt(seller.reserved_bytes) < bytes || BigInt(seller.reserved_units) < units) return route.fulfill({ status: 409, json: { error: "insufficient_credit" } });
    const receipt: SellerOrderReceipt = { event_id: String(body.event_id), actor_user_id: user.id, representative_id: "rep-a", seller_id: seller.id,
      action, client, ...(previous ? { previous_client: previous } : {}), volume_debit_bytes: bytes.toString(), unlimited_debit_units: units.toString(),
      reserved_bytes_before: seller.reserved_bytes, reserved_bytes_after: (BigInt(seller.reserved_bytes) - bytes).toString(),
      reserved_units_before: seller.reserved_units, reserved_units_after: (BigInt(seller.reserved_units) - units).toString(), created_unix: now, replayed: false };
    receipts.set(receipt.event_id, receipt); clients.set(id, client);
    seller.reserved_bytes = receipt.reserved_bytes_after; seller.reserved_units = receipt.reserved_units_after;
    if (options.lose === action && !lost) { lost = true; return route.abort("failed"); }
    if (options.corrupt && !corrupted) {
      corrupted = true;
      return route.fulfill({ status: action === "create" ? 201 : 200, json: { ...receipt,
        ...(options.corrupt === "actor" ? { actor_user_id: "other-user" } : { reserved_bytes_after: "0" }) } });
    }
    return route.fulfill({ status: action === "create" ? 201 : 200, json: receipt });
  });
  return { writes, seller, clients, receipts };
}

async function open(page: Page, owner = false) {
  await page.goto(owner ? "/representatives" : "/account");
  if (owner) {
    await page.getByRole("button", { name: "Manage account" }).click();
    await page.getByRole("button", { name: "Subordinate sellers", exact: true }).click();
  }
  await page.getByRole("button", { name: /Order reserve seller/ }).click();
}
async function createVolume(page: Page, amount = "100") {
  await page.getByLabel("Order client ID").fill("client-order-a");
  await page.getByLabel("Order client name").fill("Synthetic client order");
  await page.getByLabel("Order volume unit").selectOption("bytes");
  await page.getByLabel("Order volume", { exact: true }).fill(amount);
  await page.getByLabel("Order reason").fill("Reviewed synthetic sale");
  await page.getByRole("button", { name: "Record client creation", exact: true }).click();
}
async function renew(page: Page, amount = "0", days = "15") {
  await page.getByRole("button", { name: "Renew client", exact: true }).click();
  await page.getByLabel("Order client ID").fill("client-order-a");
  await page.getByLabel("Order days").fill(days);
  await page.getByLabel("Order volume unit").selectOption("bytes");
  await page.getByLabel("Order volume", { exact: true }).fill(amount);
  await page.getByLabel("Order reason").fill("Reviewed synthetic renewal");
  await page.getByRole("button", { name: "Record renewal", exact: true }).click();
}
const receipt = (page: Page) => page.getByLabel("Seller order receipt");

test("representative with zero free credit submits exact seller volume and shows recorded balances", async ({ page }) => {
  const f = await fixture(page);
  await open(page);
  await createVolume(page, "9007199254740993");
  await expect(receipt(page)).toBeVisible();
  await expect(receipt(page).getByText("9,007,199,254,740,993", { exact: true })).toBeVisible();
  await expect(receipt(page).getByText("1,000", { exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(1);
  expect(f.writes[0]).toMatchObject({ csrf, path: "/api/v1/workspace/representatives/rep-a/sellers/seller-a/clients",
    body: { representative_id: "rep-a", seller_id: "seller-a", client_id: "client-order-a", volume_bytes: "9007199254740993", unlimited: false } });
  expect(f.seller.reserved_bytes).toBe("1000");
  await expect(receipt(page).getByText("The receipt records the credit charge. Client activation and subscription delivery follow separately.")).toBeVisible();
});

test("owner creates unlimited client and renews with cumulative units without volume debit", async ({ page }) => {
  const f = await fixture(page, { owner: true });
  await open(page, true);
  await page.getByLabel("Order client ID").fill("client-order-a");
  await page.getByLabel("Order client name").fill("Unlimited synthetic order");
  await page.getByLabel("Order plan").selectOption("unlimited");
  await page.getByLabel("Order days").fill("60");
  await page.getByLabel("Order users").fill("3");
  await page.getByLabel("Order reason").fill("Synthetic unlimited sale");
  await page.getByRole("button", { name: "Record client creation", exact: true }).click();
  await expect(receipt(page)).toBeVisible();
  expect(f.seller.reserved_units).toBe("94");
  await renew(page);
  await expect.poll(() => f.seller.reserved_units).toBe("92");
  await expect(receipt(page)).toBeVisible();
  await renew(page);
  await expect.poll(() => f.seller.reserved_units).toBe("91");
  await expect(receipt(page)).toBeVisible();
  expect(f.seller.reserved_bytes).toBe(initialBytes);
  expect(f.writes.map(w => w.body.volume_bytes ?? w.body.additional_volume_bytes)).toEqual(["0", "0", "0"]);
  expect(f.writes[1].path).toBe("/api/v1/workspace/representatives/rep-a/sellers/seller-a/clients/client-order-a/extend");
});

test("lost creation response retains exact request across revoked retry and reload", async ({ page }) => {
  const f = await fixture(page, { lose: "create", denyRetry: true });
  await open(page); await createVolume(page);
  const retry = page.getByRole("button", { name: "Retry same request", exact: true });
  await expect(retry).toBeVisible();
  await expect(page.getByLabel("Order client ID")).toBeDisabled();
  await page.reload();
  await retry.click();
  await expect.poll(() => f.writes.length).toBe(2);
  await expect(retry).toBeEnabled();
  await expect(page.getByLabel("Order volume", { exact: true })).toBeDisabled();
  await page.reload();
  await retry.click();
  await expect(receipt(page).getByText("Recovered existing receipt")).toBeVisible();
  expect(f.writes).toHaveLength(3);
  expect(f.writes[0]).toEqual(f.writes[1]); expect(f.writes[1]).toEqual(f.writes[2]);
  expect(f.clients.size).toBe(1);
  expect(f.seller.reserved_bytes).toBe((BigInt(initialBytes) - 100n).toString());
});

test("lost renewal response is recovered without charging its additional volume twice", async ({ page }) => {
  const f = await fixture(page, { lose: "extend" });
  await open(page); await createVolume(page);
  await expect(receipt(page)).toBeVisible();
  await renew(page, "40");
  await expect(page.getByRole("button", { name: "Retry same request", exact: true })).toBeVisible();
  await page.reload();
  await page.getByRole("button", { name: "Retry same request", exact: true }).click();
  await expect(receipt(page).getByText("Recovered existing receipt")).toBeVisible();
  expect(f.writes).toHaveLength(3);
  expect(f.writes[1]).toEqual(f.writes[2]);
  expect(f.clients.get("client-order-a")?.volume_bytes).toBe("140");
  expect(f.seller.reserved_bytes).toBe((BigInt(initialBytes) - 140n).toString());
});

for (const corrupt of ["actor", "balance"] as const) test(`a success response with unverified ${corrupt} retains the original nonce`, async ({ page }) => {
  const f = await fixture(page, { corrupt });
  await open(page); await createVolume(page);
  await expect(page.getByRole("button", { name: "Retry same request", exact: true })).toBeVisible();
  await expect(receipt(page)).toHaveCount(0);
  await page.getByRole("button", { name: "Retry same request", exact: true }).click();
  await expect(receipt(page)).toBeVisible();
  expect(f.writes).toHaveLength(2); expect(f.writes[0]).toEqual(f.writes[1]);
  expect(f.seller.reserved_bytes).toBe((BigInt(initialBytes) - 100n).toString());
});

for (const capability of [undefined, false, "true"]) test(`missing, false or malformed capability blocks orders: ${String(capability)}`, async ({ page }) => {
  const f = await fixture(page, { capability });
  await open(page);
  await expect(page.getByText("Client orders from seller reserves are not enabled on this HUB.")).toBeVisible();
  await expect(page.getByRole("form", { name: "Create seller client" })).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
});

test("current account grants override stale session permissions and disabled sellers block both operations", async ({ page }) => {
  const f = await fixture(page, { create: false, renew: true });
  await open(page);
  await expect(page.getByRole("button", { name: "Create client", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Renew client", exact: true })).toBeEnabled();
  await expect(page.getByRole("form", { name: "Renew seller client" })).toBeVisible();
  f.seller.enabled = false;
  await page.getByRole("region", { name: "Seller management" }).getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(page.getByRole("button", { name: "Renew client", exact: true })).toBeDisabled();
  await expect(page.getByLabel("Order client ID")).toBeDisabled();
  expect(f.writes).toHaveLength(0);
});

test("an order is not sent when its recovery journal cannot be saved", async ({ page }) => {
  await page.addInitScript(() => {
    const save = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("dark-pro-seller-request-v2:")) throw new DOMException("Full", "QuotaExceededError");
      return save.call(this, key, value);
    };
  });
  const f = await fixture(page);
  await open(page); await createVolume(page);
  await expect(page.getByText("The browser could not save the recovery receipt; no request was sent. Restore browser storage and retry.")).toBeVisible();
  expect(f.writes).toHaveLength(0);
});

test("current renewal revocation disables renewal while creation remains available", async ({ page }) => {
  const f = await fixture(page, { create: true, renew: false });
  await open(page);
  await expect(page.getByRole("button", { name: "Create client", exact: true })).toBeEnabled();
  await expect(page.getByRole("button", { name: "Renew client", exact: true })).toBeDisabled();
  await expect(page.getByRole("form", { name: "Create seller client" })).toBeVisible();
  expect(f.writes).toHaveLength(0);
});

test("definitive insufficient reserve clears the failed event without inventing a receipt", async ({ page }) => {
  const f = await fixture(page);
  await open(page); await createVolume(page, "1000000000000000000");
  await expect(page.getByText("The change was rejected. Refresh the current information and try again.")).toBeVisible();
  await expect(receipt(page)).toHaveCount(0);
  await expect(page.getByLabel("Order client ID")).toBeEnabled();
  expect(await page.evaluate(() => sessionStorage.getItem("dark-pro-seller-request-v2:rep-a:rep-a"))).toBeNull();
  expect(f.writes).toHaveLength(1); expect(f.clients.size).toBe(0); expect(f.seller.reserved_bytes).toBe(initialBytes);
  await createVolume(page, "100");
  await expect(receipt(page)).toBeVisible();
  expect(f.writes).toHaveLength(2);
  expect(f.writes[0].body.event_id).not.toBe(f.writes[1].body.event_id);
  expect(f.seller.reserved_bytes).toBe((BigInt(initialBytes) - 100n).toString());
});

test("Persian order form and verified receipt amounts fit a narrow mobile screen", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.addInitScript(() => localStorage.setItem("dark-pro-lang", "fa"));
  await fixture(page); await open(page);
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await page.getByLabel("Order client ID").fill("client-order-a");
  await page.getByLabel("Order client name").fill("مشتری آزمایشی فروشنده");
  await page.getByLabel("Order volume unit").selectOption("bytes");
  await page.getByLabel("Order volume", { exact: true }).fill("100");
  await page.getByLabel("Order reason").fill("ثبت آزمایشی از سهمیه فروشنده");
  await page.getByRole("button", { name: "ثبت ساخت مشتری", exact: true }).click();
  await expect(receipt(page)).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false);
  await page.screenshot({ path: test.info().outputPath("seller-orders-mobile-fa.png"), fullPage: true });
});
