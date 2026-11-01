import { defineConfig, devices } from "@playwright/test";

/**
 * `pnpm screenshots` captures each page of the web interface, light and dark, with sensitive details replaced by examples, for the docs site and the README. See `capture.spec.ts` for the options.
 *
 * Without SCREENSHOT_URL it starts `vite --mode mock` and captures the mock data; with it, it signs in to that running Encore.
 */
const target = process.env.SCREENSHOT_URL;
const mockPort = Number(process.env.E2E_MOCK_PORT ?? 5177);
const baseURL = target ? `${target.replace(/\/+$/, "")}/` : `http://127.0.0.1:${mockPort}/`;

export default defineConfig({
  outputDir: "../../test-results/screenshots",
  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"], deviceScaleFactor: 2, viewport: { height: 900, width: 1440 } },
    },
    // An iPhone-sized screen in Chromium, for the menu bar and the phone layouts; images get a -phone suffix.
    {
      name: "phone",
      use: {
        ...devices["Desktop Chrome"],
        deviceScaleFactor: 2,
        hasTouch: true,
        isMobile: true,
        viewport: { height: 844, width: 390 },
      },
    },
  ],
  reporter: "list",
  testDir: ".",
  timeout: 120_000,
  use: { baseURL, ignoreHTTPSErrors: !!process.env.SCREENSHOT_INSECURE },
  workers: 1,
  ...(target
    ? {}
    : {
        webServer: {
          command: `pnpm exec vite --mode mock --host 127.0.0.1 --port ${mockPort} --strictPort`,
          cwd: "../..",
          reuseExistingServer: true,
          timeout: 60_000,
          url: baseURL,
        },
      }),
});
