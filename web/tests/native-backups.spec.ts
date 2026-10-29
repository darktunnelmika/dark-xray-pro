import { test, expect, type Page } from "@playwright/test";
import { createServer, request as httpRequest } from "node:http";
import type { AddressInfo } from "node:net";
import type { BackupArchive, BackupJob, BackupSettings, BackupStatus } from "../src/nativeBackups";
import { readBackupStatus } from "../src/nativeBackups";

const base = "/api/v1/workspace/owner/backups";
const csrf = "c".repeat(64);
const now = () => Math.floor(Date.now() / 1000);
function archive(): BackupArchive {
  return { id: "a".repeat(32), job_id: "b".repeat(32), created_unix: now() - 300,
    size_bytes: 3_221_291_009, payload_bytes: 4_221_291_009, sha256: "d".repeat(64), source_revision: "e".repeat(40),
    schema_version: 16, verification: "checksum_verified", protected: true, downloadable: true };
}
function job(event = "backup-fixture-request-0001"): BackupJob {
  return { id: "1".repeat(32), event_id: event, actor_id: "owner", action: "create", trigger: "manual",
    state: "queued", phase: "queued", created_unix: now(), started_unix: 0, finished_unix: 0, archive_id: "",
    cancel_requested: false, error_code: "", warning_code: "", downtime_ms: 0, revision: 1 };
}
function status(): BackupStatus {
  return { enabled: true, runner_status: "ready", recipient_fingerprint: "f".repeat(64),
    capabilities: { create: true, download: true, cancel: true, schedules: true, retention: true, upload: false, restore: false },
    settings: { revision: 1, schedule: { enabled: false, frequency: "daily", time: "03:00", timezone: "Asia/Tehran", weekday: 0 },
      retention: { keep_last: 7, max_age_days: 30 } },
    next_run_unix: 0, jobs: [], archives: [archive()] };
}
type Write = { method: string; path: string; body: Record<string, unknown>; csrf: string | undefined };
async function fixture(page: Page, options: {
  role?: "owner" | "representative"; signed?: boolean; disabled?: boolean;
  loseCreate?: boolean; hideRecordedUntilReplay?: boolean; conflictSettings?: boolean; loseSettings?: boolean;
} = {}) {
  let signed = options.signed ?? true, unavailable = false, loseCreate = !!options.loseCreate;
  let conflictSettings = !!options.conflictSettings, hiddenJob: BackupJob | undefined;
  let loseSettings = !!options.loseSettings;
  const settingsReceipts = new Map<string, BackupSettings>();
  const role = options.role ?? "owner";
  const user = { id: role, username: "backup-test", role, representative_id: role === "owner" ? "owner" : "rep-test",
    permissions: { view_clients: true } };
  const session = { authenticated: true, user, csrf_token: csrf, expires_unix: now() + 7200 };
  const current = status(), writes: Write[] = [], reads: string[] = [], downloads: string[] = [];
  if (options.disabled) {
    current.enabled = false; current.runner_status = "disabled"; current.recipient_fingerprint = "";
    current.capabilities = { create: false, download: false, cancel: false, schedules: false, retention: false, upload: false, restore: false };
    current.archives = [];
  }
  await page.route("**/api/v1/**", async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname;
    if (path === "/api/v1/session/status") return route.fulfill({ json: signed ? session : { authenticated: false } });
    if (path === "/api/v1/session/login") { signed = true; return route.fulfill({ json: session }); }
    if (path === "/api/v1/session/logout") { signed = false; return route.fulfill({ json: { logged_out: true } }); }
    if (!signed) return route.fulfill({ status: 401, json: { error: "session_required" } });
    if (path === base && request.method() === "GET") {
      reads.push(path);
      if (unavailable) return route.fulfill({ status: 503, json: { error: "backup_worker_unavailable" } });
      return route.fulfill({ json: current });
    }
    if (path === base + "/archives/" + "a".repeat(32) + "/download") {
      downloads.push(request.resourceType());
      return route.fulfill({ status: 200, contentType: "application/octet-stream",
        headers: { "Content-Disposition": 'attachment; filename="native-pro-test.backup"' }, body: "synthetic-encrypted-test-download" });
    }
    if (path.startsWith(base) && request.method() !== "GET") {
      const body = request.postDataJSON() as Record<string, unknown>;
      writes.push({ path, method: request.method(), body, csrf: request.headers()["x-csrf-token"] });
      expect(request.headers()["x-csrf-token"]).toBe(csrf);
      expect(body).not.toHaveProperty("actor_id");
      if (path === base + "/jobs") {
        expect(Object.keys(body).sort()).toEqual(["action", "event_id"]);
        const existing = current.jobs.find(item => item.event_id === body.event_id) ?? (hiddenJob?.event_id === body.event_id ? hiddenJob : undefined);
        let serial = 1;
        while (current.jobs.some(item => item.id === serial.toString(16).padStart(32, "0"))) serial++;
        const created = existing ?? { ...job(String(body.event_id)), id: serial.toString(16).padStart(32, "0") };
        current.runner_status = "busy"; current.capabilities.create = false;
        if (loseCreate) {
          loseCreate = false;
          if (options.hideRecordedUntilReplay) hiddenJob = created;
          else current.jobs = [created, ...current.jobs.filter(item => item.id !== created.id)];
          return route.abort("failed");
        }
        current.jobs = [created, ...current.jobs.filter(item => item.id !== created.id)]; hiddenJob = undefined;
        return route.fulfill({ status: 202, json: { job: created, replayed: !!existing } });
      }
      if (path === base + "/jobs/" + "1".repeat(32) + "/cancel") {
        expect(Object.keys(body)).toEqual(["event_id"]);
        current.jobs[0] = { ...current.jobs[0], cancel_requested: true, state: "cancelling", revision: current.jobs[0].revision + 1 };
        return route.fulfill({ json: { job: current.jobs[0], replayed: false } });
      }
      if (path === base + "/settings" && request.method() === "PUT") {
        const receipt = settingsReceipts.get(String(body.event_id));
        if (receipt) return route.fulfill({ json: { settings: receipt, replayed: true } });
        if (conflictSettings) {
          conflictSettings = false;
          current.settings = { ...current.settings, revision: 2, retention: { keep_last: 9, max_age_days: 45 } };
          return route.fulfill({ status: 409, json: { error: "backup_control_conflict" } });
        }
        expect(body.expected_revision).toBe(current.settings.revision);
        current.settings = { revision: current.settings.revision + 1, schedule: body.schedule, retention: body.retention } as BackupSettings;
        settingsReceipts.set(String(body.event_id), structuredClone(current.settings));
        current.next_run_unix = current.settings.schedule.enabled ? now() + 3600 : 0;
        if (loseSettings) { loseSettings = false; return route.abort("failed"); }
        return route.fulfill({ json: { settings: current.settings, replayed: false } });
      }
    }
    if (path.endsWith("/me")) return route.fulfill({ json: { user,
      balance: { id: user.representative_id, volume_credit_bytes: 0, unlimited_credit_units: 0 }, data_plane: "not_activated" } });
    const field = path.split("/").at(-1)!;
    return route.fulfill({ json: { [field]: [], total: 0, limit: 25, offset: 0, has_more: false } });
  });
  return { current, writes, reads, downloads, unavailable: (value: boolean) => { unavailable = value; } };
}
const createButton = (page: Page) => page.getByRole("button", { name: "Create backup", exact: true });
const refreshButton = (page: Page) => page.getByRole("button", { name: "Refresh status", exact: true });

