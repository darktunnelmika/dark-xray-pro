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
  await page.goto(base.href);
  await page.getByRole('button', { name: 'Business', exact: true }).click();
  await page.getByLabel('Username', { exact: true }).fill(control.username);
  await page.getByLabel('Password', { exact: true }).fill(control.password);
  await page.getByRole('button', { name: 'Sign in', exact: true }).click();
  await expect(page.getByText('Owner-only operations', { exact: true })).toBeVisible();

  const createRep = page.getByRole('heading', { name: 'Create representative', exact: true }).locator('..');
  await createRep.getByLabel('Representative ID', { exact: true }).fill('live-usage-rep');
  await createRep.getByLabel('Display name', { exact: true }).fill('Live acceptance representative');
  await createRep.getByRole('button', { name: 'Create representative', exact: true }).click();
  await expect(page.locator('.business-records').getByText('Live acceptance representative (live-usage-rep)', { exact: true })).toBeVisible();

  const grant = page.getByRole('heading', { name: 'Adjust representative credit', exact: true }).locator('..');
  await grant.getByLabel('Representative ID', { exact: true }).fill('live-usage-rep');
  await grant.getByLabel('Volume delta (GB, +/-)', { exact: true }).fill('0.05');
  await grant.getByLabel('Reason', { exact: true }).fill('Disposable real-browser acceptance');
  await grant.getByRole('button', { name: 'Record credit transaction', exact: true }).click();
  await expect(grant.getByLabel('Reason', { exact: true })).toHaveValue('');

  const form = page.locator('.business-client-form');
  // The labelled ID row also contains a Random button; scope the input to
  // this form rather than assuming that the full accessible label is exact.
  await form.getByLabel('Client ID').fill('live-usage-client');
  await form.getByLabel('Client name', { exact: true }).fill('Live metered customer');
  await form.getByLabel('Representative ID', { exact: true }).selectOption('live-usage-rep');
  await form.getByLabel('Plan type', { exact: true }).selectOption('volume');
  await form.getByLabel('Plan volume (GB)', { exact: true }).fill('0.01');
  await form.getByRole('button', { name: 'Create pending client', exact: true }).click();
  await expect(page.locator('.business-records strong').filter({ hasText: 'Live metered customer (live-usage-client)' })).toBeVisible();
  await page.getByRole('button', { name: 'Manage', exact: true }).click();
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
