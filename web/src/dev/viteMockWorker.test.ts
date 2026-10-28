import { mkdtempSync, readFileSync } from "node:fs";
import { type IncomingHttpHeaders, get } from "node:http";
import { createRequire } from "node:module";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { type ViteDevServer, createServer, resolveConfig } from "vite";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

import { server as mockApi } from "@mocks/node";

const configFile = resolve(process.cwd(), "vite.config.ts");

async function startVite(mode: string): Promise<{ port: number; vite: ViteDevServer }> {
  const vite = await createServer({
    // A throwaway cache: the development cache belongs to any dev server running from this checkout, and closing this one would clean it up underneath it.
    cacheDir: mkdtempSync(join(tmpdir(), "encore-vite-test-")),
    configFile,
    logLevel: "silent",
    mode,
    server: { port: 0, strictPort: false },
  });
  await vite.listen();
  return { port: Number(new URL(vite.resolvedUrls?.local[0] ?? "").port), vite };
}

function fetchWorker(port: number): Promise<{ body: string; headers: IncomingHttpHeaders; status: number }> {
  return new Promise((resolve, reject) => {
    const req = get({ host: "localhost", path: "/mockServiceWorker.js", port }, (res) => {
      let body = "";
      res.setEncoding("utf8");
      res.on("data", (chunk: string) => {
        body += chunk;
      });
      res.on("end", () => resolve({ body, headers: res.headers, status: res.statusCode ?? 0 }));
    });
    req.on("error", reject);
  });
}

// The mock API's service worker must be the one from the installed msw (the page's msw refuses a worker from another version) and must never reach a production build.
describe("mock service worker", () => {
  let mock: { port: number; vite: ViteDevServer };
  let dev: { port: number; vite: ViteDevServer };

  beforeAll(async () => {
    // These requests go to real servers, not the mock API.
    mockApi.close();
    mock = await startVite("mock");
    dev = await startVite("development");
  }, 30_000);

  afterAll(async () => {
    await mock?.vite.close();
    await dev?.vite.close();
    mockApi.listen({ onUnhandledFrame: "error" });
  });

  it("serves the installed msw's worker in mock mode", async () => {
    const installed = readFileSync(createRequire(configFile).resolve("msw/mockServiceWorker.js"), "utf8");

    const { body, headers, status } = await fetchWorker(mock.port);

    expect(status).toBe(200);
    expect(headers["content-type"]).toMatch(/javascript/);
    expect(headers["service-worker-allowed"]).toBe("/");
    expect(body).toBe(installed);
  });

  it("doesn't serve a worker outside mock mode", async () => {
    const { body } = await fetchWorker(dev.port);

    expect(body).not.toContain("PACKAGE_VERSION");
  });

  it("leaves the worker out of builds", async () => {
    for (const mode of ["production", "mock"]) {
      const config = await resolveConfig({ configFile, logLevel: "silent", mode }, "build");
      expect(config.plugins.map((plugin) => plugin.name)).not.toContain("msw");
    }
  });
});
