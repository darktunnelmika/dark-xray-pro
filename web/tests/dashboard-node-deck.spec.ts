import { expect, test, type Page } from "@playwright/test";

// Browser-only synthetic fixtures are never shipped with the PRO application.
const session = { authenticated: true,
  user: { id: "owner", username: "qa-owner", role: "owner", representative_id: "owner" },
  csrf_token: "a".repeat(64), expires_unix: 1900000000 };

function fleet(mode: "fresh" | "zero" | "delayed" = "fresh") {
  const empty = mode === "zero";
  return {
    as_of: new Date().toISOString(),
    total_nodes: empty ? 0 : 1,
    online_nodes: empty ? 0 : 1,
    offline_nodes: 0,
    interface_rx_bits_per_second: empty ? 0 : 7_200_000,
    interface_tx_bits_per_second: empty ? 0 : 1_500_000,
    physical_rates_complete: !empty && mode === "fresh",
    metric_scope: "default_route_interface_not_billable",
    nodes: empty ? [] : [{
      node_id: "usa-pro-qa", name: "USA PRO QA", interface: "eth0",
      status: "online", cpu_cores: 2, cpu_percent: 12.5,
      ram_total_bytes: 8_589_934_592, ram_used_bytes: 2_147_483_648,
      disk_total_bytes: 42_949_672_960, disk_used_bytes: 10_737_418_240,
      load_1: 0.2, tcp_inuse: 18,
      rx_bits_per_second: mode === "fresh" ? 7_200_000 : null,
      tx_bits_per_second: mode === "fresh" ? 1_500_000 : null,
      last_seen: new Date().toISOString(), metrics_at: new Date().toISOString(),
      metrics_current: mode === "fresh",
    }],
  };
}
function hub() {
  return {
    scope: "hub_host_physical_not_billable", source: "local_linux",
    sampled_at: new Date().toISOString(), metrics_current: true, cpu_cores: 4, cpu_percent: 20,
    ram_total_bytes: 8_589_934_592, ram_used_bytes: 2_147_483_648,
    disk_total_bytes: 85_899_345_920, disk_used_bytes: 21_474_836_480,
    load_1: 1.2, load_5: 0.5, load_15: 0.2,
    host_uptime_seconds: 90000, hub_process_uptime_seconds: 3600,
    hub_process_rss_bytes: 94_371_840, tcp_inuse: 30,
    interface: "eth0", rx_bits_per_second: 2_200_000, tx_bits_per_second: 800_000,
    history: [],
  };
}
async function setup(page: Page, mode: "fresh" | "zero" | "delayed" = "fresh") {
  const calls: string[] = [];
  let broken = false;
  await page.route("**/api/v1/**", async route => {
    const path = new URL(route.request().url()).pathname;
    calls.push(route.request().method() + " " + path);
    if (path === "/api/v1/session/status") return route.fulfill({ json: session });
    if (path === "/api/v1/fleet") return route.fulfill({ json: broken ?
      { ...fleet(mode), total_nodes: 99, online_nodes: 99 } : fleet(mode) });
    if (path === "/api/v1/workspace/hub/metrics") return route.fulfill({ json: hub() });
    if (path === "/api/v1/workspace/nodes") return route.fulfill({ json:
      { mode: "new_pro_lab_only", nodes: [], installer_available: false, agent_transport_scope: "loopback_only" } });
    if (path === "/api/v1/workspace/nodes/usa-pro-qa/history") return route.fulfill({ json: {
      node_id: "usa-pro-qa", window: "1h", bucket_seconds: 60,
      scope: "host_interface_not_billable", points: [],
    } });
    if (path.startsWith("/api/v1/workspace/")) return route.fulfill({ json: {
      clients: [], groups: [], operations: [], events: [], representatives: [],
    } });
    return route.fulfill({ status: 404, json: {} });
  });
  return { calls, breakFleet: () => { broken = true; } };
}

