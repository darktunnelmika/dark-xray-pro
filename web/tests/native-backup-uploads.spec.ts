import { test, expect, type Page, type Route } from "@playwright/test";
import { createHash } from "node:crypto";
import { createServer, request as httpRequest } from "node:http";
import type { AddressInfo } from "node:net";
import { readBackupStatus, type BackupStatus } from "../src/nativeBackups";
import { maxInboxBytes, maxUploadBytes, readUploadStatus, uploadPendingKey, validBackupUpload,
  type BackupUpload, type BackupUploadStatus, type PendingUpload } from "../src/nativeBackupUploads";

const base = "/api/v1/workspace/owner/backups";
const uploads = base + "/uploads";
const csrf = "c".repeat(64);
const now = () => Math.floor(Date.now() / 1000);
const data = Buffer.from("age-encryption.org/v1\nsynthetic-encrypted-backup-for-browser-tests\n");
const digest = (body: Buffer) => createHash("sha256").update(body).digest("hex");
function status(): BackupStatus {
  return { enabled: true, runner_status: "ready", recipient_fingerprint: "a".repeat(64),
    capabilities: { create: true, download: true, cancel: true, schedules: true, retention: true, upload: true, restore: false },
    settings: { revision: 1, schedule: { enabled: false, frequency: "daily", time: "03:00", timezone: "Asia/Tehran", weekday: 0 }, retention: { keep_last: 7, max_age_days: 30 } },
    next_run_unix: 0, jobs: [], archives: [] };
}
function upload(overrides: Partial<BackupUpload> = {}): BackupUpload {
  return { id: "1".repeat(32), event_id: "upload-test-event-0001", actor_id: "owner", size_bytes: data.length, sha256: digest(data),
    state: "awaiting_upload", created_unix: now(), updated_unix: now(), received_bytes: 0, verification: "unverified", downloadable: false,
    error_code: "", attempts: 0, ...overrides };
}
function inbox(): BackupUploadStatus {
  return { enabled: true, max_upload_bytes: maxUploadBytes, max_inbox_bytes: maxInboxBytes, max_entries: 4096, max_active_entries: 32, reserved_bytes: 0, uploads: [] };
}
type Write = { method: string; path: string; body?: unknown; bytes?: Buffer; headers: Record<string, string> };
async function fixture(page: Page, options: { capability?: boolean; ownerID?: string; loseCreate?: boolean; hideCreate?: boolean;
  allocationError?: { status: number; error: string };
  content?: (route: Route, record: BackupUpload, attempt: number) => Promise<void>; cancel?: (route: Route, record: BackupUpload) => Promise<void> } = {}) {
  const current = inbox(), backup = status(), writes: Write[] = [], downloads: string[] = [];
  const ownerID = options.ownerID ?? "owner";
  let createLost = options.loseCreate ?? false, hidden: BackupUpload | undefined, unavailable = false;
  backup.capabilities.upload = options.capability ?? true;
  await page.route("**/api/v1/**", async route => {
    const request = route.request(), path = new URL(request.url()).pathname, method = request.method();
    if (path === "/api/v1/session/status") return route.fulfill({ json: { authenticated: true, user: { id: ownerID, username: "upload-owner", role: "owner", representative_id: "owner" }, csrf_token: csrf, expires_unix: now() + 7200 } });
    if (path === base) return route.fulfill({ json: backup });
    if (path === uploads && method === "GET") return unavailable ? route.fulfill({ status: 503, json: { error: "backup_worker_unavailable" } }) : route.fulfill({ json: current });
    if (path === uploads && method === "POST") {
      const body = request.postDataJSON(); writes.push({ method, path, body, headers: request.headers() });
      expect(Object.keys(body).sort()).toEqual(["event_id", "sha256", "size_bytes"]); expect(request.headers()["x-csrf-token"]).toBe(csrf);
      if (options.allocationError) return route.fulfill({ status: options.allocationError.status, json: { error: options.allocationError.error } });
      let record = current.uploads.find(item => item.event_id === body.event_id) ?? (hidden?.event_id === body.event_id ? hidden : undefined);
      const replayed = !!record;
      if (!record) record = upload({ id: (current.uploads.length + 1).toString(16).padStart(32, "0"), actor_id: ownerID, event_id: body.event_id, size_bytes: body.size_bytes, sha256: body.sha256 });
      if (createLost) { createLost = false; if (options.hideCreate) hidden = record; else current.uploads.push(record); return route.abort("failed"); }
      if (!current.uploads.some(item => item.id === record!.id)) current.uploads.push(record);
      hidden = undefined; current.reserved_bytes = current.uploads.reduce((sum, item) => sum + item.size_bytes, 0);
      return route.fulfill({ status: replayed ? 200 : 201, json: { upload: record, replayed } });
    }
    if (path.startsWith(uploads + "/")) {
      const [, , action] = path.slice(uploads.length).split("/");
      const id = path.slice(uploads.length).split("/")[1];
      const record = current.uploads.find(item => item.id === id);
      if (!record) return route.fulfill({ status: 404, json: { error: "upload_not_found" } });
      if (method === "GET" && action === "download") {
        downloads.push(request.resourceType());
        return route.fulfill({ status: 200, contentType: "application/octet-stream", headers: { "Content-Disposition": 'attachment; filename="uploaded-copy.backup"' }, body: data });
      }
      if (method === "GET") return route.fulfill({ json: record });
      if (method === "PUT" && action === "content") {
        writes.push({ method, path, bytes: request.postDataBuffer() ?? Buffer.alloc(0), headers: request.headers() });
        expect(request.headers()["content-type"]).toBe("application/octet-stream");
        expect(request.headers()["x-csrf-token"]).toBe(csrf); expect(request.headers()["x-backup-sha256"]).toBe(record.sha256);
        record.attempts++; record.state = "receiving";
        if (options.content) return options.content(route, record, record.attempts);
        record.state = "received"; record.received_bytes = record.size_bytes; record.verification = "ciphertext_checksum_verified"; record.downloadable = true;
        return route.fulfill({ json: { upload: record, replayed: false } });
      }
      if (method === "POST") {
        const body = request.postDataJSON(); writes.push({ method, path, body, headers: request.headers() });
        expect(body).toEqual({ action }); expect(request.headers()["x-csrf-token"]).toBe(csrf);
        if (action === "cancel") {
          if (options.cancel) return options.cancel(route, record);
          record.state = "cancelled"; record.downloadable = false;
        } else if (action === "remove") { record.state = "deleted"; record.downloadable = false; current.reserved_bytes -= record.size_bytes; }
        else return route.fulfill({ status: 404, json: { error: "not_found" } });
        return route.fulfill({ json: { upload: record, replayed: false } });
      }
    }
    return route.fulfill({ json: {} });
  });
  return { current, backup, writes, downloads, unavailable: (value: boolean) => { unavailable = value; } };
}
const section = (page: Page) => page.getByRole("region", { name: "Upload an encrypted copy", exact: true });
const input = (page: Page) => page.getByLabel("Encrypted backup file", { exact: true });
const start = (page: Page) => page.getByRole("button", { name: "Upload encrypted file", exact: true });
const refresh = (page: Page) => page.getByRole("button", { name: "Refresh uploaded copies", exact: true });
async function select(page: Page, body = data, name = "native-copy.backup") {
  await input(page).setInputFiles({ name, mimeType: "application/octet-stream", buffer: body });
  await expect(section(page).getByText("File checksum ready.", { exact: true })).toBeVisible();
}
function markReceived(record: BackupUpload) {
  record.state = "received"; record.received_bytes = record.size_bytes; record.verification = "ciphertext_checksum_verified"; record.downloadable = true;
  record.attempts = Math.max(1, record.attempts); record.error_code = "";
}

