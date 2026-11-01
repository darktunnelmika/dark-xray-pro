import { test, expect, type Page } from "@playwright/test";
const base = "/api/v1/workspace/owner/backups";
const sha = "d".repeat(64), archiveID = "a".repeat(32), uploadID = "b".repeat(32);
const now = () => Math.floor(Date.now() / 1000);
function makeJob(body: Record<string, unknown>) {
  return { id: "1".repeat(32), event_id: String(body.event_id), actor_id: "owner", source_kind: String(body.source_kind), source_id: String(body.source_id), sha256: String(body.sha256),
    state: "queued", phase: "queued", created_unix: now(), finished_unix: 0, revision: 1, error_code: "", table_count: 0, data_restored: false };
}
async function fixture(page: Page, options: { lose?: boolean; hide?: boolean; disabled?: boolean; role?: string } = {}) {
  const state = { jobs: [] as ReturnType<typeof makeJob>[], writes: [] as Record<string, unknown>[], offline: false, hidden: !!options.hide, lose: !!options.lose, reads: [] as string[] };
  const role = options.role ?? "owner";
  const user = { id: role, username: "backup-test", role, representative_id: role === "owner" ? "owner" : "rep-test", permissions: { view_clients: true } };
  const csrf = "c".repeat(64);
  await page.route("**/api/v1/**", async route => {
    const req = route.request(), path = new URL(req.url()).pathname;
    if (path === "/api/v1/session/status") return route.fulfill({ json: { authenticated: true, user, csrf_token: csrf, expires_unix: now() + 7200 } });
    if (path === base) return route.fulfill({ json: { enabled: true, runner_status: "ready", recipient_fingerprint: "f".repeat(64),
      capabilities: { create: true, download: true, cancel: true, schedules: true, retention: true, upload: true, restore: !options.disabled },
      settings: { revision: 1, schedule: { enabled: false, frequency: "daily", time: "03:00", timezone: "Asia/Tehran", weekday: 0 }, retention: { keep_last: 7, max_age_days: 30 } },
      next_run_unix: 0, jobs: [], archives: [{ id: archiveID, job_id: archiveID, created_unix: now() - 300, size_bytes: 100426, payload_bytes: 1003761, sha256: sha, source_revision: "e".repeat(40), schema_version: 8, verification: "checksum_verified", protected: true, downloadable: true }] } });
    if (path === base + "/uploads") return route.fulfill({ json: { enabled: true, max_upload_bytes: 66 * 1024 ** 3, max_inbox_bytes: 132 * 1024 ** 3, reserved_bytes: 100426, max_entries: 4096, max_active_entries: 32,
      uploads: [{ id: uploadID, event_id: "upload-synthetic-fixture-0001", actor_id: "owner", size_bytes: 100426, sha256: sha, state: "received", created_unix: now() - 100, updated_unix: now(), received_bytes: 100426, verification: "ciphertext_checksum_verified", downloadable: true, error_code: "", attempts: 1 }] } });
    if (path === base + "/restores") {
      if (req.method() === "GET") {
        state.reads.push(path);
        if (state.offline) return route.fulfill({ status: 503, json: { error: "unavailable" } });
        return route.fulfill({ json: { enabled: !options.disabled, jobs: state.hidden ? [] : state.jobs } });
      }
      const body = req.postDataJSON() as Record<string, unknown>;
      expect(req.headers()["x-csrf-token"]).toBe(csrf);
      expect(Object.keys(body).sort()).toEqual(["confirmation", "event_id", "sha256", "source_id", "source_kind"]);
      expect(body.confirmation).toBe("RESTORE");
      const existing = state.jobs.find(j => j.event_id === body.event_id);
      const job = existing ?? makeJob(body);
      state.jobs = [job]; state.writes.push(body);
      if (state.lose) { state.lose = false; return route.abort("failed"); }
      state.hidden = false;
      return route.fulfill({ status: 202, json: { job, replayed: !!existing } });
    }
    if (path.endsWith("/me")) return route.fulfill({ json: { user, balance: { id: user.representative_id, volume_credit_bytes: 0, unlimited_credit_units: 0 }, data_plane: "not_activated" } });
    const field = path.split("/").at(-1)!;
    return route.fulfill({ json: { [field]: [], total: 0, limit: 25, offset: 0, has_more: false } });
  });
  return state;
}
async function selectArchive(page: Page) {
  await page.goto("/backups");
  await page.getByRole("button", { name: "Restore this backup", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Restore a backup" })).toBeVisible();
}
const submit = (page: Page) => page.getByRole("button", { name: "Restore panel data", exact: true });
async function confirm(page: Page) {
  await page.getByLabel("Type RESTORE to confirm replacement").fill("RESTORE");
  await expect(submit(page)).toBeEnabled(); await submit(page).click();
}

test("restore requires explicit typed confirmation and binds exact archive", async ({ page }) => {
  const f = await fixture(page); await selectArchive(page);
  await expect(submit(page)).toBeDisabled();
  await page.getByLabel("Type RESTORE to confirm replacement").fill("restore"); await expect(submit(page)).toBeDisabled();
  await confirm(page); expect(f.writes).toHaveLength(1);
  expect(f.writes[0]).toMatchObject({ source_kind: "archive", source_id: archiveID, sha256: sha, confirmation: "RESTORE" });
  await expect(page.getByText("Panel data restored and service health confirmed.", { exact: true })).toHaveCount(0);
  f.jobs[0] = { ...f.jobs[0], state: "succeeded", phase: "complete", data_restored: true, table_count: 60, finished_unix: now(), revision: 5 };
  await expect(page.getByText("Panel data restored and service health confirmed.", { exact: true })).toBeVisible();
});
test("closing confirmation before submission never changes data", async ({ page }) => {
  const f = await fixture(page); await selectArchive(page);
  await page.getByRole("button", { name: "Keep current data" }).click();
  await expect(page.getByRole("dialog")).not.toBeVisible(); expect(f.writes).toHaveLength(0);
});
test("received upload restores through the same confirmation with its own ID", async ({ page }) => {
  const f = await fixture(page); await page.goto("/backups");
  await page.getByRole("button", { name: "Restore this file" }).click(); await confirm(page);
  expect(f.writes[0]).toMatchObject({ source_kind: "upload", source_id: uploadID, sha256: sha });
});
test("lost restore response is reconciled after reload without another write", async ({ page }) => {
  const f = await fixture(page, { lose: true }); await selectArchive(page); await confirm(page);
  await expect.poll(() => f.writes.length).toBe(1); await page.reload();
  await expect(page.getByRole("region", { name: "Restore status", exact: true }).first()).toBeVisible();
  expect(f.writes).toHaveLength(1);
  await expect(page.getByRole("button", { name: "Retry the same request" })).toHaveCount(0);
});
test("unconfirmed restore is only retried explicitly with the unchanged request", async ({ page }) => {
  const f = await fixture(page, { lose: true, hide: true }); await selectArchive(page); await confirm(page);
  await expect.poll(() => f.writes.length).toBe(1); await page.reload();
  const retry = page.getByRole("button", { name: "Retry the same request" }); await expect(retry).toBeEnabled();
  expect(f.writes).toHaveLength(1); await retry.click(); await expect.poll(() => f.writes.length).toBe(2);
  expect(f.writes[1]).toEqual(f.writes[0]); expect(f.jobs).toHaveLength(1);
});
test("offline state and invalid success cannot falsely claim restored data", async ({ page }) => {
  const f = await fixture(page); await selectArchive(page); await confirm(page);
  f.jobs[0] = { ...f.jobs[0], state: "succeeded", phase: "complete", finished_unix: now(), table_count: 60, data_restored: false };
  await expect(page.getByText(/Connection interrupted. This is the last confirmed restore state/)).toBeVisible();
  await expect(page.getByText("Panel data restored and service health confirmed.", { exact: true })).toHaveCount(0);
});
test("recovery attention never becomes an ordinary failed or successful restore", async ({ page }) => {
  const f = await fixture(page); await selectArchive(page); await confirm(page);
  f.jobs[0] = { ...f.jobs[0], state: "recovery_required", phase: "starting_restored", data_restored: true, table_count: 60 };
  await expect(page.getByText(/Recovery needs operator attention/)).toBeVisible();
  await expect(page.getByText("Panel data restored and service health confirmed.", { exact: true })).toHaveCount(0);
});
test("disabled installation hides both restore actions", async ({ page }) => {
  const f = await fixture(page, { disabled: true }); await page.goto("/backups");
  await expect(page.getByRole("button", { name: "Create backup", exact: true })).toBeEnabled();
  await expect(page.getByRole("button", { name: /^Restore this/ })).toHaveCount(0); expect(f.writes).toHaveLength(0);
});
test("representative never requests or sees restore controls", async ({ page }) => {
  const f = await fixture(page, { role: "representative" }); await page.goto("/backups");
  await expect(page).toHaveURL(/\/clients$/); expect(f.reads).toHaveLength(0); expect(f.writes).toHaveLength(0);
});
