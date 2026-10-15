import { expect, test, type Page } from "@playwright/test";

const rep = { id: "rep-a", username: "rep-a", role: "representative", representative_id: "rep-a" };
const owner = { id: "owner", username: "proowner", role: "owner", representative_id: "owner" };
const noFleet = {
  as_of: new Date("2026-10-15T09:00:00Z").toISOString(),
  total_nodes: 0, online_nodes: 0, offline_nodes: 0,
  metric_scope: "default_route_interface_not_billable",
  interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false,
  nodes: [],
};
async function mockBase(page: Page) {
  await page.route("**/api/v1/fleet", (route) => route.fulfill({
    status: 200, contentType: "application/json", body: JSON.stringify(noFleet),
  }));
}
function successfulLogin(user: typeof rep) {
  return { authenticated: true, user, csrf_token: "a".repeat(64), expires_unix: 1800000000 };
}
async function mockBusiness(page: Page, user: typeof rep) {
  const requests: { path: string; method: string; csrf: string; body: unknown }[] = [];
  const runningClients: unknown[] = [];
  await page.route("**/api/v1/session/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (path.endsWith("/status")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ enabled: true, authenticated: false }) });
    } else if (path.endsWith("/login")) {
      const body = request.postDataJSON() as Record<string, unknown>;
      expect(body.password).toBe("test-private-password-for-lab");
      await route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify(successfulLogin(user)) });
    } else {
      await route.fulfill({ status: 404, body: "{}" });
    }
  });
  await page.route("**/api/v1/workspace/**", async (route) => {
    const request = route.request();
    const path = new URL(request.url()).pathname;
    if (request.method() === "POST") {
      const body: unknown = request.postDataJSON();
      requests.push({ path, method: "POST", csrf: request.headers()["x-csrf-token"] ?? "", body });
      if (path.endsWith("/clients")) {
        const n = body as Record<string, unknown>;
        runningClients.push({
          id: n.client_id, name: n.name, representative_id: n.representative_id,
          plan_kind: n.unlimited ? "unlimited" : "volume", ip_count: n.ip_count,
          volume_bytes: n.volume_bytes, duration_days: n.duration_days,
          charged_unlimited_units: 10, deployment_state: "pending_node_apply",
          expires_unix: 1800000000,
        });
      }
      await route.fulfill({ status: 201, contentType: "application/json", body: JSON.stringify(body) });
      return;
    }
    if (path.endsWith("/me")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ user, balance: {
          id: user.representative_id, display_name: "Visible Account", kind: user.role,
          enabled: true, volume_credit_bytes: 10737418240, unlimited_credit_units: 90,
        }, data_plane: "not_activated" }),
      });
    } else if (path.endsWith("/clients")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ clients: runningClients }) });
    } else if (path.endsWith("/events")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ events: [{ event_id: "credit-test-123456789", kind: "credit_adjustment",
          representative_id: user.representative_id, object_id: "", reason: "Private grant",
          volume_delta_bytes: 10737418240, unlimited_delta_units: 100 }] }) });
    } else if (path.endsWith("/representatives")) {
      await route.fulfill({ status: 200, contentType: "application/json",
        body: JSON.stringify({ representatives: [
          { id: "owner", display_name: "Owner", kind: "owner", enabled: true, volume_credit_bytes: 0, unlimited_credit_units: 0 },
          { id: "rep-a", display_name: "Rep A", kind: "representative", enabled: true, volume_credit_bytes: 0, unlimited_credit_units: 90 },
        ] }) });
    } else {
      await route.fulfill({ status: 404, body: "{}" });
    }
  });
  return requests;
}
test("Business page never invents owner, representative or credit when backend is disabled", async ({ page }) => {
  await mockBase(page);
  await page.route("**/api/v1/session/status", (route) => route.fulfill({ status: 404, body: "{}" }));
  await page.goto("/");
  await page.getByRole("button", { name: "Business" }).click();
  await expect(page.getByText("Business storage is not enabled on this LAB HUB yet.")).toBeVisible();
  await expect(page.getByRole("button", { name: "Create representative" })).toHaveCount(0);
  await expect(page.getByText("90", { exact: true })).toHaveCount(0);
});

test("representative only sees own scoped business tools and submits CSRF-bound pending client", async ({ page }) => {
  await mockBase(page);
  const requests = await mockBusiness(page, rep);
  await page.setViewportSize({ width: 320, height: 750 });
  await page.goto("/");
  await page.getByRole("button", { name: "Open menu" }).click();
  await page.getByRole("button", { name: "Business" }).click();
  await expect(page.getByRole("heading", { name: "Sign in" })).toBeVisible();
  await page.getByLabel("Username").fill("rep-a");
  await page.getByLabel("Password").fill("test-private-password-for-lab");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByText("Visible Account")).toBeHidden(); // not rendered from balance display name
  await expect(page.getByText("Scoped to your representative account.")).toBeVisible();
  await expect(page.getByText("90", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create representative" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Record credit transaction" })).toHaveCount(0);
  await page.getByLabel("Client ID").fill("sample-pro-rep");
  await page.getByLabel("Client name").fill("Private Test User");
  await page.getByLabel("Plan type").selectOption("unlimited");
  await page.getByLabel("Duration (days)").fill("60");
  await page.getByLabel("IP/User slots (0 = unlimited for volume)").fill("5");
  await page.getByRole("button", { name: "Create pending client" }).click();
  await expect(page.getByText("Private Test User", { exact: false })).toBeVisible();
  expect(requests).toHaveLength(1);
  expect(requests[0].path).toBe("/api/v1/workspace/clients");
  expect(requests[0].csrf).toBe("a".repeat(64));
  const body = requests[0].body as Record<string, unknown>;
  expect(body.representative_id).toBe("rep-a");
  expect(body.duration_days).toBe(60);
  expect(body.ip_count).toBe(5);
  expect(body.unlimited).toBe(true);
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 2);
  expect(overflow).toBe(false);
});

test("owner gets real scoped representative forms in Persian without sending owner token to browser", async ({ page }) => {
  await mockBase(page);
  const requests = await mockBusiness(page, owner);
  await page.goto("/");
  await page.getByRole("button", { name: "Business" }).click();
  await page.getByLabel("Username").fill("proowner");
  await page.getByLabel("Password").fill("test-private-password-for-lab");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Create representative" })).toBeVisible();
  await page.getByRole("button", { name: "Switch language" }).click();
  await expect(page.getByRole("heading", { name: "ساخت نماینده" })).toBeVisible();
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  const form = page.getByRole("heading", { name: "ساخت نماینده" }).locator("..");
  await form.getByLabel("شناسه نماینده").fill("rep-new");
  await form.getByLabel("نام نمایشی").fill("نماینده جدید");
  await form.getByRole("button", { name: "ساخت نماینده" }).click();
  expect(requests).toHaveLength(1);
  expect(requests[0].csrf).toBe("a".repeat(64));
  const body = requests[0].body as Record<string, unknown>;
  expect(body.id).toBe("rep-new");
  expect(body.name).toBe("نماینده جدید");
  const leaked = await page.evaluate(() => JSON.stringify(localStorage));
  expect(leaked).not.toContain("csrf_token");
  expect(leaked).not.toContain("test-private-password-for-lab");
});
