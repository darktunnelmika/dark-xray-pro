import { expect, test, type Locator, type Page } from "@playwright/test";
import { readInboundSubscriptionSecret } from "../src/inboundSubscriptions";

const csrf = "a".repeat(64);
const alpha = "sub-alpha", beta = "sub-beta";
const tokenFor = (id: string, generation: number) => Buffer.alloc(32, (id === alpha ? 16 : 64) + generation).toString("base64url");

test("managed subscription secret validation rejects scope, URL injection and noncanonical token encoding", () => {
  const token = tokenFor(alpha, 3), path = "/api/v1/inbound-subscriptions/" + token;
  const valid = { client_id: alpha, generation: 3, token, path };
  expect(readInboundSubscriptionSecret(valid, alpha, 3)).toBe(path);
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_";
  const noncanonical = token.slice(0, -1) + alphabet[alphabet.indexOf(token.at(-1)!) + 1];
  for (const changed of [{ client_id: beta }, { generation: 4 }, { path: "https://example.org/" + token }, { path: path + "?next=another" },
    { token: noncanonical, path: "/api/v1/inbound-subscriptions/" + noncanonical }, { password: "unexpected-private-response" }]) {
    expect(() => readInboundSubscriptionSecret({ ...valid, ...changed }, alpha, 3)).toThrow();
  }
});
type TokenState = "absent" | "active" | "revoked";
type Head = { generation: number; state: TokenState };
type Call = { path: string; method: string; body: Record<string, unknown>; csrf: string | undefined };
type Options = {
  enabled?: boolean; denied?: boolean; manage?: boolean; active?: boolean;
  loseFirstMutation?: boolean; delayReveal?: boolean; mutationStatus?: number;
  metadata?: (body: Record<string, unknown>) => Record<string, unknown>;
  receipt?: (body: Record<string, unknown>) => Record<string, unknown>;
  secret?: (body: Record<string, unknown>) => Record<string, unknown>;
};

