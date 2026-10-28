import type { ComponentType } from "react";

import type { ErrorComponentProps } from "@tanstack/react-router";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@api/client";
import { resetErrorReports } from "@lib/reportError";
import { clientErrors, getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { Route, isServerUnreachable } from "@routes/__root";
import { FakeEventSource } from "@test/eventSource";
import { renderApp } from "@test/utils";

const RootError = Route.options.errorComponent as ComponentType<ErrorComponentProps>;

describe("isServerUnreachable", () => {
  it.each([
    ["no connection", new ApiError("offline", 0), true],
    ["a server error", new ApiError("internal", 500), true],
    ["a bad gateway", new ApiError("bad gateway", 502), true],
    ["a client error", new ApiError("not found", 404), false],
    ["a redirect status", new ApiError("moved", 399), false],
    ["Chrome's failed fetch", new TypeError("Failed to fetch"), true],
    ["Firefox's network error", new TypeError("NetworkError when attempting to fetch resource."), true],
    ["a bug", new TypeError("chip is undefined"), false],
    ["a plain error mentioning the network", new Error("network down"), false],
    ["a string", "Failed to fetch", false],
  ])("treats %s correctly", (_name, error, expected) => {
    expect(isServerUnreachable(error)).toBe(expected);
  });
});

describe("root error screen", () => {
  let reload: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    clientErrors.length = 0;
    resetErrorReports();
    sessionStorage.clear();
    reload = vi.fn();
    vi.stubGlobal("location", { ...window.location, reload });
  });

  afterEach(() => vi.unstubAllGlobals());

  it("says the page broke, reports the error and reloads on Try again", async () => {
    const user = userEvent.setup();
    const reset = vi.fn();
    render(<RootError error={new TypeError("chip is undefined")} reset={reset} />);

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("Something went wrong on this page");
    expect(alert).toHaveTextContent("chip is undefined");
    await waitFor(() =>
      expect(clientErrors).toEqual([expect.objectContaining({ kind: "render", message: "chip is undefined" })]),
    );

    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(reset).toHaveBeenCalledOnce();
    expect(reload).toHaveBeenCalledOnce();
  });

  it("says Encore isn't responding and doesn't try to report to it", async () => {
    render(<RootError error={new ApiError("The Encore server could not be reached.", 0)} reset={vi.fn()} />);

    expect(screen.getByRole("heading", { name: "Encore isn't responding" })).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("The Encore server could not be reached.");
    // Give a report the chance to be sent, then check none was.
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(clientErrors).toHaveLength(0);
  });

  it("reloads once for a stale part of the app instead of reporting it", async () => {
    const error = new TypeError("Failed to fetch dynamically imported module: /static/kodi-abc.js");
    render(<RootError error={error} reset={vi.fn()} />);
    expect(reload).toHaveBeenCalledOnce();
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(clientErrors).toHaveLength(0);
  });

  it("shows the error, and reports it, when a stale part keeps failing after the reload", async () => {
    sessionStorage.setItem("encore.staleReload", String(Date.now()));
    const error = new TypeError("error loading dynamically imported module: /static/kodi-abc.js");
    render(<RootError error={error} reset={vi.fn()} />);
    expect(reload).not.toHaveBeenCalled();
    expect(screen.getByRole("heading", { name: "Something went wrong on this page" })).toBeInTheDocument();
    await waitFor(() => expect(clientErrors).toEqual([expect.objectContaining({ message: error.message })]));
  });
});

describe("root route", () => {
  it("shows the error screen when the server can't tell who is signed in", async () => {
    server.use(http.get("*/api/v1/auth/state", () => new HttpResponse(null, { status: 503 })));
    renderApp("/");
    expect(
      await screen.findByRole("heading", { name: "Encore isn't responding" }, { timeout: 4000 }),
    ).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("Request failed with HTTP 503.");
  });

  it("shows a not-found page with a way back", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/no-such-page");
    expect(await screen.findByRole("heading", { name: "Page not found" })).toBeInTheDocument();

    await user.click(screen.getByRole("link", { name: "Back to the overview" }));
    await screen.findByRole("heading", { level: 1, name: "Overview" });
    expect(router.state.location.pathname).toBe("/");
  });

  it("opens the event stream only once signed in", async () => {
    getDb().authenticated = false;
    renderApp("/login");
    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    expect(FakeEventSource.open).toBe(0);
  });

  it("keeps one event stream open for the signed-in app", async () => {
    renderApp("/");
    await screen.findByRole("heading", { level: 1, name: "Overview" });
    await waitFor(() => expect(FakeEventSource.open).toBe(1));
    expect(FakeEventSource.instances[0]?.url).toBe("/api/v1/events");
    expect(FakeEventSource.instances[0]?.withCredentials).toBe(true);
  });
});
