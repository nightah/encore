import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import type { Instance, Library } from "@api/generated/model";
import { keys } from "@api/keys";
import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { FakeEventSource } from "@test/eventSource";
import { failWith, recordRequests, renderApp } from "@test/utils";

async function card(name: string) {
  return within(await screen.findByRole("article", { name }));
}

function patchInstance(
  id: number,
  patch: Partial<Omit<Instance, "status" | "library">> & {
    status?: Partial<Instance["status"]>;
    library?: Partial<Instance["library"]>;
  },
) {
  const db = getDb();
  db.instances = db.instances.map((i) =>
    i.id === id
      ? { ...i, ...patch, library: { ...i.library, ...patch.library }, status: { ...i.status, ...patch.status } }
      : i,
  );
}

function patchLibrary(id: number, patch: Partial<Library>) {
  const db = getDb();
  db.libraries = db.libraries.map((l) => (l.id === id ? { ...l, ...patch } : l));
}

describe("Kodi instances page", () => {
  it("shows the network search until discovery answers", async () => {
    let answer: () => void = () => {};
    server.use(
      http.get(
        "*/api/v1/kodi/discovered",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().discovered));
          }),
      ),
    );
    renderApp("/kodi");

    const found = await screen.findByRole("complementary", { name: "Found on your network" });
    expect(await within(found).findByRole("status")).toHaveTextContent("Searching for Kodi with mDNS");
    answer();
    expect(await within(found).findByText("LibreELEC-Study")).toBeInTheDocument();
    expect(within(found).queryByText(/Searching for Kodi/)).not.toBeInTheDocument();
  });

  it("shows each instance, the shared library and discovered Kodi", async () => {
    renderApp("/kodi");

    expect(await screen.findByRole("heading", { level: 1, name: "Kodi instances" })).toBeInTheDocument();
    const lounge = await card("Lounge");
    expect(lounge.getByText("Connected · WebSocket")).toBeInTheDocument();
    expect(lounge.getByText("Shared library · source")).toBeInTheDocument();
    expect(lounge.getByText("192.168.1.20:8080")).toBeInTheDocument();
    expect((await card("Bedroom")).getByText("Connected · HTTP only")).toBeInTheDocument();
    expect(screen.getByText(/The WebSocket is off/)).toBeInTheDocument();
    expect((await card("Kids' room")).getByRole("button", { name: "Wake now" })).toBeInTheDocument();

    const shared = screen.getByRole("region", { name: "Shared library: Lounge and Bedroom" });
    expect(within(shared).getByText("Likely shared")).toBeInTheDocument();
    expect(within(shared).getByRole("combobox", { name: "Library source" })).toHaveValue("1");

    expect(await screen.findByText("LibreELEC-Study")).toBeInTheDocument();
  });

  it("runs a connection test and lists each check", async () => {
    const user = userEvent.setup();
    renderApp("/kodi");
    const lounge = await card("Lounge");

    await user.click(lounge.getByRole("button", { name: "Test connection" }));

    const results = await lounge.findByRole("list", { name: "Connection test results" });
    expect(within(results).getByText("JSONRPC.Ping over HTTP")).toBeInTheDocument();
    expect(within(results).getByText("12 ms")).toBeInTheDocument();
    expect(lounge.getByRole("button", { name: "Hide test results" })).toBeInTheDocument();
  });

  it("makes an instance the default for unlinked Echos with the CSRF token", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    const bedroom = await card("Bedroom");

    await user.click(bedroom.getByRole("button", { name: "Use for unlinked Echos" }));

    expect(await bedroom.findByText("Default for unlinked Echos")).toBeInTheDocument();
    const call = log.find((r) => r.method === "POST" && r.path === "/kodi/instances/2/default");
    expect(call?.csrf).toBe("mock-csrf-token");
    expect(getDb().instances.find((i) => i.id === 2)?.default_for_unlinked).toBe(true);
  });

  it("opens config-managed instances read-only", async () => {
    const user = userEvent.setup();
    renderApp("/kodi");
    const kids = await card("Kids' room");

    await user.click(kids.getByRole("button", { name: "Edit Kids' room" }));

    const dialog = await screen.findByRole("dialog", { name: "Edit Kids' room" });
    expect(within(dialog).getByText("Managed by config")).toBeInTheDocument();
    expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveAttribute("readonly");
    expect(within(dialog).queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: "Remove…" })).not.toBeInTheDocument();
  });

  it("shows server validation errors next to the field", async () => {
    const user = userEvent.setup();
    renderApp("/kodi");
    await card("Lounge");

    await user.click(screen.getByRole("button", { name: "Add Kodi manually" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a Kodi instance" });
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "Lounge");
    await user.type(within(dialog).getByRole("textbox", { name: "JSON-RPC URL" }), "http://192.168.1.60:8080/jsonrpc");
    await user.click(screen.getByRole("button", { name: "Add Kodi instance" }));

    expect(await within(dialog).findByText("Another Kodi instance already uses this name.")).toBeInTheDocument();
    expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveAttribute("aria-invalid", "true");
  });

  it("prefills the add dialog from a discovered instance and adds it", async () => {
    const user = userEvent.setup();
    renderApp("/kodi");

    await user.click(await screen.findByRole("button", { name: "Add LibreELEC-Study" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a Kodi instance" });
    expect(within(dialog).getByRole("textbox", { name: "JSON-RPC URL" })).toHaveValue(
      "http://192.168.1.47:8080/jsonrpc",
    );
    await user.click(screen.getByRole("button", { name: "Add Kodi instance" }));

    expect(await screen.findByRole("article", { name: "LibreELEC-Study" })).toBeInTheDocument();
  });

  it("saves an instance's title language, time zone and shutdown behaviour", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    const lounge = await card("Lounge");

    await user.click(lounge.getByRole("button", { name: "Edit Lounge" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Lounge" });
    const shutdown = within(dialog).getByRole("combobox", { name: "“Shut down”" });
    expect(shutdown).toHaveValue("inherit");

    await user.selectOptions(within(dialog).getByRole("combobox", { name: "Title language" }), "de");
    await user.type(within(dialog).getByRole("textbox", { name: "Time zone" }), "Australia/Melbourne");
    await user.selectOptions(shutdown, "quit");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(getDb().instances.find((i) => i.name === "Lounge")?.shutdown).toBe("quit"));
    expect(log.find((r) => r.method === "PATCH")?.body).toMatchObject({
      language: "de",
      shutdown: "quit",
      timezone: "Australia/Melbourne",
    });
  });

  it("forgets an unconfirmed removal when the dialog closes", async () => {
    const user = userEvent.setup();
    renderApp("/kodi");
    const lounge = await card("Lounge");

    await user.click(lounge.getByRole("button", { name: "Edit Lounge" }));
    let dialog = await screen.findByRole("dialog", { name: "Edit Lounge" });
    await user.click(within(dialog).getByRole("button", { name: "Remove…" }));
    expect(within(dialog).getByRole("button", { name: "Keep it" })).toBeVisible();
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    await user.click(lounge.getByRole("button", { name: "Edit Lounge" }));
    dialog = await screen.findByRole("dialog", { name: "Edit Lounge" });
    expect(within(dialog).getByRole("button", { name: "Remove…" })).toBeVisible();
    expect(within(dialog).queryByRole("button", { name: "Keep it" })).not.toBeInTheDocument();
    expect(getDb().instances.some((i) => i.name === "Lounge")).toBe(true);
  });

  it("updates a card from an instance event", async () => {
    renderApp("/kodi");
    const kids = await card("Kids' room");
    expect(kids.getByText("Asleep")).toBeInTheDocument();
    await waitFor(() => expect(FakeEventSource.open).toBeGreaterThan(0));

    const instance = getDb().instances.find((i) => i.id === 3);
    FakeEventSource.emit("instance", {
      ...instance,
      status: { ...instance?.status, state: "connected" },
    });

    expect(await kids.findByText("Connected · WebSocket")).toBeInTheDocument();
  });
  it("shares a library between instances that use the same database", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    await card("Kids' room");

    await user.click(screen.getByRole("button", { name: "Share a library" }));
    const dialog = await screen.findByRole("dialog", { name: "Share a library" });
    const confirm = within(dialog).getByRole("button", { name: /^Share library/ });
    await user.click(within(dialog).getByRole("checkbox", { name: "Kids' room" }));
    expect(confirm).toBeDisabled();
    expect(within(dialog).getByText(/Shares a library with Lounge/)).toBeInTheDocument();

    await user.click(within(dialog).getByRole("checkbox", { name: "Bedroom" }));
    await user.click(within(dialog).getByRole("button", { name: "Share library between 2" }));

    await waitFor(() => expect(log.find((r) => r.path === "/libraries/group")?.body).toEqual({ instance_ids: [3, 2] }));
    expect(await screen.findByRole("heading", { name: /Shared library: Kids' room and Bedroom/ })).toBeInTheDocument();
  });

  it("adds an instance to a shared library with its members already ticked", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    await card("Kids' room");

    await user.click(screen.getByRole("button", { name: "Add an instance" }));
    const dialog = await screen.findByRole("dialog", { name: "Share a library" });
    expect(within(dialog).getByRole("checkbox", { name: "Lounge" })).toBeChecked();
    expect(within(dialog).getByRole("checkbox", { name: "Bedroom" })).toBeChecked();

    await user.click(within(dialog).getByRole("checkbox", { name: "Kids' room" }));
    await user.click(within(dialog).getByRole("button", { name: "Share library between 3" }));

    await waitFor(() =>
      expect(log.find((r) => r.path === "/libraries/group")?.body).toEqual({ instance_ids: [1, 2, 3] }),
    );
  });

  it("waits for the shared libraries before saying who an instance shares with", async () => {
    let answer: () => void = () => {};
    server.use(
      http.get(
        "*/api/v1/libraries",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().libraries));
          }),
      ),
    );
    const { queryClient } = renderApp("/kodi");

    await waitFor(() => expect(queryClient.getQueryState(keys.instances)?.status).toBe("success"));
    expect(screen.queryByText(/^Shared with/)).not.toBeInTheDocument();
    expect(screen.getByText("Loading…")).toBeInTheDocument();

    answer();
    expect(await (await card("Lounge")).findByText("Shared with Bedroom")).toBeInTheDocument();
    expect(screen.queryByText("Loading…")).not.toBeInTheDocument();
  });

  it("says the shared libraries didn't load instead of sharing with no one", async () => {
    failWith("get", "/libraries", 500, "boom");
    const { queryClient } = renderApp("/kodi");

    await waitFor(() => expect(queryClient.getQueryState(keys.libraries)?.status).toBe("error"));
    await waitFor(() => expect(queryClient.getQueryState(keys.instances)?.status).toBe("success"));
    expect(screen.queryByText(/^Shared with/)).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("boom");
  });
});

describe("Kodi instance card", () => {
  it("starts a library sync", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");

    await user.click((await card("Lounge")).getByRole("button", { name: "Sync library" }));

    expect((await screen.findAllByText(/Library sync started/)).length).toBeGreaterThan(0);
    expect(log.find((r) => r.method === "POST")?.path).toBe("/kodi/instances/1/sync");
  });

  it("wakes a sleeping instance and says it's waiting for Kodi", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    const kids = await card("Kids' room");
    expect(kids.queryByRole("button", { name: "Test connection" })).not.toBeInTheDocument();
    expect(kids.getByText("60 s, then “it's still waking up”")).toBeInTheDocument();

    await user.click(kids.getByRole("button", { name: "Wake now" }));

    expect(await kids.findByRole("button", { name: "Waking… (waiting for Kodi)" })).toBeDisabled();
    expect(log.find((r) => r.method === "POST")?.path).toBe("/kodi/instances/3/wake");
  });

  it("offers no wake for a sleeping instance without a MAC address", async () => {
    patchInstance(3, { mac: "" });
    renderApp("/kodi");
    const kids = await card("Kids' room");

    expect(kids.queryByRole("button", { name: "Wake now" })).not.toBeInTheDocument();
    expect(kids.getByText("Not set")).toBeInTheDocument();
  });

  it("warns when Kodi can't be reached", async () => {
    patchInstance(1, { status: { state: "offline" } });
    renderApp("/kodi");
    const lounge = await card("Lounge");

    expect(lounge.getByText(/Encore can't reach Lounge/)).toBeInTheDocument();
    expect(lounge.queryByRole("button", { name: "Sync library" })).not.toBeInTheDocument();
    expect(lounge.getByRole("button", { name: "Wake now" })).toBeInTheDocument();
  });

  it("hides the connection test results again", async () => {
    const user = userEvent.setup();
    renderApp("/kodi");
    const lounge = await card("Lounge");

    await user.click(lounge.getByRole("button", { name: "Test connection" }));
    await lounge.findByRole("list", { name: "Connection test results" });
    await user.click(lounge.getByRole("button", { name: "Hide test results" }));

    expect(lounge.queryByRole("list", { name: "Connection test results" })).not.toBeInTheDocument();
    expect(lounge.getByRole("button", { name: "Test connection" })).toBeInTheDocument();
  });

  it("marks failed checks and leaves out their time", async () => {
    const user = userEvent.setup();
    renderApp("/kodi");
    const bedroom = await card("Bedroom");

    await user.click(bedroom.getByRole("button", { name: "Test connection" }));

    const results = await bedroom.findByRole("list", { name: "Connection test results" });
    const items = within(results).getAllByRole("listitem");
    expect(items).toHaveLength(4);
    expect(items[1]).toHaveTextContent(/^Failed: WebSocket on :9090 refused/);
    expect(items[1]).not.toHaveTextContent(/\d+ ms$/);
    expect(items[0]).toHaveTextContent("Passed: JSONRPC.Ping over HTTP12 ms");
  });

  it("shows why a connection test couldn't run", async () => {
    failWith("post", "/kodi/instances/1/test", 502, "Kodi closed the connection.");
    const user = userEvent.setup();
    renderApp("/kodi");
    const lounge = await card("Lounge");

    await user.click(lounge.getByRole("button", { name: "Test connection" }));

    expect(await lounge.findByRole("alert")).toHaveTextContent("Kodi closed the connection.");
    expect(lounge.getByRole("button", { name: "Hide test results" })).toBeInTheDocument();
  });

  it("explains an HTTP-only instance with its own library, quoting its name as text", async () => {
    patchInstance(2, { library: { is_source: true, shared: false }, name: "<b>Den</b>" });
    getDb().libraries = getDb().libraries.filter((l) => l.id !== 3);
    renderApp("/kodi");
    const den = await card("<b>Den</b>");

    expect(den.getByText(/Encore syncs the library on a schedule/)).toHaveTextContent(
      "can't see what <b>Den</b> is playing",
    );
    expect(den.getByText("Its own library")).toBeInTheDocument();
  });

  it("names the library source when an HTTP-only instance shares a library", async () => {
    renderApp("/kodi");
    const bedroom = await card("Bedroom");
    expect(bedroom.getByText(/Library updates still arrive through Lounge \(the library source\)/)).toBeInTheDocument();
    expect(bedroom.getByText("Shared with Lounge")).toBeInTheDocument();
  });

  it("falls back to “the library source” when the shared library isn't known", async () => {
    getDb().libraries = [];
    renderApp("/kodi");
    const bedroom = await card("Bedroom");
    expect(bedroom.getByText(/arrive through the library source \(the library source\)/)).toBeInTheDocument();
  });

  it("summarises a connected instance's library, events and next sync", async () => {
    renderApp("/kodi");
    const lounge = await card("Lounge");

    expect(lounge.getByText("2,184 movies · 312 shows · shared with Bedroom")).toBeInTheDocument();
    expect(lounge.getByText("Conclave (2024) · findable")).toBeInTheDocument();
    expect(lounge.getByText(/^Every 15 min · next \d/)).toBeInTheDocument();
    expect(lounge.getByText("Kodi 21.3 · API 13.5")).toBeInTheDocument();
  });

  it("summarises a connected instance that hasn't been heard from or synced yet", async () => {
    patchInstance(3, {
      library: { movies: 1, newest_title: null, shows: 1 },
      status: { last_event_at: null, next_sync_at: null, state: "connected" },
    });
    renderApp("/kodi");
    const kids = await card("Kids' room");

    expect(kids.getByText("1 movie · 1 show")).toBeInTheDocument();
    expect(kids.getByText("None yet")).toBeInTheDocument();
    expect(kids.getByText("Every 15 min")).toBeInTheDocument();
    expect(kids.getByText("-")).toBeInTheDocument();
    expect(kids.getByText("Its own library")).toBeInTheDocument();
  });

  it("gives a newest title without a year and says when it can't be found yet", async () => {
    patchInstance(3, {
      library: { newest_title: { findable: false, title: "Bluey S3 E49", year: 0 } },
      status: { state: "connected" },
    });
    renderApp("/kodi");
    expect((await card("Kids' room")).getByText("Bluey S3 E49 · not findable yet")).toBeInTheDocument();
  });
});

describe("Found on your network", () => {
  it("says nothing new was found and keeps scanning", async () => {
    getDb().discovered = [];
    renderApp("/kodi");
    const found = await screen.findByRole("complementary", { name: "Found on your network" });

    expect(await within(found).findByText("Nothing new found.")).toBeInTheDocument();
    expect(within(found).getByText(/^Last scan .* · scanning continuously$/)).toBeInTheDocument();
    expect(within(found).queryByRole("button")).not.toBeInTheDocument();
  });

  it("says when discovery isn't available", async () => {
    failWith("get", "/kodi/discovered", 503, "mDNS is off.");
    renderApp("/kodi");
    const found = await screen.findByRole("complementary", { name: "Found on your network" });

    expect(await within(found).findByText("Discovery isn't available right now.")).toBeInTheDocument();
    expect(within(found).queryByText(/Last scan/)).not.toBeInTheDocument();
  });

  it("shows the Kodi version only when it was advertised", async () => {
    getDb().discovered = [
      { host: "192.168.1.60", http_port: 8080, name: "Garage", version: "", ws_port: 0 },
      { host: "192.168.1.61", http_port: 8080, name: "Shed", version: "21.3", ws_port: 9090 },
    ];
    renderApp("/kodi");
    const found = await screen.findByRole("complementary", { name: "Found on your network" });

    expect(await within(found).findByText("192.168.1.60")).toBeInTheDocument();
    expect(within(found).getByText("192.168.1.61 · Kodi 21.3")).toBeInTheDocument();
  });

  it("prefills no WebSocket address when none was advertised", async () => {
    getDb().discovered = [{ host: "192.168.1.60", http_port: 8081, name: "Garage", version: "", ws_port: 0 }];
    const user = userEvent.setup();
    renderApp("/kodi");

    await user.click(await screen.findByRole("button", { name: "Add Garage" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a Kodi instance" });
    expect(within(dialog).getByRole("textbox", { name: "JSON-RPC URL" })).toHaveValue(
      "http://192.168.1.60:8081/jsonrpc",
    );
    expect(within(dialog).getByRole("textbox", { name: "WebSocket URL" })).toHaveValue("");
    expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveValue("Garage");
  });
});

describe("Shared library panel", () => {
  const panel = async (name = "Lounge and Bedroom") => screen.findByRole("region", { name: `Shared library: ${name}` });

  it.each<[string, Partial<Library>, RegExp]>([
    [
      "likely shared, with evidence",
      {},
      /^These instances appear to use the same Kodi database: 200 of 200 .*\. The next time .* on both instances at once; .* listens for changes from both\.$/,
    ],
    [
      "likely shared, without evidence",
      { evidence: "" },
      /^These instances appear to use the same Kodi database\. The next time/,
    ],
    [
      "confirmed shared",
      { state: "confirmed_shared" },
      /^These instances use the same Kodi database: playback on one changed .* on both at once\. Encore keeps/,
    ],
    ["grouped by hand", { state: "manual" }, /^You grouped these instances by hand\. Encore keeps .* from both\.$/],
  ])("explains a library that is %s", async (_name, patch, explanation) => {
    patchLibrary(3, patch);
    renderApp("/kodi");
    expect(within(await panel()).getByText(explanation)).toBeInTheDocument();
  });

  it("speaks of “all of them” when more than two instances share", async () => {
    patchLibrary(3, {
      members: [
        { instance_id: 1, name: "Lounge" },
        { instance_id: 2, name: "Bedroom" },
        { instance_id: 3, name: "Kids' room" },
      ],
      state: "confirmed_shared",
    });
    renderApp("/kodi");
    const region = await panel();
    expect(
      within(region).getByText(/on all of them at once\. .* listens for changes from all of them\.$/),
    ).toBeInTheDocument();
    expect(within(region).getByText("Confirmed shared")).toBeInTheDocument();
    expect(within(region).queryByRole("button", { name: "Add an instance" })).not.toBeInTheDocument();
  });

  it("changes the library source", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    const source = within(await panel()).getByRole("combobox", { name: "Library source" });

    await user.selectOptions(source, "Bedroom");

    await waitFor(() => expect(log.find((r) => r.method === "PATCH")?.body).toEqual({ source_instance_id: 2 }));
    expect(log.find((r) => r.method === "PATCH")?.path).toBe("/libraries/3");
    expect((await screen.findAllByText("Library source changed.")).length).toBeGreaterThan(0);
    await waitFor(() => expect(source).toHaveValue("2"));
  });

  it("says which instance Encore syncs from while the source is unreachable", async () => {
    patchLibrary(3, { syncing_instance_id: 2 });
    renderApp("/kodi");
    expect(
      within(await panel()).getByText("The source is unreachable, so Encore is syncing from Bedroom."),
    ).toBeInTheDocument();
  });

  it("splits an instance out of the group, suggesting one that isn't the source", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");

    await user.click(within(await panel()).getByRole("button", { name: "Not shared? Split" }));
    const dialog = await screen.findByRole("dialog", { name: "Split the shared library" });
    expect(within(dialog).getByRole("radio", { name: "Bedroom" })).toBeChecked();
    expect(within(dialog).getByRole("radio", { name: "LoungeCurrent library source" })).not.toBeChecked();

    await user.click(within(dialog).getByRole("radio", { name: /Lounge/ }));
    await user.click(within(dialog).getByRole("button", { name: "Split" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.find((r) => r.method === "POST")).toMatchObject({
      body: { instance_id: 1 },
      path: "/libraries/3/split",
    });
    await waitFor(() => expect(screen.queryByRole("region", { name: /^Shared library/ })).not.toBeInTheDocument());
  });

  it("cancels a split without changing anything, and reopens on the suggestion", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    const split = within(await panel()).getByRole("button", { name: "Not shared? Split" });

    await user.click(split);
    let dialog = await screen.findByRole("dialog", { name: "Split the shared library" });
    await user.click(within(dialog).getByRole("radio", { name: /Lounge/ }));
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(dialog).not.toBeInTheDocument());

    await user.click(split);
    dialog = await screen.findByRole("dialog", { name: "Split the shared library" });
    expect(within(dialog).getByRole("radio", { name: "Bedroom" })).toBeChecked();
    expect(log.some((r) => r.method !== "GET")).toBe(false);
  });
});

describe("Share a library dialog", () => {
  it("unticks an instance and closes without sharing", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    await card("Kids' room");

    await user.click(screen.getByRole("button", { name: "Share a library" }));
    const dialog = await screen.findByRole("dialog", { name: "Share a library" });
    await user.click(within(dialog).getByRole("checkbox", { name: "Lounge" }));
    await user.click(within(dialog).getByRole("checkbox", { name: "Bedroom" }));
    expect(within(dialog).getByRole("button", { name: "Share library between 2" })).toBeEnabled();
    await user.click(within(dialog).getByRole("checkbox", { name: "Lounge" }));
    expect(within(dialog).getByRole("checkbox", { name: "Lounge" })).not.toBeChecked();
    expect(within(dialog).getByRole("button", { name: "Share library" })).toBeDisabled();

    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.some((r) => r.method !== "GET")).toBe(false);
  });

  it("shows why sharing failed and starts clean the next time", async () => {
    failWith("post", "/libraries/group", 409, "Bedroom is being synced.");
    const user = userEvent.setup();
    renderApp("/kodi");
    await card("Kids' room");

    await user.click(screen.getByRole("button", { name: "Share a library" }));
    let dialog = await screen.findByRole("dialog", { name: "Share a library" });
    await user.click(within(dialog).getByRole("checkbox", { name: "Kids' room" }));
    await user.click(within(dialog).getByRole("checkbox", { name: "Bedroom" }));
    await user.click(within(dialog).getByRole("button", { name: "Share library between 2" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Bedroom is being synced.");

    await user.keyboard("{Escape}");
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Share a library" }));
    dialog = await screen.findByRole("dialog", { name: "Share a library" });
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
    expect(within(dialog).getByRole("checkbox", { name: "Kids' room" })).not.toBeChecked();
  });

  it("isn't offered with a single Kodi instance", async () => {
    const db = getDb();
    db.instances = db.instances.filter((i) => i.id === 3);
    db.libraries = db.libraries.filter((l) => l.id === 4);
    renderApp("/kodi");
    await card("Kids' room");
    expect(screen.queryByRole("button", { name: "Share a library" })).not.toBeInTheDocument();
  });
});

describe("Kodi instance dialog", () => {
  it("says there are no Kodi instances yet and adds the first one", async () => {
    const db = getDb();
    db.instances = [];
    db.libraries = [];
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");

    expect(await screen.findByText("No Kodi instances yet")).toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: "Add Kodi manually" })[1] as HTMLElement);
    const dialog = await screen.findByRole("dialog", { name: "Add a Kodi instance" });
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "  Garage ");
    await user.type(within(dialog).getByRole("textbox", { name: "JSON-RPC URL" }), "http://192.168.1.60:8080/jsonrpc");
    await user.type(within(dialog).getByLabelText("Kodi password"), "hunter2");
    await user.click(within(dialog).getByRole("button", { name: "Add Kodi instance" }));

    expect(await screen.findByRole("article", { name: "Garage" })).toBeInTheDocument();
    // A new instance leaves out the optional fields left empty, so the server applies its defaults.
    expect(log.find((r) => r.method === "POST")?.body).toEqual({
      language: "en",
      name: "Garage",
      password: "hunter2",
      shutdown: "inherit",
      timezone: "",
      url: "http://192.168.1.60:8080/jsonrpc",
      username: "kodi",
    });
  });

  it.each([
    [
      "JSON-RPC URL",
      "192.168.1.60:8080",
      "Enter an http:// or https:// address, e.g. http://192.168.1.20:8080/jsonrpc.",
    ],
    ["WebSocket URL", "http://192.168.1.60:9090", "Enter a ws:// or wss:// address, or leave it empty."],
    ["MAC address (Wake-on-LAN)", "DC:A6:32:4B:19", "Enter a MAC address like DC:A6:32:4B:19:0E, or leave it empty."],
  ])("rejects an invalid %s before sending", async (label, value, message) => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");
    await card("Lounge");

    await user.click(screen.getByRole("button", { name: "Add Kodi manually" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a Kodi instance" });
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "Garage");
    if (label !== "JSON-RPC URL") {
      await user.type(
        within(dialog).getByRole("textbox", { name: "JSON-RPC URL" }),
        "http://192.168.1.60:8080/jsonrpc",
      );
    }
    await user.type(within(dialog).getByRole("textbox", { name: label }), value);
    await user.click(within(dialog).getByRole("button", { name: "Add Kodi instance" }));

    expect(await within(dialog).findByText(message)).toBeInTheDocument();
    expect(within(dialog).getByRole("textbox", { name: label })).toHaveAttribute("aria-invalid", "true");
    expect(log.some((r) => r.method === "POST")).toBe(false);
  });

  it("asks for a name before sending", async () => {
    const user = userEvent.setup();
    renderApp("/kodi");
    await card("Lounge");

    await user.click(screen.getByRole("button", { name: "Add Kodi manually" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a Kodi instance" });
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "   ");
    await user.click(within(dialog).getByRole("button", { name: "Add Kodi instance" }));

    expect(await within(dialog).findByText("Give it a name, like the room it's in.")).toBeInTheDocument();
  });

  it("sends cleared optional fields when editing, so they're removed, and keeps the saved password", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");

    await user.click((await card("Lounge")).getByRole("button", { name: "Edit Lounge" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Lounge" });
    expect(within(dialog).getByText("A password is saved. Leave this empty to keep it.")).toBeInTheDocument();
    await user.clear(within(dialog).getByRole("textbox", { name: "MAC address (Wake-on-LAN)" }));
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    const body = log.find((r) => r.method === "PATCH")?.body;
    expect(body).toMatchObject({ broadcast: "", mac: "", websocket_url: "ws://192.168.1.20:9090/jsonrpc" });
    expect(body).not.toHaveProperty("password");
  });

  it("closes a config-managed instance without saving", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");

    await user.click((await card("Kids' room")).getByRole("button", { name: "Edit Kids' room" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Kids' room" });
    await user.click(within(dialog).getAllByRole("button", { name: "Close" }).at(-1) as HTMLElement);

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.some((r) => r.method !== "GET")).toBe(false);
  });

  it("removes an instance once confirmed", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/kodi");

    await user.click((await card("Bedroom")).getByRole("button", { name: "Edit Bedroom" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Bedroom" });
    await user.click(within(dialog).getByRole("button", { name: "Remove…" }));
    expect(within(dialog).getByText(/Remove Bedroom\? Echos linked to it stop working/)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Keep it" }));
    await user.click(within(dialog).getByRole("button", { name: "Remove…" }));
    await user.click(within(dialog).getByRole("button", { name: "Remove" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.filter((r) => r.method === "DELETE").map((r) => r.path)).toEqual(["/kodi/instances/2"]);
    await waitFor(() => expect(screen.queryByRole("article", { name: "Bedroom" })).not.toBeInTheDocument());
  });

  it("shows a failure that isn't about one field and keeps the dialog open", async () => {
    failWith("patch", "/kodi/instances/1", 500, "Couldn't save the instance.");
    const user = userEvent.setup();
    renderApp("/kodi");

    await user.click((await card("Lounge")).getByRole("button", { name: "Edit Lounge" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Lounge" });
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Couldn't save the instance.");
    expect(dialog).toBeInTheDocument();
  });

  it("shows a network failure and keeps the dialog open", async () => {
    server.use(http.patch("*/api/v1/kodi/instances/1", () => HttpResponse.error()));
    const user = userEvent.setup();
    renderApp("/kodi");

    await user.click((await card("Lounge")).getByRole("button", { name: "Edit Lounge" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Lounge" });
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    expect(await within(dialog).findByRole("alert")).not.toBeEmptyDOMElement();
    expect(dialog).toBeInTheDocument();
  });
});
