import { defineConfig, devices } from "@playwright/test"
const external = process.env.E2E_DASHBOARD_BASE_URL
export default defineConfig({
  testDir: "./tests", testMatch: ["admin-dashboard.spec.ts", "treasury-outflow.spec.ts"], workers: 1, timeout: 40_000, reporter: "list",
  use: { baseURL: external ?? "http://localhost:3107", screenshot: "only-on-failure", ignoreHTTPSErrors: !!external },
  projects: [{ name: "chrome", use: { ...devices["Desktop Chrome"], channel: "chrome" } }],
  webServer: external ? undefined : { command: "npm run dev -- --port 3107", url: "http://localhost:3107", reuseExistingServer: true, timeout: 120_000 },
})
