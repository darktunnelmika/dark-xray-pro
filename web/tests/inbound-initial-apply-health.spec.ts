import { expect, test, type Page } from "@playwright/test";
import type { PromotionReceipt } from "../src/inboundPlanExecution";
import { initialHealthBinding, readInitialHealthPreparation, readInitialHealthRound, readInitialHealthSnapshot, readInitialHealthState, type InitialHealthAttempt, type InitialHealthRound } from "../src/inboundInitialApplyHealth";
import { readInitialLabSnapshot, readLabSnapshot, readSubscription } from "../src/labSubscriptions";
import { readScope } from "../src/meteredRouteTransactions";

// Disposable browser fixtures. These requests never reach a live HUB or Agent.
const inboundID = 8123, csrf = "a".repeat(64), installation = "1".repeat(32), digest = "b".repeat(64), stageID = "2".repeat(32), applyID = "3".repeat(32), epochID = "4".repeat(32);
const base = `/api/v1/workspace/native-inbound-plans/${inboundID}`, healthPath = base + "/initial-health", routePath = "/api/v1/workspace/native-nodes/promotion-qa/metered-route-transactions";
const clients = ["promotion-client-0", "promotion-client-1"], token = "A".repeat(43);
const promotion: PromotionReceipt = { event_id: "active-promotion-commit-0001", inbound_id: inboundID, plan_revision: 1, source_inbound_revision: 1, inbound_revision: 2, node_id: "promotion-qa", installation_id: installation, draft_revision: 1, client_ids: clients, review_sha256: digest, promoted: true, runtime_changed: false, subscription_published: false, created_unix: 1800000002, replayed: false };
const binding = { initial_apply_sha: digest, apply_command_id: applyID, stage_id: stageID, epoch_id: epochID };
function preparation() { return { event_id: "5".repeat(32), promotion_event_id: promotion.event_id, inbound_id: inboundID, node_id: promotion.node_id, installation_id: installation, prepared: true as const, created_unix_nano: Date.now() * 1e6, replayed: false }; }
function acceptedRound(id = "6".repeat(32), at = Date.now()): InitialHealthRound {
 return { id, scope: "lab_initial_apply", inbound_id: inboundID, promotion_event_id: promotion.event_id, node_id: promotion.node_id, installation_id: installation, ...binding, endpoint_sha: "c".repeat(64), manifest_sha: "d".repeat(64), created_unix_nano: at * 1e6, expires_unix_nano: (at + 8000) * 1e6, members: [{ inbound_id: inboundID, intent_id: "7".repeat(32), port: 19666, state: "accepted", challenge_id: "8".repeat(32), accepted_unix_nano: (at + 1) * 1e6 }], state: "complete", current: true, replayed: false };
}
function initialSnapshot(round = acceptedRound()) {
 return { id: "9".repeat(32), event_id: "a".repeat(32), client_id: clients[0], handover_id: "" as const, round_id: round.id, node_id: promotion.node_id, installation_id: installation, scope: "lab_initial_apply" as const, delivery_revision: 1, inbound_id: inboundID, promotion_event_id: promotion.event_id, initial_apply_sha: digest, revision: 1, created_unix_nano: round.created_unix_nano + 1e6, expires_unix_nano: round.expires_unix_nano, route_count: 1 as const, current: true };
}
type FixtureOptions = { prepared?: boolean; eligible?: boolean; token?: "absent" | "active" | "revoked"; language?: "en" | "fa"; member?: "pending" | "issued" | "accepted"; tamperState?: (v: any) => void; tamperSubscription?: (v: any) => void };
async function fixture(page: Page, options: FixtureOptions = {}) {
 const writes: { path: string; body: any; raw: string; csrf: string }[] = [], reads: string[] = [];
 const state = { prepared: options.prepared ?? true, eligible: options.eligible ?? options.prepared !== false, preparation: options.prepared === false ? undefined : preparation(), tokenState: options.token ?? "active", generation: options.token === "absent" ? 0 : 1, delivery: 0, revision: 0, offset: 0, failure: "" as "" | "prepare_before" | "round_before" | "snapshot_after", latest: undefined as InitialHealthRound | undefined, snapshot: undefined as ReturnType<typeof initialSnapshot> | undefined };
 const rounds = new Map<string, InitialHealthRound>(), snapshots = new Map<string, ReturnType<typeof initialSnapshot>>();
 const now = () => Date.now() + state.offset;
 function roundView(r: InitialHealthRound) { const current = state.eligible && r.state === "complete" && r.expires_unix_nano / 1e6 > now(); return { ...r, state: r.expires_unix_nano / 1e6 <= now() ? "expired" : !state.eligible ? "state_changed" : r.state, current, server_time_unix_milli: now() }; }
 function healthState() {
  const value: any = { enabled: true, available: true, scope: "lab_initial_apply", inbound_id: inboundID, promotion_event_id: promotion.event_id, node_id: promotion.node_id, installation_id: installation, direct_port: 19666, inbound_revision: 2, draft_revision: 1, prepared: state.prepared, can_prepare: !state.prepared, eligible: state.eligible, blockers: state.eligible ? [] : [{ field: "apply", code: "first_apply_required" }], ...(state.preparation ? { preparation: state.preparation } : {}), ...(state.eligible ? binding : {}), client_ids: clients, ...(state.latest ? { latest_round: roundView(state.latest) } : {}), server_time_unix_milli: now() };
  options.tamperState?.(value); return value;
 }
 function subscription(clientID: string) {
  const value: any = { enabled: true, scope: "lab_initial_apply", client_id: clientID, generation: state.generation, state: state.tokenState, server_time_unix_milli: now(), delivery_revision: state.delivery, delivery_scope: state.delivery ? "lab_initial_apply" : "", initial_apply_revision: state.revision, handover_revision: 0, ...(state.snapshot ? { snapshot: { ...state.snapshot, current: state.eligible && state.tokenState === "active" && state.snapshot.expires_unix_nano / 1e6 > now() } } : {}) };
  options.tamperSubscription?.(value); return value;
 }
 const profile = { id: inboundID, name: "Initial health saved plan", revision: 1, enabled: false, archived: false, bound_clients: 0, transport: "tcp", grpc_service: "", reality_target: "example.org:443", server_names: ["example.org"], short_ids: ["aabb"], public_key: "SYNTHETIC_PUBLIC_KEY" };
 await page.addInitScript(language => { localStorage.setItem("dark-pro-lang", language); localStorage.setItem("dark-pro-theme", "dark"); }, options.language ?? "en");
 await page.route("**/api/v1/session/status", route => route.fulfill({ json: { authenticated: true, user: { id: "owner", username: "test-owner", role: "owner", representative_id: "owner" }, csrf_token: csrf, expires_unix: 1900000000 } }));
 await page.route("**/api/v1/fleet", route => route.fulfill({ json: { nodes: [], total_nodes: 0, online_nodes: 0, offline_nodes: 0, as_of: new Date().toISOString(), interface_rx_bits_per_second: 0, interface_tx_bits_per_second: 0, physical_rates_complete: false, metric_scope: "default_route_interface_not_billable" } }));
 await page.route("**/api/v1/workspace/**", async route => {
  const request = route.request(), path = new URL(request.url()).pathname;
  if (request.method() === "GET") {
   reads.push(path);
   if (path === "/api/v1/workspace/native-inbounds-state") return route.fulfill({ json: { inbounds: [{ ...profile, revision: 2, enabled: true, bound_clients: 2 }], nodes: [{ node_id: promotion.node_id, installation_id: installation, name: "Disposable promotion Node" }], targets: [{ inbound_id: inboundID, node_id: promotion.node_id, installation_id: installation, listen_ip: "127.0.0.1", direct_port: 19666, swap_port: 0 }], runtime: [] } });
   if (path === "/api/v1/workspace/native-inbound-plans/capabilities") return route.fulfill({ json: { available: true, version: 3, protocols: ["vless"], securities: ["reality", "tls"], transports: ["tcp", "grpc", "xhttp", "ws", "httpupgrade"], runtime_apply: false, profile_options: true, max_nodes: 64, max_hosts_per_node: 64, plans: [{ inbound_id: inboundID, plan_revision: 1, inbound_revision: 1, status: "SAVED_DISABLED", node_ids: [promotion.node_id], promotion }] } });
   if (path === base + "/promotion") return route.fulfill({ json: { available: true, scope: "single_direct_tcp_reality_loopback", promoted: true, blockers: [], receipt: promotion } });
   if (path === healthPath) return route.fulfill({ json: healthState() });
   if (path.startsWith(healthPath + "/rounds/")) { const r = rounds.get(path.split("/").at(-1)!); return r ? route.fulfill({ json: roundView(r) }) : route.fulfill({ status: 404, json: { error: "not_found" } }); }
   if (path.startsWith("/api/v1/workspace/clients/") && path.endsWith("/subscription")) return route.fulfill({ json: subscription(path.split("/").at(-2)!) });
   if (path === "/api/v1/workspace/metered-route-transactions") return route.fulfill({ json: { enabled: true, scope: "metered_tcp_loopback_lab", health_enabled: true, health_rounds_enabled: false, initial_apply_health_enabled: true, lab_subscriptions_enabled: true, node_id: promotion.node_id, installation_id: installation, nodes: [{ node_id: promotion.node_id, installation_id: installation }] } });
   if (path === routePath) return route.fulfill({ json: { node_id: promotion.node_id, installation_id: installation, binary_sha: digest, reported_at_unix_nano: now() * 1e6, server_time_unix_milli: now(), observation_fresh: true, drafts_valid: true, status: { instance: "e".repeat(32), serial: 0, ...(state.eligible ? { current_stage_id: stageID } : {}), usage: state.eligible ? { epoch_id: epochID, epoch_sha: digest, admission_sha: digest, settlement: { epoch_id: "", sequence: "0", sha256: "" } } : { settlement: { epoch_id: "", sequence: "0", sha256: "" } }, observed: { highwater: state.eligible ? 1 : 0, saved_sequence: state.eligible ? 1 : 0, saved_sha: state.eligible ? digest : "", active_sequence: state.eligible ? 1 : 0, active_sha: state.eligible ? digest : "", process_ready: state.eligible, phase: state.eligible ? "active" : "awaiting_fresh_lease", lease_expires_unix_nano: state.eligible ? (now() + 30000) * 1e6 : 0, data_plane_verified: false, subscription_published: false, no_listeners: !state.eligible } }, review: [{ inbound_id: inboundID, inbound_revision: 2, draft_revision: 1, name: profile.name, listen_ip: "127.0.0.1", direct_port: 19666, tunnel_port: 0 }], controls: { stage: state.prepared && !state.eligible, ...(state.eligible ? { drain: { stage_id: stageID, ports: [19666] } } : {}) }, commands: [] } });
   if (path.endsWith("/operations")) return route.fulfill({ json: { operations: [] } });
   if (path.endsWith("/clients")) return route.fulfill({ json: { client_ids: clients } });
   return route.fulfill({ status: 404, json: { error: "not_found" } });
  }
  const body = request.postDataJSON(); writes.push({ path, body, raw: request.postData()!, csrf: request.headers()["x-csrf-token"] ?? "" });
  if (path === healthPath + "/prepare") {
   if (state.failure === "prepare_before") return route.fulfill({ status: 503, json: { error: "unknown" } });
   const replayed = state.prepared; state.prepared = true; state.preparation ??= { ...preparation(), event_id: body.event_id };
   return route.fulfill({ status: replayed ? 200 : 201, json: { ...state.preparation, replayed, server_time_unix_milli: now() } });
  }
  if (path === healthPath + "/rounds") {
   if (state.failure === "round_before") return route.fulfill({ status: 503, json: { error: "unknown" } });
   let r = rounds.get(body.id); const replayed = !!r;
   if (!r) { r = acceptedRound(body.id, now()); if (options.member && options.member !== "accepted") { r.state = "pending"; r.current = false; r.members[0] = { inbound_id: inboundID, intent_id: "7".repeat(32), port: 19666, state: options.member, ...(options.member === "issued" ? { challenge_id: "8".repeat(32) } : {}) }; } rounds.set(r.id, r); state.latest = r; }
   return route.fulfill({ status: replayed ? 200 : 201, json: { ...roundView(r), replayed } });
  }
  if (path === healthPath + "/snapshot") {
   let snapshot = snapshots.get(body.event_id); const replayed = !!snapshot;
   if (!snapshot) {
    const r = rounds.get(body.round_id);
    if (!r || r.state !== "complete" || !state.eligible || r.expires_unix_nano / 1e6 <= now() || body.expected_revision !== state.revision || body.expected_delivery_revision !== state.delivery) return route.fulfill({ status: 409, json: { error: "conflict" } });
    snapshot = { ...initialSnapshot(r), event_id: body.event_id, client_id: body.client_id, revision: ++state.revision, delivery_revision: ++state.delivery, created_unix_nano: now() * 1e6 }; snapshots.set(body.event_id, snapshot); state.snapshot = snapshot;
   }
   if (state.failure === "snapshot_after") return route.fulfill({ status: 503, json: { error: "unknown" } });
   return route.fulfill({ status: replayed ? 200 : 201, json: { ...snapshot, current: snapshot.expires_unix_nano / 1e6 > now(), replayed, server_time_unix_milli: now() } });
  }
  if (path.endsWith("/subscription/issue") || path.endsWith("/subscription/rotate")) { state.generation++; state.tokenState = "active"; return route.fulfill({ status: 201, json: { event_id: body.event_id, client_id: path.split("/").at(-3), generation: state.generation, state: "active", replayed: false, superseded: false } }); }
  if (path.endsWith("/subscription/reveal")) return route.fulfill({ json: { client_id: path.split("/").at(-3), generation: state.generation, token, path: "/api/v1/subscriptions/" + token } });
  return route.fulfill({ status: 405, json: { error: "unexpected_mutation" } });
 });
 return { state, writes, reads, accept: () => { const r = state.latest!; r.state = "complete"; r.current = true; r.members[0] = { ...r.members[0], state: "accepted", challenge_id: "8".repeat(32), accepted_unix_nano: now() * 1e6 }; } };
}
const panel = (page: Page) => page.getByRole("region", { name: "First-Apply health and LAB delivery", exact: true });
const probe = (page: Page) => panel(page).getByRole("button", { name: "Review and test the connection", exact: true });
const record = (page: Page) => panel(page).getByRole("button", { name: "Record this client's LAB output", exact: true });
async function open(page: Page) { await page.goto(`/inbounds?inbound=${inboundID}&tab=overview`); await expect(panel(page).getByLabel("First-Apply subscription client", { exact: true })).toBeVisible(); }
async function requestProof(page: Page) { page.once("dialog", dialog => dialog.accept()); await probe(page).click(); }

