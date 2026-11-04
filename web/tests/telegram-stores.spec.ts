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

test("metadata-only host keeps registration separate from unavailable connection controls", async ({ page }) => {
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
  await expect(page.locator(".telegram-store-card").filter({ hasText: "New owner brand" }).locator(".telegram-state")).toHaveText("Connection not configured");
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


// Synthetic credentials only. A real token must never be put in a browser test.
const labToken = "10003:ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
type ManagedStore = {
  id: string; bot_id: number; representative_id: string; name: string; manager_user_id: number;
  enabled: boolean; revision: number; connection_state: string; last_poll_unix: number;
  credential_configured: boolean; bot_username: string; connection_problem: string;
  queue_status_available: boolean; inbox_pending: number; financial_retry: number; review_pending: number;
  outbox_pending: number; outbox_unknown: number; delivery_review: number;
};
async function managedFixture(page: Page, role: Role = "owner") {
  const state = {
    revision: 7,
    stores: [{
      id: "shop-a", bot_id: 10001, representative_id: "rep-a", name: "Example store", manager_user_id: 10002,
      enabled: true, revision: 1, connection_state: "polling", last_poll_unix: now,
      credential_configured: true, bot_username: "existing_lab_bot", connection_problem: "",
      queue_status_available: true, inbox_pending: 0, financial_retry: 0, review_pending: 0,
      outbox_pending: 0, outbox_unknown: 0, delivery_review: 0,
    }] as ManagedStore[],
    identity: { bot_id: 10003, username: "new_lab_bot" },
    verifyError: "",
    connectionErrors: [] as string[],
    verifyGate: null as Promise<void> | null,
    writes: [] as { path: string; body: Record<string, unknown>; csrf?: string }[],
  };
  const user = { id: role === "owner" ? "owner" : "rep-a", role, username: "test", representative_id: role === "owner" ? "owner" : "rep-a", permissions: { view_clients: true } };
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user, csrf_token: csrf, expires_unix: now + 3600 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { nodes: [], online_nodes: 0, offline_nodes: 0, total_nodes: 0, as_of: new Date().toISOString() } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (path === "/api/v1/workspace/me") return route.fulfill({ json: { user, balance: {}, capabilities: { telegram_bots_v1: true } } });
    if (path === "/api/v1/workspace/telegram" && request.method() === "GET") return route.fulfill({ json: {
      enabled: true, can_configure: role === "owner", can_manage_connections: role === "owner", runtime_revision: state.revision,
      stores: state.stores.map(store => ({ ...store, connection_state: store.enabled ? store.connection_state : "store_disabled" })),
    } });
    if (path === "/api/v1/workspace/representatives") {
      const reps = [{ id: "rep-a", display_name: "Alpha representative", kind: "representative", enabled: true },
        { id: "rep-b", display_name: "نماینده بتا", kind: "representative", enabled: true }];
      const query = url.searchParams.get("q") ?? "", filtered = reps.filter(rep => (rep.id + rep.display_name).includes(query));
      return route.fulfill({ json: { representatives: filtered, has_more: false, offset: 0, limit: 50, total: filtered.length } });
    }
    if (request.method() !== "POST") return route.fulfill({ status: 404, json: { error: "not_found" } });
    const body = request.postDataJSON() as Record<string, unknown>;
    state.writes.push({ path, body, csrf: request.headers()["x-csrf-token"] });
    if (role !== "owner" || request.headers()["x-csrf-token"] !== csrf) return route.fulfill({ status: 403, json: { error: "owner_permission_required" } });
    if (path === "/api/v1/workspace/telegram/verify") {
      if (state.verifyGate) await state.verifyGate;
      if (state.verifyError) return route.fulfill({ status: state.verifyError === "telegram_webhook_exists" ? 409 : state.verifyError === "session_required" ? 401 : 422, json: { error: state.verifyError } }).catch(() => {});
      return route.fulfill({ json: state.identity }).catch(() => {});
    }
    if (path === "/api/v1/workspace/telegram/stores") {
      const index = state.stores.findIndex(store => store.id === body.id), previous = state.stores[index];
      if (body.expected_revision !== (previous?.revision ?? 0)) return route.fulfill({ status: 409, json: { error: "revision_conflict" } });
      const result = { ...previous, ...body, revision: (previous?.revision ?? 0) + 1,
        connection_state: previous?.connection_state ?? "not_configured", last_poll_unix: previous?.last_poll_unix ?? 0,
        credential_configured: previous?.credential_configured ?? false, bot_username: previous?.bot_username ?? "", connection_problem: "",
        queue_status_available: false, inbox_pending: 0, financial_retry: 0, review_pending: 0, outbox_pending: 0, outbox_unknown: 0, delivery_review: 0,
      } as ManagedStore;
      if (index >= 0) state.stores[index] = result; else state.stores.push(result);
      return route.fulfill({ json: result });
    }
    const match = path.match(/^\/api\/v1\/workspace\/telegram\/stores\/([^/]+)\/(check|connection)$/);
    if (match) {
      const store = state.stores.find(value => value.id === decodeURIComponent(match[1]));
      if (!store) return route.fulfill({ status: 404, json: { error: "telegram_store_not_found" } });
      if (match[2] === "check") return route.fulfill({ json: { bot_id: store.bot_id, username: store.bot_username } });
      const error = state.connectionErrors.shift();
      if (error) {
        if (error === "telegram_revision_conflict") state.revision++;
        return route.fulfill({ status: error === "telegram_revision_conflict" ? 409 : 503, json: { error } });
      }
      if (body.expected_revision !== state.revision) return route.fulfill({ status: 409, json: { error: "telegram_revision_conflict" } });
      state.revision++;
      store.credential_configured = true;
      if (body.token) store.bot_username = state.identity.username;
      store.connection_state = "starting"; store.last_poll_unix = 0;
      return route.fulfill({ json: { revision: state.revision, bot_id: store.bot_id, username: store.bot_username, state: "starting" } });
    }
    return route.fulfill({ status: 404, json: { error: "not_found" } });
  });
  return state;
}
async function newBotForm(page: Page) {
  await page.getByRole("button", { name: "Add bot", exact: true }).click();
  const form = page.getByRole("form", { name: "Store settings" });
  await form.getByLabel("Bot token", { exact: true }).fill(labToken);
  await form.getByRole("button", { name: "Verify bot", exact: true }).click();
  await expect(form.locator(".telegram-verified")).toContainText("@new_lab_bot");
  await form.getByLabel("Store name", { exact: true }).fill("New owner brand");
  await form.getByLabel("Manager numeric ID", { exact: true }).fill("10004");
  return form;
}
async function expectNoStoredToken(page: Page) {
  const values = await page.evaluate(() => ({
    local: Object.keys(localStorage).map(key => [key, localStorage.getItem(key)]),
    session: Object.keys(sessionStorage).map(key => [key, sessionStorage.getItem(key)]),
    url: location.href,
  }));
  expect(JSON.stringify(values)).not.toContain(labToken);
}

