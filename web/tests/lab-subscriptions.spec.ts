import { expect, test, type Locator, type Page } from "@playwright/test";

const csrf = "a".repeat(64);
const alpha = "sub-alpha", beta = "sub-beta";
const tokenFor = (id: string, generation: number) => Buffer.alloc(32, (id === alpha ? 16 : 64) + generation).toString("base64url");
type TokenState = "absent" | "active" | "revoked";
type Head = { generation: number; state: TokenState };
type Call = { path: string; method: string; body: Record<string, unknown>; csrf: string | undefined };
type Options = {
  enabled?: boolean; denied?: boolean; manage?: boolean; active?: boolean;
  loseFirstMutation?: boolean; delayReveal?: boolean;
  metadata?: (body: Record<string, unknown>) => Record<string, unknown>;
  secret?: (body: Record<string, unknown>) => Record<string, unknown>;
};

function snapshotMetadata(value: Record<string, unknown>, changes: Record<string, unknown> = {}) {
  const created = Date.now() * 1e6;
  return { ...value, snapshot: {
    id: "b".repeat(32), event_id: "c".repeat(32), client_id: value.client_id,
    handover_id: "1".repeat(32), round_id: "2".repeat(32), node_id: "lab-target",
    installation_id: "3".repeat(32), revision: 1, created_unix_nano: created,
    expires_unix_nano: created + 8e9, route_count: 2, current: true, ...changes,
  } };
}

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
    if (path.startsWith("/api/v1/subscriptions/")) {
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
    const subscription = path.match(/^\/api\/v1\/workspace\/clients\/([^/]+)\/subscription(?:\/(issue|rotate|revoke|reveal))?$/);
    if (subscription) {
      const [, id, action] = subscription, head = heads.get(id);
      const body = request.method() === "POST" ? request.postDataJSON() as Record<string, unknown> : {};
      calls.push({ path, method: request.method(), body, csrf: request.headers()["x-csrf-token"] });
      if (!head) return route.fulfill({ status: 404, json: { error: "client_not_found" } });
      if (options.denied) return route.fulfill({ status: 403, json: { error: "scope_denied" } });
      if (request.method() === "GET" && !action) {
        const value: Record<string, unknown> = { enabled: options.enabled !== false, scope: "lab_single_handover", client_id: id };
        if (options.enabled !== false) Object.assign(value, head, { server_time_unix_milli: Date.now() });
        return route.fulfill({ json: options.metadata ? options.metadata(value) : value });
      }
      if (options.enabled === false || options.manage === false || request.method() !== "POST" || request.headers()["x-csrf-token"] !== csrf) {
        return route.fulfill({ status: 403, json: { error: "write_denied" } });
      }
      if (action === "reveal") {
        if (head.state !== "active" || body.expected_generation !== head.generation) return route.fulfill({ status: 409, json: { error: "generation_changed" } });
        const token = tokenFor(id, head.generation);
        const value = { client_id: id, generation: head.generation, token, path: "/api/v1/subscriptions/" + token };
        await revealGate;
        // Switching client aborts the request; a late test response is benign.
        await route.fulfill({ json: options.secret ? options.secret(value) : value }).catch(() => {});
        return;
      }
      if (!action || !/^[a-f0-9]{32}$/.test(String(body.event_id))) return route.fulfill({ status: 400, json: { error: "invalid_operation" } });
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
      return route.fulfill({ json: receipt });
    }
    if (path.endsWith("/edits")) return route.fulfill({ json: { edits: [], limit: 20, offset: 0, audit: "append_only" } });
    const client = clients.find(c => path === "/api/v1/workspace/clients/" + c.id);
    if (client) return route.fulfill({ json: client });
    return route.fulfill({ status: 404, json: { error: "not_found" } });
  });
  return { calls, consumers, heads, releaseReveal };
}

const panel = (page: Page) => page.getByRole("region", { name: "Client LAB subscription" });
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
  expect(stored).not.toContain("/api/v1/subscriptions/");
  expect(stored).not.toContain(csrf);
}

