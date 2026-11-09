import { defineConfig } from "@playwright/test"

// Pure contract/math tests require neither a browser nor a development server.
export default defineConfig({
  testDir: "./tests/unit",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: 0,
  reporter: "list",
})
