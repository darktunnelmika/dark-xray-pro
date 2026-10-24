import { expect, test, type Page } from "@playwright/test";
import type { CreationPlan } from "../src/InboundCreationModel";
import type { PromotionInput, PromotionPreview, PromotionReceipt, PromotionRequest, PromotionIssue } from "../src/inboundPlanExecution";
import type { Channel, CommandRequest } from "../src/meteredRouteTransactions";

// Disposable synthetic UI fixtures only. No production service or Node is contacted.
const inboundID = 8123, csrf = "a".repeat(64), installation = "1".repeat(32), digest = "b".repeat(64);
const base = `/api/v1/workspace/native-inbound-plans/${inboundID}`, promotion = base + "/promotion";
const routeBase = "/api/v1/workspace/native-nodes/promotion-qa/metered-route-transactions";
function savedPlan(): CreationPlan {
  return { profile: { id: inboundID, name: "Saved promotion plan", revision: 1, enabled: false, archived: false, bound_clients: 0, transport: "tcp", grpc_service: "", reality_target: "example.org:443", server_names: ["example.org"], short_ids: ["aabb"], public_key: "SYNTHETIC_PUBLIC_KEY" },
    nodes: [{ node_id: "promotion-qa", installation_id: installation, listen_ip: "127.0.0.1", direct_port: 19666, direct_hosts: ["127.0.0.1"], tunnel_port: 0, tunnel_hosts: [], swap_enabled: false, swap_source_node_id: "", swap_source_installation_id: "", swap_source_listen_ip: "", swap_source_port: 0, swap_entry_address: "", swap_entry_port: 0, swap_destination_port: 0 }],
    plan_revision: 1, inbound_revision: 1, status: "SAVED_DISABLED", runtime_apply: false, editable: true, created_unix: 1800000000, updated_unix: 1800000000, last_event_id: "saved-plan-fixture-0001", reason: "Saved complete plan" };
}
type Options = { language?: "en" | "fa"; available?: boolean; blockers?: PromotionIssue[]; legacy?: boolean; role?: string;
  tamperPreview?: (preview: PromotionPreview) => void; tamperReceipt?: (receipt: PromotionReceipt) => void;
  error?: { field: string; code: string }; promoted?: boolean };
