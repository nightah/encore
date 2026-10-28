import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, delay, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";

import type { RequestDetail } from "@api/generated/model";
import { SETUP_STORAGE_KEY, getSetupProgress } from "@lib/setupProgress";
import { getDb, resetDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { FakeEventSource } from "@test/eventSource";
import { recordRequests, renderApp } from "@test/utils";

async function heading(name: string | RegExp) {
  return screen.findByRole("heading", { level: 1, name }, { timeout: 4000 });
}

describe("setup wizard", () => {
  beforeEach(() => {
    resetDb("fresh");
  });

  it("redirects to the wizard on first run and walks all six steps", async () => {
    const user = userEvent.setup();
    const requests = recordRequests();
    const { router } = renderApp("/");

    await heading("Create the admin account");
    expect(router.state.location.pathname).toBe("/setup");
    expect(screen.getByRole("button", { name: "Back" })).toBeDisabled();
    expect(screen.queryByRole("button", { name: "Skip for now" })).not.toBeInTheDocument();
    await user.type(screen.getByLabelText("Setup token"), "WRONG-TOKEN");
    await user.type(screen.getByLabelText("Password"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(await screen.findByText("That setup token doesn't match the one in the logs.")).toBeInTheDocument();

    await user.clear(screen.getByLabelText("Setup token"));
    await user.type(screen.getByLabelText("Setup token"), "K7QF-2M9X-PL4D");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await heading("Tell us your public address");
    expect(getDb().admin?.username).toBe("admin");
    expect(screen.getByLabelText("Public base URL")).toHaveValue("https://encore.example.net");
    await user.click(screen.getByRole("button", { name: "Check address" }));
    const checks = await screen.findByRole("list", { name: "Reachability check results" });
    expect(within(checks).getAllByRole("listitem")).toHaveLength(3);
    expect(within(checks).getByText(/answers with HTTP 400/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await heading("Add your first Kodi");
    expect(await screen.findByRole("radio", { name: /Lounge/ })).toBeChecked();
    await user.click(screen.getByRole("button", { name: "Add and test" }));
    // The checks show straight away, each running until the test answers.
    const results = await screen.findByRole("list", { name: "Connection test results" });
    expect(within(results).getAllByText("Checking:")).toHaveLength(4);
    // In the order the server runs them, so rows don't move when results arrive.
    expect(
      within(results)
        .getAllByRole("listitem")
        .map((li) => li.textContent?.replace("Checking: ", "")),
    ).toEqual(["JSON-RPC over HTTP", "WebSocket", "Credentials", "Library"]);
    expect(screen.getByRole("button", { name: "Continue" })).toBeDisabled();
    await waitFor(() => expect(within(results).queryByText("Checking:")).not.toBeInTheDocument());
    expect(getDb().instances.map((i) => i.name)).toEqual(["Lounge"]);

    // Add another without leaving the step: the form comes back without the one just added.
    await user.click(screen.getByRole("button", { name: "Add another" }));
    expect(await screen.findByText(/Already added: Lounge/)).toBeInTheDocument();
    expect(await screen.findByRole("radio", { name: /LibreELEC-Bedroom/ })).toBeChecked();
    expect(screen.queryByRole("radio", { name: /^Lounge/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Add and test" }));
    await screen.findByText(/LibreELEC-Bedroom was added/);
    await waitFor(() => expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled());
    expect(getDb().instances.map((i) => i.name)).toEqual(["Lounge", "LibreELEC-Bedroom"]);
    await user.click(screen.getByRole("button", { name: "Continue" }));

    // The refresh token fallback, as tests can't follow the Login with Amazon redirect.
    await heading("Connect your Amazon developer account");
    expect(await screen.findByText("https://encore.example.net/api/v1/oauth/lwa/callback")).toBeInTheDocument();
    // Over HTTPS the callback is the only return URL to register, and the refresh token the only fallback.
    expect(screen.queryByText("http://127.0.0.1:9090/cb")).not.toBeInTheDocument();
    await user.click(screen.getByText(/Can't sign in through the callback/));
    expect(screen.queryByRole("radio", { name: "Paste the redirect URL" })).not.toBeInTheDocument();
    await user.type(screen.getByLabelText("Refresh token"), "Atzr|IwEBIexample");
    await user.click(screen.getByRole("button", { name: "Connect" }));
    expect(await screen.findByText(/Connected to Amazon \(vendor/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await heading("Deploy the skill");
    expect(screen.getByRole("checkbox", { name: "en-AU" })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "en-GB" })).not.toBeChecked();
    // The invocation is encore or kodi; each privacy question explains itself.
    expect(screen.getByRole("radio", { name: "encore" })).toBeChecked();
    expect(screen.getByRole("radio", { name: "kodi" })).not.toBeChecked();
    expect(screen.getByText("“Alexa, ask encore to…”")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "About: The skill is export compliant" })).toBeInTheDocument();
    // Nothing is answered for the user, and deploying waits for every answer.
    expect(screen.getByRole("radio", { name: "The skill is export compliant: yes" })).not.toBeChecked();
    expect(screen.getByRole("radio", { name: "The skill is export compliant: no" })).not.toBeChecked();
    expect(screen.getByRole("button", { name: "Deploy skill" })).toBeDisabled();
    await user.click(screen.getByRole("radio", { name: "The skill collects personal information: no" }));
    await user.click(screen.getByRole("radio", { name: "The skill is directed at children under 13: no" }));
    await user.click(screen.getByRole("radio", { name: "The skill is export compliant: yes" }));
    await user.click(screen.getByRole("radio", { name: "The skill contains advertising: no" }));
    expect(screen.getByText("1 question left to answer before deploying.")).toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: "The skill allows purchases: no" }));
    await user.click(screen.getByRole("button", { name: "Deploy skill" }));
    expect(await screen.findByText("Deploying…", { selector: "span" })).toBeInTheDocument();
    expect(
      await screen.findByText(/Deployed. The skill is enabled for testing/, {}, { timeout: 8000 }),
    ).toBeInTheDocument();
    const settings = requests.filter((r) => r.method === "PUT" && r.path === "/skill/settings").at(-1);
    expect(settings?.body).toMatchObject({
      invocation: "encore",
      locales_enabled: ["en-AU"],
      privacy: {
        allows_purchases: false,
        contains_ads: false,
        is_child_directed: false,
        is_export_compliant: true,
        uses_personal_info: false,
      },
    });
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await heading("Say hello");
    expect(screen.getByText("Waiting for the first request…")).toBeInTheDocument();
    expect(await screen.findByText(/Request received from a new Echo/, {}, { timeout: 6000 })).toBeInTheDocument();
    await user.type(screen.getByLabelText("Echo name"), "Lounge Echo Show");
    await user.click(screen.getByRole("button", { name: "Finish" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(getDb().devices[0]?.name).toBe("Lounge Echo Show");
    expect(window.localStorage.getItem(SETUP_STORAGE_KEY)).toBeNull();

    // Every write carried the CSRF token from /auth/state (setup/admin is public, before the session).
    const writes = requests.filter((r) => r.method !== "GET" && r.path !== "/setup/admin");
    expect(writes.length).toBeGreaterThan(5);
    expect(writes.every((r) => r.csrf === "mock-csrf-token")).toBe(true);
  }, 30_000);

  it("waits for an Echo, not the simulator's request from the deploy's smoke test", async () => {
    resetDb("configured");
    window.localStorage.setItem(
      SETUP_STORAGE_KEY,
      JSON.stringify({ done: [1, 2, 3, 4, 5], skipped: [], step: 6, waitingSince: new Date(0).toISOString() }),
    );
    const [first] = getDb().requests;
    if (!first) throw new Error("the configured sample has requests");
    const simulated: RequestDetail = { ...first, id: 5001, simulated: true, time: new Date().toISOString() };
    // Only the simulator has talked to Encore so far.
    getDb().requests = [simulated];
    renderApp("/setup");

    await heading("Say hello");
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));
    FakeEventSource.emit("request", { ...simulated, id: 5002 });
    expect(screen.getByText("Waiting for the first request…")).toBeInTheDocument();
    expect(screen.queryByText(/Request received/)).not.toBeInTheDocument();
  });

  it("shows the network search until Kodi is found, and offers another search when none is", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2], skipped: [], step: 3 }));
    let answer: () => void = () => {};
    server.use(
      http.get(
        "*/api/v1/kodi/discovered",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json([]));
          }),
      ),
    );
    renderApp("/setup");

    await heading("Add your first Kodi");
    expect(await screen.findByRole("status")).toHaveTextContent("Searching for Kodi with mDNS");
    answer();
    expect(await screen.findByRole("button", { name: "Search again" })).toBeInTheDocument();
    expect(screen.queryByText(/Searching for Kodi/)).not.toBeInTheDocument();
    expect(screen.getByText(/No Kodi answered on your network/)).toBeInTheDocument();
  });

  it("skips steps and remembers progress", async () => {
    const user = userEvent.setup();
    resetDb("fresh");
    renderApp("/setup");
    await heading("Create the admin account");
    await user.type(screen.getByLabelText("Setup token"), "K7QF-2M9X-PL4D");
    await user.type(screen.getByLabelText("Password"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    await heading("Tell us your public address");
    await user.click(screen.getByRole("button", { name: "Skip for now" }));
    await heading("Add your first Kodi");
    expect(getSetupProgress()).toMatchObject({ done: [1], skipped: [2], step: 3 });

    const steps = screen.getByRole("list", { name: "Setup steps" });
    expect(within(steps).getByText("Skipped")).toBeInTheDocument();

    // Back returns to the skipped step.
    await user.click(screen.getByRole("button", { name: "Back" }));
    await heading("Tell us your public address");
  });

  it("stores the endpoint from the address check without a toast", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1], skipped: [], step: 2 }));
    const user = userEvent.setup();
    const requests = recordRequests();
    const { queryClient } = renderApp("/setup");
    await heading("Tell us your public address");
    await user.click(screen.getByRole("button", { name: "Check address" }));

    await waitFor(() => expect(requests.some((r) => r.method === "PUT" && r.path === "/skill/settings")).toBe(true));
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(screen.queryByText("Skill settings saved.")).not.toBeInTheDocument();
  });

  it("resumes where you left off and offers it from the shell", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2, 3], skipped: [], step: 4 }));
    const user = userEvent.setup();
    renderApp("/");
    expect(await screen.findByText(/Setup isn't finished yet. You're on step 4 of 6./)).toBeInTheDocument();
    await user.click(screen.getByRole("link", { name: /Continue setup/ }));
    await heading("Connect your Amazon developer account");
    // The configured sample is already connected.
    expect(await screen.findByText(/Connected to Amazon \(vendor M2X8K4P1QF\)/)).toBeInTheDocument();
  });

  it("shows why Login with Amazon failed when it comes back to the wizard", async () => {
    resetDb("configured");
    getDb().skill.amazon.connected = false;
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2, 3], skipped: [], step: 4 }));
    window.history.replaceState(null, "", "/setup?amazon_error=access_denied");
    renderApp("/setup?amazon_error=access_denied");

    await heading("Connect your Amazon developer account");
    expect(await screen.findByText("Amazon didn't connect: access_denied")).toBeInTheDocument();
    expect(window.location.search).toBe("");
  });

  it("asks for the local return URL and offers both paste fallbacks without HTTPS", async () => {
    const user = userEvent.setup();
    resetDb("configured");
    getDb().skill.amazon.connected = false;
    getDb().publicUrl = "http://encore.lan:8080";
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2, 3], skipped: [], step: 4 }));
    renderApp("/setup");

    await heading("Connect your Amazon developer account");
    expect(await screen.findByText("http://127.0.0.1:9090/cb")).toBeInTheDocument();
    expect(screen.queryByText(/encore\.lan:8080\/api\/v1\/oauth/)).not.toBeInTheDocument();
    await user.click(screen.getByText(/No HTTPS callback/));
    expect(screen.getByRole("radio", { name: "Paste the redirect URL" })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: "Paste a refresh token" })).toBeInTheDocument();
  });

  it("chooses what the skill can play before deploying", async () => {
    const user = userEvent.setup();
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2, 3, 4], skipped: [], step: 5 }));
    renderApp("/setup");

    await heading("Deploy the skill");
    const movies = await screen.findByRole("checkbox", { name: "Movies" });
    const music = screen.getByRole("checkbox", { name: /Music/ });
    expect(movies).toBeChecked();
    expect(screen.getByRole("checkbox", { name: "TV shows" })).toBeChecked();
    expect(music).toBeChecked();

    await user.click(movies);
    await user.click(screen.getByRole("checkbox", { name: "TV shows" }));
    expect(music).toBeChecked();
    expect(music).toHaveAttribute("aria-disabled", "true");
  });

  it("saves what the skill can play before the first Kodi is added", async () => {
    const user = userEvent.setup();
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2], skipped: [], step: 3 }));
    const requests = recordRequests();
    // The skill's settings arrive after the step renders, as they can on a busy machine, so the boxes are first shown unticked and disabled.
    server.use(
      http.get("*/skill", async () => {
        await delay(100);
      }),
    );
    renderApp("/setup");

    await heading("Add your first Kodi");
    const music = await screen.findByRole("checkbox", { checked: true, name: /Music/ });
    await waitFor(() => expect(music).not.toHaveAttribute("aria-disabled", "true"));
    await user.click(music);

    await waitFor(() => expect(getDb().skill.content).toEqual(["movies", "tv"]));
    expect(requests.filter((r) => r.method === "PUT" && r.path === "/skill/content")).toHaveLength(1);
    expect(requests.some((r) => r.path === "/skill/settings")).toBe(false);
    expect(music).not.toBeChecked();

    // The last one left can't be unticked.
    await user.click(screen.getByRole("checkbox", { name: "Movies" }));
    await waitFor(() => expect(getDb().skill.content).toEqual(["tv"]));
    expect(screen.getByRole("checkbox", { name: "TV shows" })).toHaveAttribute("aria-disabled", "true");
  });

  it("asks a signed-out admin to sign in before resuming", async () => {
    resetDb("configured");
    getDb().authenticated = false;
    const { router } = renderApp("/setup");
    await screen.findByRole("heading", { level: 1, name: "Sign in" });
    expect(router.state.location.pathname).toBe("/login");
    expect(router.state.location.search).toMatchObject({ redirect: "/setup" });
  });
});