// This fixture never resolves a subscription URL. A separate trap records any
// accidental consumer fetch, independent of the management API mock.
async function fixture(page: Page, options: Options = {}) {
  const calls: Call[] = [], consumers: string[] = [];
  const user = { id: "rep-a", username: "rep-a", role: "representative", representative_id: "rep-a", permissions: {
    view_clients: true, manage_clients: options.manage !== false, create_clients: false,
    renew_clients: false, edit_clients: false, manage_groups: false, view_finance: false,
  } };
  const clients = [alpha, beta].map((id, i) => ({
    id, name: i === 0 ? "Alpha customer" : "Beta customer",
    uuid: `${i + 1}2345678-1234-4234-8234-123456789abc`, representative_id: "rep-a",
    plan_kind: "volume", ip_count: 0, volume_bytes: 1073741824, duration_days: 30,
    charged_volume_bytes: 1073741824, charged_unlimited_units: 0,
    deployment_state: "pending_node_apply", archived: false, expires_unix: 1900000000,
  }));
  const heads = new Map<string, Head>(clients.map(c => [c.id, {
    generation: options.active ? 3 : 0, state: options.active ? "active" : "absent",
  }]));
  const events = new Map<string, { fingerprint: string; receipt: Record<string, unknown> }>();
  let lost = false;
  let releaseReveal = () => {};
  const revealGate = options.delayReveal ? new Promise<void>(resolve => { releaseReveal = resolve; }) : Promise.resolve();
  await page.addInitScript(() => {
    const writes: Array<[string, string]> = [];
    Object.defineProperty(window, "__subscriptionStorageWrites", { value: writes });
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      writes.push([String(key), String(value)]);
      return original.call(this, key, value);
    };
  });
  await page.route("**/api/v1/**", async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (path.startsWith("/api/v1/inbound-subscriptions/")) {
      consumers.push(request.url());
      return route.fulfill({ status: 500, body: "consumer_fetch_not_authorized_by_test" });
    }
    if (path === "/api/v1/session/status") return route.fulfill({ json: {
      authenticated: true, user, csrf_token: csrf, expires_unix: 1900000000,
    } });
    if (path === "/api/v1/fleet") return route.fulfill({ json: {
      as_of: new Date().toISOString(), total_nodes: 0, online_nodes: 0, offline_nodes: 0,
      nodes: [], metric_scope: "default_route_interface_not_billable", physical_rates_complete: false,
      interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0,
    } });
    if (path === "/api/v1/workspace/me") return route.fulfill({ json: {
      user, data_plane: "not_activated", balance: { id: "rep-a", display_name: "QA Rep A", kind: "representative",
        enabled: true, volume_credit_bytes: 10737418240, unlimited_credit_units: 100 },
    } });
    if (path === "/api/v1/workspace/clients") {
      const params = url.searchParams;
      const q = params.get("q") ?? "", limit = Number(params.get("limit") ?? 50), offset = Number(params.get("offset") ?? 0);
      const state = params.get("state") ?? "all", plan = params.get("plan") ?? "all", sort = params.get("sort") ?? "newest";
      const rows = clients.filter(c => !q || c.id.includes(q) || c.name.toLowerCase().includes(q.toLowerCase()));
      return route.fulfill({ json: { clients: rows.slice(offset, offset + limit), total: rows.length,
        limit, offset, q, state, plan, sort, has_more: offset + limit < rows.length, representative_id: "rep-a" } });
    }
    if (path === "/api/v1/workspace/client-operations") return route.fulfill({ json: { operations: [] } });
    const subscription = path.match(/^\/api\/v1\/workspace\/clients\/([^/]+)\/inbound-subscription(?:\/(issue|rotate|revoke|reveal))?$/);
    if (subscription) {
      const [, id, action] = subscription, head = heads.get(id);
      const body = request.method() === "POST" ? request.postDataJSON() as Record<string, unknown> : {};
      calls.push({ path, method: request.method(), body, csrf: request.headers()["x-csrf-token"] });
      if (!head) return route.fulfill({ status: 404, json: { error: "client_not_found" } });
      if (options.denied) return route.fulfill({ status: 403, json: { error: "scope_denied" } });
      if (request.method() === "GET" && !action) {
        const value: Record<string, unknown> = { enabled: options.enabled !== false, client_id: id };
        if (options.enabled !== false) Object.assign(value, head, { server_time_unix_milli: Date.now(), available: true, route_count: 2, formats: ["uri", "json", "xray", "sing-box", "wireguard"], expires_unix_nano: (Date.now() + 15000) * 1e6, path_verified: false, blockers: [] });
        return route.fulfill({ json: options.metadata ? options.metadata(value) : value });
      }
      if (options.enabled === false || options.manage === false || request.method() !== "POST" || request.headers()["x-csrf-token"] !== csrf) {
        return route.fulfill({ status: 403, json: { error: "write_denied" } });
      }
      if (action === "reveal") {
        if (head.state !== "active" || body.expected_generation !== head.generation) return route.fulfill({ status: 409, json: { error: "generation_changed" } });
        const token = tokenFor(id, head.generation);
        const value = { client_id: id, generation: head.generation, token, path: "/api/v1/inbound-subscriptions/" + token };
        await revealGate;
        // Switching client aborts the request; a late test response is benign.
        await route.fulfill({ json: options.secret ? options.secret(value) : value }).catch(() => {});
        return;
      }
      if (options.mutationStatus) return route.fulfill({ status: options.mutationStatus, json: { error: "operation_rejected" } });
      if (!action || !/^managed-sub-[a-z0-9-]{20,80}$/.test(String(body.event_id))) return route.fulfill({ status: 400, json: { error: "invalid_operation" } });
      const fingerprint = JSON.stringify({ id, action, body }), previous = events.get(String(body.event_id));
      if (previous) {
        if (previous.fingerprint !== fingerprint) return route.fulfill({ status: 409, json: { error: "event_conflict" } });
        return route.fulfill({ json: { ...previous.receipt, replayed: true, superseded: head.generation !== previous.receipt.generation } });
      }
      if (body.expected_generation !== head.generation || action === "issue" && head.state !== "absent" || action === "revoke" && head.state !== "active") {
        return route.fulfill({ status: 409, json: { error: "generation_changed" } });
      }
      head.generation++;
      head.state = action === "revoke" ? "revoked" : "active";
      const receipt = { event_id: body.event_id, client_id: id, generation: head.generation, state: head.state, replayed: false, superseded: false };
      events.set(String(body.event_id), { fingerprint, receipt });
      if (options.loseFirstMutation && !lost) {
        lost = true;
        return route.abort("failed");
      }
      return route.fulfill({ json: options.receipt ? options.receipt(receipt) : receipt });
    }
    if (path.endsWith("/edits")) return route.fulfill({ json: { edits: [], limit: 20, offset: 0, audit: "append_only" } });
    const client = clients.find(c => path === "/api/v1/workspace/clients/" + c.id);
    if (client) return route.fulfill({ json: client });
    return route.fulfill({ status: 404, json: { error: "not_found" } });
  });
  return { calls, consumers, heads, releaseReveal, user };
}

