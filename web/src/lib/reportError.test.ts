import { beforeEach, describe, expect, it } from "vitest";

import { ApiError, setCsrfToken } from "@api/client";
import { reportError, resetErrorReports } from "@lib/reportError";
import { clientErrors } from "@mocks/handlers";
import { isServerUnreachable } from "@routes/__root";

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
});

describe("isServerUnreachable", () => {
  it("tells an unreachable server from a page that broke", () => {
    expect(isServerUnreachable(new ApiError("bad gateway", 502))).toBe(true);
    expect(isServerUnreachable(new TypeError("Failed to fetch"))).toBe(true);
    expect(isServerUnreachable(new ApiError("not found", 404))).toBe(false);
    expect(isServerUnreachable(new TypeError("chip is undefined"))).toBe(false);
  });
});