test("initial preparation gates Stage, binds the original promotion, and never starts execution or proof", async ({ page }) => {
 const f = await fixture(page, { prepared: false, eligible: false, token: "absent" }); await open(page);
 const stage = page.getByRole("region", { name: "Metered TCP LAB transactions", exact: true }).getByRole("button", { name: "Review and Stage", exact: true });
 await expect(stage).toBeDisabled(); await expect(probe(page)).toBeDisabled(); await expect(record(page)).toBeDisabled(); expect(f.writes).toHaveLength(0);
 page.once("dialog", dialog => dialog.accept()); await panel(page).getByRole("button", { name: "Prepare the protocol probe", exact: true }).click();
 await expect(panel(page).getByText("Protocol-test identity prepared", { exact: true })).toBeVisible(); await expect(stage).toBeEnabled();
 expect(f.writes[0].body).toEqual({ event_id: expect.stringMatching(/^[a-f0-9]{32}$/), promotion_event_id: promotion.event_id, confirmed: true });
 page.once("dialog", dialog => dialog.accept()); await panel(page).getByRole("button", { name: "Issue subscription token", exact: true }).click(); await expect(panel(page).getByText("Subscription token active", { exact: true })).toBeVisible();
 await page.reload(); await expect(panel(page).getByText("Protocol-test identity prepared", { exact: true })).toBeVisible(); await expect(probe(page)).toBeDisabled(); await expect(record(page)).toBeDisabled();
 expect(f.writes.map(w => w.path)).toEqual([healthPath + "/prepare", "/api/v1/workspace/clients/" + clients[0] + "/subscription/issue"]);
});

