import { expect, test, type Page } from "@playwright/test";
import type { Channel, Command, CommandRequest, Observation, Scope } from "../src/routeTransactions";

const csrf = "a".repeat(64), installation = "1".repeat(32), instance = "2".repeat(32), digest = "d".repeat(64);
const id = (n: number) => n.toString(16).padStart(32, "0");
const apiBase = "/api/v1/workspace/native-nodes/route-qa/route-transactions";
const stopped = (): Observation => ({ highwater: 0, saved_sequence: 0, saved_sha: "", active_sequence: 0, active_sha: "", process_ready: false, phase: "awaiting_fresh_lease", lease_expires_unix_nano: 0, data_plane_verified: false, subscription_published: false, no_listeners: false });
const ready = (sequence = 1, leaseMS = 30000): Observation => ({ ...stopped(), highwater: sequence, saved_sequence: sequence, saved_sha: digest, active_sequence: sequence, active_sha: digest, process_ready: true, phase: "ready", lease_expires_unix_nano: (Date.now() + leaseMS) * 1e6 });
function ack(c: Command, outcome: string, observed: Observation = stopped()): Command {
 return { ...c, state: "acknowledged", receipt: { command_id: c.id, action: c.action, stage_id: c.stage_id, node_id: c.node_id, installation_id: c.installation_id, serial: c.serial, runtime_instance: instance, signature: "f".repeat(64), outcome, observed_unix_nano: Date.now() * 1e6, observed } };
}
function command(n: number, action: Command["action"], stage = id(1)): Command {
 return { id: id(n), action, stage_id: action === "stage" ? id(n) : stage, serial: n, node_id: "route-qa", installation_id: installation, state: "queued", created_unix_nano: Date.now() * 1e6 };
}
async function fixture(page: Page, role = "owner") {
 const scope: Scope = { enabled: true, scope: "empty_loopback_lab", node_id: "route-qa", installation_id: installation };
 const view: Channel = {
  node_id: "route-qa", installation_id: installation, binary_sha: "b".repeat(64), server_time_unix_milli: Date.now(), reported_at_unix_nano: Date.now() * 1e6, observation_fresh: true, drafts_valid: true,
  status: { instance, serial: 0, observed: stopped() }, controls: { stage: true }, commands: [],
  review: [{ inbound_id: 301, inbound_revision: 2, draft_revision: 3, name: "Direct LAB", listen_ip: "127.0.0.1", direct_port: 26123, tunnel_port: 0 },
   { inbound_id: 302, inbound_revision: 4, draft_revision: 5, name: "Tunnel LAB", listen_ip: "127.0.0.1", direct_port: 0, tunnel_port: 26124 }],
 };
 const state = { scope, view, readError: false, freshPoll: true, malformed: false, post: "normal" as "normal" | "before_commit" | "after_commit" | "conflict" };
 const writes: { path: string; body: CommandRequest | { confirm_execution_may_have_occurred: boolean }; raw: string; csrf: string }[] = [];
 const reads: string[] = [];
 await page.route("**/api/v1/session/status", r => r.fulfill({ json: { authenticated: true, user: { id: role, username: "test-owner", role, representative_id: role === "owner" ? "owner" : "rep-a" }, csrf_token: csrf, expires_unix: 1900000000 } }));
 await page.route("**/api/v1/fleet", r => r.fulfill({ json: { nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0, as_of: new Date().toISOString(), interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } }));
 await page.route("**/api/v1/workspace/**", async route => {
  const req = route.request(), path = new URL(req.url()).pathname;
  if (req.method() === "GET") {
   reads.push(path);
   if (path === "/api/v1/workspace/native-inbounds-state") return route.fulfill({ json: { inbounds: [], nodes: [{ node_id: "route-qa", installation_id: installation, name: "Disposable LAB" }], targets: [], runtime: [] } });
   if (path === "/api/v1/workspace/clients") return route.fulfill({ json: { clients: [] } });
   if (path === "/api/v1/workspace/route-transactions") return route.fulfill({ json: state.scope });
   if (path === apiBase) {
    if (state.readError) return route.fulfill({ status: 503, json: { error: "business_store_unavailable" } });
    view.server_time_unix_milli = Date.now();
    if (state.freshPoll) view.reported_at_unix_nano = Date.now() * 1e6;
    return route.fulfill({ json: { ...view, ...(state.malformed ? { installation_id: "9".repeat(32) } : {}), controls: view.commands.some(c => ["queued", "delivered"].includes(c.state)) ? { stage: false } : view.controls } });
   }
   return route.fulfill({ status: 404, json: { error: "not_found" } });
  }
  writes.push({ path, body: req.postDataJSON(), raw: req.postData()!, csrf: req.headers()["x-csrf-token"] ?? "" });
  if (state.post === "before_commit") return route.fulfill({ status: 503, json: { error: "business_store_unavailable" } });
  if (state.post === "conflict") return route.fulfill({ status: 409, json: { error: "business_event_or_identity_conflict" } });
  if (path.endsWith("/abandon")) {
   view.commands.find(c => c.id === path.split("/").at(-2))!.state = "abandoned_unknown";
   return route.fulfill({ json: { state: "abandoned_unknown", process_stopped: false, requires_fresh_observation: true } });
  }
  if (path === apiBase) {
   const body = req.postDataJSON() as CommandRequest;
   let row = view.commands.find(c => c.id === body.id);
   if (!row) { row = { ...command(view.commands.length + 1, body.action, body.stage_id), id: body.id, stage_id: body.action === "stage" ? body.id : body.stage_id }; view.commands.unshift(row); }
   return state.post === "after_commit" ? route.fulfill({ status: 503, json: { error: "business_store_unavailable" } }) : route.fulfill({ status: 202, json: row });
  }
  return route.fulfill({ status: 404, json: { error: "not_found" } });
 });
 return { state, view, writes, reads };
}
async function openDiagnostics(page: Page) {
 const toggle = page.getByRole("button", { name: "Diagnostics", exact: true });
 await expect(toggle).toBeVisible();
 if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
}
async function openPage(page: Page) {
 await page.goto("/inbounds");
 await openDiagnostics(page);
}
const panel = (page: Page) => page.getByRole("region", { name: "LAB route transactions", exact: true });
const refresh = (page: Page) => panel(page).getByRole("button", { name: "Refresh transactions", exact: true }).click();
async function confirm(page: Page, name: string) { page.once("dialog", d => d.accept()); await panel(page).getByRole("button", { name, exact: true }).click(); }