test("upload capability accepts true without enabling restoration and upload DTO never invents native verification", () => {
  expect(readBackupStatus(status()).capabilities.upload).toBe(true);
  expect(() => readBackupStatus({ ...status(), capabilities: { ...status().capabilities, restore: true } })).toThrow();
  const value = upload(); expect(validBackupUpload(value)).toBe(true);
  expect(validBackupUpload({ ...value, state: "received", verification: "ciphertext_checksum_verified" })).toBe(false);
  expect(validBackupUpload({ ...value, verification: "restore_verified" })).toBe(false);
  expect(validBackupUpload({ ...value, state: "awaiting_upload", downloadable: true })).toBe(false);
  expect(validBackupUpload({ ...value, size_bytes: maxUploadBytes + 1 })).toBe(false);
  const received = upload(); markReceived(received); expect(validBackupUpload(received)).toBe(true);
  expect(validBackupUpload({ ...received, downloadable: false })).toBe(false);
  expect(validBackupUpload({ ...received, attempts: 0 })).toBe(false);
  expect(validBackupUpload({ ...received, error_code: "publication_unknown" })).toBe(false);
  expect(validBackupUpload({ ...value, received_bytes: 1 })).toBe(false);
  expect(validBackupUpload({ ...value, attempts: 1 })).toBe(false);
  const bounded = inbox(); bounded.uploads = Array.from({ length: 4096 }, (_, index) => upload({ id: index.toString(16).padStart(32, "0"), event_id: "upload-capacity-event-" + index }));
  expect(readUploadStatus(bounded)).toBe(bounded);
  expect(() => readUploadStatus({ ...bounded, uploads: [...bounded.uploads, upload()] })).toThrow();
});