async function fixture(page: Page, options: Options = {}) {
  const plan = savedPlan();
  const clients = Array.from({ length: 28 }, (_, index) => ({ id: `promotion-client-${String(index).padStart(2, "0")}`, name: `Disposable client ${String(index).padStart(2, "0")}`, representative_id: "owner", archived: false, deployment_state: "pending_node_apply", expires_unix: 1900000000 }));
  clients[24].archived = true; clients[25].deployment_state = "disabled";
  const writes: Array<{ path: string; raw: string; body: any; csrf: string }> = [], reads: string[] = [];
  const state = { available: options.available !== false, blockers: options.blockers ?? [], mode: "normal" as "normal" | "before_commit" | "after_commit", error: options.error, receipt: undefined as PromotionReceipt | undefined, fresh: true };
  const channel: Channel = { node_id: "promotion-qa", installation_id: installation, binary_sha: digest, reported_at_unix_nano: Date.now() * 1e6, server_time_unix_milli: Date.now(), observation_fresh: true, drafts_valid: true,
    status: { instance: "2".repeat(32), serial: 0, usage: { settlement: { epoch_id: "", sequence: "0", sha256: "" } }, observed: { highwater: 0, saved_sequence: 0, saved_sha: "", active_sequence: 0, active_sha: "", process_ready: false, phase: "awaiting_fresh_lease", lease_expires_unix_nano: 0, data_plane_verified: false, subscription_published: false, no_listeners: true } },
    review: [{ inbound_id: inboundID, inbound_revision: 2, draft_revision: 1, name: plan.profile.name, listen_ip: "127.0.0.1", direct_port: 19666, tunnel_port: 0 }], controls: { stage: true }, commands: [] };
  const receiptFor = (input: PromotionInput): PromotionReceipt => ({ event_id: input.event_id, inbound_id: inboundID, plan_revision: 1, source_inbound_revision: 1, inbound_revision: 2, node_id: "promotion-qa", installation_id: installation, draft_revision: 1, client_ids: [...input.client_ids].sort(), review_sha256: digest, promoted: true, runtime_changed: false, subscription_published: false, created_unix: 1800000002, replayed: false });
  if (options.promoted) state.receipt = receiptFor({ event_id: "prior-promotion-fixture", expected_plan_revision: 1, expected_inbound_revision: 1, client_ids: [clients[0].id], reason: "Existing reviewed preparation" });
  await page.addInitScript(language => { localStorage.setItem("dark-pro-lang", language); localStorage.setItem("dark-pro-theme", "dark"); }, options.language ?? "en");
  await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user: { id: options.role ?? "owner", username: "test-owner", role: options.role ?? "owner", representative_id: "owner" }, csrf_token: csrf, expires_unix: 1900000000 } }));
  await page.route("**/api/v1/fleet", route => route.fulfill({ json: { nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0, as_of: new Date().toISOString(), interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } }));
  await page.route("**/api/v1/workspace/**", async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (request.method() === "GET") {
      reads.push(path);
      if (path === "/api/v1/workspace/native-inbounds-state") return route.fulfill({ json: { inbounds: [{ ...plan.profile, ...(state.receipt ? { enabled: true, revision: 2, bound_clients: state.receipt.client_ids.length } : {}) }], nodes: [{ node_id: "promotion-qa", installation_id: installation, name: "Disposable promotion Node" }, { node_id: "other-qa", installation_id: "9".repeat(32), name: "Other approved Node" }], targets: state.receipt ? [{ inbound_id: inboundID, node_id: "promotion-qa", installation_id: installation, listen_ip: "127.0.0.1", direct_port: 19666, swap_port: 0 }] : [], runtime: [] } });
      if (path === "/api/v1/workspace/native-inbound-plans/capabilities") return route.fulfill({ json: { available: true, version: 3, protocols: ["vless"], securities: ["reality", "tls"], transports: ["tcp", "grpc", "xhttp", "ws", "httpupgrade"], runtime_apply: false, profile_options: true, max_nodes: 64, max_hosts_per_node: 64, plans: [{ inbound_id: inboundID, plan_revision: plan.plan_revision, inbound_revision: plan.inbound_revision, status: plan.status, node_ids: ["promotion-qa"], promotion: state.receipt }] } });
      if (path === base) return route.fulfill({ json: { plan: { ...plan, ...(state.receipt ? { promotion: state.receipt, editable: false, edit_blocker: "plan_promoted" } : {}) }, references_current: true } });
      if (path === promotion) return options.legacy ? route.fulfill({ status: 404, json: { error: "not_found" } }) : route.fulfill({ json: { available: state.available, scope: "single_direct_tcp_reality_loopback", promoted: !!state.receipt, blockers: state.blockers, receipt: state.receipt } });
      if (path === "/api/v1/workspace/clients") {
        const q = url.searchParams.get("q") || "", offset = Number(url.searchParams.get("offset") || "0"), limit = Number(url.searchParams.get("limit") || "25");
        const filtered = clients.filter(client => `${client.name} ${client.id}`.includes(q));
        return route.fulfill({ json: { clients: filtered.slice(offset, offset + limit), total: filtered.length, offset, limit, has_more: offset + limit < filtered.length } });
      }
      if (path === "/api/v1/workspace/metered-route-transactions") return route.fulfill({ json: { enabled: true, scope: "metered_tcp_loopback_lab", node_id: "other-qa", installation_id: "9".repeat(32), nodes: [{ node_id: "other-qa", installation_id: "9".repeat(32) }, { node_id: "promotion-qa", installation_id: installation }] } });
      if (path === routeBase) {
        channel.server_time_unix_milli = Date.now(); if (state.fresh) channel.reported_at_unix_nano = Date.now() * 1e6;
        return route.fulfill({ json: channel });
      }
      if (path.endsWith("/operations")) return route.fulfill({ json: { operations: [] } });
      if (path.endsWith("/clients")) return route.fulfill({ json: { client_ids: state.receipt?.client_ids ?? [] } });
      return route.fulfill({ status: 404, json: { error: "not_found" } });
    }
    const body = request.postDataJSON(), raw = request.postData()!;
    writes.push({ path, raw, body, csrf: request.headers()["x-csrf-token"] ?? "" });
    if (path === promotion + "/preview") {
      if (state.error) return route.fulfill({ status: 409, json: { error: "business_event_or_identity_conflict", issue: state.error } });
      const preview: PromotionPreview = { eligible: true, scope: "single_direct_tcp_reality_loopback", inbound_id: inboundID, plan_revision: 1, inbound_revision: 1, node_id: "promotion-qa", installation_id: installation, direct_port: 19666, direct_hosts: ["127.0.0.1"], client_ids: [...body.client_ids].sort(), review_sha256: digest, blockers: [], runtime_changed: false, subscription_published: false };
      options.tamperPreview?.(preview); return route.fulfill({ json: preview });
    }
    if (path === promotion) {
      if (!body.confirmed || body.review_sha256 !== digest) return route.fulfill({ status: 422, json: { error: "business_invalid" } });
      if (state.error) return route.fulfill({ status: 409, json: { error: "business_event_or_identity_conflict", issue: state.error } });
      if (state.mode === "before_commit") return route.fulfill({ status: 503, json: { error: "response_lost" } });
      if (options.tamperReceipt) { const wrong = receiptFor(body); options.tamperReceipt(wrong); return route.fulfill({ json: wrong }); }
      const replayed = !!state.receipt; state.receipt ??= receiptFor(body);
      return state.mode === "after_commit" ? route.fulfill({ status: 500, json: { error: "response_lost" } }) : route.fulfill({ status: replayed ? 200 : 201, json: { ...state.receipt, replayed } });
    }
    if (path === routeBase) {
      const input = body as CommandRequest;
      const command = { id: input.id, node_id: channel.node_id, installation_id: installation, action: input.action, stage_id: input.action === "stage" ? input.id : input.stage_id, serial: 1, state: "queued" as const, created_unix_nano: Date.now() * 1e6 };
      channel.commands.push(command); channel.controls = { stage: false };
      return route.fulfill({ status: 202, json: command });
    }
    return route.fulfill({ status: 405, json: { error: "unexpected_mutation" } });
  });
  return { plan, clients, state, channel, writes, reads };
}
const panel = (page: Page) => page.getByRole("region", { name: "Inbound LAB preparation", exact: true });
async function open(page: Page, language: "en" | "fa" = "en") {
  await page.goto(`/inbounds?inbound=${inboundID}&tab=overview`);
  await panel(page).getByRole("button", { name: language === "fa" ? "آماده‌سازی اجرای LAB" : "Prepare LAB execution", exact: true }).click();
}
async function review(page: Page) {
  await panel(page).getByLabel("Select LAB client: promotion-client-00", { exact: true }).check();
  await panel(page).getByLabel("LAB preparation reason", { exact: true }).fill("Prepare the saved LAB plan");
  await panel(page).getByRole("button", { name: "Review LAB preparation", exact: true }).click();
  await expect(panel(page).getByRole("button", { name: "Prepare reviewed plan", exact: true })).toBeEnabled();
}
async function prepare(page: Page) {
  page.once("dialog", dialog => dialog.accept());
  await panel(page).getByRole("button", { name: "Prepare reviewed plan", exact: true }).click();
}