test("P0-H3 Stage reviews all Node drafts; explicit Apply and Renew keep ACK separate from live observation", async ({ page }) => {
 const f = await fixture(page); await openPage(page); const p = panel(page);
 await expect(p.getByRole("button", { name: "Review and Stage", exact: true })).toBeEnabled();
 await expect(p.getByText("Direct LAB · #301")).toBeVisible(); await expect(p.getByText("Tunnel LAB · #302")).toBeVisible(); expect(f.writes).toHaveLength(0);
 page.once("dialog", d => d.dismiss()); await p.getByRole("button", { name: "Review and Stage", exact: true }).click(); expect(f.writes).toHaveLength(0);
 await confirm(page, "Review and Stage");
 await expect(p.getByText("Queued · not yet delivered", { exact: true })).toBeVisible();
 expect(f.writes[0].body).toMatchObject({ action: "stage", stage_id: "", runtime_instance: instance, expected: { process_ready: false, highwater: 0 }, drafts: [{ inbound_id: 301, inbound_revision: 2, draft_revision: 3 }, { inbound_id: 302, inbound_revision: 4, draft_revision: 5 }] });
 expect(f.writes[0].csrf).toBe(csrf); expect((f.writes[0].body as CommandRequest).id).toMatch(/^[a-f0-9]{32}$/);
 await expect(p.getByRole("button", { name: "Apply staged configuration", exact: true })).toBeDisabled();
 f.view.commands[0] = ack(f.view.commands[0], "staged"); const stageID = f.view.commands[0].id;
 f.view.status.serial = 1; f.view.status.staged_id = stageID; f.view.controls.apply = { stage_id: stageID, ports: [26123, 26124], expires_unix_milli: Date.now() + 60000 };
 await refresh(page); await expect(p.getByRole("button", { name: "Apply staged configuration", exact: true })).toBeEnabled();
 await confirm(page, "Apply staged configuration"); await expect.poll(() => f.writes.length).toBe(2);
 expect(f.writes[1].body).toMatchObject({ action: "apply", stage_id: stageID, drafts: [], expected: { process_ready: false } });
 await expect(p.getByText("Queued · not yet delivered", { exact: true })).toBeVisible();
 f.view.commands[0] = ack(f.view.commands[0], "applied", ready()); f.state.freshPoll = false;
 f.view.reported_at_unix_nano = f.view.commands[0].receipt!.observed_unix_nano - 1e6;
 await refresh(page); await expect(p.getByText("Recorded outcome: Applied", { exact: false })).toBeVisible();
 await expect(p.getByText("Process reported ready · lease current", { exact: true })).toHaveCount(0);
 await expect(p.getByRole("button", { name: "Renew once (30s max)", exact: true })).toBeDisabled();
 f.state.freshPoll = true; f.view.status.observed = ready(); f.view.status.serial = 2; f.view.status.current_stage_id = stageID; delete f.view.status.staged_id; delete f.view.controls.apply; f.view.controls.renew = { stage_id: stageID, ports: [26123, 26124] };
 await refresh(page); await expect(p.getByText("Process reported ready · lease current", { exact: true })).toBeVisible();
 expect(f.writes).toHaveLength(2);
 await confirm(page, "Renew once (30s max)"); await expect.poll(() => f.writes.length).toBe(3);
 expect(f.writes[2].body).toMatchObject({ action: "renew", stage_id: stageID, drafts: [], expected: { process_ready: true, active_sequence: 1, active_sha: digest } });
 expect(f.writes.every(w => w.path === apiBase && w.csrf === csrf)).toBe(true);
});