test("reviewed first Apply requires explicit proof, snapshot, reveal and fetch without persisting a secret", async ({ page }) => {
 const f = await fixture(page); await open(page); await expect(probe(page)).toBeEnabled(); await expect(record(page)).toBeDisabled();
 await expect(panel(page).getByLabel("First-Apply subscription client").locator("option")).toHaveText(clients);
 await panel(page).getByRole("button", { name: "Show subscription URL", exact: true }).click(); await expect(panel(page).getByLabel("First-Apply subscription URL")).toHaveValue(new RegExp(token));
 await requestProof(page); await expect(record(page)).toBeEnabled(); await record(page).click();
 await expect(panel(page).locator('[data-initial-snapshot-state="current"]')).toBeVisible(); await expect(panel(page).getByRole("link", { name: "Open checked LAB output", exact: true })).toHaveAttribute("href", new RegExp(token));
 expect(f.writes.map(w => w.path)).toEqual(["/api/v1/workspace/clients/" + clients[0] + "/subscription/reveal", healthPath + "/rounds", healthPath + "/snapshot"]);
 expect(f.writes[2].body).toEqual({ event_id: expect.stringMatching(/^[a-f0-9]{32}$/), client_id: clients[0], round_id: f.writes[1].body.id, initial_apply_sha: digest, expected_revision: 0, expected_delivery_revision: 0, confirmed: true }); expect(f.writes.every(w => w.csrf === csrf)).toBe(true);
 const stored = await page.evaluate(() => JSON.stringify({ ...sessionStorage })); expect(stored).not.toContain(token); expect(stored).not.toContain("/api/v1/subscriptions/"); expect(stored).not.toMatch(/"(?:uuid|private_key|signature)"/);
 await page.getByRole("tab", { name: "Routes", exact: true }).click(); await page.getByRole("tab", { name: "Overview", exact: true }).click(); await expect(panel(page).getByLabel("First-Apply subscription URL")).toHaveCount(0);
});

