import { HttpResponse, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";

import { ApiError, getCsrfToken, setCsrfToken } from "@api/client";
import { installErrorReporting, reportError, resetErrorReports } from "@lib/reportError";
import { clientErrors, getDb } from "@mocks/handlers";
import { isServerUnreachable } from "@routes/__root";
import { server } from "@test/server";
import { recordRequests } from "@test/utils";

describe("reportError", () => {
  beforeEach(() => {
    clientErrors.length = 0;
    resetErrorReports();
  });

  it("reports an error once, with the page and stack, fetching a CSRF token first if needed", async () => {
    setCsrfToken(undefined);
    const error = new TypeError('can\'t access property "tone", chip is undefined');

    await reportError("render", error);
    await reportError("render", error);

    expect(clientErrors).toHaveLength(1);
    expect(clientErrors[0]).toMatchObject({
      kind: "render",
      message: error.message,
      path: window.location.pathname,
    });
    expect(clientErrors[0]?.stack).toContain("TypeError");
  });

  it("reports non-Error values as text", async () => {
    await reportError("unhandled", "boom");
    expect(clientErrors[0]).toMatchObject({ kind: "unhandled", message: "boom", stack: "" });
  });

  it("fetches the CSRF token first and sends it with the report", async () => {
    const requests = recordRequests();
    await reportError("render", new Error("first load"));
    expect(requests.map((r) => `${r.method} ${r.path}`)).toEqual(["GET /auth/state", "POST /client-errors"]);
    expect(requests[1]?.csrf).toBe(getDb().csrf);
    expect(getCsrfToken()).toBe(getDb().csrf);
  });

  it("uses a CSRF token it already has", async () => {
    setCsrfToken("known-token");
    const requests = recordRequests();
    await reportError("render", new Error("later"));
    expect(requests.map((r) => `${r.method} ${r.path}`)).toEqual(["POST /client-errors"]);
    expect(requests[0]?.csrf).toBe("known-token");
  });

  it("still reports when the CSRF token can't be fetched", async () => {
    server.use(http.get("*/api/v1/auth/state", () => new HttpResponse(null, { status: 503 })));
    await reportError("render", new Error("no token"));
    expect(clientErrors).toEqual([expect.objectContaining({ message: "no token" })]);
    expect(getCsrfToken()).toBeUndefined();
  });

  it("reports the same message again for another kind of error", async () => {
    await reportError("render", "boom");
    await reportError("unhandled", "boom");
    expect(clientErrors.map((r) => r.kind)).toEqual(["render", "unhandled"]);
  });

  it("stops after ten reports per page load", async () => {
    for (let i = 0; i < 12; i++) await reportError("render", `error ${i}`);
    expect(clientErrors).toHaveLength(10);
    expect(clientErrors.at(-1)?.message).toBe("error 9");
  });

  it("sends an empty stack for an error that has none", async () => {
    const error = new Error("no stack");
    error.stack = undefined;
    await reportError("render", error);
    expect(clientErrors[0]).toMatchObject({ message: "no stack", stack: "" });
  });

  it("keeps reports small and never empty", async () => {
    const error = new Error("x".repeat(5000));
    error.stack = "s".repeat(20_000);
    await reportError("render", error);
    await reportError("render", "");
    expect(clientErrors[0]?.message).toHaveLength(1000);
    expect(clientErrors[0]?.stack).toHaveLength(8000);
    expect(clientErrors[1]?.message).toBe("unknown error");
  });

  it("never throws, even when the server can't be reached", async () => {
    server.use(http.post("*/api/v1/client-errors", () => HttpResponse.error()));
    await expect(reportError("render", new Error("offline"))).resolves.toBeUndefined();
  });
});

describe("installErrorReporting", () => {
  beforeEach(() => {
    clientErrors.length = 0;
    resetErrorReports();
  });

  it("reports uncaught errors and unhandled rejections", async () => {
    installErrorReporting();

    window.dispatchEvent(new ErrorEvent("error", { error: new TypeError("x is undefined"), message: "ignored" }));
    window.dispatchEvent(new ErrorEvent("error", { message: "Script error." }));
    const rejection = new Event("unhandledrejection") as Event & { reason?: unknown };
    rejection.reason = new Error("promise rejected");
    window.dispatchEvent(rejection);

    // The reports are sent concurrently, so in any order.
    await expect
      .poll(() => clientErrors.map((r) => `${r.kind}: ${r.message}`).sort())
      .toEqual(["unhandled: Script error.", "unhandled: promise rejected", "unhandled: x is undefined"]);
  });
});

describe("isServerUnreachable", () => {
  it("tells an unreachable server from a page that broke", () => {
    expect(isServerUnreachable(new ApiError("bad gateway", 502))).toBe(true);
    expect(isServerUnreachable(new TypeError("Failed to fetch"))).toBe(true);
    expect(isServerUnreachable(new ApiError("not found", 404))).toBe(false);
    expect(isServerUnreachable(new TypeError("chip is undefined"))).toBe(false);
  });
});