test("N18 issues and reveals only explicitly, formats URLs, and never fetches or stores the bearer URL", async ({ page }) => {
  const f = await fixture(page);
  await page.goto("/clients");
  const section = await selectClient(page);
  await expect(section.getByText("No subscription token issued", { exact: true })).toBeVisible();
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(0);
  expect(f.consumers).toHaveLength(0);
  await section.getByRole("button", { name: "Issue subscription token", exact: true }).click();
  await expect(section.getByText("Subscription token active", { exact: true })).toBeVisible();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  expect(f.calls.filter(c => c.path.endsWith("/reveal"))).toHaveLength(0);
  const issue = f.calls.find(c => c.path.endsWith("/issue"))!;
  expect(issue.body).toEqual({ event_id: expect.stringMatching(/^[a-f0-9]{32}$/), expected_generation: 0 });
  expect(issue.csrf).toBe(csrf);
  await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
  const input = section.getByLabel("Subscription URL", { exact: true }), token = tokenFor(alpha, 1);
  const origin = new URL(page.url()).origin;
  await expect(input).toHaveValue(origin + "/api/v1/subscriptions/" + token);
  await expect(input).toHaveAttribute("readonly", "");
  await expect(input).toHaveAttribute("dir", "ltr");
  await section.getByRole("combobox", { name: "Subscription format", exact: true }).selectOption("raw");
  await expect(input).toHaveValue(origin + "/api/v1/subscriptions/" + token + "?format=raw");
  await section.getByRole("combobox", { name: "Subscription format", exact: true }).selectOption("base64");
  await expect(input).toHaveValue(origin + "/api/v1/subscriptions/" + token);
  expect(f.calls.filter(c => c.path.endsWith("/reveal"))).toHaveLength(1);
  expect(f.calls.find(c => c.path.endsWith("/reveal"))?.body).toEqual({ expected_generation: 1 });
  await expectNoStoredSecret(page, [token]);
  expect(f.consumers).toHaveLength(0);
  await section.getByRole("button", { name: "Hide URL", exact: true }).click();
  await expect(input).toHaveCount(0);
  await page.reload();
  await selectClient(page);
  await expect(panel(page).getByText("Subscription token active", { exact: true })).toBeVisible();
  await expect(panel(page).getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  expect(f.calls.filter(c => c.path.endsWith("/reveal"))).toHaveLength(1);
  expect(f.consumers).toHaveLength(0);
});

test("N18 hides secrets on client switch, hidden tab, and component unmount", async ({ page }) => {
  const f = await fixture(page, { active: true });
  await page.goto("/clients");
  let section = await selectClient(page);
  await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveValue(new RegExp(tokenFor(alpha, 3)));
  section = await selectClient(page, beta);
  await expect(section.getByText("Subscription token active", { exact: true })).toBeVisible();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  expect(f.calls.filter(c => c.path.endsWith("/reveal"))).toHaveLength(1);
  await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveValue(new RegExp(tokenFor(beta, 3)));
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  await page.evaluate(() => { Object.defineProperty(document, "hidden", { configurable: true, get: () => false }); });
  await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toBeVisible();
  await page.getByRole("region", { name: "Client directory" }).getByRole("searchbox").fill("not-present");
  await expect(panel(page)).toHaveCount(0);
  await expect(page.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3), tokenFor(beta, 3)]);
  expect(f.consumers).toHaveLength(0);
});

test("N18 ignores a delayed reveal response after switching its client", async ({ page }) => {
  const f = await fixture(page, { active: true, delayReveal: true });
  await page.goto("/clients");
  let section = await selectClient(page);
  await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
  await expect.poll(() => f.calls.filter(c => c.path.endsWith("/reveal")).length).toBe(1);
  section = await selectClient(page, beta);
  f.releaseReveal();
  await expect(section.getByRole("button", { name: "Show subscription URL", exact: true })).toBeEnabled();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3)]);
  expect(f.consumers).toHaveLength(0);
});