test("a disabled upload capability does not expose or request the inbox", async ({ page }) => {
  const f = await fixture(page, { capability: false });
  await page.goto("/backups"); await expect(page.getByRole("button", { name: "Create backup", exact: true })).toBeEnabled();
  await expect(section(page)).toHaveCount(0); expect(f.writes).toHaveLength(0);
});

test("bounded worker hashing binds one streamed File upload to the exact ciphertext receipt", async ({ page }) => {
  const f = await fixture(page);
  await page.route("**/assets/nativeBackupUploadHash.worker-*.js", async route => {
    const response = await route.fetch();
    const guard = `const originalArrayBuffer=Blob.prototype.arrayBuffer;Blob.prototype.arrayBuffer=function(){if(this.size>1048576)throw Error('unbounded file read');return originalArrayBuffer.call(this)};`;
    await route.fulfill({ response, body: guard + await response.text() });
  });
  await page.addInitScript(() => {
    const original = XMLHttpRequest.prototype.send;
    XMLHttpRequest.prototype.send = function (body?: Document | XMLHttpRequestBodyInit | null) {
      if (body instanceof Blob && !(body instanceof File)) throw new Error("upload must send File directly");
      return original.call(this, body);
    };
  });
  const large = Buffer.alloc((3 << 20) + 17, 71); large[1024] = 29;
  await page.goto("/backups"); await select(page, large); await expect(start(page)).toBeEnabled(); await start(page).click();
  await expect(section(page).getByRole("link", { name: "Download uploaded copy" })).toBeVisible();
  expect(f.writes).toHaveLength(2); expect(f.writes[1].bytes).toEqual(large);
  expect(f.current.uploads[0].sha256).toBe(digest(large));
  await expect(section(page).getByText("Native contents have not been decrypted or validated.", { exact: true })).toBeVisible();
  expect(await page.evaluate(key => sessionStorage.getItem(key), uploadPendingKey("owner"))).toBeNull();
  await expect(section(page).getByText("Restore verified", { exact: true })).toHaveCount(0);
});