test("owner verifies a token then saves and activates its verified bot identity", async ({ page }) => {
  const state = await managedFixture(page);
  await page.goto("/telegram");
  const form = await newBotForm(page);
  expect(state.writes).toHaveLength(1);
  expect(state.writes[0]).toMatchObject({ path: "/api/v1/workspace/telegram/verify", body: { token: labToken }, csrf });
  await expect(form.getByLabel("Bot numeric ID", { exact: true })).toHaveCount(0);
  await form.getByRole("button", { name: "Save & activate bot", exact: true }).click();
  const card = page.locator(".telegram-store-card").filter({ has: page.getByRole("heading", { name: "New owner brand", exact: true }) });
  await expect(card).toBeVisible();
  await expect(form).toHaveCount(0);
  const registration = state.writes.find(write => write.path.endsWith("/stores"))!;
  expect(registration.body).toMatchObject({ bot_id: 10003, manager_user_id: 10004, representative_id: "owner", expected_revision: 0, enabled: true });
  expect(registration.body.id).toMatch(/^store-[a-f0-9-]+$/);
  expect(registration.body).not.toHaveProperty("token");
  const connection = state.writes.find(write => write.path.endsWith("/connection"))!;
  expect(connection).toMatchObject({ csrf, body: { token: labToken, expected_revision: 7 } });
  expect(connection.path).toBe("/api/v1/workspace/telegram/stores/" + registration.body.id + "/connection");
  await expect(card.locator(".telegram-state")).toHaveText("Checking bot identity");
  await expect(card.getByRole("link", { name: "Open bot" })).toHaveAttribute("href", "https://t.me/new_lab_bot");
  const stored = state.stores.find(store => store.id === registration.body.id)!;
  stored.connection_state = "polling"; stored.last_poll_unix = now;
  await page.getByRole("button", { name: "Refresh", exact: true }).click();
  await expect(card.locator(".telegram-state")).toHaveText("Receiving updates");
  await card.getByRole("button", { name: "Manage bot", exact: true }).click();
  await expect(page.getByLabel("New bot token (optional)", { exact: true })).toHaveValue("");
  await expectNoStoredToken(page);
});

