import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { keys } from "@api/keys";
import i18n from "@i18n/index";
import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
import { FakeEventSource } from "@test/eventSource";
import { failWith, recordRequests, renderApp } from "@test/utils";

describe("Echo devices page", () => {
  it("lists Echos, the unlinked banner and rooms", async () => {
    renderApp("/echo-devices");

    expect(await screen.findByRole("heading", { level: 1, name: "Echo devices and rooms" })).toBeInTheDocument();
    const table = await screen.findByRole("table", { name: "Devices" });
    expect(within(table).getByText("Lounge Echo Show")).toBeInTheDocument();
    expect(within(table).getByText("amzn1.ask.device.AFQ…2LM")).toBeInTheDocument();
    expect(within(table).getByText("Not linked")).toBeInTheDocument();

    const banner = screen.getByRole("region", { name: "New Echo heard: not linked to a Kodi yet" });
    expect(await within(banner).findByText(/“pause”/)).toBeInTheDocument();

    expect(await screen.findByRole("list", { name: "Spoken names for Lounge" })).toHaveTextContent("living room");
  });

  it("tells you to ask the skill by the invocation name chosen", async () => {
    renderApp("/echo-devices");
    expect(await screen.findByText(/Alexa, ask encore which device this is/)).toBeInTheDocument();
  });

  it("uses kodi in the hint when kodi was chosen", async () => {
    getDb().skill = { ...getDb().skill, invocation: "kodi", invocation_name: "kodi" };
    renderApp("/echo-devices");
    expect(await screen.findByText(/Alexa, ask kodi which device this is/)).toBeInTheDocument();
  });

  it("doesn't guess the invocation name in the hint before the skill settings load", async () => {
    getDb().skill = { ...getDb().skill, invocation: "kodi", invocation_name: "kodi" };
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
    renderApp("/echo-devices");

    await screen.findByRole("table", { name: "Devices" });
    await waitFor(() => expect(answer).toBeDefined());
    expect(screen.queryByText(/Alexa, ask encore/)).not.toBeInTheDocument();

    answer?.();
    expect(await screen.findByText(/Alexa, ask kodi which device this is/)).toBeInTheDocument();
  });

  it("links an unlinked Echo and removes the banner", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");
    const banner = await screen.findByRole("region", {
      name: "New Echo heard: not linked to a Kodi yet",
    });

    await user.type(within(banner).getByRole("textbox", { name: "Device name" }), "Kitchen Dot");
    await user.selectOptions(within(banner).getByRole("combobox", { name: "Controls Kodi" }), "1");
    await user.click(within(banner).getByRole("button", { name: "Link device" }));

    await waitFor(() =>
      expect(
        screen.queryByRole("region", { name: "New Echo heard: not linked to a Kodi yet" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("table", { name: "Devices" })).toHaveTextContent("Kitchen Dot");
    const patch = log.find((r) => r.method === "PATCH" && r.path === "/devices/5");
    expect(patch?.csrf).toBe("mock-csrf-token");
    expect(patch?.body).toEqual({ kodi_instance_id: 1, name: "Kitchen Dot" });
    expect(getDb().devices.find((d) => d.id === 5)?.kodi_instance_id).toBe(1);
  });

  it("adds a spoken name and explains a duplicate", async () => {
    const user = userEvent.setup();
    renderApp("/echo-devices");
    const input = await screen.findByRole("textbox", { name: "Add a spoken name for Lounge" });

    await user.type(input, "telly");
    await user.click(screen.getByRole("button", { name: "Add spoken name to Lounge" }));
    expect(
      await within(screen.getByRole("list", { name: "Spoken names for Lounge" })).findByText("telly"),
    ).toBeInTheDocument();

    await user.type(input, "bedroom");
    await user.click(screen.getByRole("button", { name: "Add spoken name to Lounge" }));
    expect(await screen.findByText("“bedroom” is already used by Bedroom.")).toBeInTheDocument();
  });

  it("edits an Echo's room and Kodi instance", async () => {
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Study Echo Spot" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Study Echo Spot" });
    await user.selectOptions(within(dialog).getByRole("combobox", { name: "Controls Kodi" }), "2");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(getDb().devices.find((d) => d.id === 4)?.kodi_instance_id).toBe(2));
  });

  it("opens each Echo with its own values, not ones left from the last", async () => {
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Study Echo Spot" }));
    let dialog = await screen.findByRole("dialog", { name: "Edit Study Echo Spot" });
    const name = within(dialog).getByRole("textbox", { name: "Name" });
    await user.clear(name);
    await user.type(name, "Not saved");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await user.click(screen.getByRole("button", { name: "Edit Lounge Echo Show" }));
    dialog = await screen.findByRole("dialog", { name: "Edit Lounge Echo Show" });
    expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveValue("Lounge Echo Show");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await user.click(screen.getByRole("button", { name: "Edit Study Echo Spot" }));
    dialog = await screen.findByRole("dialog", { name: "Edit Study Echo Spot" });
    expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveValue("Study Echo Spot");
  });

  it("reopens a room with its saved name after a cancelled edit", async () => {
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit room Study" }));
    let dialog = await screen.findByRole("dialog");
    const name = within(dialog).getByRole("textbox", { name: "Name" });
    await user.clear(name);
    await user.type(name, "Office");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await user.click(screen.getByRole("button", { name: "Edit room Study" }));
    dialog = await screen.findByRole("dialog");
    expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveValue("Study");
  });

  it("shows toasts in the interface language", async () => {
    i18n.addResourceBundle("xx", "common", { toast: { failed: "Nope", roomAdded: "Room made." } });
    await i18n.changeLanguage("xx");
    failWith("delete", "/devices/1", 500, "boom");
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Add room" }));
    const dialog = await screen.findByRole("dialog");
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "Kitchen");
    await user.click(within(dialog).getByRole("button", { name: "Add room" }));
    expect((await screen.findAllByText("Room made.")).length).toBeGreaterThan(0);

    await user.click(screen.getByRole("button", { name: "Edit Lounge Echo Show" }));
    await user.click(await screen.findByRole("button", { name: "Forget device…" }));
    await user.click(screen.getByRole("button", { name: "Forget device" }));
    expect((await screen.findAllByText("Nope")).length).toBeGreaterThan(0);
  });

  it("waits for the Kodi instances before saying which Kodi each Echo controls", async () => {
    let answer: () => void = () => {};
    server.use(
      http.get(
        "*/api/v1/kodi/instances",
        () =>
          new Promise<Response>((resolve) => {
            answer = () => resolve(HttpResponse.json(getDb().instances));
          }),
      ),
    );
    const { queryClient } = renderApp("/echo-devices");

    // The Echos and rooms are in; the Kodi instances aren't yet.
    await waitFor(() => expect(queryClient.getQueryState(keys.devices)?.status).toBe("success"));
    await waitFor(() => expect(queryClient.getQueryState(keys.rooms)?.status).toBe("success"));
    expect(screen.queryByText("Not linked")).not.toBeInTheDocument();
    expect(screen.queryByText("→ no Kodi instance")).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Loading…");

    answer();
    const table = await screen.findByRole("table", { name: "Devices" });
    expect(within(table).getAllByText("Not linked")).toHaveLength(1);
    expect(within(table).getAllByText("Lounge", { selector: "span" })).toHaveLength(2);
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("says the Kodi instances didn't load instead of showing every Echo as not linked", async () => {
    const user = userEvent.setup();
    failWith("get", "/kodi/instances", 500, "boom");
    const { queryClient } = renderApp("/echo-devices");

    await waitFor(() => expect(queryClient.getQueryState(keys.instances)?.status).toBe("error"));
    await waitFor(() => expect(queryClient.getQueryState(keys.devices)?.status).toBe("success"));
    await waitFor(() => expect(queryClient.getQueryState(keys.rooms)?.status).toBe("success"));
    expect(screen.queryByText("Not linked")).not.toBeInTheDocument();
    expect(screen.queryByText("→ no Kodi instance")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("boom");

    server.resetHandlers();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    const table = await screen.findByRole("table", { name: "Devices" });
    expect(within(table).getAllByText("Not linked")).toHaveLength(1);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

describe("Echo dialog", () => {
  it("unlinks an Echo from its room and Kodi and keeps its name when the name is cleared", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Study Echo Spot" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Study Echo Spot" });
    expect(within(dialog).getByRole("combobox", { name: "Room" })).toHaveValue("4");
    await user.clear(within(dialog).getByRole("textbox", { name: "Name" }));
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "   ");
    await user.selectOptions(within(dialog).getByRole("combobox", { name: "Room" }), "No room");
    await user.selectOptions(within(dialog).getByRole("combobox", { name: "Controls Kodi" }), "Not linked");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.find((r) => r.method === "PATCH" && r.path === "/devices/4")?.body).toEqual({
      kodi_instance_id: null,
      room_id: null,
    });
    expect(getDb().devices.find((d) => d.id === 4)).toMatchObject({
      kodi_instance_id: null,
      name: "Study Echo Spot",
      room_id: null,
    });
  });

  it("trims the new name before saving", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Study Echo Spot" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Study Echo Spot" });
    const name = within(dialog).getByRole("textbox", { name: "Name" });
    await user.clear(name);
    await user.type(name, "  Office Spot  ");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.find((r) => r.method === "PATCH")?.body).toEqual({
      kodi_instance_id: 1,
      name: "Office Spot",
      room_id: 4,
    });
    expect(screen.getByRole("table", { name: "Devices" })).toHaveTextContent("Office Spot");
  });

  it("titles an unnamed Echo generically and starts with no room and no Kodi", async () => {
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Unnamed Echo" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Echo" });
    expect(within(dialog).getByText("amzn1.ask.device.AEX…7QK")).toBeInTheDocument();
    expect(within(dialog).getByRole("textbox", { name: "Name" })).toHaveValue("");
    expect(within(dialog).getByRole("combobox", { name: "Room" })).toHaveValue("");
    expect(within(dialog).getByRole("combobox", { name: "Controls Kodi" })).toHaveValue("");
  });

  it("shows a failed save in the dialog, keeps it open, and forgets the error once closed", async () => {
    failWith("patch", "/devices/1", 500, "Database is locked.");
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Lounge Echo Show" }));
    let dialog = await screen.findByRole("dialog", { name: "Edit Lounge Echo Show" });
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Database is locked.");

    await user.keyboard("{Escape}");
    await waitFor(() => expect(dialog).not.toBeInTheDocument());

    await user.click(screen.getByRole("button", { name: "Edit Lounge Echo Show" }));
    dialog = await screen.findByRole("dialog", { name: "Edit Lounge Echo Show" });
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("doesn't carry a failed save over to the next Echo after Cancel", async () => {
    failWith("patch", "/devices/1", 500, "Database is locked.");
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Lounge Echo Show" }));
    let dialog = await screen.findByRole("dialog", { name: "Edit Lounge Echo Show" });
    await user.click(within(dialog).getByRole("button", { name: "Save" }));
    expect(await within(dialog).findByRole("alert")).toHaveTextContent("Database is locked.");

    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(dialog).not.toBeInTheDocument());

    await user.click(screen.getByRole("button", { name: "Edit Study Echo Spot" }));
    dialog = await screen.findByRole("dialog", { name: "Edit Study Echo Spot" });
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("asks before forgetting an Echo, and keeps it when you change your mind", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Bedroom Echo" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Bedroom Echo" });
    await user.click(within(dialog).getByRole("button", { name: "Forget device…" }));
    expect(within(dialog).getByText(/Forget this Echo\?/)).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: "Save" })).not.toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "Keep it" }));
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeInTheDocument();
    expect(log.some((r) => r.method === "DELETE")).toBe(false);

    await user.click(within(dialog).getByRole("button", { name: "Forget device…" }));
    await user.click(within(dialog).getByRole("button", { name: "Forget device" }));
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.find((r) => r.method === "DELETE")?.path).toBe("/devices/2");
    await waitFor(() => expect(screen.getByRole("table", { name: "Devices" })).not.toHaveTextContent("Bedroom Echo"));
  });

  it("starts each opening with the confirmation dismissed", async () => {
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit Bedroom Echo" }));
    let dialog = await screen.findByRole("dialog", { name: "Edit Bedroom Echo" });
    await user.click(within(dialog).getByRole("button", { name: "Forget device…" }));
    await user.click(within(dialog).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(dialog).not.toBeInTheDocument());

    await user.click(screen.getByRole("button", { name: "Edit Lounge Echo Show" }));
    dialog = await screen.findByRole("dialog", { name: "Edit Lounge Echo Show" });
    expect(within(dialog).getByRole("button", { name: "Forget device…" })).toBeInTheDocument();
  });
});