test("a lost allocation receipt is reconciled without automatically sending file bytes", async ({ page }) => {
  const f = await fixture(page, { loseCreate: true });
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect(section(page).getByRole("region", { name: "Saved upload request" })).toBeVisible();
  await refresh(page).click(); await expect(section(page).getByText("Ready to receive", { exact: true }).first()).toBeVisible();
  expect(f.writes).toHaveLength(1);
  await page.reload(); await expect(section(page).getByRole("region", { name: "Saved upload request" })).toBeVisible();
  expect(f.writes).toHaveLength(1); await select(page);
  await page.getByRole("button", { name: "Upload selected file from the beginning", exact: true }).click();
  await expect(section(page).getByRole("link", { name: "Download uploaded copy" })).toBeVisible();
  expect(f.writes.filter(write => write.path === uploads)).toHaveLength(1);
  expect(f.writes.filter(write => write.method === "PUT")).toHaveLength(1);
});

test("an unknown allocation retries the identical saved event and metadata after reload", async ({ page }) => {
  const f = await fixture(page, { loseCreate: true, hideCreate: true });
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect(section(page).getByRole("button", { name: "Retry saved request", exact: true })).toBeVisible();
  const first = f.writes[0].body; await page.reload();
  await section(page).getByRole("button", { name: "Retry saved request", exact: true }).click();
  await expect(section(page).getByText("Ready to receive", { exact: true }).first()).toBeVisible();
  expect(f.writes).toHaveLength(2); expect(f.writes[1].body).toEqual(first);
  expect(f.writes.filter(write => write.method === "PUT")).toHaveLength(0);
});

test("interrupted transfer requires the original full checksum after reload and reuses the same upload", async ({ page }) => {
  const f = await fixture(page, { content: async (route, record, attempt) => {
    if (attempt === 1) { record.state = "interrupted"; record.received_bytes = Math.floor(record.size_bytes / 2); return route.abort("failed"); }
    markReceived(record); return route.fulfill({ json: { upload: record, replayed: false } });
  } });
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect(section(page).getByText("Transfer interrupted", { exact: true }).first()).toBeVisible();
  const id = f.current.uploads[0].id;
  await page.reload(); await expect(input(page)).toBeEnabled();
  const different = Buffer.from(data); different[0] ^= 1;
  await input(page).setInputFiles({ name: "native-copy.backup", mimeType: "application/octet-stream", buffer: different });
  await expect(section(page).getByText(/This file’s size or checksum differs/)).toBeVisible();
  await expect(page.getByRole("button", { name: "Upload selected file from the beginning", exact: true })).toBeDisabled();
  expect(f.writes.filter(write => write.method === "PUT")).toHaveLength(1);
  await select(page); await page.getByRole("button", { name: "Upload selected file from the beginning", exact: true }).click();
  await expect(section(page).getByRole("link", { name: "Download uploaded copy" })).toBeVisible();
  expect(f.current.uploads).toHaveLength(1); expect(f.current.uploads[0].id).toBe(id);
  expect(f.writes.filter(write => write.method === "PUT").map(write => write.bytes)).toEqual([data, data]);
});

test("lost final receipt reconciles the saved ciphertext without sending a second upload", async ({ page }) => {
  const f = await fixture(page, { content: async (route, record) => { markReceived(record); await route.abort("failed"); } });
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect(section(page).getByRole("link", { name: "Download uploaded copy" })).toBeVisible();
  await page.reload(); await expect(section(page).getByRole("link", { name: "Download uploaded copy" })).toBeVisible();
  expect(f.writes).toHaveLength(2); expect(await page.evaluate(key => sessionStorage.getItem(key), uploadPendingKey("owner"))).toBeNull();
});

