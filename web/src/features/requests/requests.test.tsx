import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { FakeEventSource } from "@test/eventSource";
import { failWith, recordRequests, renderApp } from "@test/utils";

function requestList() {
  return screen.findByRole("region", { name: "Requests" });
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
});