test("an issued pending challenge is readable but does not enable delivery or block Drain", async ({ page }) => {
 const f = await fixture(page, { member: "issued" }); await open(page); await requestProof(page);
 await expect(panel(page).getByText("Protocol test queued · proof pending", { exact: true })).toBeVisible(); await expect(record(page)).toBeDisabled();
 await expect(page.getByRole("region", { name: "Metered TCP LAB transactions", exact: true }).getByRole("button", { name: "Stop and settle usage", exact: true })).toBeEnabled();
 f.accept(); await expect(record(page)).toBeEnabled(); expect(f.writes).toHaveLength(1);
});

test("expired proof cannot be revived by refresh or repeated renders", async ({ page }) => {
 const f = await fixture(page); await open(page); await requestProof(page); await expect(record(page)).toBeEnabled();
 f.state.offset = 9000; await panel(page).getByRole("button", { name: "Refresh first-Apply health", exact: true }).click();
 await expect(panel(page).getByText("Original proof deadline expired", { exact: true })).toBeVisible(); await expect(record(page)).toBeDisabled();
 await panel(page).getByRole("button", { name: "Refresh first-Apply health", exact: true }).click(); await expect(record(page)).toBeDisabled(); expect(f.writes).toHaveLength(1);
});

test("a confirmation crossing the original observation window sends no health request", async ({ page }) => {
 const f = await fixture(page); await open(page); await expect(probe(page)).toBeEnabled();
 await page.evaluate(() => { const original = Date.now; window.confirm = () => { Date.now = () => original() + 13000; return true; }; });
 await probe(page).click(); expect(f.writes).toHaveLength(0); await expect(record(page)).toBeDisabled();
});