test("a late transfer error cannot replace a completion already confirmed by status polling", async ({ page }) => {
  let held: Route | undefined;
  const f = await fixture(page, { content: async (route, record) => { markReceived(record); held = route; } });
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect.poll(() => f.current.uploads[0]?.state).toBe("received"); await refresh(page).click();
  await expect(section(page).getByRole("region", { name: "Saved upload request" })).toHaveCount(0);
  await held!.abort("failed"); await expect(input(page)).toBeEnabled();
  await expect(section(page).getByRole("link", { name: "Download uploaded copy" })).toBeVisible();
  await expect(section(page).getByText(/The response was not confirmed/)).toHaveCount(0);
  expect(f.writes).toHaveLength(2);
});

test("server checking is not completion and cancellation remains pending until a server receipt", async ({ page }) => {
  let held: Route | undefined;
  let cancelAttempts = 0;
  const f = await fixture(page, { content: async (route, record) => { record.state = "checking"; record.received_bytes = record.size_bytes; held = route; },
    cancel: async (route, record) => {
      cancelAttempts++;
      if (cancelAttempts === 1) return route.abort("failed");
      record.state = "cancelled"; await route.fulfill({ json: { upload: record, replayed: false } });
    } });
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect.poll(() => f.writes.filter(write => write.method === "PUT").length).toBe(1);
  await refresh(page).click(); await expect(section(page).getByText("Checking saved bytes", { exact: true }).first()).toBeVisible();
  await expect(section(page).getByRole("link", { name: "Download uploaded copy" })).toHaveCount(0);
  const cancel = section(page).getByRole("button", { name: /^(Cancel upload|Cancel this upload request)$/ }).first();
  await cancel.click(); await expect(section(page).getByRole("button", { name: "Retry cancellation", exact: true })).toBeVisible();
  await held?.abort("failed").catch(() => {}); await page.reload();
  await section(page).getByRole("button", { name: "Retry cancellation", exact: true }).click();
  await expect(section(page).getByText("The server confirmed this upload was cancelled.", { exact: true })).toBeVisible();
  expect(f.writes.filter(write => write.method === "PUT")).toHaveLength(1);
  expect(f.writes.filter(write => write.path.endsWith("/cancel"))).toHaveLength(2);
});

test("removal shows the exact checksum and size before sending and retains a tombstone", async ({ page }) => {
  const f = await fixture(page); const record = upload(); markReceived(record); f.current.uploads.push(record); f.current.reserved_bytes = record.size_bytes;
  await page.goto("/backups"); await section(page).getByRole("button", { name: "Remove uploaded copy", exact: true }).click();
  const confirmation = section(page).getByRole("alertdialog", { name: "Remove this exact uploaded copy?" });
  await expect(confirmation.getByText(record.sha256, { exact: true })).toBeVisible(); expect(f.writes).toHaveLength(0);
  await confirmation.getByRole("button", { name: "Confirm removal", exact: true }).click();
  await expect(section(page).getByText("The uploaded copy was removed.", { exact: true })).toBeVisible();
  expect(f.current.uploads[0].state).toBe("deleted"); expect(f.writes[0].body).toEqual({ action: "remove" });
  await expect(section(page).getByRole("button", { name: "Remove uploaded copy", exact: true })).toHaveCount(0);
});

