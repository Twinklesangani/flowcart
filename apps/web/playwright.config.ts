import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  timeout: 45_000,
  expect: { timeout: 10_000 },
  reporter: "list",
  use: {
    baseURL: process.env.WEB_URL ?? "http://127.0.0.1:3000",
    // Hosted traces can capture passwords, tokens, and refresh cookies.
    trace: process.env.FLOWCART_HOSTED_TEST === "1" ? "off" : "retain-on-failure",
    video: "off",
    ...devices["Desktop Chrome"],
  },
});
