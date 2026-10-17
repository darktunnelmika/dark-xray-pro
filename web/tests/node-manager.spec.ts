import { test, expect, type Page } from "@playwright/test";

const pairCode = "c".repeat(64);
const owner = { id: "owner", username: "new-pro-owner", role: "owner", representative_id: "owner" };
const representative = { id: "rep-01", username: "rep-01", role: "representative", representative_id: "rep-01" };

async function setup(page: Page, role: "owner" | "representative" = "owner") {
  let signed = false;
  let invites = 0;
  let cancellations = 0;
  const paths: string[] = [];
  const user = role === "owner" ? owner : representative;
  const session = { authenticated: true, user, csrf_token: "a".repeat(64), expires_unix: 1900000000 };
  await page.route("**/api/v1/**", async route => {
    const req = route.request();const path = new URL(req.url()).pathname;
    paths.push(req.method() + " " + path);
    if (path.endsWith("/session/status")) return route.fulfill({ json: signed ? session : { enabled: true, authenticated: false } });
    if (path.endsWith("/session/login")) { signed = true; return route.fulfill({ json: session }); }
    if (!signed) return route.fulfill({ status: 401, json: { error: "session_required" } });
    if (path.endsWith("/fleet")) return route.fulfill({ json: {
      as_of: new Date().toISOString(), nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0,
      interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false,
      metric_scope: "default_route_interface_not_billable",
    } });
    if (req.method() === "GET" && path === "/api/v1/workspace/nodes") return route.fulfill({
      json: { mode: "new_pro_lab_only", nodes: [] },
    });
    if (req.method() === "POST" && path === "/api/v1/workspace/nodes/invites") {
      expect(req.headers()["x-csrf-token"]).toBe(session.csrf_token);
      expect(req.headers().origin).toBe("http://127.0.0.1:" + new URL(req.url()).port);
      const data = req.postDataJSON() as { node_id: string; name: string };
      expect(data.node_id).toBe("qa-new-node");
      invites += 1;
      return route.fulfill({ status: 201, json: {
        node_id: data.node_id, name: data.name, pair_code: pairCode,
        expires_at: new Date(Date.now() + 10 * 60_000).toISOString(),
      } });
    }
    if (req.method() === "POST" && path === "/api/v1/workspace/nodes/invites/qa-new-node/cancel") {
      expect(req.headers()["x-csrf-token"]).toBe(session.csrf_token);
      cancellations += 1;
      return route.fulfill({ json: { cancelled: true } });
    }
    if (path.startsWith("/api/v1/workspace/")) return route.fulfill({ json: {
      clients: [], groups: [], events: [], operations: [], balance: {},
    } });
    return route.fulfill({ status: 403, json: { error: "not_allowed" } });
  });
  return { paths, invites: () => invites, cancellations: () => cancellations };
}
async function login(page: Page) {
  await page.getByLabel("Username", { exact: true }).fill("new-pro-owner");
  await page.getByLabel("Password", { exact: true }).fill("test-secret-not-persisted");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
}

test("owner creates and revokes a one-use pairing ticket without storing it in browser", async ({ page }) => {
  const probe = await setup(page);
  await page.goto("/nodes");
  await login(page);
  await expect(page.getByRole("heading", { name: "Add a new PRO Node" })).toBeVisible();
  await page.getByLabel("New Node ID").fill("qa-new-node");
  await page.getByLabel("Display name").fill("QA node");
  await page.getByRole("button", { name: "Generate one-time Pair Code" }).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  expect(probe.invites()).toBe(1);
  const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }));
  expect(storage).not.toContain(pairCode);
  expect(storage).not.toContain("test-secret-not-persisted");
  await page.getByRole("button", { name: "Cancel unused code" }).click();
  await expect(page.getByText("Pending Pair Code revoked. No Node or client was changed.", { exact: true })).toBeVisible();
  await expect(page.locator(".pro-node-ticket-code")).toHaveCount(0);
  expect(probe.cancellations()).toBe(1);
  await page.getByRole("button", { name: "Generate one-time Pair Code" }).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  await page.reload();
  await expect(page.getByRole("heading", { name: "Add a new PRO Node" })).toBeVisible();
  await expect(page.locator(".pro-node-ticket-code")).toHaveCount(0);
  await expect(page.getByText(/Pending Pair Code revoked/)).toHaveCount(0);
});

test("representative cannot see or request Node management", async ({ page }) => {
  const probe = await setup(page, "representative");
  await page.goto("/nodes");
  await login(page);
  await expect(page).toHaveURL(/\/clients$/);
  await expect(page.locator(".pro-node-manager")).toHaveCount(0);
  expect(probe.paths.some(s => s.includes("/workspace/nodes"))).toBe(false);
});

test("Node enrollment form fits 320px Persian and does not expose fake live status", async ({ page }) => {
  const probe = await setup(page);
  await page.setViewportSize({ width: 320, height: 760 });
  await page.goto("/nodes");
  await login(page);
  await page.getByRole("button", { name: "Switch language" }).click();
  await expect(page.getByRole("heading", { name: "افزودن نود جدید PRO" })).toBeVisible();
  await expect(page.getByText("هنوز نود تازه‌ای Pair نشده است.")).toBeVisible();
  await page.getByLabel("شناسه نود جدید").fill("qa-new-node");
  await page.getByLabel("نام نمایشی").fill("آزمایشی");
  await page.getByRole("button", { name: "ساخت کد اتصال یک‌بارمصرف" }).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
  expect(probe.invites()).toBe(1);
});
