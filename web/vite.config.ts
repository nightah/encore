/// <reference types="vitest/config" />
import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import react from "@vitejs/plugin-react";
import { msw } from "msw/vite";
import { type Plugin, defineConfig } from "vite";

// Relative, unlike the rest of the code: Vite loads this file before its aliases exist. The extension is for Vite's native config loader.
import { baseUIEntries } from "./src/dev/baseUI.ts";

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
  optimizeDeps: { include: baseUIEntries(root) },
  plugins: [
    // Must run before the React plugin.
    tanstackRouter({ autoCodeSplitting: true, target: "react" }),
    react(),
    tailwindcss(),
    keepGitkeep(),
    mode === "mock" ? mockServiceWorker() : null,
  ],
  resolve: {
    // One alias per top-level folder: @api/…, @components/…; @src for files directly in src.
    alias: {
      "@api": resolve(root, "src/api"),
      "@components": resolve(root, "src/components"),
      "@dev": resolve(root, "src/dev"),
      "@e2e": resolve(root, "e2e"),
      "@features": resolve(root, "src/features"),
      "@i18n": resolve(root, "src/i18n"),
      "@lib": resolve(root, "src/lib"),
      "@mocks": resolve(root, "src/mocks"),
      "@routes": resolve(root, "src/routes"),
      "@src": resolve(root, "src"),
      "@styles": resolve(root, "src/styles"),
      "@test": resolve(root, "src/test"),
      "@theme": resolve(root, "src/theme"),
    },
  },
  server: {
    // Extra hostnames the dev server answers to, comma-separated, for reaching it through a reverse proxy (e.g. VITE_ALLOWED_HOSTS=dev.example.net). Vite rejects other Host headers.
    allowedHosts: process.env.VITE_ALLOWED_HOSTS?.split(",")
      .map((h) => h.trim())
      .filter(Boolean),
    port: 3000,
    proxy: {
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
    // `pnpm test:coverage` (CI) writes coverage/lcov.info for Codecov and fails below the thresholds; generated code, the mock API, test helpers and main.tsx's bootstrapping (each piece it calls is tested on its own) aren't counted, as in .codecov.yml.
    coverage: {
      exclude: [
        "src/api/generated/**",
        "src/mocks/**",
        "src/test/**",
        "src/dev/**",
        "src/main.tsx",
        "src/routeTree.gen.ts",
        "**/*.test.{ts,tsx}",
      ],
      include: ["src/**/*.{ts,tsx}"],
      provider: "v8",
      reporter: ["text-summary", "lcov"],
      thresholds: {
        branches: 94,
        functions: 95,
        lines: 95,
        statements: 95,
      },
    },
    css: false,
    environment: "jsdom",
    globals: true,
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test/setup.ts"],
  },
}));
