import { test, expect, type Page } from "@playwright/test";

const pairCode = "c".repeat(64);
const owner = { id: "owner", username: "new-pro-owner", role: "owner", representative_id: "owner" };
const representative = { id: "rep-01", username: "rep-01", role: "representative", representative_id: "rep-01" };

async function setupNames(page: Page, mode: "lost" | "conflict" | "read-error" = "lost") {
  await setup(page);
  let name = "Original";
  let revision = 1;
  let reads = 0;
  const requests: Record<string, unknown>[] = [];
  let receipt: Record<string, unknown> | null = null;
  await page.route("**/api/v1/**", async route => {
    const req = route.request();
    const path = new URL(req.url()).pathname;
    const node = { node_id: "lab-name", name, installation_id: "a".repeat(32), paired_at: "2026-10-20T12:00:00Z", status: "offline", metrics_current: false, last_seen: null };
    if (path === "/api/v1/workspace/nodes") return route.fulfill({json:{mode:"new_pro_lab_only",nodes:[node]}});
    if (path.endsWith("/lab-name/name")) {
      reads++;
      if (mode === "read-error") return route.fulfill({status:503,json:{error:"unavailable"}});
      return route.fulfill({json:{node,revision,audit_count:receipt ? 1 : 0,recent:receipt ? [receipt] : []}});
    }
    if (path.endsWith("/lab-name/rename")) {
      expect(req.headers()["x-csrf-token"]).toBe("a".repeat(64));
      const input = req.postDataJSON(); requests.push(input);
      if (mode === "conflict") { name="Other owner";revision=2;return route.fulfill({status:409,json:{error:"node_name_conflict"}}); }
      if (!receipt) {
        expect(input.expected_revision).toBe(1);
        receipt={...input,previous_name:name,revision:2,actor_id:"owner",at:"2026-10-20T12:01:00Z"};
        name=input.name;revision=2;
        return route.fulfill({status:503,json:{error:"node_name_outcome_unknown"}});
      }
      expect(input).toEqual(requests[0]);
      return route.fulfill({json:{receipt}});
    }
    return route.fallback();
  });
  return {requests, reads:()=>reads};
}

test("name editor retries a committed lost reply with the identical event and reads live state", async ({page}) => {
  const probe=await setupNames(page);
  await page.goto("/nodes");await login(page);
  await expect(page.getByRole("button",{name:"Manage display name"})).toBeVisible();
  expect(probe.reads()).toBe(0);
  await page.getByRole("button",{name:"Manage display name"}).click();
  await page.getByLabel("New display name").fill("Renamed");
  await page.getByRole("button",{name:"Save name",exact:true}).click();
  await expect(page.getByText(/Outcome is unknown/)).toBeVisible();
  await expect(page.getByLabel("New display name")).toBeDisabled();
  await page.getByRole("button",{name:"Retry same request"}).click();
  await expect(page.getByText("Receipt confirmed; current state refreshed.")).toBeVisible();
  await expect(page.getByLabel("New display name")).toHaveValue("Renamed");
  expect(probe.requests).toHaveLength(2);
  expect(probe.requests[0]).toEqual(probe.requests[1]);
  await expect(page.locator(".pro-node-name-editor li")).toHaveCount(1);
});

test("name revision conflict locks editing until explicit refresh", async ({page}) => {
  await setupNames(page,"conflict");await page.goto("/nodes");await login(page);
  await page.getByRole("button",{name:"Manage display name"}).click();
  await page.getByLabel("New display name").fill("Stale change");
  await page.getByRole("button",{name:"Save name",exact:true}).click();
  await expect(page.getByText(/Revision or event conflict/)).toBeVisible();
  await expect(page.getByRole("button",{name:"Save name",exact:true})).toBeDisabled();
  await page.getByRole("button",{name:"Read current name"}).click();
  await expect(page.getByLabel("New display name")).toHaveValue("Other owner");
  await expect(page.getByText("Revision: 2",{exact:true})).toBeVisible();
});

