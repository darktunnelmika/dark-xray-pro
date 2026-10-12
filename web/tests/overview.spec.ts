import { expect, test, type Page } from "@playwright/test";

// Synthetic browser fixtures are restricted to the test harness.
// Runtime UI never ships mock customer, Node or billing data.
const baseNode = {
  node_id: "de-01", name: "Germany lab", interface: "ens3",
  status: "online", cpu_cores: 4, cpu_percent: 22.2,
  ram_total_bytes: 8_589_934_592, ram_used_bytes: 3_221_225_472,
  rx_bits_per_second: 8_000_000, tx_bits_per_second: 2_000_000,
  last_seen: "2026-10-12T07:00:00Z",
};
const fleet = {
  as_of: "2026-10-12T07:00:00Z",
  total_nodes: 2, online_nodes: 1, offline_nodes: 1,
  physical_rx_bits_per_second: 8_000_000,
  physical_tx_bits_per_second: 2_000_000,
  physical_rates_complete: true,
  metric_scope: "physical_interface_not_billable",
  nodes: [
    baseNode,
    { ...baseNode, node_id: "nl-02", name: "Netherlands lab", status: "offline",
      rx_bits_per_second: null, tx_bits_per_second: null },
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
  await page.getByRole("button", { name: "شب" }).click();
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
    physical_rx_bits_per_second: 0, physical_tx_bits_per_second: 0,
    nodes: fleet.nodes.map((node) => ({
      ...node, rx_bits_per_second: null, tx_bits_per_second: null,
    })),
  }));
  await page.goto("/");
  await expect(page.getByText("Germany lab")).toBeVisible();
  await expect(page.getByText("0 Mbps")).toHaveCount(0);
  await expect(page.getByText("Network rate on the default interface. Not customer-billable usage.").first()).toBeVisible();
});

test("320px viewport avoids body overflow", async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 750 });
  await mockFleet(page);
  await page.goto("/");
  await expect(page.getByText("Germany lab")).toBeVisible();
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2);
  expect(overflow).toBe(false);
});