test("verification refuses an existing webhook before registration or activation", async ({ page }) => {
  const state = await managedFixture(page);
  state.verifyError = "telegram_webhook_exists";
  await page.goto("/telegram");
  await page.getByRole("button", { name: "Add bot", exact: true }).click();
  const form = page.getByRole("form", { name: "Store settings" });
  await form.getByLabel("Bot token", { exact: true }).fill(labToken);
  await form.getByRole("button", { name: "Verify bot", exact: true }).click();
  await expect(form.getByRole("alert")).toContainText("connected to another service");
  await expect(form.getByRole("button", { name: "Save & activate bot", exact: true })).toBeDisabled();
  expect(state.writes.map(write => write.path)).toEqual(["/api/v1/workspace/telegram/verify"]);
  await form.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("button", { name: "Add bot", exact: true }).click();
  await expect(page.getByLabel("Bot token", { exact: true })).toHaveValue("");
  await expectNoStoredToken(page);
});

test("partial connection failure retries the saved store without another registration", async ({ page }) => {
  const state = await managedFixture(page);
  state.connectionErrors.push("telegram_transport_unavailable");
  await page.goto("/telegram");
  const form = await newBotForm(page);
  await form.getByRole("button", { name: "Save & activate bot", exact: true }).click();
  await expect(form).toContainText("The store was saved, but its connection was not confirmed");
  await expect(form.getByLabel("Bot token", { exact: true })).toHaveValue("");
  expect(state.writes.filter(write => write.path.endsWith("/stores"))).toHaveLength(1);
  await form.getByLabel("Bot token", { exact: true }).fill(labToken);
  await form.getByRole("button", { name: "Verify bot", exact: true }).click();
  await expect(form.locator(".telegram-verified")).toBeVisible();
  await form.getByRole("button", { name: "Save & activate bot", exact: true }).click();
  await expect(form).toHaveCount(0);
  expect(state.writes.filter(write => write.path.endsWith("/stores"))).toHaveLength(1);
  expect(state.writes.filter(write => write.path.endsWith("/connection"))).toHaveLength(2);
  expect(state.stores.filter(store => store.bot_id === 10003)).toHaveLength(1);
  await expectNoStoredToken(page);
});

test("saved-token check is distinct from polling and reconnect needs no token re-entry", async ({ page }) => {
  const state = await managedFixture(page);
  state.stores[0].connection_state = "error";
  await page.goto("/telegram");
  const card = page.locator(".telegram-store-card").filter({ has: page.getByRole("heading", { name: "Example store" }) });
  await card.getByRole("button", { name: "Check connection", exact: true }).click();
  await expect(card.getByRole("status")).toContainText("Telegram verified this bot");
  await expect(card.locator(".telegram-state")).toHaveText("Connection needs attention");
  expect(state.writes).toHaveLength(1);
  expect(state.writes[0]).toMatchObject({ path: "/api/v1/workspace/telegram/stores/shop-a/check", body: {}, csrf });
  await card.getByRole("button", { name: "Reconnect", exact: true }).click();
  await expect(card.locator(".telegram-state")).toHaveText("Checking bot identity");
  expect(state.writes[1]).toMatchObject({ path: "/api/v1/workspace/telegram/stores/shop-a/connection", body: { token: "", expected_revision: 7 }, csrf });
  await expect(page.locator("input[type=password]")).toHaveCount(0);
});

test("connection conflict refreshes the revision and never retries automatically", async ({ page }) => {
  const state = await managedFixture(page);
  state.connectionErrors.push("telegram_revision_conflict");
  await page.goto("/telegram");
  const card = page.locator(".telegram-store-card").filter({ has: page.getByRole("heading", { name: "Example store" }) });
  await card.getByRole("button", { name: "Reconnect", exact: true }).click();
  await expect(card.getByRole("alert")).toContainText("changed in another session");
  await expect(card.getByRole("button", { name: "Reconnect", exact: true })).toBeEnabled();
  expect(state.writes).toHaveLength(1);
  await card.getByRole("button", { name: "Reconnect", exact: true }).click();
  await expect(card.locator(".telegram-state")).toHaveText("Checking bot identity");
  expect(state.writes).toHaveLength(2);
  expect(state.writes[1].body).toEqual({ token: "", expected_revision: 8 });
});

test("token replacement cannot change the existing store's bot binding", async ({ page }) => {
  const state = await managedFixture(page);
  await page.goto("/telegram");
  await page.getByRole("button", { name: "Manage bot", exact: true }).click();
  const form = page.getByRole("form", { name: "Store settings" });
  await form.getByLabel("New bot token (optional)", { exact: true }).fill(labToken);
  await form.getByRole("button", { name: "Verify bot", exact: true }).click();
  await expect(form.getByRole("alert")).toContainText("belongs to a different bot");
  await expect(form.getByRole("button", { name: "Save & activate bot", exact: true })).toBeDisabled();
  expect(state.writes).toHaveLength(1);
  expect(state.stores[0].bot_id).toBe(10001);
});

