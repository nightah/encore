import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { RECENT_REQUESTS } from "@api/events";
import { FakeEventSource } from "@test/eventSource";
import { failWith, recordRequests, renderApp } from "@test/utils";

describe("overview", () => {
  it("shows status tiles, Kodi instances, recent requests and attention items", async () => {
    renderApp("/");
    const status = await screen.findByRole("region", { name: "Status" });
    expect(within(status).getByText("Live")).toBeInTheDocument();
    expect(within(status).getByText("Development stage · 1 locale")).toBeInTheDocument();
    expect(within(status).getByText("182 ms")).toBeInTheDocument();
    expect(await within(status).findByText(/Lounge and Bedroom share one/)).toBeInTheDocument();

    const kodi = screen.getByRole("region", { name: "Kodi instances" });
    expect(within(kodi).getByRole("heading", { name: "Lounge" })).toBeInTheDocument();
    expect(within(kodi).getByText("Playing")).toBeInTheDocument();
    expect(within(kodi).getByText("S2 · E4 “Woe's Hollow”")).toBeInTheDocument();
    expect(within(kodi).getByText("31:12 / 49:03")).toBeInTheDocument();
    expect(within(kodi).getByText("Asleep")).toBeInTheDocument();
    expect(within(kodi).getByText(/Voice requests will wake it automatically/)).toBeInTheDocument();

    const recent = screen.getByRole("table", { name: "Recent requests" });
    expect(within(recent).getByText("“play the new dune”")).toBeInTheDocument();
    expect(within(recent).getByText("Asked which one")).toBeInTheDocument();
    expect(within(recent).getByText("Unnamed Echo")).toBeInTheDocument();

    const attention = screen.getByRole("region", { name: "Needs attention" });
    expect(within(attention).getByRole("link", { name: "Link device" })).toHaveAttribute("href", "/echo-devices");
  });

  it("opens a recent request in the request log", async () => {
    const user = userEvent.setup();
    const { router } = renderApp("/");
    const recent = await screen.findByRole("table", { name: "Recent requests" });

    await user.click(within(recent).getByText("“play the new dune”"));

    expect(await screen.findByRole("heading", { level: 2, name: "“play the new dune”" })).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/request-log");
    expect(router.state.location.search).toMatchObject({ id: expect.any(Number) });
  });

  it("is read-only: it never writes", async () => {
    const requests = recordRequests();
    renderApp("/");
    await screen.findByRole("region", { name: "Status" });
    expect(requests.every((r) => r.method === "GET")).toBe(true);
    expect(screen.queryByRole("button", { name: /wake|sync|play|pause/i })).not.toBeInTheDocument();
  });

  it("keeps the full list of recent requests as live ones arrive, without the simulator's", async () => {
    renderApp("/");
    const recent = await screen.findByRole("table", { name: "Recent requests" });
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));
    const rows = () => within(recent).getAllByRole("row").length - 1;
    expect(rows()).toBe(RECENT_REQUESTS);

    const base = {
      device_id: 2,
      device_name: "Bedroom Echo",
      duration_ms: 20,
      heard: "",
      intent: "AMAZON.PauseIntent",
      locale: "en-AU",
      outcome: "ok",
      target: "Bedroom",
    };
    FakeEventSource.emit("request", { ...base, id: 9101, simulated: false, time: new Date().toISOString() });
    expect(await within(recent).findByText("AMAZON.PauseIntent")).toBeInTheDocument();
    expect(rows()).toBe(RECENT_REQUESTS);

    FakeEventSource.emit("request", {
      ...base,
      id: 9102,
      intent: "LaunchRequest",
      simulated: true,
      time: new Date().toISOString(),
    });
    // Events are handled in order, so once a later live request shows, the simulated one has been handled and left out.
    FakeEventSource.emit("request", { ...base, id: 9103, intent: "AMAZON.StopIntent", time: new Date().toISOString() });
    expect(await within(recent).findByText("AMAZON.StopIntent")).toBeInTheDocument();
    expect(within(recent).queryByText("LaunchRequest")).not.toBeInTheDocument();
  });

  it("adds requests and replaces attention items from the event stream", async () => {
    renderApp("/");
    const recent = await screen.findByRole("table", { name: "Recent requests" });
    expect(FakeEventSource.open).toBeGreaterThan(0);
    const tile = screen.getByText("Requests today").parentElement as HTMLElement;
    const before = Number(within(tile).getByText(/^\d+$/).textContent);

    FakeEventSource.emit("request", {
      device_id: 2,
      device_name: "Bedroom Echo",
      duration_ms: 133,
      heard: "play slow horses",
      id: 9001,
      intent: "PlayMedia",
      locale: "en-AU",
      outcome: "played",
      target: "Bedroom",
      time: new Date().toISOString(),
    });
    expect(await within(recent).findByText("“play slow horses”")).toBeInTheDocument();
    expect(within(tile).getByText(String(before + 1))).toBeInTheDocument();

    FakeEventSource.emit("attention", []);
    expect(await screen.findByText("Nothing needs your attention.")).toBeInTheDocument();
  });

  it("shows an error with a retry", async () => {
    failWith("get", "/overview", 500, "The database is locked.");
    renderApp("/");
    expect(await screen.findByRole("alert")).toHaveTextContent("The database is locked.");
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });
});
