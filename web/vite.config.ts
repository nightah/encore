/// <reference types="vitest/config" />
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { playwright } from "@vitest/browser-playwright";
import { msw } from "msw/vite";
import { type Plugin, defineConfig } from "vite";

// Relative, unlike the rest of the code: Vite loads this file before its aliases exist. The extension is for Vite's native config loader.
import { baseUIEntries } from "./src/dev/baseUI.ts";
import { testDeps } from "./src/dev/testDeps.ts";

const root = fileURLToPath(new URL(".", import.meta.url));
const outDir = resolve(root, "../internal/server/dist");
const apiTarget = process.env.ENCORE_API_URL ?? "http://127.0.0.1:8080";

/** `emptyOutDir` wipes internal/server/dist on every build, including the committed `.gitkeep` that keeps `//go:embed all:dist` compiling on a clean checkout. Put it back. */
function keepGitkeep(): Plugin {
  return {
    apply: "build",
    closeBundle() {
      writeFileSync(resolve(outDir, ".gitkeep"), "");
    },
    name: "encore:keep-gitkeep",
  };
}

/**
 * `pnpm dev:mock` (`vite --mode mock`) serves the API from MSW in the browser. msw's plugin serves its service worker straight from the installed package, so the worker always matches the msw the page runs. `apply: "serve"` keeps it out of builds: in worker-only mode the plugin would otherwise emit the worker into any build it's part of.
 */
function mockServiceWorker(): Plugin {
  return { ...msw({ mode: "worker-only" }), apply: "serve" };
}

export default defineConfig(({ mode }) => ({
  // Relative asset URLs so the Go server can mount the UI under any base path.
  base: "./",
  build: {
    assetsDir: "static",
    emptyOutDir: true,
    outDir,
    sourcemap: false,
  },
  // One dependency cache per mode: the dev server and a mock-mode server (pnpm dev:mock, the mock e2e run) pre-bundle differently, and a shared cache would make each rebuild it under the other, breaking open pages ("error loading dynamically imported module").
  cacheDir: resolve(root, "node_modules/.vite", mode),
  optimizeDeps: { include: [...new Set([...baseUIEntries(root), ...(mode === "test" ? testDeps(root) : [])])] },
  plugins: [
    // Must run before the React plugin. No route HMR glue under Vitest: it never runs there (no import.meta.hot), and its source map points back into each route file, so coverage counted it as untested route code.
    tanstackRouter({ autoCodeSplitting: true, codeSplittingOptions: { addHmr: mode !== "test" }, target: "react" }),
    react(),
    tailwindcss(),
    keepGitkeep(),
    mode === "mock" ? mockServiceWorker() : null,
  ],
  resolve: { tsconfigPaths: true },
  server: {
    // Extra hostnames the dev server answers to, comma-separated, for reaching it through a reverse proxy (e.g. VITE_ALLOWED_HOSTS=dev.example.net). Vite rejects other Host headers.
    allowedHosts: process.env.VITE_ALLOWED_HOSTS?.split(",")
      .map((h) => h.trim())
      .filter(Boolean),
    port: 3000,
    // Not under Vitest: the tests have their own mock API, and what they load beyond it (a cover image) must not reach a real Encore.
    proxy:
      mode === "test"
        ? undefined
        : {
            // Server-sent events: keep the connection open indefinitely and never buffer.
            "/api/v1/events": {
              changeOrigin: true,
              configure(proxy) {
                // Only adjust the headers here: the proxy copies them to the response after this handler, so flushing headers early would send them without Content-Type: text/event-stream, and EventSource rejects such a stream.
                proxy.on("proxyRes", (proxyRes) => {
                  proxyRes.headers["cache-control"] = "no-cache, no-transform";
                  proxyRes.headers["x-accel-buffering"] = "no";
                });
              },
              headers: { Accept: "text/event-stream" },
              proxyTimeout: 0,
              target: apiTarget,
              timeout: 0,
            },
            // Everything else under /api. A regular expression that leaves out the event stream, so the two rules don't depend on their order.
            "^/api/(?!v1/events)": {
              changeOrigin: true,
              target: apiTarget,
            },
          },
    strictPort: true,
  },
  test: {
    // `pnpm test:coverage` (CI) writes coverage/lcov.info for Codecov and fails below the thresholds; generated code, the mock API, test helpers, main.tsx's bootstrapping (each piece it calls is tested on its own) and pageLocation.ts (the real reload and redirect, which tests replace) aren't counted, as in .codecov.yml.
    coverage: {
      exclude: [
        "src/api/generated/**",
        "src/mocks/**",
        "src/test/**",
        "src/dev/**",
        "src/main.tsx",
        "src/lib/pageLocation.ts",
        "src/routeTree.gen.ts",
        "**/*.test.{ts,tsx}",
      ],
      include: ["src/**/*.{ts,tsx}"],
      provider: "v8",
      reporter: ["text-summary", "lcov"],
      thresholds: {
        branches: 99,
        functions: 100,
        lines: 99,
        statements: 99,
      },
    },
    css: false,
    globals: true,
    // Tests run in headless Chromium, against the mock API's service worker; `*.node.test.ts` files read the repository or start servers, so they run in Node.
    projects: [
      {
        extends: true,
        test: { environment: "node", include: ["src/**/*.node.test.ts"], name: "node" },
      },
      {
        extends: true,
        test: {
          browser: {
            enabled: true,
            headless: true,
            instances: [{ browser: "chromium" }],
            // The same browser settings on every machine, whatever its own language, time zone and colour scheme.
            provider: playwright({
              contextOptions: { colorScheme: "light", locale: "en-US", timezoneId: "Australia/Melbourne" },
            }),
            screenshotFailures: false,
            // A laptop-sized page, with the rail and side-by-side panes; phone layouts set their own size with page.viewport.
            viewport: { height: 900, width: 1280 },
          },
          exclude: ["src/**/*.node.test.ts"],
          include: ["src/**/*.test.{ts,tsx}"],
          name: "browser",
          setupFiles: ["./src/test/setup.ts"],
        },
      },
    ],
  },
}));