test("N18 invalidates an in-flight reveal when the workspace refreshes metadata", async ({ page }) => {
  const f = await fixture(page, { active: true, delayReveal: true });
  await page.goto("/clients");
  const section = await selectClient(page);
  await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
  await expect.poll(() => f.calls.filter(c => c.path.endsWith("/reveal")).length).toBe(1);
  const before = f.calls.filter(c => c.method === "GET").length;
  await page.getByRole("region", { name: "Business workspace" }).getByRole("button", { name: "Refresh", exact: true }).click();
  await expect.poll(() => f.calls.filter(c => c.method === "GET").length).toBeGreaterThan(before);
  await expect(section.getByText("Subscription token active", { exact: true })).toBeVisible();
  f.releaseReveal();
  await expect(section.getByRole("button", { name: "Show subscription URL", exact: true })).toBeEnabled();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3)]);
  expect(f.consumers).toHaveLength(0);
});

test("N18 keeps an old reveal hidden after leaving and returning to the tab before its reply", async ({ page }) => {
  const f = await fixture(page, { active: true, delayReveal: true });
  await page.goto("/clients");
  const section = await selectClient(page);
  await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
  await expect.poll(() => f.calls.filter(c => c.path.endsWith("/reveal")).length).toBe(1);
  await page.evaluate(() => {
    Object.defineProperty(document, "hidden", { configurable: true, get: () => true });
    document.dispatchEvent(new Event("visibilitychange"));
    Object.defineProperty(document, "hidden", { configurable: true, get: () => false });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  f.releaseReveal();
  await expect(section.getByRole("button", { name: "Show subscription URL", exact: true })).toBeEnabled();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3)]);
  expect(f.consumers).toHaveLength(0);
});

test("N18 removes an explicitly revealed secret after one minute", async ({ page }) => {
  await page.clock.install({ time: new Date() });
  const f = await fixture(page, { active: true });
  await page.goto("/clients");
  const section = await selectClient(page);
  await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toBeVisible();
  await page.clock.fastForward(60_001);
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3)]);
  expect(f.consumers).toHaveLength(0);
});

test("N18 retries a lost mutation reply with the exact event and generation across reload", async ({ page }) => {
  const f = await fixture(page, { loseFirstMutation: true });
  await page.goto("/clients");
  let section = await selectClient(page);
  await section.getByRole("button", { name: "Issue subscription token", exact: true }).click();
  await expect(section.getByRole("button", { name: "Retry exact token operation", exact: true })).toBeEnabled();
  const first = f.calls.find(c => c.path.endsWith("/issue"))!;
  expect(first.body.expected_generation).toBe(0);
  await expectNoStoredSecret(page, [tokenFor(alpha, 1)]);
  await page.reload();
  section = await selectClient(page);
  await expect(section.getByText("Subscription token active", { exact: true })).toBeVisible();
  await expect(section.getByRole("button", { name: "Retry exact token operation", exact: true })).toBeEnabled();
  await expect(section.getByRole("button", { name: "Show subscription URL", exact: true })).toBeDisabled();
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(1);
  await section.getByRole("button", { name: "Retry exact token operation", exact: true }).click();
  await expect(section.getByText("Token operation recorded.", { exact: true })).toBeVisible();
  const retries = f.calls.filter(c => c.path.endsWith("/issue"));
  expect(retries).toHaveLength(2);
  expect(retries[1].body).toEqual(first.body);
  expect(retries[1].csrf).toBe(csrf);
  await expect(section.getByRole("button", { name: "Retry exact token operation", exact: true })).toHaveCount(0);
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  expect(f.heads.get(alpha)).toEqual({ generation: 1, state: "active" });
  expect(f.consumers).toHaveLength(0);
});

test("N18 sends no mutation when the non-secret exact retry cannot be retained", async ({ page }) => {
  const f = await fixture(page);
  await page.goto("/clients");
  const section = await selectClient(page);
  await expect(section.getByRole("button", { name: "Issue subscription token", exact: true })).toBeEnabled();
  await page.evaluate(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("dark-pro-subscription-attempt:")) throw new Error("storage_unavailable");
      return original.call(this, key, value);
    };
  });
  await section.getByRole("button", { name: "Issue subscription token", exact: true }).click();
  await expect(section.getByText("Subscription state could not be verified. Refresh before another action.", { exact: true })).toBeVisible();
  await expect(section.getByRole("button", { name: "Issue subscription token", exact: true })).toBeDisabled();
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(0);
  expect(f.consumers).toHaveLength(0);
});