const panel = (page: Page) => page.getByRole("region", { name: "Client inbound subscription" });
async function selectClient(page: Page, id = alpha, persian = false): Promise<Locator> {
  await page.getByRole("region", { name: "Client directory" }).locator(".clients-explorer-row")
    .filter({ hasText: id }).getByRole("button", { name: persian ? "مدیریت" : "Manage", exact: true }).click();
  const section = panel(page);
  await expect(section).toBeVisible();
  return section;
}
async function storageContents(page: Page) {
  return page.evaluate(() => JSON.stringify({
    local: Object.entries(localStorage), session: Object.entries(sessionStorage),
    writes: (window as unknown as { __subscriptionStorageWrites: Array<[string, string]> }).__subscriptionStorageWrites,
  }));
}
async function expectNoStoredSecret(page: Page, tokens: string[]) {
  const stored = await storageContents(page);
  for (const token of tokens) expect(stored).not.toContain(token);
  expect(stored).not.toContain("/api/v1/inbound-subscriptions/");
  expect(stored).not.toContain(csrf);
}

test("managed subscription issues, reveals, rotates and revokes explicitly without storing or fetching credentials", async ({ page }) => {
  const f = await fixture(page); page.on("dialog", dialog => dialog.accept());
  await page.goto("/clients"); const section = await selectClient(page);
  await expect(section.getByText(/No link issued/)).toBeVisible();
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(0);
  await section.getByRole("button", { name: "Create subscription link", exact: true }).click();
  await expect(section.getByText(/Link active/)).toBeVisible();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  const issue = f.calls.find(c => c.path.endsWith("/issue"))!;
  expect(issue.body).toEqual({ event_id: expect.stringMatching(/^managed-sub-[a-z0-9-]{36}$/), expected_generation: 0 });
  expect(issue.csrf).toBe(csrf);
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveValue(new RegExp(tokenFor(alpha, 1) + "\\?format=uri$"));
  await section.getByRole("combobox", { name: "Format", exact: true }).selectOption("wireguard");
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveValue(/\?format=wireguard$/);
  await expect(section.getByRole("link", { name: "Download configuration" })).toHaveAttribute("href", `/api/v1/workspace/clients/${alpha}/inbound-subscription/export?format=wireguard`);
  await expect(section.getByText("Engine readiness is recorded; the complete connection path has not been tested.")).toBeVisible();
  await section.getByRole("button", { name: "Replace link", exact: true }).click();
  await expect.poll(() => f.heads.get(alpha)?.generation).toBe(2);
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveValue(new RegExp(tokenFor(alpha, 2)));
  await section.getByRole("button", { name: "Revoke link", exact: true }).click();
  await expect(section.getByText(/Link revoked/)).toBeVisible();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  expect(f.heads.get(alpha)).toEqual({ generation: 3, state: "revoked" });
  await expectNoStoredSecret(page, [tokenFor(alpha, 1), tokenFor(alpha, 2)]);
  expect(f.consumers).toHaveLength(0);
});

test("managed subscription hides a revealed link when polled generation changes and rejects a late reveal after revocation", async ({ page }) => {
  const options: Options = { active: true, delayReveal: true }, f = await fixture(page, options);
  await page.goto("/clients"); const section = await selectClient(page);
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect.poll(() => f.calls.filter(c => c.path.endsWith("/reveal")).length).toBe(1);
  f.heads.set(alpha, { generation: 4, state: "revoked" });
  // Metadata polling continues during an outstanding secret request.
  await expect(section.getByText(/Link revoked/)).toBeVisible({ timeout: 8000 });
  f.releaseReveal();
  await expect(section.getByRole("button", { name: "Replace link", exact: true })).toBeEnabled();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  f.heads.set(alpha, { generation: 5, state: "active" });
  await section.getByRole("button", { name: "Refresh", exact: true }).click();
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveValue(new RegExp(tokenFor(alpha, 5)));
  f.heads.set(alpha, { generation: 6, state: "active" });
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0, { timeout: 8000 });
  await expectNoStoredSecret(page, [tokenFor(alpha, 3), tokenFor(alpha, 5)]);
});