test("saved plan preparation binds exactly the reviewed clients, preserves the authoring record and reuses pinned metered controls", async ({ page }) => {
  const f = await fixture(page); await open(page); await review(page);
  expect(f.writes).toHaveLength(1);
  expect(f.writes[0].body).toEqual(expect.objectContaining({ expected_plan_revision: 1, expected_inbound_revision: 1, client_ids: ["promotion-client-00"], reason: "Prepare the saved LAB plan" }));
  expect(f.writes[0].body).not.toHaveProperty("confirmed"); expect(f.writes[0].body).not.toHaveProperty("review_sha256");
  page.once("dialog", dialog => dialog.dismiss()); await panel(page).getByRole("button", { name: "Prepare reviewed plan", exact: true }).click(); expect(f.writes).toHaveLength(1);
  await prepare(page);
  await expect(panel(page).locator('[data-promotion-state="prepared"]')).toBeVisible();
  expect(f.writes).toHaveLength(2); expect(f.writes[1].body).toEqual({ ...f.writes[0].body, review_sha256: digest, confirmed: true }); expect(f.writes.every(write => write.csrf === csrf)).toBe(true);
  const runtime = page.getByRole("region", { name: "Metered TCP LAB transactions", exact: true });
  await expect(runtime.getByRole("button", { name: "Review and Stage", exact: true })).toBeEnabled();
  await expect(runtime.getByRole("combobox", { name: "Approved Node", exact: true })).toHaveCount(0);
  expect(f.reads).not.toContain("/api/v1/workspace/native-nodes/other-qa/metered-route-transactions");
  await expect(page.getByRole("button", { name: "Archive saved setup", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Enable Inbound", exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Edit profile", exact: true })).toHaveCount(0);
  await page.getByRole("tab", { name: "Routes", exact: true }).click(); await expect(page.getByRole("button", { name: "Add Node target", exact: true })).toHaveCount(0);
  await page.getByRole("tab", { name: "Clients", exact: true }).click(); await expect(page.getByRole("heading", { name: "Prepared client bindings", exact: true })).toBeVisible(); await expect(page.getByRole("button", { name: "Attach selected", exact: true })).toHaveCount(0);
  await page.getByRole("tab", { name: "Overview", exact: true }).click();
  await page.getByRole("button", { name: "Open saved setup", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await expect(dialog.getByRole("tab")).toHaveText(["Connection", "Dark Swap", "Advanced", "Review"]); await expect(dialog.getByLabel("Inbound name", { exact: true })).toBeDisabled();
  await expect(dialog.getByText(/original configuration is retained read-only/)).toBeVisible(); await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await page.reload(); await expect(panel(page).locator('[data-promotion-state="prepared"]')).toBeVisible(); expect(f.writes).toHaveLength(2);
  await page.getByRole("button", { name: "Diagnostics", exact: true }).click(); await expect(page.getByRole("region", { name: "Metered TCP LAB transactions", exact: true })).toHaveCount(1);
  page.once("dialog", dialog => dialog.accept()); await runtime.getByRole("button", { name: "Review and Stage", exact: true }).click();
  await expect.poll(() => f.writes.length).toBe(3); expect(f.writes[2].path).toBe(routeBase); expect(f.writes[2].body.drafts).toEqual([{ inbound_id: inboundID, inbound_revision: 2, draft_revision: 1 }]);
  await expect(runtime.getByText("Queued · not yet delivered", { exact: true })).toBeVisible(); await expect(runtime.getByText("Process reported ready · lease current", { exact: true })).toHaveCount(0);
  expect(f.writes.filter(write => write.body.action === "apply")).toHaveLength(0);
});

test("client intent survives pagination and search, and every edit invalidates the reviewed confirmation", async ({ page }) => {
  const f = await fixture(page); await open(page);
  await panel(page).getByLabel("Select LAB client: promotion-client-00", { exact: true }).check();
  await expect(panel(page).getByLabel("Select LAB client: promotion-client-24", { exact: true })).toBeDisabled();
  await panel(page).getByRole("button", { name: "More clients", exact: true }).click();
  await expect(panel(page).getByLabel("Select LAB client: promotion-client-25", { exact: true })).toBeDisabled();
  await panel(page).getByLabel("Select LAB client: promotion-client-26", { exact: true }).check();
  await panel(page).getByLabel("Search clients for LAB execution", { exact: true }).fill("client-03"); await panel(page).getByRole("button", { name: "Search clients", exact: true }).click();
  await panel(page).getByLabel("Select LAB client: promotion-client-03", { exact: true }).check();
  await expect(panel(page).getByRole("heading", { name: "Selected clients (3)", exact: true })).toBeVisible();
  await panel(page).getByLabel("LAB preparation reason", { exact: true }).fill("Cross-page review"); await panel(page).getByRole("button", { name: "Review LAB preparation", exact: true }).click();
  await expect(panel(page).locator("[data-promotion-review]")).toBeVisible(); expect(f.writes[0].body.client_ids).toEqual(["promotion-client-00", "promotion-client-03", "promotion-client-26"]);
  await panel(page).getByRole("button", { name: "Remove selected client: promotion-client-00", exact: true }).click(); await expect(panel(page).getByRole("button", { name: "Prepare reviewed plan", exact: true })).toHaveCount(0);
  expect(f.writes).toHaveLength(1);
});

test("valid client IDs that match Object prototype names require explicit selection", async ({ page }) => {
  const f = await fixture(page); f.clients[0].id = "constructor"; f.clients[1].id = "toString";
  await open(page);
  for (const id of ["constructor", "toString"]) { const checkbox = panel(page).getByLabel(`Select LAB client: ${id}`, { exact: true }); await expect(checkbox).not.toBeChecked(); await checkbox.check(); }
  await expect(panel(page).getByRole("heading", { name: "Selected clients (2)", exact: true })).toBeVisible();
  await panel(page).getByRole("button", { name: "Remove selected client: constructor", exact: true }).click(); await expect(panel(page).getByLabel("Select LAB client: constructor", { exact: true })).not.toBeChecked();
  await panel(page).getByLabel("LAB preparation reason", { exact: true }).fill("Explicit valid IDs"); await panel(page).getByRole("button", { name: "Review LAB preparation", exact: true }).click(); await expect(panel(page).locator("[data-promotion-review]")).toBeVisible();
  expect(f.writes[0].body.client_ids).toEqual(["toString"]);
});

for (const [code, available, text] of [
  ["promotion_disabled", false, "LAB preparation is disabled on this HUB"],
  ["node_not_approved", false, "This Node is not approved"],
  ["promotion_transport_unsupported", true, "This phase supports TCP / REALITY only"],
  ["promotion_direct_only", true, "Tunnel and Dark Swap paths cannot execute"],
  ["promotion_endpoints_unsupported", true, "Use simple Direct Hosts for this phase"],
  ["promotion_observation_stale", true, "A fresh signed observation"],
] as const) test(`preparation blocker ${code} preserves the setup and sends no mutation`, async ({ page }) => {
  const f = await fixture(page, { available, blockers: [{ field: "nodes", code }] });
  await page.goto(`/inbounds?inbound=${inboundID}`); await expect(panel(page).getByText(new RegExp(text))).toBeVisible();
  await expect(panel(page).getByRole("button", { name: "Prepare LAB execution", exact: true })).toHaveCount(0);
  expect(f.writes).toHaveLength(0); expect(f.reads.filter(path => path === "/api/v1/workspace/clients")).toHaveLength(0);
  await expect(page.getByRole("button", { name: "Open saved setup", exact: true })).toHaveCount(1);
  await panel(page).getByRole("button", { name: "Review required settings", exact: true }).click(); await expect(page.getByRole("dialog").getByLabel("Inbound name", { exact: true })).toBeEnabled();
});

test("an older server never exposes promotion mutations", async ({ page }) => {
  const f = await fixture(page, { legacy: true }); await page.goto(`/inbounds?inbound=${inboundID}`);
  await expect(panel(page).getByText("This server does not provide saved-plan execution preparation.", { exact: true })).toBeVisible(); expect(f.writes).toHaveLength(0);
});

test("representatives cannot discover or prepare a saved plan for execution", async ({ page }) => {
  const f = await fixture(page, { role: "representative" }); await page.goto(`/inbounds?inbound=${inboundID}`);
  await expect(page).toHaveURL(/\/clients$/); await expect(panel(page)).toHaveCount(0);
  expect(f.reads.filter(path => path.startsWith(base))).toHaveLength(0); expect(f.writes).toHaveLength(0);
});

for (const [field, code, tab, label] of [
  ["listen_ip", "promotion_loopback_required", "Dark Swap", "Destination listening IP: promotion-qa"],
  ["profile.transport", "promotion_transport_unsupported", "Connection", "TCP / REALITY"],
] as const) test(`preparation blocker ${field} opens and focuses its existing editor control`, async ({ page }) => {
  const f = await fixture(page, { blockers: [{ field, code, ...(field === "listen_ip" ? { node_id: "promotion-qa" } : {}) }] });
  await page.goto(`/inbounds?inbound=${inboundID}`); await panel(page).getByRole("button", { name: "Review required settings", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Edit Inbound setup", exact: true });
  await expect(dialog.getByRole("tab", { name: tab, exact: true })).toHaveAttribute("aria-selected", "true"); await expect(dialog.getByLabel(label, { exact: true })).toBeFocused(); expect(f.writes).toHaveLength(0);
});

for (const [name, mutate] of [
  ["different listener", (value: PromotionPreview) => { value.direct_port++; }],
  ["different client set", (value: PromotionPreview) => { value.client_ids = ["promotion-client-01"]; }],
  ["runtime activation claim", (value: PromotionPreview) => { (value as any).runtime_changed = true; }],
  ["private material", (value: PromotionPreview) => { (value as any).private_key = "synthetic-secret"; }],
] as const) test(`promotion review rejects ${name}`, async ({ page }) => {
  const f = await fixture(page, { tamperPreview: mutate }); await open(page);
  await panel(page).getByLabel("Select LAB client: promotion-client-00", { exact: true }).check(); await panel(page).getByLabel("LAB preparation reason", { exact: true }).fill("Validate exact review");
  await panel(page).getByRole("button", { name: "Review LAB preparation", exact: true }).click(); await expect(panel(page).getByRole("alert")).toBeVisible(); await expect(panel(page).getByRole("button", { name: "Prepare reviewed plan", exact: true })).toHaveCount(0); expect(f.writes).toHaveLength(1);
});

for (const [name, mutate] of [
  ["different clients", (value: PromotionReceipt) => { value.client_ids = ["promotion-client-01"]; }],
  ["wrong source revision", (value: PromotionReceipt) => { value.source_inbound_revision = 2; value.inbound_revision = 3; }],
  ["publication claim", (value: PromotionReceipt) => { (value as any).subscription_published = true; }],
] as const) test(`promotion retains the exact request after a receipt with ${name}`, async ({ page }) => {
  const f = await fixture(page, { tamperReceipt: mutate }); await open(page); await review(page); await prepare(page);
  await expect(panel(page).getByRole("button", { name: "Retry exact preparation", exact: true })).toBeEnabled(); await expect(panel(page).locator('[data-promotion-state="prepared"]')).toHaveCount(0); expect(f.writes).toHaveLength(2);
});

test("an uncertain preparation survives reload and retries identical bytes only on explicit action", async ({ page }) => {
  const f = await fixture(page); f.state.mode = "before_commit"; await open(page); await review(page); await prepare(page);
  await expect(panel(page).getByRole("button", { name: "Retry exact preparation", exact: true })).toBeEnabled(); const original = f.writes[1].raw;
  await page.reload(); await expect(panel(page).getByRole("button", { name: "Retry exact preparation", exact: true })).toBeEnabled(); expect(f.writes).toHaveLength(2);
  f.state.mode = "normal"; page.once("dialog", dialog => dialog.accept()); await panel(page).getByRole("button", { name: "Retry exact preparation", exact: true }).click();
  await expect(panel(page).locator('[data-promotion-state="prepared"]')).toBeVisible(); expect(f.writes[2].raw).toBe(original); expect(f.writes[2].csrf).toBe(csrf);
  expect(await page.evaluate(() => Object.keys(sessionStorage).filter(key => key.startsWith("dark-pro-plan-promotion:")))).toEqual([]);
});

test("a committed preparation with a lost reply reconciles from its exact receipt without another POST", async ({ page }) => {
  const f = await fixture(page); f.state.mode = "after_commit"; await open(page); await review(page); await prepare(page);
  await expect(panel(page).getByRole("button", { name: "Retry exact preparation", exact: true })).toBeEnabled();
  await panel(page).getByRole("button", { name: "Refresh preparation", exact: true }).click(); await expect(panel(page).locator('[data-promotion-state="prepared"]')).toBeVisible(); expect(f.writes).toHaveLength(2);
});

test("authoritative client changes reject a reviewed commit and require another review", async ({ page }) => {
  const f = await fixture(page); await open(page); await review(page); f.state.error = { field: "client_ids", code: "promotion_client_ineligible" }; await prepare(page);
  await expect(panel(page).getByText("A selected client is expired, disabled or lacks eligible account or usage state.", { exact: true })).toBeVisible(); await expect(panel(page).getByRole("heading", { name: "Selected clients (1)", exact: true })).toBeFocused();
  await expect(panel(page).getByRole("button", { name: "Prepare reviewed plan", exact: true })).toHaveCount(0); await expect(panel(page).getByRole("button", { name: "Retry exact preparation", exact: true })).toHaveCount(0); expect(f.writes).toHaveLength(2);
});

test("a tab that cannot retain the reviewed request sends no promotion commit", async ({ page }) => {
  const f = await fixture(page); await open(page); await review(page);
  await page.evaluate(() => { const original = Storage.prototype.setItem; Storage.prototype.setItem = function(key, value) { if (key.startsWith("dark-pro-plan-promotion:")) throw Error("synthetic storage failure"); original.call(this, key, value); }; });
  await prepare(page); await expect(panel(page).getByText("The exact request could not be retained in this tab. No new preparation request was sent.", { exact: true })).toBeVisible(); expect(f.writes).toHaveLength(1);
});

test("a promoted plan never stages a different Node draft set and remains prepared when discovery is disabled", async ({ page }) => {
  const f = await fixture(page, { promoted: true, available: false, blockers: [{ field: "promotion", code: "promotion_disabled" }] });
  f.channel.review.push({ ...f.channel.review[0], inbound_id: 9999 });
  await page.goto(`/inbounds?inbound=${inboundID}`); await expect(panel(page).locator('[data-promotion-state="prepared"]')).toBeVisible();
  const runtime = page.getByRole("region", { name: "Metered TCP LAB transactions", exact: true }); await expect(runtime.getByRole("button", { name: "Review and Stage", exact: true })).toBeDisabled();
  await expect(runtime.getByText(/Node draft set no longer matches/)).toBeVisible(); expect(f.writes).toHaveLength(0);
  await page.getByRole("button", { name: "Back to Inbounds", exact: true }).click(); await page.getByRole("combobox", { name: "Status", exact: true }).selectOption("promoted");
  await expect(page.getByRole("row").filter({ hasText: "Saved promotion plan" }).getByText("Prepared for LAB", { exact: true })).toBeVisible();
});

for (const language of ["en", "fa"] as const) test(`preparation, confirmation and prepared status fit 320px ${language}`, async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 320, height: 740 }); const f = await fixture(page, { language }); await open(page, language);
  await panel(page).getByLabel("Select LAB client: promotion-client-00", { exact: true }).check();
  await panel(page).getByLabel(language === "fa" ? "دلیل آماده‌سازی LAB" : "LAB preparation reason", { exact: true }).fill(language === "fa" ? "آماده‌سازی طرح آزمایشی" : "Prepare disposable plan");
  await panel(page).getByRole("button", { name: language === "fa" ? "بررسی آماده‌سازی LAB" : "Review LAB preparation", exact: true }).click(); await expect(panel(page).locator("[data-promotion-review]")).toBeVisible();
  await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await panel(page).locator("[data-promotion-review]").scrollIntoViewIfNeeded(); await page.screenshot({ path: testInfo.outputPath(`promotion-review-${language}-320.png`) });
  page.once("dialog", dialog => dialog.accept()); await panel(page).getByRole("button", { name: language === "fa" ? "آماده‌سازی طرح بررسی‌شده" : "Prepare reviewed plan", exact: true }).click();
  await expect(panel(page).locator('[data-promotion-state="prepared"]')).toBeVisible(); await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await panel(page).scrollIntoViewIfNeeded(); await page.screenshot({ path: testInfo.outputPath(`promotion-prepared-${language}-320.png`) }); expect(f.writes).toHaveLength(2);
});
