// Copyright (c) 2026 Super Durable, Inc.
//
// Licensed under the Sustainable Use License 1.0.
// You may not use this file except in compliance with the License.
// See the LICENSE file in the repository root.
//
// SPDX-License-Identifier: LicenseRef-Sustainable-Use-1.0

import { defineConfig } from "@playwright/test";
const baseURL = process.env.SUPERVERSE_E2E_BASE_URL;
if (!baseURL)
  throw new Error("SUPERVERSE_E2E_BASE_URL must identify a running Local Native, Local Kind or hosted V2 Studio");
if (process.env.SUPERVERSE_STUDIO_MOCK === "true") throw new Error("Real E2E cannot run with Studio mock mode");
// Credential-bearing local runs may disable captured browser artifacts while
// retaining assertions, JSON results and coverage.
const captureOff = process.env.SUPERVERSE_E2E_CAPTURE === "off";
// Long real provider calls left idle API-request sockets that reset on reuse in
// local acceptance. Use fresh HTTP connections; do not retry failed operations
// or relax any snapshot, Stream, identity or provider-outcome assertion.
export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 20 * 60_000,
  expect: { timeout: 30_000 },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  projects: [{ name: "chromium", use: { browserName: "chromium" } }],
  reporter: [["line"], ["json", { outputFile: "test-results/e2e-results.json" }]],
  use: {
    baseURL,
    extraHTTPHeaders: { Connection: "close" },
    headless: true,
    trace: captureOff ? "off" : "retain-on-failure",
    screenshot: captureOff ? "off" : "only-on-failure",
    ...(captureOff ? { video: "off" as const } : {}),
    ignoreHTTPSErrors: false,
    launchOptions: {
      executablePath:
        process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ??
        (process.platform === "darwin" ? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" : undefined),
    },
  },
});
