import { mkdtempSync } from "node:fs";
import { type Server, createServer as createHttpServer, get } from "node:http";
import type { AddressInfo } from "node:net";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { type ViteDevServer, createServer } from "vite";
import { afterAll, beforeAll, describe, expect, it } from "vitest";

// The dev server's /api/v1/events proxy must pass the stream through with its headers: without Content-Type: text/event-stream, the browser's EventSource rejects it and the UI gets no live updates.
describe("dev server event stream proxy", () => {
  let backend: Server;
  let vite: ViteDevServer;
  let port: number;

  beforeAll(async () => {
    backend = createHttpServer((_req, res) => {
      res.writeHead(200, { "Content-Type": "text/event-stream" });
      res.write("event: hello\ndata: {}\n\n");
    });
    await new Promise<void>((resolve) => backend.listen(0, "127.0.0.1", resolve));

    process.env.ENCORE_API_URL = `http://127.0.0.1:${(backend.address() as AddressInfo).port}`;
    vite = await createServer({
      // A throwaway cache: the development cache belongs to any dev server running from this checkout, and closing this one would clean it up underneath it.
      cacheDir: mkdtempSync(join(tmpdir(), "encore-vite-test-")),
      configFile: resolve(process.cwd(), "vite.config.ts"),
      logLevel: "silent",
      server: { port: 0, strictPort: false },
    });
    await vite.listen();
    port = Number(new URL(vite.resolvedUrls?.local[0] ?? "").port);
  }, 30_000);

  afterAll(async () => {
    await vite?.close();
    backend?.closeAllConnections();
    backend?.close();
    delete process.env.ENCORE_API_URL;
  });

  it("keeps the event stream's content type and delivers events", async () => {
    const { headers, first } = await new Promise<{
      headers: Record<string, unknown>;
      first: string;
    }>((resolve, reject) => {
      const req = get({ host: "localhost", path: "/api/v1/events", port }, (res) => {
        res.setEncoding("utf8");
        res.once("data", (chunk: string) => {
          resolve({ first: chunk, headers: res.headers });
          req.destroy();
        });
      });
      req.on("error", reject);
    });

    expect(headers["content-type"]).toBe("text/event-stream");
    expect(headers["cache-control"]).toBe("no-cache, no-transform");
    expect(first).toContain("event: hello");
  });

  it("sends the rest of the API through the general rule", async () => {
    const headers = await new Promise<Record<string, unknown>>((resolve, reject) => {
      const req = get({ host: "localhost", path: "/api/v1/overview", port }, (res) => {
        resolve(res.headers);
        req.destroy();
      });
      req.on("error", reject);
    });

    // The test backend answers everything with this type.
    expect(headers["content-type"]).toBe("text/event-stream");
    expect(headers["cache-control"]).toBeUndefined();
  });
});
