import { expect, test, type Page } from "@playwright/test";

const csrf = "c".repeat(64);
const now = Math.floor(Date.now() / 1000);
type Role = "owner" | "representative";
async function fixture(page: Page, role: Role, enabled = true) {
  const writes: { body: Record<string, unknown>; csrf?: string }[] = [];
  const user = { id: role === "owner" ? "owner" : "rep-a", role, username: "test", representative_id: role === "owner" ? "owner" : "rep-a", permissions: { view_clients: true } };
  const stores = [{ id: "shop-a", bot_id: 10001, representative_id: "rep-a", name: "Example store", manager_user_id: 10002, enabled: true, revision: 1, connection_state: "polling", last_poll_unix: now, queue_status_available: true, inbox_pending: 4, financial_retry: 2, review_pending: 1, outbox_pending: 3, outbox_unknown: 1, delivery_review: 0 }];
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user, csrf_token: csrf, expires_unix: now + 3600 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { nodes: [], online_nodes: 0, offline_nodes: 0, total_nodes: 0, as_of: new Date().toISOString() } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const request = route.request(), path = new URL(request.url()).pathname;
    if (path === "/api/v1/workspace/me") return route.fulfill({ json: { user, balance: {}, capabilities: { telegram_bots_v1: enabled } } });
    if (path === "/api/v1/workspace/telegram" && request.method() === "GET") return route.fulfill({ json: { enabled, can_configure: enabled && role === "owner", stores: enabled ? stores : [] } });
    if (path === "/api/v1/workspace/telegram/stores" && request.method() === "POST") {
      const body = request.postDataJSON(); writes.push({ body, csrf: request.headers()["x-csrf-token"] });
      if (role !== "owner" || request.headers()["x-csrf-token"] !== csrf) return route.fulfill({ status: 403, json: { error: "denied" } });
      const store = { ...body, revision: 1, connection_state: "not_configured", last_poll_unix: 0 };
      stores.push(store); return route.fulfill({ json: store });
    }
    return route.fulfill({ status: 404, json: { error: "not_found" } });
  });
  return writes;
}

test("disabled Telegram has an honest empty state and no store mutations", async ({ page }) => {
  const writes = await fixture(page, "owner", false);
  await page.goto("/telegram");
  await expect(page.getByRole("heading", { name: "Telegram stores are not enabled" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Add store", exact: true })).toHaveCount(0);
  await expect(page.locator("input")).toHaveCount(0);
  expect(writes).toHaveLength(0);
});

test("representative sees scoped status without owner store controls", async ({ page }) => {
  const writes = await fixture(page, "representative");
  await page.goto("/telegram");
  await expect(page.getByRole("heading", { name: "Example store" })).toBeVisible();
  await expect(page.locator(".telegram-store-card .telegram-state")).toHaveText("Receiving updates");
  await expect(page.locator(".telegram-queues")).toContainText("Financial retries");
  await expect(page.locator(".telegram-queues")).toContainText("An uncertain send is not repeated automatically.");
  await expect(page.getByRole("button", { name: /Add store|Manage store/ })).toHaveCount(0);
  expect(writes).toHaveLength(0);
});

test("owner registers numeric bot identity with CSRF and no credential fields", async ({ page }) => {
  const writes = await fixture(page, "owner");
  await page.goto("/telegram");
  await page.getByRole("button", { name: "Add store", exact: true }).click();
  const form = page.getByRole("form", { name: "Store settings" });
  await form.getByLabel("Store ID", { exact: true }).fill("new-shop");
  await form.getByLabel("Store name", { exact: true }).fill("New owner brand");
  await form.getByLabel("Bot numeric ID", { exact: true }).fill("10003");
  await form.getByLabel("Manager numeric ID", { exact: true }).fill("10004");
  await expect(form.locator("input[type=password],input[name*=token]")).toHaveCount(0);
  expect(writes).toHaveLength(0);
  await form.getByRole("button", { name: "Save store", exact: true }).click();
  await expect(page.getByRole("heading", { name: "New owner brand" })).toBeVisible();
  expect(writes).toHaveLength(1);
  expect(writes[0]).toMatchObject({ csrf, body: { id: "new-shop", bot_id: 10003, manager_user_id: 10004, representative_id: "owner", expected_revision: 0, enabled: true } });
  expect(writes[0].body.event_id).toMatch(/^telegram-store-/);
  await expect(page.locator(".telegram-store-card").filter({ hasText: "New owner brand" }).locator(".telegram-state")).toHaveText("Credential not configured");
});

test("320px Persian store form remains usable and contains no secret storage", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 820 });
  await page.addInitScript(() => localStorage.setItem("dark-pro-lang", "fa"));
  await fixture(page, "owner");
  await page.goto("/telegram");
  await page.getByRole("button", { name: "افزودن فروشگاه", exact: true }).click();
  await expect(page.getByRole("form", { name: "تنظیمات فروشگاه" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.dir)).toBe("rtl");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  expect(await page.evaluate(() => Object.keys(localStorage).filter(key => /token|secret|credential/i.test(key)))).toEqual([]);
});

test("failed status fetch never displays a connected badge", async ({ page }) => {
  await fixture(page, "owner");
  await page.route("**/api/v1/workspace/telegram", route => route.fulfill({ status: 503, json: { error: "unavailable" } }));
  await page.goto("/telegram");
  await expect(page.getByRole("alert")).toContainText("could not be loaded");
  await expect(page.locator(".telegram-store-card")).toHaveCount(0);
});
