import { defineConfig, devices } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: [
    "portal-invoice-detail.spec.ts",
    "portal-overview.spec.ts",
    "portal-webhooks.spec.ts",
  ],
  testIgnore: "**/unit/**",
  fullyParallel: true,
  retries: 0,
  workers: 2,
  reporter: "line",
  outputDir: "test-results/invoice-artifacts",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://127.0.0.1:3107",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    {
      name: "chrome",
      use: { ...devices["Desktop Chrome"], channel: "chrome" },
    },
  ],
});