test("Persian name editor remains locked on unreadable state at 320px", async ({page}) => {
  const probe=await setupNames(page,"read-error");await page.setViewportSize({width:320,height:760});
  await page.goto("/nodes");await login(page);
  await page.getByRole("button",{name:"Switch language"}).click();
  await page.getByRole("button",{name:"مدیریت نام نمایشی"}).click();
  await expect(page.getByText("نام فعلی قابل دریافت نیست؛ ویرایش قفل است.")).toBeVisible();
  await expect(page.getByRole("button",{name:"ذخیرهٔ نام"})).toBeDisabled();
  expect(probe.requests).toHaveLength(0);
  const overflow = await page.evaluate(() => [...document.querySelectorAll("main *")].map(el => ({ tag: el.tagName, cls: el.className, width: el.getBoundingClientRect().width, left: el.getBoundingClientRect().left, right: el.getBoundingClientRect().right })).filter(el => el.left < -2 || el.right > innerWidth + 2));
  expect(overflow).toEqual([]);
});

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
      json: { mode: "new_pro_lab_only", nodes: [],
        installer_available: true, agent_transport_scope: "loopback_only" },
    });
    if (req.method() === "GET" && path === "/api/v1/workspace/nodes/installer-bundle") {
      return route.fulfill({ status: 200, contentType: "application/zip",
        headers: { "Content-Disposition": "attachment; filename=\"dark-pro-new-node-installer.zip\"" },
        body: "PK-test-only-isolated-bundle" });
    }
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


test("owner downloads verified installer before the short-lived ticket and never places Pair Code into a shell command",async({page})=>{
  const probe=await setup(page);
  await page.goto("/nodes");await login(page);
  const installer=page.getByRole("region",{name:"New Node installer · signed-in owner only"});
  await expect(installer).toBeVisible();
  await expect(installer.getByText(/loopback-only/)).toBeVisible();
  const download=page.waitForEvent("download");
  await installer.getByRole("button",{name:"Download checked Agent bundle"}).click();
  const file=await download;
  expect(file.suggestedFilename()).toBe("dark-pro-new-node-installer.zip");
  await page.getByLabel("New Node ID").fill("qa-new-node");
  await page.getByLabel("Display name").fill("Fresh QA only");
  await page.getByRole("button",{name:"Generate one-time Pair Code"}).click();
  await expect(page.locator(".pro-node-ticket-code")).toHaveText(pairCode);
  const command=await page.locator(".pro-node-command pre").innerText();
  expect(command).toContain("DARK_PRO_NODE_ID='qa-new-node'");
  expect(command).toContain("DARK_PRO_HUB_AGENT_URL='https://VERIFIED_AGENT_HOST:24443'");
  expect(command).toContain("DARK_PRO_NEW_NODE_ACK=I_CONFIRM_NEW_EMPTY_PRO_NODE");
  expect(command).not.toContain(pairCode);
  expect(command).not.toContain("agent_token");
  expect(probe.paths.some(p=>p==="GET /api/v1/workspace/nodes/installer-bundle")).toBe(true);
  await page.reload();
  await expect(page.locator(".pro-node-ticket-code")).toHaveCount(0);
  await expect(page.locator(".pro-node-command")).toHaveCount(0);
  const stored=await page.evaluate(()=>JSON.stringify({...localStorage,...sessionStorage}));
  expect(stored).not.toContain(pairCode);
});

test("fresh Node installer instructions and command do not overflow 320px Persian RTL",async({page})=>{
  await setup(page);
  await page.setViewportSize({width:320,height:760});
  await page.goto("/nodes");await login(page);
  await page.getByRole("button",{name:"Switch language"}).click();
  await expect(page.getByRole("region",{name:"نصب Agent نود جدید · فقط مالک"})).toBeVisible();
  await page.getByLabel("شناسه نود جدید").fill("qa-new-node");
  await page.getByLabel("نام نمایشی").fill("نود جدید");
  await page.getByRole("button",{name:"ساخت کد اتصال یک‌بارمصرف"}).click();
  await expect(page.getByRole("region",{name:"فقط داخل VPS جدید اجرا کن"})).toBeVisible();
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+2)).toBe(true);
});
