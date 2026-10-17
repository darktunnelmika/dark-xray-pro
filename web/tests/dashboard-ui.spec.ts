import { test, expect, type Page } from "@playwright/test";

// All fixtures below exist exclusively inside Playwright, never in the panel.
const session = { authenticated: true, user: { id: "owner", username: "qa-owner", role: "owner", representative_id: "owner" },
  csrf_token: "a".repeat(64), expires_unix: 1900000000 };
const freshNode = {
  node_id: "fresh-pro-lab", name: "Independent PRO Agent", interface: "ens3",
  status: "online", cpu_cores: 4, cpu_percent: 37.5, ram_total_bytes: 8_589_934_592,
  ram_used_bytes: 2_147_483_648, rx_bits_per_second: 5_000_000,
  tx_bits_per_second: 1_500_000, last_seen: new Date().toISOString(),
  metrics_at: new Date().toISOString(), metrics_current: true,
  disk_total_bytes: 42_949_672_960, disk_used_bytes: 10_737_418_240,
};
const delayedNode = { ...freshNode, node_id: "slow-pro-lab", name: "Slow collector",
  status: "online", metrics_current: false, rx_bits_per_second: null, tx_bits_per_second: null };
function fleet() {
  return { as_of: new Date().toISOString(), total_nodes: 2, online_nodes: 2, offline_nodes: 0,
    physical_rates_complete: false, interface_rx_bits_per_second: 5_000_000, interface_tx_bits_per_second: 1_500_000,
    metric_scope: "default_route_interface_not_billable", nodes: [freshNode, delayedNode] };
}
async function setup(page: Page) {
  const calls: string[] = [];
  await page.route("**/api/v1/**", async route => {
    const url = new URL(route.request().url()); const path = url.pathname;
    calls.push(route.request().method() + " " + path + url.search);
    if (path === "/api/v1/session/status") return route.fulfill({ json: session });
    if (path === "/api/v1/fleet") return route.fulfill({ json: fleet() });
    if (path === "/api/v1/workspace/nodes/fresh-pro-lab/history") {
      const windowValue = url.searchParams.get("window");
      const now = Math.floor(Date.now() / 60_000) * 60;
      return route.fulfill({ json: {
        node_id: freshNode.node_id, window: windowValue, bucket_seconds: windowValue === "1h" ? 60 : 300,
        scope: "host_interface_not_billable",
        points: [{ ts_unix: now-120, cpu_percent: 37.5, ram_used_bytes: 2_147_483_648,
          ram_total_bytes: 8_589_934_592, disk_used_bytes: null, disk_total_bytes: null,
          load_1: 0.9, tcp_inuse: 20, rx_bits_per_second: null, tx_bits_per_second: null, observations: 1 }],
      } });
    }
    if (path === "/api/v1/workspace/nodes") return route.fulfill({ json: { mode: "new_pro_lab_only", nodes: [] } });
    if (path.startsWith("/api/v1/workspace/")) return route.fulfill({ json: { clients: [], groups: [], representatives: [], operations: [], events: [] } });
    return route.fulfill({ status: 404, json: {} });
  });
  return calls;
}

test("dashboard uses fresh-only CPU and refuses incomplete physical rates or fabricated chart history", async ({ page }) => {
  const calls = await setup(page);
  await page.goto("/dashboard");
  const panel = page.getByRole("region", { name: "Fleet intelligence" });
  await expect(panel).toBeVisible();
  await expect(panel.getByText("37.5%")).toBeVisible();
  await expect(panel.locator(".dash-quality-line").getByText("Fresh metrics")).toBeVisible();
  await expect(panel.locator(".dash-quality-line").getByText("Delayed metrics")).toBeVisible();
  await expect(panel.getByText("2 / 2")).toBeVisible();
  await expect(panel.getByText("—", { exact: true }).first()).toBeVisible();
  expect(calls.some(s => s.includes("/history?"))).toBe(false);
  await expect(page.getByText("Slow collector")).toBeVisible();
});

test("stored 1h and 24h charts are lazy and request only selected genuine PRO Node", async ({ page }) => {
  const calls = await setup(page);
  await page.goto("/dashboard");
  const panel = page.getByRole("region", { name: "Fleet intelligence" });
  expect(calls.some(s => s.includes("/history?"))).toBe(false);
  await panel.getByRole("button", { name: "View stored history" }).click();
  const history = page.getByRole("region", { name: "Real Node history" });
  await expect(history).toBeVisible();
  await expect(history.getByText(/1 observed buckets/)).toBeVisible();
  await history.getByRole("button", { name: "24 hours" }).click();
  await expect.poll(() => calls.filter(s => s.includes("/history?window=24h")).length).toBe(1);
  await panel.getByRole("button", { name: "Hide history" }).click();
  await expect(history).toHaveCount(0);
});

test("320px Persian RTL dark mode keeps grouped menu usable, account controls visible and no body overflow", async ({ page }) => {
  await setup(page);
  await page.setViewportSize({ width: 320, height: 750 });
  await page.goto("/dashboard");
  await page.getByRole("button", { name: "Switch language" }).click();
  await page.getByRole("button", { name: "Toggle dark and light mode" }).click();
  await page.getByRole("button", { name: "بازکردن منو" }).click();
  const nav = page.getByRole("navigation", { name: "Main navigation" });
  await expect(nav.getByText("زیرساخت")).toBeVisible();
  await expect(nav.getByText("مدیریت")).toBeVisible();
  await expect(nav.getByText("حساب و تنظیمات")).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.getByRole("button", { name: "حساب من: qa-owner" })).toBeVisible();
  expect(await page.evaluate(() => document.body.style.overflow)).toBe("hidden");
  await page.keyboard.press("Escape");
  await expect(page.locator(".sidebar")).not.toHaveClass(/sidebar-open/);
  expect(await page.evaluate(() => document.body.style.overflow)).toBe("");
  await expect(page.getByRole("region", { name: "نمای هوشمند زیرساخت" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
});

test("business pages do not keep polling fleet while the owner is elsewhere", async ({ page }) => {
  const calls = await setup(page);
  await page.goto("/settings");
  await expect(page.getByRole("heading", { name: "Appearance & lab" })).toBeVisible();
  await page.waitForTimeout(1200);
  expect(calls.filter(s => s.startsWith("GET /api/v1/fleet"))).toHaveLength(0);
  await page.getByRole("button", { name: "Overview" }).click();
  await expect(page.getByRole("region", { name: "Fleet intelligence" })).toBeVisible();
  await expect.poll(() => calls.filter(s => s.startsWith("GET /api/v1/fleet")).length).toBeGreaterThan(0);
});

test("inconsistent fleet totals cannot replace the last verified snapshot", async ({ page }) => {
  await setup(page);
  let corrupt = false;
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: corrupt ?
    { ...fleet(), total_nodes: 99, online_nodes: 99 } : fleet() }));
  await page.goto("/dashboard");
  const panel = page.getByRole("region", { name: "Fleet intelligence" });
  await expect(panel.getByText("Verified fleet snapshot")).toBeVisible();
  await expect(page.getByText("Independent PRO Agent")).toBeVisible();
  corrupt = true;
  await expect(panel.getByText(/Last verified snapshot/)).toBeVisible({ timeout: 12000 });
  await expect(page.getByText("Independent PRO Agent")).toBeVisible();
  await expect(panel.getByText("99 / 99")).toHaveCount(0);
});