test("an uncertain health request survives reload and retry sends exactly the original request", async ({ page }) => {
 const f = await fixture(page); f.state.failure = "round_before"; await open(page); await requestProof(page);
 await expect(panel(page).getByRole("button", { name: "Retry the exact retained request", exact: true })).toBeVisible(); const original = f.writes[0].raw;
 await page.reload(); await expect(panel(page).getByRole("button", { name: "Retry the exact retained request", exact: true })).toBeEnabled(); expect(f.writes).toHaveLength(1);
 f.state.failure = ""; page.once("dialog", dialog => dialog.accept()); await panel(page).getByRole("button", { name: "Retry the exact retained request", exact: true }).click();
 await expect(record(page)).toBeEnabled(); expect(f.writes).toHaveLength(2); expect(f.writes[1].raw).toBe(original);
});

test("lost snapshot response reconciles after reload as historical without another mutation", async ({ page }) => {
 const f = await fixture(page); await open(page); await requestProof(page); await expect(record(page)).toBeEnabled(); f.state.failure = "snapshot_after"; await record(page).click();
 await expect(panel(page).getByRole("button", { name: "Retry the exact retained request", exact: true })).toBeVisible(); f.state.offset = 9000;
 await page.reload(); await expect(panel(page).locator('[data-initial-snapshot-state="historical"]')).toBeVisible(); await expect(panel(page).getByRole("button", { name: "Retry the exact retained request", exact: true })).toHaveCount(0); expect(f.writes).toHaveLength(2);
});