test("managed subscription clears secrets on client changes and revoked manage permission", async ({ page }) => {
  const f = await fixture(page, { active: true });
  await page.goto("/clients"); let section = await selectClient(page);
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toBeVisible();
  section = await selectClient(page, beta);
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toBeVisible();
  f.user.permissions.manage_clients = false;
  await page.evaluate(() => window.dispatchEvent(new StorageEvent("storage", { key: "dark-pro-session-change", newValue: "permissions-updated" })));
  await expect(page.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  section = await selectClient(page, beta);
  await expect(section.getByText(/Link active/)).toBeVisible();
  await expect(section.getByRole("button", { name: /Show link|Replace link|Revoke link/ })).toHaveCount(0);
  await expect(section.getByRole("link", { name: "Download configuration" })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3), tokenFor(beta, 3)]);
});

test("managed subscription workspace refresh invalidates an outstanding reveal", async ({ page }) => {
  const f = await fixture(page, { active: true, delayReveal: true });
  await page.goto("/clients"); const section = await selectClient(page);
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect.poll(() => f.calls.filter(call => call.path.endsWith("/reveal")).length).toBe(1);
  const reads = f.calls.filter(call => call.method === "GET").length;
  await page.getByRole("region", { name: "Business workspace" }).locator(".business-topbar-actions").getByRole("button", { name: "Refresh", exact: true }).click();
  await expect.poll(() => f.calls.filter(call => call.method === "GET").length).toBeGreaterThan(reads);
  f.releaseReveal();
  await expect(section.getByRole("button", { name: "Show link", exact: true })).toBeEnabled();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3)]);
});

test("managed subscription retains the exact unknown operation across reload without automatic replay", async ({ page }) => {
  const f = await fixture(page, { loseFirstMutation: true });
  await page.goto("/clients"); let section = await selectClient(page);
  await section.getByRole("button", { name: "Create subscription link", exact: true }).click();
  await expect(section.getByRole("button", { name: "Retry exact operation", exact: true })).toBeEnabled();
  const first = f.calls.find(c => c.path.endsWith("/issue"))!;
  await expect(section.getByRole("button", { name: "Discard rejected request" })).toHaveCount(0);
  await page.reload(); section = await selectClient(page);
  await expect(section.getByRole("button", { name: "Retry exact operation", exact: true })).toBeEnabled();
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(1);
  await section.getByRole("button", { name: "Retry exact operation", exact: true }).click();
  await expect(section.getByText("Operation recorded.", { exact: true })).toBeVisible();
  expect(f.calls.filter(c => c.path.endsWith("/issue"))[1].body).toEqual(first.body);
  expect(f.heads.get(alpha)?.generation).toBe(1);
  await expectNoStoredSecret(page, [tokenFor(alpha, 1)]);
});

for (const status of [400, 403, 409, 422]) test(`managed subscription lets a definitive HTTP ${status} rejection be discarded`, async ({ page }) => {
  const options: Options = { mutationStatus: status }, f = await fixture(page, options);
  await page.goto("/clients"); const section = await selectClient(page);
  await section.getByRole("button", { name: "Create subscription link", exact: true }).click();
  await expect(section.getByRole("button", { name: "Discard rejected request", exact: true })).toBeEnabled();
  await expect(section.getByRole("button", { name: "Retry exact operation", exact: true })).toHaveCount(0);
  options.mutationStatus = undefined;
  await section.getByRole("button", { name: "Discard rejected request", exact: true }).click();
  await section.getByRole("button", { name: "Create subscription link", exact: true }).click();
  await expect(section.getByText(/Link active/)).toBeVisible();
  const calls = f.calls.filter(c => c.path.endsWith("/issue"));
  expect(calls).toHaveLength(2); expect(calls[0].body.event_id).not.toBe(calls[1].body.event_id);
  expect(f.heads.get(alpha)?.generation).toBe(1);
});