test("status validator accepts the store capacity and rejects records beyond it", () => {
  const bounded = status();
  bounded.jobs = Array.from({ length: 4096 }, (_, i) => ({ ...job("record-" + i), id: i.toString(16).padStart(32, "0"),
    state: "succeeded", phase: "complete", finished_unix: now() }));
  bounded.archives = Array.from({ length: 4096 }, (_, i) => ({ ...archive(), id: i.toString(16).padStart(32, "0") }));
  expect(readBackupStatus(bounded)).toBe(bounded);
  expect(() => readBackupStatus({ ...bounded, jobs: [...bounded.jobs, { ...job(), id: "f".repeat(32) }] })).toThrow("invalid_backup_status");
  expect(() => readBackupStatus({ ...bounded, archives: [...bounded.archives, { ...archive(), id: "f".repeat(32) }] })).toThrow("invalid_backup_status");
});

test("owner bookmark requires login and restores the real backups page", async ({ page }) => {
  const f = await fixture(page, { signed: false });
  await page.goto("/backups");
  await expect(page.getByRole("heading", { name: "Sign in", exact: true })).toBeVisible();
  expect(f.reads).toHaveLength(0);
  await page.getByLabel("Username", { exact: true }).fill("backup-test");
  await page.getByLabel("Password", { exact: true }).fill("synthetic-test-password");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page).toHaveURL(/\/backups$/);
  await expect(createButton(page)).toBeEnabled();
  await expect(page.getByText("Panel backup not available", { exact: true })).toHaveCount(0);
});

