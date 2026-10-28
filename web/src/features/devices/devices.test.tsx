import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { HttpResponse, http } from "msw";
import { describe, expect, it } from "vitest";

import { keys } from "@api/keys";
import i18n from "@i18n/index";
import { getDb } from "@mocks/handlers";
import { server } from "@mocks/node";
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