test("storage failure prevents preparation and proof writes", async ({ page }) => {
 const f = await fixture(page); await page.addInitScript(() => { const original = Storage.prototype.setItem; Storage.prototype.setItem = function (key, value) { if (key.startsWith("dark-pro-initial-health:")) throw Error("fixture storage unavailable"); original.call(this, key, value); }; });
 await open(page); await requestProof(page); await expect(panel(page).getByText("The exact request could not be saved in this tab. No new write was sent.", { exact: true })).toBeVisible(); expect(f.writes).toHaveLength(0);
});

test("changed token generation invalidates the reviewed client's delivery action", async ({ page }) => {
 const f = await fixture(page); await open(page); await requestProof(page); await expect(record(page)).toBeEnabled(); f.state.tokenState = "revoked"; f.state.generation++;
 await panel(page).getByRole("button", { name: "Refresh first-Apply health", exact: true }).click(); await expect(panel(page).getByText("Subscription token revoked", { exact: true })).toBeVisible(); await expect(record(page)).toBeDisabled(); expect(f.writes).toHaveLength(1);
});

test("a current process with incomplete delivery authority metadata cannot start a proof", async ({ page }) => {
 const f = await fixture(page, { tamperSubscription: value => { delete value.delivery_revision; } }); await open(page);
 await expect(panel(page).getByText("The selected client's subscription state could not be verified.", { exact: true })).toBeVisible(); await expect(probe(page)).toBeDisabled(); expect(f.writes).toHaveLength(0);
});

test("the compact Persian health panel fits a 320px viewport and adds no Node selector", async ({ page }) => {
 await page.setViewportSize({ width: 320, height: 900 }); const f = await fixture(page, { language: "fa" }); await open(page);
 await expect(panel(page).getByRole("heading", { name: "بررسی اولین اجرای LAB", exact: true })).toBeVisible();
 expect(await panel(page).evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true); await expect(panel(page).getByRole("combobox")).toHaveCount(1); expect(f.writes).toHaveLength(0);
});

test("public health parsers preserve original promotion identity and reject private or mismatched context", () => {
 expect(readInitialHealthPreparation(preparation(), promotion).promotion_event_id).toBe("active-promotion-commit-0001");
 const round = acceptedRound(); expect(readInitialHealthRound(round, promotion, { id: round.id, binding }).members[0].state).toBe("accepted");
 for (const changed of [{ ...round, node_id: "other-node" }, { ...round, epoch_id: "f".repeat(32) }, { ...round, uuid: "synthetic-private" }, { ...round, expires_unix_nano: round.created_unix_nano + 9e9 }, { ...round, members: [] }]) expect(() => readInitialHealthRound(changed, promotion, { id: round.id, binding })).toThrow();
 const incomplete = { ...round, state: "pending", current: false, members: [{ ...round.members[0], state: "issued", accepted_unix_nano: undefined }] };
 expect(readInitialHealthRound(incomplete, promotion).members[0].state).toBe("issued"); expect(() => readInitialHealthRound({ ...incomplete, members: [{ ...incomplete.members[0], challenge_id: undefined }] }, promotion)).toThrow();
});

