// Launched only by the opt-in Go real-core acceptance test. No mocked routes.
import { chromium, expect } from '@playwright/test';
import { readFile, access } from 'node:fs/promises';
import { setTimeout as delay } from 'node:timers/promises';

const base = new URL(process.env.DARK_PRO_QA_UI_URL || '');
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1') throw new Error('QA loopback only');
const control = JSON.parse(await readFile(process.env.DARK_PRO_QA_CONTROL, 'utf8'));
const browser = await chromium.launch({ headless: true });
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 900 } });
  page.setDefaultTimeout(15000);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.goto(new URL("/representatives", base).href);
  await page.getByLabel('Username', { exact: true }).fill(control.username);
  await page.getByLabel('Password', { exact: true }).fill(control.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByText('Owner-only operations', { exact: true })).toBeVisible();

  // V2 requires opening the single creation dialog, then the profile.
  // Keep all existing real SQLite/Core/credit and usage assertions below.
  await page.getByRole('button', { name: 'Create representative', exact: true }).click();
  const createRep = page.getByRole('dialog', { name: 'Create representative' });
  await createRep.getByLabel('Representative ID').fill('live-usage-rep');
  await createRep.getByLabel('Display name', { exact: true }).fill('Live acceptance representative');
  await createRep.getByRole('button', { name: 'Create account', exact: true }).click();
  await expect(createRep).toHaveCount(0);
  await page.getByRole('button', { name: 'Back to directory', exact: true }).click();
  await expect(page.locator('.rep-dir-row').filter({ hasText: 'live-usage-rep' })
    .getByText('Live acceptance representative', { exact: true })).toBeVisible();
  await page.locator('.rep-dir-row').filter({ hasText: 'live-usage-rep' })
    .getByRole('button', { name: 'Manage account', exact: true }).click();
  await page.getByRole('button', { name: 'Credit', exact: true }).click();
  const grant = page.locator('.rep-v2-detail-form');
  await grant.getByLabel('Volume delta (GB, +/-)', { exact: true }).fill('0.05');
  await grant.getByLabel('Reason', { exact: true }).fill('Disposable real-browser acceptance');
  await grant.getByRole('button', { name: 'Record credit transaction', exact: true }).click();
  await expect(grant.getByLabel('Reason', { exact: true })).toHaveValue('');

  await page.getByRole('button', { name: 'Clients', exact: true }).click();
  const form = page.locator('.business-client-form');
  // Scope to the real customer form. Implicit labels include the Random
  // button or select option text; do not assume an option-free exact name.
  await form.getByLabel('Client ID').fill('live-usage-client');
  await form.getByLabel('Client name', { exact: true }).fill('Live metered customer');
  await form.getByLabel('Representative ID').selectOption('live-usage-rep');
  await form.getByLabel('Plan type').selectOption('volume');
  await form.getByLabel('Plan volume (GB)', { exact: true }).fill('0.01');
  await form.getByRole('button', { name: 'Create pending client', exact: true }).click();
  const storedClient = page.locator('.clients-explorer-row').filter({ hasText: 'live-usage-client' });
  await expect(storedClient.getByText('Live metered customer', { exact: true })).toBeVisible();
  await storedClient.getByRole('button', { name: 'Manage', exact: true }).click();
  const card = page.getByRole('region', { name: 'Recorded client usage', exact: true });
  await expect(card.getByRole('status')).toHaveText('Eligible in the quota ledger only');
  await expect(card.locator('dl > div').filter({ hasText: 'Total recorded' }).locator('dd')).toHaveText('0 bytes');
  process.stdout.write('NATIVE_UI_CUSTOMER_CREATED\n');

  const end = Date.now() + 45000;
  let ready = false;
  while (Date.now() < end) {
    try { await access(control.receipt_marker); ready = true; break; } catch { await delay(100); }
  }
  if (!ready) throw new Error('No real core/SQLite commit proof');
  const proof = JSON.parse(await readFile(control.receipt_marker, 'utf8'));
  await card.getByRole('button', { name: 'Refresh usage', exact: true }).click();
  await expect(card.getByRole('status')).toHaveText('Volume exhausted');
  await expect(card.locator('dl > div').filter({ hasText: 'Total recorded' }).locator('dd')).toHaveText(BigInt(proof.total).toLocaleString('en-US') + ' bytes');
  await expect(card.locator('dl > div').filter({ hasText: 'Remaining' }).locator('dd')).toHaveText('0 bytes');
  // Check a real server-rendered API response again after changing language.
  await page.getByRole('button', { name: 'Switch language', exact: true }).click();
  await expect(card.getByRole('status')).toHaveText('حجم تمام شده');
  await page.setViewportSize({ width: 320, height: 800 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 2)).toBe(true);
  if (errors.length) throw new Error('Browser errors: ' + errors.join('; '));
  process.stdout.write('NATIVE_UI_COMMITTED_USAGE_VERIFIED\n');
} finally {
  await browser.close();
}
