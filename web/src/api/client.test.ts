import { describe, expect, it } from "vitest";

import {
  API_BASE,
  ApiError,
  buildUrl,
  errorMessage,
  fieldErrorsOf,
  getCsrfToken,
  problemErrorsOf,
  problemMessage,
  setCsrfToken,
} from "@api/client";

describe("ApiError", () => {
  it("keeps an object body as the problem and ignores anything else", () => {
    const problem = { detail: "nope", status: 400, title: "Bad Request" };
    expect(new ApiError("x", 400, problem).problem).toBe(problem);
    expect(new ApiError("x", 400, "plain text").problem).toBeUndefined();
    expect(new ApiError("x", 400, ["a"]).problem).toBeUndefined();
    expect(new ApiError("x", 400, null).problem).toBeUndefined();
    expect(new ApiError("x", 400).problem).toBeUndefined();
  });

  it("is an Error with its status and raw body", () => {
    const error = new ApiError("Request failed", 503, "upstream down");
    expect(error).toBeInstanceOf(Error);
    expect(error.name).toBe("ApiError");
    expect(error.message).toBe("Request failed");
    expect(error.status).toBe(503);
    expect(error.body).toBe("upstream down");
  });

  it("keys validation errors by the last segment of their location, the first message winning", () => {
    const error = new ApiError("invalid", 422, {
      errors: [
        { location: "body.password", message: "too short" },
        { location: "body.password", message: "needs a digit" },
        { location: "body.user.name", message: "required" },
        { location: "query", message: "bad" },
      ],
    });
    expect(error.fieldErrors).toEqual({ name: "required", password: "too short", query: "bad" });
  });

  it("skips validation errors without a location or a message", () => {
    const error = new ApiError("invalid", 422, {
      errors: [{ message: "where?" }, { location: "body.totp" }, { location: "", message: "empty" }],
    });
    expect(error.fieldErrors).toEqual({});
  });

  it("has no field errors without a problem body", () => {
    expect(new ApiError("down", 502, "Bad Gateway").fieldErrors).toEqual({});
  });
});

describe("problemErrorsOf and fieldErrorsOf", () => {
  const invalid = new ApiError("invalid", 422, {
    errors: [{ location: "body.oidc.client_id", message: "required" }],
  });

  it("read an API error's validation problems, raw and keyed by field", () => {
    expect(problemErrorsOf(invalid)).toEqual([{ location: "body.oidc.client_id", message: "required" }]);
    expect(fieldErrorsOf(invalid)).toEqual({ client_id: "required" });
  });

  it("find none in an API error without validation problems", () => {
    expect(problemErrorsOf(new ApiError("down", 502, "Bad Gateway"))).toEqual([]);
    expect(problemErrorsOf(new ApiError("conflict", 409, { detail: "Room exists." }))).toEqual([]);
    expect(fieldErrorsOf(new ApiError("down", 502, "Bad Gateway"))).toEqual({});
  });

  it("find none in anything that isn't an API error", () => {
    const lookalike = Object.assign(new Error("boom"), { fieldErrors: { name: "x" }, problem: { errors: [{}] } });
    for (const error of [lookalike, new TypeError("x is undefined"), "boom", undefined, null]) {
      expect(problemErrorsOf(error)).toEqual([]);
      expect(fieldErrorsOf(error)).toEqual({});
    }
  });
});

describe("problemMessage", () => {
  it("prefers the problem's detail, then its title", () => {
    expect(problemMessage({ detail: "Room exists.", title: "Conflict" }, 409)).toBe("Room exists.");
    expect(problemMessage({ title: "Conflict" }, 409)).toBe("Conflict");
    expect(problemMessage({ detail: "", title: "Conflict" }, 409)).toBe("Conflict");
  });

  it("falls back to the HTTP status for bodies that aren't problems", () => {
    expect(problemMessage(undefined, 500)).toBe("Request failed with HTTP 500.");
    expect(problemMessage("<html>Bad Gateway</html>", 502)).toBe("Request failed with HTTP 502.");
    expect(problemMessage({}, 418)).toBe("Request failed with HTTP 418.");
  });
});

describe("errorMessage", () => {
  it("uses the problem's detail or title for API errors", () => {
    expect(errorMessage(new ApiError("generic", 409, { detail: "Room exists.", title: "Conflict" }))).toBe(
      "Room exists.",
    );
    expect(errorMessage(new ApiError("generic", 404, { title: "Not Found" }))).toBe("Not Found");
  });

  it("uses the error's own message when the problem says nothing", () => {
    expect(errorMessage(new ApiError("Request failed with HTTP 502.", 502, "Bad Gateway"))).toBe(
      "Request failed with HTTP 502.",
    );
    expect(errorMessage(new ApiError("Not Found", 404, { detail: "", title: "" }))).toBe("Not Found");
  });

  it("uses the message of any other error, and a generic one for non-errors", () => {
    expect(errorMessage(new TypeError("Failed to fetch"))).toBe("Failed to fetch");
    expect(errorMessage("boom")).toBe("Something went wrong.");
    expect(errorMessage(undefined)).toBe("Something went wrong.");
  });
});

describe("CSRF token", () => {
  it("is kept until replaced, and an empty token clears it", () => {
    setCsrfToken("abc");
    expect(getCsrfToken()).toBe("abc");
    setCsrfToken("");
    expect(getCsrfToken()).toBeUndefined();
    setCsrfToken("def");
    setCsrfToken(undefined);
    expect(getCsrfToken()).toBeUndefined();
  });
});

describe("buildUrl", () => {
  it("resolves paths under the API base", () => {
    expect(API_BASE).toBe("/api/v1");
    expect(buildUrl("/events")).toBe("/api/v1/events");
  });

  it("adds the query, leaving out empty values and encoding the rest", () => {
    expect(buildUrl("/config/export", { include_secrets: false })).toBe("/api/v1/config/export?include_secrets=false");
    expect(buildUrl("/requests", { a: "", b: null, c: undefined, d: 0, q: "Böse & gut" })).toBe(
      "/api/v1/requests?d=0&q=B%C3%B6se+%26+gut",
    );
  });

  it("leaves no dangling question mark when every value is empty", () => {
    expect(buildUrl("/requests", {})).toBe("/api/v1/requests");
    expect(buildUrl("/requests", { device: undefined, q: "" })).toBe("/api/v1/requests");
  });
});