test("320px Persian onboarding selects a native representative and normalizes numeric IDs", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 820 });
  await page.addInitScript(() => localStorage.setItem("dark-pro-lang", "fa"));
  const state = await managedFixture(page);
  await page.goto("/telegram");
  await page.getByRole("button", { name: "افزودن ربات", exact: true }).click();
  const form = page.getByRole("form", { name: "تنظیمات فروشگاه" });
  await form.getByLabel("توکن ربات", { exact: true }).fill(labToken);
  await form.getByRole("button", { name: "بررسی ربات", exact: true }).click();
  await expect(form.locator(".telegram-verified")).toContainText("@new_lab_bot");
  await form.getByLabel("نام فروشگاه", { exact: true }).fill("فروشگاه آزمایشی");
  await form.getByLabel("شناسهٔ عددی مدیر", { exact: true }).fill("۱۲۳۴۵۶۷۸۹");
  await form.getByRole("radio", { name: "نماینده", exact: true }).check();
  await expect(form.getByRole("combobox", { name: "حساب نماینده", exact: true })).toContainText("نماینده بتا");
  await form.getByRole("combobox", { name: "حساب نماینده", exact: true }).selectOption("rep-b");
  expect(await page.evaluate(() => document.documentElement.dir)).toBe("rtl");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  await form.getByRole("button", { name: "ذخیره و فعال‌سازی ربات", exact: true }).click();
  await expect(form).toHaveCount(0);
  const registration = state.writes.find(write => write.path.endsWith("/stores"))!;
  expect(registration.body).toMatchObject({ manager_user_id: 123456789, representative_id: "rep-b", bot_id: 10003 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1)).toBe(true);
  await expectNoStoredToken(page);
});

test("representative never sees owner credential or activation controls", async ({ page }) => {
  const state = await managedFixture(page, "representative");
  await page.goto("/telegram");
  await expect(page.getByRole("heading", { name: "Example store" })).toBeVisible();
  await expect(page.getByRole("button", { name: /Add bot|Manage bot|Check connection|Reconnect|Pause store/ })).toHaveCount(0);
  await expect(page.locator("input[type=password]")).toHaveCount(0);
  expect(state.writes).toHaveLength(0);
});

test("cancel discards a token and ignores a late verification response", async ({ page }) => {
  const state = await managedFixture(page);
  let release!: () => void;
  state.verifyGate = new Promise<void>(resolve => { release = resolve; });
  await page.goto("/telegram");
  await page.getByRole("button", { name: "Add bot", exact: true }).click();
  const form = page.getByRole("form", { name: "Store settings" });
  await form.getByLabel("Bot token", { exact: true }).fill(labToken);
  await form.getByRole("button", { name: "Verify bot", exact: true }).click();
  await expect(form.getByRole("button", { name: "Checking…", exact: true })).toBeVisible();
  await form.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.getByRole("button", { name: "Add bot", exact: true }).click();
  release();
  await expect(page.getByLabel("Bot token", { exact: true })).toHaveValue("");
  await expect(page.locator(".telegram-verified")).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Save & activate bot", exact: true })).toBeDisabled();
  await expectNoStoredToken(page);
});

test("revoked session removes the secret form and never stores its token", async ({ page }) => {
  const state = await managedFixture(page);
  state.verifyError = "session_required";
  await page.goto("/telegram");
  await page.getByRole("button", { name: "Add bot", exact: true }).click();
  const form = page.getByRole("form", { name: "Store settings" });
  await form.getByLabel("Bot token", { exact: true }).fill(labToken);
  await form.getByRole("button", { name: "Verify bot", exact: true }).click();
  await expect(page.locator("input[name=telegram-credential]")).toHaveCount(0);
  await expect(page.getByRole("form", { name: "Store settings" })).toHaveCount(0);
  expect(state.writes).toHaveLength(1);
  await expectNoStoredToken(page);
});

test("store pause and resume change metadata without exposing or replacing credentials", async ({ page }) => {
  const state = await managedFixture(page);
  await page.goto("/telegram");
  const card = page.locator(".telegram-store-card").filter({ has: page.getByRole("heading", { name: "Example store" }) });
  await card.getByRole("button", { name: "Pause store", exact: true }).click();
  await expect(card.locator(".telegram-state")).toHaveText("Store paused");
  await expect(card).toContainText("payment records continue");
  expect(state.writes[0].body).toMatchObject({ enabled: false, expected_revision: 1, bot_id: 10001 });
  expect(state.writes[0].body).not.toHaveProperty("token");
  await card.getByRole("button", { name: "Resume store", exact: true }).click();
  await expect(card.locator(".telegram-state")).toHaveText("Receiving updates");
  expect(state.writes[1].body).toMatchObject({ enabled: true, expected_revision: 2 });
  expect(state.writes.every(write => write.path.endsWith("/stores"))).toBe(true);
});
