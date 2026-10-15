import { expect, test, type Page } from "@playwright/test";

// Synthetic browser fixtures are restricted to the test harness.
// Runtime UI never ships mock customer, Node or billing data.
const baseNode = {
  node_id: "de-01", name: "Germany lab", interface: "ens3",
  status: "online", cpu_cores: 4, cpu_percent: 22.2,
  ram_total_bytes: 8_589_934_592, ram_used_bytes: 3_221_225_472,
  rx_bits_per_second: 8_000_000, tx_bits_per_second: 2_000_000,
  last_seen: "2026-10-12T07:00:00Z",
  metrics_at: "2026-10-12T07:00:00Z", metrics_current: true,
};
const fleet = {
  as_of: "2026-10-12T07:00:00Z",
  total_nodes: 2, online_nodes: 1, offline_nodes: 1,
  interface_rx_bits_per_second: 8_000_000,
  interface_tx_bits_per_second: 2_000_000,
  physical_rates_complete: true,
  metric_scope: "default_route_interface_not_billable",
  nodes: [
    baseNode,
    { ...baseNode, node_id: "nl-02", name: "Netherlands lab", status: "offline",
      rx_bits_per_second: null, tx_bits_per_second: null, metrics_current: false },
  ],
};
async function mockFleet(page: Page, getData: () => unknown = () => fleet) {
  await page.route("**/api/v1/fleet", async (route) => {
    await route.fulfill({
      status: 200, contentType: "application/json", body: JSON.stringify(getData()),
    });
  });
}

test("dashboard and Nodes show actual API values without pretending offline Nodes vanished", async ({ page }) => {
  await mockFleet(page);
  await page.goto("/");
  await expect(page.getByRole("heading", { name: "Infrastructure, at a glance." })).toBeVisible();
  await expect(page.getByText("Germany lab")).toBeVisible();
  await expect(page.getByText("Netherlands lab")).toBeVisible();
  await expect(page.getByText("8 Mbps").first()).toBeVisible();
  await page.getByRole("button", { name: "Nodes" }).click();
  await expect(page.getByRole("heading", { name: "Node fleet" })).toBeVisible();
  await expect(page.getByText("Netherlands lab")).toBeVisible();
  await expect(page.getByText("4 cores")).toHaveCount(2);
});

test("theme, language and mobile navigation survive refresh without horizontal overflow", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await mockFleet(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Open menu" }).click();
  await page.getByRole("button", { name: "Settings" }).click();
  await page.getByRole("button", { name: "فارسی" }).click();
  await page.getByRole("button", { name: "شب", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await page.reload();
  await expect(page.getByRole("heading", { name: "ظاهر و محیط آزمایش" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await expect(page.locator("html")).toHaveAttribute("lang", "fa");
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2);
  expect(overflow).toBe(false);
});

test("transient API failure preserves last verified Nodes and reports an actual error", async ({ page }) => {
  let fail = false;
  await page.route("**/api/v1/fleet", async (route) => {
    if (fail) await route.fulfill({ status: 503, contentType: "application/json", body: "{}" });
    else await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(fleet) });
  });
  await page.goto("/");
  await expect(page.getByText("Germany lab")).toBeVisible();
  fail = true;
  await expect(page.getByRole("alert")).toBeVisible({ timeout: 10_000 });
  await expect(page.getByText("Germany lab")).toBeVisible();
  await expect(page.getByText("Netherlands lab")).toBeVisible();
});

test("first physical sample has no invented zero or billable usage", async ({ page }) => {
  await mockFleet(page, () => ({
    ...fleet, physical_rates_complete: false,
    interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0,
    nodes: fleet.nodes.map((node) => ({
      ...node, rx_bits_per_second: null, tx_bits_per_second: null,
    })),
  }));
  await page.goto("/");
  await expect(page.getByText("Germany lab")).toBeVisible();
  await expect(page.getByText("0 Mbps")).toHaveCount(0);
  await expect(page.getByText("Default-route interface rate, not customer-billable traffic.").first()).toBeVisible();
});

test("320px viewport avoids body overflow", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 750 });
  await mockFleet(page);
  await page.goto("/");
  await expect(page.getByText("Germany lab")).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2);
  expect(overflow).toBe(false);
});

test("Online heartbeat stays Online while delayed metrics show unknown instead of stale CPU", async ({ page }) => {
  const busy = {
    ...fleet, physical_rates_complete: false,
    nodes: fleet.nodes.map((node) =>
      node.node_id === "de-01"
        ? { ...node, metrics_current: false, rx_bits_per_second: null, tx_bits_per_second: null }
        : node),
  };
  await mockFleet(page, () => busy);
  await page.goto("/");
  await expect(page.getByText("Germany lab")).toBeVisible();
  await expect(page.getByText("Stats delayed")).toBeVisible();
  await expect(page.getByText("Online", { exact: true }).last()).toBeVisible();
  await expect(page.getByText("0 Mbps")).toHaveCount(0);
});


