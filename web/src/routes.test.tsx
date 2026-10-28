import type { ComponentType } from "react";

import type { ErrorComponentProps } from "@tanstack/react-router";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiError } from "@api/client";
import { resetErrorReports } from "@lib/reportError";
import { clientErrors, getDb, resetDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { Route, isServerUnreachable } from "@routes/__root";
import { FakeEventSource } from "@test/eventSource";
import { failWith, recordRequests, renderApp } from "@test/utils";

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

describe("route gates", () => {
  it("lets a signed-out admin resume setup only after signing in", async () => {
    getDb().authenticated = false;
    const { router } = renderApp("/setup");
    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toEqual({ redirect: "/setup" });
  });

  it("opens the setup wizard on first run without signing in", async () => {
    resetDb("fresh");
    const { router } = renderApp("/setup");
    await screen.findByRole("heading", { level: 1, name: "Create the admin account" });
    expect(router.state.location.pathname).toBe("/setup");
  });

  it("keeps the query string when sending a signed-out visitor to sign in", async () => {
    getDb().authenticated = false;
    const { router } = renderApp("/request-log?outcome=error");
    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    expect(router.state.location.search).toEqual({ redirect: "/request-log?outcome=error" });
  });
});

describe("login page", () => {
  async function signIn(path: string, totp = "") {
    getDb().authenticated = false;
    const user = userEvent.setup();
    const result = renderApp(path);
    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    await user.type(screen.getByLabelText("Username"), "admin");
    await user.type(screen.getByLabelText("Password"), "correct-horse-battery");
    if (totp) await user.type(screen.getByLabelText("One-time code"), totp);
    await user.click(screen.getByRole("button", { name: "Sign in" }));
    return result;
  }

  it.each([
    ["an address on another site", "https://evil.example/steal"],
    ["a protocol-relative address", "//evil.example/steal"],
    ["a relative path", "kodi"],
  ])("goes to the overview instead of %s", async (_name, target) => {
    const { router } = await signIn(`/login?redirect=${encodeURIComponent(target)}`);
    await screen.findByRole("heading", { level: 1, name: "Overview" });
    expect(router.state.location.pathname).toBe("/");
  });

  it("checks the one-time code's format before calling the server", async () => {
    const log = recordRequests();
    await signIn("/login", "12ab");
    expect(await screen.findByText("Enter the 6-digit code from your authenticator app.")).toBeInTheDocument();
    expect(log.some((r) => r.path === "/auth/login")).toBe(false);
  });

  it("sends the one-time code only when one is typed", async () => {
    const log = recordRequests();
    await signIn("/login");
    await waitFor(() =>
      expect(log.find((r) => r.path === "/auth/login")?.body).toEqual({
        password: "correct-horse-battery",
        username: "admin",
      }),
    );
  });

  it("puts the server's field errors next to the fields, in its own words for the code", async () => {
    server.use(
      http.post("*/api/v1/auth/login", () =>
        HttpResponse.json(
          {
            detail: "validation failed",
            errors: [
              { location: "body.totp", message: "totp: wrong code" },
              { location: "body.username", message: "No such user." },
              { location: "body.remember", message: "Not a field on this form." },
            ],
            status: 422,
            title: "Unprocessable Entity",
          },
          { headers: { "Content-Type": "application/problem+json" }, status: 422 },
        ),
      ),
    );
    await signIn("/login", "123456");
    expect(await screen.findByText("No such user.")).toBeInTheDocument();
    expect(screen.getByText("Enter the 6-digit code from your authenticator app.")).toBeInTheDocument();
    expect(screen.queryByText("totp: wrong code")).not.toBeInTheDocument();
    expect(screen.queryByText("Not a field on this form.")).not.toBeInTheDocument();
    // Field errors replace the general alert.
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows why single sign-on failed", async () => {
    getDb().authenticated = false;
    renderApp(`/login?error=${encodeURIComponent("The identity provider refused the sign-in.")}`);
    expect(await screen.findByRole("alert")).toHaveTextContent("The identity provider refused the sign-in.");
  });

  it("explains header sign-in and offers no form", async () => {
    getDb().authenticated = false;
    getDb().authMode = "header";
    renderApp("/login");
    expect(await screen.findByText(/Encore trusts your reverse proxy to sign you in/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Password")).not.toBeInTheDocument();
  });
});

describe("settings page", () => {
  it("opens the tab named in the address and keeps the address in step", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/settings?tab=secrets");
    const tabs = await screen.findByRole("tablist", { name: "Settings sections" });
    expect(within(tabs).getByRole("tab", { name: "Secrets" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("button", { name: "Save changes" })).toBeDisabled();

    await user.click(within(tabs).getByRole("tab", { name: "Import and export" }));
    await waitFor(() => expect(router.state.location.search).toEqual({ tab: "backup" }));
  });

  it("opens General for a tab it doesn't know, as from an old bookmark", async () => {
    const { router } = renderApp("/settings?tab=bogus");
    const tabs = await screen.findByRole("tablist", { name: "Settings sections" });
    expect(within(tabs).getByRole("tab", { name: "General" })).toHaveAttribute("aria-selected", "true");
    expect(router.state.location.search).toEqual({});
  });

  it("forgets unsaved changes' state when switching tabs", async () => {
    const user = userEvent.setup();
    renderApp("/settings?tab=general");
    await user.selectOptions(await screen.findByRole("combobox", { name: "Log level" }), "debug");
    const save = screen.getByRole("button", { name: "Save changes" });
    await waitFor(() => expect(save).toBeEnabled());

    await user.click(screen.getByRole("tab", { name: "Features" }));
    await waitFor(() => expect(save).toBeDisabled());
  });
});

describe("overview page", () => {
  it("says what to do about an offline, an asleep and an idle Kodi instance", async () => {
    const db = getDb();
    const [first, second, third] = db.instances;
    if (!first || !second || !third) throw new Error("the sample has three instances");
    db.instances = [
      { ...first, name: "Lounge", now_playing: null, status: { ...first.status, state: "offline" } },
      {
        ...second,
        mac: "",
        name: "Bedroom",
        now_playing: null,
        status: { ...second.status, last_event_at: null, state: "asleep" },
      },
      { ...third, name: "Study", now_playing: null, status: { ...third.status, state: "connected" } },
    ];
    renderApp("/");

    const card = async (name: string) =>
      (await screen.findByRole("heading", { level: 3, name })).closest("div")?.parentElement as HTMLElement;
    const lounge = await card("Lounge");
    expect(lounge).toHaveTextContent("Not answering. Check that Kodi is running and reachable.");
    expect(within(lounge).getByRole("link", { name: "Test it on the Kodi page" })).toHaveAttribute("href", "/kodi");

    const bedroom = await card("Bedroom");
    expect(bedroom).toHaveTextContent("Asleep. Add its MAC address so voice requests can wake it.");

    expect(await card("Study")).toHaveTextContent("Nothing playing.");
  });

  it("opens a recent request from its time link", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/");
    const table = await screen.findByRole("table", { name: "Recent requests" });
    const [link] = within(table).getAllByRole("link", { name: /^Open the .* request in the log$/ });
    if (!link) throw new Error("no recent request");

    await user.click(link);

    await screen.findByRole("heading", { level: 1, name: "Request log" });
    expect(router.state.location.search).toEqual({ id: getDb().requests[0]?.id });
  });
});

describe("page load errors", () => {
  it.each([
    ["/", "/overview", "Overview"],
    ["/kodi", "/kodi/instances", "Kodi instances"],
    ["/echo-devices", "/devices", "Echo devices and rooms"],
    ["/settings", "/settings", "Settings"],
  ])("%s offers to retry when GET %s fails", async (path, endpoint, title) => {
    const user = userEvent.setup();
    failWith("get", endpoint, 500, "The database is locked.");
    renderApp(path);
    expect(await screen.findByRole("heading", { level: 1, name: title })).toBeInTheDocument();
    expect(await screen.findByText("The database is locked.", {}, { timeout: 4000 })).toBeInTheDocument();

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(screen.queryByText("The database is locked.")).not.toBeInTheDocument());
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });
});