for (const malformed of [
  { name: "another client", change: (v: Record<string, unknown>) => ({ ...v, client_id: beta }) },
  { name: "another generation", change: (v: Record<string, unknown>) => ({ ...v, generation: Number(v.generation) + 1 }) },
  { name: "an external URL", change: (v: Record<string, unknown>) => ({ ...v, path: "https://example.org/" + v.token }) },
  { name: "another token path", change: (v: Record<string, unknown>) => ({ ...v, path: "/api/v1/subscriptions/" + tokenFor(beta, 3) }) },
  { name: "a path with query injection", change: (v: Record<string, unknown>) => ({ ...v, path: v.path + "?format=raw&next=other" }) },
  { name: "noncanonical base64", change: (v: Record<string, unknown>) => {
    const token = "A".repeat(42) + "B";
    return { ...v, token, path: "/api/v1/subscriptions/" + token };
  } },
  { name: "a padded token", change: (v: Record<string, unknown>) => ({ ...v, token: v.token + "=", path: v.path + "=" }) },
]) {
  test(`N18 refuses a reveal response containing ${malformed.name}`, async ({ page }) => {
    const f = await fixture(page, { active: true, secret: malformed.change });
    await page.goto("/clients");
    const section = await selectClient(page);
    await section.getByRole("button", { name: "Show subscription URL", exact: true }).click();
    await expect(section.getByText("Subscription state could not be verified. Refresh before another action.", { exact: true })).toBeVisible();
    await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
    await expectNoStoredSecret(page, [tokenFor(alpha, 3), tokenFor(beta, 3)]);
    expect(f.calls.filter(c => c.path.endsWith("/reveal"))).toHaveLength(1);
    expect(f.consumers).toHaveLength(0);
  });
}

for (const unavailable of [
  { name: "default-off discovery", options: { enabled: false }, message: "LAB subscription delivery is disabled on this HUB." },
  { name: "403 metadata", options: { denied: true }, message: "Access to this client's subscription is no longer authorized." },
  { name: "cross-client metadata", options: { metadata: (v: Record<string, unknown>) => ({ ...v, client_id: beta }) }, message: "Subscription state could not be verified. Refresh before another action." },
  { name: "inconsistent generation", options: { active: true, metadata: (v: Record<string, unknown>) => ({ ...v, generation: 0 }) }, message: "Subscription state could not be verified. Refresh before another action." },
  { name: "out-of-range snapshot timestamps", options: { metadata: (v: Record<string, unknown>) => snapshotMetadata(v, { created_unix_nano: 1e25, expires_unix_nano: 1e25 + 4e9 }) }, message: "Subscription state could not be verified. Refresh before another action." },
  { name: "out-of-range server time", options: { metadata: (v: Record<string, unknown>) => ({ ...v, server_time_unix_milli: Number.MAX_SAFE_INTEGER }) }, message: "Subscription state could not be verified. Refresh before another action." },
  { name: "zero snapshot revision", options: { metadata: (v: Record<string, unknown>) => snapshotMetadata(v, { revision: 0 }) }, message: "Subscription state could not be verified. Refresh before another action." },
  { name: "invalid snapshot event ID", options: { metadata: (v: Record<string, unknown>) => snapshotMetadata(v, { event_id: "not-an-event" }) }, message: "Subscription state could not be verified. Refresh before another action." },
]) {
  test(`N18 offers no token controls after ${unavailable.name}`, async ({ page }) => {
    const f = await fixture(page, unavailable.options);
    await page.goto("/clients");
    const section = await selectClient(page);
    await expect(section.getByText(unavailable.message, { exact: true })).toBeVisible();
    await expect(section.getByRole("button", { name: /Issue subscription token|Show subscription URL|Replace subscription token|Revoke subscription token/ })).toHaveCount(0);
    expect(f.calls.filter(c => c.method === "POST")).toHaveLength(0);
    expect(f.consumers).toHaveLength(0);
  });
}

