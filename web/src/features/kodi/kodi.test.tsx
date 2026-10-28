import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { FakeEventSource } from "@test/eventSource";
import { recordRequests, renderApp } from "@test/utils";

async function card(name: string) {
  return within(await screen.findByRole("article", { name }));
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
});
