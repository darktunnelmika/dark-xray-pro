import { expect, test, type Page } from "@playwright/test";

async function fixture(page: Page, options: { unlimited?: boolean; state?: string; crossScope?: boolean; enabled?: boolean } = {}) {
  const user = { id: "rep-refund", username: "refund-qa", role: "representative", representative_id: "rep-refund" };
  const client = { id: "refund-client", representative_id: user.representative_id, name: "Refund client", uuid: "12345678-1234-4234-9234-123456789abc", plan_kind: options.unlimited ? "unlimited" : "volume", ip_count: options.unlimited ? 2 : 0, volume_bytes: 1000, duration_days: 30, charged_volume_bytes: 1000, charged_unlimited_units: options.unlimited ? 2 : 0, expires_unix: 1800000000, deployment_state: "pending_node_apply", archived: false };
  let refundState = options.state ?? "waiting_final_usage";
  const writes: string[] = [];
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user, csrf_token: "a".repeat(64), expires_unix: 1900000000 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { as_of: new Date().toISOString(), total_nodes: 0, online_nodes: 0, offline_nodes: 0, nodes: [], metric_scope: "default_route_interface_not_billable", physical_rates_complete: false } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname;
    if (req.method() !== "GET") { writes.push(path); return route.fulfill({ status: 403, json: { error: "unexpected_mutation" } }); }
    if (path.endsWith("/me")) return route.fulfill({ json: { user, data_plane: "not_activated", balance: { id: user.representative_id, display_name: "Refund Representative", kind: "representative", enabled: true, volume_credit_bytes: 1000, unlimited_credit_units: 8 } } });
    if (path.endsWith("/clients")) return route.fulfill({ json: { clients: [client], representative_id: user.representative_id, total: 1, has_more: false, ...Object.fromEntries(["q", "state", "plan", "sort"].map(k => [k, url.searchParams.get(k)])), limit: Number(url.searchParams.get("limit")), offset: Number(url.searchParams.get("offset")) } });
    if (path.endsWith("/volume-refund")) {
      const completed = refundState === "already_refunded";
      return route.fulfill({ json: {
        request: { client_id: client.id, expected_expires_unix: client.expires_unix, expected_charged_volume_bytes: "1000" },
        representative_id: user.representative_id, generation: "a".repeat(64), trigger: "archived", state: refundState,
        volume_bytes: completed ? "400" : "1000", used_bytes: "400", ...(completed ? { funded_bytes: "1000", refund_bytes: "600" } : {}),
      } });
    }
    if (path.endsWith("/volume-refunds")) {
      if (options.enabled === false) return route.fulfill({ status: 404, json: { error: "not_found" } });
      return route.fulfill({ json: { representative_id: options.crossScope ? "other" : user.representative_id, limit: 25, offset: Number(url.searchParams.get("offset")), refunds: [{ generation: "b".repeat(64), client_id: "deleted-client", representative_id: user.representative_id, trigger: "archived", refunded_bytes: "9007199254740993", used_bytes: "1", volume_before_bytes: "9007199254740994", created_unix: 1790000000 }] } });
    }
    if (path.endsWith("/events")) return route.fulfill({ json: { events: [] } });
    if (path.endsWith("/client-operations")) return route.fulfill({ json: { operations: [] } });
    if (path.endsWith("/" + client.id)) return route.fulfill({ json: client });
    return route.fulfill({ status: 404, json: { error: "not_found" } });
  });
  return { writes, setState: (state: string) => { refundState = state; } };
}
async function openClient(page: Page) {
  await page.goto("/clients");
  await page.getByRole("region", { name: "Client directory" }).locator(".clients-explorer-row").filter({ hasText: "refund-client" }).getByRole("button", { name: "Manage", exact: true }).click();
  return page.getByRole("region", { name: "Client volume return" });
}
test("final usage waiting becomes verified credit without a browser money mutation", async ({ page }) => {
  const f = await fixture(page);
  const region = await openClient(page);
  await expect(region.getByText("Waiting for the final usage report. Credit will be returned automatically after settlement.")).toBeVisible();
  await expect(region.getByText("Returned to representative credit", { exact: false })).toHaveCount(0);
  f.setState("already_refunded");
  await region.getByRole("button", { name: "Refresh volume returns" }).click();
  await expect(region.getByText("The unused volume has been returned to the same representative.")).toBeVisible();
  await expect(region.getByText("600", { exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(0);
});
test("unlimited configuration explicitly retains token accounting", async ({ page }) => {
  const f = await fixture(page, { unlimited: true, state: "not_volume" });
  const region = await openClient(page);
  await expect(region.getByText("This is an unlimited plan. Its token credit is unchanged.")).toBeVisible();
  await expect(region.getByText("Returned to representative credit", { exact: false })).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
});
test("Persian mobile history preserves exact volume above JS safe integer range", async ({ page }) => {
  await page.setViewportSize({ width: 360, height: 800 });
  await page.addInitScript(() => localStorage.setItem("dark-pro-lang", "fa"));
  await fixture(page);
  await page.goto("/finance");
  const region = page.getByRole("region", { name: "Volume return history" });
  await expect(region.getByText("حذف دستی", { exact: false })).toBeVisible();
  await expect(region.getByText(9007199254740993n.toLocaleString("fa-IR"), { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)).toBe(false);
  await page.screenshot({ path: test.info().outputPath("volume-refunds-fa-mobile.png"), fullPage: true });
});
test("foreign financial history and unconfigured feature cannot appear as valid zero refunds", async ({ page }) => {
  await fixture(page, { crossScope: true });
  await page.goto("/finance");
  const region = page.getByRole("region", { name: "Volume return history" });
  await expect(region.getByText("The volume return could not be verified. No refund or zero amount is assumed.")).toBeVisible();
  await expect(region.getByText("deleted-client", { exact: true })).toHaveCount(0);
  await page.unrouteAll();
  await fixture(page, { enabled: false });
  await page.reload();
  await expect(region.getByText("Automatic volume returns have not been enabled on this HUB yet.")).toBeVisible();
});
