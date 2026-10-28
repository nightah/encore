import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { ApiError, setCsrfToken } from "@api/client";
import { orvalFetch } from "@api/orvalFetch";
import { server } from "@mocks/node";

interface Seen {
  url: string;
  method: string;
  accept: string | null;
  csrf: string | null;
  credentials: RequestCredentials;
}

/** Answers every request to /api/v1/probe with `respond`, recording what was sent. */
function probe(respond: () => Response) {
  const seen: Seen[] = [];
  server.use(
    http.all("*/api/v1/probe", ({ request }) => {
      seen.push({
        accept: request.headers.get("Accept"),
        credentials: request.credentials,
        csrf: request.headers.get("X-CSRF-Token"),
        method: request.method,
        url: new URL(request.url).pathname + new URL(request.url).search,
      });
      return respond();
    }),
  );
  return seen;
}

describe("orvalFetch", () => {
  it("strips the generated /api/v1 prefix once and resolves to the JSON body", async () => {
    const seen = probe(() => HttpResponse.json({ ok: true }));
    await expect(orvalFetch("/api/v1/probe?x=1")).resolves.toEqual({ ok: true });
    expect(seen).toEqual([
      expect.objectContaining({ accept: "application/json", method: "GET", url: "/api/v1/probe?x=1" }),
    ]);
  });

  it("sends the CSRF token on writes but never on reads", async () => {
    setCsrfToken("tok-1");
    const seen = probe(() => HttpResponse.json({}));
    await orvalFetch("/api/v1/probe");
    await orvalFetch("/api/v1/probe", { method: "post" });
    await orvalFetch("/api/v1/probe", { method: "DELETE" });
    expect(seen.map((s) => [s.method, s.csrf])).toEqual([
      ["GET", null],
      ["POST", "tok-1"],
      ["DELETE", "tok-1"],
    ]);
  });

  it("sends writes without a CSRF header when there is no token yet", async () => {
    const seen = probe(() => HttpResponse.json({}));
    await orvalFetch("/api/v1/probe", { method: "PUT" });
    expect(seen[0]?.csrf).toBeNull();
  });

  it("keeps the caller's headers but always asks for JSON", async () => {
    let contentType: string | null = null;
    let accept: string | null = null;
    server.use(
      http.post("*/api/v1/probe", ({ request }) => {
        contentType = request.headers.get("Content-Type");
        accept = request.headers.get("Accept");
        return HttpResponse.json({});
      }),
    );
    await orvalFetch("/api/v1/probe", {
      body: "{}",
      headers: { Accept: "text/html", "Content-Type": "application/json" },
      method: "POST",
    });
    expect(contentType).toBe("application/json");
    expect(accept).toBe("application/json");
  });

  it("resolves to undefined for 204 No Content", async () => {
    probe(() => new HttpResponse(null, { status: 204 }));
    await expect(orvalFetch("/api/v1/probe", { method: "DELETE" })).resolves.toBeUndefined();
  });

  it("resolves to text for bodies that aren't JSON", async () => {
    probe(() => HttpResponse.text("server: encore\n"));
    await expect(orvalFetch("/api/v1/probe")).resolves.toBe("server: encore\n");
  });

  it("resolves to undefined when a JSON body doesn't parse", async () => {
    probe(() => new HttpResponse("{not json", { headers: { "Content-Type": "application/json" } }));
    await expect(orvalFetch("/api/v1/probe")).resolves.toBeUndefined();
  });

  it("throws an ApiError carrying the problem for error responses", async () => {
    const problem = {
      detail: "a room with that name exists",
      errors: [{ location: "body.name", message: "taken" }],
      status: 409,
      title: "Conflict",
    };
    probe(() => HttpResponse.json(problem, { headers: { "Content-Type": "application/problem+json" }, status: 409 }));
    const error = await orvalFetch("/api/v1/probe", { method: "POST" }).catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ message: "a room with that name exists", problem, status: 409 });
    expect((error as ApiError).fieldErrors).toEqual({ name: "taken" });
  });

  it("names the HTTP status for error responses without a problem", async () => {
    probe(() => HttpResponse.text("<html>Bad Gateway</html>", { status: 502 }));
    const error = await orvalFetch("/api/v1/probe").catch((e: unknown) => e);
    expect(error).toMatchObject({
      body: "<html>Bad Gateway</html>",
      message: "Request failed with HTTP 502.",
      status: 502,
    });
    expect((error as ApiError).problem).toBeUndefined();
  });

  it("turns a network failure into an ApiError with status 0", async () => {
    probe(() => HttpResponse.error());
    const error = await orvalFetch("/api/v1/probe").catch((e: unknown) => e);
    expect(error).toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ message: "The Encore server could not be reached.", status: 0 });
  });

  it("lets a cancelled request reject as an AbortError, not a connection error", async () => {
    probe(() => HttpResponse.json({}));
    const controller = new AbortController();
    controller.abort();
    const error = await orvalFetch("/api/v1/probe", { signal: controller.signal }).catch((e: unknown) => e);
    expect(error).not.toBeInstanceOf(ApiError);
    expect(error).toMatchObject({ name: "AbortError" });
  });
});
