import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { afterEach, describe, expect, it, vi } from "vitest";

import { keys } from "@api/keys";
import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { FakeEventSource } from "@test/eventSource";
import { failWith, recordRequests, renderApp } from "@test/utils";

afterEach(() => vi.restoreAllMocks());

function requestList() {
  return screen.findByRole("region", { name: "Requests" });
}

/** The request rows currently listed (each is a toggle button). */
function rows() {
  const list = screen.getByRole("region", { name: "Requests" });
  return [
    ...within(list).queryAllByRole("button", { pressed: true }),
    ...within(list).queryAllByRole("button", { pressed: false }),
  ];
}

describe("Request log", () => {
  it("lists requests, selects the newest and shows a request's detail on click", async () => {
    const user = userEvent.setup();
    renderApp("/request-log");

    const list = await requestList();
    const rows = within(list)
      .getAllByRole("button", { pressed: false })
      .concat(within(list).getAllByRole("button", { pressed: true }));
    expect(rows.length).toBeGreaterThan(6);
    expect(within(list).getByRole("button", { name: /play the next episode of severance/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );
    expect(
      await screen.findByRole("heading", {
        level: 2,
        name: "“play the next episode of severance”",
      }),
    ).toBeInTheDocument();

    await user.click(within(list).getByRole("button", { name: /play the new dune/ }));

    expect(await screen.findByRole("heading", { level: 2, name: "“play the new dune”" })).toBeInTheDocument();
    expect(screen.getByText("Dune: Part Two (2024)")).toBeInTheDocument();
    expect(screen.getByText(/score within 0.03 of each other/)).toBeInTheDocument();
    expect(screen.getByText('Title = "the new dune"')).toBeInTheDocument();
    expect(screen.getAllByText(/verify 3 ms · match 4 ms · Kodi 0 ms · total 61 ms/).length).toBeGreaterThan(0);
  });

  it("prepends requests that arrive on the event stream", async () => {
    renderApp("/request-log");
    const list = await requestList();
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));

    FakeEventSource.emit("request", {
      device_id: 2,
      device_name: "Bedroom Echo",
      duration_ms: 133,
      heard: "play conclave",
      id: 9001,
      intent: "PlayMedia",
      locale: "en-AU",
      outcome: "played",
      target: "Bedroom",
      time: new Date().toISOString(),
    });

    const first = await within(list).findByRole("button", { name: /play conclave/ });
    expect(within(list).getAllByRole("button")[0]).toBe(first);
    expect(screen.getByText(/New request from Bedroom Echo: “play conclave”/)).toBeInTheDocument();
  });

  it("names the intent once for a request with nothing heard", async () => {
    renderApp("/request-log");
    const list = await requestList();
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));

    FakeEventSource.emit("request", {
      device_id: 0,
      device_name: "",
      duration_ms: 12,
      heard: "",
      id: 9002,
      intent: "CurrentPlayItemEnquiry",
      locale: "en-AU",
      outcome: "ok",
      target: "Lounge",
      time: new Date().toISOString(),
    });

    const row = await within(list).findByRole("button", { name: /CurrentPlayItemEnquiry/ });
    expect(row).toHaveTextContent(/CurrentPlayItemEnquiry\s*Unnamed Echo/);
    expect(within(row).getAllByText(/CurrentPlayItemEnquiry/)).toHaveLength(1);
  });

  it("labels requests from Amazon's simulator", async () => {
    renderApp("/request-log");
    const list = await requestList();
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));

    FakeEventSource.emit("request", {
      device_id: null,
      device_name: "",
      duration_ms: 20,
      heard: "",
      id: 9003,
      intent: "LaunchRequest",
      locale: "en-AU",
      outcome: "ok",
      simulated: true,
      target: "",
      time: new Date().toISOString(),
    });

    const row = await within(list).findByRole("button", { name: /LaunchRequest/ });
    expect(row).toHaveTextContent("Amazon simulator");
  });

  it("filters by outcome through the query string", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/request-log");
    await requestList();

    await user.selectOptions(screen.getByLabelText("Outcome"), "not_found");

    await waitFor(() =>
      expect(log.some((r) => r.path.startsWith("/requests?") && r.path.includes("outcome=not_found"))).toBe(true),
    );
    const list = await requestList();
    await waitFor(() => expect(within(list).getAllByRole("button")).toHaveLength(1));
    expect(within(list).getByRole("button", { name: /play hot fuzzy/ })).toBeInTheDocument();
  });

  it("clears the outcome filter with All outcomes", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/request-log?outcome=not_found");
    const list = await requestList();
    await waitFor(() => expect(within(list).getAllByRole("button")).toHaveLength(1));

    await user.selectOptions(screen.getByLabelText("Outcome"), "All outcomes");

    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("outcome"));
    await waitFor(() => expect(rows().length).toBeGreaterThan(1));
  });

  it("keeps the page when Enter is pressed in the search box", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/request-log");
    await requestList();
    const submits: boolean[] = [];
    const onSubmit = (event: Event) => submits.push(event.defaultPrevented);
    window.addEventListener("submit", onSubmit);
    try {
      await user.type(screen.getByRole("searchbox", { name: "Search what was heard" }), "severance{Enter}");
    } finally {
      window.removeEventListener("submit", onSubmit);
    }
    // The browser would reload the page for a submit nobody cancelled.
    expect(submits).toEqual([true]);
    await waitFor(() => expect(router.state.location.search).toMatchObject({ q: "severance" }));
  });

  it("shows the first match instead of a request the new filter hides", async () => {
    const user = userEvent.setup();
    renderApp("/request-log?id=1003");
    await screen.findByRole("heading", { level: 2, name: "“pause”" });

    await user.selectOptions(screen.getByLabelText("Outcome"), "not_found");

    expect(await screen.findByRole("heading", { level: 2, name: "“play hot fuzzy”" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { level: 2, name: "“pause”" })).not.toBeInTheDocument();
  });

  it("replays a request as a dry run", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/request-log");
    const list = await requestList();

    await user.click(within(list).getByRole("button", { name: /play blue y/ }));
    await screen.findByRole("heading", { level: 2, name: "“play blue y”" });
    await user.click(screen.getByRole("button", { name: "Replay (dry run)" }));

    const result = await screen.findByRole("region", { name: "Replay result" });
    expect(within(result).getByText(/Player.Open/)).toBeInTheDocument();
    expect(within(result).getByText(/Top match: Bluey \(2018\)/)).toBeInTheDocument();
    const replay = log.find((r) => r.method === "POST");
    expect(replay?.path).toBe("/requests/1004/replay");
    expect(replay?.csrf).toBe("mock-csrf-token");
  });

  it("shows an error when the log can't be loaded", async () => {
    failWith("get", "/requests", 500, "The request log database is locked.");
    renderApp("/request-log");

    expect(await screen.findByRole("alert")).toHaveTextContent("The request log database is locked.");
  });

  it("doesn't show all Echos in the device filter while the Echo names load", async () => {
    let answer: (() => void) | undefined;
    server.use(
      http.get(
        "*/api/v1/devices",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().devices));
          }),
      ),
    );
    renderApp("/request-log?device=2");
    await requestList();
    await waitFor(() => expect(answer).toBeDefined());

    const filter = screen.getByLabelText<HTMLSelectElement>("Echo device");
    expect(filter.selectedOptions[0]).not.toHaveTextContent("All Echos");
    expect(filter.selectedOptions[0]).toHaveTextContent("Loading…");

    answer?.();
    await waitFor(() => expect(filter.selectedOptions[0]).toHaveTextContent("Bedroom Echo"));
    expect(within(filter).queryByText("Loading…")).not.toBeInTheDocument();
  });

  it("keeps the filtered Echo selected and says the Echos didn't load when they fail", async () => {
    const user = userEvent.setup();
    failWith("get", "/devices", 500, "boom");
    const { queryClient } = renderApp("/request-log?device=2");
    await requestList();

    await waitFor(() => expect(queryClient.getQueryState(keys.devices)?.status).toBe("error"));
    const filter = screen.getByLabelText<HTMLSelectElement>("Echo device");
    expect(filter.selectedOptions[0]).toHaveTextContent("Echo #2");
    expect(screen.getByRole("alert")).toHaveTextContent("boom");

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    await waitFor(() => expect(filter.selectedOptions[0]).toHaveTextContent("Bedroom Echo"));
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("doesn't guess the invocation name in the empty log when the skill settings didn't load", async () => {
    getDb().requests = [];
    getDb().skill = { ...getDb().skill, invocation: "kodi", invocation_name: "kodi" };
    failWith("get", "/skill", 500, "boom");
    const { queryClient } = renderApp("/request-log");

    expect(await screen.findByText("No requests yet")).toBeInTheDocument();
    await waitFor(() => expect(queryClient.getQueryState(keys.skill)?.status).toBe("error"));
    expect(screen.queryByText(/ask encore/)).not.toBeInTheDocument();
    expect(screen.getByText("Requests appear here as soon as an Echo talks to Encore.")).toBeInTheDocument();
  });

  it("names a filtered Echo that's no longer in the device list by its id", async () => {
    renderApp("/request-log?device=99");
    await screen.findByText("No matching requests");

    const filter = screen.getByLabelText<HTMLSelectElement>("Echo device");
    await waitFor(() => expect(within(filter).getByText("Bedroom Echo")).toBeInTheDocument());
    expect(filter.selectedOptions[0]).toHaveTextContent("Echo #99");
  });

  it("loads the log again from Try again", async () => {
    const user = userEvent.setup();
    failWith("get", "/requests", 500, "The request log database is locked.");
    renderApp("/request-log");
    expect(await screen.findByRole("alert")).toHaveTextContent("The request log database is locked.");

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await requestList()).toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it("searches what was heard once typing pauses", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    const { router } = renderApp("/request-log");
    await requestList();

    await user.type(screen.getByRole("searchbox", { name: "Search what was heard" }), "  Severance ");

    await waitFor(() => expect(router.state.location.search).toMatchObject({ q: "Severance" }));
    await waitFor(() => expect(rows()).toHaveLength(1));
    expect(rows()[0]).toHaveTextContent("severance");
    // One search for the whole phrase, not one per keystroke.
    expect(log.filter((r) => r.path.includes("q="))).toEqual([
      expect.objectContaining({ path: "/requests?limit=50&q=Severance" }),
    ]);

    await user.clear(screen.getByRole("searchbox", { name: "Search what was heard" }));
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("q"));
  });

  it("filters by Echo, naming unnamed ones", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/request-log");
    await requestList();
    const device = screen.getByRole("combobox", { name: "Echo device" });
    await waitFor(() => expect(within(device).getAllByRole("option").length).toBeGreaterThan(1));
    expect(within(device).getByRole("option", { name: "Unnamed Echo" })).toBeInTheDocument();

    await user.selectOptions(device, "Bedroom Echo");

    await waitFor(() => expect(router.state.location.search).toMatchObject({ device: 2 }));
    await waitFor(() => expect(rows().every((row) => row.textContent?.includes("Bedroom Echo"))).toBe(true));
    expect(rows().length).toBeGreaterThan(0);

    await user.selectOptions(device, "All Echos");
    await waitFor(() => expect(router.state.location.search).not.toHaveProperty("device"));
  });

  it("opens the request named in the address", async () => {
    renderApp("/request-log?id=1002");
    expect(await screen.findByRole("heading", { level: 2, name: "“shuffle radiohead”" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /shuffle radiohead/, pressed: true })).toBeInTheDocument();
  });

  it("ignores filters in the address that make no sense", async () => {
    const { router } = renderApp("/request-log?outcome=exploded&device=abc");
    await requestList();
    expect(router.state.location.search).toEqual({});
    expect(screen.getByRole("combobox", { name: "Outcome" })).toHaveValue("");
  });

  it("loads older requests a page at a time", async () => {
    const user = userEvent.setup();
    renderApp("/request-log");
    await requestList();
    expect(rows()).toHaveLength(50);

    await user.click(screen.getByRole("button", { name: "Load older requests" }));

    await waitFor(() => expect(rows()).toHaveLength(getDb().requests.length));
    expect(screen.queryByRole("button", { name: "Load older requests" })).not.toBeInTheDocument();
  });

  it("says when nothing has been heard yet, with what to say", async () => {
    getDb().requests = [];
    renderApp("/request-log");
    expect(await screen.findByText("No requests yet")).toBeInTheDocument();
    expect(screen.getByText(/Say “Alexa, ask encore what's playing.”/)).toBeInTheDocument();
  });

  it("says when no request matches the filters", async () => {
    renderApp("/request-log?q=nothing-like-this");
    expect(await screen.findByText("No matching requests")).toBeInTheDocument();
    expect(screen.getByText("Try a different search or clear the filters.")).toBeInTheDocument();
  });

  it.each(["device=1", "outcome=error"])("says no request matches when filtered by %s", async (filter) => {
    getDb().requests = getDb().requests.filter((r) => r.device_id !== 1 && r.outcome !== "error");
    renderApp(`/request-log?${filter}`);
    expect(await screen.findByText("No matching requests")).toBeInTheDocument();
    expect(screen.queryByText("No requests yet")).not.toBeInTheDocument();
  });

  it("shows when the live feed drops", async () => {
    renderApp("/request-log");
    await requestList();
    expect(await screen.findByText(/^Live ·/)).toBeInTheDocument();

    FakeEventSource.instances[0]?.onerror?.(new Event("error"));
    expect(await screen.findByText(/^Reconnecting… ·/)).toBeInTheDocument();
  });

  it("explains an unlinked Echo's request and a silent answer", async () => {
    const unlinked = getDb().requests.find((r) => r.id === 1003);
    if (unlinked) unlinked.speech = "";
    renderApp("/request-log?id=1003");
    await screen.findByRole("heading", { level: 2, name: "“pause”" });

    expect(screen.getByText("None (device not linked)")).toBeInTheDocument();
    expect(screen.getByText(/This Echo isn't linked to a Kodi instance/)).toBeInTheDocument();
    expect(screen.getByText("Nothing (silent).")).toBeInTheDocument();
    expect(screen.getByText("None.")).toBeInTheDocument();
  });

  it("says a request that failed before matching reached no Kodi instance, without candidates", async () => {
    const request = getDb().requests.find((r) => r.id === 1003);
    if (!request) throw new Error("no sample request");
    Object.assign(request, { match: null, outcome: "error", target: "" });
    renderApp("/request-log?id=1003");
    await screen.findByRole("heading", { level: 2, name: "“pause”" });

    expect(screen.getByText("None")).toBeInTheDocument();
    expect(screen.queryByText("None (device not linked)")).not.toBeInTheDocument();
    expect(screen.queryByText("Match candidates")).not.toBeInTheDocument();
    expect(screen.queryByText(/no default is set for unlinked Echos/)).not.toBeInTheDocument();
  });

  it("splits time between Encore and Kodi for requests logged without timing, and flags failed calls", async () => {
    const request = getDb().requests.find((r) => r.id === 1002);
    if (!request?.match) throw new Error("no sample request");
    request.timing = null;
    request.kodi_calls = [
      { method: "Player.Open", ms: 140, ok: false },
      { method: "JSONRPC.Ping", ms: 60, ok: true },
    ];
    request.match = { ...request.match, error: "Kodi didn't answer." };
    renderApp("/request-log?id=1002");
    await screen.findByRole("heading", { level: 2, name: "“shuffle radiohead”" });

    expect(screen.getAllByText("Encore 6.2 s · Kodi 200 ms · total 6.4 s").length).toBeGreaterThan(0);
    expect(screen.getByText("(failed)")).toBeInTheDocument();
    expect(screen.getByText("Kodi didn't answer.")).toBeInTheDocument();
  });

  it("never shows negative Encore time when Kodi's calls outlast the request", async () => {
    const request = getDb().requests.find((r) => r.id === 1002);
    if (!request) throw new Error("no sample request");
    request.timing = null;
    request.duration_ms = 500;
    request.kodi_calls = [{ method: "Player.Open", ms: 800, ok: true }];
    renderApp("/request-log?id=1002");
    await screen.findByRole("heading", { level: 2, name: "“shuffle radiohead”" });

    expect(screen.getAllByText("Encore 0 ms · Kodi 800 ms · total 500 ms").length).toBeGreaterThan(0);
  });

  it("lists only the slots Alexa filled", async () => {
    const request = getDb().requests.find((r) => r.id === 1001);
    if (!request) throw new Error("no sample request");
    request.slots = { Room: "", Title: "hot fuzzy", Year: "" };
    renderApp("/request-log?id=1001");
    await screen.findByRole("heading", { level: 2, name: "“play hot fuzzy”" });

    expect(screen.getByText('Title = "hot fuzzy"')).toBeInTheDocument();
    expect(screen.queryByText(/Room =/)).not.toBeInTheDocument();
  });

  it("says when a match had no candidates", async () => {
    const request = getDb().requests.find((r) => r.id === 1001);
    if (!request?.match) throw new Error("no sample request");
    request.match = { ...request.match, candidates: [] };
    renderApp("/request-log?id=1001");
    await screen.findByRole("heading", { level: 2, name: "“play hot fuzzy”" });
    expect(screen.getByText("No candidates.")).toBeInTheDocument();
  });

  it("copies a request as JSON", async () => {
    const user = userEvent.setup();
    renderApp("/request-log?id=1002");
    await screen.findByRole("heading", { level: 2, name: "“shuffle radiohead”" });

    await user.click(screen.getByRole("button", { name: "Copy JSON" }));

    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();
    expect(JSON.parse(await navigator.clipboard.readText())).toMatchObject({ heard: "shuffle radiohead", id: 1002 });
  });

  it("goes back to Copy JSON two seconds after copying", async () => {
    renderApp("/request-log?id=1002");
    await screen.findByRole("heading", { level: 2, name: "“shuffle radiohead”" });
    // Fake timers would also stall TanStack Query's own scheduling, so run the page's two-second timer by hand instead of waiting for it.
    const timers = vi.spyOn(globalThis, "setTimeout");
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Copy JSON" }));
    expect(await screen.findByRole("button", { name: "Copied" })).toBeInTheDocument();

    const reset = timers.mock.calls.find(([, ms]) => ms === 2000)?.[0];
    if (typeof reset !== "function") throw new Error("no two-second timer");
    act(() => reset());
    expect(screen.getByRole("button", { name: "Copy JSON" })).toBeInTheDocument();
  });

  it("explains a replay that matched nothing", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("*/api/v1/requests/1001/replay", () =>
        HttpResponse.json({
          match: { candidates: [], decision: "not_found", error: "", margin: 0, normalized: "hot fuzzy", target: "" },
          speech: "I couldn't find hot fuzzy.",
          would_call: [],
        }),
      ),
    );
    renderApp("/request-log?id=1001");
    await screen.findByRole("heading", { level: 2, name: "“play hot fuzzy”" });
    await user.click(screen.getByRole("button", { name: "Replay (dry run)" }));

    const result = await screen.findByRole("region", { name: "Replay result" });
    expect(within(result).getByText("Nothing in this library comes close.")).toBeInTheDocument();
    expect(within(result).queryByText(/Top match/)).not.toBeInTheDocument();
    expect(within(result).getByText("Alexa would say: “I couldn't find hot fuzzy.”")).toBeInTheDocument();
  });

  it("doesn't claim to have copied when the clipboard refuses", async () => {
    const user = userEvent.setup();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(new DOMException("denied", "NotAllowedError"));
    renderApp("/request-log?id=1002");
    await screen.findByRole("heading", { level: 2, name: "“shuffle radiohead”" });

    await user.click(screen.getByRole("button", { name: "Copy JSON" }));

    expect(screen.getByRole("button", { name: "Copy JSON" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Copied" })).not.toBeInTheDocument();
  });

  it("shows a replay without Kodi calls, and a replay that fails", async () => {
    const user = userEvent.setup();
    server.use(
      http.post("*/api/v1/requests/1003/replay", () => HttpResponse.json({ match: null, speech: "", would_call: [] })),
    );
    renderApp("/request-log?id=1003");
    await screen.findByRole("heading", { level: 2, name: "“pause”" });
    await user.click(screen.getByRole("button", { name: "Replay (dry run)" }));
    const result = await screen.findByRole("region", { name: "Replay result" });
    expect(within(result).getByText("No Kodi calls.")).toBeInTheDocument();
    expect(within(result).queryByText(/Alexa would say/)).not.toBeInTheDocument();

    failWith("post", "/requests/1003/replay", 503, "The matcher is rebuilding its index.");
    await user.click(screen.getByRole("button", { name: "Replay (dry run)" }));
    const pane = screen.getByRole("region", { name: "“pause”" });
    expect(await within(pane).findByText("The matcher is rebuilding its index.")).toBeInTheDocument();
    // Shown next to the button only, not again in a toast.
    expect(screen.queryByText("That didn't work")).not.toBeInTheDocument();
    expect(screen.getAllByText("The matcher is rebuilding its index.")).toHaveLength(1);
  });

  it("offers to retry a request that fails to load", async () => {
    const user = userEvent.setup();
    failWith("get", "/requests/1002", 500, "Database is locked.");
    renderApp("/request-log?id=1002");
    expect(await screen.findByRole("alert", {}, { timeout: 4000 })).toHaveTextContent("Database is locked.");

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { level: 2, name: "“shuffle radiohead”" })).toBeInTheDocument();
  });
});