describe("Rooms", () => {
  it("says there are no rooms yet", async () => {
    getDb().rooms = [];
    renderApp("/echo-devices");
    expect(await screen.findByText("No rooms yet.")).toBeInTheDocument();
  });

  it("shows which Kodi instance each room plays on, or that it has none", async () => {
    getDb().rooms = [
      { aliases: [], id: 1, kodi_instance_id: 2, name: "Lounge" },
      { aliases: [], id: 2, kodi_instance_id: null, name: "Garage" },
      { aliases: [], id: 3, kodi_instance_id: 99, name: "Attic" },
    ];
    renderApp("/echo-devices");

    expect(await screen.findByText("→ Kodi “Bedroom”")).toBeInTheDocument();
    expect(screen.getAllByText("→ no Kodi instance")).toHaveLength(2);
  });

  it("removes a spoken name", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");
    const list = await screen.findByRole("list", { name: "Spoken names for Lounge" });

    await user.click(within(list).getByRole("button", { name: "Remove spoken name “big tv”" }));

    await waitFor(() => expect(list).not.toHaveTextContent("big tv"));
    expect(log.find((r) => r.method === "DELETE")?.path).toBe("/rooms/1/aliases/3");
    expect(getDb().rooms[0]?.aliases.map((a) => a.alias)).toEqual(["lounge", "living room"]);
  });

  it("ignores a blank spoken name and trims the one it sends", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");
    const input = await screen.findByRole("textbox", { name: "Add a spoken name for Study" });
    const add = screen.getByRole("button", { name: "Add spoken name to Study" });

    await user.type(input, "   ");
    await user.click(add);
    expect(log.some((r) => r.method === "POST")).toBe(false);

    await user.clear(input);
    await user.type(input, "  office  ");
    await user.click(add);
    await waitFor(() => expect(input).toHaveValue(""));
    expect(log.find((r) => r.method === "POST")?.body).toEqual({ alias: "office" });
  });

  it("clears a duplicate spoken name's error as soon as you edit it", async () => {
    const user = userEvent.setup();
    renderApp("/echo-devices");
    const input = await screen.findByRole("textbox", { name: "Add a spoken name for Study" });

    await user.type(input, "lounge");
    await user.click(screen.getByRole("button", { name: "Add spoken name to Study" }));
    const message = "“lounge” is already used by Lounge.";
    expect(await screen.findByText(message)).toBeInTheDocument();
    expect(input).toHaveAccessibleDescription(message);

    await user.type(input, "s");
    expect(screen.queryByText(message)).not.toBeInTheDocument();
  });

  it("adds a room that plays on a chosen Kodi instance", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Add room" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a room" });
    expect(within(dialog).queryByRole("button", { name: "Remove…" })).not.toBeInTheDocument();
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), " Kitchen ");
    await user.selectOptions(within(dialog).getByRole("combobox", { name: "Kodi instance" }), "Bedroom");
    await user.click(within(dialog).getByRole("button", { name: "Add room" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.find((r) => r.method === "POST" && r.path === "/rooms")?.body).toEqual({
      kodi_instance_id: 2,
      name: "Kitchen",
    });
    expect(await screen.findByRole("list", { name: "Spoken names for Kitchen" })).toBeInTheDocument();
  });

  it("adds a room without a Kodi instance", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Add room" }));
    const dialog = await screen.findByRole("dialog", { name: "Add a room" });
    await user.type(within(dialog).getByRole("textbox", { name: "Name" }), "Garage");
    await user.click(within(dialog).getByRole("button", { name: "Add room" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.find((r) => r.method === "POST" && r.path === "/rooms")?.body).toEqual({ name: "Garage" });
  });

  it("explains a rejected room name and forgets it after closing", async () => {
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Add room" }));
    let dialog = await screen.findByRole("dialog", { name: "Add a room" });
    await user.click(within(dialog).getByRole("button", { name: "Add room" }));
    expect(await within(dialog).findByText("Give the room a name.")).toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "Add room" }));
    dialog = await screen.findByRole("dialog", { name: "Add a room" });
    expect(within(dialog).queryByText("Give the room a name.")).not.toBeInTheDocument();
  });

  it("edits a room's name and clears its Kodi instance", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit room Study" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Study" });
    expect(within(dialog).getByRole("combobox", { name: "Kodi instance" })).toHaveValue("1");
    const name = within(dialog).getByRole("textbox", { name: "Name" });
    await user.clear(name);
    await user.type(name, "Office ");
    await user.selectOptions(within(dialog).getByRole("combobox", { name: "Kodi instance" }), "None");
    await user.click(within(dialog).getByRole("button", { name: "Save" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.find((r) => r.method === "PATCH" && r.path === "/rooms/4")?.body).toEqual({
      kodi_instance_id: null,
      name: "Office",
    });
    expect(await screen.findByRole("button", { name: "Edit room Office" })).toBeInTheDocument();
  });

  it("opens a room without a Kodi instance on None", async () => {
    getDb().rooms = [{ aliases: [], id: 9, kodi_instance_id: null, name: "Garage" }];
    const user = userEvent.setup();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit room Garage" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Garage" });
    expect(within(dialog).getByRole("combobox", { name: "Kodi instance" })).toHaveValue("");
  });

  it("asks before removing a room, and removes it once confirmed", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit room Study" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Study" });
    await user.click(within(dialog).getByRole("button", { name: "Remove…" }));
    expect(within(dialog).getByText(/Remove Study and its spoken names\?/)).toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Keep it" }));
    expect(within(dialog).getByRole("button", { name: "Save" })).toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "Remove…" }));
    await user.click(within(dialog).getByRole("button", { name: "Remove room" }));

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.find((r) => r.method === "DELETE")?.path).toBe("/rooms/4");
    await waitFor(() => expect(screen.queryByRole("button", { name: "Edit room Study" })).not.toBeInTheDocument());
  });

  it("closes the room dialog with Escape without saving", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(await screen.findByRole("button", { name: "Edit room Study" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Study" });
    await user.keyboard("{Escape}");

    await waitFor(() => expect(dialog).not.toBeInTheDocument());
    expect(log.some((r) => r.method !== "GET")).toBe(false);
  });
});