test("another owner can stream a download but cannot mutate the original actor’s upload", async ({ page, baseURL }) => {
  const f = await fixture(page, { ownerID: "second-owner" }); const record = upload(); markReceived(record); f.current.uploads.push(record);
  await page.addInitScript(() => {
    Response.prototype.blob = function () { throw new Error("Uploaded copy downloads must stream."); };
    const original = window.fetch;
    window.fetch = (...args) => { if (String(args[0]).includes("/uploads/") && String(args[0]).endsWith("/download")) throw new Error("Download must use native anchor."); return original(...args); };
  });
  // Native download requests can bypass page routing. This loopback endpoint
  // verifies the actual browser request and successful streamed file result.
  const downloadPath = uploads + "/" + record.id + "/download", upstream = new URL(baseURL!);
  let downloads = 0;
  const server = createServer((request, response) => {
    if (request.url === downloadPath) { downloads++; response.writeHead(200, { "Content-Type": "application/octet-stream", "Content-Disposition": 'attachment; filename="uploaded-copy.backup"' }); response.end(data); return; }
    const proxy = httpRequest({ hostname: "127.0.0.1", port: upstream.port, path: request.url, method: request.method }, incoming => {
      response.writeHead(incoming.statusCode ?? 502, incoming.headers); incoming.pipe(response);
    });
    proxy.on("error", () => response.writeHead(502).end()); request.pipe(proxy);
  });
  await new Promise<void>(resolve => server.listen(0, "127.0.0.1", resolve));
  try {
    await page.goto("http://127.0.0.1:" + (server.address() as AddressInfo).port + "/backups");
    const anchor = section(page).getByRole("link", { name: "Download uploaded copy", exact: true });
    await expect(anchor).toHaveAttribute("href", downloadPath);
    await expect(section(page).getByRole("button", { name: "Remove uploaded copy", exact: true })).toHaveCount(0);
    const download = page.waitForEvent("download"); await anchor.click(); const result = await download;
    expect(result.suggestedFilename()).toBe("uploaded-copy.backup"); expect(await result.failure()).toBeNull();
    expect(downloads).toBe(1); expect(f.writes).toHaveLength(0);
  } finally { server.closeAllConnections(); await new Promise<void>((resolve, reject) => server.close(error => error ? reject(error) : resolve())); }
});

test("checksum calculation can be cancelled before any request or file bytes are sent", async ({ page }) => {
  const f = await fixture(page);
  await page.route("**/assets/nativeBackupUploadHash.worker-*.js", async route => {
    const response = await route.fetch();
    const delay = `const originalArrayBuffer=Blob.prototype.arrayBuffer;Blob.prototype.arrayBuffer=async function(){await new Promise(resolve=>setTimeout(resolve,200));return originalArrayBuffer.call(this)};`;
    await route.fulfill({ response, body: delay + await response.text() });
  });
  await page.goto("/backups"); await input(page).setInputFiles({ name: "hash-cancel.backup", mimeType: "application/octet-stream", buffer: Buffer.alloc(2 << 20, 71) });
  await section(page).getByRole("button", { name: "Cancel checksum calculation", exact: true }).click();
  await expect(section(page).getByText("Checksum calculation cancelled. No file was sent.", { exact: true })).toBeVisible();
  await expect(start(page)).toBeDisabled(); await expect(input(page)).toBeEnabled(); expect(f.writes).toHaveLength(0);
});

test("an inbox outage preserves the last receipt and blocks a new upload", async ({ page }) => {
  const f = await fixture(page); const record = upload(); markReceived(record); f.current.uploads.push(record);
  await page.goto("/backups"); await expect(section(page).getByRole("link", { name: "Download uploaded copy" })).toBeVisible();
  f.unavailable(true); await refresh(page).click();
  await expect(section(page).getByText("Connection interrupted. The last confirmed upload status is shown.", { exact: true })).toBeVisible();
  await expect(section(page).getByText("Ciphertext received", { exact: true })).toBeVisible();
  await expect(input(page)).toBeDisabled(); await expect(start(page)).toBeDisabled(); expect(f.writes).toHaveLength(0);
});

test("an unavailable browser receipt store prevents the allocation and upload", async ({ page }) => {
  const f = await fixture(page);
  await page.addInitScript(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) { if (key.startsWith("dark-pro-backup-upload-v1:")) throw new Error("storage unavailable"); return original.call(this, key, value); };
  });
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect(section(page).getByText("The browser could not save the request record. No new request was sent.", { exact: true })).toBeVisible();
  expect(f.writes).toHaveLength(0);
});