test("Node monitor is the immediate second dashboard section under HUB, not at the page bottom", async ({ page }) => {
  const probe = await setup(page);
  await page.goto("/dashboard");
  await expect(page.getByRole("region", { name: "HUB live monitor" })).toBeVisible();
  const deck = page.getByRole("region", { name: "Node fleet monitoring" });
  await expect(deck).toBeVisible();
  const order = await page.locator(".dashboard-v2").evaluate(root =>
    Array.from(root.children).map(el => el.className.toString()));
  expect(order[0]).toContain("hub-live");
  expect(order[1]).toContain("node-deck");
  expect(order.indexOf("node-deck")).toBe(1);
  expect(order.findIndex(x => x.includes("dash-metric-grid"))).toBeGreaterThan(1);
  await expect(deck.getByText("USA PRO QA")).toBeVisible();
  await expect(deck.getByText("12.5%")).toBeVisible();
  await expect(deck.getByText("2 GiB / 8 GiB")).toBeVisible();
  await expect(deck.getByText("7.2 Mbps")).toBeVisible();
  await expect(deck.getByText("1.5 Mbps")).toBeVisible();
  await expect(deck.getByText("Host interface only · not customer-billable traffic")).toBeVisible();
  await expect(page.locator(".node-list")).toHaveCount(0);
  expect(probe.calls.filter(x => x === "GET /api/v1/fleet")).toHaveLength(1);
  expect(probe.calls.filter(x => x.includes("/history"))).toHaveLength(0);
});

test("one click from Node card opens the matching Node details without duplicate dashboard rows", async ({ page }) => {
  await setup(page);
  await page.goto("/dashboard");
  const deck = page.getByRole("region", { name: "Node fleet monitoring" });
  await deck.getByRole("button", { name: "Inspect Node" }).click();
  await expect(page).toHaveURL(/\/nodes$/);
  await expect(page.locator(".node-inspector")).toBeVisible();
  await expect(page.getByRole("button", { name: "Inspect USA PRO QA" })).toHaveAttribute("aria-pressed", "true");
  await expect(deck).toHaveCount(0);
});

test("invalid later fleet reading preserves last known Node identity but never claims stale CPU is live", async ({ page }) => {
  const probe = await setup(page);
  await page.goto("/dashboard");
  const deck = page.getByRole("region", { name: "Node fleet monitoring" });
  await expect(deck.getByText("12.5%")).toBeVisible();
  probe.breakFleet();
  await expect(deck.getByText(/Snapshot delayed/)).toBeVisible({ timeout: 12000 });
  await expect(deck.getByText("USA PRO QA")).toBeVisible();
  await expect(deck.locator(".node-deck-cpu-text strong")).toHaveText("—");
  await expect(deck.getByText("7.2 Mbps")).toHaveCount(0);
  await expect(deck.getByText("Last verified status: Online")).toBeVisible();
  await expect(deck.locator(".node-deck-trend-pending")).toBeVisible();
});

test("delayed Node metrics are unknown rather than zero even if heartbeat is Online", async ({ page }) => {
  await setup(page, "delayed");
  await page.goto("/dashboard");
  const deck = page.getByRole("region", { name: "Node fleet monitoring" });
  await expect(deck.getByText("USA PRO QA")).toBeVisible();
  await expect(deck.getByText("Metrics delayed")).toBeVisible();
  await expect(deck.locator(".node-deck-cpu-text strong")).toHaveText("—");
  await expect(deck.getByText("0 Mbps")).toHaveCount(0);
});

test("320px Persian RTL Node deck is immediately below HUB with no horizontal overflow", async ({ page }) => {
  await setup(page);
  await page.setViewportSize({ width: 320, height: 760 });
  await page.goto("/dashboard");
  await page.getByRole("button", { name: "Switch language" }).click();
  const deck = page.getByRole("region", { name: "Node fleet monitoring" });
  await expect(deck.getByRole("heading", { name: "تمام نودها؛ در یک قاب." })).toBeVisible();
  await expect(deck.getByText("USA PRO QA")).toBeVisible();
  await expect(deck.getByText("دریافت فیزیکی")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
  const positions = await page.evaluate(() => {
    const a = document.querySelector(".hub-live")!.getBoundingClientRect();
    const b = document.querySelector(".node-deck")!.getBoundingClientRect();
    return { hubBottom: a.bottom, nodesTop: b.top };
  });
  expect(positions.nodesTop).toBeGreaterThan(positions.hubBottom);
  expect(positions.nodesTop - positions.hubBottom).toBeLessThan(40);
});

test("no registered Nodes produces an honest empty fleet directly under an independently live HUB", async ({ page }) => {
  await setup(page, "zero");
  await page.goto("/dashboard");
  const deck = page.getByRole("region", { name: "Node fleet monitoring" });
  await expect(deck.getByRole("heading", { name: "No Nodes connected yet" })).toBeVisible();
  await expect(page.getByRole("region", { name: "HUB live monitor" }).getByText("HUB API responsive")).toBeVisible();
  await expect(deck.locator(".node-deck-card")).toHaveCount(0);
  await expect(deck.getByText("0", { exact: true })).toHaveCount(4);
});
