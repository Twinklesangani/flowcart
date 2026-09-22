import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: "list",
  use: {
    baseURL: process.env.WEB_URL ?? "http://127.0.0.1:3000",
    trace: "retain-on-failure",
    video: "off",
    ...devices["Desktop Chrome"],
  },
});