test("initial snapshot allows an earlier bounded deadline and requires exact origin and global revision", () => {
 const round = acceptedRound(), attempt: Extract<InitialHealthAttempt, { action: "snapshot" }> = { action: "snapshot", body: { event_id: "a".repeat(32), client_id: clients[0], round_id: round.id, initial_apply_sha: digest, expected_revision: 0, expected_delivery_revision: 0, confirmed: true }, round };
 expect(readInitialHealthSnapshot({ ...initialSnapshot(round), expires_unix_nano: round.expires_unix_nano - 1e9 }, promotion, attempt).route_count).toBe(1);
 for (const changed of [{ ...initialSnapshot(round), handover_id: "f".repeat(32) }, { ...initialSnapshot(round), initial_apply_sha: "f".repeat(64) }, { ...initialSnapshot(round), delivery_revision: 2 }, { ...initialSnapshot(round), expires_unix_nano: round.expires_unix_nano + 1e6 }]) expect(() => readInitialHealthSnapshot(changed, promotion, attempt)).toThrow();
 expect(() => readInitialLabSnapshot({ ...initialSnapshot(round), token }, clients[0])).toThrow();
});

test("old handover snapshot validation remains strict while historical pre-journal receipts remain readable", () => {
 const initial = initialSnapshot(), legacy: any = { ...initial, handover_id: "f".repeat(32), scope: "lab_single_handover", current: false, replayed: true };
 delete legacy.inbound_id; delete legacy.promotion_event_id; delete legacy.initial_apply_sha; delete legacy.delivery_revision;
 expect(readLabSnapshot(legacy, clients[0]).current).toBe(false); expect(() => readLabSnapshot({ ...legacy, current: true }, clients[0])).toThrow(); expect(() => readLabSnapshot(initial, clients[0])).toThrow();
 delete legacy.scope; expect(readLabSnapshot({ ...legacy, current: true }, clients[0]).current).toBe(true);
});

test("shared subscription authority validates selected origin independently of both local revision counters", () => {
 const view = { enabled: true, scope: "lab_initial_apply", client_id: clients[0], generation: 1, state: "active", server_time_unix_milli: Date.now(), delivery_revision: 1, delivery_scope: "lab_initial_apply", initial_apply_revision: 1, handover_revision: 7, snapshot: initialSnapshot() };
 expect(readSubscription(view, clients[0]).enabled).toBe(true);
 for (const changed of [{ ...view, delivery_scope: "lab_single_handover" }, { ...view, delivery_revision: 2 }, { ...view, initial_apply_revision: 7 }, { ...view, token }]) expect(() => readSubscription(changed, clients[0])).toThrow();
 expect(initialHealthBinding(binding)).toEqual(binding);
 expect(() => readInitialHealthState({ enabled: false, scope: "lab_initial_apply", inbound_id: inboundID, server_time_unix_milli: Date.now(), blockers: [{ field: "initial_health", code: "initial_health_disabled" }], uuid: "private" }, promotion)).toThrow();
});

test("single-Node initial health permits subscription scope without weakening legacy or consumer scope", () => {
 const scope = { enabled: true, scope: "metered_tcp_loopback_lab", node_id: promotion.node_id, installation_id: installation, nodes: [{ node_id: promotion.node_id, installation_id: installation }], health_enabled: true, health_rounds_enabled: false, initial_apply_health_enabled: true, lab_subscriptions_enabled: true };
 expect(readScope(scope).initial_apply_health_enabled).toBe(true);
 for (const changed of [{ ...scope, health_enabled: false }, { ...scope, health_rounds_enabled: true }, { ...scope, consumer_manifests_enabled: true }, { ...scope, nodes: [...scope.nodes, { node_id: "other", installation_id: "9".repeat(32) }] }, { ...scope, initial_apply_health_enabled: false }]) expect(() => readScope(changed)).toThrow();
});