test("a definite capacity rejection clears the unallocated request so an existing copy can be removed", async ({ page }) => {
  const f = await fixture(page, { allocationError: { status: 409, error: "backup_capacity" } });
  const record = upload(); markReceived(record); f.current.uploads.push(record); f.current.reserved_bytes = record.size_bytes;
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect(section(page).getByText("The inbox could not reserve this file. Review stored copies and available space before a new attempt.", { exact: true })).toBeVisible();
  await expect(section(page).getByRole("region", { name: "Saved upload request" })).toHaveCount(0);
  expect(await page.evaluate(key => sessionStorage.getItem(key), uploadPendingKey("owner"))).toBeNull();
  await section(page).getByRole("button", { name: "Remove uploaded copy", exact: true }).click();
  await section(page).getByRole("button", { name: "Confirm removal", exact: true }).click();
  await expect(section(page).getByText("The uploaded copy was removed.", { exact: true })).toBeVisible();
  expect(f.writes.filter(write => write.method === "PUT")).toHaveLength(0);
});

test("an allocation conflict preserves its original event after reload", async ({ page }) => {
  const f = await fixture(page, { allocationError: { status: 409, error: "backup_conflict" } });
  await page.goto("/backups"); await select(page); await start(page).click();
  await expect(section(page).getByRole("button", { name: "Retry saved request", exact: true })).toBeVisible();
  const saved = await page.evaluate(key => sessionStorage.getItem(key), uploadPendingKey("owner"));
  await page.reload(); await expect(section(page).getByRole("region", { name: "Saved upload request" })).toBeVisible();
  expect(await page.evaluate(key => sessionStorage.getItem(key), uploadPendingKey("owner"))).toBe(saved);
  expect(f.writes).toHaveLength(1);
});

test("uncertain publication remains visible and cannot be retried or removed from the page", async ({ page }) => {
  const f = await fixture(page); const record = upload({ state: "publication_unknown", error_code: "publication_unknown", received_bytes: data.length }); f.current.uploads.push(record);
  const saved: PendingUpload = { version: 1, actor_id: record.actor_id, event_id: record.event_id, size_bytes: record.size_bytes, sha256: record.sha256, upload_id: record.id, intent: "upload" };
  await page.addInitScript(({ key, value }) => sessionStorage.setItem(key, JSON.stringify(value)), { key: uploadPendingKey("owner"), value: saved });
  await page.goto("/backups"); await expect(section(page).getByText("Publication uncertain", { exact: true }).first()).toBeVisible();
  await expect(input(page)).toBeDisabled(); await expect(section(page).getByRole("button", { name: "Remove uploaded copy", exact: true })).toHaveCount(0);
  await expect(section(page).getByRole("button", { name: "Upload selected file from the beginning", exact: true })).toBeDisabled(); expect(f.writes).toHaveLength(0);
});

for (const theme of ["light", "dark"]) test("Persian 320px encrypted upload inbox fits in " + theme, async ({ page }) => {
  await page.setViewportSize({ width: 320, height: 820 });
  await page.addInitScript(value => { localStorage.setItem("dark-pro-lang", "fa"); localStorage.setItem("dark-pro-theme", value); }, theme);
  const f = await fixture(page); const record = upload(); markReceived(record); f.current.uploads.push(record);
  await page.goto("/backups"); const area = page.getByRole("region", { name: "بارگذاری نسخهٔ رمزگذاری‌شده", exact: true });
  await expect(area).toBeVisible(); await area.getByText("جزئیات بارگذاری", { exact: true }).click();
  await area.getByRole("button", { name: "حذف نسخهٔ بارگذاری‌شده", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", theme);
  const dimensions = await page.evaluate(() => ({ width: document.documentElement.clientWidth, scroll: document.documentElement.scrollWidth }));
  expect(dimensions.scroll).toBeLessThanOrEqual(dimensions.width + 1);
  await area.screenshot({ path: test.info().outputPath("native-upload-fa-" + theme + ".png"), animations: "disabled" });
});