test("representative cannot open or request the owner backup surface", async ({ page }) => {
  const f = await fixture(page, { role: "representative" });
  await page.goto("/backups");
  await expect(page).toHaveURL(/\/clients$/);
  await expect(page.getByRole("navigation").getByRole("button", { name: "Backups", exact: true, includeHidden: true })).toHaveCount(0);
  await expect(page.getByRole("region", { name: "Native backups", exact: true })).toHaveCount(0);
  expect(f.reads).toHaveLength(0); expect(f.writes).toHaveLength(0);
});

test("disabled capability keeps creation and settings unavailable", async ({ page }) => {
  const f = await fixture(page, { disabled: true });
  await page.goto("/backups");
  await expect(page.getByText("Panel backups are not configured yet.", { exact: true })).toBeVisible();
  await expect(createButton(page)).toBeDisabled();
  await expect(page.getByLabel("Enable scheduled backups", { exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Save settings", exact: true })).toBeDisabled();
  expect(f.writes).toHaveLength(0);
});

test("creation sends one stable owner request and never claims completion from acceptance", async ({ page }) => {
  const f = await fixture(page);
  await page.goto("/backups"); await expect(createButton(page)).toBeEnabled();
  await createButton(page).click();
  await expect(page.getByText("Backup request recorded.", { exact: true })).toBeVisible();
  await expect(page.getByText("Waiting to start", { exact: true })).toBeVisible();
  await expect(createButton(page)).toBeDisabled();
  expect(f.writes).toHaveLength(1);
  expect(f.writes[0].body).toEqual({ event_id: expect.stringMatching(/^backup-/), action: "create" });
  await expect(page.getByText("Completed", { exact: true })).toHaveCount(0);
});

test("lost creation response is reconciled after reload without another POST", async ({ page }) => {
  const f = await fixture(page, { loseCreate: true });
  await page.goto("/backups"); await expect(createButton(page)).toBeEnabled();
  await createButton(page).click();
  await expect(page.getByRole("button", { name: "Retry same request", exact: true })).toBeVisible();
  const original = f.writes[0].body;
  await page.reload();
  await expect(page.getByText("The previous request was found in the recorded status.", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Retry same request", exact: true })).toHaveCount(0);
  expect(f.writes).toHaveLength(1);
  expect(f.current.jobs[0].event_id).toBe(original.event_id);
  expect(await page.evaluate(() => sessionStorage.getItem("dark-pro-backup-request-v1:owner"))).toBeNull();
});

test("unknown creation outcome retains the identical request across reload and retry", async ({ page }) => {
  const f = await fixture(page, { loseCreate: true, hideRecordedUntilReplay: true });
  await page.goto("/backups"); await expect(createButton(page)).toBeEnabled();
  await createButton(page).click();
  await expect(page.getByRole("button", { name: "Retry same request", exact: true })).toBeVisible();
  await page.reload();
  const retry = page.getByRole("button", { name: "Retry same request", exact: true });
  await expect(retry).toBeEnabled(); await retry.click();
  await expect(retry).toHaveCount(0);
  expect(f.writes).toHaveLength(2); expect(f.writes[1]).toEqual(f.writes[0]);
  expect(f.current.jobs).toHaveLength(1);
});

test("settings conflict preserves edits and requires a new explicit save against the current revision", async ({ page }) => {
  const f = await fixture(page, { conflictSettings: true });
  await page.goto("/backups"); await expect(createButton(page)).toBeEnabled();
  await page.getByLabel("Keep latest copies", { exact: true }).fill("12");
  await page.getByRole("button", { name: "Save settings", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Settings changed elsewhere. Your edits are still here.");
  await expect(page.getByLabel("Keep latest copies", { exact: true })).toHaveValue("12");
  expect(f.writes).toHaveLength(1); expect(f.current.settings.retention.keep_last).toBe(9);
  const save = page.getByRole("button", { name: "Save my edits", exact: true });
  await expect(save).toBeEnabled(); await save.click();
  await expect(page.getByText("Backup settings saved.", { exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(2);
  expect(f.writes[0].body.expected_revision).toBe(1); expect(f.writes[1].body.expected_revision).toBe(2);
  expect(f.writes[0].body.event_id).not.toBe(f.writes[1].body.event_id);
  expect(f.current.settings.retention.keep_last).toBe(12);
  await expect(page.getByRole("link", { name: "Download archive", exact: true })).toHaveCount(1);
});

test("cancellation remains requested until the server records a terminal state", async ({ page }) => {
  const f = await fixture(page);
  f.current.runner_status = "busy"; f.current.capabilities.create = false;
  f.current.jobs = [{ ...job(), state: "running", phase: "creating_offline", started_unix: now() }];
  await page.goto("/backups");
  await page.getByRole("button", { name: "Request cancellation", exact: true }).click();
  await expect(page.getByText("Cancellation request recorded.", { exact: true })).toBeVisible();
  await expect(page.getByText("Cancelling safely", { exact: true })).toBeVisible();
  await expect(page.getByText("Cancelled", { exact: true })).toHaveCount(0);
  expect(f.writes).toHaveLength(1);
  f.current.jobs[0] = { ...f.current.jobs[0], state: "cancelled", phase: "complete", finished_unix: now(), revision: 3 };
  f.current.runner_status = "ready"; f.current.capabilities.create = true;
  await refreshButton(page).click();
  await expect(page.getByText("Cancelled", { exact: true })).toBeVisible();
});

test("lost settings receipt replays the same policy after reload without replacing newer settings", async ({ page }) => {
  const f = await fixture(page, { loseSettings: true });
  await page.goto("/backups"); await expect(createButton(page)).toBeEnabled();
  await page.getByLabel("Enable scheduled backups", { exact: true }).check();
  await page.getByLabel("Repeat", { exact: true }).selectOption("weekly");
  await page.getByLabel("Day", { exact: true }).selectOption("6");
  await page.getByLabel("Time", { exact: true }).fill("06:15");
  await page.getByLabel("Time zone", { exact: true }).fill("UTC");
  await page.getByLabel("Keep latest copies", { exact: true }).fill("11");
  await page.getByRole("button", { name: "Save settings", exact: true }).click();
  await expect(page.getByRole("button", { name: "Retry same request", exact: true })).toBeVisible();
  expect(f.current.settings.schedule).toEqual({ enabled: true, frequency: "weekly", weekday: 6, time: "06:15", timezone: "UTC" });
  f.current.settings = { ...f.current.settings, revision: 3, retention: { keep_last: 14, max_age_days: 30 } };
  await page.reload();
  await expect(page.getByLabel("Keep latest copies", { exact: true })).toHaveValue("11");
  await expect(page.getByLabel("Keep latest copies", { exact: true })).toBeDisabled();
  const retry = page.getByRole("button", { name: "Retry same request", exact: true });
  await expect(retry).toBeEnabled(); await retry.click();
  await expect(page.getByText("Backup settings saved.", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Keep latest copies", { exact: true })).toHaveValue("14");
  expect(f.writes).toHaveLength(2); expect(f.writes[1]).toEqual(f.writes[0]);
  expect(f.current.settings.revision).toBe(3);
});

test("offline backup keeps the last confirmed phase and defers cancellation until reconnection", async ({ page }) => {
  const f = await fixture(page);
  f.current.runner_status = "busy"; f.current.capabilities.create = false;
  f.current.jobs = [{ ...job(), state: "running", phase: "preflight", started_unix: now() }];
  await page.goto("/backups");
  const cancel = page.getByRole("button", { name: "Request cancellation", exact: true });
  await expect(cancel).toBeEnabled();
  f.unavailable(true); await refreshButton(page).click();
  await expect(page.getByText(/Progress and cancellation become available when the panel reconnects/)).toBeVisible();
  await expect(cancel).toBeDisabled();
  await expect(page.getByText("Checking readiness", { exact: true })).toBeVisible();
  await expect(page.getByText("Creating the encrypted copy", { exact: true })).toHaveCount(0);
  expect(f.writes).toHaveLength(0);
  f.unavailable(false); f.current.jobs[0].phase = "checking_archive";
  f.current.jobs[0].revision++;
  await refreshButton(page).click();
  await expect(page.getByText("Checking the stored archive", { exact: true })).toBeVisible();
  await expect(cancel).toBeEnabled();
});

test("an active download ends the attempt and a later explicit creation uses a new request ID", async ({ page }) => {
  const f = await fixture(page);
  await page.goto("/backups"); await expect(createButton(page)).toBeEnabled();
  await createButton(page).click();
  await expect(page.getByText("Waiting to start", { exact: true })).toBeVisible();
  const firstEvent = f.writes[0].body.event_id;
  f.current.jobs[0] = { ...f.current.jobs[0], state: "failed", phase: "complete", error_code: "download_busy", finished_unix: now(), revision: 2 };
  f.current.runner_status = "ready"; f.current.capabilities.create = true;
  await refreshButton(page).click();
  await expect(page.getByText(/After the transfer finishes, choose Create backup to start a new attempt/)).toBeVisible();
  expect(f.writes).toHaveLength(1);
  await expect(createButton(page)).toBeEnabled(); await createButton(page).click();
  await expect(page.getByText("Waiting to start", { exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(2); expect(f.writes[1].body.event_id).not.toBe(firstEvent);
  expect(f.current.jobs).toHaveLength(2);
});

test("unknown archive publication is explained even when the server records cancellation", async ({ page }) => {
  const f = await fixture(page);
  f.current.jobs = [{ ...job(), state: "cancelled", phase: "complete", error_code: "publication_unknown", finished_unix: now() }];
  await page.goto("/backups");
  await expect(page.getByText(/A file may exist, but the outcome of this attempt is uncertain/)).toBeVisible();
  await expect(page.getByText("Cancelled", { exact: true })).toBeVisible();
  await expect(page.getByText("Completed", { exact: true })).toHaveCount(0);
});

test("retention warning preserves the confirmed archive and explains incomplete cleanup", async ({ page }) => {
  const f = await fixture(page);
  f.current.jobs = [{ ...job(), state: "succeeded", phase: "complete", archive_id: archive().id, warning_code: "retention_incomplete", finished_unix: now() }];
  await page.goto("/backups");
  await expect(page.getByText("Completed", { exact: true })).toBeVisible();
  await expect(page.getByText(/The new archive was recorded, but removal of older copies did not finish/)).toBeVisible();
  await expect(page.getByRole("link", { name: "Download archive", exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(0);
});

test("runner recovery requirement is visible and blocks new backups even without a job receipt", async ({ page }) => {
  const f = await fixture(page);
  f.current.runner_status = "recovery_required";
  await page.goto("/backups");
  await expect(page.getByRole("alert")).toContainText("The panel service needs recovery.");
  await expect(createButton(page)).toBeDisabled();
  f.current.runner_status = "ready";
  f.current.jobs = [{ ...job(), state: "recovery_required", phase: "restarting_hub", error_code: "hub_resume_unconfirmed" }];
  await refreshButton(page).click();
  await expect(page.getByText("Recovery required", { exact: true })).toBeVisible();
  await expect(createButton(page)).toBeDisabled();
  await expect(page.getByRole("button", { name: "Request cancellation", exact: true })).toHaveCount(0);
});

test("worker outage preserves recorded archives and blocks a new mutation until status returns", async ({ page }) => {
  const f = await fixture(page);
  await page.goto("/backups"); await expect(createButton(page)).toBeEnabled();
  f.unavailable(true); await refreshButton(page).click();
  await expect(page.getByText(/Connection interrupted\. The last confirmed status/)).toBeVisible();
  await expect(createButton(page)).toBeDisabled();
  await expect(page.getByText("Checksum verified", { exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(0);
  f.unavailable(false); await refreshButton(page).click();
  await expect(createButton(page)).toBeEnabled();
});

test("archive download uses the browser stream without fetching a Blob", async ({ page, baseURL }) => {
  await fixture(page);
  await page.addInitScript(() => {
    Response.prototype.blob = function () { throw new Error("Backup downloads must not buffer a Blob."); };
    const realFetch = window.fetch;
    window.fetch = (...args) => {
      if (String(args[0]).includes("/archives/") && String(args[0]).endsWith("/download")) throw new Error("Use the browser download stream.");
      return realFetch(...args);
    };
  });
  // Native download requests can bypass Playwright page routing. Serve this one
  // endpoint on loopback and proxy the already-built UI to the ordinary test server.
  const downloadPath = base + "/archives/" + "a".repeat(32) + "/download";
  const upstream = new URL(baseURL!);
  let downloadCount = 0;
  const server = createServer((request, response) => {
    if (request.url === downloadPath) {
      downloadCount++;
      response.writeHead(200, { "Content-Type": "application/octet-stream", "Content-Disposition": 'attachment; filename="native-pro-test.backup"' });
      response.end("synthetic-encrypted-test-download"); return;
    }
    const proxy = httpRequest({ hostname: "127.0.0.1", port: upstream.port, path: request.url, method: request.method }, incoming => {
      response.writeHead(incoming.statusCode ?? 502, incoming.headers); incoming.pipe(response);
    });
    proxy.on("error", () => response.writeHead(502).end()); request.pipe(proxy);
  });
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  try {
    await page.goto("http://127.0.0.1:" + (server.address() as AddressInfo).port + "/backups");
    const link = page.getByRole("link", { name: "Download archive", exact: true });
    await expect(link).toHaveAttribute("href", downloadPath);
    const downloaded = page.waitForEvent("download");
    await link.click();
    const file = await downloaded;
    expect(file.suggestedFilename()).toBe("native-pro-test.backup");
    expect(await file.failure()).toBeNull(); expect(downloadCount).toBe(1);
    await expect(page.getByText(/Decryption and recovery compatibility have not been verified/)).toBeVisible();
    await expect(page.getByText("Restore verified", { exact: true })).toHaveCount(0);
  } finally {
    server.closeAllConnections();
    await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve()));
  }
});

test("a recovery-storage failure does not send a backup request", async ({ page }) => {
  const f = await fixture(page);
  await page.addInitScript(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (key.startsWith("dark-pro-backup-request-v1:")) throw new DOMException("Full", "QuotaExceededError");
      original.call(this, key, value);
    };
  });
  await page.goto("/backups"); await expect(createButton(page)).toBeEnabled();
  await createButton(page).click();
  await expect(page.getByText("The browser could not save the recovery record. No request was sent.", { exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(0);
});

for (const theme of ["light", "dark"]) test("Persian 320px backup controls and long hashes fit in " + theme, async ({ page }) => {
  await fixture(page);
  await page.setViewportSize({ width: 320, height: 780 });
  await page.addInitScript(value => {
    localStorage.setItem("dark-pro-lang", "fa"); localStorage.setItem("dark-pro-theme", value);
  }, theme);
  await page.goto("/backups");
  await expect(page.getByRole("button", { name: "ساخت پشتیبان", exact: true })).toBeEnabled();
  await expect(page.locator("html")).toHaveAttribute("dir", "rtl");
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
  await page.getByLabel("تکرار", { exact: true }).selectOption("weekly");
  await expect(page.getByLabel("روز", { exact: true })).toBeVisible();
  await page.getByText("جزئیات آرشیو", { exact: true }).click();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(page.getByRole("link", { name: "دریافت آرشیو", exact: true })).toBeVisible();
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: "instant" }));
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(0);
  await page.screenshot({ path: test.info().outputPath("native-backups-fa-" + theme + ".png"), fullPage: true, animations: "disabled" });
});