describe("Unlinked Echo banner", () => {
  const banner = () => screen.findByRole("region", { name: "New Echo heard: not linked to a Kodi yet" });

  it("says the first request was today and quotes what was heard as plain text", async () => {
    const db = getDb();
    db.devices = db.devices.map((d) => (d.id === 5 ? { ...d, first_seen: new Date().toISOString() } : d));
    db.requests = db.requests.map((r) => (r.device_id === 5 ? { ...r, heard: "<mono>Bluey</mono> & co" } : r));
    renderApp("/echo-devices");

    const region = await banner();
    await waitFor(() => expect(region).toHaveTextContent("today: “<mono>Bluey</mono> & co”"));
    expect(within(region).getByText("amzn1.ask.device.AEX…7QK")).toHaveClass("font-mono");
  });

  it("gives the day of the first request when it wasn't today", async () => {
    const db = getDb();
    db.devices = db.devices.map((d) => (d.id === 5 ? { ...d, first_seen: "2026-01-02T03:04:05Z" } : d));
    renderApp("/echo-devices");

    const region = await banner();
    await waitFor(() => expect(region).toHaveTextContent(/First request 2 Jan: “pause”/));
    expect(region).not.toHaveTextContent("today");
  });

  it("leaves out what was heard while it isn't known", async () => {
    const db = getDb();
    db.requests = db.requests.filter((r) => r.device_id !== 5);
    renderApp("/echo-devices");

    const region = await banner();
    expect(region).toHaveTextContent(/First request .*\. Device amzn1/);
    expect(region).not.toHaveTextContent("“");
    expect(region).not.toHaveTextContent("undefined");
  });

  it("links an Echo to the first Kodi instance without renaming it", async () => {
    const user = userEvent.setup();
    const log = recordRequests();
    renderApp("/echo-devices");

    await user.click(within(await banner()).getByRole("button", { name: "Link device" }));

    await waitFor(() => expect(log.find((r) => r.method === "PATCH")?.body).toEqual({ kodi_instance_id: 1 }));
    expect((await screen.findAllByText("Echo linked.")).length).toBeGreaterThan(0);
  });

  it("can't link an Echo while there's no Kodi instance", async () => {
    getDb().instances = [];
    renderApp("/echo-devices");

    const region = await banner();
    expect(within(region).queryAllByRole("option")).toHaveLength(0);
    expect(within(region).getByRole("button", { name: "Link device" })).toBeDisabled();
  });

  it("shows why linking failed and keeps the banner", async () => {
    failWith("patch", "/devices/5", 409, "That Kodi instance was removed.");
    const user = userEvent.setup();
    renderApp("/echo-devices");

    const region = await banner();
    await user.click(within(region).getByRole("button", { name: "Link device" }));

    expect(await within(region).findByRole("alert")).toHaveTextContent("That Kodi instance was removed.");
    expect(region).toBeInTheDocument();
  });

  it("highlights the Echo you just asked “which device is this?”", async () => {
    renderApp("/echo-devices");
    await banner();

    act(() => FakeEventSource.emit("request", { device_id: 5, intent: "IdentifyEcho" }));

    expect(
      await screen.findByRole("region", {
        name: "This is the Echo you just spoke to: not linked to a Kodi yet",
      }),
    ).toBeInTheDocument();
    expect(screen.getByText("You just spoke to an unnamed Echo.")).toBeInTheDocument();
    expect(screen.getByText("This is the Echo you just spoke to")).toBeInTheDocument();
  });
});
