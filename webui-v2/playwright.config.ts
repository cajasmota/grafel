import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./e2e",
  timeout: 20_000,
  use: {
    baseURL: "http://localhost:47280",
    trace: "on-first-retry",
  },
  webServer: {
    command: "npm run dev",
    port: 47280,
    reuseExistingServer: true,
    timeout: 30_000,
  },
});