test("P0-H3 historical Apply ACK after restart cannot imply liveness; Rollback names the saved Stage", async ({ page }) => {
 const f = await fixture(page); f.view.commands = [ack(command(2, "apply"), "applied", ready()), ack(command(1, "stage"), "staged")];
 f.view.status = { instance: "3".repeat(32), serial: 2, current_stage_id: id(1), observed: { ...stopped(), highwater: 1, saved_sequence: 1, saved_sha: digest } };
 f.view.controls.rollback = { stage_id: id(1), ports: [26123] }; await openPage(page); const p = panel(page);
 await expect(p.getByText("Agent reports process not ready", { exact: true })).toBeVisible();
 await expect(p.getByText("Signed receipt verified and stored by HUB", { exact: true })).toHaveCount(2);
 await expect(p.getByRole("button", { name: "Renew once (30s max)", exact: true })).toBeDisabled();
 await confirm(page, "Rollback to saved configuration"); await expect.poll(() => f.writes.length).toBe(1);
 expect(f.writes[0].body).toMatchObject({ action: "rollback", stage_id: id(1), runtime_instance: "3".repeat(32), drafts: [], expected: { process_ready: false, saved_sha: digest, highwater: 1 } });
});

for (const storageAvailable of [true, false]) {
test(storageAvailable ? "P0-H3 ambiguous request survives reload and retries identical bytes; no automatic POST" : "P0-H3 ambiguous request survives hiding Diagnostics when browser storage is unavailable", async ({ page }) => {
 const f = await fixture(page); f.state.post = "before_commit"; await openPage(page);
 if (!storageAvailable) await page.evaluate(() => {
  const setItem = Storage.prototype.setItem;
  Storage.prototype.setItem = function (key: string, value: string) {
   if (this === sessionStorage && key.startsWith("dark-pro-route-attempt:")) throw new DOMException("Fixture storage unavailable", "QuotaExceededError");
   return setItem.call(this, key, value);
  };
 });
 await confirm(page, "Review and Stage"); await expect(panel(page).getByRole("button", { name: "Retry same request", exact: true })).toBeEnabled();
 expect(f.writes).toHaveLength(1); const first = f.writes[0].raw;
 if (!storageAvailable) expect(await page.evaluate(() => Object.keys(sessionStorage).filter(k => k.startsWith("dark-pro-route-attempt:")).length)).toBe(0);
 await page.getByRole("button", { name: "Diagnostics", exact: true }).click();
 await expect(panel(page)).toBeHidden();
 await openDiagnostics(page);
 await expect(panel(page).getByRole("button", { name: "Retry same request", exact: true })).toBeEnabled();
 expect(f.writes).toHaveLength(1);
 if (storageAvailable) { await page.reload(); await openDiagnostics(page); await expect(panel(page).getByRole("button", { name: "Retry same request", exact: true })).toBeEnabled(); }
 expect(f.writes).toHaveLength(1); await expect(panel(page).getByRole("button", { name: "Review and Stage", exact: true })).toBeDisabled();
 f.state.post = "normal"; await confirm(page, "Retry same request");
 await expect(panel(page).getByText("Queued · not yet delivered", { exact: true })).toBeVisible();
 expect(f.writes).toHaveLength(2); expect(f.writes[1].raw).toBe(first);
 await expect(panel(page).getByRole("button", { name: "Retry same request", exact: true })).toHaveCount(0);
 expect(await page.evaluate(() => Object.keys(sessionStorage).filter(k => k.startsWith("dark-pro-route-attempt:")).length)).toBe(0);
});
}