test("managed subscription treats malformed success as unknown and stores only an opaque retry", async ({ page }) => {
  const f = await fixture(page, { receipt: value => ({ ...value, client_id: beta }) });
  await page.goto("/clients"); const section = await selectClient(page);
  await section.getByRole("button", { name: "Create subscription link", exact: true }).click();
  await expect(section.getByRole("button", { name: "Retry exact operation", exact: true })).toBeEnabled();
  await expect(section.getByRole("button", { name: "Discard rejected request", exact: true })).toHaveCount(0);
  const stored = await page.evaluate(() => JSON.parse(sessionStorage.getItem("darkpro:managed-sub:rep-a:sub-alpha")!));
  expect(Object.keys(stored).sort()).toEqual(["action", "event_id", "expected_generation"]);
  await section.getByRole("button", { name: "Retry exact operation", exact: true }).click();
  await expect(section.getByText("Operation recorded.", { exact: true })).toBeVisible();
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(2);
  await expectNoStoredSecret(page, [tokenFor(alpha, 1)]);
});

for (const invalid of [
  { name: "cross-client scope", change: (v: Record<string, unknown>) => ({ ...v, client_id: beta }) },
  { name: "secret in nested metadata", change: (v: Record<string, unknown>) => ({ ...v, extra: { token: "must-not-be-public" } }) },
  { name: "negative route count", change: (v: Record<string, unknown>) => ({ ...v, route_count: -1 }) },
  { name: "unknown export format", change: (v: Record<string, unknown>) => ({ ...v, formats: ["executable"] }) },
]) test(`managed subscription rejects ${invalid.name}`, async ({ page }) => {
  const f = await fixture(page, { active: true, metadata: invalid.change });
  await page.goto("/clients"); const section = await selectClient(page);
  await expect(section.getByText("Subscription state could not be verified. Please refresh.", { exact: true })).toBeVisible();
  await expect(section.getByRole("button", { name: /Show link|Create subscription link|Replace link|Revoke link/ })).toHaveCount(0);
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(0);
});

test("managed subscription clears secrets on denial, hidden tab and one-minute timeout", async ({ page }) => {
  await page.clock.install(); const options: Options = { active: true }, f = await fixture(page, options);
  await page.goto("/clients"); const section = await selectClient(page);
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toBeVisible();
  await page.evaluate(() => { Object.defineProperty(document, "hidden", { value: true, configurable: true }); document.dispatchEvent(new Event("visibilitychange")); });
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  await page.evaluate(() => { Object.defineProperty(document, "hidden", { value: false, configurable: true }); document.dispatchEvent(new Event("visibilitychange")); });
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toBeVisible();
  await page.clock.fastForward(60001);
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  await section.getByRole("button", { name: "Show link", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toBeVisible();
  options.denied = true; await page.clock.fastForward(5001);
  await expect(section.getByText("Subscription state could not be verified. Please refresh.", { exact: true })).toBeVisible();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("region", { name: "Client LAB subscription" })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3)]); expect(f.consumers).toHaveLength(0);
});

test("managed subscription expires delivery locally and fits Persian light mode at 320px", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 760 }); await page.clock.install();
  const expires = Date.now() + 4000;
  const f = await fixture(page, { active: true, metadata: v => ({ ...v, expires_unix_nano: expires * 1e6 }) });
  await page.addInitScript(() => { localStorage.setItem("dark-pro-lang", "fa"); localStorage.setItem("dark-pro-theme", "light"); });
  await page.goto("/clients"); const section = await selectClient(page, alpha, true);
  await expect(section.getByRole("heading", { name: "اشتراک اینباند", exact: true })).toBeVisible();
  await section.getByRole("button", { name: "نمایش لینک", exact: true }).click();
  await expect(section.getByLabel("Inbound subscription URL", { exact: true })).toHaveValue(new RegExp(tokenFor(alpha, 3)));
  await expect(section.getByRole("link", { name: "دریافت تنظیمات" })).toBeVisible();
  expect(await section.evaluate(el => el.scrollWidth <= el.clientWidth + 2)).toBe(true);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
  await page.clock.fastForward(4001);
  await expect(section.getByRole("link", { name: "دریافت تنظیمات" })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3)]); expect(f.consumers).toHaveLength(0);
});
