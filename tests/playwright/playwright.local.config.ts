import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: ".",
  testMatch: "observability.spec.ts",
  outputDir: "./artifacts/results",
  reporter: [
    ["list"],
    ["html", { outputFolder: "./artifacts/report", open: "never" }],
  ],
  use: {
    baseURL: process.env.WEB_URL || "http://127.0.0.1:4318",
    viewport: { width: 1440, height: 1000 },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  workers: 1,
  retries: 0,
  timeout: 20000,
});