test("P0-H3 lost reply after HUB commit reconciles by ID; conflicts require a new reviewed action", async ({ page }) => {
 const f = await fixture(page); f.state.post = "after_commit"; await openPage(page); await confirm(page, "Review and Stage");
 await expect(panel(page).getByText("Queued · not yet delivered", { exact: true })).toBeVisible();
 await expect(panel(page).getByRole("button", { name: "Retry same request", exact: true })).toHaveCount(0); expect(f.writes).toHaveLength(1);
 f.view.commands[0].state = "abandoned_unknown"; f.state.post = "conflict";
 await refresh(page); await confirm(page, "Review and Stage");
 await expect(panel(page).getByText("State changed or the request was refused. Review the refreshed state before creating another command.")).toBeVisible();
 await expect(panel(page).getByRole("button", { name: "Retry same request", exact: true })).toHaveCount(0);
 expect(f.writes).toHaveLength(2); expect(f.writes[0].raw).not.toBe(f.writes[1].raw);
});

test("P0-H3 delivered command and abandonment never claim a stopped process; late ACK stays historical", async ({ page }) => {
 const f = await fixture(page); const pending = command(2, "apply"); pending.state = "delivered";
 f.view.commands = [pending]; f.view.status.observed = ready(); f.view.status.serial = 1;
 await openPage(page); const p = panel(page);
 await expect(p.getByText("Observation stale · current process state unconfirmed", { exact: true })).toBeVisible();
 page.once("dialog", d => d.dismiss()); await p.getByRole("button", { name: "Abandon pending command", exact: true }).click(); expect(f.writes).toHaveLength(0);
 await confirm(page, "Abandon pending command"); await expect(p.getByText("Command abandoned with execution unknown. This does not stop Xray; a fresh Agent observation is still required.")).toBeVisible();
 expect(f.writes[0].body).toEqual({ confirm_execution_may_have_occurred: true }); expect(f.writes[0].path).toBe(apiBase + "/" + id(2) + "/abandon");
 f.view.commands[0] = { ...ack(pending, "applied", ready()), state: "abandoned_unknown" }; f.view.status.serial = 2;
 await refresh(page); await expect(p.getByText("Abandoned · execution unknown", { exact: true })).toBeVisible();
 await expect(p.getByText("Signed receipt verified and stored by HUB", { exact: true })).toBeVisible();
 await expect(p.getByText("Agent reports process not ready", { exact: true })).toHaveCount(0);
});