test("Node Manager inspects the exact selected identity and preserves it across refresh", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 750 });
  await mockFleet(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Open menu" }).click();
  await page.getByRole("button", { name: "Nodes" }).click();
  await expect(page.getByText("Select a Node to inspect its status.")).toBeVisible();
  await page.getByRole("button", { name: "Inspect Netherlands lab" }).click();
  const detail = page.getByRole("region", { name: "Node details" });
  await expect(detail.getByRole("heading", { name: "Netherlands lab" })).toBeVisible();
  await expect(detail.getByText("Offline")).toBeVisible();
  await expect(detail.locator(".node-inspector-field").filter({ hasText: "CPU" }).locator("strong")).toHaveText("—");
  await expect(page.getByRole("button", { name: "Inspect Netherlands lab" })).toHaveAttribute("aria-pressed", "true");
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2);
  expect(overflow).toBe(false);
  await page.reload();
  await expect(page.getByRole("region", { name: "Node details" }).getByRole("heading", { name: "Netherlands lab" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Inspect Netherlands lab" })).toHaveAttribute("aria-pressed", "true");
});

test("Node Manager does not mislabel delayed metrics as Offline or fabricate zero rates", async ({ page }) => {
  await mockFleet(page, () => ({
    ...fleet,
    nodes: fleet.nodes.map((node) => node.node_id === "de-01" ? {
      ...node, metrics_current: false, rx_bits_per_second: null, tx_bits_per_second: null,
    } : node),
  }));
  await page.goto("/");
  await page.getByRole("button", { name: "Nodes" }).click();
  await page.getByRole("button", { name: "Inspect Germany lab" }).click();
  const detail = page.getByRole("region", { name: "Node details" });
  await expect(detail.getByText("Online")).toBeVisible();
  await expect(detail.getByText("Delayed / unavailable")).toBeVisible();
  await expect(detail.locator(".node-inspector-field").filter({ hasText: "Fleet interface RX" }).locator("strong")).toHaveText("—");
  await expect(detail.getByText("Read-only LAB view.", { exact: false })).toBeVisible();
});

test("empty Node Manager explicitly keeps enrollment private, without trying owner API", async ({ page }) => {
  let ownerRequests = 0;
  page.on("request", (request) => { if (request.url().includes("/api/v1/admin/")) ownerRequests++; });
  await mockFleet(page, () => ({
    ...fleet, nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0,
    physical_rates_complete: false,
  }));
  await page.goto("/");
  await page.getByRole("button", { name: "Nodes" }).click();
  await expect(page.getByText("New LAB Node pairing is owner-only on localhost.", { exact: false })).toBeVisible();
  await expect(page.getByRole("region", { name: "Node details" })).toHaveCount(0);
  expect(ownerRequests).toBe(0);
});


test("Inbound planning keeps enrollment and activation private with no connected Nodes", async ({ page }) => {
  let unsafeRequests = 0;
  page.on("request", (request) => {
    if (request.method() !== "GET" || request.url().includes("/api/v1/admin/")) unsafeRequests++;
  });
  await mockFleet(page, () => ({ ...fleet, nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0 }));
  await page.goto("/");
  await page.getByRole("button", { name: "Inbounds" }).click();
  await expect(page.getByRole("heading", { name: "Inbound deployment draft" })).toBeVisible();
  await expect(page.getByText("No independently paired PRO Nodes are available yet.")).toBeVisible();
  await expect(page.getByLabel("Target Node")).toBeDisabled();
  await expect(page.getByText("PREVIEW ONLY", { exact: false })).toBeVisible();
  expect(unsafeRequests).toBe(0);
});

test("Direct + Dark Swap preview uses exactly three swap inputs and flags shared-port attribution", async ({ page }) => {
  let unsafeRequests = 0;
  page.on("request", (request) => {
    if (request.url().includes("/api/v1/admin/") || request.method() === "POST") unsafeRequests++;
  });
  await mockFleet(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Inbounds" }).click();
  await page.getByLabel("Target Node").selectOption("de-01");
  await page.getByLabel("Inbound ID").fill("1185");
  await page.getByLabel("Direct Node port").fill("8443");
  await page.getByRole("checkbox", { name: "Dark Swap" }).check();
  await expect(page.locator(".inbound-swap-fields input")).toHaveCount(3);
  await page.getByLabel("Destination tunnel port").fill("6800");
  await page.getByLabel("Node port", { exact: true }).fill("8443");
  await page.getByLabel("Tunnel host or IPv4").fill("example.net");
  const preview = page.getByRole("region", { name: "Inbound route preview" });
  await expect(preview.locator(".inbound-preview-rows > div")).toHaveCount(2);
  await expect(preview.getByText("Shared Node port:", { exact: false })).toBeVisible();
  await expect(preview).toContainText("example.net:6800");
  await expect(preview).toContainText("Activation blocked");
  await page.getByLabel("Node port", { exact: true }).fill("9443");
  await expect(preview.getByText("Distinct Node ports:", { exact: false })).toBeVisible();
  await page.getByLabel("Tunnel host or IPv4").fill("https://invalid.example");
  await expect(preview.getByText("Use a valid IPv4 address or DNS hostname", { exact: false })).toBeVisible();
  await expect(preview.locator(".inbound-preview-rows")).toHaveCount(0);
  expect(unsafeRequests).toBe(0);
});

test("mobile RTL Dark Swap draft stays readable and never enables Apply", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 750 });
  await mockFleet(page);
  await page.goto("/");
  await page.getByRole("button", { name: "Open menu" }).click();
  await page.getByRole("button", { name: "Inbounds" }).click();
  await page.getByRole("button", { name: "EN" }).click();
  await expect(page.getByRole("heading", { name: "پیش‌نویس اینباند" })).toBeVisible();
  await page.getByLabel("نود مقصد").selectOption("de-01");
  await page.getByLabel("شناسه اینباند").fill("42");
  await page.getByLabel("پورت نود مستقیم").fill("8443");
  await page.getByRole("checkbox", { name: "دارک سواپ" }).check();
  await page.getByLabel("پورت تانل مقصد").fill("6800");
  await page.getByLabel("پورت نود", { exact: true }).fill("9443");
  await page.getByLabel("هاست یا IP تانل").fill("198.51.100.1");
  await expect(page.getByText("فقط پیش‌نمایش", { exact: false })).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2);
  expect(overflow).toBe(false);
});
