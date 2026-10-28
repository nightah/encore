import { defineConfig, devices } from "@playwright/test";

import { AUTH_STATE, realServer } from "@e2e/real/env";

/**
 * Two ways to run E2E:
 *
 * - `pnpm test:e2e:mock` (E2E_MOCK=1): starts `vite --mode mock` (MSW in the browser) and runs `e2e/mock/` and the shared specs. No Go server needed.
 * - `pnpm test:e2e`: builds the UI and Encore (or uses `E2E_ENCORE_BIN`), then starts a fake Kodi and a fresh Encore through `e2e/real/global-setup.ts`, runs the first-run journey in `e2e/real/`, then the shared specs signed in as the admin it created.
 *
 * Shared specs live directly in `e2e/` and must pass in both modes.
 */
const mock = !!process.env.E2E_MOCK;
// Not the dev server's port: an existing server on mockPort is reused outside CI, and a dev server there isn't serving the mock.
const mockPort = Number(process.env.E2E_MOCK_PORT ?? 3001);
const baseURL = mock ? `http://127.0.0.1:${mockPort}/` : realServer.baseURL;
const chromium = devices["Desktop Chrome"];

export default defineConfig({
  forbidOnly: !!process.env.CI,
  fullyParallel: true,
  reporter: process.env.CI ? "github" : "list",
  retries: process.env.CI ? 2 : 0,
  testDir: "./e2e",
  use: { baseURL, trace: "on-first-retry" },
  ...(mock
    ? {
        projects: [{ name: "chromium", use: { ...chromium } }],
        testIgnore: ["real/**", "screenshots/**"],
        webServer: {
          command: `pnpm exec vite --mode mock --host 127.0.0.1 --port ${mockPort} --strictPort`,
          reuseExistingServer: !process.env.CI,
          timeout: 60_000,
          url: baseURL,
        },
      }
    : {
        globalSetup: "./e2e/real/global-setup.ts",
        projects: [
          // The journey creates the admin account, so it runs first, once, on the fresh server. A retry would find setup already done and fail at its first step, hiding the real failure, so it keeps a trace of the one run instead.
          {
            name: "journey",
            retries: 0,
            testMatch: "real/**/*.spec.ts",
            use: { ...chromium, trace: "retain-on-failure" },
          },
          {
            dependencies: ["journey"],
            name: "chromium",
            testIgnore: ["mock/**", "real/**", "screenshots/**"],
            use: { ...chromium, storageState: AUTH_STATE },
          },
        ],
      }),
});