test("P0-H3 lease expiry uses server time despite browser clock skew and never auto-renews", async ({ page }) => {
 const f = await fixture(page); f.view.status.observed = ready(1, 2500); f.view.status.current_stage_id = id(1); f.view.controls.renew = { stage_id: id(1), ports: [26123] };
 await page.addInitScript(() => { const real = Date.now; Date.now = () => real() - 86400000; });
 await openPage(page); const p = panel(page);
 await expect(p.getByText("Process reported ready · lease current", { exact: true })).toBeVisible();
 await expect(p.getByText("Lease expired · current process state unconfirmed", { exact: true })).toBeVisible();
 await expect(p.getByRole("button", { name: "Renew once (30s max)", exact: true })).toBeDisabled(); expect(f.writes).toHaveLength(0);
 f.state.readError = true; await refresh(page);
 await expect(p.getByRole("alert")).toContainText("Controls are blocked"); await expect(p.getByRole("button", { name: "Review and Stage", exact: true })).toBeDisabled();
});

test("P0-H3 scope mismatch, missing drafts and disabled capability block commands", async ({ page }) => {
 const f = await fixture(page); f.state.malformed = true; await openPage(page); const p = panel(page);
 await expect(p.getByRole("alert")).toBeVisible(); await expect(p.getByRole("button", { name: "Review and Stage", exact: true })).toHaveCount(0);
 f.state.malformed = false; f.view.drafts_valid = false; f.view.review[1].draft_revision = 0; f.view.controls.stage = false; await refresh(page);
 await expect(p.getByText("Save a valid Direct/Tunnel draft for every enabled Inbound before Stage.")).toBeVisible(); await expect(p.getByRole("button", { name: "Review and Stage", exact: true })).toBeDisabled();
 f.state.scope.enabled = false; await refresh(page);
 await expect(p.getByText("Route transactions are disabled on this HUB.")).toBeVisible(); await expect(p.getByRole("button", { name: "Review and Stage", exact: true })).toHaveCount(0); expect(f.writes).toHaveLength(0);
});

test("P0-H3 Persian 320px history and controls stay readable without overflow", async ({ page }) => {
 const f = await fixture(page); f.view.commands = [{ ...command(1, "stage"), state: "abandoned_unknown" }];
 await page.setViewportSize({ width: 320, height: 780 }); await openPage(page);
 await page.getByRole("button", { name: "Switch language" }).click(); const p = panel(page);
 await expect(p.getByRole("heading", { name: "تراکنش‌های مسیر LAB" })).toBeVisible();
 await p.getByText("جزئیات حسابرسی", { exact: true }).click();
 await expect(p.getByText("رهاشده · نتیجهٔ اجرا نامشخص", { exact: true })).toBeVisible();
 await expect(p.getByRole("button", { name: "بررسی و Stage", exact: true })).toBeEnabled();
 expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true); expect(f.writes).toHaveLength(0);
 await p.screenshot({ path: "test-results/route-transactions-fa-320.png" });
});

test("P0-H3 representatives cannot discover the owner transaction scope", async ({ page }) => {
 const f = await fixture(page, "representative"); await page.goto("/inbounds");
 await expect(page).toHaveURL(/\/clients$/); await expect(panel(page)).toHaveCount(0);
 expect(f.reads.some(path => path.includes("route-transactions"))).toBe(false); expect(f.writes).toHaveLength(0);
});
