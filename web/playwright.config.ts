import { defineConfig, devices } from "@playwright/test";

const reviewPort = Number(process.env.DARK_PRO_TEST_PORT ?? "5173");
if (!Number.isInteger(reviewPort) || reviewPort < 1024 || reviewPort > 65535) throw new Error("invalid test port");
export default defineConfig({
  testDir: "./tests",
  timeout: 15_000,
  expect: { timeout: 7_000 },
  fullyParallel: true,
  workers: process.env.CI ? 2 : undefined,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: "list",
  use: { baseURL: `http://127.0.0.1:${reviewPort}`, trace: "retain-on-failure" },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
  webServer: {
    command: "npm run build && node tests/serve-built.mjs",
    url: `http://127.0.0.1:${reviewPort}`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
