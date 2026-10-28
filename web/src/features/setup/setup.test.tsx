import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, delay, http } from "msw";
import { beforeEach, describe, expect, it } from "vitest";

import type { RequestDetail } from "@api/generated/model";
import { keys } from "@api/keys";
import { SETUP_STORAGE_KEY, getSetupProgress } from "@lib/setupProgress";
import { getDb, resetDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { FakeEventSource } from "@test/eventSource";
import { failWith, recordRequests, renderApp } from "@test/utils";

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
    expect(await screen.findByLabelText("Public base URL")).toHaveValue("https://encore.example.net");
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
    FakeEventSource.emit("request", { ...simulated, device_name: "Simulator", id: 5002 });
    FakeEventSource.emit("request", {
      ...simulated,
      device_name: "Kitchen Echo",
      id: 5003,
      simulated: false,
      time: new Date().toISOString(),
    });
    expect(await screen.findByText(/Request received from Kitchen Echo at/)).toBeInTheDocument();
    expect(screen.queryByText(/Simulator/)).not.toBeInTheDocument();
  });

  it("waits for the skill settings before saying what to ask Alexa", async () => {
    resetDb("configured");
    getDb().skill = { ...getDb().skill, invocation: "kodi", invocation_name: "kodi" };
    window.localStorage.setItem(
      SETUP_STORAGE_KEY,
      JSON.stringify({ done: [1, 2, 3, 4, 5], skipped: [], step: 6, waitingSince: new Date().toISOString() }),
    );
    let answer: (() => void) | undefined;
    server.use(
      http.get(
        "*/api/v1/skill",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().skill));
          }),
      ),
    );
    renderApp("/setup");

    await heading("Say hello");
    await waitFor(() => expect(answer).toBeDefined());
    expect(screen.queryByText(/ask encore/)).not.toBeInTheDocument();
    expect(screen.getByText("Loading…")).toBeInTheDocument();

    answer?.();
    expect(await screen.findByText("“Alexa, ask kodi what's playing.”")).toBeInTheDocument();
    expect(screen.getByText(/if “ask kodi” isn't recognised/)).toBeInTheDocument();
    expect(screen.queryByText(/ask encore/)).not.toBeInTheDocument();
    expect(screen.queryByText("Loading…")).not.toBeInTheDocument();
  });

  it("says the skill settings didn't load instead of guessing what to ask Alexa", async () => {
    resetDb("configured");
    getDb().skill = { ...getDb().skill, invocation: "kodi", invocation_name: "kodi" };
    window.localStorage.setItem(
      SETUP_STORAGE_KEY,
      JSON.stringify({ done: [1, 2, 3, 4, 5], skipped: [], step: 6, waitingSince: new Date().toISOString() }),
    );
    failWith("get", "/skill", 500, "boom");
    const user = userEvent.setup();
    const { queryClient } = renderApp("/setup");

    await heading("Say hello");
    await waitFor(() => expect(queryClient.getQueryState(keys.skill)?.status).toBe("error"));
    expect(screen.queryByText(/ask encore/)).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("boom");

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("“Alexa, ask kodi what's playing.”")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
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

  it("says the network search failed instead of that no Kodi answered", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2], skipped: [], step: 3 }));
    failWith("get", "/kodi/discovered", 503, "mDNS is unavailable");
    const { queryClient } = renderApp("/setup");

    await heading("Add your first Kodi");
    await waitFor(() => expect(queryClient.getQueryState(keys.discovered)?.status).toBe("error"));
    expect(screen.queryByText(/No Kodi answered on your network/)).not.toBeInTheDocument();
    expect(screen.getByText(/Encore couldn't search your network for Kodi/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Search again" })).toBeInTheDocument();
  });

  it("starts from the admin account on a fresh install, whatever this browser remembers", async () => {
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2, 3], skipped: [], step: 4 }));
    renderApp("/setup");

    await heading("Create the admin account");
    const steps = within(screen.getByRole("list", { name: "Setup steps" }));
    expect(steps.queryAllByText("(done)", { exact: false })).toHaveLength(0);
    expect(steps.queryAllByRole("button")).toHaveLength(0);
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
    await user.click(await screen.findByRole("button", { name: "Check address" }));

    await waitFor(() => expect(requests.some((r) => r.method === "PUT" && r.path === "/skill/settings")).toBe(true));
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(screen.queryByText("Skill settings saved.")).not.toBeInTheDocument();
  });

  it("waits for the skill settings before showing the address", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1], skipped: [], step: 2 }));
    let answer: () => void = () => {};
    server.use(
      http.get(
        "*/api/v1/skill",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().skill));
          }),
      ),
    );
    renderApp("/setup");

    await heading("Tell us your public address");
    expect(screen.queryByLabelText("Public base URL")).not.toBeInTheDocument();
    expect(await screen.findByRole("status")).toHaveTextContent("Loading…");
    expect(screen.queryByRole("button", { name: "Check address" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Skip for now" })).toBeEnabled();

    answer();
    expect(await screen.findByLabelText("Public base URL")).toHaveValue("https://encore.example.net");
    expect(screen.getByRole("button", { name: "Check address" })).toBeEnabled();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("says the skill settings didn't load instead of showing an empty address", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1], skipped: [], step: 2 }));
    failWith("get", "/skill", 500, "boom");
    const user = userEvent.setup();
    const { queryClient } = renderApp("/setup");

    await heading("Tell us your public address");
    await waitFor(() => expect(queryClient.getQueryState(keys.skill)?.status).toBe("error"));
    expect(screen.queryByLabelText("Public base URL")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("boom");
    expect(screen.getByRole("button", { name: "Skip for now" })).toBeEnabled();

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByLabelText("Public base URL")).toHaveValue("https://encore.example.net");
  });

  it("waits for the skill settings before offering to connect Amazon", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2, 3], skipped: [], step: 4 }));
    const requests = recordRequests();
    let answer: () => void = () => {};
    server.use(
      http.get(
        "*/api/v1/skill",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().skill));
          }),
      ),
    );
    renderApp("/setup");

    // The configured sample is already connected, so the sign-in form must never show.
    await heading("Connect your Amazon developer account");
    expect(screen.queryByLabelText("Client ID")).not.toBeInTheDocument();
    expect(await screen.findByRole("status")).toHaveTextContent("Loading…");
    expect(screen.getByRole("button", { name: "Skip for now" })).toBeEnabled();

    answer();
    expect(await screen.findByText(/Connected to Amazon \(vendor M2X8K4P1QF\)/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Client ID")).not.toBeInTheDocument();
    expect(requests.some((r) => r.path.startsWith("/skill/amazon/authorize"))).toBe(false);
  });

  it("says the skill settings didn't load instead of asking to connect Amazon", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2, 3], skipped: [], step: 4 }));
    failWith("get", "/skill", 500, "boom");
    const { queryClient } = renderApp("/setup");

    await heading("Connect your Amazon developer account");
    await waitFor(() => expect(queryClient.getQueryState(keys.skill)?.status).toBe("error"));
    expect(screen.queryByLabelText("Client ID")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("boom");
    expect(screen.getByRole("button", { name: "Skip for now" })).toBeEnabled();
  });

  it("shows the deploy step loading, and why it didn't load, with a way to move on", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2, 3, 4], skipped: [], step: 5 }));
    let answer: () => void = () => {};
    server.use(
      http.get(
        "*/api/v1/skill",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().skill));
          }),
      ),
    );
    const first = renderApp("/setup");

    await heading("Deploy the skill");
    expect(await screen.findByRole("status")).toHaveTextContent("Loading…");
    expect(screen.getByRole("button", { name: "Back" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Skip for now" })).toBeEnabled();
    answer();
    expect(await screen.findByRole("button", { name: "Deploy skill" })).toBeInTheDocument();
    first.unmount();

    failWith("get", "/skill", 500, "boom");
    renderApp("/setup");
    await heading("Deploy the skill");
    expect(await screen.findByRole("alert")).toHaveTextContent("boom");
    expect(screen.getByRole("button", { name: "Skip for now" })).toBeEnabled();
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
    // The skill's settings arrive after the step renders, as they can on a busy machine, so the boxes show only once they're in.
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

  it("waits for the skill settings before showing what the skill can play on the Kodi step", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2], skipped: [], step: 3 }));
    let answer: (() => void) | undefined;
    server.use(
      http.get(
        "*/api/v1/skill",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().skill));
          }),
      ),
    );
    renderApp("/setup");

    await heading("Add your first Kodi");
    await waitFor(() => expect(answer).toBeDefined());
    const content = screen.getByRole("group", { name: "What it can play" });
    expect(within(content).queryByRole("checkbox", { checked: false })).not.toBeInTheDocument();
    expect(within(content).getByRole("status")).toHaveTextContent("Loading…");

    answer?.();
    expect(await within(content).findByRole("checkbox", { name: "Movies" })).toBeChecked();
    expect(within(content).getByRole("checkbox", { name: "TV shows" })).toBeChecked();
    expect(within(content).getByRole("checkbox", { name: /Music/ })).toBeChecked();
    expect(within(content).queryByRole("status")).not.toBeInTheDocument();
  });

  it("says the skill settings didn't load instead of showing nothing the skill can play", async () => {
    resetDb("configured");
    window.localStorage.setItem(SETUP_STORAGE_KEY, JSON.stringify({ done: [1, 2], skipped: [], step: 3 }));
    failWith("get", "/skill", 500, "boom");
    const user = userEvent.setup();
    const { queryClient } = renderApp("/setup");

    await heading("Add your first Kodi");
    await waitFor(() => expect(queryClient.getQueryState(keys.skill)?.status).toBe("error"));
    const content = screen.getByRole("group", { name: "What it can play" });
    expect(within(content).queryByRole("checkbox")).not.toBeInTheDocument();
    expect(within(content).getByRole("alert")).toHaveTextContent("boom");

    server.resetHandlers();
    await user.click(within(content).getByRole("button", { name: "Try again" }));
    expect(await within(content).findByRole("checkbox", { name: "Movies" })).toBeChecked();
    expect(within(content).queryByRole("alert")).not.toBeInTheDocument();
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

/** Opens the wizard on `step` of a configured install, with the steps before it done. */
function startAt(step: number, progress: Partial<{ done: number[]; skipped: number[]; reached: number }> = {}) {
  resetDb("configured");
  window.localStorage.setItem(
    SETUP_STORAGE_KEY,
    JSON.stringify({
      done: progress.done ?? Array.from({ length: step - 1 }, (_, i) => i + 1),
      reached: progress.reached,
      skipped: progress.skipped ?? [],
      step,
    }),
  );
}

function problem422(errors: Array<{ location: string; message: string }>, detail = "Check the highlighted fields.") {
  return HttpResponse.json(
    { detail, errors, status: 422, title: "Unprocessable Entity", type: "about:blank" },
    { headers: { "Content-Type": "application/problem+json" }, status: 422 },
  );
}

describe("setup wizard steps list", () => {
  it("lets you jump back to a step you've reached, but not ahead or to the admin account", async () => {
    startAt(4);
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    const steps = within(screen.getByRole("list", { name: "Setup steps" }));
    expect(steps.queryByRole("button", { name: /Admin account/ })).not.toBeInTheDocument();
    expect(steps.queryByRole("button", { name: /Amazon account/ })).not.toBeInTheDocument();
    expect(steps.queryByRole("button", { name: /Deploy skill/ })).not.toBeInTheDocument();
    expect(steps.getByText("Amazon account").closest("li")).toHaveAttribute("aria-current", "step");
    expect(steps.getAllByText("(done)", { exact: false })).toHaveLength(3);

    await user.click(steps.getByRole("button", { name: /Public address/ }));
    await heading("Tell us your public address");
    expect(getSetupProgress()).toMatchObject({ step: 2 });
    expect(screen.getByRole("button", { name: "Back" })).toBeDisabled();

    expect(steps.queryByRole("button", { name: /Deploy skill/ })).not.toBeInTheDocument();
    await user.click(steps.getByRole("button", { name: /Kodi/ }));
    await heading("Add your first Kodi");
  });

  it("keeps the furthest step reached when you go back, so you can jump forward to it again", async () => {
    startAt(4);
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");
    const steps = within(screen.getByRole("list", { name: "Setup steps" }));

    await user.click(steps.getByRole("button", { name: /Public address/ }));
    await heading("Tell us your public address");
    expect(getSetupProgress()).toMatchObject({ reached: 4, step: 2 });
    expect(steps.queryByRole("button", { name: /Deploy skill/ })).not.toBeInTheDocument();
    await user.click(steps.getByRole("button", { name: /Amazon account/ }));
    await heading("Connect your Amazon developer account");

    await user.click(screen.getByRole("button", { name: "Back" }));
    await heading("Add your first Kodi");
    await user.click(steps.getByRole("button", { name: /Amazon account/ }));
    await heading("Connect your Amazon developer account");
  });

  it("remembers the furthest step reached when it resumes", async () => {
    startAt(2, { done: [1, 2, 3], reached: 4 });
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Tell us your public address");

    const steps = within(screen.getByRole("list", { name: "Setup steps" }));
    expect(steps.queryByRole("button", { name: /Deploy skill/ })).not.toBeInTheDocument();
    await user.click(steps.getByRole("button", { name: /Amazon account/ }));
    await heading("Connect your Amazon developer account");
  });

  it("clears a skipped step once it's finished", async () => {
    startAt(3, { done: [1], skipped: [2] });
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Add your first Kodi");

    await user.click(screen.getByRole("button", { name: "Back" }));
    await heading("Tell us your public address");
    await user.click(screen.getByRole("button", { name: "Check address" }));
    await user.click(await screen.findByRole("button", { name: "Continue" }));

    await heading("Add your first Kodi");
    expect(getSetupProgress()).toMatchObject({ done: [1, 2], skipped: [], step: 3 });
    expect(within(screen.getByRole("list", { name: "Setup steps" })).queryByText("Skipped")).not.toBeInTheDocument();
  });
});

describe("setup admin step", () => {
  it("checks the fields before creating the account", async () => {
    resetDb("fresh");
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Create the admin account");

    await user.clear(screen.getByLabelText("Username"));
    await user.type(screen.getByLabelText("Password"), "too-short");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(await screen.findByText("Paste the setup token from Encore's log.")).toBeInTheDocument();
    expect(screen.getByText("Choose a username.")).toBeInTheDocument();
    expect(screen.getByText("Use at least 12 characters.")).toBeInTheDocument();
    expect(log.some((r) => r.path === "/setup/admin")).toBe(false);
  });

  it("shows a refusal that isn't about one field", async () => {
    resetDb("fresh");
    failWith("post", "/setup/admin", 409, "An admin account already exists. Sign in instead.");
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Create the admin account");

    await user.type(screen.getByLabelText("Setup token"), "K7QF-2M9X-PL4D");
    await user.type(screen.getByLabelText("Password"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("An admin account already exists. Sign in instead.");
    expect(screen.getByRole("heading", { level: 1, name: "Create the admin account" })).toBeInTheDocument();
  });

  it("puts a rejected setup token under its field, without a separate alert", async () => {
    resetDb("fresh");
    server.use(
      http.post("*/api/v1/setup/admin", () =>
        problem422([{ location: "body.setup_token", message: "That isn't this server's setup token." }]),
      ),
    );
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Create the admin account");

    await user.type(screen.getByLabelText("Setup token"), "WRONG-TOKEN");
    await user.type(screen.getByLabelText("Password"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(await screen.findByLabelText("Setup token")).toHaveAccessibleDescription(
      "That isn't this server's setup token.",
    );
    expect(screen.getByLabelText("Setup token")).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 1, name: "Create the admin account" })).toBeInTheDocument();
  });

  it("says when Encore couldn't be reached to create the account", async () => {
    resetDb("fresh");
    server.use(http.post("*/api/v1/setup/admin", () => HttpResponse.error()));
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Create the admin account");

    await user.type(screen.getByLabelText("Setup token"), "K7QF-2M9X-PL4D");
    await user.type(screen.getByLabelText("Password"), "correct-horse-battery");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("The Encore server could not be reached.");
    expect(screen.getByRole("heading", { level: 1, name: "Create the admin account" })).toBeInTheDocument();
  });

  it("says the account exists when you return to it after creating it", async () => {
    startAt(1, { done: [1] });
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Create the admin account");

    expect(await screen.findByText("The admin account exists, and you're signed in as admin.")).toBeInTheDocument();
    expect(screen.queryByLabelText("Setup token")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await heading("Tell us your public address");
  });
});

describe("setup address step", () => {
  it("trims the address and stores the skill endpoint under it", async () => {
    startAt(2);
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Tell us your public address");

    const field = screen.getByLabelText("Public base URL");
    // A url input has no text selection in jsdom, so user-event can't clear it; set the value as a paste would.
    fireEvent.change(field, { target: { value: " https://kodi.example.org/ " } });
    await user.click(screen.getByRole("button", { name: "Check address" }));

    await waitFor(() =>
      expect(log.find((r) => r.method === "PUT" && r.path === "/skill/settings")?.body).toMatchObject({
        endpoint: { url: "https://kodi.example.org/api/v1/alexa" },
      }),
    );
    expect(log.find((r) => r.path === "/setup/reachability")?.body).toEqual({ url: "https://kodi.example.org" });
    expect(screen.getByText("Every admin route needs a sign-in.")).toBeInTheDocument();
  });

  it("puts a rejected address under the field and lets you fix it", async () => {
    startAt(2);
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Tell us your public address");

    const field = screen.getByLabelText("Public base URL");
    await user.clear(field);
    fireEvent.change(field, { target: { value: "http://encore.lan" } });
    await user.click(screen.getByRole("button", { name: "Check address" }));

    expect(await screen.findByText("Alexa only calls HTTPS endpoints on port 443.")).toBeInTheDocument();
    expect(field).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("button", { name: "Continue" })).not.toBeInTheDocument();

    await user.type(field, "s");
    expect(screen.queryByText("Alexa only calls HTTPS endpoints on port 443.")).not.toBeInTheDocument();
  });

  it("shows a failed check under the field", async () => {
    startAt(2);
    failWith("post", "/setup/reachability", 502, "The proxy didn't answer.");
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Tell us your public address");

    await user.click(screen.getByRole("button", { name: "Check address" }));

    expect(await screen.findByText("The proxy didn't answer.")).toBeInTheDocument();
    expect(log.some((r) => r.path === "/skill/settings")).toBe(false);
  });

  it("offers to check again when a check fails", async () => {
    startAt(2);
    server.use(
      http.post("*/api/v1/setup/reachability", () =>
        HttpResponse.json({
          checks: [{ detail: "Certificate expired", level: "fail", name: "certificate", note: "" }],
          url: "https://encore.example.net",
        }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Tell us your public address");

    await user.click(screen.getByRole("button", { name: "Check address" }));

    const results = await screen.findByRole("list", { name: "Reachability check results" });
    expect(within(results).getByRole("listitem")).toHaveTextContent("Failed: Certificate expired");
    expect(within(results).getByRole("listitem").querySelector("[aria-hidden='true']")).toHaveClass("bg-bad");
    expect(screen.getByRole("button", { name: "Check again" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Continue" })).not.toBeInTheDocument();
  });

  it("lets you continue past a warning, marking it as one", async () => {
    startAt(2);
    server.use(
      http.post("*/api/v1/setup/reachability", () =>
        HttpResponse.json({
          checks: [
            { detail: "Alexa endpoint answers", level: "ok", name: "alexa_endpoint", note: "" },
            { detail: "Admin API reachable from outside", level: "warn", name: "admin_exposed", note: "" },
          ],
          url: "https://encore.example.net",
        }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Tell us your public address");

    await user.click(screen.getByRole("button", { name: "Check address" }));

    const results = await screen.findByRole("list", { name: "Reachability check results" });
    expect(
      within(results)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["Passed: Alexa endpoint answers", "Warning: Admin API reachable from outside"]);
    const dots = within(results)
      .getAllByRole("listitem")
      .map((item) => item.querySelector("[aria-hidden='true']"));
    expect(dots[0]).toHaveClass("bg-dot-ok");
    expect(dots[1]).toHaveClass("bg-dot-warn");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await heading("Add your first Kodi");
  });

  it("checks an address typed before the skill has loaded, saving no endpoint", async () => {
    startAt(2);
    server.use(http.get("*/api/v1/skill", () => delay("infinite")));
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Tell us your public address");

    await user.type(screen.getByLabelText("Public base URL"), "https://kodi.example.org");
    await user.click(screen.getByRole("button", { name: "Check address" }));

    expect(await screen.findByRole("list", { name: "Reachability check results" })).toBeInTheDocument();
    expect(log.some((r) => r.path === "/setup/reachability")).toBe(true);
    expect(log.some((r) => r.path === "/skill/settings")).toBe(false);
  });

  it.each([404, 405, 501])("lets you continue when this server can't check addresses (%i)", async (status) => {
    startAt(2);
    failWith("post", "/setup/reachability", status, "Not Found");
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Tell us your public address");

    await user.click(screen.getByRole("button", { name: "Check address" }));

    expect(await screen.findByText(/This server can't check the address from outside yet/)).toBeInTheDocument();
    expect(screen.queryByText("Not Found")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Continue" }));
    await heading("Add your first Kodi");
  });

  it("starts empty when the stored endpoint isn't an address", async () => {
    startAt(2);
    getDb().skill.endpoint.url = "not a url";
    renderApp("/setup");
    await heading("Tell us your public address");

    await waitFor(() => expect(screen.getByLabelText("Public base URL")).toHaveValue(""));
    expect(screen.getByRole("button", { name: "Check address" })).toBeDisabled();
  });
});

describe("setup Kodi step", () => {
  it("adds a Kodi by address, assuming its WebSocket on port 9090", async () => {
    startAt(3);
    getDb().discovered = [];
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Add your first Kodi");

    expect(await screen.findByRole("radio", { name: /Enter an address manually/ })).toBeChecked();
    expect(await screen.findByText(/Already added: Lounge, Bedroom, Kids' room/)).toBeInTheDocument();
    await user.type(screen.getByLabelText("Name"), " Garage ");
    await user.type(screen.getByLabelText("JSON-RPC address"), " http://192.168.1.60:8080/jsonrpc ");
    await user.clear(screen.getByLabelText("Kodi username"));
    await user.click(screen.getByRole("button", { name: "Add and test" }));

    expect(await screen.findByText(/Garage was added/)).toBeInTheDocument();
    expect(log.find((r) => r.method === "POST" && r.path === "/kodi/instances")?.body).toEqual({
      name: "Garage",
      url: "http://192.168.1.60:8080/jsonrpc",
      websocket_url: "ws://192.168.1.60:9090/jsonrpc",
    });
  });

  it("sends no WebSocket address for an address that can't be parsed", async () => {
    startAt(3);
    getDb().discovered = [];
    server.use(http.post("*/api/v1/kodi/instances", () => problem422([{ location: "body.url", message: "Bad URL." }])));
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Add your first Kodi");

    await user.type(await screen.findByLabelText("Name"), "Garage");
    await user.type(screen.getByLabelText("JSON-RPC address"), "garage:8080");
    await user.type(screen.getByLabelText("Kodi password"), "pw");
    await user.click(screen.getByRole("button", { name: "Add and test" }));

    expect(await screen.findByText("Bad URL.")).toBeInTheDocument();
    expect(log.find((r) => r.method === "POST")?.body).toEqual({
      name: "Garage",
      password: "pw",
      url: "garage:8080",
      username: "kodi",
    });
    // Back on the form with what was typed.
    expect(screen.getByLabelText("Name")).toHaveValue("Garage");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows a failure to add that isn't about one field", async () => {
    startAt(3);
    failWith("post", "/kodi/instances", 500, "The database is locked.");
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Add your first Kodi");

    await user.click(await screen.findByRole("button", { name: "Add and test" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("The database is locked.");
  });

  it("describes each discovered Kodi and adds one without a WebSocket port as HTTP only", async () => {
    startAt(3);
    getDb().discovered = [{ host: "192.168.1.60", http_port: 8080, name: "Garage", version: "", ws_port: 0 }];
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Add your first Kodi");

    const garage = await screen.findByRole("radio", { name: /Garage/ });
    expect(garage).toHaveAccessibleName("Garage192.168.1.60");
    expect(screen.getByText("We found these on your network.", { exact: false })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Add and test" }));

    // Without a WebSocket the test reports it, and points to the Kodi page.
    expect(await screen.findByText(/You can fix this later on the Kodi page/)).toBeInTheDocument();
    expect(log.find((r) => r.method === "POST" && r.path === "/kodi/instances")?.body).toEqual({
      name: "Garage",
      url: "http://192.168.1.60:8080/jsonrpc",
      username: "kodi",
    });
  });

  it("says when the connection test couldn't run, and lets you continue", async () => {
    startAt(3);
    server.use(
      http.post("*/api/v1/kodi/instances/:id/test", () =>
        HttpResponse.json(
          { detail: "Kodi closed the connection.", status: 502, title: "Bad Gateway" },
          { headers: { "Content-Type": "application/problem+json" }, status: 502 },
        ),
      ),
    );
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Add your first Kodi");

    await user.click(await screen.findByRole("button", { name: "Add and test" }));

    expect(await screen.findByText("The connection test didn't run: Kodi closed the connection.")).toHaveAttribute(
      "role",
      "alert",
    );
    const results = screen.getByRole("list", { name: "Connection test results" });
    expect(within(results).getAllByText("Failed:", { exact: false })).toHaveLength(4);
    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
  });

  it("shows why what the skill plays couldn't be saved", async () => {
    startAt(3);
    failWith("put", "/skill/content", 500, "Couldn't save.");
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Add your first Kodi");

    const music = await screen.findByRole("checkbox", { name: /Music/ });
    await waitFor(() => expect(music).not.toHaveAttribute("aria-disabled", "true"));
    await user.click(music);

    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't save.");
  });

  it("searches the network again on request", async () => {
    startAt(3);
    getDb().discovered = [];
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Add your first Kodi");

    const again = await screen.findByRole("button", { name: "Search again" });
    getDb().discovered = [{ host: "192.168.1.60", http_port: 8080, name: "Garage", version: "21.3", ws_port: 9090 }];
    await user.click(again);

    expect(await screen.findByRole("radio", { name: /Garage/ })).toBeChecked();
    expect(screen.getByText("192.168.1.60 · Kodi 21.3 · WebSocket :9090")).toBeInTheDocument();
    expect(log.filter((r) => r.path === "/kodi/discovered").length).toBeGreaterThanOrEqual(2);
  });
});

describe("setup Amazon step", () => {
  it("says it's connected even when the vendor isn't known", async () => {
    startAt(4);
    getDb().skill.amazon.vendor_id = "";
    renderApp("/setup");
    await heading("Connect your Amazon developer account");
    expect(await screen.findByText("Connected to Amazon. Encore can deploy the skill now.")).toBeInTheDocument();
  });

  it("asks to authorise again when Amazon wants a new sign-in, keeping the client ID", async () => {
    startAt(4);
    getDb().skill.amazon.needs_reauthorise = true;
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    expect(await screen.findByText(/Amazon asked Encore to sign in again/)).toBeInTheDocument();
    expect(screen.getByLabelText("Client ID")).toHaveValue("amzn1.application-oa2-client.4f1c8d2b7a6e4e92a");
    const authorise = screen.getByRole("button", { name: "Authorise with Amazon" });
    expect(authorise).toBeDisabled();
    await user.type(screen.getByLabelText("Client secret"), "secret");
    expect(authorise).toBeEnabled();
  });

  it("saves the credentials and asks Amazon where to sign in", async () => {
    startAt(4);
    getDb().skill.amazon.connected = false;
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    await user.type(await screen.findByLabelText("Client secret"), "lwa-secret");
    await user.click(screen.getByRole("button", { name: "Authorise with Amazon" }));

    await waitFor(() => expect(log.some((r) => r.path === "/skill/amazon/authorize?then=setup")).toBe(true));
    expect(log.find((r) => r.path === "/skill/amazon/credentials")?.body).toEqual({
      client_id: "amzn1.application-oa2-client.4f1c8d2b7a6e4e92a",
      client_secret: "lwa-secret",
    });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("puts a rejected client ID under its field", async () => {
    startAt(4);
    getDb().skill.amazon.connected = false;
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    const id = await screen.findByLabelText("Client ID");
    await user.clear(id);
    await user.type(id, "not-a-client-id");
    await user.type(screen.getByLabelText("Client secret"), "lwa-secret");
    await user.click(screen.getByRole("button", { name: "Authorise with Amazon" }));

    expect(
      await screen.findByText("A Login with Amazon client ID starts with amzn1.application-oa2-client."),
    ).toBeInTheDocument();
    expect(id).toHaveAttribute("aria-invalid", "true");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows why the credentials couldn't be saved and stays on the step", async () => {
    startAt(4);
    getDb().skill.amazon.connected = false;
    failWith("put", "/skill/amazon/credentials", 500, "Couldn't store the secret.");
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    await user.type(await screen.findByLabelText("Client secret"), "lwa-secret");
    await user.click(screen.getByRole("button", { name: "Authorise with Amazon" }));

    expect(await screen.findByRole("alert")).toHaveTextContent("Couldn't store the secret.");
    expect(log.some((r) => r.path.startsWith("/skill/amazon/authorize?"))).toBe(false);
    expect(screen.getByRole("button", { name: "Authorise with Amazon" })).toBeEnabled();
  });

  it("explains when Amazon gives no sign-in address", async () => {
    startAt(4);
    getDb().skill.amazon.connected = false;
    server.use(
      http.get("*/api/v1/skill/amazon/authorize", () =>
        HttpResponse.json({ return_urls: ["https://encore.example.net/api/v1/oauth/lwa/callback"], url: "" }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    await user.type(await screen.findByLabelText("Client secret"), "lwa-secret");
    await user.click(screen.getByRole("button", { name: "Authorise with Amazon" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Save the Login with Amazon client ID and secret first.",
    );
    expect(screen.getByRole("button", { name: "Authorise with Amazon" })).toBeEnabled();
  });

  it("works out the return URL from the skill endpoint when Amazon's list isn't available", async () => {
    startAt(4);
    getDb().skill.amazon.connected = false;
    getDb().skill.endpoint.url = "https://home.example.org/api/v1/alexa";
    failWith("get", "/skill/amazon/authorize", 500, "Amazon is unavailable.");
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    expect(await screen.findByText("https://home.example.org/api/v1/oauth/lwa/callback")).toBeInTheDocument();
    expect(screen.getByText("Under Web Settings, add this Allowed Return URL:")).toBeInTheDocument();
  });

  it("lists no return URL when neither Amazon nor the skill endpoint gives one", async () => {
    startAt(4);
    getDb().skill.amazon.connected = false;
    getDb().skill.endpoint.url = "not a url";
    failWith("get", "/skill/amazon/authorize", 500, "Amazon is unavailable.");
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    expect(await screen.findByText("Under Web Settings, add this Allowed Return URL:")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copy return URL" })).not.toBeInTheDocument();
  });

  it("connects with a pasted refresh token without HTTPS", async () => {
    startAt(4);
    const db = getDb();
    db.skill.amazon.connected = false;
    db.publicUrl = "http://encore.lan:8080";
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    await user.click(await screen.findByText(/No HTTPS callback/));
    await user.click(screen.getByRole("radio", { name: "Paste a refresh token" }));
    await user.type(screen.getByLabelText("Refresh token"), " Atzr|token ");
    await user.click(screen.getByRole("button", { name: "Connect" }));

    expect(await screen.findByText(/Connected to Amazon \(vendor/)).toBeInTheDocument();
    expect(log.find((r) => r.path === "/skill/amazon/paste")?.body).toEqual({ refresh_token: "Atzr|token" });
  });

  it("pastes a redirect URL with Enter, and starts afresh when switching to a refresh token", async () => {
    startAt(4);
    const db = getDb();
    db.skill.amazon.connected = false;
    db.publicUrl = "http://encore.lan:8080";
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    await user.click(await screen.findByText(/No HTTPS callback/));
    expect(screen.getByRole("radio", { name: "Paste the redirect URL" })).toHaveAttribute("aria-checked", "true");
    const redirect = screen.getByLabelText("Redirect URL");
    await user.type(redirect, "http://127.0.0.1:9090/cb?state=x{Enter}");

    expect(await screen.findByText(/That URL has no code parameter/)).toBeInTheDocument();
    expect(log.find((r) => r.path === "/skill/amazon/paste")?.body).toEqual({
      redirect_url: "http://127.0.0.1:9090/cb?state=x",
    });
    // Enter connected with the pasted URL rather than submitting the credentials above it.
    expect(log.some((r) => r.path === "/skill/amazon/credentials")).toBe(false);

    await user.click(screen.getByRole("radio", { name: "Paste a refresh token" }));
    const token = screen.getByLabelText("Refresh token");
    expect(token).toHaveValue("");
    expect(screen.queryByText(/That URL has no code parameter/)).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Connect" })).toBeDisabled();

    await user.type(token, "   {Enter}");
    expect(log.filter((r) => r.path === "/skill/amazon/paste")).toHaveLength(1);
  });

  it("offers the refresh token over HTTPS when the step opens before the skill has loaded", async () => {
    // Opening the step straight away, as after Login with Amazon sends you back, renders it before the skill and its return URLs arrive.
    startAt(4);
    getDb().skill.amazon.connected = false;
    failWith("post", "/skill/amazon/paste", 502, "Amazon didn't answer.");
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Connect your Amazon developer account");

    await user.click(await screen.findByText(/Can't sign in through the callback/));
    expect(screen.queryByLabelText("Redirect URL")).not.toBeInTheDocument();
    await user.type(screen.getByLabelText("Refresh token"), "Atzr|token");
    await user.click(screen.getByRole("button", { name: "Connect" }));

    expect(await screen.findByText("Amazon didn't answer.")).toBeInTheDocument();
    expect(log.find((r) => r.path === "/skill/amazon/paste")?.body).toEqual({ refresh_token: "Atzr|token" });
  });
});

describe("setup deploy step", () => {
  it("sends you to the Amazon step first when Amazon isn't connected", async () => {
    startAt(5);
    getDb().skill.amazon.connected = false;
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Deploy the skill");

    expect(await screen.findByText(/Connect your Amazon developer account first/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Go to Amazon account" }));
    await heading("Connect your Amazon developer account");
  });

  it("keeps the locales in the offered order and needs at least one", async () => {
    startAt(5);
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Deploy the skill");

    await user.click(await screen.findByRole("checkbox", { name: "de-DE" }));
    await user.click(screen.getByRole("checkbox", { name: "en-GB" }));
    await user.click(screen.getByRole("checkbox", { name: "en-AU" }));
    await user.click(screen.getByRole("checkbox", { name: "en-AU" }));

    await user.click(screen.getByRole("button", { name: "Deploy skill" }));
    await waitFor(() =>
      expect(log.find((r) => r.method === "PUT" && r.path === "/skill/settings")?.body).toMatchObject({
        locales_enabled: ["en-AU", "en-GB", "de-DE"],
      }),
    );
  });

  it("can't deploy without a locale", async () => {
    startAt(5);
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Deploy the skill");

    await user.click(await screen.findByRole("checkbox", { name: "en-AU" }));
    expect(screen.getByRole("button", { name: "Deploy skill" })).toBeDisabled();
  });

  it("explains that French uses “kodi” while encore is chosen", async () => {
    startAt(5);
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Deploy the skill");

    await user.click(await screen.findByRole("checkbox", { name: "fr-FR" }));
    expect(screen.getByText(/French uses “kodi”/)).toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: "kodi" }));
    expect(screen.getByText("“Alexa, ask kodi to…”")).toBeInTheDocument();
    expect(screen.queryByText(/French uses “kodi”/)).not.toBeInTheDocument();
  });

  it.each([
    ["locales", "body.locales_enabled", "choose at least one locale"],
    ["content", "body.content", "choose at least one of movies, tv or music"],
  ])("puts a rejected %s choice under it", async (_name, location, message) => {
    startAt(5);
    server.use(http.put("*/api/v1/skill/settings", () => problem422([{ location, message }], "validation failed")));
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Deploy the skill");

    await user.click(await screen.findByRole("button", { name: "Deploy skill" }));

    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Step 5: Deploy skill" })).queryByRole("alert")).toBeNull();
  });

  it("puts a rejected invocation under it", async () => {
    startAt(5);
    server.use(
      http.put("*/api/v1/skill/settings", () =>
        problem422([{ location: "body.invocation", message: "must be encore or kodi" }], "validation failed"),
      ),
    );
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/setup");
    await heading("Deploy the skill");

    await user.click(await screen.findByRole("button", { name: "Deploy skill" }));

    expect(await screen.findByText("must be encore or kodi")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(log.some((r) => r.path === "/skill/deploy")).toBe(false);
  });

  it("shows a rejected setting the step has no field for", async () => {
    // As the server answers when the address step was skipped and the stored endpoint isn't https.
    startAt(5);
    server.use(
      http.put("*/api/v1/skill/settings", () =>
        problem422(
          [{ location: "body.endpoint.url", message: "must be an https:// URL; Alexa only calls HTTPS endpoints" }],
          "validation failed: endpoint.url must be an https:// URL",
        ),
      ),
    );
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Deploy the skill");

    await user.click(await screen.findByRole("button", { name: "Deploy skill" }));

    const step = screen.getByRole("region", { name: "Step 5: Deploy skill" });
    expect(await within(step).findByRole("alert")).toHaveTextContent(
      "validation failed: endpoint.url must be an https:// URL",
    );
  });

  it("shows why the deploy couldn't start", async () => {
    startAt(5);
    failWith("post", "/skill/deploy", 409, "A deploy is already running.");
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Deploy the skill");

    await user.click(await screen.findByRole("button", { name: "Deploy skill" }));

    const step = screen.getByRole("region", { name: "Step 5: Deploy skill" });
    expect(await within(step).findByRole("alert")).toHaveTextContent("A deploy is already running.");
    await waitFor(() => expect(screen.getByRole("button", { name: "Deploy skill" })).toBeEnabled());
  });

  it.each([
    ["Amazon rejected the package", "The deploy failed at “upload”: Amazon rejected the package."],
    ["", "The deploy failed at “upload”: no details."],
  ])("says at which step a deploy failed (%j) and offers another try", async (message, shown) => {
    startAt(5);
    server.use(
      http.post("*/api/v1/skill/deploy", () => HttpResponse.json({ id: "d-9" }, { status: 202 })),
      http.get("*/api/v1/skill/deploy/d-9", () =>
        HttpResponse.json({
          error: message,
          events: [
            {
              at: new Date().toISOString(),
              locales: {},
              message: "Built the skill package",
              state: "succeeded",
              step: "package",
            },
            { at: new Date().toISOString(), locales: {}, message, state: "failed", step: "upload" },
          ],
          id: "d-9",
          state: "failed",
        }),
      ),
    );
    const user = userEvent.setup();
    renderApp("/setup");
    await heading("Deploy the skill");

    await user.click(await screen.findByRole("button", { name: "Deploy skill" }));

    expect(await screen.findByText(shown)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeEnabled();
    expect(screen.queryByRole("button", { name: "Continue" })).not.toBeInTheDocument();
  });
});

describe("setup hello step", () => {
  it("names the Echo that spoke when it already has a name, and finishes without renaming", async () => {
    startAt(6);
    const user = userEvent.setup();
    const log = recordRequests();
    const { router } = renderApp("/setup");
    await heading("Say hello");
    expect(screen.getByText("“Alexa, ask encore what's playing.”")).toBeInTheDocument();
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));

    FakeEventSource.emit("request", {
      device_id: 1,
      device_name: "Lounge Echo Show",
      intent: "WhatsPlaying",
      simulated: false,
      time: new Date().toISOString(),
    });

    expect(await screen.findByText(/Request received from Lounge Echo Show at/)).toBeInTheDocument();
    expect(screen.queryByLabelText("Echo name")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Finish" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(log.some((r) => r.method === "PATCH")).toBe(false);
  });

  it("counts a request from just before the step opened", async () => {
    startAt(6);
    const [first] = getDb().requests;
    if (!first) throw new Error("the configured sample has requests");
    getDb().requests = [{ ...first, device_name: "Bedroom Echo", simulated: false, time: new Date().toISOString() }];
    renderApp("/setup");
    await heading("Say hello");
    expect(await screen.findByText(/Request received from Bedroom Echo at/)).toBeInTheDocument();
  });

  it("finishes without naming a new Echo when no name is typed", async () => {
    startAt(6);
    const user = userEvent.setup();
    const log = recordRequests();
    const { router } = renderApp("/setup");
    await heading("Say hello");
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));
    FakeEventSource.emit("request", {
      device_id: 5,
      device_name: "",
      simulated: false,
      time: new Date().toISOString(),
    });

    await user.type(await screen.findByLabelText("Echo name"), "   ");
    await user.click(screen.getByRole("button", { name: "Finish" }));

    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(log.some((r) => r.method === "PATCH")).toBe(false);
  });

  it("shows why the Echo couldn't be named when it isn't about the name", async () => {
    startAt(6);
    const user = userEvent.setup();
    const { router } = renderApp("/setup");
    await heading("Say hello");
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));
    FakeEventSource.emit("request", {
      device_id: 5,
      device_name: "",
      simulated: false,
      time: new Date().toISOString(),
    });
    failWith("patch", "/devices/5", 500, "Database is locked.");

    await user.type(await screen.findByLabelText("Echo name"), "Lounge Echo Show");
    await user.click(screen.getByRole("button", { name: "Finish" }));

    expect(await screen.findByLabelText("Echo name")).toHaveAccessibleDescription(/Database is locked\./);
    expect(router.state.location.pathname).toBe("/setup");
  });

  it("stays on the step when the Echo's name is rejected", async () => {
    startAt(6);
    const user = userEvent.setup();
    const { router } = renderApp("/setup");
    await heading("Say hello");
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));
    FakeEventSource.emit("request", {
      device_id: 5,
      device_name: "",
      simulated: false,
      time: new Date().toISOString(),
    });
    server.use(
      http.patch("*/api/v1/devices/5", () => problem422([{ location: "body.name", message: "That name is taken." }])),
    );

    await user.type(await screen.findByLabelText("Echo name"), "Lounge Echo Show");
    await user.click(screen.getByRole("button", { name: "Finish" }));

    expect(await screen.findByText("That name is taken.")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/setup");
    expect(getSetupProgress()).not.toBeNull();
  });

  it("offers no skipping on the last step", async () => {
    startAt(6);
    renderApp("/setup");
    await heading("Say hello");
    expect(screen.queryByRole("button", { name: "Skip for now" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Back" })).toBeEnabled();
  });
});