test("N18 accepts snapshot metadata with a distinct server snapshot ID and client event ID", async ({ page }) => {
  const f = await fixture(page, { active: true, metadata: v => snapshotMetadata(v) });
  await page.goto("/clients");
  const section = await selectClient(page);
  await expect(section.getByText("LAB snapshot is within its checked validity window", { exact: true })).toBeVisible();
  await expect(section.getByText("Routes: 2", { exact: true })).toBeVisible();
  await expect(section.getByRole("button", { name: "Show subscription URL", exact: true })).toBeEnabled();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(0);
  expect(f.consumers).toHaveLength(0);
});

test("N19 loaded consumer snapshot identifies its approved mapping without relabeling local snapshots",async({page})=>{
 const f=await fixture(page,{active:true,metadata:v=>snapshotMetadata(v,{consumer_manifest_id:"4".repeat(32),consumer_manifest_revision:2,consumer_manifest_sha:"5".repeat(64)})});
 await page.goto("/clients");const section=await selectClient(page);
 await expect(section.getByText("LAB snapshot is within its checked validity window",{exact:true})).toBeVisible();
 await expect(section.getByText(/This snapshot delivers the consumer routes of one approved mapping/)).toBeVisible();
 await expect(section.getByText(/This stage delivers the verified local LAB routes/)).toHaveCount(0);
 expect(f.calls.filter(c=>c.method==="POST")).toHaveLength(0);expect(f.consumers).toHaveLength(0);
});

for(const [label,ref] of [
 ["missing revision",{consumer_manifest_id:"4".repeat(32),consumer_manifest_sha:"5".repeat(64)}],
 ["missing hash",{consumer_manifest_id:"4".repeat(32),consumer_manifest_revision:2}],
 ["unsafe revision",{consumer_manifest_id:"4".repeat(32),consumer_manifest_revision:Number.MAX_SAFE_INTEGER,consumer_manifest_sha:"5".repeat(64)}],
] as const)test(`N19 loaded snapshot with ${label} cannot be treated as verified`,async({page})=>{
 const f=await fixture(page,{active:true,metadata:v=>snapshotMetadata(v,ref)});await page.goto("/clients");const section=await selectClient(page);
 await expect(section.getByText("Subscription state could not be verified. Refresh before another action.",{exact:true})).toBeVisible();
 await expect(section.getByRole("button",{name:"Show subscription URL",exact:true})).toHaveCount(0);await expect(section.getByText("LAB snapshot is within its checked validity window",{exact:true})).toHaveCount(0);expect(f.consumers).toHaveLength(0);
});

test("N18 view-only representatives can read metadata without secret or mutation controls", async ({ page }) => {
  const f = await fixture(page, { manage: false, active: true });
  await page.goto("/clients");
  const section = await selectClient(page);
  await expect(section.getByText("Subscription token active", { exact: true })).toBeVisible();
  await expect(section.getByRole("button", { name: /Issue subscription token|Show subscription URL|Replace subscription token|Revoke subscription token/ })).toHaveCount(0);
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveCount(0);
  expect(f.calls.filter(c => c.method === "POST")).toHaveLength(0);
  expect(f.consumers).toHaveLength(0);
});

test("N18 Persian subscription controls and the long bearer URL fit 320px", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 760 });
  const f = await fixture(page, { active: true });
  await page.goto("/clients");
  await page.getByRole("button", { name: "Switch language" }).click();
  const section = await selectClient(page, alpha, true);
  await expect(section.getByRole("heading", { name: "اشتراک آزمایشی LAB", exact: true })).toBeVisible();
  await section.getByRole("button", { name: "نمایش لینک اشتراک", exact: true }).click();
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveValue(new RegExp(tokenFor(alpha, 3)));
  await section.getByRole("combobox", { name: "فرمت اشتراک", exact: true }).selectOption("raw");
  await expect(section.getByLabel("Subscription URL", { exact: true })).toHaveAttribute("dir", "ltr");
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
  expect(await section.evaluate(el => el.scrollWidth <= el.clientWidth + 2)).toBe(true);
  await expectNoStoredSecret(page, [tokenFor(alpha, 3)]);
  expect(f.consumers).toHaveLength(0);
});
